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

// TestValidateToolsMap_有效级别 测试有效权限级别校验。
func TestValidateToolsMap_有效级别(t *testing.T) {
	result, err := validateToolsMap(map[string]any{"bash": "allow"})
	if err != nil {
		t.Fatalf("有效级别不应返回错误: %v", err)
	}
	if result["bash"] != "allow" {
		t.Fatalf("bash=%v，期望 allow", result["bash"])
	}
}

// TestValidateToolsMap_无效级别 测试无效权限级别校验。
func TestValidateToolsMap_无效级别(t *testing.T) {
	_, err := validateToolsMap(map[string]any{"bash": "invalid"})
	if err == nil {
		t.Fatal("无效级别应返回错误")
	}
}

// TestValidateToolsMap_非字典 测试非字典类型校验。
func TestValidateToolsMap_非字典(t *testing.T) {
	_, err := validateToolsMap("not a map")
	if err == nil {
		t.Fatal("非字典类型应返回错误")
	}
}

// TestValidateToolsMap_星号格式 测试 {"*": "level"} 格式。
func TestValidateToolsMap_星号格式(t *testing.T) {
	result, err := validateToolsMap(map[string]any{"bash": map[string]any{"*": "ask"}})
	if err != nil {
		t.Fatalf("星号格式不应返回错误: %v", err)
	}
	if result["bash"] != "ask" {
		t.Fatalf("bash=%v，期望 ask", result["bash"])
	}
}

// TestNormalizeRuleTools_字符串 测试字符串归一化。
func TestNormalizeRuleTools_字符串(t *testing.T) {
	result, err := normalizeRuleTools("bash")
	if err != nil {
		t.Fatalf("字符串不应返回错误: %v", err)
	}
	if len(result) != 1 || result[0] != "bash" {
		t.Fatalf("结果=%v，期望 [bash]", result)
	}
}

// TestNormalizeRuleTools_数组 测试数组归一化。
func TestNormalizeRuleTools_数组(t *testing.T) {
	result, err := normalizeRuleTools([]any{"bash", "read_file"})
	if err != nil {
		t.Fatalf("数组不应返回错误: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("长度=%d，期望 2", len(result))
	}
}

// TestNormalizeRuleTools_空字符串 测试空字符串过滤。
func TestNormalizeRuleTools_空字符串(t *testing.T) {
	result, _ := normalizeRuleTools("")
	if len(result) != 0 {
		t.Fatalf("空字符串应返回空，得到 %v", result)
	}
}

// TestNormalizeRuleSeverityAction 测试 severity/action 归一化。
func TestNormalizeRuleSeverityAction(t *testing.T) {
	rule := map[string]any{
		"severity": "high",
		"action":   "ALLOW",
	}
	normalizeRuleSeverityAction(rule)
	if rule["severity"] != "HIGH" {
		t.Fatalf("severity=%v，期望 HIGH", rule["severity"])
	}
	if rule["action"] != "allow" {
		t.Fatalf("action=%v，期望 allow", rule["action"])
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
