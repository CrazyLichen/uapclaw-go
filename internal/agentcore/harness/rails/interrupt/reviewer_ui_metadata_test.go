package interrupt

import (
	"strings"
	"testing"
)

// ──────────────────────────── 三层合并 ────────────────────────────

// TestReviewerUIMetadata_从interaction取 测试第一层来源。
func TestReviewerUIMetadata_从interaction取(t *testing.T) {
	interaction := map[string]any{
		"metadata": map[string]any{"risk_level": "HIGH"},
	}
	got := reviewerUIMetadataFromInteraction(interaction, map[string]any{})
	if got["risk_level"] != "HIGH" {
		t.Errorf("risk_level = %v, want HIGH", got["risk_level"])
	}
}

// TestReviewerUIMetadata_从valueMetadata取 测试第二层来源。
func TestReviewerUIMetadata_从valueMetadata取(t *testing.T) {
	value := map[string]any{
		"metadata": map[string]any{"evidence_summary": "命中规则 R1"},
	}
	got := reviewerUIMetadataFromInteraction(map[string]any{}, value)
	if got["evidence_summary"] != "命中规则 R1" {
		t.Errorf("evidence_summary = %v", got["evidence_summary"])
	}
}

// TestReviewerUIMetadata_从value顶层取 测试第三层来源。
//
// Python: for key in _AUTO_REVIEWER_UI_METADATA_KEYS:
//
//	value = _read_value_field(value_obj, key, None)
//	if value not in (None, ""): metadata[key] = value
func TestReviewerUIMetadata_从value顶层取(t *testing.T) {
	value := map[string]any{"decision_source": "auto_reviewer"}
	got := reviewerUIMetadataFromInteraction(map[string]any{}, value)
	if got["decision_source"] != "auto_reviewer" {
		t.Errorf("decision_source = %v", got["decision_source"])
	}
}

// TestReviewerUIMetadata_后者覆盖前者 测试合并优先级。
func TestReviewerUIMetadata_后者覆盖前者(t *testing.T) {
	interaction := map[string]any{
		"metadata": map[string]any{"risk_level": "LOW"},
	}
	value := map[string]any{
		"metadata": map[string]any{"risk_level": "HIGH"},
	}
	got := reviewerUIMetadataFromInteraction(interaction, value)
	if got["risk_level"] != "HIGH" {
		t.Errorf("risk_level = %v, want HIGH（value.metadata 应覆盖 interaction.metadata）", got["risk_level"])
	}

	// value 顶层优先级最高
	value["risk_level"] = "CRITICAL"
	got = reviewerUIMetadataFromInteraction(interaction, value)
	if got["risk_level"] != "CRITICAL" {
		t.Errorf("risk_level = %v, want CRITICAL（value 顶层优先级最高）", got["risk_level"])
	}
}

// ──────────────────────────── 白名单过滤 ────────────────────────────

// TestReviewerUIMetadata_非白名单键被丢弃 测试白名单。
func TestReviewerUIMetadata_非白名单键被丢弃(t *testing.T) {
	interaction := map[string]any{
		"metadata": map[string]any{
			"risk_level":        "LOW",
			"internal_trace_id": "should_be_dropped",
			"cache_marker":      "x",
		},
	}
	got := reviewerUIMetadataFromInteraction(interaction, map[string]any{})
	if _, exists := got["internal_trace_id"]; exists {
		t.Error("非白名单键 internal_trace_id 不应投影")
	}
	if _, exists := got["cache_marker"]; exists {
		t.Error("非白名单键 cache_marker 不应投影")
	}
	if got["risk_level"] != "LOW" {
		t.Error("白名单键 risk_level 应保留")
	}
}

// TestReviewerUIMetadata_空值跳过 测试 nil 与空串被跳过。
func TestReviewerUIMetadata_空值跳过(t *testing.T) {
	interaction := map[string]any{
		"metadata": map[string]any{
			"risk_level":       "",
			"decision_source":  nil,
			"evidence_summary": "有内容",
		},
	}
	got := reviewerUIMetadataFromInteraction(interaction, map[string]any{})
	if _, exists := got["risk_level"]; exists {
		t.Error("空串应被跳过")
	}
	if _, exists := got["decision_source"]; exists {
		t.Error("nil 应被跳过")
	}
	if got["evidence_summary"] != "有内容" {
		t.Error("非空值应保留")
	}
}

// ──────────────────────────── 列表处理 ────────────────────────────

