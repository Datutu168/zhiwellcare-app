package httpapi

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"zhiwellcare/backend/internal/config"
	"zhiwellcare/backend/internal/model"
	"zhiwellcare/backend/internal/objectstore"
	"zhiwellcare/backend/internal/store"
)

var modelIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)

func validStatus(status string) bool { return status == "on" || status == "off" }

func containsAll(tags, required []string) bool {
	set := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		set[tag] = struct{}{}
	}
	for _, tag := range required {
		if _, exists := set[tag]; !exists {
			return false
		}
	}
	return true
}

// catalogHandler 承载“面向 APP 的公开目录”与“后台运营 CRUD”。
type catalogHandler struct {
	store  store.Combined
	assets objectstore.Store
	cfg    *config.Config
}

// fillResourceAssets 目录条目里 resourceUrl 为空、但资产库存在
// 「该 gameId + resourceVersion 的 published 游戏资源」时，回填 CDN/预签名地址
// 与 sha256/size（前端据此下载并校验资源包）。
func (h *catalogHandler) fillResourceAssets(c *gin.Context, games []model.GameCatalogItem) []model.GameCatalogItem {
	for index := range games {
		game := &games[index]
		if game.ResourceURL != "" || game.GameID == "" {
			continue
		}
		asset, err := h.store.FindPublishedAsset(c, model.AssetKindGame, game.GameID, game.ResourceVersion)
		if err != nil || asset == nil {
			continue
		}
		url, err := assetObjectURL(c.Request.Context(), h.assets, h.cfg, asset.ObjectKey)
		if err != nil || url == "" {
			continue
		}
		game.ResourceURL = url
		game.ResourceSha256 = asset.SHA256
		game.ResourceSize = asset.Size
	}
	return games
}

// ---------- 公开目录（APP 读取，无需登录） ----------

func (h *catalogHandler) publicDeviceModels(c *gin.Context) {
	items, err := h.store.ListDeviceModels(c, false)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, items)
}

func (h *catalogHandler) publicGames(c *gin.Context) {
	items, err := h.store.ListGames(c, false)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, h.fillResourceAssets(c, items))
}

// deviceGames 返回某型号可用游戏：设备标签 ⊇ 游戏所需标签（匹配公式）。
func (h *catalogHandler) deviceGames(c *gin.Context) {
	modelID := c.Param("modelId")
	device, err := h.store.GetDeviceModel(c, modelID)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	if device.Status != "on" {
		fail(c, http.StatusNotFound, "该设备型号暂未开放")
		return
	}
	games, err := h.store.ListGames(c, false)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	matched := make([]model.GameCatalogItem, 0, len(games))
	for _, game := range games {
		if containsAll(device.CapabilityTags, game.RequiredTags) {
			matched = append(matched, game)
		}
	}
	ok(c, h.fillResourceAssets(c, matched))
}

// ---------- 设备型号管理（admin） ----------

func (h *catalogHandler) adminListDevices(c *gin.Context) {
	items, err := h.store.ListDeviceModels(c, true)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, items)
}

func (h *catalogHandler) adminCreateDevice(c *gin.Context) {
	var req model.DeviceModel
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	req.ModelID = strings.TrimSpace(req.ModelID)
	if !modelIDPattern.MatchString(req.ModelID) {
		failBadRequest(c, "型号 ID 需为 2-64 位小写字母/数字/中划线")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		failBadRequest(c, "型号名称不能为空")
		return
	}
	if req.Status == "" {
		req.Status = "on"
	}
	if !validStatus(req.Status) {
		failBadRequest(c, "状态只能为 on/off")
		return
	}
	if err := h.store.UpsertDeviceModel(c, req); err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, gin.H{"modelId": req.ModelID})
}

func (h *catalogHandler) adminUpdateDevice(c *gin.Context) {
	var req model.DeviceModel
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	req.ModelID = c.Param("modelId")
	if strings.TrimSpace(req.Name) == "" {
		failBadRequest(c, "型号名称不能为空")
		return
	}
	if req.Status == "" {
		req.Status = "on"
	}
	if !validStatus(req.Status) {
		failBadRequest(c, "状态只能为 on/off")
		return
	}
	if err := h.store.UpsertDeviceModel(c, req); err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, gin.H{"modelId": req.ModelID})
}

func (h *catalogHandler) adminDeleteDevice(c *gin.Context) {
	if err := h.store.DeleteDeviceModel(c, c.Param("modelId")); err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

func (h *catalogHandler) adminSetDeviceStatus(c *gin.Context) {
	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || !validStatus(req.Status) {
		failBadRequest(c, "状态只能为 on/off")
		return
	}
	if err := h.store.SetDeviceModelStatus(c, c.Param("modelId"), req.Status); err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, gin.H{"status": req.Status})
}

// ---------- 游戏目录管理（admin） ----------

func (h *catalogHandler) adminListGames(c *gin.Context) {
	items, err := h.store.ListGames(c, true)
	if err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, items)
}

func validateGame(req *model.GameCatalogItem) string {
	req.GameID = strings.TrimSpace(req.GameID)
	if !modelIDPattern.MatchString(req.GameID) {
		return "游戏 ID 需为 2-64 位小写字母/数字/中划线"
	}
	if strings.TrimSpace(req.Name) == "" {
		return "游戏名称不能为空"
	}
	if req.Status == "" {
		req.Status = "off"
	}
	if !validStatus(req.Status) {
		return "状态只能为 on/off"
	}
	if req.GrayRatio < 0 || req.GrayRatio > 1 {
		return "灰度比例需在 0-1 之间"
	}
	return ""
}

func (h *catalogHandler) adminCreateGame(c *gin.Context) {
	var req model.GameCatalogItem
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	if message := validateGame(&req); message != "" {
		failBadRequest(c, message)
		return
	}
	if err := h.store.UpsertGame(c, req); err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, gin.H{"gameId": req.GameID})
}

func (h *catalogHandler) adminUpdateGame(c *gin.Context) {
	var req model.GameCatalogItem
	if err := c.ShouldBindJSON(&req); err != nil {
		failBadRequest(c, "请求参数格式不正确")
		return
	}
	req.GameID = c.Param("gameId")
	if message := validateGame(&req); message != "" {
		failBadRequest(c, message)
		return
	}
	if err := h.store.UpsertGame(c, req); err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, gin.H{"gameId": req.GameID})
}

func (h *catalogHandler) adminDeleteGame(c *gin.Context) {
	if err := h.store.DeleteGame(c, c.Param("gameId")); err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

func (h *catalogHandler) adminSetGameStatus(c *gin.Context) {
	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || !validStatus(req.Status) {
		failBadRequest(c, "状态只能为 on/off")
		return
	}
	if err := h.store.SetGameStatus(c, c.Param("gameId"), req.Status); err != nil {
		mapStoreError(c, err)
		return
	}
	ok(c, gin.H{"status": req.Status})
}
