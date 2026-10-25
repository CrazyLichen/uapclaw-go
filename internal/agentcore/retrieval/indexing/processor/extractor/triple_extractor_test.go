package extractor

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
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
		{json.Number("1"), "1"},                                 // Python json 解码 int → str(1)="1"
		{json.Number("1.0"), "1.0"},                             // Python float → str(1.0)="1.0"
		{json.Number("1.5"), "1.5"},                             // 最短往返同形
		{json.Number("10000000000000000"), "10000000000000000"}, // int 超界仍整数字面（Python int 无界）
		{json.Number("1e16"), "1e+16"},                          // Python float(1e16) → str → "1e+16"
		{json.Number("0.0001"), "0.0001"},
	}
	for _, c := range cases {
		if got := pyStr(c.in); got != c.want {
			t.Errorf("pyStr(%v) 应为 %q，实际 %q", c.in, c.want, got)
		}
	}
}

// ──────────────────────────── Extract/Process 测试 ────────────────────────────

// captionTripleClient 可编程 mock：按序返回预置响应或错误，记录 Invoke 参数。
//
// 并发安全设计：responses/errs 构造后只读；callCount/current/concurrentPeak
// 走 atomic；lastMessages/lastTemperature 经 mu 保护（多 chunk 并发 Invoke
// 写入不 race，go test -race 下安全）。
type captionTripleClient struct {
	stubTripleClient
	// mu 保护 last* 字段（并发 Invoke 写入）
	mu sync.Mutex
	// responses 按调用序预置响应（content 字段）
	responses []string
	// errs 按调用序预置错误（与 responses 索引对应，优先于 content）
	errs []error
	// callCount Invoke 调用计数
	callCount int32
	// current 当前并发数（进入 +1 / 退出 -1）
	current int32
	// concurrentPeak 并发峰值（CAS 更新）
	concurrentPeak int32
	// lastTemperature 最近一次 Invoke 的温度（NewInvokeParams 提取）
	lastTemperature *float64
	// lastMessages 最近一次 Invoke 的消息
	lastMessages model_clients.MessagesParam
}

// Invoke 按预置返回；耗尽时返回错误。
func (c *captionTripleClient) Invoke(_ context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	cur := atomic.AddInt32(&c.current, 1)
	for {
		peak := atomic.LoadInt32(&c.concurrentPeak)
		if cur <= peak || atomic.CompareAndSwapInt32(&c.concurrentPeak, peak, cur) {
			break
		}
	}
	defer atomic.AddInt32(&c.current, -1)

	idx := int(atomic.AddInt32(&c.callCount, 1)) - 1
	c.mu.Lock()
	c.lastMessages = messages
	c.lastTemperature = model_clients.NewInvokeParams(opts...).Temperature
	c.mu.Unlock()

	if c.errs != nil && idx < len(c.errs) {
		if c.errs[idx] != nil {
			return nil, c.errs[idx]
		}
	}
	if idx < len(c.responses) {
		return llmschema.NewAssistantMessage(c.responses[idx]), nil
	}
	return nil, errors.New("mock responses exhausted")
}

// newExtractTestModelWithMock 构造带可编程 mock 的 Model 并返回 mock 引用。
func newExtractTestModelWithMock(t *testing.T, responses []string, errs []error) (*llm.Model, *captionTripleClient) {
	t.Helper()
	clientCfg, err := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	if err != nil {
		t.Fatalf("构造客户端配置失败: %v", err)
	}
	mock := &captionTripleClient{responses: responses, errs: errs}
	model, err := llm.NewModel(clientCfg, llmschema.NewModelRequestConfig(), llm.WithClient(mock))
	if err != nil {
		t.Fatalf("构造 Model 失败: %v", err)
	}
	return model, mock
}

