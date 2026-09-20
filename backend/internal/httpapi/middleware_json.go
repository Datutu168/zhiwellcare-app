package httpapi

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// utf8BOM 是 UTF-8 字节序标记（EF BB BF）。
//
// 背景：Windows 侧 `Set-Content -Encoding utf8` / `Out-File`（PowerShell 5.1 及更早）
// 写出的请求体文件默认带 BOM，客户端再原样 POST 过来时，JSON 解析器会在首字节 `EF` 处失败
// → 旧行为是 400「请求参数格式不正确」，把「客户端编码问题」伪装成「参数校验失败」，
// 团队反复踩坑。这里只做一件事：把 JSON 请求体开头的 BOM 剥掉。
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// maxBOMStripBody 仅对不超过 1 MiB 的 JSON 请求体做处理。
//
// 为什么必须收窄（不要全局缓冲 body）：
//   - 本地对象存储的直传端点 `PUT/GET /api/v1/files/*key` 单文件上限 64MB 且是流式转发，
//     一旦在这里把 body 整个读进内存，大文件上传会直接吃掉进程内存；
//   - 该端点的 Content-Type 是 application/octet-stream，被下面的 JSON 判定天然排除，
//     而体积阈值又保证即使有人给一个超大 body 打上 application/json 也不会被缓冲。
const maxBOMStripBody = 1 << 20

// StripJSONBOM 中间件：Content-Type 为 JSON（application/json 或 xxx+json）且
// Content-Length 在 (0, 1MiB] 之间时，窥探前 3 字节；命中 UTF-8 BOM 则剥掉后重建
// c.Request.Body（同步修正 ContentLength），否则**原样透传**（把已读的 3 字节拼回）。
//
// 其它情况（非 JSON、无 Content-Length 的分块请求、超过阈值的大 body、带 Content-Encoding
// 的压缩体）完全不做任何读取或缓冲，保持既有行为。
func StripJSONBOM() gin.HandlerFunc {
	return func(c *gin.Context) {
		body := c.Request.Body
		if body == nil || body == http.NoBody {
			c.Next()
			return
		}
		if !isJSONContentType(c.ContentType()) {
			c.Next()
			return
		}
		// ContentLength == -1：分块传输（长度未知），不做处理；过大 body 同样跳过。
		if c.Request.ContentLength <= 0 || c.Request.ContentLength > maxBOMStripBody {
			c.Next()
			return
		}
		// 压缩体（gzip 等）首字节不是 BOM，但显式排除可避免「先解压再判断」的歧义。
		if encoding := strings.TrimSpace(c.GetHeader("Content-Encoding")); encoding != "" && !strings.EqualFold(encoding, "identity") {
			c.Next()
			return
		}

		head := make([]byte, len(utf8BOM))
		read, err := io.ReadFull(body, head)
		switch {
		case err == nil && bytes.Equal(head, utf8BOM):
			// 命中 BOM：丢弃它，其余字节原样作为新的 body。
			c.Request.Body = io.NopCloser(body)
			c.Request.ContentLength -= int64(len(utf8BOM))
			if c.Request.ContentLength < 0 {
				c.Request.ContentLength = 0
			}
		case err == nil:
			// 不是 BOM：把已读的 3 字节拼回，body 内容与原始完全一致。
			c.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(head[:read]), body))
		case err == io.ErrUnexpectedEOF || err == io.EOF:
			// body 不足 3 字节：原样拼回（短 body 由后续校验逻辑处理）。
			if read > 0 {
				c.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(head[:read]), body))
			}
		default:
			// 读取出错：不动 body，交给后续 handler 处理（不改变既有失败语义）。
		}
		c.Next()
	}
}

// isJSONContentType 判断媒体类型是否为 JSON（gin 的 c.ContentType() 已去掉 `; charset=` 参数）。
func isJSONContentType(contentType string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(contentType))
	if mediaType == "" {
		return false
	}
	// 容忍带参数调用（例如直接被单测传入 "application/json; charset=utf-8"）。
	if index := strings.IndexByte(mediaType, ';'); index >= 0 {
		mediaType = strings.TrimSpace(mediaType[:index])
	}
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}
