package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"zhiwellcare/backend/internal/auth"
	"zhiwellcare/backend/internal/cache"
	"zhiwellcare/backend/internal/config"
	"zhiwellcare/backend/internal/objectstore"
	"zhiwellcare/backend/internal/store"
)

// newStorageTestEngine 装配带缓存 + 对象存储的引擎（内存存储 + 本地磁盘对象存储）。
func newStorageTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cacheStore := cache.NewMemory()
	assets, err := objectstore.NewLocal(objectstore.Options{
		LocalDir:     t.TempDir(),
		LocalBaseURL: "http://127.0.0.1:8080",
		SignSecret:   "upload-secret",
	})
	if err != nil {
		t.Fatalf("初始化对象存储失败: %v", err)
	}
	cfg := &config.Config{
		Addr:             ":0",
		JWTSecret:        "test-secret",
		AccessTokenTTL:   2 * time.Hour,
		RefreshTokenTTL:  30 * 24 * time.Hour,
		CORSOrigins:      []string{"http://localhost:1420"},
		UploadURLTTL:     15 * time.Minute,
		UploadSignSecret: "upload-secret",
		PublicBaseURL:    "http://127.0.0.1:8080",
	}
	manager := auth.NewTokenManager(cfg.JWTSecret, cfg.AccessTokenTTL)
	return NewEngine(Deps{
		Config:    cfg,
		Store:     store.NewMemory(),
		Manager:   manager,
		Cache:     cacheStore,
		Sessions:  cache.NewSessions(cacheStore),
		Devices:   cache.NewDevices(cacheStore, time.Minute),
		Assets:    assets,
		StartedAt: time.Now(),
	})
}

// registerUser 注册并返回访问令牌与刷新令牌。
func registerUser(t *testing.T, engine http.Handler, phone string) authData {
	t.Helper()
	status, env := doJSON(t, engine, http.MethodPost, "/api/v1/auth/register", "", map[string]string{
		"phone": phone, "password": "pass123456", "nickname": "训练员",
	})
	if status != http.StatusOK {
		t.Fatalf("注册失败: status=%d message=%s", status, env.Message)
	}
	return decode[authData](t, env.Data)
}

func submitRecord(t *testing.T, engine http.Handler, token, recordID string) {
	t.Helper()
	status, env := doJSON(t, engine, http.MethodPost, "/api/v1/training/records", token, map[string]any{
		"recordId":      recordID,
		"schemaVersion": 2,
		"gameId":        "kart-racing",
		"gameName":      "卡丁竞速",
		"completedAt":   time.Now().UnixMilli(),
		"device":        map[string]any{"modelId": "wobble-wrist-band", "modelName": "不倒翁手腕训练仪"},
		"statistics":    map[string]any{"score": 10},
	})
	if status != http.StatusOK {
		t.Fatalf("上报训练记录失败: status=%d message=%s", status, env.Message)
	}
}

