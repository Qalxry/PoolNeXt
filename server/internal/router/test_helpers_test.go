package router_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"poolx/internal/model"
	"poolx/internal/pkg/common"
)

type apiResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

const testRootPassword = "router-test-pass"

func setupTestDB(t *testing.T) {
	t.Helper()

	originalSQLitePath := common.SQLitePath

	common.SQLitePath = filepath.Join(t.TempDir(), "router-test.db")

	if err := model.InitDB(); err != nil {
		t.Fatalf("init test db: %v", err)
	}
	root := model.User{Username: "root"}
	if err := root.FillUserByUsername(); err != nil {
		t.Fatalf("load bootstrap root: %v", err)
	}
	root.Password = testRootPassword
	if err := root.UpdateWithPasswordPolicy(true, false); err != nil {
		t.Fatalf("set router test root password: %v", err)
	}

	t.Cleanup(func() {
		if err := model.CloseDB(); err != nil {
			t.Fatalf("close test db: %v", err)
		}
		model.DB = nil
		common.SQLitePath = originalSQLitePath
	})
}
