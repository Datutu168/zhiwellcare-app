package httpapi

import (
	"strings"

	"github.com/gin-gonic/gin"

	"zhiwellcare/backend/internal/cache"
	"zhiwellcare/backend/internal/model"
	"zhiwellcare/backend/internal/store"
)

// deviceRegistryHandler 承载设备白名单与「设备标识 → 型号」映射的运营接口。
//
// 写路径必须同步失效 cache.Devices 的读缓存，否则运营改动要等 TTL 才生效。
type deviceRegistryHandler struct {
	store   store.Combined
	devices *cache.Devices
}

// ---------- 设备白名单 ----------

// listWhitelist GET /api/v1/admin/device-whitelist
func (h *deviceRegistryHandler) listWhitelist(c *gin.Context) {
	items, err := h.store.ListDeviceWhitelist(c)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, gin.H{"items": items})
}

type whitelistRequest struct {
	DeviceKey string `json:"deviceKey"`
	ModelID   string `json:"modelId"`
	Note      string `json:"note"`
	// Enabled 可选：缺省视为启用（契约里的 POST 体只有 deviceKey/modelId/note）。
	Enabled *bool `json:"enabled"`
}

// upsertWhitelist POST /api/v1/admin/device-whitelist
func (h *deviceRegistryHandler) upsertWhitelist(c *gin.Context) {
	var req whitelistRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	req.DeviceKey = strings.TrimSpace(req.DeviceKey)
	if req.DeviceKey == "" {
		failBadRequest(c, "deviceKey 不能为空")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	entry, err := h.store.UpsertDeviceWhitelist(c, model.DeviceWhitelistEntry{
		DeviceKey: req.DeviceKey,
		ModelID:   strings.TrimSpace(req.ModelID),
		Note:      req.Note,
		Enabled:   enabled,
	})
	if err != nil {
		mapStoreError(c, err)
		return
	}
	h.invalidateWhitelist(c)
	ok(c, entry)
}

// deleteWhitelist DELETE /api/v1/admin/device-whitelist/:deviceKey
func (h *deviceRegistryHandler) deleteWhitelist(c *gin.Context) {
	deviceKey := strings.TrimSpace(c.Param("deviceKey"))
	if err := h.store.DeleteDeviceWhitelist(c, deviceKey); err != nil {
		mapStoreError(c, err)
		return
	}
	h.invalidateWhitelist(c)
	ok(c, gin.H{"deleted": true})
}

// invalidateWhitelist 失效白名单读缓存（下一次读取回源 PG）。
func (h *deviceRegistryHandler) invalidateWhitelist(c *gin.Context) {
	if h.devices != nil {
		_ = h.devices.ForgetWhitelist(c.Request.Context())
	}
}

// ---------- 设备映射 ----------

// listMappings GET /api/v1/admin/device-mappings
func (h *deviceRegistryHandler) listMappings(c *gin.Context) {
	items, err := h.store.ListDeviceMappings(c)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, gin.H{"items": items})
}

type mappingRequest struct {
	DeviceKey string `json:"deviceKey"`
	ModelID   string `json:"modelId"`
	Note      string `json:"note"`
}

// upsertMapping POST /api/v1/admin/device-mappings
func (h *deviceRegistryHandler) upsertMapping(c *gin.Context) {
	var req mappingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	req.DeviceKey = strings.TrimSpace(req.DeviceKey)
	req.ModelID = strings.TrimSpace(req.ModelID)
	if req.DeviceKey == "" {
		failBadRequest(c, "deviceKey 不能为空")
		return
	}
	if req.ModelID == "" {
		failBadRequest(c, "modelId 不能为空")
		return
	}
	entry, err := h.store.UpsertDeviceMapping(c, model.DeviceMappingEntry{
		DeviceKey: req.DeviceKey,
		ModelID:   req.ModelID,
		Note:      req.Note,
	})
	if err != nil {
		mapStoreError(c, err)
		return
	}
	h.invalidateMapping(c, entry.DeviceKey)
	ok(c, entry)
}

// deleteMapping DELETE /api/v1/admin/device-mappings/:deviceKey
func (h *deviceRegistryHandler) deleteMapping(c *gin.Context) {
	deviceKey := strings.TrimSpace(c.Param("deviceKey"))
	if err := h.store.DeleteDeviceMapping(c, deviceKey); err != nil {
		mapStoreError(c, err)
		return
	}
	h.invalidateMapping(c, deviceKey)
	ok(c, gin.H{"deleted": true})
}

// invalidateMapping 失效单个设备映射读缓存。
func (h *deviceRegistryHandler) invalidateMapping(c *gin.Context, deviceKey string) {
	if h.devices != nil {
		_ = h.devices.ForgetMapping(c.Request.Context(), deviceKey)
	}
}
