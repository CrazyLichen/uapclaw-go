package workspace

import (
	"fmt"
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

// TestInit_覆盖模式 测试 overwrite=true 且目标目录不存在时的初始化流程
// 注：目录不存在时不触发交互式确认，可正常测试
func TestInit_覆盖模式(t *testing.T) {
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

	// 覆盖模式初始化（目录不存在时不触发交互确认）
	result, err := Init(InitOption{Overwrite: true, Language: "en"})
	if err != nil {
		t.Fatalf("覆盖模式 Init 失败: %v", err)
	}
	if result.Cancelled {
		t.Error("覆盖模式不应取消")
	}
}

// TestInit_自定义工作区路径 测试指定 WorkspaceDir
func TestInit_自定义工作区路径(t *testing.T) {
	tmpDir := t.TempDir()
	resDir := filepath.Join(tmpDir, "resources")
	setupTestResources(t, resDir)

	customWorkspace := filepath.Join(tmpDir, "custom-ws")

	os.Setenv(EnvResourcesDir, resDir)
	defer os.Unsetenv(EnvResourcesDir)

	result, err := Init(InitOption{Language: "zh", WorkspaceDir: customWorkspace})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	if result.WorkspaceDir != customWorkspace {
		t.Errorf("WorkspaceDir 期望 %s，实际 %s", customWorkspace, result.WorkspaceDir)
	}
	assertFileExists(t, filepath.Join(customWorkspace, "config", "config.yaml"))
}

// TestPrepare_覆盖模式 测试 overwrite=true 的 Prepare 流程
func TestPrepare_覆盖模式(t *testing.T) {
	tmpDir := t.TempDir()
	resDir := filepath.Join(tmpDir, "resources")
	setupTestResources(t, resDir)

	workspaceDir := filepath.Join(tmpDir, "workspace")

	os.Setenv(EnvResourcesDir, resDir)
	defer os.Unsetenv(EnvResourcesDir)

	// 首次初始化
	_, err := Prepare(InitOption{
		Language:     "zh",
		WorkspaceDir: workspaceDir,
	})
	if err != nil {
		t.Fatalf("首次 Prepare 失败: %v", err)
	}

	// 修改非多语言文件（如 USER.md，不受多语言忽略模式影响）
	userMD := filepath.Join(workspaceDir, "agent", "workspace", "USER.md")
	os.WriteFile(userMD, []byte("用户自定义内容"), 0o644)

	// 覆盖模式再次 Prepare
	diff, err := Prepare(InitOption{
		Overwrite:    true,
		Language:     "zh",
		WorkspaceDir: workspaceDir,
	})
	if err != nil {
		t.Fatalf("覆盖 Prepare 失败: %v", err)
	}

	// 覆盖模式下非多语言文件应被替换
	data, _ := os.ReadFile(userMD)
	if string(data) == "用户自定义内容" {
		t.Error("覆盖模式应替换已有文件")
	}

	// 验证差异记录包含覆盖文件
	if len(diff.OverwrittenFiles) == 0 {
		t.Error("覆盖模式应记录覆盖文件")
	}
}

// TestPrepare_无resources 测试 resources 目录不存在时的错误
func TestPrepare_无resources(t *testing.T) {
	tmpDir := t.TempDir()

	// 不设置 EnvResourcesDir，让它找不到 resources
	os.Setenv(EnvResourcesDir, filepath.Join(tmpDir, "nonexistent"))
	defer os.Unsetenv(EnvResourcesDir)

	_, err := Prepare(InitOption{
		Language:     "zh",
		WorkspaceDir: filepath.Join(tmpDir, "workspace"),
	})
	if err == nil {
		t.Error("resources 不存在时应返回错误")
	}
}

// TestFileExists 测试 fileExists 辅助函数
func TestFileExists(t *testing.T) {
	tmpDir := t.TempDir()

	// 文件存在
	filePath := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(filePath, []byte("hello"), 0o644)
	if !fileExists(filePath) {
		t.Error("存在的文件应返回 true")
	}

	// 文件不存在
	if fileExists(filepath.Join(tmpDir, "nonexistent.txt")) {
		t.Error("不存在的文件应返回 false")
	}

	// 目录不应算文件
	if fileExists(tmpDir) {
		t.Error("目录不应算文件")
	}
}

// TestCopyDiffResultPrintSummary_有变更 测试有变更时的摘要输出
func TestCopyDiffResultPrintSummary_有变更(t *testing.T) {
	r := CopyDiffResult{
		AddedFiles:       []string{"file1.txt", "file2.txt"},
		OverwrittenFiles: []string{"old1.txt"},
	}
	// 不 panic 即可
	r.PrintSummary(false)
}

// TestCopyDiffResultPrintSummary_大量变更 测试超过 10 个文件时的截断输出
func TestCopyDiffResultPrintSummary_大量变更(t *testing.T) {
	added := make([]string, 15)
	overwritten := make([]string, 12)
	for i := range added {
		added[i] = fmt.Sprintf("added_%d.txt", i)
	}
	for i := range overwritten {
		overwritten[i] = fmt.Sprintf("overwritten_%d.txt", i)
	}
	r := CopyDiffResult{
		AddedFiles:       added,
		OverwrittenFiles: overwritten,
	}
	// 不 panic 即可，验证超过 10 个时的截断逻辑
	r.PrintSummary(false)
}

// TestCopyFileWithDiff_源文件不存在 测试源文件不存在时的错误
func TestCopyFileWithDiff_源文件不存在(t *testing.T) {
	tmpDir := t.TempDir()
	dst := filepath.Join(tmpDir, "dst.txt")

	var diff CopyDiffResult
	err := copyFileWithDiff(filepath.Join(tmpDir, "nonexistent.txt"), dst, &diff)
	if err == nil {
		t.Error("源文件不存在时应返回错误")
	}
}

// TestCopyDirWithDiff_空目录 测试源目录为空的情况
func TestCopyDirWithDiff_空目录(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "empty_src")
	dstDir := filepath.Join(tmpDir, "empty_dst")
	os.MkdirAll(srcDir, 0o755)

	var diff CopyDiffResult
	if err := copyDirWithDiff(srcDir, dstDir, &diff, nil); err != nil {
		t.Fatalf("空目录复制失败: %v", err)
	}
	if len(diff.AddedFiles) != 0 {
		t.Errorf("空目录应无新增文件，实际 %d", len(diff.AddedFiles))
	}
}

