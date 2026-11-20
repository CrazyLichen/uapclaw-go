package manifest

// builtin_elements.go —— core 内置元素常量段 + 构造注册段（1:1 对齐上游
// manifest/builtin_elements.py 单文件结构：常量 :64-81 + 构造段）。
//
// Python: openjiuwen/harness/manifest/builtin_elements.py（533 行）——
// @builtin_element 装饰器把各默认 rail/tool/subagent 构造器灌进 manifest
// catalog，register_from_catalog 按 ElementKind 分派到三张注册表。
// Go 无 catalog（偏差 F6/D-9），本文件显式注册；经
// schema.SetBuiltinElementsRegisteredHook 在 init 挂进 RailSpec.Build /
// BuiltinToolSpec.Build / SubAgentSpec.Build 的「步骤 1：确保内置元素
// 已注册」钩子（schema 包不能反向 import 本包，钩子注入是既定缝）。
//
// 基座缺失的元素（lsp/observability）注册 nil 工厂并登记 ⤴️15.20——
// 对齐 Python「provider 存在但返回 None 跳过」的门控语义。
//
// 目录豁免注记：2026-12-04 核实 rails/tools/subagents 均不 import manifest，
// manifest→rails 无环——构造段迁回本包，恢复与上游单文件 1:1
// （原 harness/core_elements.go 已删除，规则 8）。

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	clirails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/subagent"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/subagents"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/multimodal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/web_tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/worktree"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// coreElementsRegistered 幂等守卫（对齐 Python ensure_builtin_elements_registered）。
var coreElementsRegistered bool

// ──────────────────────────── 导出函数 ────────────────────────────

// init 把内置元素注册钩子挂进 schema（RailSpec/BuiltinToolSpec/SubAgentSpec
// 的 Build 步骤 1 会调一次；本包可 import schema 而非反向）。
func init() {
	hschema.SetBuiltinElementsRegisteredHook(EnsureCoreElementsRegistered)
}

// EnsureCoreElementsRegistered 幂等注册 core.* 内置元素 provider。
// Python: ensure_builtin_elements_registered (manifest/builtin_elements.py)。
func EnsureCoreElementsRegistered() {
	if coreElementsRegistered {
		return
	}
	registerCoreRailProviders()
	registerCoreToolProviders()
	registerCoreSubagentProviders()
	coreElementsRegistered = true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// registerCoreRailProviders 注册 core.* rail provider。
// Python: builtin_elements.py 的 rail 段（class rails 与 factory rails）。
func registerCoreRailProviders() {
	// core.security → SafetyPromptRail（始终添加的默认安全轨）
	// Python: SecurityRail/SafetyPromptRail
	hschema.RegisterRailProvider("core.security", func(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
		return security.NewSafetyPromptRail(), nil
	})
	// core.task_planning → TaskPlanningRail
	// Python: builder=TaskPlanningRail（:405，class builder 无参构造）
	hschema.RegisterRailProvider("core.task_planning", func(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
		return rails.NewTaskPlanningRail(), nil
	})
	// core.sys_operation → SysOperationRail
	// Python: SysOperationRail
	hschema.RegisterRailProvider("core.sys_operation", func(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
		return rails.NewSysOperationRail(), nil
	})
	// core.ask_user → AskUserRail（HITL 用户交互）
	// Python: AskUserRail
	hschema.RegisterRailProvider("core.ask_user", func(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
		return interrupt.NewAskUserRail(), nil
	})
	// core.confirm_interrupt → ConfirmInterruptRail（工具名列表从 params 取）
	// Python: ConfirmInterruptRail
	hschema.RegisterRailProvider("core.confirm_interrupt", func(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
		var names []string
		if raw, ok := params["tool_names"].([]any); ok {
			for _, item := range raw {
				if s, ok := item.(string); ok {
					names = append(names, s)
				}
			}
		}
		if len(names) > 0 {
			return interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames(names...)), nil
		}
		return interrupt.NewConfirmInterruptRail(), nil
	})
	// core.heartbeat → HeartbeatRail
	// Python: HeartbeatRail
	hschema.RegisterRailProvider("core.heartbeat", func(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
		return rails.NewHeartbeatRail(), nil
	})
	// core.skill_use → SkillUseRail
	// Python: _build_skill_use_rail (:84-114)：params 缺 skills_dir 时从
	// workspace skills 节点 + 默认 CLI 目录派生；SkillUseRail 不接受 language。
	hschema.RegisterRailProvider("core.skill_use", func(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
		return skills.NewSkillUseRail(skillUseDirs(params, ctx)), nil
	})
	// core.subagent → SubagentRail
	// Python: SubagentRail
	hschema.RegisterRailProvider("core.subagent", func(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
		return subagent.NewSubagentRail(), nil
	})
	// core.worktree → WorktreeRail
	// Python: _build_worktree_rail (:155-165)：WorktreeConfig(**params)
	hschema.RegisterRailProvider("core.worktree", func(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
		cfg := worktree.WorktreeConfig{Enabled: boolParamOr(params, "enabled", false)}
		if v, ok := params["base_dir"].(string); ok {
			cfg.BaseDir = v
		}
		for key, field := range map[string]*[]string{
			"sparse_paths":        &cfg.SparsePaths,
			"symlink_directories": &cfg.SymlinkDirectories,
		} {
			if v, ok := params[key].([]any); ok {
				for _, item := range v {
					if sv, ok := item.(string); ok {
						*field = append(*field, sv)
					}
				}
			}
		}
		return worktree.NewWorktreeRail(worktree.WithWorktreeRailConfig(cfg)), nil
	})
	// core.token_tracking / core.tool_tracking → 15.5 tracking rails
	// Python: builder=TokenTrackingRail / ToolTrackingRail（:429/:435，class builder）
	hschema.RegisterRailProvider("core.token_tracking", func(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
		return clirails.NewTokenTrackingRail(), nil
	})
	hschema.RegisterRailProvider("core.tool_tracking", func(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
		return clirails.NewToolTrackingRail(), nil
	})
	// core.lsp / core.observability：基座 Go 未实施 ⤴️15.20
	// （Python: _build_lsp_rail 从 context.project_dir 根；_build_observability_rail
	// 双道门 extra 安装 + 初始化，任一不满足 → None）
	for _, name := range []string{"core.lsp", "core.observability"} {
		name := name
		hschema.RegisterRailProvider(name, func(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
			return nil, nil
		})
	}
}

