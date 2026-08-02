---
title: 多模态工具 (9.38-49) 实现设计
date: 2027-01-15
type: design
scope: 9.38-49
python_refs:
  - openjiuwen/harness/tools/multimodal/vision.py
  - openjiuwen/harness/tools/multimodal/audio.py
  - openjiuwen/harness/tools/multimodal/video_understanding.py
  - openjiuwen/harness/prompts/tools/vision.py
  - openjiuwen/harness/prompts/tools/audio.py
  - openjiuwen/harness/prompts/tools/video_understanding.py
  - openjiuwen/harness/schema/config.py (VisionModelConfig/AudioModelConfig)
go_refs:
  - internal/agentcore/foundation/llm/schema/message.go (ContentPart/ImageURL/MessageContent)
  - internal/agentcore/foundation/llm/model_clients/base_client.go (BaseModelClient interface)
  - internal/agentcore/harness/schema/config.go (待新增 VisionModelConfig/AudioModelConfig)
---

# 多模态工具 (9.38-49) 实现设计

## 1. 目标

在 Go 项目中实现6个多模态工具，对齐 Python `openjiuwen/harness/tools/multimodal/` 的完整逻辑：

- **Vision**: ImageOCRTool + VisualQuestionAnsweringTool
- **Audio**: AudioTranscriptionTool + AudioQuestionAnsweringTool + AudioMetadataTool
- **Video**: VideoUnderstandingTool

代码逻辑与 Python 一致——构造多模态 content block 消息 → 模型调用，只是入口从 Python `OpenAI().chat.completions.create()` 改为 Go `BaseModelClient.Invoke()`。

## 2. 前置基础设施变更

### 2.1 扩展 ContentPart（新增 InputAudio + VideoURL）

当前 `ContentPart` 只支持 `text` + `image_url`，需扩展以支持 `input_audio` 和 `video_url`：

```go
// 修改文件：internal/agentcore/foundation/llm/schema/message.go

type InputAudio struct {
    // Data base64 编码音频数据
    Data string `json:"data"`
    // Format 音频格式（mp3/wav/m4a 等）
    Format string `json:"format"`
}

type VideoURL struct {
    // URL 视频 URL（http URL 或 data:URI）
    URL string `json:"url"`
}

type ContentPart struct {
    Type      string     `json:"type"`
    Text      string     `json:"text,omitempty"`
    ImageURL  *ImageURL  `json:"image_url,omitempty"`
    InputAudio *InputAudio `json:"input_audio,omitempty"`   // 新增
    VideoURL  *VideoURL  `json:"video_url,omitempty"`       // 新增
}
```

**Why**: Python 的 `input_audio` content block（AudioQA）和 `video_url` content block（Video）在 Go 中没有对应类型。对齐 Python 必须支持这两种 content block。

**How to apply**: 修改 `ContentPart` 结构体，新增字段+JSON tag。OpenAI 客户端的 `convertOneMessage` 和 DashScope 客户端序列化时已自动处理 `ContentPart` JSON，无需额外改动。

### 2.2 BaseModelClient 接口新增 TranscribeAudio 方法

`AudioTranscriptionTool` 调用的是 OpenAI `audio.transcriptions.create` API，这不是 chat.completions，无法走 `Invoke()`。需新增专用接口：

```go
// 修改文件：internal/agentcore/foundation/llm/model_clients/base_client.go

type TranscriptionResponse struct {
    // Text 转写文本
    Text string `json:"text"`
}

type BaseModelClient interface {
    // ... 已有方法 ...
    // TranscribeAudio 调用音频转写 API（对应 Python openai.audio.transcriptions.create）。
    // 不支持时返回 exception.StatusMethodNotSupported 错误。
    TranscribeAudio(ctx context.Context, audioPath string, opts ...TranscribeAudioOption) (*TranscriptionResponse, error)
}
```

各客户端实现：
- **OpenAI 客户端**：调用 `/audio/transcriptions` endpoint
- **DashScope 客户端**：调用 DashScope 对应 API
- **其他客户端**：返回不支持错误（与 GenerateImage 等方法一致）

### 2.3 新增配置结构体

