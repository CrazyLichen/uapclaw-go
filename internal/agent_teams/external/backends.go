// Python 源：openjiuwen/agent_teams/external/cli_agent/backends.py（190 行全文一比一）
// 归属修正（15.16e-T5）：此前落在 runtime/externalcli 包，但 Python 本文件在
// agent_teams/external/cli_agent 层——tools 包（team_backend）需消费
// IsKnownBackend/AvailableBackends 且不得依赖 runtime 层，按规则 9 抽回
// external 包（tools → external 无环；externalcli 包改为引用本包）。
package external

// 外部 CLI 后端种类注册表：对齐 Python
// openjiuwen/agent_teams/external/cli_agent/backends.py（190 行全文一比一）。

import (
	"fmt"
	"slices"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
)

// adaptersProviderFn adapter 面注入缝（偏差 D-15.16-e14）：Python 的
// backends.py 与 adapters.py 同在 external/cli_agent 包内直接互调；Go 的
// adapters 半区落在 runtime/externalcli（与 spawn/runtime 聚合），为避免
// tools → external → externalcli → tools 的环，adapter 可用集经本缝注入，
// externalcli 包构造期（init）调用 SetAdaptersProvider 回填。
// Python: available_adapters (adapters.py:509) 直调。
var adaptersProviderFn func() []string

// SetAdaptersProvider 注入 adapter 可用集（externalcli 包 init 调用）。
// Python 无对应独立函数（同包直调）。
func SetAdaptersProvider(fn func() []string) {
	adaptersProviderFn = fn
}

// SetAdaptersBuildProvider 注入 adapter 构建（externalcli 包 init 调用）。
func SetAdaptersBuildProvider(fn func(name string) (*AdapterBuildResult, error)) {
	adaptersBuildFn = fn
}

// ──────────────────────────── 枚举 ────────────────────────────

// BackendKind 后端种类："sdk" / "adapter" / "provider"。
// Python: BackendKind (backends.py:14)
type BackendKind string

