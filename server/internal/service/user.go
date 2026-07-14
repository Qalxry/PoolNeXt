package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"poolx/internal/model"
	"poolx/internal/pkg/common"
	"poolx/internal/pkg/utils/mail"
	"poolx/internal/pkg/utils/security"
	"poolx/internal/pkg/utils/validation"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

const (
	maxUsernameLength         = 12
	maxPasswordLength         = 20
	maxDisplayNameLength      = 20
	maxEmailLength            = 50
	maxVerificationCodeLength = 128
	maxOAuthCodeLength        = 1024
	maxOAuthStateLength       = 128
	maxWeChatIdentityLength   = 256
	maxOAuthResponseBytes     = 1 << 20
)

type LoginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type PasswordResetInput struct {
	Email string `json:"email"`
	Token string `json:"token"`
}

type ManageUserInput struct {
	Username string `json:"username"`
	Action   string `json:"action"`
}

type GitHubOAuthResponse struct {
	AccessToken string `json:"access_token"`
	Scope       string `json:"scope"`
	TokenType   string `json:"token_type"`
}

type GitHubUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type WeChatLoginResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

func validateBoundedField(value string, field string, max int) error {
	if utf8.RuneCountInString(value) > max {
		return fmt.Errorf("%s 长度不能超过 %d 个字符", field, max)
	}
	return nil
}

func truncateRunes(value string, max int) string {
	if max <= 0 || utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max])
}

func canonicalEmail(email string) (string, error) {
	email = model.NormalizeEmailIdentity(email)
	if err := validateBoundedField(email, "邮箱地址", maxEmailLength); err != nil {
		return "", err
	}
	return email, nil
}

func AuthenticateUser(input LoginInput) (*model.User, error) {
	if !common.PasswordLoginEnabled {
		return nil, fmt.Errorf("管理员关闭了密码登录")
	}
	if input.Username == "" || input.Password == "" {
		return nil, fmt.Errorf("无效的参数")
	}
	if err := validateBoundedField(input.Username, "用户名", maxUsernameLength); err != nil {
		return nil, fmt.Errorf("无效的参数")
	}
	if err := validateBoundedField(input.Password, "密码", maxPasswordLength); err != nil {
		return nil, fmt.Errorf("无效的参数")
	}
	user := model.User{Username: input.Username, Password: input.Password}
	if err := user.ValidateAndFill(); err != nil {
		return nil, err
	}
	return &user, nil
}

func RegisterUser(user model.User) error {
	if !common.RegisterEnabled {
		return fmt.Errorf("管理员关闭了新用户注册")
	}
	if !common.PasswordRegisterEnabled {
		return fmt.Errorf("管理员关闭了通过密码进行注册，请使用第三方账户验证的形式进行注册")
	}
	if err := validateBoundedField(user.Username, "用户名", maxUsernameLength); err != nil {
		return fmt.Errorf("输入不合法 %s", err.Error())
	}
	if err := validateBoundedField(user.Password, "密码", maxPasswordLength); err != nil {
		return fmt.Errorf("输入不合法 %s", err.Error())
	}
	if err := validateBoundedField(user.VerificationCode, "验证码", maxVerificationCodeLength); err != nil {
		return fmt.Errorf("输入不合法 %s", err.Error())
	}
	var err error
	user.Email, err = canonicalEmail(user.Email)
	if err != nil {
		return fmt.Errorf("输入不合法 %s", err.Error())
	}
	if err := validation.Validate.Struct(&user); err != nil {
		return fmt.Errorf("输入不合法 %s", err.Error())
	}
	if common.EmailVerificationEnabled {
		if user.Email == "" || user.VerificationCode == "" {
			return fmt.Errorf("管理员开启了邮箱验证，请输入邮箱地址和验证码")
		}
		if !security.ConsumeCodeWithKey(user.Email, user.VerificationCode, security.EmailVerificationPurpose) {
			return fmt.Errorf("验证码错误或已过期")
		}
	}
	cleanUser := model.User{
		Username:    user.Username,
		Password:    user.Password,
		DisplayName: user.Username,
	}
	if common.EmailVerificationEnabled {
		cleanUser.Email = user.Email
	}
	if err := cleanUser.Insert(); err != nil {
		if model.IsUniqueConstraintError(err) {
			return fmt.Errorf("用户名或邮箱地址已被占用")
		}
		return err
	}
	return nil
}

