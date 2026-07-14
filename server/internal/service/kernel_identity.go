package service

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"poolx/internal/model"
	"runtime"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const kernelBinaryIdentityTable = "kernel_binary_identities"

type kernelBinaryIdentityRecord struct {
	ID              uint64    `gorm:"primaryKey"`
	KernelType      string    `gorm:"size:32;not null;uniqueIndex:idx_kernel_binary_identity_path,priority:1"`
	PathKey         string    `gorm:"size:64;not null;uniqueIndex:idx_kernel_binary_identity_path,priority:2"`
	RegisteredPath  string    `gorm:"size:2048;not null"`
	CanonicalPath   string    `gorm:"size:2048;not null;index"`
	Mode            string    `gorm:"size:32;not null;index"`
	Source          string    `gorm:"size:32;not null"`
	DeviceID        uint64    `gorm:"not null;default:0;index"`
	Inode           uint64    `gorm:"not null;default:0;index"`
	Size            int64     `gorm:"not null;default:0"`
	FileMode        uint32    `gorm:"not null;default:0"`
	ModTimeUnixNano int64     `gorm:"not null;default:0"`
	SHA256          string    `gorm:"size:64;not null;default:''"`
	CreatedAt       time.Time `gorm:"not null"`
	UpdatedAt       time.Time `gorm:"not null"`
}

func (kernelBinaryIdentityRecord) TableName() string {
	return kernelBinaryIdentityTable
}

type kernelFileIdentity struct {
	RegisteredPath  string
	CanonicalPath   string
	Exists          bool
	DeviceID        uint64
	Inode           uint64
	Size            int64
	FileMode        uint32
	ModTimeUnixNano int64
	SHA256          string
}

type kernelDirectoryIdentity struct {
	DeviceID uint64
	Inode    uint64
}

func ensureKernelIdentitySchema() error {
	if model.DB == nil {
		return errors.New("database is not initialized")
	}
	if err := model.DB.AutoMigrate(&kernelBinaryIdentityRecord{}); err != nil {
		return fmt.Errorf("initialize kernel identity schema: %w", err)
	}
	return nil
}

func loadKernelIdentityRecords() ([]kernelBinaryIdentityRecord, error) {
	if err := ensureKernelIdentitySchema(); err != nil {
		return nil, err
	}
	var records []kernelBinaryIdentityRecord
	if err := model.DB.Order("id ASC").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("load kernel identities: %w", err)
	}
	return records, nil
}

func persistKernelIdentity(tx *gorm.DB, identity kernelBinaryIdentityRecord) error {
	updates := []string{
		"registered_path",
		"canonical_path",
		"mode",
		"source",
		"device_id",
		"inode",
		"size",
		"file_mode",
		"mod_time_unix_nano",
		"sha256",
		"updated_at",
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "kernel_type"}, {Name: "path_key"}},
		DoUpdates: clause.AssignmentColumns(updates),
	}).Create(&identity).Error
}

func persistKernelOption(tx *gorm.DB, key string, value string) error {
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&model.Option{Key: key, Value: value}).Error
}

func persistKernelState(identity kernelBinaryIdentityRecord, options map[string]string) error {
	if err := ensureKernelIdentitySchema(); err != nil {
		return err
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := persistKernelIdentity(tx, identity); err != nil {
			return fmt.Errorf("persist kernel identity: %w", err)
		}
		for key, value := range options {
			if err := persistKernelOption(tx, key, value); err != nil {
				return fmt.Errorf("persist kernel option %s: %w", key, err)
			}
		}
		return nil
	})
}

func persistKernelIdentityOnly(identity kernelBinaryIdentityRecord) error {
	if err := ensureKernelIdentitySchema(); err != nil {
		return err
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := persistKernelIdentity(tx, identity); err != nil {
			return fmt.Errorf("persist kernel identity: %w", err)
		}
		return nil
	})
}