```go
// 新增文件：internal/agentcore/harness/schema/multimodal_config.go

type VisionModelConfig struct {
    // APIKey API 密钥
    APIKey string `json:"api_key"`
    // BaseURL API 地址（默认 https://api.openai.com/v1）
    BaseURL string `json:"base_url"`
    // Model 模型名称（默认 gpt-4o）
    Model string `json:"model"`
    // MaxRetries 最大重试次数（默认 3）
    MaxRetries int `json:"max_retries"`
}

// FromEnv 从环境变量读取配置
// 对齐 Python: VisionModelConfig.from_env()
func (c *VisionModelConfig) FromEnv() *VisionModelConfig

type AudioModelConfig struct {
    // APIKey API 密钥
    APIKey string `json:"api_key"`
    // BaseURL API 地址
    BaseURL string `json:"base_url"`
    // TranscriptionModel 转写模型名称（默认 whisper-1）
    TranscriptionModel string `json:"transcription_model"`
    // QuestionAnsweringModel 问答模型名称（默认 gpt-4o-audio-preview）
    QuestionAnsweringModel string `json:"question_answering_model"`
    // MaxRetries 最大重试次数（默认 3）
    MaxRetries int `json:"max_retries"`
    // HTTPTimeout HTTP 超时秒数
    HTTPTimeout int `json:"http_timeout"`
    // MaxAudioBytes 最大音频文件字节
    MaxAudioBytes int64 `json:"max_audio_bytes"`
    // ACRAccessKey ACRCloud 访问密钥
    ACRAccessKey string `json:"acr_access_key"`
    // ACRAccessSecret ACRCloud 访问密钥
    ACRAccessSecret string `json:"acr_access_secret"`
    // ACRBaseURL ACRCloud API 地址
    ACRBaseURL string `json:"acr_base_url"`
}

// FromEnv 从环境变量读取配置
// 对齐 Python: AudioModelConfig.from_env()
func (c *AudioModelConfig) FromEnv() *AudioModelConfig
```

**Why**: Python 多模态工具通过 `VisionModelConfig` 和 `AudioModelConfig` 传入 API 密钥/模型/重试等配置。Go 需要同等配置结构，并支持 `FromEnv()` 环境变量回退。

**How to apply**: 在 `harness/schema/` 下新增文件，与现有 `config.go` 同包。

## 3. 工具实现

### 3.1 包结构

```
internal/agentcore/harness/tools/multimodal/
├── doc.go                    # 包文档
├── vision.go                 # ImageOCRTool + VisualQuestionAnsweringTool + create_vision_tools
├── audio.go                  # 3个音频工具 + create_audio_tools
├── video_understanding.go    # VideoUnderstandingTool
├── audio_helpers.go          # 音频辅助函数（resolve_audio_path/get_audio_duration/encode_audio_file/acrcloud）
├── vision_helpers.go         # 视觉辅助函数（build_image_content/extract_response_text）
├── video_helpers.go          # 视频辅助函数（normalize_video_url）
├── multimodal_test.go        # 综合测试
├── vision_test.go            # 视觉工具测试
├── audio_test.go             # 音频工具测试
├── video_test.go             # 视频工具测试
```

### 3.2 Vision 工具（2个）

**ImageOCRTool** — 从图片提取文本

调用链（对齐 Python `vision.py`）：
```
ImageOCRTool.Invoke(ctx, inputs, opts)
  → buildImageContent(imagePathOrURL)     // URL→image_url block；本地→base64→data:URI
  → callVisionModel(imageContent, prompt, visionConfig)
    → 构造 UserMessage(WithMultiModalContent({type:"text",text:prompt}, {type:"image_url",image_url:{url}}))
    → BaseModelClient.Invoke(ctx, messages, opts...)  // 走框架调用链
    → extractResponseText(response)      // 提取文本
    → 指数退避重试（max_retries=3，429/500/502/503/504 可重试）
  → return {text, model}
```

**VisualQuestionAnsweringTool** — 理解图片并回答问题

调用链（对齐 Python）：
```
VQATool.Invoke(ctx, inputs, opts)
  → buildImageContent(imagePathOrURL)
  → if include_ocr:
      callVisionModel(imageContent, DEFAULT_OCR_PROMPT, visionConfig)  // 第一次调用：OCR
  → 构造 VQA prompt（DEFAULT_VQA_PROMPT_TEMPLATE 或纯 question）
  → callVisionModel(imageContent, vqaPrompt, visionConfig)             // 第二次调用：VQA
  → return {answer, ocr_text, model}
```