func ListUsers(page int) ([]*model.User, error) {
	if page < 0 {
		page = 0
	}
	return model.GetAllUsers(page*common.ItemsPerPage, common.ItemsPerPage)
}

func SearchUsers(keyword string) ([]*model.User, error) {
	return model.SearchUsers(keyword)
}

func GetUserForRole(targetID int, requesterRole int) (*model.User, error) {
	user, err := model.GetUserById(targetID, false)
	if err != nil {
		return nil, err
	}
	if requesterRole <= user.Role {
		return nil, fmt.Errorf("无权获取同级或更高等级用户的信息")
	}
	return user, nil
}

func GetSelf(id int) (*model.User, error) {
	return model.GetUserById(id, false)
}

func GenerateUserToken(id int) (string, error) {
	user, err := model.GetUserById(id, true)
	if err != nil {
		return "", err
	}
	if user.Status != common.UserStatusEnabled || user.AuthVersion == 0 {
		return "", fmt.Errorf("用户凭据状态已变化，请重新登录后重试")
	}
	randomPart := security.GenerateRandomString(32)
	if randomPart == "" {
		return "", fmt.Errorf("生成访问令牌失败，请重试")
	}
	token := fmt.Sprintf("v%d.%s", user.AuthVersion, randomPart)
	if model.DB.Where("token = ?", token).First(&model.User{}).RowsAffected != 0 {
		return "", fmt.Errorf("访问令牌发生冲突，请重试")
	}
	result := model.DB.Model(&model.User{}).
		Where("id = ? AND auth_version = ? AND status = ?", user.Id, user.AuthVersion, common.UserStatusEnabled).
		Update("token", token)
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected != 1 {
		return "", fmt.Errorf("用户凭据状态已变化，请重新登录后重试")
	}
	return token, nil
}

func UpdateUserAsAdmin(updatedUser model.User, requesterRole int) error {
	if updatedUser.Id == 0 {
		return fmt.Errorf("无效的参数")
	}
	originalPassword := updatedUser.Password
	if updatedUser.Password == "" {
		updatedUser.Password = "$I_LOVE_U"
	}
	if err := validation.Validate.Struct(&updatedUser); err != nil {
		return fmt.Errorf("输入不合法 %s", err.Error())
	}
	if updatedUser.Role != 0 && updatedUser.Role != common.RoleCommonUser && updatedUser.Role != common.RoleAdminUser {
		return fmt.Errorf("无效的用户权限等级")
	}
	if updatedUser.Status != 0 && updatedUser.Status != common.UserStatusEnabled && updatedUser.Status != common.UserStatusDisabled {
		return fmt.Errorf("无效的用户状态")
	}
	if updatedUser.Role != 0 && requesterRole <= updatedUser.Role {
		return fmt.Errorf("无权将其他用户权限等级提升到大于等于自己的权限等级")
	}
	updatedUser.Password = originalPassword
	if err := model.UpdateUserAsAdminAtomic(&updatedUser, requesterRole, originalPassword != "", originalPassword != ""); err != nil {
		if errors.Is(err, model.ErrUserMutationForbidden) {
			return fmt.Errorf("无权更新同权限等级或更高权限等级的用户信息")
		}
		if model.IsUniqueConstraintError(err) {
			return fmt.Errorf("用户名或外部身份已被其他用户占用")
		}
		return err
	}
	return nil
}

func UpdateSelf(id int, user model.User) (*model.User, error) {
	originalPassword := user.Password
	if user.Password == "" {
		user.Password = "$I_LOVE_U"
	}
	if err := validation.Validate.Struct(&user); err != nil {
		return nil, fmt.Errorf("输入不合法 %s", err.Error())
	}
	current, err := model.GetUserById(id, true)
	if err != nil {
		return nil, err
	}
	if current.MustChangePassword && originalPassword == "" {
		return nil, fmt.Errorf("首次登录或密码重置后必须设置新密码")
	}
	if originalPassword != "" && security.ValidatePasswordAndHash(originalPassword, current.Password) {
		return nil, fmt.Errorf("新密码不能与当前密码相同")
	}
	cleanUser := model.User{
		Id:          id,
		Username:    user.Username,
		Password:    originalPassword,
		DisplayName: user.DisplayName,
	}
	if err := cleanUser.UpdateWithPasswordPolicy(originalPassword != "", false); err != nil {
		return nil, err
	}
	return model.GetUserById(id, true)
}

