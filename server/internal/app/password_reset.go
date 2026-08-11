package app

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"poolx/internal/model"
	"poolx/internal/pkg/common"

	"golang.org/x/term"
	"gorm.io/gorm"
)

// readPasswordTwice prompts for a new password twice and returns it.
// It is a package-level variable so tests can inject deterministic input.
var readPasswordTwice = defaultReadPasswordTwice

func defaultReadPasswordTwice() (string, error) {
	fmt.Print("请输入新密码: ")
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("读取密码失败: %w", err)
	}
	fmt.Print("请再次输入新密码: ")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("读取密码失败: %w", err)
	}
	if string(first) != string(second) {
		return "", errors.New("两次输入的密码不一致")
	}
	return string(first), nil
}

// ResetAdminPasswordCLI resets the root admin password interactively and
// marks the account as requiring a password change on next sign-in.
// It is invoked through the --reset-password flag and exits without
// starting the HTTP server, so it never exposes a network surface.
func ResetAdminPasswordCLI() error {
	if err := model.InitDB(); err != nil {
		return err
	}
	defer func() {
		if err := model.CloseDB(); err != nil {
			slog.Error("close database failed", "error", err)
		}
	}()

	var root model.User
	err := model.DB.Where("role = ?", common.RoleRootUser).First(&root).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("未找到管理员账号，请先正常启动一次服务以完成初始化")
	}
	if err != nil {
		return fmt.Errorf("查询管理员账号失败: %w", err)
	}

	newPassword, err := readPasswordTwice()
	if err != nil {
		return err
	}
	if err := validateResetPassword(newPassword); err != nil {
		return err
	}

	root.Password = newPassword
	if err := root.UpdateWithPasswordPolicy(true, true); err != nil {
		return fmt.Errorf("更新密码失败: %w", err)
	}
	fmt.Println("密码已重置，请使用新密码登录；下次登录将要求修改密码。")
	return nil
}

func validateResetPassword(password string) error {
	if len(password) < 8 || len(password) > 20 {
		return errors.New("密码长度必须为 8 到 20 个字符")
	}
	return nil
}
