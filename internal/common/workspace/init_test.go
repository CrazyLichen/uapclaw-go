package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInit_默认实例首次初始化 测试默认实例的首次初始化
func TestInit_默认实例首次初始化(t *testing.T) {
	tmpDir := t.TempDir()

	// 创建 resources 目录结构
	resDir := filepath.Join(tmpDir, "resources")
	setupTestResources(t, resDir)

	os.Setenv(EnvDataDir, filepath.Join(tmpDir, "workspace"))
	os.Setenv(EnvResourcesDir, resDir)
	defer func() {
		os.Unsetenv(EnvDataDir)
		os.Unsetenv(EnvResourcesDir)
		SetUserHome("")
	}()
	SetUserHome("")

	result, err := Init(InitOption{
		Language: "zh",
	})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	if result.Cancelled {
		t.Error("不应取消")
	}

	// 验证关键文件存在
	ws := filepath.Join(tmpDir, "workspace")
	assertFileExists(t, filepath.Join(ws, "config", "config.yaml"))
	assertFileExists(t, filepath.Join(ws, "config", ".env"))
	assertDirExists(t, filepath.Join(ws, "agent", ".checkpoint"))
	assertDirExists(t, filepath.Join(ws, "agent", ".logs"))
	assertDirExists(t, filepath.Join(ws, "agent", "sessions"))
	assertFileExists(t, filepath.Join(ws, "agent", "workspace", "AGENT.md"))
	assertFileExists(t, filepath.Join(ws, "agent", "workspace", "SOUL.md"))
	assertFileExists(t, filepath.Join(ws, "agent", "workspace", "IDENTITY.md"))
	assertFileExists(t, filepath.Join(ws, "agent", "workspace", "HEARTBEAT.md"))
	assertFileExists(t, filepath.Join(ws, "agent", "workspace", "USER.md"))
	assertFileExists(t, filepath.Join(ws, "agent", "workspace", "memory", "MEMORY.md"))
}

// TestInit_英文语言 测试英文语言初始化
func TestInit_英文语言(t *testing.T) {
	tmpDir := t.TempDir()
	resDir := filepath.Join(tmpDir, "resources")
	setupTestResources(t, resDir)

	os.Setenv(EnvDataDir, filepath.Join(tmpDir, "workspace"))
	os.Setenv(EnvResourcesDir, resDir)
	defer func() {
		os.Unsetenv(EnvDataDir)
		os.Unsetenv(EnvResourcesDir)
		SetUserHome("")
	}()
	SetUserHome("")

	result, err := Init(InitOption{
		Language: "en",
	})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	if result.Cancelled {
		t.Error("不应取消")
	}

	// 验证 AGENT.md 内容是英文
	ws := filepath.Join(tmpDir, "workspace")
	data, err := os.ReadFile(filepath.Join(ws, "agent", "workspace", "AGENT.md"))
	if err != nil {
		t.Fatalf("读取 AGENT.md 失败: %v", err)
	}
	if !strings.Contains(string(data), "English content") {
		t.Errorf("AGENT.md 应该是英文版，实际内容: %q", string(data))
	}
}

// TestInit_命名实例 测试命名实例初始化
func TestInit_命名实例(t *testing.T) {
	tmpDir := t.TempDir()
	resDir := filepath.Join(tmpDir, "resources")
	setupTestResources(t, resDir)

	os.Setenv(EnvHome, tmpDir)
	os.Setenv(EnvDataDir, filepath.Join(tmpDir, "workspace"))
	os.Setenv(EnvResourcesDir, resDir)
	defer func() {
		os.Unsetenv(EnvHome)
		os.Unsetenv(EnvDataDir)
		os.Unsetenv(EnvResourcesDir)
		SetUserHome("")
	}()
	SetUserHome("")

	result, err := Init(InitOption{
		InstanceName: "alice",
		Language:     "zh",
	})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	if result.Cancelled {
		t.Error("不应取消")
	}

	// 验证实例工作区
	instanceWs := InstanceWorkspacePath("alice")
	assertFileExists(t, filepath.Join(instanceWs, "config", "config.yaml"))

	// 验证 instances.yaml 已更新
	config, err := GetInstanceConfig("alice")
	if err != nil {
		t.Fatalf("GetInstanceConfig 失败: %v", err)
	}
	if config == nil {
		t.Fatal("alice 实例应存在于 instances.yaml")
	}

	// 验证 bootstrap .env 存在
	assertFileExists(t, filepath.Join(instanceWs, ".env"))
}

// TestInit_无效实例名 测试无效的实例名称
func TestInit_无效实例名(t *testing.T) {
	_, err := Init(InitOption{
		InstanceName: "default", // 保留名称
		Language:     "zh",
	})
	if err == nil {
		t.Error("保留名称应返回错误")
	}
}

