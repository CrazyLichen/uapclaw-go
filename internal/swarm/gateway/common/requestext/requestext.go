package requestext

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

// ──────────────────────────── 枚举 ────────────────────────────

// 环境变量与常量（Python: request_ext.py:27-40）。
const (
	// EnvForwardHeaders 扩展透传白名单 env（Swarm 名）。
	EnvForwardHeaders = "JIUWENSWARM_REQUEST_EXT_FORWARD_HEADERS"
	// EnvForwardHeadersLegacy 企业 kub 兼容 env 名。
	EnvForwardHeadersLegacy = "JIUWENCLAW_REQUEST_EXT_FORWARD_HEADERS"
	// MetadataKey ext 在 Message.metadata 中的键名。
	MetadataKey = "ext"
	// InternalHeaderName Gateway → AgentServer 的内部 REST 载体头名。
	// 浏览器侧仍使用配置白名单中的原始 header/query，不能把本头当作公开入口或鉴权凭据。
	InternalHeaderName = "X-Jiuwenswarm-Request-Ext"
	// MaxInternalJSONBytes 内部载体 JSON 上限；Base64URL 后最大约 5.34 KiB。
	MaxInternalJSONBytes = 4 * 1024
	// MaxInternalHeaderChars 内部载体 Base64URL 字符上限。
	MaxInternalHeaderChars = 6 * 1024
)

// extKey context.Context 载体键（对齐 Python _request_ext ContextVar，D-B2-10）。
type extKey struct{}

// RequestExtCodecError 内部请求扩展字段无法安全编码或解码。
// Python: request_ext.py:42-45 RequestExtCodecError。
type RequestExtCodecError struct {
	// Msg 错误描述
	Msg string
}

func (e *RequestExtCodecError) Error() string { return e.Msg }

// ──────────────────────────── 全局变量 ────────────────────────────

