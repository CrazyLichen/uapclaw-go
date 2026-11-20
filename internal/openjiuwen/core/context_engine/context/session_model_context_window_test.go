package context

import "testing"

// TestUpdateModelContext_覆盖全局优先级 验证模型级窗口在无全局值时生效、清空后回落到模型名解析。
func TestUpdateModelContext_覆盖全局优先级(t *testing.T) {
	mc := newTestSessionModelContext() // 同包助手（session_model_context_test.go）
	mc.UpdateModelContext("qwen3-max", 999999)
	if got := mc.ContextMaxTokens(); got != 999999 {
		t.Fatalf("override 生效后窗口 = %d, want 999999", got)
	}
	mc.UpdateModelContext("qwen3-max", 0) // <=0 清除 override，回落到按模型名解析（内置表 262144）
	if got := mc.ContextMaxTokens(); got != 262144 {
		t.Fatalf("清除 override 后应回落内置表 262144，got %d", got)
	}
}

// TestUpdateModelContext_全局窗口优先 验证全局 context_window_tokens 始终最高优先。
func TestUpdateModelContext_全局窗口优先(t *testing.T) {
	mc := newTestSessionModelContext(func(o *testContextOpts) { o.contextWindowTokens = 131072 })
	mc.UpdateModelContext("qwen3-max", 262144)
	if got := mc.ContextMaxTokens(); got != 131072 {
		t.Fatalf("全局窗口应最高优先，got %d", got)
	}
}

// TestUpdateModelContext_新模型名参与解析 验证清掉 override 后按新模型名查表。
func TestUpdateModelContext_新模型名参与解析(t *testing.T) {
	mc := newTestSessionModelContext()
	mc.UpdateModelContext("glm-4-long", 0)
	if got := mc.ContextMaxTokens(); got != 1000000 {
		t.Fatalf("应命中内置表 glm-4-long=1000000，got %d", got)
	}
}
