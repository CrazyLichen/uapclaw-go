package sys_operation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── NewLocalWorkConfig ────────────────────────────

// TestNewLocalWorkConfig 测试本地工作目录配置默认值
// 对齐 Python LocalWorkConfig：shell_allowlist 有默认列表，restrict_to_sandbox=False。
func TestNewLocalWorkConfig(t *testing.T) {
	cfg := NewLocalWorkConfig()
	assert.False(t, cfg.RestrictToSandbox, "RestrictToSandbox 默认应为 false，对齐 Python")
	assert.NotEmpty(t, cfg.ShellAllowlist, "ShellAllowlist 应有默认白名单")
	assert.Contains(t, cfg.ShellAllowlist, "echo")
	assert.Contains(t, cfg.ShellAllowlist, "git")
	assert.Contains(t, cfg.ShellAllowlist, "python3")
	assert.Nil(t, cfg.DangerousPatterns)
}

// ──────────────────────────── NewSandboxGatewayConfig ────────────────────────────

// TestNewSandboxGatewayConfig 测试沙箱网关配置默认值
// 对齐 Python SandboxGatewayConfig：isolation, launcher_config, timeout_seconds。
func TestNewSandboxGatewayConfig(t *testing.T) {
	cfg := NewSandboxGatewayConfig()
	require.NotNil(t, cfg.LauncherConfig, "LauncherConfig 不应为 nil")
	assert.Equal(t, "pre_deploy", cfg.LauncherConfig.LauncherType, "LauncherType 默认应对齐 Python")
	assert.Equal(t, "aio", cfg.LauncherConfig.SandboxType, "SandboxType 默认应对齐 Python PreDeployLauncherConfig")
	assert.Equal(t, 30, cfg.TimeoutSeconds, "TimeoutSeconds 默认应为 30，对齐 Python")
	assert.Equal(t, ContainerScopeSession, cfg.Isolation.ContainerScope, "ContainerScope 默认应对齐 Python SESSION")
}

// ──────────────────────────── NewSysOperationCard ────────────────────────────

// TestNewSysOperationCard 测试系统操作卡片默认值
func TestNewSysOperationCard(t *testing.T) {
	card := NewSysOperationCard()
	assert.Equal(t, OperationModeLocal, card.Mode)
	assert.NotNil(t, card.ID)
	assert.NotEmpty(t, card.ID)
}

// TestNewSysOperationCardWithMode 测试指定操作模式创建系统操作卡片
func TestNewSysOperationCardWithMode(t *testing.T) {
	card := NewSysOperationCardWithMode(OperationModeSandbox)
	assert.Equal(t, OperationModeSandbox, card.Mode)
}

// ──────────────────────────── SysOperationCardOption ────────────────────────────

// TestWithSysOpMode 测试设置操作模式选项
func TestWithSysOpMode(t *testing.T) {
	card := NewSysOperationCard(WithSysOpMode(OperationModeSandbox))
	assert.Equal(t, OperationModeSandbox, card.Mode)
}

// TestWithSysOpWorkConfig 测试设置本地工作目录配置选项
func TestWithSysOpWorkConfig(t *testing.T) {
	workCfg := NewLocalWorkConfig()
	workCfg.SandboxRoot = []string{"/tmp/sandbox"}
	card := NewSysOperationCard(WithSysOpWorkConfig(workCfg))
	require.NotNil(t, card.WorkConfig)
	assert.Equal(t, []string{"/tmp/sandbox"}, card.WorkConfig.SandboxRoot)
	assert.False(t, card.WorkConfig.RestrictToSandbox, "RestrictToSandbox 应为 false")
}

// TestWithSysOpGatewayConfig 测试设置沙箱网关配置选项
func TestWithSysOpGatewayConfig(t *testing.T) {
	gwCfg := NewSandboxGatewayConfig()
	gwCfg.TimeoutSeconds = 60
	card := NewSysOperationCard(WithSysOpGatewayConfig(gwCfg))
	require.NotNil(t, card.GatewayConfig)
	assert.Equal(t, 60, card.GatewayConfig.TimeoutSeconds)
}

// TestWithSysOpIsolationKeyTemplate 测试设置隔离键模板选项
func TestWithSysOpIsolationKeyTemplate(t *testing.T) {
	card := NewSysOperationCard(WithSysOpIsolationKeyTemplate("my_template"))
	assert.Equal(t, "my_template", card.IsolationKeyTemplate())
}

// ──────────────────────────── GenerateToolID ────────────────────────────

// TestGenerateToolID 测试生成工具标识格式
func TestGenerateToolID(t *testing.T) {
	card := NewSysOperationCard()
	card.ID = "test_card"
	toolID := card.GenerateToolID("fs", "read_file")
	assert.Equal(t, "test_card.fs.read_file", toolID)
}

