package workspace

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InstanceConfig 命名实例配置。
//
// 对应 Python: jiuwenswarm/instance_manager/config.py InstanceConfig
type InstanceConfig struct {
	Name      string         // 实例名称（唯一标识）
	Workspace string         // 实例工作区路径
	Ports     map[string]int // 端口分配
}

// InstanceStatus 实例运行状态。
//
// 1.7 仅定义结构，进程检测在领域十二实现。
// 对应 Python: jiuwenswarm/instance_manager/config.py InstanceStatus
type InstanceStatus struct {
	Name      string         // 实例名称
	Running   bool           // 是否运行中
	PID       int            // 进程 ID（0 表示无）
	Workspace string         // 工作区路径
	Ports     map[string]int // 端口分配
	StartedAt int64          // 启动时间戳（Unix 秒，0 表示无）
}

// instancesYAMLData instances.yaml 的顶层结构。
//
// 预留结构体，当前未直接使用（通过 map[string]any 操作 YAML 数据）。
//
//nolint:unused // 预留结构体，后续领域实现时启用
type instancesYAMLData struct {
	Instances map[string]*instanceEntry `yaml:"instances"`
}

// instanceEntry instances.yaml 中单个实例的条目。
//
// 预留结构体，当前未直接使用（通过 map[string]any 操作 YAML 数据）。
//
//nolint:unused // 预留结构体，后续领域实现时启用
type instanceEntry struct {
	Workspace string         `yaml:"workspace,omitempty"`
	Ports     map[string]int `yaml:"ports,omitempty"`
}

// ──────────────────────────── 常量 ────────────────────────────
const (
	// BasePortAgentServer 默认实例的 AgentServer 基准端口。
	BasePortAgentServer = 18092
	// BasePortWeb 默认实例的 Web 基准端口。
	BasePortWeb = 19000
	// BasePortGateway 默认实例的 Gateway 基准端口。
	BasePortGateway = 19001
	// BasePortFrontend 默认实例的 Frontend 基准端口。
	BasePortFrontend = 5173

	// PortStep 端口步进：index * PortStep。
	PortStep = 1000

	// PIDFilename 实例 PID 文件名。
	PIDFilename = ".instance.pid"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// basePorts 默认实例的基准端口表。
var basePorts = map[string]int{
	"agent_server": BasePortAgentServer,
	"web":          BasePortWeb,
	"gateway":      BasePortGateway,
	"frontend":     BasePortFrontend,
}

// PortTypes 端口类型列表。
var PortTypes = []string{"agent_server", "web", "gateway", "frontend"}

// reservedNames 保留名称集合。
var reservedNames = map[string]bool{
	"default": true, "config": true, "tmp": true,
	"uapclaw": true, "all": true,
}

// instanceNamePattern 实例名称正则：字母/数字/下划线/连字符。
var instanceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateInstanceName 验证实例名称，返回 nil 表示合法。
//
// 规则（与 Python validate_instance_name() 对齐）：
//   - 长度 1-64
//   - 只含字母、数字、下划线、连字符
//   - 不能以点开头
//   - 不能是保留名称
func ValidateInstanceName(name string) error {
	if name == "" {
		return fmt.Errorf("实例名称不能为空")
	}
	if len(name) > 64 {
		return fmt.Errorf("实例名称不能超过 64 个字符，实际 %d 个", len(name))
	}
	if !instanceNamePattern.MatchString(name) {
		return fmt.Errorf("实例名称只能包含字母、数字、下划线、连字符")
	}
	if name[0] == '.' {
		return fmt.Errorf("实例名称不能以点开头")
	}
	if reservedNames[name] {
		return fmt.Errorf("实例名称 %q 是保留名称", name)
	}
	return nil
}

// IsValidInstanceName 验证实例名称是否合法。
func IsValidInstanceName(name string) bool {
	return ValidateInstanceName(name) == nil
}

// CalculateInstancePorts 计算实例端口分配：base + index * PortStep。
//
// index=0 为默认实例，index=1+ 为命名实例。
// 对应 Python: calculate_instance_ports(index)
func CalculateInstancePorts(index int) map[string]int {
	ports := make(map[string]int, len(basePorts))
	for k, v := range basePorts {
		ports[k] = v + index*PortStep
	}
	return ports
}

// ComputeAutoPort 计算单个端口类型。
//
// 对应 Python: compute_auto_port(port_type, index)
func ComputeAutoPort(portType string, index int) int {
	base, ok := basePorts[portType]
	if !ok {
		return 10000 + index*PortStep // 未知类型降级
	}
	return base + index*PortStep
}

// InstancesYAMLPath 返回 instances.yaml 路径：~/.uapclaw/instances.yaml
//
// 对应 Python: get_instances_yaml_path()
func InstancesYAMLPath() string {
	return filepath.Join(WorkspaceDir(), "instances.yaml")
}

// InstancesDir 返回命名实例根目录：~/.uapclaw-instances/
//
// 对应 Python: get_instances_dir()
func InstancesDir() string {
	return filepath.Join(UserHomeDir(), DefaultInstancesDir)
}

// InstanceWorkspacePath 返回命名实例工作区路径：~/.uapclaw-instances/<name>/
//
// 对应 Python: get_instance_workspace_path(name)
func InstanceWorkspacePath(name string) string {
	return filepath.Join(InstancesDir(), name)
}

// LoadInstancesYAML 加载 instances.yaml。
//
// 文件不存在时返回空结构（不是错误）。
// 对应 Python: load_instances_yaml()
func LoadInstancesYAML() (map[string]any, error) {
	path := InstancesYAMLPath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{"instances": map[string]any{}}, nil
		}
		return nil, fmt.Errorf("读取 instances.yaml 失败: %w", err)
	}

	var result map[string]any
	if err := yaml.Unmarshal(data, &result); err != nil {
		logger.Warn(logComponent).Str("path", path).Err(err).Msg("解析 instances.yaml 失败")
		return nil, fmt.Errorf("解析 instances.yaml 失败: %w", err)
	}

	if result == nil {
		result = map[string]any{"instances": map[string]any{}}
	}
	if _, ok := result["instances"]; !ok {
		result["instances"] = map[string]any{}
	}

	return result, nil
}