func TestStorageFlowSampleUploadAndDownload(t *testing.T) {
	engine := newStorageTestEngine(t)
	session := registerUser(t, engine, "13800001111")
	token := session.Tokens.AccessToken
	submitRecord(t, engine, token, "rec-1")

	// 1) 申请直传地址
	status, env := doJSON(t, engine, http.MethodPost, "/api/v1/training/records/rec-1/samples/upload-url", token, map[string]string{
		"filename": "samples.jsonl", "contentType": "application/json",
	})
	if status != http.StatusOK {
		t.Fatalf("申请直传地址失败: status=%d message=%s", status, env.Message)
	}
	upload := decode[struct {
		Key       string `json:"key"`
		UploadURL string `json:"uploadUrl"`
		Method    string `json:"method"`
	}](t, env.Data)
	if upload.Method != http.MethodPut {
		t.Fatalf("上传方法 = %q，期望 PUT", upload.Method)
	}
	if !strings.HasPrefix(upload.Key, "samples/") {
		t.Fatalf("对象键应以 samples/ 开头，得到 %q", upload.Key)
	}

	// 2) 用签名地址直传（本地模式下指向本服务的 /api/v1/files/*）
	payload := []byte("{\"angleX\":1}\n{\"angleX\":2}\n")
	parsed, err := url.Parse(upload.UploadURL)
	if err != nil {
		t.Fatalf("解析上传地址失败: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, parsed.RequestURI(), bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("直传失败: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 3) 申请下载地址并取回内容
	status, env = doJSON(t, engine, http.MethodGet, "/api/v1/training/records/rec-1/samples/samples.jsonl/url", token, nil)
	if status != http.StatusOK {
		t.Fatalf("申请下载地址失败: status=%d message=%s", status, env.Message)
	}
	download := decode[struct {
		DownloadURL string `json:"downloadUrl"`
	}](t, env.Data)
	parsedDownload, err := url.Parse(download.DownloadURL)
	if err != nil {
		t.Fatalf("解析下载地址失败: %v", err)
	}
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, parsedDownload.RequestURI(), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("下载失败: status=%d body=%s", rec.Code, rec.Body.String())
	}
	body, _ := io.ReadAll(rec.Body)
	if !bytes.Equal(body, payload) {
		t.Fatalf("下载内容不一致: %s", body)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestSampleAccessControlAndSignature(t *testing.T) {
	engine := newStorageTestEngine(t)
	owner := registerUser(t, engine, "13800002222")
	other := registerUser(t, engine, "13800003333")
	submitRecord(t, engine, owner.Tokens.AccessToken, "rec-owner")

	// 他人不能为别人的记录申请直传地址
	status, env := doJSON(t, engine, http.MethodPost, "/api/v1/training/records/rec-owner/samples/upload-url",
		other.Tokens.AccessToken, map[string]string{"filename": "s.jsonl"})
	if status != http.StatusForbidden {
		t.Fatalf("非归属者应被拒绝，得到 status=%d message=%s", status, env.Message)
	}

	// 不存在的记录 → 404
	status, _ = doJSON(t, engine, http.MethodPost, "/api/v1/training/records/missing/samples/upload-url",
		owner.Tokens.AccessToken, map[string]string{"filename": "s.jsonl"})
	if status != http.StatusNotFound {
		t.Fatalf("不存在的记录应 404，得到 %d", status)
	}

	// 申请下载地址：文件不存在 → 404
	status, _ = doJSON(t, engine, http.MethodGet, "/api/v1/training/records/rec-owner/samples/none.jsonl/url",
		owner.Tokens.AccessToken, nil)
	if status != http.StatusNotFound {
		t.Fatalf("不存在的对象应 404，得到 %d", status)
	}

	// 篡改签名 → 403
	status, env = doJSON(t, engine, http.MethodPost, "/api/v1/training/records/rec-owner/samples/upload-url",
		owner.Tokens.AccessToken, map[string]string{"filename": "s.jsonl"})
	if status != http.StatusOK {
		t.Fatalf("申请直传地址失败: %s", env.Message)
	}
	upload := decode[struct {
		UploadURL string `json:"uploadUrl"`
	}](t, env.Data)
	tampered, err := url.Parse(upload.UploadURL)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	query := tampered.Query()
	query.Set("sig", strings.Repeat("0", len(query.Get("sig"))))
	tampered.RawQuery = query.Encode()

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, tampered.RequestURI(), strings.NewReader("x")))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("签名被篡改应 403，得到 %d", rec.Code)
	}

	// 过期签名 → 403
	expired := objectstore.SignLocal([]byte("upload-secret"), objectstore.OpPut, "samples/x/y/z.jsonl", time.Now().Add(-time.Minute).Unix())
	rec = httptest.NewRecorder()
	expiredAt := time.Now().Add(-time.Minute).Unix()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPut,
		"/api/v1/files/samples/x/y/z.jsonl?op=put&exp="+strconv.FormatInt(expiredAt, 10)+"&sig="+expired, strings.NewReader("x")))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("过期签名应 403，得到 %d", rec.Code)
	}

	// 穿越对象键 → 400（校验在签名之前，避免探测）
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/files/../../etc/passwd?op=get&exp=1&sig=x", nil))
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusMovedPermanently {
		t.Fatalf("非法对象键应 400，得到 %d", rec.Code)
	}
}

func TestLogoutRevokesAccessToken(t *testing.T) {
	engine := newStorageTestEngine(t)
	session := registerUser(t, engine, "13800004444")
	token := session.Tokens.AccessToken

	// 登出前可用
	if status, _ := doJSON(t, engine, http.MethodGet, "/api/v1/me", token, nil); status != http.StatusOK {
		t.Fatalf("登出前 /me 应可用，得到 %d", status)
	}
	// 登出：同时带刷新令牌与访问令牌
	status, env := doJSON(t, engine, http.MethodPost, "/api/v1/auth/logout", token, map[string]string{
		"refreshToken": session.Tokens.RefreshToken,
	})
	if status != http.StatusOK {
		t.Fatalf("登出失败: status=%d message=%s", status, env.Message)
	}
	// 访问令牌进入吊销名单后立即失效（无需等自然过期）
	status, env = doJSON(t, engine, http.MethodGet, "/api/v1/me", token, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("登出后 /me 应 401，得到 %d", status)
	}
	if !strings.Contains(env.Message, "登录状态已失效") {
		t.Fatalf("错误文案应提示登录失效，得到 %q", env.Message)
	}
}

func TestHealthzReportsMiddlewareBackends(t *testing.T) {
	engine := newStorageTestEngine(t)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz 应 200，得到 %d", rec.Code)
	}
	var payload struct {
		Data struct {
			Status  string `json:"status"`
			Cache   string `json:"cache"`
			Storage string `json:"storage"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析 healthz 失败: %v", err)
	}
	if payload.Data.Status != "ok" || payload.Data.Cache != "memory" || payload.Data.Storage != "local" {
		t.Fatalf("healthz = %+v", payload.Data)
	}
}

func TestRateLimitUsesCacheStore(t *testing.T) {
	engine := newStorageTestEngine(t)
	// 登录接口限流为每分钟 20 次：连续错误请求超过阈值后应出现 429。
	last := http.StatusOK
	for i := 0; i < 30; i++ {
		status, _ := doJSON(t, engine, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
			"phone": "13800005555", "password": "wrong-password",
		})
		last = status
		if status == http.StatusTooManyRequests {
			break
		}
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("超过阈值后应返回 429，最后状态 %d", last)
	}
}
