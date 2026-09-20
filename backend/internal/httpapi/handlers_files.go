package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"zhiwellcare/backend/internal/config"
	"zhiwellcare/backend/internal/model"
	"zhiwellcare/backend/internal/objectstore"
	"zhiwellcare/backend/internal/store"
)

// maxDirectUploadBytes 签名直传单文件上限（训练采样文件为 JSONL/二进制片段，64MB 足够）。
const maxDirectUploadBytes = 64 << 20

// fileHandler 负责对象存储相关的 HTTP 接口。
//
// 两种上传路径：
//   - S3 部署：预签名 URL 直传对象存储，文件不经过业务服务器；
//   - 本地回退：预签名 URL 指向本服务 /api/v1/files/*（HMAC 校验）。
//
// 无论哪种，客户端拿到的都是「预签名地址 + PUT」，流程一致。
type fileHandler struct {
	store  store.Combined
	assets objectstore.Store
	cfg    *config.Config
}

// uploadURLRequest 申请直传地址。
type uploadURLRequest struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
}

// uploadURLResponse 直传地址应答。
type uploadURLResponse struct {
	Key              string `json:"key"`
	UploadURL        string `json:"uploadUrl"`
	Method           string `json:"method"`
	ExpiresInSeconds int64  `json:"expiresInSeconds"`
	// PublicURL 配置 CDN 时给出公开下载地址，客户端无需再申请下载地址。
	PublicURL string `json:"publicUrl,omitempty"`
}

// sampleUploadURL POST /api/v1/training/records/:recordId/samples/upload-url
// 训练高频采样文件直传：仅记录归属者可申请（管理员放行）。
func (h *fileHandler) sampleUploadURL(c *gin.Context) {
	recordID := strings.TrimSpace(c.Param("recordId"))
	if recordID == "" {
		failBadRequest(c, "recordId 不能为空")
		return
	}
	record, err := h.store.GetTrainingRecord(c, recordID)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	if !canAccessRecord(c, record) {
		fail(c, http.StatusForbidden, "无权访问该训练记录")
		return
	}

	var req uploadURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	filename := strings.TrimSpace(req.Filename)
	if filename == "" {
		failBadRequest(c, "filename 不能为空")
		return
	}
	owner := ""
	if record.UserID != nil {
		owner = *record.UserID
	}
	key := objectstore.SampleKey(owner, record.RecordID, filename)
	if err := objectstore.ValidateKey(key); err != nil {
		failBadRequest(c, "文件名不合法")
		return
	}

	uploadURL, err := h.assets.PresignPut(c, key, h.cfg.UploadURLTTL)
	if err != nil {
		fail(c, http.StatusServiceUnavailable, "对象存储不可用，请稍后重试")
		return
	}
	ok(c, uploadURLResponse{
		Key:              key,
		UploadURL:        uploadURL,
		Method:           http.MethodPut,
		ExpiresInSeconds: int64(h.cfg.UploadURLTTL.Seconds()),
		PublicURL:        h.assets.PublicURL(key),
	})
}

// sampleDownloadURL GET /api/v1/training/records/:recordId/samples/:filename/url
func (h *fileHandler) sampleDownloadURL(c *gin.Context) {
	recordID := strings.TrimSpace(c.Param("recordId"))
	record, err := h.store.GetTrainingRecord(c, recordID)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	if !canAccessRecord(c, record) {
		fail(c, http.StatusForbidden, "无权访问该训练记录")
		return
	}
	owner := ""
	if record.UserID != nil {
		owner = *record.UserID
	}
	key := objectstore.SampleKey(owner, record.RecordID, c.Param("filename"))

	// 先确认对象存在，避免返回一个必然 404 的地址。
	if _, err := h.assets.Stat(c, key); err != nil {
		if errors.Is(err, objectstore.ErrObjectNotFound) {
			fail(c, http.StatusNotFound, "采样文件不存在")
			return
		}
		fail(c, http.StatusServiceUnavailable, "对象存储不可用，请稍后重试")
		return
	}
	downloadURL, err := h.assets.PresignGet(c, key, h.cfg.UploadURLTTL)
	if err != nil {
		fail(c, http.StatusServiceUnavailable, "对象存储不可用，请稍后重试")
		return
	}
	ok(c, gin.H{
		"key":              key,
		"downloadUrl":      downloadURL,
		"method":           http.MethodGet,
		"expiresInSeconds": int64(h.cfg.UploadURLTTL.Seconds()),
	})
}

// localFile PUT/GET /api/v1/files/*key
// 本地对象存储的签名直传端点：校验 HMAC 签名与有效期，不依赖登录态。
func (h *fileHandler) localFile(c *gin.Context) {
	if h.assets.Kind() != "local" {
		// S3 部署下该端点应被网关屏蔽；显式返回 404 避免误用。
		fail(c, http.StatusNotFound, "接口不存在")
		return
	}
	key := strings.TrimPrefix(c.Param("key"), "/")
	if err := objectstore.ValidateKey(key); err != nil {
		failBadRequest(c, "对象键不合法")
		return
	}
	op := c.Query("op")
	expires := int64(0)
	if raw := c.Query("exp"); raw != "" {
		parsed, err := parseExpires(raw)
		if err != nil {
			failBadRequest(c, "exp 参数不合法")
			return
		}
		expires = parsed
	}
	if err := objectstore.VerifyLocal([]byte(h.cfg.UploadSignSecret), op, key, expires, c.Query("sig")); err != nil {
		fail(c, http.StatusForbidden, "签名无效或已过期")
		return
	}

	switch op {
	case objectstore.OpPut:
		h.localPut(c, key)
	case objectstore.OpGet:
		h.localGet(c, key)
	default:
		failBadRequest(c, "op 参数不合法")
	}
}

func (h *fileHandler) localPut(c *gin.Context, key string) {
	body := http.MaxBytesReader(c.Writer, c.Request.Body, maxDirectUploadBytes)
	contentType := c.GetHeader("Content-Type")
	info, err := h.assets.Put(c, key, body, c.Request.ContentLength, contentType)
	if err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			fail(c, http.StatusRequestEntityTooLarge, "文件超出大小限制")
			return
		}
		fail(c, http.StatusInternalServerError, "保存文件失败")
		return
	}
	ok(c, gin.H{"key": info.Key, "size": info.Size, "contentType": info.ContentType})
}

func (h *fileHandler) localGet(c *gin.Context, key string) {
	reader, info, err := h.assets.Get(c, key)
	if err != nil {
		if errors.Is(err, objectstore.ErrObjectNotFound) {
			fail(c, http.StatusNotFound, "文件不存在")
			return
		}
		fail(c, http.StatusInternalServerError, "读取文件失败")
		return
	}
	defer func() { _ = reader.Close() }()

	c.Header("Content-Type", info.ContentType)
	c.Header("Content-Length", strconv.FormatInt(info.Size, 10))
	// 预签名地址本身带有效期，允许缓存但需重新校验。
	c.Header("Cache-Control", "private, max-age=300")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, reader)
}

// canAccessRecord 判断当前用户能否访问该训练记录：本人或管理员。
func canAccessRecord(c *gin.Context, record *model.TrainingSummary) bool {
	if record == nil {
		return false
	}
	if role, exists := c.Get(ctxRole); exists && role == "admin" {
		return true
	}
	current, exists := c.Get(ctxUserID)
	if !exists || record.UserID == nil {
		return false
	}
	return current == *record.UserID
}

// parseExpires 解析有效期时间戳（秒）。
func parseExpires(raw string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
}
