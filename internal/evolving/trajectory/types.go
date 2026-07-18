package trajectory

// ──────────────────────────── 结构体 ────────────────────────────

// StepDetail 执行步骤的详细数据接口。
//
// LLM 步骤由 LLMCallDetail 实现，工具步骤由 ToolCallDetail 实现。
// StepKind() 方法提供类型判别，也可通过 Go 类型断言 switch d.(type) 判别。
//
// 对应 Python: StepDetail = Union[LLMCallDetail, ToolCallDetail]
type StepDetail interface {
	// StepKind 返回步骤类型（llm 或 tool）。
	StepKind() StepKind
}

// LLMCallDetail LLM 调用完整执行数据。
//
// 对应 Python: LLMCallDetail dataclass
type LLMCallDetail struct {
	// Model 模型名称
	Model string
	// Messages 消息列表（字典形式，外部存入时通过 MessageToDict 转换）
	Messages []map[string]any
	// Response 模型响应（字典形式，nil 表示无响应）
	Response map[string]any
	// Tools 工具定义列表（可选）
	Tools []map[string]any
	// Usage 使用量信息（可选）
	Usage map[string]any
	// Meta 扩展元数据
	Meta map[string]any
}

// ToolCallDetail 工具调用完整执行数据。
//
// 对应 Python: ToolCallDetail dataclass
type ToolCallDetail struct {
	// ToolName 工具名称
	ToolName string
	// CallArgs 调用参数（JSON dict 形式，可选）
	CallArgs map[string]any
	// CallResult 调用结果（JSON dict 形式，可选）
	CallResult map[string]any
	// ToolDescription 工具描述（可选）
	ToolDescription string
	// ToolSchema 工具 JSON Schema（可选）
	ToolSchema map[string]any
	// ToolCallID 工具调用 ID，用于脚本产物跟踪（可选）
	ToolCallID string
}

// TrajectoryStep 执行轨迹中的单个步骤。
//
// 字段分类：
//   - 核心执行事实：Kind, Error, StartTimeMs, EndTimeMs
//   - 结构化详情：Detail (LLMCallDetail | ToolCallDetail | nil)
//   - 后注入字段：Reward, PromptTokenIDs, CompletionTokenIDs, Logprobs
//   - 扩展元数据：Meta
//
// 对应 Python: TrajectoryStep dataclass
type TrajectoryStep struct {
	// Kind 步骤类型（llm/tool）
	Kind StepKind
	// Error 错误信息（可选）
	Error map[string]any
	// StartTimeMs 步骤开始时间（毫秒时间戳，可选）
	StartTimeMs int
	// EndTimeMs 步骤结束时间（毫秒时间戳，可选）
	EndTimeMs int
	// Detail 结构化步骤数据（LLMCallDetail 或 ToolCallDetail，可选）
	Detail StepDetail
	// Reward 标量奖励，来自 PRM 或 SignalDetector（可选）
	Reward float64
	// PromptTokenIDs 提示词 token ID 列表，仅 kind=llm（可选）
	PromptTokenIDs []int
	// CompletionTokenIDs 补全 token ID 列表，仅 kind=llm（可选）
	CompletionTokenIDs []int
	// Logprobs token 对数概率，仅 kind=llm（可选）
	Logprobs any
	// Meta 扩展元数据，包含 operator_id、agent_id、invoke 关系等
	Meta map[string]any
}

// Trajectory 完整执行轨迹。
//
// 对应 Python: Trajectory dataclass
type Trajectory struct {
	// ExecutionID 唯一执行标识符
	ExecutionID string
	// Steps 有序执行步骤列表
	Steps []*TrajectoryStep
	// Source 执行来源："online"（deepagents）或 "offline"（trainer）
	Source string
	// CaseID 离线模式下的数据集用例标识（可选）
	CaseID string
	// SessionID 在线模式下的会话 ID（可选）
	SessionID string
	// Cost 聚合成本指标（可选）
	Cost CostInfo
	// Meta 扩展元数据，包含 member_id、member_count 等
	Meta map[string]any
}

// CostInfo 聚合成本指标。
//
// 对应 Python: CostInfo = Dict[str, int]  # {"input_tokens": N, "output_tokens": M}
type CostInfo map[string]int

// ──────────────────────────── 枚举 ────────────────────────────

// StepKind 执行步骤类型。
//
// 对应 Python: StepKind = Literal["llm", "tool"]
type StepKind string

const (
	// StepKindLLM LLM 调用步骤
	StepKindLLM StepKind = "llm"
	// StepKindTool 工具调用步骤
	StepKindTool StepKind = "tool"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// CrossMemberMetaKeys 跨成员元数据键集合。
	// 用于判断 Trajectory 是否处于团队协作成员上下文。
	//
	// 对应 Python: openjiuwen/agent_evolving/trajectory/aggregator.py
	// CROSS_MEMBER_META_KEYS = frozenset({"invoke_id", "parent_invoke_id", "child_invokes"})
	CrossMemberMetaKeys = map[string]bool{
		"invoke_id":        true,
		"parent_invoke_id": true,
		"child_invokes":    true,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// StepKind 返回 StepKindLLM，实现 StepDetail 接口。
func (d *LLMCallDetail) StepKind() StepKind { return StepKindLLM }

// StepKind 返回 StepKindTool，实现 StepDetail 接口。
func (d *ToolCallDetail) StepKind() StepKind { return StepKindTool }

// ToMessages 返回 LLM 步骤中记录的消息字典列表。
//
// 遍历所有 kind=llm 且 detail 为 LLMCallDetail 的步骤，
// 提取 messages 和 response。
// Messages 已是字典形式（外部存入时已转换），直接追加；
// Response 同为字典形式，检查是否含 role 或 content 键后追加。
//
// 对齐 Python:
//
//	for step in self.steps:
//	    if step.kind != "llm" or not isinstance(step.detail, LLMCallDetail):
//	        continue
//	    messages.extend(self._message_to_dict(message) for message in step.detail.messages)
//	    response_message = self._message_to_dict(response) if response is not None else None
//	    if response_message and ("role" in response_message or "content" in response_message):
//	        messages.append(response_message)
//
// 对应 Python: Trajectory.to_messages()
func (t *Trajectory) ToMessages() []map[string]any {
	messages := make([]map[string]any, 0)
	for _, step := range t.Steps {
		if step.Kind != StepKindLLM {
			continue
		}
		llmDetail, ok := step.Detail.(*LLMCallDetail)
		if !ok {
			continue
		}
		// Messages 已是 []map[string]any，直接追加
		messages = append(messages, llmDetail.Messages...)
		// Response 已是 map[string]any，检查是否含 role 或 content 键
		if llmDetail.Response != nil {
			if _, hasRole := llmDetail.Response["role"]; hasRole {
				messages = append(messages, llmDetail.Response)
			} else if _, hasContent := llmDetail.Response["content"]; hasContent {
				messages = append(messages, llmDetail.Response)
			}
		}
	}
	return messages
}
