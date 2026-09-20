package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"zhiwellcare/backend/internal/config"
	"zhiwellcare/backend/internal/model"
	"zhiwellcare/backend/internal/objectstore"
	"zhiwellcare/backend/internal/store"
)

// assetHandler 承载资产登记（Web 包 / 游戏资源 / 固件）与公开下发接口。
type assetHandler struct {
	store  store.Combined
	assets objectstore.Store
	cfg    *config.Config
}

// assetObjectURL 生成已发布资产的下发地址：
// 配置了 CDN（Assets.PublicURL 非空）用公开地址，否则回退对象存储预签名下载地址。
func assetObjectURL(ctx context.Context, objects objectstore.Store, cfg *config.Config, key string) (string, error) {
	if objects == nil || key == "" {
		return "", nil
	}
	if public := objects.PublicURL(key); public != "" {
		return public, nil
	}
	ttl := 15 * time.Minute
	if cfg != nil && cfg.UploadURLTTL > 0 {
		ttl = cfg.UploadURLTTL
	}
	return objects.PresignGet(ctx, key, ttl)
}

// assetKey 按资产类型生成对象键（与 objectstore 的键构造器一一对应）。
func assetKey(kind, refID, version, filename string) string {
	switch kind {
	case model.AssetKindWeb:
		return objectstore.WebBundleKey(refID, version, filename)
	case model.AssetKindGame:
		return objectstore.GameResourceKey(refID, version, filename)
	case model.AssetKindFirmware:
		return objectstore.FirmwareKey(refID, version, filename)
	default:
		return ""
	}
}

// ---------- 资产登记（后台） ----------

type assetUploadRequest struct {
	Kind        string `json:"kind"`
	RefID       string `json:"refId"`
	Version     string `json:"version"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
}

// uploadURL POST /api/v1/admin/assets/upload-url
func (h *assetHandler) uploadURL(c *gin.Context) {
	var req assetUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	req.Kind = strings.TrimSpace(req.Kind)
	req.RefID = strings.TrimSpace(req.RefID)
	req.Version = strings.TrimSpace(req.Version)
	req.Filename = strings.TrimSpace(req.Filename)
	if !model.ValidAssetKind(req.Kind) {
		failBadRequest(c, "kind 只能为 web/game/firmware")
		return
	}
	if req.RefID == "" || req.Version == "" || req.Filename == "" {
		failBadRequest(c, "refId / version / filename 为必填")
		return
	}
	if h.assets == nil {
		fail(c, http.StatusServiceUnavailable, "对象存储不可用，请稍后重试")
		return
	}
	key := assetKey(req.Kind, req.RefID, req.Version, req.Filename)
	if err := objectstore.ValidateKey(key); err != nil {
		failBadRequest(c, "文件名不合法")
		return
	}
	ttl := 15 * time.Minute
	if h.cfg != nil && h.cfg.UploadURLTTL > 0 {
		ttl = h.cfg.UploadURLTTL
	}
	presigned, err := h.assets.PresignPut(c.Request.Context(), key, ttl)
	if err != nil {
		fail(c, http.StatusServiceUnavailable, "对象存储不可用，请稍后重试")
		return
	}
	ok(c, uploadURLResponse{
		Key:              key,
		UploadURL:        presigned,
		Method:           http.MethodPut,
		ExpiresInSeconds: int64(ttl.Seconds()),
		PublicURL:        h.assets.PublicURL(key),
	})
}

type assetCreateRequest struct {
	Kind        string `json:"kind"`
	RefID       string `json:"refId"`
	Version     string `json:"version"`
	Filename    string `json:"filename"`
	ObjectKey   string `json:"objectKey"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	ContentType string `json:"contentType"`
	Notes       string `json:"notes"`
}

// create POST /api/v1/admin/assets（登记资产记录，201）
func (h *assetHandler) create(c *gin.Context) {
	var req assetCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	req.Kind = strings.TrimSpace(req.Kind)
	req.RefID = strings.TrimSpace(req.RefID)
	req.Version = strings.TrimSpace(req.Version)
	req.Filename = strings.TrimSpace(req.Filename)
	req.ObjectKey = strings.TrimSpace(req.ObjectKey)
	if !model.ValidAssetKind(req.Kind) {
		failBadRequest(c, "kind 只能为 web/game/firmware")
		return
	}
	if req.Version == "" || req.Filename == "" {
		failBadRequest(c, "version / filename 为必填")
		return
	}
	if err := objectstore.ValidateKey(req.ObjectKey); err != nil {
		failBadRequest(c, "objectKey 不合法")
		return
	}
	asset, err := h.store.CreateAsset(c, model.Asset{
		Kind:        req.Kind,
		RefID:       req.RefID,
		Version:     req.Version,
		Filename:    req.Filename,
		ObjectKey:   req.ObjectKey,
		Size:        req.Size,
		SHA256:      strings.ToLower(strings.TrimSpace(req.SHA256)),
		ContentType: req.ContentType,
		Notes:       req.Notes,
		Status:      model.AssetStatusDraft,
	})
	if err != nil {
		mapRBACError(c, err)
		return
	}
	created(c, asset)
}

