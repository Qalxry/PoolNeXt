package migrate

import "gorm.io/gorm"

// V4 persists proxy-node health streaks and quarantine state used by the
// bounded background health checker and runtime node filtering.
func V4(hooks Hooks) Migration {
	validate := func(db *gorm.DB, backend string) error {
		return runNamedSteps(db, backend,
			NamedStep{Name: "validate_proxy_node_health_schema", Run: hooks.ValidateSchema},
			NamedStep{Name: "validate_user_identity_schema", Run: validateUserIdentitySchema},
		)
	}
	return Migration{
		FromVersion: 3,
		ToVersion:   4,
		Migrate: func(db *gorm.DB, backend string) error {
			return runNamedSteps(db, backend,
				NamedStep{Name: "apply_proxy_node_health_schema", Run: hooks.ApplySchema},
			)
		},
		Validate: validate,
	}
}
