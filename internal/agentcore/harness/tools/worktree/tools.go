package worktree

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	toolspkg "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
)

// ──────────────────────────── 结构体 ────────────────────────────

// worktreeToolBase 包内私有工具基类。
// Python: _WorktreeToolBase(Tool)
//
// Worktree 操作本质上是单次生命周期调用，不支持流式。
type worktreeToolBase struct {
	// card 工具配置卡片
	card *tool.ToolCard
	// manager Worktree 管理器
	manager *WorktreeManager
}

// EnterWorktreeTool 创建或进入隔离 git worktree 的工具。
// Python: EnterWorktreeTool
type EnterWorktreeTool struct {
	worktreeToolBase
	// language 语言偏好
	language string
	// agentID Agent 标识
	agentID string
}

// ExitWorktreeTool 退出当前 worktree 会话的工具。
// Python: ExitWorktreeTool
type ExitWorktreeTool struct {
	worktreeToolBase
	// language 语言偏好
	language string
	// agentID Agent 标识
	agentID string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// randomSlugAdjectives 随机 slug 形容词列表。
// Python: _generate_random_slug() → adjectives
var randomSlugAdjectives = []string{"swift", "bright", "calm", "keen", "bold"}

// randomSlugNouns 随机 slug 名词列表。
// Python: _generate_random_slug() → nouns
var randomSlugNouns = []string{"fox", "owl", "elm", "oak", "ray"}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEnterWorktreeTool 创建 EnterWorktreeTool 实例。
// Python: EnterWorktreeTool.__init__(manager, language, agent_id)
func NewEnterWorktreeTool(manager *WorktreeManager, language, agentID string) (*EnterWorktreeTool, error) {
	card, err := toolspkg.BuildToolCard("enter_worktree", "worktree.enter", language, nil, agentID)
	if err != nil {
		return nil, fmt.Errorf("构建 enter_worktree ToolCard 失败: %w", err)
	}
	return &EnterWorktreeTool{
		worktreeToolBase: worktreeToolBase{card: card, manager: manager},
		language:         language,
		agentID:          agentID,
	}, nil
}

// NewExitWorktreeTool 创建 ExitWorktreeTool 实例。
// Python: ExitWorktreeTool.__init__(manager, language, agent_id)
func NewExitWorktreeTool(manager *WorktreeManager, language, agentID string) (*ExitWorktreeTool, error) {
	card, err := toolspkg.BuildToolCard("exit_worktree", "worktree.exit", language, nil, agentID)
	if err != nil {
		return nil, fmt.Errorf("构建 exit_worktree ToolCard 失败: %w", err)
	}
	return &ExitWorktreeTool{
		worktreeToolBase: worktreeToolBase{card: card, manager: manager},
		language:         language,
		agentID:          agentID,
	}, nil
}

// Card 返回工具卡片。
func (t *worktreeToolBase) Card() *tool.ToolCard { return t.card }

// Stream 不支持流式调用。
func (t *worktreeToolBase) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.String())
}

// Invoke EnterWorktreeTool 的执行入口。
// Python: EnterWorktreeTool.invoke(inputs, **kwargs)
func (t *EnterWorktreeTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	// 优先通过 manager.sessionState 获取（S-21 修复），回退到 ctx 传播
	var existing *WorktreeSession
	if t.manager != nil && t.manager.SessionState() != nil {
		existing = t.manager.SessionState().GetCurrentSession()
	} else {
		existing = GetCurrentSession(ctx)
	}
	if existing != nil {
		return map[string]any{
			"error": fmt.Sprintf("Already in worktree '%s'. Exit first with exit_worktree.", existing.WorktreeName),
		}, nil
	}

	slug, existed, err := t.resolveSlug(ctx, inputs)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	ownerID, tag := resolveOwner(inputs)

	session, err := t.manager.Enter(ctx, slug, ownerID, tag)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("Failed to create worktree: %s", err)}, nil
	}

	// 切换 CWD
	cwdState := cwd.CwdStateFromCtx(ctx)
	if cwdState != nil {
		cwdState.SetCwd(session.WorktreePath)
		cwdState.SetOriginalCwd(session.WorktreePath)
	}

	message := fmt.Sprintf("Created worktree at %s on branch %s. CWD switched to worktree.",
		session.WorktreePath, session.WorktreeBranch)
	if existed {
		message = fmt.Sprintf("Entered existing worktree at %s on branch %s. CWD switched to worktree.",
			session.WorktreePath, session.WorktreeBranch)
	}

	result := map[string]any{
		"worktree_path":   session.WorktreePath,
		"worktree_branch": session.WorktreeBranch,
		"message":         message,
	}
	if existed {
		result["existed"] = true
	}
	return result, nil
}

