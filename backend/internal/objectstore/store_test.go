package objectstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newLocalForTest(t *testing.T) *LocalStore {
	t.Helper()
	store, err := NewLocal(Options{
		LocalDir:     t.TempDir(),
		LocalBaseURL: "http://127.0.0.1:8080",
		SignSecret:   "test-secret",
	})
	if err != nil {
		t.Fatalf("创建本地对象存储失败: %v", err)
	}
	return store
}

func TestKeyBuildersAndValidation(t *testing.T) {
	cases := map[string]string{
		WebBundleKey("android", "0.2.0", "app.zip"):         "web/android/0.2.0/app.zip",
		GameResourceKey("kart-racing", "1.0.0", "a.png"):    "games/kart-racing/1.0.0/a.png",
		FirmwareKey("wobble-wrist-band", "1.2.0", "fw.bin"): "firmware/wobble-wrist-band/1.2.0/fw.bin",
		SampleKey("user-1", "rec-9", "samples.jsonl"):       "samples/user-1/rec-9/samples.jsonl",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("键构造 = %q，期望 %q", got, want)
		}
		if err := ValidateKey(got); err != nil {
			t.Errorf("构造出的键应合法，得到 %v", err)
		}
	}

	// 路径穿越与异常输入必须被拒绝或清洗。
	if err := ValidateKey("samples/../../etc/passwd"); err == nil {
		t.Error("含 .. 的键应被拒绝")
	}
	if err := ValidateKey("/abs/path"); err == nil {
		t.Error("以 / 开头的键应被拒绝")
	}
	if err := ValidateKey("a//b"); err == nil {
		t.Error("含空片段的键应被拒绝")
	}
	if err := ValidateKey("a?b=1"); err == nil {
		t.Error("含非法字符的键应被拒绝")
	}
	if err := ValidateKey(strings.Repeat("a", maxKeyLength+1)); err == nil {
		t.Error("超长键应被拒绝")
	}
	if err := ValidateKey(""); err == nil {
		t.Error("空键应被拒绝")
	}
	// 清洗：穿越片段不会进入最终键。
	suspicious := SampleKey("../../etc", "rec/../1", "p.jsonl")
	if strings.Contains(suspicious, "..") {
		t.Errorf("清洗后的键不应含 ..：%s", suspicious)
	}
	if err := ValidateKey(suspicious); err != nil {
		t.Errorf("清洗后的键应合法，得到 %v（%s）", err, suspicious)
	}
}

func TestLocalStorePutGetStatDelete(t *testing.T) {
	ctx := context.Background()
	store := newLocalForTest(t)
	key := SampleKey("user-1", "rec-1", "samples.jsonl")
	payload := []byte(`{"angleX":1}`)

	info, err := store.Put(ctx, key, bytes.NewReader(payload), int64(len(payload)), "application/json")
	if err != nil {
		t.Fatalf("Put 失败: %v", err)
	}
	if info.Size != int64(len(payload)) {
		t.Fatalf("Size = %d，期望 %d", info.Size, len(payload))
	}
	if info.ContentType != "application/json" {
		t.Fatalf("ContentType = %q", info.ContentType)
	}

	reader, gotInfo, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		_ = reader.Close()
		t.Fatalf("读取失败: %v", err)
	}
	// Windows 下文件句柄不释放就无法删除，必须先关闭（生产调用方同样应先 close）。
	if err := reader.Close(); err != nil {
		t.Fatalf("关闭读取器失败: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("内容不一致: %s", data)
	}
	if gotInfo.Size != int64(len(payload)) {
		t.Fatalf("Get 返回 Size = %d", gotInfo.Size)
	}

	stat, err := store.Stat(ctx, key)
	if err != nil || stat.Size != int64(len(payload)) {
		t.Fatalf("Stat = (%+v,%v)", stat, err)
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if _, _, err := store.Get(ctx, key); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("删除后 Get 应返回 ErrObjectNotFound，得到 %v", err)
	}
	// 删除不存在对象是幂等的。
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("重复删除应幂等: %v", err)
	}
	if _, err := store.Stat(ctx, key); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("Stat 不存在对象应返回 ErrObjectNotFound，得到 %v", err)
	}
}

func TestLocalStoreContentTypeByExtension(t *testing.T) {
	ctx := context.Background()
	store := newLocalForTest(t)
	cases := map[string]string{
		"firmware/board/1.0.0/fw.bin": "application/octet-stream",
		"games/kart/1.0.0/tile.png":   "image/png",
		"web/android/0.2.0/app.zip":   "application/zip",
	}
	for key, want := range cases {
		if _, err := store.Put(ctx, key, strings.NewReader("x"), 1, ""); err != nil {
			t.Fatalf("Put(%s) 失败: %v", key, err)
		}
		info, err := store.Stat(ctx, key)
		if err != nil {
			t.Fatalf("Stat(%s) 失败: %v", key, err)
		}
		if info.ContentType != want {
			t.Errorf("%s ContentType = %q，期望 %q", key, info.ContentType, want)
		}
	}
}

