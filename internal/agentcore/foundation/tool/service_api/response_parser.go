package service_api

import (
	"compress/flate"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	 zlib "compress/zlib"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseResponseParser HTTP 响应解析器接口。
//
// 对应 Python: openjiuwen/core/foundation/tool/service_api/response_parser.py (BaseResponseParser)
type BaseResponseParser interface {
	// CanParse 判断是否能解析此 content-type 的响应
	CanParse(contentType string, statusCode int, headers map[string]string) bool
	// Parse 解析响应数据
	Parse(data []byte, headers map[string]string) (any, error)
}

// BaseResponseDecompressor HTTP 响应解压器接口。
//
// 对应 Python: openjiuwen/core/foundation/tool/service_api/response_parser.py (BaseResponseDecompressor)
type BaseResponseDecompressor interface {
	// CanDecompress 判断是否能解压此编码
	CanDecompress(encoding string) bool
	// Decompress 解压数据
	Decompress(data []byte) ([]byte, error)
}

// JsonResponseParser JSON 响应解析器。
//
// 识别标准 JSON content-type（application/json、text/json）
// 和 RFC 6839 +json 后缀类型（如 application/video+json、application/hal+json）。
//
// 对应 Python: JsonResponseParser
type JsonResponseParser struct{}

// TextResponseParser 文本响应解析器。
//
// 识别 text/*、application/xml 等 content-type。
//
// 对应 Python: TextResponseParser
type TextResponseParser struct{}

// GzipDecompressor GZIP 解压器。
//
// 对应 Python: GzipDecompressor
type GzipDecompressor struct{}

// DeflateDecompressor Deflate 解压器。
//
// 对应 Python: DeflateDecompressor
type DeflateDecompressor struct{}

// ParserRegistry 响应解析器注册表，单例模式。
//
// 注册解析器和解压器，根据 content-type 选择解析器，
// 根据 content-encoding 解压响应数据。
//
// 对应 Python: ParserRegistry（Singleton 元类）
type ParserRegistry struct {
	parsers      []BaseResponseParser
	decompressors map[string]BaseResponseDecompressor
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	registryOnce sync.Once
	registryInst *ParserRegistry
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetParserRegistry 获取全局 ParserRegistry 单例。
func GetParserRegistry() *ParserRegistry {
	registryOnce.Do(func() {
		registryInst = &ParserRegistry{}
		registryInst.registerDefaults()
	})
	return registryInst
}

// Register 注册响应解析器（按注册顺序匹配，先注册的优先）。
func (r *ParserRegistry) Register(parser BaseResponseParser) {
	r.parsers = append(r.parsers, parser)
}

// RegisterDecompressor 注册响应解压器。
func (r *ParserRegistry) RegisterDecompressor(encoding string, decompressor BaseResponseDecompressor) {
	r.decompressors[strings.ToLower(encoding)] = decompressor
}

// Parse 解析 HTTP 响应数据。
//
// 流程：
//  1. 按 content-encoding 解压
//  2. 按 content-type 选择解析器
//  3. 调用解析器解析数据
//
// 对应 Python: ParserRegistry.parse()
func (r *ParserRegistry) Parse(headers map[string]string, data []byte, statusCode int) (any, error) {
	lowerHeaders := toLowerHeaders(headers)
	contentType := lowerHeaders["content-type"]
	if contentType == "" {
		contentType = "text/plain"
	}
	contentEncoding := lowerHeaders["content-encoding"]

	// 1. 解压
	if contentEncoding != "" && len(data) > 0 {
		data = r.applyDecompression(data, contentEncoding)
	}

	// 2. 选择解析器
	for _, parser := range r.parsers {
		if parser.CanParse(contentType, statusCode, headers) {
			return parser.Parse(data, headers)
		}
	}

	return nil, fmt.Errorf("未找到响应解析器，content-type: %s", contentType)
}

// CanParse 判断 JSON 解析器是否能解析此 content-type。
//
// 支持：
//   - 标准 JSON 类型：application/json、text/json、text/x-json、application/javascript
//   - RFC 6839 +json 后缀：application/video+json、application/hal+json
//   - 无 Content-Type 时检查 Accept 头
func (p JsonResponseParser) CanParse(contentType string, statusCode int, headers map[string]string) bool {
	if contentType == "" {
		// 无 Content-Type，检查 Accept 头
		if statusCode == 200 {
			accept := ""
			if headers != nil {
				accept = headers["Accept"]
				if accept == "" {
					accept = headers["accept"]
				}
			}
			lowerAccept := strings.ToLower(accept)
			if strings.Contains(lowerAccept, "application/json") || strings.Contains(lowerAccept, "json") {
				return true
			}
		}
		return false
	}

	contentTypeLower := strings.ToLower(contentType)

	// 标准 JSON content-type
	jsonContentTypes := []string{
		"application/json",
		"text/json",
		"text/x-json",
		"application/javascript",
	}
	for _, ct := range jsonContentTypes {
		if contentTypeLower == ct {
			return true
		}
	}

	// RFC 6839 +json 后缀
	if strings.HasSuffix(contentTypeLower, "+json") {
		return true
	}

	// 子字符串匹配
	if strings.Contains(contentTypeLower, "application/json") || strings.Contains(contentTypeLower, "text/json") {
		return true
	}

	return false
}

// Parse 解析 JSON 响应数据。
func (p JsonResponseParser) Parse(data []byte, headers map[string]string) (any, error) {
	if len(data) == 0 {
		return map[string]any{}, nil
	}

	// 解码字节
	decoded, err := decodeBytes(data, headers)
	if err != nil {
		return nil, fmt.Errorf("JSON 响应字节解码失败: %w", err)
	}

	var result any
	if err := json.Unmarshal([]byte(decoded), &result); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}
	return result, nil
}

// CanParse 判断文本解析器是否能解析此 content-type。
func (p TextResponseParser) CanParse(contentType string, statusCode int, headers map[string]string) bool {
	if contentType == "" {
		return false
	}

	textContentTypes := []string{
		"text/plain",
		"text/html",
		"text/xml",
		"text/css",
		"text/javascript",
		"text/csv",
		"application/xml",
		"application/xhtml+xml",
		"application/javascript",
		"application/x-www-form-urlencoded",
	}

	contentTypeLower := strings.ToLower(contentType)

	for _, ct := range textContentTypes {
		if contentTypeLower == ct {
			return true
		}
	}

	// text/* 通配
	if strings.HasPrefix(contentTypeLower, "text/") {
		return true
	}

	// XML 类型（不含 json）
	if strings.Contains(contentTypeLower, "xml") && !strings.Contains(contentTypeLower, "json") {
		return true
	}

	return false
}

// Parse 解析文本响应数据。
func (p TextResponseParser) Parse(data []byte, headers map[string]string) (any, error) {
	if len(data) == 0 {
		return "", nil
	}

	decoded, err := decodeBytes(data, headers)
	if err != nil {
		return nil, fmt.Errorf("文本响应字节解码失败: %w", err)
	}
	return decoded, nil
}

// CanDecompress 判断是否支持 GZIP 解压。
func (d GzipDecompressor) CanDecompress(encoding string) bool {
	lower := strings.ToLower(encoding)
	return lower == "gzip" || lower == "x-gzip"
}

// Decompress 解压 GZIP 数据。
func (d GzipDecompressor) Decompress(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(strings.NewReader(string(data)))
	if err != nil {
		// 尝试 zlib 解压作为备选
		if r, zlibErr := zlib.NewReader(strings.NewReader(string(data))); zlibErr == nil {
			defer r.Close()
			return io.ReadAll(r)
		}
		return nil, fmt.Errorf("GZIP 解压失败: %w", err)
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// CanDecompress 判断是否支持 Deflate 解压。
func (d DeflateDecompressor) CanDecompress(encoding string) bool {
	return strings.ToLower(encoding) == "deflate"
}

// Decompress 解压 Deflate 数据。
func (d DeflateDecompressor) Decompress(data []byte) ([]byte, error) {
	reader := flate.NewReader(strings.NewReader(string(data)))
	defer reader.Close()
	result, err := io.ReadAll(reader)
	if err != nil {
		// 尝试无 header 模式
		reader2 := flate.NewReader(strings.NewReader(string(data)))
		defer reader2.Close()
		result, err = io.ReadAll(reader2)
		if err != nil {
			return nil, fmt.Errorf("Deflate 解压失败: %w", err)
		}
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// registerDefaults 注册默认解析器和解压器。
func (r *ParserRegistry) registerDefaults() {
	r.parsers = []BaseResponseParser{
		JsonResponseParser{},
		TextResponseParser{},
	}
	r.decompressors = map[string]BaseResponseDecompressor{
		"gzip":    GzipDecompressor{},
		"deflate": DeflateDecompressor{},
	}
}

// applyDecompression 按 content-encoding 解压数据。
func (r *ParserRegistry) applyDecompression(data []byte, contentEncoding string) []byte {
	if contentEncoding == "" || len(data) == 0 {
		return data
	}

	encodings := strings.Split(contentEncoding, ",")
	for _, enc := range encodings {
		enc = strings.TrimSpace(strings.ToLower(enc))
		if decompressor, ok := r.decompressors[enc]; ok {
			if decompressor.CanDecompress(enc) {
				decompressed, err := decompressor.Decompress(data)
				if err != nil {
					break
				}
				data = decompressed
			}
		}
	}
	return data
}

// extractCharset 从 Content-Type 头提取 charset 编码。
func extractCharset(contentType string) string {
	if contentType == "" {
		return ""
	}
	parts := strings.Split(contentType, ";")
	for _, part := range parts[1:] {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "charset=") {
			charset := strings.SplitN(part, "=", 2)[1]
			charset = strings.TrimSpace(charset)
			charset = strings.Trim(charset, `"'`)
			return charset
		}
	}
	return ""
}

// decodeBytes 按编码解码字节数据为字符串。
func decodeBytes(data []byte, headers map[string]string) (string, error) {
	contentType := ""
	if headers != nil {
		contentType = headers["Content-Type"]
		if contentType == "" {
			contentType = headers["content-type"]
		}
	}
	encoding := extractCharset(contentType)
	if encoding == "" {
		encoding = "utf-8"
	}

	// 简化：Go 标准库的 string(data) 假定 UTF-8
	// 对于非 UTF-8 编码，需要引入 golang.org/x/text/encoding 包
	// 当前仅支持 UTF-8 和 ASCII 兼容编码
	if strings.ToLower(encoding) == "utf-8" || strings.ToLower(encoding) == "us-ascii" {
		return string(data), nil
	}
	// 非 UTF-8 编码回退到 UTF-8 尝试
	return string(data), nil
}

// toLowerHeaders 将 headers 的键转换为小写。
func toLowerHeaders(headers map[string]string) map[string]string {
	result := make(map[string]string, len(headers))
	for k, v := range headers {
		result[strings.ToLower(k)] = v
	}
	return result
}
