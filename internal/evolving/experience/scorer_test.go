package experience

import (
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
)

// ──────────────────────────── 导出函数 ────────────────────────────

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

// TestNewExperienceScorer 测试评分器创建
func TestNewExperienceScorer(t *testing.T) {
	t.Run("nil llmModel 返回错误", func(t *testing.T) {
		scorer, err := NewExperienceScorer(nil, "qwen-max", "cn", nil, nil)
		if err == nil {
			t.Errorf("nil llmModel 应返回错误")
		}
		if scorer != nil {
			t.Errorf("nil llmModel 应返回 nil scorer")
		}
		if !strings.Contains(err.Error(), "llmModel 不能为 nil") {
			t.Errorf("错误消息 = %s, 应包含 'llmModel 不能为 nil'", err.Error())
		}
	})
	t.Run("有效 llmModel 默认策略", func(t *testing.T) {
		mockModel := &llm.Model{}
		scorer, err := NewExperienceScorer(mockModel, "qwen-max", "cn", nil, nil)
		if err != nil {
			t.Errorf("NewExperienceScorer 失败: %v", err)
		}
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
	t.Run("有效 llmModel 自定义策略", func(t *testing.T) {
		mockModel := &llm.Model{}
		ep := llm_resilience.LLMInvokePolicy{AttemptTimeoutSecs: 30, TotalBudgetSecs: 60, MaxAttempts: 3}
		sp := llm_resilience.LLMInvokePolicy{AttemptTimeoutSecs: 120, TotalBudgetSecs: 240, MaxAttempts: 1}
		scorer, err := NewExperienceScorer(mockModel, "gpt-4", "en", &ep, &sp)
		if err != nil {
			t.Errorf("NewExperienceScorer 失败: %v", err)
		}
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
	t.Run("nil llmModel 不更新", func(t *testing.T) {
		mockModel := &llm.Model{}
		scorer, err := NewExperienceScorer(mockModel, "old-model", "cn", nil, nil)
		if err != nil {
			t.Errorf("NewExperienceScorer 失败: %v", err)
		}
		scorer.UpdateLLM(nil, "new-model")
		// nil llmModel 应被拒绝，model 保持不变
		if scorer.model != "old-model" {
			t.Errorf("model = %s, 期望 old-model（nil 不应更新）", scorer.model)
		}
	})
	t.Run("有效 llmModel 更新", func(t *testing.T) {
		oldModel := &llm.Model{}
		newModel := &llm.Model{}
		scorer, err := NewExperienceScorer(oldModel, "old-model", "cn", nil, nil)
		if err != nil {
			t.Errorf("NewExperienceScorer 失败: %v", err)
		}
		scorer.UpdateLLM(newModel, "new-model")
		if scorer.model != "new-model" {
			t.Errorf("model = %s, 期望 new-model", scorer.model)
		}
	})
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
	// 对齐 Python: fromisoformat 可解析无时区时间戳，然后补 UTC
	t.Run("无时区ISO", func(t *testing.T) {
		ts := "2025-01-15T10:30:00"
		got, err := parseTimestamp(ts)
		if err != nil {
			t.Fatalf("parseTimestamp(无时区) 失败: %v", err)
		}
		if got.Year() != 2025 {
			t.Errorf("Year = %d, 期望 2025", got.Year())
		}
		if got.Location().String() != "UTC" {
			t.Errorf("无时区时间戳应补 UTC，实际 %v", got.Location())
		}
	})
	t.Run("无时区ISO纳秒", func(t *testing.T) {
		ts := "2025-01-15T10:30:00.123456789"
		got, err := parseTimestamp(ts)
		if err != nil {
			t.Fatalf("parseTimestamp(无时区纳秒) 失败: %v", err)
		}
		if got.Year() != 2025 {
			t.Errorf("Year = %d, 期望 2025", got.Year())
		}
		if got.Nanosecond() != 123456789 {
			t.Errorf("Nanosecond = %d, 期望 123456789", got.Nanosecond())
		}
	})
	t.Run("带时区偏移", func(t *testing.T) {
		ts := "2025-01-15T10:30:00+08:00"
		got, err := parseTimestamp(ts)
		if err != nil {
			t.Fatalf("parseTimestamp(带偏移) 失败: %v", err)
		}
		if got.Location().String() != "UTC" {
			t.Errorf("应为 UTC 时区，实际 %v", got.Location())
		}
		// +08:00 的 10:30 → UTC 的 02:30
		if got.Hour() != 2 {
			t.Errorf("Hour = %d, 期望 2（UTC 转换后）", got.Hour())
		}
	})
}
