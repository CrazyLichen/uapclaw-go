package checkpointer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ──────────────────────────── 接口 ────────────────────────────

// EntityHooks 单实体状态存储的钩子接口。
// 对应 Python: BaseSingleStateStorage 的 _get_entity_id/_get_state_to_save/_restore_state
// Go 不支持虚方法分派，通过接口注入实现模板方法模式。
type EntityHooks interface {
	// GetEntityID 获取实体 ID（Agent 返回 agentID，AgentTeam 返回 teamID）
	GetEntityID(session CheckpointerSession) string
	// GetStateToSave 获取需要保存的状态
	GetStateToSave(session CheckpointerSession) any
	// RestoreState 将恢复的状态设置回 session
	RestoreState(session CheckpointerSession, savedState any)
}

// ──────────────────────────── 结构体 ────────────────────────────

// basePersistenceStorage 持久化单实体状态存储基类。
// 持有 BaseKVStore + Serializer + EntityHooks，
// 通过 EntityHooks 注入实现 Python 模板方法模式。
// Save/Recover/Clear/Exists 为固定骨架，通过 hooks 调用子类逻辑。
//
// 对应 Python: persistence.py (BaseSingleStateStorage)
type basePersistenceStorage struct {
	// kvStore KV 存储后端
	kvStore kv.BaseKVStore
	// serde 序列化器
	serde Serializer
	// hooks 实体钩子（注入点）
	hooks EntityHooks
	// namespace 命名空间（"agent" / "agent-team"）
	namespace string
	// entityLabel 实体标签（"agent" / "agent_team"），用于日志
	entityLabel string
	// stateBlobsKey 状态数据键后缀
	stateBlobsKey string
	// stateDumpTypeKey 状态类型键后缀
	stateDumpTypeKey string
}

// agentEntityHooks Agent 存储钩子实现。
type agentEntityHooks struct{}

// agentTeamEntityHooks AgentTeam 存储钩子实现。
type agentTeamEntityHooks struct{}

// PersistenceAgentStorage Agent 持久化状态存储。
// 对应 Python: persistence.py (AgentStorage)
type PersistenceAgentStorage struct {
	basePersistenceStorage
}

// PersistenceAgentTeamStorage AgentTeam 持久化状态存储。
// 对应 Python: persistence.py (AgentTeamStorage)
type PersistenceAgentTeamStorage struct {
	basePersistenceStorage
}

// PersistenceWorkflowStorage Workflow 持久化状态存储。
// 独立于 basePersistenceStorage，因为需要同时保存 state + updates 两类数据（4 个 key）。
// 对应 Python: persistence.py (WorkflowStorage)
type PersistenceWorkflowStorage struct {
	// kvStore KV 存储后端
	kvStore kv.BaseKVStore
	// serde 序列化器
	serde Serializer
}

// PersistenceCheckpointer 持久化检查点器，所有状态存储在 BaseKVStore 中。
// 对应 Python: persistence.py (PersistenceCheckpointer)
type PersistenceCheckpointer struct {
	// kvStore KV 存储后端
	kvStore kv.BaseKVStore
	// agentStorage Agent 持久化状态存储
	agentStorage *PersistenceAgentStorage
	// agentTeamStorage AgentTeam 持久化状态存储
	agentTeamStorage *PersistenceAgentTeamStorage
	// workflowStorage Workflow 持久化状态存储
	workflowStorage *PersistenceWorkflowStorage
	// graphStore 图状态存储
	// ⤵️ 8.7 回填
	graphStore any
}