// list GET /api/v1/admin/assets?kind=&refId=
func (h *assetHandler) list(c *gin.Context) {
	items, err := h.store.ListAssets(c, strings.TrimSpace(c.Query("kind")), strings.TrimSpace(c.Query("refId")), strings.TrimSpace(c.Query("status")))
	if err != nil {
		mapRBACError(c, err)
		return
	}
	ok(c, gin.H{"items": items})
}

// setStatus PATCH /api/v1/admin/assets/:id/status
func (h *assetHandler) setStatus(c *gin.Context) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		failBadRequest(c, "资产 ID 不合法")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || !model.ValidAssetStatus(req.Status) {
		failBadRequest(c, "status 只能为 draft/published/offline")
		return
	}
	asset, err := h.store.SetAssetStatus(c, id, req.Status)
	if err != nil {
		mapRBACError(c, err)
		return
	}
	ok(c, asset)
}

// remove DELETE /api/v1/admin/assets/:id
func (h *assetHandler) remove(c *gin.Context) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		failBadRequest(c, "资产 ID 不合法")
		return
	}
	if err := h.store.DeleteAsset(c, id); err != nil {
		mapRBACError(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

// ---------- 公开下发（无需登录） ----------

type releaseUpToDate struct {
	UpToDate bool   `json:"upToDate"`
	Version  string `json:"version"`
}

type releaseUpdate struct {
	UpToDate    bool       `json:"upToDate"`
	Version     string     `json:"version"`
	URL         string     `json:"url"`
	SHA256      string     `json:"sha256"`
	Size        int64      `json:"size"`
	Notes       []string   `json:"notes"`
	PublishedAt *time.Time `json:"publishedAt"`
}

// splitNotes 资产备注按行拆成数组（notes 字段在契约里是数组）。
func splitNotes(notes string) []string {
	out := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(notes, "\r\n", "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// release 组装「有更新」响应；assets 为 nil 或地址生成失败时返回 error。
func (h *assetHandler) release(c *gin.Context, asset *model.Asset) {
	if asset == nil {
		ok(c, releaseUpToDate{UpToDate: true, Version: ""})
		return
	}
	current := strings.TrimSpace(c.Query("currentVersion"))
	if asset.Version == current {
		ok(c, releaseUpToDate{UpToDate: true, Version: asset.Version})
		return
	}
	url, err := assetObjectURL(c.Request.Context(), h.assets, h.cfg, asset.ObjectKey)
	if err != nil || url == "" {
		fail(c, http.StatusServiceUnavailable, "资源地址不可用，请稍后重试")
		return
	}
	ok(c, releaseUpdate{
		UpToDate:    false,
		Version:     asset.Version,
		URL:         url,
		SHA256:      asset.SHA256,
		Size:        asset.Size,
		Notes:       splitNotes(asset.Notes),
		PublishedAt: asset.PublishedAt,
	})
}

// publicWebBundle GET /api/v1/app/web-bundle?platform=android&currentVersion=0.1.0
func (h *assetHandler) publicWebBundle(c *gin.Context) {
	platform := strings.TrimSpace(c.Query("platform"))
	if platform == "" {
		// 缺省按 Android（当前消费端唯一平台）处理，避免客户端漏传参数直接失败。
		platform = "android"
	}
	asset, err := h.store.LatestPublishedAsset(c, model.AssetKindWeb, platform)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			ok(c, releaseUpToDate{UpToDate: true, Version: ""})
			return
		}
		mapStoreError(c, err)
		return
	}
	h.release(c, asset)
}

// publicFirmware GET /api/v1/device/:modelId/firmware?currentVersion=
func (h *assetHandler) publicFirmware(c *gin.Context) {
	modelID := strings.TrimSpace(c.Param("modelId"))
	if modelID == "" {
		failBadRequest(c, "modelId 不能为空")
		return
	}
	asset, err := h.store.LatestPublishedAsset(c, model.AssetKindFirmware, modelID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			ok(c, releaseUpToDate{UpToDate: true, Version: ""})
			return
		}
		mapStoreError(c, err)
		return
	}
	h.release(c, asset)
}
