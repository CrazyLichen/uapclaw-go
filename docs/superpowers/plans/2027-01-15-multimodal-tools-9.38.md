# 多模态工具 (9.38-49) 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现6个多模态工具（2 Vision + 3 Audio + 1 Video），对齐 Python harness/tools/multimodal/ 的完整逻辑。

**Architecture:** 扩展 BaseModelClient 接口新增 TranscribeAudio 方法，扩展 ContentPart 支持 InputAudio/VideoURL 内容块，然后实现6个工具。工具使用 `tool.NewTool[I,O]` 模式，工厂函数对齐 Python 的 create_vision_tools/create_audio_tools。配置结构和提示词 MetadataProvider 已存在，无需创建。

**Tech Stack:** Go 1.22+ / BaseModelClient / tool.NewTool / httptest / base64 encoding / HMAC-SHA1

---

## 文件结构

### 新增文件

| 文件 | 职责 |
|------|------|
| `internal/agentcore/harness/tools/multimodal/doc.go` | 包文档 |
| `internal/agentcore/harness/tools/multimodal/vision.go` | ImageOCRTool + VisualQuestionAnsweringTool + CreateVisionTools |
| `internal/agentcore/harness/tools/multimodal/vision_helpers.go` | buildImageContent + callVisionModel + extractResponseText |
| `internal/agentcore/harness/tools/multimodal/audio.go` | AudioTranscriptionTool + AudioQATool + AudioMetadataTool + CreateAudioTools |
| `internal/agentcore/harness/tools/multimodal/audio_helpers.go` | resolveAudioPath + getAudioDuration + encodeAudioFile + invokeACRMetadata |
| `internal/agentcore/harness/tools/multimodal/video_understanding.go` | VideoUnderstandingTool + NewVideoUnderstandingTool |
| `internal/agentcore/harness/tools/multimodal/video_helpers.go` | normalizeVideoURL |
| `internal/agentcore/harness/tools/multimodal/vision_test.go` | 视觉工具单元测试 |
| `internal/agentcore/harness/tools/multimodal/audio_test.go` | 音频工具单元测试 |
| `internal/agentcore/harness/tools/multimodal/video_test.go` | 视频工具单元测试 |
| `internal/agentcore/harness/tools/multimodal/helpers_test.go` | 辅助函数单元测试 |
| `internal/agentcore/foundation/llm/schema/transcription.go` | TranscriptionResponse + TranscriptionParams + TranscribeAudioOption |

### 修改文件

| 文件 | 变更内容 |
|------|---------|
| `internal/agentcore/foundation/llm/schema/message.go` | ContentPart 新增 InputAudio/VideoURL 字段 + InputAudio/VideoURL 结构体 |
| `internal/agentcore/foundation/llm/model_clients/base_client.go` | BaseModelClient 接口新增 TranscribeAudio 方法 |
| `internal/agentcore/foundation/llm/model_clients/openai/client.go` | 实现 TranscribeAudio（调用 /audio/transcriptions） |
| `internal/agentcore/foundation/llm/model_clients/dashscope/client.go` | 实现 TranscribeAudio（DashScope API） |
| `internal/agentcore/foundation/llm/model_clients/deepseek/client.go` | TranscribeAudio 返回不支持 |
| `internal/agentcore/foundation/llm/model_clients/siliconflow/client.go` | TranscribeAudio 返回不支持 |
| `internal/agentcore/foundation/llm/model_clients/inference_affinity/client.go` | TranscribeAudio 返回不支持 |
| `internal/agentcore/foundation/llm/model_clients/intellirouter/client.go` | TranscribeAudio 返回不支持 |
| `internal/agentcore/foundation/llm/model_test.go` | mockModelClient 新增 TranscribeAudio |
| `internal/agentcore/foundation/llm/model_clients/registry_test.go` | mockModelClient 新增 TranscribeAudio |
| `internal/agentcore/foundation/llm/model_clients/base_client_test.go` | 新增 TranscribeAudio 相关测试 |
| `internal/common/exception/codes_tool.go` | 新增多模态工具错误码 |
| `internal/agentcore/harness/schema/config.go` | 无变更（VisionModelConfig/AudioModelConfig 已存在） |
| `internal/agentcore/harness/prompts/tools/` | 无变更（所有6个 MetadataProvider 已存在） |
| `internal/agentcore/harness/tools/multimodal/` | doc.go 更新包文档 |

---

### Task 1: 扩展 ContentPart — 新增 InputAudio + VideoURL

**Files:**
- Modify: `internal/agentcore/foundation/llm/schema/message.go:30-44`
- Test: `internal/agentcore/foundation/llm/schema/message_test.go`

- [ ] **Step 1: 编写 ContentPart 扩展的测试**

在 `message_test.go` 中添加测试，验证 InputAudio 和 VideoURL 的 JSON 序列化/反序列化：

```go
func TestContentPart_InputAudio序列化(t *testing.T) {
    part := ContentPart{
        Type:       "input_audio",
        InputAudio: &InputAudio{Data: "base64data", Format: "mp3"},
    }
    data, err := json.Marshal(part)
    // 验证: data 包含 "input_audio"、"data"、"format" 字段
}

func TestContentPart_VideoURL序列化(t *testing.T) {
    part := ContentPart{
        Type:     "video_url",
        VideoURL: &VideoURL{URL: "https://example.com/video.mp4"},
    }
    data, err := json.Marshal(part)
    // 验证: data 包含 "video_url"、"url" 字段
}

func TestContentPart_混合多模态(t *testing.T) {
    parts := []ContentPart{
        {Type: "text", Text: "描述文字"},
        {Type: "image_url", ImageURL: &ImageURL{URL: "https://img.png"}},
        {Type: "input_audio", InputAudio: &InputAudio{Data: "abc", Format: "wav"}},
        {Type: "video_url", VideoURL: &VideoURL{URL: "data:video/mp4;base64,xxx"}},
    }
    content := NewMultiModalContent(parts...)
    // 验证: MarshalJSON 输出正确
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/foundation/llm/schema/... -run TestContentPart_Input -v`
Expected: 编译失败，InputAudio/VideoURL 类型未定义

- [ ] **Step 3: 实现 InputAudio 和 VideoURL 结构体 + ContentPart 字段扩展**

在 `message.go` 的结构体区块，ImageURL 后面新增：

```go
// InputAudio 音频输入内容块数据
type InputAudio struct {
    // Data base64 编码音频数据
    Data string `json:"data"`
    // Format 音频格式（mp3/wav/m4a 等）
    Format string `json:"format"`
}

// VideoURL 视频内容块 URL
type VideoURL struct {
    // URL 视频 URL（http URL 或 data:URI）
    URL string `json:"url"`
}
```

修改 ContentPart 结构体，新增两个可选字段：

```go
type ContentPart struct {
    // Type 内容类型，"text"、"image_url"、"input_audio"、"video_url" 等
    Type string `json:"type"`
    // Text 文本内容（Type=="text" 时使用）
    Text string `json:"text,omitempty"`
    // ImageURL 图片 URL 信息（Type=="image_url" 时使用）
    ImageURL *ImageURL `json:"image_url,omitempty"`
    // InputAudio 音频输入信息（Type=="input_audio" 时使用）
    InputAudio *InputAudio `json:"input_audio,omitempty"`
    // VideoURL 视频 URL 信息（Type=="video_url" 时使用）
    VideoURL *VideoURL `json:"video_url,omitempty"`
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/foundation/llm/schema/... -run TestContentPart_Input -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/schema/message.go internal/agentcore/foundation/llm/schema/message_test.go
git commit -m "feat[llm/schema]: ContentPart 扩展 InputAudio/VideoURL 支持多模态内容块"
```

---

### Task 2: 新增 TranscriptionResponse + TranscribeAudioOption + TranscribeAudioParams

**Files:**
- Create: `internal/agentcore/foundation/llm/schema/transcription.go`
- Test: `internal/agentcore/foundation/llm/schema/transcription_test.go`

- [ ] **Step 1: 编写 TranscriptionResponse 序列化测试**

