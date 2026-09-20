package store

import (
	"context"

	"zhiwellcare/backend/internal/model"
)

// DeviceModelStore 设备型号目录（后台运营）。
type DeviceModelStore interface {
	ListDeviceModels(ctx context.Context, includeOff bool) ([]model.DeviceModel, error)
	GetDeviceModel(ctx context.Context, modelID string) (*model.DeviceModel, error)
	UpsertDeviceModel(ctx context.Context, item model.DeviceModel) error
	DeleteDeviceModel(ctx context.Context, modelID string) error
	SetDeviceModelStatus(ctx context.Context, modelID, status string) error
}

// GameCatalogStore 游戏目录（后台运营）。
type GameCatalogStore interface {
	ListGames(ctx context.Context, includeOff bool) ([]model.GameCatalogItem, error)
	GetGame(ctx context.Context, gameID string) (*model.GameCatalogItem, error)
	UpsertGame(ctx context.Context, item model.GameCatalogItem) error
	DeleteGame(ctx context.Context, gameID string) error
	SetGameStatus(ctx context.Context, gameID, status string) error
}

// ContentStore 内容中心（课程 / 商城商品），后台运营维护、APP 只读已上架项。
//
// 与 GameCatalogStore 同构：includeOff=false 时只返回 status='on'，列表按 sort 升序
// （sort 相同按 ID 升序，保证输出稳定）。删除/改状态时目标不存在返回 ErrNotFound。
type ContentStore interface {
	ListCourses(ctx context.Context, includeOff bool) ([]model.Course, error)
	UpsertCourse(ctx context.Context, item model.Course) error
	DeleteCourse(ctx context.Context, courseID string) error
	SetCourseStatus(ctx context.Context, courseID, status string) error

	ListGoods(ctx context.Context, includeOff bool) ([]model.MallGoods, error)
	UpsertGoods(ctx context.Context, item model.MallGoods) error
	DeleteGoods(ctx context.Context, goodsID string) error
	SetGoodsStatus(ctx context.Context, goodsID, status string) error
}

// TrainingRecordFilter 训练记录查询过滤。
type TrainingRecordFilter struct {
	UserID   string
	GameID   string
	ModelID  string
	Page     int // 从 1 开始
	PageSize int
}

// TrainingRecordStore 训练摘要。
type TrainingRecordStore interface {
	SaveTrainingRecord(ctx context.Context, record model.TrainingSummary) error
	ListTrainingRecords(ctx context.Context, filter TrainingRecordFilter) (items []model.TrainingSummary, total int64, err error)
	// GetTrainingRecord 按 recordId 返回单条摘要；不存在返回 ErrNotFound。
	// 训练采样文件上传需要校验记录归属，故单独暴露。
	GetTrainingRecord(ctx context.Context, recordID string) (*model.TrainingSummary, error)
	DashboardStats(ctx context.Context) (*model.DashboardStats, error)
}

// OAuthParams 第三方身份登录/绑定的建号参数。
type OAuthParams struct {
	Provider string
	OpenID   string
	UnionID  *string
	Nickname string
}

// OAuthStore 第三方身份。
type OAuthStore interface {
	// FindUserByOpenID 返回已绑定该 openid 的用户；不存在返回 ErrUserNotFound。
	FindUserByOpenID(ctx context.Context, provider, openid string) (*model.User, error)
	// FindUserByUnionID 用 UnionID 合并同一开放平台下的多个应用账号。
	FindUserByUnionID(ctx context.Context, provider, unionid string) (*model.User, error)
	// GetOrCreateByOAuth 按 openid 查找或创建（同时落身份绑定），created 标记是否新用户。
	GetOrCreateByOAuth(ctx context.Context, params OAuthParams) (user *model.User, created bool, err error)
	// BindPhoneToUser 微信用户补绑手机号（校验唯一性）。
	BindPhoneToUser(ctx context.Context, userID, phone string) error
}

// Combined 是应用实际依赖的全部存储。
type Combined interface {
	UserStore
	RefreshTokenStore
	DeviceModelStore
	GameCatalogStore
	ContentStore
	TrainingRecordStore
	OAuthStore
	RBACStore
	ConfigStore
	DeviceRegistryStore
	AssetStore
}
