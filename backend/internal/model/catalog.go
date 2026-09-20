package model

import (
	"encoding/json"
	"time"
)

// DeviceModel 设备型号目录（后台运营；APP 只读已上架项）。
type DeviceModel struct {
	ModelID             string   `json:"modelId"`
	Name                string   `json:"name"`
	Manufacturer        string   `json:"manufacturer"`
	Category            string   `json:"category"`
	Description         string   `json:"description"`
	CapabilityTags      []string `json:"capabilityTags"`
	SupportedHandleTags []string `json:"supportedHandleTags"`
	NamePatterns        []string `json:"namePatterns"`
	Protocol            string   `json:"protocol"`
	MinAppVersion       string   `json:"minAppVersion"`
	FirmwareUpdatable   bool     `json:"firmwareUpdatable"`
	Icon                string   `json:"icon"`
	Status              string   `json:"status"`
	// Config 机型扩展配置（JSONB，如 {"handleTags":[...],"difficulty":{...}}）；
	// 原有 text[] 列保持不动，向后兼容。
	Config    json.RawMessage `json:"config"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// GameCatalogItem 游戏目录（后台运营；requiredTags 驱动标签匹配）。
type GameCatalogItem struct {
	GameID             string   `json:"gameId"`
	Name               string   `json:"name"`
	Summary            string   `json:"summary"`
	RequiredTags       []string `json:"requiredTags"`
	DurationPresetsMin []int    `json:"durationPresetsMin"`
	ResourceVersion    string   `json:"resourceVersion"`
	ResourceURL        string   `json:"resourceUrl"`
	Status             string   `json:"status"`
	GrayBatch          *int     `json:"grayBatch,omitempty"`
	GrayRatio          float64  `json:"grayRatio"`
	CategoryLabel      string   `json:"categoryLabel"`
	PlayMode           string   `json:"playMode"`
	// Config 游戏扩展配置（JSONB，如 {"difficulty":{...}}）。
	Config json.RawMessage `json:"config"`
	// ResourceSha256 / ResourceSize 为「资源包由资产库下发」时的补充信息，
	// 由公开目录接口按 gameId + resourceVersion 命中 published 资产后回填。
	ResourceSha256 string    `json:"resourceSha256,omitempty"`
	ResourceSize   int64     `json:"resourceSize,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// TrainingSummary 训练摘要记录（高频采样文件另存 S3）。
type TrainingSummary struct {
	ID              int64           `json:"id"`
	RecordID        string          `json:"recordId"`
	UserID          *string         `json:"userId,omitempty"`
	GameID          string          `json:"gameId"`
	GameName        string          `json:"gameName"`
	DeviceModelID   string          `json:"deviceModelId"`
	DeviceModelName string          `json:"deviceModelName"`
	CapabilityTags  []string        `json:"capabilityTags"`
	Statistics      json.RawMessage `json:"statistics"`
	ClientVersion   string          `json:"clientVersion"`
	CompletedAt     time.Time       `json:"completedAt"`
	UploadedAt      time.Time       `json:"uploadedAt"`
}

// DashboardStats 后台仪表盘计数。
type DashboardStats struct {
	DeviceModels int64 `json:"deviceModels"`
	Games        int64 `json:"games"`
	Users        int64 `json:"users"`
	TrainingRecs int64 `json:"trainingRecords"`
	TodayRecs    int64 `json:"todayRecords"`
}

// OAuthIdentity 第三方身份绑定（wechat openid/unionid）。
type OAuthIdentity struct {
	ID        int64     `json:"id"`
	Provider  string    `json:"provider"`
	OpenID    string    `json:"openid"`
	UnionID   *string   `json:"unionid,omitempty"`
	UserID    string    `json:"userId"`
	CreatedAt time.Time `json:"createdAt"`
}