func TestLocalStoreRejectsTraversal(t *testing.T) {
	ctx := context.Background()
	store := newLocalForTest(t)
	// 即使绕过键构造器直接传穿越键，也必须被拒绝，且不落到根目录之外。
	if _, err := store.Put(ctx, "../escape.txt", strings.NewReader("x"), 1, ""); err == nil {
		t.Fatal("穿越键必须被拒绝")
	}
	if _, _, err := store.Get(ctx, "samples/../../secret"); err == nil {
		t.Fatal("穿越键读取必须被拒绝")
	}
	outside := filepath.Join(filepath.Dir(store.Root()), "escape.txt")
	if _, err := os.Stat(outside); err == nil {
		t.Fatalf("根目录外不应产生文件: %s", outside)
	}
}

func TestLocalStorePresignAndVerify(t *testing.T) {
	ctx := context.Background()
	store := newLocalForTest(t)
	key := FirmwareKey("board", "1.0.0", "fw.bin")

	putURL, err := store.PresignPut(ctx, key, 10*time.Minute)
	if err != nil {
		t.Fatalf("PresignPut 失败: %v", err)
	}
	if !strings.Contains(putURL, "/api/v1/files/"+key) || !strings.Contains(putURL, "op=put") {
		t.Fatalf("上传地址格式异常: %s", putURL)
	}
	getURL, err := store.PresignGet(ctx, key, 10*time.Minute)
	if err != nil {
		t.Fatalf("PresignGet 失败: %v", err)
	}
	if !strings.Contains(getURL, "op=get") {
		t.Fatalf("下载地址格式异常: %s", getURL)
	}

	// 签名可用；篡改键、操作或有效期都会失败。
	secret := []byte("test-secret")
	expires := time.Now().Add(time.Minute).Unix()
	signature := SignLocal(secret, OpGet, key, expires)
	if err := VerifyLocal(secret, OpGet, key, expires, signature); err != nil {
		t.Fatalf("合法签名应通过: %v", err)
	}
	if err := VerifyLocal(secret, OpPut, key, expires, signature); err == nil {
		t.Error("操作类型不一致应校验失败")
	}
	if err := VerifyLocal(secret, OpGet, key+"x", expires, signature); err == nil {
		t.Error("对象键不一致应校验失败")
	}
	if err := VerifyLocal(secret, OpGet, key, expires+1, signature); err == nil {
		t.Error("有效期不一致应校验失败")
	}
	if err := VerifyLocal([]byte("other"), OpGet, key, expires, signature); err == nil {
		t.Error("密钥不一致应校验失败")
	}
	if err := VerifyLocal(secret, OpGet, key, time.Now().Add(-time.Minute).Unix(), signature); err == nil {
		t.Error("过期签名应校验失败")
	}
	if err := VerifyLocal(secret, OpGet, key, expires, ""); err == nil {
		t.Error("空签名应校验失败")
	}
}

func TestLocalStorePresignRequiresBaseURL(t *testing.T) {
	store, err := NewLocal(Options{LocalDir: t.TempDir(), SignSecret: "s"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if _, err := store.PresignGet(context.Background(), "firmware/a/1/f.bin", time.Minute); err == nil {
		t.Fatal("未配置对外地址时应返回错误（避免给出不可用地址）")
	}
}

func TestLocalStoreHealthCheck(t *testing.T) {
	store := newLocalForTest(t)
	if err := store.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck 失败: %v", err)
	}
}

func TestNewFallsBackToLocalWhenS3NotConfigured(t *testing.T) {
	store, err := New(context.Background(), Options{LocalDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatalf("应回退本地实现: %v", err)
	}
	if store.Kind() != "local" {
		t.Fatalf("Kind = %q，期望 local", store.Kind())
	}
	// 配置不全（缺 secret）同样回退本地，不报错。
	partial, err := New(context.Background(), Options{
		Endpoint: "minio.example.com:9000", AccessKey: "ak", Bucket: "b", LocalDir: t.TempDir(),
	}, nil)
	if err != nil {
		t.Fatalf("配置不全时应回退本地实现: %v", err)
	}
	if partial.Kind() != "local" {
		t.Fatalf("Kind = %q，期望 local", partial.Kind())
	}
}

func TestPublicURL(t *testing.T) {
	store, err := NewLocal(Options{LocalDir: t.TempDir(), PublicBase: "https://cdn.example.com/"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if got := store.PublicURL("games/kart/1.0.0/a.png"); got != "https://cdn.example.com/games/kart/1.0.0/a.png" {
		t.Fatalf("PublicURL = %q", got)
	}
	noCDN, _ := NewLocal(Options{LocalDir: t.TempDir()})
	if got := noCDN.PublicURL("games/a"); got != "" {
		t.Fatalf("未配置 CDN 时应返回空串，得到 %q", got)
	}
	if got := JoinPublic("", "k"); got != "" {
		t.Fatalf("JoinPublic 空前缀应返回空串，得到 %q", got)
	}
}
