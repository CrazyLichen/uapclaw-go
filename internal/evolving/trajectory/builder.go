package trajectory

import (
	"github.com/google/uuid"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TrajectoryBuilder 轨迹组装器，用于在线和离线路径的轨迹构建。
//
// 职责：
//   - 步骤累积（按插入顺序）
//   - Cost 累积（input_tokens/output_tokens）
//   - 最终 Trajectory 组装
//
// 不负责：
//   - 格式转换（由调用方处理）
//   - Span 解析（由 Extractor 处理）
//   - 持久化（由 Store 处理）
//
// 对应 Python: openjiuwen/agent_evolving/trajectory/builder.py TrajectoryBuilder
type TrajectoryBuilder struct {
	// sessionID 会话标识（在线模式为 conversation_id，离线模式为 case_id）
	sessionID string
	// source 执行来源："online" 或 "offline"
	source string
	// caseID 离线模式下的用例标识（可选）
	caseID string
	// memberID 团队成员标识（可选）
	memberID string
	// maxSteps 最大保留步骤数（可选，nil 表示不限制）
	maxSteps *int
	// meta 扩展元数据
	meta map[string]any
	// steps 已累积的步骤
	steps []*TrajectoryStep
	// cost 聚合成本指标
	cost map[string]int
	// startTimeMs 首个步骤的开始时间（毫秒时间戳，可选）
	startTimeMs *int
}

// TrajectoryBuilderOption TrajectoryBuilder 构造选项函数。
type TrajectoryBuilderOption func(*TrajectoryBuilder)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTrajectoryBuilder 创建 TrajectoryBuilder 实例。
//
// 对应 Python: TrajectoryBuilder(session_id, source, case_id, member_id, meta, max_steps)
func NewTrajectoryBuilder(sessionID, source string, opts ...TrajectoryBuilderOption) *TrajectoryBuilder {
	b := &TrajectoryBuilder{
		sessionID: sessionID,
		source:    source,
		meta:      map[string]any{},
		steps:     []*TrajectoryStep{},
		cost:      map[string]int{"input_tokens": 0, "output_tokens": 0},
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// WithCaseID 设置离线模式下的用例标识。
func WithCaseID(caseID string) TrajectoryBuilderOption {
	return func(b *TrajectoryBuilder) { b.caseID = caseID }
}

// WithMemberID 设置团队成员标识。
//
// 对齐 Python: if member_id: self.meta.setdefault("member_id", member_id)
func WithMemberID(memberID string) TrajectoryBuilderOption {
	return func(b *TrajectoryBuilder) {
		b.memberID = memberID
		if b.meta == nil {
			b.meta = map[string]any{}
		}
		if _, exists := b.meta["member_id"]; !exists && memberID != "" {
			b.meta["member_id"] = memberID
		}
	}
}

// WithMeta 设置扩展元数据。
//
// 对齐 Python: self.meta: Dict[str, Any] = dict(meta or {})
func WithMeta(meta map[string]any) TrajectoryBuilderOption {
	return func(b *TrajectoryBuilder) {
		if meta != nil {
			b.meta = meta
		}
	}
}

// WithMaxSteps 设置最大保留步骤数。
//
// 对齐 Python: max_steps < 1 时抛出 ValueError。
// Go 中不 panic，maxSteps < 1 时忽略该选项。
func WithMaxSteps(maxSteps int) TrajectoryBuilderOption {
	return func(b *TrajectoryBuilder) {
		if maxSteps >= 1 {
			b.maxSteps = &maxSteps
		}
	}
}

// RecordStep 记录一个步骤并累积成本。
//
// 对齐 Python: TrajectoryBuilder.record_step(step)
func (b *TrajectoryBuilder) RecordStep(step *TrajectoryStep) {
	// 对齐 Python: self.steps.append(step)
	b.steps = append(b.steps, step)

	// 对齐 Python: if self.max_steps is not None and len(self.steps) > self.max_steps:
	//     self.steps = self.steps[-self.max_steps:]
	if b.maxSteps != nil && len(b.steps) > *b.maxSteps {
		b.steps = b.steps[len(b.steps)-*b.maxSteps:]
	}

	// 对齐 Python: if step.kind == "llm" and step.detail:
	//     if isinstance(step.detail, LLMCallDetail) and step.detail.usage:
	//         self.cost["input_tokens"] += step.detail.usage.get("prompt_tokens", 0)
	//         self.cost["output_tokens"] += step.detail.usage.get("completion_tokens", 0)
	if step.Kind == StepKindLLM {
		if detail, ok := step.Detail.(*LLMCallDetail); ok && detail != nil && detail.Usage != nil {
			if pt, ok := detail.Usage["prompt_tokens"]; ok {
				b.cost["input_tokens"] += toIntFromAny(pt)
			}
			if ct, ok := detail.Usage["completion_tokens"]; ok {
				b.cost["output_tokens"] += toIntFromAny(ct)
			}
		}
	}

	// 对齐 Python: if self._start_time_ms is None and step.start_time_ms:
	if b.startTimeMs == nil && step.StartTimeMs != 0 {
		ms := step.StartTimeMs
		b.startTimeMs = &ms
	}
}

// Build 组装最终 Trajectory。
//
// 对齐 Python: TrajectoryBuilder.build()
func (b *TrajectoryBuilder) Build() *Trajectory {
	// 对齐 Python: meta: dict[str, Any] = {}
	//     if self.member_id: meta["member_id"] = self.member_id
	meta := map[string]any{}
	for k, v := range b.meta {
		meta[k] = v
	}
	if b.memberID != "" {
		meta["member_id"] = b.memberID
	}

	// 对齐 Python: cost=self.cost if self.cost["input_tokens"] > 0 else None
	var cost CostInfo
	if b.cost["input_tokens"] > 0 {
		cost = map[string]int{
			"input_tokens":  b.cost["input_tokens"],
			"output_tokens": b.cost["output_tokens"],
		}
	}

	return &Trajectory{
		ExecutionID: generateUUID(),
		SessionID:   b.sessionID,
		Source:      b.source,
		CaseID:      b.caseID,
		Steps:       b.steps,
		Cost:        cost,
		Meta:        meta,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// generateUUID 生成唯一执行标识符。
//
// 对应 Python: _generate_uuid() -> str(uuid.uuid4())
func generateUUID() string {
	return uuid.New().String()
}

// toIntFromAny 安全地将 any 转换为 int。
func toIntFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
