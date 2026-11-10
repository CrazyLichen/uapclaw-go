package permissions

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
)

// ──────────────────────────── 辅助 ────────────────────────────

// trustedKey 构造会话级调用键。
func trustedKey(rootSessionID string) ToolInvocationKeyV1 {
	return ToolInvocationKeyV1{
		InvocationID:  "inv-1",
		RootSessionID: rootSessionID,
		RequestID:     "req-1",
		ExecutorKind:  "agent",
		ToolCallID:    "call-1",
	}
}

// newTrustedLedger 构造已绑定会话的台账。
func newTrustedLedger(sessionID string) *SessionTrustedSearchUrls {
	ledger := NewSessionTrustedSearchUrls(sessionID)
	return ledger
}

// ──────────────────────────── 常量 ────────────────────────────

func TestTrustedSearchUrls_常量(t *testing.T) {
	if MaxTrustedSearchUrlsPerSession != 500 {
		t.Errorf("MaxTrustedSearchUrlsPerSession = %d, want 500", MaxTrustedSearchUrlsPerSession)
	}
	if TrustedSearchSourceTool != "host_search_producer" {
		t.Errorf("TrustedSearchSourceTool = %q", TrustedSearchSourceTool)
	}
}

// ──────────────────────────── 用例：bind session ───────────────────────────────

func TestSessionTrustedSearchUrls_构造即绑定(t *testing.T) {
	ledger := NewSessionTrustedSearchUrls("sess-1")
	if ledger.RootSessionID() != "sess-1" {
		t.Errorf("RootSessionID = %q", ledger.RootSessionID())
	}
	if ledger.Len() != 0 {
		t.Errorf("Len = %d, want 0", ledger.Len())
	}
}

func TestSessionTrustedSearchUrls_构造去空白(t *testing.T) {
	ledger := NewSessionTrustedSearchUrls("  sess-1  ")
	if ledger.RootSessionID() != "sess-1" {
		t.Errorf("RootSessionID = %q", ledger.RootSessionID())
	}
}

func TestBindSession_空会话报错(t *testing.T) {
	ledger := NewSessionTrustedSearchUrls("")
	if err := ledger.BindSession("   "); err == nil || err.Error() != "trusted_search_session_missing" {
		t.Errorf("err = %v", err)
	}
}

func TestBindSession_首次绑定(t *testing.T) {
	ledger := NewSessionTrustedSearchUrls("")
	if err := ledger.BindSession("sess-1"); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if ledger.RootSessionID() != "sess-1" {
		t.Errorf("RootSessionID = %q", ledger.RootSessionID())
	}
}

func TestBindSession_重复绑定同会话幂等(t *testing.T) {
	ledger := NewSessionTrustedSearchUrls("sess-1")
	if err := ledger.BindSession("sess-1"); err != nil {
		t.Errorf("same session rebind should be ok: %v", err)
	}
}

func TestBindSession_不同会话报错(t *testing.T) {
	ledger := NewSessionTrustedSearchUrls("sess-1")
	err := ledger.BindSession("sess-2")
	if err == nil || err.Error() != "trusted_search_session_mismatch" {
		t.Errorf("err = %v", err)
	}
}

func TestBindSession_已关闭报错(t *testing.T) {
	ledger := NewSessionTrustedSearchUrls("sess-1")
	ledger.Dispose()
	err := ledger.BindSession("sess-1")
	if err == nil || err.Error() != "trusted_search_session_closed" {
		t.Errorf("err = %v", err)
	}
}

// ──────────────────────────── 用例：record batch ───────────────────────────────

func TestRecordBatch_正常记录(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	accepted, dropped := ledger.RecordBatch(
		trustedKey("sess-1"),
		[]string{"https://example.com/a", "https://example.com/b"},
		10,
	)
	if accepted != 2 || dropped != 0 {
		t.Errorf("got (%d, %d), want (2, 0)", accepted, dropped)
	}
	if ledger.Len() != 2 {
		t.Errorf("Len = %d", ledger.Len())
	}
}

func TestRecordBatch_URL归一化去重(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	accepted, _ := ledger.RecordBatch(
		trustedKey("sess-1"),
		[]string{"HTTPS://Example.COM/a", "https://example.com/a"},
		10,
	)
	if accepted != 1 {
		t.Errorf("accepted = %d, want 1（归一后去重）", accepted)
	}
}

