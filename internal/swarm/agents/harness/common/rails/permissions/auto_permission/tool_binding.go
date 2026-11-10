// 读取当前可执行绑定的身份，不做权限决策。
// 本文件回答一个问题：我要放行的那个工具，是不是我原以为的那个实现？
// 它只做身份校验，判断结果交给调用方。
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/permissions/tool_binding.py
package auto_permission

import (
	"reflect"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// resolveToolByID 按 ID 取工具实例，默认走全局 ResourceMgr。
// 不传 WithSession，等价于 Python 的 session=None——不加 trace 装饰，
// 拿到的就是裸实现；若加了装饰会变成 *decorator.TracedTool，
// 精确类型校验必然失败。
// 返回 nil 表示工具不存在（Python 同样返回 None，不抛异常）。
// 变量形式供测试注入，对齐 common/tools/send_file.go 的 SetXxxFunc 惯例。
// Python: Runner.resource_mgr.get_tool(tool_id, session=None)
var resolveToolByID = defaultResolveToolByID

// defaultResolveToolByID 默认的工具查找实现（走全局 ResourceMgr）。
var defaultResolveToolByID = func(id string) tool.Tool {
	tools, err := runner.GetResourceMgr().GetTool([]string{id})
	if err != nil || len(tools) == 0 {
		return nil
	}
	return tools[0]
}

// ──────────────────────────── 导出函数 ────────────────────────────

// MatchesBoundMethod 比较调用方提供的两个绑定身份。
// Python 的 matches_bound_method 读方法对象上的 __self__ 与 __func__；
// Go 的函数值不携带接收者信息（a.Do 与 b.Do 的代码指针相同），
// 因此接收者必须由调用方显式传入，这是与 Python 的签名差异（spec D1）。
// 判定顺序与 Python 一致：
//  1. expectedOwner 为 nil → false
//  2. expectedFunc 为 nil → false
//  3. receiver 与 expectedOwner 非同一对象 → false
//  4. method 与 expectedFunc 代码指针不同 → false
//  5. 否则 true
//
// Python: matches_bound_method(method, *, expected_owner, expected_func)
func MatchesBoundMethod(receiver any, method any, expectedOwner any, expectedFunc any) bool {
	if expectedOwner == nil || expectedFunc == nil {
		return false
	}
	if !identityEqual(receiver, expectedOwner) {
		return false
	}
	return funcPtrEqual(method, expectedFunc)
}

// ResetResolveToolByIDFunc 恢复工具查找的默认实现（测试清理用）。
//
// 全局变量在测试间会互相污染：某个用例注入假实现后不清理，后续用例
// 会走假实现导致 ResolveToolBinding 返回 nil。
func ResetResolveToolByIDFunc() {
	resolveToolByID = defaultResolveToolByID
}

// SetResolveToolByIDFunc 替换工具查找实现，仅供测试注入。
// 对齐 common/tools/send_file.go 的 SetSendFilePushFunc 成对注入惯例。
func SetResolveToolByIDFunc(fn func(id string) tool.Tool) {
	if fn != nil {
		resolveToolByID = fn
	}
}

// ResolveToolBinding 返回确切注册的实现，不匹配一律返回 nil。
// 三重校验（与 Python 逐条对应，步骤顺序一字不差）：
//  1. 实现类型必须精确等于 expectedType——子类不算合格，
//     因为子类可以重写 Invoke。这里校验的是「是否同一个实现」，
//     而不是「能否赋值给这个类型」。
//  2. 卡片必须是同一个对象（指针同一），不是字段相等的另一张卡。
//  3. 查找时不带 session，避免拿到 trace 装饰后的包装类型。
//
// 六档返回语义与 Python 完全一致（spec §3.2.2 印证表）：
// 能力不存在 / 能力非 ToolCard / 工具不存在 / 类型不符 / 卡片不同一 → nil；
// 全部通过 → resource。
// Python: resolve_tool_binding(agent, tool_name, expected_type)
func ResolveToolBinding(
	am agentinterfaces.AbilityManagerInterface,
	toolName string,
	expectedType reflect.Type,
) tool.Tool {
	// 1. 查能力卡片（Python: card = agent.ability_manager.get(tool_name)）
	card := am.Get(toolName)
	if card == nil {
		return nil
	}

	// 2. 按卡片 ID 查实现（Python: get_tool(card.id, session=None)）
	// 用 schema.Ability 接口自带的 AbilityID()，无需先断言成 *tool.ToolCard，
	// 保证与 Python 的步骤顺序完全一致。
	resource := resolveToolByID(card.AbilityID())
	if resource == nil {
		return nil
	}

	// 3. 精确类型校验（Python: exact_type = type(resource) is expected_type）
	if reflect.TypeOf(resource) != expectedType {
		return nil
	}

	// 4. 卡片同一性校验（Python: resource.card is card）
	// 指针同一性比较必须拿到 *tool.ToolCard 具体值，断言在此处才需要。
	cardPtr, ok := card.(*tool.ToolCard)
	if !ok {
		return nil
	}
	if resource.Card() != cardPtr {
		return nil
	}

	return resource
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// identityEqual 判断两个值是否为同一对象（对应 Python 的 is）。
// 指针、map、slice、func、chan、unsafe.Pointer 取地址指针比较；
// 其余类型退化为值相等（Python 中值类型无 is 语义差别的场景，Go 用 == 承载）。
// 任一方为 nil 视为不相等（nil 的同一性在调用方已短路）。
func identityEqual(a, b any) bool {
	if a == nil || b == nil {
		return false
	}
	av := reflect.ValueOf(a)
	bv := reflect.ValueOf(b)
	if av.Type() != bv.Type() {
		return false
	}
	// 引用类型比较底层地址；其余类型（值类型）退化为 == 值比较。
	switch av.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return av.Pointer() == bv.Pointer()
	default:
		return a == b
	}
}

// funcPtrEqual 比较两个函数值的代码指针。
// Go 函数值不可 == 比较，只能取代码指针。
// 先例：agentcore/harness/rails/model_anomaly.go funcPtrEqual、sys_operation/registry.go:73
func funcPtrEqual(a, b any) bool {
	if a == nil || b == nil {
		return false
	}
	av := reflect.ValueOf(a)
	bv := reflect.ValueOf(b)
	if av.Kind() != reflect.Func || bv.Kind() != reflect.Func {
		return false
	}
	if av.Type() != bv.Type() {
		return false
	}
	return av.Pointer() == bv.Pointer()
}
