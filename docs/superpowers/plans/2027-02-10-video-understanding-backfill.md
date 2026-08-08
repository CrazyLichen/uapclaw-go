# VideoUnderstandingTool 回填 + ThinkingEnabled + YAML 配置映射 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 回填 VideoUnderstandingTool 的 DeepAdapter 注册链路，新增 VideoModelConfig 结构体、thinking_enabled 参数支持、YAML 配置映射，同时回填 Vision 和 Audio 工具注册链路。

**Architecture:** 4 层改动——配置层（VideoModelConfig + multimodal_config YAML 映射）→ 工具层（签名变更 + ThinkingEnabled + 默认模型 glm-4.6v）→ 注册层（DeepAdapter 3 处回填 + Vision/Audio 回填）→ 测试 + IMPLEMENTATION_PLAN 更新。

**Tech Stack:** Go 1.22+，BaseModelClient + WithInvokeExtra 传递 thinking payload，os.Setenv 环境变量映射

---

## 文件结构

| 文件路径 | 操作 | 职责 |
|----------|------|------|
| `internal/agentcore/harness/schema/config.go` | 修改 | 新增 VideoModelConfig 结构体 + 常量/构造函数/FromEnv |
| `internal/agentcore/harness/schema/config_test.go` | 修改 | 新增 VideoModelConfig 测试 |
| `internal/agentcore/harness/tools/multimodal/video_understanding.go` | 修改 | 签名从 VisionModelConfig → VideoModelConfig + ThinkingEnabled + defaultVideoModel |
| `internal/agentcore/harness/tools/multimodal/video_helpers.go` | 不改 | 已完整 |
| `internal/agentcore/harness/tools/multimodal/video_test.go` | 修改 | 适配新签名 + 新增 ThinkingEnabled/默认模型测试 |
| `internal/agentcore/harness/prompts/tools/video_understanding.go` | 修改 | 新增 thinking_enabled 参数描述 |
| `internal/swarm/server/adapter/deep_adapter.go` | 修改 | videoToolRegistered → videoModelConfig + resolveModelClientFromConfig |
| `internal/swarm/server/adapter/deep_adapter_tools.go` | 修改 | buildVideoModelConfig 回填 + getToolCards 步骤4/5/6 + syncMultimodalToolsForRuntime |
| `internal/swarm/server/adapter/multimodal_config.go` | 新建 | YAML 配置映射（ApplyVideoModelConfigFromYAML + DedicatedMultimodalModelConfigured） |
| `internal/swarm/server/adapter/multimodal_config_test.go` | 新建 | 映射测试 |
| `IMPLEMENTATION_PLAN.md` | 修改 | 更新回填状态标记 |

---

### Task 1: VideoModelConfig 结构体 + 常量/构造函数/FromEnv

**Files:**
- Modify: `internal/agentcore/harness/schema/config.go`
- Modify: `internal/agentcore/harness/schema/config_test.go`

- [ ] **Step 1: 在 config.go 常量区块新增 DefaultVideoModel**

在 `config.go` 的常量区块（约 L242-267），在 `DefaultACRBaseURL` 之后新增：

```go
	// DefaultVideoModel 默认视频理解模型（对齐 jiuwenswarm: glm-4.6v）
	DefaultVideoModel = "glm-4.6v"
```

- [ ] **Step 2: 在 config.go 结构体区块新增 VideoModelConfig**

在 `AudioModelConfig` 结构体之后（约 L64），新增：

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
	// ThinkingEnabled 是否启用思维模式（对齐 jiuwenswarm: thinking_enabled）
	ThinkingEnabled bool `json:"thinking_enabled"`
}
```

- [ ] **Step 3: 在 config.go 导出函数区块新增 NewVideoModelConfig 和 FromEnv**

在 `NewAudioModelConfig` 之后（约 L341），新增：

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

- [ ] **Step 4: 在 config_test.go 新增 VideoModelConfig 测试**

在 `config_test.go` 文件末尾新增：

```go
func TestNewVideoModelConfig(t *testing.T) {
	cfg := schema.NewVideoModelConfig()
	if cfg.Model != schema.DefaultVideoModel {
		t.Errorf("Model = %v, 期望 %v", cfg.Model, schema.DefaultVideoModel)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %v, 期望 3", cfg.MaxRetries)
	}
	if cfg.ThinkingEnabled {
		t.Error("ThinkingEnabled 默认应为 false")
	}
}

