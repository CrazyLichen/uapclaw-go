# VideoUnderstandingTool 回填 + ThinkingEnabled + YAML 配置映射 设计

> 对齐 Python: jiuwenswarm/agents/harness/common/tools/video_tools.py + multimodal_config.py
> 以及 agent-core/openjiuwen/harness/tools/multimodal/video_understanding.py

## 1. 背景

IMPLEMENTATION_PLAN.md 中 9.38-49 已标记 ✅，VideoUnderstandingTool 的**核心实现**已完成
（`NewVideoUnderstandingTool` + `NormalizeVideoURL` + clampInt/clampFloat + 测试覆盖率）。
但 DeepAdapter 中有 3 处 `⤵️ 9.38-49` 标记尚未回填——视频工具从 DeepAdapter 到 Agent 运行时的
注册链路是断开的，工具不会自动出现在 Agent 的可用工具列表中。

同时 Go 当前实现只对齐了 agent-core 版（BaseModelClient.Invoke + VisionModelConfig），
缺少 jiuwenswarm 版的以下功能：
- thinking_enabled 参数支持
- glm-4.6v 默认模型
- YAML 配置映射（models.video.model_config → 环境变量）

本次设计补齐以上差距，并同时回填 Vision 和 Audio 工具的注册链路。

## 2. 设计决策

| 决策项 | 选择 | 原因 |
|--------|------|------|
| 对齐版本 | 方案C：回填 + YAML 配置 + thinking_enabled | 完整对齐 jiuwenswarm 版 |
| thinking 传递方式 | 方式A：WithInvokeExtra | 不修改 BaseModelClient 接口，Extra 支持嵌套字典合并 |
| 配置结构体 | 方案D1：独立 VideoModelConfig | 与 VisionModelConfig 完全独立，类型清晰，Vision 工具零变动 |
| 前置检查 | OpenAI client Extra 合并逻辑 | 已确认 BuildRequestParams 步骤5 直接赋值 Extra 键到 reqParams，嵌套字典正确传递 |
| 额外回填 | 同时回填 Vision（步骤4）和 Audio（步骤5）注册链路 | 同文件改动，最小成本完成更多回填 |

## 3. 配置层设计

### 3.1 VideoModelConfig 结构体

文件：`internal/agentcore/harness/schema/config.go`

```go
// VideoModelConfig 视频模型运行时配置
type VideoModelConfig struct {
    // APIKey API 密钥
    APIKey string `json:"api_key"`
    // BaseURL API 基础地址
    BaseURL string `json:"base_url"`
    // Model 模型名称（默认 glm-4.6v）
    Model string `json:"model"`
    // MaxRetries 最大重试次数
    MaxRetries int `json:"max_retries"`
    // ThinkingEnabled 是否启用思维模式
    ThinkingEnabled bool `json:"thinking_enabled"`
}
```

新增常量：

```go
const (
    // DefaultVideoModel 默认视频理解模型（对齐 jiuwenswarm: glm-4.6v）
    DefaultVideoModel = "glm-4.6v"
)
```

新增方法：

```go
// NewVideoModelConfig 创建带默认值的视频模型配置
func NewVideoModelConfig() *VideoModelConfig {
    return &VideoModelConfig{
        Model:      DefaultVideoModel,
        MaxRetries: 3,
    }
}

// FromEnv 从环境变量构建视频模型配置
func (VideoModelConfig) FromEnv() VideoModelConfig {
    apiKey := os.Getenv("VIDEO_API_KEY")
    baseURL := envOr("VIDEO_API_BASE", "ZHIPU_API_URL", "API_BASE")
    if baseURL == "" {
        baseURL = DefaultOpenAIBaseURL
    }
    model := envOr("VIDEO_MODEL_NAME", "MODEL_NAME")
    if model == "" {
        model = DefaultVideoModel
    }
    thinkingEnabled := os.Getenv("VIDEO_THINKING_ENABLED") == "true"
    return VideoModelConfig{
        APIKey:          apiKey,
        BaseURL:         baseURL,
        Model:           model,
        MaxRetries:      parseIntEnv("VIDEO_MAX_RETRIES", 3),
        ThinkingEnabled: thinkingEnabled,
    }
}
```

