// T12 单测：compression_recall 工具渲染/未命中/B-15 抽取。
package compressionrecall

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor/recall"
)

// TestBuildMissHint 未命中提示模板插值。
// Python: compression_recall.py:29。
func TestBuildMissHint(t *testing.T) {
	result := map[string]any{
		"archives_in_session": []any{
			map[string]any{"memory_id": "m1"},
			map[string]any{"memory_id": "m2"},
		},
		"recall_root": "/ws/context/s1_context/recall",
	}
	hint := buildMissHint(result, "cn")
	if !strings.Contains(hint, "2 个压缩归档") || !strings.Contains(hint, "m1, m2") {
		t.Errorf("提示应含归档数与列表: %q", hint)
	}
	if !strings.Contains(hint, "/ws/context/s1_context/recall") {
		t.Error("提示应含归档根目录")
	}
	// en 回退
	if hint := buildMissHint(map[string]any{}, "en"); !strings.Contains(hint, "No matching") {
		t.Errorf("en 模板应生效: %q", hint)
	}
}

// TestRenderRecallContent 渲染头部与片段行。
// Python: compression_recall.py:44。
func TestRenderRecallContent(t *testing.T) {
	result := map[string]any{
		"chunks": []any{
			map[string]any{
				"memory_id": "m1", "turn_id": "t1", "chunk_id": "c1",
				"score": 0.9, "content": "  original text  ",
			},
		},
		"archive_path":        "/ws/context/s1_context/recall/20260101_query/metadata.json",
		"archives_in_session": []any{map[string]any{"memory_id": "m1"}},
		"returned_tokens":     120,
		"truncated":           true,
		"memory_id":           "m1",
	}
	got := renderRecallContent(result, "cn")
	if !strings.Contains(got, "召回 1 个原文片段") || !strings.Contains(got, "已达上限") {
		t.Errorf("头部渲染不符: %q", got[:min(120, len(got))])
	}
	if !strings.Contains(got, "original text") {
		t.Error("片段体应保留")
	}
}

// TestRecall_B15QueryExtraction B-15：query 从结构化 user content 抽取（recall 半区既有实现锚点）。
func TestRecall_B15QueryExtraction(t *testing.T) {
	// contentToQueryText/stripChannelEnvelope 已随 24.2 recall 落地——锚点断言防回退
	got := recall.ExtractQueryText("fix login bug")
	if got != "fix login bug" {
		t.Errorf("结构化 content 抽取 want 'fix login bug' got %q", got)
	}
}

// TestToolInvoke 无会话返回失败。
func TestToolInvoke(t *testing.T) {
	tool, _, err := NewCompressionRecallTool("/ws", "cn", "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	out, err := tool.Invoke(nil, map[string]any{"query": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if success, _ := out["success"].(bool); success {
		t.Error("无会话应失败")
	}
	if errMsg, _ := out["error"].(string); !strings.Contains(errMsg, "requires the current runtime session") {
		t.Errorf("错误信息不符: %q", errMsg)
	}
}
