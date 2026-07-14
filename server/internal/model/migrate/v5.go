package migrate

import "gorm.io/gorm"

// V5 adds structured event metadata to bounded application logs.
func V5(hooks Hooks) Migration {
	validate := func(db *gorm.DB, backend string) error {
		return runNamedSteps(db, backend,
			NamedStep{Name: "validate_structured_app_log_schema", Run: hooks.ValidateSchema},
			NamedStep{Name: "validate_user_identity_schema", Run: validateUserIdentitySchema},
		)
	}
	return Migration{
		FromVersion: 4,
		ToVersion:   5,
		Migrate: func(db *gorm.DB, backend string) error {
			return runNamedSteps(db, backend,
				NamedStep{Name: "apply_structured_app_log_schema", Run: hooks.ApplySchema},
			)
		},
		Validate: validate,
	}
}
