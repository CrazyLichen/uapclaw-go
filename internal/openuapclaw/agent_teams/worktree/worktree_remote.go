package worktree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	agent_teams "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/worktree"
)

// 本文件为远端节点分布式 worktree 后端（团队专属）。
// Python: agent_teams/worktree_remote.py（一文件对一文件；上游即位于 agent_teams 包，
// 因依赖 agent_teams.paths 的浅克隆缓存布局——落点按 Python 包归属，撤 D-25.7-f）。
//
// 跨机 worktree 隔离：leader 经 Messager 发送 worktree 生命周期请求；
// 每个远端节点维护自己的浅克隆并在其中创建本地 worktree。
//
// 本文件导入不影响 harness 后端注册表——需要远端后端的调用方直接构造
// RemoteWorktreeBackend(config, messager, nodeID) 并作为 backend= 传给
// WorktreeManager 构造（Python worktree_remote.py 模块注释语义）。

// ──────────────────────────── 结构体 ────────────────────────────

// WorktreeMessager 远端通信窄接口（偏差 D-25.7-b：Python messager: Any 的
// send_and_wait 面 → Go 结构化窄接口；团队 messager 适配器实现）。
type WorktreeMessager interface {
	// SendAndWait 发送载荷到节点并等待响应。
	SendAndWait(ctx context.Context, nodeID string, payload map[string]any) (map[string]any, error)
}

// WorktreeRemoteRequest 发给远端节点管理 worktree 的请求。
// Python: worktree_remote.py WorktreeRemoteRequest (BaseModel, :46-65)
type WorktreeRemoteRequest struct {
	// Action 操作："create" / "remove" / "exists"
	Action string `json:"action"`
	// Slug worktree slug（create 必填）
	Slug string `json:"slug,omitempty"`
	// RepoURL 源仓库 git 远端 URL（create 必填）
	RepoURL string `json:"repo_url,omitempty"`
	// BaseBranch 新 worktree 的基准分支（默认 "main"）
	BaseBranch string `json:"base_branch,omitempty"`
	// WorktreePath 远端节点上的绝对路径（remove/exists 必填）
	WorktreePath string `json:"worktree_path,omitempty"`
	// Config 序列化的 WorktreeConfig
	Config map[string]any `json:"config,omitempty"`
}

// WorktreeRemoteResponse 远端节点 worktree 操作的响应。
// Python: worktree_remote.py WorktreeRemoteResponse (BaseModel, :68-93)
type WorktreeRemoteResponse struct {
	// Success 操作是否成功
	Success bool `json:"success"`
	// WorktreePath 远端节点上 worktree 的绝对路径
	WorktreePath string `json:"worktree_path,omitempty"`
	// WorktreeBranch 创建的 git 分支名
	WorktreeBranch string `json:"worktree_branch,omitempty"`
	// HeadCommit 创建后的 HEAD 提交 SHA
	HeadCommit string `json:"head_commit,omitempty"`
	// Existed worktree 已存在（快速恢复）
	Existed bool `json:"existed"`
	// Exists 回答 "exists" 查询为真
	Exists bool `json:"exists"`
	// Error 失败时的错误信息
	Error string `json:"error,omitempty"`
}

// RemoteWorktreeBackend 远端节点的 worktree 后端（leader 侧）。
// Python: worktree_remote.py RemoteWorktreeBackend (:96-230)
//
// 与共享仓库上的 git worktree add 不同，每个远端节点维护自己的克隆。
// 与远端的通信经 Messager（P2P）：leader 调 create/remove/exists，每次调用
// 序列化 WorktreeRemoteRequest 发给目标节点并等待 WorktreeRemoteResponse。
type RemoteWorktreeBackend struct {
	// config Worktree 配置
	config worktree.WorktreeConfig
	// messager P2P 通信面
	messager WorktreeMessager
	// nodeID 目标节点标识
	nodeID string
}

// 编译期确认实现 WorktreeBackend 协议。
var _ worktree.WorktreeBackend = (*RemoteWorktreeBackend)(nil)

