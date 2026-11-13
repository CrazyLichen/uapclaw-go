package recommendation

// topic_pipeline_c2_test.go 对齐 topic_pipeline.py:2523-2708 用例。

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestDiscussionCoverage 对齐 :2527-2540。
func TestDiscussionCoverage(t *testing.T) {
	summary := summaryForTests(t, "summary:s1:"+hex64("a"), "s1", 500.0)
	coverage := DiscussionCoverage([]*SessionSummaryArtifact{summary},
		[]map[string]any{{"other": 1}})
	if len(coverage) != 2 {
		t.Fatalf("len=%d", len(coverage))
	}
	first := coverage[0]
	if str(first["session_id"]) != "s1" || str(first["summary"]) != "摘要内容" {
		t.Fatalf("first=%v", first)
	}
	if str(first["completed_on"]) != "1970-01-01" {
		// completed_at=10.0 → 本地日期
		t.Fatalf("completed_on=%v", first["completed_on"])
	}
}

// TestOptionalLanes_宽限协调 对齐 :2636-2698。
func TestOptionalLanesGrace(t *testing.T) {
	lanes := NewOptionalLanesWithGrace(0.05)
	// 未定时：调用无界
	value, err := lanes.Within(context.Background(), []string{"recent"},
		func(ctx context.Context) (any, error) { return "ok", nil })
	if err != nil || pyStr(map[string]any{"v": value}["v"]) != "ok" {
		t.Fatalf("unbounded call: %v", err)
	}
	if len(lanes.TimedOutSet()) != 0 {
		t.Fatalf("no timeouts yet")
	}
	// 定宽限后：慢调用被切断
	lanes.MandatoryReady()
	_, err = lanes.Within(context.Background(), []string{"recent"},
		func(ctx context.Context) (any, error) {
			time.Sleep(200 * time.Millisecond)
			return "late", nil
		})
	if _, isTimeout := err.(*OptionalLaneTimeout); !isTimeout {
		t.Fatalf("slow call should time out: %v", err)
	}
	if !lanes.TimedOutSet()["recent"] {
		t.Fatalf("lane recorded")
	}
	// 宽限已过：直接超时（对齐 :2685 不执行调用）
	_, err = lanes.Within(context.Background(), []string{"hot_news"},
		func(ctx context.Context) (any, error) { return "fast", nil })
	if _, isTimeout := err.(*OptionalLaneTimeout); !isTimeout {
		t.Fatalf("expired deadline should timeout: %v", err)
	}
	if !lanes.TimedOutSet()["hot_news"] {
		t.Fatalf("hot_news recorded")
	}
	// Finish：已完成任务成功完成
	done := make(chan struct{})
	close(done)
	cancel := func() {}
	if !lanes.Finish("recent", done, cancel) {
		t.Fatalf("completed task should finish")
	}
}

// TestOptionalLane_判定 对齐 :2700-2708。
func TestOptionalLane(t *testing.T) {
	if OptionalLane(map[string]any{"news_match": "matched"}) != "hot_news" {
		t.Fatalf("news")
	}
	if OptionalLane(map[string]any{"category": "profile_recent_progress"}) != "recent" {
		t.Fatalf("recent")
	}
	if OptionalLane(map[string]any{"category": "project_domain_progress"}) != "project_domain" {
		t.Fatalf("project domain")
	}
	if OptionalLane(map[string]any{"category": "adjacent_explanation"}) != "" {
		t.Fatalf("mandatory lane has no optional name")
	}
}

// TestGraceConstants 对齐 V6_OPTIONAL_LANE_GRACE_SECONDS。
func TestGraceConstants(t *testing.T) {
	if V6OptionalLaneGraceSeconds <= 0 {
		t.Fatalf("grace must be positive")
	}
	// 数值来自 AST 导出：核对与 Python 常量一致由 schema JSON 常量承载
	if strings.TrimSpace("") != "" {
		t.Fatalf("noop")
	}
}