func DeleteUserAsAdmin(id int, requesterRole int) error {
	if err := model.DeleteUserAsAdminAtomic(id, requesterRole); err != nil {
		if errors.Is(err, model.ErrUserMutationForbidden) {
			return fmt.Errorf("无权删除同权限等级或更高权限等级的用户")
		}
		return err
	}
	return nil
}

func DeleteSelf(id int) error {
	return model.DeleteUserById(id)
}

func CreateUserAsAdmin(user model.User, requesterRole int) error {
	if user.Username == "" || user.Password == "" {
		return fmt.Errorf("无效的参数")
	}
	if user.DisplayName == "" {
		user.DisplayName = user.Username
	}
	if user.Role >= requesterRole {
		return fmt.Errorf("无法创建权限大于等于自己的用户")
	}
	cleanUser := model.User{
		Username:           user.Username,
		Password:           user.Password,
		DisplayName:        user.DisplayName,
		MustChangePassword: true,
	}
	return cleanUser.Insert()
}

func ManageUser(input ManageUserInput, requesterRole int) (*model.User, error) {
	if input.Username == "" || validateBoundedField(input.Username, "用户名", maxUsernameLength) != nil || len(input.Action) > 16 {
		return nil, fmt.Errorf("无效的参数")
	}
	credentialUpdates := func(values map[string]any) map[string]any {
		values["auth_version"] = gorm.Expr("CASE WHEN auth_version < 1 THEN 2 ELSE auth_version + 1 END")
		values["token"] = ""
		return values
	}
	base := model.DB.Model(&model.User{}).Where("username = ? AND role < ?", input.Username, requesterRole)
	var result *gorm.DB
	switch input.Action {
	case "disable":
		result = base.Updates(credentialUpdates(map[string]any{"status": common.UserStatusDisabled}))
	case "enable":
		result = base.Updates(credentialUpdates(map[string]any{"status": common.UserStatusEnabled}))
	case "delete":
		var snapshot model.User
		if err := model.DB.Select("id", "role", "status").Where("username = ?", input.Username).First(&snapshot).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("用户不存在")
			}
			return nil, err
		}
		result = model.DB.Where("id = ? AND role < ?", snapshot.Id, requesterRole).Delete(&model.User{})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected != 1 {
			return nil, fmt.Errorf("无权更新同权限等级或更高权限等级的用户信息")
		}
		return &model.User{Role: snapshot.Role, Status: snapshot.Status}, nil
	case "promote":
		if requesterRole != common.RoleRootUser {
			return nil, fmt.Errorf("普通管理员用户无法提升其他用户为管理员")
		}
		result = model.DB.Model(&model.User{}).
			Where("username = ? AND role < ?", input.Username, common.RoleAdminUser).
			Updates(credentialUpdates(map[string]any{"role": common.RoleAdminUser}))
	case "demote":
		result = model.DB.Model(&model.User{}).
			Where("username = ? AND role >= ? AND role < ?", input.Username, common.RoleAdminUser, requesterRole).
			Updates(credentialUpdates(map[string]any{"role": common.RoleCommonUser}))
	default:
		return nil, fmt.Errorf("无效的参数")
	}
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("用户不存在、状态未变化或无权执行该操作")
	}
	var user model.User
	if err := model.DB.Where("username = ?", input.Username).First(&user).Error; err != nil {
		return nil, err
	}
	return &model.User{Role: user.Role, Status: user.Status}, nil
}

func BindEmail(ctx context.Context, id int, email string, code string) error {
	var err error
	email, err = canonicalEmail(email)
	if err != nil || email == "" || validateBoundedField(code, "验证码", maxVerificationCodeLength) != nil {
		return fmt.Errorf("无效的参数")
	}
	if !security.ConsumeCodeWithKey(email, code, security.EmailVerificationPurpose) {
		return fmt.Errorf("验证码错误或已过期")
	}
	if err := model.BindUserEmail(ctx, id, email); err != nil {
		if errors.Is(err, model.ErrUserIdentityTaken) {
			return fmt.Errorf("邮箱地址已被占用")
		}
		return err
	}
	return nil
}

