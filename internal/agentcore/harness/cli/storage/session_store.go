package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 结构化会话存储（JSON 文件）。
//
// 每个 CLI 会话持久化为 ~/.openjiuwen/sessions/ 下的一个 JSON 文件。
//
// Python: openjiuwen/harness/cli/storage/session_store.py（140 行）

// ──────────────────────────── 结构体 ────────────────────────────

// StoredMessage 会话里的一条消息。
// Python: session_store.py:19-31 StoredMessage
type StoredMessage struct {
	// Role "user" / "assistant" / "system"
	Role string `json:"role"`
	// Content 消息文本
	Content string `json:"content"`
	// Timestamp ISO-8601 时间戳
	Timestamp string `json:"timestamp"`
	// TokenCount 预估 token 数（可选）
	TokenCount *int `json:"token_count,omitempty"`
}

// StoredSession 一个会话的完整记录。
// Python: session_store.py:34-44 StoredSession
type StoredSession struct {
	// SessionID 唯一标识（如 "cli-a1b2c3d4"）
	SessionID string `json:"session_id"`
	// Model 会话使用的模型名
	Model string `json:"model"`
	// CreatedAt ISO-8601 创建时间戳
	CreatedAt string `json:"created_at"`
	// Messages 有序消息列表
	Messages []StoredMessage `json:"messages"`
}

// SessionStore 基于 JSON 文件的会话存储。
// Python: session_store.py:47-58 SessionStore
type SessionStore struct {
	// storeDir 会话 JSON 文件目录
	storeDir string
	// current 当前正在记录的会话（Python: _current）
	current *StoredSession
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionStore 构造存储；storeDir 为空时用 ~/.openjiuwen/sessions/。
// Python: session_store.py:59-68 __init__
//
// 目录不存在时创建（mkdir parents, exist_ok）。
func NewSessionStore(storeDir string) (*SessionStore, error) {
	if storeDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		storeDir = filepath.Join(home, ".openjiuwen", "sessions")
	}
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return nil, err
	}
	return &SessionStore{storeDir: storeDir}, nil
}

// StoreDir 返回存储目录（测试用）。
func (s *SessionStore) StoreDir() string { return s.storeDir }

// NewSession 开始记录一个新会话。
// Python: session_store.py:70-78 new_session
func (s *SessionStore) NewSession(sessionID, model string) {
	s.current = &StoredSession{
		SessionID: sessionID,
		Model:     model,
		CreatedAt: utcNowISO(),
	}
}

// AddMessage 向当前会话追加一条消息并落盘。
// Python: session_store.py:80-91 add_message
//
// 无当前会话时静默返回（上游无报错）。
func (s *SessionStore) AddMessage(role, content string) {
	if s.current == nil {
		return
	}
	s.current.Messages = append(s.current.Messages, StoredMessage{
		Role:      role,
		Content:   content,
		Timestamp: utcNowISO(),
	})
	_ = s.save()
}

// ListSessions 返回全部已持久化会话的摘要。
// Python: session_store.py:93-126 list_sessions
//
// 崩溃写入、旧版布局或手改文件不得中止整个列表：REPL 的 /sessions 命令
// 之上没有兜底处理器，单个损坏文件曾直接把命令炸穿——逐文件跳过坏项。
func (s *SessionStore) ListSessions() []map[string]any {
	entries, err := os.ReadDir(s.storeDir)
	if err != nil {
		return nil
	}
	// Python: sorted(glob("*.json"))——按文件名排序
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	sessions := make([]map[string]any, 0, len(names))
	for _, name := range names {
		content, readErr := os.ReadFile(filepath.Join(s.storeDir, name))
		if readErr != nil {
			continue
		}
		var data map[string]json.RawMessage
		if json.Unmarshal(content, &data) != nil {
			continue
		}
		var sessionID, model, createdAt string
		var messages []any
		if !extractStrings(data, "session_id", &sessionID) ||
			!extractStrings(data, "model", &model) ||
			!extractStrings(data, "created_at", &createdAt) {
			continue
		}
		if raw, exists := data["messages"]; exists {
			if json.Unmarshal(raw, &messages) != nil {
				continue
			}
		}
		sessions = append(sessions, map[string]any{
			"id":         sessionID,
			"model":      model,
			"created_at": createdAt,
			"turns":      len(messages),
		})
	}
	return sessions
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// save 把当前会话写进它的 JSON 文件。
// Python: session_store.py:128-141 _save
//
// 写失败静默（上游 write_text 无 try；Go 显式返回 error 供调用方忽略）。
func (s *SessionStore) save() error {
	if s.current == nil {
		return nil
	}
	encoded, err := json.MarshalIndent(s.current, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.storeDir, s.current.SessionID+".json")
	return os.WriteFile(path, encoded, 0o644)
}

// utcNowISO 当前 UTC 时间的 ISO-8601 表示。
// Python: datetime.now(tz=timezone.utc).isoformat()
func utcNowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000000Z07:00")
}

// extractStrings 从 JSON map 取一个字符串字段（类型不符返回 false）。
// Python: session_store.py:104-108 的 isinstance 链
func extractStrings(data map[string]json.RawMessage, key string, target *string) bool {
	raw, exists := data[key]
	if !exists {
		return false
	}
	return json.Unmarshal(raw, target) == nil
}
