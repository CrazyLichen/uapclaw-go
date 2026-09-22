package schema

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── FeedbackType ────────────────────────────

func TestFeedbackType_String(t *testing.T) {
	tests := []struct {
		ft   FeedbackType
		want string
	}{
		{FeedbackHelpful, "helpful"},
		{FeedbackHarmful, "harmful"},
		{FeedbackNeutral, "neutral"},
	}
	for _, tt := range tests {
		if got := tt.ft.String(); got != tt.want {
			t.Errorf("FeedbackType(%d).String() = %q, want %q", tt.ft, got, tt.want)
		}
	}
}

func TestFeedbackType_MarshalJSON(t *testing.T) {
	ft := FeedbackHelpful
	data, err := json.Marshal(ft)
	if err != nil {
		t.Fatalf("MarshalJSON 失败: %v", err)
	}
	if string(data) != `"helpful"` {
		t.Errorf("MarshalJSON = %s, want \"helpful\"", data)
	}
}

func TestFeedbackType_UnmarshalJSON(t *testing.T) {
	var ft FeedbackType
	err := json.Unmarshal([]byte(`"harmful"`), &ft)
	if err != nil {
		t.Fatalf("UnmarshalJSON 失败: %v", err)
	}
	if ft != FeedbackHarmful {
		t.Errorf("UnmarshalJSON = %d, want %d", ft, FeedbackHarmful)
	}
}

func TestFeedbackType_UnmarshalJSON_无效值(t *testing.T) {
	var ft FeedbackType
	err := json.Unmarshal([]byte(`"invalid"`), &ft)
	if err == nil {
		t.Error("期望返回 error，得到 nil")
	}
}

// ──────────────────────────── Trajectory ────────────────────────────

func TestTrajectory_IsSuccess(t *testing.T) {
	traj := Trajectory{Feedback: FeedbackHelpful}
	if !traj.IsSuccess() {
		t.Error("IsSuccess() 应返回 true")
	}
	if traj.IsFailure() {
		t.Error("IsFailure() 应返回 false")
	}
}

func TestTrajectory_IsFailure(t *testing.T) {
	traj := Trajectory{Feedback: FeedbackHarmful}
	if traj.IsSuccess() {
		t.Error("IsSuccess() 应返回 false")
	}
	if !traj.IsFailure() {
		t.Error("IsFailure() 应返回 true")
	}
}

func TestTrajectory_IsNeutral(t *testing.T) {
	traj := Trajectory{Feedback: FeedbackNeutral}
	if traj.IsSuccess() {
		t.Error("中性反馈 IsSuccess() 应返回 false")
	}
	if traj.IsFailure() {
		t.Error("中性反馈 IsFailure() 应返回 false")
	}
}

func TestTrajectory_ToDict(t *testing.T) {
	traj := Trajectory{
		Query:    "test query",
		Response: "test response",
		Feedback: FeedbackHelpful,
		Context:  map[string]any{"key": "value"},
	}
	dict := traj.ToDict()
	if dict["query"] != "test query" {
		t.Errorf("ToDict query = %v, want test query", dict["query"])
	}
	if dict["feedback"] != "helpful" {
		t.Errorf("ToDict feedback = %v, want helpful", dict["feedback"])
	}
}

func TestTrajectory_FromDict(t *testing.T) {
	data := map[string]any{
		"query":    "from dict query",
		"response": "from dict response",
		"feedback": "harmful",
		"context":  map[string]any{"env": "python"},
	}
	traj := TrajectoryFromDict(data)
	if traj.Query != "from dict query" {
		t.Errorf("Query = %q, want from dict query", traj.Query)
	}
	if traj.Feedback != FeedbackHarmful {
		t.Errorf("Feedback = %d, want FeedbackHarmful", traj.Feedback)
	}
	if traj.Context["env"] != "python" {
		t.Errorf("Context[env] = %v, want python", traj.Context["env"])
	}
}

func TestTrajectory_FromDict_默认值(t *testing.T) {
	data := map[string]any{
		"query": "q",
	}
	traj := TrajectoryFromDict(data)
	if traj.Feedback != FeedbackNeutral {
		t.Errorf("默认 Feedback = %v, want FeedbackNeutral(%v)", traj.Feedback, FeedbackNeutral)
	}
	if traj.Context == nil {
		t.Error("默认 Context 不应为 nil")
	}
}

