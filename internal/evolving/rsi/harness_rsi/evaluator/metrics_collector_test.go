package evaluator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/evaluator/judger"
)

// TestIsExecutionOnlyEvaluation 遗留完成态评分识别。
func TestIsExecutionOnlyEvaluation(t *testing.T) {
	if !judger.IsExecutionOnlyEvaluation(map[string]any{"method": "none"}) {
		t.Fatal("method=none 应识别")
	}
	if !judger.IsExecutionOnlyEvaluation(map[string]any{"method": "rubric", "metadata": map[string]any{"rule_engine_status": "backend_completed"}}) {
		t.Fatal("backend_completed 应识别")
	}
	if judger.IsExecutionOnlyEvaluation(map[string]any{"method": "rubric"}) {
		t.Fatal("正常评分不应识别")
	}
	if judger.IsExecutionOnlyEvaluation(map[string]any{"method": "rubric", "metadata": map[string]any{"rule_engine_status": "ok"}}) {
		t.Fatal("其他状态不应识别")
	}
}

// TestMetricsCollectorCollect 指标聚合全分支。
func TestMetricsCollectorCollect(t *testing.T) {
	dir := t.TempDir()
	writeCase := func(t *testing.T, name, body string) {
		t.Helper()
		caseDir := filepath.Join(dir, name)
		if err := os.MkdirAll(caseDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(caseDir, "result.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeCase(t, "c1", `{"status":"passed","score":0.8,"evaluation":{"method":"rubric"}}`)
	writeCase(t, "c2", `{"status":"failed","score":0.2,"evaluation":{"method":"rubric","passed":false}}`)
	writeCase(t, "c3", `{"status":"passed","evaluation":{"method":"llm_as_judge"}}`)
	output := filepath.Join(dir, "out", "summary.json")
	got, err := NewMetricsCollector().Collect(dir, output)
	if err != nil {
		t.Fatalf("聚合失败: %v", err)
	}
	raw, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	var summary map[string]any
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if summary["total_cases"].(float64) != 3 || summary["passed_cases"].(float64) != 2 || summary["failed_cases"].(float64) != 1 {
		t.Fatalf("计数错误: %v", summary)
	}
	if summary["evaluation_method"] != "rubric" {
		t.Fatalf("方法聚合错误: %v", summary["evaluation_method"])
	}
	avg := summary["average_score"].(float64)
	if avg < 0.49 || avg > 0.51 {
		t.Fatalf("均值错误: %v", avg)
	}
	// 遗留完成态评分拒绝
	writeCase(t, "c4", `{"status":"passed","evaluation":{"method":"none"}}`)
	if _, err := NewMetricsCollector().Collect(dir, output); err == nil {
		t.Fatal("遗留完成态评分应拒绝")
	}
	// 非 mapping 结果拒绝
	bad := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bad, "c1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "c1", "result.json"), []byte(`[1,2]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewMetricsCollector().Collect(bad, filepath.Join(bad, "s.json")); err == nil {
		t.Fatal("非 mapping 应拒绝")
	}
	// 无用例目录
	empty := t.TempDir()
	out2, err := NewMetricsCollector().Collect(empty, filepath.Join(empty, "s.json"))
	if err != nil {
		t.Fatalf("空目录应通过: %v", err)
	}
	raw2, _ := os.ReadFile(out2)
	var summary2 map[string]any
	_ = json.Unmarshal(raw2, &summary2)
	if summary2["average_score"] != nil || summary2["evaluation_method"] != "" {
		t.Fatalf("空聚合字段错误: %v", summary2)
	}
}
