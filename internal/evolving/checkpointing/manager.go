package checkpointing

import (
	"fmt"
	"reflect"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/evolving"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CheckpointManager 检查点管理器接口。
//
// 定义检查点保存时机判断、构建和恢复的核心协议。
// DefaultCheckpointManager 是默认实现。
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/manager.py CheckpointManager(Protocol)
type CheckpointManager interface {
	// ShouldSave 判断是否应保存检查点。
	ShouldSave(epoch int, improved bool) bool
	// BuildCheckpoint 从 agent 和 progress 构建检查点数据。
	// progress 参数为 any 类型（避免与 trainer 包循环依赖），
	// Trainer 调用时传入 *Progress。
	BuildCheckpoint(agent evolving.TrainableAgent, progress any, updaterState map[string]any) *EvolveCheckpoint
	// Restore 从检查点恢复 agent 状态，返回 progress 恢复信息。
	Restore(agent evolving.TrainableAgent, checkpoint *EvolveCheckpoint) map[string]any
}

// DefaultCheckpointManager 默认检查点管理器实现。
//
// 保存时机：分数提升或每 N 个 epoch。
// 恢复内容：operators_state + progress best/epoch。
// 待定变更管理：内存中的 pending map。
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/manager.py DefaultCheckpointManager
type DefaultCheckpointManager struct {
	runID            string
	ckptVersion      string
	saveEveryNEpochs int
	saveOnImprove    bool
	pending          map[string][]*PendingChange
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDefaultCheckpointManager 创建默认检查点管理器。
//
// 对应 Python: DefaultCheckpointManager.__init__(run_id, checkpoint_version, save_every_n_epochs, save_on_improve)
func NewDefaultCheckpointManager(
	runID string,
	ckptVersion string,
	saveEveryNEpochs int,
	saveOnImprove bool,
) *DefaultCheckpointManager {
	if runID == "" {
		runID = generateUUID()
	}
	if saveEveryNEpochs < 1 {
		saveEveryNEpochs = 1
	}
	return &DefaultCheckpointManager{
		runID:            runID,
		ckptVersion:      ckptVersion,
		saveEveryNEpochs: saveEveryNEpochs,
		saveOnImprove:    saveOnImprove,
		pending:          make(map[string][]*PendingChange),
	}
}

// RunID 返回运行标识。
// 对应 Python: DefaultCheckpointManager.run_id (property)
func (m *DefaultCheckpointManager) RunID() string {
	return m.runID
}

// ShouldSave 判断是否应保存检查点。
//
// 逻辑：saveOnImprove && improved → true 或 epoch % saveEveryNEpochs == 0 → true
// 对应 Python: DefaultCheckpointManager.should_save(epoch, improved)
func (m *DefaultCheckpointManager) ShouldSave(epoch int, improved bool) bool {
	if m.saveOnImprove && improved {
		return true
	}
	return epoch%m.saveEveryNEpochs == 0
}

// BuildCheckpoint 从 agent 和 progress 构建检查点数据。
//
// progress 参数为 any，通过反射提取 epoch/batch/bestScore/seed/currentEpochScore。
// 对应 Python: DefaultCheckpointManager.build_checkpoint(agent, progress, updater_state)
func (m *DefaultCheckpointManager) BuildCheckpoint(
	agent evolving.TrainableAgent,
	progress any,
	updaterState map[string]any,
) *EvolveCheckpoint {
	operatorsState := snapshotOperatorsState(agent)

	// 从 progress 提取字段（any 类型，使用反射）
	// 对齐 Python: step = {"epoch": int(getattr(progress, "current_epoch", 0)), ...}
	epoch := extractIntField(progress, "CurrentEpoch", 0)
	batch := extractIntField(progress, "CurrentBatchIter", 0)
	bestScore := extractFloatField(progress, "BestScore", 0.0)
	currentScore := extractFloatField(progress, "CurrentEpochScore", 0.0)
	seed := extractIntPtrField(progress, "Seed")

	return &EvolveCheckpoint{
		Version:         m.ckptVersion,
		RunID:           m.runID,
		Step:            map[string]int{"epoch": epoch, "batch": batch},
		Best:            map[string]any{"best_score": bestScore},
		Seed:            seed,
		OperatorsState:  operatorsState,
		UpdaterState:    updaterState,
		SearcherState:   map[string]any{},
		LastMetrics:     map[string]any{"current_epoch_score": currentScore},
	}
}

// Restore 从检查点恢复 agent 状态，返回 progress 恢复信息。
//
// 恢复所有 Operator 状态，返回 {"start_epoch", "best_score", "run_id"}。
// 对应 Python: DefaultCheckpointManager.restore(agent, checkpoint)
func (m *DefaultCheckpointManager) Restore(
	agent evolving.TrainableAgent,
	checkpoint *EvolveCheckpoint,
) map[string]any {
	restoreOperatorsState(agent, checkpoint.OperatorsState)
	return map[string]any{
		"start_epoch": getIntFromMap(checkpoint.Step, "epoch", 0),
		"best_score":  getFloatFromMap(checkpoint.Best, "best_score", 0.0),
		"run_id":      checkpoint.RunID,
	}
}

// AddPending 添加待定变更到内存存储。
// 对应 Python: DefaultCheckpointManager.add_pending(operator_id, change)
func (m *DefaultCheckpointManager) AddPending(operatorID string, change *PendingChange) {
	m.pending[operatorID] = append(m.pending[operatorID], change)
}

// GetPending 获取某 Operator 的待定变更列表。
// 对应 Python: DefaultCheckpointManager.get_pending(operator_id)
func (m *DefaultCheckpointManager) GetPending(operatorID string) []*PendingChange {
	return m.pending[operatorID]
}

// CommitPending 清空并返回 pending payload 中的 EvolutionRecord 总数。
//
// 只清空内存中的待定状态并返回记录计数，不负责写磁盘。
// 对应 Python: DefaultCheckpointManager.commit_pending(operator_id, store)
func (m *DefaultCheckpointManager) CommitPending(operatorID string) int {
	pendingList := m.pending[operatorID]
	delete(m.pending, operatorID)
	count := 0
	for _, change := range pendingList {
		count += len(change.Payload)
	}
	return count
}

// DiscardPending 按 changeID 丢弃特定的待定变更。
// 对应 Python: DefaultCheckpointManager.discard_pending(operator_id, change_id)
func (m *DefaultCheckpointManager) DiscardPending(operatorID, changeID string) {
	list := m.pending[operatorID]
	filtered := make([]*PendingChange, 0, len(list))
	for _, change := range list {
		if change.ChangeID != changeID {
			filtered = append(filtered, change)
		}
	}
	m.pending[operatorID] = filtered
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// snapshotOperatorsState 快照所有 Operator 的状态。
// 对应 Python: DefaultCheckpointManager._snapshot_operators_state(agent)
func snapshotOperatorsState(agent evolving.TrainableAgent) map[string]map[string]any {
	ops := agent.GetOperators()
	if ops == nil {
		return map[string]map[string]any{}
	}
	out := make(map[string]map[string]any, len(ops))
	for _, op := range ops {
		out[op.OperatorID()] = op.GetState()
	}
	return out
}

// restoreOperatorsState 恢复所有 Operator 的状态。
// 对应 Python: DefaultCheckpointManager._restore_operators_state(agent, operators_state)
func restoreOperatorsState(agent evolving.TrainableAgent, operatorsState map[string]map[string]any) {
	ops := agent.GetOperators()
	if ops == nil || operatorsState == nil {
		return
	}
	for operatorID, state := range operatorsState {
		op, ok := ops[operatorID]
		if ok {
			op.LoadState(state)
		}
	}
}

// extractIntField 使用反射从 any 提取 int 字段。
// 对齐 Python: int(getattr(progress, field_name, default_val))
func extractIntField(obj any, fieldName string, defaultVal int) int {
	if obj == nil {
		return defaultVal
	}
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	f := v.FieldByName(fieldName)
	if !f.IsValid() {
		return defaultVal
	}
	switch f.Kind() {
	case reflect.Int, reflect.Int64:
		return int(f.Int())
	default:
		return defaultVal
	}
}

// extractFloatField 使用反射从 any 提取 float64 字段。
func extractFloatField(obj any, fieldName string, defaultVal float64) float64 {
	if obj == nil {
		return defaultVal
	}
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	f := v.FieldByName(fieldName)
	if !f.IsValid() {
		return defaultVal
	}
	switch f.Kind() {
	case reflect.Float64, reflect.Float32:
		return f.Float()
	case reflect.Int, reflect.Int64:
		return float64(f.Int())
	default:
		return defaultVal
	}
}

// extractIntPtrField 使用反射从 any 提取 *int 字段。
func extractIntPtrField(obj any, fieldName string) *int {
	if obj == nil {
		return nil
	}
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	f := v.FieldByName(fieldName)
	if !f.IsValid() {
		return nil
	}
	if f.Kind() == reflect.Ptr {
		if f.IsNil() {
			return nil
		}
		val := int(f.Elem().Int())
		return &val
	}
	if f.Kind() == reflect.Int || f.Kind() == reflect.Int64 {
		val := int(f.Int())
		return &val
	}
	return nil
}

// getIntFromMap 从 map[string]int 提取 int 值。
func getIntFromMap(m map[string]int, key string, defaultVal int) int {
	if m == nil {
		return defaultVal
	}
	if v, ok := m[key]; ok {
		return v
	}
	return defaultVal
}

// getFloatFromMap 从 map[string]any 提取 float64 值。
func getFloatFromMap(m map[string]any, key string, defaultVal float64) float64 {
	if m == nil {
		return defaultVal
	}
	if v, ok := m[key]; ok {
		return getFloatFromAny(v, defaultVal)
	}
	return defaultVal
}

// generateUUID 生成 UUID。
func generateUUID() string {
	return fmt.Sprintf("%08x-%04x", time.Now().UnixNano()&0xFFFFFFFF, time.Now().UnixNano()>>32&0xFFFF)
}
