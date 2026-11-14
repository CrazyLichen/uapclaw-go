package harness_providers

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	hresources "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
)

// 本文件对应 Python openjiuwen/harness_providers/factory.py（473 行 / 18 函数）：
// 从 AgentTemplate 清单创建协议 harness。
//
// 清单（package_type=agent_template 的 manifest.json，或内存中的
// AgentTemplateSpec）是 harness-expert 编写的 agent 描述：身份、模型、
// persona 提示词段落、MCP 服务器，以及 portable skills 与 DeepAgent 专属
// 扩展点（tools / rails / sub-agents）。工厂把它映射到按名字选中的 provider。
//
// provider 名字的解析顺序（factory.py:12-18）：
//  1. 内置 provider（ProviderNames，闭集，永远优先）
//  2. 进程内注册的 provider（RegisterHarnessProvider）
//  3. 已安装包的 entry point
//
// ⚠️ Go 差异（spec D1）：Go 无 importlib.metadata，第 3 步不实现。
// HarnessProviderEntryPointGroup 常量保留——它是公开 API 字符串，
// 供第三方包在发布清单里声明。对应的三个 Python 函数
// （_provider_entry_points / _forget_replaced_distribution / _top_level_packages）
// 不移植。

// ──────────────────────────── 结构体（接口定义）────────────────────────────

// ProviderFactory 零参构造 HarnessProvider 的工厂函数。
// Python: factory.py:76 ProviderFactory
type ProviderFactory func() (harnessprotocol.HarnessProvider, error)

// BuildHarnessContextOptions BuildHarnessContext 的参数。
// Python: factory.py:399-416 的 keyword-only 参数
type BuildHarnessContextOptions struct {
	// Provider provider 名（必填）
	Provider string
	// HostSessionID 宿主会话标识（必填）
	HostSessionID string
	// AgentID agent 标识；空则用 card.id，再空则用 card.name
	AgentID string
	// AgentName agent 名；空则用 card.name
	AgentName string
	// Language 渲染清单提示词的语言（空则 "cn"）
	Language string
	// Cwd 工作目录（空表示未指定）
	Cwd string
	// Env 环境变量
	Env map[string]string
	// ExtraSystemPrompt 追加到系统提示词末尾的额外内容
	ExtraSystemPrompt string
	// HostCapabilities 宿主声明的能力
	HostCapabilities map[harnessprotocol.HostCapability]bool
	// ResumePolicy 恢复策略（空则 NEW）
	ResumePolicy harnessprotocol.ResumePolicy
	// Checkpoint 起始快照
	Checkpoint *harnessprotocol.HarnessCheckpoint
	// CheckpointSink 快照持久化通道
	CheckpointSink harnessprotocol.HarnessCheckpointSink
	// Interactions 交互处理器
	Interactions harnessprotocol.HarnessInteractionHandler
	// Metadata 附加元数据
	Metadata harnessprotocol.JsonObject
}

// ManifestProviderConfigOptions ManifestProviderConfig 的参数。
// Python: factory.py:281-287 的 keyword-only 参数
type ManifestProviderConfigOptions struct {
	// Provider provider 名（必填）
	Provider string
	// Config 显式覆盖（永远优先）
	Config map[string]any
	// Language 渲染清单提示词的语言
	Language string
}

