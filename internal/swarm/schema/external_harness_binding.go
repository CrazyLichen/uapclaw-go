// Python 源：jiuwenswarm/server/runtime/agent_adapter/external_harness_helpers.py
// resolve_external_harness_binding 返回 dict（:226-232/:241-250）+ interface.py
// :5915-5933 request.trusted_runtime_binding 盖章载体（15.18 对齐复核，用户裁决：
// request 章 + sessionAdapterOptions 参数缝统一类型化，禁 map/any 载体）。
package schema

import hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"

// ──────────────────────────── 结构体 ────────────────────────────

// ExternalHarnessBinding 一次绑定解析的结果（会话与运行时的不可变绑定记录）。
// kind=external_harness 时含专家模板/provider/runtime/包路径；kind=deep_agent
// 时仅含 kind。Python: external_harness_helpers.py resolve_external_harness_binding
// 返回 dict 的键名即 json tag。
type ExternalHarnessBinding struct {
	// Kind 绑定种类：external_harness / deep_agent
	Kind string `json:"kind"`
	// AgentTemplateName 专家模板名
	AgentTemplateName string `json:"agent_template_name,omitempty"`
	// ProviderName provider 名
	ProviderName string `json:"provider_name,omitempty"`
	// Runtime 专家包声明的 runtime（deep_agent 为 nil）
	Runtime *hschema.AgentRuntimeSpec `json:"runtime,omitempty"`
	// PackagePath 专家包目录
	PackagePath string `json:"package_path,omitempty"`
}