func TestRecordBatch_unsafeURL被过滤(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	accepted, _ := ledger.RecordBatch(
		trustedKey("sess-1"),
		[]string{
			"http://example.com/a",          // 非 https → 过滤
			"https://user:pw@example.com/b", // ⚠️ 归一化丢 userinfo → 变合格（上游行为）
			"https://169.254.169.254/c",     // 元数据 → 过滤
			"https://example.com/ok",        // 合格
		},
		10,
	)
	// Python 的 normalize_url_for_match 丢弃 userinfo（Hostname() 不含它），
	// 归一化后 "https://user:pw@example.com/b" → "https://example.com/b"，
	// 再判 unsafe 已无 userinfo，故被接受。此处忠实复刻上游行为。
	if accepted != 2 {
		t.Errorf("accepted = %d, want 2（ok + userinfo 归一化后合格）", accepted)
	}
	if !ledger.Contains("sess-1", "https://example.com/b") {
		t.Error("userinfo URL should be recorded as canonical without userinfo")
	}
}

func TestRecordBatch_limit限制条数(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	urls := []string{}
	for i := 0; i < 10; i++ {
		urls = append(urls, fmt.Sprintf("https://example.com/%d", i))
	}
	accepted, _ := ledger.RecordBatch(trustedKey("sess-1"), urls, 3)
	if accepted != 3 {
		t.Errorf("accepted = %d, want 3（limit 截断）", accepted)
	}
}

func TestRecordBatch_limit为零返回零(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	accepted, dropped := ledger.RecordBatch(
		trustedKey("sess-1"), []string{"https://example.com/a"}, 0)
	if accepted != 0 || dropped != 0 {
		t.Errorf("got (%d, %d), want (0, 0)", accepted, dropped)
	}
}

func TestRecordBatch_limit负数钳到零(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	accepted, _ := ledger.RecordBatch(
		trustedKey("sess-1"), []string{"https://example.com/a"}, -5)
	if accepted != 0 {
		t.Errorf("accepted = %d, want 0", accepted)
	}
}

func TestRecordBatch_limit超上限被钳(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	urls := []string{}
	for i := 0; i < 30; i++ {
		urls = append(urls, fmt.Sprintf("https://example.com/%d", i))
	}
	// limit 超 tools.MaxSearchMaxResults(20) 被钳到 20
	accepted, _ := ledger.RecordBatch(trustedKey("sess-1"), urls, 100)
	if accepted != 20 {
		t.Errorf("accepted = %d, want 20", accepted)
	}
}

func TestRecordBatch_session不匹配返回未接受(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	accepted, dropped := ledger.RecordBatch(
		trustedKey("sess-2"), []string{"https://example.com/a"}, 10)
	if accepted != 0 || dropped != 1 {
		t.Errorf("got (%d, %d), want (0, 1)（解析成功但会话不匹配）", accepted, dropped)
	}
	if ledger.Len() != 0 {
		t.Errorf("Len = %d, want 0", ledger.Len())
	}
}

func TestRecordBatch_台账未绑定时返回未接受(t *testing.T) {
	ledger := NewSessionTrustedSearchUrls("")
	accepted, dropped := ledger.RecordBatch(
		trustedKey("sess-1"), []string{"https://example.com/a"}, 10)
	if accepted != 0 || dropped != 1 {
		t.Errorf("got (%d, %d), want (0, 1)", accepted, dropped)
	}
}

func TestRecordBatch_已关闭返回零(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	ledger.Dispose()
	accepted, dropped := ledger.RecordBatch(
		trustedKey("sess-1"), []string{"https://example.com/a"}, 10)
	if accepted != 0 || dropped != 0 {
		t.Errorf("got (%d, %d), want (0, 0)", accepted, dropped)
	}
}

func TestRecordBatch_容量满时计dropped(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	// 先灌满 500：单次 limit 上限 20，需 25 批
	total := 0
	for batch := 0; batch < 25; batch++ {
		urls := []string{}
		for i := 0; i < tools.MaxSearchMaxResults; i++ {
			urls = append(urls, fmt.Sprintf("https://example.com/%d-%d", batch, i))
		}
		accepted, _ := ledger.RecordBatch(trustedKey("sess-1"), urls, tools.MaxSearchMaxResults)
		total += accepted
	}
	if total != MaxTrustedSearchUrlsPerSession {
		t.Fatalf("fill total = %d, want %d", total, MaxTrustedSearchUrlsPerSession)
	}
	if ledger.Len() != MaxTrustedSearchUrlsPerSession {
		t.Fatalf("Len = %d, want %d", ledger.Len(), MaxTrustedSearchUrlsPerSession)
	}
	// 再灌，容量已满 → dropped
	more := []string{}
	for i := 0; i < 5; i++ {
		more = append(more, fmt.Sprintf("https://other.com/%d", i))
	}
	accepted2, dropped2 := ledger.RecordBatch(trustedKey("sess-1"), more, 10)
	if accepted2 != 0 || dropped2 != 5 {
		t.Errorf("overflow got (%d, %d), want (0, 5)", accepted2, dropped2)
	}
}

