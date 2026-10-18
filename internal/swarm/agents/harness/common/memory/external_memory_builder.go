package memory

import (
	"os"

	memrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"
	ext "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/external"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// mem0DefaultUser mem0 默认用户标识（品牌映射：Python jiuwenswarm-user）
	mem0DefaultUser = "uapclawswarm-user"
	// mem0DefaultAgent mem0 默认 Agent 标识（品牌映射：Python jiuwenswarm）
	mem0DefaultAgent = "uapclawswarm"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// builtinProviders 内置 provider 名称集合
// Python: _BUILTIN_PROVIDERS = {"openjiuwen", "mem0", "openviking"} (builder.py L26)
var builtinProviders = map[string]struct{}{
	"openjiuwen": {}, "mem0": {}, "openviking": {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildExternalMemoryRail 从配置构建 ExternalMemoryRail，禁用/失败返回 nil。
// 对齐 Python build_external_memory_rail (builder.py L29-78)：
// 任何失败返回 nil——主流程永不阻断。
//
// provider 分发：openjiuwen / mem0 / openviking / 其他（插件，当前不可用）。
// 注意：Go 侧不包含 agentarts 分支（严格对齐 jiuwenswarm builder 四路分发，
// agentarts provider 保留在 agentcore 但不经此 builder 接线）。
//
// workspaceDir 为签名占位（Python builder 签名带它但函数体未用，
// LTM 目录用 get_user_workspace_dir 即 workspace.WorkspaceDir）。
//
// sessionID 传 ""：空串经 provider 侧 if po.SessionID != "" 忽略，
// 落 provider 自身默认 ltm.DefaultValue "__default__"——与 Python
// ExternalMemoryRail 不传 session_id 参数（默认 "__default__"）等价。
func BuildExternalMemoryRail(cfg map[string]any, workspaceDir string) agentinterfaces.AgentRail {
	_ = workspaceDir // 对齐 Python：签名保留但函数体未用

	extCfg := GetExternalMemoryConfig(cfg)
	if extCfg.Provider == "" {
		return nil
	}

	var provider ext.MemoryProvider
	if _, ok := builtinProviders[extCfg.Provider]; ok {
		switch extCfg.Provider {
		case "openjiuwen":
			provider = buildOpenJiuwenProvider(extCfg)
		case "mem0":
			provider = buildMem0Provider(extCfg)
		case "openviking":
			provider = buildOpenVikingProvider(extCfg)
		}
	} else {
		provider = loadPluginProvider(extCfg.Provider, extCfg.AllowedPlugins)
	}
	if provider == nil {
		return nil
	}

	rail := memrail.NewExternalMemoryRail(provider, extCfg.UserID, extCfg.ScopeID, "")
	logger.Info(logComponent).
		Str("event_type", "external_memory_builder").
		Str("provider", extCfg.Provider).
		Msg("ExternalMemoryRail built")
	return rail
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildOpenJiuwenProvider 构建 openjiuwen LTM provider。
// 对齐 Python _build_openjiuwen_provider (L81-86)。
func buildOpenJiuwenProvider(extCfg ExternalMemoryConfig) ext.MemoryProvider {
	providerConfig, err := BuildOpenJiuwenProviderConfig(extCfg, nil)
	if err != nil {
		// Python: mkdir 失败抛异常 → builder 外层 except → None
		logger.Warn(logComponent).
			Str("event_type", "external_memory_builder").
			Str("provider", "openjiuwen").
			Err(err).
			Msg("build provider failed")
		return nil
	}
	return ext.NewOpenJiuwenProvider(providerConfig)
}

// buildMem0Provider 构建 Mem0 SaaS provider。
// 对齐 Python _build_mem0_provider (L89-107)：
// config 段优先，MEM0_* env 回退，默认值经品牌映射 uapclawswarm。
func buildMem0Provider(extCfg ExternalMemoryConfig) ext.MemoryProvider {
	mem0Cfg := extCfg.Mem0
	strOr := func(key string) string {
		if s, ok := mem0Cfg[key].(string); ok && s != "" {
			return s
		}
		return ""
	}
	// Python L93: api_key = mem0_cfg.get("api_key") or os.environ.get("MEM0_API_KEY", "")
	apiKey := strOrEnv(strOr("api_key"), "MEM0_API_KEY")
	// Python L94: user_id = ... or os.environ.get("MEM0_USER_ID", "jiuwenswarm-user")
	userID := strOrEnv(strOr("user_id"), "MEM0_USER_ID")
	if userID == "" {
		userID = mem0DefaultUser
	}
	// Python L95: agent_id = ... or os.environ.get("MEM0_AGENT_ID", "jiuwenswarm")
	agentID := strOrEnv(strOr("agent_id"), "MEM0_AGENT_ID")
	if agentID == "" {
		agentID = mem0DefaultAgent
	}
	// Python L96: rerank = bool(mem0_cfg.get("rerank", True))
	rerank := true
	if b, ok := mem0Cfg["rerank"].(bool); ok {
		rerank = b
	}

	provider := ext.NewMem0Provider(apiKey, userID, agentID, rerank)
	if !provider.IsAvailable() {
		// Python L104-106: logger.warning("[ExternalMemoryBuilder] Mem0 unavailable (no API key)")
		logger.Warn(logComponent).
			Str("event_type", "external_memory_builder").
			Msg("Mem0 unavailable (no API key)")
		return nil
	}
	return provider
}

// buildOpenVikingProvider 构建 OpenViking provider。
// 对齐 Python _build_openviking_provider (L110-130)：
// endpoint/api_key/user 的 env 回退由 provider 内部完成（NewOpenVikingProvider
// 已实现 OPENVIKING_* 读取）；account 的 env 回退在 builder 层完成，
// 且 env 缺省时落 "root"（Python L118 基线；provider 内部默认 "default"
// 是 openjiuwen 基线，builder 显式传值覆盖之）。
// agent 不传（空串落 provider 内部默认 hermes）——对齐 Python builder 不传 agent。
func buildOpenVikingProvider(extCfg ExternalMemoryConfig) ext.MemoryProvider {
	vkCfg := extCfg.OpenViking
	strOr := func(key string) string {
		if s, ok := vkCfg[key].(string); ok && s != "" {
			return s
		}
		return ""
	}
	endpoint := strOr("endpoint")
	apiKey := strOr("api_key")
	account := strOr("account")
	user := strOr("user")
	// Python L118: account = vk_cfg.get("account") or os.environ.get("OPENVIKING_ACCOUNT", "root")
	if account == "" {
		if envVal := os.Getenv("OPENVIKING_ACCOUNT"); envVal != "" {
			account = envVal
		} else {
			account = "root"
		}
	}

	// Python builder 不传 agent 参数（provider 默认 hermes）→ 传空串
	provider := ext.NewOpenVikingProvider(endpoint, apiKey, account, user, "")
	if !provider.IsAvailable() {
		// Python L127-129: logger.warning("[ExternalMemoryBuilder] OpenViking unavailable (no endpoint)")
		logger.Warn(logComponent).
			Str("event_type", "external_memory_builder").
			Msg("OpenViking unavailable (no endpoint)")
		return nil
	}
	return provider
}

// loadPluginProvider 加载用户安装的记忆插件。
// 对齐 Python _load_plugin_provider (L133-142)：plugin_discovery 模块
// 在 jiuwenswarm 中不存在 → ImportError → 恒 warn + None。
// Go 侧同样无插件机制，恒返回 nil。
func loadPluginProvider(name string, allowed []string) ext.MemoryProvider {
	logger.Warn(logComponent).
		Str("event_type", "external_memory_builder").
		Str("plugin", name).
		Int("allowed_count", len(allowed)).
		Msg("plugin requested but plugin_discovery not yet available")
	return nil
}
