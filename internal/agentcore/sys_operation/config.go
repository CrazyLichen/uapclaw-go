package sys_operation

import (
	"encoding/json"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LocalWorkConfig 本地工作目录配置，控制 Shell 命令执行的安全边界。
// 对齐 Python LocalWorkConfig：shell_allowlist, sandbox_root([]string), restrict_to_sandbox, dangerous_patterns。
type LocalWorkConfig struct {
	// ShellAllowlist Shell 命令白名单
	ShellAllowlist []string `yaml:"shell_allowlist" json:"shell_allowlist"`
	// SandboxRoot 沙箱根目录（对齐 Python sandbox_root: Optional[List[str]]）
	SandboxRoot []string `yaml:"sandbox_root" json:"sandbox_root"`
	// RestrictToSandbox 是否限制在沙箱目录内
	RestrictToSandbox bool `yaml:"restrict_to_sandbox" json:"restrict_to_sandbox"`
	// DangerousPatterns 危险命令模式列表
	DangerousPatterns []string `yaml:"dangerous_patterns" json:"dangerous_patterns"`
}

// SandboxIsolationConfig 沙箱隔离配置，定义容器隔离与命名粒度。
// 对齐 Python SandboxIsolationConfig：custom_id, container_scope, prefix。
type SandboxIsolationConfig struct {
	// CustomID 核心身份覆盖，设置后替换自动的 session_id 或 context_id
	CustomID string `yaml:"custom_id" json:"custom_id,omitempty"`
	// ContainerScope 容器粒度模板：SYSTEM / SESSION / CUSTOM
	ContainerScope ContainerScope `yaml:"container_scope" json:"container_scope"`
	// Prefix 命名空间前缀，用于同一作用域内隔离多个角色/任务
	Prefix string `yaml:"prefix" json:"prefix,omitempty"`
}

// SandboxLauncherConfig 沙箱启动器配置，定义如何获取/连接沙箱运行时。
// 对齐 Python SandboxLauncherConfig：launcher_type, gateway_url, sandbox_type, on_stop, idle_ttl_seconds, extra_params。
type SandboxLauncherConfig struct {
	// LauncherType 启动器类型
	LauncherType string `yaml:"launcher_type" json:"launcher_type"`
	// GatewayURL 远端沙箱网关服务端点
	GatewayURL string `yaml:"gateway_url" json:"gateway_url"`
	// SandboxType 沙箱 Provider 类型，如 aio/e2b/mock
	SandboxType string `yaml:"sandbox_type" json:"sandbox_type"`
	// OnStop 停止行为：delete（销毁）/ pause（暂停）/ keep（保持运行）
	OnStop string `yaml:"on_stop" json:"on_stop"`
	// IdleTTLSeconds 空闲驱逐 TTL（秒），超时后始终 delete
	IdleTTLSeconds *int `yaml:"idle_ttl_seconds" json:"idle_ttl_seconds,omitempty"`
	// ExtraParams 传递给启动器的任意参数
	ExtraParams map[string]any `yaml:"extra_params" json:"extra_params,omitempty"`
}

// PreDeployLauncherConfig 预部署启动器配置，用于已存在的可通过 HTTP/WS 访问的沙箱。
// 对齐 Python PreDeployLauncherConfig：launcher_type="pre_deploy", sandbox_type, base_url。
// 嵌入 SandboxLauncherConfig，额外增加 BaseURL 字段。
type PreDeployLauncherConfig struct {
	// LauncherType 启动器类型，固定为 "pre_deploy"
	LauncherType string `yaml:"launcher_type" json:"launcher_type"`
	// GatewayURL 远端网关地址（继承自 SandboxLauncherConfig）
	GatewayURL string `yaml:"gateway_url" json:"gateway_url"`
	// SandboxType 沙箱 Provider 类型
	SandboxType string `yaml:"sandbox_type" json:"sandbox_type"`
	// OnStop 停止行为
	OnStop string `yaml:"on_stop" json:"on_stop"`
	// IdleTTLSeconds 空闲驱逐 TTL
	IdleTTLSeconds *int `yaml:"idle_ttl_seconds" json:"idle_ttl_seconds,omitempty"`
	// ExtraParams 任意参数
	ExtraParams map[string]any `yaml:"extra_params" json:"extra_params,omitempty"`
	// BaseURL 沙箱服务基础 URL（http:// 或 ws://）
	BaseURL string `yaml:"base_url" json:"base_url"`
}

// SandboxGatewayConfig 沙箱网关配置，定义沙箱实例的网关连接与认证信息。
// 对齐 Python SandboxGatewayConfig：isolation, launcher_config, timeout_seconds, auth_headers, auth_query_params。
type SandboxGatewayConfig struct {
	// Isolation 隔离与命名策略配置
	Isolation SandboxIsolationConfig `yaml:"isolation" json:"isolation"`
	// LauncherConfig 启动器配置，对齐 Python Union[PreDeployLauncherConfig, SandboxLauncherConfig]。
	// 生产中 100% 使用 PreDeployLauncherConfig，Go 用具体类型代替 Python Union。
	LauncherConfig *PreDeployLauncherConfig `yaml:"launcher_config" json:"launcher_config"`
	// TimeoutSeconds 统一超时时间（秒），包含请求+就绪检测
	TimeoutSeconds int `yaml:"timeout_seconds" json:"timeout_seconds"`
	// AuthHeaders 认证 HTTP 头
	AuthHeaders map[string]string `yaml:"auth_headers" json:"auth_headers,omitempty"`
	// AuthQueryParams 认证查询参数
	AuthQueryParams map[string]string `yaml:"auth_query_params" json:"auth_query_params,omitempty"`
}

// GatewayStoreConfig 网关存储配置。
// 对齐 Python GatewayStoreConfig：type, redis_url。
type GatewayStoreConfig struct {
	// Type 存储类型，Phase 1 仅支持 memory
	Type string `yaml:"type" json:"type"`
	// RedisURL Redis 连接地址
	RedisURL string `yaml:"redis_url" json:"redis_url,omitempty"`
}

// GatewayConfig 网关配置。
// 对齐 Python GatewayConfig：store。
type GatewayConfig struct {
	// Store 存储配置
	Store GatewayStoreConfig `yaml:"store" json:"store"`
}

// SandboxCreateRequest 沙箱创建请求。
// 对齐 Python SandboxCreateRequest：isolation_key, config。
type SandboxCreateRequest struct {
	// IsolationKey 隔离键
	IsolationKey string `yaml:"isolation_key" json:"isolation_key,omitempty"`
	// Config 沙箱网关配置
	Config SandboxGatewayConfig `yaml:"config" json:"config"`
}

// GatewayInvokeRequest 网关全链路路由请求。
// 对齐 Python GatewayInvokeRequest：op_type, method, params, isolation_key。
type GatewayInvokeRequest struct {
	// OpType 操作类型：fs / shell / code
	OpType string `json:"op_type"`
	// Method 方法名，如 read_file, execute_cmd
	Method string `json:"method"`
	// Params 方法参数
	Params map[string]any `json:"params"`
	// IsolationKey 沙箱隔离键
	IsolationKey string `json:"isolation_key,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ContainerScope 容器作用域枚举。
// 对齐 Python ContainerScope：SYSTEM, SESSION, CUSTOM。
type ContainerScope int

const (
	// ContainerScopeSystem 系统级容器
	ContainerScopeSystem ContainerScope = 0
	// ContainerScopeSession 会话级容器
	ContainerScopeSession ContainerScope = 1
	// ContainerScopeCustom 自定义容器
	ContainerScopeCustom ContainerScope = 2
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLocalWorkConfig 创建 LocalWorkConfig 实例。
// 对齐 Python LocalWorkConfig 默认值：shell_allowlist 有完整默认列表，restrict_to_sandbox=False。
func NewLocalWorkConfig() *LocalWorkConfig {
	return &LocalWorkConfig{
		ShellAllowlist: []string{
			"echo", "rg", "ls", "dir", "cd", "pwd",
			"python", "python3", "pip", "pip3",
			"npm", "node", "git",
			"cat", "type",
			"mkdir", "md", "rm", "rd",
			"cp", "copy", "mv", "move",
			"grep", "find", "curl", "wget",
			"ps", "df", "ping",
		},
		RestrictToSandbox: false,
	}
}

// NewSandboxIsolationConfig 创建 SandboxIsolationConfig 实例，默认 ContainerScope=SESSION。
// 对齐 Python SandboxIsolationConfig 默认值。
func NewSandboxIsolationConfig() SandboxIsolationConfig {
	return SandboxIsolationConfig{
		ContainerScope: ContainerScopeSession,
	}
}

// NewSandboxLauncherConfig 创建 SandboxLauncherConfig 实例。
// 对齐 Python SandboxLauncherConfig 默认值：launcher_type="pre_deploy", sandbox_type="mock", on_stop="delete"。
func NewSandboxLauncherConfig() *SandboxLauncherConfig {
	return &SandboxLauncherConfig{
		LauncherType: "pre_deploy",
		SandboxType:  "mock",
		OnStop:       "delete",
	}
}

// NewPreDeployLauncherConfig 创建 PreDeployLauncherConfig 实例。
// 对齐 Python PreDeployLauncherConfig：launcher_type="pre_deploy", sandbox_type="aio"。
func NewPreDeployLauncherConfig(baseURL string) *PreDeployLauncherConfig {
	return &PreDeployLauncherConfig{
		LauncherType: "pre_deploy",
		SandboxType:  "aio",
		OnStop:       "delete",
		BaseURL:      baseURL,
	}
}

// NewSandboxGatewayConfig 创建 SandboxGatewayConfig 实例。
// 对齐 Python SandboxGatewayConfig 默认值：
// isolation=默认(SESSION), launcher_config=默认(pre_deploy+aio), timeout=30。
func NewSandboxGatewayConfig() *SandboxGatewayConfig {
	return &SandboxGatewayConfig{
		Isolation:      NewSandboxIsolationConfig(),
		LauncherConfig: NewPreDeployLauncherConfig(""),
		TimeoutSeconds: 30,
	}
}

// NewGatewayStoreConfig 创建 GatewayStoreConfig 实例，默认 type=memory。
func NewGatewayStoreConfig() GatewayStoreConfig {
	return GatewayStoreConfig{Type: "memory"}
}

// NewGatewayConfig 创建 GatewayConfig 实例。
func NewGatewayConfig() GatewayConfig {
	return GatewayConfig{Store: NewGatewayStoreConfig()}
}

// String 返回容器作用域的字符串表示
func (s ContainerScope) String() string {
	switch s {
	case ContainerScopeSystem:
		return "system"
	case ContainerScopeSession:
		return "session"
	case ContainerScopeCustom:
		return "custom"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// MarshalJSON 实现 json.Marshaler 接口
func (s ContainerScope) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON 实现 json.Unmarshaler 接口
func (s *ContainerScope) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	switch str {
	case "system":
		*s = ContainerScopeSystem
	case "session":
		*s = ContainerScopeSession
	case "custom":
		*s = ContainerScopeCustom
	default:
		return fmt.Errorf("未知的容器作用域: %s", str)
	}
	return nil
}
