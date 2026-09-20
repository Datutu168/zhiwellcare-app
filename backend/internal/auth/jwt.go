package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenManager 签发/校验 JWT 访问令牌。
type TokenManager struct {
	secret []byte
	ttl    time.Duration
}

// Claims 是访问令牌携带的自定义声明。
type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

func NewTokenManager(secret string, ttl time.Duration) *TokenManager {
	return &TokenManager{secret: []byte(secret), ttl: ttl}
}

func (m *TokenManager) Issue(userID, role string) (token string, expiresInSeconds int64, err error) {
	now := time.Now()
	tokenID, err := newTokenID()
	if err != nil {
		return "", 0, err
	}
	claims := Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			// ID 为令牌唯一标识（jti），用于会话吊销名单与审计追踪。
			ID:        tokenID,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
			Issuer:    "zhiwellcare",
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", 0, err
	}
	return signed, int64(m.ttl.Seconds()), nil
}

var ErrInvalidToken = errors.New("令牌无效或已过期")

// ParseClaims 校验访问令牌并返回完整声明。
// 登出需要按令牌剩余有效期登记吊销名单，因此单独暴露声明。
func (m *TokenManager) ParseClaims(raw string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(raw, &Claims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, ErrInvalidToken
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, ErrInvalidToken
	}
	if claims.Subject == "" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// Parse 校验访问令牌并返回用户 ID 与角色。
func (m *TokenManager) Parse(raw string) (userID, role string, err error) {
	claims, err := m.ParseClaims(raw)
	if err != nil {
		return "", "", err
	}
	return claims.Subject, claims.Role, nil
}

// RemainingTTL 返回令牌剩余有效期；已过期或缺少 exp 时返回 0。
func (c *Claims) RemainingTTL() time.Duration {
	if c == nil || c.ExpiresAt == nil {
		return 0
	}
	remaining := time.Until(c.ExpiresAt.Time)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// newTokenID 生成访问令牌唯一标识（jti）。
func newTokenID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// NewRefreshSecret 生成 32 字节随机刷新令牌明文（64 位十六进制）。
func NewRefreshSecret() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// HashRefreshToken 仅保存刷新令牌的 SHA-256 哈希，泄库不可重放。
func HashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
