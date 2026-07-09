package skilldev

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StateStore SkillDev 任务状态存储（本地文件实现）。
//
// 线程/协程安全注意：当前本地文件实现不加锁，
// 因为路由层保证同一 task_id 的请求始终路由到同一实例，不存在并发写入。
type StateStore struct {
	// baseDir SkillDev 工作区根目录
	baseDir string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件
	logComponent = logger.ComponentAgentServer
	// stateFileName 状态文件名
	stateFileName = "state.json"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStateStore 创建新的 StateStore 实例。
//
// baseDir 约定为 getWorkspaceDir() / "skilldev"，即 ~/.jiuwenswarm/agent/workspace/skilldev/
func NewStateStore(baseDir string) *StateStore {
	return &StateStore{baseDir: baseDir}
}

// SaveState 将状态序列化并写入 state.json（checkpoint）。
func (s *StateStore) SaveState(taskID string, state *SkillDevState) error {
	state.Touch()
	stateFile := s.stateFile(taskID)

	if err := os.MkdirAll(filepath.Dir(stateFile), 0o755); err != nil {
		logger.Error(logComponent).Str("task_id", taskID).Err(err).Msg("[StateStore] 创建状态目录失败")
		return err
	}

	data := state.ToCheckpointDict()
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		logger.Error(logComponent).Str("task_id", taskID).Err(err).Msg("[StateStore] 序列化状态失败")
		return err
	}

	if err := os.WriteFile(stateFile, bytes, 0o644); err != nil {
		logger.Error(logComponent).Str("task_id", taskID).Err(err).Msg("[StateStore] 写入状态文件失败")
		return err
	}

	logger.Debug(logComponent).
		Str("task_id", taskID).
		Str("stage", string(state.Stage)).
		Msg("[StateStore] checkpoint saved")

	return nil
}

// LoadState 从 state.json 恢复状态，不存在则返回 nil。
func (s *StateStore) LoadState(taskID string) (*SkillDevState, error) {
	stateFile := s.stateFile(taskID)

	data, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Warn(logComponent).Str("task_id", taskID).Msg("[StateStore] state not found")
			return nil, nil
		}
		logger.Error(logComponent).Str("task_id", taskID).Err(err).Msg("[StateStore] 读取状态文件失败")
		return nil, err
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		logger.Error(logComponent).Str("task_id", taskID).Err(err).Msg("[StateStore] 解析状态文件失败")
		return nil, err
	}

	state := FromCheckpointDict(m)
	logger.Debug(logComponent).
		Str("task_id", taskID).
		Str("stage", string(state.Stage)).
		Msg("[StateStore] state loaded")

	return state, nil
}

// ListTasks 列出所有存在 checkpoint 的 task_id。
func (s *StateStore) ListTasks() ([]string, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		logger.Error(logComponent).Err(err).Msg("[StateStore] 读取工作区目录失败")
		return nil, err
	}

	var tasks []string
	for _, entry := range entries {
		if entry.IsDir() {
			stateFile := filepath.Join(s.baseDir, entry.Name(), stateFileName)
			if _, err := os.Stat(stateFile); err == nil {
				tasks = append(tasks, entry.Name())
			}
		}
	}
	return tasks, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stateFile 返回指定任务的 state.json 文件路径。
func (s *StateStore) stateFile(taskID string) string {
	return filepath.Join(s.baseDir, taskID, stateFileName)
}
