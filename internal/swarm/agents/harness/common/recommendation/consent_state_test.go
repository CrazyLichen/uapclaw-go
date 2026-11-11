package recommendation

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// resetWorkspaceForTest 重定向工作区到临时目录（对照 session 包既有写法）。
// 注意 Cleanup 顺序：t.Setenv 注册的还原先注册、后执行（cleanup 是 LIFO），
// 因此本助手必须在 Setenv 之后注册 path.ResetCache，确保 env 还原后再复位缓存，
// 否则缓存会残留临时目录，污染同包后续读工作区的测试（situation 在飞共享实测踩坑）。
func resetWorkspaceForTest(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	path.ResetCache()
	t.Cleanup(path.ResetCache)
	return tmpDir
}

// TestReadConsentState_无库降级 库不存在返回未记录，不产生文件。
func TestReadConsentState_无库降级(t *testing.T) {
	resetWorkspaceForTest(t)

	state := readConsentState()
	assert.Nil(t, state.historyAfter)
	assert.Nil(t, state.deletedAt)
	_, err := os.Stat(filepath.Join(workspaceDirForTest(), ConsentDBName))
	assert.True(t, os.IsNotExist(err), "探测不得建库")
}

// TestReadConsentState_缺表降级 库存在但无表（首次 opt-in 前的正常态）静默返回未记录。
func TestReadConsentState_缺表降级(t *testing.T) {
	resetWorkspaceForTest(t)
	dbPath := filepath.Join(workspaceDirForTest(), ConsentDBName)
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	state := readConsentState()
	assert.Nil(t, state.historyAfter)
	assert.Nil(t, state.deletedAt)
}

// TestReadConsentState_正常读取 有行时读出两个边界。
func TestReadConsentState_正常读取(t *testing.T) {
	resetWorkspaceForTest(t)
	dbPath := filepath.Join(workspaceDirForTest(), ConsentDBName)
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE recommendation_consent_state (
		id INTEGER PRIMARY KEY CHECK (id = 1), history_after REAL, deleted_at REAL, updated_at REAL NOT NULL)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO recommendation_consent_state VALUES (1, 100.5, 200.25, 300.0)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	state := readConsentState()
	require.NotNil(t, state.historyAfter)
	require.NotNil(t, state.deletedAt)
	assert.Equal(t, 100.5, *state.historyAfter)
	assert.Equal(t, 200.25, *state.deletedAt)
}

// TestReadConsentState_空值行 行存在但两列为 NULL → 两个 nil。
func TestReadConsentState_空值行(t *testing.T) {
	resetWorkspaceForTest(t)
	dbPath := filepath.Join(workspaceDirForTest(), ConsentDBName)
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE recommendation_consent_state (
		id INTEGER PRIMARY KEY CHECK (id = 1), history_after REAL, deleted_at REAL, updated_at REAL NOT NULL)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO recommendation_consent_state VALUES (1, NULL, NULL, 1.0)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	state := readConsentState()
	assert.Nil(t, state.historyAfter)
	assert.Nil(t, state.deletedAt)
}

// workspaceDirForTest 当前（已重定向的）工作区目录。
func workspaceDirForTest() string {
	return workspace.AgentWorkspaceDir()
}