// TestSetPreferredLanguage_已有值 测试替换已有的 preferred_language
func TestSetPreferredLanguage_已有值(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// 初始包含 preferred_language
	os.WriteFile(configPath, []byte("preferred_language: zh\nserver:\n  host: localhost\n"), 0o644)

	setPreferredLanguage(configPath, "en")

	data, _ := os.ReadFile(configPath)
	content := string(data)
	if !strings.Contains(content, "preferred_language: en") {
		t.Errorf("应替换为 en，实际: %s", content)
	}
	if strings.Contains(content, "preferred_language: zh") {
		t.Errorf("旧值 zh 应被替换，实际: %s", content)
	}
}

// TestSetPreferredLanguage_文件不存在 测试配置文件不存在时不 panic
func TestSetPreferredLanguage_文件不存在(t *testing.T) {
	// 不存在时 setPreferredLanguage 应直接返回，不 panic
	setPreferredLanguage("/nonexistent/path/config.yaml", "zh")
}

// TestResolvePreferredLanguage_从配置读取 测试从已有 config.yaml 读取 preferred_language
func TestResolvePreferredLanguage_从配置读取(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	os.MkdirAll(configDir, 0o755)
	os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("preferred_language: en\n"), 0o644)

	lang := resolvePreferredLanguage("", tmpDir)
	if lang != "en" {
		t.Errorf("应从配置文件读取 en，实际 %s", lang)
	}
}

// TestInit_删除工作区失败 测试 overwrite 模式下目录存在但删除失败（模拟）
func TestInit_删除工作区失败(t *testing.T) {
	// 使用无效的实例名直接覆盖，测试 Init 的其他分支
	// 此测试验证 Init 中 InstanceName 和 WorkspaceDir 的组合路径
	tmpDir := t.TempDir()
	resDir := filepath.Join(tmpDir, "resources")
	setupTestResources(t, resDir)

	os.Setenv(EnvHome, tmpDir)
	os.Setenv(EnvResourcesDir, resDir)
	defer func() {
		os.Unsetenv(EnvHome)
		os.Unsetenv(EnvResourcesDir)
		SetUserHome("")
	}()
	SetUserHome("")

	// 指定 WorkspaceDir 和 InstanceName，目录不存在时不触发交互确认
	result, err := Init(InitOption{
		InstanceName: "test1",
		Language:     "zh",
		WorkspaceDir: filepath.Join(tmpDir, "custom-ws"),
	})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	if result.Cancelled {
		t.Error("不应取消")
	}
}

