package usage

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildSnapshot_满足前端校验 校验 v1 必填字段齐全（前端 parseContextUsageSnapshot）。
func TestBuildSnapshot_满足前端校验(t *testing.T) {
	snapshot := BuildSnapshot(SnapshotInput{
		Phase:              PhasePostCall,
		RequestID:          "req-1",
		SessionID:          "sess-1",
		LimitTokens:        200000,
		SystemPromptTokens: 1000,
		ToolsTokens:        2000,
		SkillsTokens:       300,
		MessagesTokens:     5000,
	})
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	text := string(data)
	for _, key := range []string{
		`"event_type":"context.usage"`,
		`"schema_version":"context-usage.v1"`,
		`"phase":"post_call"`,
		`"request_id":"req-1"`,
		`"product_session_id":"sess-1"`,
		`"team_id":""`,
		`"member_name":""`,
		`"depth":0`,
		`"context_window":{`,
		`"limit_tokens":200000`,
		`"parts":{`,
		`"session_kv_cache_hit_rate":null`,
	} {
		if !strings.Contains(text, key) {
			t.Fatalf("缺少 %s，实际 %s", key, text)
		}
	}
	if !strings.Contains(text, `"percentage_of_window"`) {
		t.Fatalf("窗口已知时每个分类都应带占比")
	}
}

// TestBuildSnapshot_provider归因到messages 验证差额记到消息类别。
func TestBuildSnapshot_provider归因到messages(t *testing.T) {
	meta := &schema.UsageMetadata{InputTokens: 12000}
	snapshot := BuildSnapshot(SnapshotInput{
		Phase:              PhasePostCall,
		SessionID:          "s",
		LimitTokens:        200000,
		SystemPromptTokens: 1000,
		MessagesTokens:     5000,
		Usage:              meta,
	})
	if got := snapshot.Parts[CategoryMessages].Tokens; got != 11000 {
		t.Fatalf("差额应归因到 messages（12000-6000+5000），got %d", got)
	}
	if snapshot.ContextWindow.TokensSource != "provider_usage" {
		t.Fatalf("tokens_source 应为 provider_usage，got %s", snapshot.ContextWindow.TokensSource)
	}
	if snapshot.ContextWindow.InputTokens == nil || *snapshot.ContextWindow.InputTokens != 12000 {
		t.Fatalf("input_tokens 应为 12000")
	}
	if snapshot.ContextWindow.OccupancyRate == nil || *snapshot.ContextWindow.OccupancyRate != 0.06 {
		t.Fatalf("occupancy_rate 应为 0.06，got %v", snapshot.ContextWindow.OccupancyRate)
	}
}

// TestBuildSnapshot_无窗口无provider 验证降级路径不产生占比与占用率。
func TestBuildSnapshot_无窗口无provider(t *testing.T) {
	snapshot := BuildSnapshot(SnapshotInput{Phase: PhasePreCall, SessionID: "s", MessagesTokens: 100})
	if snapshot.ContextWindow.LimitTokens != nil || snapshot.ContextWindow.OccupancyRate != nil {
		t.Fatalf("窗口未知时不应产生上限/占用率")
	}
	if snapshot.Parts[CategoryMessages].PercentageOfWindow != nil {
		t.Fatalf("窗口未知时不应产生分类占比")
	}
	if snapshot.ContextWindow.TokensSource != "local_estimate" {
		t.Fatalf("来源应为 local_estimate，got %s", snapshot.ContextWindow.TokensSource)
	}
}

// TestRequestCacheFromUsage 覆盖全字段/仅输入/未上报/命中超过输入。
func TestRequestCacheFromUsage(t *testing.T) {
	if got := RequestCacheFromUsage(nil); got.Status != "not_reported" {
		t.Fatalf("nil 用量应未上报，got %s", got.Status)
	}

	full := RequestCacheFromUsage(&schema.UsageMetadata{InputTokens: 1000, CacheTokens: 400})
	if full.HitRate == nil || *full.HitRate != 0.4 {
		t.Fatalf("命中率应为 0.4，got %v", full.HitRate)
	}
	if full.CacheMissTokens == nil || *full.CacheMissTokens != 600 {
		t.Fatalf("未命中应为 600，got %v", full.CacheMissTokens)
	}

	onlyInput := RequestCacheFromUsage(&schema.UsageMetadata{InputTokens: 500})
	if onlyInput.CacheReadTokens != nil || onlyInput.Status != "reported" {
		t.Fatalf("只有输入时应 reported 但无命中字段: %+v", onlyInput)
	}

	clamped := RequestCacheFromUsage(&schema.UsageMetadata{InputTokens: 100, CacheTokens: 300})
	if clamped.CacheMissTokens == nil || *clamped.CacheMissTokens != 0 {
		t.Fatalf("命中超过输入时未命中应钳制为 0，got %v", clamped.CacheMissTokens)
	}
	if clamped.HitRate == nil || *clamped.HitRate != 1 {
		t.Fatalf("命中率应封顶 1，got %v", clamped.HitRate)
	}
}

