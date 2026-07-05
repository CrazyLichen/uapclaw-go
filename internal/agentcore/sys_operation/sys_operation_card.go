package sys_operation

import (
	"fmt"
	"strings"

	schema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LocalWorkConfig 本地工作目录配置，控制 Shell 命令执行的安全边界。
type LocalWorkConfig struct {
	// ShellAllowlist Shell 命令白名单
	ShellAllowlist []string `yaml:"shell_allowlist" json:"shell_allowlist"`
	// SandboxRoot 沙箱根目录
	SandboxRoot string `yaml:"sandbox_root" json:"sandbox_root"`
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

// SysOperationCard 系统操作配置卡片，嵌入 BaseCard 提供身份标识，
// 并携带操作模式、工作目录配置和沙箱网关配置。
type SysOperationCard struct {
	schema.BaseCard
	// Mode 操作模式
	Mode OperationMode `json:"mode"`
	// WorkConfig 本地工作目录配置
	WorkConfig *LocalWorkConfig `json:"work_config,omitempty"`
	// GatewayConfig 沙箱网关配置
	GatewayConfig *SandboxGatewayConfig `json:"gateway_config,omitempty"`
	// isolationKeyTemplate 隔离键模板
	isolationKeyTemplate string
}

// ToolIdProxy 工具标识代理，用于通过方法链生成指定操作类型的工具标识。
type ToolIdProxy struct {
	// cardID 卡片标识
	cardID string
	// opType 操作类型
	opType string
}

// SysOperationCardOption SysOperationCard 构造选项函数
type SysOperationCardOption func(*SysOperationCard)

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

// NewSysOperationCard 创建 SysOperationCard 实例，默认 Mode 为 OperationModeLocal。
func NewSysOperationCard(opts ...SysOperationCardOption) *SysOperationCard {
	card := &SysOperationCard{
		BaseCard: *schema.NewBaseCard(),
		Mode:     OperationModeLocal,
	}
	for _, opt := range opts {
		opt(card)
	}
	return card
}

// NewSysOperationCardWithMode 创建指定操作模式的 SysOperationCard 实例。
func NewSysOperationCardWithMode(mode OperationMode) *SysOperationCard {
	return NewSysOperationCard(WithSysOpMode(mode))
}

// GenerateToolID 生成工具标识，格式为 {cardID}.{opType}.{methodName}。
func (c *SysOperationCard) GenerateToolID(opType, methodName string) string {
	return fmt.Sprintf("%s.%s.%s", c.ID, opType, methodName)
}

// IsolationKeyTemplate 返回隔离键模板。
func (c *SysOperationCard) IsolationKeyTemplate() string {
	return c.isolationKeyTemplate
}

// SetIsolationKeyTemplate 设置隔离键模板。
func (c *SysOperationCard) SetIsolationKeyTemplate(tpl string) {
	c.isolationKeyTemplate = tpl
}

// Fs 返回文件系统操作的工具标识代理。
func (c *SysOperationCard) Fs() *ToolIdProxy {
	return &ToolIdProxy{cardID: c.ID, opType: "fs"}
}

// Shell 返回 Shell 操作的工具标识代理。
func (c *SysOperationCard) Shell() *ToolIdProxy {
	return &ToolIdProxy{cardID: c.ID, opType: "shell"}
}

// Code 返回代码执行的工具标识代理。
func (c *SysOperationCard) Code() *ToolIdProxy {
	return &ToolIdProxy{cardID: c.ID, opType: "code"}
}

// ToolID 生成工具标识，格式为 {cardID}.{opType}.{methodName}。
func (p *ToolIdProxy) ToolID(methodName string) string {
	return fmt.Sprintf("%s.%s.%s", p.cardID, p.opType, methodName)
}

// WithSysOpMode 设置操作模式。
func WithSysOpMode(mode OperationMode) SysOperationCardOption {
	return func(c *SysOperationCard) { c.Mode = mode }
}

// WithSysOpWorkConfig 设置本地工作目录配置。
func WithSysOpWorkConfig(config *LocalWorkConfig) SysOperationCardOption {
	return func(c *SysOperationCard) { c.WorkConfig = config }
}

// WithSysOpGatewayConfig 设置沙箱网关配置。
func WithSysOpGatewayConfig(config *SandboxGatewayConfig) SysOperationCardOption {
	return func(c *SysOperationCard) { c.GatewayConfig = config }
}

// WithSysOpIsolationKeyTemplate 设置隔离键模板。
func WithSysOpIsolationKeyTemplate(tpl string) SysOperationCardOption {
	return func(c *SysOperationCard) { c.isolationKeyTemplate = tpl }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// generateIsolationKeyTemplate 生成隔离键模板，格式为 {containerScope}_{launcherType}_{sandboxType}_{prefix}_{identity}。
// 其中 identity 取值：SYSTEM 时为 "system"，SESSION 时为 "{session_id}"，CUSTOM 时为 customID。
func generateIsolationKeyTemplate(isolationPrefix string, containerScope ContainerScope, customID string, launcherType string, sandboxType string) string {
	var identity string
	switch containerScope {
	case ContainerScopeSystem:
		identity = "system"
	case ContainerScopeSession:
		identity = "{session_id}"
	case ContainerScopeCustom:
		identity = customID
	default:
		identity = customID
	}

	parts := []string{
		containerScope.String(),
		launcherType,
		sandboxType,
		isolationPrefix,
		identity,
	}
	return strings.Join(parts, "_")
}