// TestPrepare_增量模式 测试增量模式不覆盖已有文件
func TestPrepare_增量模式(t *testing.T) {
	tmpDir := t.TempDir()
	resDir := filepath.Join(tmpDir, "resources")
	setupTestResources(t, resDir)

	workspaceDir := filepath.Join(tmpDir, "workspace")

	// 首次初始化
	os.Setenv(EnvResourcesDir, resDir)
	defer os.Unsetenv(EnvResourcesDir)

	_, err := Prepare(InitOption{
		Language:     "zh",
		WorkspaceDir: workspaceDir,
	})
	if err != nil {
		t.Fatalf("首次 Prepare 失败: %v", err)
	}

	// 修改 AGENT.md
	agentMD := filepath.Join(workspaceDir, "agent", "workspace", "AGENT.md")
	os.WriteFile(agentMD, []byte("用户自定义内容"), 0o644)

	// 增量再次 Prepare（overwrite=false）
	_, err = Prepare(InitOption{
		Language:     "zh",
		WorkspaceDir: workspaceDir,
	})
	if err != nil {
		t.Fatalf("增量 Prepare 失败: %v", err)
	}

	// 验证用户内容未被覆盖
	data, _ := os.ReadFile(agentMD)
	if string(data) != "用户自定义内容" {
		t.Error("增量模式不应覆盖已有文件")
	}
}

// TestSetPreferredLanguage 测试语言偏好写入
func TestSetPreferredLanguage(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// 初始内容
	os.WriteFile(configPath, []byte("server:\n  host: localhost\n"), 0o644)

	setPreferredLanguage(configPath, "zh")

	data, _ := os.ReadFile(configPath)
	if !strings.Contains(string(data), "preferred_language: zh") {
		t.Errorf("应包含 preferred_language: zh，实际: %s", string(data))
	}

	// 更新已有值
	setPreferredLanguage(configPath, "en")

	data, _ = os.ReadFile(configPath)
	if !strings.Contains(string(data), "preferred_language: en") {
		t.Errorf("应包含 preferred_language: en，实际: %s", string(data))
	}
}

// TestResolvePreferredLanguage 测试语言解析优先级
func TestResolvePreferredLanguage(t *testing.T) {
	// 显式参数
	lang := resolvePreferredLanguage("en", "/nonexistent")
	if lang != "en" {
		t.Errorf("显式参数应优先，期望 en，实际 %s", lang)
	}

	// 无效参数降级为 zh
	lang = resolvePreferredLanguage("invalid", "/nonexistent")
	if lang != "zh" {
		t.Errorf("无效参数应降级为 zh，实际 %s", lang)
	}

	// 默认 zh
	lang = resolvePreferredLanguage("", "/nonexistent")
	if lang != "zh" {
		t.Errorf("默认应为 zh，实际 %s", lang)
	}
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// setupTestResources 创建测试用的 resources 目录结构
func setupTestResources(t *testing.T, resDir string) {
	t.Helper()

	// 创建目录结构
	dirs := []string{
		filepath.Join(resDir, "agent", "workspace", "memory"),
	}
	for _, d := range dirs {
		os.MkdirAll(d, 0o755)
	}

	// config.yaml
	os.WriteFile(filepath.Join(resDir, "config.yaml"), []byte("server:\n  host: localhost\n"), 0o644)

	// .env.template
	os.WriteFile(filepath.Join(resDir, ".env.template"), []byte("API_KEY=sk-test\n"), 0o644)

	// 多语言文件
	ws := filepath.Join(resDir, "agent", "workspace")
	os.WriteFile(filepath.Join(ws, "AGENT_ZH.md"), []byte("# 智能体\n中文内容"), 0o644)
	os.WriteFile(filepath.Join(ws, "AGENT_EN.md"), []byte("# AGENT\nEnglish content"), 0o644)
	os.WriteFile(filepath.Join(ws, "HEARTBEAT_ZH.md"), []byte("心跳中文"), 0o644)
	os.WriteFile(filepath.Join(ws, "HEARTBEAT_EN.md"), []byte("Heartbeat English"), 0o644)
	os.WriteFile(filepath.Join(ws, "IDENTITY_ZH.md"), []byte("身份中文"), 0o644)
	os.WriteFile(filepath.Join(ws, "IDENTITY_EN.md"), []byte("Identity English"), 0o644)
	os.WriteFile(filepath.Join(ws, "SOUL_ZH.md"), []byte("灵魂中文"), 0o644)
	os.WriteFile(filepath.Join(ws, "SOUL_EN.md"), []byte("Soul English"), 0o644)
	os.WriteFile(filepath.Join(ws, "USER.md"), []byte("# User"), 0o644)
	os.WriteFile(filepath.Join(ws, "agent-data.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(ws, "memory", "MEMORY_ZH.md"), []byte("记忆中文"), 0o644)
	os.WriteFile(filepath.Join(ws, "memory", "MEMORY_EN.md"), []byte("Memory English"), 0o644)
}

// assertFileExists 断言文件存在
func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("文件应存在: %s", path)
	}
}

// assertDirExists 断言目录存在
func assertDirExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		t.Errorf("目录应存在: %s", path)
		return
	}
	if !info.IsDir() {
		t.Errorf("路径应为目录: %s", path)
	}
}
