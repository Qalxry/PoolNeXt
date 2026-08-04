package migrate

import "gorm.io/gorm"

// V6 persists ordered Mihomo-native routing rules.
func V6(hooks Hooks) Migration {
	validate := func(db *gorm.DB, backend string) error {
		return runNamedSteps(db, backend,
			NamedStep{Name: "validate_routing_rule_schema", Run: hooks.ValidateSchema},
			NamedStep{Name: "validate_user_identity_schema", Run: validateUserIdentitySchema},
		)
	}
	return Migration{
		FromVersion: 5,
		ToVersion:   6,
		Migrate: func(db *gorm.DB, backend string) error {
			return runNamedSteps(db, backend,
				NamedStep{Name: "apply_routing_rule_schema", Run: hooks.ApplySchema},
			)
		},
		Validate: validate,
	}
}
