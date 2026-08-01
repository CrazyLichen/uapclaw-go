package experience

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestExperienceProposal_RecordCount 测试 RecordCount 方法
func TestExperienceProposal_RecordCount(t *testing.T) {
	t.Run("有记录", func(t *testing.T) {
		p := ExperienceProposal{
			Records: []checkpointing.EvolutionRecord{
				{ID: "ev_001"}, {ID: "ev_002"}, {ID: "ev_003"},
			},
		}
		if p.RecordCount() != 3 {
			t.Errorf("RecordCount() = %d, 期望 3", p.RecordCount())
		}
	})
	t.Run("无记录", func(t *testing.T) {
		p := ExperienceProposal{Records: nil}
		if p.RecordCount() != 0 {
			t.Errorf("RecordCount() = %d, 期望 0", p.RecordCount())
		}
	})
}

// TestExperienceApplyResult_Ok 测试 Ok 方法
func TestExperienceApplyResult_Ok(t *testing.T) {
	t.Run("全部成功", func(t *testing.T) {
		r := ExperienceApplyResult{AppliedCount: 3, PendingCount: 0, Errors: nil}
		if !r.Ok() {
			t.Errorf("Ok() = false, 期望 true")
		}
	})
	t.Run("有错误", func(t *testing.T) {
		r := ExperienceApplyResult{AppliedCount: 2, Errors: []string{"append failed"}}
		if r.Ok() {
			t.Errorf("Ok() = true, 期望 false（有错误）")
		}
	})
	t.Run("有待定", func(t *testing.T) {
		r := ExperienceApplyResult{AppliedCount: 2, PendingCount: 1, Errors: nil}
		if r.Ok() {
			t.Errorf("Ok() = true, 期望 false（有待定）")
		}
	})
}

// TestExperienceApprovalRequest_ToHostResult 测试审批请求的 ToHostResult
func TestExperienceApprovalRequest_ToHostResult(t *testing.T) {
	t.Run("有 PendingChange", func(t *testing.T) {
		pending := &checkpointing.PendingChange{
			ChangeType: schema.SkillExperienceEntry,
			Payload:    []checkpointing.EvolutionRecord{{ID: "r1"}, {ID: "r2"}},
		}
		reqID := "req_001"
		r := ExperienceApprovalRequest{SkillName: "test_skill", PendingChange: pending, RequestID: &reqID}
		result := r.ToHostResult()
		if result.SkillName != "test_skill" {
			t.Errorf("SkillName = %s, 期望 test_skill", result.SkillName)
		}
		if result.ChangeType != schema.SkillExperienceEntry {
			t.Errorf("ChangeType = %s, 期望 %s", result.ChangeType, schema.SkillExperienceEntry)
		}
		if result.PendingCount != 2 {
			t.Errorf("PendingCount = %d, 期望 2", result.PendingCount)
		}
		if result.Effect != schema.PendingChangeEffect {
			t.Errorf("Effect = %s, 期望 %s", result.Effect, schema.PendingChangeEffect)
		}
		if result.Status != "pending_approval" {
			t.Errorf("Status = %s, 期望 pending_approval", result.Status)
		}
	})
	t.Run("无 PendingChange", func(t *testing.T) {
		reqID := "req_002"
		r := ExperienceApprovalRequest{SkillName: "test_skill", PendingChange: nil, RequestID: &reqID}
		result := r.ToHostResult()
		if result.PendingCount != 0 {
			t.Errorf("PendingCount = %d, 期望 0", result.PendingCount)
		}
	})
	t.Run("无 RequestID", func(t *testing.T) {
		pending := &checkpointing.PendingChange{ChangeType: schema.SkillExperienceEntry, Payload: []checkpointing.EvolutionRecord{{ID: "r1"}}}
		r := ExperienceApprovalRequest{SkillName: "test_skill", PendingChange: pending, RequestID: nil}
		result := r.ToHostResult()
		if result.RequestID != nil && *result.RequestID != "" {
			t.Errorf("RequestID 应为空字符串")
		}
	})
}

// TestExperienceApplyResult_ToHostResult 测试应用结果的 ToHostResult
func TestExperienceApplyResult_ToHostResult(t *testing.T) {
	t.Run("全部通过", func(t *testing.T) {
		reqID := "req_003"
		r := ExperienceApplyResult{SkillName: "test_skill", AppliedCount: 5, PendingCount: 0, RejectedCount: 0}
		result := r.ToHostResult(&reqID, schema.SkillExperienceEntry)
		if result.Status != "persisted" {
			t.Errorf("Status = %s, 期望 persisted", result.Status)
		}
		if result.AppliedCount != 5 {
			t.Errorf("AppliedCount = %d, 期望 5", result.AppliedCount)
		}
	})
	t.Run("部分通过", func(t *testing.T) {
		reqID := "req_004"
		r := ExperienceApplyResult{SkillName: "test_skill", AppliedCount: 3, PendingCount: 2, RejectedCount: 0, Errors: nil}
		result := r.ToHostResult(&reqID, schema.SkillExperienceEntry)
		if result.Status != "partial" {
			t.Errorf("Status = %s, 期望 partial（有 pending）", result.Status)
		}
	})
	t.Run("有拒绝", func(t *testing.T) {
		reqID := "req_005"
		r := ExperienceApplyResult{SkillName: "test_skill", RejectedCount: 3}
		result := r.ToHostResult(&reqID, schema.SkillExperienceEntry)
		if result.Status != "rejected" {
			t.Errorf("Status = %s, 期望 rejected", result.Status)
		}
		if result.RejectedCount != 3 {
			t.Errorf("RejectedCount = %d, 期望 3", result.RejectedCount)
		}
	})
}

