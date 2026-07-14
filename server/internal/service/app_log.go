package service

import (
	"fmt"
	"poolx/internal/model"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxAppLogMessageBytes = 4096
	appLogTruncatedSuffix = " [truncated]"
)

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

func (appLogger) Push(classification string, level string, message string) error {
	classification = normalizeLogClassification(classification)
	level = normalizeLogLevel(level)
	message = sanitizeAppLogMessage(message)
	if message == "" {
		return fmt.Errorf("日志内容不能为空")
	}

	return model.CreateAppLog(&model.AppLog{
		Classification: classification,
		Level:          level,
		Message:        message,
	})
}

func sanitizeAppLogMessage(message string) string {
	message = strings.Map(func(value rune) rune {
		if unicode.IsControl(value) {
			return ' '
		}
		return value
	}, message)
	message = strings.Join(strings.Fields(message), " ")
	if len(message) <= maxAppLogMessageBytes {
		return message
	}

	end := maxAppLogMessageBytes - len(appLogTruncatedSuffix)
	for end > 0 && !utf8.ValidString(message[:end]) {
		end--
	}
	return strings.TrimSpace(message[:end]) + appLogTruncatedSuffix
}

func ListAppLogs(limit int, afterID int, classification string) (*AppLogList, error) {
	classification = normalizeLogClassificationOptional(classification)

	var (
		items []*model.AppLog
		err   error
	)

	if afterID > 0 {
		items, err = model.GetAppLogsAfterID(afterID, limit, classification)
	} else {
		items, err = model.GetRecentAppLogs(limit, classification)
	}
	if err != nil {
		return nil, err
	}

	return &AppLogList{Items: items}, nil
}

func PushFrontendAppLog(input AppLogPushInput) error {
	return AppLog.Push(input.Classification, input.Level, input.Message)
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