// registerCoreToolProviders 注册 core.* tool provider。
// Python: builtin_elements.py 的 tool 段（各 @tool 单例的 catalog 声明）。
func registerCoreToolProviders() {
	// core.web_search → web_free_search
	// Python: _build_web_free_search (:212-224)——WebToolInput.resolve 从
	// context 取 language + member_card_id（agent_id 命名空间化）
	hschema.RegisterToolProvider("core.web_search", func(params map[string]any, ctx hschema.BuildContextInterface) ([]any, error) {
		return []any{web_tools.NewWebFreeSearchTool(webLanguage(ctx), webAgentID(ctx))}, nil
	})
	// core.web_fetch → web_fetch_webpage
	// Python: _build_web_fetch (:226-238)
	hschema.RegisterToolProvider("core.web_fetch", func(params map[string]any, ctx hschema.BuildContextInterface) ([]any, error) {
		return []any{web_tools.NewWebFetchWebpageTool(webLanguage(ctx), webAgentID(ctx))}, nil
	})
	// core.web_paid_search → web_paid_search（付费 key 未配置 → 空表）
	// Python: _build_web_paid_search (:240-254)：is_paid_search_enabled() 门控
	hschema.RegisterToolProvider("core.web_paid_search", func(params map[string]any, ctx hschema.BuildContextInterface) ([]any, error) {
		if !web_tools.IsPaidSearchEnabled() {
			return nil, nil
		}
		return []any{web_tools.NewWebPaidSearchTool(webLanguage(ctx), webAgentID(ctx))}, nil
	})
	// core.vision → 视觉工具组
	// Python: _build_vision_tool_group (:273-296)：params.vision_model_config
	// → VisionModelConfig → is_vision_model_config_complete 完整性门控
	// （不完整 → 空表）→ create_vision_tools(language, config, agent_id)
	hschema.RegisterToolProvider("core.vision", func(params map[string]any, ctx hschema.BuildContextInterface) ([]any, error) {
		cfg := visionModelConfigFromParams(params)
		if cfg == nil || !isVisionModelConfigComplete(cfg) {
			return nil, nil
		}
		return toolsToAny(multimodal.CreateVisionTools(cfg, webLanguage(ctx), webAgentID(ctx))), nil
	})
	// core.audio → 音频工具组
	// Python: _build_audio_tool_group (:319-366)：dedicated=false 或无
	// audio_model_config → 仅 audio_metadata（无需大模型）；齐全 → 全组
	hschema.RegisterToolProvider("core.audio", func(params map[string]any, ctx hschema.BuildContextInterface) ([]any, error) {
		dedicated, _ := params["dedicated"].(bool)
		audioCfg := audioModelConfigFromParams(params)
		language, agentID := webLanguage(ctx), webAgentID(ctx)
		if !dedicated || audioCfg == nil {
			// Python: AudioMetadataTool(language, AudioModelConfig(), agent_id)
			// ——Go 基座 NewAudioMetadataTool 需要 client 形参；metadata 不调
			// 模型，nil client 即可（偏差注记：基座签名差异）。
			return toolsToAny([]tool.Tool{multimodal.NewAudioMetadataTool(&hschema.AudioModelConfig{}, language, agentID)}), nil
		}
		return toolsToAny(multimodal.CreateAudioTools(audioCfg, language, agentID)), nil
	})
}

