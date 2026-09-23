package schema

import (
	"encoding/json"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Trajectory 表示一次任务执行的轨迹。
// 对齐 Python Trajectory(BaseModel)。
//
// 轨迹捕获：
//   - 执行的查询/任务
//   - 生成的响应/输出
//   - 结果反馈（有帮助/有害/中性）
type Trajectory struct {
	// Query 执行的查询或任务
	Query string `json:"query"`
	// Response 生成的响应或输出
	Response string `json:"response"`
	// Feedback 结果反馈，默认中性
	Feedback FeedbackType `json:"feedback"`
	// Context 执行的附加上下文
	Context map[string]any `json:"context"`
}

// TrajectoryBatch 轨迹批量数据。
// 对齐 Python TrajectoryBatch(BaseModel)。
type TrajectoryBatch struct {
	// Trajectories 轨迹列表
	Trajectories []Trajectory `json:"trajectories"`
	// UserID 用户标识
	UserID string `json:"user_id"`
	// Metadata 批次元数据
	Metadata map[string]any `json:"metadata"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// FeedbackType 轨迹结果反馈类型。
// 对齐 Python FeedbackType(str, Enum)，JSON 序列化输出字符串值。
type FeedbackType int

const (
	// FeedbackHelpful 有帮助
	FeedbackHelpful FeedbackType = iota
	// FeedbackHarmful 有害
	FeedbackHarmful
	// FeedbackNeutral 中性
	FeedbackNeutral
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// feedbackHelpfulStr JSON 序列化值
	feedbackHelpfulStr = "helpful"
	// feedbackHarmfulStr JSON 序列化值
	feedbackHarmfulStr = "harmful"
	// feedbackNeutralStr JSON 序列化值
	feedbackNeutralStr = "neutral"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TrajectoryFromDict 从字典创建 Trajectory。
// 对齐 Python Trajectory.from_dict(data) classmethod。
func TrajectoryFromDict(data map[string]any) *Trajectory {
	query, _ := data["query"].(string)
	response, _ := data["response"].(string)

	// 对齐 Python default=FeedbackType.NEUTRAL，Go 零值为 FeedbackHelpful(iota=0)，
	// 必须显式设置默认值为 FeedbackNeutral
	feedback := FeedbackNeutral
	if fb, ok := data["feedback"]; ok {
		switch v := fb.(type) {
		case string:
			if parsed, err := feedbackFromString(v); err == nil {
				feedback = parsed
			} else {
				feedback = FeedbackNeutral
			}
		case FeedbackType:
			feedback = v
		default:
			feedback = FeedbackNeutral
		}
	}

	var ctx map[string]any
	if c, ok := data["context"]; ok && c != nil {
		if m, ok := c.(map[string]any); ok {
			ctx = m
		}
	}
	if ctx == nil {
		ctx = make(map[string]any)
	}

	return &Trajectory{
		Query:    query,
		Response: response,
		Feedback: feedback,
		Context:  ctx,
	}
}

// String 实现 Stringer 接口。
// 对齐 Python FeedbackType.__str__()，输出 "helpful"/"harmful"/"neutral"。
func (ft FeedbackType) String() string {
	switch ft {
	case FeedbackHelpful:
		return feedbackHelpfulStr
	case FeedbackHarmful:
		return feedbackHarmfulStr
	case FeedbackNeutral:
		return feedbackNeutralStr
	default:
		return fmt.Sprintf("FeedbackType(%d)", ft)
	}
}

// MarshalJSON 实现 json.Marshaler 接口。
// 对齐 Python ConfigDict(use_enum_values=True)，序列化为字符串。
func (ft FeedbackType) MarshalJSON() ([]byte, error) {
	return json.Marshal(ft.String())
}

// UnmarshalJSON 实现 json.Unmarshaler 接口。
func (ft *FeedbackType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := feedbackFromString(s)
	if err != nil {
		return err
	}
	*ft = parsed
	return nil
}

// IsSuccess 检查轨迹是否成功。
// 对齐 Python Trajectory.is_success()，Feedback == helpful 时返回 true。
func (t *Trajectory) IsSuccess() bool {
	return t.Feedback == FeedbackHelpful
}

// IsFailure 检查轨迹是否失败。
// 对齐 Python Trajectory.is_failure()，Feedback == harmful 时返回 true。
func (t *Trajectory) IsFailure() bool {
	return t.Feedback == FeedbackHarmful
}

// ToDict 转换为字典。
// 对齐 Python Trajectory.to_dict()，即 Pydantic model_dump()。
func (t *Trajectory) ToDict() map[string]any {
	return map[string]any{
		"query":    t.Query,
		"response": t.Response,
		"feedback": t.Feedback.String(),
		"context":  t.Context,
	}
}

// String 实现 Stringer 接口。
// 对齐 Python Trajectory.__repr__()，query 超过 50 字符时截断。
func (t *Trajectory) String() string {
	queryPreview := t.Query
	if len(queryPreview) > 50 {
		queryPreview = queryPreview[:50] + "..."
	}
	return fmt.Sprintf("Trajectory(query='%s', feedback=%s)", queryPreview, t.Feedback)
}

// UnmarshalJSON 实现 json.Unmarshaler 接口。
// 对齐 Python default=FeedbackType.NEUTRAL，当 JSON 缺少 feedback 字段时使用中性默认值。
func (t *Trajectory) UnmarshalJSON(data []byte) error {
	// 使用临时类型避免递归调用 UnmarshalJSON
	type alias Trajectory
	var a alias
	a.Feedback = FeedbackNeutral // 设置默认值，对齐 Python
	a.Context = make(map[string]any)
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*t = Trajectory(a)
	return nil
}

// GetSuccessTrajectories 获取成功的轨迹列表。
// 对齐 Python TrajectoryBatch.get_success_trajectories()。
func (b *TrajectoryBatch) GetSuccessTrajectories() []Trajectory {
	var result []Trajectory
	for i := range b.Trajectories {
		if b.Trajectories[i].IsSuccess() {
			result = append(result, b.Trajectories[i])
		}
	}
	return result
}

// GetFailureTrajectories 获取失败的轨迹列表。
// 对齐 Python TrajectoryBatch.get_failure_trajectories()。
func (b *TrajectoryBatch) GetFailureTrajectories() []Trajectory {
	var result []Trajectory
	for i := range b.Trajectories {
		if b.Trajectories[i].IsFailure() {
			result = append(result, b.Trajectories[i])
		}
	}
	return result
}

// CountByFeedback 按反馈类型统计轨迹数量。
// 对齐 Python TrajectoryBatch.count_by_feedback()，返回按 FeedbackType 键的计数。
func (b *TrajectoryBatch) CountByFeedback() map[FeedbackType]int {
	counts := map[FeedbackType]int{
		FeedbackHelpful: 0,
		FeedbackHarmful: 0,
		FeedbackNeutral: 0,
	}
	for i := range b.Trajectories {
		counts[b.Trajectories[i].Feedback]++
	}
	return counts
}

// String 实现 Stringer 接口。
// 对齐 Python TrajectoryBatch.__repr__()。
func (b *TrajectoryBatch) String() string {
	counts := b.CountByFeedback()
	return fmt.Sprintf(
		"TrajectoryBatch(user=%s, total=%d, helpful=%d, harmful=%d)",
		b.UserID,
		len(b.Trajectories),
		counts[FeedbackHelpful],
		counts[FeedbackHarmful],
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// feedbackFromString 从字符串解析 FeedbackType。
func feedbackFromString(s string) (FeedbackType, error) {
	switch s {
	case feedbackHelpfulStr:
		return FeedbackHelpful, nil
	case feedbackHarmfulStr:
		return FeedbackHarmful, nil
	case feedbackNeutralStr:
		return FeedbackNeutral, nil
	default:
		return FeedbackNeutral, fmt.Errorf("未知的 FeedbackType: %q", s)
	}
}