// TestExtract_多分块 对齐 test_extract_multiple_chunks（invoke 计数=2）
//
// 注意：多 chunk 并发下 triples 顺序 = chunk 序 × 每 chunk 内列表序（方案 A
// 扫描合并），两 chunk 各 2 条共 4 条。
func TestExtract_多分块(t *testing.T) {
	content := `{"triples": [["Alice", "knows", "Bob"], ["Bob", "works_at", "Company"]]}`
	model, mock := newExtractTestModelWithMock(t, []string{content, content}, nil)
	ex := NewTripleExtractor(model, "test-model", WithTripleMaxConcurrent(2))
	chunks := []common.TextChunk{
		{ID: "1", Text: "Alice knows Bob", DocID: "doc_1"},
		{ID: "2", Text: "Charlie knows David", DocID: "doc_1"},
	}
	triples, err := ex.Extract(context.Background(), chunks)
	if err != nil {
		t.Fatalf("Extract 不应报错: %v", err)
	}
	if got := atomic.LoadInt32(&mock.callCount); got != 2 {
		t.Errorf("invoke 应调用 2 次，实际 %d", got)
	}
	if len(triples) != 4 {
		t.Errorf("应抽取 4 条三元组（2 chunk × 2 条），实际 %d", len(triples))
	}
	// 首条应来自 chunk 1（按 chunk 序合并）
	if triples[0].Subject != "Alice" {
		t.Errorf("首条三元组 Subject 应为 Alice（chunk 序），实际 %q", triples[0].Subject)
	}
	// metadata 注入 doc_id/chunk_id
	if triples[0].Metadata["chunk_id"] != "1" || triples[0].Metadata["doc_id"] != "doc_1" {
		t.Errorf("metadata 应注入 doc_id=doc_1 chunk_id=1，实际 %v", triples[0].Metadata)
	}
}

// TestExtract_invoke异常 对齐 test_extract_with_exception（断言 155507 + 消息包含）
func TestExtract_invoke异常(t *testing.T) {
	model, _ := newExtractTestModelWithMock(t, nil, []error{errors.New("429 too many requests")})
	ex := NewTripleExtractor(model, "test-model")
	chunks := []common.TextChunk{{ID: "1", Text: "Alice knows Bob", DocID: "doc_1"}}
	_, err := ex.Extract(context.Background(), chunks)
	if err == nil {
		t.Fatal("invoke 异常应报错")
	}
	var be *exception.BaseError
	if !errors.As(err, &be) {
		t.Fatalf("应为 BaseError，实际 %T", err)
	}
	if be.Code() != 155507 {
		t.Errorf("错误码应为 155507，实际 %d", be.Code())
	}
	if !strings.Contains(be.Message(), "429 too many requests") {
		t.Errorf("消息应包含 \"429 too many requests\"，实际 %q", be.Message())
	}
}

// TestExtract_invalidJSON 对齐 test_extract_invalid_json（"parsed" in msg）
func TestExtract_invalidJSON(t *testing.T) {
	model, _ := newExtractTestModelWithMock(t, []string{"Invalid JSON response"}, nil)
	ex := NewTripleExtractor(model, "test-model")
	chunks := []common.TextChunk{{ID: "1", Text: "Alice knows Bob", DocID: "doc_1"}}
	_, err := ex.Extract(context.Background(), chunks)
	if err == nil {
		t.Fatal("invalid JSON 应报错")
	}
	var be *exception.BaseError
	if !errors.As(err, &be) {
		t.Fatalf("应为 BaseError，实际 %T", err)
	}
	if be.Code() != 155507 {
		t.Errorf("错误码应为 155507，实际 %d", be.Code())
	}
	// 对齐 assert "parsed" in (exc_info.value.message or "").lower()
	if !strings.Contains(strings.ToLower(be.Message()), "parsed") {
		t.Errorf("消息应包含 \"parsed\"（lower），实际 %q", be.Message())
	}
}

// TestExtract_空chunks 对齐 test_extract_empty_chunks（返回空且 invoke 未调）
func TestExtract_空chunks(t *testing.T) {
	model, mock := newExtractTestModelWithMock(t, []string{"unused"}, nil)
	ex := NewTripleExtractor(model, "test-model")
	triples, err := ex.Extract(context.Background(), []common.TextChunk{})
	if err != nil {
		t.Fatalf("空 chunks 不应报错: %v", err)
	}
	if len(triples) != 0 {
		t.Errorf("应返回空列表，实际 %d", len(triples))
	}
	if got := atomic.LoadInt32(&mock.callCount); got != 0 {
		t.Errorf("invoke 不应被调用，实际 %d 次", got)
	}
}

