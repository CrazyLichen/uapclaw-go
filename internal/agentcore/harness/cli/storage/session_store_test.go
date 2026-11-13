package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件对齐 tests/unit_tests/harness_cli/test_session_store.py（若存在）；
// 上游用例覆盖：新建/追加/落盘往返/损坏文件跳过。

func newTestStore(t *testing.T) *SessionStore {
	t.Helper()
	store, err := NewSessionStore(t.TempDir())
	require.NoError(t, err)
	return store
}

// TestSessionStore_默认目录 未传目录时用 ~/.openjiuwen/sessions。
func TestSessionStore_默认目录(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	store, err := NewSessionStore("")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".openjiuwen", "sessions"), store.StoreDir())
}

// TestSessionStore_新建与落盘往返 完整链路。
func TestSessionStore_新建与落盘往返(t *testing.T) {
	store := newTestStore(t)
	store.NewSession("cli-a1b2", "gpt-x")
	store.AddMessage("user", "你好")
	store.AddMessage("assistant", "好的")

	content, err := os.ReadFile(filepath.Join(store.StoreDir(), "cli-a1b2.json"))
	require.NoError(t, err)
	assert.Contains(t, string(content), `"session_id": "cli-a1b2"`)
	assert.Contains(t, string(content), `"model": "gpt-x"`)
	assert.Contains(t, string(content), `"role": "user"`)
	assert.Contains(t, string(content), `"role": "assistant"`)
}

// TestSessionStore_AddMessage无当前会话静默 对齐 :82-84。
func TestSessionStore_AddMessage无当前会话静默(t *testing.T) {
	store := newTestStore(t)
	store.AddMessage("user", "孤儿消息") // 不 panic 即通过
	assert.Empty(t, store.ListSessions())
}

// TestSessionStore_ListSessions摘要 对齐 :93-126。
func TestSessionStore_ListSessions摘要(t *testing.T) {
	store := newTestStore(t)
	store.NewSession("cli-1", "m1")
	store.AddMessage("user", "a")
	store.AddMessage("assistant", "b")
	store.NewSession("cli-2", "m2")
	store.AddMessage("user", "c")

	sessions := store.ListSessions()
	require.Len(t, sessions, 2)
	// 按文件名排序：cli-1 在前
	assert.Equal(t, "cli-1", sessions[0]["id"])
	assert.Equal(t, "m1", sessions[0]["model"])
	assert.Equal(t, 2, sessions[0]["turns"])
	assert.Equal(t, "cli-2", sessions[1]["id"])
	assert.Equal(t, 1, sessions[1]["turns"])
}

// TestSessionStore_损坏文件跳过 坏 JSON、缺字段、类型不符逐一跳过。
func TestSessionStore_损坏文件跳过(t *testing.T) {
	store := newTestStore(t)
	dir := store.StoreDir()

	// 好会话
	store.NewSession("good", "m")
	store.AddMessage("user", "x")

	// 坏 JSON
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.json"),
		[]byte("{not json"), 0o644))
	// 缺 session_id
	require.NoError(t, os.WriteFile(filepath.Join(dir, "missing.json"),
		[]byte(`{"model":"m","created_at":"t"}`), 0o644))
	// session_id 类型不符
	require.NoError(t, os.WriteFile(filepath.Join(dir, "badtype.json"),
		[]byte(`{"session_id":42,"model":"m","created_at":"t"}`), 0o644))
	// 顶层非对象
	require.NoError(t, os.WriteFile(filepath.Join(dir, "array.json"),
		[]byte(`[1,2]`), 0o644))

	sessions := store.ListSessions()
	require.Len(t, sessions, 1)
	assert.Equal(t, "good", sessions[0]["id"])
}

// TestSessionStore_无messages字段 messages 缺省时 turns=0。
func TestSessionStore_无messages字段(t *testing.T) {
	store := newTestStore(t)
	require.NoError(t, os.WriteFile(filepath.Join(store.StoreDir(), "empty.json"),
		[]byte(`{"session_id":"e","model":"m","created_at":"t"}`), 0o644))

	sessions := store.ListSessions()
	require.Len(t, sessions, 1)
	assert.Equal(t, 0, sessions[0]["turns"])
}

// TestSessionStore_非json文件忽略 目录里的非 .json 文件不进列表。
func TestSessionStore_非json文件忽略(t *testing.T) {
	store := newTestStore(t)
	require.NoError(t, os.WriteFile(filepath.Join(store.StoreDir(), "readme.txt"),
		[]byte("hello"), 0o644))
	assert.Empty(t, store.ListSessions())
}