// TestOnlineEvolutionStatus 测试状态常量值
func TestOnlineEvolutionStatus(t *testing.T) {
	tests := []struct {
		name   string
		status OnlineEvolutionStatus
		expect string
	}{
		{"Staged", OnlineEvolutionStatusStaged, "staged"},
		{"AutoApproved", OnlineEvolutionStatusAutoApproved, "auto_approved"},
		{"NoEvolutionNoRecords", OnlineEvolutionStatusNoEvolutionNoRecords, "no_evolution_no_records"},
		{"SkippedNoInput", OnlineEvolutionStatusSkippedNoInput, "skipped_no_input"},
		{"SkippedSkillNotFound", OnlineEvolutionStatusSkippedSkillNotFound, "skipped_skill_not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.status != tt.expect {
				t.Errorf("状态 %s = %s, 期望 %s", tt.name, tt.status, tt.expect)
			}
		})
	}
}

// TestHostFacingExperienceResult_PendingApproval 测试 pending_approval 工厂方法
func TestHostFacingExperienceResult_PendingApproval(t *testing.T) {
	result := HostFacingExperienceResultPendingApproval("test_skill", "req_001", schema.SkillExperienceEntry, 3)
	if result.SkillName != "test_skill" {
		t.Errorf("SkillName = %s, 期望 test_skill", result.SkillName)
	}
	if result.Effect != schema.PendingChangeEffect {
		t.Errorf("Effect = %s, 期望 %s", result.Effect, schema.PendingChangeEffect)
	}
	if result.ChangeType != schema.SkillExperienceEntry {
		t.Errorf("ChangeType = %s, 期望 %s", result.ChangeType, schema.SkillExperienceEntry)
	}
	if result.PendingCount != 3 {
		t.Errorf("PendingCount = %d, 期望 3", result.PendingCount)
	}
	if result.Status != "pending_approval" {
		t.Errorf("Status = %s, 期望 pending_approval", result.Status)
	}
}

// TestHostFacingExperienceResult_Persisted 测试 persisted 工厂方法
func TestHostFacingExperienceResult_Persisted(t *testing.T) {
	t.Run("全部成功", func(t *testing.T) {
		reqID := "req_002"
		result := HostFacingExperienceResultPersisted("test_skill", &reqID, schema.SkillExperienceEntry, 5, 0, nil)
		if result.Status != "persisted" {
			t.Errorf("Status = %s, 期望 persisted", result.Status)
		}
		if result.AppliedCount != 5 {
			t.Errorf("AppliedCount = %d, 期望 5", result.AppliedCount)
		}
		if result.Effect != schema.StateEffect {
			t.Errorf("Effect = %s, 期望 %s", result.Effect, schema.StateEffect)
		}
	})
	t.Run("部分成功", func(t *testing.T) {
		reqID := "req_003"
		result := HostFacingExperienceResultPersisted("test_skill", &reqID, schema.SkillExperienceEntry, 3, 2, []string{"err1"})
		if result.Status != "partial" {
			t.Errorf("Status = %s, 期望 partial（有 pending+errors）", result.Status)
		}
	})
	t.Run("nil errors 变空列表", func(t *testing.T) {
		reqID := "req_006"
		result := HostFacingExperienceResultPersisted("test_skill", &reqID, schema.SkillExperienceEntry, 2, 0, nil)
		if result.Errors == nil {
			t.Errorf("Errors 应为非 nil 空列表")
		}
		if len(result.Errors) != 0 {
			t.Errorf("Errors 镀度 = %d, 期望 0", len(result.Errors))
		}
	})
}

// TestHostFacingExperienceResult_Rejected 测试 rejected 工厂方法
func TestHostFacingExperienceResult_Rejected(t *testing.T) {
	reqID := "req_004"
	result := HostFacingExperienceResultRejected("test_skill", &reqID, schema.SkillExperienceEntry, 3)
	if result.Status != "rejected" {
		t.Errorf("Status = %s, 期望 rejected", result.Status)
	}
	if result.RejectedCount != 3 {
		t.Errorf("RejectedCount = %d, 期望 3", result.RejectedCount)
	}
	if result.Effect != schema.StateEffect {
		t.Errorf("Effect = %s, 期望 %s", result.Effect, schema.StateEffect)
	}
}

// TestStrPtr 测试 strPtr 辅助函数
func TestStrPtr(t *testing.T) {
	s := "hello"
	p := strPtr(s)
	if p == nil || *p != "hello" {
		t.Errorf("strPtr(\"hello\") = %v, 期望 *\"hello\"", p)
	}
}

