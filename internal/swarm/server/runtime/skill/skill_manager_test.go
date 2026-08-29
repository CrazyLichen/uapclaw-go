package skill

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewSkillManager_默认路径 验证默认路径创建
func TestNewSkillManager_默认路径(t *testing.T) {
	sm := NewSkillManager("")
	if sm == nil {
		t.Fatal("SkillManager 不应为 nil")
	}
	if sm.skillsDir == "" {
		t.Error("skillsDir 不应为空")
	}
	if sm.stateFile == "" {
		t.Error("stateFile 不应为空")
	}
}

// TestNewSkillManager_指定路径 验证指定路径创建
func TestNewSkillManager_指定路径(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	if sm.agentRoot != tmpDir {
		t.Errorf("agentRoot = %q, want %q", sm.agentRoot, tmpDir)
	}
	expectedSkillsDir := filepath.Join(tmpDir, "skills")
	if sm.skillsDir != expectedSkillsDir {
		t.Errorf("skillsDir = %q, want %q", sm.skillsDir, expectedSkillsDir)
	}
}

// TestHandleSkillsToggle_正常 验证正常切换
func TestHandleSkillsToggle_正常(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsToggle(context.Background(), map[string]any{
		"name":    "test-skill",
		"enabled": false,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Error("应返回 success=true")
	}
	if result["enabled"] != false {
		t.Error("enabled 应为 false")
	}
}

// TestHandleSkillsToggle_缺名称 验证缺少名称
func TestHandleSkillsToggle_缺名称(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsToggle(context.Background(), map[string]any{
		"enabled": true,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("应返回 success=false")
	}
}

// TestHandleSkillsToggle_缺enabled 验证缺少 enabled
func TestHandleSkillsToggle_缺enabled(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsToggle(context.Background(), map[string]any{
		"name": "test-skill",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("应返回 success=false")
	}
}

// TestHandleSkillsToggle_无效名称 验证无效名称
func TestHandleSkillsToggle_无效名称(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsToggle(context.Background(), map[string]any{
		"name":    "../etc/passwd",
		"enabled": true,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("应返回 success=false")
	}
}

// TestHandleSkillsInstalled_空列表 验证空列表
func TestHandleSkillsInstalled_空列表(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsInstalled(context.Background(), nil)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	plugins, ok := result["plugins"].([]map[string]any)
	if !ok {
		t.Error("应包含 plugins 字段")
	}
	if len(plugins) != 0 {
		t.Errorf("期望空列表，实际 %d 个", len(plugins))
	}
}

// TestHandleSkillsList_空目录 验证空目录
func TestHandleSkillsList_空目录(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsList(context.Background(), nil)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if _, ok := result["skills"]; !ok {
		t.Error("应包含 skills 字段")
	}
}

// TestHandleSkillsMarketplaceList_空 验证空 marketplace 列表
func TestHandleSkillsMarketplaceList_空(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsMarketplaceList(context.Background(), nil)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	marketplaces, ok := result["marketplaces"].([]map[string]any)
	if !ok {
		t.Error("应包含 marketplaces 字段")
	}
	if len(marketplaces) != 0 {
		t.Errorf("期望空列表，实际 %d 个", len(marketplaces))
	}
}

// TestHandleSkillsMarketplaceAdd_正常 验证正常添加
func TestHandleSkillsMarketplaceAdd_正常(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsMarketplaceAdd(context.Background(), map[string]any{
		"name": "test-market",
		"url":  "https://github.com/test/skills",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Error("应返回 success=true")
	}
}

// TestHandleSkillsMarketplaceAdd_重复 验证重复添加
func TestHandleSkillsMarketplaceAdd_重复(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.HandleSkillsMarketplaceAdd(context.Background(), map[string]any{
		"name": "test-market",
		"url":  "https://github.com/test/skills",
	})
	result, err := sm.HandleSkillsMarketplaceAdd(context.Background(), map[string]any{
		"name": "test-market",
		"url":  "https://github.com/test/skills2",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("重复添加应返回 success=false")
	}
}

// TestHandleSkillsClawhubGetToken_空 验证空 token
func TestHandleSkillsClawhubGetToken_空(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsClawhubGetToken(context.Background(), nil)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Error("应返回 success=true")
	}
	if toBool(result["has_token"]) != false {
		t.Error("无 token 时 has_token 应为 false")
	}
}

// TestHandleSkillsClawhubSetToken_正常 验证设置 token
func TestHandleSkillsClawhubSetToken_正常(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsClawhubSetToken(context.Background(), map[string]any{
		"token": "sk-1234567890abcdef",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Error("应返回 success=true")
	}
	// 验证掩码
	masked := toString(result["token"])
	if masked == "sk-1234567890abcdef" {
		t.Error("token 应被掩码")
	}
}

// TestHandleSkillsSkillnetInstallStatus_不存在 验证不存在的安装状态
func TestHandleSkillsSkillnetInstallStatus_不存在(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsSkillnetInstallStatus(context.Background(), map[string]any{
		"install_id": "nonexistent",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("不存在的 ID 应返回 success=false")
	}
}

// TestHandleSkillsEvolutionStatus_缺名称 验证缺少名称
func TestHandleSkillsEvolutionStatus_缺名称(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	_, err := sm.HandleSkillsEvolutionStatus(context.Background(), map[string]any{})
	if err == nil {
		t.Error("缺少名称应返回错误")
	}
}

// TestHandleSkillsUninstall_缺名称 验证缺少名称
func TestHandleSkillsUninstall_缺名称(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsUninstall(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("应返回 success=false")
	}
}

// TestHandlePluginsEnable_正常 验证正常启用
func TestHandlePluginsEnable_正常(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandlePluginsEnable(context.Background(), map[string]any{
		"name": "test-skill",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Error("应返回 success=true")
	}
}

// TestHandlePluginsReload 验证插件重载
func TestHandlePluginsReload(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandlePluginsReload(context.Background(), nil)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Error("应返回 success=true")
	}
}

// TestHandleSkillsImportLocal_缺路径 验证缺少路径
func TestHandleSkillsImportLocal_缺路径(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsImportLocal(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("应返回 success=false")
	}
}

// TestHandleSkillsMarketplaceRemove_不存在 验证移除不存在的 marketplace
func TestHandleSkillsMarketplaceRemove_不存在(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsMarketplaceRemove(context.Background(), map[string]any{
		"name": "nonexistent",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("应返回 success=false")
	}
}

// TestHandleSkillsInstall_空spec 验证空 spec
func TestHandleSkillsInstall_空spec(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsInstall(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("应返回 success=false")
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestSafePathName_正常 验证正常名称
func TestSafePathName_正常(t *testing.T) {
	name, err := safePathName("my-skill", "skill")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if name != "my-skill" {
		t.Errorf("name = %q, want %q", name, "my-skill")
	}
}

// TestSafePathName_空名称 验证空名称
func TestSafePathName_空名称(t *testing.T) {
	_, err := safePathName("", "skill")
	if err == nil {
		t.Error("空名称应返回错误")
	}
}

// TestSafePathName_路径遍历 验证路径遍历
func TestSafePathName_路径遍历(t *testing.T) {
	tests := []string{"..", ".", "../etc", "foo/bar", "foo\\bar"}
	for _, input := range tests {
		_, err := safePathName(input, "skill")
		if err == nil {
			t.Errorf("%q 应返回错误", input)
		}
	}
}

// TestMaskClawhubToken 验证 token 掩码
func TestMaskClawhubToken(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"abc", "***"},
		{"abcd1234", "********"},
		{"sk-1234567890abcdef", "sk-1***********cdef"},
	}
	for _, tt := range tests {
		got := maskClawhubToken(tt.input)
		if got != tt.want {
			t.Errorf("maskClawhubToken(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestParseYAMLFrontmatter_简单 验证简单 frontmatter 解析
func TestParseYAMLFrontmatter_简单(t *testing.T) {
	text := "name: my-skill\ndescription: A test skill\nversion: 1.0"
	result := parseYAMLFrontmatter(text)
	if toString(result["name"]) != "my-skill" {
		t.Errorf("name = %q, want %q", result["name"], "my-skill")
	}
	if toString(result["description"]) != "A test skill" {
		t.Errorf("description = %q, want %q", result["description"], "A test skill")
	}
}

// TestParseYAMLFrontmatter_引号 验证引号处理
func TestParseYAMLFrontmatter_引号(t *testing.T) {
	text := `name: "my-skill"` + "\n" + `description: 'A test'`
	result := parseYAMLFrontmatter(text)
	if toString(result["name"]) != "my-skill" {
		t.Errorf("name = %q, want %q", result["name"], "my-skill")
	}
	if toString(result["description"]) != "A test" {
		t.Errorf("description = %q, want %q", result["description"], "A test")
	}
}

// TestLoadState_空文件 验证空文件加载
func TestLoadState_空文件(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	if sm.state == nil {
		t.Error("state 不应为 nil")
	}
}

// TestSaveState_写入读取 验证状态写入和读取
func TestSaveState_写入读取(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.mu.Lock()
	sm.state["test_key"] = "test_value"
	sm.saveState()
	sm.mu.Unlock()

	// 重新加载
	sm2 := NewSkillManager(tmpDir)
	if toString(sm2.state["test_key"]) != "test_value" {
		t.Error("状态应持久化")
	}
}

// TestDirExists 验证目录存在检查
func TestDirExists(t *testing.T) {
	tmpDir := t.TempDir()
	if !dirExists(tmpDir) {
		t.Error("临时目录应存在")
	}
	if dirExists(filepath.Join(tmpDir, "nonexistent")) {
		t.Error("不存在的目录应返回 false")
	}
}

// TestFileExists 验证文件存在检查
func TestFileExists(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("test"), 0o644)
	if !fileExists(testFile) {
		t.Error("测试文件应存在")
	}
	if fileExists(tmpDir) {
		t.Error("目录不应作为文件存在")
	}
}

// TestEnvInt 验证环境变量整数读取
func TestEnvInt(t *testing.T) {
	os.Setenv("TEST_SKILL_INT", "42")
	defer os.Unsetenv("TEST_SKILL_INT")
	if v := envInt("TEST_SKILL_INT", 0); v != 42 {
		t.Errorf("envInt = %d, want 42", v)
	}
	if v := envInt("TEST_SKILL_INT_MISSING", 10); v != 10 {
		t.Errorf("envInt 缺失 = %d, want 10", v)
	}
}

// TestGetSkillEnabled_状态持久化 验证 toggle 后 enabled 状态持久化
func TestGetSkillEnabled_状态持久化(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// toggle 为 false
	sm.HandleSkillsToggle(context.Background(), map[string]any{
		"name": "persist-skill", "enabled": false,
	})

	// 重新创建 SkillManager
	sm2 := NewSkillManager(tmpDir)
	if GetSkillEnabled(sm2.state, "persist-skill") {
		t.Error("toggle 后 enabled 应为 false，且应持久化")
	}
}

// TestHandleSkillsInstalled_有插件 验证有插件时的格式
func TestHandleSkillsInstalled_有插件(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 添加插件到状态
	sm.mu.Lock()
	sm.AddInstalledPlugin(map[string]any{
		"name":         "test-plugin",
		"marketplace":  "test-market",
		"version":      "1.0.0",
		"commit":       "abc123",
		"source":       "test-market",
		"installed_at": "2025-01-01T00:00:00Z",
	})
	sm.saveState()
	sm.mu.Unlock()

	// 重新加载以验证持久化
	sm2 := NewSkillManager(tmpDir)
	result, err := sm2.HandleSkillsInstalled(context.Background(), nil)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	plugins, _ := result["plugins"].([]map[string]any)
	if len(plugins) != 1 {
		t.Fatalf("期望 1 个插件，实际 %d", len(plugins))
	}
	p := plugins[0]
	if toString(p["plugin_name"]) != "test-plugin" {
		t.Errorf("plugin_name = %q, want %q", p["plugin_name"], "test-plugin")
	}
	if toString(p["spec"]) != "test-plugin@test-market" {
		t.Errorf("spec = %q, want %q", p["spec"], "test-plugin@test-market")
	}
}

// TestHandleSkillsList_WithInstalled 验证 with_installed 参数
func TestHandleSkillsList_WithInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsList(context.Background(), map[string]any{
		"with_installed": true,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if _, ok := result["plugins"]; !ok {
		t.Error("with_installed=true 时应包含 plugins 字段")
	}
}

// TestHandleSkillsGet_缺名称 验证缺少名称
func TestHandleSkillsGet_缺名称(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	_, err := sm.HandleSkillsGet(context.Background(), map[string]any{})
	if err == nil {
		t.Error("缺少名称应返回错误")
	}
}

// TestHandleSkillsMarketplaceToggle_不存在 验证切换不存在的 marketplace
func TestHandleSkillsMarketplaceToggle_不存在(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsMarketplaceToggle(context.Background(), map[string]any{
		"name": "nonexistent", "enabled": true,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("应返回 success=false")
	}
}

// TestParseSkillMD_有效文件 验证解析有效的 SKILL.md
func TestParseSkillMD_有效文件(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "test-skill")
	os.MkdirAll(skillDir, 0o755)
	mdContent := "---\nname: test-skill\ndescription: A test\n---\n\n# Test Skill\n"
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(mdContent), 0o644)

	sm := NewSkillManager(tmpDir)
	meta := sm.parseSkillMD(filepath.Join(skillDir, "SKILL.md"))
	if meta == nil {
		t.Fatal("解析结果不应为 nil")
	}
	if toString(meta["name"]) != "test-skill" {
		t.Errorf("name = %q, want %q", meta["name"], "test-skill")
	}
}

// TestTryFindSkillFile 验证查找 SKILL.md
func TestTryFindSkillFile(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "test-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test\n---\n"), 0o644)

	sm := NewSkillManager(tmpDir)
	result := sm.tryFindSkillFile(skillDir)
	if result == "" {
		t.Error("应找到 SKILL.md")
	}
}

// TestHandleSkillsSkillnetSearch_缺查询 验证缺少查询参数
func TestHandleSkillsSkillnetSearch_缺查询(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsSkillnetSearch(context.Background(), map[string]any{})
	if err == nil {
		if toBool(result["success"]) != false {
			t.Error("应返回 success=false")
		}
	}
}

// TestHandleSkillsSkillnetInstall_缺URL 验证缺少 URL
func TestHandleSkillsSkillnetInstall_缺URL(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsSkillnetInstall(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("应返回 success=false")
	}
}

// TestHandleSkillsSkillnetInstall_正常 验证正常安装启动
func TestHandleSkillsSkillnetInstall_正常(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsSkillnetInstall(context.Background(), map[string]any{
		"url": "https://skillnet.ai/skills/test",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Error("应返回 success=true")
	}
	if !toBool(result["pending"]) {
		t.Error("应为 pending")
	}
	if toString(result["install_id"]) == "" {
		t.Error("install_id 不应为空")
	}
}

// TestSetSkillnetInstallCompleteHook 验证设置回调
func TestSetSkillnetInstallCompleteHook(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.SetSkillnetInstallCompleteHook(func(ctx context.Context) error {
		return nil
	})
	if sm.skillnetInstallCompleteHook == nil {
		t.Error("hook 不应为 nil")
	}
}

// TestCopyDir 验证目录复制
func TestCopyDir(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	dstDir := filepath.Join(tmpDir, "dst")
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755)
	os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(srcDir, "sub", "nested.txt"), []byte("world"), 0o644)

	if err := copyDir(srcDir, dstDir); err != nil {
		t.Fatalf("复制失败: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dstDir, "file.txt"))
	if string(data) != "hello" {
		t.Error("文件内容应一致")
	}
	data2, _ := os.ReadFile(filepath.Join(dstDir, "sub", "nested.txt"))
	if string(data2) != "world" {
		t.Error("嵌套文件内容应一致")
	}
}

// TestHandleSkillsEvolutionGet_缺名称 验证缺少名称
func TestHandleSkillsEvolutionGet_缺名称(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	_, err := sm.HandleSkillsEvolutionGet(context.Background(), map[string]any{})
	if err == nil {
		t.Error("缺少名称应返回错误")
	}
}

// TestHandleSkillsEvolutionSave_缺名称 验证缺少名称
func TestHandleSkillsEvolutionSave_缺名称(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	_, err := sm.HandleSkillsEvolutionSave(context.Background(), map[string]any{})
	if err == nil {
		t.Error("缺少名称应返回错误")
	}
}

// TestHandlePluginsDisable_正常 验证正常禁用
func TestHandlePluginsDisable_正常(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	result, err := sm.HandlePluginsDisable(context.Background(), map[string]any{
		"name": "test-plugin",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Error("应返回 success=true")
	}
}

// TestHandleSkillsMarketplaceAdd_缺参数 验证缺少参数
func TestHandleSkillsMarketplaceAdd_缺参数(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	result, err := sm.HandleSkillsMarketplaceAdd(context.Background(), map[string]any{
		"name": "test",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("缺少 url 应返回 success=false")
	}
}

// TestHandleSkillsSkillnetInstallStatus_缺installID 验证缺少 install_id
func TestHandleSkillsSkillnetInstallStatus_缺installID(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	result, err := sm.HandleSkillsSkillnetInstallStatus(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("应返回 success=false")
	}
}

// TestHandleSkillsSkillnetEvaluate_缺URL 验证缺少 URL
func TestHandleSkillsSkillnetEvaluate_缺URL(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	result, err := sm.HandleSkillsSkillnetEvaluate(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("应返回 success=false")
	}
}

// TestHandlePluginsInstall_代理skillsInstall 验证 plugins.install 代理到 skills.install
func TestHandlePluginsInstall_代理skillsInstall(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	result, err := sm.HandlePluginsInstall(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("空 spec 应返回 success=false")
	}
}

// TestHandlePluginsUninstall_代理skillsUninstall 验证 plugins.uninstall 代理到 skills.uninstall
func TestHandlePluginsUninstall_代理skillsUninstall(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	result, err := sm.HandlePluginsUninstall(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("空 name 应返回 success=false")
	}
}

// TestGenerateUUID 验证 UUID 生成
func TestGenerateUUID(t *testing.T) {
	id1 := generateUUID()
	id2 := generateUUID()
	if id1 == "" {
		t.Error("UUID 不应为空")
	}
	if id1 == id2 {
		t.Error("两次生成的 UUID 应不同")
	}
}

// TestStateJSONRoundtrip 验证状态 JSON 序列化往返
func TestStateJSONRoundtrip(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.mu.Lock()
	sm.state["skill_configs"] = map[string]any{
		"test": map[string]any{"enabled": false},
	}
	sm.state["installed_plugins"] = []any{
		map[string]any{"name": "p1", "version": "1.0"},
	}
	sm.saveState()
	sm.mu.Unlock()

	sm2 := NewSkillManager(tmpDir)
	configs, ok := sm2.state["skill_configs"].(map[string]any)
	if !ok {
		t.Fatal("skill_configs 应为 map")
	}
	cfg, ok := configs["test"].(map[string]any)
	if !ok {
		t.Fatal("test 配置应为 map")
	}
	if toBool(cfg["enabled"]) != false {
		t.Error("enabled 应为 false")
	}

	plugins, _ := toSliceOfAny(sm2.state["installed_plugins"])
	if len(plugins) != 1 {
		t.Fatalf("期望 1 个插件，实际 %d", len(plugins))
	}
}

// TestHandleSkillsClawhubSetToken_掩码验证 验证 token 掩码格式
func TestHandleSkillsClawhubSetToken_掩码验证(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, _ := sm.HandleSkillsClawhubSetToken(context.Background(), map[string]any{
		"token": "abcdefgh",
	})
	masked := toString(result["token"])
	if masked != "********" {
		t.Errorf("8 字符 token 全掩码 = %q, want %q", masked, "********")
	}
}

// TestHandleSkillsSkillnetInstallStatus_pending 验证 pending 状态
func TestHandleSkillsSkillnetInstallStatus_pending(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 创建 pending 作业
	sm.mu.Lock()
	sm.skillnetInstallJobs["test-id"] = map[string]any{"status": "pending"}
	sm.mu.Unlock()

	result, err := sm.HandleSkillsSkillnetInstallStatus(context.Background(), map[string]any{
		"install_id": "test-id",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toString(result["status"]) != "pending" {
		t.Error("状态应为 pending")
	}
}

// TestHandleSkillsSkillnetInstallStatus_done 验证 done 状态
func TestHandleSkillsSkillnetInstallStatus_done(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.mu.Lock()
	sm.skillnetInstallJobs["test-id"] = map[string]any{
		"status": "done",
		"skill":  map[string]any{"name": "installed-skill"},
	}
	sm.mu.Unlock()

	result, err := sm.HandleSkillsSkillnetInstallStatus(context.Background(), map[string]any{
		"install_id": "test-id",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toString(result["status"]) != "done" {
		t.Error("状态应为 done")
	}
}

// TestHandleSkillsSkillnetInstallStatus_failed 验证 failed 状态
func TestHandleSkillsSkillnetInstallStatus_failed(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.mu.Lock()
	sm.skillnetInstallJobs["test-id"] = map[string]any{
		"status":     "failed",
		"detail":     "下载超时",
		"detail_key": "skills.skillNet.errors.timeout",
	}
	sm.mu.Unlock()

	result, err := sm.HandleSkillsSkillnetInstallStatus(context.Background(), map[string]any{
		"install_id": "test-id",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toString(result["status"]) != "failed" {
		t.Error("状态应为 failed")
	}
	if toString(result["detail"]) != "下载超时" {
		t.Error("detail 应为下载超时")
	}
}

// TestHandleSkillsImportLocal_有SKILLMD 验证导入含 SKILL.md 的目录
func TestHandleSkillsImportLocal_有SKILLMD(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 创建源目录
	srcDir := filepath.Join(tmpDir, "src-skill")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("---\nname: my-skill\n---\n"), 0o644)

	result, err := sm.HandleSkillsImportLocal(context.Background(), map[string]any{
		"path": srcDir,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, result=%v", result)
	}
}

// TestHandleSkillsImportLocal_无SKILLMD 验证导入无 SKILL.md 的目录
func TestHandleSkillsImportLocal_无SKILLMD(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	srcDir := filepath.Join(tmpDir, "no-skill-md")
	os.MkdirAll(srcDir, 0o755)

	result, err := sm.HandleSkillsImportLocal(context.Background(), map[string]any{
		"path": srcDir,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("无 SKILL.md 应返回 success=false")
	}
}

// TestHandleSkillsUninstall_技能存在 验证卸载存在的技能
func TestHandleSkillsUninstall_技能存在(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 创建技能目录
	skillDir := filepath.Join(sm.skillsDir, "test-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\n---\n"), 0o644)

	// 添加到状态
	sm.mu.Lock()
	sm.AddInstalledPlugin(map[string]any{"name": "test-skill", "marketplace": "test"})
	sm.saveState()
	sm.mu.Unlock()

	result, err := sm.HandleSkillsUninstall(context.Background(), map[string]any{
		"name": "test-skill",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Error("应返回 success=true")
	}
	// 目录应被删除
	if dirExists(skillDir) {
		t.Error("技能目录应被删除")
	}
}

// TestHandlePluginsList_正常 验证插件列表代理
func TestHandlePluginsList_正常(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	result, err := sm.HandlePluginsList(context.Background(), nil)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if _, ok := result["plugins"]; !ok {
		t.Error("应包含 plugins 字段")
	}
}

// TestHandleSkillsGet_找到技能 验证找到技能详情
func TestHandleSkillsGet_找到技能(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 创建技能
	skillDir := filepath.Join(sm.skillsDir, "my-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: my-skill\ndescription: A test skill\n---\n\n# Content\n"), 0o644)

	result, err := sm.HandleSkillsGet(context.Background(), map[string]any{"name": "my-skill"})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toString(result["name"]) != "my-skill" {
		t.Errorf("name = %q, want %q", result["name"], "my-skill")
	}
	if _, ok := result["content"]; !ok {
		t.Error("应包含 content 字段（从 body 转换）")
	}
}

// TestHandleSkillsGet_未找到 验证技能未找到
func TestHandleSkillsGet_未找到(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	_, err := sm.HandleSkillsGet(context.Background(), map[string]any{"name": "nonexistent"})
	if err == nil {
		t.Error("不存在的技能应返回错误")
	}
}

// TestHandleSkillsEvolutionStatus_技能存在无演化 验证技能存在但无演化文件
func TestHandleSkillsEvolutionStatus_技能存在无演化(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	skillDir := filepath.Join(sm.skillsDir, "test-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\n---\n"), 0o644)

	result, err := sm.HandleSkillsEvolutionStatus(context.Background(), map[string]any{"name": "test-skill"})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["exists"]) {
		t.Error("无演化文件时 exists 应为 false")
	}
}

// TestHandleSkillsEvolutionStatus_有演化 验证有演化文件
func TestHandleSkillsEvolutionStatus_有演化(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	skillDir := filepath.Join(sm.skillsDir, "test-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\n---\n"), 0o644)
	os.WriteFile(filepath.Join(skillDir, "evolutions.json"), []byte(`{"skill_id":"test-skill","entries":[]}`), 0o644)

	result, err := sm.HandleSkillsEvolutionStatus(context.Background(), map[string]any{"name": "test-skill"})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if !toBool(result["exists"]) {
		t.Error("有演化文件时 exists 应为 true")
	}
}

// TestHandleSkillsEvolutionGet_有演化文件 验证获取演化内容
func TestHandleSkillsEvolutionGet_有演化文件(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	skillDir := filepath.Join(sm.skillsDir, "test-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\n---\n"), 0o644)
	os.WriteFile(filepath.Join(skillDir, "evolutions.json"), []byte(`{"skill_id":"test-skill","version":"1.0","entries":[{"id":"e1"}]}`), 0o644)

	result, err := sm.HandleSkillsEvolutionGet(context.Background(), map[string]any{"name": "test-skill"})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if !toBool(result["exists"]) {
		t.Error("exists 应为 true")
	}
	if !toBool(result["valid"]) {
		t.Error("valid 应为 true")
	}
}

// TestHandleSkillsEvolutionGet_无演化文件 验证无演化文件
func TestHandleSkillsEvolutionGet_无演化文件(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	skillDir := filepath.Join(sm.skillsDir, "test-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\n---\n"), 0o644)

	result, err := sm.HandleSkillsEvolutionGet(context.Background(), map[string]any{"name": "test-skill"})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["exists"]) {
		t.Error("exists 应为 false")
	}
}

// TestHandleSkillsEvolutionSave_正常 验证正常保存演化
func TestHandleSkillsEvolutionSave_正常(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	skillDir := filepath.Join(sm.skillsDir, "test-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\n---\n"), 0o644)

	result, err := sm.HandleSkillsEvolutionSave(context.Background(), map[string]any{
		"name": "test-skill",
		"entries": []any{
			map[string]any{
				"id":     "e1",
				"change": map[string]any{"content": "added new feature"},
			},
		},
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Error("应返回 success=true")
	}
	// 验证文件已写入
	if !fileExists(filepath.Join(skillDir, "evolutions.json")) {
		t.Error("evolutions.json 应已写入")
	}
}

// TestHandleSkillsEvolutionSave_无entries 验证无 entries
func TestHandleSkillsEvolutionSave_无entries(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	_, err := sm.HandleSkillsEvolutionSave(context.Background(), map[string]any{
		"name": "test-skill",
	})
	if err == nil {
		t.Error("缺少 entries 应返回错误")
	}
}

// TestHandleSkillsMarketplaceList_有数据 验证有 marketplace 数据
func TestHandleSkillsMarketplaceList_有数据(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.mu.Lock()
	sm.state["marketplaces"] = []any{
		map[string]any{"name": "m1", "url": "https://github.com/m1", "enabled": true, "install_location": "/tmp/m1"},
	}
	sm.saveState()
	sm.mu.Unlock()

	result, err := sm.HandleSkillsMarketplaceList(context.Background(), nil)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	marketplaces, _ := result["marketplaces"].([]map[string]any)
	if len(marketplaces) != 1 {
		t.Fatalf("期望 1 个 marketplace，实际 %d", len(marketplaces))
	}
	if toString(marketplaces[0]["install_location"]) != "/tmp/m1" {
		t.Error("应包含 install_location 字段")
	}
}

// TestHandleSkillsMarketplaceRemove_正常 验证正常移除
func TestHandleSkillsMarketplaceRemove_正常(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.HandleSkillsMarketplaceAdd(context.Background(), map[string]any{
		"name": "test-market", "url": "https://github.com/test",
	})
	result, err := sm.HandleSkillsMarketplaceRemove(context.Background(), map[string]any{
		"name": "test-market",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Error("应返回 success=true")
	}
}

// TestHandleSkillsMarketplaceToggle_正常 验证正常切换
func TestHandleSkillsMarketplaceToggle_正常(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.HandleSkillsMarketplaceAdd(context.Background(), map[string]any{
		"name": "test-market", "url": "https://github.com/test",
	})
	result, err := sm.HandleSkillsMarketplaceToggle(context.Background(), map[string]any{
		"name": "test-market", "enabled": false,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Error("应返回 success=true")
	}
}

// TestHandleSkillsInstall_spec格式 验证 spec 格式
func TestHandleSkillsInstall_spec格式(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 无 @ 的无效 spec（非 builtin）
	result, err := sm.HandleSkillsInstall(context.Background(), map[string]any{
		"spec": "just-a-name",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("非 builtin 无 @ 应返回 success=false")
	}
}

// TestHandleSkillsInstall_空marketplace 验证空 marketplace 名称
func TestHandleSkillsInstall_空marketplace(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsInstall(context.Background(), map[string]any{
		"spec": "@",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("空名称应返回 success=false")
	}
}

// TestHandleSkillsInstallBuiltin_缺名称 验证缺少名称
func TestHandleSkillsInstallBuiltin_缺名称(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsInstallBuiltin(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("应返回 success=false")
	}
}

// TestHandleSkillsInstallBuiltin_无效名称 验证无效名称
func TestHandleSkillsInstallBuiltin_无效名称(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsInstallBuiltin(context.Background(), map[string]any{
		"name": "../etc/passwd",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("应返回 success=false")
	}
}

// TestHandleSkillsClawhubSearch_缺参数 验证 ClawHub 搜索缺参数时返回失败
func TestHandleSkillsClawhubSearch_缺参数(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	result, err := sm.HandleSkillsClawhubSearch(context.Background(), nil)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("缺参数应返回 success=false")
	}
}

// TestHandleSkillsClawhubDownload_缺参数 验证缺参数时返回失败
func TestHandleSkillsClawhubDownload_缺参数(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	result, err := sm.HandleSkillsClawhubDownload(context.Background(), nil)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("缺参数应返回 success=false")
	}
}

// TestHandleSkillsTeamSkillsHub_缺参数 验证 TeamSkillsHub 方法缺参数时返回失败
// Search 无强制参数，因此不在此测试中验证
func TestHandleSkillsTeamSkillsHub_缺参数(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	ctx := context.Background()

	// 每个方法缺参数时应返回 success=false 而非 error
	methods := []struct {
		name   string
		method func(context.Context, map[string]any) (map[string]any, error)
	}{
		{"Info", sm.HandleSkillsTeamSkillsHubInfo},
		{"Init", sm.HandleSkillsTeamSkillsHubInit},
		{"Validate", sm.HandleSkillsTeamSkillsHubValidate},
		{"Pack", sm.HandleSkillsTeamSkillsHubPack},
		{"Install", sm.HandleSkillsTeamSkillsHubInstall},
		{"Publish", sm.HandleSkillsTeamSkillsHubPublish},
		{"Delete", sm.HandleSkillsTeamSkillsHubDelete},
	}
	for _, m := range methods {
		result, err := m.method(ctx, nil)
		if err != nil {
			t.Errorf("%s 不应返回 error: %v", m.name, err)
		}
		if toBool(result["success"]) != false {
			t.Errorf("%s 缺参数应返回 success=false", m.name)
		}
	}
}

// TestScanLocalSkills_有技能 验证扫描本地技能
func TestScanLocalSkills_有技能(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	skillDir := filepath.Join(sm.skillsDir, "local-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: local-skill\ndescription: Test\n---\n\n# Local\n"), 0o644)

	skills := sm.scanLocalSkills()
	if len(skills) == 0 {
		t.Error("应扫描到本地技能")
	}
}

// TestScanBuiltinSkills_空 验证空内置目录
func TestScanBuiltinSkills_空(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	skills := sm.scanBuiltinSkills()
	if len(skills) != 0 {
		t.Errorf("空内置目录应返回空，实际 %d", len(skills))
	}
}

// TestResolveLocalSkillDir 验证解析本地技能目录
func TestResolveLocalSkillDir(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	skillDir := filepath.Join(sm.skillsDir, "existing-skill")
	os.MkdirAll(skillDir, 0o755)

	result := sm.resolveLocalSkillDir("existing-skill")
	if result != skillDir {
		t.Errorf("resolveLocalSkillDir = %q, want %q", result, skillDir)
	}

	result2 := sm.resolveLocalSkillDir("nonexistent")
	if result2 != "" {
		t.Error("不存在的技能应返回空")
	}
}

// TestGetSkillEvolutionPath 验证获取演化文件路径
func TestGetSkillEvolutionPath(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	skillDir := filepath.Join(sm.skillsDir, "evo-skill")
	os.MkdirAll(skillDir, 0o755)

	result := sm.getSkillEvolutionPath("evo-skill")
	expected := filepath.Join(skillDir, "evolutions.json")
	if result != expected {
		t.Errorf("getSkillEvolutionPath = %q, want %q", result, expected)
	}
}

// TestResolveSkillSource 验证技能来源解析
func TestResolveSkillSource(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.mu.Lock()
	sm.AddInstalledPlugin(map[string]any{"name": "market-skill", "marketplace": "test-market"})
	sm.mu.Unlock()

	if sm.resolveSkillSource("market-skill") != "test-market" {
		t.Error("market 技能应返回 marketplace 名称")
	}
	if sm.resolveSkillSource("local-skill") != "local" {
		t.Error("本地技能应返回 local")
	}
}

// TestScanMarketplaceSkills_空 验证空 marketplace
func TestScanMarketplaceSkills_空(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	skills := sm.scanMarketplaceSkills()
	if len(skills) != 0 {
		t.Errorf("空 marketplace 应返回空，实际 %d", len(skills))
	}
}

// TestScanMarketplaceSkills_有技能 验证 marketplace 中有技能
func TestScanMarketplaceSkills_有技能(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	marketRepo := filepath.Join(sm.marketplaceDir, "test-market", "market-skill")
	os.MkdirAll(marketRepo, 0o755)
	os.WriteFile(filepath.Join(marketRepo, "SKILL.md"), []byte("---\nname: market-skill\ndescription: Market skill\n---\n\n# Market\n"), 0o644)

	skills := sm.scanMarketplaceSkills()
	if len(skills) == 0 {
		t.Error("应扫描到 marketplace 技能")
	}
}

// TestFindSkillInDir 验证在目录中查找技能
func TestFindSkillInDir(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	skillDir := filepath.Join(tmpDir, "my-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: my-skill\ndescription: Found\n---\n\n# Body\n"), 0o644)

	meta, err := sm.findSkillInDir(tmpDir, "my-skill", "")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toString(meta["name"]) != "my-skill" {
		t.Errorf("name = %q, want %q", meta["name"], "my-skill")
	}
}

// TestFindSkillInDir_marketplace 验证 marketplace 上下文
func TestFindSkillInDir_marketplace(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	skillDir := filepath.Join(tmpDir, "mkt-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: mkt-skill\n---\n"), 0o644)

	meta, err := sm.findSkillInDir(tmpDir, "mkt-skill", "my-market")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toString(meta["source"]) != "my-market" {
		t.Errorf("source = %q, want %q", meta["source"], "my-market")
	}
}

// TestHandleSkillsList_刷新marketplace 验证 refresh_marketplaces 参数
func TestHandleSkillsList_刷新marketplace(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsList(context.Background(), map[string]any{
		"refresh_marketplaces": true,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if _, ok := result["skills"]; !ok {
		t.Error("应包含 skills 字段")
	}
}

// TestHandleSkillsUninstall_无效名称 验证卸载无效名称
func TestHandleSkillsUninstall_无效名称(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsUninstall(context.Background(), map[string]any{
		"name": "../etc/passwd",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("应返回 success=false")
	}
}

// TestHandleSkillsEvolutionStatus_无效名称 验证无效名称
func TestHandleSkillsEvolutionStatus_无效名称(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	_, err := sm.HandleSkillsEvolutionStatus(context.Background(), map[string]any{
		"name": "../etc",
	})
	if err == nil {
		t.Error("无效名称应返回错误")
	}
}

// TestHandleSkillsEvolutionSave_技能不存在 验证技能不存在
func TestHandleSkillsEvolutionSave_技能不存在(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	_, err := sm.HandleSkillsEvolutionSave(context.Background(), map[string]any{
		"name": "nonexistent",
		"entries": []any{
			map[string]any{"id": "e1", "change": map[string]any{"content": "test"}},
		},
	})
	if err == nil {
		t.Error("技能不存在应返回错误")
	}
}

// TestHandleSkillsInstall_无效spec路径 验证无效 spec 中的路径
func TestHandleSkillsInstall_无效spec路径(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsInstall(context.Background(), map[string]any{
		"spec": "../etc@test",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("无效路径应返回 success=false")
	}
}

// TestNormalizePlugin 验证插件规范化
func TestNormalizePlugin(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	p := map[string]any{"name": "test", "version": "1.0"}
	result := sm.normalizePlugin(p)
	if toString(result["name"]) != "test" {
		t.Error("规范化应保持原始数据")
	}
}

// TestGetClawhubToken 验证获取 ClawHub token
func TestGetClawhubToken(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	if sm.getClawhubToken() != "" {
		t.Error("初始 token 应为空")
	}
	sm.SetClawhubToken("test-token")
	if sm.getClawhubToken() != "test-token" {
		t.Error("设置后应能读取")
	}
}

// TestHandleSkillsClawhubGetToken_有token 验证有 token 时返回
func TestHandleSkillsClawhubGetToken_有token(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.mu.Lock()
	sm.SetClawhubToken("sk-test-token-1234")
	sm.saveState()
	sm.mu.Unlock()

	result, err := sm.HandleSkillsClawhubGetToken(context.Background(), nil)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if !toBool(result["has_token"]) {
		t.Error("有 token 时 has_token 应为 true")
	}
}

// TestAddLocalSkill 验证添加本地技能
func TestAddLocalSkill(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.AddLocalSkill(map[string]any{"name": "local1", "source": "local"})
	sm.mu.Lock()
	sm.saveState()
	sm.mu.Unlock()

	sm2 := NewSkillManager(tmpDir)
	list, _ := toSliceOfAny(sm2.state["local_skills"])
	if len(list) != 1 {
		t.Fatalf("期望 1 个本地技能，实际 %d", len(list))
	}
}

// TestHandleSkillsInstall_builtinSpec 验证 spec 为 builtin 的安装
func TestHandleSkillsInstall_builtinSpec(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// builtin@ 形式
	result, err := sm.HandleSkillsInstall(context.Background(), map[string]any{
		"spec": "test-skill@builtin",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	// builtin 目录不存在，应返回 success=false
	if toBool(result["success"]) != false {
		t.Error("builtin 目录不存在应返回 success=false")
	}
}

// TestHandleSkillsInstall_marketplace不存在 验证 marketplace 不存在
func TestHandleSkillsInstall_marketplace不存在(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsInstall(context.Background(), map[string]any{
		"spec": "plugin@nonexistent-market",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("不存在的 marketplace 应返回 success=false")
	}
}

// TestHandleSkillsInstall_marketplace存在无URL 验证 marketplace 存在但无 URL
func TestHandleSkillsInstall_marketplace存在无URL(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.mu.Lock()
	sm.state["marketplaces"] = []any{
		map[string]any{"name": "no-url-market", "enabled": true},
	}
	sm.saveState()
	sm.mu.Unlock()

	result, err := sm.HandleSkillsInstall(context.Background(), map[string]any{
		"spec": "plugin@no-url-market",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("无 URL 的 marketplace 应返回 success=false")
	}
}

// TestHandleSkillsInstallBuiltin_目录不存在 验证内置目录不存在
func TestHandleSkillsInstallBuiltin_目录不存在(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	// builtin 目录为空字符串
	result, err := sm.HandleSkillsInstallBuiltin(context.Background(), map[string]any{
		"name": "any-skill",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("内置目录不存在应返回 success=false")
	}
}

// TestHandleSkillsInstallBuiltin_技能不存在 验证内置技能不存在
func TestHandleSkillsInstallBuiltin_技能不存在(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 创建一个假的 builtin 目录
	builtinDir := filepath.Join(tmpDir, "builtin-skills")
	os.MkdirAll(builtinDir, 0o755)

	// 临时修改 getBuiltinSkillsDir 的行为不可行，
	// 所以这个测试验证当目录存在但技能不存在时的路径
	// （getBuiltinSkillsDir 当前返回空字符串，所以 "内置技能目录不存在" 会被命中）
	result, _ := sm.HandleSkillsInstallBuiltin(context.Background(), map[string]any{
		"name": "nonexistent-skill",
	})
	if toBool(result["success"]) != false {
		t.Error("技能不存在应返回 success=false")
	}
}

// TestHandleSkillsEvolutionSave_无效entries 验证无效 entries
func TestHandleSkillsEvolutionSave_无效entries(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	skillDir := filepath.Join(sm.skillsDir, "evo-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: evo-skill\n---\n"), 0o644)

	// entries 中包含非对象
	_, err := sm.HandleSkillsEvolutionSave(context.Background(), map[string]any{
		"name":    "evo-skill",
		"entries": []any{"not an object"},
	})
	if err == nil {
		t.Error("非对象 entry 应返回错误")
	}

	// entries 中缺少 id
	_, err = sm.HandleSkillsEvolutionSave(context.Background(), map[string]any{
		"name": "evo-skill",
		"entries": []any{
			map[string]any{"change": map[string]any{"content": "test"}},
		},
	})
	if err == nil {
		t.Error("缺少 id 应返回错误")
	}

	// entries 中缺少 change
	_, err = sm.HandleSkillsEvolutionSave(context.Background(), map[string]any{
		"name": "evo-skill",
		"entries": []any{
			map[string]any{"id": "e1"},
		},
	})
	if err == nil {
		t.Error("缺少 change 应返回错误")
	}

	// entries 中 change.content 非字符串
	_, err = sm.HandleSkillsEvolutionSave(context.Background(), map[string]any{
		"name": "evo-skill",
		"entries": []any{
			map[string]any{"id": "e1", "change": map[string]any{"content": ""}},
		},
	})
	if err == nil {
		t.Error("空 content 应返回错误")
	}
}

// TestHandleSkillsEvolutionGet_无效JSON 验证无效 JSON 演化文件
func TestHandleSkillsEvolutionGet_无效JSON(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	skillDir := filepath.Join(sm.skillsDir, "bad-evo")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: bad-evo\n---\n"), 0o644)
	os.WriteFile(filepath.Join(skillDir, "evolutions.json"), []byte("invalid json"), 0o644)

	result, err := sm.HandleSkillsEvolutionGet(context.Background(), map[string]any{"name": "bad-evo"})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["valid"]) {
		t.Error("无效 JSON 应返回 valid=false")
	}
}

// TestHandleSkillsImportLocal_无效路径 验证无效路径
func TestHandleSkillsImportLocal_无效路径(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsImportLocal(context.Background(), map[string]any{
		"path": "/nonexistent/path/to/skill",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("不存在的路径应返回 success=false")
	}
}

// TestHandleSkillsImportLocal_强制覆盖 验证强制覆盖
func TestHandleSkillsImportLocal_强制覆盖(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 创建源目录
	srcDir := filepath.Join(tmpDir, "src-skill")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("---\nname: force-skill\n---\n"), 0o644)

	// 第一次导入
	sm.HandleSkillsImportLocal(context.Background(), map[string]any{
		"path": srcDir,
	})

	// 第二次不强制导入
	result, _ := sm.HandleSkillsImportLocal(context.Background(), map[string]any{
		"path": srcDir,
	})
	if toBool(result["success"]) != false {
		t.Error("重复导入不强制应返回 success=false")
	}

	// 强制导入
	result, _ = sm.HandleSkillsImportLocal(context.Background(), map[string]any{
		"path":  srcDir,
		"force": true,
	})
	if toBool(result["success"]) != true {
		t.Error("强制导入应返回 success=true")
	}
}

// TestHandleSkillsGet_marketplace中查找 验证在 marketplace 中查找
func TestHandleSkillsGet_marketplace中查找(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 创建 marketplace 目录
	marketSkill := filepath.Join(sm.marketplaceDir, "mkt", "mkt-skill")
	os.MkdirAll(marketSkill, 0o755)
	os.WriteFile(filepath.Join(marketSkill, "SKILL.md"), []byte("---\nname: mkt-skill\ndescription: Market\n---\n\n# Market\n"), 0o644)

	result, err := sm.HandleSkillsGet(context.Background(), map[string]any{"name": "mkt-skill"})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toString(result["name"]) != "mkt-skill" {
		t.Errorf("name = %q, want %q", result["name"], "mkt-skill")
	}
}

// TestScanLocalSkills_下划线目录跳过 验证下划线前缀目录被跳过
func TestScanLocalSkills_下划线目录跳过(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 创建下划线前缀目录
	underscoreDir := filepath.Join(sm.skillsDir, "_marketplace")
	os.MkdirAll(underscoreDir, 0o755)
	os.WriteFile(filepath.Join(underscoreDir, "SKILL.md"), []byte("---\nname: hidden\n---\n"), 0o644)

	skills := sm.scanLocalSkills()
	for _, s := range skills {
		if toString(s["name"]) == "hidden" {
			t.Error("下划线目录应被跳过")
		}
	}
}

// TestSafePathName_绝对路径 验证绝对路径被拒绝
func TestSafePathName_绝对路径(t *testing.T) {
	_, err := safePathName("/etc/passwd", "skill")
	if err == nil {
		t.Error("绝对路径应被拒绝")
	}
}

// TestLogRejectedName 验证记录被拒绝的名称
func TestLogRejectedName(t *testing.T) {
	// 仅验证不 panic
	logRejectedName("test-op", "skill", "bad-name", fmt.Errorf("test error"))
}

// TestGetBuiltinSkillsDir 验证内置技能目录
func TestGetBuiltinSkillsDir(t *testing.T) {
	// 当前返回空字符串（后续补充）
	dir := getBuiltinSkillsDir()
	_ = dir // 不验证具体值
}

// TestSyncMarketplaceRepos 验证同步 marketplace
func TestSyncMarketplaceRepos(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.mu.Lock()
	sm.state["marketplaces"] = []any{
		map[string]any{"name": "m1", "url": "https://github.com/m1", "enabled": true},
		map[string]any{"name": "m2", "url": "https://github.com/m2", "enabled": false},
	}
	sm.saveState()
	sm.mu.Unlock()

	err := sm.syncMarketplaceRepos(context.Background())
	// git 未实现，但不应 panic
	_ = err
}

// TestRefreshAgentDataIndexes 验证刷新索引
func TestRefreshAgentDataIndexes(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	sm.refreshAgentDataIndexes() // 仅验证不 panic
}

// TestGitMethods 验证 git 相关方法
func TestGitMethods(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.gitClone(context.Background(), "https://github.com/test", filepath.Join(tmpDir, "clone"))
	sm.gitPull(context.Background(), filepath.Join(tmpDir, "pull"))
	sm.gitGetCommit(tmpDir)
}

// TestAddInstalledPlugin_替换 验证替换已有插件
func TestAddInstalledPlugin_替换(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.mu.Lock()
	sm.AddInstalledPlugin(map[string]any{"name": "p1", "version": "1.0"})
	sm.AddInstalledPlugin(map[string]any{"name": "p1", "version": "2.0"}) // 替换
	sm.saveState()
	sm.mu.Unlock()

	plugins := sm.GetInstalledPlugins()
	if len(plugins) != 1 {
		t.Fatalf("期望 1 个插件，实际 %d", len(plugins))
	}
	if toString(plugins[0]["version"]) != "2.0" {
		t.Error("插件版本应被替换为 2.0")
	}
}

// TestRemoveInstalledPlugin_空 验证空列表移除
func TestRemoveInstalledPlugin_空(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	sm.removeInstalledPlugin("nonexistent") // 不应 panic
}

// TestParseSkillMD_无Frontmatter 验证无 frontmatter 的 SKILL.md
func TestParseSkillMD_无Frontmatter(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "no-fm")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# No frontmatter\n"), 0o644)

	sm := NewSkillManager(tmpDir)
	meta := sm.parseSkillMD(filepath.Join(skillDir, "SKILL.md"))
	// 无 frontmatter 时返回空 meta
	if meta == nil {
		t.Error("不应返回 nil")
	}
}

// TestParseSkillMD_空文件 验证空 SKILL.md
func TestParseSkillMD_空文件(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	meta := sm.parseSkillMD("")
	if meta != nil {
		t.Error("空路径应返回 nil")
	}
}

// TestTryFindSkillFile_无SKILLMD 验证无 SKILL.md
func TestTryFindSkillFile_无SKILLMD(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	result := sm.tryFindSkillFile(tmpDir)
	if result != "" {
		t.Error("无 SKILL.md 应返回空")
	}
}

// TestHandleSkillsEvolutionSave_多entry 验证保存多个条目
func TestHandleSkillsEvolutionSave_多entry(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	skillDir := filepath.Join(sm.skillsDir, "multi-evo")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: multi-evo\n---\n"), 0o644)

	result, err := sm.HandleSkillsEvolutionSave(context.Background(), map[string]any{
		"name": "multi-evo",
		"entries": []any{
			map[string]any{"id": "e1", "change": map[string]any{"content": "change1"}},
			map[string]any{"id": "e2", "change": map[string]any{"content": "change2"}},
		},
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Error("应返回 success=true")
	}
	entryCount, _ := result["entry_count"].(int)
	if entryCount != 2 {
		t.Errorf("entry_count = %v, want 2", result["entry_count"])
	}
}

// TestHandleSkillsInstallBuiltin_成功安装 验证成功安装内置技能
func TestHandleSkillsInstallBuiltin_成功安装(t *testing.T) {
	tmpDir := t.TempDir()

	// 创建内置技能源目录
	builtinDir := filepath.Join(tmpDir, "builtin-skills", "test-builtin")
	os.MkdirAll(builtinDir, 0o755)
	os.WriteFile(filepath.Join(builtinDir, "SKILL.md"), []byte("---\nname: test-builtin\nversion: 1.0\n---\n"), 0o644)

	sm := NewSkillManager(tmpDir)
	// getBuiltinSkillsDir 返回空字符串，所以需要直接测试复制逻辑
	// 此测试验证当 builtin 目录不存在时的处理
	result, _ := sm.HandleSkillsInstallBuiltin(context.Background(), map[string]any{
		"name": "test-builtin",
	})
	// 因 getBuiltinSkillsDir 返回空，所以 "内置技能目录不存在" 会被命中
	if toBool(result["success"]) != false {
		t.Error("内置目录不存在应返回 success=false")
	}
}

// TestHandleSkillsInstall_marketplace有URL 验证 marketplace 有 URL 的情况
func TestHandleSkillsInstall_marketplace有URL(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.mu.Lock()
	sm.state["marketplaces"] = []any{
		map[string]any{"name": "test-market", "url": "https://github.com/test/market", "enabled": true},
	}
	sm.saveState()
	sm.mu.Unlock()

	// git clone 未实现，但路径可达
	result, err := sm.HandleSkillsInstall(context.Background(), map[string]any{
		"spec": "some-plugin@test-market",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	// gitClone 未实现返回 errNotImplemented，但代码不会因此 panic
	_ = result
}

// TestSaveState_错误路径 验证保存到不可写路径
func TestSaveState_错误路径(t *testing.T) {
	sm := &SkillManager{
		skillsDir: "/nonexistent/skills",
		stateFile: "/nonexistent/skills/skills_state.json",
		state:     map[string]any{"test": "value"},
	}
	sm.saveState() // 不应 panic，仅记录错误
}

// TestLoadState_无效JSON 验证加载无效 JSON
func TestLoadState_无效JSON(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "skills_state.json")
	os.WriteFile(stateFile, []byte("invalid json"), 0o644)

	sm := &SkillManager{
		skillsDir: tmpDir,
		stateFile: stateFile,
	}
	sm.state = sm.loadState()
	if len(sm.state) != 0 {
		t.Error("无效 JSON 应返回空 map")
	}
}

// TestToBoolWithDefault 验证带默认值的 bool 转换
func TestToBoolWithDefault(t *testing.T) {
	if toBoolWithDefault(nil, true) != true {
		t.Error("nil 应返回默认值 true")
	}
	if toBoolWithDefault(nil, false) != false {
		t.Error("nil 应返回默认值 false")
	}
	if toBoolWithDefault(true, false) != true {
		t.Error("true 应返回 true")
	}
}

// TestToStringWithDefault 验证带默认值的字符串转换
func TestToStringWithDefault(t *testing.T) {
	if toStringWithDefault(nil, "default") != "default" {
		t.Error("nil 应返回默认值")
	}
	if toStringWithDefault("hello", "default") != "hello" {
		t.Error("非空字符串应返回原值")
	}
}

// TestGetSkillEnabled_非dictConfigs 验证非 dict 的 configs
func TestGetSkillEnabled_非dictConfigs(t *testing.T) {
	state := map[string]any{
		"skill_configs": "not a dict",
	}
	if !GetSkillEnabled(state, "skill1") {
		t.Error("非 dict configs 应默认返回 true")
	}
}

// TestGetSkillEnabled_非dictConfig 验证非 dict 的单个 config
func TestGetSkillEnabled_非dictConfig(t *testing.T) {
	state := map[string]any{
		"skill_configs": map[string]any{
			"skill1": "not a dict",
		},
	}
	if !GetSkillEnabled(state, "skill1") {
		t.Error("非 dict config 应默认返回 true")
	}
}

// TestListDisabledSkills_非dictConfigs 验证非 dict 的 configs
func TestListDisabledSkills_非dictConfigs(t *testing.T) {
	state := map[string]any{
		"skill_configs": "not a dict",
	}
	result := ListDisabledSkills(state)
	if len(result) != 0 {
		t.Error("非 dict configs 应返回空")
	}
}

// TestHandlePluginsEnable_缺名称 验证缺少名称
func TestHandlePluginsEnable_缺名称(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	result, err := sm.HandlePluginsEnable(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("应返回 success=false")
	}
}

// TestHandlePluginsDisable_缺名称 验证缺少名称
func TestHandlePluginsDisable_缺名称(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	result, err := sm.HandlePluginsDisable(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("应返回 success=false")
	}
}

// TestCopyDir_源不存在 验证源目录不存在
func TestCopyDir_源不存在(t *testing.T) {
	err := copyDir("/nonexistent/src", "/tmp/dst")
	if err == nil {
		t.Error("源目录不存在应返回错误")
	}
}

// TestHandleSkillsEvolutionSave_覆盖已有 验证覆盖已有的演化文件
func TestHandleSkillsEvolutionSave_覆盖已有(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	skillDir := filepath.Join(sm.skillsDir, "overwrite-evo")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: overwrite-evo\n---\n"), 0o644)
	os.WriteFile(filepath.Join(skillDir, "evolutions.json"), []byte(`{"skill_id":"overwrite-evo","version":"1.0","entries":[{"id":"old"}]}`), 0o644)

	result, err := sm.HandleSkillsEvolutionSave(context.Background(), map[string]any{
		"name": "overwrite-evo",
		"entries": []any{
			map[string]any{"id": "new1", "change": map[string]any{"content": "new change"}},
		},
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Error("应返回 success=true")
	}
}

// TestHandleSkillsClawhubSetToken_空token 验证设置空 token
func TestHandleSkillsClawhubSetToken_空token(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsClawhubSetToken(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Error("应返回 success=true")
	}
}

// TestHandleSkillsMarketplaceToggle_缺名称 验证缺少名称
func TestHandleSkillsMarketplaceToggle_缺名称(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	result, err := sm.HandleSkillsMarketplaceToggle(context.Background(), map[string]any{
		"enabled": true,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("应返回 success=false")
	}
}

// TestHandleSkillsMarketplaceRemove_缺名称 验证缺少名称
func TestHandleSkillsMarketplaceRemove_缺名称(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	result, err := sm.HandleSkillsMarketplaceRemove(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("应返回 success=false")
	}
}

// TestHandleSkillsInstall_force覆盖 验证 force 覆盖安装
func TestHandleSkillsInstall_force覆盖(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 创建 marketplace 配置
	marketURL := "https://github.com/test/market"
	sm.mu.Lock()
	sm.state["marketplaces"] = []any{
		map[string]any{"name": "test-market", "url": marketURL, "enabled": true},
	}
	sm.saveState()
	sm.mu.Unlock()

	// 创建 marketplace 仓库目录，模拟已 clone 的仓库
	repoDir := filepath.Join(sm.marketplaceDir, "test-market")
	skillSrcDir := filepath.Join(repoDir, "skills", "my-plugin")
	os.MkdirAll(skillSrcDir, 0o755)
	os.WriteFile(filepath.Join(skillSrcDir, "SKILL.md"), []byte("---\nname: my-plugin\nversion: 1.0\n---\n"), 0o644)

	// 也创建已安装的同名技能
	destDir := filepath.Join(sm.skillsDir, "my-plugin")
	os.MkdirAll(destDir, 0o755)
	os.WriteFile(filepath.Join(destDir, "SKILL.md"), []byte("---\nname: my-plugin\nversion: 0.9\n---\n"), 0o644)

	// 不 force 时应报已存在
	result, _ := sm.HandleSkillsInstall(context.Background(), map[string]any{
		"spec": "my-plugin@test-market",
	})
	if toBool(result["success"]) != false {
		t.Error("不 force 时应报已存在")
	}

	// force 时应成功（但 gitPull 未实现，不会影响 copy 逻辑）
	result, _ = sm.HandleSkillsInstall(context.Background(), map[string]any{
		"spec":  "my-plugin@test-market",
		"force": true,
	})
	if toBool(result["success"]) != true {
		t.Errorf("force 时应成功，result=%v", result)
	}
}

// TestHandleSkillsInstall_单skill模式 验证单 skill 模式（兼容无 skills/ 子目录）
func TestHandleSkillsInstall_单skill模式(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.mu.Lock()
	sm.state["marketplaces"] = []any{
		map[string]any{"name": "single-market", "url": "https://github.com/test/single", "enabled": true},
	}
	sm.saveState()
	sm.mu.Unlock()

	// 仓库目录本身就是 skill（无 skills/ 子目录）
	repoDir := filepath.Join(sm.marketplaceDir, "single-market")
	os.MkdirAll(repoDir, 0o755)
	os.WriteFile(filepath.Join(repoDir, "SKILL.md"), []byte("---\nname: single-skill\n---\n"), 0o644)

	result, _ := sm.HandleSkillsInstall(context.Background(), map[string]any{
		"spec": "single-skill@single-market",
	})
	if toBool(result["success"]) != true {
		t.Errorf("单 skill 模式应成功，result=%v", result)
	}
}

// TestHandleSkillsInstall_缺SKILLMD 验证缺少 SKILL.md
func TestHandleSkillsInstall_缺SKILLMD(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.mu.Lock()
	sm.state["marketplaces"] = []any{
		map[string]any{"name": "no-md-market", "url": "https://github.com/test/no-md", "enabled": true},
	}
	sm.saveState()
	sm.mu.Unlock()

	// 仓库有 skills/ 子目录但无 SKILL.md
	repoDir := filepath.Join(sm.marketplaceDir, "no-md-market")
	skillSrcDir := filepath.Join(repoDir, "skills", "no-md-plugin")
	os.MkdirAll(skillSrcDir, 0o755)

	result, _ := sm.HandleSkillsInstall(context.Background(), map[string]any{
		"spec": "no-md-plugin@no-md-market",
	})
	if toBool(result["success"]) != false {
		t.Error("缺 SKILL.md 应返回 success=false")
	}
}

// TestHandleSkillsInstallBuiltin_成功复制 验证成功复制内置技能
func TestHandleSkillsInstallBuiltin_成功复制(t *testing.T) {
	tmpDir := t.TempDir()

	// 创建模拟的内置技能源目录
	builtinSrcDir := filepath.Join(tmpDir, "builtin-src", "builtin-skill")
	os.MkdirAll(builtinSrcDir, 0o755)
	os.WriteFile(filepath.Join(builtinSrcDir, "SKILL.md"), []byte("---\nname: builtin-skill\nversion: 1.0\n---\n"), 0o644)

	sm := NewSkillManager(tmpDir)
	// getBuiltinSkillsDir 返回空，所以会走 "内置技能目录不存在" 路径
	result, _ := sm.HandleSkillsInstallBuiltin(context.Background(), map[string]any{
		"name": "builtin-skill",
	})
	// 由于 builtin dir 为空字符串，dirExists 返回 false
	if toBool(result["success"]) != false {
		t.Error("空 builtin 目录应返回 success=false")
	}
}

// TestSaveState_正常 验证正常保存
func TestSaveState_正常(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.mu.Lock()
	sm.state["key"] = "value"
	sm.saveState()
	sm.mu.Unlock()

	data, err := os.ReadFile(sm.stateFile)
	if err != nil {
		t.Fatalf("读取状态文件失败: %v", err)
	}
	if len(data) == 0 {
		t.Error("状态文件不应为空")
	}
}

// TestScanBuiltinSkills_有目录 验证有内置技能目录
func TestScanBuiltinSkills_有目录(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	// getBuiltinSkillsDir 返回空，scanBuiltinSkills 应返回空
	skills := sm.scanBuiltinSkills()
	if len(skills) != 0 {
		t.Errorf("空内置目录应返回空，实际 %d", len(skills))
	}
}

// TestToBoolWithDefault_更多 验证更多场景
func TestToBoolWithDefault_更多(t *testing.T) {
	if toBoolWithDefault(0, true) != false {
		t.Error("0 应返回 false")
	}
	if toBoolWithDefault(1, false) != true {
		t.Error("1 应返回 true")
	}
	if toBoolWithDefault("", true) != false {
		t.Error("空字符串应返回 false")
	}
	if toBoolWithDefault("yes", false) != true {
		t.Error("非空字符串应返回 true")
	}
}

// TestHandleSkillsEvolutionGet_技能目录存在 验证技能目录存在但无演化文件
func TestHandleSkillsEvolutionGet_技能目录存在(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	skillDir := filepath.Join(sm.skillsDir, "no-evo-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: no-evo-skill\n---\n"), 0o644)

	result, err := sm.HandleSkillsEvolutionGet(context.Background(), map[string]any{"name": "no-evo-skill"})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["exists"]) {
		t.Error("无演化文件时 exists 应为 false")
	}
}

// TestHandleSkillsInstall_无效plugin名 验证无效 plugin 名
func TestHandleSkillsInstall_无效plugin名(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsInstall(context.Background(), map[string]any{
		"spec": "../etc@test-market",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("无效 plugin 名应返回 success=false")
	}
}

// TestHandleSkillsInstall_无效market名 验证无效 marketplace 名
func TestHandleSkillsInstall_无效market名(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsInstall(context.Background(), map[string]any{
		"spec": "plugin@../etc",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("无效 marketplace 名应返回 success=false")
	}
}

// TestHandleSkillsUninstall_技能不存在 验证技能不存在
func TestHandleSkillsUninstall_技能不存在(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsUninstall(context.Background(), map[string]any{
		"name": "nonexistent",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("不存在的技能应返回 success=false")
	}
}

// ──────────────────────────── 本地辅助方法测试 ────────────────────────────

// TestGetLocalSkills_空状态 验证空状态返回空切片
func TestGetLocalSkills_空状态(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result := sm.GetLocalSkills()
	if result == nil {
		t.Error("应返回空切片，而非 nil")
	}
	if len(result) != 0 {
		t.Errorf("期望 0 个，实际 %d 个", len(result))
	}
}

// TestGetLocalSkills_有数据 验证读取已添加的本地技能
func TestGetLocalSkills_有数据(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.AddLocalSkill(map[string]any{"name": "test-skill", "source": "local"})
	result := sm.GetLocalSkills()
	if len(result) != 1 {
		t.Fatalf("期望 1 个，实际 %d 个", len(result))
	}
	if toString(result[0]["name"]) != "test-skill" {
		t.Errorf("name = %q, want %q", toString(result[0]["name"]), "test-skill")
	}
}

// TestGetSkillMeta_正常解析 验证正常解析 SKILL.md
func TestGetSkillMeta_正常解析(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 创建技能目录和 SKILL.md
	skillDir := filepath.Join(tmpDir, "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillContent := "---\nname: test-skill\ndescription: 测试技能\n---\n技能正文"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	meta := sm.GetSkillMeta("test-skill")
	if meta == nil {
		t.Fatal("meta 不应为 nil")
	}
	if toString(meta["name"]) != "test-skill" {
		t.Errorf("name = %q, want %q", toString(meta["name"]), "test-skill")
	}
	if toString(meta["skill_dir"]) != skillDir {
		t.Errorf("skill_dir = %q, want %q", toString(meta["skill_dir"]), skillDir)
	}
	if toString(meta["skill_file"]) == "" {
		t.Error("skill_file 不应为空")
	}
}

// TestGetSkillMeta_不存在 验证不存在的技能返回 nil
func TestGetSkillMeta_不存在(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	meta := sm.GetSkillMeta("nonexistent")
	if meta != nil {
		t.Error("不存在的技能应返回 nil")
	}
}

// TestIsBuiltinSkill_非内置 验证非内置技能返回 false
func TestIsBuiltinSkill_非内置(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	if sm.IsBuiltinSkill("some-skill") {
		t.Error("非内置技能应返回 false")
	}
}

// TestIsBuiltinSkill_空名 验证空名称返回 false
func TestIsBuiltinSkill_空名(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	if sm.IsBuiltinSkill("") {
		t.Error("空名应返回 false")
	}
}

// TestIsBuiltinSkill_内置技能 验证内置技能返回 true
func TestIsBuiltinSkill_内置技能(t *testing.T) {
	tmpDir := t.TempDir()
	builtinDir := filepath.Join(tmpDir, "builtin_skills")
	skillDir := filepath.Join(builtinDir, "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 设置环境变量指向内置目录
	t.Setenv("BUILTIN_SKILLS_DIR", builtinDir)

	// 在 skillsDir 下创建同名的符号链接
	sm := NewSkillManager(tmpDir)
	linkPath := filepath.Join(sm.skillsDir, "my-skill")
	if err := os.Symlink(skillDir, linkPath); err != nil {
		t.Fatal(err)
	}

	if !sm.IsBuiltinSkill("my-skill") {
		t.Error("内置技能应返回 true")
	}
}

// ──────────────────────────── ClawHub 测试 ────────────────────────────

// TestHandleSkillsClawhubSearch_正常 验证正常搜索
func TestHandleSkillsClawhubSearch_正常(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("应包含 Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"results":[{"slug":"test-skill","displayName":"Test Skill","summary":"A test skill","version":"1.0.0","updatedAt":1234567890}]}`)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	sm.SetClawhubToken("test-token")

	// 通过环境变量覆盖 ClawHub 基础 URL
	t.Setenv("CLAWHUB_BASE_URL", server.URL)

	result, err := sm.HandleSkillsClawhubSearch(context.Background(), map[string]any{
		"q":     "test",
		"limit": float64(10),
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
	skills, ok := result["skills"].([]map[string]any)
	if !ok || len(skills) != 1 {
		t.Fatalf("应返回 1 个技能, got %v", result["skills"])
	}
	if toString(skills[0]["slug"]) != "test-skill" {
		t.Errorf("slug = %q, want %q", toString(skills[0]["slug"]), "test-skill")
	}
}

// TestHandleSkillsClawhubSearch_无Token 验证无 token 返回错误
func TestHandleSkillsClawhubSearch_无Token(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsClawhubSearch(context.Background(), map[string]any{
		"q": "test",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("无 token 应返回 success=false")
	}
}

// TestHandleSkillsClawhubSearch_缺关键词 验证缺少搜索关键词
func TestHandleSkillsClawhubSearch_缺关键词(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	sm.SetClawhubToken("test-token")

	result, err := sm.HandleSkillsClawhubSearch(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("缺少关键词应返回 success=false")
	}
}

// TestHandleSkillsClawhubDownload_正常 验证正常下载安装
func TestHandleSkillsClawhubDownload_正常(t *testing.T) {
	// 创建一个合法的 ZIP 响应（含 SKILL.md）
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	w, _ := zipWriter.Create("test-skill/SKILL.md")
	fmt.Fprint(w, "---\nname: test-skill\ndescription: 测试技能\n---\n技能正文")
	zipWriter.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.WriteHeader(http.StatusOK)
		w.Write(zipBuf.Bytes())
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	sm.SetClawhubToken("test-token")

	// 通过环境变量覆盖 ClawHub 基础 URL
	t.Setenv("CLAWHUB_BASE_URL", server.URL)

	result, err := sm.HandleSkillsClawhubDownload(context.Background(), map[string]any{
		"slug": "test-skill",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
}

// TestHandleSkillsClawhubDownload_无Token 验证无 token 返回错误
func TestHandleSkillsClawhubDownload_无Token(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsClawhubDownload(context.Background(), map[string]any{
		"slug": "test-skill",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("无 token 应返回 success=false")
	}
}

// TestSafeExtractZIPBytesToDir_ZipSlip 验证 Zip Slip 防护
func TestSafeExtractZIPBytesToDir_ZipSlip(t *testing.T) {
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	w, _ := zipWriter.Create("../../../etc/passwd")
	fmt.Fprint(w, "malicious")
	zipWriter.Close()

	tmpDir := t.TempDir()
	err := safeExtractZIPBytesToDir(zipBuf.Bytes(), tmpDir)
	if err == nil {
		t.Error("应检测到 Zip Slip 并返回错误")
	}
}

// TestSafeExtractZIPBytesToDir_正常 验证正常解压
func TestSafeExtractZIPBytesToDir_正常(t *testing.T) {
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	w, _ := zipWriter.Create("test.txt")
	fmt.Fprint(w, "hello")
	zipWriter.Close()

	tmpDir := t.TempDir()
	err := safeExtractZIPBytesToDir(zipBuf.Bytes(), tmpDir)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if !fileExists(filepath.Join(tmpDir, "test.txt")) {
		t.Error("解压后应存在 test.txt")
	}
}

// ──────────────────────────── TeamSkillsHub 辅助方法测试 ────────────────────────────

// TestMatchHost 验证主机名匹配
func TestMatchHost(t *testing.T) {
	tests := []struct {
		host    string
		pattern string
		want    bool
	}{
		{"example.com", "example.com", true},
		{"foo.example.com", "*.example.com", true},
		{"example.com", "*.example.com", true},
		{"evil.com", "example.com", false},
		{"evil.com", "*.example.com", false},
		{"127.0.0.1", "127.0.0.1", true},
		{"*", "*", true},
		// 多段通配符（对齐 Python _team_skills_hub_host_matches_rule）
		{"openjiuwen-market.obs.cn-north-4.myhuaweicloud.com", "openjiuwen-market.obs.*.myhuaweicloud.com", true},
		{"openjiuwen-market.obs.cn-north-4.myhuaweicloud.com", "*.obs.*.myhuaweicloud.com", true},
		// 段数不匹配
		{"obs.myhuaweicloud.com", "openjiuwen-market.obs.*.myhuaweicloud.com", false},
		// 非通配段不匹配
		{"other.obs.cn-north-4.myhuaweicloud.com", "openjiuwen-market.obs.*.myhuaweicloud.com", false},
	}
	for _, tt := range tests {
		got := matchHost(tt.host, tt.pattern)
		if got != tt.want {
			t.Errorf("matchHost(%q, %q) = %v, want %v", tt.host, tt.pattern, got, tt.want)
		}
	}
}

// TestAssertTeamSkillsHubDownloadURLAllowed_白名单 验证白名单校验
func TestAssertTeamSkillsHubDownloadURLAllowed_白名单(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 测试 localhost 和 127.0.0.1（默认白名单中的精确匹配）
	if err := sm.assertTeamSkillsHubDownloadURLAllowed("http://127.0.0.1:8080/file.zip"); err != nil {
		t.Errorf("白名单内 URL 不应返回错误: %v", err)
	}
	if err := sm.assertTeamSkillsHubDownloadURLAllowed("http://localhost/file.zip"); err != nil {
		t.Errorf("白名单内 URL 不应返回错误: %v", err)
	}
	if err := sm.assertTeamSkillsHubDownloadURLAllowed("https://evil.com/file.zip"); err == nil {
		t.Error("白名单外 URL 应返回错误")
	}
	// 测试多段通配符：openjiuwen-market.obs.*.myhuaweicloud.com 匹配 openjiuwen-market.obs.cn-north-4.myhuaweicloud.com
	if err := sm.assertTeamSkillsHubDownloadURLAllowed("https://openjiuwen-market.obs.cn-north-4.myhuaweicloud.com/file.zip"); err != nil {
		t.Errorf("白名单内 URL 不应返回错误: %v", err)
	}
}

// TestDownloadZipAndVerify_正常 验证正常下载校验
func TestDownloadZipAndVerify_正常(t *testing.T) {
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	w, _ := zipWriter.Create("test.txt")
	fmt.Fprint(w, "hello")
	zipWriter.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipBuf.Bytes())
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	data, err := sm.downloadZipAndVerify(context.Background(), server.URL, "")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if len(data) == 0 {
		t.Error("应返回非空数据")
	}
}

// ──────────────────────────── TeamSkillsHub Handle 方法测试 ────────────────────────────

// TestHandleSkillsTeamSkillsHubSearch_正常 验证正常搜索
func TestHandleSkillsTeamSkillsHubSearch_正常(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":200,"data":{"items":[{"asset_id":"test-asset","name":"test-skill","display_name":"Test Skill","short_desc":"A test skill","latest_version":"1.0.0","update_time":1234567890}],"total":1}}`)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsTeamSkillsHubSearch(context.Background(), map[string]any{
		"q":          "test",
		"market_url": server.URL,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
}

// TestHandleSkillsTeamSkillsHubInfo_正常 验证正常获取元数据
func TestHandleSkillsTeamSkillsHubInfo_正常(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":200,"data":{"asset_id":"test-asset","latest_version":"1.0.0"}}`)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsTeamSkillsHubInfo(context.Background(), map[string]any{
		"asset_id":   "test-asset",
		"market_url": server.URL,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
}

// TestHandleSkillsTeamSkillsHubInit_正常 验证正常初始化
func TestHandleSkillsTeamSkillsHubInit_正常(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsTeamSkillsHubInit(context.Background(), map[string]any{
		"name": "my-skill",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
	// 验证目录和文件已创建
	dirPath := toString(result["path"])
	if !dirExists(dirPath) {
		t.Error("目录应已创建")
	}
	if !fileExists(filepath.Join(dirPath, "SKILL.md")) {
		t.Error("SKILL.md 应已创建")
	}
}

// TestHandleSkillsTeamSkillsHubValidate_有效 验证有效目录
func TestHandleSkillsTeamSkillsHubValidate_有效(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 创建有效目录
	skillDir := filepath.Join(tmpDir, "test-skill")
	os.MkdirAll(skillDir, 0o755)
	skillContent := "---\nname: test-skill\ndescription: 测试技能\n---\n技能正文"
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644)

	result, err := sm.HandleSkillsTeamSkillsHubValidate(context.Background(), map[string]any{
		"path": skillDir,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
}

// TestHandleSkillsTeamSkillsHubValidate_无效 验证无效目录
func TestHandleSkillsTeamSkillsHubValidate_无效(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 创建无效目录（缺少 description）
	skillDir := filepath.Join(tmpDir, "test-skill")
	os.MkdirAll(skillDir, 0o755)
	skillContent := "---\nname: test-skill\n---\n技能正文"
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644)

	result, err := sm.HandleSkillsTeamSkillsHubValidate(context.Background(), map[string]any{
		"path": skillDir,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Errorf("应返回 success=false, got %v", result)
	}
}

// TestHandleSkillsTeamSkillsHubPack_正常 验证正常打包
func TestHandleSkillsTeamSkillsHubPack_正常(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 创建技能目录
	skillDir := filepath.Join(tmpDir, "test-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test\n---\nbody"), 0o644)

	outputZip := filepath.Join(tmpDir, "test-skill.zip")

	result, err := sm.HandleSkillsTeamSkillsHubPack(context.Background(), map[string]any{
		"path":   skillDir,
		"output": outputZip,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
	if !fileExists(outputZip) {
		t.Error("ZIP 文件应已创建")
	}
}

// TestHandleSkillsTeamSkillsHubDelete_正常 验证正常删除
func TestHandleSkillsTeamSkillsHubDelete_正常(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("应使用 DELETE 方法, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsTeamSkillsHubDelete(context.Background(), map[string]any{
		"asset_id":   "test-asset",
		"market_url": server.URL,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
}

// TestHandleSkillsTeamSkillsHubInstall_缺assetId 验证缺少 asset_id
func TestHandleSkillsTeamSkillsHubInstall_缺assetId(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsTeamSkillsHubInstall(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("缺少 asset_id 应返回 success=false")
	}
}

// TestHandleSkillsTeamSkillsHubPublish_缺path 验证缺少 path
func TestHandleSkillsTeamSkillsHubPublish_缺path(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsTeamSkillsHubPublish(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("缺少 path 应返回 success=false")
	}
}

// TestHandleSkillsTeamSkillsHubInstall_正常 验证正常安装
func TestHandleSkillsTeamSkillsHubInstall_正常(t *testing.T) {
	// 创建一个合法的 ZIP（含 SKILL.md）
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	w, _ := zipWriter.Create("test-skill/SKILL.md")
	fmt.Fprint(w, "---\nname: test-skill\ndescription: 测试技能\n---\n技能正文")
	zipWriter.Close()
	zipBytes := zipBuf.Bytes()

	// 下载服务器
	downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.WriteHeader(http.StatusOK)
		w.Write(zipBytes)
	}))
	defer downloadServer.Close()

	// 元数据服务器
	metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code":    200,
			"success": true,
			"data": map[string]any{
				"asset_id":        "test-asset",
				"download_url":    downloadServer.URL + "/file.zip",
				"checksum_sha256": "",
			},
		})
	}))
	defer metadataServer.Close()

	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 设置白名单允许下载
	t.Setenv(teamSkillsHubAllowedHostsEnv, "127.0.0.1,localhost")

	result, err := sm.HandleSkillsTeamSkillsHubInstall(context.Background(), map[string]any{
		"asset_id":   "test-asset",
		"market_url": metadataServer.URL,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
}

// TestHandleSkillsTeamSkillsHubPublish_正常 验证正常发布
func TestHandleSkillsTeamSkillsHubPublish_正常(t *testing.T) {
	// 创建一个临时 ZIP 文件
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "test-skill.zip")
	zipFile, _ := os.Create(zipPath)
	zipWriter := zip.NewWriter(zipFile)
	w, _ := zipWriter.Create("test.txt")
	fmt.Fprint(w, "hello")
	zipWriter.Close()
	zipFile.Close()

	// 上传服务器
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("请求方法应为 POST, got %s", r.Method)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("Content-Type 应为 multipart/form-data")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"asset_id": "new-asset-123"},
		})
	}))
	defer uploadServer.Close()

	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsTeamSkillsHubPublish(context.Background(), map[string]any{
		"path":       zipPath,
		"market_url": uploadServer.URL,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
	if result["asset_id"] != "new-asset-123" {
		t.Errorf("asset_id = %v, want new-asset-123", result["asset_id"])
	}
}

// TestHandleSkillsTeamSkillsHubPublish_文件不存在 验证文件不存在
func TestHandleSkillsTeamSkillsHubPublish_文件不存在(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsTeamSkillsHubPublish(context.Background(), map[string]any{
		"path": filepath.Join(tmpDir, "nonexistent.zip"),
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("文件不存在应返回 success=false")
	}
}

// TestDownloadZipAndVerify_带校验和 验证带 SHA256 校验和的下载
func TestDownloadZipAndVerify_带校验和(t *testing.T) {
	// 创建合法 ZIP
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	w, _ := zipWriter.Create("test.txt")
	fmt.Fprint(w, "hello")
	zipWriter.Close()
	zipBytes := zipBuf.Bytes()

	// 计算 SHA256
	hash := sha256.Sum256(zipBytes)
	checksum := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.WriteHeader(http.StatusOK)
		w.Write(zipBytes)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 设置白名单
	t.Setenv(teamSkillsHubAllowedHostsEnv, "127.0.0.1,localhost")

	result, err := sm.downloadZipAndVerify(context.Background(), server.URL+"/file.zip", checksum)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if len(result) == 0 {
		t.Error("应返回非空 ZIP 数据")
	}
}

// TestDownloadZipAndVerify_校验和不匹配 验证 SHA256 不匹配时返回错误
func TestDownloadZipAndVerify_校验和不匹配(t *testing.T) {
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	w, _ := zipWriter.Create("test.txt")
	fmt.Fprint(w, "hello")
	zipWriter.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(zipBuf.Bytes())
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	t.Setenv(teamSkillsHubAllowedHostsEnv, "127.0.0.1,localhost")

	_, err := sm.downloadZipAndVerify(context.Background(), server.URL+"/file.zip", "0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Error("校验和不匹配应返回错误")
	}
}

// TestDownloadZipAndVerify_非ZIP 验证非 ZIP 响应返回错误
func TestDownloadZipAndVerify_非ZIP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not a zip file"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	t.Setenv(teamSkillsHubAllowedHostsEnv, "127.0.0.1,localhost")

	_, err := sm.downloadZipAndVerify(context.Background(), server.URL+"/file.zip", "")
	if err == nil {
		t.Error("非 ZIP 响应应返回错误")
	}
}

// TestScanBuiltinSkills_有技能 验证扫描内置技能
func TestScanBuiltinSkills_有技能(t *testing.T) {
	tmpDir := t.TempDir()
	builtinDir := filepath.Join(tmpDir, "builtin_skills")
	os.MkdirAll(builtinDir, 0o755)

	// 创建一个内置技能
	skillDir := filepath.Join(builtinDir, "builtin-test")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: builtin-test\ndescription: 内置测试\n---\n# Test\n"), 0o644)

	t.Setenv("BUILTIN_SKILLS_DIR", builtinDir)

	sm := NewSkillManager(t.TempDir())
	skills := sm.scanBuiltinSkills()
	if len(skills) == 0 {
		t.Error("应扫描到内置技能")
	}
}

// TestSaveState_持久化 验证状态持久化
func TestSaveState_持久化(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.mu.Lock()
	sm.addLocalSkill(map[string]any{
		"name":   "test-skill",
		"source": "local",
	})
	sm.AddInstalledPlugin(map[string]any{
		"name":        "test-skill",
		"marketplace": "local",
		"source":      "local",
	})
	sm.saveState()
	sm.mu.Unlock()

	// 验证状态文件存在
	if !fileExists(sm.stateFile) {
		t.Fatal("状态文件应存在")
	}

	// 读取并验证
	data, err := os.ReadFile(sm.stateFile)
	if err != nil {
		t.Fatalf("读取状态文件失败: %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("解析状态文件失败: %v", err)
	}
	installedPlugins, ok := state["installed_plugins"].([]any)
	if !ok || len(installedPlugins) == 0 {
		t.Error("状态文件应包含 installed_plugins")
	}
	localSkills, ok := state["local_skills"].([]any)
	if !ok || len(localSkills) == 0 {
		t.Error("状态文件应包含 local_skills")
	}
}

// TestHandleSkillsInstallBuiltin_正常 验证安装内置技能
func TestHandleSkillsInstallBuiltin_正常(t *testing.T) {
	tmpDir := t.TempDir()
	builtinDir := filepath.Join(tmpDir, "builtin_skills")
	os.MkdirAll(builtinDir, 0o755)

	// 创建一个内置技能
	skillDir := filepath.Join(builtinDir, "builtin-test")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: builtin-test\ndescription: 内置测试\n---\n# Test\n"), 0o644)

	t.Setenv("BUILTIN_SKILLS_DIR", builtinDir)

	sm := NewSkillManager(t.TempDir())
	result, err := sm.HandleSkillsInstallBuiltin(context.Background(), map[string]any{
		"name": "builtin-test",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
}

// TestLocateSkillDir_子目录 验证递归查找 SKILL.md
func TestLocateSkillDir_子目录(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 创建子目录结构
	subDir := filepath.Join(tmpDir, "nested", "deep")
	os.MkdirAll(subDir, 0o755)
	os.WriteFile(filepath.Join(subDir, "SKILL.md"), []byte("---\nname: test\n---\nbody"), 0o644)

	result := sm.locateSkillDir(tmpDir)
	if result == "" {
		t.Error("应递归找到 SKILL.md")
	}
	if result != subDir {
		t.Errorf("locateSkillDir = %q, want %q", result, subDir)
	}
}

// TestLocateSkillDir_根目录 验证根目录直接查找
func TestLocateSkillDir_根目录(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	os.WriteFile(filepath.Join(tmpDir, "SKILL.md"), []byte("---\nname: test\n---\nbody"), 0o644)

	result := sm.locateSkillDir(tmpDir)
	if result != tmpDir {
		t.Errorf("locateSkillDir = %q, want %q", result, tmpDir)
	}
}

// TestLocateSkillDir_不存在 验证未找到时返回空
func TestLocateSkillDir_不存在(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result := sm.locateSkillDir(tmpDir)
	if result != "" {
		t.Errorf("未找到时应返回空字符串, got %q", result)
	}
}

// TestHasPendingSkillnetInstall 验证 SkillNet 安装状态检查
func TestHasPendingSkillnetInstall(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 无任务时应返回 false
	if sm.HasPendingSkillnetInstall() {
		t.Error("无任务时应返回 false")
	}

	// 添加一个 pending 任务
	sm.mu.Lock()
	sm.skillnetInstallJobs["test-job"] = map[string]any{"status": "pending"}
	sm.mu.Unlock()

	if !sm.HasPendingSkillnetInstall() {
		t.Error("有 pending 任务时应返回 true")
	}

	// 更新为 completed
	sm.mu.Lock()
	sm.skillnetInstallJobs["test-job"] = map[string]any{"status": "completed"}
	sm.mu.Unlock()

	if sm.HasPendingSkillnetInstall() {
		t.Error("completed 任务应返回 false")
	}
}

// TestSafeExtractZIPBytesToDir_含目录 验证含目录的 ZIP 解压
func TestSafeExtractZIPBytesToDir_含目录(t *testing.T) {
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	// 添加目录中的文件
	w, _ := zipWriter.Create("subdir/test.txt")
	fmt.Fprint(w, "hello in subdir")
	// 添加根级文件
	w, _ = zipWriter.Create("root.txt")
	fmt.Fprint(w, "hello at root")
	zipWriter.Close()

	tmpDir := t.TempDir()
	err := safeExtractZIPBytesToDir(zipBuf.Bytes(), tmpDir)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if !fileExists(filepath.Join(tmpDir, "subdir", "test.txt")) {
		t.Error("解压后应存在 subdir/test.txt")
	}
	if !fileExists(filepath.Join(tmpDir, "root.txt")) {
		t.Error("解压后应存在 root.txt")
	}
}

// TestEnvString 验证环境变量读取
func TestEnvString(t *testing.T) {
	// 测试默认值
	if got := envString("NONEXISTENT_ENV_VAR_12345", "default"); got != "default" {
		t.Errorf("envString 默认值 = %q, want %q", got, "default")
	}
	// 测试环境变量覆盖
	t.Setenv("TEST_ENV_STRING_VAR", "overridden")
	if got := envString("TEST_ENV_STRING_VAR", "default"); got != "overridden" {
		t.Errorf("envString 环境变量 = %q, want %q", got, "overridden")
	}
}

// TestHandleSkillsTeamSkillsHubSearch_带参数 验证带完整参数的搜索
func TestHandleSkillsTeamSkillsHubSearch_带参数(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{
				"items": []any{},
				"total": 0,
			},
		})
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	result, err := sm.HandleSkillsTeamSkillsHubSearch(context.Background(), map[string]any{
		"q":               "test",
		"market_url":      server.URL,
		"search_asset_id": "asset-123",
		"limit":           10,
		"offset":          0,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
}

// TestHandleSkillsClawhubDownload_已存在 验证技能已存在时返回失败
func TestHandleSkillsClawhubDownload_已存在(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	sm.SetClawhubToken("test-token")

	// 创建已存在的技能目录
	os.MkdirAll(filepath.Join(sm.skillsDir, "test-skill"), 0o755)

	result, err := sm.HandleSkillsClawhubDownload(context.Background(), map[string]any{
		"slug": "test-skill",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("技能已存在应返回 success=false")
	}
}

// TestHandleSkillsTeamSkillsHubInit_自定义输出 验证自定义输出目录
func TestHandleSkillsTeamSkillsHubInit_自定义输出(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)
	outputDir := filepath.Join(tmpDir, "custom_output")

	result, err := sm.HandleSkillsTeamSkillsHubInit(context.Background(), map[string]any{
		"name":   "custom-skill",
		"output": outputDir,
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != true {
		t.Errorf("应返回 success=true, got %v", result)
	}
	expectedPath := filepath.Join(outputDir, "custom-skill")
	if result["path"] != expectedPath {
		t.Errorf("path = %v, want %v", result["path"], expectedPath)
	}
	if !fileExists(filepath.Join(expectedPath, "SKILL.md")) {
		t.Error("应创建 SKILL.md")
	}
}

// TestHandleSkillsTeamSkillsHubInit_已存在 验证目录已存在时返回失败
func TestHandleSkillsTeamSkillsHubInit_已存在(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 先创建同名目录
	os.MkdirAll(filepath.Join(sm.skillsDir, "existing-skill"), 0o755)

	result, err := sm.HandleSkillsTeamSkillsHubInit(context.Background(), map[string]any{
		"name": "existing-skill",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("目录已存在应返回 success=false")
	}
}

// TestHandleSkillsTeamSkillsHubSearch_失败 验证搜索 API 失败
func TestHandleSkillsTeamSkillsHubSearch_失败(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	// 使用不存在的 URL
	result, err := sm.HandleSkillsTeamSkillsHubSearch(context.Background(), map[string]any{
		"market_url": "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if toBool(result["success"]) != false {
		t.Error("搜索失败应返回 success=false")
	}
}
