package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"zhiwellcare/backend/internal/auth"
	"zhiwellcare/backend/internal/cache"
	"zhiwellcare/backend/internal/config"
	"zhiwellcare/backend/internal/model"
	"zhiwellcare/backend/internal/objectstore"
	"zhiwellcare/backend/internal/rbac"
	"zhiwellcare/backend/internal/store"
)

// Deps 是路由装配所需的依赖。
type Deps struct {
	Config   *config.Config
	Store    store.Combined
	Manager  *auth.TokenManager
	WeChat   WeChatExchange // 微信 code2session；nil 时走本地 Mock
	Cache    cache.Store    // Redis / 内存缓存；nil 时限流退化为进程内计数
	Sessions *cache.Sessions
	Devices  *cache.Devices
	Assets   objectstore.Store // 对象存储（Web 包 / 游戏资源 / 固件 / 采样文件）
	// Permissions 用户权限解析器；nil 时按 Store + Cache 自动装配（TTL 取 DeviceCacheTTL）。
	Permissions *rbac.Resolver
	// StartedAt 用于 /healthz
	StartedAt time.Time
}

// NewEngine 构建 Gin 引擎（测试可复用，不监听端口）。
func NewEngine(deps Deps) *gin.Engine {
	if !deps.Config.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	engine := gin.New()
	// StripJSONBOM：只对小体积 JSON 请求体剥离 UTF-8 BOM（Windows 脚本常写出带 BOM 的 body，
	// 否则会被 JSON 解析器拒绝、报成「参数格式不正确」）；非 JSON 与超过 1MiB 的请求体
	// （如本地对象存储的 64MB 直传端点 PUT /api/v1/files/*key）完全不做读取或缓冲。
	engine.Use(gin.Recovery(), RequestID(), SecurityHeaders(), StripJSONBOM())
	if deps.Config.LogRequests {
		engine.Use(AccessLog())
	}
	engine.Use(cors.New(cors.Config{
		// Capacitor(Android/iOS)、Tauri(Windows) 与网页来源的 scheme 不同
		// （http://localhost / capacitor://localhost / http://tauri.localhost），
		// 因此用自定义校验函数支持全部白名单来源。
		AllowOriginFunc:  func(origin string) bool { return allowOrigin(deps.Config.CORSOrigins, origin) },
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}))

	// 权限解析器：TTL 复用设备缓存时长（默认 5 分钟），权限变更后由 handler 显式失效。
	permissions := deps.Permissions
	if permissions == nil {
		permissions = rbac.NewResolver(deps.Store, deps.Cache, deps.Config.DeviceCacheTTL)
	}

	authAPI := &authHandler{store: deps.Store, manager: deps.Manager, refreshTTL: deps.Config.RefreshTokenTTL, exchange: deps.WeChat, sessions: deps.Sessions}
	userAPI := &userHandler{store: deps.Store}
	catalogAPI := &catalogHandler{store: deps.Store, assets: deps.Assets, cfg: deps.Config}
	contentAPI := &contentHandler{store: deps.Store}
	adminUserAPI := &adminUserHandler{store: deps.Store}
	recordAPI := &recordHandler{store: deps.Store}
	fileAPI := &fileHandler{store: deps.Store, assets: deps.Assets, cfg: deps.Config}
	rbacAPI := &rbacHandler{store: deps.Store, perms: permissions}
	registryAPI := &deviceRegistryHandler{store: deps.Store, devices: deps.Devices}
	assetAPI := &assetHandler{store: deps.Store, assets: deps.Assets, cfg: deps.Config}
	requireAuth := RequireAuth(deps.Manager, deps.Sessions)
	// 权限点鉴权：admin 角色拥有全部权限，因此原有后台行为不变。
	allow := func(code string) gin.HandlerFunc { return RequirePermission(permissions, code) }

	// 健康检查（网页/壳探测用，无需鉴权）
	engine.GET("/healthz", func(c *gin.Context) {
		payload := gin.H{"status": "ok", "uptimeSeconds": int64(time.Since(deps.StartedAt).Seconds())}
		// 附带中间件后端类型，便于运维快速确认 Redis/S3 是否真正生效。
		if deps.Cache != nil {
			payload["cache"] = deps.Cache.Kind()
		}
		if deps.Assets != nil {
			payload["storage"] = deps.Assets.Kind()
		}
		ok(c, payload)
	})

	// 本地对象存储的签名直传端点（HMAC 校验，不依赖登录态；S3 部署下返回 404）
	if deps.Assets != nil {
		engine.PUT("/api/v1/files/*key", fileAPI.localFile)
		engine.GET("/api/v1/files/*key", fileAPI.localFile)
	}

	v1 := engine.Group("/api/v1")
	{
		// 公开目录：APP 训练游戏中心读取（后端动态下发，支持灰度/上下架）
		catalog := v1.Group("/catalog")
		{
			catalog.GET("/devices", catalogAPI.publicDeviceModels)
			catalog.GET("/games", catalogAPI.publicGames)
			// 内容中心：课程 / 商城商品（只下发已上架项，按 sort 升序）
			catalog.GET("/courses", contentAPI.publicCourses)
			catalog.GET("/goods", contentAPI.publicGoods)
		}
		v1.GET("/device/:modelId/games", catalogAPI.deviceGames)
		// 公开下发：Web 资源包 / 设备固件（有更新给地址，无更新只给版本）
		v1.GET("/app/web-bundle", assetAPI.publicWebBundle)
		v1.GET("/device/:modelId/firmware", assetAPI.publicFirmware)

		authGroup := v1.Group("/auth")
		{
			authGroup.POST("/register", RateLimit(deps.Cache, time.Minute, 10), authAPI.register)
			authGroup.POST("/login", RateLimit(deps.Cache, time.Minute, 20), authAPI.login)
			authGroup.POST("/refresh", authAPI.refresh)
			authGroup.POST("/logout", authAPI.logout)
			// 微信 UnionID 登录（真实凭据见 APP_WECHAT_APPID/SECRET；未配置走 Mock）
			authGroup.POST("/wechat/login", RateLimit(deps.Cache, time.Minute, 20), authAPI.wechatLogin)
		}

		protected := v1.Group("", requireAuth)
		{
			protected.GET("/me", userAPI.me)
			protected.PATCH("/me", userAPI.updateMe)
			protected.PATCH("/me/phone", authAPI.bindPhone) // 微信用户补绑手机号
			protected.POST("/training/records", recordAPI.submit)
			protected.GET("/me/training-records", recordAPI.myRecords)
			// 后台前端据此控制菜单/按钮（登录即可读自己的角色与权限）
			protected.GET("/me/permissions", rbacAPI.myPermissions)
			// 训练高频采样原始文件走对象存储直传（S3 预签名 / 本地签名端点）
			protected.POST("/training/records/:recordId/samples/upload-url", fileAPI.sampleUploadURL)
			protected.GET("/training/records/:recordId/samples/:filename/url", fileAPI.sampleDownloadURL)
		}

		admin := v1.Group("/admin", requireAuth)
		{
			admin.GET("/stats/dashboard", allow(model.PermRecordRead), adminUserAPI.dashboard)
			// 设备型号运营
			admin.GET("/devices", allow(model.PermDeviceRead), catalogAPI.adminListDevices)
			admin.POST("/devices", allow(model.PermDeviceWrite), catalogAPI.adminCreateDevice)
			admin.PUT("/devices/:modelId", allow(model.PermDeviceWrite), catalogAPI.adminUpdateDevice)
			admin.DELETE("/devices/:modelId", allow(model.PermDeviceWrite), catalogAPI.adminDeleteDevice)
			admin.PATCH("/devices/:modelId/status", allow(model.PermDeviceWrite), catalogAPI.adminSetDeviceStatus)
			// 游戏目录运营
			admin.GET("/games", allow(model.PermGameRead), catalogAPI.adminListGames)
			admin.POST("/games", allow(model.PermGameWrite), catalogAPI.adminCreateGame)
			admin.PUT("/games/:gameId", allow(model.PermGameWrite), catalogAPI.adminUpdateGame)
			admin.DELETE("/games/:gameId", allow(model.PermGameWrite), catalogAPI.adminDeleteGame)
			admin.PATCH("/games/:gameId/status", allow(model.PermGameWrite), catalogAPI.adminSetGameStatus)
			// 内容中心（课程 / 商城商品）
			admin.GET("/courses", allow(model.PermContentRead), contentAPI.adminListCourses)
			admin.POST("/courses", allow(model.PermContentWrite), contentAPI.adminCreateCourse)
			admin.PUT("/courses/:courseId", allow(model.PermContentWrite), contentAPI.adminUpdateCourse)
			admin.DELETE("/courses/:courseId", allow(model.PermContentWrite), contentAPI.adminDeleteCourse)
			admin.PATCH("/courses/:courseId/status", allow(model.PermContentWrite), contentAPI.adminSetCourseStatus)
			admin.GET("/goods", allow(model.PermContentRead), contentAPI.adminListGoods)
			admin.POST("/goods", allow(model.PermContentWrite), contentAPI.adminCreateGoods)
			admin.PUT("/goods/:goodsId", allow(model.PermContentWrite), contentAPI.adminUpdateGoods)
			admin.DELETE("/goods/:goodsId", allow(model.PermContentWrite), contentAPI.adminDeleteGoods)
			admin.PATCH("/goods/:goodsId/status", allow(model.PermContentWrite), contentAPI.adminSetGoodsStatus)
			// 用户与训练记录
			admin.GET("/users", allow(model.PermUserRead), adminUserAPI.listUsers)
			admin.PATCH("/users/:id/status", allow(model.PermUserWrite), adminUserAPI.setUserStatus)
			admin.GET("/training-records", allow(model.PermRecordRead), recordAPI.adminList)
			// RBAC：权限点 / 角色 / 用户角色
			admin.GET("/permissions", allow(model.PermRoleRead), rbacAPI.listPermissions)
			admin.GET("/roles", allow(model.PermRoleRead), rbacAPI.listRoles)
			admin.POST("/roles", allow(model.PermRoleWrite), rbacAPI.createRole)
			admin.PUT("/roles/:code", allow(model.PermRoleWrite), rbacAPI.updateRole)
			admin.DELETE("/roles/:code", allow(model.PermRoleWrite), rbacAPI.deleteRole)
			admin.GET("/users/:id/roles", allow(model.PermUserRead), rbacAPI.getUserRoles)
			admin.PUT("/users/:id/roles", allow(model.PermUserWrite), rbacAPI.setUserRoles)
			// 等同于 /api/v1/me/permissions，方便后台前端统一走 admin 前缀
			admin.GET("/me/permissions", rbacAPI.myPermissions)
			// 配置中心（JSONB）
			admin.GET("/config", allow(model.PermConfigRead), rbacAPI.listConfig)
			admin.GET("/config/:key", allow(model.PermConfigRead), rbacAPI.getConfig)
			admin.PUT("/config/:key", allow(model.PermConfigWrite), rbacAPI.setConfig)
			// 设备白名单 / 设备映射（写后失效 cache.Devices 读缓存）
			admin.GET("/device-whitelist", allow(model.PermWhitelistRead), registryAPI.listWhitelist)
			admin.POST("/device-whitelist", allow(model.PermWhitelistWrite), registryAPI.upsertWhitelist)
			admin.DELETE("/device-whitelist/:deviceKey", allow(model.PermWhitelistWrite), registryAPI.deleteWhitelist)
			admin.GET("/device-mappings", allow(model.PermDeviceRead), registryAPI.listMappings)
			admin.POST("/device-mappings", allow(model.PermDeviceWrite), registryAPI.upsertMapping)
			admin.DELETE("/device-mappings/:deviceKey", allow(model.PermDeviceWrite), registryAPI.deleteMapping)
			// 资产登记与发布
			admin.POST("/assets/upload-url", allow(model.PermAssetWrite), assetAPI.uploadURL)
			admin.POST("/assets", allow(model.PermAssetWrite), assetAPI.create)
			admin.GET("/assets", allow(model.PermAssetRead), assetAPI.list)
			admin.PATCH("/assets/:id/status", allow(model.PermAssetWrite), assetAPI.setStatus)
			admin.DELETE("/assets/:id", allow(model.PermAssetWrite), assetAPI.remove)
		}
	}

	engine.NoRoute(func(c *gin.Context) {
		fail(c, http.StatusNotFound, "接口不存在")
	})
	return engine
}

// allowOrigin 校验跨端来源：精确白名单 + 本地开发来源宽松放行。
func allowOrigin(allowed []string, origin string) bool {
	if origin == "" {
		return false
	}
	for _, item := range allowed {
		if item == "*" || origin == item {
			return true
		}
	}
	// 本地开发与原生壳默认放行（vite 任意端口 / Android WebView / Tauri 自定义 scheme）。
	if strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:") {
		return true
	}
	return strings.HasPrefix(origin, "capacitor://") || strings.HasPrefix(origin, "http://tauri.localhost")
}
