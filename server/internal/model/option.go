package model

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"poolx/internal/pkg/common"
	"strconv"
	"strings"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const clashSecretByteLength = 32

var optionUpdateMu sync.Mutex
var ensureClashSecretMu sync.Mutex

type Option struct {
	Key   string `json:"key" gorm:"primaryKey"`
	Value string `json:"value"`
}

func AllOption() ([]*Option, error) {
	var options []*Option
	var err error
	err = DB.Find(&options).Error
	return options, err
}

func InitOptionMap() {
	common.OptionMapRWMutex.Lock()
	common.OptionMap = make(map[string]string)
	common.OptionMap["FileUploadPermission"] = strconv.Itoa(common.FileUploadPermission)
	common.OptionMap["FileDownloadPermission"] = strconv.Itoa(common.FileDownloadPermission)
	common.OptionMap["ImageUploadPermission"] = strconv.Itoa(common.ImageUploadPermission)
	common.OptionMap["ImageDownloadPermission"] = strconv.Itoa(common.ImageDownloadPermission)
	common.OptionMap["PasswordLoginEnabled"] = strconv.FormatBool(common.PasswordLoginEnabled)
	common.OptionMap["PasswordRegisterEnabled"] = strconv.FormatBool(common.PasswordRegisterEnabled)
	common.OptionMap["EmailVerificationEnabled"] = strconv.FormatBool(common.EmailVerificationEnabled)
	common.OptionMap["GitHubOAuthEnabled"] = strconv.FormatBool(common.GitHubOAuthEnabled)
	common.OptionMap["WeChatAuthEnabled"] = strconv.FormatBool(common.WeChatAuthEnabled)
	common.OptionMap["TurnstileCheckEnabled"] = strconv.FormatBool(common.TurnstileCheckEnabled)
	common.OptionMap["RegisterEnabled"] = strconv.FormatBool(common.RegisterEnabled)
	common.OptionMap["SMTPServer"] = ""
	common.OptionMap["SMTPPort"] = strconv.Itoa(common.SMTPPort)
	common.OptionMap["SMTPAccount"] = ""
	common.OptionMap["SMTPToken"] = ""
	common.OptionMap["Notice"] = ""
	common.OptionMap["About"] = ""
	common.OptionMap["Footer"] = common.Footer
	common.OptionMap["HomePageLink"] = common.HomePageLink
	common.OptionMap["SystemName"] = common.SystemName
	common.OptionMap["ServerAddress"] = ""
	common.OptionMap["ServerUpdateRepo"] = common.ServerUpdateRepo
	common.OptionMap["KernelType"] = common.KernelType
	common.OptionMap["MihomoBinaryPath"] = common.MihomoBinaryPath
	common.OptionMap["MihomoBinaryVersion"] = common.MihomoBinaryVersion
	common.OptionMap["MihomoBinarySource"] = common.MihomoBinarySource
	common.OptionMap["NodeTestDefaultURL"] = common.NodeTestDefaultURL
	common.OptionMap["NodeTestDefaultTimeoutMS"] = strconv.Itoa(common.NodeTestDefaultTimeoutMS)
	common.OptionMap["RuntimeAutoStartEnabled"] = strconv.FormatBool(common.RuntimeAutoStartEnabled)
	common.OptionMap["ClashAllowLAN"] = strconv.FormatBool(common.ClashAllowLAN)
	common.OptionMap["ClashExternalController"] = common.ClashExternalController
	common.OptionMap["ClashMode"] = common.ClashMode
	common.OptionMap["ClashSecret"] = common.ClashSecret
	common.OptionMap["GeoIPProvider"] = common.GeoIPProvider
	common.OptionMap["GitHubClientId"] = ""
	common.OptionMap["GitHubClientSecret"] = ""
	common.OptionMap["WeChatServerAddress"] = ""
	common.OptionMap["WeChatServerToken"] = ""
	common.OptionMap["WeChatAccountQRCodeImageURL"] = ""
	common.OptionMap["TurnstileSiteKey"] = ""
	common.OptionMap["TurnstileSecretKey"] = ""
	common.OptionMap["GlobalApiRateLimitNum"] = strconv.Itoa(common.GlobalApiRateLimitNum)
	common.OptionMap["GlobalApiRateLimitDuration"] = strconv.FormatInt(common.GlobalApiRateLimitDuration, 10)
	common.OptionMap["GlobalWebRateLimitNum"] = strconv.Itoa(common.GlobalWebRateLimitNum)
	common.OptionMap["GlobalWebRateLimitDuration"] = strconv.FormatInt(common.GlobalWebRateLimitDuration, 10)
	common.OptionMap["UploadRateLimitNum"] = strconv.Itoa(common.UploadRateLimitNum)
	common.OptionMap["UploadRateLimitDuration"] = strconv.FormatInt(common.UploadRateLimitDuration, 10)
	common.OptionMap["DownloadRateLimitNum"] = strconv.Itoa(common.DownloadRateLimitNum)
	common.OptionMap["DownloadRateLimitDuration"] = strconv.FormatInt(common.DownloadRateLimitDuration, 10)
	common.OptionMap["CriticalRateLimitNum"] = strconv.Itoa(common.CriticalRateLimitNum)
	common.OptionMap["CriticalRateLimitDuration"] = strconv.FormatInt(common.CriticalRateLimitDuration, 10)
	common.OptionMapRWMutex.Unlock()
	options, err := AllOption()
	if err != nil {
		slog.Error("load options failed", "error", err)
	} else {
		for _, option := range options {
			updateOptionMap(option.Key, option.Value)
		}
	}

	secret, err := EnsureClashSecret()
	if err != nil {
		// Keep the effective runtime secret random even if persistence is temporarily unavailable.
		if secret != "" {
			updateOptionMap("ClashSecret", secret)
		}
		slog.Error("initialize random ClashSecret failed", "error", err)
	}
}