func TestVideoModelConfig_FromEnv(t *testing.T) {
	os.Setenv("VIDEO_API_KEY", "test-key")
	os.Setenv("VIDEO_API_BASE", "https://api.example.com/v1")
	os.Setenv("VIDEO_MODEL_NAME", "glm-4.6v")
	os.Setenv("VIDEO_THINKING_ENABLED", "true")
	defer func() {
		os.Unsetenv("VIDEO_API_KEY")
		os.Unsetenv("VIDEO_API_BASE")
		os.Unsetenv("VIDEO_MODEL_NAME")
		os.Unsetenv("VIDEO_THINKING_ENABLED")
	}()

	cfg := schema.VideoModelConfig{}.FromEnv()
	if cfg.APIKey != "test-key" {
		t.Errorf("APIKey = %v, 期望 test-key", cfg.APIKey)
	}
	if cfg.BaseURL != "https://api.example.com/v1" {
		t.Errorf("BaseURL = %v", cfg.BaseURL)
	}
	if cfg.Model != "glm-4.6v" {
		t.Errorf("Model = %v, 期望 glm-4.6v", cfg.Model)
	}
	if !cfg.ThinkingEnabled {
		t.Error("ThinkingEnabled 应为 true")
	}
}

func TestVideoModelConfig_DefaultConstants(t *testing.T) {
	if schema.DefaultVideoModel != "glm-4.6v" {
		t.Errorf("DefaultVideoModel = %v, 期望 glm-4.6v", schema.DefaultVideoModel)
	}
}
```

- [ ] **Step 5: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/schema/... -v -run "TestVideoModel|TestNewVideoModel" -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```
feat(harness/schema): 新增 VideoModelConfig 结构体（对齐 jiuwenswarm glm-4.6v + thinking_enabled）
```

---

### Task 2: multimodal_config.go — YAML 配置映射

**Files:**
- Create: `internal/swarm/server/adapter/multimodal_config.go`
- Create: `internal/swarm/server/adapter/multimodal_config_test.go`

- [ ] **Step 1: 创建 multimodal_config.go**

创建 `/home/opensource/uap-claw-go/internal/swarm/server/adapter/multimodal_config.go`，内容对齐 Python `jiuwenswarm/agents/harness/common/tools/multimodal_config.py`：

```go
package adapter

