package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// TestValidateInstanceName 测试实例名称验证
func TestValidateInstanceName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"合法_简单", "alice", false},
		{"合法_含连字符", "my-agent", false},
		{"合法_含下划线", "my_agent", false},
		{"合法_含数字", "agent123", false},
		{"非法_空字符串", "", true},
		{"非法_过长", "a12345678901234567890123456789012345678901234567890123456789012345", true},
		{"非法_含空格", "my agent", true},
		{"非法_以点开头", ".hidden", true},
		{"非法_保留名_default", "default", true},
		{"非法_保留名_config", "config", true},
		{"非法_保留名_tmp", "tmp", true},
		{"非法_保留名_uapclaw", "uapclaw", true},
		{"非法_保留名_all", "all", true},
		{"非法_特殊字符", "agent@123", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateInstanceName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateInstanceName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

// TestIsValidInstanceName 测试实例名称快捷验证
func TestIsValidInstanceName(t *testing.T) {
	if !IsValidInstanceName("alice") {
		t.Error("alice 应该是合法名称")
	}
	if IsValidInstanceName("default") {
		t.Error("default 应该是非法名称")
	}
}

// TestCalculateInstancePorts 测试端口分配
func TestCalculateInstancePorts(t *testing.T) {
	// 默认实例 (index=0)
	ports0 := CalculateInstancePorts(0)
	if ports0["agent_server"] != BasePortAgentServer {
		t.Errorf("index=0 agent_server 期望 %d，实际 %d", BasePortAgentServer, ports0["agent_server"])
	}

	// 第一个命名实例 (index=1)
	ports1 := CalculateInstancePorts(1)
	if ports1["agent_server"] != BasePortAgentServer+PortStep {
		t.Errorf("index=1 agent_server 期望 %d，实际 %d", BasePortAgentServer+PortStep, ports1["agent_server"])
	}
	if ports1["web"] != BasePortWeb+PortStep {
		t.Errorf("index=1 web 期望 %d，实际 %d", BasePortWeb+PortStep, ports1["web"])
	}
}

// TestComputeAutoPort 测试单个端口计算
func TestComputeAutoPort(t *testing.T) {
	port := ComputeAutoPort("agent_server", 2)
	expected := BasePortAgentServer + 2*PortStep
	if port != expected {
		t.Errorf("ComputeAutoPort(agent_server, 2) 期望 %d，实际 %d", expected, port)
	}

	// 未知类型降级
	port = ComputeAutoPort("unknown", 1)
	if port != 10000+PortStep {
		t.Errorf("ComputeAutoPort(unknown, 1) 降级期望 %d，实际 %d", 10000+PortStep, port)
	}
}

// TestInstancesYAMLPath 测试 instances.yaml 路径
func TestInstancesYAMLPath(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	path := InstancesYAMLPath()
	expected := filepath.Join(tmpDir, "instances.yaml")
	if path != expected {
		t.Errorf("InstancesYAMLPath 期望 %q，实际 %q", expected, path)
	}
}

// TestInstancesDir 测试命名实例根目录
func TestInstancesDir(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvHome, tmpDir)
	defer os.Unsetenv(EnvHome)
	SetUserHome("")

	dir := InstancesDir()
	expected := filepath.Join(tmpDir, DefaultInstancesDir)
	if dir != expected {
		t.Errorf("InstancesDir 期望 %q，实际 %q", expected, dir)
	}
}

// TestInstanceWorkspacePath 测试命名实例工作区路径
func TestInstanceWorkspacePath(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvHome, tmpDir)
	defer os.Unsetenv(EnvHome)
	SetUserHome("")

	path := InstanceWorkspacePath("alice")
	expected := filepath.Join(tmpDir, DefaultInstancesDir, "alice")
	if path != expected {
		t.Errorf("InstanceWorkspacePath 期望 %q，实际 %q", expected, path)
	}
}

// TestLoadSaveInstancesYAML 测试 instances.yaml 读写
func TestLoadSaveInstancesYAML(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	// 文件不存在时应返回空结构
	data, err := LoadInstancesYAML()
	if err != nil {
		t.Fatalf("LoadInstancesYAML 失败: %v", err)
	}
	if _, ok := data["instances"]; !ok {
		t.Error("LoadInstancesYAML 应返回含 instances 键的 map")
	}

	// 写入数据
	data["instances"] = map[string]any{
		"alice": map[string]any{
			"workspace": "/tmp/alice",
			"ports": map[string]any{
				"agent_server": 28092,
				"web":          29000,
			},
		},
	}

	if err := SaveInstancesYAML(data); err != nil {
		t.Fatalf("SaveInstancesYAML 失败: %v", err)
	}

	// 重新加载验证
	data2, err := LoadInstancesYAML()
	if err != nil {
		t.Fatalf("重新加载失败: %v", err)
	}

	instances, ok := data2["instances"].(map[string]any)
	if !ok {
		t.Fatal("instances 类型错误")
	}
	if _, ok := instances["alice"]; !ok {
		t.Error("应包含 alice 实例")
	}
}

