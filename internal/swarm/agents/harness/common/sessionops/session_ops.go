package sessionops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentmode "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/agent_mode"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentsession "github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	singleagentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ForkSessionParams fork_session 参数
type ForkSessionParams struct {
	// SourceSessionID 源会话标识
	SourceSessionID string
	// TargetSessionID 目标会话标识
	TargetSessionID string
	// Title 分叉标题（可选）
	Title string
	// ChannelID 通道标识
	ChannelID string
}

// ForkSessionResult fork_session 返回值
type ForkSessionResult struct {
	// SessionID 新会话标识
	SessionID string
	// SourceSessionID 源会话标识
	SourceSessionID string
	// Title 最终标题
	Title string
}

// RewindSessionParams rewind_session 参数
type RewindSessionParams struct {
	// SessionID 会话标识
	SessionID string
	// TurnIndex 回退到的用户 turn 索引（从 1 开始）
	TurnIndex int
}

// RewindSessionResult rewind_session 返回值
type RewindSessionResult struct {
	// SessionID 会话标识
	SessionID string
	// TurnIndex 回退到的 turn 索引
	TurnIndex int
	// Content 被移除的 turn 内容
	Content string
	// ContentPreview 内容预览（前 80 字符）
	ContentPreview string
	// RemainingRecords 截断后剩余记录数
	RemainingRecords int
	// RemovedRecords 移除的记录数
	RemovedRecords int
}

// SessionTurnInfo 单个用户 turn 信息
type SessionTurnInfo struct {
	// TurnIndex turn 索引（从 1 开始）
	TurnIndex int
	// ContentPreview 内容预览
	ContentPreview string
	// Timestamp 时间戳
	Timestamp float64
	// ID 消息 ID
	ID string
	// RequestID 请求 ID
	RequestID string
	// Stats diff 统计
	Stats map[string]int
	// Files 变更文件列表
	Files []TurnFileInfo
}

// TurnFileInfo turn 中变更的文件信息
type TurnFileInfo struct {
	// Path 文件路径
	Path string
	// LinesAdded 新增行数
	LinesAdded int
	// LinesRemoved 删除行数
	LinesRemoved int
	// IsNewFile 是否为新建文件
	IsNewFile bool
}

// ListSessionTurnsResult list_session_turns 返回值
type ListSessionTurnsResult struct {
	// Turns turn 列表
	Turns []SessionTurnInfo
	// Total 用户消息总数
	Total int
}

// RestoreSessionFilesResult restore_session_files 返回值
type RestoreSessionFilesResult struct {
	// SessionID 会话标识
	SessionID string
	// TurnIndex 目标 turn 索引
	TurnIndex int
	// RestoredFiles 恢复的文件列表
	RestoredFiles []string
	// DeletedFiles 删除的文件列表
	DeletedFiles []string
	// Errors 恢复错误列表
	Errors []FileRestoreError
}

