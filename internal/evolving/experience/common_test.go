package experience

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestMakePendingChange 测试构建暂存变更
func TestMakePendingChange(t *testing.T) {
	t.Run("基本创建", func(t *testing.T) {
		records := []checkpointing.EvolutionRecord{{ID: "ev_001"}}
		p := MakePendingChange("test_skill", records, "", nil, nil, false)
		if p.SkillName != "test_skill" {
			t.Errorf("SkillName = %s, 期望 test_skill", p.SkillName)
		}
		if len(p.Payload) != 1 {
			t.Errorf("Payload 镀度 = %d, 期望 1", len(p.Payload))
		}
		if p.IsSharedRecords {
			t.Errorf("IsSharedRecords = true, 期望 false")
		}
	})
	t.Run("有 requestIDPrefix", func(t *testing.T) {
		records := []checkpointing.EvolutionRecord{{ID: "ev_001"}}
		p := MakePendingChange("test_skill", records, "req_prefix", nil, nil, true)
		if !strings.Contains(p.ChangeID, "req_prefix") {
			t.Errorf("ChangeID = %s, 应包含 req_prefix", p.ChangeID)
		}
		if !p.IsSharedRecords {
			t.Errorf("IsSharedRecords = false, 期望 true")
		}
	})
	t.Run("空 requestIDPrefix", func(t *testing.T) {
		records := []checkpointing.EvolutionRecord{{ID: "ev_001"}}
		p := MakePendingChange("test_skill", records, "", nil, nil, false)
		if strings.Contains(p.ChangeID, "prefix_") {
			t.Errorf("空 prefix 不应拼接")
		}
	})
}

// TestRejectPendingChange 测试拒绝暂存变更
func TestRejectPendingChange(t *testing.T) {
	records := []checkpointing.EvolutionRecord{{ID: "r1"}, {ID: "r2"}, {ID: "r3"}}
	pending := &checkpointing.PendingChange{SkillName: "test_skill", Payload: records}
	result := RejectPendingChange(pending)
	if result.SkillName != "test_skill" {
		t.Errorf("SkillName = %s, 期望 test_skill", result.SkillName)
	}
	if result.RejectedCount != 3 {
		t.Errorf("RejectedCount = %d, 期望 3", result.RejectedCount)
	}
}

// TestGetStrFromAny 测试 map[string]any 字符串提取
func TestGetStrFromAny(t *testing.T) {
	t.Run("字符串存在", func(t *testing.T) {
		m := map[string]any{"key": "value"}
		if got := getStrFromAny(m, "key", "default"); got != "value" {
			t.Errorf("getStrFromAny = %s, 期望 value", got)
		}
	})
	t.Run("key 不存在", func(t *testing.T) {
		m := map[string]any{}
		if got := getStrFromAny(m, "key", "default"); got != "default" {
			t.Errorf("getStrFromAny = %s, 期望 default", got)
		}
	})
	t.Run("非字符串类型", func(t *testing.T) {
		m := map[string]any{"key": 123}
		if got := getStrFromAny(m, "key", "default"); got != "default" {
			t.Errorf("getStrFromAny(非字符串) = %s, 期望 default", got)
		}
	})
}

// TestGetStrSliceFromAny 测试 map[string]any 字符串切片提取
func TestGetStrSliceFromAny(t *testing.T) {
	t.Run("[]any 类型", func(t *testing.T) {
		m := map[string]any{"ids": []any{"a", "b", "c"}}
		got := getStrSliceFromAny(m, "ids")
		if len(got) != 3 || got[0] != "a" {
			t.Errorf("getStrSliceFromAny([]any) = %v, 期望 [a,b,c]", got)
		}
	})
	t.Run("[]string 类型", func(t *testing.T) {
		m := map[string]any{"ids": []string{"x", "y"}}
		got := getStrSliceFromAny(m, "ids")
		if len(got) != 2 || got[0] != "x" {
			t.Errorf("getStrSliceFromAny([]string) = %v, 期望 [x,y]", got)
		}
	})
	t.Run("key 不存在", func(t *testing.T) {
		m := map[string]any{}
		if got := getStrSliceFromAny(m, "ids"); got != nil {
			t.Errorf("getStrSliceFromAny(不存在) = %v, 期望 nil", got)
		}
	})
}