func SendEmailVerification(email string) error {
	var err error
	email, err = canonicalEmail(email)
	if err != nil {
		return fmt.Errorf("无效的参数")
	}
	if err := validation.Validate.Var(email, "required,email"); err != nil {
		return fmt.Errorf("无效的参数")
	}
	if model.IsEmailAlreadyTaken(email) {
		return fmt.Errorf("邮箱地址已被占用")
	}
	code := security.GenerateVerificationCode(6)
	security.RegisterVerificationCodeWithKey(email, code, security.EmailVerificationPurpose)
	subject := fmt.Sprintf("%s邮箱验证邮件", common.SystemName)
	content := fmt.Sprintf("<p>您好，你正在进行%s邮箱验证。</p><p>您的验证码为: <strong>%s</strong></p><p>验证码 %d 分钟内有效，如果不是本人操作，请忽略。</p>", common.SystemName, code, security.VerificationValidMinutes)
	if err := mail.SendEmail(subject, email, content); err != nil {
		security.DeleteKey(email, security.EmailVerificationPurpose)
		return err
	}
	return nil
}

func SendPasswordResetEmail(email string) error {
	var err error
	email, err = canonicalEmail(email)
	if err != nil {
		return fmt.Errorf("无效的参数")
	}
	if err := validation.Validate.Var(email, "required,email"); err != nil {
		return fmt.Errorf("无效的参数")
	}
	if !model.IsEmailAlreadyTaken(email) {
		return fmt.Errorf("该邮箱地址未注册")
	}
	code := security.GenerateVerificationCode(0)
	security.RegisterVerificationCodeWithKey(email, code, security.PasswordResetPurpose)
	link := fmt.Sprintf("%s/user/reset?email=%s&token=%s", common.ServerAddress, url.QueryEscape(email), url.QueryEscape(code))
	subject := fmt.Sprintf("%s密码重置", common.SystemName)
	content := fmt.Sprintf("<p>您好，你正在进行%s密码重置。</p><p>点击<a href='%s'>此处</a>进行密码重置。</p><p>重置链接 %d 分钟内有效，如果不是本人操作，请忽略。</p>", common.SystemName, link, security.VerificationValidMinutes)
	if err := mail.SendEmail(subject, email, content); err != nil {
		security.DeleteKey(email, security.PasswordResetPurpose)
		return err
	}
	return nil
}

func ResetPassword(input PasswordResetInput) (string, error) {
	var err error
	input.Email, err = canonicalEmail(input.Email)
	if err != nil || input.Email == "" || input.Token == "" || validateBoundedField(input.Token, "重置令牌", maxVerificationCodeLength) != nil {
		return "", fmt.Errorf("无效的参数")
	}
	if !security.ConsumeCodeWithKey(input.Email, input.Token, security.PasswordResetPurpose) {
		return "", fmt.Errorf("重置链接非法或已过期")
	}
	password := security.GenerateVerificationCode(12)
	if err := model.ResetUserPasswordByEmail(input.Email, password); err != nil {
		return "", err
	}
	return password, nil
}

func NormalizeLocalRedirect(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/", nil
	}
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, "\\") {
		return "", fmt.Errorf("redirect 必须是站内绝对路径")
	}
	if containsRedirectControl(value) {
		return "", fmt.Errorf("redirect 包含非法字符")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil ||
		parsed.IsAbs() ||
		parsed.Host != "" ||
		!strings.HasPrefix(parsed.Path, "/") ||
		strings.HasPrefix(parsed.Path, "//") ||
		strings.Contains(parsed.Path, "\\") ||
		containsRedirectControl(parsed.Path) {
		return "", fmt.Errorf("redirect 必须是站内绝对路径")
	}
	return value, nil
}

func containsRedirectControl(value string) bool {
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return true
		}
	}
	return false
}

func BuildGitHubAuthorizationURL(state string) (string, error) {
	if !common.GitHubOAuthEnabled || common.GitHubClientId == "" {
		return "", fmt.Errorf("管理员未开启通过 GitHub 登录以及注册")
	}
	if state == "" || validateBoundedField(state, "OAuth state", maxOAuthStateLength) != nil {
		return "", fmt.Errorf("OAuth state 不能为空")
	}
	authorizeURL := url.URL{
		Scheme: "https",
		Host:   "github.com",
		Path:   "/login/oauth/authorize",
	}
	query := authorizeURL.Query()
	query.Set("client_id", common.GitHubClientId)
	query.Set("scope", "user:email")
	query.Set("state", state)
	authorizeURL.RawQuery = query.Encode()
	return authorizeURL.String(), nil
}