// FileRestoreError 文件恢复错误
type FileRestoreError struct {
	// File 文件路径
	File string
	// Error 错误消息
	Error string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentChannel
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// nonUserAuthoredTags 非用户 authored 的标签，对齐 Python _NON_USER_AUTHORED_TAGS
	nonUserAuthoredTags = []string{
		"<local-command-stdout>",
		"<local-command-stderr>",
		"<bash-stdout>",
		"<bash-stderr>",
		"<task-notification>",
		"<tick>",
		"<teammate-message",
	}

	// fileContentRe 剥离 <file-content> 块的正则
	fileContentRe = regexp.MustCompile(`(?s)<file-content[^>]*>.*?</file-content>`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ForkSession 分叉会话：复制 history.json + 添加 forked_from 元数据 + 写 metadata。
// Python: fork_session(*, source_session_id, target_session_id, title, channel_id)
func ForkSession(params ForkSessionParams) (*ForkSessionResult, error) {
	sessionsDir := workspace.AgentSessionsDir()
	sourceDir := filepath.Join(sessionsDir, params.SourceSessionID)
	targetDir := filepath.Join(sessionsDir, params.TargetSessionID)

	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("source session not found")
	}
	if _, err := os.Stat(targetDir); !os.IsNotExist(err) {
		return nil, fmt.Errorf("target session already exists")
	}

	// Python: target_dir.mkdir(parents=True, exist_ok=True)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建目标目录失败: %w", err)
	}

	// Python: shutil.copy2(source_history, target_history)
	sourceHistory := filepath.Join(sourceDir, "history.json")
	targetHistory := filepath.Join(targetDir, "history.json")

	if _, err := os.Stat(sourceHistory); err == nil {
		// 复制 history.json
		if err := copyFile(sourceHistory, targetHistory); err != nil {
			logger.Warn(logComponent).Err(err).Msg("fork: 复制 history.json 失败")
		} else {
			// Python: 添加 forked_from 元数据到每条记录
			data, err := os.ReadFile(targetHistory)
			if err == nil {
				var records []map[string]any
				if err := json.Unmarshal(data, &records); err == nil {
					for _, record := range records {
						record["forked_from"] = map[string]any{
							"session_id":  params.SourceSessionID,
							"original_id": record["id"],
						}
					}
					// Python: json.dumps(data, ensure_ascii=False, indent=2)
					if updated, err := json.MarshalIndent(records, "", "  "); err == nil {
						os.WriteFile(targetHistory, updated, 0o644)
					}
				}
			}
		}
	}

	// Python: source_meta = get_session_metadata(source_session_id)
	sourceMeta := session.GetSessionMetadata(params.SourceSessionID)

	// Python: 决定 base_name
	baseName := ""
	if params.Title != "" {
		baseName = params.Title
	} else if title, _ := sourceMeta["title"].(string); title != "" {
		baseName = title
	}
	// Python: 不从 first prompt 推导——"(Branch)" alone is cleaner for status bar

	// Python: 获取现有标题集合
	existingTitles := map[string]bool{}
	allSessions, _ := session.GetAllSessionsMetadata(500, 0)
	for _, s := range allSessions {
		if t, ok := s["title"].(string); ok && t != "" {
			existingTitles[t] = true
		}
	}

	finalTitle := getUniqueForkName(baseName, existingTitles)
	sourceMode, _ := sourceMeta["mode"].(string)

	// Python: 写入目标会话元数据
	metadata := map[string]any{
		"session_id":      params.TargetSessionID,
		"channel_id":      params.ChannelID,
		"user_id":         sourceMeta["user_id"],
		"created_at":      session.CurrentTimestamp(),
		"last_message_at": sourceMeta["last_message_at"],
		"title":           finalTitle,
		"message_count":   sourceMeta["message_count"],
		"mode":            sourceMode,
		"forked_from":     params.SourceSessionID,
	}
	session.EnqueueMetadataWrite(params.TargetSessionID, metadata)

	return &ForkSessionResult{
		SessionID:       params.TargetSessionID,
		SourceSessionID: params.SourceSessionID,
		Title:           finalTitle,
	}, nil
}

// RewindSession 回退会话：截断 history.json 到指定 turn + 更新 message_count。
// Python: rewind_session(*, session_id, turn_index)
func RewindSession(params RewindSessionParams) (*RewindSessionResult, error) {
	if params.TurnIndex < 1 {
		return nil, fmt.Errorf("turn_index must be >= 1")
	}

	sessionsDir := workspace.AgentSessionsDir()
	historyPath := filepath.Join(sessionsDir, params.SessionID, "history.json")
	if _, err := os.Stat(historyPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("session history not found")
	}

	// Python: 读取 history 定位用户 turn
	data, err := os.ReadFile(historyPath)
	if err != nil {
		return nil, fmt.Errorf("读取 history.json 失败: %w", err)
	}
	var history []map[string]any
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("invalid history format")
	}

	userPositions := []int{}
	for i, record := range history {
		if role, _ := record["role"].(string); role == "user" {
			userPositions = append(userPositions, i)
		}
	}

	totalTurns := len(userPositions)
	if totalTurns == 0 {
		return nil, fmt.Errorf("no user messages in session")
	}
	if params.TurnIndex > totalTurns {
		return nil, fmt.Errorf("turn_index %d exceeds total turns (%d)", params.TurnIndex, totalTurns)
	}

	// Python: cut_index = target_user_index
	cutIndex := userPositions[params.TurnIndex-1]

	// Python: 提取被移除的 turn 内容
	removedTurnContent := ""
	if cutIndex < len(history) {
		content := history[cutIndex]["content"]
		raw := ""
		switch v := content.(type) {
		case string:
			raw = v
		default:
			raw = fmt.Sprintf("%v", v)
		}
		// Python: 剥离 <file-content> 块
		removedTurnContent = fileContentRe.ReplaceAllString(raw, "")
		removedTurnContent = strings.TrimSpace(removedTurnContent)
	}

	// Python: result = truncate_history_records(session_id=session_id, cut_index=cut_index)
	result, err := session.TruncateHistoryRecords(params.SessionID, cutIndex)
	if err != nil {
		return nil, fmt.Errorf("截断 history 失败: %w", err)
	}

	// Python: update_session_metadata(session_id=session_id, set_message_count=result["remaining_records"])
	// 勘误 E3: 使用 SessionMetadataUpdate 结构体
	remaining := result.RemainingRecords
	session.UpdateSessionMetadata(session.SessionMetadataUpdate{
		SessionID:       params.SessionID,
		SetMessageCount: &remaining,
	})

	contentPreview := ""
	if len(removedTurnContent) > 80 {
		contentPreview = removedTurnContent[:80]
	} else {
		contentPreview = removedTurnContent
	}

	return &RewindSessionResult{
		SessionID:        params.SessionID,
		TurnIndex:        params.TurnIndex,
		Content:          removedTurnContent,
		ContentPreview:   contentPreview,
		RemainingRecords: result.RemainingRecords,
		RemovedRecords:   result.RemovedRecords,
	}, nil
}

