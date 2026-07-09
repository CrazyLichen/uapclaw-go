package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GitHubTree 表示 GitHub 目录树的元数据。
//
// 对应 Python: GitHubTree
type GitHubTree struct {
	// RepoOwner GitHub 仓库所有者
	RepoOwner string
	// RepoName GitHub 仓库名称
	RepoName string
	// TreeRef Git tree 引用，"HEAD" 表示根，子目录使用对应 hash
	TreeRef string
	// Directory 相对于 tree_ref 的搜索目录路径
	Directory string
}

// GitHubError GitHub API 异常。
//
// 对应 Python: GitHubError(Exception)
type GitHubError struct {
	// Message 错误信息
	Message string
}

// RemoteSkillUtil 远程技能工具类，从 GitHub 下载技能文件。
//
// 对应 Python: RemoteSkillUtil
type RemoteSkillUtil struct {
	// sysOperationID 系统操作 ID
	sysOperationID string
	// fsProvider 文件系统操作提供者（写入文件时使用）
	fsProvider FsProvider
	// httpClient HTTP 客户端（可注入，默认使用 http.DefaultClient）
	httpClient *http.Client
}

// gitHubTreeItem GitHub Tree API 返回的单个条目
type gitHubTreeItem struct {
	Path string `json:"path"`
	Type string `json:"type"` // "blob" 或 "tree"
	SHA  string `json:"sha"`
}

// gitHubTreeResponse GitHub Tree API 响应
type gitHubTreeResponse struct {
	Tree      []gitHubTreeItem `json:"tree"`
	Truncated bool             `json:"truncated"`
	Message   string           `json:"message,omitempty"`
}

// gitHubFileItem 带相对路径的文件条目（用于 search_github_for_skills 结果）
type gitHubFileItem struct {
	Path         string
	Type         string
	SHA          string
	RelativePath string
}

// ──────────────────────────── 常量 ────────────────────────────

// githubAPI GitHub API 基础 URL
const githubAPI = "https://api.github.com"

// skillsDirName 技能目录名
const skillsDirName = "skills/"

// githubRequestTimeout GitHub API 请求超时时间
const githubRequestTimeout = 30 * time.Second

// ──────────────────────────── 全局变量 ────────────────────────────

// ensure 接口检查
var _ bytes.Buffer

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGitHubTree 创建 GitHubTree 实例。
//
// 对应 Python: GitHubTree.__init__(repo_owner, repo_name, tree_ref="HEAD", directory=Path(""))
func NewGitHubTree(repoOwner, repoName, treeRef, directory string) *GitHubTree {
	if treeRef == "" {
		treeRef = "HEAD"
	}
	return &GitHubTree{
		RepoOwner: repoOwner,
		RepoName:  repoName,
		TreeRef:   treeRef,
		Directory: directory,
	}
}

// Clone 克隆 GitHubTree 实例。
//
// 对应 Python: GitHubTree.clone()
func (t *GitHubTree) Clone() *GitHubTree {
	return &GitHubTree{
		RepoOwner: t.RepoOwner,
		RepoName:  t.RepoName,
		TreeRef:   t.TreeRef,
		Directory: t.Directory,
	}
}

// Error 返回 GitHubError 的错误信息。
func (e *GitHubError) Error() string {
	return e.Message
}

// NewRemoteSkillUtil 创建 RemoteSkillUtil 实例。
//
// 对应 Python: RemoteSkillUtil.__init__(sys_operation_id)
func NewRemoteSkillUtil(sysOperationID string) *RemoteSkillUtil {
	return &RemoteSkillUtil{
		sysOperationID: sysOperationID,
		fsProvider:     &osFsProvider{},
		httpClient:     &http.Client{Timeout: githubRequestTimeout},
	}
}

// NewRemoteSkillUtilWithProvider 创建使用自定义 FsProvider 的 RemoteSkillUtil 实例。
func NewRemoteSkillUtilWithProvider(sysOperationID string, provider FsProvider) *RemoteSkillUtil {
	return &RemoteSkillUtil{
		sysOperationID: sysOperationID,
		fsProvider:     provider,
		httpClient:     &http.Client{Timeout: githubRequestTimeout},
	}
}

// SetSysOperationID 更新系统操作 ID。
//
// 对应 Python: RemoteSkillUtil.set_sys_operation_id(sys_operation_id)
func (r *RemoteSkillUtil) SetSysOperationID(sysOperationID string) {
	r.sysOperationID = sysOperationID
}

// SysOperationID 返回当前系统操作 ID。
func (r *RemoteSkillUtil) SysOperationID() string {
	return r.sysOperationID
}

// SetFsProvider 设置文件系统操作提供者。
func (r *RemoteSkillUtil) SetFsProvider(provider FsProvider) {
	r.fsProvider = provider
}

