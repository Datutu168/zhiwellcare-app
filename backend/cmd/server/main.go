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
	if cfg.DBURL != "" {
		pgStore, err = store.NewPostgres(ctx, cfg.DBURL)
		if err != nil {
			log.Fatalf("[db] %v", err)
		}
		defer pgStore.Close()
		if err := pgStore.RunMigrations(ctx, "migrations"); err != nil {
			log.Fatalf("[db] 迁移失败: %v", err)
		}
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
