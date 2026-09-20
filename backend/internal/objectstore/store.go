// Package objectstore 统一对象存储访问：Web 包 / 游戏资源 / 固件 / 训练采样文件。
//
// 两种实现：
//   - S3（MinIO / 阿里云 OSS / 腾讯云 COS / AWS S3，均走 S3 兼容协议），
//     上传下载都用预签名 URL，客户端直连对象存储，不经过业务服务器；
//   - 本地磁盘回退（未配置 S3 时），预签名 URL 指向本服务的签名接口，
//     让本地开发与单机部署拥有与生产一致的客户端流程。
package objectstore

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path"
	"regexp"
	"strings"
	"time"
)

// 对象键前缀：按用途分区，便于生命周期策略与权限隔离。
const (
	PrefixWeb      = "web"      // Web 前端资源包（Capacitor/Tauri 热更新包）
	PrefixGames    = "games"    // 游戏资源（美术/关卡/音频）
	PrefixFirmware = "firmware" // 设备固件
	PrefixSamples  = "samples"  // 训练高频采样原始文件
)

// maxKeyLength 限制对象键长度，避免异常长的键污染存储。
const maxKeyLength = 512

// keyPattern 只允许可安全映射为文件路径与 URL 的字符。
var keyPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// ObjectInfo 描述一个对象。
type ObjectInfo struct {
	Key         string    `json:"key"`
	Size        int64     `json:"size"`
	ContentType string    `json:"contentType"`
	ETag        string    `json:"etag,omitempty"`
	ModifiedAt  time.Time `json:"modifiedAt,omitempty"`
}

// Store 是对象存储最小接口。
type Store interface {
	// Kind 返回实现类型：s3 | local
	Kind() string
	Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) (ObjectInfo, error)
	Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)
	Stat(ctx context.Context, key string) (ObjectInfo, error)
	Delete(ctx context.Context, key string) error
	// PresignPut 返回客户端直传地址（有效期 ttl）。
	PresignPut(ctx context.Context, key string, ttl time.Duration) (string, error)
	// PresignGet 返回客户端下载地址（有效期 ttl）。
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	// PublicURL 返回 CDN 公开地址；未配置 CDN 前缀时返回空串，调用方改用 PresignGet。
	PublicURL(key string) string
	HealthCheck(ctx context.Context) error
	Close() error
}

// Options 是对象存储装配参数。
type Options struct {
	// ---- S3 兼容配置（任一为空则回退本地磁盘）----
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool

	// LocalDir 本地磁盘回退根目录。
	LocalDir string
	// PublicBase CDN / 公开访问前缀（如 https://cdn.example.com），用于 PublicURL。
	PublicBase string
	// LocalBaseURL 本地模式下预签名地址指向的本服务地址（如 http://127.0.0.1:8080）。
	LocalBaseURL string
	// SignSecret 本地模式预签名密钥。
	SignSecret string
}

// configured 判断 S3 是否配置齐全。
func (o Options) configured() bool {
	return o.Endpoint != "" && o.AccessKey != "" && o.SecretKey != "" && o.Bucket != ""
}

// New 按配置创建对象存储；S3 配置不全或连接失败时回退本地磁盘，保证服务可用。
func New(ctx context.Context, opts Options, logger *slog.Logger) (Store, error) {
	if opts.configured() {
		store, err := NewS3(ctx, opts)
		if err == nil {
			if logger != nil {
				logger.Info("对象存储已启用", "backend", "s3", "endpoint", opts.Endpoint, "bucket", opts.Bucket)
			}
			return store, nil
		}
		if logger != nil {
			logger.Warn("S3 不可用，回退本地磁盘对象存储", "err", err)
		}
	} else if logger != nil {
		logger.Info("未配置 APP_S3_*，对象存储使用本地磁盘", "dir", opts.LocalDir)
	}
	return NewLocal(opts)
}

// ---------- 对象键构造 ----------

// sanitizeSegment 清洗路径片段：去掉分隔符与可疑字符，避免路径穿越。
func sanitizeSegment(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\\", "-")
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, "..", "-")
	if value == "" {
		return "_"
	}
	return value
}

// ValidateKey 校验对象键合法（不含穿越、字符受限、长度受限）。
func ValidateKey(key string) error {
	if key == "" {
		return fmt.Errorf("对象键不能为空")
	}
	if len(key) > maxKeyLength {
		return fmt.Errorf("对象键过长（>%d）", maxKeyLength)
	}
	if strings.HasPrefix(key, "/") {
		return fmt.Errorf("对象键不能以 / 开头")
	}
	if strings.HasSuffix(key, "/") {
		return fmt.Errorf("对象键不能以 / 结尾")
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("对象键包含非法路径片段")
		}
	}
	if !keyPattern.MatchString(key) {
		return fmt.Errorf("对象键包含非法字符")
	}
	return nil
}

// WebBundleKey Web 资源包键：web/{platform}/{version}/app.zip
func WebBundleKey(platform, version, filename string) string {
	return path.Join(PrefixWeb, sanitizeSegment(platform), sanitizeSegment(version), sanitizeSegment(filename))
}

// GameResourceKey 游戏资源键：games/{gameId}/{version}/{filename}
func GameResourceKey(gameID, version, filename string) string {
	return path.Join(PrefixGames, sanitizeSegment(gameID), sanitizeSegment(version), sanitizeSegment(filename))
}

// FirmwareKey 固件键：firmware/{modelId}/{version}/{filename}
func FirmwareKey(modelID, version, filename string) string {
	return path.Join(PrefixFirmware, sanitizeSegment(modelID), sanitizeSegment(version), sanitizeSegment(filename))
}

// SampleKey 训练采样文件键：samples/{userId}/{recordId}/{filename}
func SampleKey(userID, recordID, filename string) string {
	return path.Join(PrefixSamples, sanitizeSegment(userID), sanitizeSegment(recordID), sanitizeSegment(filename))
}

// JoinPublic 拼接公开访问地址（CDN 前缀 + 对象键）。
func JoinPublic(base, key string) string {
	if base == "" || key == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(key, "/")
}