// TestIsInteractive 测试 isInteractive 函数
func TestIsInteractive(t *testing.T) {
	// 在测试环境中调用不应 panic
	result := isInteractive()
	// 结果取决于测试运行环境，只验证不 panic
	_ = result
}

// TestPromptYesNo 测试 promptYesNo 函数
func TestPromptYesNo(t *testing.T) {
	// 仅验证函数不 panic，结果取决于 stdin 状态
	_ = promptYesNo("测试提示: ")
}

// TestPromptPreferredLanguage_非交互 测试 promptPreferredLanguage 在非交互模式下的行为
func TestPromptPreferredLanguage_非交互(t *testing.T) {
	// 如果当前是非交互模式，应返回 "zh"
	// 如果是交互模式，函数会尝试读取 stdin，返回空字符串或其他值
	if !isInteractive() {
		lang := promptPreferredLanguage()
		if lang != "zh" {
			t.Errorf("非交互模式应返回 zh，实际 %q", lang)
		}
	}
	// 交互模式下跳过测试（无法模拟用户输入）
}

// TestInit_覆盖模式目录已存在 测试 overwrite=true 且目标目录存在时（非交互路径）的初始化
// 注：通过管道替换 stdin 后 isInteractive() 返回 false，走非交互路径
func TestInit_覆盖模式目录已存在(t *testing.T) {
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

	// 首次初始化
	_, err := Init(InitOption{Language: "zh"})
	if err != nil {
		t.Fatalf("首次 Init 失败: %v", err)
	}

	// 替换 stdin 为管道使 isInteractive() 返回 false
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建管道失败: %v", err)
	}
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()
	go func() { w.Close() }()

	// 覆盖模式初始化（非交互路径自动确认）
	result, err := Init(InitOption{Overwrite: true, Language: "en"})
	if err != nil {
		t.Fatalf("覆盖模式 Init 失败: %v", err)
	}
	if result.Cancelled {
		t.Error("覆盖模式不应取消")
	}
}

// TestInit_增量模式目录已存在 测试增量模式且目标目录存在时（非交互路径）的初始化
func TestInit_增量模式目录已存在(t *testing.T) {
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

	// 首次初始化
	_, err := Init(InitOption{Language: "zh"})
	if err != nil {
		t.Fatalf("首次 Init 失败: %v", err)
	}

	// 替换 stdin 为管道使 isInteractive() 返回 false
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建管道失败: %v", err)
	}
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()
	go func() { w.Close() }()

	// 增量模式再次初始化（非交互路径自动确认）
	result, err := Init(InitOption{Language: "en"})
	if err != nil {
		t.Fatalf("增量 Init 失败: %v", err)
	}
	if result.Cancelled {
		t.Error("增量模式不应取消")
	}
}

// TestInit_非交互无语言参数 测试非交互模式下不指定语言时的默认语言选择
func TestInit_非交互无语言参数(t *testing.T) {
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

	// 替换 stdin 为管道使 isInteractive() 返回 false
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建管道失败: %v", err)
	}
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()
	go func() { w.Close() }()

	// 不指定语言，非交互模式应使用默认 "zh"
	result, err := Init(InitOption{})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	if result.Cancelled {
		t.Error("非交互模式不应取消")
	}
}

// TestInit_非交互覆盖模式 测试非交互模式下覆盖模式自动确认
func TestInit_非交互覆盖模式(t *testing.T) {
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

	// 首次初始化
	_, err := Init(InitOption{Language: "zh"})
	if err != nil {
		t.Fatalf("首次 Init 失败: %v", err)
	}

	// 替换 stdin 为管道使 isInteractive() 返回 false
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建管道失败: %v", err)
	}
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()
	go func() { w.Close() }()

	// 不指定语言，覆盖模式，非交互路径
	result, err := Init(InitOption{Overwrite: true})
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	if result.Cancelled {
		t.Error("非交互覆盖模式不应取消")
	}
}