// ListSessionTurns 列出会话中所有用户 turn（含 diff 统计）。
// Python: list_session_turns(*, session_id)
func ListSessionTurns(sessionID string) *ListSessionTurnsResult {
	sessionsDir := workspace.AgentSessionsDir()
	historyPath := filepath.Join(sessionsDir, sessionID, "history.json")

	if _, err := os.Stat(historyPath); os.IsNotExist(err) {
		return &ListSessionTurnsResult{Turns: nil, Total: 0}
	}

	data, err := os.ReadFile(historyPath)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("list_session_turns: 读取 history.json 失败")
		return &ListSessionTurnsResult{Turns: nil, Total: 0}
	}

	var history []map[string]any
	if err := json.Unmarshal(data, &history); err != nil {
		return &ListSessionTurnsResult{Turns: nil, Total: 0}
	}

	// Python: 获取 diff 统计
	diffStatsMap := map[int]map[string]int{}
	diffFilesMap := map[int][]TurnFileInfo{}
	ds := utils.GetDiffService()
	// 勘误 E2: 使用 DiffService.GetProjectDirFromMetadata
	projectDir := ds.GetProjectDirFromMetadata(sessionID)
	turnDiffs := ds.GetTurnDiffs(sessionID, projectDir)
	for _, td := range turnDiffs {
		ti := td.TurnIndex
		if ti > 0 {
			diffStatsMap[ti] = map[string]int{
				"filesChanged":  td.Stats.FilesChanged,
				"linesAdded":    td.Stats.LinesAdded,
				"linesRemoved": td.Stats.LinesRemoved,
			}
			// 勘误 E6: TurnDiff.Files 是 map[string]*FileDiff，不是切片
			for fp, f := range td.Files {
				diffFilesMap[ti] = append(diffFilesMap[ti], TurnFileInfo{
					Path:         fp,
					LinesAdded:   f.LinesAdded,
					LinesRemoved: f.LinesRemoved,
					IsNewFile:    f.IsNewFile,
				})
			}
		}
	}

	// Python: 遍历 history，过滤用户 turn
	turns := []SessionTurnInfo{}
	userCount := 0
	for _, record := range history {
		role, _ := record["role"].(string)
		if role != "user" {
			continue
		}
		userCount++

		content, _ := record["content"].(string)
		// Python: if isinstance(content, str) and not _is_selectable_user_message(content): continue
		if !isSelectableUserMessage(content) {
			continue
		}

		// Python: 剥离 <file-content> 块
		cleaned := fileContentRe.ReplaceAllString(content, "")
		preview := strings.TrimSpace(cleaned)
		if len(preview) > 80 {
			preview = preview[:80]
		}

		stats := diffStatsMap[userCount]
		if stats == nil {
			stats = map[string]int{"filesChanged": 0, "linesAdded": 0, "linesRemoved": 0}
		}

		id, _ := record["id"].(string)
		requestID, _ := record["request_id"].(string)
		timestamp, _ := record["timestamp"].(float64)

		turns = append(turns, SessionTurnInfo{
			TurnIndex:      userCount,
			ContentPreview: preview,
			Timestamp:      timestamp,
			ID:             id,
			RequestID:      requestID,
			Stats:          stats,
			Files:          diffFilesMap[userCount],
		})
	}

	return &ListSessionTurnsResult{Turns: turns, Total: userCount}
}