func TestTrajectory_ToDict_FromDict_往返(t *testing.T) {
	original := Trajectory{
		Query:    "往返测试",
		Response: "响应内容",
		Feedback: FeedbackHelpful,
		Context:  map[string]any{"k": "v"},
	}
	dict := original.ToDict()
	restored := TrajectoryFromDict(dict)
	if restored.Query != original.Query {
		t.Errorf("Query = %q, want %q", restored.Query, original.Query)
	}
	if restored.Response != original.Response {
		t.Errorf("Response = %q, want %q", restored.Response, original.Response)
	}
	if restored.Feedback != original.Feedback {
		t.Errorf("Feedback = %d, want %d", restored.Feedback, original.Feedback)
	}
}

func TestTrajectory_String(t *testing.T) {
	short := Trajectory{Query: "short", Feedback: FeedbackHelpful}
	if short.String() != "Trajectory(query='short', feedback=helpful)" {
		t.Errorf("String() = %q", short.String())
	}

	long := Trajectory{Query: "this is a very long query that exceeds fifty characters limit for display", Feedback: FeedbackHarmful}
	s := long.String()
	// 验证 query 部分被截断到 50 字符 + "..."
	if len(s) > 120 {
		t.Errorf("String() 过长: %q", s)
	}
}

func TestTrajectory_JSON往返(t *testing.T) {
	original := Trajectory{
		Query:    "如何实现缓存",
		Response: "使用 lru_cache",
		Feedback: FeedbackHelpful,
		Context:  map[string]any{"env": "python"},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored Trajectory
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.Query != original.Query {
		t.Errorf("Query = %q, want %q", restored.Query, original.Query)
	}
	if restored.Feedback != original.Feedback {
		t.Errorf("Feedback = %d, want %d", restored.Feedback, original.Feedback)
	}
}

// ──────────────────────────── TrajectoryBatch ────────────────────────────

func TestTrajectoryBatch_GetSuccessTrajectories(t *testing.T) {
	batch := TrajectoryBatch{
		Trajectories: []Trajectory{
			{Query: "q1", Feedback: FeedbackHelpful},
			{Query: "q2", Feedback: FeedbackHarmful},
			{Query: "q3", Feedback: FeedbackHelpful},
			{Query: "q4", Feedback: FeedbackNeutral},
		},
		UserID: "user1",
	}
	success := batch.GetSuccessTrajectories()
	if len(success) != 2 {
		t.Errorf("GetSuccessTrajectories 返回 %d 条, want 2", len(success))
	}
	failure := batch.GetFailureTrajectories()
	if len(failure) != 1 {
		t.Errorf("GetFailureTrajectories 返回 %d 条, want 1", len(failure))
	}
}

func TestTrajectoryBatch_GetSuccessTrajectories_空批次(t *testing.T) {
	batch := TrajectoryBatch{
		Trajectories: []Trajectory{},
		UserID:       "user1",
	}
	success := batch.GetSuccessTrajectories()
	if len(success) != 0 {
		t.Errorf("空批次 GetSuccessTrajectories 返回 %d 条, want 0", len(success))
	}
}

func TestTrajectoryBatch_CountByFeedback(t *testing.T) {
	batch := TrajectoryBatch{
		Trajectories: []Trajectory{
			{Feedback: FeedbackHelpful},
			{Feedback: FeedbackHelpful},
			{Feedback: FeedbackHarmful},
			{Feedback: FeedbackNeutral},
		},
		UserID: "user1",
	}
	counts := batch.CountByFeedback()
	if counts[FeedbackHelpful] != 2 {
		t.Errorf("Helpful = %d, want 2", counts[FeedbackHelpful])
	}
	if counts[FeedbackHarmful] != 1 {
		t.Errorf("Harmful = %d, want 1", counts[FeedbackHarmful])
	}
	if counts[FeedbackNeutral] != 1 {
		t.Errorf("Neutral = %d, want 1", counts[FeedbackNeutral])
	}
}

func TestTrajectoryBatch_String(t *testing.T) {
	batch := TrajectoryBatch{
		Trajectories: []Trajectory{
			{Feedback: FeedbackHelpful},
			{Feedback: FeedbackHarmful},
		},
		UserID: "user1",
	}
	s := batch.String()
	if s == "" {
		t.Error("String() 不应为空")
	}
}

func TestTrajectoryBatch_JSON往返(t *testing.T) {
	original := TrajectoryBatch{
		Trajectories: []Trajectory{
			{Query: "q1", Response: "r1", Feedback: FeedbackHelpful},
		},
		UserID:   "user1",
		Metadata: map[string]any{"key": "val"},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var restored TrajectoryBatch
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if restored.UserID != "user1" {
		t.Errorf("UserID = %q, want user1", restored.UserID)
	}
	if len(restored.Trajectories) != 1 {
		t.Errorf("Trajectories 长度 = %d, want 1", len(restored.Trajectories))
	}
}
