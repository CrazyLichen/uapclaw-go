package harness

// core_elements.go —— core 内置元素的 provider 注册段（前置件 A5）。
//
// Python: openjiuwen/harness/manifest/builtin_elements.py 的构造注册段 +
// ensure_builtin_elements_registered——@builtin_element 装饰器把各默认
// rail/tool/subagent 构造器灌进 manifest catalog，register_from_catalog
// 按 ElementKind 分派到三张注册表。Go 无 catalog（偏差 F6/D-9），本文件
// 显式注册；经 schema.SetBuiltinElementsRegisteredHook 在 init 挂进
// RailSpec.Build / BuiltinToolSpec.Build / SubAgentSpec.Build 的
// 「步骤 1：确保内置元素已注册」钩子（schema 包不能反向 import 本包，
// 钩子注入是既定缝）。
//
// 基座缺失的元素（token_tracking/tool_tracking/observability/lsp/
// worktree 按需）注册 nil 工厂并登记 ⤴️15.20——对齐 Python「provider
// 存在但返回 None 跳过」的门控语义。

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/subagent"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/subagents"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/multimodal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/web_tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/worktree"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// coreElementsRegistered 幂等守卫（对齐 Python ensure_builtin_elements_registered）。
	coreElementsRegistered bool
)

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
	// core.task_planning → TaskPlanningRail（enable_task_planning 门控在
	// factory 默认集；provider 按声明物化，语言从 params 取，缺省 cn）
	// Python: TaskPlanningRail
	hschema.RegisterRailProvider("core.task_planning", func(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
		language := paramStringOr(params, "language", "cn")
		return rails.NewTaskPlanningRail(rails.WithLanguage(language)), nil
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
	// core.skill_use → SkillUseRail（skills 目录从 params 取；nil = 仅发现模式）
	// Python: SkillUseRail
	hschema.RegisterRailProvider("core.skill_use", func(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
		return skills.NewSkillUseRail(paramStringSliceOr(params, "skills_dirs")), nil
	})
	// core.subagent → SubagentRail
	// Python: SubagentRail
	hschema.RegisterRailProvider("core.subagent", func(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
		return subagent.NewSubagentRail(), nil
	})
	// core.worktree → WorktreeRail
	// Python: WorktreeRail
	hschema.RegisterRailProvider("core.worktree", func(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
		return worktree.NewWorktreeRail(), nil
	})
	// core.lsp / core.token_tracking / core.tool_tracking / core.observability：
	// 基座 Go 未实施 ⤴️15.20（LSP 章 / 15.5 tracking rail 接线 / 观测）。
	for _, name := range []string{"core.lsp", "core.token_tracking", "core.tool_tracking", "core.observability"} {
		name := name
		hschema.RegisterRailProvider(name, func(params map[string]any, ctx hschema.BuildContextInterface) (any, error) {
			return nil, nil
		})
	}
}

// registerCoreToolProviders 注册 core.* tool provider。
// Python: builtin_elements.py 的 tool 段（各 @tool 单例的 catalog 声明）。
func registerCoreToolProviders() {
	// core.web_search → web_free_search（语言/agent id 从 params 取）
	// Python: web_search（free search 通道）
	hschema.RegisterToolProvider("core.web_search", func(params map[string]any, ctx hschema.BuildContextInterface) ([]any, error) {
		language, agentID := paramsLanguageAgentID(params)
		return []any{web_tools.NewWebFreeSearchTool(language, agentID)}, nil
	})
	// core.web_fetch → web_fetch_webpage
	// Python: web_fetch
	hschema.RegisterToolProvider("core.web_fetch", func(params map[string]any, ctx hschema.BuildContextInterface) ([]any, error) {
		language, agentID := paramsLanguageAgentID(params)
		return []any{web_tools.NewWebFetchWebpageTool(language, agentID)}, nil
	})
	// core.web_paid_search → web_paid_search
	// Python: paid_search
	hschema.RegisterToolProvider("core.web_paid_search", func(params map[string]any, ctx hschema.BuildContextInterface) ([]any, error) {
		language, agentID := paramsLanguageAgentID(params)
		return []any{web_tools.NewWebPaidSearchTool(language, agentID)}, nil
	})
	// core.vision → 视觉工具组（模型配置从 params.vision_model_config 取，
	// 由 config_specs toolParams 烘焙；未配置 → nil 跳过）
	// Python: vision 工具组（create_vision_tools(language, vision_model_config, agent_id)）
	hschema.RegisterToolProvider("core.vision", func(params map[string]any, ctx hschema.BuildContextInterface) ([]any, error) {
		return visionAudioTools(params, "vision")
	})
	// core.audio → 音频工具组（同上，audio_model_config）
	// Python: audio 工具组
	hschema.RegisterToolProvider("core.audio", func(params map[string]any, ctx hschema.BuildContextInterface) ([]any, error) {
		return visionAudioTools(params, "audio")
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

// paramsLanguageAgentID 提取 provider 通用 language/agent_id 参数。
func paramsLanguageAgentID(params map[string]any) (string, string) {
	return paramStringOr(params, "language", "cn"), paramStringOr(params, "agent_id", "")
}

// visionAudioTools 从烘焙 kwargs 构建视觉/音频工具组。
// 客户端构建镜像 deep_adapter.resolveModelClientFromConfig（Python
// init_model(provider="OpenAI", ...)）；配置缺失 → nil 跳过。
func visionAudioTools(params map[string]any, kind string) ([]any, error) {
	cfgKey := kind + "_model_config"
	raw, _ := params[cfgKey].(map[string]any)
	if len(raw) == 0 {
		return nil, nil
	}
	getStr := func(key string) string {
		v, _ := raw[key].(string)
		return v
	}
	getInt := func(key string) int {
		switch typed := raw[key].(type) {
		case int:
			return typed
		case float64:
			return int(typed)
		default:
			return 3
		}
	}
	modelName := getStr("model")
	if kind == "audio" {
		modelName = getStr("question_answering_model")
	}
	m, err := llm.InitModel("OpenAI", modelName, getStr("api_key"), getStr("base_url"),
		llm.WithInitMaxRetries(getInt("max_retries")),
		llm.WithInitVerifySSL(false),
	)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Str("kind", kind).Msg("vision/audio model client build failed")
		return nil, nil
	}
	language := paramStringOr(params, "language", "cn")
	agentID := paramStringOr(params, "agent_id", "")
	if kind == "vision" {
		visionCfg := &hschema.VisionModelConfig{
			APIKey: getStr("api_key"), BaseURL: getStr("base_url"),
			Model: getStr("model"), MaxRetries: getInt("max_retries"),
		}
		return toolsToAny(multimodal.CreateVisionTools(m.GetClient(), visionCfg, language, agentID)), nil
	}
	audioCfg := &hschema.AudioModelConfig{
		APIKey: getStr("api_key"), BaseURL: getStr("base_url"),
		QAModel: modelName, MaxRetries: getInt("max_retries"),
	}
	return toolsToAny(multimodal.CreateAudioTools(m.GetClient(), audioCfg, language, agentID)), nil
}

// toolsToAnyWith 工具组转 provider 返回形态（vision/audio 组带 language/agentID 形参）。
func toolsToAny(tools []tool.Tool) []any {
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, t)
	}
	return out
}

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

