package controller

import (
	"net/url"
	"poolx/internal/middleware"
	"poolx/internal/model"
	"poolx/internal/service"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

const authAuditFieldLimit = 64

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type ManageRequest struct {
	Username string `json:"username"`
	Action   string `json:"action"`
}

type EmailBindRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type WeChatBindRequest struct {
	Code string `json:"code"`
}

func Login(c *gin.Context) {
	var loginRequest LoginRequest
	if err := decodeJSONBody(c.Request.Body, &loginRequest); err != nil {
		_ = service.AppLog.Push(model.AppLogClassificationSecurity, model.AppLogLevelWarn, "login rejected | invalid request payload")
		respondFailure(c, "无效的参数")
		return
	}
	user, err := service.AuthenticateUser(service.LoginInput(loginRequest))
	if err != nil {
		_ = service.AppLog.Push(model.AppLogClassificationSecurity, model.AppLogLevelWarn, "login failed | username="+authAuditValue(loginRequest.Username)+" | reason="+authAuditValue(err.Error()))
		respondFailure(c, err.Error())
		return
	}
	_ = service.AppLog.Push(model.AppLogClassificationSecurity, model.AppLogLevelInfo, "login succeeded | username="+authAuditValue(user.Username))
	setupLogin(user, c)
}

func setupLogin(user *model.User, c *gin.Context) {
	if err := middleware.SetAuthenticatedSession(c, user); err != nil {
		respondFailure(c, "无法保存会话信息，请重试")
		return
	}
	respondSuccess(c, cleanAuthUser(user))
}

func cleanAuthUser(user *model.User) model.User {
	return model.User{
		Id:                 user.Id,
		Username:           user.Username,
		DisplayName:        user.DisplayName,
		Role:               user.Role,
		Status:             user.Status,
		MustChangePassword: user.MustChangePassword,
		Email:              user.Email,
		GitHubId:           user.GitHubId,
		WeChatId:           user.WeChatId,
	}
}

func Logout(c *gin.Context) {
	username := c.GetString("username")
	if err := middleware.DestroySession(c); err != nil {
		respondFailure(c, err.Error())
		return
	}
	_ = service.AppLog.Push(model.AppLogClassificationSecurity, model.AppLogLevelInfo, "logout succeeded | username="+authAuditValue(username))
	respondSuccessMessage(c, "")
}

func Register(c *gin.Context) {
	var user model.User
	if err := decodeJSONBody(c.Request.Body, &user); err != nil {
		_ = service.AppLog.Push(model.AppLogClassificationSecurity, model.AppLogLevelWarn, "register rejected | invalid request payload")
		respondFailure(c, "无效的参数")
		return
	}
	if err := service.RegisterUser(user); err != nil {
		_ = service.AppLog.Push(model.AppLogClassificationSecurity, model.AppLogLevelWarn, "register failed | username="+authAuditValue(user.Username)+" | reason="+authAuditValue(err.Error()))
		respondFailure(c, err.Error())
		return
	}
	_ = service.AppLog.Push(model.AppLogClassificationBusiness, model.AppLogLevelInfo, "register succeeded | username="+authAuditValue(user.Username))
	respondSuccessMessage(c, "")
}

func GetAllUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("p"))
	users, err := service.ListUsers(page)
	if err != nil {
		respondFailure(c, err.Error())
		return
	}
	respondSuccess(c, users)
}

func SearchUsers(c *gin.Context) {
	users, err := service.SearchUsers(c.Query("keyword"))
	if err != nil {
		respondFailure(c, err.Error())
		return
	}
	respondSuccess(c, users)
}

func GetUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		respondFailure(c, err.Error())
		return
	}
	user, err := service.GetUserForRole(id, c.GetInt("role"))
	if err != nil {
		respondFailure(c, err.Error())
		return
	}
	respondSuccess(c, user)
}

func GenerateToken(c *gin.Context) {
	token, err := service.GenerateUserToken(c.GetInt("id"))
	if err != nil {
		respondFailure(c, err.Error())
		return
	}
	respondSuccess(c, token)
}

func GetSelf(c *gin.Context) {
	user, err := service.GetSelf(c.GetInt("id"))
	if err != nil {
		respondFailure(c, err.Error())
		return
	}
	respondSuccess(c, user)
}

