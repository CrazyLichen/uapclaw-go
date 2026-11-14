// ModelSelection DTO：稳定的模型与模型组业务引用及 resolver 产物。
// 对应 Python 代码：gateway/common/model_selection.py:1-76（该文件无双副本，gateway 侧为唯一源）。
package common

import (
	"errors"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModelSelection 稳定的模型/模型组业务引用。
// Python: model_selection.py:10 ModelSelection（pydantic extra="forbid"）。
// 序列化键与上游 JSON 契约一致，不得改名。
type ModelSelection struct {
	// Type 引用类型，仅接受 "model" / "model_group"（Python Literal 约束，校验见 Validate）
	Type string `json:"type"`
	// ID 模型或模型组标识
	ID string `json:"id"`
	// RouteID 路由标识，仅 model_group 引用有效；空串语义同 Python None
	RouteID string `json:"route_id"`
}

// ResolvedModel 解析后的单模型（Python: model_selection.py:41 ResolvedModel）。
type ResolvedModel struct {
	// ModelID 模型标识
	ModelID string `json:"model_id"`
	// Source 来源：defaults / agentos
	Source string `json:"source"`
	// ModelName 模型名
	ModelName string `json:"model_name"`
	// Provider 客户端提供方
	Provider string `json:"provider"`
	// APIBase 接口地址
	APIBase string `json:"api_base"`
	// APIKey 接口密钥
	APIKey string `json:"api_key"`
	// EndpointProfile 端点画像（Python None → 空串）
	EndpointProfile string `json:"endpoint_profile"`
	// FallbackTag 回退标签（Python None → 空串）
	FallbackTag string `json:"fallback_tag"`
	// ModelDescription 模型描述（Python None → 空串）
	ModelDescription string `json:"model_description"`
	// ClientOptions 客户端选项（已剔除保留键，Python None → 空 map）
	ClientOptions map[string]any `json:"client_options"`
	// RequestDefaults 请求默认参数（Python None → 空 map）
	RequestDefaults map[string]any `json:"request_defaults"`
}

// ResolvedRoute 解析后的单条路由（Python: model_selection.py:56 ResolvedRoute）。
type ResolvedRoute struct {
	// RouteID 路由标识
	RouteID string `json:"route_id"`
	// Model 路由指向的模型
	Model ResolvedModel `json:"model"`
	// Enabled 是否启用
	Enabled bool `json:"enabled"`
	// RequestOverrides 请求参数覆盖（Python None → 空 map）
	RequestOverrides map[string]any `json:"request_overrides"`
	// TPM 每分钟 token 上限（Python None → 0）
	TPM int `json:"tpm"`
	// RPM 每分钟请求上限（Python None → 0）
	RPM int `json:"rpm"`
	// Timeout 超时秒数（Python None → 0）
	Timeout float64 `json:"timeout"`
}

// ResolvedModelGroup 解析后的模型组（Python: model_selection.py:67 ResolvedModelGroup）。
type ResolvedModelGroup struct {
	// ModelGroupID 模型组标识
	ModelGroupID string `json:"model_group_id"`
	// Routes 路由列表
	Routes []ResolvedRoute `json:"routes"`
	// RequestConfig 组级请求配置（Python None → 空 map）
	RequestConfig map[string]any `json:"request_config"`
	// Routing 组级路由配置（Python None → 空 map）
	Routing map[string]any `json:"routing"`
}

// ResolvedSelection 联合产物：单模型或模型组。
// Python: model_selection.py:75 ResolvedSelection = Union[ResolvedModel, ResolvedModelGroup]。
// Go 无联合类型，调用方用类型断言区分（spec §3.1）。
type ResolvedSelection any

// ──────────────────────────── 导出函数 ────────────────────────────

// Validate 校验 ModelSelection 三个约束。
// Python: model_selection.py:16 validate_id / :24 validate_route_id / :34 validate_route_scope。
// 上游 pydantic validator 抛 ValueError，Go 侧等价返回普通 error，文案逐字对齐。
func (s *ModelSelection) Validate() error {
	// Python: model_selection.py:16-22 validate_id：去空白后不得为空
	if strings.TrimSpace(s.ID) == "" {
		return errors.New("id must not be empty")
	}
	// Python: model_selection.py:24-32 validate_route_id：非 None 时去空白不得为空
	if s.RouteID != "" && strings.TrimSpace(s.RouteID) == "" {
		return errors.New("route_id must not be empty")
	}
	// Python: model_selection.py:34-38 validate_route_scope：model 引用不得带 route_id
	if s.Type == "model" && s.RouteID != "" {
		return errors.New("route_id is only valid for a model_group selection")
	}
	return nil
}
