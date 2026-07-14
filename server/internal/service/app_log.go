package service

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"poolx/internal/model"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxAppLogMessageBytes  = 4096
	maxAppLogDetailBytes   = 512
	maxAppLogDetailFields  = 24
	appLogTruncatedSuffix  = " [truncated]"
	maxAppLogRows          = 5000
	appLogRetentionDays    = 30
	appLogPruneEveryWrites = 100
)

var appLogEventInvalidCharacters = regexp.MustCompile(`[^a-z0-9._-]+`)
var appLogWriteCount atomic.Uint64
var appLogPruneMu sync.Mutex

type appLogger struct{}

var AppLog = appLogger{}

type AppLogList struct {
	Items []*model.AppLog `json:"items"`
}

type AppLogPushInput struct {
	Classification string `json:"classification"`
	Level          string `json:"level"`
	Message        string `json:"message"`
}

type AppLogEventInput struct {
	Classification string
	Level          string
	Event          string
	Source         string
	Message        string
	Details        map[string]string
}

func (appLogger) Push(classification string, level string, message string) error {
	summary, event, details := parseStructuredLogMessage(message)
	return AppLog.PushEvent(AppLogEventInput{
		Classification: classification,
		Level:          level,
		Event:          event,
		Source:         "backend",
		Message:        summary,
		Details:        details,
	})
}

func (appLogger) PushEvent(input AppLogEventInput) error {
	classification := normalizeLogClassification(input.Classification)
	level := normalizeLogLevel(input.Level)
	message := sanitizeAppLogMessage(input.Message)
	if message == "" {
		return fmt.Errorf("日志内容不能为空")
	}
	event := normalizeAppLogEvent(input.Event)
	source := normalizeAppLogSource(input.Source)
	detailsJSON, err := json.Marshal(sanitizeAppLogDetails(input.Details))
	if err != nil {
		return fmt.Errorf("编码日志字段失败: %w", err)
	}

	if err := model.CreateAppLog(&model.AppLog{
		Classification: classification,
		Level:          level,
		Event:          event,
		Source:         source,
		Message:        message,
		DetailsJSON:    string(detailsJSON),
	}); err != nil {
		return err
	}

	writeCount := appLogWriteCount.Add(1)
	if writeCount == 1 || writeCount%appLogPruneEveryWrites == 0 {
		if err := PruneAppLogs(); err != nil {
			slog.Warn("prune application logs failed", "error", err)
		}
	}
	return nil
}

func sanitizeAppLogMessage(message string) string {
	message = strings.Map(func(value rune) rune {
		if unicode.IsControl(value) {
			return ' '
		}
		return value
	}, message)
	message = strings.Join(strings.Fields(message), " ")
	return truncateUTF8(message, maxAppLogMessageBytes)
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	end := maxBytes - len(appLogTruncatedSuffix)
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return strings.TrimSpace(value[:end]) + appLogTruncatedSuffix
}

func ListAppLogs(limit int, afterID int, classification string, level string, source string, keyword string) (*AppLogList, error) {
	filter := model.AppLogListFilter{
		Classification: normalizeLogClassificationOptional(classification),
		Level:          normalizeLogLevelOptional(level),
		Source:         normalizeAppLogSourceOptional(source),
		Keyword:        strings.TrimSpace(keyword),
	}

	var (
		items []*model.AppLog
		err   error
	)
	if afterID > 0 {
		items, err = model.GetAppLogsAfterID(afterID, limit, filter)
	} else {
		items, err = model.GetRecentAppLogs(limit, filter)
	}
	if err != nil {
		return nil, err
	}
	normalizeListedAppLogs(items)
	return &AppLogList{Items: items}, nil
}

