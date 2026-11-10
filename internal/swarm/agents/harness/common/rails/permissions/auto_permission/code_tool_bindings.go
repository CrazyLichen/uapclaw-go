// 闭集 Code 控制路由的可执行绑定校验。
//
// 对应 Python 代码：_auto_permission/code_tool_bindings.py（110 行）
// Python: trusted_code_route / register_lsp_read_semantics / _control_invoke_matches
//
// 作用：识别「本次调用确实来自宿主内置的 Code 模式控制工具」，命中时可走
// 受信任的封闭路由；任何一处身份存疑返回空串（fail-closed，走正常审批）。
//
// ## 偏差 V-7：只落 agent_mode 三工具，LSP 整层挂 ⤵️ 9.17
//
// LSP 部分依赖 `LspRail` / `LspTool` / `LspToolInput` 等 Go 侧缺失的 SDK 类型
// （约 1,239 行），本批不实现。工工具名为 `lsp` 一律返回空串。
//
// ## 偏差 V-18 的延伸：装配期登记具体类型
//
// Python 的 `_CONTROL_TYPES` 是类对象常量；Go 的三个控制工具是函数式实现、
// 返回 `tool.Tool` 接口，无导出具体类型。改为 `SetCodeControlToolTypes`
// 装配期登记 + `ResolveToolBinding` 精确类型比对（与
// `SetReadonlyToolTypes` / `SetNativePathExecutors` 同一设计）。
//
// ## 偏差 V-4 的延伸：无闭包链内省
//
// Python 的 `_control_invoke_matches` 用 `_closure` / `_invoke_matches` 逐层
// 剥开 SDK 装饰器闭包；Go 无此机制。改用 `ResolveToolBinding` 的
// 「精确类型 + 卡片同一性」两重校验，并校验 rail 的 `MarkTrusted` 身份。

package auto_permission