### 3.3 Audio 工具（3个）

**AudioTranscriptionTool** — 转写音频为文本

调用链（对齐 Python）：
```
AudioTranscriptionTool.Invoke(ctx, inputs, opts)
  → resolveAudioPath(audioPathOrURL, audioConfig)  // URL→下载到临时文件；本地→验证路径
  → BaseModelClient.TranscribeAudio(ctx, audioPath, opts...)  // 调用 audio.transcriptions API
  → 清理临时文件（finally）
  → return {text, model}
```

**AudioQuestionAnsweringTool** — 基于音频内容回答问题

调用链（对齐 Python）：
```
AudioQATool.Invoke(ctx, inputs, opts)
  → resolveAudioPath(audioPathOrURL, audioConfig)
  → encodeAudioFile(audioPath)                     // base64 编码 + MIME 推断
  → getAudioDuration(audioPath)                     // 计算时长（优先 wave，降级 mutagen 等效）
  → 构造 UserMessage(WithMultiModalContent({type:"text"}, {type:"input_audio",input_audio:{data,format}}))
  → BaseModelClient.Invoke(ctx, messages, opts...)  // 调用 chat.completions + input_audio
  → return {answer, duration_seconds, model}
```

**AudioMetadataTool** — 检查音频时长+识别歌曲元数据

调用链（对齐 Python）：
```
AudioMetadataTool.Invoke(ctx, inputs, opts)
  → resolveAudioPath(audioPathOrURL, audioConfig)
  → getAudioDuration(audioPath)                     // 计算时长
  → if ACR credentials configured AND duration ≤ 15s:
      invokeACRMetadata(audioPath, audioConfig)     // 独立 HTTP 调用 ACRCloud HMAC 签名
  → return {duration_seconds, title, artist, release_date, score, identified, note}
```

**特殊说明**：AudioMetadataTool 的 ACRCloud 部分不走 BaseModelClient，因为这是一个完全独立的第三方 HTTP API（HMAC 签名认证），与 LLM 调用无关。这与 Python 一致。

### 3.4 Video 工具（1个）

**VideoUnderstandingTool** — 理解视频并回答查询

调用链（对齐 Python `video_understanding.py`）：
```
VideoUnderstandingTool.Invoke(ctx, inputs, opts)
  → normalizeVideoURL(videoPath)                   // URL→保持；本地→base64→data:URI
  → 构造 UserMessage(WithMultiModalContent({type:"video_url",video_url:{url}}, {type:"text",text:query}))
  → BaseModelClient.Invoke(ctx, messages, opts...)  // 调用 chat.completions + video_url
  → extractResponseText(response)
  → return {query, video_path, model, answer}
```

## 4. 提示词

在 `harness/prompts/tools/` 下新增3个文件：

| 文件 | 内容 |
|------|------|
| `vision.go` | ImageOCR/VQA 双语描述 + JSON Schema + 2个 MetadataProvider |
| `audio.go` | 3个音频工具双语描述 + JSON Schema + 3个 MetadataProvider |
| `video_understanding.go` | Video 双语描述 + JSON Schema + 1个 MetadataProvider |

所有提示词字符串**一比一复刻 Python 原文**（按项目规则），不做自行翻译。

## 5. 工厂函数

对齐 Python 的 `create_vision_tools()` 和 `create_audio_tools()`：

```go
// CreateVisionTools 创建视觉工具集（共享同一个 VisionModelConfig）
func CreateVisionTools(visionConfig *hschema.VisionModelConfig, language, agentID string) []tool.Tool

// CreateAudioTools 创建音频工具集（共享同一个 AudioModelConfig）
func CreateAudioTools(audioConfig *hschema.AudioModelConfig, language, agentID string) []tool.Tool

// NewVideoUnderstandingTool 创建视频理解工具
func NewVideoUnderstandingTool(visionConfig *hschema.VisionModelConfig, language, agentID string) tool.Tool
```

## 6. 辅助函数详细设计

### 6.1 buildImageContent（对齐 Python `_build_image_content`）