// TestCalcEffectiveness 测试贝叶斯平滑 Effectiveness
func TestCalcEffectiveness(t *testing.T) {
	t.Run("nil stats", func(t *testing.T) {
		if got := CalcEffectiveness(nil); got != 0.5 {
			t.Errorf("CalcEffectiveness(nil) = %f, 期望 0.5", got)
		}
	})
	t.Run("零值 stats", func(t *testing.T) {
		stats := &checkpointing.UsageStats{}
		if got := CalcEffectiveness(stats); got != 0.5 {
			t.Errorf("CalcEffectiveness(零值) = %f, 期望 0.5（贝叶斯平滑）", got)
		}
	})
	t.Run("3正2负", func(t *testing.T) {
		stats := &checkpointing.UsageStats{TimesPositive: 3, TimesNegative: 2}
		got := CalcEffectiveness(stats)
		expect := float64(3+1) / float64(5+2)
		if got != expect {
			t.Errorf("CalcEffectiveness(3正2负) = %f, 期望 %f", got, expect)
		}
	})
	t.Run("10正0负", func(t *testing.T) {
		stats := &checkpointing.UsageStats{TimesPositive: 10, TimesNegative: 0}
		got := CalcEffectiveness(stats)
		expect := float64(10+1) / float64(10+2)
		if got != expect {
			t.Errorf("CalcEffectiveness(10正0负) = %f, 期望 %f", got, expect)
		}
	})
}

// TestCalcUtilization 测试 Utilization 评分
func TestCalcUtilization(t *testing.T) {
	t.Run("nil stats", func(t *testing.T) {
		if got := CalcUtilization(nil); got != 0.5 {
			t.Errorf("CalcUtilization(nil) = %f, 期望 0.5", got)
		}
	})
	t.Run("零值 TimesPresented", func(t *testing.T) {
		stats := &checkpointing.UsageStats{}
		if got := CalcUtilization(stats); got != 0.5 {
			t.Errorf("CalcUtilization(零值) = %f, 期望 0.5", got)
		}
	})
	t.Run("5展示3使用", func(t *testing.T) {
		stats := &checkpointing.UsageStats{TimesPresented: 5, TimesUsed: 3}
		got := CalcUtilization(stats)
		expect := float64(3) / float64(5)
		if got != expect {
			t.Errorf("CalcUtilization(5展示3使用) = %f, 期望 %f", got, expect)
		}
	})
}

// TestCalcFreshness 测试 Freshness 评分
func TestCalcFreshness(t *testing.T) {
	t.Run("空时间戳", func(t *testing.T) {
		record := &checkpointing.EvolutionRecord{Timestamp: ""}
		if got := CalcFreshness(record, nil); got != 0.5 {
			t.Errorf("CalcFreshness(空时间戳) = %f, 期望 0.5", got)
		}
	})
	t.Run("无效时间戳", func(t *testing.T) {
		record := &checkpointing.EvolutionRecord{Timestamp: "not-a-date"}
		if got := CalcFreshness(record, nil); got != 0.5 {
			t.Errorf("CalcFreshness(无效时间戳) = %f, 期望 0.5", got)
		}
	})
	t.Run("近期记录", func(t *testing.T) {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		record := &checkpointing.EvolutionRecord{Timestamp: now}
		got := CalcFreshness(record, nil)
		if got < 0.9 || got > 1.0 {
			t.Errorf("CalcFreshness(近期) = %f, 期望接近 1.0", got)
		}
	})
	t.Run("版本不匹配惩罚", func(t *testing.T) {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		sv := "v2"
		record := &checkpointing.EvolutionRecord{Timestamp: now, SkillVersion: &sv}
		current := "v3"
		got := CalcFreshness(record, &current)
		noMatch := CalcFreshness(&checkpointing.EvolutionRecord{Timestamp: now, SkillVersion: nil}, nil)
		expect := noMatch * StaleVersionPenalty
		if got != expect {
			t.Errorf("CalcFreshness(版本不匹配) = %f, 期望 %f", got, expect)
		}
	})
	t.Run("版本匹配不惩罚", func(t *testing.T) {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		sv := "v1"
		record := &checkpointing.EvolutionRecord{Timestamp: now, SkillVersion: &sv}
		current := "v1"
		got := CalcFreshness(record, &current)
		noVersion := CalcFreshness(&checkpointing.EvolutionRecord{Timestamp: now, SkillVersion: nil}, nil)
		if got != noVersion {
			t.Errorf("版本匹配不应惩罚: got=%f, noVersion=%f", got, noVersion)
		}
	})
}

// TestCalcScore 测试综合评分 WE*e + WU*u + WF*f
func TestCalcScore(t *testing.T) {
	t.Run("nil UsageStats 初始化为零值", func(t *testing.T) {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		record := &checkpointing.EvolutionRecord{Timestamp: now, UsageStats: nil}
		got := CalcScore(record, nil)
		e := CalcEffectiveness(&checkpointing.UsageStats{})
		u := CalcUtilization(&checkpointing.UsageStats{})
		f := CalcFreshness(record, nil)
		expect := WE*e + WU*u + WF*f
		if got != expect {
			t.Errorf("CalcScore(nil stats) = %f, 期望 %f", got, expect)
		}
	})
	t.Run("完整评分", func(t *testing.T) {
		stats := &checkpointing.UsageStats{TimesPositive: 5, TimesNegative: 1, TimesPresented: 10, TimesUsed: 7}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		record := &checkpointing.EvolutionRecord{Timestamp: now, UsageStats: stats}
		got := CalcScore(record, nil)
		e := CalcEffectiveness(stats)
		u := CalcUtilization(stats)
		f := CalcFreshness(record, nil)
		expect := WE*e + WU*u + WF*f
		if got != expect {
			t.Errorf("CalcScore = %f, 期望 %f", got, expect)
		}
	})
}

