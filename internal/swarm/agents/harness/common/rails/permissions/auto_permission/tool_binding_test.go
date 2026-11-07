package auto_permission

import "testing"

// boundOwner 用于测试匹配的接收者
type boundOwner struct {
	// tag 区分实例
	tag string
}

// Do 目标方法
func (o *boundOwner) Do() {}

// Other 另一个方法，用于验证函数不同
func (o *boundOwner) Other() {}

// TestMatchesBoundMethod_正常 验证接收者与函数都相同时为 true
func TestMatchesBoundMethod_正常(t *testing.T) {
	owner := &boundOwner{tag: "a"}
	if !MatchesBoundMethod(owner, (*boundOwner).Do, owner, (*boundOwner).Do) {
		t.Fatal("接收者与函数均相同应返回 true")
	}
}

// TestMatchesBoundMethod_owner为nil 验证 expectedOwner 为 nil 时短路
func TestMatchesBoundMethod_owner为nil(t *testing.T) {
	owner := &boundOwner{tag: "a"}
	if MatchesBoundMethod(owner, (*boundOwner).Do, nil, (*boundOwner).Do) {
		t.Fatal("expectedOwner 为 nil 应返回 false")
	}
}

// TestMatchesBoundMethod_func为nil 验证 expectedFunc 为 nil 时短路
func TestMatchesBoundMethod_func为nil(t *testing.T) {
	owner := &boundOwner{tag: "a"}
	if MatchesBoundMethod(owner, (*boundOwner).Do, owner, nil) {
		t.Fatal("expectedFunc 为 nil 应返回 false")
	}
}

// TestMatchesBoundMethod_receiver不同 验证不同实例的同名方法判不匹配
//
// 这是本函数存在的核心理由：Go 里 a.Do 与 b.Do 的代码指针相同，
// 只有显式比较 receiver 才能区分。
func TestMatchesBoundMethod_receiver不同(t *testing.T) {
	a := &boundOwner{tag: "a"}
	b := &boundOwner{tag: "b"}
	if MatchesBoundMethod(a, (*boundOwner).Do, b, (*boundOwner).Do) {
		t.Fatal("接收者不同应返回 false")
	}
}

// TestMatchesBoundMethod_函数不同 验证同一接收者的不同方法判不匹配
func TestMatchesBoundMethod_函数不同(t *testing.T) {
	owner := &boundOwner{tag: "a"}
	if MatchesBoundMethod(owner, (*boundOwner).Other, owner, (*boundOwner).Do) {
		t.Fatal("函数不同应返回 false")
	}
}

// TestMatchesBoundMethod_非指针接收者 验证值类型接收者的同一性判断
func TestMatchesBoundMethod_非指针接收者(t *testing.T) {
	type valueOwner struct{ tag string }
	if MatchesBoundMethod(valueOwner{tag: "a"}, valueOwner{tag: "a"}, valueOwner{tag: "a"}, valueOwner{tag: "a"}) {
		t.Fatal("值类型不同实例（即使字段相等）应返回 false")
	}
}
