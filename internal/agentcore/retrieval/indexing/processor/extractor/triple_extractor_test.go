package extractor

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// stubTripleClient 惰性 mock：所有方法零行为（captioner_test.go stubCaptionClient 先例）
type stubTripleClient struct{}

func (s *stubTripleClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return llmschema.NewAssistantMessage(""), nil
}
func (s *stubTripleClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, errors.New("not implemented")
}
func (s *stubTripleClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubTripleClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubTripleClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubTripleClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubTripleClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (s *stubTripleClient) SupportsKVCacheRelease() bool { return false }

// newTripleTestModel 构造带 stub 客户端的 Model（WithClient 注入，走真实 Invoke 链）
func newTripleTestModel(t *testing.T) *llm.Model {
	t.Helper()
	clientCfg, err := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	if err != nil {
		t.Fatalf("构造客户端配置失败: %v", err)
	}
	model, err := llm.NewModel(clientCfg, llmschema.NewModelRequestConfig(), llm.WithClient(&stubTripleClient{}))
	if err != nil {
		t.Fatalf("构造 Model 失败: %v", err)
	}
	return model
}

// TestTripleExtractor_构造 测试初始化（对齐 test_init）
func TestTripleExtractor_构造(t *testing.T) {
	model := newTripleTestModel(t)
	ex := NewTripleExtractor(model, "test-model",
		WithTripleTemperature(0.0), WithTripleMaxConcurrent(10))
	if ex.llmClient != model {
		t.Error("llmClient 应为构造传入的 model")
	}
	if ex.modelName != "test-model" {
		t.Errorf("modelName 应为 test-model，实际 %s", ex.modelName)
	}
	if ex.temperature != 0.0 {
		t.Errorf("temperature 应为 0.0，实际 %v", ex.temperature)
	}
	if cap(ex.limiter) != 10 {
		t.Errorf("limiter 容量应为 10，实际 %d", cap(ex.limiter))
	}
}

// TestTripleExtractor_默认值构造 测试默认构造（对齐 test_init_with_defaults）
func TestTripleExtractor_默认值构造(t *testing.T) {
	ex := NewTripleExtractor(newTripleTestModel(t), "test-model")
	if ex.temperature != 0.0 {
		t.Errorf("默认 temperature 应为 0.0，实际 %v", ex.temperature)
	}
	if cap(ex.limiter) != 50 {
		t.Errorf("默认 limiter 容量应为 50，实际 %d", cap(ex.limiter))
	}
}

// ──────────────────────────── buildPrompt 测试 ────────────────────────────

// TestBuildPrompt_标题为空 测试空标题回落 Untitled（Go 增强 §7.2-4）
func TestBuildPrompt_标题为空(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	prompt := e.buildPrompt("Alice knows Bob", "")
	if !strings.Contains(prompt, "Title:\nUntitled\n") {
		t.Error("空标题应渲染为 Untitled")
	}
	if !strings.Contains(prompt, "Passage:\nAlice knows Bob") {
		t.Error("passage 应原样渲染")
	}
}

// TestBuildPrompt_非空标题 测试非空标题渲染
func TestBuildPrompt_非空标题(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	prompt := e.buildPrompt("body text", "My Title")
	if !strings.Contains(prompt, "Title:\nMy Title\n") {
		t.Error("非空标题应原样渲染")
	}
}

// TestBuildPrompt_花括号安全 测试 passage 含 { 特殊字符不破坏 Replacer（Go 增强 §7.2-4）
func TestBuildPrompt_花括号安全(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	prompt := e.buildPrompt("var x = {a: 1}; func() { return 2; }", "T")
	// 花括号原样保留（NewReplacer 只替换 {title}/{passage} 字面量，
	// passage 内部花括号替换后注入不参与二次解析）
	if !strings.Contains(prompt, "var x = {a: 1}") {
		t.Error("passage 花括号应原样保留")
	}
	if !strings.Contains(prompt, "Title:\nT\n") {
		t.Error("标题替换应不受 passage 花括号影响")
	}
}

// TestBuildPrompt_模板完整性 测试模板关键段落齐备（Go 增强：防逐字复制走样）
func TestBuildPrompt_模板完整性(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	prompt := e.buildPrompt("p", "t")
	for _, want := range []string{
		"# Instruction",
		"# Demonstration 1",
		"# Demonstration 2",
		"Magic Johnson",
		"Elden Ring",
		`"named_entities"`,
		`"triples"`,
		`If no triples are found, return {"named_entities": [...], "triples": []}`,
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("模板应包含 %q", want)
		}
	}
}

// ──────────────────────────── parseTriples 测试（Python 7 例 1:1）────────────────────────────

