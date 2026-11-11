package openai

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// userMsg 用给定内容构造 UserMessage（Content 是提升字段，不能直接写字面量）。
func userMsg(content llmschema.MessageContent) *llmschema.UserMessage {
	msg := llmschema.NewUserMessage("")
	msg.Content = content
	return msg
}

// newDashScopeClient 构造指向测试服务器的 DashScope 客户端。
func newDashScopeClient(t *testing.T, serverURL string) *OpenAIModelClient {
	t.Helper()
	c, err := NewOpenAIModelClient(
		&llmschema.ModelRequestConfig{ModelName: "wan2.6-image"},
		&llmschema.ModelClientConfig{
			ClientID:        "cid",
			ClientProvider:  "OpenAI",
			EndpointProfile: "dashscope",
			APIKey:          "sk-test",
			APIBase:         serverURL + "/compatible-mode/v1",
		},
	)
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	return c
}

// TestResolveDashScopeBaseURL_兼容模式剥离
// Python: dashscope.base_http_api_url = api_base（上游原生路径，Go 额外归一）
func TestResolveDashScopeBaseURL_兼容模式剥离(t *testing.T) {
	cases := map[string]string{
		"https://dashscope.aliyuncs.com/compatible-mode/v1": "https://dashscope.aliyuncs.com",
		"https://dashscope.aliyuncs.com":                    "https://dashscope.aliyuncs.com",
		"https://custom-proxy.example.com":                  "https://custom-proxy.example.com",
	}
	for in, want := range cases {
		if got := ResolveDashScopeBaseURL(in); got != want {
			t.Errorf("ResolveDashScopeBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestGenerateImage_成功返回图片URL
// Python: test_openai_model_client.py:770 test_dashscope_profile_converts_text_and_reference_images_for_generation
func TestGenerateImage_成功返回图片URL(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status_code":200,"request_id":"r1","output":{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":[{"image":"https://example.test/out.png"}]}}]}}`)
	}))
	defer server.Close()

	c := newDashScopeClient(t, server.URL)
	resp, err := c.GenerateImage(t.Context(),
		[]*llmschema.UserMessage{userMsg(llmschema.NewMultiModalContent(
			llmschema.ContentPart{Type: "text", Text: "a cat"},
		))},
		model_clients.WithImageSize("1280*720"),
	)
	if err != nil {
		t.Fatalf("GenerateImage 失败: %v", err)
	}
	if gotPath != multiModalPath {
		t.Errorf("路径 = %q, want %q", gotPath, multiModalPath)
	}
	if len(resp.Images) != 1 || resp.Images[0] != "https://example.test/out.png" {
		t.Fatalf("Images = %#v", resp.Images)
	}
}

// TestGenerateImage_参数校验_超过3张图
// Python: test_openai_model_client.py:804 test_dashscope_profile_rejects_invalid_image_generation_content
func TestGenerateImage_参数校验_超过3张图(t *testing.T) {
	c := newDashScopeClient(t, "http://127.0.0.1:1")
	_, err := c.GenerateImage(t.Context(),
		[]*llmschema.UserMessage{userMsg(llmschema.NewMultiModalContent(
			llmschema.ContentPart{Type: "text", Text: "x"},
			llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "a"}},
			llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "b"}},
			llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "c"}},
			llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "d"}},
		))},
	)
	if err == nil {
		t.Fatal("超过 3 张图应报错")
	}
}

// TestGenerateImage_参数校验_多条消息
// Python: test_openai_model_client.py:804
func TestGenerateImage_参数校验_多条消息(t *testing.T) {
	c := newDashScopeClient(t, "http://127.0.0.1:1")
	_, err := c.GenerateImage(t.Context(),
		[]*llmschema.UserMessage{
			userMsg(llmschema.NewMultiModalContent(llmschema.ContentPart{Type: "text", Text: "a"})),
			userMsg(llmschema.NewMultiModalContent(llmschema.ContentPart{Type: "text", Text: "b"})),
		},
	)
	if err == nil {
		t.Fatal("多条消息应报错")
	}
}

// TestGenerateSpeech_成功返回音频
// Python: openai_model_client.py:2379 generate_speech
func TestGenerateSpeech_成功返回音频(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status_code":200,"output":{"url":"https://example.test/a.mp3"}}`)
	}))
	defer server.Close()

	c := newDashScopeClient(t, server.URL)
	resp, err := c.GenerateSpeech(t.Context(),
		[]*llmschema.UserMessage{userMsg(llmschema.NewTextContent("你好"))},
		model_clients.WithSpeechVoice("Cherry"),
	)
	if err != nil {
		t.Fatalf("GenerateSpeech 失败: %v", err)
	}
	if resp.AudioURL == nil || *resp.AudioURL != "https://example.test/a.mp3" {
		t.Fatalf("AudioURL = %v", resp.AudioURL)
	}
}