// TestAggregateSessionCache 覆盖空集/全未上报/部分缺失。
func TestAggregateSessionCache(t *testing.T) {
	empty := AggregateSessionCache(nil)
	if empty.CallsTotal != 0 || empty.Status != "not_reported" {
		t.Fatalf("空集应 not_reported: %+v", empty)
	}

	input, read := 1000, 400
	partialInput := 500
	aggregated := AggregateSessionCache([]RequestKVCacheUsage{
		{InputTokens: &input, CacheReadTokens: &read},
		{InputTokens: &partialInput},
		{},
	})
	if aggregated.CallsTotal != 3 || aggregated.CallsObserved != 1 ||
		aggregated.CallsPartial != 1 || aggregated.CallsNotReported != 1 {
		t.Fatalf("计数错误: %+v", aggregated)
	}
	if aggregated.InputTokensTotal != 1500 || aggregated.CacheReadTokensTotal != 400 {
		t.Fatalf("合计错误: %+v", aggregated)
	}
	if aggregated.WeightedHitRate == nil {
		t.Fatal("应有加权命中率")
	}
	if aggregated.DataQuality != "partial" {
		t.Fatalf("数据完整度应为 partial，got %s", aggregated.DataQuality)
	}
}

// TestRequestCacheFromUsage_新字段优先 CacheReadTokens 非空时直接采用，
// 不再由 CacheTokens 旧算法推导；新字段缺失时回落旧算法。
// 对齐设计文档批次 C-B3（session_aggregator 切新字段）。
func TestRequestCacheFromUsage_新字段优先(t *testing.T) {
	read, miss := 60, 40
	meta := &schema.UsageMetadata{InputTokens: 100, CacheTokens: 60,
		CacheReadTokens: &read, CacheMissTokens: &miss, CacheStatus: "observed", CacheSource: "provider_usage"}
	out := RequestCacheFromUsage(meta)
	if out.CacheReadTokens == nil || *out.CacheReadTokens != 60 {
		t.Fatalf("read: %+v", out)
	}
	// miss 用 provider 上报值 40（旧算法会算 input-read=40 恰好同值，改用区分值）
	read2, miss2 := 60, 25
	out2 := RequestCacheFromUsage(&schema.UsageMetadata{InputTokens: 100,
		CacheReadTokens: &read2, CacheMissTokens: &miss2, CacheStatus: "observed", CacheSource: "provider_usage"})
	if out2.CacheMissTokens == nil || *out2.CacheMissTokens != 25 {
		t.Fatalf("miss 应取 provider 上报 25 而非推导 40: %+v", out2)
	}
	if out.Status != "reported" || out.Source != "provider_usage" {
		t.Fatalf("status: %+v", out)
	}

	// 新字段有 read 无 miss：miss 由 input-read 推导
	read3 := 30
	out3 := RequestCacheFromUsage(&schema.UsageMetadata{InputTokens: 100,
		CacheReadTokens: &read3, CacheStatus: "observed"})
	if out3.CacheMissTokens == nil || *out3.CacheMissTokens != 70 {
		t.Fatalf("推导 miss: %+v", out3)
	}

	// 旧字段回落：无新字段但 CacheTokens>0 时维持现有算法
	legacy := RequestCacheFromUsage(&schema.UsageMetadata{InputTokens: 100, CacheTokens: 50})
	if legacy.CacheReadTokens == nil || *legacy.CacheReadTokens != 50 || legacy.Status != "reported" {
		t.Fatalf("legacy: %+v", legacy)
	}
}
