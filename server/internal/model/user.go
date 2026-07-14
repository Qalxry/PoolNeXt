package model

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"poolx/internal/pkg/common"
	"poolx/internal/pkg/utils/security"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

var (
	ErrUserIdentityTaken     = errors.New("user identity is already bound")
	ErrUserMutationForbidden = errors.New("user mutation is forbidden or target does not exist")
)

// User if you add sensitive fields, don't forget to clean them in setupLogin function.
// Otherwise, the sensitive information will be saved on local storage in plain text!
type User struct {
	Id                 int    `json:"id"`
	Username           string `json:"username" gorm:"unique;index" validate:"max=12"`
	Password           string `json:"password,omitempty" gorm:"not null;" validate:"min=8,max=20"`
	DisplayName        string `json:"display_name" gorm:"index" validate:"max=20"`
	Role               int    `json:"role" gorm:"type:int;default:1"`   // admin, common
	Status             int    `json:"status" gorm:"type:int;default:1"` // enabled, disabled
	Token              string `json:"-" gorm:"index"`
	AuthVersion        uint64 `json:"-" gorm:"not null;default:1"`
	MustChangePassword bool   `json:"must_change_password" gorm:"not null;default:false"`
	Email              string `json:"email" gorm:"index" validate:"max=50"`
	GitHubId           string `json:"github_id" gorm:"column:github_id;index"`
	WeChatId           string `json:"wechat_id" gorm:"column:wechat_id;index"`
	VerificationCode   string `json:"verification_code" gorm:"-:all"` // this field is only for Email verification, don't save it to database!
}

