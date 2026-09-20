package httpapi

import (
	"context"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"zhiwellcare/backend/internal/auth"
	"zhiwellcare/backend/internal/cache"
	"zhiwellcare/backend/internal/model"
	"zhiwellcare/backend/internal/store"
	"zhiwellcare/backend/internal/wechat"
)

// phonePattern 中国大陆手机号。
var phonePattern = regexp.MustCompile(`^1[3-9]\d{9}$`)

// WeChatExchange 微信 code2session 抽象（真实 Client 或本地 Mock）。
type WeChatExchange func(ctx context.Context, code string) (*wechat.Session, error)

type authHandler struct {
	store      store.Combined
	manager    *auth.TokenManager
	refreshTTL time.Duration
	exchange   WeChatExchange
	// sessions 访问令牌吊销名单；nil 时登出只作废刷新令牌。
	sessions *cache.Sessions
}

type registerRequest struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
}

type loginRequest struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type logoutRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type authResponse struct {
	User   model.SafeUser  `json:"user"`
	Tokens model.TokenPair `json:"tokens"`
}

func (h *authHandler) register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	req.Phone = trimSpace(req.Phone)
	req.Nickname = trimSpace(req.Nickname)
	if !phonePattern.MatchString(req.Phone) {
		failBadRequest(c, "请输入正确的手机号")
		return
	}
	if message := validatePassword(req.Password); message != "" {
		failBadRequest(c, message)
		return
	}
	if utf8.RuneCountInString(req.Nickname) > 20 {
		failBadRequest(c, "昵称最长 20 个字符")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	user, err := h.store.CreateUser(c, store.CreateUserParams{
		Phone:        req.Phone,
		PasswordHash: hash,
		Nickname:     req.Nickname,
	})
	if err != nil {
		mapStoreError(c, err)
		return
	}
	tokens, err := h.issuePair(c, user)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, authResponse{User: user.ToSafe(), Tokens: *tokens})
}

func (h *authHandler) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	req.Phone = trimSpace(req.Phone)
	if !phonePattern.MatchString(req.Phone) {
		failUnauthorized(c, "手机号或密码不正确")
		return
	}
	user, err := h.store.GetUserByPhone(c, req.Phone)
	if err != nil {
		// 用户不存在与密码错误返回同一文案，避免账号枚举。
		if err == store.ErrUserNotFound {
			failUnauthorized(c, "手机号或密码不正确")
			return
		}
		mapStoreError(c, err)
		return
	}
	if user.Status != 1 {
		fail(c, 403, "该账号已被停用，请联系客服")
		return
	}
	if !auth.VerifyPassword(user.PasswordHash, req.Password) {
		failUnauthorized(c, "手机号或密码不正确")
		return
	}
	_ = h.store.TouchLastLogin(c, user.ID)
	tokens, err := h.issuePair(c, user)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, authResponse{User: user.ToSafe(), Tokens: *tokens})
}

func (h *authHandler) refresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	raw := trimSpace(req.RefreshToken)
	if raw == "" {
		failUnauthorized(c, "登录状态已失效，请重新登录")
		return
	}
	hash := auth.HashRefreshToken(raw)
	row, err := h.store.FindByHash(c, hash)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	if row.RevokedAt != nil {
		failUnauthorized(c, "登录状态已失效，请重新登录")
		return
	}
	if time.Now().After(row.ExpiresAt) {
		failUnauthorized(c, "登录状态已过期，请重新登录")
		return
	}
	user, err := h.store.GetUserByID(c, row.UserID)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	if user.Status != 1 {
		fail(c, 403, "该账号已被停用，请联系客服")
		return
	}
	// 轮换：旧刷新令牌作废，签发新对（旧令牌被重放时无法二次使用）。
	tokens, err := h.issuePair(c, user)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	_ = h.store.Revoke(c, hash)
	ok(c, authResponse{User: user.ToSafe(), Tokens: *tokens})
}

func (h *authHandler) logout(c *gin.Context) {
	var req logoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	if raw := trimSpace(req.RefreshToken); raw != "" {
		_ = h.store.Revoke(c, auth.HashRefreshToken(raw)) // 幂等：不存在也视为成功
	}
	// 访问令牌是 JWT（无状态），这里按剩余有效期登记进吊销名单，
	// 让「已登出的令牌」立即失效而不是等到自然过期。
	if h.sessions != nil {
		if raw := bearerToken(c.GetHeader("Authorization")); raw != "" {
			if claims, err := h.manager.ParseClaims(raw); err == nil {
				_ = h.sessions.Revoke(c, cache.Fingerprint(raw), claims.RemainingTTL())
			}
		}
	}
	ok(c, gin.H{"loggedOut": true})
}

// issuePair 签发访问令牌 + 刷新令牌，并把刷新令牌哈希入库。
func (h *authHandler) issuePair(ctx *gin.Context, user *model.User) (*model.TokenPair, error) {
	access, expiresIn, err := h.manager.Issue(user.ID, user.Role)
	if err != nil {
		return nil, err
	}
	refreshSecret, err := auth.NewRefreshSecret()
	if err != nil {
		return nil, err
	}
	if err := h.store.Save(ctx, user.ID, auth.HashRefreshToken(refreshSecret), time.Now().Add(h.refreshTTL)); err != nil {
		return nil, err
	}
	return &model.TokenPair{
		AccessToken:      access,
		RefreshToken:     refreshSecret,
		ExpiresInSeconds: expiresIn,
	}, nil
}

func validatePassword(password string) string {
	if len(password) < 6 {
		return "密码至少 6 位"
	}
	if len(password) > 64 {
		return "密码最长 64 位"
	}
	return ""
}

func trimSpace(value string) string {
	start := 0
	end := len(value)
	for start < end && value[start] == ' ' {
		start++
	}
	for end > start && value[end-1] == ' ' {
		end--
	}
	return value[start:end]
}