import (
	"os"
	"strings"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ApplyVideoModelConfigFromYAML 从 config.yaml 读取视频模型配置并设置环境变量。
// 对齐 Python: apply_video_model_config_from_yaml(config_base)
//
// 配置优先级:
// 1. models.video.model_config
// 2. embed.video_model + embed.embed_api_key/embed_api_base
// 3. 环境变量 VIDEO_API_KEY/VIDEO_API_BASE/VIDEO_MODEL_NAME
func ApplyVideoModelConfigFromYAML(configBase map[string]any) {
	if configBase == nil {
		os.Unsetenv("VIDEO_UNDERSTANDING_STRICT")
		return
	}

	mc := getModelConfig(configBase, "video")
	apiKey := strVal(mc["api_key"])
	apiBase := strVal(mc["api_base"])
	modelName := strOr(mc["model_name"], mc["model"])
	provider := strVal(mc["model_provider"])
	strict := parseBool(mc["strict"], false)

	if strict {
		os.Setenv("VIDEO_UNDERSTANDING_STRICT", "1")
	} else {
		os.Unsetenv("VIDEO_UNDERSTANDING_STRICT")
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
	}

	if apiKey != "" {
		os.Setenv("VIDEO_API_KEY", apiKey)
	}
	if apiBase != "" {
		os.Setenv("VIDEO_API_BASE", apiBase)
	}
	if modelName != "" {
		os.Setenv("VIDEO_MODEL_NAME", modelName)
	}
	if provider != "" {
		os.Setenv("VIDEO_PROVIDER", provider)
	}
}

// DedicatedMultimodalModelConfigured 检查 models.{modelType} 是否有独立 api_key。
// 对齐 Python: dedicated_multimodal_model_configured(config_base, model_type)
func DedicatedMultimodalModelConfigured(configBase map[string]any, modelType string) bool {
	if modelType != "audio" && modelType != "vision" && modelType != "video" {
		return false
	}
	mc := getModelConfig(configBase, modelType)
	apiKey := strings.TrimSpace(strVal(mc["api_key"]))
	return apiKey != ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// embedModelKeyMap embed 配置中的模型名称键映射。
// 对齐 Python: _EMBED_MODEL_KEY_MAP
var embedModelKeyMap = map[string]string{
	"audio":     "audio_model",
	"vision":    "vision_model",
	"video":     "video_model",
	"image_gen": "image_gen_model",
}

// getModelConfig 从 config.yaml 解析 models.{modelType}.model_config。
// 对齐 Python: _get_model_config(config_base, model_type)
func getModelConfig(configBase map[string]any, modelType string) map[string]any {
	if configBase == nil {
		return nil
	}
	rawModels, _ := configBase["models"].(map[string]any)
	if rawModels != nil {
		inner, _ := rawModels[modelType].(map[string]any)
		if inner != nil {
			mc, _ := inner["model_config"].(map[string]any)
			if mc != nil {
				return mc
			}
			mc2, _ := inner["model_client_config"].(map[string]any)
			if mc2 != nil {
				return mc2
			}
		}
		return nil
	}
	// models 可能是列表格式
	rawModelsList, _ := configBase["models"].([]any)
	if rawModelsList != nil {
		for _, block := range rawModelsList {
			b, _ := block.(map[string]any)
			if b == nil {
				continue
			}
			inner, _ := b[modelType].(map[string]any)
			if inner != nil {
				mc, _ := inner["model_config"].(map[string]any)
				if mc != nil {
					return mc
				}
				mc2, _ := inner["model_client_config"].(map[string]any)
				if mc2 != nil {
					return mc2
				}
			}
		}
	}
	return nil
}

// getEmbedConfig 从 config.yaml 解析 embed 配置。
// 对齐 Python: _get_embed_config(config_base)
func getEmbedConfig(configBase map[string]any) map[string]any {
	if configBase == nil {
		return nil
	}
	embed, _ := configBase["embed"].(map[string]any)
	return embed
}

// getEmbedModelName 从 embed 配置获取模型名称。
// 对齐 Python: _get_embed_model_name(embed_cfg, model_type)
func getEmbedModelName(embedCfg map[string]any, modelType string) string {
	key, ok := embedModelKeyMap[modelType]
	if !ok || embedCfg == nil {
		return ""
	}
	return strings.TrimSpace(strVal(embedCfg[key]))
}

// parseBool 解析布尔值。
// 对齐 Python: _parse_bool(val, default)
func parseBool(val any, defaultVal bool) bool {
	if val == nil {
		return defaultVal
	}
	if b, ok := val.(bool); ok {
		return b
	}
	s := strings.ToLower(strings.TrimSpace(strVal(val)))
	return s == "1" || s == "true" || s == "yes" || s == "on"
}

// strVal 提取字符串值
func strVal(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(strings.ToString(v))
}

// strOr 返回第一个非空字符串值
func strOr(v1, v2 any) string {
	s1 := strVal(v1)
	if s1 != "" {
		return s1
	}
	return strVal(v2)
}
```

注意：`strings.ToString` 不存在于标准库。需要使用 `fmt.Sprintf("%v", v)` 替代。修正：

```go
// strVal 提取字符串值
func strVal(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}
```

需要补充 `fmt` import。

- [ ] **Step 2: 创建 multimodal_config_test.go**

创建 `/home/opensource/uap-claw-go/internal/swarm/server/adapter/multimodal_config_test.go`：

```go
package adapter

import (
	"os"
	"testing"
)

func TestApplyVideoModelConfigFromYAML_YAML配置优先(t *testing.T) {
	configBase := map[string]any{
		"models": map[string]any{
			"video": map[string]any{
				"model_config": map[string]any{
					"api_key":    "yaml-video-key",
					"api_base":   "https://yaml-video.example.com/v1",
					"model_name": "yaml-video-model",
				},
			},
		},
	}

	ApplyVideoModelConfigFromYAML(configBase)
	defer func() {
		os.Unsetenv("VIDEO_API_KEY")
		os.Unsetenv("VIDEO_API_BASE")
		os.Unsetenv("VIDEO_MODEL_NAME")
	}()

	if os.Getenv("VIDEO_API_KEY") != "yaml-video-key" {
		t.Errorf("VIDEO_API_KEY = %v, 期望 yaml-video-key", os.Getenv("VIDEO_API_KEY"))
	}
	if os.Getenv("VIDEO_API_BASE") != "https://yaml-video.example.com/v1" {
		t.Errorf("VIDEO_API_BASE = %v", os.Getenv("VIDEO_API_BASE"))
	}
	if os.Getenv("VIDEO_MODEL_NAME") != "yaml-video-model" {
		t.Errorf("VIDEO_MODEL_NAME = %v, 期望 yaml-video-model", os.Getenv("VIDEO_MODEL_NAME"))
	}
}

func TestApplyVideoModelConfigFromYAML_Embed回退(t *testing.T) {
	configBase := map[string]any{
		"embed": map[string]any{
			"embed_api_key":  "embed-key",
			"embed_api_base": "https://embed.example.com/v1",
			"video_model":    "embed-video-model",
		},
	}

	ApplyVideoModelConfigFromYAML(configBase)
	defer func() {
		os.Unsetenv("VIDEO_API_KEY")
		os.Unsetenv("VIDEO_API_BASE")
		os.Unsetenv("VIDEO_MODEL_NAME")
	}()

	if os.Getenv("VIDEO_API_KEY") != "embed-key" {
		t.Errorf("VIDEO_API_KEY = %v, 期望 embed-key", os.Getenv("VIDEO_API_KEY"))
	}
	if os.Getenv("VIDEO_API_BASE") != "https://embed.example.com/v1" {
		t.Errorf("VIDEO_API_BASE = %v", os.Getenv("VIDEO_API_BASE"))
	}
	if os.Getenv("VIDEO_MODEL_NAME") != "embed-video-model" {
		t.Errorf("VIDEO_MODEL_NAME = %v", os.Getenv("VIDEO_MODEL_NAME"))
	}
}

func TestApplyVideoModelConfigFromYAML_Strict模式(t *testing.T) {
	configBase := map[string]any{
		"models": map[string]any{
			"video": map[string]any{
				"model_config": map[string]any{
					"api_key": "strict-key",
					"strict":  true,
				},
			},
		},
		"embed": map[string]any{
			"embed_api_key": "should-not-be-used",
		},
	}

	ApplyVideoModelConfigFromYAML(configBase)
	defer func() {
		os.Unsetenv("VIDEO_API_KEY")
		os.Unsetenv("VIDEO_UNDERSTANDING_STRICT")
	}()

	if os.Getenv("VIDEO_API_KEY") != "strict-key" {
		t.Errorf("VIDEO_API_KEY = %v, 期望 strict-key", os.Getenv("VIDEO_API_KEY"))
	}
	if os.Getenv("VIDEO_UNDERSTANDING_STRICT") != "1" {
		t.Error("VIDEO_UNDERSTANDING_STRICT 应为 1")
	}
}

func TestDedicatedMultimodalModelConfigured_有独立APIKey(t *testing.T) {
	configBase := map[string]any{
		"models": map[string]any{
			"video": map[string]any{
				"model_config": map[string]any{
					"api_key": "my-video-key",
				},
			},
		},
	}
	if !DedicatedMultimodalModelConfigured(configBase, "video") {
		t.Error("应有独立 api_key")
	}
}

func TestDedicatedMultimodalModelConfigured_无独立APIKey(t *testing.T) {
	configBase := map[string]any{
		"models": map[string]any{
			"video": map[string]any{
				"model_config": map[string]any{},
			},
		},
	}
	if DedicatedMultimodalModelConfigured(configBase, "video") {
		t.Error("不应有独立 api_key")
	}
}
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/server/adapter/... -v -run "TestApplyVideoModel|TestDedicatedMultimodal" -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```
feat(swarm/adapter): 新增 multimodal_config.go — YAML 配置映射（对齐 Python multimodal_config.py）
```

---

### Task 3: VideoUnderstandingTool 签名变更 + ThinkingEnabled + 默认模型

**Files:**
- Modify: `internal/agentcore/harness/tools/multimodal/video_understanding.go`
- Modify: `internal/agentcore/harness/prompts/tools/video_understanding.go`

- [ ] **Step 1: 修改 VideoUnderstandingInput 新增 ThinkingEnabled 字段**

在 `video_understanding.go` 的 `VideoUnderstandingInput` 结构体（约 L19-32），在 `TimeoutSeconds` 后新增：

```go
	// ThinkingEnabled 可选，是否启用思维模式（对齐 jiuwenswarm: thinking_enabled, 默认 false）
	ThinkingEnabled bool `json:"thinking_enabled,omitempty"`
```

- [ ] **Step 2: 新增 defaultVideoModel 常量**

在 `video_understanding.go` 的常量区块（约 L36-55），在 `maxVideoTimeout` 之后新增：

```go
	// defaultVideoModel 默认视频理解模型（对齐 jiuwenswarm: glm-4.6v）
	defaultVideoModel = "glm-4.6v"
```

- [ ] **Step 3: 修改 NewVideoUnderstandingTool 签名**

将 `NewVideoUnderstandingTool` 函数签名（约 L63-67）从：

```go
func NewVideoUnderstandingTool(
	client modelclients.BaseModelClient,
	config *hschema.VisionModelConfig,
	language, agentID string,
) tool.Tool {
```

改为：

```go
func NewVideoUnderstandingTool(
	client modelclients.BaseModelClient,
	config *hschema.VideoModelConfig,
	language, agentID string,
) tool.Tool {
```

同时更新函数注释（约 L59-62）：

```go
// NewVideoUnderstandingTool 创建视频理解工具。
//
// 对齐 Python: VideoUnderstandingTool.__init__ + VideoUnderstandingTool.invoke
// 使用 VideoModelConfig 配置（独立于 VisionModelConfig），通过 BaseModelClient.Invoke 调用 video_url 消息
// 支持 ThinkingEnabled 参数，通过 WithInvokeExtra 传递 thinking payload
```

- [ ] **Step 4: 修改闭包内默认模型选择逻辑**

将闭包内模型选择逻辑（约 L95-104）从：

```go
			modelName := input.Model
			if modelName == "" {
				modelName = config.Model
			}
			if modelName == "" {
				return nil, exception.NewBaseError(
					exception.StatusToolMultimodalVideoConfigInvalid,
					exception.WithMsg("video understanding model name is empty"),
				)
			}
```

改为：

```go
			// 参数裁剪（对齐 Python: max(128, min(max_tokens, 8192)) 等）
			modelName := input.Model
			if modelName == "" && config != nil {
				modelName = config.Model
			}
			if modelName == "" {
				modelName = defaultVideoModel // 对齐 jiuwenswarm: glm-4.6v
			}
```

注意：这里不再返回错误，而是使用默认值 `defaultVideoModel`（对齐 Python jiuwenswarm 版的 `default_model = "glm-4.6v"`）。

- [ ] **Step 5: 修改 Invoke 调用逻辑，替换为 opts 模式 + thinking_enabled**

将闭包内 Invoke 调用逻辑（约 L126-133）从：

```go
			// 调用模型（对齐 Python: self.model.invoke(messages, model, max_tokens, temperature, timeout)）
			timeoutFloat := float64(timeoutSeconds)
			resp, err := client.Invoke(ctx, messages,
				modelclients.WithInvokeModel(modelName),
				modelclients.WithInvokeMaxTokens(maxTokens),
				modelclients.WithInvokeTemperature(temperature),
				modelclients.WithInvokeTimeout(timeoutFloat),
			)
```

改为：

```go
			// 调用模型（对齐 Python: self.model.invoke(messages, model, max_tokens, temperature, timeout)）
			timeoutFloat := float64(timeoutSeconds)
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

- [ ] **Step 6: 修改配置校验错误消息**

将配置校验逻辑（约 L72-78）的错误消息从 `"vision_model_config is not configured"` 改为 `"video_model_config is not configured"`：

```go
			if config == nil || config.APIKey == "" {
				return nil, exception.NewBaseError(
					exception.StatusToolMultimodalVideoConfigInvalid,
					exception.WithMsg("video_model_config is not configured: api_key is required"),
				)
			}
```

- [ ] **Step 7: 更新提示词参数描述**

在 `video_understanding.go`（prompts/tools）的 `GetVideoUnderstandingMetadataProviderInputParams` 函数中，在 `p` map（约 L24-31）新增：

```go
		"thinking_enabled": {"cn": "可选，是否启用思维模式", "en": "Optional, enable thinking mode"},
```

在 properties map（约 L40-47）新增：

```go
			"thinking_enabled": map[string]any{"type": "boolean", "description": d("thinking_enabled")},
```

- [ ] **Step 8: 运行编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/harness/...`
Expected: 编译成功

- [ ] **Step 9: 提交**

```
feat(harness/multimodal): VideoUnderstandingTool 改用 VideoModelConfig + ThinkingEnabled + glm-4.6v 默认模型
```

---

### Task 4: video_test.go 适配新签名 + 新增测试

**Files:**
- Modify: `internal/agentcore/harness/tools/multimodal/video_test.go`

- [ ] **Step 1: 读取当前 video_test.go 并定位所有 VisionModelConfig 引用**

先读取文件，找到所有使用 `hschema.VisionModelConfig` 的地方，替换为 `hschema.VideoModelConfig`。

- [ ] **Step 2: 全量替换 VisionModelConfig → VideoModelConfig**

在 `video_test.go` 中将所有 `hschema.VisionModelConfig` 替换为 `hschema.VideoModelConfig`。
同时将构造函数中的 `&hschema.VisionModelConfig{...}` 改为 `&hschema.VideoModelConfig{...}`。
注意保持 APIKey/BaseURL/Model/MaxRetries 字段不变，新增 ThinkingEnabled 字段（测试中默认不设置或设为 false）。

- [ ] **Step 3: 新增 TestNewVideoUnderstandingTool_ThinkingEnabled 测试**

```go
func TestNewVideoUnderstandingTool_ThinkingEnabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mock_model_clients.NewMockBaseModelClient(ctrl)
	config := &hschema.VideoModelConfig{
		APIKey:          "test-key",
		BaseURL:         "https://api.example.com/v1",
		Model:           "glm-4.6v",
		ThinkingEnabled: true,
	}

	// 验证 Invoke 时 Extra 包含 thinking payload
	mockClient.EXPECT().Invoke(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, msgs modelclients.MessagesParam, opts ...modelclients.InvokeOption) (*llmschema.AssistantMessage, error) {
			// 检查 opts 中是否包含 WithInvokeExtra
			params := modelclients.NewInvokeParams(opts...)
			if params.Extra == nil {
				t.Error("Expected Extra to contain thinking payload")
			}
			thinking, _ := params.Extra["thinking"].(map[string]any)
			if thinking["type"] != "enabled" {
				t.Errorf("thinking.type = %v, 期望 enabled", thinking["type"])
			}
			return llmschema.NewAssistantMessage("视频内容分析结果", nil), nil
		},
	).Times(1)

	videoTool := multimodal.NewVideoUnderstandingTool(mockClient, config, "cn", "test-agent")

	input := multimodal.VideoUnderstandingInput{
		Query:           "视频中发生了什么？",
		VideoPath:       "https://example.com/video.mp4",
		ThinkingEnabled: true,
	}

	_, err := videoTool.Invoke(context.Background(), input)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
}
```

- [ ] **Step 4: 新增 TestNewVideoUnderstandingTool_ThinkingEnabledFalse 测试**

```go
func TestNewVideoUnderstandingTool_ThinkingEnabledFalse(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mock_model_clients.NewMockBaseModelClient(ctrl)
	config := &hschema.VideoModelConfig{
		APIKey: "test-key",
		Model:  "glm-4.6v",
	}

	mockClient.EXPECT().Invoke(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, msgs modelclients.MessagesParam, opts ...modelclients.InvokeOption) (*llmschema.AssistantMessage, error) {
			params := modelclients.NewInvokeParams(opts...)
			if params.Extra != nil {
				if _, ok := params.Extra["thinking"]; ok {
					t.Error("ThinkingEnabled=false 时不应有 thinking payload")
				}
			}
			return llmschema.NewAssistantMessage("结果", nil), nil
		},
	).Times(1)

	videoTool := multimodal.NewVideoUnderstandingTool(mockClient, config, "cn", "test-agent")
	input := multimodal.VideoUnderstandingInput{
		Query:     "视频中有什么？",
		VideoPath: "https://example.com/video.mp4",
	}

	_, err := videoTool.Invoke(context.Background(), input)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
}
```

- [ ] **Step 5: 新增 TestNewVideoUnderstandingTool_默认模型GLM4v 测试**

```go
func TestNewVideoUnderstandingTool_默认模型GLM4v(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mock_model_clients.NewMockBaseModelClient(ctrl)
	config := &hschema.VideoModelConfig{
		APIKey: "test-key",
		// Model 字段为空，应使用 defaultVideoModel = "glm-4.6v"
	}

	mockClient.EXPECT().Invoke(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, msgs modelclients.MessagesParam, opts ...modelclients.InvokeOption) (*llmschema.AssistantMessage, error) {
			params := modelclients.NewInvokeParams(opts...)
			if params.Model != "glm-4.6v" {
				t.Errorf("Model = %v, 期望 glm-4.6v", params.Model)
			}
			return llmschema.NewAssistantMessage("结果", nil), nil
		},
	).Times(1)

	videoTool := multimodal.NewVideoUnderstandingTool(mockClient, config, "cn", "test-agent")
	input := multimodal.VideoUnderstandingInput{
		Query:     "视频描述",
		VideoPath: "https://example.com/video.mp4",
	}

	_, err := videoTool.Invoke(context.Background(), input)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
}
```

- [ ] **Step 6: 更新 TestVideoConstants 新增 defaultVideoModel**

在 `TestVideoConstants` 测试中新增验证：

```go
	if defaultVideoModel != "glm-4.6v" {
		t.Errorf("defaultVideoModel = %v, 期望 glm-4.6v", defaultVideoModel)
	}
```

注意：由于 `defaultVideoModel` 是包内常量，同包测试可直接访问。

- [ ] **Step 7: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/tools/multimodal/... -v -run "TestVideo|TestNewVideoUnderstanding" -count=1`
Expected: PASS

- [ ] **Step 8: 提交**

```
test(harness/multimodal): 适配 VideoModelConfig 签名 + 新增 ThinkingEnabled/默认模型测试
```

---

### Task 5: DeepAdapter 字段变更 + resolveModelClient 辅助方法

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go`
- Modify: `internal/swarm/server/adapter/deep_adapter_tools.go`

- [ ] **Step 1: 在 deep_adapter.go 新增 videoModelConfig 字段**

在 `deep_adapter.go` 的 DeepAdapter 结构体中，找到 `videoToolRegistered bool` 字段（标记 `⤵️ 10.6.24`），在其前面新增：

```go
	// videoModelConfig 视频模型配置
	videoModelConfig *hschema.VideoModelConfig
```

保留原有的 `videoToolRegistered bool` 字段（用于热同步判断）。

- [ ] **Step 2: 修改 refreshMultimodalConfigs 方法**

在 `deep_adapter_tools.go` 的 `refreshMultimodalConfigs` 方法（约 L302-307），修改 `d.videoToolRegistered = d.buildVideoModelConfig(configBase)` 为：

```go
	d.videoModelConfig = d.buildVideoModelConfig(configBase)
	d.videoToolRegistered = d.videoModelConfig != nil
```

- [ ] **Step 3: 修改 buildVideoModelConfig 返回类型和实现**

将 `buildVideoModelConfig` 方法（约 L387-406）从返回 `bool` 改为返回 `*hschema.VideoModelConfig`。完整替换为：

```go
// buildVideoModelConfig 从配置构建视频模型配置。
// 对齐 Python: _build_video_model_config(config_base) (line 1244-1260)
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

- [ ] **Step 4: 在 deep_adapter.go 新增 resolveModelClientFromConfig 辅助方法**

在 `deep_adapter.go` 中（非导出函数区块），新增 3 个辅助方法：

```go
// resolveVisionModelClient 基于 visionModelConfig 构建 BaseModelClient。
func (d *DeepAdapter) resolveVisionModelClient() modelclients.BaseModelClient {
	return d.resolveModelClientFromConfig(d.visionModelConfig.APIKey, d.visionModelConfig.BaseURL, d.visionModelConfig.Model, d.visionModelConfig.MaxRetries)
}

// resolveAudioModelClient 基于 audioModelConfig 构建 BaseModelClient。
func (d *DeepAdapter) resolveAudioModelClient() modelclients.BaseModelClient {
	return d.resolveModelClientFromConfig(d.audioModelConfig.APIKey, d.audioModelConfig.BaseURL, d.audioModelConfig.QAModel, d.audioModelConfig.MaxRetries)
}

// resolveVideoModelClient 基于 videoModelConfig 构建 BaseModelClient。
func (d *DeepAdapter) resolveVideoModelClient() modelclients.BaseModelClient {
	return d.resolveModelClientFromConfig(d.videoModelConfig.APIKey, d.videoModelConfig.BaseURL, d.videoModelConfig.Model, d.videoModelConfig.MaxRetries)
}

// resolveModelClientFromConfig 统一的模型客户端构建逻辑。
// 对齐 Python: init_model(provider="OpenAI", ...)
func (d *DeepAdapter) resolveModelClientFromConfig(apiKey, apiBase, model string, maxRetries int) modelclients.BaseModelClient {
	m, err := llm.InitModel("OpenAI", model, apiKey, apiBase,
		llm.WithInitMaxRetries(maxRetries),
		llm.WithInitVerifySSL(false),
	)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("model", model).Msg("构建模型客户端失败")
		return nil
	}
	return m.GetClient()
}
```

需要新增 import `modelclients` 包和确认 `llm` 包已导入（已存在于 deep_adapter.go）。

- [ ] **Step 5: 运行编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/adapter/...`
Expected: 编译成功

- [ ] **Step 6: 提交**

```
feat(swarm/adapter): DeepAdapter 新增 videoModelConfig + resolveModelClientFromConfig 辅助方法
```

---

### Task 6: DeepAdapter getToolCards 步骤4/5/6 回填 + syncMultimodalToolsForRuntime 回填

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_tools.go`

- [ ] **Step 1: 回填 getToolCards 步骤4（Vision 工具注册）**

将 `getToolCards` 方法中步骤4（约 L495-505）从注释桩代码替换为实际实现：

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

需要在 import 中新增 `"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/multimodal"` 和确认 `modelclients` import。

- [ ] **Step 2: 回填 getToolCards 步骤5（Audio 工具注册）**

将步骤5（约 L514-524）替换为：

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

- [ ] **Step 3: 回填 getToolCards 步骤6（Video 工具注册）**

将步骤6（约 L532-537）替换为：

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

- [ ] **Step 4: 回填 syncMultimodalToolsForRuntime Vision 部分**

将视觉工具同步（约 L251-254）从注释桩替换为：

```go
	if d.visionModelConfig != nil && !d.visionToolsRegistered {
		client := d.resolveVisionModelClient()
		visionTools := multimodal.CreateVisionTools(client, d.visionModelConfig, d.resolveRuntimeLanguage(), "")
		cards := make([]*tool.ToolCard, len(visionTools))
		for i, t := range visionTools {
			cards[i] = t.Card()
		}
		d.syncToolsToManager(ctx, cards, visionTools, nil, "vision")
		d.visionToolsRegistered = true
	}
	if d.visionModelConfig == nil && d.visionToolsRegistered {
		d.removeRegisteredTools([]string{ToolNameVision})
		d.visionToolsRegistered = false
	}
```

- [ ] **Step 5: 回填 syncMultimodalToolsForRuntime Audio 部分**

将音频工具同步（约 L261-264）替换为：

```go
	if d.audioModelConfig != nil && !d.audioToolsRegistered {
		client := d.resolveAudioModelClient()
		audioTools := multimodal.CreateAudioTools(client, d.audioModelConfig, d.resolveRuntimeLanguage(), "")
		cards := make([]*tool.ToolCard, len(audioTools))
		for i, t := range audioTools {
			cards[i] = t.Card()
		}
		d.syncToolsToManager(ctx, cards, audioTools, nil, "audio")
		d.audioToolsRegistered = true
	}
	if d.audioModelConfig == nil && d.audioToolsRegistered {
		d.removeRegisteredTools([]string{ToolNameAudioTranscription})
		d.audioToolsRegistered = false
	}
```

- [ ] **Step 6: 回填 syncMultimodalToolsForRuntime Video 部分**

将视频工具同步（约 L270-274）替换为：

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

- [ ] **Step 7: 运行编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/adapter/...`
Expected: 编译成功

- [ ] **Step 8: 提交**

```
feat(swarm/adapter): 回填 Vision/Audio/Video 工具注册链路 + 热同步
```

---

### Task 7: IMPLEMENTATION_PLAN.md 更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 读取 IMPLEMENTATION_PLAN.md 9.38-49 行**

定位到 L584（9.38-49 行），确认当前内容。

- [ ] **Step 2: 更新 9.38-49 描述**

将 9.38-49 行中的 `✅VideoUnderstandingTool` 替换为更详细的描述：

```
✅VideoUnderstandingTool(incl.VideoModelConfig+ThinkingEnabled+glm-4.6v默认+注册链路回填)
```

在 `✅多模态(...)` 整体描述中追加 Vision/Audio 注册回填标记：

```
✅多模态(ImageOCRTool+VQATool+AudioTranscriptionTool+AudioQATool+AudioMetadataTool+VideoUnderstandingTool(incl.VideoModelConfig+ThinkingEnabled+glm-4.6v默认+注册链路回填)+TranscribeAudio接口+ContentPart扩展+Vision/Audio注册回填+覆盖率85.3%)
```

- [ ] **Step 3: 清除 DeepAdapter ⤵️ 标记**

在 `deep_adapter_tools.go` 中，清除所有已回填的 `⤵️ 9.38-49` 标记：
- 步骤4 Vision 注释中的 `⤵️ 9.38-49: create_vision_tools() 尚未实现`
- 步骤5 Audio 注释中的 `⤵️ 9.38-49: _iter_runtime_audio_tools() 尚未实现`
- 步骤6 Video 注释中的 `⤵️ 9.38-49: video_understanding 工具实例尚未实现`
- syncMultimodalToolsForRuntime 中的 `⤵️ 9.38-49` 标记
- buildVideoModelConfig 中的 `⤵️ 9.38-49` 标记

- [ ] **Step 4: 提交**

```
docs: 更新 IMPLEMENTATION_PLAN.md 回填状态 + 清除 ⤵️ 标记
```

---

### Task 8: 全量编译 + 测试验证

**Files:** 无改动

- [ ] **Step 1: 运行全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 2: 运行相关包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/schema/... ./internal/agentcore/harness/tools/multimodal/... ./internal/swarm/server/adapter/... -v -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 3: 运行覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/harness/schema/... ./internal/agentcore/harness/tools/multimodal/... ./internal/swarm/server/adapter/...`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 4: 最终提交（如有修正）**

如果有编译或测试中发现的问题并修正，做一次修正提交。

---

## 自查结果

**1. Spec 覆盖率：**
- 配置层（VideoModelConfig + multimodal_config）→ Task 1 + Task 2 ✅
- 工具层（签名变更 + ThinkingEnabled + 默认模型 + 提示词）→ Task 3 ✅
- 测试层（video_test + config_test + multimodal_config_test）→ Task 4 ✅
- 注册层（DeepAdapter 字段 + 回填 + resolveModelClient）→ Task 5 + Task 6 ✅
- IMPLEMENTATION_PLAN 更新 → Task 7 ✅
- 全量验证 → Task 8 ✅

**2. Placeholder 扫描：** 无 TBD/TODO/待实现。所有代码步骤都有具体代码。

**3. 类型一致性：**
- Task 1 定义 VideoModelConfig → Task 3 使用 `*hschema.VideoModelConfig` → Task 4/5/6 使用同类型 ✅
- Task 1 定义 DefaultVideoModel="glm-4.6v" → Task 3 使用 defaultVideoModel → Task 4 测试验证 ✅
- Task 2 定义 ApplyVideoModelConfigFromYAML → Task 5 buildVideoModelConfig 调用 ✅
- Task 5 定义 resolveModelClientFromConfig 使用 llm.InitModel → Task 6 调用 resolveVisionModelClient/resolveAudioModelClient/resolveVideoModelClient ✅
