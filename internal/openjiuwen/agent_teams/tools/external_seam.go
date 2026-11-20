// 外部 CLI 翻译面注入缝：Python lazy import 的 Go 等价物。
//
// 依赖方向说明（Python vs Go）：agent_teams/external/ 顶层（client.py 等）
// 静态 import agent_teams.tools（client.py:36-39），而 tools 的对应消费
// （tool_member.py:314 等）是**函数内 lazy import**——Python 以运行期解析
// 保持双向依赖两侧都真实。Go 无 lazy import，包级静态 import 必然成环，
// 故 tools 侧的 6 个纯函数翻译符号以函数变量承载、external 包 init 注入
// 真实实现（运行期解析语义与 Python lazy import 等价）。
// 偏差登记：D-15.16-e20（Python lazy import → Go 函数变量缝）。

package tools

import (
	atschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/schema"
)

// ──────────────────────────── 缝变量 ────────────────────────────

var (
	// seamCLIAgentForProvider runtime provider → CLI kind。
	// Python: cli_agent_for_provider (external/manifest.py)
	seamCLIAgentForProvider func(provider string) string

	// seamExternalCliAgentSpecFromTemplate AgentTemplate runtime 翻译。
	// Python: external_cli_agent_spec_from_template (external/manifest.py)
	seamExternalCliAgentSpecFromTemplate func(template *hschema.AgentTemplateSpec) (*atschema.ExternalCliAgentSpec, error)

	// seamMergeExternalCliConfig 专家模板启动配置叠加声明配置。
	// Python: merge_external_cli_config (external/manifest.py)
	seamMergeExternalCliConfig func(declared, templateConfig *atschema.ExternalCliAgentSpec) (*atschema.ExternalCliAgentSpec, error)

	// seamModelApiProtocol provider → 协议标签。
	// Python: model_api_protocol (external/cli_agent/model_compat.py)
	seamModelApiProtocol func(provider string) string

	// seamProviderFilterForCLI CLI kind → provider 过滤器（未知 kind 得 nil）。
	// Python: provider_filter_for_cli (external/cli_agent/model_compat.py)
	seamProviderFilterForCLI func(cliAgent string) func(string) bool

	// seamTakesAuthFallback CLI kind 自登录失败时是否切团队模型。
	// Python: takes_auth_fallback (external/cli_agent/backends.py)
	seamTakesAuthFallback func(cliAgent string) bool

	// seamIsKnownBackend CLI kind 是否已知后端。
	// Python: is_known_backend (external/cli_agent/backends.py)
	seamIsKnownBackend func(name string) bool

	// seamAvailableBackends 全部已知后端名。
	// Python: available_backends (external/cli_agent/backends.py)
	seamAvailableBackends func() []string
)

// ──────────────────────────── 注入点（external 包 init 调用） ────────────────────────────

// SetExternalSeams 注入外部 CLI 翻译面（external 包 init 调用一次）。
func SetExternalSeams(seams ExternalSeams) {
	seamCLIAgentForProvider = seams.CLIAgentForProvider
	seamExternalCliAgentSpecFromTemplate = seams.ExternalCliAgentSpecFromTemplate
	seamMergeExternalCliConfig = seams.MergeExternalCliConfig
	seamModelApiProtocol = seams.ModelApiProtocol
	seamProviderFilterForCLI = seams.ProviderFilterForCLI
	seamTakesAuthFallback = seams.TakesAuthFallback
	seamIsKnownBackend = seams.IsKnownBackend
	seamAvailableBackends = seams.AvailableBackends
}

// ExternalSeams 注入集。
type ExternalSeams struct {
	CLIAgentForProvider              func(provider string) string
	ExternalCliAgentSpecFromTemplate func(template *hschema.AgentTemplateSpec) (*atschema.ExternalCliAgentSpec, error)
	MergeExternalCliConfig           func(declared, templateConfig *atschema.ExternalCliAgentSpec) (*atschema.ExternalCliAgentSpec, error)
	ModelApiProtocol                 func(provider string) string
	ProviderFilterForCLI             func(cliAgent string) func(string) bool
	TakesAuthFallback                func(cliAgent string) bool
	IsKnownBackend                   func(name string) bool
	AvailableBackends                func() []string
}

// SeamIsKnownBackend is_known_backend 缝取值（team_backend SpawnExternalCLI 消费）。
func SeamIsKnownBackend(name string) bool {
	if seamIsKnownBackend != nil {
		return seamIsKnownBackend(name)
	}
	return false
}

// SeamAvailableBackends available_backends 缝取值。
func SeamAvailableBackends() []string {
	if seamAvailableBackends != nil {
		return seamAvailableBackends()
	}
	return nil
}