// registerCoreSubagentProviders 注册 core.* subagent provider。
// Python: builtin_elements.py 的 subagent 段（explore/plan 工厂声明）。
func registerCoreSubagentProviders() {
	// core.explore_agent → explore 子代理
	hschema.RegisterSubagentProvider("core.explore_agent", func(factoryKwargs map[string]any, ctx hschema.BuildContextInterface) (*hschema.SubAgentConfig, error) {
		model := parentModelFromExtras(ctx)
		return subagents.BuildExploreAgentConfig(model, paramsToSubagentCreateParams(factoryKwargs)), nil
	})
	// core.plan_agent → plan 子代理
	hschema.RegisterSubagentProvider("core.plan_agent", func(factoryKwargs map[string]any, ctx hschema.BuildContextInterface) (*hschema.SubAgentConfig, error) {
		model := parentModelFromExtras(ctx)
		return subagents.BuildPlanAgentConfig(model, paramsToSubagentCreateParams(factoryKwargs)), nil
	})
}

// ──────────────────────────── 非导出函数（助手） ────────────────────────────

// paramStringOr 读字符串参数（缺省回退）。
func paramStringOr(params map[string]any, key, fallback string) string {
	if v, ok := params[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

// paramStringSliceOr 读字符串切片参数。
func paramStringSliceOr(params map[string]any, key string) []string {
	raw, ok := params[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// webLanguage WebToolInput.language：context language，缺省 cn（:198-199）。
func webLanguage(ctx hschema.BuildContextInterface) string {
	if ctx != nil {
		if v := ctx.GetLanguage(); v != "" {
			return v
		}
	}
	return "cn"
}

// webAgentID WebToolInput.agent_id：context member_card_id（工具 id 命名空间）。
func webAgentID(ctx hschema.BuildContextInterface) string {
	if ctx != nil {
		return ctx.GetMemberCardID()
	}
	return ""
}

// isVisionModelConfigComplete 视觉模型配置完整性门控。
// Python: is_vision_model_config_complete (schema/config.py:97-108)：
// api_key/base_url/model 三者非空。
func isVisionModelConfigComplete(cfg *hschema.VisionModelConfig) bool {
	return cfg != nil &&
		strings.TrimSpace(cfg.APIKey) != "" &&
		strings.TrimSpace(cfg.BaseURL) != "" &&
		strings.TrimSpace(cfg.Model) != ""
}

// visionModelConfigFromParams 从烘焙 kwargs 构造 VisionModelConfig。
// Python: VisionModelConfig(**inp.vision_model_config)（:287）
func visionModelConfigFromParams(params map[string]any) *hschema.VisionModelConfig {
	raw, _ := params["vision_model_config"].(map[string]any)
	if len(raw) == 0 {
		return nil
	}
	cfg := &hschema.VisionModelConfig{
		APIKey:  strParam(raw, "api_key"),
		BaseURL: strParam(raw, "base_url"),
		Model:   strParam(raw, "model"),
	}
	switch typed := raw["max_retries"].(type) {
	case int:
		cfg.MaxRetries = typed
	case float64:
		cfg.MaxRetries = int(typed)
	}
	return cfg
}

// audioModelConfigFromParams 从烘焙 kwargs 构造 AudioModelConfig。
// Python: AudioModelConfig(**inp.audio_model_config)（:357）
func audioModelConfigFromParams(params map[string]any) *hschema.AudioModelConfig {
	raw, _ := params["audio_model_config"].(map[string]any)
	if len(raw) == 0 {
		return nil
	}
	cfg := &hschema.AudioModelConfig{
		APIKey:             strParam(raw, "api_key"),
		BaseURL:            strParam(raw, "base_url"),
		TranscriptionModel: strParam(raw, "transcription_model"),
		QAModel:            strParam(raw, "question_answering_model"),
	}
	switch typed := raw["max_retries"].(type) {
	case int:
		cfg.MaxRetries = typed
	case float64:
		cfg.MaxRetries = int(typed)
	}
	return cfg
}

// boolParamOr 布尔参数缺省（builtin_elements 局部助手）。
func boolParamOr(params map[string]any, key string, fallback bool) bool {
	if v, ok := params[key].(bool); ok {
		return v
	}
	return fallback
}

// strParam 读字符串键。
func strParam(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// skillUseDirs SkillUseRail 目录派生。
// Python: _build_skill_use_rail (:89-103)：params["skills_dir"] 优先；缺省
// 从 workspace skills 节点 + 默认 CLI 目录派生。
func skillUseDirs(params map[string]any, ctx hschema.BuildContextInterface) []string {
	if raw := paramStringSliceOr(params, "skills_dir"); len(raw) > 0 {
		return raw
	}
	dirs := make([]string, 0, 3)
	if ctx != nil {
		if ws := ctx.GetWorkspace(); ws != nil {
			if p := ws.GetNodePath(workspace.WorkspaceNodeSkills); p != nil && *p != "" {
				dirs = append(dirs, *p)
			}
		}
	}
	// Python 字面目录（上游原文，不本地化）
	for _, d := range []string{"~/.openjiuwen/workspace/skills", "~/.claude/skills"} {
		dirs = append(dirs, pathutil.ExpandHome(d))
	}
	return dirs
}

// toolsToAnyWith 工具组转 provider 返回形态（vision/audio 组带 language/agentID 形参）。
func toolsToAny(tools []tool.Tool) []any {
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, t)
	}
	return out
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TaskPlanning 任务规划元素。Python: builtin_elements.py:64
	TaskPlanning = "core.task_planning"
	// SkillUse 技能使用元素。Python: builtin_elements.py:65
	SkillUse = "core.skill_use"
	// Subagent 子代理元素。Python: builtin_elements.py:66
	Subagent = "core.subagent"
	// SysOperation 系统操作元素。Python: builtin_elements.py:67
	SysOperation = "core.sys_operation"
	// Security 安全元素。Python: builtin_elements.py:68
	Security = "core.security"
	// Heartbeat 心跳元素。Python: builtin_elements.py:69
	Heartbeat = "core.heartbeat"
	// Worktree 工作树元素。Python: builtin_elements.py:70
	Worktree = "core.worktree"
	// LSP 语言服务器元素。Python: builtin_elements.py:71
	LSP = "core.lsp"
	// TokenTracking token 追踪元素。Python: builtin_elements.py:72
	TokenTracking = "core.token_tracking"
	// ToolTracking 工具追踪元素。Python: builtin_elements.py:73
	ToolTracking = "core.tool_tracking"
	// AskUser 询问用户元素。Python: builtin_elements.py:74
	AskUser = "core.ask_user"
	// ConfirmInterrupt 确认中断元素。Python: builtin_elements.py:75
	ConfirmInterrupt = "core.confirm_interrupt"
	// WebSearch 网页搜索元素。Python: builtin_elements.py:76
	WebSearch = "core.web_search"
	// WebFetch 网页抓取元素。Python: builtin_elements.py:77
	WebFetch = "core.web_fetch"
	// WebPaidSearch 付费网页搜索元素。Python: builtin_elements.py:78
	WebPaidSearch = "core.web_paid_search"
	// Vision 视觉元素。Python: builtin_elements.py:79
	Vision = "core.vision"
	// Audio 音频元素。Python: builtin_elements.py:80
	Audio = "core.audio"
	// Observability 可观测元素。Python: builtin_elements.py:81
	Observability = "core.observability"
)

// parentModelFromExtras 从构建上下文取父模型（与 resolve_spec_parts 写入键同源）。
// Python: ctx.extras["_parent_model"]。
func parentModelFromExtras(ctx hschema.BuildContextInterface) *llm.Model {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.GetExtras()["_parent_model"].(*llm.Model); ok {
		return v
	}
	return nil
}

// paramsToSubagentCreateParams factory_kwargs 摊成基座构造参数。
func paramsToSubagentCreateParams(kwargs map[string]any) *hschema.SubagentCreateParams {
	params := &hschema.SubagentCreateParams{}
	if v, ok := kwargs["language"].(string); ok {
		params.Language = v
	}
	switch typed := kwargs["max_iterations"].(type) {
	case int:
		params.MaxIterations = typed
	case float64:
		params.MaxIterations = int(typed)
	}
	if v, ok := kwargs["workspace_root"].(string); ok && v != "" {
		params.Workspace = &workspace.Workspace{RootPath: v}
	}
	return params
}