// CreateHarnessOptions CreateHarness 的参数。
// Python: factory.py:369-374 的 keyword-only 参数
type CreateHarnessOptions struct {
	// Provider provider 名（必填）
	Provider string
	// Config provider 特定覆盖（合并在清单派生的配置之上）
	Config map[string]any
	// Language 渲染清单提示词的语言
	Language string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// HarnessProviderEntryPointGroup 第三方 provider 包声明的 entry point 组名。
	// STABLE PUBLIC API：改动会破坏所有外部 provider。
	// Python: factory.py:63-65 HARNESS_PROVIDER_ENTRY_POINT_GROUP
	HarnessProviderEntryPointGroup = "openjiuwen.harness_providers"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ProviderNames 内置 provider 名（闭集，永远优先于注册与安装）。
// Python: factory.py:62 PROVIDER_NAMES
var ProviderNames = []string{"native", "native_v2", "claudecode", "codex", "dsh"}

// nativeProviders native 系 provider 名。
// Python: factory.py:68 _NATIVE_PROVIDERS = frozenset({"native", "native_v2"})
//
// 保留硬编码而非按 card 能力推断：与上游逐字对应（规则 9）。
var nativeProviders = map[string]bool{"native": true, "native_v2": true}

// deepAgentOnlySections 只有进程内 DeepAgent 能物化的清单段。
// Python: factory.py:67 _DEEP_AGENT_ONLY_SECTIONS
var deepAgentOnlySections = []string{"tools", "rails", "subagents"}

var (
	// registeredProvidersMu 保护 registeredProviders。
	registeredProvidersMu sync.RWMutex
	// registeredProviders 进程内注册表：name → provider 工厂。
	// Python: factory.py:70 _REGISTERED_PROVIDERS
	registeredProviders = map[string]ProviderFactory{}
)

// ──────────────────────────── 导出函数：注册表 ────────────────────────────

// RegisterHarnessProvider 进程内注册一个第三方 provider。
// Python: factory.py:79-95 register_harness_provider
//
// 适用于未作为已安装包发布的 provider（测试、私有代码）。
// 已发布的包应声明 entry point。
func RegisterHarnessProvider(name string, factory ProviderFactory) error {
	if name == "" || slices.Contains(ProviderNames, name) {
		return fmt.Errorf("cannot register harness provider %q: empty or built-in name", name)
	}
	registeredProvidersMu.Lock()
	defer registeredProvidersMu.Unlock()
	registeredProviders[name] = factory
	return nil
}

// UnregisterHarnessProvider 移除进程内注册；未知名忽略。
// Python: factory.py:98-100 unregister_harness_provider
func UnregisterHarnessProvider(name string) {
	registeredProvidersMu.Lock()
	defer registeredProvidersMu.Unlock()
	delete(registeredProviders, name)
}

// AvailableProviders 返回每个可解析的 provider 名：内置、注册、已安装。
// Python: factory.py:103-109 available_providers
//
// ⚠️ Go 无 entry point 机制，故只含内置 + 进程内注册。
func AvailableProviders() []string {
	names := append([]string(nil), ProviderNames...)
	registeredProvidersMu.RLock()
	defer registeredProvidersMu.RUnlock()
	for name := range registeredProviders {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

// ResolveProvider 返回注册在 provider 名下的 provider SPI 实现。
// Python: factory.py:112-134 resolve_provider
//
// 解析顺序：内置名 → 进程内注册 → 错误。
// 内置名已声明但尚未链接（15.6-15.9 未落地）时返回明确错误，
// 让调用方能区分「名字拼错」与「章节未实现」。
func ResolveProvider(provider string) (harnessprotocol.HarnessProvider, error) {
	registeredProvidersMu.RLock()
	factory, registered := registeredProviders[provider]
	registeredProvidersMu.RUnlock()

	if slices.Contains(ProviderNames, provider) {
		if !registered {
			return nil, fmt.Errorf(
				"harness provider %q is declared but not yet linked", provider)
		}
		return asProvider(provider, factory)
	}
	if registered {
		return asProvider(provider, factory)
	}
	return nil, fmt.Errorf("unknown harness provider %q; expected one of %s",
		provider, strings.Join(AvailableProviders(), ", "))
}

// ──────────────────────────── 导出函数：manifest 转换 ────────────────────────────

// ManifestSourceFromPath 从模板包目录（或其 manifest.json）加载 spec。
// Python: factory.py:234 load_manifest 的路径分支
//
// ⚠️ 与上游的偏差（规则 9 登记）：上游 load_manifest 是
// `AgentTemplateSpec | str | Path` 的 union——spec 直通或路径加载两态。
// Go 侧把 union 在 API 层拆开：本函数只管路径加载；spec 对象由调用方
// 直接传给 CreateHarness / BuildHarnessContext（编译期类型安全，
// 上游的 any 运行时报错形态不复制）。路径形态的调用方先走这里。
func ManifestSourceFromPath(manifestPath string) (hschema.AgentTemplateSpec, error) {
	manifest, err := hresources.FindAgentTemplateManifest(manifestPath)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	return hresources.LoadAgentTemplatePackage(manifest)
}

// ManifestMcpServers 把清单的 MCP 声明翻译成协议 MCP 服务器配置。
// Python: factory.py:237-241 manifest_mcp_servers
func ManifestMcpServers(manifest hschema.AgentTemplateSpec) ([]harnessprotocol.McpServerConfig, error) {
	servers := make([]harnessprotocol.McpServerConfig, 0, len(manifest.Mcps))
	for _, spec := range manifest.Mcps {
		server, err := mcpServerConfig(spec)
		if err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return servers, nil
}

// StdioCommand 解析包相对路径的 stdio 脚本参数。
// Python: factory.py:266-278 _stdio_command
//
// cwd 非空且参数是相对路径、且拼接后确实是文件时解析成绝对路径；
// 否则参数原样保留。
func StdioCommand(spec hschema.McpServerSpec) ([]string, error) {
	var cwd string
	if spec.Cwd != "" {
		expanded, err := expandHome(spec.Cwd)
		if err != nil {
			return nil, err
		}
		cwd = expanded
	}
	args := make([]string, 0, len(spec.Args))
	for _, arg := range spec.Args {
		if cwd != "" && !filepath.IsAbs(arg) {
			located := filepath.Join(cwd, arg)
			if info, statErr := os.Stat(located); statErr == nil && !info.IsDir() {
				absolute, absErr := filepath.Abs(located)
				if absErr != nil {
					return nil, absErr
				}
				args = append(args, absolute)
				continue
			}
		}
		args = append(args, arg)
	}
	return append([]string{spec.Command}, args...), nil
}

// ManifestProviderConfig 为 manifest 派生 provider SPI 配置。
// Python: factory.py:281-315 manifest_provider_config
//
// 显式的 config 键永远优先；清单填充模型与 portable skills
// （对 native 系 provider 是整个模板）。模型通过 provider 的 modelConfig
// 钩子到达其余 provider。
func ManifestProviderConfig(manifest hschema.AgentTemplateSpec,
	opts ManifestProviderConfigOptions) (map[string]any, error) {
	values := make(map[string]any, len(opts.Config)+3)
	for key, value := range opts.Config {
		values[key] = value
	}

	// native 系：整份模板 + 语言（factory.py:297-301）
	if nativeProviders[opts.Provider] {
		if _, exists := values["agent_template"]; !exists {
			values["agent_template"] = manifest.ModelDump()
		}
		if opts.Language != "" {
			if _, exists := values["language"]; !exists {
				values["language"] = opts.Language
			}
		}
		return values, nil
	}

	// 第三方 provider 拒绝 DeepAgent 专属段（factory.py:302-307）
	for _, section := range deepAgentOnlySections {
		if hasDeepAgentSection(manifest, section) {
			return nil, fmt.Errorf(
				"manifest section %q depends on the DeepAgent framework; provider %q cannot materialize it",
				section, opts.Provider)
		}
	}
	// portable skills（factory.py:308-309）
	if len(manifest.Skills) > 0 {
		if _, exists := values["skills"]; !exists {
			skills := make([]map[string]any, 0, len(manifest.Skills))
			for _, skill := range manifest.Skills {
				skills = append(skills, skill.ModelDump())
			}
			values["skills"] = skills
		}
	}
	// 模型映射（factory.py:310-314）
	choice, ok := manifestModelChoice(manifest)
	if !ok {
		return values, nil
	}
	fragment, err := ProviderModelConfig(opts.Provider, choice, nil)
	if err != nil {
		return nil, err
	}
	for key, value := range fragment {
		if _, exists := values[key]; !exists {
			values[key] = value
		}
	}
	return values, nil
}

// ProviderModelConfig 通过 provider 的 modelConfig 把模型选择映射到其配置。
// Python: factory.py:337-366 provider_model_config
func ProviderModelConfig(provider string, choice harnessprotocol.ModelChoice,
	fallback *harnessprotocol.ModelChoice) (map[string]any, error) {
	resolved, err := ResolveProvider(provider)
	if err != nil {
		return nil, err
	}
	modelConfigProvider, ok := resolved.(harnessprotocol.ModelConfigProvider)
	if !ok {
		return nil, fmt.Errorf("harness provider %q does not configure models", provider)
	}
	card := resolved.Card()
	if fallback != nil && !card.Supports(harnessprotocol.HarnessCapabilityAuthFallback) {
		return nil, fmt.Errorf("harness provider %q has no authentication fallback", provider)
	}
	usesEndpoint := choice.Endpoint != nil || (fallback != nil && fallback.Endpoint != nil)
	if usesEndpoint && !card.Supports(harnessprotocol.HarnessCapabilityEndpointModels) {
		return nil, fmt.Errorf("harness provider %q does not take endpoint models", provider)
	}
	fragment, err := modelConfigProvider.ModelConfig(choice, fallback)
	if err != nil {
		return nil, err
	}
	values := make(map[string]any, len(fragment))
	for key, value := range fragment {
		values[key] = value
	}
	return values, nil
}

// CreateHarness 为 manifest 在 provider 上创建一个未启动的协议 harness。
// Python: factory.py:369-396 create_harness
func CreateHarness(manifest hschema.AgentTemplateSpec,
	opts CreateHarnessOptions) (harnessprotocol.HarnessProtocol, error) {
	providerImpl, err := ResolveProvider(opts.Provider)
	if err != nil {
		return nil, err
	}
	template := manifest
	providerConfig, err := ManifestProviderConfig(template, ManifestProviderConfigOptions(opts))
	if err != nil {
		return nil, err
	}
	// FreezeJsonValue 内部用 reflect 递归，能处理配置里混入的
	// []map[string]any / []string 等具体类型，无需预归一化。
	jsonConfig, err := harnessprotocol.FreezeJsonObject(providerConfig)
	if err != nil {
		return nil, err
	}
	return providerImpl.Create(jsonConfig)
}

// BuildHarnessContext 构造携带清单提示词与 MCP 服务器的启动上下文。
// Python: factory.py:399-455 build_harness_context
//
// 对 native / native_v2：模板由 harness 自己加载，只有 extraSystemPrompt
// 进 context；对第三方 provider：persona 提示词段落渲染进 systemPrompt，
// 清单的 MCP 服务器变成 mcpServers。
func BuildHarnessContext(manifest hschema.AgentTemplateSpec,
	opts BuildHarnessContextOptions) (harnessprotocol.HarnessContext, error) {
	template := manifest
	card := template.AgentCard
	var promptParts []string
	var mcpServers []harnessprotocol.McpServerConfig
	var err error

	if !nativeProviders[opts.Provider] {
		language := opts.Language
		if language == "" {
			language = "cn"
		}
		if rendered := hresources.RenderAgentTemplateSystemPrompt(template, language, nil); rendered != "" {
			promptParts = append(promptParts, rendered)
		}
		mcpServers, err = ManifestMcpServers(template)
		if err != nil {
			return harnessprotocol.HarnessContext{}, err
		}
	}
	if opts.ExtraSystemPrompt != "" {
		promptParts = append(promptParts, opts.ExtraSystemPrompt)
	}

	// 能力集：宿主声明的 + 派生的两项（factory.py:436-440）
	capabilities := make(map[harnessprotocol.HostCapability]bool, len(opts.HostCapabilities)+2)
	for capability, declared := range opts.HostCapabilities {
		capabilities[capability] = declared
	}
	if opts.CheckpointSink != nil {
		capabilities[harnessprotocol.HostCapabilityCheckpointSink] = true
	}
	if len(mcpServers) > 0 {
		capabilities[harnessprotocol.HostCapabilityMcpServers] = true
	}

	// 名称与 ID 回退（factory.py:442-443）
	agentName := opts.AgentName
	if agentName == "" {
		agentName = card.Name
	}
	agentID := opts.AgentID
	if agentID == "" {
		agentID = card.ID
	}
	if agentID == "" {
		agentID = card.Name
	}
	resumePolicy := opts.ResumePolicy
	if resumePolicy == "" {
		resumePolicy = harnessprotocol.ResumePolicyNew
	}

	harnessContext := harnessprotocol.HarnessContext{
		AgentName:        agentName,
		AgentID:          agentID,
		HostSessionID:    opts.HostSessionID,
		SystemPrompt:     strings.Join(promptParts, "\n\n"),
		ProtocolVersion:  harnessprotocol.ProtocolVersion,
		HostCapabilities: capabilities,
		ResumePolicy:     resumePolicy,
		Env:              opts.Env,
		Checkpoint:       opts.Checkpoint,
		CheckpointSink:   opts.CheckpointSink,
		McpServers:       mcpServers,
		Interactions:     opts.Interactions,
		Metadata:         opts.Metadata,
	}
	if opts.Cwd != "" {
		cwd := opts.Cwd
		harnessContext.Cwd = &cwd
	}
	// env / metadata 必须非 nil（上游 dict(env or {}) / dict(metadata or {})）
	if harnessContext.Env == nil {
		harnessContext.Env = map[string]string{}
	}
	if harnessContext.Metadata == nil {
		harnessContext.Metadata = harnessprotocol.JsonObject{}
	}
	return harnessContext, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// asProvider 实例化 provider 类（或调用工厂）并校验 SPI。
// Python: factory.py:187-199 _as_provider
func asProvider(name string, factory ProviderFactory) (harnessprotocol.HarnessProvider, error) {
	if factory == nil {
		return nil, fmt.Errorf(
			"harness provider %q is neither a provider nor a provider factory", name)
	}
	provider, err := factory()
	if err != nil {
		return nil, fmt.Errorf("harness provider %q could not be instantiated: %w", name, err)
	}
	if provider == nil {
		return nil, fmt.Errorf(
			"harness provider %q does not implement HarnessProvider (card + create)", name)
	}
	return provider, nil
}

// mcpServerConfig 把一条 MCP 声明转成协议配置。
// Python: factory.py:243-263 _mcp_server_config
//
// 传输映射：stdio → STDIO；sse / streamable_http / 其余 → HTTP
// （上游只分 stdio 与「其余都走 HTTP」两支）。
func mcpServerConfig(spec hschema.McpServerSpec) (harnessprotocol.McpServerConfig, error) {
	name := spec.ServerName
	if name == "" {
		name = spec.ServerID
	}
	if name == "" {
		return harnessprotocol.McpServerConfig{}, errors.New("manifest MCP server requires server_name")
	}
	if spec.Type == "stdio" {
		if spec.Command == "" {
			return harnessprotocol.McpServerConfig{}, fmt.Errorf(
				"manifest stdio MCP server %q requires command", name)
		}
		command, err := StdioCommand(spec)
		if err != nil {
			return harnessprotocol.McpServerConfig{}, err
		}
		return harnessprotocol.McpServerConfig{
			Name:      name,
			Transport: harnessprotocol.McpTransportStdio,
			Command:   command,
			Env:       spec.Env,
		}, nil
	}
	if spec.URL == "" {
		return harnessprotocol.McpServerConfig{}, fmt.Errorf(
			"manifest %s MCP server %q requires url", spec.Type, name)
	}
	url := spec.URL
	return harnessprotocol.McpServerConfig{
		Name:      name,
		Transport: harnessprotocol.McpTransportHTTP,
		URL:       &url,
		Headers:   spec.AuthHeaders,
	}, nil
}

// hasDeepAgentSection 清单是否有某个 DeepAgent 专属段。
// Python: factory.py:303 `if getattr(manifest, section)`
func hasDeepAgentSection(manifest hschema.AgentTemplateSpec, section string) bool {
	switch section {
	case "tools":
		return len(manifest.Tools) > 0
	case "rails":
		return len(manifest.Rails) > 0
	case "subagents":
		return len(manifest.Subagents) > 0
	default:
		return false
	}
}

// manifestModelChoice 返回清单的模型；命名了 API base 时由该端点服务。
// Python: factory.py:318-334 _manifest_model_choice
func manifestModelChoice(manifest hschema.AgentTemplateSpec) (harnessprotocol.ModelChoice, bool) {
	model := manifest.Model
	if model == nil {
		return harnessprotocol.ModelChoice{}, false
	}
	var modelName string
	if model.ModelRequestConfig != nil && model.ModelRequestConfig.ModelName != "" {
		modelName = model.ModelRequestConfig.ModelName
	}
	if model.ModelClientConfig.APIBase == "" {
		choice := harnessprotocol.ModelChoice{}
		if modelName != "" {
			choice.Model = &modelName
		}
		return choice, true
	}
	client := model.ModelClientConfig
	apiBase := client.APIBase
	endpoint := &harnessprotocol.ModelEndpoint{APIBase: &apiBase}
	if client.APIKey != "" {
		apiKey := client.APIKey
		endpoint.APIKey = &apiKey
	}
	if providerName := providerLabel(client.ClientProvider); providerName != "" {
		endpoint.Provider = &providerName
	}
	choice := harnessprotocol.ModelChoice{Endpoint: endpoint}
	if modelName != "" {
		choice.Model = &modelName
	}
	return choice, true
}

// providerLabel 取 client_provider 的字符串值。
// Python: factory.py:332 `str(getattr(raw_provider, "value", raw_provider))`
//
// Python 的枚举取 .value，否则 str() 原值。Go 侧 JSON 解出来通常是 string，
// 但也可能是自定义类型（实现了 String()）。
func providerLabel(raw any) string {
	switch typed := raw.(type) {
	case nil:
		return ""
	case string:
		return typed
	case interface{ String() string }:
		return typed.String()
	default:
		return fmt.Sprintf("%v", raw)
	}
}

// expandHome 把前导 ~ 展开成用户主目录。
// Python: factory.py:268 `Path(spec.cwd).expanduser()`
func expandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// 无法展开时原样返回，与 Python expanduser 的宽松语义一致
		return path, nil
	}
	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
