package tools

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	upath "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// setupImageGenWorkspaceEnv 隔离 Agent 工作区目录（测试辅助）。
//
// 落盘路径 workspace.AgentWorkspaceDir() 在 CI 等未初始化环境不存在，
// 注入 UAPCLAW_DATA_DIR 指向临时目录并重置路径缓存，测试后恢复。
// 需同时创建 config 子目录（模拟已初始化，避免回退 resources）
// 与 agent/workspace 落盘目录（os.WriteFile 不建父目录）。
func setupImageGenWorkspaceEnv(t *testing.T) {
	t.Helper()
	wsRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wsRoot, "config"), 0o755); err != nil {
		t.Fatalf("创建 config 目录失败: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(wsRoot, "agent", "workspace"), 0o755); err != nil {
		t.Fatalf("创建 agent workspace 目录失败: %v", err)
	}
	t.Setenv("UAPCLAW_DATA_DIR", wsRoot)
	upath.ResetCache()
	t.Cleanup(func() {
		os.Unsetenv("UAPCLAW_DATA_DIR")
		upath.ResetCache()
	})
}

// fakeImageGenClient 测试用图片生成假客户端
type fakeImageGenClient struct {
	resp *llmschema.ImageGenerationResponse
	err  error
}

func (f *fakeImageGenClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return nil, nil
}
func (f *fakeImageGenClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, nil
}
func (f *fakeImageGenClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return f.resp, f.err
}
func (f *fakeImageGenClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, nil
}
func (f *fakeImageGenClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, nil
}
func (f *fakeImageGenClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, nil
}
func (f *fakeImageGenClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (f *fakeImageGenClient) SupportsKVCacheRelease() bool {
	return false
}

// injectFakeImageGenModel 注入 fake client 的 model builder，测试后恢复
func injectFakeImageGenModel(t *testing.T, fakeClient *fakeImageGenClient) {
	t.Helper()
	realModel, err := llm.NewModel(&llmschema.ModelClientConfig{ClientID: "image_gen_test", ClientProvider: "OpenAI", APIKey: "x", APIBase: "https://test.example.com/v1"}, nil, llm.WithClient(fakeClient))
	if err != nil {
		t.Fatalf("构造 fake model 失败: %v", err)
	}
	origBuilder := imageGenModelBuilder
	imageGenModelBuilder = func() (*llm.Model, error) { return realModel, nil }
	t.Cleanup(func() { imageGenModelBuilder = origBuilder })
}

// TestGetImageGenAPICredentials_默认链 验证凭据链的回退顺序
// Python: _get_image_gen_api_credentials (image_tools.py L97-112)
func TestGetImageGenAPICredentials_默认链(t *testing.T) {
	t.Setenv("IMAGE_GEN_API_KEY", "k1")
	t.Setenv("IMAGE_GEN_API_BASE", "")
	t.Setenv("IMAGE_GEN_MODEL_NAME", "")
	t.Setenv("IMAGE_GEN_PROVIDER", "")
	t.Setenv("API_KEY", "")
	t.Setenv("API_BASE", "https://fallback.example.com/v1")
	k, b, m, p := getImageGenAPICredentials()
	if k != "k1" || b != "https://fallback.example.com/v1" || m != "wanx-v1" || p != "DashScope" {
		t.Errorf("凭据链异常: %s %s %s %s", k, b, m, p)
	}

	// 全空时 base 回退 DashScope 默认
	t.Setenv("IMAGE_GEN_API_KEY", "")
	t.Setenv("API_BASE", "")
	k, b, m, p = getImageGenAPICredentials()
	if k != "" || b != "https://dashscope.aliyuncs.com/api/v1" || m != "wanx-v1" || p != "DashScope" {
		t.Errorf("空值回退异常: %q %q %q %q", k, b, m, p)
	}
}

// TestInvokeModelImageGeneration_无key报错 验证缺少 key 的错误文案
// Python: image_tools.py L339-340
func TestInvokeModelImageGeneration_无key报错(t *testing.T) {
	t.Setenv("IMAGE_GEN_API_KEY", "")
	t.Setenv("API_KEY", "")
	result := invokeModelImageGeneration(t.Context(), "a cat", "1024x1024", "standard")
	if result["error"] == nil {
		t.Fatalf("无 key 应报错: %v", result)
	}
	if !strings.Contains(result["error"].(string), "IMAGE_GEN_API_KEY or API_KEY is not configured for image generation.") {
		t.Errorf("错误文案异常: %v", result["error"])
	}
}

// TestInvokeModelImageGeneration_base64落盘 验证 base64 分支落盘
// Python: image_tools.py L383-392
func TestInvokeModelImageGeneration_base64落盘(t *testing.T) {
	setupImageGenWorkspaceEnv(t)
	fakeClient := &fakeImageGenClient{
		resp: &llmschema.ImageGenerationResponse{ImagesBase64: []string{base64.StdEncoding.EncodeToString([]byte("pngdata"))}},
	}
	injectFakeImageGenModel(t, fakeClient)

	t.Setenv("IMAGE_GEN_API_KEY", "k")
	result := invokeModelImageGeneration(t.Context(), "a cat", "1024x1024", "standard")
	if result["error"] != nil {
		t.Fatalf("不应报错: %v", result["error"])
	}
	path := result["image_path"].(string)
	if !strings.Contains(filepath.Base(path), "generated_") {
		t.Errorf("输出文件名异常: %s", path)
	}
	if !strings.HasSuffix(path, ".png") {
		t.Errorf("输出文件扩展名异常: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "pngdata" {
		t.Errorf("落盘内容异常: %v %q", err, data)
	}
	if result["revised_prompt"] != "a cat" {
		t.Errorf("revised_prompt 异常: %v", result["revised_prompt"])
	}
}

// TestInvokeModelImageGeneration_URL下载 验证 URL 分支下载落盘 + original_url
// Python: image_tools.py L393-409
func TestInvokeModelImageGeneration_URL下载(t *testing.T) {
	setupImageGenWorkspaceEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证固定 UA（对齐 Python requests.get(headers={"User-Agent": ua})）
		if !strings.Contains(r.Header.Get("User-Agent"), "Chrome/91.0.4472.124") {
			t.Errorf("User-Agent 异常: %q", r.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte("imagedata"))
	}))
	defer srv.Close()
	fakeClient := &fakeImageGenClient{
		resp: &llmschema.ImageGenerationResponse{Images: []string{srv.URL + "/img.png"}},
	}
	injectFakeImageGenModel(t, fakeClient)

	t.Setenv("IMAGE_GEN_API_KEY", "k")
	result := invokeModelImageGeneration(t.Context(), "a dog", "1024x1024", "standard")
	if result["error"] != nil {
		t.Fatalf("不应报错: %v", result["error"])
	}
	if result["original_url"] != srv.URL+"/img.png" {
		t.Errorf("URL 分支应含 original_url: %v", result)
	}
	data, err := os.ReadFile(result["image_path"].(string))
	if err != nil || string(data) != "imagedata" {
		t.Errorf("下载落盘内容异常: %v %q", err, data)
	}
}