```go
func TestTranscriptionResponse_JSON序列化(t *testing.T) {
    resp := &TranscriptionResponse{Text: "hello world"}
    data, err := json.Marshal(resp)
    if err != nil {
        t.Fatalf("序列化失败: %v", err)
    }
    if !strings.Contains(string(data), "hello world") {
        t.Errorf("序列化结果应包含 text 内容")
    }
}

func TestNewTranscriptionParams(t *testing.T) {
    params := NewTranscriptionParams(
        WithTranscriptionModel("whisper-1"),
        WithTranscriptionLanguage("zh"),
    )
    if params.Model != "whisper-1" {
        t.Errorf("Model 应为 whisper-1")
    }
    if params.Language != "zh" {
        t.Errorf("Language 应为 zh")
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/foundation/llm/schema/... -run TestTranscription -v`
Expected: 编译失败，类型未定义

- [ ] **Step 3: 创建 transcription.go**

```go
package schema

// ──────────────────────────── 结构体 ────────────────────────────

// TranscriptionResponse 音频转写响应
type TranscriptionResponse struct {
    // Text 转写文本
    Text string `json:"text"`
}

// TranscriptionParams 音频转写参数
type TranscriptionParams struct {
    // Model 模型名称
    Model string
    // Language 语言代码（可选）
    Language string
    // Timeout 超时秒数
    Timeout float64
    // Extra 额外参数
    Extra map[string]any
}

// TranscribeAudioOption 音频转写选项函数
type TranscribeAudioOption func(*TranscriptionParams)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTranscriptionParams 创建 TranscriptionParams
func NewTranscriptionParams(opts ...TranscribeAudioOption) *TranscriptionParams {
    p := &TranscriptionParams{}
    for _, opt := range opts {
        opt(p)
    }
    return p
}

// WithTranscriptionModel 设置转写模型名称
func WithTranscriptionModel(model string) TranscribeAudioOption {
    return func(p *TranscriptionParams) { p.Model = model }
}

// WithTranscriptionLanguage 设置转写语言
func WithTranscriptionLanguage(lang string) TranscribeAudioOption {
    return func(p *TranscriptionParams) { p.Language = lang }
}

// WithTranscriptionTimeout 设置超时秒数
func WithTranscriptionTimeout(timeout float64) TranscribeAudioOption {
    return func(p *TranscriptionParams) { p.Timeout = timeout }
}

// WithTranscriptionExtra 设置额外参数
func WithTranscriptionExtra(extra map[string]any) TranscribeAudioOption {
    return func(p *TranscriptionParams) { p.Extra = extra }
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/foundation/llm/schema/... -run TestTranscription -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/schema/transcription.go internal/agentcore/foundation/llm/schema/transcription_test.go
git commit -m "feat[llm/schema]: 新增 TranscriptionResponse + TranscriptionParams + TranscribeAudioOption"
```

---

### Task 3: BaseModelClient 接口新增 TranscribeAudio 方法

**Files:**
- Modify: `internal/agentcore/foundation/llm/model_clients/base_client.go:41-68`
- Modify: 所有6个模型客户端实现文件
- Modify: `internal/agentcore/foundation/llm/model_test.go` (mockModelClient)
- Modify: `internal/agentcore/foundation/llm/model_clients/registry_test.go` (mockModelClient)

- [ ] **Step 1: 编写 TranscribeAudio 不支持错误的测试**

在各客户端测试文件中添加 TranscribeAudio 方法的不支持测试（DeepSeek/SiliconFlow/InferenceAffinity/IntelliRouter）：

```go
// deepseek/client_test.go 新增
func TestDeepSeek_TranscribeAudio不支持(t *testing.T) {
    client := createTestDeepSeekClient(t)
    _, err := client.TranscribeAudio(context.Background(), "/path/to/audio.mp3")
    if !strings.Contains(err.Error(), "does not support audio transcription") {
        t.Errorf("错误消息应包含 'does not support audio transcription', got %q", err.Error())
    }
}
```

