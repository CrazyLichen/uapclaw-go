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

// SandboxGatewayConfig 沙箱网关配置，定义沙箱实例的网关连接与认证信息。
// 对齐 Python SandboxGatewayConfig：gateway_url, gateway_token, launcher_type, sandbox_type, sandbox_image, timeout。
// 扁平结构，不含嵌套 Isolation/LauncherConfig 子结构。
type SandboxGatewayConfig struct {
	// GatewayURL 网关地址
	GatewayURL string `yaml:"gateway_url" json:"gateway_url"`
	// GatewayToken 网关认证令牌
	GatewayToken string `yaml:"gateway_token" json:"gateway_token,omitempty"`
	// LauncherType 启动器类型
	LauncherType string `yaml:"launcher_type" json:"launcher_type"`
	// SandboxType 沙箱类型
	SandboxType string `yaml:"sandbox_type" json:"sandbox_type"`
	// SandboxImage 沙箱镜像
	SandboxImage string `yaml:"sandbox_image" json:"sandbox_image,omitempty"`
	// TimeoutSeconds 超时时间（秒）
	TimeoutSeconds float64 `yaml:"timeout_seconds" json:"timeout_seconds"`
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
			"echo", "rg", "ls", "cat", "head", "tail", "find", "grep",
			"awk", "sed", "sort", "uniq", "wc", "diff", "curl", "wget",
			"git", "make", "cmake", "cargo", "go", "python3", "python",
			"node", "npm", "npx", "yarn", "pnpm", "pip", "pip3",
			"mv", "cp", "mkdir", "touch", "chmod", "chown",
			"tar", "gzip", "gunzip", "zip", "unzip",
			"docker", "kubectl", "terraform",
		},
		RestrictToSandbox: false,
	}
}

// NewSandboxGatewayConfig 创建 SandboxGatewayConfig 实例。
// 对齐 Python SandboxGatewayConfig 默认值：
// gateway_url="http://localhost:8080", launcher_type="pre_deploy", sandbox_type="aio", timeout=300.0。
func NewSandboxGatewayConfig() *SandboxGatewayConfig {
	return &SandboxGatewayConfig{
		GatewayURL:     "http://localhost:8080",
		LauncherType:   "pre_deploy",
		SandboxType:    "aio",
		TimeoutSeconds: 300.0,
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
