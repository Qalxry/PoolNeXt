package service

import (
	"poolx/internal/model"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestSanitizeAppLogMessageNormalizesControlCharacters(t *testing.T) {
	got := sanitizeAppLogMessage("  login\nfailed\tuser=alice\x00  ")
	if got != "login failed user=alice" {
		t.Fatalf("unexpected sanitized message: %q", got)
	}
}

func TestAppLogPushStoresStructuredEventAndDetails(t *testing.T) {
	setupServiceTestDB(t)

	if err := AppLog.Push(model.AppLogClassificationSecurity, model.AppLogLevelWarn, "login failed | username=alice | reason=bad password"); err != nil {
		t.Fatalf("push structured log: %v", err)
	}
	logs, err := ListAppLogs(10, 0, "security", "warn", "backend", "")
	if err != nil {
		t.Fatalf("list structured logs: %v", err)
	}
	if len(logs.Items) != 1 {
		t.Fatalf("expected one structured log, got %d", len(logs.Items))
	}
	item := logs.Items[0]
	if item.Message != "login failed" || item.Event != "login_failed" || item.Source != "backend" {
		t.Fatalf("unexpected structured log: %+v", item)
	}
	if item.Details["username"] != "alice" || item.Details["reason"] != "bad password" {
		t.Fatalf("unexpected structured details: %+v", item.Details)
	}
}

func TestPushFrontendAppLogMarksFrontendSource(t *testing.T) {
	setupServiceTestDB(t)

	if err := PushFrontendAppLog(AppLogPushInput{
		Classification: model.AppLogClassificationBusiness,
		Level:          model.AppLogLevelInfo,
		Message:        "page opened | route=/nodes",
	}); err != nil {
		t.Fatalf("push frontend log: %v", err)
	}
	logs, err := ListAppLogs(10, 0, "", "", "frontend", "")
	if err != nil {
		t.Fatalf("list frontend logs: %v", err)
	}
	if len(logs.Items) != 1 || logs.Items[0].Source != "frontend" {
		t.Fatalf("unexpected frontend logs: %+v", logs.Items)
	}
}

func TestPruneAppLogsDeletesExpiredRows(t *testing.T) {
	setupServiceTestDB(t)
	old := model.AppLog{
		Classification: model.AppLogClassificationSystem,
		Level:          model.AppLogLevelInfo,
		Event:          "old_event",
		Source:         "backend",
		Message:        "old",
		DetailsJSON:    "{}",
		CreatedAt:      time.Now().AddDate(0, 0, -(appLogRetentionDays + 1)),
	}
	fresh := old
	fresh.Event = "fresh_event"
	fresh.Message = "fresh"
	fresh.CreatedAt = time.Now()
	if err := model.DB.Create(&old).Error; err != nil {
		t.Fatalf("create old log: %v", err)
	}
	if err := model.DB.Create(&fresh).Error; err != nil {
		t.Fatalf("create fresh log: %v", err)
	}
	if err := PruneAppLogs(); err != nil {
		t.Fatalf("prune logs: %v", err)
	}
	var messages []string
	if err := model.DB.Model(&model.AppLog{}).Order("id asc").Pluck("message", &messages).Error; err != nil {
		t.Fatalf("list pruned logs: %v", err)
	}
	if len(messages) != 1 || messages[0] != "fresh" {
		t.Fatalf("unexpected logs after pruning: %+v", messages)
	}
}

func TestPruneAppLogsBoundsRowCount(t *testing.T) {
	setupServiceTestDB(t)

	logs := make([]model.AppLog, 0, maxAppLogRows+3)
	for index := 0; index < maxAppLogRows+3; index++ {
		logs = append(logs, model.AppLog{
			Classification: model.AppLogClassificationSystem,
			Level:          model.AppLogLevelInfo,
			Event:          "retention_test",
			Source:         "backend",
			Message:        "bounded",
			DetailsJSON:    "{}",
		})
	}
	if err := model.DB.CreateInBatches(logs, 250).Error; err != nil {
		t.Fatalf("create logs for retention test: %v", err)
	}
	if err := PruneAppLogs(); err != nil {
		t.Fatalf("prune bounded logs: %v", err)
	}

	var count int64
	if err := model.DB.Model(&model.AppLog{}).Count(&count).Error; err != nil {
		t.Fatalf("count bounded logs: %v", err)
	}
	if count != maxAppLogRows {
		t.Fatalf("expected %d retained logs, got %d", maxAppLogRows, count)
	}
}

func TestSanitizeAppLogMessageTruncatesAtUTF8Boundary(t *testing.T) {
	got := sanitizeAppLogMessage(strings.Repeat("界", maxAppLogMessageBytes))
	if len(got) > maxAppLogMessageBytes {
		t.Fatalf("sanitized message exceeds byte limit: %d", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatal("sanitized message is not valid UTF-8")
	}
	if !strings.HasSuffix(got, appLogTruncatedSuffix) {
		t.Fatalf("truncated message lacks suffix: %q", got[len(got)-32:])
	}
}
