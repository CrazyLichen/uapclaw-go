package permissions

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDispatchPermissionsConfigRequest_未知方法 测试未知方法返回错误。
func TestDispatchPermissionsConfigRequest_未知方法(t *testing.T) {
	ok, payload := DispatchPermissionsConfigRequest("unknown.method", nil)
	if ok {
		t.Fatal("未知方法应返回 ok=false")
	}
	if payload["code"] != "BAD_REQUEST" {
		t.Fatalf("code=%v，期望 BAD_REQUEST", payload["code"])
	}
}

// TestDispatchPermissionsConfigRequest_ToolsGet 测试获取工具权限。
func TestDispatchPermissionsConfigRequest_ToolsGet(t *testing.T) {
	ok, payload := DispatchPermissionsConfigRequest("permissions.tools.get", nil)
	if !ok {
		t.Fatal("ToolsGet 应返回 ok=true")
	}
	if _, hasTools := payload["tools"]; !hasTools {
		t.Fatal("payload 应包含 tools")
	}
}

// TestDispatchPermissionsConfigRequest_RulesGet 测试获取权限规则。
func TestDispatchPermissionsConfigRequest_RulesGet(t *testing.T) {
	ok, payload := DispatchPermissionsConfigRequest("permissions.rules.get", nil)
	if !ok {
		t.Fatal("RulesGet 应返回 ok=true")
	}
	if _, hasRules := payload["rules"]; !hasRules {
		t.Fatal("payload 应包含 rules")
	}
}

// TestDispatchPermissionsConfigRequest_ApprovalOverridesGet 测试获取审批覆盖。
func TestDispatchPermissionsConfigRequest_ApprovalOverridesGet(t *testing.T) {
	ok, payload := DispatchPermissionsConfigRequest("permissions.approval_overrides.get", nil)
	if !ok {
		t.Fatal("ApprovalOverridesGet 应返回 ok=true")
	}
	if _, hasOverrides := payload["approval_overrides"]; !hasOverrides {
		t.Fatal("payload 应包含 approval_overrides")
	}
}

// TestDispatchPermissionsConfigRequest_ToolsSet_缺少params 测试缺少 params 时返回错误。
func TestDispatchPermissionsConfigRequest_ToolsSet_缺少params(t *testing.T) {
	ok, payload := DispatchPermissionsConfigRequest("permissions.tools.set", nil)
	if ok {
		t.Fatal("缺少 params 应返回 ok=false")
	}
	if payload["code"] != "BAD_REQUEST" {
		t.Fatalf("code=%v，期望 BAD_REQUEST", payload["code"])
	}
}

// TestDispatchPermissionsConfigRequest_ToolsUpdate_缺少tool 测试缺少 tool 参数时返回错误。
func TestDispatchPermissionsConfigRequest_ToolsUpdate_缺少tool(t *testing.T) {
	ok, payload := DispatchPermissionsConfigRequest("permissions.tools.update", map[string]any{"level": "allow"})
	if ok {
		t.Fatal("缺少 tool 应返回 ok=false")
	}
	if payload["code"] != "BAD_REQUEST" {
		t.Fatalf("code=%v，期望 BAD_REQUEST", payload["code"])
	}
}

// TestDispatchPermissionsConfigRequest_ToolsUpdate_缺少level 测试缺少 level 参数时返回错误。
func TestDispatchPermissionsConfigRequest_ToolsUpdate_缺少level(t *testing.T) {
	ok, payload := DispatchPermissionsConfigRequest("permissions.tools.update", map[string]any{"tool": "bash"})
	if ok {
		t.Fatal("缺少 level 应返回 ok=false")
	}
	if payload["code"] != "BAD_REQUEST" {
		t.Fatalf("code=%v，期望 BAD_REQUEST", payload["code"])
	}
}

// TestDispatchPermissionsConfigRequest_ToolsDelete_缺少tool 测试缺少 tool 参数时返回错误。
func TestDispatchPermissionsConfigRequest_ToolsDelete_缺少tool(t *testing.T) {
	ok, _ := DispatchPermissionsConfigRequest("permissions.tools.delete", map[string]any{})
	if ok {
		t.Fatal("缺少 tool 应返回 ok=false")
	}
}

// TestDispatchPermissionsConfigRequest_RulesCreate_缺少rule 测试缺少 rule 参数时返回错误。
func TestDispatchPermissionsConfigRequest_RulesCreate_缺少rule(t *testing.T) {
	ok, payload := DispatchPermissionsConfigRequest("permissions.rules.create", map[string]any{})
	if ok {
		t.Fatal("缺少 rule 应返回 ok=false")
	}
	if payload["code"] != "BAD_REQUEST" {
		t.Fatalf("code=%v，期望 BAD_REQUEST", payload["code"])
	}
}

// TestDispatchPermissionsConfigRequest_RulesUpdate_缺少patch 测试缺少 patch 参数时返回错误。
func TestDispatchPermissionsConfigRequest_RulesUpdate_缺少patch(t *testing.T) {
	ok, payload := DispatchPermissionsConfigRequest("permissions.rules.update", map[string]any{"id": "r1"})
	if ok {
		t.Fatal("缺少 patch 应返回 ok=false")
	}
	if payload["code"] != "BAD_REQUEST" {
		t.Fatalf("code=%v，期望 BAD_REQUEST", payload["code"])
	}
}

// TestParseParams 测试参数解析。
func TestParseParams(t *testing.T) {
	// 空
	if result := ParseParams(nil); result != nil {
		t.Fatalf("nil 应返回 nil，得到 %v", result)
	}
	// 有效 JSON
	result := ParseParams([]byte(`{"tool": "bash", "level": "allow"}`))
	if result == nil {
		t.Fatal("有效 JSON 应返回非 nil")
	}
	if result["tool"] != "bash" {
		t.Fatalf("tool=%v，期望 bash", result["tool"])
	}
	// 无效 JSON
	if result := ParseParams([]byte(`invalid`)); result != nil {
		t.Fatalf("无效 JSON 应返回 nil，得到 %v", result)
	}
}
