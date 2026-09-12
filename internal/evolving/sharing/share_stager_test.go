package sharing

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
)

// ──────────────────────────── 辅助 ────────────────────────────

// newTestShareStager 创建用于测试的 ShareStager 实例。
func newTestShareStager(t *testing.T) (*ShareStager, *ExperienceSharer) {
	t.Helper()
	bk := newMockBackend()
	sharer := NewExperienceSharer(bk, "", 1, 0, nil)
	extractor := NewKeywordExtractor(nil, "", "cn", QUERY_KEYWORDS_LLM_POLICY)
	stager := NewShareStager(extractor, sharer, 0.6, nil)
	return stager, sharer
}

// newStagerRecord 创建测试用 EvolutionRecord。
func newStagerRecord(id, source string, score float64) checkpointing.EvolutionRecord {
	return checkpointing.EvolutionRecord{
		ID:     id,
		Source: source,
		Score:  score,
		Change: checkpointing.EvolutionPatch{
			Section:  "Instructions",
			Action:   "append",
			Content:  "test content",
			Keywords: []string{"test"},
			Summary:  strPtr("test summary"),
		},
	}
}

// strPtr 返回字符串指针。
func strPtr(s string) *string {
	return &s
}

// ──────────────────────────── messagesHasSuccessfulTool 测试 ────────────────────────────

// TestMessagesHasSuccessfulTool_有成功工具 工具内容无失败关键词 → true
func TestMessagesHasSuccessfulTool_有成功工具(t *testing.T) {
	messages := []map[string]any{
		{"role": "tool", "content": "file content here"},
	}
	if !messagesHasSuccessfulTool(messages) {
		t.Fatal("期望 true，实际 false")
	}
}

// TestMessagesHasSuccessfulTool_全是失败 工具内容含失败关键词 → false
func TestMessagesHasSuccessfulTool_全是失败(t *testing.T) {
	messages := []map[string]any{
		{"role": "tool", "content": "Error: something went wrong"},
		{"role": "tool", "content": "failed to execute"},
	}
	if messagesHasSuccessfulTool(messages) {
		t.Fatal("期望 false，实际 true")
	}
}

// TestMessagesHasSuccessfulTool_无消息 nil → false
func TestMessagesHasSuccessfulTool_无消息(t *testing.T) {
	if messagesHasSuccessfulTool(nil) {
		t.Fatal("期望 false，实际 true")
	}
}

// TestMessagesHasSuccessfulTool_空消息列表 → false
func TestMessagesHasSuccessfulTool_空消息列表(t *testing.T) {
	if messagesHasSuccessfulTool([]map[string]any{}) {
		t.Fatal("期望 false，实际 true")
	}
}

// ──────────────────────────── ScreenAndStage 测试 ────────────────────────────

// TestShareStager_ScreenAndStage_正常记录通过 source="user_correction" + score=0.8
func TestShareStager_ScreenAndStage_正常记录通过(t *testing.T) {
	stager, sharer := newTestShareStager(t)
	records := []checkpointing.EvolutionRecord{
		newStagerRecord("ev_001", "user_correction", 0.8),
	}
	result := stager.ScreenAndStage(context.Background(), "test_skill", records, nil)

	if len(result.StagedForShare) != 1 {
		t.Fatalf("期望 1 条通过，实际 %d", len(result.StagedForShare))
	}
	if len(result.DroppedForShare) != 0 {
		t.Fatalf("期望 0 条丢弃，实际 %d", len(result.DroppedForShare))
	}
	if !sharer.HasPending("test_skill") {
		t.Fatal("期望 sharer 有待上传经验")
	}
}

// TestShareStager_ScreenAndStage_ExecutionFailure无成功工具 丢弃
func TestShareStager_ScreenAndStage_ExecutionFailure无成功工具(t *testing.T) {
	stager, _ := newTestShareStager(t)
	records := []checkpointing.EvolutionRecord{
		newStagerRecord("ev_002", "execution_failure", 0.8),
	}
	// 消息中没有成功的工具调用
	messages := []map[string]any{
		{"role": "tool", "content": "Error: command not found"},
	}
	result := stager.ScreenAndStage(context.Background(), "test_skill", records, messages)

	if len(result.StagedForShare) != 0 {
		t.Fatalf("期望 0 条通过，实际 %d", len(result.StagedForShare))
	}
	if len(result.DroppedForShare) != 1 {
		t.Fatalf("期望 1 条丢弃，实际 %d", len(result.DroppedForShare))
	}
	if result.DroppedForShare[0].Reason != "execution failure without successful follow-up tool call" {
		t.Fatalf("丢弃原因不匹配: %s", result.DroppedForShare[0].Reason)
	}
}