// Invoke ExitWorktreeTool 的执行入口。
// Python: ExitWorktreeTool.invoke(inputs, **kwargs)
func (t *ExitWorktreeTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	session := GetCurrentSession(ctx)
	if session == nil {
		return map[string]any{"error": "No active worktree session to exit."}, nil
	}

	action, _ := inputs["action"].(string)
	if action != "keep" && action != "remove" {
		return map[string]any{"error": "'action' must be 'keep' or 'remove'."}, nil
	}

	discard := false
	if v, ok := inputs["discard_changes"]; ok {
		if b, ok := v.(bool); ok {
			discard = b
		}
	}

	// 统计变更（用于 remove + discard 场景的报告）
	var discardedFiles, discardedCommits *int
	if action == "remove" && discard {
		summary := t.manager.CountChanges(ctx, session)
		if summary != nil {
			discardedFiles = &summary.ChangedFiles
			discardedCommits = &summary.Commits
		}
	}

	result, err := t.manager.Exit(ctx, action, discard)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	// 恢复 CWD
	if originalCwd, ok := result["original_cwd"]; ok && originalCwd != "" {
		cwdState := cwd.CwdStateFromCtx(ctx)
		if cwdState != nil {
			cwdState.SetCwd(originalCwd)
			cwdState.SetOriginalCwd(originalCwd)
		}
	}

	// 构建人类可读消息
	branch := result["worktree_branch"]
	if branch == "" {
		branch = "unknown"
	}
	var message string
	if action == "keep" {
		message = fmt.Sprintf("Kept worktree '%s' (branch %s). In this session, enter_worktree without a name will re-enter it; from another session, pass name='%s'. Returned to %s",
			session.WorktreeName, branch, session.WorktreeName, result["original_cwd"])
	} else {
		message = fmt.Sprintf("Removed worktree '%s' (branch %s). Returned to %s",
			session.WorktreeName, branch, result["original_cwd"])
	}

	data := map[string]any{
		"action":          result["action"],
		"worktree_name":   session.WorktreeName,
		"worktree_path":   result["worktree_path"],
		"worktree_branch": result["worktree_branch"],
		"original_cwd":    result["original_cwd"],
		"message":         message,
	}
	if discardedFiles != nil {
		data["discarded_files"] = *discardedFiles
	}
	if discardedCommits != nil {
		data["discarded_commits"] = *discardedCommits
	}
	return data, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveSlug 解析 worktree slug。
// Python: EnterWorktreeTool._resolve_slug(inputs)
func (t *EnterWorktreeTool) resolveSlug(ctx context.Context, inputs map[string]any) (string, bool, error) {
	requested := inputs["name"]

	if requested == nil {
		// 无 name：使用默认或生成随机
		slug := GetDefaultWorktreeName(ctx)
		if slug == "" {
			slug = generateRandomSlug()
			if err := ValidateSlug(slug); err != nil {
				return "", false, err
			}
			SetDefaultWorktreeName(ctx, slug)
		} else {
			if err := ValidateSlug(slug); err != nil {
				return "", false, err
			}
		}
		existed := t.slugExists(ctx, slug)
		return slug, existed, nil
	}

	name, ok := requested.(string)
	if !ok {
		return "", false, fmt.Errorf("'name' must be a string")
	}
	selected := strings.TrimSpace(name)
	if selected == "" {
		return "", false, fmt.Errorf("'name' must not be empty")
	}

	if err := ValidateSlug(selected); err != nil {
		return "", false, err
	}
	existed := t.slugExists(ctx, selected)
	return selected, existed, nil
}

// slugExists 检查 slug 对应的 worktree 是否已存在。
// Python: EnterWorktreeTool._slug_exists(slug)
func (t *EnterWorktreeTool) slugExists(ctx context.Context, slug string) bool {
	workspace := cwd.GetWorkspace(ctx)
	if workspace == "" {
		return false
	}
	targetPath := WorktreePathFor(workspace, slug)
	return t.manager.Backend().Exists(ctx, targetPath)
}

// generateRandomSlug 生成短随机 worktree 名称。
// Python: _generate_random_slug()
//
// 格式：<adjective>-<noun>-<4hex>
func generateRandomSlug() string {
	adj := randomSlugAdjectives[randomInt(len(randomSlugAdjectives))]
	noun := randomSlugNouns[randomInt(len(randomSlugNouns))]
	suffix := randomHex(2)
	return fmt.Sprintf("%s-%s-%s", adj, noun, suffix)
}

// resolveOwner 从 inputs 中解析 owner 信息。
// Python: _resolve_owner(kwargs) — kwargs.get("owner_id") or kwargs.get("member_name", "")
func resolveOwner(inputs map[string]any) (string, string) {
	ownerID := ""
	if v, ok := inputs["owner_id"]; ok {
		if s, ok := v.(string); ok && s != "" {
			ownerID = s
		}
	}
	if ownerID == "" {
		if v, ok := inputs["member_name"]; ok {
			if s, ok := v.(string); ok {
				ownerID = s
			}
		}
	}
	tag := ""
	if v, ok := inputs["tag"]; ok {
		if s, ok := v.(string); ok && s != "" {
			tag = s
		}
	}
	if tag == "" {
		if v, ok := inputs["team_name"]; ok {
			if s, ok := v.(string); ok {
				tag = s
			}
		}
	}
	return ownerID, tag
}

// randomInt 生成 [0, n) 范围的随机整数
func randomInt(n int) int {
	if n <= 0 {
		return 0
	}
	b := make([]byte, 1)
	_, _ = rand.Read(b)
	return int(b[0]) % n
}

// randomHex 生成 n 字节的随机十六进制字符串
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
