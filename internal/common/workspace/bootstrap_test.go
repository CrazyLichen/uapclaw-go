package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCreateBootstrapEnv 测试生成 bootstrap .env 文件
func TestCreateBootstrapEnv(t *testing.T) {
	tmpDir := t.TempDir()

	config := &InstanceConfig{
		Name:      "alice",
		Workspace: filepath.Join(tmpDir, "alice"),
		Ports:     CalculateInstancePorts(1),
	}

	envPath, err := CreateBootstrapEnv(config)
	if err != nil {
		t.Fatalf("CreateBootstrapEnv 失败: %v", err)
	}

	// 验证路径
	expectedPath := filepath.Join(tmpDir, "alice", ".env")
	if envPath != expectedPath {
		t.Errorf("路径期望 %q，实际 %q", expectedPath, envPath)
	}

	// 验证文件内容
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("读取 .env 失败: %v", err)
	}

	content := string(data)

	// 验证包含关键行
	if !strings.Contains(content, "UAPCLAW_DATA_DIR=") {
		t.Error("应包含 UAPCLAW_DATA_DIR")
	}
	if !strings.Contains(content, "UAPCLAW_INSTANCE=alice") {
		t.Error("应包含 UAPCLAW_INSTANCE=alice")
	}
	if !strings.Contains(content, "AGENT_SERVER_PORT=") {
		t.Error("应包含 AGENT_SERVER_PORT")
	}
	if !strings.Contains(content, "WEB_PORT=") {
		t.Error("应包含 WEB_PORT")
	}
	if !strings.Contains(content, "GATEWAY_PORT=") {
		t.Error("应包含 GATEWAY_PORT")
	}
	if !strings.Contains(content, "FRONTEND_PORT=") {
		t.Error("应包含 FRONTEND_PORT")
	}
}

// TestCreateBootstrapEnv_端口值 测试端口值正确写入
func TestCreateBootstrapEnv_端口值(t *testing.T) {
	tmpDir := t.TempDir()

	ports := CalculateInstancePorts(1) // alice index=1
	config := &InstanceConfig{
		Name:      "alice",
		Workspace: filepath.Join(tmpDir, "alice"),
		Ports:     ports,
	}

	envPath, _ := CreateBootstrapEnv(config)
	data, _ := os.ReadFile(envPath)
	content := string(data)

	// 验证具体端口值
	if !strings.Contains(content, "AGENT_SERVER_PORT=19092") {
		t.Errorf("应包含 AGENT_SERVER_PORT=19092，内容: %s", content)
	}
}

// TestCreateBootstrapEnv_nil配置 测试 nil 配置
func TestCreateBootstrapEnv_nil配置(t *testing.T) {
	_, err := CreateBootstrapEnv(nil)
	if err == nil {
		t.Error("nil 配置应返回错误")
	}
}

// TestCreateBootstrapEnvForName 测试按名称生成
func TestCreateBootstrapEnvForName(t *testing.T) {
	tmpDir := t.TempDir()

	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	// 先注册实例
	workspacePath := InstanceWorkspacePath("alice")
	UpdateInstancesYAML("alice", workspacePath, CalculateInstancePorts(1))

	envPath, err := CreateBootstrapEnvForName("alice", workspacePath)
	if err != nil {
		t.Fatalf("CreateBootstrapEnvForName 失败: %v", err)
	}

	// 验证文件存在
	if _, err := os.Stat(envPath); err != nil {
		t.Errorf("文件应存在: %v", err)
	}

	data, _ := os.ReadFile(envPath)
	if !strings.Contains(string(data), "UAPCLAW_INSTANCE=alice") {
		t.Error("应包含 UAPCLAW_INSTANCE=alice")
	}
}
