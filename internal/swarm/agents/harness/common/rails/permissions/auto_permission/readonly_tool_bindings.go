// 封闭的内置观察类工具的只读快速路径身份校验。
//
// 对应 Python 代码：_auto_permission/readonly_tool_bindings.py（320 行）
//
// 作用：识别「这个工具调用确实来自宿主内置的、未被替换的只读观察工具」，
// 命中时可跳过 LLM 审查直接放行；**任何一处身份存疑都返回 false**（fail-closed）。
//
// ## 已知偏差 V-4：闭包链内省 → MarkTrusted()
//
// Python 用 `_closure` 逐层剥开 SDK 装饰器闭包，比对
// `__code__` / `__closure__.cell_contents` / `__globals__` 证明可执行体未被替换。
// Go 的函数不是对象、闭包捕获变量不可枚举，**该机制不存在**。
//
// 替代方案（9.90d-1 Task 14 已落地）：受信任构造函数调用
// `DeepAgentRail.MarkTrusted()`，本文件用 `IsTrusted()` 校验；
// `NewProgressiveToolRail` 已在本批补上该调用。
// 残余风险：绕过构造函数后手工调 MarkTrusted 无法检测，已在 spec V-4 登记并接受。
//
// ## 范围（决策 D3）：只覆盖 ProgressiveToolRail
//
// Python 原文件覆盖 10+ 个内置工具（list_skill / get_current_goal /
// recall_compressed_context / cron / heartbeat ...），这些依赖 V-6 前置件
// ≥4,286 行、Go 侧未落地（spec §5.1）。本批只实现 `tool_search` +
// ProgressiveToolRail 一条路径，其余工具名一律 false（走正常审批链）。

package auto_permission

import (
	"reflect"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/tool_display"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// 注：owner agent 直接用 `*harness.DeepAgent` 具体类型，不定义窄接口——
// `DeepAgent.ReactAgent()` 返回 `*agents.ReActAgent`，用 `any` 描述会让
// Go 的接口方法签名不匹配（方法的返回类型必须逐字相同），导致真实
// DeepAgent 无法满足接口。这是 Go 类型系统的硬约束。

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// toolSearchName 本批唯一支持的只读快速路径工具名
	//
	// Python: "tool_search"
	toolSearchName = "tool_search"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// progressiveToolRailType ProgressiveToolRail 的具体类型（精确匹配用）
	progressiveToolRailType = reflect.TypeOf((*rails.ProgressiveToolRail)(nil))
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// trustedReadonlyBinding 判定本次调用是否命中受信任的只读快速路径。
//
// Python: trusted_readonly_binding(invocation, session_id, *, owner_agent=None)
//
// 返回 false 表示**不启用快速路径**（走正常审批），不表示拒绝调用。
func trustedReadonlyBinding(
	invocation ToolInvocation,
	sessionID string,
	ownerAgent any,
) bool {
	// D3：其余工具名一律不走快速路径
	if invocation.ToolName != toolSearchName {
		return false
	}
	return sessionObservationBinding(invocation, sessionID, ownerAgent)
}

// sessionObservationBinding 校验 owner / callback agent 桥接与 rail 身份。
//
// Python: _session_observation_binding(invocation, session_id, agent)
// 只保留 ProgressiveToolRail 分支（D3），其余 SDK 工具分支留待 V-6 前置件。
func sessionObservationBinding(
	invocation ToolInvocation,
	sessionID string,
	ownerAgent any,
) bool {
	if invocation.Ctx == nil || sessionID == "" {
		return false
	}
	owner, ok := ownerAgent.(*harness.DeepAgent)
	if !ok || owner == nil {
		return false
	}

	// ① owner / callback agent 桥接：内层 ReActAgent 或自身，且能力管理器同一
	callbackAgent := invocation.Ctx.Agent()
	if callbackAgent == nil {
		return false
	}
	if !deepAgentBridgeMatches(owner, callbackAgent) {
		return false
	}

	// ② 会话一致、未终止
	session := invocation.Ctx.Session()
	if session == nil || session.GetSessionID() != sessionID {
		return false
	}
	if boundID := boundSessionIDOf(ownerAgent); boundID != sessionID {
		return false
	}
	if owner.Phase() == hschema.InteractionPhaseTerminated {
		return false
	}

	// ③ 恰好一个 ProgressiveToolRail，具体类型匹配
	found := owner.FindRailsByType(progressiveToolRailType)
	if len(found) != 1 {
		return false
	}
	progressive, isConcrete := found[0].(*rails.ProgressiveToolRail)
	if !isConcrete || reflect.TypeOf(found[0]) != progressiveToolRailType {
		return false
	}

	// ④ V-4：MarkTrusted 替代闭包链内省
	if !progressive.IsTrusted() {
		return false
	}

	// ⑤ tool_search 参数合法
	_, searchArgs := tool_display.ExtractCallGoal(invocation.ToolArgs)
	argsMap, _ := searchArgs.(map[string]any)
	if argsMap == nil || !permissions.ToolSearchArgsValid(argsMap, true) {
		return false
	}

	// ⑥ 工具绑定校验（类型 + 卡片同一性）
	if callbackAgent.AbilityManager() == nil {
		return false
	}
	return ResolveToolBinding(callbackAgent.AbilityManager(), toolSearchName,
		toolSearchToolType()) != nil
}

// deepAgentBridgeMatches 校验 callback agent 与 owner 的桥接关系。
//
// Python: callback_agent is agent or callback_agent is agent.react_agent
//
//	and callback_agent.ability_manager is agent.ability_manager
func deepAgentBridgeMatches(
	owner *harness.DeepAgent,
	callbackAgent agentinterfaces.BaseAgent,
) bool {
	byIdentity := reflect.ValueOf(owner).Pointer() == reflect.ValueOf(callbackAgent).Pointer()
	byReactAgent := false
	if react := owner.ReactAgent(); react != nil {
		byReactAgent = reflect.ValueOf(react).Pointer() == reflect.ValueOf(callbackAgent).Pointer()
	}
	if !byIdentity && !byReactAgent {
		return false
	}
	return callbackAgent.AbilityManager() == owner.AbilityManager()
}

// boundSessionIDOf 读取 owner 绑定的会话标识。
//
// Python: agent._bound_session_id
// Go 侧无导出访问器，用反射读非导出字段（偏差 V-17，见 spec）。
func boundSessionIDOf(ownerAgent any) string {
	value := reflect.ValueOf(ownerAgent)
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return ""
	}
	field := value.FieldByName("boundSessionID")
	if !field.IsValid() || field.Kind() != reflect.String {
		return ""
	}
	return field.String()
}

