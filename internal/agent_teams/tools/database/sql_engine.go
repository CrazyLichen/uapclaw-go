package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/sessionctx"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SqlTeamDatabase SQL 数据库门面。
// 对齐 Python: TeamDatabase (openjiuwen/agent_teams/tools/database/__init__.py)
// 拥有 *gorm.DB 生命周期和跨表事务，单表操作通过 DAO 属性调用。
type SqlTeamDatabase struct {
	// db GORM 数据库实例
	db *gorm.DB
	// config 数据库配置
	config DatabaseConfig
	// teamDao TeamDao 实例
	teamDao *SQLTeamDao
	// memberDao MemberDao 实例
	memberDao *SQLMemberDao
	// taskDao TaskDao 实例
	taskDao *SQLTaskDao
	// messageDao MessageDao 实例
	messageDao *SQLMessageDao
	// initialized 是否已初始化
	initialized bool
	// mu 保护并发初始化
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件
	logComponent = logger.ComponentCommon
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	_ TeamDatabase = (*SqlTeamDatabase)(nil) // SqlTeamDatabase 必须满足 TeamDatabase 接口
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSqlTeamDatabase 创建 SQL 数据库实例（未初始化，需调用 Initialize）。
func NewSqlTeamDatabase(config DatabaseConfig) *SqlTeamDatabase {
	return &SqlTeamDatabase{config: config}
}

// Initialize 初始化数据库引擎、创建表、接入 DAO。
// 对齐 Python: TeamDatabase.initialize() — 带双重检查锁的惰性初始化
func (s *SqlTeamDatabase) Initialize(ctx context.Context) error {
	if s.initialized {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initialized {
		return nil
	}

	db, err := s.newGormDB(ctx)
	if err != nil {
		return err
	}
	s.db = db
	s.teamDao = &SQLTeamDao{db: db}
	s.memberDao = &SQLMemberDao{db: db}
	s.taskDao = &SQLTaskDao{db: db}
	s.messageDao = &SQLMessageDao{db: db}
	s.initialized = true

	// 对齐 Python: 初始化后执行 create_cur_session_tables
	return s.CreateCurSessionTables(ctx)
}

// CreateCurSessionTables 创建当前会话动态表。
// 对齐 Python: create_cur_session_tables(engine)
func (s *SqlTeamDatabase) CreateCurSessionTables(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	sessionID := sessionctx.GetSessionID(ctx)
	if sessionID == "" {
		// 对齐 Python: team_logger.warning("No session_id in context, cannot create session tables")
		logger.Warn(logComponent).Msg("上下文中无 session_id，无法创建会话表")
		return nil
	}
	suffix := SanitizeSessionIDForTable(sessionID)
	err := createSessionTablesDDL(s.db, suffix)
	if err != nil {
		return err
	}
	logger.Info(logComponent).Str("session_id", sessionID).Msg("会话动态表已就绪")
	return nil
}

// DropCurSessionTables 删除当前会话动态表。
// 对齐 Python: drop_cur_session_tables(engine)
func (s *SqlTeamDatabase) DropCurSessionTables(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	sessionID := sessionctx.GetSessionID(ctx)
	if sessionID == "" {
		logger.Warn(logComponent).Msg("上下文中无 session_id，无法删除会话表")
		return nil
	}
	suffix := SanitizeSessionIDForTable(sessionID)
	dropSessionTablesDDL(s.db, suffix)
	logger.Info(logComponent).Str("session_id", sessionID).Msg("已删除会话动态表")
	return nil
}

// CleanupAllRuntimeState 清理所有运行时状态（删除动态表 + 清空静态表）。
// 对齐 Python: cleanup_all_runtime_state(engine) -> (deleted_tables, cleared_tables)
func (s *SqlTeamDatabase) CleanupAllRuntimeState(ctx context.Context) ([]string, []string, error) {
	if s.db == nil {
		return nil, nil, nil
	}

	var droppedTables []string
	var clearedTables []string

	// 对齐 Python: _get_table_names(sync_conn)
	tableNames, err := s.db.Migrator().GetTables()
	if err != nil {
		return nil, nil, fmt.Errorf("获取表名列表失败: %w", err)
	}

	// 对齐 Python: 匹配 TEAM_DYNAMIC_TABLE_PREFIXES → DROP
	for _, name := range tableNames {
		if isDynamicTable(name) {
			if dropErr := s.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteTableName(name))).Error; dropErr != nil {
				logger.Warn(logComponent).Str("table", name).Err(dropErr).Msg("删除动态表失败")
			} else {
				droppedTables = append(droppedTables, name)
			}
		}
	}

	// 对齐 Python: 匹配 TEAM_STATIC_TABLES_TO_CLEAR → DELETE FROM
	for _, name := range TeamStaticTablesToClear {
		if containsString(tableNames, name) {
			if clearErr := s.db.Exec(fmt.Sprintf("DELETE FROM %s", quoteTableName(name))).Error; clearErr != nil {
				logger.Warn(logComponent).Str("table", name).Err(clearErr).Msg("清空静态表失败")
			} else {
				clearedTables = append(clearedTables, name)
			}
		}
	}

	logger.Info(logComponent).
		Strs("deleted_dynamic_tables", droppedTables).
		Strs("cleared_static_tables", clearedTables).
		Msg("清理团队运行时状态完成")
	return droppedTables, clearedTables, nil
}