// TestGenerateVideo_成功返回视频
// Python: openai_model_client.py:2301 generate_video
func TestGenerateVideo_成功返回视频(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status_code":200,"output":{"video_url":"https://example.test/v.mp4","duration":5}}`)
	}))
	defer server.Close()

	c := newDashScopeClient(t, server.URL)
	resp, err := c.GenerateVideo(t.Context(),
		[]*llmschema.UserMessage{userMsg(llmschema.NewTextContent("a dog"))},
	)
	if err != nil {
		t.Fatalf("GenerateVideo 失败: %v", err)
	}
	if gotPath != videoSynthesisPath {
		t.Errorf("路径 = %q, want %q", gotPath, videoSynthesisPath)
	}
	if resp.VideoURL == nil || *resp.VideoURL != "https://example.test/v.mp4" {
		t.Fatalf("VideoURL = %v", resp.VideoURL)
	}
}

// TestGenerateImage_HTTP错误码 验证 DashScope 业务错误码被识别
func TestGenerateImage_HTTP错误码(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status_code":400,"code":"InvalidParameter","message":"bad size"}`)
	}))
	defer server.Close()

	c := newDashScopeClient(t, server.URL)
	_, err := c.GenerateImage(t.Context(),
		[]*llmschema.UserMessage{userMsg(llmschema.NewMultiModalContent(llmschema.ContentPart{Type: "text", Text: "x"}))},
	)
	if err == nil {
		t.Fatal("应返回错误")
	}
}

// ──────────────────────────── KV 能力 ────────────────────────────

// TestSupportsKVCacheRelease_按mode判断 验证能力由 extensions.kv_cache.mode 驱动。
func TestSupportsKVCacheRelease_按mode判断(t *testing.T) {
	cases := []struct {
		mode string
		want bool
	}{
		{"", false},
		{"none", false},
		{"affinity", true},
	}
	for _, tc := range cases {
		c, err := NewOpenAIModelClient(
			&llmschema.ModelRequestConfig{ModelName: "m"},
			&llmschema.ModelClientConfig{
				ClientID:       "cid",
				ClientProvider: "OpenAI",
				APIKey:         "k",
				APIBase:        "http://127.0.0.1:1/v1",
				Extensions: &llmschema.LLMExtensionsConfig{
					KVCache: llmschema.KVCacheExtensionConfig{Mode: tc.mode},
				},
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		if got := c.SupportsKVCacheRelease(); got != tc.want {
			t.Errorf("mode=%q → %v, want %v", tc.mode, got, tc.want)
		}
	}
}

// TestRelease_affinity模式发请求 验证 affinity 下真正调用释放接口。
func TestRelease_affinity模式发请求(t *testing.T) {
	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c, err := NewOpenAIModelClient(
		&llmschema.ModelRequestConfig{ModelName: "m"},
		&llmschema.ModelClientConfig{
			ClientID:       "cid",
			ClientProvider: "OpenAI",
			APIKey:         "sk-affinity",
			APIBase:        server.URL + "/v1",
			Extensions: &llmschema.LLMExtensionsConfig{
				KVCache: llmschema.KVCacheExtensionConfig{Mode: "affinity"},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := c.Release(t.Context(), model_clients.WithReleaseSessionID("sess-1"))
	if err != nil {
		t.Fatalf("Release 失败: %v", err)
	}
	if !ok {
		t.Fatal("Release 应返回 true")
	}
	if gotPath != "/v1"+releaseKVCachePath {
		t.Errorf("路径 = %q", gotPath)
	}
	if gotAuth != "Bearer sk-affinity" {
		t.Errorf("Authorization = %q", gotAuth)
	}
}