// toolSearchToolType 返回 ToolSearchTool 的具体类型。
//
// Python: ToolSearchTool（来自 tool_discovery 包）
// Go 侧该工具为函数式实现、无导出具体类型，故用装配期登记的类型；
// 未登记时返回 nil，使 ResolveToolBinding 返回 nil（fail-closed）。
func toolSearchToolType() reflect.Type {
	return registeredReadonlyToolType(toolSearchName)
}

// ──────────────────────────── 装配期登记（V-4 延伸） ────────────────────────────

var (
	// readonlyToolTypes 装配期登记的只读工具具体类型表（工具名 → 类型）。
	//
	// Python 的 _CONTROL_TYPES / _invoke_matches 靠类对象常量；Go 的工具是
	// 函数式实现、无导出具体类型，改由装配期登记。未登记的工具一律
	// fail-closed（不启用快速路径）。
	readonlyToolTypesMu sync.RWMutex
	readonlyToolTypes   = map[string]reflect.Type{}
)

// SetReadonlyToolTypes 在装配期登记只读工具的具体类型。
//
// 与 SetNativePathExecutors（invocation_context.go）同一设计：
// 用「装配期登记 + 运行期类型/实例比对」替代 Python 的类对象常量。
func SetReadonlyToolTypes(types map[string]reflect.Type) {
	readonlyToolTypesMu.Lock()
	defer readonlyToolTypesMu.Unlock()
	readonlyToolTypes = make(map[string]reflect.Type, len(types))
	for name, toolType := range types {
		if toolType == nil {
			continue
		}
		readonlyToolTypes[name] = toolType
	}
}

// registeredReadonlyToolType 取登记的具体类型，未登记返回 nil。
func registeredReadonlyToolType(toolName string) reflect.Type {
	readonlyToolTypesMu.RLock()
	defer readonlyToolTypesMu.RUnlock()
	return readonlyToolTypes[toolName]
}