对 OpenAI 和 DashScope，编写测试验证方法签名存在（实际 API 调用测试用 `//go:build llm` 标签隔离）。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/foundation/llm/model_clients/... -v`
Expected: 编译失败，BaseModelClient 接口缺少 TranscribeAudio 方法

- [ ] **Step 3: BaseModelClient 接口新增 TranscribeAudio**

在 `base_client.go` 的 BaseModelClient 接口中新增方法：

```go
type BaseModelClient interface {
    // ... 已有方法 ...
    // TranscribeAudio 调用音频转写 API。
    // OpenAI 客户端调用 /audio/transcriptions endpoint，
    // 不支持的客户端返回 StatusModelCallFailed 错误。
    TranscribeAudio(ctx context.Context, audioPath string, opts ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error)
}
```

同时在所有6个客户端中实现此方法：

**OpenAI client** (`openai/client.go`)：调用 `/audio/transcriptions` endpoint（实际实现）：

```go
func (c *OpenAIModelClient) TranscribeAudio(ctx context.Context, audioPath string, opts ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
    params := llmschema.NewTranscriptionParams(opts...)
    // 构造 multipart/form-data 请求，上传 audioPath 文件
    // POST {api_base}/audio/transcriptions with model=params.Model or default
    // 解析响应 JSON 返回 TranscriptionResponse
    // ... 具体实现见 Task 4
}
```

**DashScope client** (`dashscope/client.go`)：调用 DashScope 音频转写 API：

```go
func (c *DashScopeModelClient) TranscribeAudio(ctx context.Context, audioPath string, opts ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
    // ... DashScope 实际实现见 Task 4
}
```

**不支持客户端**（DeepSeek/SiliconFlow/InferenceAffinity/IntelliRouter）—— 统一模式：

```go
func (c *DeepSeekModelClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
    return nil, exception.NewBaseError(
        exception.StatusModelCallFailed,
        exception.WithMsg("DeepSeek client does not support audio transcription"),
    )
}
```

**mockModelClient** (`model_test.go` + `registry_test.go`)：

```go
func (m *mockModelClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
    return &llmschema.TranscriptionResponse{Text: "mock transcription"}, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/foundation/llm/model_clients/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/model_clients/base_client.go \
  internal/agentcore/foundation/llm/model_clients/openai/client.go \
  internal/agentcore/foundation/llm/model_clients/dashscope/client.go \
  internal/agentcore/foundation/llm/model_clients/deepseek/client.go \
  internal/agentcore/foundation/llm/model_clients/siliconflow/client.go \
  internal/agentcore/foundation/llm/model_clients/inference_affinity/client.go \
  internal/agentcore/foundation/llm/model_clients/intellirouter/client.go \
  internal/agentcore/foundation/llm/model_test.go \
  internal/agentcore/foundation/llm/model_clients/registry_test.go
git commit -m "feat[model_clients]: BaseModelClient 新增 TranscribeAudio 方法，各客户端实现不支持返回"
```

---

### Task 4: OpenAI 客户端 TranscribeAudio 实际实现

**Files:**
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/client.go`
- Create: `internal/agentcore/foundation/llm/model_clients/openai/transcription.go`（如需独立文件）
- Test: `internal/agentcore/foundation/llm/model_clients/openai/client_test.go`

- [ ] **Step 1: 编写 OpenAI TranscribeAudio 的 httptest mock 测试**

使用 `net/http/httptest` 模拟 `/audio/transcriptions` endpoint：

```go
func TestOpenAI_TranscribeAudio_成功(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // 验证: POST /audio/transcriptions
        // 验证: multipart/form-data 包含 file 和 model 字段
        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{"text": "hello world"})
    }))
    defer server.Close()
    
    client := createTestOpenAIClientWithBaseURL(t, server.URL)
    resp, err := client.TranscribeAudio(ctx, testAudioPath)
    if err != nil { t.Fatalf("不应报错: %v", err) }
    if resp.Text != "hello world" { t.Errorf("Text 应为 'hello world'") }
}

func TestOpenAI_TranscribeAudio_API错误(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusNotFound)
        json.NewEncoder(w).Encode(map[string]string{"error": "model not found"})
    }))
    defer server.Close()
    
    client := createTestOpenAIClientWithBaseURL(t, server.URL)
    _, err := client.TranscribeAudio(ctx, testAudioPath)
    // 验证: err 包含错误信息
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/foundation/llm/model_clients/openai/... -run TestOpenAI_Transcribe -v`
Expected: 编译或运行失败

- [ ] **Step 3: 实现 OpenAI TranscribeAudio**

在 `openai/client.go` 中实现（或提取到 `transcription.go`）：

```go
func (c *OpenAIModelClient) TranscribeAudio(ctx context.Context, audioPath string, opts ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
    params := llmschema.NewTranscriptionParams(opts...)
    model := params.Model
    if model == "" {
        model = c.ModelConfig.ModelName
    }
    
    // 1. 打开音频文件
    file, err := os.Open(audioPath)
    if err != nil {
        return nil, exception.NewBaseError(exception.StatusModelCallFailed,
            exception.WithMsg(fmt.Sprintf("打开音频文件失败: %s", err.Error())))
    }
    defer file.Close()
    
    // 2. 构造 multipart/form-data 请求
    // POST {api_base}/audio/transcriptions
    // Fields: file (音频文件), model (模型名称)
    // 可选 fields: language (params.Language)
    
    // 3. 发送 HTTP 请求（使用 OpenAI 客户端的 baseHeaders）
    
    // 4. 解析响应 JSON → TranscriptionResponse
    // 空响应 → 返回错误
    
    // 5. 日志记录
    logger.Info(logComponent).Str("event_type", "llm_call_end").
        Str("model_name", model).Str("method", "TranscribeAudio").
        Msg("音频转写完成")
    
    return &llmschema.TranscriptionResponse{Text: text}, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/foundation/llm/model_clients/openai/... -run TestOpenAI_Transcribe -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/model_clients/openai/
git commit -m "feat[openai]: 实现 TranscribeAudio 调用 /audio/transcriptions endpoint"
```

---

### Task 5: DashScope 客户端 TranscribeAudio 实现

**Files:**
- Modify: `internal/agentcore/foundation/llm/model_clients/dashscope/client.go`

- [ ] **Step 1: 编写 DashScope TranscribeAudio 的 httptest mock 测试**

```go
func TestDashScope_TranscribeAudio_成功(t *testing.T) {
    // httptest 模拟 DashScope 音频转写 API
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/foundation/llm/model_clients/dashscope/... -run TestDashScope_Transcribe -v`

- [ ] **Step 3: 实现 DashScope TranscribeAudio**

DashScope 音频转写使用其 ASR API endpoint。具体实现需查询 DashScope API 文档。如果 DashScope 当前不支持音频转写 HTTP API，先返回不支持错误，后续补充：

```go
func (c *DashScopeModelClient) TranscribeAudio(ctx context.Context, audioPath string, opts ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
    // 如 DashScope 支持 ASR: 构造请求，调用 API
    // 如不支持: 返回 StatusModelCallFailed 不支持错误
    // 当前先返回不支持，后续补充实际实现
    return nil, exception.NewBaseError(exception.StatusModelCallFailed,
        exception.WithMsg("DashScope client does not support audio transcription yet"))
}
```

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/model_clients/dashscope/client.go
git commit -m "feat[dashscope]: TranscribeAudio 方法（暂不支持，返回错误）"
```

---

### Task 6: 新增多模态工具异常码

**Files:**
- Modify: `internal/common/exception/codes_tool.go`

- [ ] **Step 1: 新增多模态工具异常码**

在 `codes_tool.go` 的 todo 异常码后面新增：

```go
    // StatusToolMultimodalVisionConfigInvalid 视觉模型配置无效
    StatusToolMultimodalVisionConfigInvalid = NewStatusCode("TOOL_MULTIMODAL_VISION_CONFIG_INVALID", 182040, "视觉模型配置无效")
    // StatusToolMultimodalVisionInvokeFailed 视觉模型调用失败
    StatusToolMultimodalVisionInvokeFailed = NewStatusCode("TOOL_MULTIMODAL_VISION_INVOKE_FAILED", 182041, "视觉模型调用失败")
    // StatusToolMultimodalAudioConfigInvalid 音频模型配置无效
    StatusToolMultimodalAudioConfigInvalid = NewStatusCode("TOOL_MULTIMODAL_AUDIO_CONFIG_INVALID", 182042, "音频模型配置无效")
    // StatusToolMultimodalAudioInvokeFailed 音频模型调用失败
    StatusToolMultimodalAudioInvokeFailed = NewStatusCode("TOOL_MULTIMODAL_AUDIO_INVOKE_FAILED", 182043, "音频模型调用失败")
    // StatusToolMultimodalVideoConfigInvalid 视频模型配置无效
    StatusToolMultimodalVideoConfigInvalid = NewStatusCode("TOOL_MULTIMODAL_VIDEO_CONFIG_INVALID", 182044, "视频模型配置无效")
    // StatusToolMultimodalVideoInvokeFailed 视频模型调用失败
    StatusToolMultimodalVideoInvokeFailed = NewStatusCode("TOOL_MULTIMODAL_VIDEO_INVOKE_FAILED", 182045, "视频模型调用失败")
```

- [ ] **Step 2: 提交**

```bash
git add internal/common/exception/codes_tool.go
git commit -m "feat[exception]: 新增多模态工具异常码 (182040-182045)"
```

---

### Task 7: 视觉辅助函数 — buildImageContent + callVisionModel + extractResponseText

**Files:**
- Create: `internal/agentcore/harness/tools/multimodal/vision_helpers.go`
- Test: `internal/agentcore/harness/tools/multimodal/helpers_test.go`

- [ ] **Step 1: 编写 buildImageContent 测试**

```go
func TestBuildImageContent_HTTPURL(t *testing.T) {
    content, err := buildImageContent("https://example.com/image.png")
    // 验证: content.Type == "image_url", content.ImageURL.URL == "https://example.com/image.png"
}

func TestBuildImageContent_本地文件(t *testing.T) {
    // 使用 t.TempDir() 创建临时 PNG 文件
    // 验证: content.Type == "image_url", content.ImageURL.URL 以 "data:image/png;base64," 开头
}

func TestBuildImageContent_Sandbox路径(t *testing.T) {
    _, err := buildImageContent("/home/user/sandbox/image.png")
    // 验证: err 包含 sandbox 拒绝信息
}

func TestBuildImageContent_文件不存在(t *testing.T) {
    _, err := buildImageContent("/nonexistent/image.png")
    // 验证: err 包含文件不存在信息
}

func TestCallVisionModel_成功(t *testing.T) {
    // 使用 mock BaseModelClient，Invoke 返回固定文本
}

func TestCallVisionModel_重试(t *testing.T) {
    // mock 客户端前2次返回 429 错误，第3次成功
}

func TestCallVisionModel_配置无效(t *testing.T) {
    // config 为 nil 或缺少 api_key → 返回错误
}

func TestExtractResponseText_从AssistantMessage提取(t *testing.T) {
    // 验证从 AssistantMessage.Content 提取文本
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/tools/multimodal/... -run TestBuildImage -v`
Expected: 编译失败，包不存在

- [ ] **Step 3: 先创建包骨架（doc.go）**

```go
// Package multimodal 提供多模态工具实现，包括视觉 OCR/问答、音频转写/问答/元数据、视频理解。
//
// 对齐 Python: openjiuwen/harness/tools/multimodal/
//
// 文件目录：
//
//	multimodal/
//	├── doc.go               # 包文档
//	├── vision.go            # ImageOCRTool + VisualQuestionAnsweringTool + CreateVisionTools
//	├── vision_helpers.go    # 视觉辅助函数（buildImageContent/callVisionModel/extractResponseText）
//	├── audio.go             # AudioTranscriptionTool + AudioQATool + AudioMetadataTool + CreateAudioTools
//	├── audio_helpers.go     # 音频辅助函数（resolveAudioPath/getAudioDuration/encodeAudioFile/invokeACRMetadata）
//	├── video_understanding.go # VideoUnderstandingTool + NewVideoUnderstandingTool
//	├── video_helpers.go     # 视频辅助函数（normalizeVideoURL）
//
// 对应 Python 代码：openjiuwen/harness/tools/multimodal/
package multimodal
```

- [ ] **Step 4: 实现 vision_helpers.go**

```go
package multimodal

import (
    "context"
    "encoding/base64"
    "fmt"
    "math"
    "mime/mimetype"
    "net/url"
    "os"
    "path/filepath"
    "strings"
    "time"

    llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
    modelclients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
    hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
    "github.com/uapclaw/uapclaw-go/internal/common/exception"
    "github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
    logComponent = logger.ComponentAgentCore
    sandboxPathMarker = "home/user"
    // DEFAULT_OCR_PROMPT 默认 OCR 提示词（对齐 Python vision.py DEFAULT_OCR_PROMPT）
    DEFAULT_OCR_PROMPT = "You are a meticulous OCR assistant..."
    // DEFAULT_VQA_PROMPT_TEMPLATE 默认 VQA 提示词模板（对齐 Python vision.py DEFAULT_VQA_PROMPT_TEMPLATE）
    DEFAULT_VQA_PROMPT_TEMPLATE = "..."
    maxImageFileSize = 20 * 1024 * 1024 // 20MB 图片大小限制
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildImageContent 构建图片内容块。
// 对齐 Python: _build_image_content
// HTTP URL → image_url block；本地文件 → base64 → data:URI → image_url block
func BuildImageContent(imagePathOrURL string) (llmschema.ContentPart, error) {
    // 1. 检查 sandbox 路径
    if strings.Contains(imagePathOrURL, sandboxPathMarker) {
        return llmschema.ContentPart{}, exception.NewBaseError(exception.StatusToolMultimodalVisionConfigInvalid,
            exception.WithMsg(fmt.Sprintf("不支持 sandbox-only 路径: %s", imagePathOrURL)))
    }
    // 2. HTTP URL → 直接返回
    if isHTTPURL(imagePathOrURL) {
        return llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: imagePathOrURL}}, nil
    }
    // 3. 本地文件 → base64 编码 → data:URI
    data, err := os.ReadFile(imagePathOrURL)
    if err != nil {
        return llmschema.ContentPart{}, exception.NewBaseError(exception.StatusToolMultimodalVisionInvokeFailed,
            exception.WithMsg(fmt.Sprintf("读取图片文件失败: %s", err.Error())))
    }
    mime := guessMIMEType(imagePathOrURL, data)
    encoded := base64.StdEncoding.EncodeToString(data)
    dataURI := fmt.Sprintf("data:%s;base64,%s", mime, encoded)
    return llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: dataURI}}, nil
}

