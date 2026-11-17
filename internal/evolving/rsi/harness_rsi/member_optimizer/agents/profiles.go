// Package agents 提供 MemberOptimizer DeepAgent 的装配与结构化输出助手。
//
// 对齐 Python openjiuwen/rsi/harness_rsi/member_optimizer/agents/（17.4c1 落
// profiles/output/prompts embed 与 factory；planner/executor/repair 工厂依赖的
// 执行细节随 17.4c2/c3 消费方落地）。
//
// 文件目录：
//
//	agents/
//	├── doc.go        # 包文档
//	├── embed.go     # prompts/*.md go:embed
//	├── factory.go   # 模型载入与 agent 工厂
//	├── output.go    # 结构化输出解析与重试循环
//	├── profiles.go  # 五类 agent 档案
//	└── prompts/    # 提示词模板（逐字复制 Python agents/prompts/）
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/member_optimizer/agents/
package agents

// ──────────────────────────── 结构体 ────────────────────────────

// MemberOptimizerAgentProfile 单个 Member Optimizer Agent 的档案配置。
// Python: profiles.py MemberOptimizerAgentProfile（frozen dataclass）。
type MemberOptimizerAgentProfile struct {
	// Key 档案键
	Key string
	// AgentName agent 名
	AgentName string
	// Description 描述
	Description string
	// PromptFile 提示词文件名
	PromptFile string
	// MaxIterations 最大迭代（默认 300）
	MaxIterations int
	// EnabledSkills 启用的技能
	EnabledSkills []string
}

// ──────────────────────────── 常量 ────────────────────────────

// RoleAttribution 定责 agent 档案。
// Python: profiles.py ROLE_ATTRIBUTION。
var RoleAttribution = MemberOptimizerAgentProfile{
	Key:           "role_attribution",
	AgentName:     "member_role_attributor",
	Description:   "Attributes Team issues to member Expert Harness roles.",
	PromptFile:    "role_attribution.md",
	MaxIterations: 300,
}

// MechanismAttribution 病根归因 agent 档案。
// Python: profiles.py MECHANISM_ATTRIBUTION。
var MechanismAttribution = MemberOptimizerAgentProfile{
	Key:           "mechanism_attribution",
	AgentName:     "member_mechanism_attributor",
	Description:   "Diagnoses role-internal Expert Harness failure mechanisms.",
	PromptFile:    "mechanism_attribution.md",
	MaxIterations: 300,
}

// ActionPlanning 开单 agent 档案。
// Python: profiles.py ACTION_PLANNING。
var ActionPlanning = MemberOptimizerAgentProfile{
	Key:           "action_planning",
	AgentName:     "member_action_planner",
	Description:   "Plans constrained member Expert Harness changes.",
	PromptFile:    "action_planning.md",
	MaxIterations: 300,
}

// ActionExecution 动刀 agent 档案。
// Python: profiles.py ACTION_EXECUTION。
var ActionExecution = MemberOptimizerAgentProfile{
	Key:           "action_execution",
	AgentName:     "member_action_executor",
	Description:   "Executes one constrained Expert Harness package action.",
	PromptFile:    "action_execution.md",
	MaxIterations: 300,
}

// VerificationRepair 验证补救 agent 档案。
// Python: profiles.py VERIFICATION_REPAIR。
var VerificationRepair = MemberOptimizerAgentProfile{
	Key:           "verification_repair",
	AgentName:     "member_harness_verifier_repair",
	Description:   "Repairs deterministic verification failures in one role package.",
	PromptFile:    "verification_repair.md",
	MaxIterations: 300,
}

// agentProfiles 档案注册表（声明序稳定）。
var agentProfiles = []MemberOptimizerAgentProfile{
	RoleAttribution,
	MechanismAttribution,
	ActionPlanning,
	ActionExecution,
	VerificationRepair,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetAgentProfile 返回内建档案；未知键报错（Python KeyError 等价）。
// Python: profiles.py:72-78 get_agent_profile。
func GetAgentProfile(key string) (MemberOptimizerAgentProfile, error) {
	for _, profile := range agentProfiles {
		if profile.Key == key {
			return profile, nil
		}
	}
	return MemberOptimizerAgentProfile{}, &UnknownProfileError{Key: key}
}

// IterAgentProfiles 按声明序返回内建档案。
// Python: profiles.py:80-88 iter_agent_profiles。
func IterAgentProfiles() []MemberOptimizerAgentProfile {
	out := make([]MemberOptimizerAgentProfile, len(agentProfiles))
	copy(out, agentProfiles)
	return out
}

// UnknownProfileError 未知档案键错误（Python KeyError 等价）。
type UnknownProfileError struct {
	// Key 档案键
	Key string
}

// Error 实现 error 接口（文案逐字对齐 Python KeyError 消息）。
func (e *UnknownProfileError) Error() string {
	return "unknown Member Optimizer Agent profile: " + e.Key
}

// ──────────────────────────── 非导出函数 ────────────────────────────