// TestGetFloatPtrFromAny 测试 map[string]any float64 指针提取
func TestGetFloatPtrFromAny(t *testing.T) {
	t.Run("float64 存在", func(t *testing.T) {
		m := map[string]any{"score": 0.85}
		got := getFloatPtrFromAny(m, "score")
		if got == nil || *got != 0.85 {
			t.Errorf("getFloatPtrFromAny = %v, 期望 0.85", got)
		}
	})
	t.Run("key 不存在", func(t *testing.T) {
		m := map[string]any{}
		if got := getFloatPtrFromAny(m, "score"); got != nil {
			t.Errorf("getFloatPtrFromAny(不存在) = %v, 期望 nil", got)
		}
	})
	t.Run("非 float64 类型", func(t *testing.T) {
		m := map[string]any{"score": "bad"}
		if got := getFloatPtrFromAny(m, "score"); got != nil {
			t.Errorf("getFloatPtrFromAny(非float) = %v, 期望 nil", got)
		}
	})
}

// TestGenerateShortID 测试短 ID 生成
func TestGenerateShortID(t *testing.T) {
	id1 := generateShortID()
	id2 := generateShortID()
	if len(id1) != 8 {
		t.Errorf("generateShortID 镀度 = %d, 期望 8", len(id1))
	}
	if id1 == id2 {
		t.Errorf("连续两次 generateShortID 相同: %s", id1)
	}
}

// TestFilterBodyRecords 测试筛选 BODY 类型记录
func TestFilterBodyRecords(t *testing.T) {
	t.Run("混合记录", func(t *testing.T) {
		records := []checkpointing.EvolutionRecord{
			{ID: "desc_1", Change: checkpointing.EvolutionPatch{Target: signal.EvolutionTargetDescription}},
			{ID: "body_1", Change: checkpointing.EvolutionPatch{Target: signal.EvolutionTargetBody}},
			{ID: "script_1", Change: checkpointing.EvolutionPatch{Target: signal.EvolutionTargetScript}},
			{ID: "body_2", Change: checkpointing.EvolutionPatch{Target: signal.EvolutionTargetBody}},
		}
		got := filterBodyRecords(records)
		if len(got) != 2 {
			t.Errorf("filterBodyRecords 镀度 = %d, 期望 2", len(got))
		}
	})
	t.Run("无 BODY 记录", func(t *testing.T) {
		records := []checkpointing.EvolutionRecord{
			{Change: checkpointing.EvolutionPatch{Target: signal.EvolutionTargetDescription}},
		}
		got := filterBodyRecords(records)
		if len(got) != 0 {
			t.Errorf("filterBodyRecords 镀度 = %d, 期望 0", len(got))
		}
	})
}

// TestIsBodyRecord 测试 BODY 类型判断
func TestIsBodyRecord(t *testing.T) {
	t.Run("BODY 类型", func(t *testing.T) {
		r := &checkpointing.EvolutionRecord{Change: checkpointing.EvolutionPatch{Target: signal.EvolutionTargetBody}}
		if !isBodyRecord(r) {
			t.Errorf("isBodyRecord(BODY) = false, 期望 true")
		}
	})
	t.Run("DESCRIPTION 类型", func(t *testing.T) {
		r := &checkpointing.EvolutionRecord{Change: checkpointing.EvolutionPatch{Target: signal.EvolutionTargetDescription}}
		if isBodyRecord(r) {
			t.Errorf("isBodyRecord(DESCRIPTION) = true, 期望 false")
		}
	})
}

