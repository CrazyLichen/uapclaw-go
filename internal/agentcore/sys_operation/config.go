package sys_operation

import "fmt"

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

// SandboxIsolationConfig 沙箱隔离配置，定义沙箱实例的隔离策略。
type SandboxIsolationConfig struct {
	// CustomID 自定义容器标识
	CustomID string `yaml:"custom_id" json:"custom_id"`
	// ContainerScope 容器作用域
	ContainerScope ContainerScope `yaml:"container_scope" json:"container_scope"`
	// Prefix 隔离键前缀
	Prefix string `yaml:"prefix" json:"prefix"`
}

// SandboxLauncherConfig 沙箱启动器配置，定义沙箱实例的启动参数。
type SandboxLauncherConfig struct {
	// LauncherType 启动器类型
	LauncherType string `yaml:"launcher_type" json:"launcher_type"`
	// GatewayURL 网关地址
	GatewayURL string `yaml:"gateway_url" json:"gateway_url"`
	// SandboxType 沙箱类型
	SandboxType string `yaml:"sandbox_type" json:"sandbox_type"`
	// OnStop 停止时行为
	OnStop string `yaml:"on_stop" json:"on_stop"`
	// IdleTTLSeconds 空闲存活时间（秒）
	IdleTTLSeconds int `yaml:"idle_ttl_seconds" json:"idle_ttl_seconds"`
	// ExtraParams 额外参数
	ExtraParams map[string]any `yaml:"extra_params" json:"extra_params"`
}

// SandboxGatewayConfig 沙箱网关配置，定义沙箱实例的网关连接与认证信息。
type SandboxGatewayConfig struct {
	// Isolation 隔离配置
	Isolation SandboxIsolationConfig `yaml:"isolation" json:"isolation"`
	// LauncherConfig 启动器配置
	LauncherConfig SandboxLauncherConfig `yaml:"launcher_config" json:"launcher_config"`
	// TimeoutSeconds 超时时间（秒）
	TimeoutSeconds float64 `yaml:"timeout_seconds" json:"timeout_seconds"`
	// AuthHeaders 认证请求头
	AuthHeaders map[string]string `yaml:"auth_headers" json:"auth_headers"`
	// AuthQueryParams 认证查询参数
	AuthQueryParams map[string]string `yaml:"auth_query_params" json:"auth_query_params"`
}

// ContainerScope 容器作用域枚举
type ContainerScope int

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ContainerScopeSystem 系统级容器
	ContainerScopeSystem ContainerScope = 0
	// ContainerScopeSession 会话级容器
	ContainerScopeSession ContainerScope = 1
	// ContainerScopeCustom 自定义容器
	ContainerScopeCustom ContainerScope = 2
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLocalWorkConfig 创建 LocalWorkConfig 实例，默认 RestrictToSandbox 为 true。
func NewLocalWorkConfig() *LocalWorkConfig {
	return &LocalWorkConfig{
		RestrictToSandbox: true,
	}
}

// NewSandboxGatewayConfig 创建 SandboxGatewayConfig 实例，默认 TimeoutSeconds 为 30.0。
func NewSandboxGatewayConfig() *SandboxGatewayConfig {
	return &SandboxGatewayConfig{
		TimeoutSeconds: 30.0,
	}
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
