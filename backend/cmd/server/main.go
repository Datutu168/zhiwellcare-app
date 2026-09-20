// 智为康乐消费版后端服务入口。
//
// 运行方式：
//
//	cd backend
//	go run ./cmd/server
//
// 环境变量见 internal/config 与 backend/.env.example。
package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"zhiwellcare/backend/internal/auth"
	"zhiwellcare/backend/internal/cache"
	"zhiwellcare/backend/internal/config"
	"zhiwellcare/backend/internal/httpapi"
	"zhiwellcare/backend/internal/objectstore"
	"zhiwellcare/backend/internal/rbac"
	"zhiwellcare/backend/internal/store"
	"zhiwellcare/backend/internal/wechat"
)

func main() {
	// .env 可选：不存在时静默忽略（例如容器中直接用环境变量）。
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[config] %v", err)
	}
	if cfg.LogJSON {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	}
	ctx := context.Background()

	var data store.Combined
	var pgStore *store.Postgres
	// migrationsApplied 标记「本次启动是否在持久化库上执行过迁移」：
	// 迁移（如 004 给 admin 补授 content:*）直接写库，绕过了应用写路径的缓存失效，
	// 需要在本进程开始服务之前清一次权限缓存（见下方 purgeRBACCache）。
	migrationsApplied := false
	if cfg.DBURL != "" {
		pgStore, err = store.NewPostgres(ctx, cfg.DBURL)
		if err != nil {
			log.Fatalf("[db] %v", err)
		}
		defer pgStore.Close()
		if err := pgStore.RunMigrations(ctx, "migrations"); err != nil {
			log.Fatalf("[db] 迁移失败: %v", err)
		}
		migrationsApplied = true
		data = pgStore
		// 启动即打印「实际连到哪个库」（不含口令）：
		// APP_DB_URL 被误清空、godotenv 从 .env 回填正式库这类事故可以一眼看见。
		dbHost, dbName := store.DBTarget(cfg.DBURL)
		slog.Info("数据库已连接", "driver", "postgresql", "host", dbHost, "database", dbName)
		if !isLocalDBHost(dbHost) {
			slog.Warn("数据库不在本机，请确认 APP_DB_URL 指向的环境符合预期（避免误连正式库）",
				"host", dbHost, "database", dbName)
		}
	} else {
		data = store.NewMemory()
		slog.Warn("数据库未配置 APP_DB_URL，使用内存存储（演示模式，重启数据丢失）")
	}

	// 启动引导：把指定手机号的已注册用户提升为管理员（RBAC 初始化用，可留空）。
	bootstrapAdmin(ctx, data, cfg.AdminBootstrapPhone)

	manager := auth.NewTokenManager(cfg.JWTSecret, cfg.AccessTokenTTL)

	// 缓存：Redis 优先（多实例共享限流/会话/设备映射），未配置或连不上时回退进程内实现。
	cacheStore := cache.New(ctx, cfg.RedisURL, slog.Default())
	defer func() { _ = cacheStore.Close() }()
	sessions := cache.NewSessions(cacheStore)
	devices := cache.NewDevices(cacheStore, cfg.DeviceCacheTTL)
	slog.Info("缓存就绪", "backend", cacheStore.Kind())

	// 权限缓存清理：必须在「迁移 + 启动引导」之后、开始服务之前。
	//
	// 迁移与 bootstrapAdmin 都是直接写库，不走分配角色/改角色权限那些会精确失效缓存的写路径；
	// 而 Redis 开 AOF 时 zwkl:rbac:user:* 会跨重启存活，于是升级后（例如 004 给 admin 补授
	// content:read/content:write）服务仍按旧的 15 个权限判定，管理员访问新接口会 403 直到 TTL 到期。
	// 因此这里按前缀整体清一次；清理失败只告警，绝不阻断启动（缓存只是加速层，回源即可恢复）。
	// 条件：本次启动确实写过 RBAC 相关数据 —— 持久化库启动（迁移已执行）或配置了管理员引导
	// （bootstrapAdmin 会写 user_roles）；两者都会绕过写路径的缓存失效。
	if migrationsApplied || cfg.AdminBootstrapPhone != "" {
		purgeRBACCache(ctx, cacheStore)
	}

	// 对象存储：S3 优先，未配置或不可用时回退本地磁盘（客户端流程一致）。
	assets, err := objectstore.New(ctx, objectstore.Options{
		Endpoint:     cfg.S3Endpoint,
		Region:       cfg.S3Region,
		AccessKey:    cfg.S3AccessKey,
		SecretKey:    cfg.S3SecretKey,
		Bucket:       cfg.S3Bucket,
		UseSSL:       cfg.S3UseSSL,
		LocalDir:     cfg.S3LocalDir,
		PublicBase:   cfg.CDNBase,
		LocalBaseURL: cfg.PublicBaseURL,
		SignSecret:   cfg.UploadSignSecret,
	}, slog.Default())
	if err != nil {
		log.Fatalf("[storage] %v", err)
	}
	slog.Info("对象存储就绪", "backend", assets.Kind())

	var exchange httpapi.WeChatExchange
	if cfg.WeChatAppID != "" && cfg.WeChatSecret != "" {
		client := wechat.NewClient(cfg.WeChatAppID, cfg.WeChatSecret)
		exchange = client.Code2Session
		slog.Info("微信登录已启用", "appid", cfg.WeChatAppID)
	} else {
		slog.Warn("未配置微信 AppID/Secret，微信登录使用本地 Mock（仅联调用）")
	}

	engine := httpapi.NewEngine(httpapi.Deps{
		Config:    cfg,
		Store:     data,
		Manager:   manager,
		WeChat:    exchange,
		Cache:     cacheStore,
		Sessions:  sessions,
		Devices:   devices,
		Assets:    assets,
		StartedAt: time.Now(),
	})

	server := &http.Server{
		Addr:         cfg.Addr,
		Handler:      engine,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		slog.Info("智为康乐后端已启动", "addr", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[http] 监听失败: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	slog.Info("服务已优雅退出")
}

// isLocalDBHost 判断数据库主机是否为本机（localhost / 回环地址 / 未指定即 Unix socket）。
// 供启动日志决定是否发出「可能误连正式库」的醒目告警。
func isLocalDBHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "", "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	}
	return false
}

