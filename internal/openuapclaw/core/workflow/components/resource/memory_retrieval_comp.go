// 长记忆检索组件（Python memory_retrieval_comp.py 一比一）。
// 配置直接持 *ltm.LongTermMemory 实例（无 lazy/无 ResourceMgr；
// Python arbitrary_types_allowed 同款依赖注入形态）。
package resource

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/security"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	ltm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/memory/ltm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
)

// memoryRetrievalLogComponent 长记忆检索组件日志组件
const memoryRetrievalLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryRetrievalCompConfig 组件配置（Python :28-34 dataclass kw_only）。
// DEFAULT_VALUE="__default__" 语义原样对齐（Python LongTermMemory.DEFAULT_VALUE）。
type MemoryRetrievalCompConfig struct {
	// Memory 长记忆实例（Python config.memory 直接持实例）
	Memory *ltm.LongTermMemory
	// ScopeID 作用域标识（默认 "__default__"）
	ScopeID string
	// UserID 用户标识（默认 "__default__"）
	UserID string
	// Threshold 最低相关度阈值（Python 默认 0.3）
	Threshold float64
}

// newMemoryRetrievalConfigDefaults 填充默认值（Python dataclass 默认参数等价）。
func newMemoryRetrievalConfigDefaults(cfg *MemoryRetrievalCompConfig) *MemoryRetrievalCompConfig {
	if cfg == nil {
		cfg = &MemoryRetrievalCompConfig{}
	}
	if cfg.ScopeID == "" {
		cfg.ScopeID = ltm.DefaultValue
	}
	if cfg.UserID == "" {
		cfg.UserID = ltm.DefaultValue
	}
	if cfg.Threshold == 0 {
		cfg.Threshold = 0.3
	}
	return cfg
}

// MemoryRetrievalInput 输入（Python :36-40；top_k 默认 5；extra=allow 只取已知键）
type MemoryRetrievalInput struct {
	Query string
	TopK  int
}

// validateMemoryRetrievalInputs 校验并提取输入（Python validate_inputs :133-144）。
func validateMemoryRetrievalInputs(inputs map[string]any) (MemoryRetrievalInput, error) {
	in := MemoryRetrievalInput{TopK: 5} // Python Field(default=5)
	raw, ok := inputs["query"]
	if !ok {
		return in, exception.BuildError(
			exception.StatusComponentMemoryRetrievalInputParamError,
			exception.WithMsg("query\n  Field required"))
	}
	// pydantic str 字段 lax 模式不接受 int→str 强转（round-1 D-25-4 同款）——直接类型断言
	query, ok := raw.(string)
	if !ok {
		return in, exception.BuildError(
			exception.StatusComponentMemoryRetrievalInputParamError,
			exception.WithMsg(fmt.Sprintf("query\n  Input should be a valid string [%v]", raw)))
	}
	in.Query = query
	if rawTopK, ok := inputs["top_k"]; ok {
		// Python int 字段 strict 语义（pydantic int 不接受 float）：逐类型收窄
		switch tv := rawTopK.(type) {
		case int:
			in.TopK = tv
		case int64:
			in.TopK = int(tv)
		default:
			return in, exception.BuildError(
				exception.StatusComponentMemoryRetrievalInputParamError,
				exception.WithMsg(fmt.Sprintf("top_k\n  Input should be a valid integer [%v]", rawTopK)))
		}
	}
	return in, nil
}

// formatMemoryRetrievalOutput 组装输出（Python _format_output :146-152；
// MemoryRetrievalOutput model_dump 等价 map）
func formatMemoryRetrievalOutput(fragment, summary []*ltm.MemResult) map[string]any {
	if fragment == nil {
		fragment = []*ltm.MemResult{}
	}
	if summary == nil {
		summary = []*ltm.MemResult{}
	}
	return map[string]any{
		"fragment_memory_results": fragment,
		"summary_results":         summary,
	}
}

// MemoryRetrievalExecutable 执行体（Python :42-52 字段一比一）。
type MemoryRetrievalExecutable struct {
	// WorkflowComponent 8.17 组件基座（InvokeFn 槽承载 invoke）
	components.WorkflowComponent
	// cfg 配置
	cfg *MemoryRetrievalCompConfig
	// memory 长记忆实例（Python self._memory，构造期绑定）
	memory *ltm.LongTermMemory
	// sess 会话（Python self._session）
	sess *session.NodeSessionFacade
}