// TestUpdateScore 测试评分更新
func TestUpdateScore(t *testing.T) {
	t.Run("nil UsageStats 初始化", func(t *testing.T) {
		record := &checkpointing.EvolutionRecord{UsageStats: nil}
		evalResult := map[string]any{"used": true, "positive": true, "negative": false}
		got := UpdateScore(record, evalResult, nil)
		if record.UsageStats == nil {
			t.Errorf("UsageStats 应被初始化")
		}
		if record.UsageStats.TimesUsed != 1 {
			t.Errorf("TimesUsed = %d, 期望 1", record.UsageStats.TimesUsed)
		}
		if record.UsageStats.TimesPositive != 1 {
			t.Errorf("TimesPositive = %d, 期望 1", record.UsageStats.TimesPositive)
		}
		if record.UsageStats.TimesNegative != 0 {
			t.Errorf("TimesNegative = %d, 期望 0", record.UsageStats.TimesNegative)
		}
		if record.UsageStats.LastEvaluatedAt == nil {
			t.Errorf("LastEvaluatedAt 应被设置")
		}
		if got <= 0 {
			t.Errorf("UpdateScore 返回值 = %f, 应大于 0", got)
		}
	})
	t.Run("全部 false", func(t *testing.T) {
		stats := &checkpointing.UsageStats{TimesUsed: 5, TimesPositive: 3, TimesNegative: 2}
		record := &checkpointing.EvolutionRecord{UsageStats: stats}
		evalResult := map[string]any{"used": false, "positive": false, "negative": false}
		got := UpdateScore(record, evalResult, nil)
		if record.UsageStats.TimesUsed != 5 {
			t.Errorf("TimesUsed = %d, 期望 5（不增长）", record.UsageStats.TimesUsed)
		}
		if got <= 0 {
			t.Errorf("UpdateScore 返回值 = %f, 应大于 0", got)
		}
	})
}

// TestParseLLMJSON 测试 LLM JSON 解析
func TestParseLLMJSON(t *testing.T) {
	t.Run("空字符串", func(t *testing.T) {
		if got := parseLLMJSON(""); got != nil {
			t.Errorf("parseLLMJSON('') = %v, 期望 nil", got)
		}
	})
	t.Run("空白字符串", func(t *testing.T) {
		if got := parseLLMJSON("   "); got != nil {
			t.Errorf("parseLLMJSON('   ') = %v, 期望 nil", got)
		}
	})
	t.Run("标准 JSON 数组", func(t *testing.T) {
		input := `[{"record_id": "r1", "used": true, "positive": true, "negative": false}]`
		got := parseLLMJSON(input)
		if len(got) != 1 {
			t.Errorf("parseLLMJSON 长度 = %d, 期望 1", len(got))
		}
		if got[0]["record_id"] != "r1" {
			t.Errorf("record_id = %v, 期望 r1", got[0]["record_id"])
		}
	})
	t.Run("带 markdown code block", func(t *testing.T) {
		input := "```json\n[{\"record_id\": \"r2\", \"used\": false}]\n```"
		got := parseLLMJSON(input)
		if len(got) != 1 {
			t.Errorf("parseLLMJSON(code block) 长度 = %d, 期望 1", len(got))
		}
	})
	t.Run("单个 JSON 对象", func(t *testing.T) {
		input := `{"record_id": "r3", "used": true}`
		got := parseLLMJSON(input)
		if len(got) != 1 {
			t.Errorf("parseLLMJSON(单对象) 长度 = %d, 期望 1", len(got))
		}
	})
	t.Run("尾逗号", func(t *testing.T) {
		input := `[{"record_id": "r4",},]`
		got := parseLLMJSON(input)
		if len(got) != 1 {
			t.Errorf("parseLLMJSON(尾逗号) 长度 = %d, 期望 1", len(got))
		}
	})
	t.Run("无效 JSON", func(t *testing.T) {
		input := "this is not json"
		if got := parseLLMJSON(input); got != nil {
			t.Errorf("parseLLMJSON(无效) = %v, 期望 nil", got)
		}
	})
	t.Run("混合类型数组（非 map 元素被忽略）", func(t *testing.T) {
		input := `[42, {"record_id": "r6"}, "hello"]`
		got := parseLLMJSON(input)
		if len(got) != 1 {
			t.Errorf("parseLLMJSON(混合) 镀度 = %d, 期望 1", len(got))
		}
	})
	t.Run("regexp 提取嵌套数组", func(t *testing.T) {
		input := "some text before [{\"id\": \"r7\"}] some text after"
		got := parseLLMJSON(input)
		if len(got) != 1 {
			t.Errorf("parseLLMJSON(嵌套) 镀度 = %d, 期望 1", len(got))
		}
	})
}