// DropSessionTablesByID 按 sessionID 删除动态表。
// 对齐 Python: drop_session_tables_by_id(engine, session_id) -> dropped
func (s *SqlTeamDatabase) DropSessionTablesByID(ctx context.Context, sessionID string) ([]string, error) {
	if s.db == nil || sessionID == "" {
		return nil, nil
	}
	suffix := SanitizeSessionIDForTable(sessionID)

	// 对齐 Python: 遍历 TEAM_DYNAMIC_TABLE_PREFIXES，检查表是否存在，DROP
	var dropped []string
	tableNames, _ := s.db.Migrator().GetTables()
	for _, prefix := range TeamDynamicTablePrefixes {
		expectedTable := prefix + suffix
		if containsString(tableNames, expectedTable) {
			if dropErr := s.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteTableName(expectedTable))).Error; dropErr != nil {
				logger.Warn(logComponent).Str("table", expectedTable).Err(dropErr).Msg("删除会话表失败")
			} else {
				dropped = append(dropped, expectedTable)
			}
		}
	}

	if len(dropped) > 0 {
		logger.Info(logComponent).Str("session_id", sessionID).Strs("dropped", dropped).Msg("按 ID 删除会话动态表完成")
	}
	return dropped, nil
}

// ForceDeleteTeamSession 跨表拆卸：删 team_info 行 + 删成员 + drop 会话动态表。
// 对齐 Python: force_delete_team_session(team_name) -> bool
func (s *SqlTeamDatabase) ForceDeleteTeamSession(ctx context.Context, teamName string) bool {
	if !s.initialized {
		return false
	}
	// 对齐 Python: await self.team.delete_team(team_name)
	s.teamDao.DeleteTeam(ctx, teamName)

	// 对齐 Python: 删除该团队所有成员
	s.db.WithContext(ctx).Table("team_member").Where("team_name = ?", teamName).Delete(nil)

	// 对齐 Python: try: await _drop_cur_session_tables(self.engine)
	if err := s.DropCurSessionTables(ctx); err != nil {
		logger.Error(logComponent).Str("team_name", teamName).Err(err).Msg("强制删除团队会话动态表失败")
		return false
	}

	logger.Info(logComponent).Str("team_name", teamName).Msg("强制删除团队会话数据完成")
	return true
}

// Close 关闭数据库引擎并释放连接。
// 对齐 Python: TeamDatabase.close()
func (s *SqlTeamDatabase) Close() error {
	if s.db != nil {
		sqlDB, err := s.db.DB()
		if err == nil && sqlDB != nil {
			_ = sqlDB.Close()
		}
		s.db = nil
		s.initialized = false
		s.teamDao = nil
		s.memberDao = nil
		s.taskDao = nil
		s.messageDao = nil
		logger.Info(logComponent).Msg("SQL 数据库引擎已关闭")
	}
	return nil
}

// Team 返回 TeamDao。对齐 Python: self.team = TeamDao(...)
func (s *SqlTeamDatabase) Team() TeamDao { return s.teamDao }

// Member 返回 MemberDao。对齐 Python: self.member = MemberDao(...)
func (s *SqlTeamDatabase) Member() MemberDao { return s.memberDao }

// Task 返回 TaskDao。对齐 Python: self.task = TaskDao(...)
func (s *SqlTeamDatabase) Task() TaskDao { return s.taskDao }

// Message 返回 MessageDao。对齐 Python: self.message = MessageDao(...)
func (s *SqlTeamDatabase) Message() MessageDao { return s.messageDao }