### 3.2 multimodal_config.go — YAML 配置映射

文件：`internal/swarm/server/adapter/multimodal_config.go`（新建）

对齐 Python: `jiuwenswarm/agents/harness/common/tools/multimodal_config.py`

核心函数：

```go
// ApplyVideoModelConfigFromYAML 从 config.yaml 读取视频模型配置并设置环境变量
// 对齐 Python: apply_video_model_config_from_yaml(config_base)
//
// 配置优先级:
// 1. models.video.model_config
// 2. embed.video_model + embed.embed_api_key/embed_api_base
// 3. 环境变量 VIDEO_API_KEY/VIDEO_API_BASE/VIDEO_MODEL_NAME
func ApplyVideoModelConfigFromYAML(configBase map[string]any) {
    // 1. 从 YAML 解析 models.video.model_config
    mc := getModelConfig(configBase, "video")
    apiKey := strVal(mc["api_key"])
    apiBase := strVal(mc["api_base"])
    modelName := strOr(mc["model_name"], mc["model"])
    provider := strVal(mc["model_provider"])
    strict := parseBool(mc["strict"], false)

    // 2. strict=false 时回落到 embed + 环境变量
    if !strict {
        embedCfg := getEmbedConfig(configBase)
        if apiKey == "" {
            apiKey = strOr(embedCfg["embed_api_key"], os.Getenv("API_KEY"))
        }
        if apiBase == "" {
            apiBase = strOr(embedCfg["embed_api_base"], os.Getenv("API_BASE"))
        }
        if modelName == "" {
            modelName = strOr(getEmbedModelName(embedCfg, "video"), os.Getenv("MODEL_NAME"))
        }
        if provider == "" {
            provider = os.Getenv("MODEL_PROVIDER")
        }
    } else {
        os.Setenv("VIDEO_UNDERSTANDING_STRICT", "1")
    }

    // 3. 写入环境变量
    if apiKey != "" { os.Setenv("VIDEO_API_KEY", apiKey) }
    if apiBase != "" { os.Setenv("VIDEO_API_BASE", apiBase) }
    if modelName != "" { os.Setenv("VIDEO_MODEL_NAME", modelName) }
    if provider != "" { os.Setenv("VIDEO_PROVIDER", provider) }
}

// DedicatedMultimodalModelConfigured 检查 models.{modelType} 是否有独立 api_key
// 对齐 Python: dedicated_multimodal_model_configured(config_base, model_type)
func DedicatedMultimodalModelConfigured(configBase map[string]any, modelType string) bool {
    if modelType != "audio" && modelType != "vision" && modelType != "video" {
        return false
    }
    mc := getModelConfig(configBase, modelType)
    apiKey := strings.TrimSpace(strVal(mc["api_key"]))
    return apiKey != ""
}
```

辅助函数（同文件）：

```go
// getModelConfig 从 config.yaml 解析 models.{modelType}.model_config
// 对齐 Python: _get_model_config(config_base, model_type)
func getModelConfig(configBase map[string]any, modelType string) map[string]any { ... }

// getEmbedConfig 从 config.yaml 解析 embed 配置
// 对齐 Python: _get_embed_config(config_base)
func getEmbedConfig(configBase map[string]any) map[string]any { ... }

// getEmbedModelName 从 embed 配置获取模型名称
// 对齐 Python: _get_embed_model_name(embed_cfg, model_type)
func getEmbedModelName(embedCfg map[string]any, modelType string) string { ... }

// parseBool 对齐 Python: _parse_bool(val, default)
func parseBool(val any, defaultVal bool) bool { ... }

// strVal / strOr 等字符串辅助
func strVal(v any) string { ... }
func strOr(v1, v2 any) string { ... }
```

## 4. 工具层设计

### 4.1 VideoUnderstandingInput 新增字段

文件：`internal/agentcore/harness/tools/multimodal/video_understanding.go`

```go
type VideoUnderstandingInput struct {
    Query           string  `json:"query"`
    VideoPath       string  `json:"video_path"`
    Model           string  `json:"model,omitempty"`
    MaxTokens       int     `json:"max_tokens,omitempty"`
    Temperature     float64 `json:"temperature,omitempty"`
    TimeoutSeconds  int     `json:"timeout_seconds,omitempty"`
    ThinkingEnabled bool    `json:"thinking_enabled,omitempty"`  // 新增
}
```