func TestRecordBatch_重复URL不重复计数(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	ledger.RecordBatch(trustedKey("sess-1"), []string{"https://example.com/a"}, 10)
	accepted, dropped := ledger.RecordBatch(
		trustedKey("sess-1"), []string{"https://example.com/a"}, 10)
	if accepted != 0 || dropped != 0 {
		t.Errorf("got (%d, %d), want (0, 0)（已存在不计数）", accepted, dropped)
	}
}

func TestRecordBatch_非法URL跳过(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	accepted, _ := ledger.RecordBatch(
		trustedKey("sess-1"),
		[]string{"", "not-a-url", "/just/path", "https://example.com/ok"},
		10,
	)
	if accepted != 1 {
		t.Errorf("accepted = %d, want 1", accepted)
	}
}

// ──────────────────────────── 用例：contains / sources ───────────────────────────────

func TestContains_命中与未命中(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	ledger.RecordBatch(trustedKey("sess-1"), []string{"https://example.com/a"}, 10)

	if !ledger.Contains("sess-1", "https://example.com/a") {
		t.Error("should contain recorded url")
	}
	if ledger.Contains("sess-1", "https://example.com/other") {
		t.Error("should not contain unrecorded url")
	}
	if ledger.Contains("sess-2", "https://example.com/a") {
		t.Error("different session should not match")
	}
}

func TestContains_归一化后比对(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	ledger.RecordBatch(trustedKey("sess-1"), []string{"https://example.com/a"}, 10)

	if !ledger.Contains("sess-1", "HTTPS://Example.COM/a") {
		t.Error("should normalize before comparing")
	}
}

func TestContains_已关闭返回false(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	ledger.RecordBatch(trustedKey("sess-1"), []string{"https://example.com/a"}, 10)
	ledger.Dispose()
	if ledger.Contains("sess-1", "https://example.com/a") {
		t.Error("disposed ledger should return false")
	}
}

func TestSources_产出RecentURLSource(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	ledger.RecordBatch(trustedKey("sess-1"),
		[]string{"https://example.com/a", "https://other.org/b"}, 10)

	sources := ledger.Sources("sess-1")
	if len(sources) != 2 {
		t.Fatalf("sources = %v", sources)
	}
	for _, src := range sources {
		if !src.Trusted {
			t.Error("trusted should be true")
		}
		if src.SourceTool != TrustedSearchSourceTool {
			t.Errorf("source_tool = %q", src.SourceTool)
		}
		// Python 不传 source_kind → 默认 "recent_search_result"
		if src.SourceKind != "recent_search_result" {
			t.Errorf("source_kind = %q, want recent_search_result", src.SourceKind)
		}
		if src.Host == "" {
			t.Errorf("host should be non-empty for %q", src.URL)
		}
	}
	if sources[0].Host != "example.com" {
		t.Errorf("host = %q, want example.com", sources[0].Host)
	}
}

