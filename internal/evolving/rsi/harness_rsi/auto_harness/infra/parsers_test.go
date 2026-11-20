package infra

import (
	"strings"
	"testing"

	autoharness "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/auto_harness"
)

// TestParseTasks_围栏与裸数组
func TestParseTasks_围栏与裸数组(t *testing.T) {
	fenced := "前缀说明\n```json\n[{\"topic\":\"t1\",\"description\":\"d\",\"files\":[\"a.py\"],\"expected_effect\":\"e\",\"pipeline_name\":\"pr_pipeline\"}]\n```\n后缀"
	tasks := ParseTasks(fenced)
	if len(tasks) != 1 || tasks[0].Topic != "t1" || tasks[0].PipelineName != autoharness.MetaEvolvePipeline {
		t.Fatalf("围栏解析 = %+v", tasks)
	}
	if tasks[0].Status != autoharness.TaskStatusPending {
		t.Fatalf("默认状态 = %v", tasks[0].Status)
	}
	bare := `[{"topic":"t2"},{"nope":1},{"topic":""}]`
	tasks2 := ParseTasks(bare)
	if len(tasks2) != 1 || tasks2[0].Topic != "t2" {
		t.Fatalf("裸数组解析 = %+v", tasks2)
	}
	if got := ParseTasks("no json here"); len(got) != 0 {
		t.Fatalf("无 JSON 应为空: %v", got)
	}
	if got := ParseTasks("```json\n[bad json\n```"); len(got) != 0 {
		t.Fatalf("坏 JSON 应为空: %v", got)
	}
}

// TestParseLearnings
func TestParseLearnings(t *testing.T) {
	raw := "```json\n[{\"topic\":\"a\",\"summary\":\"s\"},{\"x\":1},\"str\"]\n```"
	items := ParseLearnings(raw)
	if len(items) != 1 || items[0]["topic"] != "a" {
		t.Fatalf("learnings = %v", items)
	}
	if got := ParseLearnings("{\"not\":\"array\"}"); got != nil {
		t.Fatalf("非数组输入 = %v", got)
	}
}

// TestParsePRDraft_严格与宽松 验证三层解析链
func TestParsePRDraft_严格与宽松(t *testing.T) {
	// 严格 JSON
	ok := "```json\n{\"title\":\"fix x\",\"kind\":\"bug\",\"body\":\"## 描述\"}\n```"
	draft, errStr := ParsePRDraftWithError(ok)
	if draft == nil || errStr != "" || draft.Title != "fix x" || draft.Kind != "bug" {
		t.Fatalf("严格解析 = %v %q", draft, errStr)
	}
	// /kind 从 body 提取
	nok := "{\"title\":\"t\",\"body\":\"/kind feature\\nbody\"}"
	draft2, _ := ParsePRDraftWithError(nok)
	if draft2 == nil || draft2.Kind != "feature" {
		t.Fatalf("body /kind = %v", draft2)
	}
	// 宽松：body 含未转义引号与围栏
	loose := "```json\n{\"title\":\"loose\",\"kind\":\"refactor\",\"body\":\"line1 \\\"quoted\\\" \n```code```\nend\"}\n```"
	draft3, errStr3 := ParsePRDraftWithError(loose)
	if draft3 == nil {
		t.Fatalf("宽松解析失败: %q", errStr3)
	}
	// 缺 body → 错误
	miss, errStr4 := ParsePRDraftWithError("{\"title\":\"only\"}")
	if miss != nil || errStr4 != "缺少 title 或 body" {
		t.Fatalf("缺字段 = %v %q", miss, errStr4)
	}
	// 非法 kind
	bad, errStr5 := ParsePRDraftWithError("{\"title\":\"t\",\"kind\":\"zzz\",\"body\":\"b\"}")
	if bad != nil || errStr5 != "kind 必须是 bug/task/feature/refactor/clean_code 之一" {
		t.Fatalf("非法 kind = %v %q", bad, errStr5)
	}
	// 完全不可解析
	none, errStr6 := ParsePRDraftWithError("hello world no json")
	if none != nil || errStr6 == "" {
		t.Fatalf("不可解析 = %v %q", none, errStr6)
	}
	if ParsePRDraft("bad") != nil {
		t.Fatal("ParsePRDraft 包装应返回 nil")
	}
}

// TestParsePipelineSelection
func TestParsePipelineSelection(t *testing.T) {
	raw := "```json\n{\"pipeline_name\":\"meta\",\"reason\":\"r\",\"alternatives\":[\"extended\",\"junk\"],\"confidence\":0.8,\"risk_level\":\"low\",\"required_inputs\":[\"a\"],\"fallback_pipeline\":\"\"}\n```"
	sel := ParsePipelineSelection(raw)
	// 注意：Python normalize_pipeline_name 为精确匹配别名，"meta" 原样返回
	// （"meta"→meta_evolve_pipeline 的映射只存在于 normalize_pipeline_preference）
	if sel == nil || sel.PipelineName != "meta" ||
		len(sel.Alternatives) != 2 || sel.Alternatives[0] != "extended" ||
		sel.Confidence != 0.8 || sel.FallbackPipeline != "" {
		t.Fatalf("解析 = %+v", sel)
	}
	if got := ParsePipelineSelection("{\"pipeline_name\":\"\"}"); got != nil {
		t.Fatalf("空名 = %v", got)
	}
	if got := ParsePipelineSelection("no object"); got != nil {
		t.Fatalf("无对象 = %v", got)
	}
}

