package model

import (
	"os"
	migratepkg "poolx/internal/model/migrate"
	"poolx/internal/pkg/common"
	"poolx/internal/pkg/utils/security"
	"strings"
	"testing"
)

type legacyAuthUserV2 struct {
	Id          int    `gorm:"primaryKey;autoIncrement"`
	Username    string `gorm:"unique;index"`
	Password    string `gorm:"not null"`
	DisplayName string
	Role        int
	Status      int
	Token       string
	Email       string
	GitHubId    string `gorm:"column:github_id"`
	WeChatId    string `gorm:"column:wechat_id"`
}

func (legacyAuthUserV2) TableName() string { return "users" }

func withTestModelDB(t *testing.T) {
	t.Helper()
	previousDB := DB
	DB = openTestSQLiteDB(t, "user-security.db")
	t.Cleanup(func() {
		DB = previousDB
	})
}

func bootstrapPasswordFromFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bootstrap credential file: %v", err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		if password, ok := strings.CutPrefix(line, "password: "); ok {
			return password
		}
	}
	t.Fatal("bootstrap password was not present in credential file")
	return ""
}

func TestCreateRootAccountUsesOneTimeCredentialFile(t *testing.T) {
	withTestModelDB(t)
	credentialPath := t.TempDir() + "/bootstrap.txt"
	t.Setenv(bootstrapCredentialFileEnv, credentialPath)

	if err := createRootAccountIfNeed(); err != nil {
		t.Fatalf("createRootAccountIfNeed: %v", err)
	}

	password := bootstrapPasswordFromFile(t, credentialPath)
	if password == "123456" || len(password) < 16 {
		t.Fatalf("bootstrap password is weak: %q", password)
	}
	info, err := os.Stat(credentialPath)
	if err != nil {
		t.Fatalf("stat bootstrap credential file: %v", err)
	}
	if permissions := info.Mode().Perm(); permissions != 0o600 {
		t.Fatalf("unexpected bootstrap credential permissions: %o", permissions)
	}

	var root User
	if err := DB.Where("username = ?", "root").First(&root).Error; err != nil {
		t.Fatalf("load root user: %v", err)
	}
	if !root.MustChangePassword || root.AuthVersion != 1 {
		t.Fatalf("unexpected root security state: %+v", root)
	}
	if !security.ValidatePasswordAndHash(password, root.Password) {
		t.Fatal("credential file password does not authenticate root")
	}
	if security.ValidatePasswordAndHash("123456", root.Password) {
		t.Fatal("legacy fixed password still authenticates root")
	}

	root.Password = "A-new-root-password"
	if err := root.UpdateWithPasswordPolicy(true, false); err != nil {
		t.Fatalf("change bootstrap root password: %v", err)
	}
	if _, err := os.Stat(credentialPath); !os.IsNotExist(err) {
		t.Fatalf("bootstrap credential file should be removed, stat err=%v", err)
	}
	if root.MustChangePassword || root.AuthVersion != 2 {
		t.Fatalf("password change did not update security state: %+v", root)
	}
}

func TestSensitiveUserChangesRevokeSessionsAndTokens(t *testing.T) {
	withTestModelDB(t)
	user := User{
		Username:    "alice",
		Password:    "old-password",
		DisplayName: "Alice",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Token:       "v1.existing-token",
		AuthVersion: 1,
	}
	if err := user.Insert(); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if ValidateUserToken("Bearer v1.existing-token") == nil {
		t.Fatal("expected original token to be valid")
	}

	user.Status = common.UserStatusDisabled
	if err := user.Update(false); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	if user.AuthVersion != 2 || user.Token != "" {
		t.Fatalf("disable did not revoke credentials: %+v", user)
	}
	if ValidateUserToken("Bearer v1.existing-token") != nil {
		t.Fatal("revoked token remained valid")
	}
}

func TestPasswordResetRevokesCredentialsAndRequiresAnotherChange(t *testing.T) {
	withTestModelDB(t)
	user := User{
		Username:    "bob",
		Password:    "old-password",
		DisplayName: "Bob",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Token:       "legacy-token",
		Email:       "bob@example.com",
		AuthVersion: 1,
	}
	if err := user.Insert(); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := ResetUserPasswordByEmail(user.Email, "temporary-password"); err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if err := DB.First(&user, user.Id).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if user.AuthVersion != 2 || user.Token != "" || !user.MustChangePassword {
		t.Fatalf("unexpected password reset security state: %+v", user)
	}
}