// TestExtract_nil客户端 对齐 §8-5 路径（nil → 155507 硬失败）
func TestExtract_nil客户端(t *testing.T) {
	ex := NewTripleExtractor(nil, "test-model")
	chunks := []common.TextChunk{{ID: "1", Text: "Alice knows Bob", DocID: "doc_1"}}
	_, err := ex.Extract(context.Background(), chunks)
	if err == nil {
		t.Fatal("nil llmClient 应报错")
	}
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Code() != 155507 {
		t.Fatalf("应为 155507 BaseError，实际 %v", err)
	}
}

// TestExtract_首错按chunk序 §7.2-2：首错选取按 chunk 序而非执行/完成序
//
// maxConcurrent=1 下 goroutine 抢信号量的调度序不确定——mock 按提示词内容
// 分派（prompt 内嵌 chunk 文本 → 确定性识别 chunk 身份），与调度序解耦。
// 双向验证：场景 A chunk0 失败/chunk1 成功 → 首错含 "c0"；
// 场景 B chunk0 成功/chunk1 失败 → 首错含 "c1"（成功结果随 raise 丢弃）。
// 注意：chunk 文本用 "alpha"/"beta"（few-shot 模板 L102 含 "selected first
// overall" 字样，若用 "first" 作 dispatch 键，另一 chunk 的 prompt 同时含
// 模板 "first" 与自身键，map 迭代序随机导致误匹配——曾引发 ~30% flaky）。
func TestExtract_首错按chunk序(t *testing.T) {
	okContent := `{"triples": [["a", "b", "c"]]}`
	// 场景 A：c0 失败（提示词含 "alpha"）→ 首错取 c0
	modelA, _ := newExtractDispatchModelWithMock(t, map[string]string{
		"alpha": "boom-c0",
		"beta":  okContent,
	})
	exA := NewTripleExtractor(modelA, "test-model", WithTripleMaxConcurrent(1))
	_, err := exA.Extract(context.Background(), []common.TextChunk{
		{ID: "c0", Text: "alpha", DocID: "doc"},
		{ID: "c1", Text: "beta", DocID: "doc"},
	})
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Code() != 155507 {
		t.Fatalf("场景 A 应为 155507 BaseError，实际 %v", err)
	}
	if !strings.Contains(be.Message(), "c0") || !strings.Contains(be.Message(), "boom-c0") {
		t.Errorf("场景 A 首错消息应含 \"c0: boom-c0\"，实际 %q", be.Message())
	}

	// 场景 B：c1 失败（提示词含 "beta"）→ 首错取 c1（c0 成功结果丢弃）
	modelB, _ := newExtractDispatchModelWithMock(t, map[string]string{
		"alpha": okContent,
		"beta":  "boom-c1",
	})
	exB := NewTripleExtractor(modelB, "test-model", WithTripleMaxConcurrent(1))
	_, err = exB.Extract(context.Background(), []common.TextChunk{
		{ID: "c0", Text: "alpha", DocID: "doc"},
		{ID: "c1", Text: "beta", DocID: "doc"},
	})
	if !errors.As(err, &be) || be.Code() != 155507 {
		t.Fatalf("场景 B 应为 155507 BaseError，实际 %v", err)
	}
	if !strings.Contains(be.Message(), "c1") || !strings.Contains(be.Message(), "boom-c1") {
		t.Errorf("场景 B 首错消息应含 \"c1: boom-c1\"，实际 %q", be.Message())
	}
}

