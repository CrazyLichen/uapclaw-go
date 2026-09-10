package team_workspace

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolTranslator 工具翻译器闭包。
// Python: Translator = Callable[..., str]
//
// 因 team_workspace 包不能导入 tools/locales 包（循环依赖：
// team_workspace → tools/locales → schema → team_workspace），
// 在此定义等价类型，调用者负责将 locales.Translator 转换为此类型。
//
// 调用方式与 locales.Translator 完全一致:
//   - t("workspace_meta")           → 加载 descs/{lang}/workspace_meta.md 作为描述
//   - t("workspace_meta", "action") → 查 STRINGS["workspace_meta.action"]
type ToolTranslator func(tool string, key ...string) string

// WorkspaceMetaTool 工作空间元数据工具。
// Python: WorkspaceMetaTool (team_workspace/tools.py)
//
// 文件 I/O 通过标准 read_file/write_file/glob 工具经 .team/ 挂载点完成。
// 本工具仅处理没有文件系统等价物的锁管理和版本历史查询。
//
// 支持 4 种操作：
//   - lock:   获取文件锁
//   - unlock: 释放文件锁
//   - locks:  列出所有活跃锁
//   - history: 查看文件版本历史
type WorkspaceMetaTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
	// ws 工作空间管理器
	ws *TeamWorkspaceManager
	// memberName 调用者成员名
	memberName string
	// displayName 调用者显示名
	displayName string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// actionLock 获取文件锁操作
	actionLock = "lock"
	// actionUnlock 释放文件锁操作
	actionUnlock = "unlock"
	// actionLocks 列出活跃锁操作
	actionLocks = "locks"
	// actionHistory 查看版本历史操作
	actionHistory = "history"
	// toolIDWorkspaceMeta 工具 ID
	// Python: "team.workspace_meta"
	toolIDWorkspaceMeta = "team.workspace_meta"
	// toolNameWorkspaceMeta 工具名
	toolNameWorkspaceMeta = "workspace_meta"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorkspaceMetaTool 创建 WorkspaceMetaTool 实例。
// Python: WorkspaceMetaTool.__init__(workspace, t)
//
// 参数:
//   - ws: 团队工作空间管理器
//   - t: 翻译器闭包
//   - memberName: 调用者成员名（对齐 Python kwargs.member_name）
//   - displayName: 调用者显示名（对齐 Python kwargs.display_name）
func NewWorkspaceMetaTool(ws *TeamWorkspaceManager, t ToolTranslator, memberName, displayName string) *WorkspaceMetaTool {
	// 构建 InputParams
	// Python: self.card.input_params = {"type": "object", "properties": {...}, "required": ["action"]}
	actionParam := &schema.Param{
		Name:        "action",
		Description: t(toolNameWorkspaceMeta, "action"),
		Type:        schema.ParamTypeString,
		Required:    true,
		Enum:        []any{actionLock, actionUnlock, actionLocks, actionHistory},
	}
	pathParam := schema.NewStringParam("path", t(toolNameWorkspaceMeta, "path"), false)

	card := tool.NewToolCardWithID(
		toolIDWorkspaceMeta,
		toolNameWorkspaceMeta,
		t(toolNameWorkspaceMeta),
		[]*schema.Param{actionParam, pathParam},
		nil,
	)

	return &WorkspaceMetaTool{
		card:        card,
		ws:          ws,
		memberName:  memberName,
		displayName: displayName,
	}
}

// Card 返回工具配置卡片。
func (t *WorkspaceMetaTool) Card() *tool.ToolCard {
	return t.card
}

// Invoke 执行工作空间元数据操作。
// Python: WorkspaceMetaTool.invoke(inputs, **kwargs)
//
// 根据 action 分发到 lock/unlock/locks/history 四个分支。
// 返回 map[string]any 遵循项目约定格式: {"success": bool, "data": ..., "error": ...}
func (t *WorkspaceMetaTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	action, _ := inputs["action"].(string)
	path, _ := inputs["path"].(string)

	switch action {
	case actionLock:
		return t.handleLock(ctx, path), nil
	case actionUnlock:
		return t.handleUnlock(ctx, path), nil
	case actionLocks:
		return t.handleLocks(), nil
	case actionHistory:
		return t.handleHistory(ctx, path), nil
	default:
		logger.Warn(logComponent).
			Str("event_type", "workspace_meta_unknown_action").
			Str("action", action).
			Msg("未知工作空间元数据操作")
		return map[string]any{
			"success": false,
			"error":   fmt.Sprintf("未知操作 '%s'", action),
		}, nil
	}
}