// TestCommitPendingChange 使用临时文件系统测试暂存变更提交
func TestCommitPendingChange(t *testing.T) {
	t.Run("changeID 不存在", func(t *testing.T) {
		pendingByID := map[string]*PendingChange{}
		_, err := CommitPendingChange(context.Background(), pendingByID, "nonexistent", nil)
		if err == nil {
			t.Errorf("期望返回错误")
		}
		if !strings.Contains(err.Error(), "不存在") {
			t.Errorf("错误消息 = %s, 应包含 '不存在'", err.Error())
		}
	})
	t.Run("changeType 不匹配", func(t *testing.T) {
		pendingByID := map[string]*PendingChange{
			"cid1": &checkpointing.PendingChange{ChangeType: "bad_type"},
		}
		_, err := CommitPendingChange(context.Background(), pendingByID, "cid1", nil)
		if err == nil {
			t.Errorf("期望返回错误")
		}
		if !strings.Contains(err.Error(), "不匹配") {
			t.Errorf("错误消息 = %s, 应包含 '不匹配'", err.Error())
		}
	})
	t.Run("正常提交", func(t *testing.T) {
		tmpDir := t.TempDir()
		skillDir := filepath.Join(tmpDir, "skills")
		os.MkdirAll(skillDir, 0o755)
		store := checkpointing.NewEvolutionStore([]string{skillDir})

		// 创建技能目录和空的 evolutions.json
		skillPath := filepath.Join(skillDir, "test_skill")
		os.MkdirAll(skillPath, 0o755)
		evoFile := filepath.Join(skillPath, "evolutions.json")
		os.WriteFile(evoFile, []byte(`{"skill_id":"test_skill","version":"1.0.0","updated_at":"","entries":[]}`), 0o644)

		records := []checkpointing.EvolutionRecord{
			{ID: "ev_001", Change: checkpointing.EvolutionPatch{Target: signal.EvolutionTargetBody, Section: "Instructions", Content: "test"}},
		}
		pendingByID := map[string]*PendingChange{
			"cid2": checkpointing.NewPendingChange("test_skill", records, nil, nil),
		}
		pendingByID["cid2"].ChangeType = schema.SkillExperienceEntry

		result, err := CommitPendingChange(context.Background(), pendingByID, "cid2", store)
		if err != nil {
			t.Errorf("CommitPendingChange 失败: %v", err)
		}
		if result.AppliedCount != 1 {
			t.Errorf("AppliedCount = %d, 期望 1", result.AppliedCount)
		}
		if result.PendingCount != 0 {
			t.Errorf("PendingCount = %d, 期望 0", result.PendingCount)
		}
	})
}

// TestFormatEvolutionRecords 测试格式化演进记录
func TestFormatEvolutionRecords(t *testing.T) {
	t.Run("中文", func(t *testing.T) {
		records := []checkpointing.EvolutionRecord{
			{ID: "ev_001", Source: "optimizer", Timestamp: "2025-01-01", Score: 0.75, Change: checkpointing.EvolutionPatch{Section: "Instructions", Content: "新指令"}},
		}
		got := FormatEvolutionRecords(records, "cn")
		if !strings.Contains(got, "经验") {
			t.Errorf("中文模式缺少 '经验'")
		}
	})
	t.Run("英文", func(t *testing.T) {
		records := []checkpointing.EvolutionRecord{
			{ID: "ev_002", Source: "optimizer", Timestamp: "2025-01-01", Score: 0.60, Change: checkpointing.EvolutionPatch{Section: "Examples", Content: "new example"}},
		}
		got := FormatEvolutionRecords(records, "en")
		if !strings.Contains(got, "Experience") {
			t.Errorf("英文模式缺少 'Experience'")
		}
	})
	t.Run("空记录中文", func(t *testing.T) {
		if got := FormatEvolutionRecords(nil, "cn"); got != "（无演进经验）" {
			t.Errorf("空记录中文 = %s", got)
		}
	})
	t.Run("空记录英文", func(t *testing.T) {
		if got := FormatEvolutionRecords(nil, "en"); got != "(no evolution records)" {
			t.Errorf("空记录英文 = %s", got)
		}
	})
	t.Run("空 Section 用问号", func(t *testing.T) {
		records := []checkpointing.EvolutionRecord{
			{ID: "ev_003", Source: "", Timestamp: "2025-01-01", Score: 0.5, Change: checkpointing.EvolutionPatch{Section: "", Content: "test"}},
		}
		got := FormatEvolutionRecords(records, "cn")
		if !strings.Contains(got, "Section: ?") {
			t.Errorf("空 Section 应显示 ?, got=%s", got)
		}
		if !strings.Contains(got, "source: unknown") {
			t.Errorf("空 Source 应显示 unknown")
		}
	})
	t.Run("多条记录", func(t *testing.T) {
		records := []checkpointing.EvolutionRecord{
			{ID: "ev_a", Source: "s1", Timestamp: "t1", Score: 0.5, Change: checkpointing.EvolutionPatch{Section: "Sec1", Content: "C1"}},
			{ID: "ev_b", Source: "s2", Timestamp: "t2", Score: 0.8, Change: checkpointing.EvolutionPatch{Section: "Sec2", Content: "C2"}},
		}
		got := FormatEvolutionRecords(records, "cn")
		if !strings.Contains(got, "\n\n") {
			t.Errorf("多条记录应以双换行分隔")
		}
	})
}