// WithTx 返回绑定指定事务的临时 DAO 实例，用于跨表操作。
func (s *SqlTeamDatabase) WithTx(tx *gorm.DB) (*SQLTeamDao, *SQLMemberDao, *SQLTaskDao, *SQLMessageDao) {
	return s.teamDao.withTx(tx),
		s.memberDao.withTx(tx),
		s.taskDao.withTx(tx),
		s.messageDao.withTx(tx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newGormDB 根据 config 创建配置好的 *gorm.DB。
// 对齐 Python: initialize_engine(config) -> (AsyncEngine, async_sessionmaker)
func (s *SqlTeamDatabase) newGormDB(_ context.Context) (*gorm.DB, error) {
	gormConfig := &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	}

	var db *gorm.DB
	var err error

	switch s.config.DBType {
	case DatabaseTypeSQLite:
		connStr := s.config.ConnectionString
		if connStr == "" || connStr == ":memory:" {
			// 对齐 Python: :memory: 用 StaticPool
			db, err = gorm.Open(sqlite.Open(":memory:"), gormConfig)
		} else {
			// 对齐 Python: 文件模式，确保父目录存在
			dbPath := filepath.FromSlash(connStr)
			if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
				_ = os.MkdirAll(dir, 0o755)
			}
			// 对齐 Python: WAL + busy_timeout
			dsn := fmt.Sprintf("%s?_journal_mode=WAL&_busy_timeout=%d", dbPath, s.config.DBTimeout*1000)
			db, err = gorm.Open(sqlite.Open(dsn), gormConfig)
		}
		if err != nil {
			return nil, fmt.Errorf("SQLite 引擎创建失败: %w", err)
		}
		// 对齐 Python: PRAGMA foreign_keys=ON
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_, _ = sqlDB.Exec("PRAGMA foreign_keys=ON")
			if s.config.DBEnableWAL && connStr != ":memory:" && connStr != "" {
				_, _ = sqlDB.Exec("PRAGMA journal_mode=WAL")
			}
			// 对齐 Python: AsyncAdaptedQueuePool(pool_size=5, max_overflow=0)
			if connStr != "" && connStr != ":memory:" {
				sqlDB.SetMaxOpenConns(5)
				sqlDB.SetMaxIdleConns(5)
			}
		}

	case DatabaseTypePostgreSQL:
		connStr := s.config.ConnectionString
		if connStr == "" {
			return nil, fmt.Errorf("PostgreSQL 需要非空 connection_string")
		}
		db, err = gorm.Open(postgres.Open(connStr), gormConfig)
		if err != nil {
			return nil, fmt.Errorf("PostgreSQL 引擎创建失败: %w", err)
		}
		// 对齐 Python: pool_size=10, max_overflow=20, pool_recycle=1800
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.SetMaxOpenConns(10)
			sqlDB.SetMaxIdleConns(10)
			sqlDB.SetConnMaxLifetime(1800e9) // 1800 秒
		}

	case DatabaseTypeMySQL:
		// MySQL 驱动未 vendor，暂不支持，返回错误提示
		return nil, fmt.Errorf("MySQL 驱动未纳入 vendor，请使用 SQLite 或 PostgreSQL")

	default:
		return nil, fmt.Errorf("不支持的数据库类型: %s", s.config.DBType)
	}

	// 对齐 Python: await conn.run_sync(SQLModel.metadata.create_all) — 建静态表
	if autoErr := db.AutoMigrate(&Team{}, &TeamMember{}); autoErr != nil {
		return nil, fmt.Errorf("静态表迁移失败: %w", autoErr)
	}

	// 对齐 Python: await conn.run_sync(_ensure_team_member_role_column) — 迁移补 role 列
	ensureTeamMemberRoleColumn(db)

	logger.Info(logComponent).Str("db_type", string(s.config.DBType)).Msg("SQL 数据库引擎初始化完成")
	return db, nil
}

// ensureTeamMemberRoleColumn 检查并补充 team_member 表的 role 列。
// 对齐 Python: _ensure_team_member_role_column(sync_conn)
// SQLModel.metadata.create_all 只创建不存在的表，不会 ALTER 已有表。
// 旧版 DB 缺 role 列时，INSERT 会失败，因此需要探测并补列。
func ensureTeamMemberRoleColumn(db *gorm.DB) {
	columns, err := db.Migrator().ColumnTypes(&TeamMember{})
	if err != nil {
		return
	}
	for _, col := range columns {
		if col.Name() == "role" {
			return // 已存在
		}
	}
	// 补列：对齐 Python ALTER TABLE team_member ADD COLUMN role TEXT NOT NULL DEFAULT 'teammate'
	db.Exec("ALTER TABLE team_member ADD COLUMN role TEXT NOT NULL DEFAULT 'teammate'")
	logger.Info(logComponent).Msg("迁移 team_member 表：补充 role 列，默认值 teammate")
}