// TestShareStager_ScreenAndStage_ExecutionFailure有成功工具 通过
func TestShareStager_ScreenAndStage_ExecutionFailure有成功工具(t *testing.T) {
	stager, _ := newTestShareStager(t)
	records := []checkpointing.EvolutionRecord{
		newStagerRecord("ev_003", "execution_failure", 0.8),
	}
	// 消息中包含成功的工具调用
	messages := []map[string]any{
		{"role": "tool", "content": "Error: command not found"},
		{"role": "tool", "content": "file contents retrieved successfully"},
	}
	result := stager.ScreenAndStage(context.Background(), "test_skill", records, messages)

	if len(result.StagedForShare) != 1 {
		t.Fatalf("期望 1 条通过，实际 %d", len(result.StagedForShare))
	}
	if len(result.DroppedForShare) != 0 {
		t.Fatalf("期望 0 条丢弃，实际 %d", len(result.DroppedForShare))
	}
}

// TestShareStager_ScreenAndStage_ScoreBelowThreshold score=0.3 < 0.6 → 丢弃
func TestShareStager_ScreenAndStage_ScoreBelowThreshold(t *testing.T) {
	stager, _ := newTestShareStager(t)
	records := []checkpointing.EvolutionRecord{
		newStagerRecord("ev_004", "user_correction", 0.3),
	}
	result := stager.ScreenAndStage(context.Background(), "test_skill", records, nil)

	if len(result.StagedForShare) != 0 {
		t.Fatalf("期望 0 条通过，实际 %d", len(result.StagedForShare))
	}
	if len(result.DroppedForShare) != 1 {
		t.Fatalf("期望 1 条丢弃，实际 %d", len(result.DroppedForShare))
	}
	if result.DroppedForShare[0].Reason != "score 0.30 below threshold 0.60" {
		t.Fatalf("丢弃原因不匹配: %s", result.DroppedForShare[0].Reason)
	}
}

// TestShareStager_ScreenAndStage_空记录 返回空 StagingResult
func TestShareStager_ScreenAndStage_空记录(t *testing.T) {
	stager, _ := newTestShareStager(t)
	result := stager.ScreenAndStage(context.Background(), "test_skill", nil, nil)

	if len(result.StagedForShare) != 0 {
		t.Fatalf("期望 0 条通过，实际 %d", len(result.StagedForShare))
	}
	if len(result.DroppedForShare) != 0 {
		t.Fatalf("期望 0 条丢弃，实际 %d", len(result.DroppedForShare))
	}
}

// ──────────────────────────── QCScoreThreshold 测试 ────────────────────────────

// TestNewShareStager_QCScoreThreshold 构造函数传入的阈值可被正确读取
func TestNewShareStager_QCScoreThreshold(t *testing.T) {
	bk := newMockBackend()
	sharer := NewExperienceSharer(bk, "", 1, 0, nil)
	extractor := NewKeywordExtractor(nil, "", "cn", QUERY_KEYWORDS_LLM_POLICY)
	userID := "user_123"
	stager := NewShareStager(extractor, sharer, 0.75, &userID)

	if stager.QCScoreThreshold() != 0.75 {
		t.Fatalf("期望 0.75，实际 %.2f", stager.QCScoreThreshold())
	}
}

// ──────────────────────────── wrap 测试 ────────────────────────────

// TestShareStager_wrap 关键词显式拷贝，修改不影响原始
func TestShareStager_wrap(t *testing.T) {
	stager, _ := newTestShareStager(t)
	record := newStagerRecord("ev_010", "user_correction", 0.9)
	keywords := []string{"k1", "k2"}
	wrapped := stager.wrap(record, keywords, "summary", "skill_a")

	if wrapped.SharingMeta.SkillName != "skill_a" {
		t.Fatalf("SkillName 不匹配: %s", wrapped.SharingMeta.SkillName)
	}
	if wrapped.SharingMeta.Confidence != 0.9 {
		t.Fatalf("Confidence 不匹配: %.2f", wrapped.SharingMeta.Confidence)
	}
	if wrapped.SharingMeta.UploadTrigger != "user_approval" {
		t.Fatalf("UploadTrigger 不匹配: %s", wrapped.SharingMeta.UploadTrigger)
	}
	if len(wrapped.Keywords) != 2 || wrapped.Keywords[0] != "k1" || wrapped.Keywords[1] != "k2" {
		t.Fatalf("Keywords 不匹配: %v", wrapped.Keywords)
	}
	// 修改 copy 后的 keywords 不影响原始
	wrapped.Keywords[0] = "modified"
	if keywords[0] != "k1" {
		t.Fatal("修改 wrapped.Keywords 影响了原始 keywords 切片")
	}
}
