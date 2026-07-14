package model

import (
	"encoding/hex"
	"errors"
	"testing"

	"poolx/internal/pkg/common"

	"gorm.io/gorm"
)

func TestInitOptionMapRotatesLegacyDefaultClashSecret(t *testing.T) {
	restore := prepareOptionModelTest(t)
	defer restore()

	common.OptionMapRWMutex.Lock()
	common.ClashSecret = common.DefaultClashSecret
	common.OptionMapRWMutex.Unlock()
	InitOptionMap()

	common.OptionMapRWMutex.RLock()
	secret := common.ClashSecret
	mapSecret := common.OptionMap["ClashSecret"]
	common.OptionMapRWMutex.RUnlock()
	if secret == "" || secret == common.DefaultClashSecret {
		t.Fatalf("legacy public controller secret was not rotated: %q", secret)
	}
	if mapSecret != secret {
		t.Fatalf("option map secret mismatch: got %q want %q", mapSecret, secret)
	}
	decoded, err := hex.DecodeString(secret)
	if err != nil || len(decoded) != clashSecretByteLength {
		t.Fatalf("generated controller secret is not %d random bytes: %q", clashSecretByteLength, secret)
	}

	var stored Option
	if err := DB.First(&stored, "key = ?", "ClashSecret").Error; err != nil {
		t.Fatalf("load persisted ClashSecret: %v", err)
	}
	if stored.Value != secret {
		t.Fatalf("persisted controller secret mismatch: got %q want %q", stored.Value, secret)
	}
}

func TestUpdateOptionKeepsMemoryUnchangedWhenDatabaseWriteFails(t *testing.T) {
	restore := prepareOptionModelTest(t)
	defer restore()
	InitOptionMap()

	if err := UpdateOption("SystemName", "before-failure"); err != nil {
		t.Fatalf("seed SystemName: %v", err)
	}
	forcedErr := errors.New("forced option persistence failure")
	const callbackName = "test:force_option_persistence_failure"
	if err := DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "options" {
			tx.AddError(forcedErr)
		}
	}); err != nil {
		t.Fatalf("register failure callback: %v", err)
	}
	defer func() {
		_ = DB.Callback().Create().Remove(callbackName)
	}()

	err := UpdateOption("SystemName", "after-failure")
	if !errors.Is(err, forcedErr) {
		t.Fatalf("expected persistence error, got %v", err)
	}
	common.OptionMapRWMutex.RLock()
	mapValue := common.OptionMap["SystemName"]
	globalValue := common.SystemName
	common.OptionMapRWMutex.RUnlock()
	if mapValue != "before-failure" || globalValue != "before-failure" {
		t.Fatalf("failed database write changed memory: map=%q global=%q", mapValue, globalValue)
	}

	var stored Option
	if err := DB.First(&stored, "key = ?", "SystemName").Error; err != nil {
		t.Fatalf("load persisted SystemName: %v", err)
	}
	if stored.Value != "before-failure" {
		t.Fatalf("failed database write changed persisted value: %q", stored.Value)
	}
}

func prepareOptionModelTest(t *testing.T) func() {
	t.Helper()

	originalDB := DB
	common.OptionMapRWMutex.RLock()
	originalSecret := common.ClashSecret
	originalSystemName := common.SystemName
	originalMap := make(map[string]string, len(common.OptionMap))
	for key, value := range common.OptionMap {
		originalMap[key] = value
	}
	common.OptionMapRWMutex.RUnlock()

	DB = openTestSQLiteDB(t, "option-test.db")
	return func() {
		DB = originalDB
		common.OptionMapRWMutex.Lock()
		common.ClashSecret = originalSecret
		common.SystemName = originalSystemName
		common.OptionMap = originalMap
		common.OptionMapRWMutex.Unlock()
	}
}