// TestFormatPresentedExperiences 测试格式化展示经验
func TestFormatPresentedExperiences(t *testing.T) {
	t.Run("空列表", func(t *testing.T) {
		if got := formatPresentedExperiences(nil); got != "" {
			t.Errorf("formatPresentedExperiences(nil) = %s, 期望空", got)
		}
	})
	t.Run("多条记录", func(t *testing.T) {
		records := []checkpointing.EvolutionRecord{
			{ID: "ev_001", Change: checkpointing.EvolutionPatch{Content: "内容1"}},
			{ID: "ev_002", Change: checkpointing.EvolutionPatch{Content: "内容2"}},
		}
		got := formatPresentedExperiences(records)
		if !strings.Contains(got, "[ev_001]") {
			t.Errorf("缺少 [ev_001]")
		}
		if !strings.Contains(got, "内容1") {
			t.Errorf("缺少 内容1")
		}
	})
}

// TestFormatScoredExperiences 测试格式化评分经验
func TestFormatScoredExperiences(t *testing.T) {
	t.Run("nil UsageStats 用零值", func(t *testing.T) {
		records := []checkpointing.EvolutionRecord{
			{ID: "ev_001", Score: 0.75, UsageStats: nil, Change: checkpointing.EvolutionPatch{Content: "简短"}},
		}
		got := formatScoredExperiences(records)
		if !strings.Contains(got, "score=0.75") {
			t.Errorf("缺少 score=0.75")
		}
		if !strings.Contains(got, "presented=0") {
			t.Errorf("缺少 presented=0（零值）")
		}
	})
}

// TestTruncateString 测试字符串截断
func TestTruncateString(t *testing.T) {
	t.Run("不需要截断", func(t *testing.T) {
		if got := truncateString("hello", 10); got != "hello" {
			t.Errorf("truncateString('hello', 10) = %s, 期望 hello", got)
		}
	})
	t.Run("需要截断", func(t *testing.T) {
		long := "abcdefghijklmnop"
		if got := truncateString(long, 5); got != "abcde" {
			t.Errorf("truncateString(long, 5) = %s, 期望 abcde", got)
		}
	})
	t.Run("恰好等于", func(t *testing.T) {
		if got := truncateString("hello", 5); got != "hello" {
			t.Errorf("truncateString('hello', 5) = %s, 期望 hello", got)
		}
	})
}

// TestGetBoolFromEvalResult 测试布尔值提取
func TestGetBoolFromEvalResult(t *testing.T) {
	t.Run("bool true", func(t *testing.T) {
		m := map[string]any{"used": true}
		if !getBoolFromEvalResult(m, "used") {
			t.Errorf("期望 true")
		}
	})
	t.Run("bool false", func(t *testing.T) {
		m := map[string]any{"used": false}
		if getBoolFromEvalResult(m, "used") {
			t.Errorf("期望 false")
		}
	})
	t.Run("string true", func(t *testing.T) {
		m := map[string]any{"used": "true"}
		if !getBoolFromEvalResult(m, "used") {
			t.Errorf("期望 true（字符串）")
		}
	})
	t.Run("string True", func(t *testing.T) {
		m := map[string]any{"used": "True"}
		if !getBoolFromEvalResult(m, "used") {
			t.Errorf("期望 true（字符串 True）")
		}
	})
	t.Run("key 不存在", func(t *testing.T) {
		m := map[string]any{}
		if getBoolFromEvalResult(m, "used") {
			t.Errorf("期望 false（不存在）")
		}
	})
	t.Run("非 bool 非 string", func(t *testing.T) {
		m := map[string]any{"used": 123}
		if getBoolFromEvalResult(m, "used") {
			t.Errorf("期望 false（非 bool/string）")
		}
	})
}