func NormalizeEmailIdentity(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func NormalizeGitHubIdentity(identity string) (string, bool) {
	identity = strings.TrimSpace(identity)
	parsed, err := strconv.ParseInt(identity, 10, 64)
	if err != nil || parsed <= 0 {
		return "", false
	}
	canonical := strconv.FormatInt(parsed, 10)
	return canonical, canonical == identity
}

func NormalizeWeChatIdentity(identity string) string {
	return strings.TrimSpace(identity)
}

func IsUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	type sqlStateError interface {
		SQLState() string
	}
	var stateErr sqlStateError
	if errors.As(err, &stateErr) && stateErr.SQLState() == "23505" {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") ||
		strings.Contains(message, "duplicate key") ||
		strings.Contains(message, "is not unique")
}

func GetMaxUserId() int {
	var user User
	DB.Last(&user)
	return user.Id
}

func GetAllUsers(startIdx int, num int) (users []*User, err error) {
	err = DB.Order("id desc").Limit(num).Offset(startIdx).Select([]string{"id", "username", "display_name", "role", "status", "email", "must_change_password"}).Find(&users).Error
	return users, err
}

func SearchUsers(keyword string) (users []*User, err error) {
	err = DB.Select([]string{"id", "username", "display_name", "role", "status", "email", "must_change_password"}).Where("id = ? or username LIKE ? or email LIKE ? or display_name LIKE ?", keyword, keyword+"%", keyword+"%", keyword+"%").Find(&users).Error
	return users, err
}

func GetUserById(id int, selectAll bool) (*User, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	user := User{Id: id}
	var err error = nil
	if selectAll {
		err = DB.First(&user, "id = ?", id).Error
	} else {
		err = DB.Select([]string{"id", "username", "display_name", "role", "status", "email", "wechat_id", "github_id", "must_change_password"}).First(&user, "id = ?", id).Error
	}
	return &user, err
}

func DeleteUserById(id int) (err error) {
	if id == 0 {
		return errors.New("id 为空！")
	}
	user := User{Id: id}
	return user.Delete()
}

func (user *User) Insert() error {
	var err error
	user.Email = NormalizeEmailIdentity(user.Email)
	user.WeChatId = NormalizeWeChatIdentity(user.WeChatId)
	if user.GitHubId != "" {
		var ok bool
		user.GitHubId, ok = NormalizeGitHubIdentity(user.GitHubId)
		if !ok {
			return errors.New("GitHub id 必须是规范的数字身份")
		}
	}
	if user.Password != "" {
		user.Password, err = security.Password2Hash(user.Password)
		if err != nil {
			return err
		}
	}
	if user.AuthVersion == 0 {
		user.AuthVersion = 1
	}
	err = DB.Create(user).Error
	return err
}

func (user *User) Update(updatePassword bool) error {
	return user.UpdateWithPasswordPolicy(updatePassword, false)
}

// UpdateWithPasswordPolicy updates the mutable user fields and revokes every
// previously issued credential when an authorization-sensitive field changes.
// mustChangePassword is only applied when updatePassword is true.
func (user *User) UpdateWithPasswordPolicy(updatePassword bool, mustChangePassword bool) error {
	if user.Id == 0 {
		return errors.New("id 为空！")
	}
	if user.Email != "" {
		user.Email = NormalizeEmailIdentity(user.Email)
	}
	if user.GitHubId != "" {
		var ok bool
		user.GitHubId, ok = NormalizeGitHubIdentity(user.GitHubId)
		if !ok {
			return errors.New("GitHub id 必须是规范的数字身份")
		}
	}
	if user.WeChatId != "" {
		user.WeChatId = NormalizeWeChatIdentity(user.WeChatId)
	}

	var err error
	if updatePassword {
		user.Password, err = security.Password2Hash(user.Password)
		if err != nil {
			return err
		}
	}

	wasBootstrapRoot := false
	err = DB.Transaction(func(tx *gorm.DB) error {
		var current User
		if err := tx.First(&current, "id = ?", user.Id).Error; err != nil {
			return err
		}

		updates := map[string]any{}
		if user.Username != "" {
			updates["username"] = user.Username
		}
		if user.DisplayName != "" {
			updates["display_name"] = user.DisplayName
		}
		if user.Role != 0 {
			updates["role"] = user.Role
		}
		if user.Status != 0 {
			updates["status"] = user.Status
		}
		if user.Token != "" {
			updates["token"] = user.Token
		}
		if user.Email != "" {
			updates["email"] = user.Email
		}
		if user.GitHubId != "" {
			updates["github_id"] = user.GitHubId
		}
		if user.WeChatId != "" {
			updates["wechat_id"] = user.WeChatId
		}
		if updatePassword {
			updates["password"] = user.Password
			updates["must_change_password"] = mustChangePassword
		}

		revokeCredentials := updatePassword ||
			(user.Role != 0 && user.Role != current.Role) ||
			(user.Status != 0 && user.Status != current.Status)
		if revokeCredentials {
			updates["auth_version"] = gorm.Expr("CASE WHEN auth_version < 1 THEN 2 ELSE auth_version + 1 END")
			updates["token"] = ""
		}

		if len(updates) > 0 {
			if err := tx.Model(&User{}).Where("id = ?", user.Id).Updates(updates).Error; err != nil {
				return err
			}
		}
		if err := tx.First(user, "id = ?", user.Id).Error; err != nil {
			return err
		}
		wasBootstrapRoot = updatePassword && !mustChangePassword && current.Role == common.RoleRootUser && current.MustChangePassword
		return nil
	})
	if err != nil {
		return err
	}
	if wasBootstrapRoot {
		if err := removeBootstrapCredentialFile(); err != nil {
			slog.Warn("remove bootstrap credential file failed", "error", err)
		}
	}
	return nil
}

func (user *User) Delete() error {
	if user.Id == 0 {
		return errors.New("id 为空！")
	}
	err := DB.Delete(user).Error
	return err
}

// ValidateAndFill check password & user status
func (user *User) ValidateAndFill() (err error) {
	// When querying with struct, GORM will only query with non-zero fields,
	// that means if your field’s value is 0, '', false or other zero values,
	// it won’t be used to build query conditions
	password := user.Password
	if user.Username == "" || password == "" {
		return errors.New("用户名或密码为空")
	}
	if err := DB.Where(User{Username: user.Username}).First(user).Error; err != nil {
		return errors.New("用户名或密码错误，或用户已被封禁")
	}
	okay := security.ValidatePasswordAndHash(password, user.Password)
	if !okay || user.Status != common.UserStatusEnabled {
		return errors.New("用户名或密码错误，或用户已被封禁")
	}
	return nil
}

func (user *User) FillUserById() error {
	if user.Id == 0 {
		return errors.New("id 为空！")
	}
	return DB.Where(User{Id: user.Id}).First(user).Error
}

func (user *User) FillUserByEmail() error {
	user.Email = NormalizeEmailIdentity(user.Email)
	if user.Email == "" {
		return errors.New("email 为空！")
	}
	return DB.Where(User{Email: user.Email}).First(user).Error
}

func (user *User) FillUserByGitHubId() error {
	identity, ok := NormalizeGitHubIdentity(user.GitHubId)
	if !ok {
		return errors.New("GitHub id 为空！")
	}
	user.GitHubId = identity
	return DB.Where(User{GitHubId: user.GitHubId}).First(user).Error
}

func (user *User) FillUserByWeChatId() error {
	user.WeChatId = NormalizeWeChatIdentity(user.WeChatId)
	if user.WeChatId == "" {
		return errors.New("WeChat id 为空！")
	}
	return DB.Where(User{WeChatId: user.WeChatId}).First(user).Error
}

func (user *User) FillUserByUsername() error {
	if user.Username == "" {
		return errors.New("username 为空！")
	}
	return DB.Where(User{Username: user.Username}).First(user).Error
}

func ValidateUserToken(token string) (user *User) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	parts := strings.Fields(token)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		token = parts[1]
	} else if len(parts) != 1 {
		return nil
	}
	user = &User{}
	if DB.Where("token = ?", token).First(user).RowsAffected != 1 {
		return nil
	}
	if user.Status != common.UserStatusEnabled || user.AuthVersion == 0 {
		return nil
	}
	if version, hasVersion := tokenAuthVersion(token); hasVersion {
		if version != user.AuthVersion {
			return nil
		}
	} else if user.AuthVersion != 1 {
		// Versionless tokens are accepted only for untouched legacy users.
		return nil
	}
	return user
}

