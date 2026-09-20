package objectstore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrObjectNotFound 对象不存在（S3 与本地实现统一返回该错误）。
var ErrObjectNotFound = errors.New("对象不存在")

// 预签名操作类型。
const (
	OpPut = "put"
	OpGet = "get"
)

// LocalStore 是本地磁盘实现：无对象存储时的回退方案。
//
// 上传下载地址指向本服务的 /api/v1/files/{key} 接口并携带 HMAC 签名，
// 客户端流程与 S3 预签名保持一致（可直传、可下载）。
type LocalStore struct {
	root       string
	publicBase string
	baseURL    string
	secret     []byte
}

// NewLocal 创建本地对象存储；目录不存在时自动创建。
func NewLocal(opts Options) (*LocalStore, error) {
	root := opts.LocalDir
	if root == "" {
		root = "storage/objects"
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("本地对象存储目录无效: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("创建本地对象存储目录失败: %w", err)
	}
	baseURL := strings.TrimRight(opts.LocalBaseURL, "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(opts.PublicBase, "/")
	}
	return &LocalStore{
		root:       abs,
		publicBase: strings.TrimRight(opts.PublicBase, "/"),
		baseURL:    baseURL,
		secret:     []byte(opts.SignSecret),
	}, nil
}

func (l *LocalStore) Kind() string { return "local" }

// resolve 把对象键映射为磁盘路径，并阻断路径穿越。
func (l *LocalStore) resolve(key string) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	full := filepath.Join(l.root, filepath.FromSlash(key))
	// 双保险：清洗后仍要确认落在根目录内。
	if full != l.root && !strings.HasPrefix(full, l.root+string(os.PathSeparator)) {
		return "", fmt.Errorf("对象键越界")
	}
	return full, nil
}

func (l *LocalStore) Put(_ context.Context, key string, reader io.Reader, _ int64, contentType string) (ObjectInfo, error) {
	full, err := l.resolve(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return ObjectInfo{}, fmt.Errorf("创建目录失败: %w", err)
	}
	// 先写临时文件再改名，避免半截文件被读到。
	temp, err := os.CreateTemp(filepath.Dir(full), ".upload-*")
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("创建临时文件失败: %w", err)
	}
	tempName := temp.Name()
	size, copyErr := io.Copy(temp, reader)
	closeErr := temp.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tempName)
		if copyErr != nil {
			return ObjectInfo{}, fmt.Errorf("写入对象失败: %w", copyErr)
		}
		return ObjectInfo{}, fmt.Errorf("写入对象失败: %w", closeErr)
	}
	if err := os.Rename(tempName, full); err != nil {
		_ = os.Remove(tempName)
		return ObjectInfo{}, fmt.Errorf("保存对象失败: %w", err)
	}
	return l.info(key, full, size, contentType)
}

func (l *LocalStore) Get(_ context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	full, err := l.resolve(key)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	file, err := os.Open(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ObjectInfo{}, fmt.Errorf("%w: %s", ErrObjectNotFound, key)
		}
		return nil, ObjectInfo{}, fmt.Errorf("读取对象失败: %w", err)
	}
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, ObjectInfo{}, fmt.Errorf("读取对象信息失败: %w", err)
	}
	info, _ := l.info(key, full, stat.Size(), "")
	info.ModifiedAt = stat.ModTime()
	return file, info, nil
}

func (l *LocalStore) Stat(_ context.Context, key string) (ObjectInfo, error) {
	full, err := l.resolve(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	stat, err := os.Stat(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ObjectInfo{}, fmt.Errorf("%w: %s", ErrObjectNotFound, key)
		}
		return ObjectInfo{}, fmt.Errorf("读取对象信息失败: %w", err)
	}
	if stat.IsDir() {
		return ObjectInfo{}, fmt.Errorf("%w: %s", ErrObjectNotFound, key)
	}
	info, _ := l.info(key, full, stat.Size(), "")
	info.ModifiedAt = stat.ModTime()
	return info, nil
}

func (l *LocalStore) Delete(_ context.Context, key string) error {
	full, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("删除对象失败: %w", err)
	}
	return nil
}

func (l *LocalStore) PresignPut(_ context.Context, key string, ttl time.Duration) (string, error) {
	return l.presign(OpPut, key, ttl)
}

func (l *LocalStore) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	return l.presign(OpGet, key, ttl)
}

func (l *LocalStore) presign(op, key string, ttl time.Duration) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	if l.baseURL == "" {
		return "", fmt.Errorf("本地对象存储未配置对外地址（请设置 APP_PUBLIC_BASE_URL 或 APP_CDN_BASE）")
	}
	expires := time.Now().Add(ttl).Unix()
	signature := SignLocal(l.secret, op, key, expires)
	return fmt.Sprintf("%s/api/v1/files/%s?op=%s&exp=%d&sig=%s", l.baseURL, key, op, expires, signature), nil
}

// PublicURL 本地模式返回 CDN/静态前缀拼接地址（未配置则为空，调用方改用 PresignGet）。
func (l *LocalStore) PublicURL(key string) string { return JoinPublic(l.publicBase, key) }

// HealthCheck 确认根目录可写。
func (l *LocalStore) HealthCheck(context.Context) error {
	probe := filepath.Join(l.root, ".healthcheck")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return fmt.Errorf("本地对象存储不可写: %w", err)
	}
	return os.Remove(probe)
}

func (l *LocalStore) Close() error { return nil }

// Root 暴露磁盘根目录（自检与运维排查用）。
func (l *LocalStore) Root() string { return l.root }

func (l *LocalStore) info(key, full string, size int64, contentType string) (ObjectInfo, error) {
	if contentType == "" {
		contentType = guessContentType(full)
	}
	return ObjectInfo{Key: key, Size: size, ContentType: contentType, ModifiedAt: time.Now()}, nil
}

// ---------- 预签名（仅本地模式使用） ----------

// SignLocal 计算本地对象接口的 HMAC-SHA256 签名。
func SignLocal(secret []byte, op, key string, expires int64) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(op))
	mac.Write([]byte("\n"))
	mac.Write([]byte(key))
	mac.Write([]byte("\n"))
	mac.Write([]byte(strconv.FormatInt(expires, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyLocal 校验签名与有效期；任何不满足都返回错误。
func VerifyLocal(secret []byte, op, key string, expires int64, signature string) error {
	if signature == "" {
		return fmt.Errorf("缺少签名")
	}
	if expires <= 0 {
		return fmt.Errorf("缺少有效期")
	}
	if time.Now().Unix() > expires {
		return fmt.Errorf("签名已过期")
	}
	expected := SignLocal(secret, op, key, expires)
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("签名不匹配")
	}
	return nil
}

// guessContentType 按扩展名推断类型（本地回退时无法依赖对象元数据）。
func guessContentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".json", ".jsonl":
		// 训练采样文件按 JSON Lines 落盘，与 .json 同样按 JSON 返回。
		return "application/json"
	case ".zip":
		return "application/zip"
	case ".apk":
		return "application/vnd.android.package-archive"
	case ".bin", ".fw":
		return "application/octet-stream"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".mp3":
		return "audio/mpeg"
	case ".txt", ".log", ".csv":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
