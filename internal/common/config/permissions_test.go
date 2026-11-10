package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// setupPermissionsConfig 把配置文件指到临时目录并写入初始内容。
func setupPermissionsConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("UAPCLAW_CONFIG_YAML", path)
	return path
}

// readConfigFile 读回配置文件顶层 map。
func readConfigFile(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var data map[string]any
	if err := yaml.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return data
}

// TestGetPermissionsTools_读到permissions节点 测试读取路径正确（bug 回归）。
//
// 迁移前的实现读 cfg["tools"]（顶层），漏了 ["permissions"] 这一层，
// 导致前端权限工具列表读到空数据。
func TestGetPermissionsTools_读到permissions节点(t *testing.T) {
	setupPermissionsConfig(t, `
permissions:
  tools:
    bash: allow
    write_file: ask
`)
	got := GetPermissionsTools()
	tools, _ := got[toolsSectionKey].(map[string]any)
	if tools == nil {
		t.Fatal("tools 为 nil，说明读错了配置路径（应为 permissions.tools 而非顶层 tools）")
	}
	if tools["bash"] != "allow" {
		t.Errorf("bash = %v, want allow", tools["bash"])
	}
	if tools["write_file"] != "ask" {
		t.Errorf("write_file = %v, want ask", tools["write_file"])
	}
}

// TestGetPermissionsTools_顶层tools不被误读 测试不会读到顶层 tools。
func TestGetPermissionsTools_顶层tools不被误读(t *testing.T) {
	setupPermissionsConfig(t, `
tools:
  decoy: allow
permissions:
  tools:
    bash: deny
`)
	got := GetPermissionsTools()
	tools, _ := got[toolsSectionKey].(map[string]any)
	if _, exists := tools["decoy"]; exists {
		t.Error("读到了顶层 tools 的 decoy，说明路径仍然是顶层")
	}
	if tools["bash"] != "deny" {
		t.Errorf("bash = %v, want deny", tools["bash"])
	}
}

// TestGetPermissionsTools_空配置返回空map 测试兜底。
func TestGetPermissionsTools_空配置返回空map(t *testing.T) {
	setupPermissionsConfig(t, "permissions: {}\n")
	got := GetPermissionsTools()
	tools, _ := got[toolsSectionKey].(map[string]any)
	if tools == nil {
		t.Fatal("tools 为 nil")
	}
	if len(tools) != 0 {
		t.Errorf("tools len = %d, want 0", len(tools))
	}
}

// TestGetPermissionsRules_读到permissions节点 测试读取路径正确（bug 回归）。
func TestGetPermissionsRules_读到permissions节点(t *testing.T) {
	setupPermissionsConfig(t, `
permissions:
  rules:
    - id: r1
      tools: [bash]
      action: deny
`)
	got := GetPermissionsRules()
	rules, _ := got[rulesSectionKey].([]any)
	if len(rules) != 1 {
		t.Fatalf("rules len = %d, want 1（应为 permissions.rules 而非顶层 rules）", len(rules))
	}
	first, _ := rules[0].(map[string]any)
	if first["id"] != "r1" {
		t.Errorf("id = %v, want r1", first["id"])
	}
}

// TestGetPermissionsRules_过滤非dict项 测试只保留 dict。
func TestGetPermissionsRules_过滤非dict项(t *testing.T) {
	setupPermissionsConfig(t, `
permissions:
  rules:
    - id: r1
    - plain_string
    - id: r2
`)
	got := GetPermissionsRules()
	rules, _ := got[rulesSectionKey].([]any)
	if len(rules) != 2 {
		t.Errorf("rules len = %d, want 2（字符串项应被过滤）", len(rules))
	}
}

// TestGetPermissionsApprovalOverrides_读取节点 测试路径。
func TestGetPermissionsApprovalOverrides_读取节点(t *testing.T) {
	setupPermissionsConfig(t, `
permissions:
  approval_overrides:
    - id: o1
      tools: [bash]
`)
	got := GetPermissionsApprovalOverrides()
	overrides, _ := got[approvalOverridesSectionKey].([]any)
	if len(overrides) != 1 {
		t.Fatalf("overrides len = %d, want 1", len(overrides))
	}
}