// Stream 不支持流式调用。
func (t *WorkspaceMetaTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.String())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleLock 处理 lock 操作。
// Python: action == "lock"
func (t *WorkspaceMetaTool) handleLock(ctx context.Context, path string) map[string]any {
	if path == "" {
		return map[string]any{
			"success": false,
			"error":   "'path' 是 lock 操作的必填参数",
		}
	}

	acquired, err := t.ws.AcquireLock(ctx, path, t.memberName, t.displayName)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "workspace_meta_lock_error").
			Str("file_path", path).
			Err(err).
			Msg("获取锁失败")
		return map[string]any{
			"success": false,
			"error":   fmt.Sprintf("获取锁失败: %v", err),
		}
	}
	if !acquired {
		lock := t.ws.GetLock(path)
		errMsg := "获取锁失败"
		if lock != nil {
			errMsg = fmt.Sprintf("文件被 %s 锁定", lock.HolderName)
		}
		return map[string]any{
			"success": false,
			"error":   errMsg,
		}
	}

	return map[string]any{
		"success": true,
		"data":    map[string]any{"locked": path},
	}
}

// handleUnlock 处理 unlock 操作。
// Python: action == "unlock"
func (t *WorkspaceMetaTool) handleUnlock(ctx context.Context, path string) map[string]any {
	if path == "" {
		return map[string]any{
			"success": false,
			"error":   "'path' 是 unlock 操作的必填参数",
		}
	}

	released, err := t.ws.ReleaseLock(ctx, path, t.memberName)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "workspace_meta_unlock_error").
			Str("file_path", path).
			Err(err).
			Msg("释放锁失败")
		return map[string]any{
			"success": false,
			"error":   fmt.Sprintf("释放锁失败: %v", err),
		}
	}
	return map[string]any{
		"success": true,
		"data":    map[string]any{"released": released},
	}
}

// handleLocks 处理 locks 操作。
// Python: action == "locks"
func (t *WorkspaceMetaTool) handleLocks() map[string]any {
	locks := t.ws.ListLocks()
	lockMaps := make([]map[string]any, 0, len(locks))
	for _, lock := range locks {
		m, err := lockToMap(lock)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "workspace_meta_locks_serialize_error").
				Str("file_path", lock.FilePath).
				Err(err).
				Msg("锁条目序列化失败")
			continue
		}
		lockMaps = append(lockMaps, m)
	}
	return map[string]any{
		"success": true,
		"data":    map[string]any{"locks": lockMaps},
	}
}

// handleHistory 处理 history 操作。
// Python: action == "history"
func (t *WorkspaceMetaTool) handleHistory(ctx context.Context, path string) map[string]any {
	if path == "" {
		return map[string]any{
			"success": false,
			"error":   "'path' 是 history 操作的必填参数",
		}
	}

	history, err := t.ws.GetHistory(ctx, path, 0) // 0 表示使用 git 默认条数
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "workspace_meta_history_error").
			Str("file_path", path).
			Err(err).
			Msg("获取版本历史失败")
		return map[string]any{
			"success": false,
			"error":   fmt.Sprintf("获取版本历史失败: %v", err),
		}
	}

	historyMaps := make([]map[string]any, 0, len(history))
	for _, entry := range history {
		m, err := historyEntryToMap(entry)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "workspace_meta_history_serialize_error").
				Str("commit", entry.Commit).
				Err(err).
				Msg("历史条目序列化失败")
			continue
		}
		historyMaps = append(historyMaps, m)
	}

	return map[string]any{
		"success": true,
		"data":    map[string]any{"history": historyMaps},
	}
}

// lockToMap 将 WorkspaceFileLock 转为 map[string]any。
// Python: lock.model_dump()
func lockToMap(lock WorkspaceFileLock) (map[string]any, error) {
	jsonBytes, err := json.Marshal(lock)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// historyEntryToMap 将 HistoryEntry 转为 map[string]any。
func historyEntryToMap(entry HistoryEntry) (map[string]any, error) {
	jsonBytes, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		return nil, err
	}
	return result, nil
}
