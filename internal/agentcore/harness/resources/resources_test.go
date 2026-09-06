package resources

import (
	"os"
	"path/filepath"
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

// TestParseBuiltinRules_严重等级 验证前 9 条规则的 severity 均为 CRITICAL，第 10 条无 severity
func TestParseBuiltinRules_严重等级(t *testing.T) {
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("解析内置规则失败: %v", err)
	}
	// 前 9 条规则 severity = CRITICAL（对齐 Python：前 9 条均有 severity 字段）
	for i := 0; i < 9 && i < len(rules.Rules); i++ {
		rule := rules.Rules[i]
		if rule.Severity != "CRITICAL" {
			t.Errorf("第 %d 条规则(%s)的 severity 为 %q，期望 CRITICAL", i, rule.ID, rule.Severity)
		}
	}
	// 第 10 条规则（shell_system_shutdown_or_reboot）无 severity，走 action=deny（对齐 Python）
	if len(rules.Rules) >= 10 {
		lastRule := rules.Rules[9]
		if lastRule.Severity != "" {
			t.Errorf("第 10 条规则(%s)的 severity 应为空（对齐 Python：只有 action=deny，无 severity），实际 %q", lastRule.ID, lastRule.Severity)
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

// TestParseBuiltinRulesFromFile_文件不存在 测试文件不存在
func TestParseBuiltinRulesFromFile_文件不存在(t *testing.T) {
	_, err := ParseBuiltinRulesFromFile("/nonexistent/path/rules.yaml")
	if err == nil {
		t.Errorf("文件不存在应返回错误")
	}
}

// TestParseBuiltinRulesFromFile_无效YAML 测试无效 YAML
func TestParseBuiltinRulesFromFile_无效YAML(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "bad.yaml")
	err := os.WriteFile(yamlPath, []byte("invalid: [yaml: content"), 0644)
	if err != nil {
		t.Fatalf("写入临时文件失败: %v", err)
	}
	_, err = ParseBuiltinRulesFromFile(yamlPath)
	if err == nil {
		t.Errorf("无效 YAML 应返回错误")
	}
}

// TestParseBuiltinRulesFromFile_有效YAML 测试有效 YAML
func TestParseBuiltinRulesFromFile_有效YAML(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "rules.yaml")
	content := `
rules:
  - id: test-rule
    target_tools:
      - bash
    pattern: "rm -rf /"
    severity: CRITICAL
`
	err := os.WriteFile(yamlPath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("写入临时文件失败: %v", err)
	}
	rules, err := ParseBuiltinRulesFromFile(yamlPath)
	if err != nil {
		t.Fatalf("解析有效 YAML 不应返回错误: %v", err)
	}
	if len(rules.Rules) != 1 {
		t.Errorf("应有 1 条规则，got %d", len(rules.Rules))
	}
	if rules.Rules[0].ID != "test-rule" {
		t.Errorf("规则 ID = %q, want %q", rules.Rules[0].ID, "test-rule")
	}
}

// TestGetFileModTime_文件不存在 测试文件不存在返回 -1
func TestGetFileModTime_文件不存在(t *testing.T) {
	mtime := GetFileModTime("/nonexistent/path/file.txt")
	if mtime != -1 {
		t.Errorf("文件不存在应返回 -1，got %v", mtime)
	}
}

// TestGetFileModTime_文件存在 测试文件存在返回正数
func TestGetFileModTime_文件存在(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(filePath, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("写入临时文件失败: %v", err)
	}
	mtime := GetFileModTime(filePath)
	if mtime <= 0 {
		t.Errorf("文件存在应返回正数，got %v", mtime)
	}
}
