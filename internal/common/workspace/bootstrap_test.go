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
	if !strings.Contains(content, "UAPCLAW_AGENT_SERVER_PORT=") {
		t.Error("应包含 UAPCLAW_AGENT_SERVER_PORT")
	}
	if !strings.Contains(content, "UAPCLAW_WEB_PORT=") {
		t.Error("应包含 UAPCLAW_WEB_PORT")
	}
	if !strings.Contains(content, "UAPCLAW_GATEWAY_PORT=") {
		t.Error("应包含 UAPCLAW_GATEWAY_PORT")
	}
	if !strings.Contains(content, "UAPCLAW_FRONTEND_PORT=") {
		t.Error("应包含 UAPCLAW_FRONTEND_PORT")
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
		t.Errorf("应包含 UAPCLAW_AGENT_SERVER_PORT=19092，内容: %s", content)
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

	_ = os.Setenv(EnvDataDir, tmpDir)
	defer func() { _ = os.Unsetenv(EnvDataDir) }()
	SetUserHome("")

	// 先注册实例
	workspacePath := InstanceWorkspacePath("alice")
	_ = UpdateInstancesYAML("alice", workspacePath, CalculateInstancePorts(1))

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

// TestCreateBootstrapEnv_部分端口 测试部分端口缺失时不 panic
func TestCreateBootstrapEnv_部分端口(t *testing.T) {
	tmpDir := t.TempDir()

	config := &InstanceConfig{
		Name:      "alice",
		Workspace: filepath.Join(tmpDir, "alice"),
		Ports: map[string]int{
			"agent_server": 28092,
			// 缺少 web, gateway, frontend 端口
		},
	}

	envPath, err := CreateBootstrapEnv(config)
	if err != nil {
		t.Fatalf("CreateBootstrapEnv 失败: %v", err)
	}

	data, _ := os.ReadFile(envPath)
	content := string(data)

	// 存在的端口应写入
	if !strings.Contains(content, "AGENT_SERVER_PORT=28092") {
		t.Error("应包含 UAPCLAW_AGENT_SERVER_PORT=28092")
	}
	// 不存在的端口不应出现
	if strings.Contains(content, "UAPCLAW_WEB_PORT=") {
		t.Error("缺失的端口不应写入")
	}
}

// TestCreateBootstrapEnvForName_不存在 测试按不存在的名称生成
func TestCreateBootstrapEnvForName_不存在(t *testing.T) {
	tmpDir := t.TempDir()

	_ = os.Setenv(EnvDataDir, tmpDir)
	defer func() { _ = os.Unsetenv(EnvDataDir) }()
	SetUserHome("")

	// 不注册实例，直接创建
	// GetInstanceIndex 会返回新序号
	envPath, err := CreateBootstrapEnvForName("nonexistent", "/tmp/nonexistent")
	if err != nil {
		t.Fatalf("CreateBootstrapEnvForName 失败: %v", err)
	}

	// 验证文件存在
	if _, err := os.Stat(envPath); err != nil {
		t.Errorf("文件应存在: %v", err)
	}
}

// TestPortEnvName 覆盖端口类型到环境变量名的映射（含未知类型）
func TestPortEnvName(t *testing.T) {
	cases := []struct {
		portType string
		want     string
		ok       bool
	}{
		{"agent_server", "UAPCLAW_AGENT_SERVER_PORT", true},
		{"web", "UAPCLAW_WEB_PORT", true},
		{"gateway", "UAPCLAW_GATEWAY_PORT", true},
		{"frontend", "UAPCLAW_FRONTEND_PORT", true},
		{"unknown", "", false},
	}
	for _, tc := range cases {
		got, ok := portEnvName(tc.portType)
		if got != tc.want || ok != tc.ok {
			t.Errorf("portEnvName(%q) = (%q,%v)，期望 (%q,%v)", tc.portType, got, ok, tc.want, tc.ok)
		}
	}
}