// WorktreeRemoteHandler 远端节点上的 worktree 请求处理器。
// Python: worktree_remote.py WorktreeRemoteHandler (:232-353)
//
// 在 Messager 上注册为直发消息处理器。确保仓库已克隆到本地，然后委托
// 本地 WorktreeManager。
type WorktreeRemoteHandler struct {
	// manager 本地 worktree 管理器
	manager *worktree.WorktreeManager
	// clonedRepos URL → 本地克隆路径缓存
	clonedRepos map[string]string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRemoteWorktreeBackend 构造远端后端。
// Python: RemoteWorktreeBackend.__init__(config, messager, node_id) (:110-114)
func NewRemoteWorktreeBackend(config worktree.WorktreeConfig, messager WorktreeMessager, nodeID string) *RemoteWorktreeBackend {
	return &RemoteWorktreeBackend{config: config, messager: messager, nodeID: nodeID}
}

// Create 请求远端节点创建 worktree。
// Python: RemoteWorktreeBackend.create(slug, repo_root, target_path) (:116-153)
//
// targetPath 在此被忽略——远端节点拥有自己的文件系统布局，从其本地
// DeepAgent workspace 解析自己的目标路径；接受该参数仅为满足
// WorktreeBackend 协议（Python del target_path 语义）。
func (b *RemoteWorktreeBackend) Create(ctx context.Context, slug, repoRoot, _ string) (*worktree.WorktreeCreateResult, error) {
	repoURL, err := b.repoURL(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	request := &WorktreeRemoteRequest{
		Action:     "create",
		Slug:       slug,
		RepoURL:    repoURL,
		BaseBranch: worktree.GetDefaultBranch(ctx, repoRoot),
		Config:     worktreeConfigToMap(b.config),
	}
	response, err := b.sendAndWait(ctx, request)
	if err != nil {
		return nil, err
	}
	if !response.Success {
		return nil, fmt.Errorf("Remote worktree creation failed: %s", response.Error)
	}
	return &worktree.WorktreeCreateResult{
		WorktreePath:   response.WorktreePath,
		WorktreeBranch: response.WorktreeBranch,
		HeadCommit:     response.HeadCommit,
		Existed:        response.Existed,
	}, nil
}

// Remove 请求远端节点删除 worktree。
// Python: RemoteWorktreeBackend.remove(worktree_path, repo_root) (:154-170)
//
// repoRoot 未用（协议占位，Python del repo_root 语义）。
func (b *RemoteWorktreeBackend) Remove(ctx context.Context, worktreePath, _ string, _ bool) bool {
	response, err := b.sendAndWait(ctx, &WorktreeRemoteRequest{
		Action:       "remove",
		WorktreePath: worktreePath,
	})
	if err != nil {
		return false
	}
	return response.Success
}

// Exists 询问远端节点 worktree 是否存在。
// Python: RemoteWorktreeBackend.exists(worktree_path) (:172-188)
func (b *RemoteWorktreeBackend) Exists(ctx context.Context, worktreePath string) bool {
	response, err := b.sendAndWait(ctx, &WorktreeRemoteRequest{
		Action:       "exists",
		WorktreePath: worktreePath,
	})
	if err != nil {
		return false
	}
	return response.Exists
}

// NewWorktreeRemoteHandler 构造远端处理器并把 manager 后端切到远端基准模式。
// Python: WorktreeRemoteHandler.__init__(manager) (:240-250)
//
// 本节点的克隆永不前进本地分支；基准必须来自每请求拉取的远端 ref，
// 而非本地优先（Python backend.remote_base = True 语义）。
func NewWorktreeRemoteHandler(manager *worktree.WorktreeManager) *WorktreeRemoteHandler {
	h := &WorktreeRemoteHandler{manager: manager, clonedRepos: map[string]string{}}
	if manager != nil && manager.Backend() != nil {
		worktree.EnableRemoteBase(manager.Backend())
	}
	return h
}

// Handle 把 worktree 请求分派到对应处理器。
// Python: WorktreeRemoteHandler.handle(request) (:251-270)
func (h *WorktreeRemoteHandler) Handle(ctx context.Context, request *WorktreeRemoteRequest) *WorktreeRemoteResponse {
	switch request.Action {
	case "create":
		return h.handleCreate(ctx, request)
	case "remove":
		return h.handleRemove(ctx, request)
	case "exists":
		return h.handleExists(ctx, request)
	default:
		return &WorktreeRemoteResponse{Success: false, Error: fmt.Sprintf("Unknown action: %s", request.Action)}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sendAndWait 经 Messager 发送请求并等待响应。
// Python: RemoteWorktreeBackend._send_and_wait(request) (:190-210)
func (b *RemoteWorktreeBackend) sendAndWait(ctx context.Context, request *WorktreeRemoteRequest) (*WorktreeRemoteResponse, error) {
	logger.Debug(logComponent).
		Str("action", request.Action).
		Str("node_id", b.nodeID).
		Msg("Sending worktree request to node")
	payload := worktreeRequestToMap(request)
	responseData, err := b.messager.SendAndWait(ctx, b.nodeID, payload)
	if err != nil {
		return nil, err
	}
	return worktreeResponseFromMap(responseData)
}

// repoURL 解析本地仓库的 origin 远端 URL。
// Python: RemoteWorktreeBackend._get_repo_url(repo_root) (:211-230)
func (b *RemoteWorktreeBackend) repoURL(ctx context.Context, repoRoot string) (string, error) {
	r := worktree.RunGit(ctx, []string{"remote", "get-url", "origin"}, repoRoot)
	if !r.OK() {
		return "", fmt.Errorf("Cannot determine remote URL")
	}
	return r.Stdout, nil
}

// handleCreate 需要时克隆仓库、拉取基准分支、创建本地 worktree。
// Python: WorktreeRemoteHandler._handle_create(request) (:272-289)
func (h *WorktreeRemoteHandler) handleCreate(ctx context.Context, request *WorktreeRemoteRequest) *WorktreeRemoteResponse {
	repoRoot, err := h.ensureRepo(ctx, request.RepoURL)
	if err != nil {
		return &WorktreeRemoteResponse{Success: false, Error: err.Error()}
	}
	worktree.FetchRef(ctx, repoRoot, orDefault(request.BaseBranch, "main"))
	result, err := h.manager.CreateOwnerWorktree(ctx, request.Slug)
	if err != nil {
		return &WorktreeRemoteResponse{Success: false, Error: err.Error()}
	}
	return &WorktreeRemoteResponse{
		Success:        true,
		WorktreePath:   result.WorktreePath,
		WorktreeBranch: result.WorktreeBranch,
		HeadCommit:     result.HeadCommit,
		Existed:        result.Existed,
	}
}

// handleRemove 删除本节点上的 worktree。
// Python: WorktreeRemoteHandler._handle_remove(request) (:291-307)
//
// 注意：Python 走 manager.remove_worktree（force 默认 False）。
func (h *WorktreeRemoteHandler) handleRemove(ctx context.Context, request *WorktreeRemoteRequest) *WorktreeRemoteResponse {
	wtPath := request.WorktreePath
	repoRoot, err := worktree.FindCanonicalGitRoot(ctx, wtPath)
	if err != nil || repoRoot == "" {
		return &WorktreeRemoteResponse{Success: false, Error: "Cannot find repo root for worktree"}
	}
	ok := h.manager.RemoveWorktree(ctx, wtPath, repoRoot)
	return &WorktreeRemoteResponse{Success: ok}
}

// handleExists 检查本节点上 worktree 是否存在。
// Python: WorktreeRemoteHandler._handle_exists(request) (:309-320)
func (h *WorktreeRemoteHandler) handleExists(ctx context.Context, request *WorktreeRemoteRequest) *WorktreeRemoteResponse {
	found := h.manager.Backend().Exists(ctx, request.WorktreePath)
	return &WorktreeRemoteResponse{Success: true, Exists: found}
}

// ensureRepo 本节点不存在仓库时克隆。
// Python: WorktreeRemoteHandler._ensure_repo(repo_url) (:322-352)
//
// 使用 {get_agent_teams_home()}/remote_repos/ 下的内容寻址目录，同一 URL
// 恒映射到同一本地路径。
func (h *WorktreeRemoteHandler) ensureRepo(ctx context.Context, repoURL string) (string, error) {
	if local, ok := h.clonedRepos[repoURL]; ok {
		return local, nil
	}

	digest := sha256.Sum256([]byte(repoURL))
	repoHash := hex.EncodeToString(digest[:])[:12]
	localPath := filepath.Join(agent_teams.GetAgentTeamsHome(), "remote_repos", repoHash)

	if _, err := os.Stat(filepath.Join(localPath, ".git")); err != nil {
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			return "", err
		}
		logger.Info(logComponent).Str("repo_url", repoURL).Str("local_path", localPath).
			Msg("Cloning remote repository")
		cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", repoURL, localPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git clone failed: %v: %s", err, out)
		}
	}

	h.clonedRepos[repoURL] = localPath
	return localPath, nil
}

// worktreeRequestToMap 请求序列化为 Messager 载荷（Python model_dump）。
func worktreeRequestToMap(r *WorktreeRemoteRequest) map[string]any {
	return map[string]any{
		"action":        r.Action,
		"slug":          r.Slug,
		"repo_url":      r.RepoURL,
		"base_branch":   r.BaseBranch,
		"worktree_path": r.WorktreePath,
		"config":        r.Config,
	}
}

// worktreeResponseFromMap 响应反序列化（Python model_validate）。
func worktreeResponseFromMap(data map[string]any) (*WorktreeRemoteResponse, error) {
	resp := &WorktreeRemoteResponse{}
	getStr := func(key string) string {
		if v, ok := data[key].(string); ok {
			return v
		}
		return ""
	}
	getBool := func(key string) bool {
		if v, ok := data[key].(bool); ok {
			return v
		}
		return false
	}
	resp.Success = getBool("success")
	resp.WorktreePath = getStr("worktree_path")
	resp.WorktreeBranch = getStr("worktree_branch")
	resp.HeadCommit = getStr("head_commit")
	resp.Existed = getBool("existed")
	resp.Exists = getBool("exists")
	resp.Error = getStr("error")
	return resp, nil
}

// worktreeConfigToMap WorktreeConfig 序列化（Python model_dump）。
func worktreeConfigToMap(cfg worktree.WorktreeConfig) map[string]any {
	return map[string]any{
		"enabled":                    cfg.Enabled,
		"base_dir":                   cfg.BaseDir,
		"base_dir_is_worktrees_dir":  cfg.BaseDirIsWorktreesDir,
		"sparse_paths":               cfg.SparsePaths,
		"symlink_directories":        cfg.SymlinkDirectories,
		"include_patterns":           cfg.IncludePatterns,
		"cleanup_after_days":         cfg.CleanupAfterDays,
		"auto_cleanup_on_shutdown":   cfg.AutoCleanupOnShutdown,
		"lifecycle_policy":           string(cfg.LifecyclePolicy),
		"repo_lock":                  cfg.RepoLock,
		"repo_lock_timeout":          cfg.RepoLockTimeout,
		"longpaths_support":          cfg.LongpathsSupport,
	}
}