// SearchGitHubForSkills 搜索 GitHub 仓库中的技能。
//
// 查找所有 SKILL.md 及同目录文件。O(N) 遍历算法，依赖文件列表按路径排序。
//
// 返回 (fileList, skillPaths, error)：fileList 为带相对路径的文件列表，
// skillPaths 为技能目录名列表。
//
// 对应 Python: RemoteSkillUtil.search_github_for_skills(tree, token)
func (r *RemoteSkillUtil) SearchGitHubForSkills(tree *GitHubTree, token string) ([]gitHubFileItem, []string, error) {
	files, truncated, err := r.listGitHubFiles(tree, token)
	if err != nil {
		return nil, nil, err
	}

	if truncated {
		logger.Warn(logger.ComponentAgentCore).
			Str("event_type", "github_tree_truncated").
			Msg("GitHub 文件列表被截断，结果可能不完整")
	}

	var fileList []gitHubFileItem
	var skillPaths []string

	// O(N) 遍历：files 按 path 排序
	for i, file := range files {
		filePath := file.Path
		if filePath == "" {
			continue
		}

		// 跳过根目录下的 SKILL.md
		parts := strings.Split(strings.ReplaceAll(filePath, "\\", "/"), "/")
		if len(parts) == 1 {
			continue
		}

		// 只处理 SKILL.md
		fileName := filepath.Base(filePath)
		if fileName != SkillFileName {
			continue
		}

		parentDir := filepath.Dir(filePath)
		baseSkillPath := filepath.Base(parentDir)

		// 添加 SKILL.md 自身
		relPath := filepath.Join(baseSkillPath, strings.TrimPrefix(filePath, parentDir+string(filepath.Separator)))
		fileList = append(fileList, gitHubFileItem{
			Path:         filePath,
			Type:         file.Type,
			SHA:          file.SHA,
			RelativePath: relPath,
		})
		skillPaths = append(skillPaths, baseSkillPath)

		// 向左搜索同目录文件
		for j := i - 1; j >= 0; j-- {
			prevFile := files[j]
			if !isRelativeTo(prevFile.Path, parentDir) {
				break
			}
			prevRelPath := filepath.Join(baseSkillPath, strings.TrimPrefix(prevFile.Path, parentDir+string(filepath.Separator)))
			fileList = append(fileList, gitHubFileItem{
				Path:         prevFile.Path,
				Type:         prevFile.Type,
				SHA:          prevFile.SHA,
				RelativePath: prevRelPath,
			})
		}

		// 向右搜索同目录文件
		for j := i + 1; j < len(files); j++ {
			nextFile := files[j]
			if !isRelativeTo(nextFile.Path, parentDir) {
				break
			}
			nextRelPath := filepath.Join(baseSkillPath, strings.TrimPrefix(nextFile.Path, parentDir+string(filepath.Separator)))
			fileList = append(fileList, gitHubFileItem{
				Path:         nextFile.Path,
				Type:         nextFile.Type,
				SHA:          nextFile.SHA,
				RelativePath: nextRelPath,
			})
		}
	}

	return fileList, skillPaths, nil
}

// UploadSkillFromGitHub 从 GitHub 下载技能文件并写入文件系统。
//
// 对应 Python: RemoteSkillUtil.upload_skill_from_github(tree, skills_dir, token)
func (r *RemoteSkillUtil) UploadSkillFromGitHub(tree *GitHubTree, skillsDir string, token string) ([]string, error) {
	fileList, skillPaths, err := r.SearchGitHubForSkills(tree, token)
	if err != nil {
		return nil, err
	}

	for _, file := range fileList {
		data, err := DownloadFileFromGitHub(tree, file.Path, token)
		if err != nil {
			return nil, fmt.Errorf("下载 GitHub 文件失败 %s: %w", file.Path, err)
		}

		relativePath := file.RelativePath
		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "skill_file_upload").
			Str("relative_path", relativePath).
			Msg("上传技能文件")

		fullPath := filepath.Join(skillsDir, relativePath)
		if err := r.fsProvider.WriteFile(fullPath, data); err != nil {
			return nil, fmt.Errorf("写入文件失败 %s: %w", fullPath, err)
		}
	}

	return skillPaths, nil
}

// DownloadFileFromGitHub 从 GitHub 下载单个文件的原始内容。
//
// 静态方法，使用 GitHub Contents API（Accept: application/vnd.github.raw）。
//
// 对应 Python: RemoteSkillUtil.download_file_from_github(tree, file_path, token)
func DownloadFileFromGitHub(tree *GitHubTree, filePath string, token string) ([]byte, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s", githubAPI, tree.RepoOwner, tree.RepoName, filePath)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github.raw")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	// 添加 ref 参数
	q := req.URL.Query()
	q.Set("ref", tree.TreeRef)
	req.URL.RawQuery = q.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 GitHub API 失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, &GitHubError{Message: fmt.Sprintf("HTTP %d 下载 %s 失败", resp.StatusCode, filePath)}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}

	return data, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// recursivelyListGitHubFiles 递归遍历 GitHub Tree API。
