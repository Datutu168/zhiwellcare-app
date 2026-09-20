package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"zhiwellcare/backend/internal/model"
)

// bomEchoEngine 只挂 StripJSONBOM + 一个回显原始 body 字节的 handler，
// 用于精确断言「中间件有没有改动请求体」。
func bomEchoEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(StripJSONBOM())
	engine.POST("/echo", func(c *gin.Context) {
		raw, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.String(http.StatusInternalServerError, "read error: %v", err)
			return
		}
		c.Data(http.StatusOK, "application/octet-stream", raw)
	})
	return engine
}

func doRaw(t *testing.T, engine *gin.Engine, contentType string, body []byte) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// TestStripJSONBOMMiddlewareStripsBOMForJSON BOM + JSON → 中间件剥掉 BOM（且只剥 3 字节）。
func TestStripJSONBOMMiddlewareStripsBOMForJSON(t *testing.T) {
	engine := bomEchoEngine()
	payload := []byte(`{"courseId":"course-bom","title":"BOM 课程","status":"on"}`)
	withBOM := append(append([]byte{}, utf8BOM...), payload...)

	code, got := doRaw(t, engine, "application/json", withBOM)
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", code)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("JSON body 的 BOM 应被剥掉:\n得到 %q\n期望 %q", got, payload)
	}
	// 无 BOM 时必须逐字节透传（不能少读/多吃）。
	code, got = doRaw(t, engine, "application/json", payload)
	if code != http.StatusOK || !bytes.Equal(got, payload) {
		t.Fatalf("无 BOM 的 JSON body 应原样透传: code=%d got=%q", code, got)
	}
	// 带 charset 参数、以及 +json 变体同样处理。
	code, got = doRaw(t, engine, "application/json; charset=utf-8", withBOM)
	if code != http.StatusOK || !bytes.Equal(got, payload) {
		t.Fatalf("application/json; charset=utf-8 应剥 BOM: code=%d got=%q", code, got)
	}
	code, got = doRaw(t, engine, "application/merge-patch+json", withBOM)
	if code != http.StatusOK || !bytes.Equal(got, payload) {
		t.Fatalf("+json 变体应剥 BOM: code=%d got=%q", code, got)
	}
	// Content-Length 需同步修正，否则后续基于长度的读取会挂住/截断。
	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewReader(withBOM))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(httptest.NewRecorder(), req)
	if req.ContentLength != int64(len(payload)) {
		t.Fatalf("ContentLength 应同步减去 BOM 长度，得到 %d，期望 %d", req.ContentLength, len(payload))
	}
}

// TestStripJSONBOMMiddlewareLeavesOtherBodiesUntouched 非 JSON / 大体积 / 压缩体一律不动：
// 尤其是本地对象存储直传端点 PUT /api/v1/files/*key（octet-stream，单文件上限 64MB，流式转发），
// 绝不能被缓冲或改写。
func TestStripJSONBOMMiddlewareLeavesOtherBodiesUntouched(t *testing.T) {
	large := bytes.Repeat([]byte("A"), maxBOMStripBody+1024)
	largeWithBOM := append(append([]byte{}, utf8BOM...), large...)

	cases := []struct {
		name        string
		contentType string
		body        []byte
	}{
		{"octet-stream 带 BOM 前缀（直传端点语义）", "application/octet-stream", append(append([]byte{}, utf8BOM...), []byte{0x01, 0x02, 0x03}...)},
		{"octet-stream 大体积（>1MiB）", "application/octet-stream", large},
		{"JSON 超过 1MiB 阈值（不做缓冲）", "application/json", largeWithBOM},
		{"JSON 压缩体（Content-Encoding 排除）", "application/json", largeWithBOM},
		{"无 Content-Type", "", append(append([]byte{}, utf8BOM...), []byte(`{"a":1}`)...)},
		{"multipart（文件上传语义）", "multipart/form-data; boundary=xyz", append(append([]byte{}, utf8BOM...), []byte("--xyz--")...)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := bomEchoEngine()
			req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewReader(tc.body))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			if strings.Contains(tc.name, "压缩") {
				req.Header.Set("Content-Encoding", "gzip")
			}
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("状态码 = %d，期望 200", rec.Code)
			}
			if !bytes.Equal(rec.Body.Bytes(), tc.body) {
				t.Fatalf("请求体不应被改动：得到 %d 字节，期望 %d 字节（前 6 字节 %x vs %x）",
					rec.Body.Len(), len(tc.body), rec.Body.Bytes()[:min(6, rec.Body.Len())], tc.body[:min(6, len(tc.body))])
			}
		})
	}
}

