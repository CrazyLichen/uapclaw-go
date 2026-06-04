package logger

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// resetGlobal 重置全局 Logger 状态，用于测试隔离。
func resetGlobal() {
	globalMu.Lock()
	defer globalMu.Unlock()
	global = nil
	globalOnce = sync.Once{}
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSetup_默认配置(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	err := Setup(WithOutputDir(tmpDir))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer Close()

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
	defer Close()

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
	defer Close()

	// 获取各组件 Logger
	for _, comp := range allComponents() {
		lg := GetLogger(comp)
		if lg.GetLevel() == zerolog.Disabled {
			t.Errorf("期望 %s Logger 已启用，实际 Disabled", comp)
		}
	}
}

func TestGetLogger_未初始化(t *testing.T) {
	resetGlobal()

	// 未初始化时返回 Nop Logger
	lg := GetLogger(ComponentGateway)
	// 向 Nop Logger 写入应该不产生任何输出
	lg.Info().Msg("不应出现")
}

func TestGetLogger_写入日志(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	err := Setup(WithOutputDir(tmpDir), WithLogLevel("debug"))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer Close()

	lg := GetLogger(ComponentGateway)
	lg.Info().Msg("测试日志消息")

	// 验证 full.log 已创建并包含内容
	fullLogPath := filepath.Join(tmpDir, "full.log")
	data, err := os.ReadFile(fullLogPath)
	if err != nil {
		t.Fatalf("读取 full.log 失败: %v", err)
	}
	if !strings.Contains(string(data), "测试日志消息") {
		t.Errorf("期望 full.log 包含 '测试日志消息'，实际 %q", string(data))
	}

	// 验证 gateway.log 已创建并包含内容
	gatewayLogPath := filepath.Join(tmpDir, "gateway.log")
	data, err = os.ReadFile(gatewayLogPath)
	if err != nil {
		t.Fatalf("读取 gateway.log 失败: %v", err)
	}
	if !strings.Contains(string(data), "测试日志消息") {
		t.Errorf("期望 gateway.log 包含 '测试日志消息'，实际 %q", string(data))
	}
}

func TestGetLogger_敏感数据脱敏(t *testing.T) {
	resetGlobal()
	tmpDir := t.TempDir()

	err := Setup(WithOutputDir(tmpDir), WithLogLevel("debug"))
	if err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer Close()

	lg := GetLogger(ComponentChannel)
	lg.Info().Str("password", "secret123").Msg("登录请求")

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
	defer Close()

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
	defer Close()

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
	defer Close()

	lg := GetLogger(ComponentGateway)

	// debug 和 info 级别应该被过滤
	lg.Debug().Msg("debug消息_不应出现")
	lg.Info().Msg("info消息_不应出现")
	lg.Warn().Msg("warn消息_应出现")

	// 验证 full.log 只包含 warn 及以上级别
	fullLogPath := filepath.Join(tmpDir, "full.log")
	data, err := os.ReadFile(fullLogPath)
	if err != nil {
		t.Fatalf("读取 full.log 失败: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "debug消息_不应出现") {
		t.Error("期望 debug 消息被过滤")
	}
	if strings.Contains(content, "info消息_不应出现") {
		t.Error("期望 info 消息被过滤")
	}
	if !strings.Contains(content, "warn消息_应出现") {
		t.Error("期望 warn 消息被保留")
	}
}
