package sys_operation

import (
	"fmt"
	"strings"

	schema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SysOperationCard 系统操作配置卡片，嵌入 BaseCard 提供身份标识，
// 并携带操作模式、隔离配置、工作目录配置和沙箱网关配置。
// 对齐 Python SysOperationCard：mode, isolation_prefix, container_scope, custom_id,
// work_config(local_work_config), gateway_config(sandbox_gateway_config)。
type SysOperationCard struct {
	schema.BaseCard
	// Mode 操作模式
	Mode OperationMode `json:"mode"`
	// IsolationPrefix 隔离键前缀（对齐 Python isolation_prefix）
	IsolationPrefix string `json:"isolation_prefix,omitempty"`
	// ContainerScope 容器作用域（对齐 Python container_scope）
	ContainerScope ContainerScope `json:"container_scope,omitempty"`
	// CustomID 自定义容器标识（对齐 Python custom_id）
	CustomID string `json:"custom_id,omitempty"`
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

// WithSysOpIsolationPrefix 设置隔离键前缀。
// 对齐 Python SysOperationCard.isolation_prefix。
func WithSysOpIsolationPrefix(prefix string) SysOperationCardOption {
	return func(c *SysOperationCard) { c.IsolationPrefix = prefix }
}

// WithSysOpContainerScope 设置容器作用域。
// 对齐 Python SysOperationCard.container_scope。
func WithSysOpContainerScope(scope ContainerScope) SysOperationCardOption {
	return func(c *SysOperationCard) { c.ContainerScope = scope }
}

// WithSysOpCustomID 设置自定义容器标识。
// 对齐 Python SysOperationCard.custom_id。
func WithSysOpCustomID(id string) SysOperationCardOption {
	return func(c *SysOperationCard) { c.CustomID = id }
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