// TestUpdatePermissionsToolInConfig_合并单键 测试写入与读回一致。
func TestUpdatePermissionsToolInConfig_合并单键(t *testing.T) {
	path := setupPermissionsConfig(t, "permissions:\n  tools:\n    bash: ask\n")

	got, err := UpdatePermissionsToolInConfig("write_file", "allow")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	tools, _ := got[toolsSectionKey].(map[string]any)
	if tools["write_file"] != "allow" {
		t.Errorf("返回 tools.write_file = %v, want allow", tools["write_file"])
	}
	if tools["bash"] != "ask" {
		t.Errorf("已有键 bash 应保留，实际 %v", tools["bash"])
	}

	// 读回验证落盘
	reloaded := GetPermissionsTools()
	reloadedTools, _ := reloaded[toolsSectionKey].(map[string]any)
	if reloadedTools["write_file"] != "allow" {
		t.Error("配置未落盘")
	}
	_ = readConfigFile(t, path)
}

// TestUpdatePermissionsToolInConfig_空名字报错 测试参数校验。
func TestUpdatePermissionsToolInConfig_空名字报错(t *testing.T) {
	setupPermissionsConfig(t, "permissions: {}\n")
	if _, err := UpdatePermissionsToolInConfig("  ", "allow"); err == nil {
		t.Fatal("err = nil, want error（空工具名应报错）")
	}
}

// TestUpdatePermissionsToolInConfig_非法级别报错 测试级别校验。
func TestUpdatePermissionsToolInConfig_非法级别报错(t *testing.T) {
	setupPermissionsConfig(t, "permissions: {}\n")
	if _, err := UpdatePermissionsToolInConfig("bash", "maybe"); err == nil {
		t.Fatal("err = nil, want error（maybe 不是合法级别）")
	}
}

// TestDeletePermissionsToolInConfig_键不存在返回false 测试幂等。
func TestDeletePermissionsToolInConfig_键不存在返回false(t *testing.T) {
	setupPermissionsConfig(t, "permissions:\n  tools:\n    bash: ask\n")
	ok, err := DeletePermissionsToolInConfig("nonexistent")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok {
		t.Error("ok = true, want false")
	}
}

// TestDeletePermissionsToolInConfig_删除成功 测试删除。
func TestDeletePermissionsToolInConfig_删除成功(t *testing.T) {
	setupPermissionsConfig(t, "permissions:\n  tools:\n    bash: ask\n    ls: allow\n")
	ok, err := DeletePermissionsToolInConfig("bash")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	reloaded := GetPermissionsTools()
	tools, _ := reloaded[toolsSectionKey].(map[string]any)
	if _, exists := tools["bash"]; exists {
		t.Error("bash 应已删除")
	}
	if tools["ls"] != "allow" {
		t.Error("ls 应保留")
	}
}

// TestDeletePermissionsToolInConfig_空名字报错 测试参数校验。
func TestDeletePermissionsToolInConfig_空名字报错(t *testing.T) {
	setupPermissionsConfig(t, "permissions: {}\n")
	if _, err := DeletePermissionsToolInConfig(""); err == nil {
		t.Fatal("err = nil, want error")
	}
}

