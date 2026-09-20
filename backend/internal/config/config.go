// Package config 从环境变量加载后端运行配置（支持 .env 文件，见 backend/.env.example）。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Addr HTTP 监听地址，如 :8080
	Addr string
	// DBURL PostgreSQL 连接串；为空时回退到内置内存存储（演示模式）
	DBURL string
	// JWTSecret 访问令牌签名密钥；生产必须通过环境注入
	JWTSecret string
	// AccessTokenTTL 访问令牌有效期
	AccessTokenTTL time.Duration
	// RefreshTokenTTL 刷新令牌有效期
	RefreshTokenTTL time.Duration
	// CORSOrigins 允许的跨端来源（网页/Capacitor/Tauri）
	CORSOrigins []string
	// LogRequests 是否打印请求日志
	LogRequests bool
	// Debug 开启 Gin 调试模式（生产关闭）
	Debug bool
	// LogJSON 结构化 JSON 日志（slog）
	LogJSON bool
	// WeChatAppID / WeChatSecret 微信开放平台（未配置则登录走 Mock，仅联调用）
	WeChatAppID  string
	WeChatSecret string
	// AdminBootstrapPhone 启动时把该手机号用户提升为 admin（演示/初始化用，可留空）
	AdminBootstrapPhone string

	// RedisURL Redis 连接串；为空时缓存退化为进程内实现（单实例可用）
	RedisURL string
	// DeviceCacheTTL 设备映射/白名单缓存时长
	DeviceCacheTTL time.Duration

	// ---- 对象存储（S3 兼容：MinIO / 阿里云 OSS / 腾讯云 COS / AWS S3）----
	// S3Endpoint 形如 minio.example.com:9000 或 s3.cn-north-1.amazonaws.com.cn
	S3Endpoint  string
	S3Region    string
	S3AccessKey string
	S3SecretKey string
	S3Bucket    string
	// S3UseSSL 是否使用 HTTPS 访问对象存储
	S3UseSSL bool
	// S3LocalDir 未配置 S3 时的本地磁盘回退目录（开发与单机部署用）
	S3LocalDir string
	// UploadURLTTL 预签名上传/下载地址有效期
	UploadURLTTL time.Duration

	// CDNBase 静态资源 CDN 前缀（Web 包 / 游戏资源 / 固件下载地址拼接用）
	CDNBase string
	// PublicBaseURL 本服务对外基础地址（本地对象存储模式生成签名直传地址用）
	PublicBaseURL string
	// UploadSignSecret 本地对象存储预签名密钥（留空取 JWTSecret）
	UploadSignSecret string
	// WebBundleVersion 当前 Web 资源版本号（客户端做增量更新对比；留空取 release-version.json）
	WebBundleVersion string
}

func Load() (*Config, error) {
	cfg := &Config{
		Addr:                getEnv("APP_ADDR", ":8080"),
		DBURL:               os.Getenv("APP_DB_URL"),
		JWTSecret:           getEnv("APP_JWT_SECRET", "zhiwellcare-dev-secret-change-me"),
		AccessTokenTTL:      2 * time.Hour,
		RefreshTokenTTL:     30 * 24 * time.Hour,
		CORSOrigins:         splitList(getEnv("APP_CORS_ORIGINS", "http://localhost:1420,http://localhost:4173,http://tauri.localhost,capacitor://localhost,http://localhost")),
		LogRequests:         getEnv("APP_LOG_REQUESTS", "true") == "true",
		Debug:               getEnv("APP_DEBUG", "false") == "true",
		LogJSON:             getEnv("APP_LOG_JSON", "true") == "true",
		WeChatAppID:         os.Getenv("APP_WECHAT_APPID"),
		WeChatSecret:        os.Getenv("APP_WECHAT_SECRET"),
		AdminBootstrapPhone: os.Getenv("APP_ADMIN_BOOTSTRAP_PHONE"),

		RedisURL:       os.Getenv("APP_REDIS_URL"),
		DeviceCacheTTL: durationFromSeconds(getEnv("APP_DEVICE_CACHE_TTL_SECONDS", "300"), 5*time.Minute),

		S3Endpoint:   os.Getenv("APP_S3_ENDPOINT"),
		S3Region:     getEnv("APP_S3_REGION", "us-east-1"),
		S3AccessKey:  os.Getenv("APP_S3_ACCESS_KEY"),
		S3SecretKey:  os.Getenv("APP_S3_SECRET_KEY"),
		S3Bucket:     os.Getenv("APP_S3_BUCKET"),
		S3UseSSL:     getEnv("APP_S3_USE_SSL", "true") == "true",
		S3LocalDir:   getEnv("APP_S3_LOCAL_DIR", "storage/objects"),
		UploadURLTTL: durationFromSeconds(getEnv("APP_UPLOAD_URL_TTL_SECONDS", "900"), 15*time.Minute),

		CDNBase:          strings.TrimRight(getEnv("APP_CDN_BASE", ""), "/"),
		PublicBaseURL:    strings.TrimRight(getEnv("APP_PUBLIC_BASE_URL", ""), "/"),
		UploadSignSecret: os.Getenv("APP_UPLOAD_SIGN_SECRET"),
		WebBundleVersion: os.Getenv("APP_WEB_BUNDLE_VERSION"),
	}
	if cfg.UploadSignSecret == "" {
		// 未单独配置时复用 JWT 密钥，避免本地部署多一个必填项。
		cfg.UploadSignSecret = cfg.JWTSecret
	}
	if !cfg.S3Configured() && cfg.PublicURLBase() == "" {
		fmt.Println("[config] 本地对象存储未配置 APP_PUBLIC_BASE_URL/APP_CDN_BASE → 无法生成直传地址，仅支持服务端中转上传")
	}
	if ttl := os.Getenv("APP_ACCESS_TTL_MINUTES"); ttl != "" {
		minutes, err := parseMinutes(ttl)
		if err != nil {
			return nil, fmt.Errorf("APP_ACCESS_TTL_MINUTES 无效: %w", err)
		}
		cfg.AccessTokenTTL = time.Duration(minutes) * time.Minute
	}
	if cfg.DBURL == "" {
		fmt.Println("[config] APP_DB_URL 未配置 → 使用内存存储（演示模式；生产请配置 PostgreSQL）")
	}
	return cfg, nil
}

// S3Configured 判断对象存储是否配置齐全（缺任一项则回退本地磁盘）。
func (c *Config) S3Configured() bool {
	return c.S3Endpoint != "" && c.S3AccessKey != "" && c.S3SecretKey != "" && c.S3Bucket != ""
}

// ObjectStoreMode 返回对象存储模式，用于启动日志与自检接口。
func (c *Config) ObjectStoreMode() string {
	if c.S3Configured() {
		return "s3"
	}
	return "local"
}

// CacheMode 返回缓存模式（redis / memory）。
func (c *Config) CacheMode() string {
	if c.RedisURL != "" {
		return "redis"
	}
	return "memory"
}

// PublicURLBase 返回静态资源对外前缀：优先 CDN，其次本服务对外地址。
func (c *Config) PublicURLBase() string {
	if c.CDNBase != "" {
		return c.CDNBase
	}
	return c.PublicBaseURL
}

func durationFromSeconds(value string, fallback time.Duration) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseMinutes(value string) (int, error) {
	var minutes int
	if _, err := fmt.Sscanf(value, "%d", &minutes); err != nil {
		return 0, err
	}
	return minutes, nil
}