func TestSchemaV3MigratesExistingUsersForCredentialRevocation(t *testing.T) {
	db := openBareTestSQLiteDB(t, "auth-v2.db")
	if err := db.AutoMigrate(&legacyAuthUserV2{}, &DatabaseSchemaVersion{}); err != nil {
		t.Fatalf("create v2 schema: %v", err)
	}
	if err := db.Create(&legacyAuthUserV2{
		Username:    "legacy",
		Password:    "hashed",
		DisplayName: "Legacy",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Email:       " Legacy@Example.COM ",
		GitHubId:    "octocat",
		WeChatId:    " wechat-legacy ",
	}).Error; err != nil {
		t.Fatalf("seed v2 user: %v", err)
	}
	_, saveVersion := migratepkg.NewSchemaVersionStore((&DatabaseSchemaVersion{}).TableName(), databaseSchemaVersionRowID)
	if err := saveVersion(db, 2); err != nil {
		t.Fatalf("record v2 schema version: %v", err)
	}

	if err := ensureDatabaseSchemaUpToDate(db, "sqlite"); err != nil {
		t.Fatalf("upgrade v2 schema: %v", err)
	}
	if !db.Migrator().HasColumn(&User{}, "auth_version") || !db.Migrator().HasColumn(&User{}, "must_change_password") {
		t.Fatal("v3 user security columns were not added")
	}
	var user User
	if err := db.Where("username = ?", "legacy").First(&user).Error; err != nil {
		t.Fatalf("load migrated user: %v", err)
	}
	if user.AuthVersion != 1 || user.MustChangePassword {
		t.Fatalf("unexpected migrated security defaults: %+v", user)
	}
	if user.Email != "legacy@example.com" || user.GitHubId != "" || user.WeChatId != "wechat-legacy" {
		t.Fatalf("unexpected migrated identity values: %+v", user)
	}

	for _, identity := range []struct {
		name   string
		column string
		value  string
	}{
		{name: "email", column: "email", value: "legacy@example.com"},
		{name: "github", column: "github_id", value: "123456"},
		{name: "wechat", column: "wechat_id", value: "wechat-unique"},
	} {
		first := User{Username: identity.name + "1", Password: "hashed", DisplayName: "First", Role: 1, Status: 1}
		second := User{Username: identity.name + "2", Password: "hashed", DisplayName: "Second", Role: 1, Status: 1}
		if identity.column != "email" {
			if err := db.Create(&first).Error; err != nil {
				t.Fatalf("seed first %s identity user: %v", identity.name, err)
			}
			if err := db.Model(&first).Update(identity.column, identity.value).Error; err != nil {
				t.Fatalf("assign first %s identity: %v", identity.name, err)
			}
		}
		if err := db.Create(&second).Error; err != nil {
			t.Fatalf("seed second %s identity user: %v", identity.name, err)
		}
		if err := db.Model(&second).Update(identity.column, identity.value).Error; err == nil {
			t.Fatalf("duplicate %s identity was accepted", identity.name)
		}
	}

	blankOne := User{Username: "blank1", Password: "hashed", DisplayName: "Blank 1", Role: 1, Status: 1}
	blankTwo := User{Username: "blank2", Password: "hashed", DisplayName: "Blank 2", Role: 1, Status: 1}
	if err := db.Create(&blankOne).Error; err != nil {
		t.Fatalf("insert first blank identity user: %v", err)
	}
	if err := db.Create(&blankTwo).Error; err != nil {
		t.Fatalf("insert second blank identity user: %v", err)
	}
}

func TestSchemaV3ValidationRejectsMissingIdentityIndexes(t *testing.T) {
	db := openBareTestSQLiteDB(t, "auth-v3-missing-indexes.db")
	if err := migratepkg.ComposeAutoMigrator(schemaMetadataModels()...)(db); err != nil {
		t.Fatalf("create schema metadata: %v", err)
	}
	if err := migratepkg.ComposeAutoMigrator(registeredModels()...)(db); err != nil {
		t.Fatalf("create current business schema: %v", err)
	}
	_, saveVersion := migratepkg.NewSchemaVersionStore((&DatabaseSchemaVersion{}).TableName(), databaseSchemaVersionRowID)
	if err := saveVersion(db, currentDatabaseSchemaVersion); err != nil {
		t.Fatalf("record v3 schema version: %v", err)
	}

	err := ensureDatabaseSchemaUpToDate(db, "sqlite")
	if err == nil {
		t.Fatal("expected v3 schema without identity indexes to be rejected")
	}
	if !strings.Contains(err.Error(), "identity index") {
		t.Fatalf("unexpected identity index validation error: %v", err)
	}
}

func TestSchemaV3RejectsDuplicateCanonicalIdentitiesWithoutAdvancingVersion(t *testing.T) {
	db := openBareTestSQLiteDB(t, "auth-v2-duplicate-identities.db")
	if err := db.AutoMigrate(&legacyAuthUserV2{}, &DatabaseSchemaVersion{}); err != nil {
		t.Fatalf("create v2 schema: %v", err)
	}
	users := []legacyAuthUserV2{
		{Username: "duplicate1", Password: "hashed", DisplayName: "One", Role: 1, Status: 1, Email: "Dup@Example.COM"},
		{Username: "duplicate2", Password: "hashed", DisplayName: "Two", Role: 1, Status: 1, Email: "dup@example.com"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed duplicate identities: %v", err)
	}
	loadVersion, saveVersion := migratepkg.NewSchemaVersionStore((&DatabaseSchemaVersion{}).TableName(), databaseSchemaVersionRowID)
	if err := saveVersion(db, 2); err != nil {
		t.Fatalf("record v2 schema version: %v", err)
	}

	err := ensureDatabaseSchemaUpToDate(db, "sqlite")
	if err == nil || !strings.Contains(err.Error(), "duplicate non-empty identity") {
		t.Fatalf("duplicate canonical identities were not rejected: %v", err)
	}
	version, exists, loadErr := loadVersion(db)
	if loadErr != nil {
		t.Fatalf("load schema version: %v", loadErr)
	}
	if !exists || version != 2 {
		t.Fatalf("failed identity migration advanced schema version: exists=%v version=%d", exists, version)
	}
}