func UpdateOption(key string, value string) error {
	optionUpdateMu.Lock()
	defer optionUpdateMu.Unlock()

	if DB == nil {
		return fmt.Errorf("persist option %q: database is not initialized", key)
	}

	common.OptionMapRWMutex.Lock()
	defer common.OptionMapRWMutex.Unlock()

	option := Option{Key: key, Value: value}
	if err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&option).Error; err != nil {
		return fmt.Errorf("persist option %q: %w", key, err)
	}
	updateOptionMapLocked(key, value)
	return nil
}

func updateOptionMap(key string, value string) {
	common.OptionMapRWMutex.Lock()
	defer common.OptionMapRWMutex.Unlock()
	updateOptionMapLocked(key, value)
}

func updateOptionMapLocked(key string, value string) {
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMap[key] = value
	if strings.HasSuffix(key, "Permission") {
		intValue, _ := strconv.Atoi(value)
		switch key {
		case "FileUploadPermission":
			common.FileUploadPermission = intValue
		case "FileDownloadPermission":
			common.FileDownloadPermission = intValue
		case "ImageUploadPermission":
			common.ImageUploadPermission = intValue
		case "ImageDownloadPermission":
			common.ImageDownloadPermission = intValue
		}
	}
	if strings.HasSuffix(key, "Enabled") {
		boolValue := value == "true"
		switch key {
		case "PasswordRegisterEnabled":
			common.PasswordRegisterEnabled = boolValue
		case "PasswordLoginEnabled":
			common.PasswordLoginEnabled = boolValue
		case "EmailVerificationEnabled":
			common.EmailVerificationEnabled = boolValue
		case "GitHubOAuthEnabled":
			common.GitHubOAuthEnabled = boolValue
		case "WeChatAuthEnabled":
			common.WeChatAuthEnabled = boolValue
		case "TurnstileCheckEnabled":
			common.TurnstileCheckEnabled = boolValue
		case "RegisterEnabled":
			common.RegisterEnabled = boolValue
		case "RuntimeAutoStartEnabled":
			common.RuntimeAutoStartEnabled = boolValue
		}
	}
	switch key {
	case "SMTPServer":
		common.SMTPServer = value
	case "SMTPPort":
		intValue, _ := strconv.Atoi(value)
		common.SMTPPort = intValue
	case "SMTPAccount":
		common.SMTPAccount = value
	case "SMTPToken":
		common.SMTPToken = value
	case "ServerAddress":
		common.ServerAddress = value
	case "ServerUpdateRepo":
		common.ServerUpdateRepo = value
	case "KernelType":
		common.KernelType = value
	case "MihomoBinaryPath":
		common.MihomoBinaryPath = value
	case "MihomoBinaryVersion":
		common.MihomoBinaryVersion = value
	case "MihomoBinarySource":
		common.MihomoBinarySource = value
	case "NodeTestDefaultURL":
		if strings.TrimSpace(value) != "" {
			common.NodeTestDefaultURL = value
		}
	case "NodeTestDefaultTimeoutMS":
		if v, err := strconv.Atoi(value); err == nil && v > 0 {
			common.NodeTestDefaultTimeoutMS = v
		}
	case "ClashAllowLAN":
		common.ClashAllowLAN = value == "true"
	case "ClashExternalController":
		if strings.TrimSpace(value) != "" {
			common.ClashExternalController = value
		}
	case "ClashMode":
		if strings.TrimSpace(value) != "" {
			common.ClashMode = value
		}
	case "ClashSecret":
		if strings.TrimSpace(value) != "" {
			common.ClashSecret = value
		}
	case "GeoIPProvider":
		common.GeoIPProvider = value
	case "GitHubClientId":
		common.GitHubClientId = value
	case "GitHubClientSecret":
		common.GitHubClientSecret = value
	case "Footer":
		common.Footer = value
	case "HomePageLink":
		common.HomePageLink = value
	case "SystemName":
		common.SystemName = value
	case "WeChatServerAddress":
		common.WeChatServerAddress = value
	case "WeChatServerToken":
		common.WeChatServerToken = value
	case "WeChatAccountQRCodeImageURL":
		common.WeChatAccountQRCodeImageURL = value
	case "TurnstileSiteKey":
		common.TurnstileSiteKey = value
	case "TurnstileSecretKey":
		common.TurnstileSecretKey = value
	case "GlobalApiRateLimitNum":
		if v, err := strconv.Atoi(value); err == nil && v > 0 {
			common.GlobalApiRateLimitNum = v
		}
	case "GlobalApiRateLimitDuration":
		if v, err := strconv.ParseInt(value, 10, 64); err == nil && v > 0 {
			common.GlobalApiRateLimitDuration = v
		}
	case "GlobalWebRateLimitNum":
		if v, err := strconv.Atoi(value); err == nil && v > 0 {
			common.GlobalWebRateLimitNum = v
		}
	case "GlobalWebRateLimitDuration":
		if v, err := strconv.ParseInt(value, 10, 64); err == nil && v > 0 {
			common.GlobalWebRateLimitDuration = v
		}
	case "UploadRateLimitNum":
		if v, err := strconv.Atoi(value); err == nil && v > 0 {
			common.UploadRateLimitNum = v
		}
	case "UploadRateLimitDuration":
		if v, err := strconv.ParseInt(value, 10, 64); err == nil && v > 0 {
			common.UploadRateLimitDuration = v
		}
	case "DownloadRateLimitNum":
		if v, err := strconv.Atoi(value); err == nil && v > 0 {
			common.DownloadRateLimitNum = v
		}
	case "DownloadRateLimitDuration":
		if v, err := strconv.ParseInt(value, 10, 64); err == nil && v > 0 {
			common.DownloadRateLimitDuration = v
		}
	case "CriticalRateLimitNum":
		if v, err := strconv.Atoi(value); err == nil && v > 0 {
			common.CriticalRateLimitNum = v
		}
	case "CriticalRateLimitDuration":
		if v, err := strconv.ParseInt(value, 10, 64); err == nil && v > 0 {
			common.CriticalRateLimitDuration = v
		}
	}
}