const (
	// BackendKindSDK SDK 后端（claude / codex）。
	BackendKindSDK BackendKind = "sdk"
	// BackendKindAdapter 子进程 adapter 后端。
	BackendKindAdapter BackendKind = "adapter"
	// BackendKindProvider 第三方 harness provider 后端。
	BackendKindProvider BackendKind = "provider"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SdkRequirement 后端在其成员启动时 import 的可选 SDK。
//
// Python: SdkRequirement (backends.py:18)
type SdkRequirement struct {
	// Module 成员注册前探测的顶层 import 名。
	Module string
	// Distribution module 不可导入时安装的包名。
	Distribution string
	// Extra 拉取该包的 openjiuwen 可选依赖 extra。
	Extra string
}

// ExternalCliBackend 一个外部 CLI 后端种类的静态元数据。
//
// Python: ExternalCliBackend (backends.py:32)
type ExternalCliBackend struct {
	// Name 后端名
	Name string
	// Kind 后端种类
	Kind BackendKind
	// SupportsCommandOverride 是否支持 command 覆盖
	SupportsCommandOverride bool
	// InjectsSystemPromptViaArg 是否经启动参数注入 system prompt
	InjectsSystemPromptViaArg bool
	// SDKRequirement 成员启动时需要的 SDK；子进程 adapter 为 nil。
	SDKRequirement *SdkRequirement
	// Provider 驱动成员的 harness provider；子进程 adapter 为 nil。
	Provider string
}

// ──────────────────────────── 全局变量 ────────────────────────────

// sdkBackends SDK 后端注册表。
// Python: _SDK_BACKENDS (backends.py:58)
var sdkBackends = map[string]*ExternalCliBackend{
	"claude": {
		Name:                      "claude",
		Kind:                      BackendKindSDK,
		SupportsCommandOverride:   false,
		InjectsSystemPromptViaArg: true,
		SDKRequirement: &SdkRequirement{
			Module:       "claude_agent_sdk",
			Distribution: "claude-agent-sdk",
			Extra:        "claude",
		},
		Provider: "claudecode",
	},
	"codex": {
		Name:                      "codex",
		Kind:                      BackendKindSDK,
		SupportsCommandOverride:   false,
		InjectsSystemPromptViaArg: true,
		SDKRequirement: &SdkRequirement{
			Module:       "openai_codex",
			Distribution: "openai-codex",
			Extra:        "codex",
		},
		Provider: "codex",
	},
}

// sdkBackendNames SDK 后端名集（Python (*_SDK_BACKENDS) 展开）。
func sdkBackendNames() []string {
	names := make([]string, 0, len(sdkBackends))
	for name := range sdkBackends {
		names = append(names, name)
	}
	return names
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BackendFor 返回 name 的后端元数据，未支持时 nil。
// 查找顺序：SDK 后端 → 子进程 adapter → 第三方 harness provider（经进程内
// 注册或 openjiuwen.harness_providers 入口点组安装）。内置 provider 不是成员
// 后端；Claude Code 与 Codex 成员用其 SDK 后端。
//
// Python: backend_for (backends.py:92)
func BackendFor(name string) *ExternalCliBackend {
	if sdkBackend, ok := sdkBackends[name]; ok {
		copied := *sdkBackend
		return &copied
	}
	if adapterNames := adaptersAvailable(); slices.Contains(adapterNames, name) {
		adapter, err := adaptersBuild(name)
		if err != nil {
			return nil
		}
		return &ExternalCliBackend{
			Name:                      name,
			Kind:                      BackendKindAdapter,
			SupportsCommandOverride:   true,
			InjectsSystemPromptViaArg: adapter.InjectsSystemPromptViaArg(),
		}
	}
	if slices.Contains(ThirdPartyProviders(), name) {
		// system prompt 走 HarnessContext.system_prompt；provider 决定其 runtime
		// 如何接收它。
		return &ExternalCliBackend{
			Name:                      name,
			Kind:                      BackendKindProvider,
			SupportsCommandOverride:   false,
			InjectsSystemPromptViaArg: true,
			Provider:                  name,
		}
	}
	return nil
}

// ThirdPartyProviders 返回非内置的 harness provider 名。
//
// Python: third_party_providers (backends.py:114)
func ThirdPartyProviders() []string {
	names := harness_providers.AvailableProviders()
	out := make([]string, 0, len(names))
	for _, name := range names {
		if !slices.Contains(harness_providers.ProviderNames, name) {
			out = append(out, name)
		}
	}
	return out
}

// ProviderTakesEndpointModels 第三方 provider name 是否在 host 端点上跑模型。
// 仅当 provider 参与模型映射（ModelConfigProvider，protocol.py:65 的可选接口）
// 且其 card 声明 ENDPOINT_MODELS 时为 true；非 ModelConfigProvider 的 provider
// 恒 false。
//
// Python: provider_takes_endpoint_models (backends.py:122)
func ProviderTakesEndpointModels(name string) bool {
	provider, err := harness_providers.ResolveProvider(name)
	if err != nil {
		return false
	}
	// Python: isinstance(provider, ModelConfigProvider) 前置（backends.py:129）
	if _, ok := provider.(harnessprotocol.ModelConfigProvider); !ok {
		return false
	}
	card := provider.Card()
	return card.Supports(harnessprotocol.HarnessCapabilityEndpointModels)
}

// TakesAuthFallback CLI 种类 name 在其登录失败时是否切换到团队模型。
// 回退是 host 端点上的团队池条目，所以成员的 provider 必须参与模型映射
// （ModelConfigProvider）且其 card 同时声明 AUTH_FALLBACK 与 ENDPOINT_MODELS；
// 子进程 adapter 永远不会。
//
// Python: takes_auth_fallback (backends.py:133)
func TakesAuthFallback(name string) bool {
	backend := BackendFor(name)
	if backend == nil || backend.Provider == "" {
		return false
	}
	provider, err := harness_providers.ResolveProvider(backend.Provider)
	if err != nil {
		return false
	}
	// Python: isinstance(provider, ModelConfigProvider) 前置（backends.py:145）
	if _, ok := provider.(harnessprotocol.ModelConfigProvider); !ok {
		return false
	}
	card := provider.Card()
	return card.Supports(harnessprotocol.HarnessCapabilityAuthFallback) &&
		card.Supports(harnessprotocol.HarnessCapabilityEndpointModels)
}

// AvailableBackends 返回全部支持的外部 CLI 后端名。
//
// Python: available_backends (backends.py:153)
func AvailableBackends() []string {
	known := append([]string{}, "claude", "codex")
	known = append(known, adaptersAvailable()...)
	for _, name := range ThirdPartyProviders() {
		if !slices.Contains(known, name) {
			known = append(known, name)
		}
	}
	return known
}

// IsKnownBackend name 是否为支持的外部 CLI 后端。
//
// Python: is_known_backend (backends.py:160)
func IsKnownBackend(name string) bool {
	return BackendFor(name) != nil
}

// MissingSDKRequirement 当后端的 SDK 不可用时返回其 SdkRequirement。
// Python 用 importlib.util.find_spec 探测——不在本处 import SDK；harness 仍在
// 成员真正启动时惰性 import。Go 侧 SDK 依赖在编译期链接（同 15.9 的 SDK 工厂
// 缝模式），运行期不存在可探测的"缺失 SDK"状态，故本批恒返回 nil 并在此登记：
// claude/codex 分支体随 15.7/15.8 落地时如需运行期探测再补（D-15.13-x 关联）。
//
// Python: missing_sdk_requirement (backends.py:165)
func MissingSDKRequirement(name string) *SdkRequirement {
	backend := BackendFor(name)
	if backend == nil || backend.SDKRequirement == nil {
		return nil
	}
	return nil
}

// ──────────────────────────── adapter 缝助手（D-15.16-e14） ────────────────────────────

// adaptersAvailable adapter 可用名集（缝；externalcli 注入）。
func adaptersAvailable() []string {
	if adaptersProviderFn != nil {
		return adaptersProviderFn()
	}
	return nil
}

// adaptersBuild adapter 构建（缝）。
func adaptersBuild(name string) (*AdapterBuildResult, error) {
	if adaptersBuildFn == nil {
		return nil, fmt.Errorf("adapter provider not injected")
	}
	return adaptersBuildFn(name)
}

// AdapterBuildResult adapter 构建产物（缝承载类型；实际构造在 externalcli）。
type AdapterBuildResult struct {
	// Name adapter 名
	Name string
	// Command 启动 argv
	Command []string
	// BypassFlags 权限绕过 flag
	BypassFlags []string
	// StreamMode 流模式
	StreamMode string
	// injectsSystemPromptViaArg 是否经 argv 注入 system prompt
	injectsSystemPromptViaArg bool
}

// InjectsSystemPromptViaArg 是否经 argv 注入 system prompt。
// Python: CliAgentAdapter.injects_system_prompt_via_arg
func (a *AdapterBuildResult) InjectsSystemPromptViaArg() bool {
	return a.injectsSystemPromptViaArg
}

// SetInjectsSystemPromptViaArg 注入侧写值（跨包构造用）。
func (a *AdapterBuildResult) SetInjectsSystemPromptViaArg(v bool) {
	a.injectsSystemPromptViaArg = v
}

// adaptersBuildFn adapter 构建注入缝。
var adaptersBuildFn func(name string) (*AdapterBuildResult, error)