// createSessionTablesDDL 用手写 DDL 创建4张动态表。
// 对齐 Python: create_cur_session_tables(engine) — 逐模型调用 __table__.create(checkfirst=True)
func createSessionTablesDDL(db *gorm.DB, suffix string) error {
	stmts := []string{
		fmt.Sprintf(createTaskTableDDL, suffix),
		fmt.Sprintf(createDepTableDDL, suffix),
		fmt.Sprintf(createMessageTableDDL, suffix),
		fmt.Sprintf(createReadStatusTableDDL, suffix),
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("创建动态表失败: %w", err)
		}
	}
	return nil
}

// dropSessionTablesDDL 用 DDL 删除4张动态表。
// 对齐 Python: drop_cur_session_tables(engine) — 逐模型调用 __table__.drop(checkfirst=True)
func dropSessionTablesDDL(db *gorm.DB, suffix string) {
	tables := []string{
		"team_task_dependency_" + suffix,
		"team_task_" + suffix,
		"team_message_" + suffix,
		"message_read_status_" + suffix,
	}
	for _, t := range tables {
		if dbErr := db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteTableName(t))).Error; dbErr != nil {
			logger.Warn(logComponent).Str("table", t).Err(dbErr).Msg("删除动态表失败")
		}
	}
}

// DDL 常量：统一兼容类型（INTEGER/TEXT），SQLite 和 PostgreSQL 都能接受。
// is_read / read_at 用 DEFAULT NULL 表示广播消息/未初始化水位。
const (
	// 对齐 Python: TeamTaskBase (team_task_{suffix})
	createTaskTableDDL = `CREATE TABLE IF NOT EXISTS team_task_%s (
    task_id      TEXT PRIMARY KEY,
    team_name    TEXT NOT NULL DEFAULT '',
    title        TEXT NOT NULL DEFAULT '',
    content      TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT '',
    assignee     TEXT NOT NULL DEFAULT '',
    updated_at   INTEGER NOT NULL DEFAULT 0
)`

	// 对齐 Python: TeamTaskDependencyBase (team_task_dependency_{suffix})
	// 复合主键 (task_id, depends_on_task_id)，task_id 和 depends_on_task_id 引用同 suffix 的 team_task
	createDepTableDDL = `CREATE TABLE IF NOT EXISTS team_task_dependency_%s (
    task_id            TEXT NOT NULL DEFAULT '',
    depends_on_task_id TEXT NOT NULL DEFAULT '',
    team_name          TEXT NOT NULL DEFAULT '',
    resolved           INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (task_id, depends_on_task_id)
)`

	// 对齐 Python: TeamMessageBase (team_message_{suffix})
	createMessageTableDDL = `CREATE TABLE IF NOT EXISTS team_message_%s (
    message_id       TEXT PRIMARY KEY,
    team_name        TEXT NOT NULL DEFAULT '',
    from_member_name TEXT NOT NULL DEFAULT '',
    to_member_name   TEXT NOT NULL DEFAULT '',
    content          TEXT NOT NULL DEFAULT '',
    timestamp        INTEGER NOT NULL DEFAULT 0,
    broadcast        INTEGER NOT NULL DEFAULT 0,
    is_read          INTEGER DEFAULT NULL
)`

	// 对齐 Python: MessageReadStatusBase (message_read_status_{suffix})
	createReadStatusTableDDL = `CREATE TABLE IF NOT EXISTS message_read_status_%s (
    member_name TEXT NOT NULL DEFAULT '',
    team_name   TEXT NOT NULL DEFAULT '',
    read_at     INTEGER DEFAULT NULL,
    PRIMARY KEY (member_name, team_name)
)`
)

// isDynamicTable 判断表名是否为动态表（按前缀匹配）。
// 对齐 Python: table_name.startswith(TEAM_DYNAMIC_TABLE_PREFIXES)
func isDynamicTable(tableName string) bool {
	for _, prefix := range TeamDynamicTablePrefixes {
		if len(tableName) > len(prefix) && tableName[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// quoteTableName 给表名加引号（SQLite/PostgreSQL 通用）。
// 对齐 Python: quoted_name = table_name.replace('"', '""')
func quoteTableName(name string) string {
	return `"` + name + `"`
}

// containsString 检查字符串切片是否包含指定值。
func containsString(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