// EnsureClashSecret rotates the legacy public default and persists one random
// controller secret shared by Runtime and the Zashboard proxy.
func EnsureClashSecret() (string, error) {
	ensureClashSecretMu.Lock()
	defer ensureClashSecretMu.Unlock()

	common.OptionMapRWMutex.RLock()
	current := strings.TrimSpace(common.ClashSecret)
	common.OptionMapRWMutex.RUnlock()
	if current != "" && current != common.DefaultClashSecret {
		if DB == nil {
			return current, fmt.Errorf("persist ClashSecret: database is not initialized")
		}
		var stored Option
		err := DB.First(&stored, "key = ?", "ClashSecret").Error
		switch {
		case err == nil && strings.TrimSpace(stored.Value) == current:
			return current, nil
		case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
			return current, fmt.Errorf("load ClashSecret: %w", err)
		}
		if err := UpdateOption("ClashSecret", current); err != nil {
			return current, err
		}
		return current, nil
	}

	secret, err := generateClashSecret()
	if err != nil {
		return "", err
	}
	if err := UpdateOption("ClashSecret", secret); err != nil {
		return secret, err
	}
	return secret, nil
}

func generateClashSecret() (string, error) {
	buffer := make([]byte, clashSecretByteLength)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate ClashSecret: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}
