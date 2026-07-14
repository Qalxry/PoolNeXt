//go:build !linux

package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func kernelFileObjectID(os.FileInfo) (uint64, uint64) {
	return 0, 0
}

func secureReplaceExecutableFile(
	tempPath string,
	targetPath string,
	expectedPrepared kernelFileIdentity,
	expectedParent kernelDirectoryIdentity,
	expectedTarget kernelFileIdentity,
) error {
	parentIdentity, err := captureKernelDirectoryIdentity(filepath.Dir(targetPath))
	if err != nil {
		return err
	}
	if expectedParent != parentIdentity {
		return ErrKernelPathChanged
	}
	preparedIdentity, err := captureKernelFileIdentity(tempPath)
	if err != nil {
		return err
	}
	if !sameKernelFileIdentity(expectedPrepared, preparedIdentity) {
		return ErrKernelPathChanged
	}
	if info, err := os.Lstat(targetPath); err == nil {
		if !expectedTarget.Exists {
			return ErrKernelPathChanged
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: target is a symbolic link", ErrUnsafeKernelPath)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: target is not a regular file", ErrUnsafeKernelPath)
		}
		targetIdentity, identityErr := captureKernelFileIdentity(targetPath)
		if identityErr != nil {
			return identityErr
		}
		if !sameKernelFileIdentity(expectedTarget, targetIdentity) {
			return ErrKernelPathChanged
		}
		if runtime.GOOS == "windows" {
			if err := os.Remove(targetPath); err != nil {
				return fmt.Errorf("remove existing Mihomo binary: %w", err)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat Mihomo target: %w", err)
	} else if expectedTarget.Exists {
		return ErrKernelPathChanged
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		return fmt.Errorf("install Mihomo binary: %w", err)
	}
	return nil
}

func captureKernelDirectoryIdentity(directoryPath string) (kernelDirectoryIdentity, error) {
	info, err := os.Lstat(directoryPath)
	if err != nil {
		return kernelDirectoryIdentity{}, fmt.Errorf("stat Mihomo target directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return kernelDirectoryIdentity{}, fmt.Errorf("%w: target parent is not a real directory", ErrUnsafeKernelPath)
	}
	deviceID, inode := kernelFileObjectID(info)
	return kernelDirectoryIdentity{DeviceID: deviceID, Inode: inode}, nil
}

func ensureKernelTargetParent(directoryPath string) error {
	absolute, err := filepath.Abs(directoryPath)
	if err != nil {
		return fmt.Errorf("resolve Mihomo target directory: %w", err)
	}
	absolute = filepath.Clean(absolute)
	volume := filepath.VolumeName(absolute)
	current := volume
	if filepath.IsAbs(absolute) {
		current += string(filepath.Separator)
	}
	remaining := strings.TrimPrefix(strings.TrimPrefix(absolute, volume), string(filepath.Separator))
	for _, component := range strings.Split(remaining, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		next := filepath.Join(current, component)
		info, statErr := os.Lstat(next)
		if errors.Is(statErr, os.ErrNotExist) {
			if mkdirErr := os.Mkdir(next, 0o755); mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
				return fmt.Errorf("create Mihomo target directory %s: %w", next, mkdirErr)
			}
			info, statErr = os.Lstat(next)
		}
		if statErr != nil {
			return fmt.Errorf("stat Mihomo target directory %s: %w", next, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%w: target parent component %s is not a real directory", ErrUnsafeKernelPath, next)
		}
		current = next
	}
	return nil
}
