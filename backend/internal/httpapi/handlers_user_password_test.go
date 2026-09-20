package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"zhiwellcare/backend/internal/store"
)

// putBody 以 PUT 发送 JSON（与 postBody/patchBody 同风格，供 /me/password 使用）。
func putBody(t *testing.T, engine *gin.Engine, path, token string, body any) (int, envelope) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	var result envelope
	_ = json.Unmarshal(rec.Body.Bytes(), &result)
	return rec.Code, result
}

// putRawBody 发送原始请求体（用于畸形 JSON 用例）。
func putRawBody(t *testing.T, engine *gin.Engine, path, token, raw string) (int, envelope) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader([]byte(raw)))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	var result envelope
	_ = json.Unmarshal(rec.Body.Bytes(), &result)
	return rec.Code, result
}

// TestChangePassword 自助改密：鉴权 → 校验 → 落地 → 新旧密码登录口径。
func TestChangePassword(t *testing.T) {
	data := store.NewMemory()
	engine := buildEngineWithData(t, data)

	const phone = "13800009001"
	const oldPassword = "old123456"
	const newPassword = "new123456"
	token := registerAndLogin(t, engine, phone, oldPassword)

	// 1) 未登录 → 401
	if code, _ := putBody(t, engine, "/api/v1/me/password", "", map[string]any{
		"oldPassword": oldPassword, "newPassword": newPassword,
	}); code != http.StatusUnauthorized {
		t.Fatalf("未登录改密应 401，实际 %d", code)
	}

	// 2) 原密码错误 → 401「原密码不正确」
	code, res := putBody(t, engine, "/api/v1/me/password", token, map[string]any{
		"oldPassword": "wrong-old-pass", "newPassword": newPassword,
	})
	if code != http.StatusUnauthorized || res.Message != "原密码不正确" {
		t.Fatalf("原密码错误应 401 原密码不正确，实际 %d %s", code, res.Message)
	}

	// 3) 新密码过短 → 400（沿用 validatePassword 文案）
	code, res = putBody(t, engine, "/api/v1/me/password", token, map[string]any{
		"oldPassword": oldPassword, "newPassword": "12345",
	})
	if code != http.StatusBadRequest || res.Message != "密码至少 6 位" {
		t.Fatalf("短密码应 400 密码至少 6 位，实际 %d %s", code, res.Message)
	}

	// 4) 新密码与原密码相同 → 400
	code, res = putBody(t, engine, "/api/v1/me/password", token, map[string]any{
		"oldPassword": oldPassword, "newPassword": oldPassword,
	})
	if code != http.StatusBadRequest || res.Message != "新密码不能与原密码相同" {
		t.Fatalf("新旧相同应 400 新密码不能与原密码相同，实际 %d %s", code, res.Message)
	}

	// 5) 缺字段 → 400
	code, res = putBody(t, engine, "/api/v1/me/password", token, map[string]any{"oldPassword": oldPassword})
	if code != http.StatusBadRequest || res.Message != "请求参数格式不正确" {
		t.Fatalf("缺字段应 400 请求参数格式不正确，实际 %d %s", code, res.Message)
	}

	// 6) 畸形 JSON → 400
	code, res = putRawBody(t, engine, "/api/v1/me/password", token, "{not-json")
	if code != http.StatusBadRequest || res.Message != "请求参数格式不正确" {
		t.Fatalf("畸形 JSON 应 400 请求参数格式不正确，实际 %d %s", code, res.Message)
	}

	// 7) 成功 → 200 且 data.updated == true
	code, res = putBody(t, engine, "/api/v1/me/password", token, map[string]any{
		"oldPassword": oldPassword, "newPassword": newPassword,
	})
	if code != http.StatusOK || res.Code != 0 {
		t.Fatalf("改密失败: %d %s", code, res.Message)
	}
	if updated := decode[map[string]any](t, res.Data)["updated"]; updated != true {
		t.Fatalf("成功响应应为 data.updated=true，实际 %v", updated)
	}

	// 8) 新密码可登录；旧密码登录失败
	if newToken := loginToken(t, engine, phone, newPassword); newToken == "" {
		t.Fatal("新密码登录未拿到令牌")
	}
	code, res = postBody(t, engine, "/api/v1/auth/login", "", map[string]any{"phone": phone, "password": oldPassword})
	if code != http.StatusUnauthorized {
		t.Fatalf("旧密码应登录失败，实际 %d %s", code, res.Message)
	}
}
