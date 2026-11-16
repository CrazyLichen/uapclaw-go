package tasks

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"

	_ "github.com/mattn/go-sqlite3"
)

// logComponent tasks 包日志组件。
// Python 侧无对应实现：Python 用模块级 logging.getLogger。
const logComponent = logger.ComponentCommon

// ──────────────────────────── 结构体 ────────────────────────────

// TaskStore 任务事实与幂等用户操作的 SQLite 记录。
// 对齐 tasks/store.py:20-114 TaskStore。
type TaskStore struct {
	// Path 账本数据库文件路径
	Path string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TaskDatabase 默认账本路径：<agent 根目录>/voice-agent-tasks.sqlite。
// 对齐 tasks/store.py:10-13 task_database（get_agent_root_dir → workspace 根目录）。
func TaskDatabase() string {
	return filepath.Join(workspace.AgentRootDir(), "voice-agent-tasks.sqlite")
}

// NewTaskStore 打开并初始化账本。
// 对齐 tasks/store.py:21-35 __init__。
func NewTaskStore(path string) (*TaskStore, error) {
	if err := mkdirParent(path); err != nil {
		return nil, err
	}
	store := &TaskStore{Path: path}
	db, err := store.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	schema := `
		CREATE TABLE IF NOT EXISTS managed_tasks (
			id TEXT PRIMARY KEY, owner TEXT NOT NULL, session TEXT NOT NULL,
			body TEXT NOT NULL);
		CREATE INDEX IF NOT EXISTS managed_tasks_scope ON managed_tasks(owner,session);
		CREATE TABLE IF NOT EXISTS task_commands (
			owner TEXT NOT NULL, session TEXT NOT NULL, id TEXT NOT NULL,
			fingerprint TEXT NOT NULL, task_id TEXT NOT NULL, body TEXT NOT NULL,
			PRIMARY KEY(owner,session,id));`
	if _, err := db.Exec(schema); err != nil {
		logger.Error(logComponent).Err(err).Str("path", path).Msg("账本建表失败")
		return nil, err
	}
	return store, nil
}

// Encode JSON 编码（键排序、紧凑、不转义非 ASCII）。
// 对齐 tasks/store.py:16-18 encode。
func Encode(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(data)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// open 打开 SQLite 连接（busy 超时 10 秒）。
// 对齐 tasks/store.py:28 sqlite3.connect(self.path, timeout=10)。
func (s *TaskStore) open() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", s.Path+"?_busy_timeout=10000&_journal_mode=WAL&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	// _txlock=immediate：所有事务以 BEGIN IMMEDIATE 开启（写锁前置，对齐 Python BEGIN IMMEDIATE）
	return db, nil
}

// transaction 开启 IMMEDIATE 写事务并提交/回滚。
// 对齐 tasks/store.py:37-49 transaction 上下文管理器。
func (s *TaskStore) transaction(fn func(db *sql.Tx) error) error {
	db, err := s.open()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	// 立即锁语义由 DSN _txlock=immediate 提供（对齐 Python BEGIN IMMEDIATE）
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// txGet 读取单条任务体；owner/session 用于作用域校验。
// 对齐 tasks/store.py:51-62 get（静态方法 → Go 包内函数）。
func txGet(db *sql.Tx, taskID string, owner, session any) (map[string]any, error) {
	row := db.QueryRow("SELECT owner, session, body FROM managed_tasks WHERE id=?", taskID)
	var rowOwner, rowSession, body string
	if err := row.Scan(&rowOwner, &rowSession, &body); err != nil {
		return nil, errors.New("Task not found in this conversation")
	}
	if owner != nil && rowOwner != owner.(string) {
		return nil, errors.New("Task not found in this conversation")
	}
	if session != nil && rowSession != session.(string) {
		return nil, errors.New("Task not found in this conversation")
	}
	var task map[string]any
	if err := json.Unmarshal([]byte(body), &task); err != nil {
		return nil, err
	}
	return task, nil
}

// txPut 写入（幂等 upsert）。
// 对齐 tasks/store.py:64-69 put。
func txPut(db *sql.Tx, task map[string]any) error {
	id, _ := task["id"].(string)
	owner, _ := task["owner"].(string)
	session, _ := task["session"].(string)
	_, err := db.Exec(
		"INSERT INTO managed_tasks VALUES (?,?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body",
		id, owner, session, Encode(task))
	if err != nil {
		logger.Error(logComponent).Err(err).Str("task_id", id).Msg("任务写入失败")
	}
	return err
}

// txRows 按作用域读取任务列表（owner nil 表全量）。
// 对齐 tasks/store.py:71-80 rows。
func txRows(db *sql.Tx, owner, session any) ([]map[string]any, error) {
	var rows *sql.Rows
	var err error
	if owner == nil {
		rows, err = db.Query("SELECT body FROM managed_tasks ORDER BY rowid")
	} else {
		rows, err = db.Query(
			"SELECT body FROM managed_tasks WHERE owner=? AND session=? ORDER BY rowid",
			owner.(string), session.(string))
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []map[string]any{}
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var task map[string]any
		if err := json.Unmarshal([]byte(body), &task); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// txReplay 命令幂等重放：同 command_id 不同参数报冲突。
// 对齐 tasks/store.py:82-98 replay。
func txReplay(db *sql.Tx, owner, session, commandID string, request any) (map[string]any, string, error) {
	if commandID == "" || strings.TrimSpace(commandID) == "" || len(commandID) > 256 {
		return nil, "", errors.New("A stable command_id is required")
	}
	fingerprint := sha256Hex(Encode(request))
	row := db.QueryRow(
		"SELECT body FROM task_commands WHERE owner=? AND session=? AND id=?",
		owner, session, commandID)
	var body string
	err := row.Scan(&body)
	if err == sql.ErrNoRows {
		return nil, fingerprint, nil
	}
	if err != nil {
		return nil, "", err
	}
	var receipt map[string]any
	if err := json.Unmarshal([]byte(body), &receipt); err != nil {
		return nil, "", err
	}
	// 指纹校验：body 里存的是回执，指纹在行内
	var stored string
	_ = db.QueryRow(
		"SELECT fingerprint FROM task_commands WHERE owner=? AND session=? AND id=?",
		owner, session, commandID).Scan(&stored)
	if stored != fingerprint {
		return nil, fingerprint, errors.New("Command identity reused with different parameters")
	}
	return receipt, fingerprint, nil
}

// txCommand 记录命令回执。
// 对齐 tasks/store.py:99-110 command。
func txCommand(db *sql.Tx, task map[string]any, commandID, fingerprint string, receipt map[string]any) error {
	owner, _ := task["owner"].(string)
	session, _ := task["session"].(string)
	taskID, _ := task["id"].(string)
	_, err := db.Exec(
		"INSERT INTO task_commands VALUES (?,?,?,?,?,?)",
		owner, session, commandID, fingerprint, taskID, Encode(receipt))
	if err != nil {
		logger.Error(logComponent).Err(err).Str("command_id", commandID).Msg("命令回执写入失败")
	}
	return err
}

// read 事务内读取单任务。
// 对齐 tasks/store.py:112-114 read。
func (s *TaskStore) read(taskID string, owner, session any) (map[string]any, error) {
	var task map[string]any
	err := s.transaction(func(db *sql.Tx) error {
		var err error
		task, err = txGet(db, taskID, owner, session)
		return err
	})
	return task, err
}

// update 读-改-写单任务（变更才算 sequence 递增）。
// 对齐 tasks/store.py:116-123 update。
func (s *TaskStore) update(taskID string, change func(map[string]any)) (map[string]any, error) {
	var task map[string]any
	err := s.transaction(func(db *sql.Tx) error {
		current, err := txGet(db, taskID, nil, nil)
		if err != nil {
			return err
		}
		previous := Encode(current)
		change(current)
		if Encode(current) != previous {
			current["sequence"] = numAdd(current["sequence"], 1)
			if err := txPut(db, current); err != nil {
				return err
			}
		}
		task = current
		return nil
	})
	return task, err
}

// ──────────────────────────── 非导出辅助 ────────────────────────────

// sha256Hex 摘要。
// Python 侧无对应实现：hashlib 调用等价。
func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// numAdd any 数值加法（任务体经 JSON 往返为 float64）。
func numAdd(value any, delta int) int {
	switch v := value.(type) {
	case float64:
		return int(v) + delta
	case int:
		return v + delta
	case int64:
		return int(v) + delta
	}
	return delta
}

// numInt any 数值取整（缺省 0）。
func numInt(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

// mkdirParent 建父目录。
func mkdirParent(path string) error {
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

var _ = fmt.Sprintf

// ──────────────────────────── 导出包装（跨包只读/事务入口） ────────────────────────────

// Transaction 事务入口（task_adapter/bridge 跨包使用）。
// Python 侧无对应实现：Python 同包直接用 contextmanager。
func (s *TaskStore) Transaction(fn func(tx *sql.Tx) error) error { return s.transaction(fn) }

// Rows 作用域读取（跨包使用）。
func (s *TaskStore) Rows(tx *sql.Tx, owner, session any) ([]map[string]any, error) {
	return txRows(tx, owner, session)
}

// Read 单任务读取（跨包使用）。
func (s *TaskStore) Read(taskID string, owner, session any) (map[string]any, error) {
	return s.read(taskID, owner, session)
}

// GetInTx 事务内读取（bridge 使用）。
func (s *TaskStore) GetInTx(tx *sql.Tx, taskID string) (map[string]any, error) {
	return txGet(tx, taskID, nil, nil)
}

// ReplayInTx 事务内命令重放（bridge 使用）。
func (s *TaskStore) ReplayInTx(tx *sql.Tx, owner, session, commandID string, request any) (map[string]any, string, error) {
	return txReplay(tx, owner, session, commandID, request)
}

// sqlTxType 事务类型别名（duplex 包引用）。
type sqlTxType = sql.Tx

// Update 任务变更（跨包/测试使用）。
func (s *TaskStore) Update(taskID string, change func(map[string]any)) (map[string]any, error) {
	return s.update(taskID, change)
}

// ReadDirect 轻量直读（不开写事务；对齐 Python sqlite 直读语义）。
// Python 侧无对应实现：Python transaction 读与 Go 等价，但 Go _txlock=immediate
// 使读也取写锁，轮询场景需无锁读避免与派发事务互相饥饿（偏差 O-8 spec 登记）。
func (s *TaskStore) ReadDirect(taskID string, owner, session any) (map[string]any, error) {
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	row := db.QueryRow("SELECT owner, session, body FROM managed_tasks WHERE id=?", taskID)
	var rowOwner, rowSession, body string
	if err := row.Scan(&rowOwner, &rowSession, &body); err != nil {
		return nil, errors.New("Task not found in this conversation")
	}
	if owner != nil && rowOwner != owner.(string) {
		return nil, errors.New("Task not found in this conversation")
	}
	if session != nil && rowSession != session.(string) {
		return nil, errors.New("Task not found in this conversation")
	}
	var task map[string]any
	if err := json.Unmarshal([]byte(body), &task); err != nil {
		return nil, err
	}
	return task, nil
}

// RowsDirect 轻量直读列表（不开写事务；偏差 O-8 同 ReadDirect）。
func (s *TaskStore) RowsDirect(owner, session any) ([]map[string]any, error) {
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var rows *sql.Rows
	if owner == nil {
		rows, err = db.Query("SELECT body FROM managed_tasks ORDER BY rowid")
	} else {
		rows, err = db.Query(
			"SELECT body FROM managed_tasks WHERE owner=? AND session=? ORDER BY rowid",
			owner.(string), session.(string))
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []map[string]any{}
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var task map[string]any
		if err := json.Unmarshal([]byte(body), &task); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// TxPut 导出写入口（集成测试与 bridge 使用）。
func TxPut(db *sql.Tx, task map[string]any) error { return txPut(db, task) }