### 4.2 NewVideoUnderstandingTool 签名变更

```go
// 从：
func NewVideoUnderstandingTool(client modelclients.BaseModelClient, config *hschema.VisionModelConfig, language, agentID string) tool.Tool

// 改为：
func NewVideoUnderstandingTool(client modelclients.BaseModelClient, config *hschema.VideoModelConfig, language, agentID string) tool.Tool
```

### 4.3 thinking_enabled 传递逻辑

在闭包内，参数裁剪后、调用 client.Invoke 时：

```go
opts := []modelclients.InvokeOption{
    modelclients.WithInvokeModel(modelName),
    modelclients.WithInvokeMaxTokens(maxTokens),
    modelclients.WithInvokeTemperature(temperature),
    modelclients.WithInvokeTimeout(timeoutFloat),
}

// thinking_enabled → WithInvokeExtra（对齐 jiuwenswarm: payload["thinking"] = {"type": "enabled"}）
if input.ThinkingEnabled || (config != nil && config.ThinkingEnabled) {
    opts = append(opts, modelclients.WithInvokeExtra(map[string]any{
        "thinking": map[string]any{"type": "enabled"},
    }))
}

resp, err := client.Invoke(ctx, messages, opts...)
```

### 4.4 默认模型选择

```go
modelName := input.Model
if modelName == "" && config != nil {
    modelName = config.Model
}
if modelName == "" {
    modelName = defaultVideoModel  // "glm-4.6v"，对齐 jiuwenswarm
}
```

新增常量：

```go
const (
    // defaultVideoModel 默认视频理解模型（对齐 jiuwenswarm: glm-4.6v）
    defaultVideoModel = "glm-4.6v"
)
```

### 4.5 提示词参数描述更新

文件：`internal/agentcore/harness/prompts/tools/video_understanding.go`

在 `GetVideoUnderstandingMetadataProviderInputParams` 的 properties 中新增：

```go
"thinking_enabled": map[string]any{
    "type":        "boolean",
    "description": cnOrEn(language, "可选，是否启用思维模式", "Optional, enable thinking mode"),
},
```

注意：`thinking_enabled` 不是 `required` 字段。

## 5. 注册层设计（DeepAdapter 回填）

### 5.1 DeepAdapter 字段变更

文件：`internal/swarm/server/adapter/deep_adapter.go`

```go
// 从：
videoToolRegistered bool   // ⤵️ 10.6.24 多模态工具

// 改为：
videoModelConfig *hschema.VideoModelConfig  // 视频模型配置
```

保留 `videoToolRegistered bool` 用于热同步判断。

### 5.2 buildVideoModelConfig 回填

文件：`internal/swarm/server/adapter/deep_adapter_tools.go`

从返回 `bool` 改为返回 `*VideoModelConfig`：

```go
func (d *DeepAdapter) buildVideoModelConfig(configBase map[string]any) *hschema.VideoModelConfig {
    // 1. YAML 配置映射到环境变量
    ApplyVideoModelConfigFromYAML(configBase)

    // 2. 检查 models.video 是否有独立 api_key
    if !DedicatedMultimodalModelConfigured(configBase, "video") {
        logger.Info(logComponent).Msg("跳过 video_understanding: config.yaml 中 models.video 无独立 api_key")
        return nil
    }

    // 3. 从环境变量构建 VideoModelConfig
    cfg := hschema.VideoModelConfig{}.FromEnv()
    if cfg.APIKey == "" {
        logger.Info(logComponent).Msg("视频工具跳过: 配置不完整 (VIDEO_API_KEY 未设置)")
        return nil
    }
    return &cfg
}
```

### 5.3 getToolCards 步骤4/5/6 回填

**步骤4（Vision）**：

```go
if d.visionModelConfig != nil {
    client := d.resolveVisionModelClient()
    visionTools := multimodal.CreateVisionTools(client, d.visionModelConfig, d.resolveRuntimeLanguage(), agentID)
    for _, t := range visionTools {
        if err := runner.GetResourceMgr().AddTool(t); err != nil {
            logger.Warn(logComponent).Err(err).Msg("注册 vision 工具到 ResourceMgr 失败")
        }
        toolCards = append(toolCards, t.Card())
    }
    d.visionToolsRegistered = len(visionTools) > 0
}
```