// RestoreSessionFiles 恢复文件到指定 turn 的状态。
// Python: restore_session_files(*, session_id, turn_index)
func RestoreSessionFiles(sessionID string, turnIndex int) *RestoreSessionFilesResult {
	ds := utils.GetDiffService()
	// 勘误 E2: 使用 DiffService.GetProjectDirFromMetadata
	projectDir := ds.GetProjectDirFromMetadata(sessionID)
	filesToRestore := ds.GetFilesToRestore(sessionID, turnIndex, projectDir)

	result := &RestoreSessionFilesResult{
		SessionID:     sessionID,
		TurnIndex:     turnIndex,
		RestoredFiles: []string{},
		DeletedFiles:  []string{},
		Errors:        []FileRestoreError{},
	}

	if len(filesToRestore) == 0 {
		return result
	}

	// Python: for file_path, info in files_to_restore.items():
	for filePath, info := range filesToRestore {
		switch info.Action {
		case "write":
			// 勘误 E5: FileRestoreInfo.RestoreContent 是 *string，不是 string
			if info.RestoreContent != nil {
				dir := filepath.Dir(filePath)
				os.MkdirAll(dir, 0o755)
				if err := os.WriteFile(filePath, []byte(*info.RestoreContent), 0o644); err != nil {
					result.Errors = append(result.Errors, FileRestoreError{File: filePath, Error: err.Error()})
					logger.Warn(logComponent).Str("file", filePath).Err(err).Msg("restore_session_files: 写回文件失败")
				} else {
					result.RestoredFiles = append(result.RestoredFiles, filePath)
				}
			}
		case "delete":
			// 文件由 agent 在目标 turn 后创建，删除
			if _, err := os.Stat(filePath); err == nil {
				if err := os.Remove(filePath); err != nil {
					result.Errors = append(result.Errors, FileRestoreError{File: filePath, Error: err.Error()})
					logger.Warn(logComponent).Str("file", filePath).Err(err).Msg("restore_session_files: 删除文件失败")
				} else {
					result.DeletedFiles = append(result.DeletedFiles, filePath)
				}
			}
		}
	}

	logger.Info(logComponent).
		Str("session_id", sessionID).
		Int("turn_index", turnIndex).
		Int("restored", len(result.RestoredFiles)).
		Int("deleted", len(result.DeletedFiles)).
		Int("errors", len(result.Errors)).
		Msg("restore_session_files 完成")

	return result
}

