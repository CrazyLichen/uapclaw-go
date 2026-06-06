package version

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewGitHubReleasesSource 验证创建 GitHub 版本源实例
func TestNewGitHubReleasesSource(t *testing.T) {
	s := NewGitHubReleasesSource("openJiuwen", "uapclaw")

	if s.owner != "openJiuwen" {
		t.Errorf("owner = %q, 期望 %q", s.owner, "openJiuwen")
	}
	if s.repo != "uapclaw" {
		t.Errorf("repo = %q, 期望 %q", s.repo, "uapclaw")
	}
	if s.timeout != defaultGitHubTimeout {
		t.Errorf("timeout = %v, 期望 %v", s.timeout, defaultGitHubTimeout)
	}
	if s.token != "" {
		t.Errorf("token 应为空，实际为 %q", s.token)
	}
}

// TestNewGitHubReleasesSource_WithOptions 验证可选配置
func TestNewGitHubReleasesSource_WithOptions(t *testing.T) {
	customURL := "https://github.example.com/api/v3/repos/owner/repo/releases/latest"
	customTimeout := 10 * time.Second

	s := NewGitHubReleasesSource("owner", "repo",
		WithToken("test-token"),
		WithAPIURL(customURL),
		WithTimeout(customTimeout),
	)

	if s.token != "test-token" {
		t.Errorf("token = %q, 期望 %q", s.token, "test-token")
	}
	if s.apiURL != customURL {
		t.Errorf("apiURL = %q, 期望 %q", s.apiURL, customURL)
	}
	if s.timeout != customTimeout {
		t.Errorf("timeout = %v, 期望 %v", s.timeout, customTimeout)
	}
}

// TestGitHubReleasesSource_FetchLatest 验证正常获取最新版本
func TestGitHubReleasesSource_FetchLatest(t *testing.T) {
	// 构造 mock 响应
	response := map[string]interface{}{
		"tag_name":     "v0.2.0",
		"published_at": "2025-07-12T00:00:00Z",
		"body":         "修复若干问题",
		"prerelease":   false,
		"draft":        false,
		"assets": []interface{}{
			map[string]interface{}{
				"name":               "uapclaw-0.2.0-linux-amd64.tar.gz",
				"browser_download_url": "https://github.com/openJiuwen/uapclaw/releases/download/v0.2.0/uapclaw-0.2.0-linux-amd64.tar.gz",
				"size":               float64(1024000),
			},
		},
	}
	body, _ := json.Marshal(response)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求头
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept 头 = %q, 期望 %q", r.Header.Get("Accept"), "application/json")
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("User-Agent 头不应为空")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer server.Close()

	s := NewGitHubReleasesSource("openJiuwen", "uapclaw", WithAPIURL(server.URL))
	info, err := s.FetchLatest(context.Background())
	if err != nil {
		t.Fatalf("FetchLatest 失败: %v", err)
	}

	if info.Version != "0.2.0" {
		t.Errorf("Version = %q, 期望 %q", info.Version, "0.2.0")
	}
	if info.ReleaseNotes != "修复若干问题" {
		t.Errorf("ReleaseNotes = %q, 期望 %q", info.ReleaseNotes, "修复若干问题")
	}
	if info.SourceType != "github" {
		t.Errorf("SourceType = %q, 期望 %q", info.SourceType, "github")
	}
	if info.Prerelease {
		t.Error("Prerelease 应为 false")
	}
	if len(info.Assets) != 1 {
		t.Fatalf("Assets 数量 = %d, 期望 1", len(info.Assets))
	}
	if info.Assets[0].Name != "uapclaw-0.2.0-linux-amd64.tar.gz" {
		t.Errorf("Assets[0].Name = %q, 期望 %q", info.Assets[0].Name, "uapclaw-0.2.0-linux-amd64.tar.gz")
	}
	if info.Assets[0].Size != 1024000 {
		t.Errorf("Assets[0].Size = %d, 期望 %d", info.Assets[0].Size, 1024000)
	}
}

// TestGitHubReleasesSource_FetchLatest_Prerelease 验证预发布版本标记
func TestGitHubReleasesSource_FetchLatest_Prerelease(t *testing.T) {
	response := map[string]interface{}{
		"tag_name":   "v0.2.0-beta1",
		"prerelease": true,
		"draft":      false,
		"assets":     []interface{}{},
	}
	body, _ := json.Marshal(response)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer server.Close()

	s := NewGitHubReleasesSource("owner", "repo", WithAPIURL(server.URL))
	info, err := s.FetchLatest(context.Background())
	if err != nil {
		t.Fatalf("FetchLatest 失败: %v", err)
	}

	if !info.Prerelease {
		t.Error("Prerelease 应为 true")
	}
	if info.Version != "0.2.0-beta1" {
		t.Errorf("Version = %q, 期望 %q", info.Version, "0.2.0-beta1")
	}
}