func UpdateUser(c *gin.Context) {
	var updatedUser model.User
	if err := decodeJSONBody(c.Request.Body, &updatedUser); err != nil || updatedUser.Id == 0 {
		respondFailure(c, "无效的参数")
		return
	}
	if err := service.UpdateUserAsAdmin(updatedUser, c.GetInt("role")); err != nil {
		respondFailure(c, err.Error())
		return
	}
	respondSuccessMessage(c, "")
}

func UpdateSelf(c *gin.Context) {
	var user model.User
	if err := decodeJSONBody(c.Request.Body, &user); err != nil {
		respondFailure(c, "无效的参数")
		return
	}
	updatedUser, err := service.UpdateSelf(c.GetInt("id"), user)
	if err != nil {
		respondFailure(c, err.Error())
		return
	}
	if err := middleware.SetAuthenticatedSession(c, updatedUser); err != nil {
		respondFailure(c, "资料已更新，但无法刷新会话，请重新登录")
		return
	}
	respondSuccessMessage(c, "")
}

func DeleteUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		respondFailure(c, err.Error())
		return
	}
	if err := service.DeleteUserAsAdmin(id, c.GetInt("role")); err != nil {
		respondFailure(c, err.Error())
		return
	}
	respondSuccessMessage(c, "")
}

func DeleteSelf(c *gin.Context) {
	if err := service.DeleteSelf(c.GetInt("id")); err != nil {
		respondFailure(c, err.Error())
		return
	}
	_ = middleware.DestroySession(c)
	respondSuccessMessage(c, "")
}

func CreateUser(c *gin.Context) {
	var user model.User
	if err := decodeJSONBody(c.Request.Body, &user); err != nil {
		respondFailure(c, "无效的参数")
		return
	}
	if err := service.CreateUserAsAdmin(user, c.GetInt("role")); err != nil {
		respondFailure(c, err.Error())
		return
	}
	respondSuccessMessage(c, "")
}

func ManageUser(c *gin.Context) {
	var req ManageRequest
	if err := decodeJSONBody(c.Request.Body, &req); err != nil {
		respondFailure(c, "无效的参数")
		return
	}
	user, err := service.ManageUser(service.ManageUserInput(req), c.GetInt("role"))
	if err != nil {
		respondFailure(c, err.Error())
		return
	}
	respondSuccess(c, user)
}

func EmailBind(c *gin.Context) {
	if !requireSameOriginMutation(c) {
		return
	}
	var req EmailBindRequest
	if err := decodeJSONBody(c.Request.Body, &req); err != nil {
		respondFailure(c, "无效的参数")
		return
	}
	if err := service.BindEmail(c.Request.Context(), c.GetInt("id"), req.Email, req.Code); err != nil {
		respondFailure(c, err.Error())
		return
	}
	respondSuccessMessage(c, "")
}

func WeChatBindSecure(c *gin.Context) {
	if !requireSameOriginMutation(c) {
		return
	}
	var req WeChatBindRequest
	if err := decodeJSONBody(c.Request.Body, &req); err != nil {
		respondFailure(c, "无效的参数")
		return
	}
	if err := service.BindWeChatAccountContext(c.Request.Context(), c.GetInt("id"), req.Code); err != nil {
		respondFailure(c, err.Error())
		return
	}
	respondSuccessMessage(c, "")
}

func requireSameOriginMutation(c *gin.Context) bool {
	if contentType := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type"))); !strings.HasPrefix(contentType, "application/json") {
		respondBadRequest(c, "仅接受 JSON 请求")
		return false
	}
	if site := strings.ToLower(strings.TrimSpace(c.GetHeader("Sec-Fetch-Site"))); site != "" && site != "same-origin" {
		respondBadRequest(c, "跨站请求已被拒绝")
		return false
	}

	source := strings.TrimSpace(c.GetHeader("Origin"))
	if source == "" {
		source = strings.TrimSpace(c.GetHeader("Referer"))
	}
	parsed, err := url.Parse(source)
	if source == "" || err != nil || parsed.Host == "" || !strings.EqualFold(parsed.Host, c.Request.Host) {
		respondBadRequest(c, "请求来源无效")
		return false
	}
	return true
}

func authAuditValue(value string) string {
	value = strings.Map(func(char rune) rune {
		if unicode.IsControl(char) {
			return ' '
		}
		return char
	}, strings.TrimSpace(value))
	if utf8.RuneCountInString(value) <= authAuditFieldLimit {
		return value
	}
	runes := []rune(value)
	return string(runes[:authAuditFieldLimit]) + "..."
}
