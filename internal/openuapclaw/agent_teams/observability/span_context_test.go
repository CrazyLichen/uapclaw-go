package observability

import "testing"

// TestNextExecutionSubjectRequestNumber_递增 同会话同主体单调递增。
// Python: span_context.py:555-564
func TestNextExecutionSubjectRequestNumber_递增(t *testing.T) {
	ResetTrajectoryContextState()
	if got := NextExecutionSubjectRequestNumber("s1", "m1"); got != 1 {
		t.Fatalf("首个序号应为 1，实际 %d", got)
	}
	if got := NextExecutionSubjectRequestNumber("s1", "m1"); got != 2 {
		t.Fatalf("序号应递增，实际 %d", got)
	}
	if got := NextExecutionSubjectRequestNumber("s1", "m2"); got != 1 {
		t.Fatalf("不同主体独立计数，实际 %d", got)
	}
}

// TestContextWindow_读回与推进 窗口读回、基线判定、delta 生成。
// Python: span_context.py:606-699
func TestContextWindow_读回与推进(t *testing.T) {
	ResetTrajectoryContextState()
	if CurrentContextWindowMessages("s1", "m1") != nil {
		t.Fatal("未提交窗口应返回 nil")
	}
	msgs := []map[string]any{
		{"message_id": "a", "role": "user", "content": "hi"},
		{"message_id": "b", "role": "assistant", "content": "yo"},
	}
	_, seq1, base1, delta1, baseline := AdvanceContextWindow("s1", "m1", "w1", msgs)
	if !baseline || seq1 != 1 || base1 != "" || len(delta1) != 0 {
		t.Fatalf("首窗应为纪元基线 seq=1 无 delta: baseline=%v seq=%d base=%q delta=%v", baseline, seq1, base1, delta1)
	}
	got := CurrentContextWindowMessages("s1", "m1")
	if got == nil || got[0]["message_id"] != "a" || len(got) != 2 {
		t.Fatalf("窗口读回不符: %v", got)
	}
	msgs2 := []map[string]any{
		{"message_id": "b", "role": "assistant", "content": "yo"},
		{"message_id": "c", "role": "user", "content": "next"},
	}
	_, seq2, base2, delta2, _ := AdvanceContextWindow("s1", "m1", "w2", msgs2)
	if seq2 != 2 || base2 != "w1" {
		t.Fatalf("序号与基窗不符: seq=%d base=%q", seq2, base2)
	}
	if len(delta2) != 2 {
		t.Fatalf("应为 remove+insert 两条，实际 %v", delta2)
	}
	if delta2[0]["op"] != "remove" || delta2[1]["op"] != "insert" {
		t.Fatalf("delta 顺序漂移: %v", delta2)
	}
}

// TestNextTrajectorySubjectPosition_共享序号 同键共享序号域。
// Python: span_context.py:702-713
func TestNextTrajectorySubjectPosition_共享序号(t *testing.T) {
	ResetTrajectoryContextState()
	_, s1 := NextTrajectorySubjectPosition("s9", "m1")
	_, s2 := NextTrajectorySubjectPosition("s9", "m1")
	if s2 != s1+1 {
		t.Fatalf("共享序号应递增: %d -> %d", s1, s2)
	}
}