// TestInvokeModelImageGeneration_无有效数据 验证空响应的错误文案
// Python: image_tools.py L411
func TestInvokeModelImageGeneration_无有效数据(t *testing.T) {
	fakeClient := &fakeImageGenClient{resp: &llmschema.ImageGenerationResponse{}}
	injectFakeImageGenModel(t, fakeClient)

	t.Setenv("IMAGE_GEN_API_KEY", "k")
	result := invokeModelImageGeneration(t.Context(), "a cat", "1024x1024", "standard")
	if result["error"] == nil || !strings.Contains(result["error"].(string), "No valid image data in response") {
		t.Errorf("空响应应报 No valid image data: %v", result)
	}
}

// TestInvokeModelImageGeneration_客户端错误 验证模型调用异常的错误文案
// Python: image_tools.py L413-414（3 次重试后 on_failure 无 → raise → except 返回错误）
func TestInvokeModelImageGeneration_客户端错误(t *testing.T) {
	fakeClient := &fakeImageGenClient{err: context.DeadlineExceeded}
	// 重试退避会 sleep 4+16 秒——注入短退避避免慢测试
	origDelay := imageGenRetryBaseDelay
	imageGenRetryBaseDelay = 0
	t.Cleanup(func() { imageGenRetryBaseDelay = origDelay })
	injectFakeImageGenModel(t, fakeClient)

	t.Setenv("IMAGE_GEN_API_KEY", "k")
	result := invokeModelImageGeneration(t.Context(), "a cat", "1024x1024", "standard")
	if result["error"] == nil || !strings.Contains(result["error"].(string), "Image generation failed") {
		t.Errorf("调用失败应报 Image generation failed: %v", result)
	}
}

// TestApplyImageGenModelConfigFromYAML_四变量设置 验证 YAML → env 映射
// Python: apply_image_gen_model_config_from_yaml (multimodal_config.py L256-285)
func TestApplyImageGenModelConfigFromYAML_四变量设置(t *testing.T) {
	t.Setenv("IMAGE_GEN_API_KEY", "")
	t.Setenv("IMAGE_GEN_API_BASE", "")
	t.Setenv("IMAGE_GEN_MODEL_NAME", "")
	t.Setenv("IMAGE_GEN_PROVIDER", "")

	configBase := map[string]any{
		"models": map[string]any{
			"image_gen": map[string]any{
				"model_config": map[string]any{
					"api_key": "yaml-key", "api_base": "https://b.example.com",
					"model_name": "m-x", "model_provider": "P1",
				},
			},
		},
	}
	ApplyImageGenModelConfigFromYAML(configBase)
	if os.Getenv("IMAGE_GEN_API_KEY") != "yaml-key" {
		t.Errorf("IMAGE_GEN_API_KEY 未生效: %q", os.Getenv("IMAGE_GEN_API_KEY"))
	}
	if os.Getenv("IMAGE_GEN_API_BASE") != "https://b.example.com" {
		t.Errorf("IMAGE_GEN_API_BASE 未生效: %q", os.Getenv("IMAGE_GEN_API_BASE"))
	}
	if os.Getenv("IMAGE_GEN_MODEL_NAME") != "m-x" {
		t.Errorf("IMAGE_GEN_MODEL_NAME 未生效: %q", os.Getenv("IMAGE_GEN_MODEL_NAME"))
	}
	if os.Getenv("IMAGE_GEN_PROVIDER") != "P1" {
		t.Errorf("IMAGE_GEN_PROVIDER 未生效: %q", os.Getenv("IMAGE_GEN_PROVIDER"))
	}
}

