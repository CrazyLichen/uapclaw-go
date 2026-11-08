package resources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestParseBuiltinRules_规则数量 验证解析后规则总数为 17（对齐 Python builtin_rules.yaml）
func TestParseBuiltinRules_规则数量(t *testing.T) {
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("解析内置规则失败: %v", err)
	}
	if len(rules.Rules) != 17 {
		t.Errorf("期望 17 条规则（对齐 Python builtin_rules.yaml），实际 %d 条", len(rules.Rules))
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

// TestParseBuiltinRules_严重等级 验证 severity 取值在 Python 定义的范围内
//
// Python builtin_rules.yaml 的 17 条命令规则 severity 为 HIGH 或 CRITICAL，
// 每条都同时带 action（ask 或 deny）。判定优先级由 action 决定，
// severity 只是风险标签（见文件头注释）。
func TestParseBuiltinRules_严重等级(t *testing.T) {
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("解析内置规则失败: %v", err)
	}
	valid := map[string]bool{"HIGH": true, "CRITICAL": true, "MEDIUM": true, "LOW": true}
	for i, rule := range rules.Rules {
		if !valid[rule.Severity] {
			t.Errorf("第 %d 条规则(%s)的 severity 为 %q，不在 HIGH/CRITICAL/MEDIUM/LOW 内", i, rule.ID, rule.Severity)
		}
		if rule.Action != "ask" && rule.Action != "deny" {
			t.Errorf("第 %d 条规则(%s)的 action 为 %q，期望 ask 或 deny", i, rule.ID, rule.Action)
		}
	}
}

// TestParseBuiltinRules_目标工具为类别 验证每条规则的 target_tools 用语义类别 shell
//
// Python builtin_rules.yaml 用语义类别 `tools: [shell]` 而非具体工具名，
// 由 tool_categories.shell_tools_from_config 解析成实际工具集合。
// Go 侧 tiered_policy 的 toolCategory 需支持 "shell" 这个类别名（见 9.90b Task 3b）。
func TestParseBuiltinRules_目标工具为类别(t *testing.T) {
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("解析内置规则失败: %v", err)
	}
	for i, rule := range rules.Rules {
		if !containsString(rule.TargetTools, "shell") {
			t.Errorf("第 %d 条规则(%s)的 target_tools 不包含类别 shell: %v", i, rule.ID, rule.TargetTools)
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

// TestParseBuiltinRules_最后规则动作 验证最后一条规则为 docker privileged（action=ask）
//
// 对齐 Python builtin_rules.yaml：第 17 条 shell_docker_privileged 的
// action 为 ask（HIGH 风险，需人工确认；不是 deny）。
func TestParseBuiltinRules_最后规则动作(t *testing.T) {
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("解析内置规则失败: %v", err)
	}
	if len(rules.Rules) == 0 {
		t.Fatal("规则列表为空")
	}
	lastRule := rules.Rules[len(rules.Rules)-1]
	if lastRule.ID != "shell_docker_privileged" {
		t.Errorf("最后一条规则 id = %q，期望 shell_docker_privileged", lastRule.ID)
	}
	if lastRule.Action != "ask" {
		t.Errorf("最后一条规则(%s)的 action 为 %q，期望 ask", lastRule.ID, lastRule.Action)
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

// TestParseBuiltinRules_三个顶层段 测试 rules / sensitive_paths / net_urls 三段都能解析
func TestParseBuiltinRules_三个顶层段(t *testing.T) {
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("ParseBuiltinRules() error = %v", err)
	}
	if len(rules.Rules) != 17 {
		t.Errorf("Rules 条数 = %d, want 17", len(rules.Rules))
	}
	if len(rules.SensitivePaths) != 18 {
		t.Errorf("SensitivePaths 条数 = %d, want 18", len(rules.SensitivePaths))
	}
	if len(rules.NetUrls) != 4 {
		t.Errorf("NetUrls 条数 = %d, want 4", len(rules.NetUrls))
	}
}

// TestParseBuiltinRules_所有规则带action 测试 17 条命令规则都有 action 字段
func TestParseBuiltinRules_所有规则带action(t *testing.T) {
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("ParseBuiltinRules() error = %v", err)
	}
	for _, r := range rules.Rules {
		if r.Action == "" {
			t.Errorf("规则 %q 缺 action 字段", r.ID)
		}
	}
}

// TestParseBuiltinRules_补回的七条规则 测试补齐的 7 条规则都在
func TestParseBuiltinRules_补回的七条规则(t *testing.T) {
	want := []string{
		"shell_ps_recursive_or_forced_delete",
		"shell_registry_delete",
		"shell_chmod_world_writable",
		"shell_ld_preload_hijack",
		"shell_clear_audit_history",
		"shell_disable_firewall",
		"shell_docker_privileged",
	}
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("ParseBuiltinRules() error = %v", err)
	}
	got := make(map[string]bool, len(rules.Rules))
	for _, r := range rules.Rules {
		got[r.ID] = true
	}
	for _, id := range want {
		if !got[id] {
			t.Errorf("缺规则 %q", id)
		}
	}
}

