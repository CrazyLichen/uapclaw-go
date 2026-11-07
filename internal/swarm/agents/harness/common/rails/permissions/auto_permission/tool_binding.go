// 读取当前可执行绑定的身份，不做权限决策。
//
// 本文件回答一个问题：我要放行的那个工具，是不是我原以为的那个实现？
// 它只做身份校验，判断结果交给调用方。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/permissions/tool_binding.py
package auto_permission

import (
	"reflect"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// MatchesBoundMethod 比较调用方提供的两个绑定身份。
//
// Python 的 matches_bound_method 读方法对象上的 __self__ 与 __func__；
// Go 的函数值不携带接收者信息（a.Do 与 b.Do 的代码指针相同），
// 因此接收者必须由调用方显式传入，这是与 Python 的签名差异（spec D1）。
//
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// identityEqual 判断两个值是否为同一对象（对应 Python 的 is）。
//
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
	switch av.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return av.Pointer() == bv.Pointer()
	default:
		return a == b
	}
}

// funcPtrEqual 比较两个函数值的代码指针。
//
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