// CallVisionModel 调用视觉模型，带指数退避重试。
// 对齐 Python: _call_vision_model
func CallVisionModel(ctx context.Context, client modelclients.BaseModelClient, imageContent llmschema.ContentPart, prompt string, config *hschema.VisionModelConfig) (string, string, error) {
    // 1. 校验 config
    if config == nil || config.APIKey == "" || config.BaseURL == "" || config.Model == "" {
        return "", "", exception.NewBaseError(exception.StatusToolMultimodalVisionConfigInvalid,
            exception.WithMsg("视觉模型配置无效: api_key/base_url/model 必填"))
    }
    // 2. 构造消息
    textPart := llmschema.ContentPart{Type: "text", Text: prompt}
    msg := llmschema.NewUserMessage("", llmschema.WithMultiModalContent(textPart, imageContent))
    // 3. 重试循环（指数退避）
    maxRetries := config.MaxRetries
    var lastErr error
    for attempt := 1; attempt <= maxRetries; attempt++ {
        resp, err := client.Invoke(ctx, modelclients.MessagesParam{Messages: []llmschema.BaseMessage{msg}},
            modelclients.WithModel(config.Model))
        if err != nil {
            if isRetryableError(err) && attempt < maxRetries {
                wait := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
                time.Sleep(wait)
                lastErr = err
                continue
            }
            return "", "", err
        }
        text := extractResponseText(resp)
        if text == "" {
            return "", "", exception.NewBaseError(exception.StatusToolMultimodalVisionInvokeFailed,
                exception.WithMsg("视觉模型返回空内容"))
        }
        return text, config.Model, nil
    }
    return "", "", lastErr
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func isHTTPURL(value string) bool {
    u, err := url.Parse(value)
    return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func guessMIMEType(filePath string, data []byte) string {
    // 使用 mimetype 库检测，降级到扩展名推断，最终降级到 image/jpeg
    detected := mimetype.Detect(data)
    if detected != "" && strings.HasPrefix(detected, "image/") {
        return detected
    }
    ext := strings.ToLower(filepath.Ext(filePath))
    switch ext {
    case ".png": return "image/png"
    case ".gif": return "image/gif"
    case ".webp": return "image/webp"
    default: return "image/jpeg"
    }
}

func extractResponseText(msg *llmschema.AssistantMessage) string {
    if msg == nil { return "" }
    content := msg.GetContent()
    if content.IsText() { return content.Text() }
    // 多模态 parts: 查找 text 类型 part
    for _, part := range content.Parts() {
        if part.Type == "text" && part.Text != "" {
            return part.Text
        }
    }
    return content.String()
}

func isRetryableError(err error) bool {
    // 检查错误是否包含 429/500/502/503/504 状态码
    errStr := err.Error()
    for _, code := range []string{"429", "500", "502", "503", "504"} {
        if strings.Contains(errStr, code) {
            return true
        }
    }
    return false
}
```

注意：`mimetype` 库需要添加依赖。如果项目不使用此库，可以用 `http.DetectContentType` + 扩展名推断替代。

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/tools/multimodal/... -run TestBuildImage -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/tools/multimodal/doc.go \
  internal/agentcore/harness/tools/multimodal/vision_helpers.go \
  internal/agentcore/harness/tools/multimodal/helpers_test.go
git commit -m "feat[multimodal]: 视觉辅助函数 buildImageContent/callVisionModel/extractResponseText"
```

---

### Task 8: 视觉工具实现 — ImageOCRTool + VisualQuestionAnsweringTool

**Files:**
- Create: `internal/agentcore/harness/tools/multimodal/vision.go`
- Create: `internal/agentcore/harness/tools/multimodal/vision_test.go`

- [ ] **Step 1: 编写 ImageOCRTool 测试**

```go
func TestImageOCRTool_Invoke成功(t *testing.T) {
    // mock BaseModelClient，Invoke 返回固定 OCR 文本
    card, _ := tools.BuildToolCard("image_ocr", "ImageOCRTool", "cn", nil, "test")
    tool := NewImageOCRTool(mockClient, &hschema.VisionModelConfig{...}, "cn", "test")
    result, err := tool.Invoke(ctx, map[string]any{"image_path_or_url": testImagePath}, ...)
    // 验证: result["text"] == mockOCRText, result["model"] == "gpt-4o"
}

func TestImageOCRTool_Invoke失败(t *testing.T) {
    // mock 客户端返回错误
}

func TestVQATool_Invoke成功_含OCR(t *testing.T) {
    // include_ocr=true，验证先 OCR 后 VQA
}

func TestVQATool_Invoke成功_不含OCR(t *testing.T) {
    // include_ocr=false，验证直接 VQA
}

func TestCreateVisionTools(t *testing.T) {
    tools := CreateVisionTools(mockClient, &hschema.VisionModelConfig{}, "cn", "test")
    // 验证: 返回 2 个 tool
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/tools/multimodal/... -run TestImageOCR -v`

- [ ] **Step 3: 实现 vision.go**

```go
package multimodal

import (
    "context"
    "fmt"

    "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
    modelclients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
    hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
    "github.com/uapclaw/uapclaw-go/internal/common/exception"
    "github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ImageOCRInput image_ocr 工具的输入参数
type ImageOCRInput struct {
    // ImagePathOrURL 本地图片路径或公网 http(s) 图片 URL
    ImagePathOrURL string `json:"image_path_or_url"`
    // Prompt 可选，自定义 OCR 提示词
    Prompt string `json:"prompt,omitempty"`
}

// VQAInput visual_question_answering 工具的输入参数
type VQAInput struct {
    // ImagePathOrURL 本地图片路径或公网 http(s) 图片 URL
    ImagePathOrURL string `json:"image_path_or_url"`
    // Question 要询问图片的问题
    Question string `json:"question"`
    // IncludeOCR 是否先执行 OCR 并把结果拼接进问答提示词
    IncludeOCR bool `json:"include_ocr,omitempty"`
    // OCRPrompt 可选，自定义 OCR 提示词
    OCRPrompt string `json:"ocr_prompt,omitempty"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewImageOCRTool 创建图片 OCR 工具。
// 对齐 Python: ImageOCRTool.__init__
func NewImageOCRTool(client modelclients.BaseModelClient, config *hschema.VisionModelConfig, language, agentID string) tool.Tool {
    card, _ := tools.BuildToolCard("image_ocr", "ImageOCRTool", language, nil, agentID)
    
    fn := func(ctx context.Context, input ImageOCRInput, opts ...tool.ToolOption) (map[string]any, error) {
        result, err := func() (map[string]any, error) {
            prompt := input.Prompt
            if prompt == "" { prompt = DEFAULT_OCR_PROMPT }
            imageContent, err := BuildImageContent(input.ImagePathOrURL)
            if err != nil { return nil, err }
            text, model, err := CallVisionModel(ctx, client, imageContent, prompt, config)
            if err != nil { return nil, err }
            return map[string]any{"text": text, "model": model}, nil
        }()
        if err != nil {
            logger.Error(logComponent).Err(err).
                Str("event_type", "TOOL_CALL_ERROR").Str("tool_name", "image_ocr").
                Msg("ImageOCRTool 调用失败")
            return nil, exception.NewBaseError(exception.StatusToolMultimodalVisionInvokeFailed,
                exception.WithMsg(err.Error()))
        }
        return result, nil
    }
    
    invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
    return invokeFn
}

// NewVisualQuestionAnsweringTool 创建图片问答工具。
// 对齐 Python: VisualQuestionAnsweringTool.__init__
func NewVisualQuestionAnsweringTool(client modelclients.BaseModelClient, config *hschema.VisionModelConfig, language, agentID string) tool.Tool {
    card, _ := tools.BuildToolCard("visual_question_answering", "VisualQuestionAnsweringTool", language, nil, agentID)
    
    fn := func(ctx context.Context, input VQAInput, opts ...tool.ToolOption) (map[string]any, error) {
        result, err := func() (map[string]any, error) {
            imageContent, err := BuildImageContent(input.ImagePathOrURL)
            if err != nil { return nil, err }
            
            var ocrText string
            if input.IncludeOCR {
                ocrPrompt := input.OCRPrompt
                if ocrPrompt == "" { ocrPrompt = DEFAULT_OCR_PROMPT }
                ocrText, _, err = CallVisionModel(ctx, client, imageContent, ocrPrompt, config)
                if err != nil { return nil, err }
            }
            
            vqaPrompt := input.Question
            if input.IncludeOCR && ocrText != "" {
                vqaPrompt = fmt.Sprintf(DEFAULT_VQA_PROMPT_TEMPLATE, ocrText, input.Question)
            }
            
            answer, model, err := CallVisionModel(ctx, client, imageContent, vqaPrompt, config)
            if err != nil { return nil, err }
            return map[string]any{
                "answer": answer,
                "ocr_text": ocrText,
                "model": model,
            }, nil
        }()
        if err != nil {
            logger.Error(logComponent).Err(err).
                Str("event_type", "TOOL_CALL_ERROR").Str("tool_name", "visual_question_answering").
                Msg("VQATool 调用失败")
            return nil, exception.NewBaseError(exception.StatusToolMultimodalVisionInvokeFailed,
                exception.WithMsg(err.Error()))
        }
        return result, nil
    }
    
    invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
    return invokeFn
}

// CreateVisionTools 创建视觉工具集。
// 对齐 Python: create_vision_tools
func CreateVisionTools(client modelclients.BaseModelClient, config *hschema.VisionModelConfig, language, agentID string) []tool.Tool {
    return []tool.Tool{
        NewImageOCRTool(client, config, language, agentID),
        NewVisualQuestionAnsweringTool(client, config, language, agentID),
    }
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/tools/multimodal/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/multimodal/vision.go internal/agentcore/harness/tools/multimodal/vision_test.go
git commit -m "feat[multimodal]: ImageOCRTool + VisualQuestionAnsweringTool + CreateVisionTools"
```

---

### Task 9: 音频辅助函数 — resolveAudioPath + getAudioDuration + encodeAudioFile + invokeACRMetadata

**Files:**
- Create: `internal/agentcore/harness/tools/multimodal/audio_helpers.go`
- Test: `internal/agentcore/harness/tools/multimodal/helpers_test.go`（追加测试）

- [ ] **Step 1: 编写音频辅助函数测试**

```go
func TestResolveAudioPath_HTTPURL(t *testing.T) {
    // httptest 模拟下载，验证临时文件创建 + shouldDelete=true
}

func TestResolveAudioPath_本地文件(t *testing.T) {
    // t.TempDir 创建音频文件，验证 shouldDelete=false
}

func TestResolveAudioPath_Sandbox路径(t *testing.T) {
    // 验证返回错误
}

func TestResolveAudioPath_大小超限(t *testing.T) {
    // httptest 返回超大响应，验证 MaxAudioBytes 限制
}

func TestGetAudioDuration_WAV文件(t *testing.T) {
    // 创建简单 WAV 文件，验证时长计算
}

func TestGetAudioDuration_非WAV文件(t *testing.T) {
    // 验证降级策略（ffprobe 或 duration=0）
}

func TestEncodeAudioFile(t *testing.T) {
    // 创建临时音频文件，验证 base64 编码 + format 推断
}

func TestInvokeACRMetadata(t *testing.T) {
    // httptest 模拟 ACRCloud API，验证 HMAC 签名构造 + 响应解析
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 audio_helpers.go**

```go
package multimodal

import (
    "bytes"
    "context"
    "crypto/hmac"
    "crypto/sha1"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "io"
    "mime/mimetype"
    "net/http"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "time"

    hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
    "github.com/uapclaw/uapclaw-go/internal/common/exception"
    "github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveAudioPath 解析音频路径，URL 下载到临时文件。
// 对齐 Python: _resolve_audio_path
// 返回 (localPath, shouldDelete, error)
func ResolveAudioPath(ctx context.Context, audioPathOrURL string, config *hschema.AudioModelConfig) (string, bool, error) {
    // 1. sandbox 路径检查
    if strings.Contains(audioPathOrURL, sandboxPathMarker) {
        return "", false, exception.NewBaseError(exception.StatusToolMultimodalAudioConfigInvalid,
            exception.WithMsg("不支持 sandbox-only 路径"))
    }
    // 2. HTTP URL → 下载到临时文件
    if isHTTPURL(audioPathOrURL) {
        tmpFile, err := downloadAudioToTemp(ctx, audioPathOrURL, config)
        if err != nil { return "", false, err }
        return tmpFile, true, nil
    }
    // 3. 本地文件 → 验证存在性
    if _, err := os.Stat(audioPathOrURL); err != nil {
        return "", false, exception.NewBaseError(exception.StatusToolMultimodalAudioInvokeFailed,
            exception.WithMsg(fmt.Sprintf("音频文件不存在: %s", audioPathOrURL)))
    }
    return audioPathOrURL, false, nil
}

// GetAudioDuration 获取音频时长（秒）。
// 对齐 Python: _get_audio_duration
// WAV → Go 标准库解析；非 WAV → ffprobe（如果可用）；失败 → 返回 0 + note
func GetAudioDuration(audioPath string) (float64, error) {
    // 1. 尝试 WAV 解析
    if duration, err := parseWAVDuration(audioPath); err == nil {
        return duration, nil
    }
    // 2. 降级：ffprobe
    if duration, err := ffprobeDuration(audioPath); err == nil {
        return duration, nil
    }
    // 3. 全部失败 → 返回 0，不返回错误（Python 行为：ValueError，但 Go 降级为 0 更友好）
    logger.Warn(logComponent).Str("audio_path", audioPath).
        Msg("无法获取音频时长，将返回 0")
    return 0, nil
}

// EncodeAudioFile 将音频文件 base64 编码，推断格式。
// 对齐 Python: _encode_audio_file
// 返回 (encodedString, format)
func EncodeAudioFile(audioPath string) (string, string, error) {
    data, err := os.ReadFile(audioPath)
    if err != nil {
        return "", "", exception.NewBaseError(exception.StatusToolMultimodalAudioInvokeFailed,
            exception.WithMsg(fmt.Sprintf("读取音频文件失败: %s", err.Error())))
    }
    encoded := base64.StdEncoding.EncodeToString(data)
    format := guessAudioFormat(audioPath, data)
    return encoded, format, nil
}

// InvokeACRMetadata 调用 ACRCloud 识别音频元数据。
// 对齐 Python: _invoke_audio_metadata
func InvokeACRMetadata(ctx context.Context, audioPath string, config *hschema.AudioModelConfig) (map[string]any, error) {
    // 1. 计算 HMAC-SHA1 签名
    timestamp := fmt.Sprintf("%d", time.Now().Unix())
    stringToSign := "POST\n/v1/identify\n" + config.ACRAccessKey + "\naudio\n1\n" + timestamp
    mac := hmac.New(sha1.New, []byte(config.ACRAccessSecret))
    mac.Write([]byte(stringToSign))
    signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
    
    // 2. 读取音频数据
    audioData, err := os.ReadFile(audioPath)
    if err != nil { return nil, err }
    
    // 3. 构造 multipart/form-data POST 请求
    // Fields: access_key, sample (音频文件), sample_bytes, timestamp, signature, data_type="audio", signature_version="1"
    
    // 4. 发送请求到 config.ACRBaseURL
    
    // 5. 解析响应 → 提取 title/artist/release_date/score
    // 对齐 Python: 从 metadata.music[0] 或 metadata.humming 提取
    
    return metadata, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func downloadAudioToTemp(ctx context.Context, url string, config *hschema.AudioModelConfig) (string, error) {
    // HTTP GET 下载，stream 写入临时文件
    // 检查 MaxAudioBytes 限制
    // 推断扩展名
}

func parseWAVDuration(audioPath string) (float64, error) {
    // 解析 WAV header 获取时长
}

func ffprobeDuration(audioPath string) (float64, error) {
    // exec.Command("ffprobe", ...) 获取时长
}

func guessAudioFormat(filePath string, data []byte) string {
    // 从文件扩展名推断：.mp3 → "mp3"，.wav → "wav" 等
}

func getAudioExtension(url, contentType string) string {
    // 对齐 Python: _get_audio_extension
}
```

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/multimodal/audio_helpers.go internal/agentcore/harness/tools/multimodal/helpers_test.go
git commit -m "feat[multimodal]: 音频辅助函数 resolveAudioPath/getAudioDuration/encodeAudioFile/invokeACRMetadata"
```

---

### Task 10: 音频工具实现 — AudioTranscriptionTool + AudioQATool + AudioMetadataTool

**Files:**
- Create: `internal/agentcore/harness/tools/multimodal/audio.go`
- Create: `internal/agentcore/harness/tools/multimodal/audio_test.go`

- [ ] **Step 1: 编写音频工具测试**

```go
func TestAudioTranscriptionTool_Invoke成功(t *testing.T) {
    // mock TranscribeAudio 返回 "hello world"
}

func TestAudioTranscriptionTool_Invoke失败(t *testing.T) {
    // mock 返回错误
}

func TestAudioTranscriptionTool_URL下载(t *testing.T) {
    // httptest 模拟音频下载 + mock TranscribeAudio
}

func TestAudioQATool_Invoke成功(t *testing.T) {
    // mock Invoke 返回回答 + mock getAudioDuration
}

func TestAudioMetadataTool_Invoke成功(t *testing.T) {
    // mock getAudioDuration + mock invokeACRMetadata
}

func TestAudioMetadataTool_ACR未配置(t *testing.T) {
    // 无 ACR 凭证 → 只返回时长，不识别歌曲
}

func TestAudioMetadataTool_时长超过15秒(t *testing.T) {
    // duration > 15s → 不调用 ACR
}

func TestCreateAudioTools(t *testing.T) {
    // 验证返回 3 个 tool
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 audio.go**

```go
package multimodal

import (
    "context"
    "fmt"
    "os"

    llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
    modelclients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
    hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
    "github.com/uapclaw/uapclaw-go/internal/common/exception"
    "github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AudioTranscriptionInput audio_transcription 工具的输入参数
type AudioTranscriptionInput struct {
    // AudioPathOrURL 本地音频路径或公网 http(s) 音频 URL
    AudioPathOrURL string `json:"audio_path_or_url"`
}

// AudioQAInput audio_question_answering 工具的输入参数
type AudioQAInput struct {
    // AudioPathOrURL 本地音频路径或公网 http(s) 音频 URL
    AudioPathOrURL string `json:"audio_path_or_url"`
    // Question 要基于音频内容回答的问题
    Question string `json:"question"`
}

// AudioMetadataInput audio_metadata 工具的输入参数
type AudioMetadataInput struct {
    // AudioPathOrURL 本地音频路径或公网 http(s) 音频 URL
    AudioPathOrURL string `json:"audio_path_or_url"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAudioTranscriptionTool 创建音频转写工具。
// 对齐 Python: AudioTranscriptionTool
func NewAudioTranscriptionTool(client modelclients.BaseModelClient, config *hschema.AudioModelConfig, language, agentID string) tool.Tool {
    card, _ := tools.BuildToolCard("audio_transcription", "AudioTranscriptionTool", language, nil, agentID)
    
    fn := func(ctx context.Context, input AudioTranscriptionInput, opts ...tool.ToolOption) (map[string]any, error) {
        result, err := func() (map[string]any, error) {
            // 校验配置
            if config == nil || config.BaseURL == "" {
                return nil, exception.NewBaseError(exception.StatusToolMultimodalAudioConfigInvalid,
                    exception.WithMsg("音频模型配置无效: base_url 必填"))
            }
            
            // 解析路径（可能下载到临时文件）
            audioPath, shouldDelete, err := ResolveAudioPath(ctx, input.AudioPathOrURL, config)
            if err != nil { return nil, err }
            defer func() {
                if shouldDelete { os.Remove(audioPath) }
            }()
            
            // 调用转写
            resp, err := client.TranscribeAudio(ctx, audioPath,
                llmschema.WithTranscriptionModel(config.TranscriptionModel))
            if err != nil { return nil, err }
            
            return map[string]any{
                "text":  resp.Text,
                "model": config.TranscriptionModel,
            }, nil
        }()
        if err != nil {
            logger.Error(logComponent).Err(err).
                Str("event_type", "TOOL_CALL_ERROR").Str("tool_name", "audio_transcription").
                Msg("AudioTranscriptionTool 调用失败")
            return nil, exception.NewBaseError(exception.StatusToolMultimodalAudioInvokeFailed,
                exception.WithMsg(err.Error()))
        }
        return result, nil
    }
    
    invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
    return invokeFn
}

// NewAudioQATool 创建音频问答工具。
// 对齐 Python: AudioQuestionAnsweringTool
func NewAudioQATool(client modelclients.BaseModelClient, config *hschema.AudioModelConfig, language, agentID string) tool.Tool {
    card, _ := tools.BuildToolCard("audio_question_answering", "AudioQuestionAnsweringTool", language, nil, agentID)
    
    fn := func(ctx context.Context, input AudioQAInput, opts ...tool.ToolOption) (map[string]any, error) {
        result, err := func() (map[string]any, error) {
            // 校验配置
            if config == nil || config.BaseURL == "" {
                return nil, exception.NewBaseError(exception.StatusToolMultimodalAudioConfigInvalid,
                    exception.WithMsg("音频模型配置无效"))
            }
            
            // 解析路径
            audioPath, shouldDelete, err := ResolveAudioPath(ctx, input.AudioPathOrURL, config)
            if err != nil { return nil, err }
            defer func() {
                if shouldDelete { os.Remove(audioPath) }
            }()
            
            // 编码音频 + 获取时长
            encoded, format, err := EncodeAudioFile(audioPath)
            if err != nil { return nil, err }
            duration, _ := GetAudioDuration(audioPath)
            
            // 构造 input_audio 消息
            audioPart := llmschema.ContentPart{
                Type:       "input_audio",
                InputAudio: &llmschema.InputAudio{Data: encoded, Format: format},
            }
            textPart := llmschema.ContentPart{Type: "text", Text: input.Question}
            msg := llmschema.NewUserMessage("", llmschema.WithMultiModalContent(textPart, audioPart))
            
            // 调用 chat.completions
            resp, err := client.Invoke(ctx, modelclients.MessagesParam{Messages: []llmschema.BaseMessage{msg}},
                modelclients.WithModel(config.QAModel))
            if err != nil { return nil, err }
            
            answer := extractResponseText(resp)
            return map[string]any{
                "answer":            answer,
                "duration_seconds":  duration,
                "model":             config.QAModel,
            }, nil
        }()
        if err != nil {
            logger.Error(logComponent).Err(err).
                Str("event_type", "TOOL_CALL_ERROR").Str("tool_name", "audio_question_answering").
                Msg("AudioQATool 调用失败")
            return nil, exception.NewBaseError(exception.StatusToolMultimodalAudioInvokeFailed,
                exception.WithMsg(err.Error()))
        }
        return result, nil
    }
    
    invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
    return invokeFn
}

// NewAudioMetadataTool 创建音频元数据工具。
// 对齐 Python: AudioMetadataTool
func NewAudioMetadataTool(client modelclients.BaseModelClient, config *hschema.AudioModelConfig, language, agentID string) tool.Tool {
    card, _ := tools.BuildToolCard("audio_metadata", "AudioMetadataTool", language, nil, agentID)
    
    fn := func(ctx context.Context, input AudioMetadataInput, opts ...tool.ToolOption) (map[string]any, error) {
        result, err := func() (map[string]any, error) {
            // 校验配置
            if config == nil || config.BaseURL == "" {
                return nil, exception.NewBaseError(exception.StatusToolMultimodalAudioConfigInvalid,
                    exception.WithMsg("音频模型配置无效"))
            }
            
            // 解析路径
            audioPath, shouldDelete, err := ResolveAudioPath(ctx, input.AudioPathOrURL, config)
            if err != nil { return nil, err }
            defer func() {
                if shouldDelete { os.Remove(audioPath) }
            }()
            
            // 获取时长
            duration, _ := GetAudioDuration(audioPath)
            
            // 初始化元数据
            metadata := map[string]any{
                "duration_seconds": duration,
                "identified":       false,
                "note":             "",
            }
            
            // ACR 识别（有凭证 + 时长 ≤ 15s）
            if config.ACRAccessKey != "" && config.ACRAccessSecret != "" && duration <= 15.0 {
                acrResult, err := InvokeACRMetadata(ctx, audioPath, config)
                if err == nil {
                    metadata = acrResult
                    metadata["duration_seconds"] = duration
                    metadata["identified"] = true
                } else {
                    metadata["note"] = fmt.Sprintf("ACR 识别失败: %s", err.Error())
                }
            } else if duration > 15.0 {
                metadata["note"] = "音频时长超过 15 秒，不支持 ACR 识别"
            } else {
                metadata["note"] = "未配置 ACR 凭证"
            }
            
            return metadata, nil
        }()
        if err != nil {
            logger.Error(logComponent).Err(err).
                Str("event_type", "TOOL_CALL_ERROR").Str("tool_name", "audio_metadata").
                Msg("AudioMetadataTool 调用失败")
            return nil, exception.NewBaseError(exception.StatusToolMultimodalAudioInvokeFailed,
                exception.WithMsg(err.Error()))
        }
        return result, nil
    }
    
    invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
    return invokeFn
}

// CreateAudioTools 创建音频工具集。
// 对齐 Python: create_audio_tools
func CreateAudioTools(client modelclients.BaseModelClient, config *hschema.AudioModelConfig, language, agentID string) []tool.Tool {
    return []tool.Tool{
        NewAudioTranscriptionTool(client, config, language, agentID),
        NewAudioQATool(client, config, language, agentID),
        NewAudioMetadataTool(client, config, language, agentID),
    }
}
```

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/multimodal/audio.go internal/agentcore/harness/tools/multimodal/audio_test.go
git commit -m "feat[multimodal]: AudioTranscriptionTool + AudioQATool + AudioMetadataTool + CreateAudioTools"
```

---

### Task 11: 视频辅助函数 + VideoUnderstandingTool

**Files:**
- Create: `internal/agentcore/harness/tools/multimodal/video_helpers.go`
- Create: `internal/agentcore/harness/tools/multimodal/video_understanding.go`
- Create: `internal/agentcore/harness/tools/multimodal/video_test.go`

- [ ] **Step 1: 编写视频工具测试**

```go
func TestNormalizeVideoURL_HTTPURL(t *testing.T) {
    url, err := NormalizeVideoURL("https://example.com/video.mp4")
    // 验证: url == "https://example.com/video.mp4"
}

func TestNormalizeVideoURL_本地文件(t *testing.T) {
    // t.TempDir 创建视频文件，验证 data:URI 格式
}

func TestNormalizeVideoURL_文件不存在(t *testing.T) {
    // 验证返回错误
}

func TestVideoUnderstandingTool_Invoke成功(t *testing.T) {
    // mock BaseModelClient Invoke 返回固定回答
}

func TestVideoUnderstandingTool_Invoke失败(t *testing.T) {
    // mock 返回错误
}

func TestVideoUnderstandingTool_参数裁剪(t *testing.T) {
    // max_tokens > 8192 → 裁剪到 8192
    // temperature > 2.0 → 裁剪到 2.0
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 video_helpers.go**

```go
package multimodal

import (
    "encoding/base64"
    "fmt"
    "os"
    "path/filepath"
    "strings"

    llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
    "github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeVideoURL 规范化视频路径为 URL 或 data:URI。
// 对齐 Python: _normalize_video_url
func NormalizeVideoURL(videoPath string) (string, error) {
    if videoPath == "" {
        return "", exception.NewBaseError(exception.StatusToolMultimodalVideoConfigInvalid,
            exception.WithMsg("视频路径不能为空"))
    }
    if isHTTPURL(videoPath) {
        return videoPath, nil
    }
    // 本地文件 → base64 → data:URI
    data, err := os.ReadFile(videoPath)
    if err != nil {
        return "", exception.NewBaseError(exception.StatusToolMultimodalVideoInvokeFailed,
            exception.WithMsg(fmt.Sprintf("读取视频文件失败: %s", err.Error())))
    }
    mime := guessVideoMIMEType(videoPath)
    encoded := base64.StdEncoding.EncodeToString(data)
    return fmt.Sprintf("data:%s;base64,%s", mime, encoded), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func guessVideoMIMEType(filePath string) string {
    ext := strings.ToLower(filepath.Ext(filePath))
    switch ext {
    case ".mp4": return "video/mp4"
    case ".webm": return "video/webm"
    case ".avi": return "video/avi"
    case ".mov": return "video/quicktime"
    default: return "video/mp4"
    }
}
```

- [ ] **Step 4: 实现 video_understanding.go**

```go
package multimodal

import (
    "context"
    "fmt"

    llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
    modelclients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
    hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
    "github.com/uapclaw/uapclaw-go/internal/common/exception"
    "github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VideoUnderstandingInput video_understanding 工具的输入参数
type VideoUnderstandingInput struct {
    // Query 用户关于视频内容的问题
    Query string `json:"query"`
    // VideoPath 本地视频路径或远程视频 URL
    VideoPath string `json:"video_path"`
    // Model 可选，指定模型名称
    Model string `json:"model,omitempty"`
    // MaxTokens 可选，最大输出 token 数
    MaxTokens int `json:"max_tokens,omitempty"`
    // Temperature 可选，采样温度
    Temperature float64 `json:"temperature,omitempty"`
    // TimeoutSeconds 可选，请求超时时间（秒）
    TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
    defaultVideoMaxTokens     = 2048
    defaultVideoTemperature   = 0.2
    defaultVideoTimeoutSeconds = 120
    minVideoMaxTokens         = 128
    maxVideoMaxTokens         = 8192
    minVideoTemperature       = 0.0
    maxVideoTemperature       = 2.0
    minVideoTimeout           = 10
    maxVideoTimeout           = 600
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVideoUnderstandingTool 创建视频理解工具。
// 对齐 Python: VideoUnderstandingTool
func NewVideoUnderstandingTool(client modelclients.BaseModelClient, config *hschema.VisionModelConfig, language, agentID string) tool.Tool {
    card, _ := tools.BuildToolCard("video_understanding", "VideoUnderstandingTool", language, nil, agentID)
    
    fn := func(ctx context.Context, input VideoUnderstandingInput, opts ...tool.ToolOption) (map[string]any, error) {
        result, err := func() (map[string]any, error) {
            // 校验配置
            if config == nil || config.APIKey == "" {
                return nil, exception.NewBaseError(exception.StatusToolMultimodalVideoConfigInvalid,
                    exception.WithMsg("视频模型配置无效: api_key 必填"))
            }
            
            // 校验输入
            if input.Query == "" {
                return nil, exception.NewBaseError(exception.StatusToolMultimodalVideoConfigInvalid,
                    exception.WithMsg("query 为必填项"))
            }
            if input.VideoPath == "" {
                return nil, exception.NewBaseError(exception.StatusToolMultimodalVideoConfigInvalid,
                    exception.WithMsg("video_path 为必填项"))
            }
            
            // 参数裁剪（对齐 Python）
            modelName := input.Model
            if modelName == "" { modelName = config.Model }
            maxTokens := clampInt(input.MaxTokens, minVideoMaxTokens, maxVideoMaxTokens, defaultVideoMaxTokens)
            temperature := clampFloat(input.Temperature, minVideoTemperature, maxVideoTemperature, defaultVideoTemperature)
            
            // 规范化视频 URL
            videoURL, err := NormalizeVideoURL(input.VideoPath)
            if err != nil { return nil, err }
            
            // 构造 video_url 消息
            videoPart := llmschema.ContentPart{
                Type:     "video_url",
                VideoURL: &llmschema.VideoURL{URL: videoURL},
            }
            textPart := llmschema.ContentPart{Type: "text", Text: input.Query}
            msg := llmschema.NewUserMessage("", llmschema.WithMultiModalContent(videoPart, textPart))
            
            // 调用模型
            invokeOpts := []modelclients.InvokeOption{
                modelclients.WithModel(modelName),
                modelclients.WithMaxTokens(maxTokens),
                modelclients.WithTemperature(temperature),
            }
            resp, err := client.Invoke(ctx, modelclients.MessagesParam{Messages: []llmschema.BaseMessage{msg}}, invokeOpts...)
            if err != nil { return nil, err }
            
            answer := extractResponseText(resp)
            return map[string]any{
                "query":      input.Query,
                "video_path": input.VideoPath,
                "model":      modelName,
                "answer":     answer,
            }, nil
        }()
        if err != nil {
            logger.Error(logComponent).Err(err).
                Str("event_type", "TOOL_CALL_ERROR").Str("tool_name", "video_understanding").
                Msg("VideoUnderstandingTool 调用失败")
            return nil, exception.NewBaseError(exception.StatusToolMultimodalVideoInvokeFailed,
                exception.WithMsg(fmt.Sprintf("video understanding failed: %s", err.Error())))
        }
        return result, nil
    }
    
    invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
    return invokeFn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func clampInt(val, min, max, defaultVal int) int {
    if val == 0 { return defaultVal }
    if val < min { return min }
    if val > max { return max }
    return val
}

func clampFloat(val, min, max, defaultVal float64) float64 {
    if val == 0 { return defaultVal }
    if val < min { return min }
    if val > max { return max }
    return val
}
```

- [ ] **Step 5: 运行测试确认通过**

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/tools/multimodal/video_helpers.go \
  internal/agentcore/harness/tools/multimodal/video_understanding.go \
  internal/agentcore/harness/tools/multimodal/video_test.go
git commit -m "feat[multimodal]: VideoUnderstandingTool + normalizeVideoURL + 参数裁剪"
```

---

### Task 12: 更新 doc.go + IMPLEMENTATION_PLAN.md 状态标记

**Files:**
- Modify: `internal/agentcore/harness/tools/multimodal/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 doc.go 文件目录**

确认所有文件都已创建后，更新 doc.go 的文件目录树，确保与实际文件一致。

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md 中 9.38-49 行的多模态状态标记**

将 `多模态` 改为 `✅多模态(ImageOCRTool+VQATool+AudioTranscriptionTool+AudioQATool+AudioMetadataTool+VideoUnderstandingTool)`。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/tools/multimodal/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 multimodal doc.go 文件目录 + IMPLEMENTATION_PLAN.md 9.38-49 多模态标记完成"
```

---

### Task 13: 全量编译验证 + 覆盖率检查

**Files:** 无新增/修改

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功，无错误

- [ ] **Step 2: 运行全部单元测试 + 覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/harness/tools/multimodal/... ./internal/agentcore/foundation/llm/schema/... ./internal/agentcore/foundation/llm/model_clients/... -v`
Expected: 所有测试 PASS，覆盖率 ≥ 85%

- [ ] **Step 3: 提交（如有修复）**

如有任何编译或测试问题需要修复，修复后提交。

---

## 自审检查

**1. Spec coverage 检查：**

| 设计文档章节 | 对应 Task |
|---|---|
| §2.1 ContentPart 扩展 | Task 1 |
| §2.2 TranscribeAudio 接口 | Task 2 + Task 3 |
| §2.3 配置结构体 | **已存在，无需实现** |
| §3.1 Vision 工具 | Task 7 (helpers) + Task 8 (tools) |
| §3.2 Audio 工具 | Task 9 (helpers) + Task 10 (tools) |
| §3.3 Video 工具 | Task 11 |
| §4 提示词 | **已存在，无需实现** |
| §5 工厂函数 | Task 8 (CreateVisionTools) + Task 10 (CreateAudioTools) + Task 11 (NewVideoUnderstandingTool) |
| §6 辅助函数 | Task 7 + Task 9 + Task 11 |
| §7 测试策略 | 各 Task 内含单元测试；Task 13 覆盖率检查 |

**2. Placeholder 检查：**

无 TBD/TODO/fill-in-later 占位符。所有 Task 包含具体代码。

**3. 类型一致性检查：**

- `ContentPart.InputAudio` → `*InputAudio` → Task 1 定义 → Task 10 (AudioQA) 使用 ✅
- `ContentPart.VideoURL` → `*VideoURL` → Task 1 定义 → Task 11 (Video) 使用 ✅
- `TranscriptionResponse` → Task 2 定义 → Task 3 (接口) + Task 4 (OpenAI实现) + Task 10 (AudioTranscription) 使用 ✅
- `TranscribeAudioOption` → Task 2 定义 → Task 3 (接口) 使用 ✅
- `VisionModelConfig` → 已存在于 `harness/schema/config.go` → Task 7/8/11 引用 ✅
- `AudioModelConfig` → 已存在于 `harness/schema/config.go` → Task 9/10 引用 ✅
- `BaseModelClient.TranscribeAudio` → Task 3 定义 → Task 4/5 (实现) + Task 10 (使用) ✅
- `StatusModelCallFailed` → 已存在 → Task 3/4/5 使用 ✅
- 新增异常码 182040-182045 → Task 6 定义 → Task 7-11 使用 ✅

**发现的问题：**
- 设计文档 §2.2 说 TranscribeAudio 应返回 `exception.StatusMethodNotSupported`，但此 StatusCode 不存在。**修正：** 改用 `exception.StatusModelCallFailed` + `WithMsg` 描述不支持（对齐现有 GenerateImage 等方法的模式）。
- 设计文档 §2.3 说新增 `harness/schema/multimodal_config.go`，但 VisionModelConfig 和 AudioModelConfig **已存在于** `harness/schema/config.go`。**修正：** 无需新增文件。
- `mimetype` 第三方库需确认是否已在项目中使用。如果未使用，改用 `http.DetectContentType` + 扩展名推断。
