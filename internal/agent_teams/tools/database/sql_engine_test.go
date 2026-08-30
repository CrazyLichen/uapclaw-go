package database

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
func newTestCtx(sessionID string) context.Context {
	ctx := context.Background()
	// 通过注入的 GetSessionIDFunc 提供 session ID
	origFunc := GetSessionIDFunc
	GetSessionIDFunc = func(_ context.Context) string { return sessionID }
	// 恢复：使用 defer 模式不方便，测试中手动管理
	_ = origFunc // 保存引用
	return ctx
}

// restoreGetSessionID 恢复 GetSessionIDFunc。
func restoreGetSessionID() {
	GetSessionIDFunc = func(_ context.Context) string { return "" }
}

func TestSqlTeamDatabase_Initialize(t *testing.T) {
	defer restoreGetSessionID()
	db := newTestSqlDB(t)
	assert.True(t, db.initialized)
}

func TestSqlTeamDatabase_CreateCurSessionTables(t *testing.T) {
	defer restoreGetSessionID()
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
	defer restoreGetSessionID()
	db := newTestSqlDB(t)
	ctx := newTestCtx("session-drop-test")

	_ = db.CreateCurSessionTables(ctx)
	err := db.DropCurSessionTables(ctx)
	assert.NoError(t, err)

	suffix := SanitizeSessionIDForTable("session-drop-test")
	assert.False(t, db.db.Migrator().HasTable("team_task_"+suffix))
}

func TestSqlTeamDatabase_CleanupAllRuntimeState(t *testing.T) {
	defer restoreGetSessionID()
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
	defer restoreGetSessionID()
	db := newTestSqlDB(t)
	ctx := newTestCtx("by-id-test")
	_ = db.CreateCurSessionTables(ctx)

	dropped, err := db.DropSessionTablesByID(ctx, "by-id-test")
	assert.NoError(t, err)
	assert.True(t, len(dropped) > 0)
}

func TestSqlTeamDatabase_Close(t *testing.T) {
	defer restoreGetSessionID()
	config := DatabaseConfig{DBType: DatabaseTypeSQLite, ConnectionString: ":memory:"}
	db := NewSqlTeamDatabase(config)
	ctx := newTestCtx("close-test")
	_ = db.Initialize(ctx)

	err := db.Close()
	assert.NoError(t, err)
	assert.False(t, db.initialized)
	assert.Nil(t, db.db)
}

func TestNewTeamDatabase_Memory(t *testing.T) {
	defer restoreGetSessionID()
	ctx := context.Background()
	db := NewTeamDatabase(ctx, NewMemoryDatabaseConfig())
	_, ok := db.(*InMemoryTeamDatabase)
	assert.True(t, ok, "应返回 InMemoryTeamDatabase")
}

func TestNewTeamDatabase_SQL(t *testing.T) {
	defer restoreGetSessionID()
	ctx := newTestCtx("factory-test")
	db := NewTeamDatabase(ctx, NewDatabaseConfig())
	_, ok := db.(*SqlTeamDatabase)
	assert.True(t, ok, "应返回 SqlTeamDatabase")
	_ = db.Close()
}

func TestNewSqlTeamDatabase_文件SQLite(t *testing.T) {
	defer restoreGetSessionID()
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
	defer restoreGetSessionID()
	config := DatabaseConfig{DBType: DatabaseTypePostgreSQL, ConnectionString: ""}
	db := NewSqlTeamDatabase(config)
	ctx := newTestCtx("pg-test")
	err := db.Initialize(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "PostgreSQL")
}

func TestNewSqlTeamDatabase_MySQL_不支持(t *testing.T) {
	defer restoreGetSessionID()
	config := DatabaseConfig{DBType: DatabaseTypeMySQL, ConnectionString: "root@/test"}
	db := NewSqlTeamDatabase(config)
	ctx := newTestCtx("mysql-test")
	err := db.Initialize(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MySQL")
}

func TestNewSqlTeamDatabase_不支持的类型(t *testing.T) {
	defer restoreGetSessionID()
	config := DatabaseConfig{DBType: "unknown", ConnectionString: ""}
	db := NewSqlTeamDatabase(config)
	ctx := newTestCtx("unknown-test")
	err := db.Initialize(ctx)
	assert.Error(t, err)
}
