package version

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ──────────────────────────── 接口 ────────────────────────────

// VersionSource 版本源接口，定义从远程获取最新版本的标准方法
type VersionSource interface {
	// FetchLatest 获取最新发布版本信息
	FetchLatest(ctx context.Context) (*ReleaseInfo, error)
	// FetchAssets 获取最新发布的资产列表
	FetchAssets(ctx context.Context) ([]ReleaseAsset, error)
}

// ──────────────────────────── 结构体 ────────────────────────────

// GitHubSourceOption GitHub 版本源的可选配置
type GitHubSourceOption func(*GitHubReleasesSource)

// GitHubReleasesSource 从 GitHub Releases API 获取最新版本
type GitHubReleasesSource struct {
	// owner 仓库所有者
	owner string
	// repo 仓库名称
	repo string
	// apiURL GitHub API 地址（支持自定义，如 GitHub Enterprise）
	apiURL string
	// token GitHub API Token（可选，用于提高速率限制）
	token string
	// timeout HTTP 请求超时时间
	timeout time.Duration
	// client HTTP 客户端
	client *http.Client
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultGitHubTimeout 默认 HTTP 请求超时
	defaultGitHubTimeout = 20 * time.Second
	// githubAPIURL 默认 GitHub API 地址
	githubAPIURL = "https://api.github.com/repos/%s/%s/releases/latest"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGitHubReleasesSource 创建 GitHub 版本源实例
//
// 参数：
//   - owner: 仓库所有者（如 "openJiuwen"）
//   - repo: 仓库名称（如 "uapclaw"）
//   - opts: 可选配置（WithToken, WithAPIURL, WithTimeout）
func NewGitHubReleasesSource(owner, repo string, opts ...GitHubSourceOption) *GitHubReleasesSource {
	s := &GitHubReleasesSource{
		owner:   owner,
		repo:    repo,
		apiURL:  fmt.Sprintf(githubAPIURL, owner, repo),
		timeout: defaultGitHubTimeout,
		client:  &http.Client{Timeout: defaultGitHubTimeout},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithToken 设置 GitHub API Token
func WithToken(token string) GitHubSourceOption {
	return func(s *GitHubReleasesSource) {
		s.token = token
	}
}

// WithAPIURL 设置自定义 GitHub API 地址（支持 GitHub Enterprise）
func WithAPIURL(url string) GitHubSourceOption {
	return func(s *GitHubReleasesSource) {
		if url != "" {
			s.apiURL = url
		}
	}
}

// WithTimeout 设置 HTTP 请求超时时间
func WithTimeout(timeout time.Duration) GitHubSourceOption {
	return func(s *GitHubReleasesSource) {
		if timeout > 0 {
			s.timeout = timeout
			s.client.Timeout = timeout
		}
	}
}

// FetchLatest 从 GitHub Releases API 获取最新发布版本信息
func (s *GitHubReleasesSource) FetchLatest(ctx context.Context) (*ReleaseInfo, error) {
	data, err := s.fetchJSON(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取 GitHub 发布信息失败: %w", err)
	}

	return s.parseRelease(data)
}

// FetchAssets 获取最新发布的资产列表
func (s *GitHubReleasesSource) FetchAssets(ctx context.Context) ([]ReleaseAsset, error) {
	info, err := s.FetchLatest(ctx)
	if err != nil {
		return nil, err
	}
	return info.Assets, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fetchJSON 从 GitHub API 获取 JSON 数据
func (s *GitHubReleasesSource) fetchJSON(ctx context.Context) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("%s-Updater/%s", s.repo, Version))
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败: %w", err)
	}

	return data, nil
}

// parseRelease 解析 GitHub API 返回的发布信息
func (s *GitHubReleasesSource) parseRelease(data map[string]interface{}) (*ReleaseInfo, error) {
	// 提取 tag_name
	tagName, _ := data["tag_name"].(string)
	ver := cleanVersion(tagName)
	if ver == "" {
		return nil, fmt.Errorf("GitHub release tag_name 为空或缺失")
	}

	// 提取其他字段
	publishedAt, _ := data["published_at"].(string)
	body, _ := data["body"].(string)
	prerelease, _ := data["prerelease"].(bool)
	draft, _ := data["draft"].(bool)

	// 提取资产列表
	var assets []ReleaseAsset
	if rawAssets, ok := data["assets"].([]interface{}); ok {
		for _, item := range rawAssets {
			assetMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := assetMap["name"].(string)
			if name == "" {
				continue
			}
			downloadURL, _ := assetMap["browser_download_url"].(string)
			size, _ := assetMap["size"].(float64)

			assets = append(assets, ReleaseAsset{
				Name:         name,
				DownloadURL:  downloadURL,
				Size:         int64(size),
			})
		}
	}

	return &ReleaseInfo{
		Version:      ver,
		ReleaseNotes: body,
		PublishedAt:  publishedAt,
		Assets:       assets,
		SourceType:   "github",
		Prerelease:   prerelease || draft,
	}, nil
}

// cleanVersion 清理版本号：去除前后空白、v/V 前缀
// 保留完整的版本号（含预发布后缀如 -beta1），仅去除 v 前缀
func cleanVersion(raw string) string {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimLeft(cleaned, "vV")
	// 如果去除 v 前缀后仍有内容，直接返回（保留 -beta1 等预发布后缀）
	if cleaned != "" {
		return cleaned
	}
	return ""
}