// TestApplyImageGenModelConfigFromYAML_model_client_config回退 验证双层 fallback
// Python: _get_model_config 的 inner.get("model_config") or inner.get("model_client_config")
func TestApplyImageGenModelConfigFromYAML_model_client_config回退(t *testing.T) {
	t.Setenv("IMAGE_GEN_API_KEY", "")
	configBase := map[string]any{
		"models": map[string]any{
			"image_gen": map[string]any{
				"model_client_config": map[string]any{
					"api_key": "mcc-key", "model_name": "m-y",
				},
			},
		},
	}
	ApplyImageGenModelConfigFromYAML(configBase)
	if os.Getenv("IMAGE_GEN_API_KEY") != "mcc-key" || os.Getenv("IMAGE_GEN_MODEL_NAME") != "m-y" {
		t.Errorf("model_client_config 回退未生效: %q %q", os.Getenv("IMAGE_GEN_API_KEY"), os.Getenv("IMAGE_GEN_MODEL_NAME"))
	}
}

// TestApplyImageGenModelConfigFromYAML_非dict配置 验证非 dict 入参直接返回
// Python: if not isinstance(config_base, dict): return
func TestApplyImageGenModelConfigFromYAML_非dict配置(t *testing.T) {
	t.Setenv("IMAGE_GEN_API_KEY", "keep-me")
	ApplyImageGenModelConfigFromYAML(nil)
	ApplyImageGenModelConfigFromYAML(map[string]any{"models": "not-a-dict"})
	if os.Getenv("IMAGE_GEN_API_KEY") != "keep-me" {
		t.Error("非 dict 配置不应改动环境变量")
	}
}

// TestGenerateImage工具_卡片与成功文案 验证工具卡片名称和成功多行文案
// Python: generate_image (image_tools.py L417-480)
func TestGenerateImage工具_卡片与成功文案(t *testing.T) {
	setupImageGenWorkspaceEnv(t)
	if GenerateImage.Card().Name != "generate_image" {
		t.Errorf("工具名异常: %s", GenerateImage.Card().Name)
	}

	fakeClient := &fakeImageGenClient{
		resp: &llmschema.ImageGenerationResponse{ImagesBase64: []string{base64.StdEncoding.EncodeToString([]byte("pngdata"))}},
	}
	injectFakeImageGenModel(t, fakeClient)
	t.Setenv("IMAGE_GEN_API_KEY", "k")

	result, err := GenerateImage.Invoke(t.Context(), map[string]any{
		"prompt": "a cat",
		"size":   "1024x1024",
	}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatalf("invoke 异常: %v", err)
	}
	text := result["result"].(string)
	// 逐字对齐 Python: Image generated successfully! / Saved to: / Prompt:
	if !strings.Contains(text, "Image generated successfully!\n") ||
		!strings.Contains(text, "Saved to: ") ||
		!strings.Contains(text, "Prompt: a cat") {
		t.Errorf("成功文案异常:\n%s", text)
	}
	// revised_prompt == prompt 时不输出 Revised prompt 行
	if strings.Contains(text, "Revised prompt:") {
		t.Errorf("revised==prompt 时不应输出 Revised prompt:\n%s", text)
	}
}

// TestGenerateImage工具_SaveDir搬移 验证 save_dir 参数的文件搬移
// Python: image_tools.py L459-465
func TestGenerateImage工具_SaveDir搬移(t *testing.T) {
	setupImageGenWorkspaceEnv(t)
	fakeClient := &fakeImageGenClient{
		resp: &llmschema.ImageGenerationResponse{ImagesBase64: []string{base64.StdEncoding.EncodeToString([]byte("pngdata"))}},
	}
	injectFakeImageGenModel(t, fakeClient)
	t.Setenv("IMAGE_GEN_API_KEY", "k")

	saveDir := t.TempDir()
	result, err := GenerateImage.Invoke(t.Context(), map[string]any{
		"prompt":   "a cat",
		"save_dir": saveDir,
	}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatalf("invoke 异常: %v", err)
	}
	text := result["result"].(string)
	// Saved to 应指向 saveDir
	idx := strings.Index(text, "Saved to: ")
	if idx < 0 {
		t.Fatalf("缺少 Saved to 行:\n%s", text)
	}
	savedPath := strings.TrimSpace(strings.SplitN(text[idx+len("Saved to: "):], "\n", 2)[0])
	if filepath.Dir(savedPath) != filepath.Clean(saveDir) {
		t.Errorf("搬移目录异常: %s（期望在 %s 下）", savedPath, saveDir)
	}
	if _, statErr := os.Stat(savedPath); statErr != nil {
		t.Errorf("搬移后文件不存在: %v", statErr)
	}
}