// TestParseBuiltinRules_敏感路径带layer字段 测试 sensitive_paths 保留全部原始键
func TestParseBuiltinRules_敏感路径带layer字段(t *testing.T) {
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("ParseBuiltinRules() error = %v", err)
	}
	first := rules.SensitivePaths[0]
	if first["id"] != "home_ssh" {
		t.Errorf("首条 id = %v, want home_ssh", first["id"])
	}
	if first["action"] != "deny" {
		t.Errorf("首条 action = %v, want deny", first["action"])
	}
	if _, ok := first["path"]; !ok {
		t.Error("首条缺 path 键")
	}
}

// TestParseBuiltinRules_netUrls内容 测试 net_urls 四条全是 deny
func TestParseBuiltinRules_netUrls内容(t *testing.T) {
	rules, err := ParseBuiltinRules()
	if err != nil {
		t.Fatalf("ParseBuiltinRules() error = %v", err)
	}
	for _, k := range []string{"169.254.169.254", "metadata.google.internal", "localhost", "127.0.0.1"} {
		v, ok := rules.NetUrls[k]
		if !ok {
			t.Errorf("net_urls 缺键 %q", k)
			continue
		}
		if v != "deny" {
			t.Errorf("net_urls[%q] = %v, want deny", k, v)
		}
	}
}

// TestResolveBuiltinRulesYAMLPath 测试包内 YAML 路径可解析且存在
func TestResolveBuiltinRulesYAMLPath(t *testing.T) {
	path := ResolveBuiltinRulesYAMLPath()
	if path == "" {
		t.Fatal("ResolveBuiltinRulesYAMLPath() 返回空串，应能定位到 builtin_rules.yaml")
	}
	if !strings.HasSuffix(path, "builtin_rules.yaml") {
		t.Errorf("path = %q，应以 builtin_rules.yaml 结尾", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("解析出的路径不存在: %v", err)
	}
	// 与硬编码的包目录一致
	if filepath.Base(filepath.Dir(path)) != "resources" {
		t.Errorf("父目录 = %q, want resources", filepath.Base(filepath.Dir(path)))
	}
}

// TestGetFileModTime 测试 mtime 读取与缺失文件的 -1 回落
func TestGetFileModTime(t *testing.T) {
	path := ResolveBuiltinRulesYAMLPath()
	if path == "" {
		t.Skip("无法定位 YAML")
	}
	if got := GetFileModTime(path); got <= 0 {
		t.Errorf("GetFileModTime(存在文件) = %v, want > 0", got)
	}
	if got := GetFileModTime(filepath.Join(t.TempDir(), "no_such_file")); got != -1 {
		t.Errorf("GetFileModTime(缺失文件) = %v, want -1", got)
	}
}
