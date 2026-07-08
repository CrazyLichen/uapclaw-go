package e2a

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// E2AProvenance 记录 E2A 信封的出处。
//
// E2A 为统一载体：ACP、A2A 等消息经转换后均应落在此结构中。
// SourceProtocol 标明进入 E2A 之前所依据的主要协议或「原生 E2A」。
// Converter / ConvertedAt / Details 标明由谁、何时、从何种具体调用转换而来。
//
// 对应 Python: jiuwenswarm/common/e2a/models.py (E2AProvenance)
type E2AProvenance struct {
	// SourceProtocol 来源协议（默认 "e2a"）
	SourceProtocol string `json:"source_protocol"`
	// Converter 转换器标识
	Converter string `json:"converter"`
	// ConvertedAt 转换时间（RFC 3339）
	ConvertedAt string `json:"converted_at"`
	// Details 转换详情
	Details map[string]any `json:"details"`
}

// E2AFileRef 文件引用（用于 params.files / params.attachments 等元素，对齐 MCP/A2A 常见形态）。
//
// 对应 Python: jiuwenswarm/common/e2a/models.py (E2AFileRef)
type E2AFileRef struct {
	// URI 文件地址
	URI string `json:"uri"`
	// Name 文件名
	Name string `json:"name"`
	// MimeType MIME 类型
	MimeType string `json:"mime_type"`
	// Size 文件大小
	Size int `json:"size"`
	// Meta 扩展元数据（对应 Python _meta）
	Meta map[string]any `json:"_meta"`
}

// E2AAuth 身份鉴权信息（按需填充）。
//
// 建议：生产环境用 CredentialRef / oauth 等间接引用，由网关在受控环境换票。
//
// 对应 Python: jiuwenswarm/common/e2a/models.py (E2AAuth)
type E2AAuth struct {
	// MethodID 方法标识
	MethodID string `json:"method_id"`
	// BearerToken Bearer 令牌
	BearerToken string `json:"bearer_token"`
	// APIKeyRef API Key 引用
	APIKeyRef string `json:"api_key_ref"`
	// CredentialRef 凭证引用
	CredentialRef string `json:"credential_ref"`
	// ExtraHeaders 额外请求头
	ExtraHeaders map[string]string `json:"extra_headers"`
	// Meta 扩展元数据（对应 Python _meta）
	Meta map[string]any `json:"_meta"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// IdentityOrigin 身份来源：谁触发了本次对 Agent 的请求。
//
// 对应 Python: jiuwenswarm/common/e2a/models.py (IdentityOrigin)
type IdentityOrigin string

const (
	// IdentityOriginSystem 系统
	IdentityOriginSystem IdentityOrigin = "system"
	// IdentityOriginUser 用户
	IdentityOriginUser IdentityOrigin = "user"
	// IdentityOriginAgent 代理
	IdentityOriginAgent IdentityOrigin = "agent"
	// IdentityOriginService 服务
	IdentityOriginService IdentityOrigin = "service"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// identityOriginLookup 字符串值到 IdentityOrigin 枚举的查找表
var identityOriginLookup map[string]IdentityOrigin

// ──────────────────────────── 导出函数 ────────────────────────────

// NewE2AProvenance 创建 E2AProvenance 实例，设置默认 SourceProtocol="e2a"。
func NewE2AProvenance() *E2AProvenance {
	return &E2AProvenance{
		SourceProtocol: E2ASourceProtocolE2A,
	}
}

// NewE2AFileRef 创建文件引用实例。
func NewE2AFileRef(uri string) *E2AFileRef {
	return &E2AFileRef{URI: uri}
}

// NewE2AAuth 创建身份鉴权实例（零值）。
func NewE2AAuth() *E2AAuth {
	return &E2AAuth{}
}

// AllIdentityOrigins 返回所有 IdentityOrigin 枚举值。
func AllIdentityOrigins() []IdentityOrigin {
	return []IdentityOrigin{
		IdentityOriginSystem,
		IdentityOriginUser,
		IdentityOriginAgent,
		IdentityOriginService,
	}
}

// ParseIdentityOrigin 从字符串解析 IdentityOrigin，不合法返回错误。
func ParseIdentityOrigin(s string) (IdentityOrigin, error) {
	if o, ok := identityOriginLookup[s]; ok {
		return o, nil
	}
	return IdentityOrigin(""), fmt.Errorf("不合法的 IdentityOrigin 值: %q", s)
}

// IsValidIdentityOrigin 判断字符串是否为合法的 IdentityOrigin 值。
func IsValidIdentityOrigin(s string) bool {
	_, ok := identityOriginLookup[s]
	return ok
}

// String 实现 fmt.Stringer 接口。
func (o IdentityOrigin) String() string {
	return string(o)
}

// GoString 实现 fmt.GoStringer 接口，返回带类型名前缀的字符串表示。
func (o IdentityOrigin) GoString() string {
	return fmt.Sprintf("e2a.IdentityOrigin(%q)", string(o))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	// 构建 IdentityOrigin 查找表
	origins := AllIdentityOrigins()
	identityOriginLookup = make(map[string]IdentityOrigin, len(origins))
	for _, o := range origins {
		identityOriginLookup[string(o)] = o
	}
}
