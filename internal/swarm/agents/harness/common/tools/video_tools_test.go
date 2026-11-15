package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────── 白名单 ────────────────────────────

// TestNormalizeVideoModelSelection 对齐 Python _normalize_video_model_selection:42-49。
func TestNormalizeVideoModelSelection(t *testing.T) {
	cases := map[string]string{
		"@Video_Understanding": "video_understanding",
		"a\\b\\Video_Tools.py": "a/b/video_tools.py",
		"  glm-4.6v  ":         "glm-4.6v",
	}
	for in, want := range cases {
		if got := NormalizeVideoModelSelection(in); got != want {
			t.Fatalf("%q → %q, want %q", in, got, want)
		}
	}
}

// TestIsVideoModelSupported 对齐 :50-57 精确/后缀/空/拒绝。
func TestIsVideoModelSupported(t *testing.T) {
	if !IsVideoModelSupported("") {
		t.Fatal("空值应放行")
	}
	if !IsVideoModelSupported("video_understanding") {
		t.Fatal("精确命中应 true")
	}
	if !IsVideoModelSupported("custom/video_tools.py") {
		t.Fatal("后缀命中应 true")
	}
	if IsVideoModelSupported("unrelated-model") {
		t.Fatal("不在白名单应 false")
	}
}

// ──────────────────────────── mime / URL ────────────────────────────

// TestGuessVideoMime 对齐 :80-91 映射表与回落。
func TestGuessVideoMime(t *testing.T) {
	cases := map[string]string{
		"a.mp4": "video/mp4", "a.mov": "video/quicktime", "a.avi": "video/x-msvideo",
		"a.mkv": "video/x-matroska", "a.webm": "video/webm", "a.mpeg": "video/mpeg",
		"a.mpg": "video/mpeg", "a.m4v": "video/x-m4v", "a.xyz": "video/mp4",
	}
	for name, want := range cases {
		if got := guessVideoMime(name); got != want {
			t.Fatalf("%s → %s, want %s", name, got, want)
		}
	}
}

// TestVideoPathToURL 对齐 :93-108 URL 原样 / 本地 data URI / 错误分支。
func TestVideoPathToURL(t *testing.T) {
	if got := videoPathToURL("https://example.com/v.mp4"); got != "https://example.com/v.mp4" {
		t.Fatalf("URL 应原样: %s", got)
	}
	p := filepath.Join(t.TempDir(), "v.mp4")
	if err := os.WriteFile(p, []byte("mp4bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := videoPathToURL(p)
	if !strings.HasPrefix(got, "data:video/mp4;base64,") {
		t.Fatalf("本地路径应转 data URI: %q", got[:min(30, len(got))])
	}
	for _, bad := range []string{"", "/nonexistent/v.mp4"} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("%q 应 panic", bad)
				}
			}()
			_ = videoPathToURL(bad)
		}()
	}
}

// ──────────────────────────── 归一化 ────────────────────────────

// TestNormalizeVideoUnderstandingRequest 对齐 :129-149 钳制与校验。
func TestNormalizeVideoUnderstandingRequest(t *testing.T) {
	t.Setenv("VIDEO_MODEL_NAME", "env-model")
	req := normalizeVideoUnderstandingRequest(map[string]any{
		"query": "q", "video_path": "v.mp4",
		"timeout_seconds": 9999, "max_tokens": 1, "temperature": 99,
	})
	if req.Model != "env-model" {
		t.Fatalf("默认模型 = %q", req.Model)
	}
	if req.TimeoutSeconds != 600 || req.MaxTokens != 128 || req.Temperature != 2.0 {
		t.Fatalf("钳制错: %d/%d/%v", req.TimeoutSeconds, req.MaxTokens, req.Temperature)
	}
	for _, bad := range []map[string]any{{}, {"query": "q"}, {"video_path": "v"}} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("%v 应 panic", bad)
				}
			}()
			_ = normalizeVideoUnderstandingRequest(bad)
		}()
	}
}

// ──────────────────────────── extract_answer ────────────────────────────

// TestExtractVideoAnswer 对齐 :110-127 四分支。
func TestExtractVideoAnswer(t *testing.T) {
	if got := extractVideoAnswer(map[string]any{}); got != "" {
		t.Fatalf("无 choices 应空: %q", got)
	}
	strResp := map[string]any{"choices": []any{map[string]any{
		"message": map[string]any{"content": "  answer  "},
	}}}
	if got := extractVideoAnswer(strResp); got != "answer" {
		t.Fatalf("字符串 content: %q", got)
	}
	listResp := map[string]any{"choices": []any{map[string]any{
		"message": map[string]any{"content": []any{
			map[string]any{"text": "l1"},
			map[string]any{"text": "l2"},
			"skip-me",
		}},
	}}}
	if got := extractVideoAnswer(listResp); got != "l1\nl2" {
		t.Fatalf("列表 content: %q", got)
	}
}

