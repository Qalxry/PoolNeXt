package model

import (
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	AppLogClassificationSystem   = "system"
	AppLogClassificationBusiness = "business"
	AppLogClassificationSecurity = "security"

	AppLogLevelDebug = "debug"
	AppLogLevelInfo  = "info"
	AppLogLevelWarn  = "warn"
	AppLogLevelError = "error"
)

type AppLog struct {
	ID             int               `json:"id" gorm:"primaryKey;autoIncrement"`
	Classification string            `json:"classification" gorm:"size:32;index;not null"`
	Level          string            `json:"level" gorm:"size:16;index;not null;default:info"`
	Event          string            `json:"event" gorm:"size:96;index;not null;default:general"`
	Source         string            `json:"source" gorm:"size:32;index;not null;default:backend"`
	Message        string            `json:"message" gorm:"type:text;not null"`
	DetailsJSON    string            `json:"-" gorm:"type:text"`
	Details        map[string]string `json:"details" gorm:"-"`
	CreatedAt      time.Time         `json:"created_at" gorm:"index"`
}

type AppLogListFilter struct {
	Classification string
	Level          string
	Source         string
	Keyword        string
}

func GetRecentAppLogs(limit int, filter AppLogListFilter) ([]*AppLog, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	query := applyAppLogFilter(DB.Order("id desc").Limit(limit), filter)
	var logs []*AppLog
	if err := query.Find(&logs).Error; err != nil {
		return nil, err
	}

	for left, right := 0, len(logs)-1; left < right; left, right = left+1, right-1 {
		logs[left], logs[right] = logs[right], logs[left]
	}
	hydrateAppLogs(logs)
	return logs, nil
}

func GetAppLogsAfterID(afterID int, limit int, filter AppLogListFilter) ([]*AppLog, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	query := applyAppLogFilter(DB.Order("id asc").Limit(limit).Where("id > ?", afterID), filter)
	var logs []*AppLog
	if err := query.Find(&logs).Error; err != nil {
		return nil, err
	}
	hydrateAppLogs(logs)
	return logs, nil
}

func CreateAppLog(logEntry *AppLog) error {
	return DB.Create(logEntry).Error
}

func DeleteAppLogsBefore(cutoff time.Time) error {
	return DB.Where("created_at < ?", cutoff).Delete(&AppLog{}).Error
}

func DeleteAppLogsThroughID(id int) error {
	if id <= 0 {
		return nil
	}
	return DB.Where("id <= ?", id).Delete(&AppLog{}).Error
}

func FindAppLogRetentionCutoffID(keep int) (int, error) {
	if keep <= 0 {
		return 0, nil
	}
	var ids []int
	if err := DB.Model(&AppLog{}).Order("id desc").Offset(keep).Limit(1).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return ids[0], nil
}

func applyAppLogFilter(query *gorm.DB, filter AppLogListFilter) *gorm.DB {
	if filter.Classification != "" {
		query = query.Where("classification = ?", filter.Classification)
	}
	if filter.Level != "" {
		query = query.Where("level = ?", filter.Level)
	}
	if filter.Source != "" {
		query = query.Where("source = ?", filter.Source)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("message LIKE ? OR event LIKE ? OR details_json LIKE ?", like, like, like)
	}
	return query
}

func hydrateAppLogs(logs []*AppLog) {
	for _, item := range logs {
		if item == nil {
			continue
		}
		item.Details = map[string]string{}
		if strings.TrimSpace(item.DetailsJSON) != "" {
			_ = json.Unmarshal([]byte(item.DetailsJSON), &item.Details)
		}
	}
}
