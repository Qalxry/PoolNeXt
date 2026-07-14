package migrate

import (
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

const (
	usersEmailIdentityIndex  = "idx_users_email_identity_unique"
	usersGitHubIdentityIndex = "idx_users_github_identity_unique"
	usersWeChatIdentityIndex = "idx_users_wechat_identity_unique"
)

type userIdentityMigrationRow struct {
	ID       int    `gorm:"column:id"`
	Email    string `gorm:"column:email"`
	GitHubID string `gorm:"column:github_id"`
	WeChatID string `gorm:"column:wechat_id"`
}

type duplicateIdentityValue struct {
	Value string `gorm:"column:value"`
	Count int64  `gorm:"column:count"`
}

// V3 adds credential revocation, first-login password state, and database-level
// uniqueness for external identities. PoolX stored the mutable GitHub login in
// github_id, so every pre-V3 GitHub binding is cleared and must be rebound.
func V3(hooks Hooks) Migration {
	validate := func(db *gorm.DB, backend string) error {
		return runNamedSteps(db, backend,
			NamedStep{Name: "validate_auth_security_schema", Run: hooks.ValidateSchema},
			NamedStep{Name: "validate_user_identity_schema", Run: validateUserIdentitySchema},
		)
	}

	return Migration{
		FromVersion: 2,
		ToVersion:   3,
		Migrate: func(db *gorm.DB, backend string) error {
			return runNamedSteps(db, backend,
				NamedStep{Name: "apply_auth_security_schema", Run: hooks.ApplySchema},
				NamedStep{Name: "normalize_user_identities", Run: normalizeUserIdentities},
				NamedStep{Name: "ensure_user_identity_uniqueness", Run: ensureUserIdentityUniqueness},
				NamedStep{Name: "create_user_identity_indexes", Run: createUserIdentityIndexes},
			)
		},
		Validate: validate,
	}
}

func normalizeUserIdentities(db *gorm.DB, backend string) error {
	_ = backend
	var users []userIdentityMigrationRow
	if err := db.Table("users").Select("id", "email", "github_id", "wechat_id").Find(&users).Error; err != nil {
		return err
	}
	for _, user := range users {
		updates := map[string]any{}
		email := strings.ToLower(strings.TrimSpace(user.Email))
		if email != user.Email {
			updates["email"] = email
		}
		// Every value written by PoolX was a mutable login. Clearing all legacy
		// values avoids trusting a numeric-looking login as a stable account ID.
		if user.GitHubID != "" {
			updates["github_id"] = ""
		}
		wechatID := strings.TrimSpace(user.WeChatID)
		if wechatID != user.WeChatID {
			updates["wechat_id"] = wechatID
		}
		if len(updates) == 0 {
			continue
		}
		if err := db.Table("users").Where("id = ?", user.ID).Updates(updates).Error; err != nil {
			return fmt.Errorf("normalize identities for user %d: %w", user.ID, err)
		}
	}
	return nil
}

func ensureUserIdentityUniqueness(db *gorm.DB, backend string) error {
	_ = backend
	for _, column := range []string{"email", "github_id", "wechat_id"} {
		duplicate, err := findDuplicateIdentity(db, column)
		if err != nil {
			return err
		}
		if duplicate.Count > 1 {
			return fmt.Errorf("users.%s contains duplicate non-empty identity %q (%d rows); resolve it before migration", column, duplicate.Value, duplicate.Count)
		}
	}
	return nil
}

func findDuplicateIdentity(db *gorm.DB, column string) (duplicateIdentityValue, error) {
	allowed := map[string]bool{"email": true, "github_id": true, "wechat_id": true}
	if !allowed[column] {
		return duplicateIdentityValue{}, fmt.Errorf("unsupported identity column %q", column)
	}
	var duplicate duplicateIdentityValue
	query := fmt.Sprintf(
		"SELECT %s AS value, COUNT(*) AS count FROM users WHERE %s <> '' GROUP BY %s HAVING COUNT(*) > 1 LIMIT 1",
		column,
		column,
		column,
	)
	if err := db.Raw(query).Scan(&duplicate).Error; err != nil {
		return duplicateIdentityValue{}, err
	}
	return duplicate, nil
}

func createUserIdentityIndexes(db *gorm.DB, backend string) error {
	_ = backend
	statements := []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS " + usersEmailIdentityIndex + " ON users (email) WHERE email <> ''",
		"CREATE UNIQUE INDEX IF NOT EXISTS " + usersGitHubIdentityIndex + " ON users (github_id) WHERE github_id <> ''",
		"CREATE UNIQUE INDEX IF NOT EXISTS " + usersWeChatIdentityIndex + " ON users (wechat_id) WHERE wechat_id <> ''",
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

func validateUserIdentitySchema(db *gorm.DB, backend string) error {
	_ = backend
	for _, index := range []string{usersEmailIdentityIndex, usersGitHubIdentityIndex, usersWeChatIdentityIndex} {
		if !db.Migrator().HasIndex("users", index) {
			return fmt.Errorf("required user identity index %s is missing", index)
		}
	}
	if err := ensureUserIdentityUniqueness(db, backend); err != nil {
		return err
	}

	var identities []string
	if err := db.Table("users").Where("github_id <> ''").Pluck("github_id", &identities).Error; err != nil {
		return err
	}
	for _, identity := range identities {
		parsed, err := strconv.ParseInt(identity, 10, 64)
		if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != identity {
			return fmt.Errorf("users.github_id contains non-canonical numeric identity %q", identity)
		}
	}
	return nil
}
