package model

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	migratepkg "poolx/internal/model/migrate"
	"poolx/internal/pkg/common"
	"poolx/internal/pkg/utils/security"
	"reflect"
	"strings"
	"sync"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var DB *gorm.DB

const bootstrapCredentialFileEnv = "POOLNEXT_BOOTSTRAP_FILE"

type dbModel struct {
	value     any
	tableName string
	hasIDPK   bool
}

func registeredModels() []any {
	return []any{
		&AppLog{},
		&File{},
		&User{},
		&Option{},
		&SourceConfig{},
		&ProxyNode{},
		&PortProfile{},
		&PortProfileTemplate{},
		&PortProfileNode{},
		&RoutingRule{},
		&RuntimeConfig{},
		&KernelInstance{},
	}
}

func schemaMetadataModels() []any {
	return []any{
		&DatabaseSchemaVersion{},
	}
}

func buildDBModels() ([]dbModel, error) {
	models := registeredModels()
	result := make([]dbModel, 0, len(models))
	namer := schema.NamingStrategy{}
	cache := &sync.Map{}
	for _, item := range models {
		parsed, err := schema.Parse(item, cache, namer)
		if err != nil {
			return nil, err
		}
		hasIDPK := len(parsed.PrimaryFields) == 1 && parsed.PrimaryFields[0].DBName == "id"
		result = append(result, dbModel{
			value:     item,
			tableName: parsed.Table,
			hasIDPK:   hasIDPK,
		})
	}
	return result, nil
}

func createRootAccountIfNeed() error {
	var user User
	err := DB.First(&user).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("check existing users: %w", err)
	}

	oneTimePassword := security.GenerateRandomString(20)
	if oneTimePassword == "" {
		return errors.New("generate bootstrap root password failed")
	}
	credentialPath, err := writeBootstrapCredentialFile("root", oneTimePassword)
	if err != nil {
		return err
	}
	rootUser := User{
		Username:           "root",
		Password:           oneTimePassword,
		Role:               common.RoleRootUser,
		Status:             common.UserStatusEnabled,
		DisplayName:        "Root User",
		AuthVersion:        1,
		MustChangePassword: true,
	}
	if err := rootUser.Insert(); err != nil {
		_ = removeBootstrapCredentialFile()
		return fmt.Errorf("create bootstrap root user: %w", err)
	}
	slog.Warn(
		"initial root account created; change the one-time password immediately",
		"username", rootUser.Username,
		"bootstrap_credentials_file", credentialPath,
	)
	return nil
}

func bootstrapCredentialFilePath() (string, error) {
	configured := strings.TrimSpace(os.Getenv(bootstrapCredentialFileEnv))
	if configured != "" {
		return filepath.Abs(configured)
	}
	baseDir := "."
	if common.SQLDSN == "" && common.SQLitePath != "" {
		baseDir = filepath.Dir(common.SQLitePath)
	}
	return filepath.Abs(filepath.Join(baseDir, "poolnext-bootstrap.txt"))
}