```
输入: image_path_or_url string
输出: ContentPart (type="image_url")
逻辑:
  1. 检查 sandbox-only 路径（含 "home/user"）→ 拒绝
  2. HTTP URL → ContentPart{Type:"image_url", ImageURL:{URL:url}}
  3. 本地文件 → base64 编码 → data:URI → ContentPart{Type:"image_url", ImageURL:{URL:"data:mime;base64,..."}}
  4. 文件不存在 → 返回错误
```

### 6.2 resolveAudioPath（对齐 Python `_resolve_audio_path`）

```
输入: audio_path_or_url, audioConfig
输出: (localPath, shouldDelete, error)
逻辑:
  1. 检查 sandbox-only 路径 → 拒绝
  2. HTTP URL → 下载到临时文件（stream，检查 max_audio_bytes 限制）
  3. 本地文件 → 验证存在性
  4. 返回 shouldDelete 标记（URL 下载的文件需在 finally 中清理）
```

### 6.3 getAudioDuration（对齐 Python `_get_audio_duration`）

```
输入: audioPath string
输出: durationSeconds float64, error
逻辑:
  1. 尝试 wave 包解析（支持 WAV 格式）
  2. 降级：尝试 Go 音频库（如 go-audio 或类似 mutagen 等效）
  3. 全部失败 → 返回错误
```

**Go 与 Python 的差异**：Python 使用 `wave` + `mutagen`；Go 没有 `mutagen` 等效库。方案：
- WAV 文件用 Go 标准库解析（类似 Python wave）
- 非 WAV 文件：用 `os/exec` 调用 `ffprobe` 获取时长（如果可用），或尝试第三方 Go 音频元数据库
- 全部不可用时返回 duration=0 + note 提示

### 6.4 invokeACRMetadata（对齐 Python `_invoke_audio_metadata`）

```
输入: audioPath, audioConfig
输出: metadata map[string]any
逻辑:
  1. 计算 HMAC-SHA1 签名（对齐 Python 签名构造）
  2. POST multipart/form-data 到 acr_base_url
  3. 解析响应 JSON → 提取 music/humming 元数据
  4. 返回 {title, artist, release_date, score, identified}
```

### 6.5 normalizeVideoURL（对齐 Python `_normalize_video_url`）

```
输入: videoPath string
输出: url string (http URL 或 data:URI)
逻辑:
  1. HTTP URL → 保持原样
  2. 本地文件 → base64 编码 → data:mime;base64,... 格式
  3. 文件不存在 → 返回错误
```

### 6.6 callVisionModel（对齐 Python `_call_vision_model`）

```
输入: imageContent ContentPart, prompt string, visionConfig VisionModelConfig
输出: (responseText, modelName, error)
逻辑:
  1. 校验 visionConfig（api_key/base_url/model 必填）
  2. 构造 messages = [UserMessage(WithMultiModalContent({text,prompt}, imageContent))]
  3. 指数退避重试循环（1..max_retries）:
     - BaseModelClient.Invoke(ctx, messages, InvokeOptionWithModel(visionConfig.Model))
     - 429/500/502/503/504 → 重试
     - 其他错误 → 直接返回
  4. extractResponseText(response)
```

## 7. 测试策略

### 7.1 单元测试（mock BaseModelClient）

- mockVisionClient：返回固定 OCR/VQA 文本
- mockAudioClient：TranscribeAudio 返回固定转写文本，Invoke 返回固定回答
- 测试覆盖：每个工具的正常路径 + 所有错误路径
- **buildImageContent/resolveAudioPath/normalizeVideoURL** 等辅助函数独立测试

### 7.2 集成测试（build tag: llm）

- 真实调用 OpenAI 视觉/音频 API
- 真实调用 ACRCloud
- 使用 `//go:build llm` 标签隔离

### 7.3 ACRCloud 测试

- mock HTTP 服务器测试 HMAC 签名构造和响应解析
- 不依赖真实 ACRCloud 服务

## 8. 不在本设计范围内

- `mobile_gui/rails/multimodal_*` — 属于 9.31 MobileGUI Agent，不在 9.38-49 范围内
- 文生图工具（`generate_image`）— 属于 DashScope 客户端已实现的 `GenerateImage` 方法
- `xiaoyi_phone_tools` — 属于特定渠道实现，不在通用 Harness 工具范围内