// TestMakePendingChange 测试构建暂存变更
func TestMakePendingChange(t *testing.T) {
	t.Run("基本创建", func(t *testing.T) {
		records := []checkpointing.EvolutionRecord{{ID: "ev_001"}}
		p := MakePendingChange("test_skill", records, nil, nil, nil, false)
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
		prefix := "req_prefix"
		records := []checkpointing.EvolutionRecord{{ID: "ev_001"}}
		p := MakePendingChange("test_skill", records, &prefix, nil, nil, true)
		if !strings.Contains(p.ChangeID, "req_prefix") {
			t.Errorf("ChangeID = %s, 应包含 req_prefix", p.ChangeID)
		}
		if !p.IsSharedRecords {
			t.Errorf("IsSharedRecords = false, 期望 true")
		}
	})
	t.Run("空 requestIDPrefix", func(t *testing.T) {
		prefix := ""
		records := []checkpointing.EvolutionRecord{{ID: "ev_001"}}
		p := MakePendingChange("test_skill", records, &prefix, nil, nil, false)
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

// TestExperienceTracker_ConsumeEvalState 测试评估状态消费
func TestExperienceTracker_ConsumeEvalState(t *testing.T) {
	t.Run("达到间隔返回记录", func(t *testing.T) {
		sessionPresentedRecords["test_sess1"] = []PresentedRecordEntry{{SkillName: "skill1", Snippet: "snippet1"}}
		sessionEvalCounter["test_sess1"] = 2
		tracker := &ExperienceTracker{evalInterval: 3}
		result := tracker.ConsumeEvalState("test_sess1")
		if len(result) != 1 {
			t.Errorf("ConsumeEvalState 镀度 = %d, 期望 1", len(result))
		}
		if sessionEvalCounter["test_sess1"] != 0 {
			t.Errorf("counter 应被重置为 0, 实际 %d", sessionEvalCounter["test_sess1"])
		}
		delete(sessionPresentedRecords, "test_sess1")
		delete(sessionEvalCounter, "test_sess1")
	})
	t.Run("未达间隔返回空", func(t *testing.T) {
		sessionEvalCounter["test_sess2"] = 0
		tracker := &ExperienceTracker{evalInterval: 3}
		result := tracker.ConsumeEvalState("test_sess2")
		if len(result) != 0 {
			t.Errorf("ConsumeEvalState 镀度 = %d, 期望 0", len(result))
		}
		if sessionEvalCounter["test_sess2"] != 1 {
			t.Errorf("counter 应为 1, 实际 %d", sessionEvalCounter["test_sess2"])
		}
		delete(sessionEvalCounter, "test_sess2")
	})
}

// TestExperienceTracker_ClearSession 测试会话清理
func TestExperienceTracker_ClearSession(t *testing.T) {
	sessionPresentedRecords["test_sess3"] = []PresentedRecordEntry{{SkillName: "skill1"}}
	sessionEvalCounter["test_sess3"] = 5
	tracker := &ExperienceTracker{}
	tracker.ClearSession("test_sess3")
	if sessionPresentedRecords["test_sess3"] != nil {
		t.Errorf("sessionPresentedRecords 应被清理")
	}
	if sessionEvalCounter["test_sess3"] != 0 {
		t.Errorf("sessionEvalCounter 应为 0")
	}
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
		preview := BuildLocalApplyPreview("test_skill", nil)
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
		preview := BuildLocalApplyPreview("test_skill", applyResults)
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

// TestSetApplyUpdatesFn 测试包级注入函数设置
func TestSetApplyUpdatesFn(t *testing.T) {
	origFn := applyUpdatesFn
	defer func() { applyUpdatesFn = origFn }()

	t.Run("设置和调用", func(t *testing.T) {
		called := false
		SetApplyUpdatesFn(func(_ map[string]operator.Operator, _ map[schema.UpdateKey]any) []schema.ApplyResult {
			called = true
			return nil
		})
		result := evolvingExecuteUpdates(nil, nil)
		if !called {
			t.Errorf("注入函数未被调用")
		}
		if result != nil {
			t.Errorf("期望 nil 结果")
		}
	})
}

// TestDefaultExecuteUpdates 测试默认更新执行逻辑
func TestDefaultExecuteUpdates(t *testing.T) {
	t.Run("nil updates 全部生成错误", func(t *testing.T) {
		updates := map[schema.UpdateKey]any{
			schema.UpdateKey{"op1", schema.ExperiencesTarget}: nil,
		}
		results := defaultExecuteUpdates(nil, updates)
		if len(results) != 1 {
			t.Errorf("镀度 = %d, 期望 1", len(results))
		}
		if results[0].Applied {
			t.Errorf("nil 更新不应 applied")
		}
		if !strings.Contains(results[0].Errors[0], "update value is nil") {
			t.Errorf("错误消息 = %s, 应包含 nil", results[0].Errors[0])
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
		store := checkpointing.NewEvolutionStore(skillDir, sys_operation.SysOperation(nil))

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

// TestNewExperienceManager 测试 ExperienceManager 创建
func TestNewExperienceManager(t *testing.T) {
	t.Run("不支持的 kind", func(t *testing.T) {
		_, err := NewExperienceManager(nil, nil, "bad_kind", "cn", nil, nil, nil)
		if err == nil {
			t.Errorf("期望返回错误")
		}
		if !strings.Contains(err.Error(), "unsupported") {
			t.Errorf("错误消息 = %s, 应包含 'unsupported'", err.Error())
		}
	})
	t.Run("skill kind", func(t *testing.T) {
		mgr, err := NewExperienceManager(nil, nil, "skill", "cn", nil, nil, nil)
		if err != nil {
			t.Errorf("NewExperienceManager 失败: %v", err)
		}
		if mgr.kind != "skill" {
			t.Errorf("kind = %s, 期望 skill", mgr.kind)
		}
		if mgr.language != "cn" {
			t.Errorf("language = %s, 期望 cn", mgr.language)
		}
	})
	t.Run("team-skill kind", func(t *testing.T) {
		mgr, err := NewExperienceManager(nil, nil, "team-skill", "en", nil, nil, nil)
		if err != nil {
			t.Errorf("NewExperienceManager 失败: %v", err)
		}
		if mgr.kind != "team-skill" {
			t.Errorf("kind = %s, 期望 team-skill", mgr.kind)
		}
	})
	t.Run("nil 参数初始化为空 map", func(t *testing.T) {
		mgr, err := NewExperienceManager(nil, nil, "skill", "cn", nil, nil, nil)
		if err != nil {
			t.Errorf("NewExperienceManager 失败: %v", err)
		}
		if len(mgr.skillOps) != 0 {
			t.Errorf("skillOps 应为空 map")
		}
		if len(mgr.pendingGovernance) != 0 {
			t.Errorf("pendingGovernance 应为空 map")
		}
	})
}

// TestExperienceManager_Properties 测试属性访问方法
func TestExperienceManager_Properties(t *testing.T) {
	mgr, _ := NewExperienceManager(nil, nil, "skill", "cn", nil, nil, nil)

	t.Run("PendingApprovalSnapshots", func(t *testing.T) {
		if mgr.PendingApprovalSnapshots() == nil {
			t.Errorf("应返回非 nil map")
		}
	})
	t.Run("PendingGovernance", func(t *testing.T) {
		if mgr.PendingGovernance() == nil {
			t.Errorf("应返回非 nil map")
		}
	})
	t.Run("SkillOps", func(t *testing.T) {
		if mgr.SkillOps() == nil {
			t.Errorf("应返回非 nil map")
		}
	})
}

// TestExperienceManager_BindPendingApprovalSnapshots 测试绑定暂存快照
func TestExperienceManager_BindPendingApprovalSnapshots(t *testing.T) {
	mgr, _ := NewExperienceManager(nil, nil, "skill", "cn", nil, nil, nil)

	t.Run("绑定 nil 重置为空 map", func(t *testing.T) {
		mgr.BindPendingApprovalSnapshots(nil)
		if len(mgr.pendingApprovalSnapshots) != 0 {
			t.Errorf("应重置为空 map")
		}
	})
	t.Run("绑定非 nil map", func(t *testing.T) {
		snapshots := map[string]*PendingChange{"cid": &checkpointing.PendingChange{ChangeID: "cid"}}
		mgr.BindPendingApprovalSnapshots(snapshots)
		if len(mgr.pendingApprovalSnapshots) != 1 {
			t.Errorf("应绑定 1 个条目")
		}
	})
}

// TestExperienceManager_RejectSimplify 测试丢弃治理操作
func TestExperienceManager_RejectSimplify(t *testing.T) {
	mgr, _ := NewExperienceManager(nil, nil, "skill", "cn", nil, nil, map[string]map[string]any{
		"gov1": {"kind": "simplify", "skill_name": "test"},
	})
	if len(mgr.pendingGovernance) != 1 {
		t.Errorf("初始应有 1 个治理操作")
	}
	mgr.RejectSimplify("gov1")
	if len(mgr.pendingGovernance) != 0 {
		t.Errorf("拒绝后应为空")
	}
}

// TestExperienceManager_getRebuildTemplate 测试获取重建提示词模板
func TestExperienceManager_getRebuildTemplate(t *testing.T) {
	t.Run("skill cn", func(t *testing.T) {
		mgr, _ := NewExperienceManager(nil, nil, "skill", "cn", nil, nil, nil)
		template := mgr.getRebuildTemplate()
		if !strings.Contains(template, "%g") {
			t.Errorf("skill cn template 应包含 %%g")
		}
	})
	t.Run("skill en", func(t *testing.T) {
		mgr, _ := NewExperienceManager(nil, nil, "skill", "en", nil, nil, nil)
		template := mgr.getRebuildTemplate()
		if !strings.Contains(template, "%g") {
			t.Errorf("skill en template 应包含 %%g")
		}
	})
	t.Run("team-skill cn", func(t *testing.T) {
		mgr, _ := NewExperienceManager(nil, nil, "team-skill", "cn", nil, nil, nil)
		template := mgr.getRebuildTemplate()
		if !strings.Contains(template, "%g") {
			t.Errorf("team-skill cn template 应包含 %%g")
		}
	})
	t.Run("未知语言回退到 en", func(t *testing.T) {
		mgr, _ := NewExperienceManager(nil, nil, "skill", "fr", nil, nil, nil)
		template := mgr.getRebuildTemplate()
		if !strings.Contains(template, "skill-creator") {
			t.Errorf("未知语言应回退到 en, 应包含 skill-creator")
		}
	})
}

// TestExperienceManager_getDefaultRebuildIntent 测试获取默认重建意图
func TestExperienceManager_getDefaultRebuildIntent(t *testing.T) {
	t.Run("skill cn", func(t *testing.T) {
		mgr, _ := NewExperienceManager(nil, nil, "skill", "cn", nil, nil, nil)
		intent := mgr.getDefaultRebuildIntent()
		if !strings.Contains(intent, "技能") {
			t.Errorf("skill cn intent 应包含 '技能'")
		}
	})
	t.Run("skill en", func(t *testing.T) {
		mgr, _ := NewExperienceManager(nil, nil, "skill", "en", nil, nil, nil)
		intent := mgr.getDefaultRebuildIntent()
		if !strings.Contains(intent, "skill") {
			t.Errorf("skill en intent 应包含 'skill'")
		}
	})
}

// TestToApplyResult 测试 PendingCommitResult 到 ExperienceApplyResult 转换
func TestToApplyResult(t *testing.T) {
	result := toApplyResult("test_skill", PendingCommitResult{AppliedCount: 3, PendingCount: 1})
	if result.SkillName != "test_skill" {
		t.Errorf("SkillName = %s, 期望 test_skill", result.SkillName)
	}
	if result.AppliedCount != 3 {
		t.Errorf("AppliedCount = %d, 期望 3", result.AppliedCount)
	}
	if result.PendingCount != 1 {
		t.Errorf("PendingCount = %d, 期望 1", result.PendingCount)
	}
}

// TestNewOnlineEvolutionOrchestrator 测试编排器创建
func TestNewOnlineEvolutionOrchestrator(t *testing.T) {
	orch := NewOnlineEvolutionOrchestrator(nil, nil, nil, nil, "test_prefix", "test_source")
	if orch.requestIDPrefix != "test_prefix" {
		t.Errorf("requestIDPrefix = %s, 期望 test_prefix", orch.requestIDPrefix)
	}
	if orch.stageSource != "test_source" {
		t.Errorf("stageSource = %s, 期望 test_source", orch.stageSource)
	}
}

// TestNewExperienceScorer 测试评分器创建
func TestNewExperienceScorer(t *testing.T) {
	t.Run("默认策略", func(t *testing.T) {
		scorer := NewExperienceScorer(nil, "qwen-max", "cn", nil, nil)
		if scorer.model != "qwen-max" {
			t.Errorf("model = %s, 期望 qwen-max", scorer.model)
		}
		if scorer.language != "cn" {
			t.Errorf("language = %s, 期望 cn", scorer.language)
		}
		if scorer.evaluatePolicy.AttemptTimeoutSecs != EvaluateLLMPolicy.AttemptTimeoutSecs {
			t.Errorf("evaluatePolicy 应使用默认值")
		}
		if scorer.simplifyPolicy.TotalBudgetSecs != SimplifyLLMPolicy.TotalBudgetSecs {
			t.Errorf("simplifyPolicy 应使用默认值")
		}
	})
	t.Run("自定义策略", func(t *testing.T) {
		ep := llm_resilience.LLMInvokePolicy{AttemptTimeoutSecs: 30, TotalBudgetSecs: 60, MaxAttempts: 3}
		sp := llm_resilience.LLMInvokePolicy{AttemptTimeoutSecs: 120, TotalBudgetSecs: 240, MaxAttempts: 1}
		scorer := NewExperienceScorer(nil, "gpt-4", "en", &ep, &sp)
		if scorer.evaluatePolicy.AttemptTimeoutSecs != 30 {
			t.Errorf("evaluatePolicy.AttemptTimeoutSecs = %f, 期望 30", scorer.evaluatePolicy.AttemptTimeoutSecs)
		}
		if scorer.simplifyPolicy.MaxAttempts != 1 {
			t.Errorf("simplifyPolicy.MaxAttempts = %d, 期望 1", scorer.simplifyPolicy.MaxAttempts)
		}
	})
}

// TestUpdateLLM 测试 LLM 热更新
func TestUpdateLLM(t *testing.T) {
	scorer := NewExperienceScorer(nil, "old-model", "cn", nil, nil)
	scorer.UpdateLLM(nil, "new-model")
	if scorer.model != "new-model" {
		t.Errorf("model = %s, 期望 new-model", scorer.model)
	}
}

// TestNewExperienceTracker 测试跟踪器创建
func TestNewExperienceTracker(t *testing.T) {
	tracker := NewExperienceTracker(nil, nil, 5)
	if tracker.evalInterval != 5 {
		t.Errorf("evalInterval = %d, 期望 5", tracker.evalInterval)
	}
}

// TestParseTimestamp 测试时间戳解析
func TestParseTimestamp(t *testing.T) {
	t.Run("RFC3339Nano", func(t *testing.T) {
		ts := "2025-01-15T10:30:00.123456789Z"
		got, err := parseTimestamp(ts)
		if err != nil {
			t.Errorf("parseTimestamp 失败: %v", err)
		}
		if got.Year() != 2025 {
			t.Errorf("Year = %d, 期望 2025", got.Year())
		}
	})
	t.Run("RFC3339", func(t *testing.T) {
		ts := "2025-01-15T10:30:00+00:00"
		got, err := parseTimestamp(ts)
		if err != nil {
			t.Errorf("parseTimestamp 失败: %v", err)
		}
		if got.Year() != 2025 {
			t.Errorf("Year = %d, 期望 2025", got.Year())
		}
	})
	t.Run("Z suffix", func(t *testing.T) {
		ts := "2025-01-15T10:30:00Z"
		got, err := parseTimestamp(ts)
		if err != nil {
			t.Errorf("parseTimestamp(Z) 失败: %v", err)
		}
		if got.Location().String() != "UTC" {
			t.Errorf("应为 UTC 时区")
		}
	})
	t.Run("无效时间", func(t *testing.T) {
		_, err := parseTimestamp("invalid")
		if err == nil {
			t.Errorf("期望返回错误")
		}
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TODO: 补充依赖 EvolutionStore 的集成测试（需要 build tag `integration`）
// TODO: 补充依赖真实 LLM API 的测试（需要 build tag `llm`）
// - Evaluate/Simplify 方法依赖真实 LLM → 需 llm tag
// - RecordPresented/RecordPresentedRecords/EvaluatePresented 依赖 store+scorer → 需 integration tag
// - OnlineEvolutionOrchestrator.Evolve/buildContext/generateLocalApplyPreview 依赖 store+updater → 需 integration tag