**步骤5（Audio）**：

```go
if d.audioModelConfig != nil {
    client := d.resolveAudioModelClient()
    audioTools := multimodal.CreateAudioTools(client, d.audioModelConfig, d.resolveRuntimeLanguage(), agentID)
    for _, t := range audioTools {
        if err := runner.GetResourceMgr().AddTool(t); err != nil {
            logger.Warn(logComponent).Err(err).Msg("注册 audio 工具到 ResourceMgr 失败")
        }
        toolCards = append(toolCards, t.Card())
    }
    d.audioToolsRegistered = len(audioTools) > 0
}
```

**步骤6（Video）**：

```go
if d.videoModelConfig != nil {
    client := d.resolveVideoModelClient()
    videoTool := multimodal.NewVideoUnderstandingTool(client, d.videoModelConfig, d.resolveRuntimeLanguage(), agentID)
    if err := runner.GetResourceMgr().AddTool(videoTool); err != nil {
        logger.Warn(logComponent).Err(err).Msg("注册 video_understanding 到 ResourceMgr 失败")
    }
    toolCards = append(toolCards, videoTool.Card())
    d.videoToolRegistered = true
}
```

### 5.4 syncMultimodalToolsForRuntime 回填

**Vision 同步**：

```go
if d.visionModelConfig != nil && !d.visionToolsRegistered {
    client := d.resolveVisionModelClient()
    visionTools := multimodal.CreateVisionTools(client, d.visionModelConfig, d.resolveRuntimeLanguage(), "")
    cards := make([]*tool.ToolCard, len(visionTools))
    for i, t := range visionTools { cards[i] = t.Card() }
    d.syncToolsToManager(ctx, cards, visionTools, nil, "vision")
    d.visionToolsRegistered = true
}
if d.visionModelConfig == nil && d.visionToolsRegistered {
    d.removeRegisteredTools([]string{ToolNameVision})
    d.visionToolsRegistered = false
}
```

**Audio 同步**：

```go
if d.audioModelConfig != nil && !d.audioToolsRegistered {
    client := d.resolveAudioModelClient()
    audioTools := multimodal.CreateAudioTools(client, d.audioModelConfig, d.resolveRuntimeLanguage(), "")
    cards := make([]*tool.ToolCard, len(audioTools))
    for i, t := range audioTools { cards[i] = t.Card() }
    d.syncToolsToManager(ctx, cards, audioTools, nil, "audio")
    d.audioToolsRegistered = true
}
if d.audioModelConfig == nil && d.audioToolsRegistered {
    d.removeRegisteredTools([]string{ToolNameAudioTranscription})
    d.audioToolsRegistered = false
}
```

**Video 同步**：

```go
if d.videoModelConfig != nil && !d.videoToolRegistered {
    client := d.resolveVideoModelClient()
    videoTool := multimodal.NewVideoUnderstandingTool(client, d.videoModelConfig, d.resolveRuntimeLanguage(), "")
    d.syncToolsToManager(ctx, []*tool.ToolCard{videoTool.Card()}, []tool.Tool{videoTool}, nil, "video")
    d.videoToolRegistered = true
}
if d.videoModelConfig == nil && d.videoToolRegistered {
    d.removeRegisteredTools([]string{ToolNameVideoUnderstanding})
    d.videoToolRegistered = false
}
```

### 5.5 resolveModelClient 辅助方法

