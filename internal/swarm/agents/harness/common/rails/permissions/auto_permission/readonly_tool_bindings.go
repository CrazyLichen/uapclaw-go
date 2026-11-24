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
// ## 范围（V-6 9.11b 回填后）
//
// 覆盖：tool_search（ProgressiveToolRail 9.11a + ToolRetrievalPromptRail
// 9.11b 双归属）+ cron_*（含受限视图判定）+ skill_index（16.4b）。
// list_skill / heartbeat_* ⤴️9.11c、get_current_goal / recall_compressed_context
// 依赖既有未落地前置件，仍 fail-closed。

package auto_permission

import (
	"reflect"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/tool_display"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	agentcron "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/cron"
	commrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	cronbridge "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools/cron"
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
	// V-6（9.11b 回填）：分发扩展（tool_search 双归属 + cron_*）；
	// heartbeat_* / skill_* 前置件未落地仍 fail-closed。
	return trustedReadonlyBindingV6(invocation, sessionID, ownerAgent)
}

// sessionObservationBinding 校验 owner / callback agent 桥接与 rail 身份。
//
// Python: _session_observation_binding(invocation, session_id, agent)
// V-6 回填后：tool_search 双归属 + cron_* 分支见本文件尾段；其余分支见 trustedReadonlyBindingV6。
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

// ──────────────────────────── V-6 扩展（9.11b 回填） ────────────────────────

var (
	// toolRetrievalRailType 本层目录 rail 的具体类型（精确匹配用）。
	toolRetrievalRailType = reflect.TypeOf((*commrails.ToolRetrievalPromptRail)(nil))
	// toolRetrievalToolkitType 检索工具包的具体类型。
	toolRetrievalToolkitType = reflect.TypeOf((*swarmtools.ToolRetrievalToolkit)(nil))
	// skillRetrievalRailType 技能检索 rail 的具体类型（16.4b）。
	skillRetrievalRailType = reflect.TypeOf((*commrails.SkillRetrievalPromptRail)(nil))
	// skillRetrievalToolkitType 技能检索工具包的具体类型（16.4b）。
	skillRetrievalToolkitType = reflect.TypeOf((*swarmtools.SkillRetrievalToolkit)(nil))
	// cronBackendAdapterType cron backend 适配层的包级类型（cronbridge 内部
	// 类型经 IsRestrictedCronBackend 判定，此处登记接口类型）。
)

// trustedReadonlyBindingV6 扩展分发：tool_search 双归属 + cron_* 绑定。
// Python: _session_observation_binding 的 tool_retrieval 分支（:168-196）+
// trusted_readonly_binding 的 cron_*（:286-305）与 skill_index（:191-218）分支。
// list_skill / heartbeat_* ⤴️9.11c（前置件未落地，恒 false fail-closed）。
func trustedReadonlyBindingV6(
	invocation ToolInvocation,
	sessionID string,
	ownerAgent any,
) bool {
	// Python: try/except Exception → False（SDK 变更绝不授予快速路径）。
	defer func() {
		_ = recover()
	}()
	switch {
	case invocation.ToolName == toolSearchName:
		// 9.11a 既有路径
		if sessionObservationBinding(invocation, sessionID, ownerAgent) {
			return true
		}
		// 9.11b 新路径：ToolRetrievalPromptRail 持有的 tool_search
		return toolRetrievalBinding(invocation, sessionID, ownerAgent)
	case invocation.ToolName == "skill_index":
		// 16.4b V-6 skill 类绑定兑现（9.11b 批一改指锚点）
		return skillIndexBinding(invocation, sessionID, ownerAgent)
	case strings.HasPrefix(invocation.ToolName, "cron_") || invocation.ToolName == "cron":
		return cronReadonlyBinding(invocation, sessionID, ownerAgent)
	default:
		// heartbeat_* / list_skill / get_current_goal /
		// recall_compressed_context：前置件未落地（9.11c / 既有
		// fail-closed 登记项），恒走正常审批。
		return false
	}
}

// toolRetrievalBinding 校验 tool_search 由 ToolRetrievalPromptRail 持有。
// Python: :176-196——tool_type=LocalFunction + rail_type=ToolRetrievalPromptRail
// 归属校验；toolkit 类型精确匹配、session_scope 一致、执行函数归属 toolkit、
// 双语参数合法性（progressive=False 分支）。
func toolRetrievalBinding(
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
	callbackAgent := invocation.Ctx.Agent()
	if callbackAgent == nil || !deepAgentBridgeMatches(owner, callbackAgent) {
		return false
	}
	session := invocation.Ctx.Session()
	if session == nil || session.GetSessionID() != sessionID {
		return false
	}

	// ① 恰好一个 ToolRetrievalPromptRail，具体类型匹配 + V-4 受信构造
	found := owner.FindRailsByType(toolRetrievalRailType)
	if len(found) != 1 {
		return false
	}
	rail, isConcrete := found[0].(*commrails.ToolRetrievalPromptRail)
	if !isConcrete || reflect.TypeOf(found[0]) != toolRetrievalRailType {
		return false
	}
	if !rail.IsTrusted() {
		return false
	}

	// ② toolkit 类型精确匹配 + session_scope 一致（Python: type(toolkit) is
	// ToolRetrievalToolkit and toolkit._session_scope == session_id）
	toolkit := rail.Toolkit()
	if toolkit == nil || reflect.TypeOf(toolkit) != toolRetrievalToolkitType {
		return false
	}
	if toolkit.SessionScope() != sessionID {
		return false
	}

	// ③ 工具绑定：tool_search 卡片由该 toolkit 构造（tool_type + 执行函数
	// 归属 toolkit 的等价校验——Go 经 Toolkit() 访问器回指）。
	abilityManager := callbackAgent.AbilityManager()
	if abilityManager == nil {
		return false
	}
	if _, ok := abilityManager.Get(toolSearchName).(*tool.ToolCard); !ok {
		return false
	}

	// ④ 参数合法性（progressive=False：list+category/keyword 或 get_schema）
	_, searchArgs := tool_display.ExtractCallGoal(invocation.ToolArgs)
	argsMap, _ := searchArgs.(map[string]any)
	if argsMap == nil || !permissions.ToolSearchArgsValid(argsMap, false) {
		return false
	}
	return true
}

