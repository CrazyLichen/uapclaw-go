package model_clients

import (
	"testing"

	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestNewInvokeParams_Defaults 测试 InvokeParams 默认值。
func TestNewInvokeParams_Defaults(t *testing.T) {
	p := NewInvokeParams()
	if p.Temperature != nil {
		t.Error("默认 Temperature 应为 nil")
	}
	if p.TopP != nil {
		t.Error("默认 TopP 应为 nil")
	}
	if p.Model != "" {
		t.Error("默认 Model 应为空")
	}
	if p.MaxTokens != nil {
		t.Error("默认 MaxTokens 应为 nil")
	}
	if p.Stop != nil {
		t.Error("默认 Stop 应为 nil")
	}
	if p.OutputParser != nil {
		t.Error("默认 OutputParser 应为 nil")
	}
	if p.Timeout != nil {
		t.Error("默认 Timeout 应为 nil")
	}
	if len(p.Tools) != 0 {
		t.Error("默认 Tools 应为空")
	}
}

// TestNewInvokeParams_WithOpts 测试 InvokeParams 组合 Options。
func TestNewInvokeParams_WithOpts(t *testing.T) {
	temp := 0.7
	topP := 0.9
	maxTokens := 1024
	stop := "[END]"
	timeout := 30.0

	p := NewInvokeParams(
		WithInvokeTemperature(temp),
		WithInvokeTopP(topP),
		WithInvokeModel("gpt-4"),
		WithInvokeMaxTokens(maxTokens),
		WithInvokeStop(stop),
		WithInvokeTimeout(timeout),
	)

	if p.Temperature == nil || *p.Temperature != temp {
		t.Errorf("Temperature = %v, 期望 %v", p.Temperature, temp)
	}
	if p.TopP == nil || *p.TopP != topP {
		t.Errorf("TopP = %v, 期望 %v", p.TopP, topP)
	}
	if p.Model != "gpt-4" {
		t.Errorf("Model = %q, 期望 %q", p.Model, "gpt-4")
	}
	if p.MaxTokens == nil || *p.MaxTokens != maxTokens {
		t.Errorf("MaxTokens = %v, 期望 %v", p.MaxTokens, maxTokens)
	}
	if p.Stop == nil || *p.Stop != stop {
		t.Errorf("Stop = %v, 期望 %v", p.Stop, stop)
	}
	if p.Timeout == nil || *p.Timeout != timeout {
		t.Errorf("Timeout = %v, 期望 %v", p.Timeout, timeout)
	}
}

// TestNewStreamParams_Defaults 测试 StreamParams 默认值。
func TestNewStreamParams_Defaults(t *testing.T) {
	p := NewStreamParams()
	if p.Temperature != nil {
		t.Error("默认 Temperature 应为 nil")
	}
	if p.Model != "" {
		t.Error("默认 Model 应为空")
	}
}

// TestNewGenerateImageParams_Defaults 测试 GenerateImageParams 默认值。
func TestNewGenerateImageParams_Defaults(t *testing.T) {
	p := NewGenerateImageParams()
	if p.Size != "1664*928" {
		t.Errorf("Size = %q, 期望 %q", p.Size, "1664*928")
	}
	if p.N != 1 {
		t.Errorf("N = %d, 期望 1", p.N)
	}
	if !p.PromptExtend {
		t.Error("PromptExtend 默认应为 true")
	}
	if p.Watermark {
		t.Error("Watermark 默认应为 false")
	}
}

// TestNewGenerateImageParams_WithOpts 测试 GenerateImageParams Options。
func TestNewGenerateImageParams_WithOpts(t *testing.T) {
	p := NewGenerateImageParams(
		WithImageModel("dall-e-3"),
		WithImageSize("1024*1024"),
		WithImageN(2),
		WithImageSeed(42),
	)
	if p.Model != "dall-e-3" {
		t.Errorf("Model = %q, 期望 %q", p.Model, "dall-e-3")
	}
	if p.Size != "1024*1024" {
		t.Errorf("Size = %q, 期望 %q", p.Size, "1024*1024")
	}
	if p.N != 2 {
		t.Errorf("N = %d, 期望 2", p.N)
	}
	if p.Seed != 42 {
		t.Errorf("Seed = %d, 期望 42", p.Seed)
	}
}

// TestNewGenerateSpeechParams_Defaults 测试 GenerateSpeechParams 默认值。
func TestNewGenerateSpeechParams_Defaults(t *testing.T) {
	p := NewGenerateSpeechParams()
	if p.Voice != "Cherry" {
		t.Errorf("Voice = %q, 期望 %q", p.Voice, "Cherry")
	}
	if p.LanguageType != "Auto" {
		t.Errorf("LanguageType = %q, 期望 %q", p.LanguageType, "Auto")
	}
}

// TestNewGenerateSpeechParams_WithOpts 测试 GenerateSpeechParams Options。
func TestNewGenerateSpeechParams_WithOpts(t *testing.T) {
	p := NewGenerateSpeechParams(
		WithSpeechModel("tts-1"),
		WithSpeechVoice("alloy"),
	)
	if p.Model != "tts-1" {
		t.Errorf("Model = %q, 期望 %q", p.Model, "tts-1")
	}
	if p.Voice != "alloy" {
		t.Errorf("Voice = %q, 期望 %q", p.Voice, "alloy")
	}
}

// TestNewGenerateVideoParams_Defaults 测试 GenerateVideoParams 默认值。
func TestNewGenerateVideoParams_Defaults(t *testing.T) {
	p := NewGenerateVideoParams()
	if p.Duration != 5 {
		t.Errorf("Duration = %d, 期望 5", p.Duration)
	}
	if !p.PromptExtend {
		t.Error("PromptExtend 默认应为 true")
	}
}

// TestNewGenerateVideoParams_WithOpts 测试 GenerateVideoParams Options。
func TestNewGenerateVideoParams_WithOpts(t *testing.T) {
	seed := 123
	p := NewGenerateVideoParams(
		WithVideoModel("video-gen"),
		WithVideoDuration(10),
		WithVideoSeed(seed),
		WithVideoImgURL("https://example.com/img.png"),
	)
	if p.Model != "video-gen" {
		t.Errorf("Model = %q, 期望 %q", p.Model, "video-gen")
	}
	if p.Duration != 10 {
		t.Errorf("Duration = %d, 期望 10", p.Duration)
	}
	if p.Seed == nil || *p.Seed != 123 {
		t.Errorf("Seed = %v, 期望 123", p.Seed)
	}
	if p.ImgURL != "https://example.com/img.png" {
		t.Errorf("ImgURL = %q, 期望 %q", p.ImgURL, "https://example.com/img.png")
	}
}

// ──────────────────────────── 未覆盖的 Invoke Option 测试 ────────────────────────────

func TestNewInvokeParams_WithOutputParser(t *testing.T) {
	parser := &testOutputParser{}
	p := NewInvokeParams(WithInvokeOutputParser(parser))
	if p.OutputParser == nil {
		t.Error("OutputParser 不应为 nil")
	}
}

func TestNewInvokeParams_WithCustomHeaders(t *testing.T) {
	headers := map[string]any{"X-Custom": "val"}
	p := NewInvokeParams(WithInvokeCustomHeaders(headers))
	if p.CustomHeaders == nil {
		t.Error("CustomHeaders 不应为 nil")
	}
	if p.CustomHeaders["X-Custom"] != "val" {
		t.Errorf("CustomHeaders[X-Custom] = %v, 期望 val", p.CustomHeaders["X-Custom"])
	}
}

func TestNewInvokeParams_WithTracerRecordData(t *testing.T) {
	data := "trace-data"
	p := NewInvokeParams(WithInvokeTracerRecordData(data))
	if p.TracerRecordData != data {
		t.Errorf("TracerRecordData = %v, 期望 %v", p.TracerRecordData, data)
	}
}

func TestNewInvokeParams_WithExtra(t *testing.T) {
	extra := map[string]any{"top_k": 50}
	p := NewInvokeParams(WithInvokeExtra(extra))
	if p.Extra == nil {
		t.Error("Extra 不应为 nil")
	}
	if p.Extra["top_k"] != 50 {
		t.Errorf("Extra[top_k] = %v, 期望 50", p.Extra["top_k"])
	}
}

// ──────────────────────────── Stream Option 测试 ────────────────────────────

func TestNewStreamParams_WithOpts(t *testing.T) {
	temp := 0.8
	topP := 0.95
	maxTokens := 2048
	stop := "[END]"
	timeout := 60.0

	p := NewStreamParams(
		WithStreamTools(commonschema.NewToolInfo("test_tool", "测试工具", nil)),
		WithStreamTemperature(temp),
		WithStreamTopP(topP),
		WithStreamModel("gpt-4"),
		WithStreamMaxTokens(maxTokens),
		WithStreamStop(stop),
		WithStreamTimeout(timeout),
	)

	if len(p.Tools) != 1 {
		t.Errorf("len(Tools) = %d, 期望 1", len(p.Tools))
	}
	if p.Temperature == nil || *p.Temperature != temp {
		t.Errorf("Temperature = %v, 期望 %v", p.Temperature, temp)
	}
	if p.TopP == nil || *p.TopP != topP {
		t.Errorf("TopP = %v, 期望 %v", p.TopP, topP)
	}
	if p.Model != "gpt-4" {
		t.Errorf("Model = %q, 期望 %q", p.Model, "gpt-4")
	}
	if p.MaxTokens == nil || *p.MaxTokens != maxTokens {
		t.Errorf("MaxTokens = %v, 期望 %v", p.MaxTokens, maxTokens)
	}
	if p.Stop == nil || *p.Stop != stop {
		t.Errorf("Stop = %v, 期望 %v", p.Stop, stop)
	}
	if p.Timeout == nil || *p.Timeout != timeout {
		t.Errorf("Timeout = %v, 期望 %v", p.Timeout, timeout)
	}
}

func TestNewStreamParams_WithOutputParser(t *testing.T) {
	parser := &testOutputParser{}
	p := NewStreamParams(WithStreamOutputParser(parser))
	if p.OutputParser == nil {
		t.Error("OutputParser 不应为 nil")
	}
}

func TestNewStreamParams_WithCustomHeaders(t *testing.T) {
	headers := map[string]any{"X-Stream": "val"}
	p := NewStreamParams(WithStreamCustomHeaders(headers))
	if p.CustomHeaders == nil {
		t.Error("CustomHeaders 不应为 nil")
	}
}

func TestNewStreamParams_WithExtra(t *testing.T) {
	extra := map[string]any{"top_k": 50}
	p := NewStreamParams(WithStreamExtra(extra))
	if p.Extra == nil {
		t.Error("Extra 不应为 nil")
	}
}

func TestNewStreamParams_WithTracerRecordData(t *testing.T) {
	data := "trace-data"
	p := NewStreamParams(WithStreamTracerRecordData(data))
	if p.TracerRecordData != data {
		t.Errorf("TracerRecordData = %v, 期望 %v", p.TracerRecordData, data)
	}
}

// ──────────────────────────── GenerateImage Option 补充测试 ────────────────────────────

func TestNewGenerateImageParams_AllOpts(t *testing.T) {
	p := NewGenerateImageParams(
		WithImageNegativePrompt("bad quality"),
		WithImagePromptExtend(false),
		WithImageWatermark(true),
		WithImageTimeout(120.0),
		WithImageExtra(map[string]any{"style": "vivid"}),
	)
	if p.NegativePrompt != "bad quality" {
		t.Errorf("NegativePrompt = %q, 期望 %q", p.NegativePrompt, "bad quality")
	}
	if p.PromptExtend {
		t.Error("PromptExtend 应为 false")
	}
	if !p.Watermark {
		t.Error("Watermark 应为 true")
	}
	if p.Timeout == nil || *p.Timeout != 120.0 {
		t.Errorf("Timeout = %v, 期望 120.0", p.Timeout)
	}
	if p.Extra == nil || p.Extra["style"] != "vivid" {
		t.Errorf("Extra[style] = %v, 期望 vivid", p.Extra["style"])
	}
}

// ──────────────────────────── GenerateSpeech Option 补充测试 ────────────────────────────

func TestNewGenerateSpeechParams_AllOpts(t *testing.T) {
	p := NewGenerateSpeechParams(
		WithSpeechLanguageType("Chinese"),
		WithSpeechTimeout(30.0),
		WithSpeechExtra(map[string]any{"speed": 1.5}),
	)
	if p.LanguageType != "Chinese" {
		t.Errorf("LanguageType = %q, 期望 %q", p.LanguageType, "Chinese")
	}
	if p.Timeout == nil || *p.Timeout != 30.0 {
		t.Errorf("Timeout = %v, 期望 30.0", p.Timeout)
	}
	if p.Extra == nil || p.Extra["speed"] != 1.5 {
		t.Errorf("Extra[speed] = %v, 期望 1.5", p.Extra["speed"])
	}
}

// ──────────────────────────── GenerateVideo Option 补充测试 ────────────────────────────

func TestNewGenerateVideoParams_AllOpts(t *testing.T) {
	p := NewGenerateVideoParams(
		WithVideoAudioURL("https://example.com/audio.mp3"),
		WithVideoSize("1280*720"),
		WithVideoResolution("720p"),
		WithVideoPromptExtend(false),
		WithVideoWatermark(true),
		WithVideoNegativePrompt("blurry"),
		WithVideoTimeout(300.0),
		WithVideoExtra(map[string]any{"fps": 30}),
	)
	if p.AudioURL != "https://example.com/audio.mp3" {
		t.Errorf("AudioURL = %q, 期望 %q", p.AudioURL, "https://example.com/audio.mp3")
	}
	if p.Size != "1280*720" {
		t.Errorf("Size = %q, 期望 %q", p.Size, "1280*720")
	}
	if p.Resolution != "720p" {
		t.Errorf("Resolution = %q, 期望 %q", p.Resolution, "720p")
	}
	if p.PromptExtend {
		t.Error("PromptExtend 应为 false")
	}
	if !p.Watermark {
		t.Error("Watermark 应为 true")
	}
	if p.NegativePrompt != "blurry" {
		t.Errorf("NegativePrompt = %q, 期望 %q", p.NegativePrompt, "blurry")
	}
	if p.Timeout == nil || *p.Timeout != 300.0 {
		t.Errorf("Timeout = %v, 期望 300.0", p.Timeout)
	}
	if p.Extra == nil || p.Extra["fps"] != 30 {
		t.Errorf("Extra[fps] = %v, 期望 30", p.Extra["fps"])
	}
}

// ──────────────────────────── ToInvokeParams / ToStreamParams 测试 ────────────────────────────

func TestToInvokeParams(t *testing.T) {
	// 测试 ToInvokeParams 直接返回自身指针
	p := NewInvokeParams(WithInvokeModel("gpt-4"))
	result := p.ToInvokeParams()
	if result != p {
		t.Error("ToInvokeParams 应返回自身指针")
	}
}

func TestToStreamParams(t *testing.T) {
	// 测试 ToStreamParams 字段逐个拷贝
	temp := 0.7
	topP := 0.9
	maxTokens := 1024
	stop := "[END]"
	timeout := 30.0

	sp := NewStreamParams(
		WithStreamTools(commonschema.NewToolInfo("test", "desc", nil)),
		WithStreamTemperature(temp),
		WithStreamTopP(topP),
		WithStreamModel("gpt-4"),
		WithStreamMaxTokens(maxTokens),
		WithStreamStop(stop),
		WithStreamOutputParser(&testOutputParser{}),
		WithStreamTimeout(timeout),
		WithStreamExtra(map[string]any{"top_k": 50}),
		WithStreamCustomHeaders(map[string]any{"X-H": "v"}),
		WithStreamTracerRecordData("trace"),
	)

	result := sp.ToStreamParams()
	if result == nil {
		t.Fatal("ToStreamParams 不应返回 nil")
	}
	if len(result.Tools) != 1 {
		t.Errorf("len(Tools) = %d, 期望 1", len(result.Tools))
	}
	if result.Temperature == nil || *result.Temperature != temp {
		t.Errorf("Temperature = %v, 期望 %v", result.Temperature, temp)
	}
	if result.TopP == nil || *result.TopP != topP {
		t.Errorf("TopP = %v, 期望 %v", result.TopP, topP)
	}
	if result.Model != "gpt-4" {
		t.Errorf("Model = %q, 期望 %q", result.Model, "gpt-4")
	}
	if result.MaxTokens == nil || *result.MaxTokens != maxTokens {
		t.Errorf("MaxTokens = %v, 期望 %v", result.MaxTokens, maxTokens)
	}
	if result.Stop == nil || *result.Stop != stop {
		t.Errorf("Stop = %v, 期望 %v", result.Stop, stop)
	}
	if result.OutputParser == nil {
		t.Error("OutputParser 不应为 nil")
	}
	if result.Timeout == nil || *result.Timeout != timeout {
		t.Errorf("Timeout = %v, 期望 %v", result.Timeout, timeout)
	}
	if result.Extra["top_k"] != 50 {
		t.Errorf("Extra[top_k] = %v, 期望 50", result.Extra["top_k"])
	}
	if result.CustomHeaders["X-H"] != "v" {
		t.Errorf("CustomHeaders[X-H] = %v, 期望 v", result.CustomHeaders["X-H"])
	}
	if result.TracerRecordData != "trace" {
		t.Errorf("TracerRecordData = %v, 期望 trace", result.TracerRecordData)
	}
}

// ──────────────────────────── 辅助类型 ────────────────────────────

// testOutputParser 测试用 OutputParser
type testOutputParser struct{}

func (p *testOutputParser) Parse(text string) (any, error) {
	return text, nil
}

// ──────────────────────────── BuildRequestParams 补充测试 ────────────────────────────

func TestBuildRequestParams_WithModelConfigDefaults(t *testing.T) {
	// 测试 model_config 中的默认参数
	mc := llmschema.NewModelRequestConfig(
		llmschema.WithModelName("gpt-4"),
		llmschema.WithTemperature(0.5),
		llmschema.WithTopP(0.9),
	)
	cc := llmschema.NewModelClientConfig("OpenAI", "key", "https://api.openai.com/v1",
		llmschema.WithVerifySSL(false),
	)
	e, err := NewBaseClientEmbed(mc, cc)
	if err != nil {
		t.Fatal(err)
	}

	messagesDict := []map[string]any{{"role": "user", "content": "hello"}}
	params := NewInvokeParams()

	result, err := e.BuildRequestParams(messagesDict, params, false)
	if err != nil {
		t.Fatalf("BuildRequestParams 报错: %v", err)
	}
	if result["temperature"] != 0.5 {
		t.Errorf("temperature = %v, 期望 0.5", result["temperature"])
	}
	if result["top_p"] != 0.9 {
		t.Errorf("top_p = %v, 期望 0.9", result["top_p"])
	}
}

func TestBuildRequestParams_ParamsOverrideModelConfig(t *testing.T) {
	// 测试方法参数覆盖 model_config 默认参数
	mc := llmschema.NewModelRequestConfig(
		llmschema.WithModelName("gpt-4"),
		llmschema.WithTemperature(0.5),
	)
	cc := llmschema.NewModelClientConfig("OpenAI", "key", "https://api.openai.com/v1",
		llmschema.WithVerifySSL(false),
	)
	e, err := NewBaseClientEmbed(mc, cc)
	if err != nil {
		t.Fatal(err)
	}

	messagesDict := []map[string]any{{"role": "user", "content": "hello"}}
	params := NewInvokeParams(WithInvokeTemperature(0.9))

	result, err := e.BuildRequestParams(messagesDict, params, false)
	if err != nil {
		t.Fatalf("BuildRequestParams 报错: %v", err)
	}
	if result["temperature"] != 0.9 {
		t.Errorf("temperature = %v, 期望 0.9（方法参数应覆盖）", result["temperature"])
	}
}

func TestBuildRequestParams_WithStopFromModelConfig(t *testing.T) {
	// 测试 model_config 中的 stop 参数
	mc := llmschema.NewModelRequestConfig(
		llmschema.WithModelName("gpt-4"),
		llmschema.WithStop("[END]"),
	)
	cc := llmschema.NewModelClientConfig("OpenAI", "key", "https://api.openai.com/v1",
		llmschema.WithVerifySSL(false),
	)
	e, err := NewBaseClientEmbed(mc, cc)
	if err != nil {
		t.Fatal(err)
	}

	messagesDict := []map[string]any{{"role": "user", "content": "hello"}}
	params := NewInvokeParams()

	result, err := e.BuildRequestParams(messagesDict, params, false)
	if err != nil {
		t.Fatalf("BuildRequestParams 报错: %v", err)
	}
	if result["stop"] != "[END]" {
		t.Errorf("stop = %v, 期望 [END]", result["stop"])
	}
}

func TestBuildRequestParams_WithExtraInternalParams(t *testing.T) {
	// 测试 Extra 中内部参数被过滤
	e := newTestClientEmbed()
	messagesDict := []map[string]any{{"role": "user", "content": "hello"}}
	params := NewInvokeParams(
		WithInvokeExtra(map[string]any{
			"output_parser":       "should-be-filtered",
			"tracer_record_data":  "should-be-filtered",
			"custom_headers":      "should-be-filtered",
			"valid_param":         "should-be-kept",
		}),
	)

	result, err := e.BuildRequestParams(messagesDict, params, false)
	if err != nil {
		t.Fatalf("BuildRequestParams 报错: %v", err)
	}
	if _, ok := result["output_parser"]; ok {
		t.Error("output_parser 应被过滤")
	}
	if _, ok := result["tracer_record_data"]; ok {
		t.Error("tracer_record_data 应被过滤")
	}
	if _, ok := result["custom_headers"]; ok {
		t.Error("custom_headers 应被过滤")
	}
	if result["valid_param"] != "should-be-kept" {
		t.Errorf("valid_param = %v, 期望 should-be-kept", result["valid_param"])
	}
}

func TestBuildRequestParams_WithNilModelConfig(t *testing.T) {
	// 测试 ModelConfig 为 nil 时从 params 获取 model
	cc := llmschema.NewModelClientConfig("OpenAI", "key", "https://api.openai.com/v1",
		llmschema.WithVerifySSL(false),
	)
	e, err := NewBaseClientEmbed(llmschema.NewModelRequestConfig(), cc)
	if err != nil {
		t.Fatal(err)
	}

	messagesDict := []map[string]any{{"role": "user", "content": "hello"}}
	params := NewInvokeParams(WithInvokeModel("gpt-4"))

	result, err := e.BuildRequestParams(messagesDict, params, false)
	if err != nil {
		t.Fatalf("BuildRequestParams 报错: %v", err)
	}
	if result["model"] != "gpt-4" {
		t.Errorf("model = %v, 期望 gpt-4", result["model"])
	}
}

// ──────────────────────────── ExtractCostInfo 补充测试 ────────────────────────────

func TestExtractCostInfo_IntCost(t *testing.T) {
	// 测试 int 类型 cost
	obj := map[string]any{"cost": 5}
	_, _, totalCost := ExtractCostInfo(obj)
	if totalCost != 5.0 {
		t.Errorf("totalCost = %f, 期望 5.0", totalCost)
	}
}

func TestExtractCostInfo_UsageCost(t *testing.T) {
	// 测试 usage_cost 字段
	obj := map[string]any{"usage_cost": 0.08}
	_, _, totalCost := ExtractCostInfo(obj)
	if totalCost != 0.08 {
		t.Errorf("totalCost = %f, 期望 0.08", totalCost)
	}
}

func TestExtractCostInfo_CostObjectWithoutTotal(t *testing.T) {
	// 测试 cost 对象无 total_cost 时自动求和
	obj := map[string]any{
		"cost": map[string]any{
			"input_cost":  0.01,
			"output_cost": 0.03,
		},
	}
	inputCost, outputCost, totalCost := ExtractCostInfo(obj)
	if inputCost != 0.01 {
		t.Errorf("inputCost = %f, 期望 0.01", inputCost)
	}
	if outputCost != 0.03 {
		t.Errorf("outputCost = %f, 期望 0.03", outputCost)
	}
	if totalCost != 0.04 {
		t.Errorf("totalCost = %f, 期望 0.04", totalCost)
	}
}

func TestExtractCostInfo_CostDetailsWithTotal(t *testing.T) {
	// 测试 cost_details 含 upstream_inference_cost
	obj := map[string]any{
		"cost_details": map[string]any{
			"upstream_inference_prompt_cost":      0.02,
			"upstream_inference_completions_cost": 0.03,
			"upstream_inference_cost":             0.06,
		},
	}
	_, _, totalCost := ExtractCostInfo(obj)
	if totalCost != 0.06 {
		t.Errorf("totalCost = %f, 期望 0.06", totalCost)
	}
}

func TestExtractCostInfo_CostObjectPromptCost(t *testing.T) {
	// 测试 cost 对象中使用 prompt_cost / completion_cost 键名
	obj := map[string]any{
		"cost": map[string]any{
			"prompt_cost":      0.02,
			"completion_cost":  0.04,
			"total_cost":       0.06,
		},
	}
	inputCost, outputCost, totalCost := ExtractCostInfo(obj)
	if inputCost != 0.02 {
		t.Errorf("inputCost = %f, 期望 0.02", inputCost)
	}
	if outputCost != 0.04 {
		t.Errorf("outputCost = %f, 期望 0.04", outputCost)
	}
	if totalCost != 0.06 {
		t.Errorf("totalCost = %f, 期望 0.06", totalCost)
	}
}

// ──────────────────────────── convertOneMessage 补充测试 ────────────────────────────

func TestConvertOneMessage_UnsupportedType(t *testing.T) {
	// 测试不支持的消息类型
	e := newTestClientEmbed()
	_, err := e.convertOneMessage("invalid message")
	if err == nil {
		t.Error("不支持的消息类型应返回错误")
	}
}

func TestConvertOneMessage_BaseMessageWithName(t *testing.T) {
	// 测试带 Name 字段的消息
	e := newTestClientEmbed()
	msg := llmschema.NewUserMessage("hello", llmschema.WithMessageName("user1"))
	result, err := e.convertOneMessage(msg)
	if err != nil {
		t.Fatalf("convertOneMessage 报错: %v", err)
	}
	if result["name"] != "user1" {
		t.Errorf("name = %v, 期望 user1", result["name"])
	}
}

// ──────────────────────────── ReleaseParams 测试 ────────────────────────────

func TestNewReleaseParams_Defaults(t *testing.T) {
	// 测试 ReleaseParams 默认值
	p := NewReleaseParams()
	if p.SessionID != "" {
		t.Error("默认 SessionID 应为空")
	}
	if p.Messages != nil {
		t.Error("默认 Messages 应为 nil")
	}
	if p.MessagesReleasedIndex != 0 {
		t.Error("默认 MessagesReleasedIndex 应为 0")
	}
	if p.Model != "" {
		t.Error("默认 Model 应为空")
	}
	if len(p.Tools) != 0 {
		t.Error("默认 Tools 应为空")
	}
	if p.ToolsReleasedIndex != nil {
		t.Error("默认 ToolsReleasedIndex 应为 nil")
	}
}

func TestNewReleaseParams_WithOpts(t *testing.T) {
	// 测试 ReleaseParams 组合 Options
	msgs := []map[string]any{{"role": "user", "content": "hello"}}
	p := NewReleaseParams(
		WithReleaseSessionID("session-123"),
		WithReleaseMessages(msgs),
		WithReleaseMessagesIndex(5),
		WithReleaseModel("qwen-72b"),
		WithReleaseTools(commonschema.NewToolInfo("test_tool", "测试工具", nil)),
		WithReleaseToolsIndex(2),
	)

	if p.SessionID != "session-123" {
		t.Errorf("SessionID = %q, 期望 %q", p.SessionID, "session-123")
	}
	if p.Messages == nil {
		t.Error("Messages 不应为 nil")
	}
	if p.MessagesReleasedIndex != 5 {
		t.Errorf("MessagesReleasedIndex = %d, 期望 5", p.MessagesReleasedIndex)
	}
	if p.Model != "qwen-72b" {
		t.Errorf("Model = %q, 期望 %q", p.Model, "qwen-72b")
	}
	if len(p.Tools) != 1 {
		t.Errorf("len(Tools) = %d, 期望 1", len(p.Tools))
	}
	if p.ToolsReleasedIndex == nil || *p.ToolsReleasedIndex != 2 {
		t.Errorf("ToolsReleasedIndex = %v, 期望 2", p.ToolsReleasedIndex)
	}
}