func tokenAuthVersion(token string) (uint64, bool) {
	prefix, _, ok := strings.Cut(token, ".")
	if !ok || !strings.HasPrefix(prefix, "v") {
		return 0, false
	}
	version, err := strconv.ParseUint(strings.TrimPrefix(prefix, "v"), 10, 64)
	return version, err == nil
}

func IsEmailAlreadyTaken(email string) bool {
	email = NormalizeEmailIdentity(email)
	return email != "" && DB.Where("email = ?", email).Limit(1).Find(&User{}).RowsAffected > 0
}

func IsWeChatIdAlreadyTaken(wechatId string) bool {
	wechatId = NormalizeWeChatIdentity(wechatId)
	return wechatId != "" && DB.Where("wechat_id = ?", wechatId).Limit(1).Find(&User{}).RowsAffected > 0
}

func IsGitHubIdAlreadyTaken(githubId string) bool {
	githubId, ok := NormalizeGitHubIdentity(githubId)
	return ok && DB.Where("github_id = ?", githubId).Limit(1).Find(&User{}).RowsAffected > 0
}

func IsUsernameAlreadyTaken(username string) bool {
	return DB.Where("username = ?", username).Find(&User{}).RowsAffected == 1
}

func BindUserEmail(ctx context.Context, userID int, email string) error {
	email = NormalizeEmailIdentity(email)
	if email == "" {
		return errors.New("邮箱地址为空")
	}
	return bindUniqueUserIdentity(ctx, userID, "email", email)
}