// TestExtractText
func TestExtractText(t *testing.T) {
	if got := ExtractText(map[string]any{"payload": map[string]any{"content": "c"}}); got != "c" {
		t.Fatalf("content = %q", got)
	}
	if got := ExtractText(map[string]any{"payload": map[string]any{"output": "o"}}); got != "o" {
		t.Fatalf("output 兜底 = %q", got)
	}
	if got := ExtractText(map[string]any{"payload": "raw"}); got != "raw" {
		t.Fatalf("字符串 payload = %q", got)
	}
	if got := ExtractText(map[string]any{}); got != "" {
		t.Fatalf("无 payload = %q", got)
	}
}

// TestParseGaps 验证表格解析、表头跳过、优先级排序
func TestParseGaps(t *testing.T) {
	raw := strings.Join([]string{
		"| competitor | feature | current_state | gap_description | impact | feasibility | suggested_approach | target_files |",
		"|---|---|---|---|---|---|---|---|",
		"| A | feat1 | none | missing | 0.5 | 0.4 | approach | a.py, b.py |",
		"| B | feat2 | weak | slow | 0.9 | 0.8 | redo | c.py |",
		"| not a row | x |",
	}, "\n")
	gaps := ParseGaps(raw)
	if len(gaps) != 2 {
		t.Fatalf("gaps = %+v", gaps)
	}
	// 0.72 > 0.2 → B 在前
	if gaps[0].Competitor != "B" {
		t.Fatalf("排序 = %+v", gaps)
	}
	if len(gaps[0].TargetFiles) != 1 || gaps[0].TargetFiles[0] != "c.py" {
		t.Fatalf("target_files = %v", gaps[0].TargetFiles)
	}
	if gaps[0].ID == "" || len(gaps[0].ID) != 8 {
		t.Fatalf("ID = %q", gaps[0].ID)
	}
	// 竞品表头变体
	gaps2 := ParseGaps("| 竞品 | f | c | g | 1 | 1 | s | t |")
	if len(gaps2) != 0 {
		t.Fatalf("中文表头应跳过: %v", gaps2)
	}
}

// TestParseExtensionDesigns 新旧格式
func TestParseExtensionDesigns(t *testing.T) {
	newFmt := "{\"package_name\":\" my_pkg \",\"designs\":[{\"extension_name\":\"e1\",\"kind\":\"constraint\",\"depends_on\":[\"x\"],\"applies_to\":[\"y\"],\"components\":[\"c\"],\"file_plan\":{\"f\":\"c\"},\"harness_config_patch\":{\"k\":1},\"skill_source\":\"s\"}]}"
	pkg, designs := ParseExtensionDesigns(newFmt)
	if pkg != "my_pkg" || len(designs) != 1 || designs[0].Kind != "constraint" ||
		len(designs[0].DependsOn) != 1 || designs[0].FilePlan["f"] != "c" {
		t.Fatalf("新格式 = %q %+v", pkg, designs)
	}
	// 非法 kind 回落 capability
	_, d2 := ParseExtensionDesigns("[{\"extension_name\":\"e2\",\"kind\":\"weird\"}]")
	if d2[0].Kind != "capability" {
		t.Fatalf("非法 kind = %q", d2[0].Kind)
	}
	// 单对象旧格式
	pkg3, d3 := ParseExtensionDesigns("{\"extension_name\":\"e3\"}")
	if pkg3 != "" || len(d3) != 1 {
		t.Fatalf("单对象 = %q %v", pkg3, d3)
	}
	// 无 extension_name 跳过
	_, d4 := ParseExtensionDesigns("[{\"gap_id\":\"g1\"}]")
	if len(d4) != 0 {
		t.Fatalf("无名应跳过: %v", d4)
	}
	// 坏 JSON
	if p, d := ParseExtensionDesigns("[bad"); p != "" || len(d) != 0 {
		t.Fatalf("坏 JSON = %q %v", p, d)
	}
}

// TestRawDecodeJSONObject_围栏内嵌套 验证不用正则截断的解码链
func TestRawDecodeJSONObject_围栏内嵌套(t *testing.T) {
	// JSON 字符串中含 ``` 围栏——非贪婪 regex 会截断，raw_decode 不应
	raw := "```json\n{\"body\":\"has ``` fence inside\"}\n```"
	item, errStr := decodeJSONObjectFromMarkdown(raw)
	if item == nil || errStr != "" || !strings.Contains(item["body"].(string), "fence") {
		t.Fatalf("raw_decode = %v %q", item, errStr)
	}
	// 前置噪声对象失败后跳到真正的对象
	item2, _ := rawDecodeJSONObject("garbage {broken {\"ok\":1}", 0)
	if item2 == nil || item2["ok"] == nil {
		t.Fatalf("跳过损坏对象 = %v", item2)
	}
	// 无对象
	got, msg := rawDecodeJSONObject("no braces", 0)
	if got != nil || msg != "未找到 JSON 对象" {
		t.Fatalf("无对象 = %v %q", got, msg)
	}
}

// TestUnescapeLooseJSONString
func TestUnescapeLooseJSONString(t *testing.T) {
	got := unescapeLooseJSONString(`a\r\nb\tc\"d\\e`)
	if got != "a\nb\tc\"d\\e" {
		t.Fatalf("反转义 = %q", got)
	}
}