// TestJSONBOMAcceptedByRealEndpoints 端到端（真实引擎 + 真实 handler）：
// 带 BOM 的 JSON 请求不再 400，字段照常解析；无 BOM 行为不变。
// 覆盖复核方报的 POST /admin/courses 与既有接口 POST /admin/games。
func TestJSONBOMAcceptedByRealEndpoints(t *testing.T) {
	env := newRBACTestEnv(t, "")
	adminToken, _ := env.newUserWithRoles(t, "13800014401", "admin")

	postWithBOM := func(path string, body string, contentType string) (int, envelope) {
		t.Helper()
		raw := append(append([]byte{}, utf8BOM...), []byte(body)...)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()
		env.engine.ServeHTTP(rec, req)
		var result envelope
		_ = json.Unmarshal(rec.Body.Bytes(), &result)
		return rec.Code, result
	}

	// 新接口：带 BOM 的课程创建 → 200，且字段解析正确（含中文与数组）
	code, res := postWithBOM("/api/v1/admin/courses",
		`{"courseId":"course-bom","title":"BOM 课程","tags":["腕部"],"status":"on","sort":7}`, "application/json")
	if code != http.StatusOK {
		t.Fatalf("带 BOM 的 JSON 应被接受，实际 %d（%s）", code, res.Message)
	}
	created := decode[struct {
		Item model.Course `json:"item"`
	}](t, res.Data)
	if created.Item.CourseID != "course-bom" || created.Item.Title != "BOM 课程" ||
		created.Item.Status != "on" || created.Item.Sort != 7 || len(created.Item.Tags) != 1 {
		t.Fatalf("带 BOM 请求的字段解析异常: %+v", created.Item)
	}
	// 落库并出现在公开目录（只含 on）→ 证明请求真的被处理了
	public := decode[[]map[string]any](t, mustGet(t, env.engine, "/api/v1/catalog/courses"))
	if len(public) != 1 || public[0]["courseId"] != "course-bom" {
		t.Fatalf("带 BOM 创建的课程应在公开目录: %+v", public)
	}

	// 既有接口（非本次新增）同样受益
	code, res = postWithBOM("/api/v1/admin/games",
		`{"gameId":"bom-game","name":"BOM 游戏","status":"on"}`, "application/json")
	if code != http.StatusOK {
		t.Fatalf("既有接口带 BOM 也应 200，实际 %d（%s）", code, res.Message)
	}

	// 无 BOM：行为不变
	code, res = doJSON(t, env.engine, http.MethodPost, "/api/v1/admin/goods", adminToken, map[string]any{
		"goodsId": "no-bom-goods", "name": "无 BOM 商品", "status": "on",
	})
	if code != http.StatusOK {
		t.Fatalf("无 BOM 请求应照常 200，实际 %d（%s）", code, res.Message)
	}

	// BOM + 非法字段仍要正常报错（不能因为剥 BOM 把校验逻辑吞掉）
	code, res = postWithBOM("/api/v1/admin/courses", `{"courseId":"BAD_ID","title":"x"}`, "application/json")
	if code != http.StatusBadRequest || res.Code != http.StatusBadRequest {
		t.Fatalf("带 BOM 的非法 ID 仍应 400，实际 %d（%s）", code, res.Message)
	}
}