func HandleGitHubOAuth(ctx context.Context, code string) (*model.User, error) {
	if !common.GitHubOAuthEnabled {
		return nil, fmt.Errorf("管理员未开启通过 GitHub 登录以及注册")
	}
	githubUser, err := getGitHubUserInfoByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	user, err := resolveGitHubUser(githubUser)
	if err != nil {
		return nil, err
	}
	if user.Status != common.UserStatusEnabled {
		return nil, fmt.Errorf("用户已被封禁")
	}
	return user, nil
}

func BindGitHubAccount(ctx context.Context, userID int, code string) error {
	if !common.GitHubOAuthEnabled {
		return fmt.Errorf("管理员未开启通过 GitHub 登录以及注册")
	}
	githubUser, err := getGitHubUserInfoByCode(ctx, code)
	if err != nil {
		return err
	}
	identity := strconv.FormatInt(githubUser.ID, 10)
	if githubUser.ID <= 0 {
		return fmt.Errorf("GitHub 返回了无效的数字身份")
	}
	if err := model.BindUserGitHubIdentity(ctx, userID, identity); err != nil {
		if errors.Is(err, model.ErrUserIdentityTaken) {
			return fmt.Errorf("该 GitHub 账户已被绑定")
		}
		return err
	}
	return nil
}