func normalizeListedAppLogs(items []*model.AppLog) {
	for _, item := range items {
		if item == nil {
			continue
		}
		if item.Details == nil {
			item.Details = map[string]string{}
		}
		if len(item.Details) == 0 && strings.Contains(item.Message, "|") {
			summary, event, details := parseStructuredLogMessage(item.Message)
			item.Message = summary
			item.Details = details
			if item.Event == "" || item.Event == "general" {
				item.Event = event
			}
		}
		if item.Event == "" {
			item.Event = "general"
		}
		if item.Source == "" {
			item.Source = "backend"
		}
	}
}

func PushFrontendAppLog(input AppLogPushInput) error {
	summary, event, details := parseStructuredLogMessage(input.Message)
	return AppLog.PushEvent(AppLogEventInput{
		Classification: input.Classification,
		Level:          input.Level,
		Event:          event,
		Source:         "frontend",
		Message:        summary,
		Details:        details,
	})
}

func PruneAppLogs() error {
	appLogPruneMu.Lock()
	defer appLogPruneMu.Unlock()
	if err := model.DeleteAppLogsBefore(time.Now().AddDate(0, 0, -appLogRetentionDays)); err != nil {
		return err
	}
	cutoffID, err := model.FindAppLogRetentionCutoffID(maxAppLogRows)
	if err != nil {
		return err
	}
	return model.DeleteAppLogsThroughID(cutoffID)
}

func parseStructuredLogMessage(message string) (string, string, map[string]string) {
	parts := strings.Split(message, "|")
	summary := sanitizeAppLogMessage(parts[0])
	details := make(map[string]string)
	for _, part := range parts[1:] {
		if len(details) >= maxAppLogDetailFields {
			break
		}
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		key = normalizeDetailKey(key)
		value = truncateUTF8(sanitizeAppLogMessage(value), maxAppLogDetailBytes)
		if key != "" && value != "" {
			details[key] = value
		}
	}
	return summary, normalizeAppLogEvent(summary), details
}

func sanitizeAppLogDetails(details map[string]string) map[string]string {
	result := make(map[string]string, min(len(details), maxAppLogDetailFields))
	for key, value := range details {
		if len(result) >= maxAppLogDetailFields {
			break
		}
		key = normalizeDetailKey(key)
		value = truncateUTF8(sanitizeAppLogMessage(value), maxAppLogDetailBytes)
		if key != "" && value != "" {
			result[key] = value
		}
	}
	return result
}

func normalizeDetailKey(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = appLogEventInvalidCharacters.ReplaceAllString(value, "_")
	return strings.Trim(value, "_")
}

func normalizeAppLogEvent(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = appLogEventInvalidCharacters.ReplaceAllString(value, "_")
	value = strings.Trim(value, "_")
	if value == "" {
		return "general"
	}
	if len(value) > 96 {
		value = value[:96]
	}
	return value
}

func normalizeAppLogSourceOptional(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "":
		return ""
	case "backend", "frontend", "mihomo", "scheduler":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return ""
	}
}

func normalizeAppLogSource(value string) string {
	value = normalizeAppLogSourceOptional(value)
	if value == "" {
		return "backend"
	}
	return value
}

func normalizeLogClassificationOptional(classification string) string {
	classification = strings.TrimSpace(strings.ToLower(classification))
	switch classification {
	case "":
		return ""
	case model.AppLogClassificationSystem, model.AppLogClassificationBusiness, model.AppLogClassificationSecurity:
		return classification
	default:
		return model.AppLogClassificationSystem
	}
}

func normalizeLogClassification(classification string) string {
	classification = normalizeLogClassificationOptional(classification)
	if classification == "" {
		return model.AppLogClassificationSystem
	}
	return classification
}

func normalizeLogLevelOptional(level string) string {
	level = strings.TrimSpace(strings.ToLower(level))
	switch level {
	case "":
		return ""
	case model.AppLogLevelDebug, model.AppLogLevelInfo, model.AppLogLevelWarn, model.AppLogLevelError:
		return level
	default:
		return model.AppLogLevelInfo
	}
}

func normalizeLogLevel(level string) string {
	level = normalizeLogLevelOptional(level)
	if level == "" {
		return model.AppLogLevelInfo
	}
	return level
}