// persistenceProvider Persistence 检查点器提供者。
// 对应 Python: PersistenceCheckpointerProvider
type persistenceProvider struct{}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// keyNums 单实体存储的 key 数量（dumpType + blob）
	keyNums = 2

	// wfStateBlobs 工作流状态数据键后缀
	wfStateBlobs = "workflow_state_blobs"
	// wfStateBlobsDumpType 工作流状态类型键后缀
	wfStateBlobsDumpType = "workflow_state_blobs_dump_type"
	// wfUpdateBlobs 工作流更新数据键后缀
	wfUpdateBlobs = "workflow_update_blobs"
	// wfUpdateBlobsDumpType 工作流更新类型键后缀
	wfUpdateBlobsDumpType = "workflow_update_blobs_dump_type"
	// wfKeyNums 工作流存储的 key 数量（state_dump_type + state_blob + update_dump_type + update_blob）
	wfKeyNums = 4
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPersistenceCheckpointer 创建持久化检查点器实例。
func NewPersistenceCheckpointer(kvStore kv.BaseKVStore) *PersistenceCheckpointer {
	logger.Info(logComponent).
		Str("action", "new_persistence_checkpointer").
		Str("storage_type", "persistence").
		Msg("创建持久化检查点器")
	return &PersistenceCheckpointer{
		kvStore:          kvStore,
		agentStorage:     newPersistenceAgentStorage(kvStore),
		agentTeamStorage: newPersistenceAgentTeamStorage(kvStore),
		workflowStorage:  newPersistenceWorkflowStorage(kvStore),
		graphStore:       nil, // ⤵️ 8.7 回填
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newPersistenceAgentStorage 创建 Agent 持久化状态存储。
func newPersistenceAgentStorage(kvStore kv.BaseKVStore) *PersistenceAgentStorage {
	return &PersistenceAgentStorage{
		basePersistenceStorage: basePersistenceStorage{
			kvStore:          kvStore,
			serde:            NewJSONSerializer(),
			namespace:        SessionNamespaceAgent,
			entityLabel:      "agent",
			stateBlobsKey:    "agent_state_blobs",
			stateDumpTypeKey: "agent_state_blobs_dump_type",
			hooks:            &agentEntityHooks{},
		},
	}
}

// newPersistenceAgentTeamStorage 创建 AgentTeam 持久化状态存储。
func newPersistenceAgentTeamStorage(kvStore kv.BaseKVStore) *PersistenceAgentTeamStorage {
	return &PersistenceAgentTeamStorage{
		basePersistenceStorage: basePersistenceStorage{
			kvStore:          kvStore,
			serde:            NewJSONSerializer(),
			namespace:        SessionNamespaceAgentTeam,
			entityLabel:      "agent_team",
			stateBlobsKey:    "agent_team_state_blobs",
			stateDumpTypeKey: "agent_team_state_blobs_dump_type",
			hooks:            &agentTeamEntityHooks{},
		},
	}
}

// newPersistenceWorkflowStorage 创建 Workflow 持久化状态存储。
func newPersistenceWorkflowStorage(kvStore kv.BaseKVStore) *PersistenceWorkflowStorage {
	return &PersistenceWorkflowStorage{
		kvStore: kvStore,
		serde:   NewJSONSerializer(),
	}
}

// pipelineGetResult 从 Pipeline 结果中获取 Get 操作的值。
func pipelineGetResult(results []kv.PipelineResult, idx int) ([]byte, error) {
	if idx >= len(results) {
		return nil, nil
	}
	if results[idx].Err != nil {
		return nil, results[idx].Err
	}
	return results[idx].Value, nil
}

// pipelineExistsResult 从 Pipeline 结果中获取 Exists 操作的布尔值。
func pipelineExistsResult(results []kv.PipelineResult, idx int) (bool, error) {
	if idx >= len(results) {
		return false, nil
	}
	if results[idx].Err != nil {
		return false, results[idx].Err
	}
	return results[idx].Exists, nil
}

// ──────────────────────────── agentEntityHooks 方法 ────────────────────────────

// GetEntityID 实现 EntityHooks 接口。
func (h *agentEntityHooks) GetEntityID(session CheckpointerSession) string {
	return GetAgentID(session)
}

// GetStateToSave 实现 EntityHooks 接口。
func (h *agentEntityHooks) GetStateToSave(session CheckpointerSession) any {
	if session.State() == nil {
		return nil
	}
	return session.State().GetState()
}

// RestoreState 实现 EntityHooks 接口。
func (h *agentEntityHooks) RestoreState(session CheckpointerSession, savedState any) {
	if session.State() == nil || savedState == nil {
		return
	}
	if st, ok := savedState.(map[string]any); ok {
		session.State().SetState(st)
	}
}

// ──────────────────────────── agentTeamEntityHooks 方法 ────────────────────────────

// GetEntityID 实现 EntityHooks 接口。
func (h *agentTeamEntityHooks) GetEntityID(session CheckpointerSession) string {
	return GetTeamID(session)
}

// GetStateToSave 实现 EntityHooks 接口。
func (h *agentTeamEntityHooks) GetStateToSave(session CheckpointerSession) any {
	if session.State() == nil {
		return nil
	}
	if asc, ok := session.State().(*state.AgentStateCollection); ok {
		return asc.GetState()
	}
	return session.State().GetGlobal(state.AllStateKey)
}

// RestoreState 实现 EntityHooks 接口。
func (h *agentTeamEntityHooks) RestoreState(session CheckpointerSession, savedState any) {
	if session.State() == nil || savedState == nil {
		return
	}
	if asc, ok := session.State().(*state.AgentStateCollection); ok {
		if st, ok := savedState.(map[string]any); ok {
			asc.SetState(st)
		}
	}
}

// ──────────────────────────── basePersistenceStorage 方法 ────────────────────────────

// Save 保存会话状态到 KVStore。
// 对应 Python: BaseSingleStateStorage.save()
func (s *basePersistenceStorage) Save(ctx context.Context, session CheckpointerSession) error {
	savedState := s.hooks.GetStateToSave(session)
	sessionID := session.SessionID()
	entityID := s.hooks.GetEntityID(session)

	stateBlob := s.serializeState(savedState)
	if stateBlob == nil {
		logger.Warn(logComponent).
			Str("event_type", "checkpoint_error").
			Str("session_id", sessionID).
			Str(s.entityLogExtraKey(), entityID).
			Str("metadata_operation", "serialize").
			Str("storage_type", "persistence").
			Msgf("序列化 %s 状态失败", s.entityLabel)
		return nil
	}

	formatTag, data := stateBlob.FormatTag, stateBlob.Data
	pipeline := s.kvStore.Pipeline(ctx)
	dumpTypeKey, blobKey := s.buildStateKeys(sessionID, entityID)
	_ = pipeline.Set(ctx, dumpTypeKey, []byte(formatTag), 0)
	_ = pipeline.Set(ctx, blobKey, data, 0)
	if _, err := pipeline.Execute(ctx); err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "checkpoint_error").
			Str("session_id", sessionID).
			Str(s.entityLogExtraKey(), entityID).
			Str("metadata_operation", "save").
			Str("storage_type", "persistence").
			Msgf("保存 %s 状态失败", s.entityLabel)
		return err
	}
	logger.Debug(logComponent).
		Str("event_type", "checkpoint_save").
		Str("session_id", sessionID).
		Str(s.entityLogExtraKey(), entityID).
		Str("storage_type", "persistence").
		Msgf("%s 状态保存成功", s.entityTitleLabel())
	return nil
}

