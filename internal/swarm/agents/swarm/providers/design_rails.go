package providers

import (
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	buildctx "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/buildctx"
)

// design_rails.go + designer_tools.go 合并说明（同段登记）：
// designer 三 rail 壳 + designer 图工具壳。基座 harness/design/rails/ 三 rail
// 752 行 + StoreNodeSpawner/DesignerGraphStore/get_design_context_registry
// （swarmflow 域）Go 未实施 ⤴️25.12（缝 F4，D-e1 同落点）——工厂签名、
// session 门控与常量注册全量保留，被依赖符号缺省语义对齐（基座缺失时
// authoring/execution 返回 nil，sanitizer 无基座依赖但按缝语义同样挂账）。
//
// Python: jiuwenswarm/agents/swarm/providers/design_rails.py（100 行）
//         + providers/designer_tools.py（145 行）。

// ──────────────────────────── 常量 ────────────────────────────

const (
	constDesignerAuthoring        = "swarm.designer_authoring"
	constDesignerExecution        = "swarm.designer_execution"
	constDesignerSurfaceSanitizer = "swarm.designer_surface_sanitizer"
	constDesignerGraphTools       = "swarm.designer_graph_tools"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildDesignerAuthoringRail designer 授权 rail（无会话跳过）。
// Python: _build_designer_authoring_rail (design_rails.py:56-65)
func buildDesignerAuthoringRail(params map[string]any, view *buildctx.SwarmBuildContext) (any, error) {
	_ = params
	if trimSpaceOrEmpty(view.SessionID) == "" {
		return nil, nil
	}
	// Python: DesignerAuthoringRail(session_id=session_id)——基座 ⤴️25.12
	return nil, nil
}

// buildDesignerExecutionRail designer 执行 rail（swarmflow + 画布触发提示段）。
// Python: _build_designer_execution_rail (design_rails.py:73-82)
func buildDesignerExecutionRail(params map[string]any, view *buildctx.SwarmBuildContext) (any, error) {
	_ = params
	if trimSpaceOrEmpty(view.SessionID) == "" {
		return nil, nil
	}
	// Python: DesignerExecutionRail(session_id=session_id)——基座 ⤴️25.12
	return nil, nil
}

// buildDesignerSurfaceSanitizerRail 从 team.design leader 表面减去非设计
// 工具/提示段（workspace_meta、org_* 工具、群聊、build_team 引导段）。
// Python: _build_designer_surface_sanitizer_rail (design_rails.py:90-94)
func buildDesignerSurfaceSanitizerRail(params map[string]any, view *buildctx.SwarmBuildContext) (any, error) {
	_ = params
	_ = view
	// Python: DesignerSurfaceSanitizerRail()——基座 ⤴️25.12
	return nil, nil
}

// buildDesignerGraphTools 设计团队 leader 的 designer 图工具。
//
// 两轨架构：graph 授权（designer_graph_autorun）不在此挂载——它是
// DesignerAuthoringRail 的唯一能力（授权与执行各持一工具 + 提示段）；
// 本 provider 只服务图编辑/上下文读取。
// Python: build_designer_graph_tools (designer_tools.py:45-80)——
// get_design_context_registry / DesignerGraphStore / StoreNodeSpawner /
// NodeExecutionContext / DesignerGraphToolkit 基座 ⤴️25.12。
func buildDesignerGraphTools(params map[string]any, view *buildctx.SwarmBuildContext) []tool.Tool {
	_ = params
	sessionID := trimSpaceOrEmpty(view.SessionID)
	_ = sessionID
	// Python: context 未绑定 graph → raise ValueError；基座 ⤴️25.12
	return nil
}

// ──────────────────────────── 导出函数（注册） ────────────────────────────

// RegisterDesignProviders 注册 design_rails 三 rail 与 designer_tools。
// Python: design_rails.py @harness_element 段（:56-94）+ designer_tools.py（:45-84）。
func RegisterDesignProviders() {
	hschema.RegisterRailProvider(constDesignerAuthoring, railProviderAdapter(buildDesignerAuthoringRail))
	hschema.RegisterRailProvider(constDesignerExecution, railProviderAdapter(buildDesignerExecutionRail))
	hschema.RegisterRailProvider(constDesignerSurfaceSanitizer, railProviderAdapter(buildDesignerSurfaceSanitizerRail))
	hschema.RegisterToolProvider(constDesignerGraphTools, toolProviderAdapter(buildDesignerGraphTools))
}