```go
func (d *DeepAdapter) resolveVisionModelClient() modelclients.BaseModelClient {
    return d.resolveModelClientFromConfig(d.visionModelConfig.APIKey, d.visionModelConfig.BaseURL, d.visionModelConfig.Model, d.visionModelConfig.MaxRetries)
}

func (d *DeepAdapter) resolveAudioModelClient() modelclients.BaseModelClient {
    return d.resolveModelClientFromConfig(d.audioModelConfig.APIKey, d.audioModelConfig.BaseURL, d.audioModelConfig.QAModel, d.audioModelConfig.MaxRetries)
}

func (d *DeepAdapter) resolveVideoModelClient() modelclients.BaseModelClient {
    return d.resolveModelClientFromConfig(d.videoModelConfig.APIKey, d.videoModelConfig.BaseURL, d.videoModelConfig.Model, d.videoModelConfig.MaxRetries)
}

func (d *DeepAdapter) resolveModelClientFromConfig(apiKey, baseURL, model string, maxRetries int) modelclients.BaseModelClient {
    config := &modelclients.ClientConfig{
        APIKey:     apiKey,
        APIBase:    baseURL,
        ModelName:  model,
        MaxRetries: maxRetries,
    }
    client, err := modelclients.NewOpenAIClient(config)
    if err != nil {
        logger.Error(logComponent).Err(err).Str("model", model).Msg("构建模型客户端失败")
        return nil
    }
    return client
}
```

## 6. 测试设计

### 6.1 video_test.go 更新

- 修改 `TestNewVideoUnderstandingTool_*` 系列测试：`*hschema.VisionModelConfig` → `*hschema.VideoModelConfig`
- 新增 `TestNewVideoUnderstandingTool_ThinkingEnabled` — 验证 thinking_enabled=true 时 Extra 包含 thinking payload
- 新增 `TestNewVideoUnderstandingTool_ThinkingEnabledFalse` — 验证默认不传 thinking
- 新增 `TestNewVideoUnderstandingTool_默认模型GLM4v` — 验证 config.Model 为空时使用 "glm-4.6v"
- 更新 `TestVideoConstants` 新增 `defaultVideoModel` 验证

### 6.2 config_test.go 新增

- `TestVideoModelConfig_FromEnv` — 环境变量构建
- `TestVideoModelConfig_NewVideoModelConfig` — 默认值验证
- `TestVideoModelConfig_DefaultConstants` — 常量验证

### 6.3 multimodal_config_test.go（新建）

- `TestApplyVideoModelConfigFromYAML_YAML配置优先`
- `TestApplyVideoModelConfigFromYAML_Embed回退`
- `TestApplyVideoModelConfigFromYAML_Strict模式`
- `TestDedicatedMultimodalModelConfigured_有独立APIKey`
- `TestDedicatedMultimodalModelConfigured_无独立APIKey`

### 6.4 deep_adapter_tools_test.go 更新

- 新增 `TestBuildVideoModelConfig_YAML配置` — 验证从 YAML 构建 VideoModelConfig
- 新增 `TestBuildVideoModelConfig_Strict模式` — strict=true 不回落
- 新增 `TestGetToolCards_Video注册` — 验证步骤6
- 新增 `TestSyncMultimodalToolsForRuntime_Video` — 验证热同步

## 7. IMPLEMENTATION_PLAN.md 更新

更新 9.38-49 描述，将 `✅VideoUnderstandingTool` 改为：

```
✅VideoUnderstandingTool(incl.ThinkingEnabled+VideoModelConfig+glm-4.6v默认+注册链路回填)
```

清除 DeepAdapter_tools.go 中所有视频相关 ⤵️ 标记。
同时标记 Vision 和 Audio 注册回填完成。

## 8. 改动文件清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `harness/schema/config.go` | 修改 | 新增 VideoModelConfig + 常量/方法 |
| `harness/schema/config_test.go` | 修改 | 新增 VideoModelConfig 测试 |
| `harness/tools/multimodal/video_understanding.go` | 修改 | 签名变更 + ThinkingEnabled + 默认模型 |
| `harness/tools/multimodal/video_helpers.go` | 不改 | 已完整 |
| `harness/tools/multimodal/video_test.go` | 修改 | 适配新签名 + 新增测试 |
| `harness/prompts/tools/video_understanding.go` | 修改 | 新增 thinking_enabled 参数描述 |
| `swarm/server/adapter/deep_adapter.go` | 修改 | 字段变更 + resolveModelClient |
| `swarm/server/adapter/deep_adapter_tools.go` | 修改 | 3处回填 + Vision/Audio 回填 |
| `swarm/server/adapter/multimodal_config.go` | 新建 | YAML 配置映射 |
| `swarm/server/adapter/multimodal_config_test.go` | 新建 | 映射测试 |
| `IMPLEMENTATION_PLAN.md` | 修改 | 更新回填状态 |