// SaveInstancesYAML 保存 instances.yaml。
//
// 对应 Python: save_instances_yaml(data)
func SaveInstancesYAML(data map[string]any) error {
	path := InstancesYAMLPath()

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建 instances.yaml 目录失败: %w", err)
	}

	content, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("序列化 instances.yaml 失败: %w", err)
	}

	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("写入 instances.yaml 失败: %w", err)
	}

	logger.Debug(logComponent).Str("path", path).Msg("已保存 instances.yaml")
	return nil
}

// UpdateInstancesYAML 添加或更新实例条目。
//
// 对应 Python: update_instances_yaml(name, workspace, ports)
func UpdateInstancesYAML(name string, workspace string, ports map[string]int) error {
	data, err := LoadInstancesYAML()
	if err != nil {
		return err
	}

	instances, ok := data["instances"].(map[string]any)
	if !ok {
		instances = map[string]any{}
		data["instances"] = instances
	}

	// 计算端口（如未提供）
	if ports == nil {
		existingNames := sortedKeys(instances)
		index := len(existingNames) + 1
		for i, n := range existingNames {
			if n == name {
				index = i + 1
				break
			}
		}
		ports = CalculateInstancePorts(index)
	}

	instances[name] = map[string]any{
		"workspace": workspace,
		"ports":     ports,
	}

	return SaveInstancesYAML(data)
}

// GetInstanceIndex 获取实例在 instances.yaml 中的序号（1 起始，0 预留给默认）。
//
// 对应 Python: get_instance_index(name)
func GetInstanceIndex(name string) (int, error) {
	data, err := LoadInstancesYAML()
	if err != nil {
		return 0, err
	}

	instances, ok := data["instances"].(map[string]any)
	if !ok {
		return 1, nil
	}

	keys := sortedKeys(instances)
	for i, k := range keys {
		if k == name {
			return i + 1, nil
		}
	}

	// 新实例，将追加到末尾
	return len(keys) + 1, nil
}

