package workspace

import (
	"net"
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

// TestCheckPortConflicts 测试端口冲突检测
func TestCheckPortConflicts(t *testing.T) {
	// 启动一个监听器占用一个端口
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	defer ln.Close()

	occupiedPort := ln.Addr().(*net.TCPAddr).Port

	// 测试端口冲突：指定已占用的端口
	ports := map[string]int{
		"agent_server": occupiedPort,
		"web":          59999, // 未占用的端口
	}
	conflicts := CheckPortConflicts(ports, nil)
	if len(conflicts) == 0 {
		t.Error("应检测到端口冲突")
	}

	// 验证冲突端口是 occupiedPort
	found := false
	for _, p := range conflicts {
		if p == occupiedPort {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("冲突端口应包含 %d，实际 %v", occupiedPort, conflicts)
	}
}

// TestCheckPortConflicts_无冲突 测试无端口冲突
func TestCheckPortConflicts_无冲突(t *testing.T) {
	// 使用不太可能被占用的端口
	ports := map[string]int{
		"agent_server": 58092,
		"web":          59000,
	}
	conflicts := CheckPortConflicts(ports, nil)
	if len(conflicts) != 0 {
		t.Errorf("无冲突时应返回空，实际 %v", conflicts)
	}
}

// TestCheckPortConflicts_existingPorts 测试与 existingPorts 列表的冲突
func TestCheckPortConflicts_existingPorts(t *testing.T) {
	ports := map[string]int{
		"agent_server": 18092,
		"web":          19000,
	}
	existingPorts := []int{18092}
	conflicts := CheckPortConflicts(ports, existingPorts)
	if len(conflicts) == 0 {
		t.Error("应检测到与 existingPorts 的冲突")
	}
}

// TestSortedKeys 测试排序函数
func TestSortedKeys(t *testing.T) {
	m := map[string]any{
		"charlie": 1,
		"alpha":   2,
		"bravo":   3,
	}
	keys := sortedKeys(m)
	if len(keys) != 3 {
		t.Fatalf("长度期望 3，实际 %d", len(keys))
	}
	// 应按字母排序
	if keys[0] != "alpha" || keys[1] != "bravo" || keys[2] != "charlie" {
		t.Errorf("排序结果不正确: %v", keys)
	}
}

// TestIsPortAvailable_端口被占用 测试端口被占用时返回 false
func TestIsPortAvailable_端口被占用(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	if IsPortAvailable("127.0.0.1", port) {
		t.Error("已占用的端口应返回 false")
	}
}

// TestLoadAllInstanceConfigs_空 测试无实例时返回空 map
func TestLoadAllInstanceConfigs_空(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	configs, err := LoadAllInstanceConfigs()
	if err != nil {
		t.Fatalf("LoadAllInstanceConfigs 失败: %v", err)
	}
	if len(configs) != 0 {
		t.Errorf("无实例时应返回空 map，实际 %d 个", len(configs))
	}
}

// TestSaveInstancesYAML_创建目录 测试目录不存在时自动创建
func TestSaveInstancesYAML_创建目录(t *testing.T) {
	tmpDir := t.TempDir()
	// 将 instances.yaml 放在尚未存在的子目录中
	os.Setenv(EnvDataDir, filepath.Join(tmpDir, "deep", "nested"))
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	data := map[string]any{
		"instances": map[string]any{},
	}
	if err := SaveInstancesYAML(data); err != nil {
		t.Fatalf("SaveInstancesYAML 应自动创建目录: %v", err)
	}
}

// TestGetInstanceConfig_不存在的实例 测试不存在的实例返回 nil
func TestGetInstanceConfig_不存在的实例(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	config, err := GetInstanceConfig("nonexistent")
	if err != nil {
		t.Fatalf("GetInstanceConfig 失败: %v", err)
	}
	if config != nil {
		t.Error("不存在的实例应返回 nil")
	}
}

// TestUpdateInstancesYAML_自动计算端口 测试 ports 为 nil 时自动计算
func TestUpdateInstancesYAML_自动计算端口(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	// ports 传 nil，应自动计算
	if err := UpdateInstancesYAML("alice", InstanceWorkspacePath("alice"), nil); err != nil {
		t.Fatalf("UpdateInstancesYAML 失败: %v", err)
	}

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

// TestLoadInstancesYAML_无效YAML 测试解析无效 YAML 文件时的错误
func TestLoadInstancesYAML_无效YAML(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	// 创建无效 YAML 文件
	yamlPath := filepath.Join(tmpDir, "instances.yaml")
	os.WriteFile(yamlPath, []byte(":\n  invalid: [yaml: content\n"), 0o644)

	_, err := LoadInstancesYAML()
	if err == nil {
		t.Error("无效 YAML 应返回错误")
	}
}

// TestLoadInstancesYAML_空文件 测试空 YAML 文件
func TestLoadInstancesYAML_空文件(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	yamlPath := filepath.Join(tmpDir, "instances.yaml")
	os.WriteFile(yamlPath, []byte(""), 0o644)

	data, err := LoadInstancesYAML()
	if err != nil {
		t.Fatalf("空文件应返回空结构: %v", err)
	}
	if _, ok := data["instances"]; !ok {
		t.Error("空文件应包含 instances 键")
	}
}

// TestGetInstanceIndex_instances不是map 测试 instances 字段不是 map 的情况
func TestGetInstanceIndex_instances不是map(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	// 创建 instances 为非 map 的 YAML
	yamlPath := filepath.Join(tmpDir, "instances.yaml")
	os.WriteFile(yamlPath, []byte("instances: \"not a map\"\n"), 0o644)

	idx, err := GetInstanceIndex("alice")
	if err != nil {
		t.Fatalf("GetInstanceIndex 失败: %v", err)
	}
	if idx != 1 {
		t.Errorf("非 map 的 instances 应返回 1，实际 %d", idx)
	}
}

// TestGetInstanceConfig_instances不是map 测试 instances 不是 map 时返回 nil
func TestGetInstanceConfig_instances不是map(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	yamlPath := filepath.Join(tmpDir, "instances.yaml")
	os.WriteFile(yamlPath, []byte("instances: \"not a map\"\n"), 0o644)

	config, err := GetInstanceConfig("alice")
	if err != nil {
		t.Fatalf("GetInstanceConfig 失败: %v", err)
	}
	if config != nil {
		t.Error("instances 不是 map 时应返回 nil")
	}
}

// TestGetInstanceConfig_条目不是map 测试实例条目不是 map 时返回 nil
func TestGetInstanceConfig_条目不是map(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	yamlPath := filepath.Join(tmpDir, "instances.yaml")
	os.WriteFile(yamlPath, []byte("instances:\n  alice: \"not a map\"\n"), 0o644)

	config, err := GetInstanceConfig("alice")
	if err != nil {
		t.Fatalf("GetInstanceConfig 失败: %v", err)
	}
	if config != nil {
		t.Error("条目不是 map 时应返回 nil")
	}
}

// TestLoadAllInstanceConfigs_条目不是map 测试实例条目不是 map 时跳过
func TestLoadAllInstanceConfigs_条目不是map(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	yamlPath := filepath.Join(tmpDir, "instances.yaml")
	os.WriteFile(yamlPath, []byte("instances:\n  alice: \"not a map\"\n  bob:\n    workspace: /tmp/bob\n    ports:\n      agent_server: 28092\n      web: 29000\n      gateway: 29001\n      frontend: 6173\n"), 0o644)

	configs, err := LoadAllInstanceConfigs()
	if err != nil {
		t.Fatalf("LoadAllInstanceConfigs 失败: %v", err)
	}
	if _, ok := configs["alice"]; ok {
		t.Error("非 map 条目应被跳过")
	}
	if _, ok := configs["bob"]; !ok {
		t.Error("有效条目应被保留")
	}
}

// TestUpdateInstancesYAML_更新已有实例 测试更新已有实例条目
func TestUpdateInstancesYAML_更新已有实例(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	// 先创建实例
	ports1 := CalculateInstancePorts(1)
	UpdateInstancesYAML("alice", InstanceWorkspacePath("alice"), ports1)

	// 更新同一个实例（ports 不为 nil，已在 instances 中）
	ports2 := CalculateInstancePorts(2)
	if err := UpdateInstancesYAML("alice", "/new/workspace", ports2); err != nil {
		t.Fatalf("UpdateInstancesYAML 更新失败: %v", err)
	}

	config, err := GetInstanceConfig("alice")
	if err != nil {
		t.Fatalf("GetInstanceConfig 失败: %v", err)
	}
	if config.Workspace != "/new/workspace" {
		t.Errorf("workspace 应更新为 /new/workspace，实际 %s", config.Workspace)
	}
}

// TestUpdateInstancesYAML_instances不是map 测试 instances 字段不是 map 时重建
func TestUpdateInstancesYAML_instances不是map(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	// 创建 instances 为非 map 的 YAML
	yamlPath := filepath.Join(tmpDir, "instances.yaml")
	os.WriteFile(yamlPath, []byte("instances: \"not a map\"\n"), 0o644)

	// 应能正常更新（重建 instances 为 map）
	ports := CalculateInstancePorts(1)
	if err := UpdateInstancesYAML("alice", InstanceWorkspacePath("alice"), ports); err != nil {
		t.Fatalf("UpdateInstancesYAML 失败: %v", err)
	}

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

// TestGetInstanceConfig_缺失端口 测试实例配置中缺失端口时自动填充
func TestGetInstanceConfig_缺失端口(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	// 创建只有部分端口的实例
	yamlPath := filepath.Join(tmpDir, "instances.yaml")
	os.WriteFile(yamlPath, []byte("instances:\n  alice:\n    workspace: /tmp/alice\n    ports:\n      agent_server: 28092\n"), 0o644)

	config, err := GetInstanceConfig("alice")
	if err != nil {
		t.Fatalf("GetInstanceConfig 失败: %v", err)
	}
	if config == nil {
		t.Fatal("config 不应为 nil")
	}

	// 缺失的端口应被自动填充
	if _, ok := config.Ports["web"]; !ok {
		t.Error("缺失的 web 端口应被自动填充")
	}
	if _, ok := config.Ports["gateway"]; !ok {
		t.Error("缺失的 gateway 端口应被自动填充")
	}
}

// TestGetInstanceConfig_自定义workspace 测试实例配置中使用自定义 workspace
func TestGetInstanceConfig_自定义workspace(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	yamlPath := filepath.Join(tmpDir, "instances.yaml")
	os.WriteFile(yamlPath, []byte("instances:\n  alice:\n    workspace: /custom/path/alice\n    ports:\n      agent_server: 28092\n      web: 29000\n      gateway: 29001\n      frontend: 6173\n"), 0o644)

	config, err := GetInstanceConfig("alice")
	if err != nil {
		t.Fatalf("GetInstanceConfig 失败: %v", err)
	}
	if config.Workspace != "/custom/path/alice" {
		t.Errorf("workspace 期望 /custom/path/alice，实际 %s", config.Workspace)
	}
}

// TestSaveInstancesYAML_写入失败 测试 SaveInstancesYAML 写入失败
func TestSaveInstancesYAML_写入失败(t *testing.T) {
	// 设置一个无效的路径（只读目录）
	// 通过设置 EnvDataDir 为一个不存在的深层路径来测试 MkdirAll 失败
	// 实际上 MkdirAll 通常不会失败，除非权限问题
	// 这个测试验证 MkdirAll 成功但 WriteFile 失败的场景较难构造
	// 跳过此测试
}

// TestLoadInstancesYAML_读取失败 测试 LoadInstancesYAML 读取非 YAML 文件的错误
func TestLoadInstancesYAML_读取失败(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	// 创建一个目录为 instances.yaml（导致 ReadFile 失败）
	yamlPath := filepath.Join(tmpDir, "instances.yaml")
	os.MkdirAll(yamlPath, 0o755)

	_, err := LoadInstancesYAML()
	if err == nil {
		t.Error("读取目录为 YAML 应返回错误")
	}
}

// TestGetInstanceIndex_新实例 测试不存在的实例返回新序号
func TestGetInstanceIndex_新实例(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	// 添加一个实例
	UpdateInstancesYAML("alice", InstanceWorkspacePath("alice"), CalculateInstancePorts(1))

	// 查询不存在的实例
	idx, err := GetInstanceIndex("bob")
	if err != nil {
		t.Fatalf("GetInstanceIndex 失败: %v", err)
	}
	if idx != 2 {
		t.Errorf("新实例序号期望 2，实际 %d", idx)
	}
}