// ──────────────────────────── 端点拼接 ────────────────────────────

// TestResolveChatCompletionsURL 对齐 :151-156。
func TestResolveChatCompletionsURL(t *testing.T) {
	if got := resolveChatCompletionsURL("https://b/v4"); got != "https://b/v4/chat/completions" {
		t.Fatalf("拼接错: %s", got)
	}
	if got := resolveChatCompletionsURL("https://b/v4/chat/completions/"); got != "https://b/v4/chat/completions" {
		t.Fatalf("已带端点应原样去尾斜杠: %s", got)
	}
	if got := resolveChatCompletionsURL(""); got != "" {
		t.Fatalf("空应空: %q", got)
	}
}

// ──────────────────────────── 端到端（假端点） ────────────────────────────

// TestGLMVideoUnderstandingSync_假端点 httptest 全链：请求头/payload/答案提取。
func TestGLMVideoUnderstandingSync_假端点(t *testing.T) {
	p := filepath.Join(t.TempDir(), "v.mp4")
	if err := os.WriteFile(p, []byte("mp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	var gotAuth, gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = map[string]any{"auth": r.Header.Get("Authorization"), "ua": r.Header.Get("User-Agent")}
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":" scene "}}]}`))
	}))
	defer server.Close()

	t.Setenv("VIDEO_API_KEY", "test-key")
	t.Setenv("VIDEO_API_BASE", server.URL)
	t.Setenv("VIDEO_MODEL_NAME", "test-model")
	req := normalizeVideoUnderstandingRequest(map[string]any{"query": "describe", "video_path": p})
	ans := glmVideoUnderstandingSync(req)
	if ans != "scene" {
		t.Fatalf("answer = %q", ans)
	}
	if gotAuth["auth"] != "Bearer test-key" {
		t.Fatalf("Authorization 错: %v", gotAuth["auth"])
	}
	if !strings.Contains(fmt.Sprint(gotAuth["ua"]), "Chrome") {
		t.Fatalf("UA 错: %v", gotAuth["ua"])
	}
	msgs := gotPayload["messages"].([]any)
	content := msgs[0].(map[string]any)["content"].([]any)
	if content[0].(map[string]any)["type"] != "video_url" {
		t.Fatal("video_url 块应在前")
	}
	if content[1].(map[string]any)["type"] != "text" {
		t.Fatal("text 块应在后")
	}
}

// TestGLMVideoUnderstandingSync_空答案与API错误 对齐 :212-218 两个分支。
func TestGLMVideoUnderstandingSync_空答案与API错误(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":""}}]}`))
	}))
	defer empty.Close()
	t.Setenv("VIDEO_API_KEY", "k")
	t.Setenv("VIDEO_API_BASE", empty.URL)
	if got := glmVideoUnderstandingSync(VideoUnderstandingRequest{
		Query: "q", VideoPath: "https://x/v.mp4", Model: "m", TimeoutSeconds: 10, MaxTokens: 100, Temperature: 0.2,
	}); got != "[ERROR]: GLM returned empty answer." {
		t.Fatalf("空答案: %q", got)
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer failing.Close()
	t.Setenv("VIDEO_API_BASE", failing.URL)
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("API 错误应 panic")
			}
			if !strings.Contains(fmt.Sprint(r), "API error 401: bad key") {
				t.Fatalf("错误文案: %v", r)
			}
		}()
		_ = glmVideoUnderstandingSync(VideoUnderstandingRequest{
			Query: "q", VideoPath: "https://x/v.mp4", Model: "m", TimeoutSeconds: 10, MaxTokens: 100, Temperature: 0.2,
		})
	}()

	// key 有 base 无 → 明确报错（对齐 :168-170）
	t.Setenv("VIDEO_API_KEY", "k")
	os.Unsetenv("VIDEO_API_BASE")
	func() {
		defer func() {
			if !strings.Contains(fmt.Sprint(recover()), "VIDEO_API_BASE is required") {
				t.Fatal("缺 base 应 panic")
			}
		}()
		_ = glmVideoUnderstandingSync(VideoUnderstandingRequest{
			Query: "q", VideoPath: "https://x/v.mp4", Model: "m", TimeoutSeconds: 10, MaxTokens: 100, Temperature: 0.2,
		})
	}()
}
