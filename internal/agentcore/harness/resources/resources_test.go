package resources

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestParseBuiltinRules_规则数量 验证解析后规则总数为 10
func TestParseBuiltinRules_规则数量(t *testing.T) {
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("解析内置规则失败: %v", err)
	}
	if len(rules.Rules) != 10 {
		t.Errorf("期望 10 条规则，实际 %d 条", len(rules.Rules))
	}
}

// TestParseBuiltinRules_规则ID非空 验证每条规则的 ID 不为空
func TestParseBuiltinRules_规则ID非空(t *testing.T) {
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("解析内置规则失败: %v", err)
	}
	for i, rule := range rules.Rules {
		if rule.ID == "" {
			t.Errorf("第 %d 条规则的 ID 为空", i)
		}
	}
}

// TestParseBuiltinRules_严重等级 验证每条规则的 severity 均为 CRITICAL
func TestParseBuiltinRules_严重等级(t *testing.T) {
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("解析内置规则失败: %v", err)
	}
	for i, rule := range rules.Rules {
		if rule.Severity != "CRITICAL" {
			t.Errorf("第 %d 条规则(%s)的 severity 为 %q，期望 CRITICAL", i, rule.ID, rule.Severity)
		}
	}
}

// TestParseBuiltinRules_目标工具包含Bash 验证每条规则的 target_tools 包含 bash
func TestParseBuiltinRules_目标工具包含Bash(t *testing.T) {
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("解析内置规则失败: %v", err)
	}
	for i, rule := range rules.Rules {
		if !containsString(rule.TargetTools, "bash") {
			t.Errorf("第 %d 条规则(%s)的 target_tools 不包含 bash: %v", i, rule.ID, rule.TargetTools)
		}
	}
}

// TestContainsString_存在 验证目标字符串存在时返回 true
func TestContainsString_存在(t *testing.T) {
	if !containsString([]string{"a", "b", "c"}, "b") {
		t.Error("期望 containsString 返回 true")
	}
}

// TestContainsString_不存在 验证目标字符串不存在时返回 false
func TestContainsString_不存在(t *testing.T) {
	if containsString([]string{"a", "b", "c"}, "d") {
		t.Error("期望 containsString 返回 false")
	}
}

// TestContainsString_空切片 验证空切片返回 false
func TestContainsString_空切片(t *testing.T) {
	if containsString([]string{}, "a") {
		t.Error("期望空切片返回 false")
	}
}

// TestParseBuiltinRules_最后规则动作 验证最后一条规则的 action 为 deny
func TestParseBuiltinRules_最后规则动作(t *testing.T) {
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("解析内置规则失败: %v", err)
	}
	if len(rules.Rules) == 0 {
		t.Fatal("规则列表为空")
	}
	lastRule := rules.Rules[len(rules.Rules)-1]
	if lastRule.Action != "deny" {
		t.Errorf("最后一条规则(%s)的 action 为 %q，期望 deny", lastRule.ID, lastRule.Action)
	}
}