func writeBootstrapCredentialFile(username string, password string) (string, error) {
	path, err := bootstrapCredentialFilePath()
	if err != nil {
		return "", fmt.Errorf("resolve bootstrap credential path: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("bootstrap credential file already exists at %s; remove it only after confirming the root account state", path)
		}
		return "", fmt.Errorf("create bootstrap credential file: %w", err)
	}
	removeOnFailure := true
	defer func() {
		_ = file.Close()
		if removeOnFailure {
			_ = os.Remove(path)
		}
	}()
	content := fmt.Sprintf(
		"PoolNeXt one-time bootstrap credentials\nusername: %s\npassword: %s\n\nSign in and change this password immediately. This file is removed after the first password change.\n",
		username,
		password,
	)
	if _, err := file.WriteString(content); err != nil {
		return "", fmt.Errorf("write bootstrap credential file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("sync bootstrap credential file: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close bootstrap credential file: %w", err)
	}
	removeOnFailure = false
	return path, nil
}

func removeBootstrapCredentialFile() error {
	path, err := bootstrapCredentialFilePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func ensureUserSecurityDefaults() error {
	return DB.Model(&User{}).Where("auth_version = 0").Update("auth_version", 1).Error
}

func CountTable(tableName string) (num int64) {
	DB.Table(tableName).Count(&num)
	return
}

func openDatabase() (*gorm.DB, string, error) {
	if common.SQLDSN != "" {
		db, err := gorm.Open(postgres.Open(common.SQLDSN), &gorm.Config{})
		if err != nil {
			return nil, "", err
		}
		return db, "postgres", nil
	}
	db, err := gorm.Open(sqlite.Open(common.SQLitePath), &gorm.Config{})
	if err != nil {
		return nil, "", err
	}
	slog.Info("database DSN not set, using SQLite as database", "sqlite_path", common.SQLitePath)
	return db, "sqlite", nil
}

func migrationHooks() migratepkg.Hooks {
	requiredModels := registeredModels()
	return migratepkg.Hooks{
		ApplySchema: migratepkg.ComposeSchemaApplier(
			migratepkg.ComposeAutoMigrator(schemaMetadataModels()...),
			migratepkg.ComposeAutoMigrator(requiredModels...),
		),
		ValidateSchema: migratepkg.ComposeSchemaValidator(
			(&DatabaseSchemaVersion{}).TableName(),
			requiredModels,
			[]migratepkg.RequiredColumn{
				{Model: &AppLog{}, Table: "app_logs", Column: "Level"},
				{Model: &AppLog{}, Table: "app_logs", Column: "event"},
				{Model: &AppLog{}, Table: "app_logs", Column: "source"},
				{Model: &AppLog{}, Table: "app_logs", Column: "details_json"},
				{Model: &User{}, Table: "users", Column: "auth_version"},
				{Model: &User{}, Table: "users", Column: "must_change_password"},
				{Model: &SourceConfig{}, Table: "source_configs", Column: "source_type"},
				{Model: &SourceConfig{}, Table: "source_configs", Column: "source_url"},
				{Model: &SourceConfig{}, Table: "source_configs", Column: "content_type"},
				{Model: &SourceConfig{}, Table: "source_configs", Column: "fetched_at"},
				{Model: &ProxyNode{}, Table: "proxy_nodes", Column: "consecutive_failures"},
				{Model: &ProxyNode{}, Table: "proxy_nodes", Column: "consecutive_successes"},
				{Model: &ProxyNode{}, Table: "proxy_nodes", Column: "quarantined_at"},
				{Model: &RoutingRule{}, Table: "routing_rules", Column: "target_profile_id"},
				{Model: &RoutingRule{}, Table: "routing_rules", Column: "sort_order"},
			},
		),
		AfterInitialize: migrateSQLiteDataIfNeeded,
	}
}

func ensureDatabaseSchemaUpToDate(db *gorm.DB, backend string) error {
	loadVersion, saveVersion := migratepkg.NewSchemaVersionStore((&DatabaseSchemaVersion{}).TableName(), databaseSchemaVersionRowID)
	requiredModels := registeredModels()
	migration := migratepkg.BuildTemplateScheduler(
		db,
		backend,
		loadVersion,
		saveVersion,
		migratepkg.ComposeEmptyChecker(requiredModels...),
		migratepkg.ComposeAutoMigrator(schemaMetadataModels()...),
		migrationHooks(),
	)
	return migration.Apply()
}

func sqliteSourceExists() bool {
	info, err := os.Stat(common.SQLitePath)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func migrateSQLiteDataIfNeeded(target *gorm.DB, backend string) error {
	if backend != "postgres" {
		return nil
	}
	empty, err := migratepkg.ComposeEmptyChecker(registeredModels()...)(target)
	if err != nil {
		return err
	}
	if !empty {
		slog.Info("skip sqlite migration because target database already has data", "backend", backend)
		return nil
	}
	if !sqliteSourceExists() {
		slog.Info("skip sqlite migration because sqlite source file was not found", "sqlite_path", common.SQLitePath)
		return nil
	}

	source, err := gorm.Open(sqlite.Open(common.SQLitePath), &gorm.Config{
		PrepareStmt: true,
	})
	if err != nil {
		return fmt.Errorf("open sqlite source database failed: %w", err)
	}
	sourceSQLDB, err := source.DB()
	if err != nil {
		return fmt.Errorf("get sqlite source database handle failed: %w", err)
	}
	defer func() {
		_ = sourceSQLDB.Close()
	}()

	models, err := buildDBModels()
	if err != nil {
		return err
	}

	slog.Info("starting sqlite to postgres database migration", "sqlite_path", common.SQLitePath)
	err = target.Transaction(func(tx *gorm.DB) error {
		for _, item := range models {
			if err := migrateTableData(source, tx, item); err != nil {
				return err
			}
			if item.hasIDPK {
				if err := resetPostgresSequence(tx, item.tableName); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	slog.Info("sqlite to postgres database migration completed", "sqlite_path", common.SQLitePath)
	return nil
}

func migrateTableData(source *gorm.DB, target *gorm.DB, item dbModel) error {
	if !source.Migrator().HasTable(item.value) {
		slog.Info("database migration progress", "table", item.tableName, "migrated", 0, "total", 0, "status", "skipped_missing_source_table")
		return nil
	}
	var total int64
	if err := source.Model(item.value).Count(&total).Error; err != nil {
		return fmt.Errorf("count sqlite table %s failed: %w", item.tableName, err)
	}
	slog.Info("database migration progress", "table", item.tableName, "migrated", 0, "total", total, "status", "starting")
	if total == 0 {
		slog.Info("database migration progress", "table", item.tableName, "migrated", 0, "total", total, "status", "completed")
		return nil
	}

	modelType := reflect.TypeOf(item.value).Elem()
	sliceType := reflect.SliceOf(modelType)
	migrated := int64(0)
	offset := 0
	const batchSize = 200

	for {
		batchPtr := reflect.New(sliceType)
		query := source.Model(item.value).Limit(batchSize).Offset(offset)
		if item.hasIDPK {
			query = query.Order("id ASC")
		}
		if err := query.Find(batchPtr.Interface()).Error; err != nil {
			return fmt.Errorf("read sqlite table %s failed: %w", item.tableName, err)
		}
		batchLen := batchPtr.Elem().Len()
		if batchLen == 0 {
			break
		}
		if err := target.Create(batchPtr.Interface()).Error; err != nil {
			return fmt.Errorf("write target table %s failed: %w", item.tableName, err)
		}
		migrated += int64(batchLen)
		offset += batchLen
		slog.Info("database migration progress", "table", item.tableName, "migrated", migrated, "total", total, "status", "running")
	}

	slog.Info("database migration progress", "table", item.tableName, "migrated", migrated, "total", total, "status", "completed")
	return nil
}

func resetPostgresSequence(db *gorm.DB, tableName string) error {
	sql := fmt.Sprintf(
		"SELECT setval(pg_get_serial_sequence('%s', 'id'), COALESCE(MAX(id), 1), MAX(id) IS NOT NULL) FROM \"%s\"",
		tableName,
		tableName,
	)
	return db.Exec(sql).Error
}

func InitDB() (err error) {
	db, backend, err := openDatabase()
	if err != nil {
		slog.Error("open database failed", "error", err)
		os.Exit(1)
	}
	DB = db
	if err = ensureDatabaseSchemaUpToDate(db, backend); err != nil {
		return err
	}
	if err = ensureUserSecurityDefaults(); err != nil {
		return err
	}
	return createRootAccountIfNeed()
}

func CloseDB() error {
	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}
	err = sqlDB.Close()
	return err
}
