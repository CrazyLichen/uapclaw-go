package processcli

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	lifecycle "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
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
