package external

// 结构化 AGENT_ACTIVITY 进度事件的共享构造器。
//
// worker rail（team_worker_backend）、会话 avatar rail（avatar_session_backend）
// 与外部 harness runtime（external.member_runtime）都发出同一个结构化活动事件，
// 且共用同一个 content 上限；本文件把上限与事件形状收敛在一处，第四个发出者
// 不会再漂移。
//
// 归包偏差 D-15.16-a4：Python 在 workflow/backends/activity_emitter.py，
// Go 全仓唯一消费方是本包 runtime（Python 侧 lazy import 正是为避免 re-enter），
// Go 侧 workflow 包尚未移植（⤴️ swarmflow 章节）；出现第二个消费方时再评估搬移。
// WorkflowProgressEvent 的引擎侧结构 ⤴️ swarmflow，本批以 AgentActivityEvent
// 最小结构承载同一字段集。
//
// 对应 Python 代码：openjiuwen/agent_teams/workflow/backends/activity_emitter.py（71 行）

// ──────────────────────────── 常量 ────────────────────────────

// ActivityContentLimit 工具活动 content 的引擎侧唯一上限。参数与结果全量透传
// ——成员调用列表展示的是真实队友的 tool_call 帧所携带的完整载荷——此上限只是
// 防止病态载荷（数 MB 的命令输出）炸掉事件、落盘的活动行与广播的护栏。
// 截断后的预览保留标记，让它读作"我提前停了"，而不是一段完整文本。
// Python: ACTIVITY_CONTENT_LIMIT
const ActivityContentLimit = 65536

// ActivityTruncatedMarker 截断标记。
// Python: ACTIVITY_TRUNCATED_MARKER
const ActivityTruncatedMarker = "…(truncated)"

// ──────────────────────────── 结构体 ────────────────────────────

// AgentActivityEvent 结构化 AGENT_ACTIVITY 事件（Python WorkflowProgressEvent
// 的 activity 形状投影；kind 恒 agent_activity）。
type AgentActivityEvent struct {
	// Kind 进度种类（恒 "agent_activity"）
	Kind string `json:"kind"`
	// Phase 相位
	Phase string `json:"phase"`
	// Label 标签
	Label string `json:"label"`
	// AgentID 发出活动的成员 id
	AgentID string `json:"agent_id"`
	// CorrelationID 关联 id
	CorrelationID string `json:"correlation_id"`
	// ActivityType 活动种类（tool_call/tool_result）
	ActivityType string `json:"activity_type"`
	// ToolCallID 工具调用 id（配对键）
	ToolCallID string `json:"tool_call_id"`
	// ToolName 工具名
	ToolName *string `json:"tool_name"`
	// Content 预览（引擎侧唯一截断点）
	Content *string `json:"content"`
	// Message 遗留消费者的文案
	Message string `json:"message"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TruncateActivityContent 把活动预览截到引擎上限并标记被截断。
// Python: truncate_activity_content
func TruncateActivityContent(content *string) *string {
	if content == nil {
		return nil
	}
	if len([]rune(*content)) <= ActivityContentLimit {
		return content
	}
	cut := string([]rune(*content)[:ActivityContentLimit]) + ActivityTruncatedMarker
	return &cut
}

// BuildActivityEvent 从工具对半边构造一条结构化 AGENT_ACTIVITY 事件。
// content 在此截断——这是所有消费方可依赖的唯一引擎侧上限；
// message 为遗留消费者保留。
// Python: build_activity_event
func BuildActivityEvent(
	activityType string,
	toolCallID string,
	toolName, content *string,
	agentID, phase, label, correlationID string,
) *AgentActivityEvent {
	name := ""
	if toolName != nil {
		name = *toolName
	}
	return &AgentActivityEvent{
		Kind:          "agent_activity",
		Phase:         phase,
		Label:         label,
		AgentID:       agentID,
		CorrelationID: correlationID,
		ActivityType:  activityType,
		ToolCallID:    toolCallID,
		ToolName:      toolName,
		Content:       TruncateActivityContent(content),
		Message:       "tool: " + name,
	}
}
