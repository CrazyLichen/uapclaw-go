package providers

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	buildctx "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/buildctx"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	subagents "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/subagents"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// code_subagents.go —— code profile 子代理工厂（四个，注册名=FactoryName 消费点）。
//
// Python: jiuwenswarm/agents/swarm/providers/code_subagents.py（330 行）。
// 父模型从 ctx.extras["_parent_model"] 读（resolve_spec_parts 步骤 3 写入，
// parentModelExtrasKey 同键）。cua 基座随 15.16f A4 落地；statusline 基座、
// browser_key 形参、browser decision/swarm settings 应用段基座缺口 ⤴️15.20。

// ──────────────────────────── 常量 ────────────────────────────

const (
	// parentModelExtrasKey 父模型在 extras 的键（与 resolve_spec_parts 写入键同值：
	// Python "_parent_model"）。Go 侧 harness 包内常量未导出，此处按同字面值声明
	// （双真源风险由同字面值 + 注册段测试对拍兜住）。
	parentModelExtrasKey    = "_parent_model"
	constCodeAgent          = "swarm.code_agent"
	constStatuslineAgent    = "swarm.statusline_setup_agent"
	constSwarmBrowserAgent  = "swarm.browser_agent"
	constSwarmCuaAgent      = "swarm.cua_agent"
	constDefaultBrowserIter = 25
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// parentModelOf 从构建上下文 extras 取父模型。
// Python: ctx.extras.get(_PARENT_MODEL_EXTRAS_KEY)（各工厂首行守卫）。
func parentModelOf(view *buildctx.SwarmBuildContext) *llm.Model {
	if v, ok := view.GetExtras()[parentModelExtrasKey].(*llm.Model); ok {
		return v
	}
	return nil
}

// subagentProviderAdapter 把具体子代理工厂适配成 schema.SubagentProvider 签名。
func subagentProviderAdapter(
	build func(factoryKwargs map[string]any, view *buildctx.SwarmBuildContext) (*hschema.SubAgentConfig, error),
) hschema.SubagentProvider {
	return func(factoryKwargs map[string]any, ctx hschema.BuildContextInterface) (*hschema.SubAgentConfig, error) {
		view := SwarmContextOf(ctx)
		if view == nil {
			return nil, nil
		}
		return build(factoryKwargs, view)
	}
}

// buildCodeAgent code 执行子代理（复用主 agent 的 CodingMemoryRail）。
//
// build_code_agent_config 需要模型；上下文无父模型时跳过（None）。传入 rails
// 会覆盖默认（SysOperationRail），因此必须与共享 CodingMemoryRail 一起显式带上。
// Python: build_code_agent (code_subagents.py:108-143)
func buildCodeAgent(factoryKwargs map[string]any, view *buildctx.SwarmBuildContext) (*hschema.SubAgentConfig, error) {
	model := parentModelOf(view)
	if model == nil {
		logger.Warn(logger.ComponentAgentServer).
			Msg("[swarm.code_agent] skipped: no parent model on build context")
		return nil, nil
	}
	// Python: rails = [SysOperationRail(), coding_memory_rail]（extras 命中时）
	// ——CodingMemoryRail 由 build_code_coding_memory 发布（基座 ⤴️15.20），
	// extras 未命中时 rails=nil（子代理用默认 rail 集）。
	maxIterations := intParamOr(factoryKwargs, "max_iterations", 0)
	spec := subagents.BuildCodeAgentConfig(model, &hschema.SubagentCreateParams{
		Workspace:     nil, // workspace 以字符串传基座：BuildCodeAgentConfig 经 params.Workspace
		Language:      CodeRuntimeLanguage(view),
		MaxIterations: maxIterations,
	})
	// Python: spec.factory_kwargs = {"auto_create_workspace": False}
	if spec.FactoryKwargs == nil {
		spec.FactoryKwargs = map[string]any{}
	}
	spec.FactoryKwargs["auto_create_workspace"] = false
	return spec, nil
}

// buildStatuslineSetupAgent TUI 状态行安装子代理。
// 基座 BuildStatuslineSetupAgentConfig Go 未实施 ⤴️15.20。
// Python: build_statusline_setup_agent (code_subagents.py:145-163)
func buildStatuslineSetupAgent(factoryKwargs map[string]any, view *buildctx.SwarmBuildContext) (*hschema.SubAgentConfig, error) {
	model := parentModelOf(view)
	if model == nil {
		logger.Warn(logger.ComponentAgentServer).
			Msg("[swarm.statusline_setup_agent] skipped: no parent model")
		return nil, nil
	}
	// Python: build_statusline_setup_agent_config(model, workspace=...,
	//   sys_operation=parent_sys_operation(ctx), language=..., max_iterations=...)
	// ——基座 ⤴️15.20。
	return nil, nil
}

// browserKeyOf 每成员 browser_key。
//
// role 只会取 leader/teammate（teammate 之间碰撞）；member_name 每成员唯一
// （leader/模板 spec 只有 role，缺 member_name 时回落 role）。session id 折入，
// 使不同并发会话同名成员不撞同一个浏览器。agent-core 会把 key 清洗为 id 安全
// 字符；空 key 保持旧版共享浏览器行为。
// Python: _browser_key (code_subagents.py:200-222)
func browserKeyOf(sessionID, memberName, role string) string {
	disc := trimSpaceOrEmpty(memberName)
	if disc == "" {
		disc = trimSpaceOrEmpty(role)
	}
	if disc == "" {
		return ""
	}
	if sessionID != "" {
		return sessionID + "-" + disc
	}
	return disc
}

// buildSwarmBrowserAgent 每成员浏览器隔离的 browser 子代理配置。
//
// build_browser_agent_config 把解析后的 RuntimeSettings（携带 per-key
// BrowserInstanceConfig）烘进 spec.factory_kwargs；保留它并只加 workspace 标志。
// 显式 Chrome 配置使 Swarm 进入受管浏览器；单 agent 适配器保留独立 Electron
// 页面绑定。
// Deviation D-15.16f-browser-key：Go BuildBrowserAgentConfig 无 browser_key
// 形参（受管模式 per-key 实例分配段 ⤴️15.20）；apply_browser_decision_config /
// apply_swarm_browser_settings 基座（browser_config/browser_runtime.py）Go
// 未实施 ⤴️15.20——browser_key 派生与日志语义全量保留。
// Python: build_swarm_browser_agent (code_subagents.py:224-259)
func buildSwarmBrowserAgent(factoryKwargs map[string]any, view *buildctx.SwarmBuildContext) (*hschema.SubAgentConfig, error) {
	model := parentModelOf(view)
	if model == nil {
		logger.Warn(logger.ComponentAgentServer).
			Msg("[swarm.browser_agent] skipped: no parent model on build context")
		return nil, nil
	}
	maxIterations := intParamOr(factoryKwargs, "max_iterations", constDefaultBrowserIter)
	browserKey := browserKeyOf(view.SessionID, view.MemberName, view.Role)
	spec := subagents.BuildBrowserAgentConfig(model, &hschema.SubagentCreateParams{
		Language:      CodeRuntimeLanguage(view),
		MaxIterations: maxIterations,
	})
	if spec.FactoryKwargs == nil {
		spec.FactoryKwargs = map[string]any{}
	}
	spec.FactoryKwargs["auto_create_workspace"] = false
	// Python: apply_browser_decision_config(spec, ctx.config) + settings 段
	// ——基座 ⤴️15.20（browser_key 随回收点传入 RuntimeSettings）。
	_ = browserKey
	logger.Info(logger.ComponentAgentServer).
		Str("member_name", view.MemberName).
		Str("role", view.Role).
		Str("browser_key", browserKey).
		Msg("[swarm.browser_agent] built")
	return spec, nil
}

// buildSwarmCuaAgent cua 桌面子代理配置（共享宿主桌面，无每成员实例键）。
// Python: build_swarm_cua_agent (code_subagents.py:294-325)
func buildSwarmCuaAgent(factoryKwargs map[string]any, view *buildctx.SwarmBuildContext) (*hschema.SubAgentConfig, error) {
	model := parentModelOf(view)
	if model == nil {
		logger.Warn(logger.ComponentAgentServer).
			Msg("[swarm.cua_agent] skipped: no parent model on build context")
		return nil, nil
	}
	// Python: cua_model_rejects_images(model) → warn + nil（:310-313）
	if cuaModelRejectsImages(model) {
		logger.Warn(logger.ComponentAgentServer).
			Msg("[swarm.cua_agent] skipped: the parent model rejects image input")
		return nil, nil
	}
	maxIterations := intParamOr(factoryKwargs, "max_iterations", 0)
	snapshotKeepLastK := intParamOr(factoryKwargs, "cua_snapshot_keep_last_k", 3)
	pauseOnUserInput := false
	if v, ok := factoryKwargs["cua_pause_on_user_input"].(bool); ok {
		pauseOnUserInput = v
	}
	spec, err := subagents.BuildCuaAgentConfig(model, &subagents.CuaAgentConfigParams{
		Workspace:            workspaceFromString(factoryKwargs, view),
		SysOperation:         parentSysOperation(view),
		Language:             CodeRuntimeLanguage(view),
		MaxIterations:        maxIterations,
		CuaSnapshotKeepLastK: snapshotKeepLastK,
		CuaPauseOnUserInput:  pauseOnUserInput,
	})
	if err != nil {
		return nil, err
	}
	// Python: spec.factory_kwargs = {**(spec.factory_kwargs or {}),
	//   "auto_create_workspace": False}（保留 cua_* 工厂 kwargs，只加工作区标志）
	if spec.FactoryKwargs == nil {
		spec.FactoryKwargs = map[string]any{}
	}
	spec.FactoryKwargs["auto_create_workspace"] = false
	return spec, nil
}

// ──────────────────────────── 导出函数（注册） ────────────────────────────

// RegisterSubagentProviders 注册 code_subagents 全部 subagent provider。
// Python: code_subagents.py @harness_element(kind=SUBAGENT) 段（:104-325）。
func RegisterSubagentProviders() {
	hschema.RegisterSubagentProvider(constCodeAgent, subagentProviderAdapter(buildCodeAgent))
	hschema.RegisterSubagentProvider(constStatuslineAgent, subagentProviderAdapter(buildStatuslineSetupAgent))
	hschema.RegisterSubagentProvider(constSwarmBrowserAgent, subagentProviderAdapter(buildSwarmBrowserAgent))
	hschema.RegisterSubagentProvider(constSwarmCuaAgent, subagentProviderAdapter(buildSwarmCuaAgent))
}

// BrowserKeyOf 导出 browser_key 派生（测试与诊断用）。
// Python: _browser_key (code_subagents.py:200-222)
func BrowserKeyOf(sessionID, memberName, role string) string {
	return browserKeyOf(sessionID, memberName, role)
}
