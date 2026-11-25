// 长记忆写入组件（Python memory_write_comp.py 一比一）。
// 配置直接持 *ltm.LongTermMemory 实例（同 memory_retrieval：无 lazy/无 ResourceMgr）。
package resource

import (
	"context"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/security"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	memoryconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/memory/config"
	ltm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/memory/ltm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
)

// memoryWriteLogComponent 长记忆写入组件日志组件
const memoryWriteLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryWriteCompConfig 组件配置（Python :31-40 dataclass kw_only）。
type MemoryWriteCompConfig struct {
	// Memory 长记忆实例
	Memory *ltm.LongTermMemory
	// ScopeID 作用域标识（默认 "__default__"）
	ScopeID string
	// UserID 用户标识（默认 "__default__"）
	UserID string
	// SessionID 会话标识（默认 "__default__"；Python 刻意不取运行时 session——
	// 记忆归属完全由三元组键决定，键在配置里不在上下文里）
	SessionID string
	// AgentConfig 记忆 agent 配置（Python default_factory=AgentMemoryConfig）
	AgentConfig *memoryconfig.AgentMemoryConfig
	// GenMem 是否从历史消息生成记忆（Python 默认 true）。
	// *bool nil=未传→默认 true，非 nil 取显式值（bool 零值无法区分未传/false）
	GenMem *bool
	// GenMemWithHistoryMsgNum 生成记忆的历史消息窗口（Python 默认 2）
	GenMemWithHistoryMsgNum int
}

// newMemoryWriteConfigDefaults 填充默认值（Python dataclass 默认参数等价）。
func newMemoryWriteConfigDefaults(cfg *MemoryWriteCompConfig) *MemoryWriteCompConfig {
	if cfg == nil {
		cfg = &MemoryWriteCompConfig{}
	}
	if cfg.ScopeID == "" {
		cfg.ScopeID = ltm.DefaultValue
	}
	if cfg.UserID == "" {
		cfg.UserID = ltm.DefaultValue
	}
	if cfg.SessionID == "" {
		cfg.SessionID = ltm.DefaultValue
	}
	if cfg.AgentConfig == nil {
		cfg.AgentConfig = &memoryconfig.AgentMemoryConfig{}
	}
	if cfg.GenMemWithHistoryMsgNum == 0 {
		cfg.GenMemWithHistoryMsgNum = 2
	}
	if cfg.GenMem == nil {
		t := true // Python dataclass 默认 True
		cfg.GenMem = &t
	}
	return cfg
}

// MemoryWriteInput 输入（Python :42-46；timestamp 可选）
type MemoryWriteInput struct {
	Messages  []llmschema.BaseMessage
	Timestamp *time.Time
}

// validateMemoryWriteInputs 校验并提取输入（Python validate_inputs :168-178）。
func validateMemoryWriteInputs(inputs map[string]any) (MemoryWriteInput, error) {
	raw, ok := inputs["messages"]
	if !ok {
		return MemoryWriteInput{}, exception.BuildError(
			exception.StatusComponentMemoryWriteInputParamError,
			exception.WithMsg("messages\n  Field required"))
	}
	msgs, ok := raw.([]llmschema.BaseMessage)
	if !ok {
		// 宽松形态：[]any 内逐项 BaseMessage（workflow 上游输出经 map 传递的常见形态）
		rawList, isList := raw.([]any)
		if !isList {
			return MemoryWriteInput{}, exception.BuildError(
				exception.StatusComponentMemoryWriteInputParamError,
				exception.WithMsg("messages\n  Input should be a valid list"))
		}
		msgs = make([]llmschema.BaseMessage, 0, len(rawList))
		for _, item := range rawList {
			m, ok := item.(llmschema.BaseMessage)
			if !ok {
				return MemoryWriteInput{}, exception.BuildError(
					exception.StatusComponentMemoryWriteInputParamError,
					exception.WithMsg(fmt.Sprintf("messages\n  Input should be a valid list of BaseMessage [%v]", item)))
			}
			msgs = append(msgs, m)
		}
	}
	in := MemoryWriteInput{Messages: msgs}
	if rawTs, ok := inputs["timestamp"]; ok && rawTs != nil {
		ts, ok := rawTs.(time.Time)
		if !ok {
			return MemoryWriteInput{}, exception.BuildError(
				exception.StatusComponentMemoryWriteInputParamError,
				exception.WithMsg("timestamp\n  Input should be a valid datetime"))
		}
		in.Timestamp = &ts
	}
	return in, nil
}