func captureKernelFileIdentity(filePath string) (kernelFileIdentity, error) {
	registeredPath, err := filepath.Abs(strings.TrimSpace(filePath))
	if err != nil {
		return kernelFileIdentity{}, fmt.Errorf("resolve kernel path: %w", err)
	}
	registeredPath = filepath.Clean(registeredPath)
	canonicalPath, err := canonicalKernelPath(registeredPath)
	if err != nil {
		return kernelFileIdentity{}, fmt.Errorf("resolve canonical kernel path: %w", err)
	}

	identity := kernelFileIdentity{
		RegisteredPath: registeredPath,
		CanonicalPath:  canonicalPath,
	}
	file, err := os.Open(canonicalPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return identity, nil
		}
		return kernelFileIdentity{}, fmt.Errorf("open kernel file: %w", err)
	}
	defer file.Close()

	before, err := file.Stat()
	if err != nil {
		return kernelFileIdentity{}, fmt.Errorf("stat opened kernel file: %w", err)
	}
	if !before.Mode().IsRegular() {
		return kernelFileIdentity{}, fmt.Errorf("kernel path is not a regular file: %s", canonicalPath)
	}

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return kernelFileIdentity{}, fmt.Errorf("hash kernel file: %w", err)
	}
	after, err := file.Stat()
	if err != nil {
		return kernelFileIdentity{}, fmt.Errorf("restat opened kernel file: %w", err)
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		return kernelFileIdentity{}, ErrKernelPathChanged
	}
	pathInfo, err := os.Stat(canonicalPath)
	if err != nil {
		return kernelFileIdentity{}, fmt.Errorf("restat kernel path: %w", err)
	}
	if !os.SameFile(after, pathInfo) {
		return kernelFileIdentity{}, ErrKernelPathChanged
	}
	finalCanonicalPath, err := filepath.EvalSymlinks(registeredPath)
	if err != nil {
		return kernelFileIdentity{}, fmt.Errorf("revalidate canonical kernel path: %w", err)
	}
	if !pathEqual(filepath.Clean(finalCanonicalPath), canonicalPath) {
		return kernelFileIdentity{}, ErrKernelPathChanged
	}

	deviceID, inode := kernelFileObjectID(after)
	identity.Exists = true
	identity.DeviceID = deviceID
	identity.Inode = inode
	identity.Size = after.Size()
	identity.FileMode = uint32(after.Mode())
	identity.ModTimeUnixNano = after.ModTime().UnixNano()
	identity.SHA256 = fmt.Sprintf("%x", hasher.Sum(nil))
	return identity, nil
}

func newKernelIdentityRecord(identity kernelFileIdentity, mode string, source string) kernelBinaryIdentityRecord {
	now := time.Now().UTC()
	pathKey := sha256.Sum256([]byte(normalizeKernelIdentityPath(identity.RegisteredPath)))
	return kernelBinaryIdentityRecord{
		KernelType:      KernelTypeMihomo,
		PathKey:         fmt.Sprintf("%x", pathKey),
		RegisteredPath:  identity.RegisteredPath,
		CanonicalPath:   identity.CanonicalPath,
		Mode:            mode,
		Source:          strings.ToLower(strings.TrimSpace(source)),
		DeviceID:        identity.DeviceID,
		Inode:           identity.Inode,
		Size:            identity.Size,
		FileMode:        identity.FileMode,
		ModTimeUnixNano: identity.ModTimeUnixNano,
		SHA256:          identity.SHA256,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func kernelRecordPathMatches(record kernelBinaryIdentityRecord, target kernelFileIdentity) bool {
	return pathEqual(record.RegisteredPath, target.RegisteredPath) ||
		pathEqual(record.RegisteredPath, target.CanonicalPath) ||
		pathEqual(record.CanonicalPath, target.RegisteredPath) ||
		pathEqual(record.CanonicalPath, target.CanonicalPath)
}

func kernelRecordObjectMatches(record kernelBinaryIdentityRecord, target kernelFileIdentity) bool {
	if !target.Exists {
		return false
	}
	if record.DeviceID != 0 && record.Inode != 0 && target.DeviceID != 0 && target.Inode != 0 {
		return record.DeviceID == target.DeviceID && record.Inode == target.Inode
	}
	return record.SHA256 != "" &&
		record.SHA256 == target.SHA256 &&
		record.Size == target.Size &&
		record.FileMode == target.FileMode &&
		record.ModTimeUnixNano == target.ModTimeUnixNano
}

func kernelRecordOwnsManagedTarget(record kernelBinaryIdentityRecord, target kernelFileIdentity) bool {
	if record.Mode != KernelModeManaged {
		return false
	}
	if !target.Exists {
		return kernelRecordPathMatches(record, target)
	}
	return kernelRecordObjectMatches(record, target)
}

func sameKernelFileIdentity(left kernelFileIdentity, right kernelFileIdentity) bool {
	if left.RegisteredPath != right.RegisteredPath || left.CanonicalPath != right.CanonicalPath || left.Exists != right.Exists {
		return false
	}
	if !left.Exists {
		return true
	}
	return left.DeviceID == right.DeviceID &&
		left.Inode == right.Inode &&
		left.Size == right.Size &&
		left.FileMode == right.FileMode &&
		left.ModTimeUnixNano == right.ModTimeUnixNano &&
		left.SHA256 == right.SHA256
}

func normalizeKernelIdentityPath(filePath string) string {
	absolute, err := filepath.Abs(strings.TrimSpace(filePath))
	if err != nil {
		return strings.TrimSpace(filePath)
	}
	absolute = filepath.Clean(absolute)
	if runtime.GOOS == "windows" {
		return strings.ToLower(absolute)
	}
	return absolute
}