// GetInstanceConfig 从 instances.yaml 加载实例配置。
//
// 对应 Python: get_instance_config(name)
func GetInstanceConfig(name string) (*InstanceConfig, error) {
	data, err := LoadInstancesYAML()
	if err != nil {
		return nil, err
	}

	instances, ok := data["instances"].(map[string]any)
	if !ok {
		return nil, nil
	}

	entry, ok := instances[name]
	if !ok {
		return nil, nil
	}

	entryMap, ok := entry.(map[string]any)
	if !ok {
		return nil, nil
	}

	// 解析 workspace
	ws := InstanceWorkspacePath(name)
	if wsVal, ok := entryMap["workspace"].(string); ok && wsVal != "" {
		ws = wsVal
	}

	// 解析 ports
	ports := make(map[string]int)
	keys := sortedKeys(instances)
	index := 1
	for i, k := range keys {
		if k == name {
			index = i + 1
			break
		}
	}

	if portsVal, ok := entryMap["ports"].(map[string]any); ok {
		for _, pt := range PortTypes {
			if pv, ok := portsVal[pt]; ok {
				if pvi, ok := pv.(int); ok {
					ports[pt] = pvi
				}
			}
		}
	}

	// 自动填充缺失端口
	for _, pt := range PortTypes {
		if _, ok := ports[pt]; !ok {
			ports[pt] = ComputeAutoPort(pt, index)
		}
	}

	return &InstanceConfig{
		Name:      name,
		Workspace: ws,
		Ports:     ports,
	}, nil
}

// LoadAllInstanceConfigs 加载所有实例配置。
//
// 对应 Python: load_all_instance_configs()
func LoadAllInstanceConfigs() (map[string]*InstanceConfig, error) {
	data, err := LoadInstancesYAML()
	if err != nil {
		return nil, err
	}

	instances, ok := data["instances"].(map[string]any)
	if !ok {
		return map[string]*InstanceConfig{}, nil
	}

	configs := make(map[string]*InstanceConfig, len(instances))
	keys := sortedKeys(instances)

	for i, name := range keys {
		entry, ok := instances[name]
		if !ok {
			continue
		}

		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}

		index := i + 1

		// 解析 workspace
		ws := InstanceWorkspacePath(name)
		if wsVal, ok := entryMap["workspace"].(string); ok && wsVal != "" {
			ws = wsVal
		}

		// 解析 ports
		ports := make(map[string]int)
		if portsVal, ok := entryMap["ports"].(map[string]any); ok {
			for _, pt := range PortTypes {
				if pv, ok := portsVal[pt]; ok {
					if pvi, ok := pv.(int); ok {
						ports[pt] = pvi
					}
				}
			}
		}

		// 自动填充缺失端口
		for _, pt := range PortTypes {
			if _, ok := ports[pt]; !ok {
				ports[pt] = ComputeAutoPort(pt, index)
			}
		}

		configs[name] = &InstanceConfig{
			Name:      name,
			Workspace: ws,
			Ports:     ports,
		}
	}

	return configs, nil
}

// IsPortAvailable 检查端口是否可用。
//
// 对应 Python: is_port_available(host, port)
func IsPortAvailable(host string, port int) bool {
	addr := fmt.Sprintf("%s:%d", host, port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

// CheckPortConflicts 检查端口冲突。
//
// 对应 Python: check_port_conflicts(ports, host, existing_ports)
func CheckPortConflicts(ports map[string]int, existingPorts []int) []int {
	existingSet := make(map[int]bool, len(existingPorts))
	for _, p := range existingPorts {
		existingSet[p] = true
	}

	var conflicts []int
	for _, port := range ports {
		if existingSet[port] {
			conflicts = append(conflicts, port)
		} else if !IsPortAvailable("127.0.0.1", port) {
			conflicts = append(conflicts, port)
		}
	}

	return conflicts
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sortedKeys 返回 map[string]any 的排序后 key 列表。
// Go 的 map 迭代顺序不确定，这里按 key 排序保证与 YAML 中的声明顺序无关。
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// 简单排序即可，不需要与 Python 的插入顺序完全一致
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