// TestReplacePermissionsToolsInConfig_整表替换 测试替换语义。
func TestReplacePermissionsToolsInConfig_整表替换(t *testing.T) {
	setupPermissionsConfig(t, "permissions:\n  tools:\n    bash: ask\n    ls: allow\n")
	err := ReplacePermissionsToolsInConfig(map[string]any{"curl": "deny"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	reloaded := GetPermissionsTools()
	tools, _ := reloaded[toolsSectionKey].(map[string]any)
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1（整表替换）", len(tools))
	}
	if tools["curl"] != "deny" {
		t.Errorf("curl = %v, want deny", tools["curl"])
	}
}

// TestReplacePermissionsToolsInConfig_非法级别报错 测试校验。
func TestReplacePermissionsToolsInConfig_非法级别报错(t *testing.T) {
	setupPermissionsConfig(t, "permissions: {}\n")
	if err := ReplacePermissionsToolsInConfig(map[string]any{"bash": "maybe"}); err == nil {
		t.Fatal("err = nil, want error（maybe 不是合法级别）")
	}
}

// TestValidateToolsMap_legacy星号写法 测试 {"*": level} 兼容。
func TestValidateToolsMap_legacy星号写法(t *testing.T) {
	got, err := ValidateToolsMap(map[string]any{
		"bash": map[string]any{"*": "allow"},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got["bash"] != "allow" {
		t.Errorf("bash = %v, want allow", got["bash"])
	}
}

// TestCreatePermissionsRuleInConfig_生成id 测试创建。
func TestCreatePermissionsRuleInConfig_生成id(t *testing.T) {
	setupPermissionsConfig(t, "permissions:\n  rules: []\n")
	got, err := CreatePermissionsRuleInConfig(map[string]any{
		"tools":  []any{"bash"},
		"action": "deny",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if id, _ := got["id"].(string); id == "" {
		t.Error("id 为空，应自动生成")
	}
}

// TestCreatePermissionsRuleInConfig_保留已有id 测试不覆盖显式 id。
func TestCreatePermissionsRuleInConfig_保留已有id(t *testing.T) {
	setupPermissionsConfig(t, "permissions:\n  rules: []\n")
	got, err := CreatePermissionsRuleInConfig(map[string]any{"id": "my-rule"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got["id"] != "my-rule" {
		t.Errorf("id = %v, want my-rule", got["id"])
	}
}

// TestUpdatePermissionsRuleInConfig_打补丁 测试按 id 更新。
func TestUpdatePermissionsRuleInConfig_打补丁(t *testing.T) {
	setupPermissionsConfig(t, `
permissions:
  rules:
    - id: r1
      tools: [bash]
      action: ask
`)
	got, err := UpdatePermissionsRuleInConfig("r1", map[string]any{"action": "deny"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got["action"] != "deny" {
		t.Errorf("action = %v, want deny", got["action"])
	}
	if got["id"] != "r1" {
		t.Errorf("id 应保持不变，实际 %v", got["id"])
	}

	rules := GetPermissionsRules()
	list, _ := rules[rulesSectionKey].([]any)
	first, _ := list[0].(map[string]any)
	if first["action"] != "deny" {
		t.Error("补丁未落盘")
	}
}

// TestUpdatePermissionsRuleInConfig_不存在报错 测试找不到规则。
func TestUpdatePermissionsRuleInConfig_不存在报错(t *testing.T) {
	setupPermissionsConfig(t, "permissions:\n  rules: []\n")
	if _, err := UpdatePermissionsRuleInConfig("nope", map[string]any{}); err == nil {
		t.Fatal("err = nil, want error")
	}
}

// TestUpdatePermissionsRuleInConfig_空id报错 测试参数校验。
func TestUpdatePermissionsRuleInConfig_空id报错(t *testing.T) {
	setupPermissionsConfig(t, "permissions:\n  rules: []\n")
	if _, err := UpdatePermissionsRuleInConfig("  ", map[string]any{}); err == nil {
		t.Fatal("err = nil, want error")
	}
}

// TestDeletePermissionsRuleInConfig_按id删除 测试删除。
func TestDeletePermissionsRuleInConfig_按id删除(t *testing.T) {
	setupPermissionsConfig(t, `
permissions:
  rules:
    - id: r1
      tools: [bash]
    - id: r2
      tools: [ls]
`)
	ok, err := DeletePermissionsRuleInConfig("r1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	got := GetPermissionsRules()
	rules, _ := got[rulesSectionKey].([]any)
	if len(rules) != 1 {
		t.Fatalf("rules len = %d, want 1", len(rules))
	}
	remaining, _ := rules[0].(map[string]any)
	if remaining["id"] != "r2" {
		t.Errorf("remaining id = %v, want r2", remaining["id"])
	}
}

// TestDeletePermissionsRuleInConfig_不存在返回false 测试幂等。
func TestDeletePermissionsRuleInConfig_不存在返回false(t *testing.T) {
	setupPermissionsConfig(t, "permissions:\n  rules: []\n")
	ok, err := DeletePermissionsRuleInConfig("nope")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok {
		t.Error("ok = true, want false")
	}
}

// TestDeletePermissionsRuleInConfig_空id报错 测试参数校验。
func TestDeletePermissionsRuleInConfig_空id报错(t *testing.T) {
	setupPermissionsConfig(t, "permissions:\n  rules: []\n")
	if _, err := DeletePermissionsRuleInConfig(""); err == nil {
		t.Fatal("err = nil, want error")
	}
}

// TestDeletePermissionsApprovalOverrideInConfig_按id删除 测试删除。
func TestDeletePermissionsApprovalOverrideInConfig_按id删除(t *testing.T) {
	setupPermissionsConfig(t, `
permissions:
  approval_overrides:
    - id: o1
      tools: [bash]
`)
	ok, err := DeletePermissionsApprovalOverrideInConfig("o1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	got := GetPermissionsApprovalOverrides()
	overrides, _ := got[approvalOverridesSectionKey].([]any)
	if len(overrides) != 0 {
		t.Errorf("overrides len = %d, want 0", len(overrides))
	}
}

// TestDeletePermissionsApprovalOverrideInConfig_不存在返回false 测试幂等。
func TestDeletePermissionsApprovalOverrideInConfig_不存在返回false(t *testing.T) {
	setupPermissionsConfig(t, "permissions:\n  approval_overrides: []\n")
	ok, err := DeletePermissionsApprovalOverrideInConfig("nope")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok {
		t.Error("ok = true, want false")
	}
}

// TestDeletePermissionsApprovalOverrideInConfig_空id报错 测试参数校验。
func TestDeletePermissionsApprovalOverrideInConfig_空id报错(t *testing.T) {
	setupPermissionsConfig(t, "permissions: {}\n")
	if _, err := DeletePermissionsApprovalOverrideInConfig("  "); err == nil {
		t.Fatal("err = nil, want error")
	}
}

// TestUpdatePermissionsOwnerScopesInConfig 测试 owner_scopes 写入。
func TestUpdatePermissionsOwnerScopesInConfig(t *testing.T) {
	path := setupPermissionsConfig(t, "permissions: {}\n")
	scopes := map[string]any{
		"web": map[string]any{
			"u1": map[string]any{"tools": map[string]any{"bash": "allow"}},
		},
	}
	if err := UpdatePermissionsOwnerScopesInConfig(scopes); err != nil {
		t.Fatalf("err = %v", err)
	}
	data := readConfigFile(t, path)
	perms, _ := data[permissionsSectionKey].(map[string]any)
	if perms[ownerScopesSectionKey] == nil {
		t.Error("owner_scopes 未写入")
	}
}

// TestReadPermissionsConfigFile_文件不存在返回空map 测试兜底。
func TestReadPermissionsConfigFile_文件不存在返回空map(t *testing.T) {
	t.Setenv("UAPCLAW_CONFIG_YAML", filepath.Join(t.TempDir(), "missing.yaml"))
	got := readPermissionsConfigFile()
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// TestReadPermissionsConfigFile_非法YAML返回空map 测试解析失败兜底。
func TestReadPermissionsConfigFile_非法YAML返回空map(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("not: [valid: yaml"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("UAPCLAW_CONFIG_YAML", path)
	got := readPermissionsConfigFile()
	if len(got) != 0 {
		t.Errorf("len = %d, want 0（非法 YAML 应返回空 map）", len(got))
	}
}

// TestRandomHex_长度正确 测试随机串长度。
func TestRandomHex_长度正确(t *testing.T) {
	if got := randomHex(8); len(got) != 16 {
		t.Errorf("len = %d, want 16（8 字节 = 16 个十六进制字符）", len(got))
	}
}

// TestPermissionsConfigFilePath_走默认路径 测试未设环境变量时回落默认路径。
//
// 只断言不 panic 且返回非空（默认路径依赖 HOME，测试环境可能有值）。
func TestPermissionsConfigFilePath_走默认路径(t *testing.T) {
	t.Setenv("UAPCLAW_CONFIG_YAML", "")
	got := permissionsConfigFilePath()
	if got == "" {
		t.Error("默认配置路径为空")
	}
}

// TestReadPermissionsConfigFromPath_空串走默认路径 测试默认路径分支。
func TestReadPermissionsConfigFromPath_空串走默认路径(t *testing.T) {
	// 默认路径可能不存在，只验证不 panic 且返回 map
	got := readPermissionsConfigFromPath("")
	if got == nil {
		t.Error("返回值不应为 nil")
	}
}

// TestWritePermissionsConfigFile_无路径报错 测试路径缺失时返回错误。
func TestWritePermissionsConfigFile_无路径报错(t *testing.T) {
	t.Setenv("UAPCLAW_CONFIG_YAML", "")
	// 默认路径由 HOME 决定，若为空则报错。这里只验证函数不 panic。
	_ = writePermissionsConfigFile(map[string]any{})
}

// TestWritePermissionsConfigFile_写入成功 测试正常写入。
func TestWritePermissionsConfigFile_写入成功(t *testing.T) {
	path := setupPermissionsConfig(t, "permissions: {}\n")
	if err := writePermissionsConfigFile(map[string]any{"foo": "bar"}); err != nil {
		t.Fatalf("err = %v", err)
	}
	data := readConfigFile(t, path)
	if data["foo"] != "bar" {
		t.Errorf("foo = %v, want bar", data["foo"])
	}
}

// TestDeletePermissionsToolInConfig_perms为空 测试无 permissions 节时返回 false。
func TestDeletePermissionsToolInConfig_perms为空(t *testing.T) {
	setupPermissionsConfig(t, "other: 1\n")
	ok, err := DeletePermissionsToolInConfig("bash")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok {
		t.Error("ok = true, want false")
	}
}

// TestDeletePermissionsRuleInConfig_perms为空 测试无 permissions 节时返回 false。
func TestDeletePermissionsRuleInConfig_perms为空(t *testing.T) {
	setupPermissionsConfig(t, "other: 1\n")
	ok, err := DeletePermissionsRuleInConfig("r1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok {
		t.Error("ok = true, want false")
	}
}

// TestDeletePermissionsApprovalOverrideInConfig_perms为空 测试无 permissions 节。
func TestDeletePermissionsApprovalOverrideInConfig_perms为空(t *testing.T) {
	setupPermissionsConfig(t, "other: 1\n")
	ok, err := DeletePermissionsApprovalOverrideInConfig("o1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok {
		t.Error("ok = true, want false")
	}
}

// TestUpdatePermissionsOwnerScopesInConfig_无permissions节 测试自动建节。
func TestUpdatePermissionsOwnerScopesInConfig_无permissions节(t *testing.T) {
	path := setupPermissionsConfig(t, "other: 1\n")
	if err := UpdatePermissionsOwnerScopesInConfig(map[string]any{"web": map[string]any{}}); err != nil {
		t.Fatalf("err = %v", err)
	}
	data := readConfigFile(t, path)
	perms, _ := data[permissionsSectionKey].(map[string]any)
	if perms == nil {
		t.Fatal("permissions 节未自动创建")
	}
	if perms[ownerScopesSectionKey] == nil {
		t.Error("owner_scopes 未写入")
	}
}

// TestReplacePermissionsToolsInConfig_无permissions节 测试自动建节。
func TestReplacePermissionsToolsInConfig_无permissions节(t *testing.T) {
	path := setupPermissionsConfig(t, "other: 1\n")
	if err := ReplacePermissionsToolsInConfig(map[string]any{"bash": "allow"}); err != nil {
		t.Fatalf("err = %v", err)
	}
	data := readConfigFile(t, path)
	perms, _ := data[permissionsSectionKey].(map[string]any)
	tools, _ := perms[toolsSectionKey].(map[string]any)
	if tools["bash"] != "allow" {
		t.Errorf("bash = %v, want allow", tools["bash"])
	}
}

// TestCreatePermissionsRuleInConfig_无permissions节 测试自动建节。
func TestCreatePermissionsRuleInConfig_无permissions节(t *testing.T) {
	path := setupPermissionsConfig(t, "other: 1\n")
	if _, err := CreatePermissionsRuleInConfig(map[string]any{"id": "r1"}); err != nil {
		t.Fatalf("err = %v", err)
	}
	data := readConfigFile(t, path)
	perms, _ := data[permissionsSectionKey].(map[string]any)
	rules, _ := perms[rulesSectionKey].([]any)
	if len(rules) != 1 {
		t.Errorf("rules len = %d, want 1", len(rules))
	}
}

// TestReadPermissionsConfigFile_空UAPCLAW但默认路径存在 测试回落默认路径读取。
func TestReadPermissionsConfigFile_空UAPCLAW但默认路径存在(t *testing.T) {
	t.Setenv("UAPCLAW_CONFIG_YAML", "")
	// 只验证不 panic
	_ = readPermissionsConfigFile()
}

// TestRandomHex_两次不同 测试随机性。
func TestRandomHex_两次不同(t *testing.T) {
	first := randomHex(8)
	second := randomHex(8)
	if first == second {
		t.Error("两次随机串相同，可能未真正随机")
	}
}
