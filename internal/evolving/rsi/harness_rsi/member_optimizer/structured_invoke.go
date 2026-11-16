// structured_invoke.go 定义父包对 agents 子包能力的调用桥（AgentsBridge）。
// Python 中 member_optimizer/role_attributor.py 直接 import 子包
// member_optimizer.agents.{factory,output}；Go 不允许子包→父包反向循环
// （agents/factory.go 已引本包 BudgetedRsiModel/schema 值表），故将桥接点
// 收拢为本结构，由 agents 包 init 注册实现（spec §4 已知偏差 D-c1-1）。

package memberoptimizer

import (
	"context"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StructuredOutputExhaustedError 结构化输出重试耗尽（Python output.py:30-32）。
type StructuredOutputExhaustedError struct {
	// Msg 错误消息
	Msg string
}

// Error 实现 error 接口。
func (e *StructuredOutputExhaustedError) Error() string { return e.Msg }

// StructuredInvokeParams 结构化调用参数（Python invoke_member_optimizer_agent_structured 关键字参数）。
type StructuredInvokeParams struct {
	// Agent DeepAgent 实例（Python 类型 Any；默认实现断言 BaseAgent）
	Agent any
	// AgentName agent 名（错误消息与会话兜底卡名）
	AgentName string
	// UserMessage 用户消息
	UserMessage string
	// SessionID 会话 ID
	SessionID string
	// RetryLimit 重试上限
	RetryLimit int
	// ParseResponse 解析函数
	ParseResponse func(string) (map[string]any, error)
	// ValidateResponse 校验函数（nil 则不校验）
	ValidateResponse func(map[string]any) []string
	// BuildRetryMessage 自定义重试消息构造（nil 则用默认）
	BuildRetryMessage func(map[string]any, string) string
	// RecoverResponse 重试时的恢复函数（替代 agent 重放；nil 则正常调 agent）
	RecoverResponse func(message, rawText string) (map[string]any, error)
	// FeedbackTracker 反馈追踪器（nil 则按 retry_limit 构造）
	FeedbackTracker *harnessrsi.FeedbackTracker
	// OutputRepair 输出修复配置（tracker 为 nil 时使用）
	OutputRepair *harnessrsi.OutputRepairConfig
}

// AttributionAgentParams 归因 agent 构造参数（Python create_xxx_agent 关键字参数收拢）。
type AttributionAgentParams struct {
	// ModelConfigRef 模型配置文件引用
	ModelConfigRef string
	// Workspace 工作空间路径（空串等价 Python None）
	Workspace string
	// AgentSkillsDirs agent 技能目录
	AgentSkillsDirs []string
}

// AgentsBridge 父包对 agents 子包能力的调用桥。
// Python: role_attributor.py / mechanism_attributor.py 直接 import 子包符号；
// Go 因包环倒置为注册桥（agents 包 init 调 RegisterAgentsBridge）。
type AgentsBridge struct {
	// InvokeStructured 结构化调用（Python invoke_member_optimizer_agent_structured）
	InvokeStructured func(context.Context, StructuredInvokeParams) (map[string]any, error)
	// ParseJSONObject JSON 对象解析（Python output.py parse_json_object_response）
	ParseJSONObject func(string) (map[string]any, error)
	// ParseYamlOrJSONObject YAML/JSON 对象解析（Python output.py parse_yaml_or_json_object_response）
	ParseYamlOrJSONObject func(string) (map[string]any, error)
	// CreateRoleAttributionAgent 定责 agent 构造（Python factory.py:179）
	CreateRoleAttributionAgent func(context.Context, AttributionAgentParams) (any, error)
	// CreateMechanismAttributionAgent 定病根 agent 构造（Python factory.py:196）
	CreateMechanismAttributionAgent func(context.Context, AttributionAgentParams) (any, error)
	// CreateActionPlanningAgent 开单 agent 构造（Python factory.py:211-228）
	CreateActionPlanningAgent func(context.Context, PlannerAgentParams) (any, error)
	// CreateActionExecutionAgent 动刀 agent 构造（Python factory.py:231-249）
	CreateActionExecutionAgent func(ctx context.Context, params ExecutorAgentParams) (any, error)
	// InvokeAgent agent 直调（Python agent.invoke(inputs, session) 通道）
	InvokeAgent func(ctx context.Context, agent any, message, sessionID string) (map[string]any, error)
	// ExtractText 响应文本提取（Python extract_agent_text）
	ExtractText func(map[string]any) string
	// PackageWriteAllowed 包写允许（Python factory.py package_write_allowed）
	PackageWriteAllowed func(string, []string) bool
	// HostReadRoots 支持的主机只读契约根（Python supported_host_read_roots）
	HostReadRoots func() []string
}

// ExecutorAgentParams 动刀 agent 构造参数（Python create_action_execution_agent 关键字收拢）。
type ExecutorAgentParams struct {
	// ModelConfigRef 模型配置引用
	ModelConfigRef string
	// Workspace 工作空间（动作 worktree）
	Workspace string
	// AgentSkillsDirs agent 技能目录
	AgentSkillsDirs []string
	// DeclaredWritePaths 声明写路径
	DeclaredWritePaths []string
}

// ──────────────────────────── 全局变量 ────────────────────────────

// agentsBridge 子包能力桥（agents 包 init 注册；测试可在同包内覆写单项）。
var agentsBridge = AgentsBridge{}

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterAgentsBridge 注册子包能力桥（由 agents 包 init 调用）。
func RegisterAgentsBridge(bridge AgentsBridge) {
	agentsBridge = bridge
}

// SnapshotAgentsBridge 取当前能力桥快照（依赖注入方/测试可在替换后还原）。
func SnapshotAgentsBridge() AgentsBridge {
	return agentsBridge
}

// RestoreAgentsBridge 还原能力桥快照。
func RestoreAgentsBridge(saved AgentsBridge) {
	agentsBridge = saved
}

// ReplaceAgentsBridge 整体替换能力桥（依赖注入入口；组合根/测试使用）。
func ReplaceAgentsBridge(bridge AgentsBridge) {
	agentsBridge = bridge
}

// ──────────────────────────── 结构体（c2 扩展） ────────────────────────────

// PlannerAgentParams 开单 agent 构造参数（Python create_action_planning_agent 关键字参数收拢）。
type PlannerAgentParams struct {
	// ModelConfigRef 模型配置文件引用
	ModelConfigRef string
	// Workspace 工作空间路径
	Workspace string
	// AgentSkillsDirs agent 技能目录
	AgentSkillsDirs []string
	// ActionDefinitions 动作定义清单（harness_rsi.ActionDefinition；类型别名别名导入）
	ActionDefinitions []harnessrsi.ActionDefinition
	// HarnessSummaries 角色 harness 结构摘要（结构护栏构造时注入——D-c2-4）
	HarnessSummaries map[string]string
}