// RewindSessionContext 重建 context_engine：加载截断后的 history → 清空旧上下文 → 构建新上下文 → 持久化。
// Python: rewind_session_context(*, deep_agent, session_id, turn_index)
func RewindSessionContext(ctx context.Context, deepAgent *harness.DeepAgent, sessionID string, turnIndex int) bool {
	if deepAgent == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("rewind_session_context: 无 deep_agent")
		return false
	}
	reactAgent := deepAgent.ReactAgent()
	if reactAgent == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("rewind_session_context: 无 react_agent")
		return false
	}

	// --- 1. 读取截断后的 history.json ---
	sessionsDir := workspace.AgentSessionsDir()
	historyPath := filepath.Join(sessionsDir, sessionID, "history.json")
	if _, err := os.Stat(historyPath); os.IsNotExist(err) {
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("rewind_session_context: history.json 不存在")
		return false
	}

	data, err := os.ReadFile(historyPath)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("rewind_session_context: 读取 history.json 失败")
		return false
	}

	var historyRecords []map[string]any
	if err := json.Unmarshal(data, &historyRecords); err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("rewind_session_context: 解析 history.json 失败")
		return false
	}

	if len(historyRecords) == 0 {
		logger.Info(logComponent).Str("session_id", sessionID).Msg("rewind_session_context: 空历史记录")
		return true
	}

	// --- 2. 转换 history.json 记录为 BaseMessage 列表 ---
	// Python: context_messages = []
	contextMessages := []llmschema.BaseMessage{}
	for _, record := range historyRecords {
		role := strings.TrimSpace(strings.ToLower(fmt.Sprintf("%v", record["role"])))
		content := record["content"]

		// Python: 处理 content 为 list 的情况
		contentStr := ""
		switch v := content.(type) {
		case string:
			contentStr = v
		case []any:
			parts := []string{}
			for _, p := range v {
				if s, ok := p.(string); ok {
					parts = append(parts, s)
				} else if m, ok := p.(map[string]any); ok {
					if t, _ := m["type"].(string); t == "text" {
						if text, _ := m["text"].(string); text != "" {
							parts = append(parts, text)
						}
					}
				}
			}
			contentStr = strings.Join(parts, " ")
		default:
			contentStr = fmt.Sprintf("%v", v)
		}

		if strings.TrimSpace(contentStr) == "" {
			continue
		}

		// Python: Only convert user / assistant / tool roles; skip system messages
		switch role {
		case "user":
			contextMessages = append(contextMessages, llmschema.NewUserMessage(contentStr))
		case "assistant":
			contextMessages = append(contextMessages, llmschema.NewAssistantMessage(contentStr))
		case "tool":
			toolCallID, _ := record["tool_call_id"].(string)
			if toolCallID == "" {
				toolCallID = "rewind-restored"
			}
			contextMessages = append(contextMessages, llmschema.NewToolMessage(toolCallID, contentStr))
		// system 及其他 role 跳过——由 agent 在下次 turn 重新生成
		default:
			// 跳过
		}
	}

	if len(contextMessages) == 0 {
		logger.Info(logComponent).Str("session_id", sessionID).Msg("rewind_session_context: 无可转换消息")
		return true
	}

	// --- 3. 清空旧上下文 ---
	// Python: context_engine = react_agent.context_engine
	contextEngine := reactAgent.ContextEngine()
	// Python: context = context_engine.get_context(session_id=session_id)
	modelCtx := contextEngine.GetContext("default_context_id", sessionID)
	if modelCtx != nil {
		logger.Info(logComponent).
			Str("session_id", sessionID).
			Int("messages", modelCtx.Len()).
			Msg("rewind_session_context: 清空旧上下文")
	}
	// 勘误 E1: WithSessionID 而非 WithClearSessionID
	contextEngine.ClearContext(ctx, ceinterface.WithSessionID(sessionID))

	// --- 4. 构建新上下文 ---
	// Python: session = create_agent_session(session_id=session_id, card=deep_agent.card)
	card := deepAgent.Card()
	sess := agentsession.CreateAgentSession(sessionID, card, nil)
	if err := sess.PreRun(ctx); err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("rewind_session_context: PreRun 失败")
		return false
	}

	// Python: Wipe stale context / deep_agent_state in the checkpointer
	sess.UpdateState(map[string]any{"context": nil})
	sess.UpdateState(map[string]any{hschema.SessionStateKey: nil})

	// Python: await context_engine.create_context(session=session, history_messages=context_messages)
	_, err = contextEngine.CreateContext(ctx, "default_context_id", sess,
		ceinterface.WithHistoryMessages(contextMessages))
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("rewind_session_context: CreateContext 失败")
		return false
	}

	// --- 5. 持久化到 checkpointer ---
	persistOK := false
	// Python: await context_engine.save_contexts(session)
	saved, err := contextEngine.SaveContexts(ctx, sess, []string{"default_context_id"})
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("rewind_session_context: SaveContexts 失败")
	} else {
		// Python: deep_agent.save_state(session)
		deepAgent.SaveState(sess, deepAgent.LoadState(sess))
		// Python: await session.post_run()
		if postErr := sess.PostRun(ctx); postErr != nil {
			logger.Warn(logComponent).Err(postErr).Str("session_id", sessionID).Msg("rewind_session_context: PostRun 失败")
		}
		persistOK = saved != nil
	}

	logger.Info(logComponent).
		Str("session_id", sessionID).
		Int("turn_index", turnIndex).
		Int("messages", len(contextMessages)).
		Bool("persist", persistOK).
		Msg("rewind_session_context: 上下文重建完成")

	return true
}