// TestGenerateToolID_不同操作类型 测试生成不同操作类型的工具标识
func TestGenerateToolID_不同操作类型(t *testing.T) {
	card := NewSysOperationCard()
	card.ID = "my_card"
	assert.Equal(t, "my_card.shell.execute", card.GenerateToolID("shell", "execute"))
	assert.Equal(t, "my_card.code.run", card.GenerateToolID("code", "run"))
}

// ──────────────────────────── IsolationKeyTemplate ────────────────────────────

// TestIsolationKeyTemplate_默认为空 测试隔离键模板默认为空
func TestIsolationKeyTemplate_默认为空(t *testing.T) {
	card := NewSysOperationCard()
	assert.Equal(t, "", card.IsolationKeyTemplate())
}

// TestSetIsolationKeyTemplate 测试设置隔离键模板
func TestSetIsolationKeyTemplate(t *testing.T) {
	card := NewSysOperationCard()
	card.SetIsolationKeyTemplate("new_template")
	assert.Equal(t, "new_template", card.IsolationKeyTemplate())
}

// ──────────────────────────── ToolIdProxy ────────────────────────────

// TestToolIdProxy_Fs 测试文件系统操作的工具标识代理
func TestToolIdProxy_Fs(t *testing.T) {
	card := NewSysOperationCard()
	card.ID = "card1"
	proxy := card.Fs()
	assert.Equal(t, "card1.fs.read_file", proxy.ToolID("read_file"))
	assert.Equal(t, "card1.fs.write_file", proxy.ToolID("write_file"))
}

// TestToolIdProxy_Shell 测试 Shell 操作的工具标识代理
func TestToolIdProxy_Shell(t *testing.T) {
	card := NewSysOperationCard()
	card.ID = "card2"
	proxy := card.Shell()
	assert.Equal(t, "card2.shell.execute", proxy.ToolID("execute"))
}

// TestToolIdProxy_Code 测试代码执行的工具标识代理
func TestToolIdProxy_Code(t *testing.T) {
	card := NewSysOperationCard()
	card.ID = "card3"
	proxy := card.Code()
	assert.Equal(t, "card3.code.run", proxy.ToolID("run"))
}

// TestToolIdProxy_ToolID 测试 ToolID 方法生成正确格式
func TestToolIdProxy_ToolID(t *testing.T) {
	proxy := &ToolIdProxy{cardID: "myid", opType: "fs"}
	assert.Equal(t, "myid.fs.read", proxy.ToolID("read"))
}

// ──────────────────────────── generateIsolationKeyTemplate ────────────────────────────

// TestGenerateIsolationKeyTemplate_SYSTEM 测试系统级容器生成隔离键模板
func TestGenerateIsolationKeyTemplate_SYSTEM(t *testing.T) {
	result, err := generateIsolationKeyTemplate("prefix1", ContainerScopeSystem, "", "docker", "python")
	require.NoError(t, err)
	assert.Equal(t, "system_docker_python_prefix1_system", result)
}

// TestGenerateIsolationKeyTemplate_SESSION 测试会话级容器生成隔离键模板
func TestGenerateIsolationKeyTemplate_SESSION(t *testing.T) {
	result, err := generateIsolationKeyTemplate("prefix2", ContainerScopeSession, "", "k8s", "node")
	require.NoError(t, err)
	assert.Equal(t, "session_k8s_node_prefix2_{session_id}", result)
}

// TestGenerateIsolationKeyTemplate_CUSTOM 测试自定义容器生成隔离键模板
func TestGenerateIsolationKeyTemplate_CUSTOM(t *testing.T) {
	result, err := generateIsolationKeyTemplate("prefix3", ContainerScopeCustom, "my_custom_id", "docker", "go")
	require.NoError(t, err)
	assert.Equal(t, "custom_docker_go_prefix3_my_custom_id", result)
}

// TestGenerateIsolationKeyTemplate_CUSTOM空customID 测试自定义容器作用域空 customID 返回错误
// 对齐 Python: container_scope is CUSTOM but custom_id is None → raise ValueError
func TestGenerateIsolationKeyTemplate_CUSTOM空customID(t *testing.T) {
	_, err := generateIsolationKeyTemplate("prefix3", ContainerScopeCustom, "", "docker", "go")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "container_scope is CUSTOM but custom_id is empty")
}

// TestGenerateIsolationKeyTemplate_空前缀 测试空前缀生成隔离键模板
func TestGenerateIsolationKeyTemplate_空前缀(t *testing.T) {
	result, err := generateIsolationKeyTemplate("", ContainerScopeSystem, "", "docker", "python")
	require.NoError(t, err)
	assert.Equal(t, "system_docker_python_system", result)
}

// TestGenerateIsolationKeyTemplate_未知作用域 测试未知容器作用域使用 customID
func TestGenerateIsolationKeyTemplate_未知作用域(t *testing.T) {
	result, err := generateIsolationKeyTemplate("pfx", ContainerScope(99), "fallback_id", "docker", "sh")
	require.NoError(t, err)
	assert.Equal(t, "unknown(99)_docker_sh_pfx_fallback_id", result)
}