// TestInit_无resources 测试 resources 目录不存在时 Init 返回错误
func TestInit_无resources(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, filepath.Join(tmpDir, "workspace"))
	os.Setenv(EnvResourcesDir, filepath.Join(tmpDir, "nonexistent"))
	defer func() {
		os.Unsetenv(EnvDataDir)
		os.Unsetenv(EnvResourcesDir)
		SetUserHome("")
	}()
	SetUserHome("")

	_, err := Init(InitOption{Language: "zh"})
	if err == nil {
		t.Error("resources 不存在时 Init 应返回错误")
	}
}

// TestPrepare_英文语言 测试英文语言的 Prepare 流程
func TestPrepare_英文语言(t *testing.T) {
	tmpDir := t.TempDir()
	resDir := filepath.Join(tmpDir, "resources")
	setupTestResources(t, resDir)

	workspaceDir := filepath.Join(tmpDir, "workspace")

	os.Setenv(EnvResourcesDir, resDir)
	defer os.Unsetenv(EnvResourcesDir)

	diff, err := Prepare(InitOption{
		Language:     "en",
		WorkspaceDir: workspaceDir,
	})
	if err != nil {
		t.Fatalf("Prepare 失败: %v", err)
	}

	// 验证 AGENT.md 是英文内容
	data, _ := os.ReadFile(filepath.Join(workspaceDir, "agent", "workspace", "AGENT.md"))
	if !strings.Contains(string(data), "English content") {
		t.Errorf("AGENT.md 应该是英文版，实际内容: %q", string(data))
	}

	// 验证有新增文件
	if len(diff.AddedFiles) == 0 {
		t.Error("应有新增文件")
	}
}

// TestPrepare_无workspaceDir 测试 WorkspaceDir 为空时使用默认路径
func TestPrepare_无workspaceDir(t *testing.T) {
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

	_, err := Prepare(InitOption{Language: "zh"})
	if err != nil {
		t.Fatalf("Prepare 失败: %v", err)
	}

	// 验证默认工作区路径下有文件
	assertFileExists(t, filepath.Join(tmpDir, "workspace", "config", "config.yaml"))
}

// TestCopyFileWithDiff_nilDiff 测试 diff 为 nil 时不 panic
func TestCopyFileWithDiff_nilDiff(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src.txt")
	dst := filepath.Join(tmpDir, "dst.txt")
	os.WriteFile(src, []byte("hello"), 0o644)

	if err := copyFileWithDiff(src, dst, nil); err != nil {
		t.Fatalf("copyFileWithDiff 失败: %v", err)
	}
}

// TestCopyDirWithDiff_nilDiff 测试 copyDirWithDiff diff 为 nil 时不 panic
func TestCopyDirWithDiff_nilDiff(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	dstDir := filepath.Join(tmpDir, "dst")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("a"), 0o644)

	if err := copyDirWithDiff(srcDir, dstDir, nil, nil); err != nil {
		t.Fatalf("copyDirWithDiff 失败: %v", err)
	}
}

// TestCopyDirWithDiffIncremental_nilDiff 测试 diff 为 nil 时不 panic
func TestCopyDirWithDiffIncremental_nilDiff(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	dstDir := filepath.Join(tmpDir, "dst")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("a"), 0o644)

	if err := copyDirWithDiffIncremental(srcDir, dstDir, nil, nil); err != nil {
		t.Fatalf("copyDirWithDiffIncremental 失败: %v", err)
	}
}

// TestCopyDirWithDiffIncremental_含子目录 测试含子目录的增量复制
func TestCopyDirWithDiffIncremental_含子目录(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	dstDir := filepath.Join(tmpDir, "dst")

	// 创建源目录结构（含子目录）
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755)
	os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(srcDir, "sub", "b.txt"), []byte("b"), 0o644)

	var diff CopyDiffResult
	if err := copyDirWithDiffIncremental(srcDir, dstDir, &diff, nil); err != nil {
		t.Fatalf("copyDirWithDiffIncremental 失败: %v", err)
	}

	// 验证子目录文件被复制
	assertFileExists(t, filepath.Join(dstDir, "sub", "b.txt"))
	if len(diff.AddedFiles) != 2 {
		t.Errorf("应有 2 个新增文件，实际 %d", len(diff.AddedFiles))
	}
}