// CopySessionState 复制 DeepAgentState：读源 state → 深拷贝变换 → 写目标 checkpointer。
// Python: copy_session_state(source_session_id, target_session_id, card, deep_agent)
func CopySessionState(ctx context.Context, sourceSessionID, targetSessionID string, card *singleagentschema.AgentCard, deepAgent *harness.DeepAgent) bool {
	// Python: 如果有 deep_agent，先刷源状态到 checkpointer
	if deepAgent != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Debug(logComponent).Any("panic", r).Msg("copy_session_state: flushSourceState panic")
				}
			}()
			flushSourceState(deepAgent, sourceSessionID)
		}()
	}

	// Python: 读源 state
	sourceSess := agentsession.CreateAgentSession(sourceSessionID, card, nil)
	if err := sourceSess.PreRun(ctx); err != nil {
		logger.Warn(logComponent).Err(err).Str("source", sourceSessionID).Msg("copy_session_state: 源 session PreRun 失败")
		return false
	}

	// 勘误 E7: GetState 返回 (any, error)，需要处理 error
	sourceStateRaw, stateErr := sourceSess.GetState(state.StringKey(agentschema.SessionStateKey))
	if stateErr != nil {
		logger.Warn(logComponent).Err(stateErr).Str("source", sourceSessionID).Msg("copy_session_state: 读取源 state 失败")
		sourceSess.PostRun(ctx)
		return false
	}

	// Python: finally: source_session.post_run()
	if postErr := sourceSess.PostRun(ctx); postErr != nil {
		logger.Warn(logComponent).Err(postErr).Str("source", sourceSessionID).Msg("copy_session_state: 源 session PostRun 失败")
	}

	if sourceStateRaw == nil {
		logger.Info(logComponent).Str("source", sourceSessionID).Msg("copy_session_state: 源无 DeepAgentState，跳过")
		return false
	}

	// Python: 深拷贝并变换
	sourceStateDict, ok := sourceStateRaw.(map[string]any)
	if !ok {
		logger.Warn(logComponent).Str("source", sourceSessionID).Msg("copy_session_state: state 类型非 map")
		return false
	}

	modifiedState := deepCopyMap(sourceStateDict)
	modifiedState["iteration"] = 0
	modifiedState["stop_condition_state"] = nil
	modifiedState["pending_follow_ups"] = []any{}

	// Python: 生成新 plan slug + 复制 plan 文件
	planMode, _ := modifiedState["plan_mode"].(map[string]any)
	if planMode != nil {
		oldSlug, _ := planMode["plan_slug"].(string)
		if oldSlug != "" {
			workspaceRoot := workspace.AgentWorkspaceDir()
			newSlug := agentmode.GetOrCreatePlanSlug(workspaceRoot)
			oldPlanPath := agentmode.ResolvePlanFilePath(workspaceRoot, oldSlug)
			newPlanPath := agentmode.ResolvePlanFilePath(workspaceRoot, newSlug)
			if _, err := os.Stat(oldPlanPath); err == nil {
				if err := copyFile(oldPlanPath, newPlanPath); err == nil {
					planMode["plan_slug"] = newSlug
					modifiedState["plan_mode"] = planMode
					logger.Info(logComponent).
						Str("old_slug", oldSlug).
						Str("new_slug", newSlug).
						Msg("copy_session_state: plan 文件已复制")
				}
			}
		}
	}

	// Python: 写目标 state
	targetSess := agentsession.CreateAgentSession(targetSessionID, card, nil)
	if err := targetSess.PreRun(ctx); err != nil {
		logger.Warn(logComponent).Err(err).Str("target", targetSessionID).Msg("copy_session_state: 目标 session PreRun 失败")
		return false
	}
	targetSess.UpdateState(map[string]any{agentschema.SessionStateKey: modifiedState})
	if postErr := targetSess.PostRun(ctx); postErr != nil {
		logger.Warn(logComponent).Err(postErr).Str("target", targetSessionID).Msg("copy_session_state: 目标 session PostRun 失败")
	}

	logger.Info(logComponent).
		Str("source", sourceSessionID).
		Str("target", targetSessionID).
		Msg("copy_session_state: DeepAgentState 已复制")

	return true
}