// TestUpdateInstancesYAML 测试更新实例条目
func TestUpdateInstancesYAML(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	ports := CalculateInstancePorts(1)
	if err := UpdateInstancesYAML("alice", InstanceWorkspacePath("alice"), ports); err != nil {
		t.Fatalf("UpdateInstancesYAML 失败: %v", err)
	}

	// 验证写入
	data, err := LoadInstancesYAML()
	if err != nil {
		t.Fatalf("LoadInstancesYAML 失败: %v", err)
	}
	instances, ok := data["instances"].(map[string]any)
	if !ok {
		t.Fatal("instances 类型错误")
	}
	if _, ok := instances["alice"]; !ok {
		t.Error("应包含 alice 实例")
	}
}

// TestGetInstanceIndex 测试获取实例序号
func TestGetInstanceIndex(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	// 先添加两个实例
	UpdateInstancesYAML("alice", InstanceWorkspacePath("alice"), CalculateInstancePorts(1))
	UpdateInstancesYAML("bob", InstanceWorkspacePath("bob"), CalculateInstancePorts(2))

	idx, err := GetInstanceIndex("alice")
	if err != nil {
		t.Fatalf("GetInstanceIndex 失败: %v", err)
	}
	if idx != 1 {
		t.Errorf("alice 序号期望 1，实际 %d", idx)
	}

	idx, err = GetInstanceIndex("bob")
	if err != nil {
		t.Fatalf("GetInstanceIndex 失败: %v", err)
	}
	if idx != 2 {
		t.Errorf("bob 序号期望 2，实际 %d", idx)
	}

	// 不存在的实例
	idx, err = GetInstanceIndex("charlie")
	if err != nil {
		t.Fatalf("GetInstanceIndex 失败: %v", err)
	}
	if idx != 3 {
		t.Errorf("charlie 序号期望 3（新追加），实际 %d", idx)
	}
}

// TestGetInstanceConfig 测试获取实例配置
func TestGetInstanceConfig(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	// 添加实例
	UpdateInstancesYAML("alice", InstanceWorkspacePath("alice"), CalculateInstancePorts(1))

	config, err := GetInstanceConfig("alice")
	if err != nil {
		t.Fatalf("GetInstanceConfig 失败: %v", err)
	}
	if config == nil {
		t.Fatal("config 不应为 nil")
	}
	if config.Name != "alice" {
		t.Errorf("Name 期望 alice，实际 %s", config.Name)
	}
	if config.Ports["agent_server"] != BasePortAgentServer+PortStep {
		t.Errorf("agent_server 端口期望 %d，实际 %d", BasePortAgentServer+PortStep, config.Ports["agent_server"])
	}

	// 不存在的实例
	config, err = GetInstanceConfig("nonexistent")
	if err != nil {
		t.Fatalf("GetInstanceConfig 失败: %v", err)
	}
	if config != nil {
		t.Error("不存在的实例应返回 nil")
	}
}

// TestLoadAllInstanceConfigs 测试加载所有实例配置
func TestLoadAllInstanceConfigs(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	UpdateInstancesYAML("alice", InstanceWorkspacePath("alice"), CalculateInstancePorts(1))
	UpdateInstancesYAML("bob", InstanceWorkspacePath("bob"), CalculateInstancePorts(2))

	configs, err := LoadAllInstanceConfigs()
	if err != nil {
		t.Fatalf("LoadAllInstanceConfigs 失败: %v", err)
	}
	if len(configs) != 2 {
		t.Errorf("实例数量期望 2，实际 %d", len(configs))
	}
	if _, ok := configs["alice"]; !ok {
		t.Error("应包含 alice")
	}
	if _, ok := configs["bob"]; !ok {
		t.Error("应包含 bob")
	}
}

// TestIsPortAvailable 测试端口可用性检查
func TestIsPortAvailable(t *testing.T) {
	// 使用一个不太可能被占用的端口
	port := 58092
	if !IsPortAvailable("127.0.0.1", port) {
		t.Errorf("端口 %d 应该可用", port)
	}
}
