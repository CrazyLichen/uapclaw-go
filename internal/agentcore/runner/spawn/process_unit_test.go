package spawn

import (
	"bufio"
	"strings"
	"testing"
)

// TestEnvSpawnProcess 常量值验证
func TestEnvSpawnProcess(t *testing.T) {
	if EnvSpawnProcess != "UAPCLAW_SPAWN_PROCESS" {
		t.Errorf("EnvSpawnProcess = %s, want UAPCLAW_SPAWN_PROCESS", EnvSpawnProcess)
	}
}

// TestEnvSpawnLoggingConfig 常量值验证
func TestEnvSpawnLoggingConfig(t *testing.T) {
	if EnvSpawnLoggingConfig != "UAPCLAW_SPAWN_LOGGING_CONFIG" {
		t.Errorf("EnvSpawnLoggingConfig = %s, want UAPCLAW_SPAWN_LOGGING_CONFIG", EnvSpawnLoggingConfig)
	}
}

// TestSpawnChildSubCommand 常量值验证
func TestSpawnChildSubCommand(t *testing.T) {
	if SpawnChildSubCommand != "spawn-child" {
		t.Errorf("SpawnChildSubCommand = %s, want spawn-child", SpawnChildSubCommand)
	}
}

// TestGetSelfExecutable 测试获取当前可执行文件路径
func TestGetSelfExecutable(t *testing.T) {
	path, err := getSelfExecutable()
	if err != nil {
		t.Fatalf("getSelfExecutable 失败: %v", err)
	}
	if path == "" {
		t.Error("可执行文件路径不应为空")
	}
}

// TestDrainStderr 测试后台读取 stderr
func TestDrainStderr(t *testing.T) {
	input := "line1\nline2\nline3\n"
	drainStderr(strings.NewReader(input))
	// drainStderr 不返回值，只验证不会 panic
}

// TestDrainStderr_空输入 测试空 stderr 输入
func TestDrainStderr_空输入(t *testing.T) {
	drainStderr(strings.NewReader(""))
	// 不应 panic
}

// TestDrainStderr_长行 测试长行 stderr
func TestDrainStderr_长行(t *testing.T) {
	longLine := strings.Repeat("x", bufio.MaxScanTokenSize/2)
	drainStderr(strings.NewReader(longLine + "\n"))
	// 不应 panic
}

// TestSpawnProcess_导入验证 验证 SpawnProcess 函数签名存在
func TestSpawnProcess_导入验证(t *testing.T) {
	// SpawnProcess 需要真实子进程，此处仅验证常量和类型
	_ = EnvSpawnProcess
	_ = EnvSpawnLoggingConfig
	_ = SpawnChildSubCommand
}