// TestParseTriples_顶层数组 对齐 test_parse_triples_json_array
func TestParseTriples_顶层数组(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`[["a", "b", "c"]]`, "d1", "c1")
	if !ok || len(triples) != 1 {
		t.Fatalf("ok=%v len=%d，应为 true/1", ok, len(triples))
	}
	if triples[0].Subject != "a" || triples[0].Predicate != "b" || triples[0].Object != "c" {
		t.Errorf("三元组字段断言失败: %+v", triples[0])
	}
}

// TestParseTriples_多余字段截取 对齐 test_parse_triples_extra_fields_ignored
func TestParseTriples_多余字段截取(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`[["a", "b", "c", "ignored", 99]]`, "d1", "c1")
	if !ok {
		t.Fatal("应为 true")
	}
	if triples[0].Object != "c" {
		t.Errorf("Object 应截取前三项为 c，实际 %s", triples[0].Object)
	}
}

// TestParseTriples_包裹dict 对齐 test_parse_triples_wrapped_dict
func TestParseTriples_包裹dict(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`{"triples": [["x", "y", "z"]]}`, "d1", "c1")
	if !ok || len(triples) != 1 {
		t.Fatalf("ok=%v len=%d，应为 true/1", ok, len(triples))
	}
	if triples[0].Subject != "x" {
		t.Errorf("Subject 应为 x，实际 %s", triples[0].Subject)
	}
}

// TestParseTriples_含named_entities 对齐 test_parse_triples_prompt_shape
func TestParseTriples_含named_entities(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`{"named_entities": ["Alice", "Bob"], "triples": [["Alice", "knows", "Bob"]]}`, "d1", "c1")
	if !ok || len(triples) != 1 {
		t.Fatalf("ok=%v len=%d，应为 true/1", ok, len(triples))
	}
	if triples[0].Subject != "Alice" {
		t.Errorf("Subject 应为 Alice，实际 %s", triples[0].Subject)
	}
}

// TestParseTriples_缺triples键 对齐 test_parse_triples_missing_triples_key_fails
func TestParseTriples_缺triples键(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`{"named_entities": ["Alice", "Bob"]}`, "d1", "c1")
	if ok {
		t.Fatal("缺 triples 键应为 false")
	}
	if len(triples) != 0 {
		t.Errorf("失败时应返回空列表，实际 %d", len(triples))
	}
}

// TestParseTriples_invalid混合 对齐 test_parse_triples_invalid_items_ignored
func TestParseTriples_invalid混合(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`{"triples": [["a", "b", "c"], ["x"], {"bad": 1}, ["y", ["nested"], "z"]]}`, "d1", "c1")
	if !ok {
		t.Fatal("混合场景应 ok=true（有 1 条合格）")
	}
	if len(triples) != 1 {
		t.Fatalf("应保留 1 条合格三元组，实际 %d", len(triples))
	}
	if triples[0].Subject != "a" {
		t.Errorf("唯一合格项 Subject 应为 a，实际 %s", triples[0].Subject)
	}
}

// TestParseTriples_全invalid 对齐 test_parse_triples_all_invalid_fails
func TestParseTriples_全invalid(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`{"triples": [["x"], {"bad": 1}]}`, "d1", "c1")
	if ok {
		t.Fatal("全 invalid 应为 false")
	}
	if len(triples) != 0 {
		t.Errorf("失败时应返回空列表，实际 %d", len(triples))
	}
}

// ──────────────────────────── parseTriples Go 增强 ────────────────────────────

// TestParseTriples_metadata注入 测试 doc_id/chunk_id 元数据注入（Go 增强）
func TestParseTriples_metadata注入(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`[["a", "b", "c"]]`, "d1", "c1")
	if !ok || len(triples) != 1 {
		t.Fatalf("ok=%v len=%d", ok, len(triples))
	}
	if triples[0].Metadata["doc_id"] != "d1" || triples[0].Metadata["chunk_id"] != "c1" {
		t.Errorf("Metadata 应注入 doc_id/chunk_id，实际 %v", triples[0].Metadata)
	}
}

// TestParseTriples_fence剥离 测试 3 行 markdown fence 剥离（Go 增强 §7.2-6）
func TestParseTriples_fence剥离(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	content := "```json\n{\"triples\": [[\"a\", \"b\", \"c\"]]}\n```"
	triples, ok := e.parseTriples(content, "d1", "c1")
	if !ok || len(triples) != 1 {
		t.Fatalf("fence 剥离后应解析成功，ok=%v len=%d", ok, len(triples))
	}
}

// TestParseTriples_fence两行不剥 测试 2 行 fence 不剥（Go 增强 §7.2-6）
func TestParseTriples_fence两行不剥(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	// len(lines)==2 不满足 >2，走原文 → 修复器无法解析 → false
	_, ok := e.parseTriples("```\n```", "d1", "c1")
	if ok {
		t.Fatal("2 行 fence 不剥离应解析失败")
	}
}