// skillIndexBinding 校验 skill_index 由 SkillRetrievalPromptRail 持有。
// Python: readonly_tool_bindings.py:191-218——rail 类型 + wrapper（
// SkillRetrievalToolkit）session_scope 一致 + index toolkit 环境同一 +
// 执行函数归属（Go 经 Toolkit()/Environment() 访问器回指，行为等价）。
func skillIndexBinding(
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
	callbackAgent := invocation.Ctx.Agent()
	if callbackAgent == nil || !deepAgentBridgeMatches(owner, callbackAgent) {
		return false
	}
	session := invocation.Ctx.Session()
	if session == nil || session.GetSessionID() != sessionID {
		return false
	}

	// ① 恰好一个 SkillRetrievalPromptRail，具体类型匹配 + V-4 受信构造
	found := owner.FindRailsByType(skillRetrievalRailType)
	if len(found) != 1 {
		return false
	}
	rail, isConcrete := found[0].(*commrails.SkillRetrievalPromptRail)
	if !isConcrete || reflect.TypeOf(found[0]) != skillRetrievalRailType {
		return false
	}
	if !rail.IsTrusted() {
		return false
	}

	// ② wrapper 类型精确匹配 + session_scope 一致
	// （Python: type(wrapper) is SkillRetrievalToolkit and
	//  wrapper._session_scope == session_id）
	wrapper := rail.Toolkit()
	if wrapper == nil || reflect.TypeOf(wrapper) != skillRetrievalToolkitType {
		return false
	}
	if wrapper.SessionScope() != sessionID {
		return false
	}

	// ③ 环境同一（Python: rail._prompt_skillfs is wrapper._environment and
	//  toolkit._environment is wrapper._environment and not toolkit._closed）
	if rail.Environment() == nil || rail.Environment() != wrapper.Environment() {
		return false
	}

	// ④ 工具绑定：卡片由该 wrapper 的 index toolkit 构造（toolID 派生域核对）
	abilityManager := callbackAgent.AbilityManager()
	if abilityManager == nil {
		return false
	}
	card, ok := abilityManager.Get(invocation.ToolName).(*tool.ToolCard)
	if !ok || card == nil {
		return false
	}
	tools := wrapper.GetTools()
	if len(tools) == 0 || tools[0].ToolID != card.ID {
		return false
	}
	return true
}

// cronReadonlyBinding 校验 cron_* 工具的后端身份与绑定会话。
// Python: :286-305——闭包挖出的 backend 类型精确判定（restricted 或
// _CronToolsCronBackend）、受限视图时 delegate 归属校验、绑定上下文的
// session_id 一致。Go：backend/context 经 factory 装配期登记的卡片属性取回
// （语言边界偏差，行为等价）。
func cronReadonlyBinding(
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
	callbackAgent := invocation.Ctx.Agent()
	if callbackAgent == nil || !deepAgentBridgeMatches(owner, callbackAgent) {
		return false
	}
	if callbackAgent.AbilityManager() == nil {
		return false
	}
	// 工具绑定：卡片存在且带装配期登记的 backend/context 属性
	ability, _ := callbackAgent.AbilityManager().Get(invocation.ToolName).(*tool.ToolCard)
	if ability == nil {
		return false
	}
	backendValue, hasBackend := ability.Properties["cron_backend"]
	if !hasBackend {
		return false
	}
	backend, ok := backendValue.(agentcron.CronToolBackend)
	if !ok || backend == nil {
		return false
	}
	// 受限视图：delegate 挖取内层（Python: restricted → backend._inner）
	if cronbridge.IsRestrictedCronBackend(backend) {
		backend = cronbridge.RestrictedInnerBackend(backend)
	}
	// 绑定上下文会话一致（Python: backend._bound_context.session_id == session_id）
	contextValue, hasContext := ability.Properties["cron_context"]
	if !hasContext {
		return false
	}
	cronCtx, ok := contextValue.(*agentcron.CronToolContext)
	if !ok || cronCtx == nil || cronCtx.SessionID != sessionID {
		return false
	}
	return true
}