// TestReviewerUIMetadata_列表项 测试列表键处理。
func TestReviewerUIMetadata_列表项(t *testing.T) {
	interaction := map[string]any{
		"metadata": map[string]any{
			"remaining_forbidden_actions": []any{"删除文件", "推送远端"},
		},
	}
	got := reviewerUIMetadataFromInteraction(interaction, map[string]any{})
	items, ok := got["remaining_forbidden_actions"].([]any)
	if !ok {
		t.Fatalf("类型 = %T, want []any", got["remaining_forbidden_actions"])
	}
	if len(items) != 2 {
		t.Errorf("len = %d, want 2", len(items))
	}
}

// TestReviewerUIMetadata_列表超8条加TRUNCATED 测试截断标记。
func TestReviewerUIMetadata_列表超8条加TRUNCATED(t *testing.T) {
	items := make([]any, 12)
	for i := range items {
		items[i] = "动作"
	}
	interaction := map[string]any{
		"metadata": map[string]any{"remaining_forbidden_actions": items},
	}
	got := reviewerUIMetadataFromInteraction(interaction, map[string]any{})
	result, _ := got["remaining_forbidden_actions"].([]any)
	if len(result) != reviewerUIMaxListItems+1 {
		t.Fatalf("len = %d, want %d（8 条 + TRUNCATED）", len(result), reviewerUIMaxListItems+1)
	}
	if result[len(result)-1] != reviewerUITruncatedMarker {
		t.Errorf("末项 = %v, want [TRUNCATED]", result[len(result)-1])
	}
}

// TestReviewerUIMetadata_列表非字符串项被过滤 测试类型过滤。
func TestReviewerUIMetadata_列表非字符串项被过滤(t *testing.T) {
	interaction := map[string]any{
		"metadata": map[string]any{
			"remaining_forbidden_actions": []any{"有效", 42, "", "  ", nil},
		},
	}
	got := reviewerUIMetadataFromInteraction(interaction, map[string]any{})
	result, _ := got["remaining_forbidden_actions"].([]any)
	if len(result) != 1 {
		t.Errorf("len = %d, want 1（只保留非空字符串）", len(result))
	}
}

// TestReviewerUIMetadata_列表类型不符跳过 测试非 list 值被跳过。
func TestReviewerUIMetadata_列表类型不符跳过(t *testing.T) {
	interaction := map[string]any{
		"metadata": map[string]any{"remaining_forbidden_actions": "not_a_list"},
	}
	got := reviewerUIMetadataFromInteraction(interaction, map[string]any{})
	if _, exists := got["remaining_forbidden_actions"]; exists {
		t.Error("非列表值应被跳过")
	}
}

// TestReviewerUIMetadata_列表全空不输出 测试过滤后为空则不出键。
func TestReviewerUIMetadata_列表全空不输出(t *testing.T) {
	interaction := map[string]any{
		"metadata": map[string]any{"remaining_forbidden_actions": []any{"", "  "}},
	}
	got := reviewerUIMetadataFromInteraction(interaction, map[string]any{})
	if _, exists := got["remaining_forbidden_actions"]; exists {
		t.Error("过滤后为空时不应输出该键")
	}
}

// ──────────────────────────── 脱敏与截断 ────────────────────────────

// TestReviewerUIMetadata_文本脱敏 测试密钥被替换。
func TestReviewerUIMetadata_文本脱敏(t *testing.T) {
	interaction := map[string]any{
		"metadata": map[string]any{
			"evidence_summary": "api_key = sk-abcdef1234567890abcdef",
		},
	}
	got := reviewerUIMetadataFromInteraction(interaction, map[string]any{})
	text, _ := got["evidence_summary"].(string)
	if strings.Contains(text, "sk-abcdef1234567890abcdef") {
		t.Errorf("got %q，密钥应被脱敏", text)
	}
}

// TestReviewerUIMetadata_文本截断到512 测试长度上限。
func TestReviewerUIMetadata_文本截断到512(t *testing.T) {
	long := strings.Repeat("字", 1000)
	interaction := map[string]any{
		"metadata": map[string]any{"evidence_summary": long},
	}
	got := reviewerUIMetadataFromInteraction(interaction, map[string]any{})
	text, _ := got["evidence_summary"].(string)
	if len([]rune(text)) > reviewerUIMaxTextLength {
		t.Errorf("rune len = %d, want <= %d", len([]rune(text)), reviewerUIMaxTextLength)
	}
}

// TestReviewerUIMetadata_空输入返回空map 测试无数据。
func TestReviewerUIMetadata_空输入返回空map(t *testing.T) {
	got := reviewerUIMetadataFromInteraction(map[string]any{}, map[string]any{})
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}