func TestSources_会话不匹配返回空(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	ledger.RecordBatch(trustedKey("sess-1"), []string{"https://example.com/a"}, 10)
	if got := ledger.Sources("sess-2"); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestSources_已关闭返回空(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	ledger.RecordBatch(trustedKey("sess-1"), []string{"https://example.com/a"}, 10)
	ledger.Dispose()
	if got := ledger.Sources("sess-1"); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

// ──────────────────────────── 用例：dispose / Len ───────────────────────────────

func TestDispose_清空并关闭(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	ledger.RecordBatch(trustedKey("sess-1"), []string{"https://example.com/a"}, 10)
	ledger.Dispose()

	if ledger.Len() != 0 {
		t.Errorf("Len = %d, want 0", ledger.Len())
	}
	if ledger.Contains("sess-1", "https://example.com/a") {
		t.Error("disposed ledger should not contain")
	}
}

func TestDispose_幂等(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	ledger.Dispose()
	ledger.Dispose() // 不应 panic
}

// ──────────────────────────── producer 租约 ────────────────────────────

func TestBindTrustedSearchProducer_绑定后可完成(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	ctx := BindTrustedSearchProducer(
		context.Background(), ledger, trustedKey("sess-1"), "mcp_free_search", 10)

	accepted, dropped := CompleteTrustedSearchProducer(ctx, "mcp_free_search", true,
		[]string{"https://example.com/a"})
	if accepted != 1 || dropped != 0 {
		t.Errorf("got (%d, %d), want (1, 0)", accepted, dropped)
	}
}

func TestCompleteTrustedSearchProducer_二次完成返回零(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	ctx := BindTrustedSearchProducer(
		context.Background(), ledger, trustedKey("sess-1"), "mcp_free_search", 10)

	CompleteTrustedSearchProducer(ctx, "mcp_free_search", true, []string{"https://example.com/a"})
	accepted, dropped := CompleteTrustedSearchProducer(ctx, "mcp_free_search", true,
		[]string{"https://example.com/b"})
	if accepted != 0 || dropped != 0 {
		t.Errorf("second complete got (%d, %d), want (0, 0)", accepted, dropped)
	}
}

func TestCompleteTrustedSearchProducer_未绑定返回零(t *testing.T) {
	accepted, dropped := CompleteTrustedSearchProducer(
		context.Background(), "mcp_free_search", true, []string{"https://example.com/a"})
	if accepted != 0 || dropped != 0 {
		t.Errorf("got (%d, %d), want (0, 0)", accepted, dropped)
	}
}

func TestCompleteTrustedSearchProducer_工具名不匹配返回零(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	ctx := BindTrustedSearchProducer(
		context.Background(), ledger, trustedKey("sess-1"), "mcp_free_search", 10)

	accepted, dropped := CompleteTrustedSearchProducer(ctx, "mcp_paid_search", true,
		[]string{"https://example.com/a"})
	if accepted != 0 || dropped != 0 {
		t.Errorf("got (%d, %d), want (0, 0)", accepted, dropped)
	}
}

func TestCompleteTrustedSearchProducer_success为false不记录(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	ctx := BindTrustedSearchProducer(
		context.Background(), ledger, trustedKey("sess-1"), "mcp_free_search", 10)

	accepted, dropped := CompleteTrustedSearchProducer(ctx, "mcp_free_search", false,
		[]string{"https://example.com/a"})
	if accepted != 0 || dropped != 0 {
		t.Errorf("got (%d, %d), want (0, 0)", accepted, dropped)
	}
	if ledger.Len() != 0 {
		t.Errorf("Len = %d, want 0", ledger.Len())
	}
}

func TestBindTrustedSearchProducer_重复绑定撤销旧租约(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	ctx := BindTrustedSearchProducer(
		context.Background(), ledger, trustedKey("sess-1"), "mcp_free_search", 10)
	// 再绑一次，旧租约被 revoke
	ctx = BindTrustedSearchProducer(
		ctx, ledger, trustedKey("sess-1"), "mcp_free_search", 10)

	accepted, _ := CompleteTrustedSearchProducer(ctx, "mcp_free_search", true,
		[]string{"https://example.com/a"})
	if accepted != 1 {
		t.Errorf("new lease should work, got accepted=%d", accepted)
	}
}

func TestClearTrustedSearchProducer_清除后完成返回零(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	ctx := BindTrustedSearchProducer(
		context.Background(), ledger, trustedKey("sess-1"), "mcp_free_search", 10)
	ctx = ClearTrustedSearchProducer(ctx)

	accepted, dropped := CompleteTrustedSearchProducer(ctx, "mcp_free_search", true,
		[]string{"https://example.com/a"})
	if accepted != 0 || dropped != 0 {
		t.Errorf("got (%d, %d), want (0, 0)", accepted, dropped)
	}
}

func TestClearTrustedSearchProducer_未绑定不panic(t *testing.T) {
	ctx := ClearTrustedSearchProducer(context.Background())
	if ctx == nil {
		t.Error("should return a ctx")
	}
}

func TestBindTrustedSearchProducer_maxResults被钳制(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	// 传 0 → NormalizeSearchMaxResults 钳到 MinSearchMaxResults(1)
	ctx := BindTrustedSearchProducer(
		context.Background(), ledger, trustedKey("sess-1"), "mcp_free_search", 0)

	accepted, _ := CompleteTrustedSearchProducer(ctx, "mcp_free_search", true,
		[]string{"https://example.com/a", "https://example.com/b"})
	if accepted != 1 {
		t.Errorf("accepted = %d, want 1（max_results=0 应钳到 1，非 clampInt 的默认 8）", accepted)
	}
}

// ──────────────────────────── 并发安全 ────────────────────────────

func TestSessionTrustedSearchUrls_并发记录(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ledger.RecordBatch(trustedKey("sess-1"),
				[]string{fmt.Sprintf("https://example.com/%d", i)}, 1)
		}(i)
	}
	wg.Wait()
	if ledger.Len() != 20 {
		t.Errorf("Len = %d, want 20", ledger.Len())
	}
}

func TestSessionTrustedSearchUrls_并发读写(t *testing.T) {
	ledger := newTrustedLedger("sess-1")
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			ledger.RecordBatch(trustedKey("sess-1"),
				[]string{fmt.Sprintf("https://example.com/%d", i)}, 1)
		}(i)
		go func() {
			defer wg.Done()
			_ = ledger.Contains("sess-1", "https://example.com/0")
			_ = ledger.Sources("sess-1")
			_ = ledger.Len()
		}()
	}
	wg.Wait()
}