// Recover 从 KVStore 恢复会话状态。
// 对应 Python: BaseSingleStateStorage.recover()
func (s *basePersistenceStorage) Recover(ctx context.Context, session CheckpointerSession, _ any) error {
	sessionID := session.SessionID()
	entityID := s.hooks.GetEntityID(session)

	pipeline := s.kvStore.Pipeline(ctx)
	dumpTypeKey, blobKey := s.buildStateKeys(sessionID, entityID)
	_ = pipeline.Get(ctx, dumpTypeKey)
	_ = pipeline.Get(ctx, blobKey)
	results, err := pipeline.Execute(ctx)
	if err != nil {
		return err
	}

	if len(results) != keyNums {
		logger.Debug(logComponent).
			Str("event_type", "checkpoint_restore").
			Str("session_id", sessionID).
			Str(s.entityLogExtraKey(), entityID).
			Int("expected_keys", keyNums).
			Int("actual_keys", len(results)).
			Msgf("恢复 %s 状态时 key 数量异常", s.entityLabel)
		return nil
	}

	dumpTypeBytes, _ := pipelineGetResult(results, 0)
	blob, _ := pipelineGetResult(results, 1)
	loadedState := s.deserializeState(dumpTypeBytes, blob)
	if loadedState == nil {
		logger.Debug(logComponent).
			Str("event_type", "checkpoint_restore").
			Str("session_id", sessionID).
			Str(s.entityLogExtraKey(), entityID).
			Str("storage_type", "persistence").
			Msgf("未找到 %s 状态", s.entityLabel)
		return nil
	}

	s.hooks.RestoreState(session, loadedState)
	logger.Debug(logComponent).
		Str("event_type", "checkpoint_restore").
		Str("session_id", sessionID).
		Str(s.entityLogExtraKey(), entityID).
		Str("storage_type", "persistence").
		Msgf("%s 状态恢复成功", s.entityTitleLabel())
	return nil
}

// Clear 清除 KVStore 中的会话状态。
func (s *basePersistenceStorage) Clear(ctx context.Context, entityID, sessionID string) error {
	dumpTypeKey, blobKey := s.buildStateKeys(sessionID, entityID)
	_, err := s.kvStore.BatchDelete(ctx, []string{dumpTypeKey, blobKey}, 0)
	if err != nil {
		return err
	}
	logger.Debug(logComponent).
		Str("event_type", "checkpoint_clear").
		Str("session_id", sessionID).
		Str(s.entityLogExtraKey(), entityID).
		Str("storage_type", "persistence").
		Msgf("%s 检查点已清除", s.entityLabel)
	return nil
}

// Exists 检查 KVStore 中是否存在会话状态。
func (s *basePersistenceStorage) Exists(ctx context.Context, session CheckpointerSession) (bool, error) {
	sessionID := session.SessionID()
	entityID := s.hooks.GetEntityID(session)

	pipeline := s.kvStore.Pipeline(ctx)
	dumpTypeKey, blobKey := s.buildStateKeys(sessionID, entityID)
	_ = pipeline.Exists(ctx, dumpTypeKey)
	_ = pipeline.Exists(ctx, blobKey)
	results, err := pipeline.Execute(ctx)
	if err != nil {
		return false, err
	}

	if len(results) != keyNums {
		return false, nil
	}
	exists0, _ := pipelineExistsResult(results, 0)
	exists1, _ := pipelineExistsResult(results, 1)
	return exists0 && exists1, nil
}