//
// 如果 directory 为空路径，使用 recursive=492 参数一次性拉取整棵 Git tree，
// 然后只保留 type == "blob" 的文件项。
// 如果 directory 非空，则逐层递归：先获取当前 tree ref 的顶层，
// 找匹配 directory 第一段的子树（type == "tree"），用其 sha 作为新的 tree_ref 继续递归。
//
// 对应 Python: RemoteSkillUtil._recursively_list_github_files(tree, current_directory, token)
func (r *RemoteSkillUtil) recursivelyListGitHubFiles(tree *GitHubTree, currentDirectory string, token string) ([]gitHubTreeItem, bool, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s", githubAPI, tree.RepoOwner, tree.RepoName, tree.TreeRef)

	relativeDirectory := tree.Directory

	if relativeDirectory == "" || relativeDirectory == "." {
		// 获取整棵树 — 使用 recursive 参数
		respData, err := r.fetchGitHubTree(url, token, true)
		if err != nil {
			return nil, false, err
		}
		if respData.Message != "" {
			return nil, false, fmt.Errorf("GitHub API 错误: %s", respData.Message)
		}

		// 只保留 blob 类型（文件）
		var files []gitHubTreeItem
		for _, item := range respData.Tree {
			if item.Type == "blob" {
				// 将路径前缀加上 currentDirectory
				if currentDirectory != "" && currentDirectory != "." {
					item.Path = currentDirectory + "/" + item.Path
				}
				files = append(files, item)
			}
		}
		return files, respData.Truncated, nil
	}

	// 非递归获取当前层的 tree
	respData, err := r.fetchGitHubTree(url, token, false)
	if err != nil {
		return nil, false, err
	}
	if respData.Message != "" {
		return nil, false, &GitHubError{Message: respData.Message}
	}

	// 取 directory 的第一段作为下一级目录名
	normalizedDir := strings.ReplaceAll(relativeDirectory, "\\", "/")
	dirParts := strings.SplitN(normalizedDir, "/", 2)
	nextDirectory := dirParts[0]
	remainderDirectory := ""
	if len(dirParts) > 1 {
		remainderDirectory = dirParts[1]
	}

	// 在当前 tree 中查找匹配的子树
	for _, item := range respData.Tree {
		if item.Type == "tree" && item.Path == nextDirectory {
			newTree := tree.Clone()
			newTree.TreeRef = item.SHA
			newTree.Directory = remainderDirectory
			newCurrentDir := nextDirectory
			if currentDirectory != "" && currentDirectory != "." {
				newCurrentDir = currentDirectory + "/" + nextDirectory
			}
			return r.recursivelyListGitHubFiles(newTree, newCurrentDir, token)
		}
	}

	return nil, false, &GitHubError{Message: fmt.Sprintf("目录 %s 在 %s 中未找到", nextDirectory, currentDirectory)}
}

// listGitHubFiles 列出 GitHub 文件（入口方法）。
//
// 如果 directory 是绝对路径，去掉根前缀。
//
// 对应 Python: RemoteSkillUtil._list_github_files(tree, token)
func (r *RemoteSkillUtil) listGitHubFiles(tree *GitHubTree, token string) ([]gitHubTreeItem, bool, error) {
	// 处理绝对路径：去掉根前缀
	dir := tree.Directory
	if filepath.IsAbs(dir) {
		// 去掉前导 / 或 \
		dir = strings.TrimLeft(dir, "/\\")
	}
	clone := tree.Clone()
	clone.Directory = dir
	return r.recursivelyListGitHubFiles(clone, "", token)
}

// fetchGitHubFiles 发送 GitHub Tree API GET 请求并解析 JSON 响应。
func (r *RemoteSkillUtil) fetchGitHubTree(url string, token string, recursive bool) (*gitHubTreeResponse, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	if recursive {
		q := req.URL.Query()
		q.Set("recursive", "492")
		req.URL.RawQuery = q.Encode()
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 GitHub Tree API 失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// 尝试解析错误消息
		var errResp struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Message != "" {
			return &gitHubTreeResponse{Message: errResp.Message}, nil
		}
		return nil, fmt.Errorf("GitHub API HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result gitHubTreeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析 GitHub Tree API 响应失败: %w", err)
	}

	return &result, nil
}

// isRelativeTo 判断 path 是否相对于 baseDir（即 path 以 baseDir 为前缀）。
//
// 对应 Python: Path.is_relative_to(parent_directory)
func isRelativeTo(path, baseDir string) bool {
	normalizedPath := strings.ReplaceAll(path, "\\", "/")
	normalizedBase := strings.ReplaceAll(baseDir, "\\", "/")
	if !strings.HasSuffix(normalizedBase, "/") {
		normalizedBase += "/"
	}
	return strings.HasPrefix(normalizedPath, normalizedBase)
}

// fetchGitHubRaw 发送 GitHub API GET 请求并返回原始响应体。
func fetchGitHubRaw(url string, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.raw")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, &GitHubError{Message: fmt.Sprintf("HTTP %d 请求 %s 失败", resp.StatusCode, url)}
	}

	return io.ReadAll(resp.Body)
}