// NewMemoryRetrievalExecutable 构造（Python __init__ :43-48）
func NewMemoryRetrievalExecutable(cfg *MemoryRetrievalCompConfig) (*MemoryRetrievalExecutable, error) {
	cfg = newMemoryRetrievalConfigDefaults(cfg)
	if cfg.Memory == nil {
		return nil, fmt.Errorf("memory is required")
	}
	e := &MemoryRetrievalExecutable{cfg: cfg, memory: cfg.Memory}
	e.InvokeFn = e.invokeEntry
	return e, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemoryRetrievalComponent 组件外壳（Python :157-168；槽注入 self，
// 基座槽优先不 override 两方法——8.20 教训）
func NewMemoryRetrievalComponent(cfg *MemoryRetrievalCompConfig) (*MemoryRetrievalComponent, error) {
	exec, err := NewMemoryRetrievalExecutable(cfg)
	if err != nil {
		return nil, err
	}
	c := &MemoryRetrievalComponent{}
	c.ToExecutableFn = func() graphexe.GeneralExecutor { return exec }
	return c, nil
}

// MemoryRetrievalComponent 组件外壳（Python MemoryRetrievalComponent）
type MemoryRetrievalComponent struct {
	components.WorkflowComponent
}

// invokeEntry 执行入口（Python invoke :55-131 步骤一比一）
func (e *MemoryRetrievalExecutable) invokeEntry(ctx context.Context,
	inputs map[string]any, sess *session.NodeSessionFacade, modelContext iface.ModelContext,
) (map[string]any, error) {
	e.sess = sess // Python self._set_session(:130-131)

	in, err := validateMemoryRetrievalInputs(inputs)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Query) == "" {
		return nil, exception.BuildError(
			exception.StatusComponentMemoryRetrievalInputParamError,
			exception.WithMsg("Query must be a non-empty string"))
	}

	// 起始日志（Python :60-77；字段逐项）
	logger.Info(memoryRetrievalLogComponent).
		Str("event_type", "WORKFLOW_COMPONENT_START").
		Str("component_id", sess.GetExecutableID()).
		Str("component_type_str", "MemoryRetrievalComponent").
		Str("session_id", sess.GetSessionID()).
		Int("query_length", len([]rune(in.Query))).
		Int("top_k", in.TopK).
		Float64("threshold", e.cfg.Threshold).
		Str("user_id", e.cfg.UserID).
		Str("scope_id", e.cfg.ScopeID).
		Bool("sensitive_mode", security.IsSensitive()).
		Msg("Memory retrieval started")

	// 双路检索（Python :79-98；同参 user_id/scope_id/threshold）
	memResults, err := e.memory.SearchUserMem(ctx, in.Query, in.TopK,
		ltm.SearchWithUserID(e.cfg.UserID),
		ltm.SearchWithScopeID(e.cfg.ScopeID),
		ltm.SearchWithThreshold(e.cfg.Threshold))
	if err != nil {
		logger.Error(memoryRetrievalLogComponent).
			Str("event_type", "WORKFLOW_COMPONENT_ERROR").
			Str("component_id", sess.GetSessionID()).
			Str("component_type_str", "MemoryRetrievalComponent").
			Str("session_id", sess.GetSessionID()).
			Err(err).
			Msg("Memory retrieval failed")
		return nil, exception.BuildError(
			exception.StatusComponentMemoryRetrievalInvokeCallFailed,
			exception.WithMsg(fmt.Sprintf("Memory retrieval call failed: %s", err)),
			exception.WithCause(err))
	}
	summaryResults, err := e.memory.SearchUserHistorySummary(ctx, in.Query, in.TopK,
		ltm.SearchWithUserID(e.cfg.UserID),
		ltm.SearchWithScopeID(e.cfg.ScopeID),
		ltm.SearchWithThreshold(e.cfg.Threshold))
	if err != nil {
		logger.Error(memoryRetrievalLogComponent).
			Str("event_type", "WORKFLOW_COMPONENT_ERROR").
			Str("component_id", sess.GetSessionID()).
			Str("component_type_str", "MemoryRetrievalComponent").
			Str("session_id", sess.GetSessionID()).
			Err(err).
			Msg("Memory retrieval failed")
		return nil, exception.BuildError(
			exception.StatusComponentMemoryRetrievalInvokeCallFailed,
			exception.WithMsg(fmt.Sprintf("Memory retrieval call failed: %s", err)),
			exception.WithCause(err))
	}

	output := formatMemoryRetrievalOutput(memResults, summaryResults)

	// 结束日志（Python :104-120）
	logger.Info(memoryRetrievalLogComponent).
		Str("event_type", "WORKFLOW_COMPONENT_END").
		Str("component_id", sess.GetExecutableID()).
		Str("component_type_str", "MemoryRetrievalComponent").
		Str("session_id", sess.GetSessionID()).
		Int("num_results", len(memResults)).
		Int("num_summary_results", len(summaryResults)).
		Bool("sensitive_mode", security.IsSensitive()).
		Msg("Memory retrieval completed")

	return output, nil
}