// buildStateKeys 构建 KV 存储键。
func (s *basePersistenceStorage) buildStateKeys(sessionID, entityID string) (string, string) {
	dumpTypeKey := BuildKeyWithNamespace(sessionID, s.namespace, entityID, s.stateDumpTypeKey)
	blobKey := BuildKeyWithNamespace(sessionID, s.namespace, entityID, s.stateBlobsKey)
	return dumpTypeKey, blobKey
}

// serializeState 序列化状态，返回 serdeTuple。
func (s *basePersistenceStorage) serializeState(st any) *serdeTuple {
	formatTag, data, err := s.serde.DumpsTyped(st)
	if err != nil {
		return nil
	}
	return &serdeTuple{FormatTag: formatTag, Data: data}
}

// deserializeState 反序列化状态。
func (s *basePersistenceStorage) deserializeState(dumpTypeBytes []byte, blob []byte) any {
	if dumpTypeBytes == nil || blob == nil {
		return nil
	}
	dumpType := string(dumpTypeBytes)
	st, err := s.serde.LoadsTyped(dumpType, blob)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "checkpoint_error").
			Str("metadata_operation", "deserialize").
			Msg("反序列化状态失败")
		return nil
	}
	return st
}

// entityLogExtraKey 返回日志字段键名（"agent_id" 或 "workflow_id"）。
func (s *basePersistenceStorage) entityLogExtraKey() string {
	if s.entityLabel == "agent" {
		return "agent_id"
	}
	return "workflow_id"
}

// entityTitleLabel 返回首字母大写的实体标签（"Agent" / "Agent_team"），用于日志。
func (s *basePersistenceStorage) entityTitleLabel() string {
	if len(s.entityLabel) == 0 {
		return ""
	}
	return strings.ToUpper(s.entityLabel[:1]) + s.entityLabel[1:]
}

// ──────────────────────────── PersistenceWorkflowStorage 方法 ────────────────────────────

// Save 保存工作流状态到 KVStore。
// 对应 Python: WorkflowStorage.save()
func (ws *PersistenceWorkflowStorage) Save(ctx context.Context, session CheckpointerSession) error {
	workflowID := session.WorkflowID()
	sessionID := session.SessionID()
	pipeline := ws.kvStore.Pipeline(ctx)
	hasOperations := false

	// 保存主状态
	mainState := session.State().GetState()
	if mainState != nil {
		stateBlob := ws.serializeState(mainState)
		if stateBlob != nil {
			dumpTypeKey := BuildKeyWithNamespace(sessionID, SessionNamespaceWorkflow, workflowID, wfStateBlobsDumpType)
			blobKey := BuildKeyWithNamespace(sessionID, SessionNamespaceWorkflow, workflowID, wfStateBlobs)
			_ = pipeline.Set(ctx, dumpTypeKey, []byte(stateBlob.FormatTag), 0)
			_ = pipeline.Set(ctx, blobKey, stateBlob.Data, 0)
			hasOperations = true
		} else {
			logger.Warn(logComponent).
				Str("event_type", "checkpoint_error").
				Str("session_id", sessionID).
				Str("workflow_id", workflowID).
				Str("metadata_operation", "serialize").
				Msg("序列化工作流状态失败")
		}
	}

	// 保存状态更新
	if commitState, ok := session.State().(*state.WorkflowCommitState); ok {
		updates := commitState.GetUpdates()
		if updates != nil {
			updatesBlob := ws.serializeState(updates)
			if updatesBlob != nil {
				dumpTypeKey := BuildKeyWithNamespace(sessionID, SessionNamespaceWorkflow, workflowID, wfUpdateBlobsDumpType)
				blobKey := BuildKeyWithNamespace(sessionID, SessionNamespaceWorkflow, workflowID, wfUpdateBlobs)
				_ = pipeline.Set(ctx, dumpTypeKey, []byte(updatesBlob.FormatTag), 0)
				_ = pipeline.Set(ctx, blobKey, updatesBlob.Data, 0)
				hasOperations = true
			}
		}
	}

	if hasOperations {
		if _, err := pipeline.Execute(ctx); err != nil {
			logger.Error(logComponent).Err(err).
				Str("event_type", "checkpoint_error").
				Str("session_id", sessionID).
				Str("workflow_id", workflowID).
				Str("metadata_operation", "save").
				Str("storage_type", "persistence").
				Msg("保存工作流状态失败")
			return err
		}
		logger.Debug(logComponent).
			Str("event_type", "checkpoint_save").
			Str("session_id", sessionID).
			Str("workflow_id", workflowID).
			Str("storage_type", "persistence").
			Msg("工作流状态保存成功")
	}
	return nil
}

