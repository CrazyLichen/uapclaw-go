package single_agent

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 枚举 ────────────────────────────

// 以下类型别名为子包 re-export，保持包内兼容。

type (
	// AgentOption Agent 调用选项函数（re-export from interfaces）
	AgentOption = interfaces.AgentOption
	// AbilityManager 能力管理器（re-export from ability 子包）
	AbilityManager = ability.AbilityManager
	// AddAbilityResult 添加能力结果（re-export from ability 子包）
	AddAbilityResult = ability.AddAbilityResult
	// ExecuteResult 工具执行结果（re-export from ability 子包）
	ExecuteResult = ability.ExecuteResult
	// AbilityExecutionError 能力执行错误（re-export from ability 子包）
	AbilityExecutionError = ability.AbilityExecutionError
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