// newExtractDispatchModelWithMock 构造按提示词内容分派的 mock Model。
// dispatch 键为 chunk 文本子串（提示词内嵌该文本），值为响应内容或
// "boom-" 前缀错误标记。
func newExtractDispatchModelWithMock(t *testing.T, dispatch map[string]string) (*llm.Model, *dispatchTripleClient) {
	t.Helper()
	clientCfg, err := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	if err != nil {
		t.Fatalf("构造客户端配置失败: %v", err)
	}
	mock := newDispatchTripleClient(dispatch)
	model, err := llm.NewModel(clientCfg, llmschema.NewModelRequestConfig(), llm.WithClient(mock))
	if err != nil {
		t.Fatalf("构造 Model 失失败: %v", err)
	}
	return model, mock
}

// newDispatchTripleClient 构造按消息文本分派的 mock 客户端。
//
// 与 captionTripleClient（按调用序）互补：并发调度序不确定的场景用本类型
// 按内容分派，确定性识别"哪个 chunk 的请求"。
func newDispatchTripleClient(dispatch map[string]string) *dispatchTripleClient {
	return &dispatchTripleClient{dispatch: dispatch}
}

// dispatchTripleClient 按提示词内容分派响应的 mock 客户端。
type dispatchTripleClient struct {
	stubTripleClient
	// dispatch 文本子串 → 响应内容（"boom-" 前缀视为错误）
	dispatch map[string]string
	// callCount Invoke 调用计数（atomic 并发安全；无 last* 字段故无需 mu）
	callCount int32
}

// Invoke 按消息文本匹配 dispatch 键分派响应。
func (c *dispatchTripleClient) Invoke(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	atomic.AddInt32(&c.callCount, 1)
	// 从消息列表提取首条文本（buildPrompt 产物为单条 UserMessage）
	promptText := ""
	if msgs := messages.Messages(); len(msgs) > 0 {
		promptText = msgs[0].GetContent().String()
	}
	for key, resp := range c.dispatch {
		if strings.Contains(promptText, key) {
			if strings.HasPrefix(resp, "boom-") {
				return nil, errors.New(resp)
			}
			return llmschema.NewAssistantMessage(resp), nil
		}
	}
	return nil, errors.New("no dispatch key matched: " + promptText)
}

// TestExtract_temperature透传 §7.2-3：mock 记录 InvokeParams 断言温度
//
// 单 chunk 场景（lastTemperature 断言无歧义）
func TestExtract_temperature透传(t *testing.T) {
	content := `{"triples": [["a", "b", "c"]]}`
	// 默认 0.0
	model, mock := newExtractTestModelWithMock(t, []string{content}, nil)
	ex := NewTripleExtractor(model, "test-model")
	_, _ = ex.Extract(context.Background(), []common.TextChunk{{ID: "1", Text: "t", DocID: "d"}})
	mock.mu.Lock()
	lt := mock.lastTemperature
	mock.mu.Unlock()
	if lt != nil && *lt != 0.0 {
		t.Errorf("默认温度应为 0.0，实际 %v", *lt)
	}
	// 自定义 0.7
	model2, mock2 := newExtractTestModelWithMock(t, []string{content}, nil)
	ex2 := NewTripleExtractor(model2, "test-model", WithTripleTemperature(0.7))
	_, _ = ex2.Extract(context.Background(), []common.TextChunk{{ID: "1", Text: "t", DocID: "d"}})
	mock2.mu.Lock()
	lt2 := mock2.lastTemperature
	mock2.mu.Unlock()
	if lt2 == nil || *lt2 != 0.7 {
		t.Errorf("自定义温度应为 0.7，实际 %v", lt2)
	}
}

// TestExtract_maxConcurrent串行性 §7.2-8：信号量并发峰值=1
func TestExtract_maxConcurrent串行性(t *testing.T) {
	content := `{"triples": [["a", "b", "c"]]}`
	responses := []string{content, content, content, content}
	model, mock := newExtractTestModelWithMock(t, responses, nil)
	ex := NewTripleExtractor(model, "test-model", WithTripleMaxConcurrent(1))
	chunks := []common.TextChunk{
		{ID: "1", Text: "a", DocID: "d"},
		{ID: "2", Text: "b", DocID: "d"},
		{ID: "3", Text: "c", DocID: "d"},
		{ID: "4", Text: "e", DocID: "d"},
	}
	_, err := ex.Extract(context.Background(), chunks)
	if err != nil {
		t.Fatalf("Extract 不应报错: %v", err)
	}
	if peak := atomic.LoadInt32(&mock.concurrentPeak); peak != 1 {
		t.Errorf("maxConcurrent=1 时并发峰值应为 1，实际 %d", peak)
	}
}