func BindUserGitHubIdentity(ctx context.Context, userID int, identity string) error {
	identity, ok := NormalizeGitHubIdentity(identity)
	if !ok {
		return errors.New("GitHub id 必须是规范的数字身份")
	}
	return bindUniqueUserIdentity(ctx, userID, "github_id", identity)
}

func BindUserWeChatIdentity(ctx context.Context, userID int, identity string) error {
	identity = NormalizeWeChatIdentity(identity)
	if identity == "" {
		return errors.New("微信身份为空")
	}
	return bindUniqueUserIdentity(ctx, userID, "wechat_id", identity)
}

func bindUniqueUserIdentity(ctx context.Context, userID int, column string, value string) error {
	if userID <= 0 {
		return errors.New("id 为空！")
	}
	allowed := map[string]bool{"email": true, "github_id": true, "wechat_id": true}
	if !allowed[column] {
		return fmt.Errorf("不支持的身份字段 %q", column)
	}
	result := DB.WithContext(ctx).Model(&User{}).Where("id = ?", userID).Update(column, value)
	if result.Error != nil {
		if IsUniqueConstraintError(result.Error) {
			return ErrUserIdentityTaken
		}
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func UpdateUserAsAdminAtomic(user *User, requesterRole int, updatePassword bool, mustChangePassword bool) error {
	if user == nil || user.Id <= 0 {
		return errors.New("id 为空！")
	}
	updates := map[string]any{}
	if user.Username != "" {
		updates["username"] = user.Username
	}
	if user.DisplayName != "" {
		updates["display_name"] = user.DisplayName
	}
	if user.Role != 0 {
		updates["role"] = user.Role
	}
	if user.Status != 0 {
		updates["status"] = user.Status
	}
	if user.Email != "" {
		updates["email"] = NormalizeEmailIdentity(user.Email)
	}
	if user.GitHubId != "" {
		identity, ok := NormalizeGitHubIdentity(user.GitHubId)
		if !ok {
			return errors.New("GitHub id 必须是规范的数字身份")
		}
		updates["github_id"] = identity
	}
	if user.WeChatId != "" {
		updates["wechat_id"] = NormalizeWeChatIdentity(user.WeChatId)
	}
	if updatePassword {
		hashedPassword, err := security.Password2Hash(user.Password)
		if err != nil {
			return err
		}
		updates["password"] = hashedPassword
		updates["must_change_password"] = mustChangePassword
	}
	if updatePassword || user.Role != 0 || user.Status != 0 {
		updates["auth_version"] = gorm.Expr("CASE WHEN auth_version < 1 THEN 2 ELSE auth_version + 1 END")
		updates["token"] = ""
	}
	if len(updates) == 0 {
		return nil
	}

	result := DB.Model(&User{}).
		Where("id = ? AND role < ?", user.Id, requesterRole).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrUserMutationForbidden
	}
	return DB.First(user, "id = ?", user.Id).Error
}

func DeleteUserAsAdminAtomic(id int, requesterRole int) error {
	if id <= 0 {
		return errors.New("id 为空！")
	}
	result := DB.Where("id = ? AND role < ?", id, requesterRole).Delete(&User{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrUserMutationForbidden
	}
	return nil
}

func ResetUserPasswordByEmail(email string, password string) error {
	email = NormalizeEmailIdentity(email)
	if email == "" || password == "" {
		return errors.New("邮箱地址或密码为空！")
	}
	hashedPassword, err := security.Password2Hash(password)
	if err != nil {
		return err
	}
	result := DB.Model(&User{}).Where("email = ?", email).Updates(map[string]any{
		"password":             hashedPassword,
		"must_change_password": true,
		"auth_version":         gorm.Expr("CASE WHEN auth_version < 1 THEN 2 ELSE auth_version + 1 END"),
		"token":                "",
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("用户不存在")
	}
	return nil
}