func resolveGitHubUser(githubUser *GitHubUser) (*model.User, error) {
	if githubUser == nil || githubUser.ID <= 0 {
		return nil, fmt.Errorf("GitHub 返回了无效的数字身份")
	}
	identity := strconv.FormatInt(githubUser.ID, 10)
	user := model.User{GitHubId: identity}
	if err := user.FillUserByGitHubId(); err == nil {
		return &user, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if !common.RegisterEnabled {
		return nil, fmt.Errorf("管理员关闭了新用户注册")
	}

	displayName := truncateRunes(strings.TrimSpace(githubUser.Name), maxDisplayNameLength)
	if displayName == "" {
		displayName = "GitHub User"
	}
	return createExternalIdentityUser("gh_", displayName, func(candidate *model.User) {
		candidate.GitHubId = identity
	}, func() (*model.User, error) {
		existing := model.User{GitHubId: identity}
		if err := existing.FillUserByGitHubId(); err != nil {
			return nil, err
		}
		return &existing, nil
	})
}

func HandleWeChatOAuth(code string) (*model.User, error) {
	if !common.WeChatAuthEnabled {
		return nil, fmt.Errorf("管理员未开启通过微信登录以及注册")
	}
	wechatID, err := getWeChatIDByCode(code)
	if err != nil {
		return nil, err
	}
	wechatID = model.NormalizeWeChatIdentity(wechatID)
	if wechatID == "" || validateBoundedField(wechatID, "微信身份", maxWeChatIdentityLength) != nil {
		return nil, fmt.Errorf("微信返回了无效的身份")
	}
	user := model.User{WeChatId: wechatID}
	if err := user.FillUserByWeChatId(); errors.Is(err, gorm.ErrRecordNotFound) {
		if !common.RegisterEnabled {
			return nil, fmt.Errorf("管理员关闭了新用户注册")
		}
		created, createErr := createExternalIdentityUser("wx_", "WeChat User", func(candidate *model.User) {
			candidate.WeChatId = wechatID
		}, func() (*model.User, error) {
			existing := model.User{WeChatId: wechatID}
			if err := existing.FillUserByWeChatId(); err != nil {
				return nil, err
			}
			return &existing, nil
		})
		if createErr != nil {
			return nil, createErr
		}
		user = *created
	} else if err != nil {
		return nil, err
	}
	if user.Status != common.UserStatusEnabled {
		return nil, fmt.Errorf("用户已被封禁")
	}
	return &user, nil
}

func BindWeChatAccount(userID int, code string) error {
	return BindWeChatAccountContext(context.Background(), userID, code)
}

func BindWeChatAccountContext(ctx context.Context, userID int, code string) error {
	if !common.WeChatAuthEnabled {
		return fmt.Errorf("管理员未开启通过微信登录以及注册")
	}
	wechatID, err := getWeChatIDByCode(code)
	if err != nil {
		return err
	}
	wechatID = model.NormalizeWeChatIdentity(wechatID)
	if wechatID == "" || validateBoundedField(wechatID, "微信身份", maxWeChatIdentityLength) != nil {
		return fmt.Errorf("微信返回了无效的身份")
	}
	if err := model.BindUserWeChatIdentity(ctx, userID, wechatID); err != nil {
		if errors.Is(err, model.ErrUserIdentityTaken) {
			return fmt.Errorf("该微信账号已被绑定")
		}
		return err
	}
	return nil
}

func createExternalIdentityUser(
	usernamePrefix string,
	displayName string,
	assignIdentity func(*model.User),
	loadConcurrentWinner func() (*model.User, error),
) (*model.User, error) {
	for attempt := 0; attempt < 5; attempt++ {
		suffix := security.GenerateRandomString(maxUsernameLength - len(usernamePrefix))
		if suffix == "" {
			return nil, fmt.Errorf("生成第三方登录用户名失败")
		}
		user := model.User{
			Username:    usernamePrefix + suffix,
			DisplayName: truncateRunes(displayName, maxDisplayNameLength),
			Role:        common.RoleCommonUser,
			Status:      common.UserStatusEnabled,
		}
		assignIdentity(&user)
		if err := user.Insert(); err == nil {
			return &user, nil
		} else if !model.IsUniqueConstraintError(err) {
			return nil, err
		}
		if winner, err := loadConcurrentWinner(); err == nil {
			return winner, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("创建第三方登录用户时发生用户名冲突，请重试")
}

func getGitHubUserInfoByCode(ctx context.Context, code string) (*GitHubUser, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	return getGitHubUserInfoByCodeWithClient(
		ctx,
		code,
		client,
		"https://github.com/login/oauth/access_token",
		"https://api.github.com/user",
	)
}

func getGitHubUserInfoByCodeWithClient(
	ctx context.Context,
	code string,
	client *http.Client,
	tokenEndpoint string,
	userEndpoint string,
) (*GitHubUser, error) {
	if code == "" || validateBoundedField(code, "OAuth code", maxOAuthCodeLength) != nil {
		return nil, errors.New("无效的参数")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		return nil, errors.New("GitHub HTTP client 为空")
	}
	values := map[string]string{"client_id": common.GitHubClientId, "client_secret": common.GitHubClientSecret, "code": code}
	jsonData, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		slog.Error("github oauth access token request failed", "error", err)
		return nil, errors.New("无法连接至 GitHub 服务器，请稍后重试！")
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("GitHub OAuth 返回异常状态: %s", res.Status)
	}
	var oauthResponse GitHubOAuthResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, maxOAuthResponseBytes)).Decode(&oauthResponse); err != nil {
		return nil, err
	}
	if oauthResponse.AccessToken == "" {
		return nil, errors.New("GitHub OAuth 未返回访问令牌")
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, userEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", oauthResponse.AccessToken))
	res2, err := client.Do(req)
	if err != nil {
		slog.Error("github user info request failed", "error", err)
		return nil, errors.New("无法连接至 GitHub 服务器，请稍后重试！")
	}
	defer res2.Body.Close()
	if res2.StatusCode < http.StatusOK || res2.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("GitHub 用户接口返回异常状态: %s", res2.Status)
	}
	var githubUser GitHubUser
	if err := json.NewDecoder(io.LimitReader(res2.Body, maxOAuthResponseBytes)).Decode(&githubUser); err != nil {
		return nil, err
	}
	if githubUser.ID <= 0 {
		return nil, errors.New("返回值非法，GitHub 数字 id 为空，请稍后重试！")
	}
	return &githubUser, nil
}

func getWeChatIDByCode(code string) (string, error) {
	if code == "" || validateBoundedField(code, "微信验证码", maxOAuthCodeLength) != nil {
		return "", errors.New("无效的参数")
	}
	endpoint := strings.TrimRight(common.WeChatServerAddress, "/") + "/api/wechat/user?code=" + url.QueryEscape(code)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", common.WeChatServerToken)
	client := http.Client{Timeout: 5 * time.Second}
	httpResponse, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer httpResponse.Body.Close()
	var res WeChatLoginResponse
	if err := json.NewDecoder(httpResponse.Body).Decode(&res); err != nil {
		return "", err
	}
	if !res.Success {
		return "", errors.New(res.Message)
	}
	if res.Data == "" {
		return "", errors.New("验证码错误或已过期")
	}
	return res.Data, nil
}