// TestGitHubReleasesSource_FetchLatest_Draft 验证 Draft 标记为预发布
func TestGitHubReleasesSource_FetchLatest_Draft(t *testing.T) {
	response := map[string]interface{}{
		"tag_name":   "v0.2.0",
		"prerelease": false,
		"draft":      true,
		"assets":     []interface{}{},
	}
	body, _ := json.Marshal(response)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer server.Close()

	s := NewGitHubReleasesSource("owner", "repo", WithAPIURL(server.URL))
	info, err := s.FetchLatest(context.Background())
	if err != nil {
		t.Fatalf("FetchLatest 失败: %v", err)
	}

	if !info.Prerelease {
		t.Error("Draft 应被标记为 Prerelease=true")
	}
}

// TestGitHubReleasesSource_FetchLatest_EmptyTag 验证 tag_name 为空时报错
func TestGitHubReleasesSource_FetchLatest_EmptyTag(t *testing.T) {
	response := map[string]interface{}{
		"tag_name":   "",
		"prerelease": false,
		"assets":     []interface{}{},
	}
	body, _ := json.Marshal(response)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer server.Close()

	s := NewGitHubReleasesSource("owner", "repo", WithAPIURL(server.URL))
	_, err := s.FetchLatest(context.Background())
	if err == nil {
		t.Fatal("tag_name 为空时应返回错误")
	}
}

// TestGitHubReleasesSource_FetchLatest_HTTPError 验证 HTTP 错误处理
func TestGitHubReleasesSource_FetchLatest_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not Found"))
	}))
	defer server.Close()

	s := NewGitHubReleasesSource("owner", "repo", WithAPIURL(server.URL))
	_, err := s.FetchLatest(context.Background())
	if err == nil {
		t.Fatal("HTTP 404 应返回错误")
	}
}

// TestGitHubReleasesSource_FetchLatest_ContextCancelled 验证上下文取消
func TestGitHubReleasesSource_FetchLatest_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 模拟延迟
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	s := NewGitHubReleasesSource("owner", "repo", WithAPIURL(server.URL))
	_, err := s.FetchLatest(ctx)
	if err == nil {
		t.Fatal("上下文超时应返回错误")
	}
}

// TestGitHubReleasesSource_FetchAssets 验证获取资产列表
func TestGitHubReleasesSource_FetchAssets(t *testing.T) {
	response := map[string]interface{}{
		"tag_name":   "v0.2.0",
		"prerelease": false,
		"assets": []interface{}{
			map[string]interface{}{
				"name":               "uapclaw-linux.tar.gz",
				"browser_download_url": "https://example.com/linux",
				"size":               float64(100),
			},
			map[string]interface{}{
				"name":               "uapclaw-darwin.tar.gz",
				"browser_download_url": "https://example.com/darwin",
				"size":               float64(200),
			},
		},
	}
	body, _ := json.Marshal(response)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer server.Close()

	s := NewGitHubReleasesSource("owner", "repo", WithAPIURL(server.URL))
	assets, err := s.FetchAssets(context.Background())
	if err != nil {
		t.Fatalf("FetchAssets 失败: %v", err)
	}
	if len(assets) != 2 {
		t.Fatalf("Assets 数量 = %d, 期望 2", len(assets))
	}
	if assets[0].Name != "uapclaw-linux.tar.gz" {
		t.Errorf("Assets[0].Name = %q, 期望 %q", assets[0].Name, "uapclaw-linux.tar.gz")
	}
	if assets[1].Name != "uapclaw-darwin.tar.gz" {
		t.Errorf("Assets[1].Name = %q, 期望 %q", assets[1].Name, "uapclaw-darwin.tar.gz")
	}
}

// TestGitHubReleasesSource_WithToken 验证 Token 注入到请求头
func TestGitHubReleasesSource_WithToken(t *testing.T) {
	response := map[string]interface{}{
		"tag_name":   "v0.1.0",
		"prerelease": false,
		"assets":     []interface{}{},
	}
	body, _ := json.Marshal(response)

	var receivedToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer server.Close()

	s := NewGitHubReleasesSource("owner", "repo",
		WithAPIURL(server.URL),
		WithToken("ghp_test123"),
	)
	_, err := s.FetchLatest(context.Background())
	if err != nil {
		t.Fatalf("FetchLatest 失败: %v", err)
	}

	expected := "Bearer ghp_test123"
	if receivedToken != expected {
		t.Errorf("Authorization 头 = %q, 期望 %q", receivedToken, expected)
	}
}

// TestCleanVersion 验证版本号清理函数
func TestCleanVersion(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"带v前缀", "v0.2.0", "0.2.0"},
		{"带V前缀", "V0.2.0", "0.2.0"},
		{"无前缀", "0.2.0", "0.2.0"},
		{"带前后空白", "  v0.2.0  ", "0.2.0"},
		{"带预发布后缀", "v0.2.0-beta1", "0.2.0-beta1"},
		{"带预发布rc后缀", "v0.2.0-rc2", "0.2.0-rc2"},
		{"空字符串", "", ""},
		{"仅v前缀", "v", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanVersion(tt.input)
			if result != tt.expected {
				t.Errorf("cleanVersion(%q) = %q, 期望 %q", tt.input, result, tt.expected)
			}
		})
	}
}