// purgeRBACCache 启动时清理全部用户权限缓存（前缀 zwkl:rbac）。
//
// 只记日志、不阻断启动：清理失败时缓存仍在（可能短暂沿用旧权限集合），
// 但迁移与 DB 已经就绪，管理员可通过重试或等 TTL（默认 5 分钟）自然收敛。
func purgeRBACCache(ctx context.Context, cacheStore cache.Store) {
	if cacheStore == nil {
		return
	}
	purgeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := rbac.PurgeCache(purgeCtx, cacheStore); err != nil {
		slog.Warn("权限缓存清理失败（不阻断启动；升级后的新权限可能要等 TTL 才会生效）",
			"prefix", rbac.CachePrefix(), "backend", cacheStore.Kind(), "err", err)
		return
	}
	slog.Info("权限缓存已清理", "prefix", rbac.CachePrefix(), "backend", cacheStore.Kind())
}

// bootstrapAdmin 启动时把指定手机号用户提升为管理员（RBAC 初始化；手机号可为空）。
func bootstrapAdmin(ctx context.Context, data store.Combined, phone string) {
	if phone == "" {
		return
	}
	user, err := data.GetUserByPhone(ctx, phone)
	if err != nil {
		slog.Warn("管理员引导跳过：手机号用户不存在", "phone", phone)
		return
	}
	if user.Role == "admin" {
		slog.Info("管理员已就绪", "phone", phone)
		return
	}
	if err := data.SetUserRole(ctx, user.ID, "admin"); err != nil {
		slog.Error("管理员引导失败", "phone", phone, "err", err)
		return
	}
	slog.Info("已提升为管理员", "phone", phone, "user_id", user.ID)
}