// Recover 从 KVStore 恢复工作流状态。
// 对应 Python: WorkflowStorage.recover()
func (ws *PersistenceWorkflowStorage) Recover(ctx context.Context, session CheckpointerSession, inputs any) error {
	workflowID := session.WorkflowID()
	sessionID := session.SessionID()

	pipeline := ws.kvStore.Pipeline(ctx)
	stateDumpTypeKey := BuildKeyWithNamespace(sessionID, SessionNamespaceWorkflow, workflowID, wfStateBlobsDumpType)
	stateBlobKey := BuildKeyWithNamespace(sessionID, SessionNamespaceWorkflow, workflowID, wfStateBlobs)
	updatesDumpTypeKey := BuildKeyWithNamespace(sessionID, SessionNamespaceWorkflow, workflowID, wfUpdateBlobsDumpType)
	updatesBlobKey := BuildKeyWithNamespace(sessionID, SessionNamespaceWorkflow, workflowID, wfUpdateBlobs)
	_ = pipeline.Get(ctx, stateDumpTypeKey)
	_ = pipeline.Get(ctx, stateBlobKey)
	_ = pipeline.Get(ctx, updatesDumpTypeKey)
	_ = pipeline.Get(ctx, updatesBlobKey)
	results, err := pipeline.Execute(ctx)
	if err != nil {
		return err
	}

	if len(results) != wfKeyNums {
		logger.Warn(logComponent).
			Str("event_type", "checkpoint_restore").
			Str("session_id", sessionID).
			Str("workflow_id", workflowID).
			Int("expected_keys", wfKeyNums).
			Int("actual_keys", len(results)).
			Msg("恢复工作流状态时 key 数量异常")
		return nil
	}

	// 恢复主状态
	stateDumpType, _ := pipelineGetResult(results, 0)
	stateBlob, _ := pipelineGetResult(results, 1)
	dumpTypeStr := string(stateDumpType)
	if stateBlob != nil && dumpTypeStr != "" && dumpTypeStr != emptyFormatTag {
		loadedState, deserErr := ws.serde.LoadsTyped(dumpTypeStr, stateBlob)
		if deserErr != nil {
			logger.Error(logComponent).Err(deserErr).
				Str("event_type", "checkpoint_error").
				Str("session_id", sessionID).
				Str("workflow_id", workflowID).
				Str("metadata_operation", "deserialize_state").
				Msg("反序列化工作流状态失败")
		} else if loadedState != nil {
			if st, ok := loadedState.(map[string]any); ok {
				session.State().SetState(st)
			}
		}
	}

	// 处理交互输入
	ws.recoverFromInputs(session, inputs)

	// 恢复状态更新
	if commitState, ok := session.State().(*state.WorkflowCommitState); ok {
		updatesDumpType, _ := pipelineGetResult(results, 2)
		updatesBlob, _ := pipelineGetResult(results, 3)
		updatesDumpTypeStr := string(updatesDumpType)
		if updatesBlob != nil && updatesDumpTypeStr != "" && updatesDumpTypeStr != emptyFormatTag {
			updates, deserErr := ws.serde.LoadsTyped(updatesDumpTypeStr, updatesBlob)
			if deserErr != nil {
				logger.Error(logComponent).Err(deserErr).
					Str("event_type", "checkpoint_error").
					Str("session_id", sessionID).
					Str("workflow_id", workflowID).
					Str("metadata_operation", "deserialize_updates").
					Msg("反序列化工作流更新失败")
			} else if updates != nil {
				if u, ok := updates.(map[string]any); ok {
					commitState.SetUpdates(u)
				}
			}
		}
	}
	return nil
}

// Clear 清除工作流状态。
func (ws *PersistenceWorkflowStorage) Clear(ctx context.Context, workflowID, sessionID string) error {
	keys := []string{
		BuildKeyWithNamespace(sessionID, SessionNamespaceWorkflow, workflowID, wfStateBlobsDumpType),
		BuildKeyWithNamespace(sessionID, SessionNamespaceWorkflow, workflowID, wfStateBlobs),
		BuildKeyWithNamespace(sessionID, SessionNamespaceWorkflow, workflowID, wfUpdateBlobsDumpType),
		BuildKeyWithNamespace(sessionID, SessionNamespaceWorkflow, workflowID, wfUpdateBlobs),
	}
	_, err := ws.kvStore.BatchDelete(ctx, keys, 0)
	if err != nil {
		return err
	}
	logger.Debug(logComponent).
		Str("event_type", "checkpoint_clear").
		Str("session_id", sessionID).
		Str("workflow_id", workflowID).
		Str("storage_type", "persistence").
		Msg("工作流检查点已清除")
	return nil
}

