package session

import "strings"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// renameTitleMaxLen rename 允许的最大标题长度，对齐 Python _RENAME_TITLE_MAX_LEN
	renameTitleMaxLen = 200
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// ApplySessionRename 实现会话重命名三种语义：查询(title=nil) / 清除(空串) / 设置(非空)。
//
// 对齐 Python apply_session_rename(params, connection_session_id, init_channel_id)
// 返回 (payload, error)；payload 包含 session_id, title, previous_title
func ApplySessionRename(
	target string,
	title *string,
	initChannelID string,
) (map[string]any, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return map[string]any{
			"error": "session_id 不能为空",
			"code":  "BAD_REQUEST",
		}, nil
	}

	metadata := GetSessionMetadata(target)

	// title 为 nil：查询模式
	if title == nil {
		currentTitle := ""
		if metadata != nil {
			if t, ok := metadata["title"].(string); ok {
				currentTitle = t
			}
		}
		return map[string]any{
			"session_id":     target,
			"title":          currentTitle,
			"previous_title": currentTitle,
		}, nil
	}

	// metadata 不存在时初始化
	if metadata == nil {
		InitSessionMetadata(target, initChannelID, "", "", "unknown", "")
		metadata = GetSessionMetadata(target)
	}

	previousTitle := ""
	if metadata != nil {
		if t, ok := metadata["title"].(string); ok {
			previousTitle = t
		}
	}

	// 截断标题
	newTitle := strings.TrimSpace(*title)
	if len(newTitle) > renameTitleMaxLen {
		newTitle = newTitle[:renameTitleMaxLen]
	}

	if newTitle != "" {
		UpdateSessionMetadata(SessionMetadataUpdate{
			SessionID: target,
			Title:     PtrStr(newTitle),
		})
	} else {
		UpdateSessionMetadata(SessionMetadataUpdate{
			SessionID:  target,
			ClearTitle: true,
		})
	}

	updated := GetSessionMetadata(target)
	updatedTitle := ""
	if updated != nil {
		if t, ok := updated["title"].(string); ok {
			updatedTitle = t
		}
	}

	return map[string]any{
		"session_id":     target,
		"title":          updatedTitle,
		"previous_title": previousTitle,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
