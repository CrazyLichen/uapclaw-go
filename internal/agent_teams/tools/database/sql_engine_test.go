package database

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/sessionctx"
)

// newTestSqlDB 创建测试用 SQLite 内存数据库。
func newTestSqlDB(t *testing.T) *SqlTeamDatabase {
	t.Helper()
	config := DatabaseConfig{DBType: DatabaseTypeSQLite, ConnectionString: ":memory:"}
	db := NewSqlTeamDatabase(config)
	ctx := newTestCtx("test-session-001")
	require.NoError(t, db.Initialize(ctx))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newTestCtx 创建带 session ID 的 context。
// 通过 sessionctx 包直接注入 SessionState，无需全局变量覆盖。
func newTestCtx(sessionID string) context.Context {
	state := sessionctx.InitSessionState()
	state.SetSessionID(sessionID)
	return sessionctx.WithSessionState(context.Background(), state)
}

// ──────────────────────────── SqlTeamDatabase 测试 ────────────────────────────

func TestSqlTeamDatabase_Initialize(t *testing.T) {
	db := newTestSqlDB(t)
	assert.True(t, db.initialized)
}

func TestSqlTeamDatabase_CreateCurSessionTables(t *testing.T) {
	db := newTestSqlDB(t)
	ctx := newTestCtx("session-ddl-test")

	err := db.CreateCurSessionTables(ctx)
	assert.NoError(t, err)

	suffix := SanitizeSessionIDForTable("session-ddl-test")
	assert.True(t, db.db.Migrator().HasTable("team_task_"+suffix))
	assert.True(t, db.db.Migrator().HasTable("team_task_dependency_"+suffix))
	assert.True(t, db.db.Migrator().HasTable("team_message_"+suffix))
	assert.True(t, db.db.Migrator().HasTable("message_read_status_"+suffix))
}

func TestSqlTeamDatabase_DropCurSessionTables(t *testing.T) {
	db := newTestSqlDB(t)
	ctx := newTestCtx("session-drop-test")

	_ = db.CreateCurSessionTables(ctx)
	err := db.DropCurSessionTables(ctx)
	assert.NoError(t, err)

	suffix := SanitizeSessionIDForTable("session-drop-test")
	assert.False(t, db.db.Migrator().HasTable("team_task_"+suffix))
}

func TestSqlTeamDatabase_CleanupAllRuntimeState(t *testing.T) {
	db := newTestSqlDB(t)
	ctx := newTestCtx("cleanup-test")
	_ = db.CreateCurSessionTables(ctx)

	// 向静态表插入数据，验证 DELETE FROM 能清理
	db.Team().CreateTeam(context.Background(), "t1", "T1", "l1", "", "")
	db.Member().CreateMember(context.Background(), "m1", "t1", "M1", "{}", "ready", "teammate", "", "", "build_mode", "", "")

	dropped, cleared, err := db.CleanupAllRuntimeState(ctx)
	assert.NoError(t, err)
	assert.True(t, len(dropped) > 0, "应删除动态表")
	assert.True(t, len(cleared) > 0, "应清空静态表")
}

func TestSqlTeamDatabase_DropSessionTablesByID(t *testing.T) {
	db := newTestSqlDB(t)
	ctx := newTestCtx("by-id-test")
	_ = db.CreateCurSessionTables(ctx)

	dropped, err := db.DropSessionTablesByID(ctx, "by-id-test")
	assert.NoError(t, err)
	assert.True(t, len(dropped) > 0)
}

func TestSqlTeamDatabase_Close(t *testing.T) {
	config := DatabaseConfig{DBType: DatabaseTypeSQLite, ConnectionString: ":memory:"}
	db := NewSqlTeamDatabase(config)
	ctx := newTestCtx("close-test")
	_ = db.Initialize(ctx)

	err := db.Close()
	assert.NoError(t, err)
	assert.False(t, db.initialized)
	assert.Nil(t, db.db)
}

// ──────────────────────────── NewTeamDatabase 工厂测试 ────────────────────────────

func TestNewTeamDatabase_Memory(t *testing.T) {
	ctx := context.Background()
	db := NewTeamDatabase(ctx, NewMemoryDatabaseConfig())
	_, ok := db.(*InMemoryTeamDatabase)
	assert.True(t, ok, "应返回 InMemoryTeamDatabase")
}

func TestNewTeamDatabase_SQL(t *testing.T) {
	ctx := newTestCtx("factory-test")
	db := NewTeamDatabase(ctx, NewDatabaseConfig())
	_, ok := db.(*SqlTeamDatabase)
	assert.True(t, ok, "应返回 SqlTeamDatabase")
	_ = db.Close()
}

// ──────────────────────────── 数据库变体测试 ────────────────────────────

func TestNewSqlTeamDatabase_文件SQLite(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	config := DatabaseConfig{DBType: DatabaseTypeSQLite, ConnectionString: dbPath, DBEnableWAL: true}
	db := NewSqlTeamDatabase(config)
	ctx := newTestCtx("file-sqlite-test")
	require.NoError(t, db.Initialize(ctx))
	t.Cleanup(func() { _ = db.Close() })
	assert.True(t, db.initialized)
}

func TestNewSqlTeamDatabase_PostgreSQL_缺连接串(t *testing.T) {
	config := DatabaseConfig{DBType: DatabaseTypePostgreSQL, ConnectionString: ""}
	db := NewSqlTeamDatabase(config)
	ctx := newTestCtx("pg-test")
	err := db.Initialize(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "PostgreSQL")
}

func TestNewSqlTeamDatabase_MySQL_不支持(t *testing.T) {
	config := DatabaseConfig{DBType: DatabaseTypeMySQL, ConnectionString: "root@/test"}
	db := NewSqlTeamDatabase(config)
	ctx := newTestCtx("mysql-test")
	err := db.Initialize(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MySQL")
}

func TestNewSqlTeamDatabase_不支持的类型(t *testing.T) {
	config := DatabaseConfig{DBType: "unknown", ConnectionString: ""}
	db := NewSqlTeamDatabase(config)
	ctx := newTestCtx("unknown-test")
	err := db.Initialize(ctx)
	assert.Error(t, err)
}

// ──────────────────────────── ForceDeleteTeamSession / WithTx 测试 ────────────────────────────

func TestSqlTeamDatabase_ForceDeleteTeamSession(t *testing.T) {
	db := newTestSqlDB(t)
	ctx := newTestCtx("force-delete-test")

	db.Team().CreateTeam(ctx, "ft1", "FT1", "l1", "", "")
	db.Member().CreateMember(ctx, "m1", "ft1", "M1", "{}", "ready", "teammate", "", "", "build_mode", "", "")

	result := db.ForceDeleteTeamSession(ctx, "ft1")
	assert.True(t, result)
}

func TestSqlTeamDatabase_WithTx(t *testing.T) {
	db := newTestSqlDB(t)
	ctx := newTestCtx("withtx-test")

	err := db.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		teamDao, memberDao, taskDao, messageDao := db.WithTx(tx)
		assert.NotNil(t, teamDao)
		assert.NotNil(t, memberDao)
		assert.NotNil(t, taskDao)
		assert.NotNil(t, messageDao)
		return nil
	})
	assert.NoError(t, err)
}
