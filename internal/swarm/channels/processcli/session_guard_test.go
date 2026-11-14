package processcli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	lifecycle "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// isolateSessionDir 为单测隔离会话根目录。
// lifecycle.GetAgentRootDir 以 UAPCLAW_DATA_DIR 推导 agent 数据根目录。
func isolateSessionDir(t *testing.T) {
	t.Helper()
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
}

// bindingTargetPath 算绑定文件路径（与实现同规则，用于测试定位）。
func bindingTargetPath(sessionID string) string {
	digest := sha256.Sum256([]byte(sessionID))
	return filepath.Join(lifecycle.GetAgentSessionsDir(), bindingDirName,
		hex.EncodeToString(digest[:])+".json")
}

func TestSessionLease_冲突返回SESSION_BUSY(t *testing.T) {
	isolateSessionDir(t)
	first := NewSessionLease("sess-1")
	require.NoError(t, first.Acquire())

	second := NewSessionLease("sess-1")
	err := second.Acquire()
	require.Error(t, err)
	var guardErr *SessionGuardError
	assert.ErrorAs(t, err, &guardErr)
	assert.Equal(t, "SESSION_BUSY", guardErr.Code)
	assert.True(t, guardErr.Retryable)

	first.Release()
	assert.NoError(t, second.Acquire())
	second.Release()
}

func TestSessionLease_释放幂等(t *testing.T) {
	isolateSessionDir(t)
	lease := NewSessionLease("sess-2")
	lease.Release() // 未 acquire 直接 release 不 panic
	require.NoError(t, lease.Acquire())
	lease.Release()
	lease.Release()
}

func TestSessionLease_不同会话互不冲突(t *testing.T) {
	isolateSessionDir(t)
	a := NewSessionLease("sess-a")
	b := NewSessionLease("sess-b")
	require.NoError(t, a.Acquire())
	require.NoError(t, b.Acquire())
	a.Release()
	b.Release()
}

func TestBindAgent_新绑定写入(t *testing.T) {
	isolateSessionDir(t)
	require.NoError(t, BindAgent("sess-b1", map[string]any{"name": "coder", "instructions": "x"}, false))
	target := bindingTargetPath("sess-b1")
	require.FileExists(t, target)
	// tmp 不残留
	entries, _ := os.ReadDir(filepath.Dir(target))
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp")
	}
}

func TestBindAgent_重复一致通过(t *testing.T) {
	isolateSessionDir(t)
	def := map[string]any{"name": "coder", "instructions": "x"}
	require.NoError(t, BindAgent("sess-b2", def, false))
	require.NoError(t, BindAgent("sess-b2", def, false)) // 同指纹再绑定放行
}

func TestBindAgent_指纹冲突(t *testing.T) {
	isolateSessionDir(t)
	require.NoError(t, BindAgent("sess-b3", map[string]any{"name": "agent-a", "instructions": "x"}, false))
	err := BindAgent("sess-b3", map[string]any{"name": "agent-b", "instructions": "y"}, false)
	var guardErr *SessionGuardError
	require.ErrorAs(t, err, &guardErr)
	assert.Equal(t, "AGENT_DEFINITION_SESSION_CONFLICT", guardErr.Code)
}

func TestBindAgent_绑定文件损坏(t *testing.T) {
	isolateSessionDir(t)
	target := bindingTargetPath("sess-b4")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte("not-json{"), 0o644))
	err := BindAgent("sess-b4", nil, false)
	var guardErr *SessionGuardError
	require.ErrorAs(t, err, &guardErr)
	assert.Equal(t, "AGENT_BINDING_INVALID", guardErr.Code)
}

func TestBindAgent_续聊已有消息会话拒绝收养(t *testing.T) {
	isolateSessionDir(t)
	// 造一个 metadata message_count=1 的"已有会话"
	dir := sessionpkg.GetSessionsDir()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, sessionpkg.WriteSessionMetadata(dir, "sess-b5",
		map[string]any{"session_id": "sess-b5", "message_count": 1}))
	err := BindAgent("sess-b5", map[string]any{"name": "agent-a", "instructions": "x"}, true)
	var guardErr *SessionGuardError
	require.ErrorAs(t, err, &guardErr)
	assert.Equal(t, "AGENT_DEFINITION_SESSION_CONFLICT", guardErr.Code)

	// message_count=0 可收养
	require.NoError(t, sessionpkg.WriteSessionMetadata(dir, "sess-b6",
		map[string]any{"session_id": "sess-b6", "message_count": 0}))
	require.NoError(t, BindAgent("sess-b6", map[string]any{"name": "agent-a", "instructions": "x"}, true))
}

func TestBindAgent_无定义写入空指纹(t *testing.T) {
	isolateSessionDir(t)
	require.NoError(t, BindAgent("sess-b7", nil, false))
	target := bindingTargetPath("sess-b7")
	raw, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"fingerprint":null`)
}