// MemoryWriteExecutable 执行体（Python :48-58 字段一比一）。
type MemoryWriteExecutable struct {
	// WorkflowComponent 8.17 组件基座（InvokeFn 槽承载 invoke）
	components.WorkflowComponent
	// cfg 配置
	cfg *MemoryWriteCompConfig
	// memory 长记忆实例（构造期绑定）
	memory *ltm.LongTermMemory
	// sess 会话（Python self._session）
	sess *session.NodeSessionFacade
}

// NewMemoryWriteExecutable 构造（Python __init__ :49-55）
func NewMemoryWriteExecutable(cfg *MemoryWriteCompConfig) (*MemoryWriteExecutable, error) {
	cfg = newMemoryWriteConfigDefaults(cfg)
	if cfg.Memory == nil {
		return nil, fmt.Errorf("memory is required")
	}
	e := &MemoryWriteExecutable{cfg: cfg, memory: cfg.Memory}
	e.InvokeFn = e.invokeEntry
	return e, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// MemoryWriteComponent 组件外壳（Python MemoryWriteComponent）
type MemoryWriteComponent struct {
	components.WorkflowComponent
}

// NewMemoryWriteComponent 构造组件外壳（槽注入 self）
func NewMemoryWriteComponent(cfg *MemoryWriteCompConfig) (*MemoryWriteComponent, error) {
	exec, err := NewMemoryWriteExecutable(cfg)
	if err != nil {
		return nil, err
	}
	c := &MemoryWriteComponent{}
	c.ToExecutableFn = func() graphexe.GeneralExecutor { return exec }
	return c, nil
}

// invokeEntry 执行入口（Python invoke :60-164 步骤一比一）
func (e *MemoryWriteExecutable) invokeEntry(ctx context.Context,
	inputs map[string]any, sess *session.NodeSessionFacade, modelContext iface.ModelContext,
) (map[string]any, error) {
	e.sess = sess // Python self._set_session(:160-161)

	in, err := validateMemoryWriteInputs(inputs)
	if err != nil {
		return nil, err
	}
	if len(in.Messages) == 0 {
		return nil, exception.BuildError(
			exception.StatusComponentMemoryWriteInputParamError,
			exception.WithMsg("Messages list cannot be empty"))
	}

	// 起始日志（Python :66-84；字段逐项）
	logger.Info(memoryWriteLogComponent).
		Str("event_type", "WORKFLOW_COMPONENT_START").
		Str("component_id", sess.GetExecutableID()).
		Str("component_type_str", "LongTermMemoryWriteComponent").
		Str("session_id", sess.GetSessionID()).
		Int("message_count", len(in.Messages)).
		Str("scope_id", e.cfg.ScopeID).
		Str("user_id", e.cfg.UserID).
		Bool("gen_mem", *e.cfg.GenMem).
		Bool("sensitive_mode", security.IsSensitive()).
		Msg("Long-term memory write started")

	// 写入（Python :86-105；AddMessagesOption 逐参透传）
	addOpts := []ltm.AddMessagesOption{
		ltm.WithUserID(e.cfg.UserID),
		ltm.WithScopeID(e.cfg.ScopeID),
		ltm.WithSessionID(e.cfg.SessionID),
		ltm.WithGenMem(*e.cfg.GenMem),
		ltm.WithGenMemWithHistoryMsgNum(e.cfg.GenMemWithHistoryMsgNum),
	}
	if in.Timestamp != nil {
		addOpts = append(addOpts, ltm.WithTimestamp(*in.Timestamp))
	}
	if _, err := e.memory.AddMessages(ctx, in.Messages, e.cfg.AgentConfig, addOpts...); err != nil {
		logger.Error(memoryWriteLogComponent).
			Str("event_type", "WORKFLOW_COMPONENT_ERROR").
			Str("component_id", sess.GetSessionID()).
			Str("component_type_str", "LongTermMemoryWriteComponent").
			Str("session_id", sess.GetSessionID()).
			Err(err).
			Msg("Long-term memory write failed")
		return nil, exception.BuildError(
			exception.StatusComponentMemoryWriteInvokeCallFailed,
			exception.WithMsg(fmt.Sprintf("Memory write call failed: %s", err)),
			exception.WithCause(err))
	}

	// 结束日志（Python :111-127）
	logger.Info(memoryWriteLogComponent).
		Str("event_type", "WORKFLOW_COMPONENT_END").
		Str("component_id", sess.GetExecutableID()).
		Str("component_type_str", "LongTermMemoryWriteComponent").
		Str("session_id", sess.GetSessionID()).
		Int("message_count", len(in.Messages)).
		Bool("sensitive_mode", security.IsSensitive()).
		Msg("Long-term memory write completed")

	// Python 输出固定 {"success": true}
	return map[string]any{"success": true}, nil
}