// runtimeOverride 运行时覆盖白名单。优先级：SetForwardHeaders > 环境变量 > 空（关闭）。
// Python: request_ext.py:51 _runtime_override。Go 无单线程语义，用互斥锁保护
// （对齐 asyncio→Go 并发移植模式）。
var (
	runtimeOverrideMu sync.RWMutex
	runtimeOverride   []string
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetForwardHeaders 整体覆盖 forward_headers（管控面 / DB 监听器使用）。
// 传 nil 等价于撤销覆盖，回落到环境变量；传空 slice 等价于关闭透传。
// Python: request_ext.py:47-56。
func SetForwardHeaders(headers []string) {
	runtimeOverrideMu.Lock()
	defer runtimeOverrideMu.Unlock()
	if headers == nil {
		runtimeOverride = nil
		return
	}
	cleaned := make([]string, 0, len(headers))
	for _, h := range headers {
		if trimmed := strings.TrimSpace(h); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	runtimeOverride = cleaned
}

// RegisterForwardHeader 加法注册单个字段名（扩展加载时使用）。
// Python: request_ext.py:59-68。
func RegisterForwardHeader(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	runtimeOverrideMu.Lock()
	defer runtimeOverrideMu.Unlock()
	if runtimeOverride == nil {
		runtimeOverride = envForwardHeaders()
	}
	for _, existing := range runtimeOverride {
		if existing == name {
			return
		}
	}
	runtimeOverride = append(runtimeOverride, name)
}

// RegisterForwardHeaders 加法注册多个字段名。
// Python: request_ext.py:71-75。
func RegisterForwardHeaders(names []string) {
	for _, n := range names {
		RegisterForwardHeader(n)
	}
}

// BuildExtFromSource 从入站 mapping（HTTP header 或 WS query 字典）按配置抽取扩展字段。
// 支持值为字符串或字符串列表（parse_qs 输出）；列表取首元素；键名匹配大小写不敏感。
// Python: request_ext.py:90-115 build_ext_from_source。
func BuildExtFromSource(source map[string]any) map[string]any {
	names := readForwardHeaders()
	if len(names) == 0 || len(source) == 0 {
		return nil
	}
	lowerToActual := map[string]string{}
	for key := range source {
		lowerToActual[strings.ToLower(key)] = key
	}
	out := map[string]any{}
	for _, name := range names {
		// 内部载体只能由 Gateway 根据普通白名单字段重新生成，不能把外部同名头
		// 当作扩展值再次封装，更不能形成“客户端自称内部调用”的歧义。
		if strings.EqualFold(name, InternalHeaderName) {
			continue
		}
		actual, ok := lowerToActual[strings.ToLower(name)]
		if !ok {
			continue
		}
		val := source[actual]
		if list, isList := val.([]string); isList {
			if len(list) == 0 {
				continue
			}
			val = list[0]
		}
		if str, isStr := val.(string); isStr && str == "" {
			continue
		}
		out[name] = val
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// EncodeInternalHeader 将白名单过滤后的 ext 编码成内部 HTTP header 值。
// Base64URL 仅解决 Unicode、引号和换行等字符的 HTTP 传输安全问题，不提供
// 加密、签名或鉴权能力。
// Python: request_ext.py:118-147 encode_internal_header。
func EncodeInternalHeader(ext map[string]any) (string, error) {
	if len(ext) == 0 {
		return "", nil
	}
	// JSON 键序排序，保证编码确定性（对齐 sort_keys=True）。
	keys := make([]string, 0, len(ext))
	for key := range ext {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make(map[string]any, len(ext))
	for _, key := range keys {
		ordered[key] = ext[key]
	}
	raw, err := json.Marshal(ordered)
	if err != nil {
		return "", &RequestExtCodecError{Msg: "request ext must contain JSON values"}
	}
	if len(raw) > MaxInternalJSONBytes {
		return "", &RequestExtCodecError{Msg: fmt.Sprintf("request ext JSON exceeds %d bytes", MaxInternalJSONBytes)}
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	if len(encoded) > MaxInternalHeaderChars {
		return "", &RequestExtCodecError{Msg: fmt.Sprintf("encoded request ext exceeds %d characters", MaxInternalHeaderChars)}
	}
	return encoded, nil
}

// DecodeInternalHeader 解码 Gateway 生成的内部 HTTP header，并校验其大小和 JSON 类型。
// Python: request_ext.py:149-183 decode_internal_header。
func DecodeInternalHeader(value string) (map[string]any, error) {
	encoded := strings.TrimSpace(value)
	if encoded == "" {
		return nil, nil
	}
	if len(encoded) > MaxInternalHeaderChars {
		return nil, &RequestExtCodecError{Msg: fmt.Sprintf("encoded request ext exceeds %d characters", MaxInternalHeaderChars)}
	}
	for _, ch := range encoded {
		if !isASCIIAlnumOrURLSafe(ch) {
			return nil, &RequestExtCodecError{Msg: "request ext is not valid Base64URL"}
		}
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, &RequestExtCodecError{Msg: "request ext is not valid Base64URL"}
	}
	if len(raw) > MaxInternalJSONBytes {
		return nil, &RequestExtCodecError{Msg: fmt.Sprintf("request ext JSON exceeds %d bytes", MaxInternalJSONBytes)}
	}
	var decoded map[string]any
	if uerr := json.Unmarshal(raw, &decoded); uerr != nil {
		return nil, &RequestExtCodecError{Msg: "request ext is not valid UTF-8 JSON"}
	}
	// Python 额外校验 keys 均为 string；Go map[string]any 键类型天然满足。
	if len(decoded) == 0 {
		return nil, nil
	}
	return decoded, nil
}

// AttachToMetadata 构造 Message 时使用：将 ext 写入 metadata。
// ext 为 nil 时返回原 metadata（Python 由 ContextVar 兜底，Go 侧由调用方显式传）。
// Python: request_ext.py:190-204 attach_to_metadata。
func AttachToMetadata(metadata map[string]any, ext map[string]any) map[string]any {
	if len(ext) == 0 {
		return metadata
	}
	out := make(map[string]any, len(metadata)+1)
	for key, value := range metadata {
		out[key] = value
	}
	out[MetadataKey] = ext
	return out
}

// WithRequestExt 把 ext 写入 ctx（AgentServer 入口抬升）。
// Python: request_ext.py:207-215 lift_from_metadata（ContextVar.set → ctx 载体，D-B2-10）。
func WithRequestExt(ctx context.Context, metadata map[string]any) context.Context {
	if metadata == nil {
		return ctx
	}
	raw, ok := metadata[MetadataKey].(map[string]any)
	if !ok || len(raw) == 0 {
		return ctx
	}
	return context.WithValue(ctx, extKey{}, raw)
}

// RequestExtFromContext 读取当前请求的扩展字段。无字段时返回空 map。
// Python: request_ext.py:223-226 get_ext。
func RequestExtFromContext(ctx context.Context) map[string]any {
	if ctx == nil {
		return map[string]any{}
	}
	cur, _ := ctx.Value(extKey{}).(map[string]any)
	out := make(map[string]any, len(cur))
	for key, value := range cur {
		out[key] = value
	}
	return out
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// envForwardHeaders 读取白名单 env（两套名字等价，先读 Swarm 名）。
// Python: request_ext.py:77-88 _env_forward_headers。
func envForwardHeaders() []string {
	raw := os.Getenv(EnvForwardHeaders)
	if raw == "" {
		raw = os.Getenv(EnvForwardHeadersLegacy)
	}
	if raw == "" {
		return []string{}
	}
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// readForwardHeaders 读取生效白名单（覆盖 > env）。
// Python: request_ext.py:84-88 _read_forward_headers。
func readForwardHeaders() []string {
	runtimeOverrideMu.RLock()
	defer runtimeOverrideMu.RUnlock()
	if runtimeOverride != nil {
		out := make([]string, len(runtimeOverride))
		copy(out, runtimeOverride)
		return out
	}
	return envForwardHeaders()
}

// isASCIIAlnumOrURLSafe Base64URL 字符判定（对齐 ch.isalnum() and ascii / "-_"）。
func isASCIIAlnumOrURLSafe(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
}
