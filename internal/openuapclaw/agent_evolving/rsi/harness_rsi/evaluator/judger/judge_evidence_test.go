package judger

// Task2 五件的单测：judge_evidence / judge_diagnostics / judge_search /
// evidence_guard / direct_evidence 关键分支。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dataloader "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/data_loader"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// TestWriteJudgeJSON_落盘缩进与父目录创建 覆盖 WriteJudgeJSON/WriteJudgeEvent。
func TestWriteJudgeJSON_落盘缩进与父目录创建(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "out.json")
	if err := WriteJudgeJSON(path, map[string]any{"k": "v"}); err != nil {
		t.Fatalf("write err: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "\n  \"k\": \"v\"") {
		t.Fatalf("应 indent=2 序列化: %s", data)
	}
	eventPath := filepath.Join(dir, "events.jsonl")
	if err := WriteJudgeEvent(eventPath, map[string]any{"a": 1}); err != nil {
		t.Fatalf("event err: %v", err)
	}
	if err := WriteJudgeEvent(eventPath, map[string]any{"a": 2}); err != nil {
		t.Fatalf("append err: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(readFileT(t, eventPath)), "\n")
	if len(lines) != 2 {
		t.Fatalf("event 应追加两行, got %d", len(lines))
	}
}

// TestResponseEvidence_长文本分页 对齐 _response_evidence 分页分支。
func TestResponseEvidence_长文本分页(t *testing.T) {
	workspace := t.TempDir()
	long := strings.Repeat("甲乙丙丁", 5000) // 20000 字符 > 12000 上限
	response, pages, err := responseEvidence(long, workspace)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	m, ok := response.(map[string]any)
	if !ok || m["original_json"] != "response/original.json" {
		t.Fatalf("超长应返回分页结构, got %T", response)
	}
	pagesList, _ := m["pages"].([]any)
	if len(pagesList) < 2 {
		t.Fatalf("应分多页, got %d", len(pagesList))
	}
	if len(pages) != len(pagesList) {
		t.Fatalf("返回分页清单与 pages 数不符")
	}
	// 无损性：拼接全部分页应还原原文。
	var rebuilt strings.Builder
	for _, p := range pagesList {
		pm := p.(map[string]any)
		data, _ := os.ReadFile(filepath.Join(workspace, pm["path"].(string)))
		rebuilt.Write(data)
	}
	if rebuilt.String() != long {
		t.Fatalf("分页应无损还原原文")
	}
	// original.json 落盘校验。
	if !fileExists(filepath.Join(workspace, "response", "original.json")) {
		t.Fatalf("original.json 应落盘")
	}
}

// TestPrepareJudgeWorkspace_本地快照 对齐 prepare_judge_workspace 主链。
func TestPrepareJudgeWorkspace_本地快照(t *testing.T) {
	caseDir := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "evidence")
	if err := os.MkdirAll(filepath.Join(caseDir, "artifacts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "artifacts", "a.txt"), []byte("artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	caseMapping := map[string]any{
		"case_id":   "c1",
		"case_path": filepath.Join(caseDir, "case.json"),
		"request":   "题目",
		"input":     "把这句话分类",
		"reference": map[string]any{"answer": "参考答案"},
	}
	task, err := dataloader.TaskInput(caseMapping)
	if err != nil {
		t.Fatalf("task input err: %v", err)
	}
	behaviors, forbidden, err := ScoringContract(caseMapping)
	if err != nil {
		t.Fatalf("contract err: %v", err)
	}
	err = PrepareJudgeWorkspace(PrepareJudgeWorkspaceParams{
		Case: caseMapping, Task: task, Response: "我的回答",
		CaseDir: caseDir, Workspace: workspace,
		Behaviors: behaviors, Forbidden: forbidden,
	})
	if err != nil {
		t.Fatalf("prepare err: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(workspace, "request.json"))
	var request map[string]any
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatalf("request.json 解析失败: %v", err)
	}
	if request["reference_answer"] != "参考答案" {
		t.Fatalf("reference_answer 应写入, got %v", request["reference_answer"])
	}
	if request["response"] != "我的回答" {
		t.Fatalf("response 应内联, got %v", request["response"])
	}
	if _, ok := request["judge_protocol"]; !ok {
		t.Fatalf("judge_protocol 应写入")
	}
	if !fileExists(filepath.Join(workspace, "artifacts", "a.txt")) {
		t.Fatalf("artifacts 应拷入工作区")
	}
	manifestPath := filepath.Join(filepath.Dir(workspace), "artifact_manifest.json")
	if !fileExists(manifestPath) {
		t.Fatalf("artifact_manifest.json 应落盘")
	}
}

// TestCollectJudgeModelCalls_诊断采集 对齐 judge_diagnostics。
func TestCollectJudgeModelCalls_诊断采集(t *testing.T) {
	ctx, calls, cancel := CollectJudgeModelCalls(context.Background())
	defer cancel()
	RecordJudgeModelResponse(ctx, "stop", &llmschema.UsageMetadata{
		InputTokens: 10, OutputTokens: 5, TotalTokens: 15,
	})
	RecordJudgeModelResponse(ctx, llmschema.FinishReasonNull, nil)
	RecordJudgeModelResponse(context.Background(), "stop", nil) // 未开启采集忽略
	if len(calls.Items) != 1 {
		t.Fatalf("应只采集一次（null finish 忽略）, got %d", len(calls.Items))
	}
	if calls.Items[0]["finish_reason"] != "stop" {
		t.Fatalf("finish_reason 应记录, got %v", calls.Items[0])
	}
	usage := calls.Items[0]["usage"].(map[string]any)
	if usage["total_tokens"] != float64(15) {
		t.Fatalf("usage.total_tokens 应为 15, got %v", usage)
	}
}

// TestBoundedText_信封不超限 对齐 bounded_text 的 90% 递减重试。
func TestBoundedText_信封不超限(t *testing.T) {
	long := strings.Repeat("x", ToolBytes*3)
	out, err := BoundedText(long, 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out) > ToolBytes {
		t.Fatalf("信封应不超过 TOOL_BYTES, got %d", len(out))
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("应为 JSON 信封: %v", err)
	}
	if envelope["truncated"] != true || envelope["total_bytes"] != float64(len(long)) {
		t.Fatalf("截断标记不符: %v", envelope)
	}
	if envelope["next_byte_offset"].(float64) <= 0 {
		t.Fatalf("next_byte_offset 应为正")
	}
}

// TestBoundToolContent_预览不宣告偏移 对齐 bound_tool_content。
func TestBoundToolContent_预览不宣告偏移(t *testing.T) {
	out := BoundToolContent(strings.Repeat("y", ToolBytes+100))
	s, ok := out.(string)
	if !ok {
		t.Fatalf("超长字符串应替换为预览信封, got %T", out)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(s), &envelope); err != nil {
		t.Fatalf("应为 JSON: %v", err)
	}
	if envelope["truncated"] != true || envelope["preview"] == nil {
		t.Fatalf("预览信封字段缺失: %v", envelope)
	}
	if strings.Contains(s, "next_byte_offset") {
		t.Fatalf("预览不得宣告文件偏移")
	}
	short := BoundToolContent("短内容")
	if s, ok := short.(string); !ok || s != "短内容" {
		t.Fatalf("短内容应原样保留")
	}
}

// TestJudgeEvidenceTool_读取分页与越界 对齐 JudgeEvidenceTool._read 分支。
func TestJudgeEvidenceTool_读取分页与越界(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "e.txt"), []byte(strings.Repeat("ab", ToolBytes)), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewJudgeEvidenceTool(workspace, "judge1")
	if tool.Card().ID != "judge1.read_evidence" || tool.Card().Name != "read_evidence" {
		t.Fatalf("卡片 ID/Name 不符: %v", tool.Card())
	}
	result, err := tool.Invoke(context.Background(), map[string]any{"path": "e.txt"})
	if err != nil {
		t.Fatalf("invoke err: %v", err)
	}
	content := result["content"].(string)
	var envelope map[string]any
	if err := json.Unmarshal([]byte(content), &envelope); err != nil {
		t.Fatalf("应为信封: %v", err)
	}
	if envelope["truncated"] != true {
		t.Fatalf("超限文件应截断")
	}
	// 越界路径拒绝。
	if _, err := tool.Invoke(context.Background(), map[string]any{"path": "../outside.txt"}); err == nil {
		t.Fatalf("越界路径应拒绝")
	}
	// 缺 path 拒绝。
	if _, err := tool.Invoke(context.Background(), map[string]any{}); err == nil {
		t.Fatalf("缺 path 应拒绝")
	}
}

// TestJudgeSearchTool_字面量匹配与限制 对齐 JudgeSearchTool._search。
func TestJudgeSearchTool_字面量匹配与限制(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("hello world\nfoo bar\nhello again"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewJudgeSearchTool(workspace, "judge1")
	if tool.Card().ID != "judge1.grep" || tool.Card().Name != "grep" {
		t.Fatalf("卡片 ID/Name 不符: %v", tool.Card())
	}
	result, err := tool.Invoke(context.Background(), map[string]any{"pattern": "hello"})
	if err != nil {
		t.Fatalf("invoke err: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result["content"].(string)), &payload); err != nil {
		t.Fatalf("应为 JSON: %v", err)
	}
	if payload["scan_complete"] != true {
		t.Fatalf("常规扫描应完整: %v", payload["limitations"])
	}
	matches := payload["matches"].([]any)
	if len(matches) != 2 {
		t.Fatalf("应命中两行, got %d", len(matches))
	}
	first := matches[0].(map[string]any)
	if first["path"] != "a.txt" || first["line"] != float64(1) {
		t.Fatalf("首个匹配应为 a.txt:1, got %v", first)
	}
	// pattern 校验。
	if _, err := tool.Invoke(context.Background(), map[string]any{"pattern": ""}); err == nil {
		t.Fatalf("空 pattern 应拒绝")
	}
	// 忽略大小写。
	result2, _ := tool.Invoke(context.Background(), map[string]any{"pattern": "HELLO", "ignore_case": true})
	var payload2 map[string]any
	_ = json.Unmarshal([]byte(result2["content"].(string)), &payload2)
	if len(payload2["matches"].([]any)) != 2 {
		t.Fatalf("ignore_case 应同样命中两行")
	}
}

// TestInlineEvidence_内联与超限 对齐 direct_evidence。
func TestInlineEvidence_内联与超限(t *testing.T) {
	workspace := t.TempDir()
	request := map[string]any{
		"evidence_files": []any{"a.txt"},
	}
	if err := os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("内容A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteJudgeJSON(filepath.Join(workspace, "request.json"), request); err != nil {
		t.Fatal(err)
	}
	content := InlineEvidence(workspace, MaxInlineBytes, false, false)
	payload, ok := content.(string)
	if !ok {
		t.Fatalf("无图片应返回字符串, got %T", content)
	}
	var inlined map[string]any
	if err := json.Unmarshal([]byte(payload), &inlined); err != nil {
		t.Fatalf("应为 JSON: %v", err)
	}
	if inlined["evidence_files"].(map[string]any)["a.txt"] != "内容A" {
		t.Fatalf("证据文件应全文内联, got %v", inlined["evidence_files"])
	}
	if !strings.Contains(inlined["evidence_note"].(string), "untrusted data") {
		t.Fatalf("evidence_note 应为直取版文案")
	}
	// 超限拒绝。
	if got := InlineEvidence(workspace, 10, false, false); got != nil {
		t.Fatalf("非 required 超限应返回 nil, got %v", got)
	}
	if got := InlineEvidence(workspace, 10, true, false); got == nil ||
		!strings.Contains(got.(error).Error(), "Judge closeout unavailable") {
		t.Fatalf("required 超限应升级报错, got %v", got)
	}
}

// TestJudgeProtocolIdentity_两种模式 对齐 judge_protocol_identity。
func TestJudgeProtocolIdentity_两种模式(t *testing.T) {
	identity, err := JudgeProtocolIdentity("binary", true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if identity["evidence_layout"] != "complete_evidence_closeout_v7" {
		t.Fatalf("布局版本不符: %v", identity)
	}
	if _, hasBaseline := identity["baseline_comparison"]; hasBaseline {
		t.Fatalf("binary 模式不应带 baseline_comparison")
	}
	continuous, err := JudgeProtocolIdentity("continuous", true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if continuous["baseline_comparison"] != "baseline_reference_v1" {
		t.Fatalf("continuous+baseline 应带标记: %v", continuous)
	}
	if continuous["prompt_sha256"] == identity["prompt_sha256"] {
		t.Fatalf("两种模式的提示词哈希应不同")
	}
}

// readFileT 测试读文件助手。
func readFileT(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