// Exists 检查工作流状态是否存在。
func (ws *PersistenceWorkflowStorage) Exists(ctx context.Context, session CheckpointerSession) (bool, error) {
	workflowID := session.WorkflowID()
	sessionID := session.SessionID()

	pipeline := ws.kvStore.Pipeline(ctx)
	stateDumpTypeKey := BuildKeyWithNamespace(sessionID, SessionNamespaceWorkflow, workflowID, wfStateBlobsDumpType)
	stateBlobKey := BuildKeyWithNamespace(sessionID, SessionNamespaceWorkflow, workflowID, wfStateBlobs)
	_ = pipeline.Exists(ctx, stateDumpTypeKey)
	_ = pipeline.Exists(ctx, stateBlobKey)
	results, err := pipeline.Execute(ctx)
	if err != nil {
		return false, err
	}

	if len(results) != keyNums {
		return false, nil
	}
	exists0, _ := pipelineExistsResult(results, 0)
	exists1, _ := pipelineExistsResult(results, 1)
	return exists0 && exists1, nil
}

// ──────────────────────────── PersistenceWorkflowStorage 非导出方法 ────────────────────────────

// serializeState 序列化状态，返回 serdeTuple。
func (ws *PersistenceWorkflowStorage) serializeState(st any) *serdeTuple {
	formatTag, data, err := ws.serde.DumpsTyped(st)
	if err != nil {
		return nil
	}
	return &serdeTuple{FormatTag: formatTag, Data: data}
}

// recoverFromInputs 从交互输入恢复工作流状态。
// 对应 Python: WorkflowStorage._process_interactive_inputs
func (ws *PersistenceWorkflowStorage) recoverFromInputs(session CheckpointerSession, inputs any) {
	if inputs == nil {
		return
	}

	// 通过类型断言获取 WorkflowState 接口
	wfState, ok := session.State().(state.WorkflowState)
	if !ok || wfState == nil {
		// 非 WorkflowState 类型，直接更新 session state
		if inputMap, ok := inputs.(map[string]any); ok {
			session.State().Update(inputMap)
		}
		return
	}

	// 简化版：将 inputs 作为交互输入处理
	if inputMap, ok := inputs.(map[string]any); ok {
		wfState.UpdateAndCommitWorkflowState(inputMap)
	} else {
		wfState.UpdateAndCommitWorkflowState(map[string]any{InteractiveInputKey: []any{inputs}})
	}
}

// ──────────────────────────── PersistenceCheckpointer 方法 ────────────────────────────

// GetThreadID 获取线程 ID（session_id:workflow_id）。
func (cp *PersistenceCheckpointer) GetThreadID(session CheckpointerSession) string {
	return GetThreadID(session)
}

// PreAgentExecute Agent 执行前恢复状态。
// 对应 Python: PersistenceCheckpointer.pre_agent_execute()
func (cp *PersistenceCheckpointer) PreAgentExecute(ctx context.Context, session CheckpointerSession, inputs any) error {
	agentID := GetAgentID(session)
	sessionID := session.SessionID()

	logger.Info(logComponent).
		Str("action", "pre_agent_execute").
		Str("event_type", "checkpoint_restore").
		Str("session_id", sessionID).
		Str("agent_id", agentID).
		Str("storage_type", "persistence").
		Msg("开始恢复 Agent 会话")

	if err := cp.agentStorage.Recover(ctx, session, nil); err != nil {
		logger.Error(logComponent).Err(err).
			Str("session_id", sessionID).
			Str("agent_id", agentID).
			Msg("恢复 Agent 会话失败")
		return err
	}

	// 如果有交互输入，设置到 session state
	if inputs != nil {
		if st := session.State(); st != nil {
			st.Update(map[string]any{InteractiveInputKey: []any{inputs}})
		}
	}
	return nil
}

// PreAgentTeamExecute AgentTeam 执行前恢复状态。
// 对应 Python: PersistenceCheckpointer.pre_agent_team_execute()
func (cp *PersistenceCheckpointer) PreAgentTeamExecute(ctx context.Context, session CheckpointerSession, inputs any) error {
	teamID := GetTeamID(session)
	sessionID := session.SessionID()

	logger.Info(logComponent).
		Str("action", "pre_agent_team_execute").
		Str("event_type", "checkpoint_restore").
		Str("session_id", sessionID).
		Str("workflow_id", teamID).
		Str("storage_type", "persistence").
		Msg("开始恢复 AgentTeam 会话")

	if err := cp.agentTeamStorage.Recover(ctx, session, nil); err != nil {
		logger.Error(logComponent).Err(err).
			Str("session_id", sessionID).
			Str("workflow_id", teamID).
			Msg("恢复 AgentTeam 会话失败")
		return err
	}

	// 如果有交互输入，更新全局状态
	if inputs != nil {
		if st := session.State(); st != nil {
			st.UpdateGlobal(map[string]any{InteractiveInputKey: []any{inputs}})
		}
	}
	return nil
}

