package logger

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// resetGlobal 重置全局 Logger 状态，用于测试隔离。
func resetGlobal() {
	globalMu.Lock()
	defer globalMu.Unlock()
	global = nil
	globalOnce = sync.Once{}
	globalSetup = false
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSetup_默认配置(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	err := Setup(WithOutputDir(tmpDir))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer func() { _ = Close() }()

	// 验证全局 Logger 已初始化
	globalMu.RLock()
	g := global
	globalMu.RUnlock()
	if g == nil {
		t.Fatal("期望全局 Logger 已初始化")
	}
	if g.outputDir != tmpDir {
		t.Errorf("期望 outputDir = %s，实际 %s", tmpDir, g.outputDir)
	}
}

func TestSetup_WithLogLevel(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	err := Setup(WithOutputDir(tmpDir), WithLogLevel("debug"))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer func() { _ = Close() }()

	levels := Levels()
	if levels.Console != LogLevelDebug {
		t.Errorf("期望 Console = debug，实际 %s", levels.Console)
	}
	if levels.Gateway != LogLevelDebug {
		t.Errorf("期望 Gateway = debug，实际 %s", levels.Gateway)
	}
}

func TestGetLogger(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	err := Setup(WithOutputDir(tmpDir))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer func() { _ = Close() }()

	// 获取各组件 Logger
	for _, comp := range allComponents() {
		lg := getLogger(comp)
		if lg.GetLevel() == zerolog.Disabled {
			t.Errorf("期望 %s Logger 已启用，实际 Disabled", comp)
		}
	}
}

func TestGetLogger_未初始化(t *testing.T) {
	resetGlobal()

	// 未初始化时返回 Nop Logger
	lg := getLogger(ComponentGateway)
	// 向 Nop Logger 写入应该不产生任何输出
	lg.Info().Msg("should not appear")
}

func TestGetLogger_写入日志(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	err := Setup(WithOutputDir(tmpDir), WithLogLevel("debug"))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer func() { _ = Close() }()

	lg := getLogger(ComponentGateway)
	lg.Info().Msg("test log message")

	// 验证 full.log 已创建并包含内容
	fullLogPath := filepath.Join(tmpDir, "full.log")
	data, err := os.ReadFile(fullLogPath)
	if err != nil {
		t.Fatalf("读取 full.log 失败: %v", err)
	}
	if !strings.Contains(string(data), "test log message") {
		t.Errorf("期望 full.log 包含 'test log message'，实际 %q", string(data))
	}

	// 验证 gateway.log 已创建并包含内容
	gatewayLogPath := filepath.Join(tmpDir, "gateway.log")
	data, err = os.ReadFile(gatewayLogPath)
	if err != nil {
		t.Fatalf("读取 gateway.log 失败: %v", err)
	}
	if !strings.Contains(string(data), "test log message") {
		t.Errorf("期望 gateway.log 包含 'test log message'，实际 %q", string(data))
	}
}

func TestGetLogger_敏感数据脱敏(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	err := Setup(WithOutputDir(tmpDir), WithLogLevel("debug"))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer func() { _ = Close() }()

	lg := getLogger(ComponentChannel)
	lg.Info().Str("password", "secret123").Msg("login request")

	// 验证 full.log 中的敏感数据已被脱敏
	fullLogPath := filepath.Join(tmpDir, "full.log")
	data, err := os.ReadFile(fullLogPath)
	if err != nil {
		t.Fatalf("读取 full.log 失败: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "secret123") {
		t.Errorf("期望密码已被脱敏，实际 %q", content)
	}
	if strings.Contains(content, SensitiveMask) {
		t.Log("敏感数据已被正确脱敏")
	}
}

func TestClose(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	err := Setup(WithOutputDir(tmpDir))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}

	err = Close()
	if err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	// 验证全局 Logger 已清空
	globalMu.RLock()
	g := global
	globalMu.RUnlock()
	if g != nil {
		t.Error("期望全局 Logger 已清空")
	}
}

func TestClose_未初始化(t *testing.T) {
	resetGlobal()

	// 未初始化时 Close 应该安全返回
	err := Close()
	if err != nil {
		t.Errorf("期望 Close 安全返回，实际 %v", err)
	}
}

func TestOutputDir(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	err := Setup(WithOutputDir(tmpDir))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer func() { _ = Close() }()

	if got := OutputDir(); got != tmpDir {
		t.Errorf("期望 OutputDir = %s，实际 %s", tmpDir, got)
	}
}

func TestOutputDir_未初始化(t *testing.T) {
	resetGlobal()

	if got := OutputDir(); got != "" {
		t.Errorf("期望空字符串，实际 %s", got)
	}
}

func TestSetup_多次调用安全(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	// 第一次调用
	err := Setup(WithOutputDir(tmpDir))
	if err != nil {
		t.Fatalf("第一次 Setup 失败: %v", err)
	}

	// 第二次调用应该是安全的（不重复初始化）
	err = Setup(WithOutputDir(tmpDir + "2"))
	if err != nil {
		t.Fatalf("第二次 Setup 失败: %v", err)
	}
	defer func() { _ = Close() }()

	// 验证仍然使用第一次的 outputDir
	if got := OutputDir(); got != tmpDir {
		t.Errorf("期望使用第一次的 outputDir = %s，实际 %s", tmpDir, got)
	}
}

func TestSetup_日志级别过滤(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	err := Setup(WithOutputDir(tmpDir), WithLogLevel("warn"))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer func() { _ = Close() }()

	lg := getLogger(ComponentGateway)

	// debug 和 info 级别应该被过滤
	lg.Debug().Msg("debug_msg_should_not_appear")
	lg.Info().Msg("info_msg_should_not_appear")
	lg.Warn().Msg("warn_msg_should_appear")

	// 验证 full.log 只包含 warn 及以上级别
	fullLogPath := filepath.Join(tmpDir, "full.log")
	data, err := os.ReadFile(fullLogPath)
	if err != nil {
		t.Fatalf("读取 full.log 失败: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "debug_msg_should_not_appear") {
		t.Error("期望 debug 消息被过滤")
	}
	if strings.Contains(content, "info_msg_should_not_appear") {
		t.Error("期望 info 消息被过滤")
	}
	if !strings.Contains(content, "warn_msg_should_appear") {
		t.Error("期望 warn 消息被保留")
	}
}

func Test组件级日志函数(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	err := Setup(WithOutputDir(tmpDir), WithLogLevel("debug"))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer func() { _ = Close() }()

	// 测试各组件级日志函数
	Info(ComponentCommon).Str("key", "val").Msg("info_test")
	Warn(ComponentGateway).Msg("warn_test")
	Error(ComponentChannel).Msg("error_test")
	Debug(ComponentCommon).Msg("debug_test")

	// 验证 full.log 包含所有日志
	fullLogPath := filepath.Join(tmpDir, "full.log")
	data, err := os.ReadFile(fullLogPath)
	if err != nil {
		t.Fatalf("读取 full.log 失败: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "info_test") {
		t.Error("期望 full.log 包含 'info_test'")
	}
	if !strings.Contains(content, "warn_test") {
		t.Error("期望 full.log 包含 'warn_test'")
	}
	if !strings.Contains(content, "error_test") {
		t.Error("期望 full.log 包含 'error_test'")
	}
	if !strings.Contains(content, "debug_test") {
		t.Error("期望 full.log 包含 'debug_test'")
	}
}

func Test组件级日志函数_未初始化(t *testing.T) {
	resetGlobal()

	// 未初始化时组件级函数应安全返回 Nop（不 panic、不输出）
	Info(ComponentCommon).Msg("should not appear")
	Warn(ComponentGateway).Msg("should not appear")
	Error(ComponentChannel).Msg("should not appear")
	Debug(ComponentCommon).Msg("should not appear")
}

func TestIsSetup(t *testing.T) {
	resetGlobal()

	if IsSetup() {
		t.Error("期望 IsSetup() 返回 false")
	}

	tmpDir := t.TempDir()
	err := Setup(WithOutputDir(tmpDir))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer func() { _ = Close() }()

	if !IsSetup() {
		t.Error("期望 IsSetup() 返回 true")
	}
}

func TestReconfigure_更新日志级别(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	// 先 Setup 初始化
	err := Setup(WithOutputDir(tmpDir), WithLogLevel("warn"))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer func() { _ = Close() }()

	// 验证初始级别
	levels := Levels()
	if levels.Gateway != LogLevelWarn {
		t.Errorf("期望初始 Gateway = warn，实际 %s", levels.Gateway)
	}

	// Reconfigure 更新为 debug
	err = Reconfigure(WithLogLevel("debug"))
	if err != nil {
		t.Fatalf("Reconfigure 失败: %v", err)
	}

	// 验证级别已更新
	levels = Levels()
	if levels.Gateway != LogLevelDebug {
		t.Errorf("期望 Reconfigure 后 Gateway = debug，实际 %s", levels.Gateway)
	}
}

func TestReconfigure_未初始化返回错误(t *testing.T) {
	resetGlobal()

	err := Reconfigure(WithLogLevel("debug"))
	if err == nil {
		t.Error("期望 Reconfigure 在未初始化时返回错误")
	}
}

func TestReconfigure_WithLoggingLevels(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	err := Setup(WithOutputDir(tmpDir), WithLogLevel("info"))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer func() { _ = Close() }()

	// 构造自定义级别
	customLevels := ResolveLoggingLevels(nil, "", "debug")
	err = Reconfigure(WithLoggingLevels(customLevels))
	if err != nil {
		t.Fatalf("Reconfigure 失败: %v", err)
	}

	levels := Levels()
	if levels.Gateway != LogLevelDebug {
		t.Errorf("期望 Gateway = debug，实际 %s", levels.Gateway)
	}
}

// TestWithRotationConfig 验证 WithRotationConfig 选项函数
func TestWithRotationConfig(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	rotCfg := NewRotationConfig()
	rotCfg.MaxSize = 50 * 1024 * 1024
	rotCfg.MaxBackups = 10

	err := Setup(WithOutputDir(tmpDir), WithRotationConfig(rotCfg))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer func() { _ = Close() }()

	// 验证日志系统已初始化
	if !IsSetup() {
		t.Error("期望 IsSetup() 返回 true")
	}
}

// TestGetLogConfigSnapshot_未初始化 验证未初始化时返回空快照
func TestGetLogConfigSnapshot_未初始化(t *testing.T) {
	resetGlobal()

	snapshot := GetLogConfigSnapshot()
	if len(snapshot) != 0 {
		t.Errorf("期望空快照，实际 %v", snapshot)
	}
}

// TestGetLogConfigSnapshot_已初始化 验证已初始化时返回完整快照
func TestGetLogConfigSnapshot_已初始化(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	err := Setup(WithOutputDir(tmpDir), WithLogLevel("debug"))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer func() { _ = Close() }()

	snapshot := GetLogConfigSnapshot()
	if len(snapshot) == 0 {
		t.Error("期望非空快照")
	}
	// 验证关键字段存在
	for _, key := range []string{"logger", "console", "common", "gateway", "channel", "agent_server", "permissions", "agent_core", "full", "output_dir"} {
		if _, ok := snapshot[key]; !ok {
			t.Errorf("快照缺少字段 %s", key)
		}
	}
}

// TestLevels_未初始化 验证未初始化时 Levels 返回默认值
func TestLevels_未初始化(t *testing.T) {
	resetGlobal()

	levels := Levels()
	// 应返回默认级别，不 panic
	_ = levels
}

// TestWithConfig_配置读取成功 验证 WithConfig 选项函数在配置有效时设置日志级别
func TestWithConfig_配置读取成功(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	cfg, err := config.New("")
	if err != nil {
		t.Fatalf("config.New 失败: %v", err)
	}

	err = Setup(WithOutputDir(tmpDir), WithConfig(cfg))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer func() { _ = Close() }()

	if !IsSetup() {
		t.Error("期望 IsSetup() 返回 true")
	}
}

// TestWithConfig_空配置 验证 WithConfig 使用空配置时不 panic
func TestWithConfig_空配置(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	cfg, err := config.New("")
	if err != nil {
		t.Fatalf("config.New 失败: %v", err)
	}

	// 空配置不应 panic，GetLoggingConfig 返回默认值
	err = Setup(WithOutputDir(tmpDir), WithConfig(cfg))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer func() { _ = Close() }()

	if !IsSetup() {
		t.Error("期望 IsSetup() 返回 true")
	}
}

// TestWithLogLevel_选项应用 验证 WithLogLevel 选项在 Logger 实例上正确设置级别
func TestWithLogLevel_选项应用(t *testing.T) {
	l := &Logger{}
	opt := WithLogLevel("debug")
	opt(l)
	// 验证 levels 被设置
	levels := l.levels
	if levels.Console != LogLevelDebug {
		t.Errorf("期望 Console = debug，实际 %s", levels.Console)
	}
}

// TestWithOutputDir_选项应用 验证 WithOutputDir 选项正确设置输出目录
func TestWithOutputDir_选项应用(t *testing.T) {
	l := &Logger{}
	opt := WithOutputDir("/tmp/test-logs")
	opt(l)
	if l.outputDir != "/tmp/test-logs" {
		t.Errorf("期望 outputDir = /tmp/test-logs，实际 %s", l.outputDir)
	}
}

// TestWithLoggingLevels_选项应用 验证 WithLoggingLevels 选项正确设置日志级别
func TestWithLoggingLevels_选项应用(t *testing.T) {
	l := &Logger{}
	customLevels := ResolveLoggingLevels(nil, "", "warn")
	opt := WithLoggingLevels(customLevels)
	opt(l)
	if l.levels.Console != LogLevelWarn {
		t.Errorf("期望 Console = warn，实际 %s", l.levels.Console)
	}
}

func TestWithConfigFile(t *testing.T) {
	// 创建临时 workspace 目录和 config/config.yaml
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "config.yaml")

	yamlContent := `logging:
  level: "debug"
  format: "json"
  console_level: "info"
  gateway: "debug"
  channel: "info"
  agent_server: "warn"
  common: "info"
  agent_core: "info"
  full: "debug"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// 设置 UAPCLAW_DATA_DIR 指向临时目录（对齐 path.ConfigFile 逻辑）
	t.Setenv("UAPCLAW_DATA_DIR", dir)

	// 重置 path 缓存和 logger
	pathutil.ResetCache()
	resetGlobal()

	// 使用 WithConfigFile 初始化
	err := Setup(WithConfigFile())
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer Close()

	// 验证日志级别被正确设置
	levels := Levels()
	if levels.Gateway != LogLevelDebug {
		t.Errorf("期望 Gateway = debug，实际 %s", levels.Gateway)
	}
	if levels.Channel != LogLevelInfo {
		t.Errorf("期望 Channel = info，实际 %s", levels.Channel)
	}
	if levels.AgentServer != LogLevelWarn {
		t.Errorf("期望 AgentServer = warn，实际 %s", levels.AgentServer)
	}
}

func TestWithConfigFile_文件不存在(t *testing.T) {
	// 指向不存在的目录（用 t.TempDir() 下的子路径，避免 CI 非 root 用户
	// 因 mkdir /nonexistent 权限不足而误判为 Setup 失败）
	t.Setenv("UAPCLAW_DATA_DIR", filepath.Join(t.TempDir(), "missing-data-dir"))

	pathutil.ResetCache()
	resetGlobal()

	// 文件不存在时应使用默认级别，不报错
	err := Setup(WithConfigFile())
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer Close()

	levels := Levels()
	// 默认应为 INFO
	if levels.Gateway != LogLevelInfo {
		t.Errorf("期望 Gateway = info（默认），实际 %s", levels.Gateway)
	}
}

// TestSetup_控制台写入stderr 控制台日志走 stderr（对齐 Python setup_logger 的
// StreamHandler 默认流），保证 stdout 只承载业务输出。
func TestSetup_控制台写入stderr(t *testing.T) {
	resetGlobal()

	dir := t.TempDir()
	errFile, err := os.CreateTemp(dir, "stderr-*.log")
	if err != nil {
		t.Fatalf("创建临时 stderr 文件失败: %v", err)
	}
	outFile, err := os.CreateTemp(dir, "stdout-*.log")
	if err != nil {
		t.Fatalf("创建临时 stdout 文件失败: %v", err)
	}

	origStderr, origStdout := os.Stderr, os.Stdout
	os.Stderr, os.Stdout = errFile, outFile
	t.Cleanup(func() {
		os.Stderr, os.Stdout = origStderr, origStdout
		_ = Close()
	})

	if err := Setup(WithOutputDir(t.TempDir())); err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	Info(ComponentCommon).Msg("console-marker")
	if err := Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	os.Stderr, os.Stdout = origStderr, origStdout

	errData, readErr := os.ReadFile(errFile.Name())
	if readErr != nil {
		t.Fatalf("读取 stderr 文件失败: %v", readErr)
	}
	if !strings.Contains(string(errData), "console-marker") {
		t.Errorf("控制台日志未写入 stderr, 内容: %q", string(errData))
	}

	outData, readErr := os.ReadFile(outFile.Name())
	if readErr != nil {
		t.Fatalf("读取 stdout 文件失败: %v", readErr)
	}
	if strings.Contains(string(outData), "console-marker") {
		t.Errorf("控制台日志不应写入 stdout, 内容: %q", string(outData))
	}
}

// TestSetup_默认输出目录受数据目录控制 默认 outputDir 走 pathutil.LogsDir()，
// 即 WorkspaceDir()/agent/.logs，随 UAPCLAW_DATA_DIR 变化（不再硬编码 ~/.uapclaw）。
func TestSetup_默认输出目录受数据目录控制(t *testing.T) {
	resetGlobal()

	dataDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", dataDir)
	pathutil.ResetCache()
	t.Cleanup(func() {
		pathutil.ResetCache()
		_ = Close()
	})

	if err := Setup(); err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}

	want := filepath.Join(dataDir, "agent", ".logs")
	if got := OutputDir(); got != want {
		t.Errorf("默认 outputDir = %q, want %q", got, want)
	}
}