// TestParseTriples_极坏输入 测试空串/纯文本输入（Go 增强 §7.2-7）
func TestParseTriples_极坏输入(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	for _, content := range []string{"", "Invalid JSON response", "   "} {
		if _, ok := e.parseTriples(content, "d1", "c1"); ok {
			t.Errorf("极坏输入 %q 不应解析成功", content)
		}
	}
}

// TestParseTriples_空triples列表 测试空列表返回 true（Python ([], True) 分支）
func TestParseTriples_空triples列表(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`{"triples": []}`, "d1", "c1")
	if !ok {
		t.Fatal("空 triples 列表应为 true（Python ([], True)）")
	}
	if len(triples) != 0 {
		t.Errorf("应为空列表，实际 %d", len(triples))
	}
}

// TestParseTriples_字段strip 测试 str(x).strip() 两步语义（Go 增强）
func TestParseTriples_字段strip(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`[[" a ", " b ", " c "]]`, "d1", "c1")
	if !ok || len(triples) != 1 {
		t.Fatalf("ok=%v len=%d", ok, len(triples))
	}
	if triples[0].Subject != "a" || triples[0].Predicate != "b" || triples[0].Object != "c" {
		t.Errorf("三字段应 strip，实际 %+v", triples[0])
	}
}

// TestParseTriples_数字类型字段 测试数字字段经 pyStr 字符串化（Go 增强 §7.2-5）
//
// 合法 JSON 主路径：jsonrepair 短路解码产出 json.Number——pyStr 整数字面
// 原样输出（对齐 Python json.loads int → str）
func TestParseTriples_数字类型字段(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`[["Magic Johnson", "draft pick number", 1]]`, "d1", "c1")
	if !ok || len(triples) != 1 {
		t.Fatalf("ok=%v len=%d", ok, len(triples))
	}
	if triples[0].Object != "1" {
		t.Errorf("数字 1 应字符串化为 \"1\"，实际 %q", triples[0].Object)
	}
}

// TestParseTriples_浮点数字段 测试浮点数字段（json.Number → Atoi 失败 →
// pyFloatStr 记法还原，对齐 Python str(1.5)="1.5"）
func TestParseTriples_浮点数字段(t *testing.T) {
	e := NewTripleExtractor(newTripleTestModel(t), "m")
	triples, ok := e.parseTriples(`[["a", "value", 1.5]]`, "d1", "c1")
	if !ok || len(triples) != 1 {
		t.Fatalf("ok=%v len=%d", ok, len(triples))
	}
	if triples[0].Object != "1.5" {
		t.Errorf("数字 1.5 应字符串化为 \"1.5\"，实际 %q", triples[0].Object)
	}
}

// ──────────────────────────── pyStr 测试（Go 增强 §7.2-5）────────────────────────────

// TestPyStr_类型字符串化 对齐 Python str() 逐类型语义
func TestPyStr_类型字符串化(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{1.0, "1.0"},     // float64 整数值补 .0
		{1e-4, "0.0001"}, // f 记法下界
		{1e16, "1e+16"},  // e 记法上界
		{true, "True"},   // Python str(True)
		{false, "False"}, // Python str(False)
		{42, "42"},       // int
		{"abc", "abc"},   // string 原样
		{nil, "None"},    // 不可达防御分支
	}
	for _, c := range cases {
		if got := pyStr(c.in); got != c.want {
			t.Errorf("pyStr(%v) 应为 %q，实际 %q", c.in, c.want, got)
		}
	}
}

// TestPyStr_jsonNumber 对齐合法 JSON 路径的 json.Number 字面输出（探针实测：
// json.Valid 短路路径经 UseNumber 产出 json.Number，三元组内数字以此形态到达）
func TestPyStr_jsonNumber(t *testing.T) {
	cases := []struct {
		in   json.Number
		want string
	}{
		{json.Number("1"), "1"},                       // Python json 解码 int → str(1)="1"
		{json.Number("1.0"), "1.0"},                   // Python float → str(1.0)="1.0"
		{json.Number("1.5"), "1.5"},                   // 最短往返同形
		{json.Number("10000000000000000"), "10000000000000000"}, // int 超界仍整数字面（Python int 无界）
		{json.Number("1e16"), "1e+16"},                // Python float(1e16) → str → "1e+16"
		{json.Number("0.0001"), "0.0001"},
	}
	for _, c := range cases {
		if got := pyStr(c.in); got != c.want {
			t.Errorf("pyStr(%v) 应为 %q，实际 %q", c.in, c.want, got)
		}
	}
}