// InterruptAgentExecute Agent 中断时保存检查点。
// 对应 Python: PersistenceCheckpointer.interrupt_agent_execute()
func (cp *PersistenceCheckpointer) InterruptAgentExecute(ctx context.Context, session CheckpointerSession) error {
	agentID := GetAgentID(session)
	sessionID := session.SessionID()

	logger.Info(logComponent).
		Str("action", "interrupt_agent_execute").
		Str("event_type", "checkpoint_save").
		Str("session_id", sessionID).
		Str("agent_id", agentID).
		Str("reason", "interaction_required").
		Str("storage_type", "persistence").
		Msg("Agent 中断时开始保存检查点")

	if err := cp.agentStorage.Save(ctx, session); err != nil {
		logger.Error(logComponent).Err(err).
			Str("action", "interrupt_agent_execute").
			Str("event_type", "checkpoint_save").
			Str("session_id", sessionID).
			Str("agent_id", agentID).
			Str("storage_type", "persistence").
			Msg("Agent 中断时保存检查点失败")
		return err
	}
	return nil
}

// PostAgentExecute Agent 执行后保存检查点。
// 对应 Python: PersistenceCheckpointer.post_agent_execute()
func (cp *PersistenceCheckpointer) PostAgentExecute(ctx context.Context, session CheckpointerSession) error {
	agentID := GetAgentID(session)
	sessionID := session.SessionID()

	logger.Info(logComponent).
		Str("action", "post_agent_execute").
		Str("event_type", "checkpoint_save").
		Str("session_id", sessionID).
		Str("agent_id", agentID).
		Str("reason", "agent_finished").
		Str("storage_type", "persistence").
		Msg("Agent 执行完成后开始保存检查点")

	if err := cp.agentStorage.Save(ctx, session); err != nil {
		logger.Error(logComponent).Err(err).
			Str("action", "post_agent_execute").
			Str("event_type", "checkpoint_save").
			Str("session_id", sessionID).
			Str("agent_id", agentID).
			Str("storage_type", "persistence").
			Msg("Agent 执行完成后保存检查点失败")
		return err
	}
	return nil
}

// PostAgentTeamExecute AgentTeam 执行后保存检查点。
// 对应 Python: PersistenceCheckpointer.post_agent_team_execute()
func (cp *PersistenceCheckpointer) PostAgentTeamExecute(ctx context.Context, session CheckpointerSession) error {
	teamID := GetTeamID(session)
	sessionID := session.SessionID()

	logger.Info(logComponent).
		Str("action", "post_agent_team_execute").
		Str("event_type", "checkpoint_save").
		Str("session_id", sessionID).
		Str("workflow_id", teamID).
		Str("reason", "team_finished").
		Str("storage_type", "persistence").
		Msg("AgentTeam 执行完成后开始保存检查点")

	if err := cp.agentTeamStorage.Save(ctx, session); err != nil {
		logger.Error(logComponent).Err(err).
			Str("action", "post_agent_team_execute").
			Str("event_type", "checkpoint_save").
			Str("session_id", sessionID).
			Str("workflow_id", teamID).
			Str("storage_type", "persistence").
			Msg("AgentTeam 执行完成后保存检查点失败")
		return err
	}
	return nil
}

// PreWorkflowExecute 工作流执行前处理检查点。
// 对应 Python: PersistenceCheckpointer.pre_workflow_execute()
func (cp *PersistenceCheckpointer) PreWorkflowExecute(ctx context.Context, session CheckpointerSession, inputs any) error {
	workflowID := session.WorkflowID()
	sessionID := session.SessionID()

	logger.Info(logComponent).
		Str("action", "pre_workflow_execute").
		Str("event_type", "checkpoint_restore").
		Str("session_id", sessionID).
		Str("workflow_id", workflowID).
		Str("storage_type", "persistence").
		Msg("开始恢复工作流会话")

	if isInteractiveInput(inputs) {
		if err := cp.workflowStorage.Recover(ctx, session, inputs); err != nil {
			logger.Error(logComponent).Err(err).
				Str("session_id", sessionID).
				Str("workflow_id", workflowID).
				Msg("恢复工作流会话失败")
			return err
		}
	} else {
		// 检查工作流状态是否存在
		exists, err := cp.workflowStorage.Exists(ctx, session)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		// 检查是否强制删除工作流状态
		if session.Config() != nil {
			if forceDel, _ := session.Config().GetEnv(ForceDelWorkflowStateKey, false).(bool); forceDel {
				logger.Info(logComponent).
					Str("action", "pre_workflow_execute").
					Str("event_type", "checkpoint_clear").
					Str("session_id", sessionID).
					Str("workflow_id", workflowID).
					Str("storage_type", "persistence").
					Msg("强制清除当前工作流所有检查点")

				// ⤵️ 8.7 回填：Graph Store 实现后添加 graphStore.Delete(sessionID, workflowID)

				if err := cp.workflowStorage.Clear(ctx, workflowID, sessionID); err != nil {
					return err
				}
				return nil
			}
		}
		return fmt.Errorf("checkpointer pre workflow execution error: session_id=%s, workflow=%s, reason=workflow state exists but non-interactive input and cleanup is disabled",
			sessionID, workflowID)
	}
	return nil
}