// TestProcess_转发 对齐 Process 契约（Python 基类一行转发）
func TestProcess_转发(t *testing.T) {
	content := `{"triples": [["a", "b", "c"]]}`
	model, mock := newExtractTestModelWithMock(t, []string{content}, nil)
	ex := NewTripleExtractor(model, "test-model")
	chunks := []common.TextChunk{{ID: "1", Text: "t", DocID: "d"}}
	triples, err := ex.Process(context.Background(), chunks)
	if err != nil {
		t.Fatalf("Process 不应报错: %v", err)
	}
	if len(triples) != 1 || atomic.LoadInt32(&mock.callCount) != 1 {
		t.Errorf("Process 应转发 Extract 产出 1 条，实际 %d", len(triples))
	}
}

// ──────────────────────────── 覆盖率补充 ────────────────────────────

// TestTruncateContent_截断分支 覆盖补充：直接调用非导出函数走 >200 分支；
// 注意探针实测纯文本经 repair 成 string 走 default false——不触发 repair err
// 路径，故 truncateContent 的截断分支直接单测
func TestTruncateContent_截断分支(t *testing.T) {
	s := strings.Repeat("x", 250)
	got := truncateContent(s, 200)
	if len(got) != 203 { // 200 字符 + "..."
		t.Errorf("截断结果应为 200+3 字符，实际 %d", len(got))
	}
	if got[:200] != s[:200] || got[200:] != "..." {
		t.Error("截断结果应保留前 200 字符 + \"...\" 后缀")
	}
	// ≤n 原样返回
	if got := truncateContent("short", 200); got != "short" {
		t.Errorf("短字符串应原样返回，实际 %q", got)
	}
}

// TestPyStr_int64字段 覆盖补充：深修复路径 int64 分支防御
func TestPyStr_int64字段(t *testing.T) {
	if got := pyStr(int64(99)); got != "99" {
		t.Errorf("pyStr(int64(99)) 应为 \"99\"，实际 %q", got)
	}
}

// TestMetadataTitle_类型分支 覆盖补充：metadata 缺 title / 非字符串 title
func TestMetadataTitle_类型分支(t *testing.T) {
	if got := metadataTitle(nil); got != "" {
		t.Errorf("nil metadata 应返回空串，实际 %q", got)
	}
	if got := metadataTitle(map[string]any{"other": "x"}); got != "" {
		t.Errorf("缺 title 键应返回空串，实际 %q", got)
	}
	// 非字符串 title（Python get 返回 None → 或 "" 后置空；Go 类型守卫置空）
	if got := metadataTitle(map[string]any{"title": 42}); got != "" {
		t.Errorf("非字符串 title 应返回空串，实际 %q", got)
	}
	if got := metadataTitle(map[string]any{"title": "T"}); got != "T" {
		t.Errorf("字符串 title 应原样返回，实际 %q", got)
	}
}

// TestPyFloatStr_特殊字面 覆盖补充：nan/inf 三分支（jsonrepair 正常输入
// 不可达，防御分支）
func TestPyFloatStr_特殊字面(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{math.NaN(), "nan"},
		{math.Inf(1), "inf"},
		{math.Inf(-1), "-inf"},
		{0.0, "0.0"},                   // abs==0 走 f 记法
		{math.Copysign(0, -1), "-0.0"}, // 真负零输出 "-0.0"（对齐 Python str(-0.0)；SA4026: -0.0 字面量即 0.0，须 Copysign 构造）
		{1e-5, "1e-05"},                // 下界外 e 记法
		{1.5e16, "1.5e+16"},            // 上界外 e 记法
	}
	for _, c := range cases {
		if got := pyFloatStr(c.in); got != c.want {
			t.Errorf("pyFloatStr(%v) 应为 %q，实际 %q", c.in, c.want, got)
		}
	}
}
