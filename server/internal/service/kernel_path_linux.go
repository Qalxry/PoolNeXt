//go:build linux

package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func kernelFileObjectID(info os.FileInfo) (uint64, uint64) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return 0, 0
	}
	return uint64(stat.Dev), stat.Ino
}

func secureReplaceExecutableFile(
	tempPath string,
	targetPath string,
	expectedPrepared kernelFileIdentity,
	expectedParent kernelDirectoryIdentity,
	expectedTarget kernelFileIdentity,
) error {
	tempAbsolute, err := filepath.Abs(tempPath)
	if err != nil {
		return fmt.Errorf("resolve prepared Mihomo path: %w", err)
	}
	targetAbsolute, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("resolve Mihomo target path: %w", err)
	}
	tempAbsolute = filepath.Clean(tempAbsolute)
	targetAbsolute = filepath.Clean(targetAbsolute)
	if !pathEqual(filepath.Dir(tempAbsolute), filepath.Dir(targetAbsolute)) {
		return errors.New("prepared Mihomo binary must be in the target directory")
	}

	directoryFD, err := openKernelDirectoryNoSymlinks(filepath.Dir(targetAbsolute))
	if err != nil {
		return err
	}
	defer unix.Close(directoryFD)
	var directoryStat unix.Stat_t
	if err := unix.Fstat(directoryFD, &directoryStat); err != nil {
		return fmt.Errorf("stat Mihomo target directory: %w", err)
	}
	if expectedParent.DeviceID != 0 && expectedParent.Inode != 0 &&
		(expectedParent.DeviceID != uint64(directoryStat.Dev) || expectedParent.Inode != directoryStat.Ino) {
		return ErrKernelPathChanged
	}

	var tempStat unix.Stat_t
	if err := unix.Fstatat(directoryFD, filepath.Base(tempAbsolute), &tempStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("stat prepared Mihomo binary: %w", err)
	}
	if tempStat.Mode&unix.S_IFMT != unix.S_IFREG {
		return errors.New("prepared Mihomo binary is not a regular file")
	}
	if expectedPrepared.DeviceID != 0 && expectedPrepared.Inode != 0 &&
		(expectedPrepared.DeviceID != uint64(tempStat.Dev) || expectedPrepared.Inode != tempStat.Ino) {
		return ErrKernelPathChanged
	}
	if expectedPrepared.Size != tempStat.Size ||
		uint32(os.FileMode(expectedPrepared.FileMode).Perm()) != tempStat.Mode&0o777 ||
		expectedPrepared.ModTimeUnixNano != tempStat.Mtim.Sec*1_000_000_000+tempStat.Mtim.Nsec {
		return ErrKernelPathChanged
	}

	var targetStat unix.Stat_t
	if err := unix.Fstatat(directoryFD, filepath.Base(targetAbsolute), &targetStat, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		if !expectedTarget.Exists {
			return ErrKernelPathChanged
		}
		if targetStat.Mode&unix.S_IFMT == unix.S_IFLNK {
			return fmt.Errorf("%w: target is a symbolic link", ErrUnsafeKernelPath)
		}
		if targetStat.Mode&unix.S_IFMT != unix.S_IFREG {
			return fmt.Errorf("%w: target is not a regular file", ErrUnsafeKernelPath)
		}
		if expectedTarget.DeviceID != 0 && expectedTarget.Inode != 0 &&
			(expectedTarget.DeviceID != uint64(targetStat.Dev) || expectedTarget.Inode != targetStat.Ino) {
			return ErrKernelPathChanged
		}
		if expectedTarget.Size != targetStat.Size ||
			uint32(os.FileMode(expectedTarget.FileMode).Perm()) != targetStat.Mode&0o777 ||
			expectedTarget.ModTimeUnixNano != targetStat.Mtim.Sec*1_000_000_000+targetStat.Mtim.Nsec {
			return ErrKernelPathChanged
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("stat Mihomo target: %w", err)
	} else if expectedTarget.Exists {
		return ErrKernelPathChanged
	}

	if expectedTarget.Exists {
		if err := unix.Renameat(directoryFD, filepath.Base(tempAbsolute), directoryFD, filepath.Base(targetAbsolute)); err != nil {
			return fmt.Errorf("install Mihomo binary: %w", err)
		}
	} else if err := unix.Renameat2(directoryFD, filepath.Base(tempAbsolute), directoryFD, filepath.Base(targetAbsolute), unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("install Mihomo binary without replacement: %w", err)
	}
	if err := unix.Fsync(directoryFD); err != nil {
		return fmt.Errorf("sync Mihomo target directory: %w", err)
	}
	return nil
}

func captureKernelDirectoryIdentity(directoryPath string) (kernelDirectoryIdentity, error) {
	directoryFD, err := openKernelDirectoryNoSymlinks(directoryPath)
	if err != nil {
		return kernelDirectoryIdentity{}, err
	}
	defer unix.Close(directoryFD)
	var stat unix.Stat_t
	if err := unix.Fstat(directoryFD, &stat); err != nil {
		return kernelDirectoryIdentity{}, fmt.Errorf("stat Mihomo target directory: %w", err)
	}
	return kernelDirectoryIdentity{DeviceID: uint64(stat.Dev), Inode: stat.Ino}, nil
}

func ensureKernelTargetParent(directoryPath string) error {
	directoryFD, err := openOrCreateKernelDirectoryNoSymlinks(directoryPath, true)
	if err != nil {
		return err
	}
	return unix.Close(directoryFD)
}

func openKernelDirectoryNoSymlinks(directoryPath string) (int, error) {
	return openOrCreateKernelDirectoryNoSymlinks(directoryPath, false)
}

func openOrCreateKernelDirectoryNoSymlinks(directoryPath string, create bool) (int, error) {
	absolute, err := filepath.Abs(directoryPath)
	if err != nil {
		return -1, fmt.Errorf("resolve Mihomo target directory: %w", err)
	}
	absolute = filepath.Clean(absolute)
	currentFD, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open filesystem root: %w", err)
	}
	for _, component := range strings.Split(strings.TrimPrefix(absolute, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		nextFD, openErr := unix.Openat(currentFD, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if errors.Is(openErr, unix.ENOENT) && create {
			if mkdirErr := unix.Mkdirat(currentFD, component, 0o755); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				_ = unix.Close(currentFD)
				return -1, fmt.Errorf("create Mihomo target directory %s: %w", absolute, mkdirErr)
			}
			nextFD, openErr = unix.Openat(currentFD, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		}
		_ = unix.Close(currentFD)
		if openErr != nil {
			return -1, fmt.Errorf("%w: directory %s: %v", ErrUnsafeKernelPath, absolute, openErr)
		}
		currentFD = nextFD
	}
	return currentFD, nil
}