// PostWorkflowExecute 工作流执行后处理检查点。
// 对应 Python: PersistenceCheckpointer.post_workflow_execute()
func (cp *PersistenceCheckpointer) PostWorkflowExecute(ctx context.Context, session CheckpointerSession, result any, exception error) error {
	sessionID := session.SessionID()
	workflowID := session.WorkflowID()

	if exception != nil {
		logger.Info(logComponent).
			Str("action", "post_workflow_execute").
			Str("event_type", "checkpoint_save").
			Str("session_id", sessionID).
			Str("workflow_id", workflowID).
			Str("reason", "exception").
			Str("storage_type", "persistence").
			Msg("工作流异常时保存检查点")
		if err := cp.workflowStorage.Save(ctx, session); err != nil {
			return err
		}
		return exception
	}

	// 检查结果中是否有中断标记
	isInterrupted := isWorkflowInterrupted(result)

	if !isInterrupted {
		// 工作流正常完成，清除检查点
		logger.Info(logComponent).
			Str("action", "post_workflow_execute").
			Str("event_type", "checkpoint_clear").
			Str("session_id", sessionID).
			Str("workflow_id", workflowID).
			Str("reason", "workflow_completed").
			Str("storage_type", "persistence").
			Msg("工作流正常完成，清除检查点")

		// ⤵️ 8.7 回填：Graph Store 实现后添加 graphStore.Delete(sessionID, workflowID)

		if err := cp.workflowStorage.Clear(ctx, workflowID, sessionID); err != nil {
			return err
		}
	} else {
		// 工作流中断，保存检查点
		logger.Info(logComponent).
			Str("action", "post_workflow_execute").
			Str("event_type", "checkpoint_save").
			Str("session_id", sessionID).
			Str("workflow_id", workflowID).
			Str("reason", "interaction_required").
			Str("storage_type", "persistence").
			Msg("工作流中断时保存检查点")
		if err := cp.workflowStorage.Save(ctx, session); err != nil {
			return err
		}
	}
	return nil
}

// SessionExists 检查会话是否存在。
// 对应 Python: PersistenceCheckpointer.session_exists()
func (cp *PersistenceCheckpointer) SessionExists(ctx context.Context, sessionID string) (bool, error) {
	if cp.kvStore == nil {
		return false, nil
	}
	// 检查是否有以 sessionID + ":" 为前缀的 key
	prefix := sessionID + ":"
	kvs, err := cp.kvStore.GetByPrefix(ctx, prefix)
	if err != nil {
		return false, err
	}
	return len(kvs) > 0, nil
}

// Release 释放会话资源。
// 对应 Python: PersistenceCheckpointer.release()
func (cp *PersistenceCheckpointer) Release(ctx context.Context, sessionID string) error {
	if cp.kvStore == nil {
		logger.Warn(logComponent).
			Str("event_type", "checkpoint_error").
			Str("session_id", sessionID).
			Str("metadata_operation", "release").
			Msg("KV store 为 nil，无法释放资源")
		return nil
	}

	logger.Info(logComponent).
		Str("action", "release").
		Str("event_type", "checkpoint_clear").
		Str("session_id", sessionID).
		Str("storage_type", "persistence").
		Msg("开始清除会话的所有检查点")

	prefix := sessionID + ":"
	if err := cp.kvStore.DeleteByPrefix(ctx, prefix, 0); err != nil {
		return err
	}

	logger.Debug(logComponent).
		Str("action", "release").
		Str("event_type", "checkpoint_clear").
		Str("session_id", sessionID).
		Str("storage_type", "persistence").
		Msg("所有会话资源已释放")
	return nil
}

// GraphStore 获取图状态存储。
// ⤵️ 8.7 回填
func (cp *PersistenceCheckpointer) GraphStore() any {
	return cp.graphStore
}

// ──────────────────────────── persistenceProvider 方法 ────────────────────────────

// Create 创建 Persistence 检查点器。
// 对应 Python: PersistenceCheckpointerProvider.create()
func (p *persistenceProvider) Create(ctx context.Context, conf map[string]any) (Checkpointer, error) {
	dbPath := "checkpointer"
	if v, ok := conf["db_path"]; ok {
		if s, ok := v.(string); ok {
			dbPath = s
		}
	}
	if !strings.HasSuffix(dbPath, ".db") {
		dbPath = dbPath + ".db"
	}
	// 确保父目录存在
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	// 使用 GORM + SQLite 创建 DbBasedKVStore
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	kvStore := kv.NewDbBasedKVStore(db)
	return NewPersistenceCheckpointer(kvStore), nil
}