import (
	"reflect"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// enterPlanModeTool 进入计划模式工具名
	enterPlanModeTool = "enter_plan_mode"
	// exitPlanModeTool 退出计划模式工具名
	exitPlanModeTool = "exit_plan_mode"
	// switchModeTool 切换模式工具名
	switchModeTool = "switch_mode"
	// lspTool LSP 工具名（V-7 未实现）
	lspTool = "lsp"

	// codeAdapterMode 宿主适配器模式标记
	//
	// Python: agent._jiuwenswarm_adapter_mode == "code"
	codeAdapterMode = "code"

	// codePlanControlRoute 计划控制路由标识
	//
	// Python: "code_plan_control"
	codePlanControlRoute = "code_plan_control"

	// codeLspReadRoute LSP 只读路由标识（V-7 未实现）
	//
	// Python: "code_lsp_read"
	codeLspReadRoute = "code_lsp_read"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// controlToolNames agent_mode 三工具闭集
	//
	// Python: _CONTROL_TYPES
	controlToolNames = map[string]struct{}{
		enterPlanModeTool: {},
		exitPlanModeTool:  {},
		switchModeTool:    {},
	}

	// codeControlToolTypes 装配期登记的控制工具具体类型表。
	codeControlToolTypesMu sync.RWMutex
	codeControlToolTypes   = map[string]reflect.Type{}

	// agentModeRailType AgentModeRail 的具体类型
	agentModeRailType = reflect.TypeOf((*rails.AgentModeRail)(nil))
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetCodeControlToolTypes 在装配期登记三个控制工具的具体类型。
//
// 与 SetReadonlyToolTypes / SetNativePathExecutors 同一设计：
// Python 用类对象常量，Go 工具是函数式实现，改由装配期登记。
func SetCodeControlToolTypes(types map[string]reflect.Type) {
	codeControlToolTypesMu.Lock()
	defer codeControlToolTypesMu.Unlock()
	codeControlToolTypes = make(map[string]reflect.Type, len(types))
	for name, toolType := range types {
		if toolType == nil {
			continue
		}
		codeControlToolTypes[name] = toolType
	}
}

// RegisterLSPReadSemantics 教会 file_guard 认识 LSP 的源路径。
//
// Python: register_lsp_read_semantics()
//
// ⤵️ 9.17 回填：LSP 整层（LspRail / LspTool / FileToolSpec 注册）在 Go 侧缺失，
// 本批为空实现。偏差 V-7。
func RegisterLSPReadSemantics() {
	// ⤵️ 9.17 回填
}

// TrustedCodeRoute 判定该调用是否来自受信任的 Code 控制路由。
//
// Python: trusted_code_route(invocation, facts, *, path_requires_approval, owner_agent)
// 返回路由标识（"code_plan_control" / "code_lsp_read"）；空串表示不启用封闭路由。
//
// 计划里的骨架签名为 (invocation, agent, sessionID) bool；本实现按 Python
// 原始签名保留 facts / path_requires_approval / owner_agent 三个判定维度，
// 返回值用字符串表达路由标识（Python 返回 str | None）。
func TrustedCodeRoute(
	invocation ToolInvocation,
	facts permissions.ToolDecisionFacts,
	pathRequiresApproval bool,
	ownerAgent any,
) string {
	// V-7：LSP 整层未实现
	if invocation.ToolName == lspTool || pathRequiresApproval {
		return ""
	}
	if _, isControl := controlToolNames[invocation.ToolName]; !isControl {
		return ""
	}
	if !facts.ArgumentsValidObject {
		return ""
	}
	if !codeOwnerBridgeValid(invocation, ownerAgent) {
		return ""
	}
	if !codeControlBindingValid(invocation) {
		return ""
	}
	return codePlanControlRoute
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// codeOwnerBridgeValid 校验 owner / callback agent 桥接与适配器模式。
//
// Python: trusted_code_route 的前半段
//  1. callback_agent is agent or agent.react_agent
//  2. callback_agent.ability_manager is agent.ability_manager
//  3. agent._jiuwenswarm_adapter_mode == "code"
func codeOwnerBridgeValid(invocation ToolInvocation, ownerAgent any) bool {
	if invocation.Ctx == nil {
		return false
	}
	owner, ok := ownerAgent.(*harness.DeepAgent)
	if !ok || owner == nil {
		return false
	}
	callbackAgent := invocation.Ctx.Agent()
	if callbackAgent == nil {
		return false
	}
	if !deepAgentBridgeMatches(owner, callbackAgent) {
		return false
	}
	if owner.UapswarmAdapterMode() != codeAdapterMode {
		return false
	}
	// V-4 延伸：AgentModeRail 必须恰好一个、具体类型、已 MarkTrusted
	found := owner.FindRailsByType(agentModeRailType)
	if len(found) != 1 {
		return false
	}
	modeRail, isConcrete := found[0].(*rails.AgentModeRail)
	if !isConcrete || reflect.TypeOf(found[0]) != agentModeRailType {
		return false
	}
	return modeRail.IsTrusted()
}

// codeControlBindingValid 校验控制工具的可执行绑定。
//
// Python: _control_invoke_matches（用 _closure 闭包链校验）
// Go 侧改用 ResolveToolBinding 的精确类型 + 卡片同一性（偏差 V-4/V-18）。
func codeControlBindingValid(invocation ToolInvocation) bool {
	if invocation.Ctx == nil || invocation.Ctx.Agent() == nil {
		return false
	}
	am := invocation.Ctx.Agent().AbilityManager()
	if am == nil {
		return false
	}
	expectedType := registeredCodeControlToolType(invocation.ToolName)
	if expectedType == nil {
		return false
	}
	return ResolveToolBinding(am, invocation.ToolName, expectedType) != nil
}

// registeredCodeControlToolType 取登记的具体类型，未登记返回 nil。
func registeredCodeControlToolType(toolName string) reflect.Type {
	codeControlToolTypesMu.RLock()
	defer codeControlToolTypesMu.RUnlock()
	return codeControlToolTypes[toolName]
}

// 编译期断言：确保 agentinterfaces 包被引用
var _ agentinterfaces.BaseAgent