// CopySessionContext 复制内存上下文：读源 context → 在目标 session 创建相同 history 的 context。
// Python: copy_session_context(deep_agent, source_session_id, target_session_id)
func CopySessionContext(ctx context.Context, deepAgent *harness.DeepAgent, sourceSessionID, targetSessionID string) bool {
	if deepAgent == nil {
		logger.Warn(logComponent).Str("source", sourceSessionID).Msg("copy_session_context: deepAgent 为 nil")
		return false
	}
	// Python: messages = deep_agent.get_current_context(session_id=source_session_id)
	messages, err := deepAgent.GetCurrentContext(ctx, sourceSessionID, "default_context_id")
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("source", sourceSessionID).Msg("copy_session_context: 读取源上下文失败")
		return false
	}

	if len(messages) == 0 {
		logger.Info(logComponent).Str("source", sourceSessionID).Msg("copy_session_context: 源无消息，跳过")
		return false
	}

	// Python: await deep_agent.create_new_context_engine(session_id=target_session_id, messages=messages)
	_, err = deepAgent.CreateNewContextEngine(ctx, targetSessionID, messages)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("target", targetSessionID).Msg("copy_session_context: 创建目标上下文失败")
		return false
	}

	logger.Info(logComponent).
		Int("messages", len(messages)).
		Str("source", sourceSessionID).
		Str("target", targetSessionID).
		Msg("copy_session_context: 上下文已复制")

	return true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getUniqueForkName 生成不重复的分叉标题。
// Python: _get_unique_fork_name(base_name, existing_titles)
func getUniqueForkName(baseName string, existingTitles map[string]bool) string {
	// Python: candidate = f"{base_name} (Branch)" if base_name else "(Branch)"
	candidate := ""
	if baseName != "" {
		candidate = baseName + " (Branch)"
	} else {
		candidate = "(Branch)"
	}
	if !existingTitles[candidate] {
		return candidate
	}

	// Python: pattern = re.compile(...)
	var pattern *regexp.Regexp
	if baseName != "" {
		pattern = regexp.MustCompile(`^` + regexp.QuoteMeta(baseName) + ` \(Branch(?: (\d+))?\)$`)
	} else {
		pattern = regexp.MustCompile(`^\(Branch(?: (\d+))?\)$`)
	}

	// Python: used_numbers: set[int] = {1}
	usedNumbers := map[int]bool{1: true}
	for title := range existingTitles {
		m := pattern.FindStringSubmatch(title)
		if m != nil {
			if m[1] != "" {
				if num, err := fmt.Sscanf(m[1], "%d", new(int)); err == nil && num == 1 {
					var n int
					fmt.Sscanf(m[1], "%d", &n)
					usedNumbers[n] = true
				}
			} else {
				usedNumbers[1] = true
			}
		}
	}

	// Python: next_number = 2; while next_number in used_numbers: next_number += 1
	nextNumber := 2
	for usedNumbers[nextNumber] {
		nextNumber++
	}

	if baseName != "" {
		return fmt.Sprintf("%s (Branch %d)", baseName, nextNumber)
	}
	return fmt.Sprintf("(Branch %d)", nextNumber)
}

// isSelectableUserMessage 判断消息是否为用户可选择的 turn（排除系统输出标签）。
// Python: _is_selectable_user_message(content)
func isSelectableUserMessage(content string) bool {
	for _, tag := range nonUserAuthoredTags {
		if strings.Contains(content, tag) {
			return false
		}
	}
	return true
}

// copyFile 复制文件（对齐 Python shutil.copy2）
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	// Python: shutil.copy2 — 复制权限
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, srcInfo.Mode())
}

// deepCopyMap 深拷贝 map[string]any（通过 JSON 序列化/反序列化）
func deepCopyMap(m map[string]any) map[string]any {
	data, _ := json.Marshal(m)
	var result map[string]any
	json.Unmarshal(data, &result)
	return result
}

// flushSourceState 将运行时状态刷入 Checkpointer。
// Python: _flush_source_state(deep_agent, session_id)
func flushSourceState(deepAgent *harness.DeepAgent, sessionID string) {
	reactAgent := deepAgent.ReactAgent()
	if reactAgent == nil {
		return
	}
	modelCtx := reactAgent.ContextEngine().GetContext("default_context_id", sessionID)
	if modelCtx == nil {
		return
	}
	// Python: session_obj = getattr(ctx, "session", None)
	sessRef := modelCtx.GetSessionRef()
	if sessRef != nil {
		deepAgent.SaveState(sessRef, deepAgent.LoadState(sessRef))
	}
}

// 确保 sessioninterfaces 被引用（用于 GetSessionRef 返回类型）
var _ sessioninterfaces.SessionFacade = nil