// TestBuildLocalApplyPreview 测试构建本地应用预览
func TestBuildLocalApplyPreview(t *testing.T) {
	t.Run("空结果", func(t *testing.T) {
		preview, err := BuildLocalApplyPreview("test_skill", nil)
		if err != nil {
			t.Fatalf("BuildLocalApplyPreview 返回错误: %v", err)
		}
		if preview.SkillName != "test_skill" {
			t.Errorf("SkillName = %s, 期望 test_skill", preview.SkillName)
		}
		if len(preview.Records) != 0 {
			t.Errorf("Records 镀度 = %d, 期望 0", len(preview.Records))
		}
	})
	t.Run("有应用结果", func(t *testing.T) {
		ct := schema.SkillExperienceEntry
		stage := schema.LocalApplyCompleted
		applyResults := []schema.ApplyResult{
			{
				Applied:        true,
				Records:        []any{checkpointing.EvolutionRecord{ID: "ev_001"}},
				ChangeType:     &ct,
				LifecycleStage: &stage,
			},
		}
		preview, err := BuildLocalApplyPreview("test_skill", applyResults)
		if err != nil {
			t.Fatalf("BuildLocalApplyPreview 返回错误: %v", err)
		}
		if preview.SkillName != "test_skill" {
			t.Errorf("SkillName = %s, 期望 test_skill", preview.SkillName)
		}
		if len(preview.Records) != 1 {
			t.Errorf("Records 镀度 = %d, 期望 1", len(preview.Records))
		}
		if preview.ChangeType != schema.SkillExperienceEntry {
			t.Errorf("ChangeType = %s, 期望 %s", preview.ChangeType, schema.SkillExperienceEntry)
		}
	})
}

// TestGetPreferredSignal 测试获取优先信号
func TestGetPreferredSignal(t *testing.T) {
	t.Run("空信号", func(t *testing.T) {
		ctx := &EvolutionContext{Signals: nil}
		if got := getPreferredSignal(ctx); got != nil {
			t.Errorf("getPreferredSignal(空) = %v, 期望 nil", got)
		}
	})
	t.Run("有 UserIntent 信号", func(t *testing.T) {
		signals := []signal.EvolutionSignal{{SignalType: "tool_failure"}, {SignalType: schema.UserIntentSignal}}
		ctx := &EvolutionContext{Signals: signals}
		got := getPreferredSignal(ctx)
		if got == nil || got.SignalType != schema.UserIntentSignal {
			t.Errorf("优先信号应为 UserIntent")
		}
	})
	t.Run("无 UserIntent 返回首个", func(t *testing.T) {
		signals := []signal.EvolutionSignal{{SignalType: "tool_failure"}, {SignalType: "trajectory_issue"}}
		ctx := &EvolutionContext{Signals: signals}
		got := getPreferredSignal(ctx)
		if got == nil || got.SignalType != "tool_failure" {
			t.Errorf("无 UserIntent 应返回首个")
		}
	})
}

// TestGetSignalType 测试获取信号类型
func TestGetSignalType(t *testing.T) {
	t.Run("有信号", func(t *testing.T) {
		ctx := &EvolutionContext{Signals: []signal.EvolutionSignal{{SignalType: "user_intent"}}}
		if got := getSignalType(ctx); got != "user_intent" {
			t.Errorf("getSignalType = %s, 期望 user_intent", got)
		}
	})
	t.Run("无信号", func(t *testing.T) {
		ctx := &EvolutionContext{Signals: nil}
		if got := getSignalType(ctx); got != "" {
			t.Errorf("getSignalType(无信号) = %s, 期望空", got)
		}
	})
}

// TestGetSignalSource 测试获取信号来源
func TestGetSignalSource(t *testing.T) {
	t.Run("有信号来源", func(t *testing.T) {
		source := "optimizer"
		sig := signal.EvolutionSignal{Context: map[string]any{"source": source}}
		ctx := &EvolutionContext{Signals: []signal.EvolutionSignal{sig}}
		got := getSignalSource(ctx)
		if got != source {
			t.Errorf("getSignalSource = %s, 期望 %s", got, source)
		}
	})
	t.Run("无信号来源", func(t *testing.T) {
		ctx := &EvolutionContext{Signals: nil}
		if got := getSignalSource(ctx); got != "" {
			t.Errorf("getSignalSource(无信号) = %s, 期望空", got)
		}
	})
}