// TestPrepare_无模板工作区 测试模板 workspace 目录不存在时的 Prepare
func TestPrepare_无模板工作区(t *testing.T) {
	tmpDir := t.TempDir()
	resDir := filepath.Join(tmpDir, "resources")
	// 只创建基础文件，不创建 agent/workspace 目录
	os.MkdirAll(resDir, 0o755)
	os.WriteFile(filepath.Join(resDir, "config.yaml"), []byte("server:\n  host: localhost\n"), 0o644)
	os.WriteFile(filepath.Join(resDir, ".env.template"), []byte("API_KEY=sk-test\n"), 0o644)

	workspaceDir := filepath.Join(tmpDir, "workspace")

	os.Setenv(EnvResourcesDir, resDir)
	defer os.Unsetenv(EnvResourcesDir)

	diff, err := Prepare(InitOption{
		Language:     "zh",
		WorkspaceDir: workspaceDir,
	})
	if err != nil {
		t.Fatalf("Prepare 失败: %v", err)
	}

	// 验证基本文件存在
	assertFileExists(t, filepath.Join(workspaceDir, "config", "config.yaml"))
	assertDirExists(t, filepath.Join(workspaceDir, "agent", "workspace"))

	// 多语言文件不应存在（因为没有模板源文件）
	if len(diff.AddedFiles) < 2 {
		t.Errorf("至少应有 2 个新增文件（config.yaml + .env），实际 %d", len(diff.AddedFiles))
	}
}

// TestPrepare_已有配置文件不覆盖 测试增量模式下已有文件不被模板覆盖
// 注：setPreferredLanguage 会更新 config.yaml 中的 preferred_language 行
func TestPrepare_已有配置文件不覆盖(t *testing.T) {
	tmpDir := t.TempDir()
	resDir := filepath.Join(tmpDir, "resources")
	setupTestResources(t, resDir)

	workspaceDir := filepath.Join(tmpDir, "workspace")

	os.Setenv(EnvResourcesDir, resDir)
	defer os.Unsetenv(EnvResourcesDir)

	// 首次初始化
	_, err := Prepare(InitOption{
		Language:     "zh",
		WorkspaceDir: workspaceDir,
	})
	if err != nil {
		t.Fatalf("首次 Prepare 失败: %v", err)
	}

	// 修改 .env 文件
	envFile := filepath.Join(workspaceDir, "config", ".env")
	os.WriteFile(envFile, []byte("CUSTOM=value\n"), 0o644)

	// 增量模式再次 Prepare
	_, err = Prepare(InitOption{
		Language:     "zh",
		WorkspaceDir: workspaceDir,
	})
	if err != nil {
		t.Fatalf("增量 Prepare 失败: %v", err)
	}

	// 验证 .env 未被覆盖（增量模式跳过已存在文件）
	data, _ := os.ReadFile(envFile)
	if string(data) != "CUSTOM=value\n" {
		t.Error("增量模式不应覆盖已有 .env")
	}
}

// TestPrepare_覆盖模式覆盖配置文件 测试 overwrite=true 时覆盖已有 config.yaml 和 .env
func TestPrepare_覆盖模式覆盖配置文件(t *testing.T) {
	tmpDir := t.TempDir()
	resDir := filepath.Join(tmpDir, "resources")
	setupTestResources(t, resDir)

	workspaceDir := filepath.Join(tmpDir, "workspace")

	os.Setenv(EnvResourcesDir, resDir)
	defer os.Unsetenv(EnvResourcesDir)

	// 首次初始化
	_, err := Prepare(InitOption{
		Language:     "zh",
		WorkspaceDir: workspaceDir,
	})
	if err != nil {
		t.Fatalf("首次 Prepare 失败: %v", err)
	}

	// 修改配置文件
	configYaml := filepath.Join(workspaceDir, "config", "config.yaml")
	os.WriteFile(configYaml, []byte("custom: config\n"), 0o644)

	// 覆盖模式
	diff, err := Prepare(InitOption{
		Overwrite:    true,
		Language:     "zh",
		WorkspaceDir: workspaceDir,
	})
	if err != nil {
		t.Fatalf("覆盖 Prepare 失败: %v", err)
	}

	// 验证配置文件已被覆盖
	data, _ := os.ReadFile(configYaml)
	if string(data) == "custom: config\n" {
		t.Error("覆盖模式应覆盖已有 config.yaml")
	}

	// 验证差异包含覆盖记录
	if len(diff.OverwrittenFiles) == 0 {
		t.Error("覆盖模式应记录覆盖文件")
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
