package app

import (
	"errors"
	"path/filepath"
	"poolx/internal/model"
	"poolx/internal/pkg/common"
	"poolx/internal/pkg/utils/security"
	"testing"
)

// resetPasswordTestDB reopens the model database against a fresh SQLite file
// and returns a cleanup that closes it.
func resetPasswordTestDB(t *testing.T) {
	t.Helper()
	common.SQLitePath = filepath.Join(t.TempDir(), "reset-test.db")
	common.SQLDSN = ""
	if err := model.InitDB(); err != nil {
		t.Fatalf("init test database: %v", err)
	}
	t.Cleanup(func() {
		_ = model.CloseDB()
	})
}

func injectPassword(t *testing.T, password string, err error) {
	t.Helper()
	original := readPasswordTwice
	readPasswordTwice = func() (string, error) { return password, err }
	t.Cleanup(func() { readPasswordTwice = original })
}

// reopenTestDB reopens the database after ResetAdminPasswordCLI closed it.
func reopenTestDB(t *testing.T) {
	t.Helper()
	if err := model.InitDB(); err != nil {
		t.Fatalf("reopen test database: %v", err)
	}
}

func TestResetAdminPasswordCLISuccess(t *testing.T) {
	resetPasswordTestDB(t)

	var root model.User
	if err := model.DB.Where("role = ?", common.RoleRootUser).First(&root).Error; err != nil {
		t.Fatalf("load initial root user: %v", err)
	}
	oldHash := root.Password
	injectPassword(t, "new-admin-pass", nil)

	if err := ResetAdminPasswordCLI(); err != nil {
		t.Fatalf("reset admin password: %v", err)
	}
	reopenTestDB(t)

	if err := model.DB.Where("role = ?", common.RoleRootUser).First(&root).Error; err != nil {
		t.Fatalf("reload root user: %v", err)
	}
	if !root.MustChangePassword {
		t.Fatalf("reset did not set must_change_password: %+v", root)
	}
	if !security.ValidatePasswordAndHash("new-admin-pass", root.Password) {
		t.Fatal("new password does not authenticate root")
	}
	if security.ValidatePasswordAndHash("old-password", oldHash) ||
		security.ValidatePasswordAndHash("old-password", root.Password) {
		t.Fatal("old password still authenticates root")
	}
	if root.AuthVersion <= 1 {
		t.Fatalf("reset did not revoke credentials: %+v", root)
	}
}

func TestResetAdminPasswordCLIPasswordMismatch(t *testing.T) {
	resetPasswordTestDB(t)
	injectPassword(t, "", errors.New("两次输入的密码不一致"))

	err := ResetAdminPasswordCLI()
	if err == nil {
		t.Fatal("expected mismatch error")
	}
	reopenTestDB(t)

	var root model.User
	if err := model.DB.Where("role = ?", common.RoleRootUser).First(&root).Error; err != nil {
		t.Fatalf("load root user: %v", err)
	}
	if root.AuthVersion != 1 {
		t.Fatalf("failed reset must not revoke credentials: %+v", root)
	}
}

func TestResetAdminPasswordCLIWeakPassword(t *testing.T) {
	resetPasswordTestDB(t)
	injectPassword(t, "short", nil)

	err := ResetAdminPasswordCLI()
	if err == nil {
		t.Fatal("expected weak password error")
	}
	reopenTestDB(t)

	var root model.User
	if err := model.DB.Where("role = ?", common.RoleRootUser).First(&root).Error; err != nil {
		t.Fatalf("load root user: %v", err)
	}
	if security.ValidatePasswordAndHash("short", root.Password) {
		t.Fatal("weak password must not be stored")
	}
}

func TestResetAdminPasswordCLINoAdminUser(t *testing.T) {
	resetPasswordTestDB(t)

	// A non-empty database with no root user: the reset must refuse instead of
	// silently recreating the bootstrap account.
	if err := model.DB.Where("1 = 1").Delete(&model.User{}).Error; err != nil {
		t.Fatalf("clear users: %v", err)
	}
	regular := model.User{
		Username:    "alice",
		Password:    "regular-pass",
		DisplayName: "Alice",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		AuthVersion: 1,
	}
	if err := regular.Insert(); err != nil {
		t.Fatalf("insert common user: %v", err)
	}
	injectPassword(t, "new-admin-pass", nil)

	err := ResetAdminPasswordCLI()
	if err == nil {
		t.Fatal("expected no-admin error")
	}
}
