package checkpointer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EntityHooks 单实体状态存储的钩子接口。
// 对应 Python: BaseSingleStateStorage 的 _get_entity_id/_get_state_to_save/_restore_state
// Go 不支持虚方法分派，通过接口注入实现模板方法模式。
type EntityHooks interface {
	// GetEntityID 获取实体 ID（Agent 返回 agentID，AgentTeam 返回 teamID）
	GetEntityID(session interfaces.InnerSession) string
	// GetStateToSave 获取需要保存的状态
	GetStateToSave(session interfaces.InnerSession) any
	// RestoreState 将恢复的状态设置回 session，返回 error 对齐 Python _restore_state 的异常传播
	RestoreState(session interfaces.InnerSession, savedState any) error
}

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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// keyNums 单实体存储的 key 数量（dumpType + blob）
	keyNums = 2
	// wfKeyNums 工作流存储的 key 数量（state×2 + updates×2），对齐 Python _KEY_NUMS = 4
	wfKeyNums = 4

	// wfStateBlobs 工作流状态数据键后缀
	wfStateBlobs = "workflow_state_blobs"
	// wfStateBlobsDumpType 工作流状态类型键后缀
	wfStateBlobsDumpType = "workflow_state_blobs_dump_type"
	// wfUpdateBlobs 工作流更新数据键后缀
	wfUpdateBlobs = "workflow_update_blobs"
	// wfUpdateBlobsDumpType 工作流更新类型键后缀
	wfUpdateBlobsDumpType = "workflow_update_blobs_dump_type"
)

// ──────────────────────────── 全局变量 ────────────────────────────

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

// GetEntityID 实现 EntityHooks 接口。
func (h *agentEntityHooks) GetEntityID(session interfaces.InnerSession) string {
	return GetAgentID(session)
}

// GetStateToSave 实现 EntityHooks 接口。
func (h *agentEntityHooks) GetStateToSave(session interfaces.InnerSession) any {
	if session.State() == nil {
		return nil
	}
	return session.State().GetState()
}

// RestoreState 实现 EntityHooks 接口。
// 对齐 Python: AgentStorage._restore_state → session.state().set_state(state)
// 返回 error 对齐 Python 的 try/except + raise 异常传播
func (h *agentEntityHooks) RestoreState(session interfaces.InnerSession, savedState any) error {
	if session.State() == nil || savedState == nil {
		return nil
	}
	st, ok := savedState.(map[string]any)
	if !ok {
		return fmt.Errorf("恢复 Agent 状态失败: savedState 类型错误，期望 map[string]any，实际 %T", savedState)
	}
	session.State().SetState(st)
	return nil
}

// GetEntityID 实现 EntityHooks 接口。
func (h *agentTeamEntityHooks) GetEntityID(session interfaces.InnerSession) string {
	return GetTeamID(session)
}

// GetStateToSave 实现 EntityHooks 接口。
// 对齐 Python: AgentTeamStorage._get_state_to_save → session.state().get_global(None)
func (h *agentTeamEntityHooks) GetStateToSave(session interfaces.InnerSession) any {
	if session.State() == nil {
		return nil
	}
	return session.State().GetGlobal(state.AllStateKey)
}

// RestoreState 实现 EntityHooks 接口。
// 对齐 Python: AgentTeamStorage._restore_state → session.state().global_state.set_state(state)
// 返回 error 对齐 Python 的 try/except + raise 异常传播
func (h *agentTeamEntityHooks) RestoreState(session interfaces.InnerSession, savedState any) error {
	if session.State() == nil || savedState == nil {
		return nil
	}
	st, ok := savedState.(map[string]any)
	if !ok {
		return fmt.Errorf("恢复 AgentTeam 状态失败: savedState 类型错误，期望 map[string]any，实际 %T", savedState)
	}
	session.State().SetGlobal(st)
	return nil
}

// Save 保存会话状态到 KVStore。
// 对应 Python: BaseSingleStateStorage.save()
func (s *basePersistenceStorage) Save(ctx context.Context, session interfaces.InnerSession) error {
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
func (s *basePersistenceStorage) Recover(ctx context.Context, session interfaces.InnerSession, _ any) error {
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

	if err := s.hooks.RestoreState(session, loadedState); err != nil {
		// 对齐 Python: except Exception as e: session_logger.error(...) + raise
		logger.Error(logComponent).Err(err).
			Str("event_type", "checkpoint_restore").
			Str("session_id", sessionID).
			Str(s.entityLogExtraKey(), entityID).
			Str("storage_type", "persistence").
			Msgf("设置 %s 状态失败", s.entityLabel)
		return err
	}
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
	deletedKeys := []string{dumpTypeKey, blobKey}
	deletedCount, err := s.kvStore.BatchDelete(ctx, deletedKeys, 0)
	if err != nil {
		return err
	}
	logger.Debug(logComponent).
		Str("event_type", "checkpoint_clear").
		Str("session_id", sessionID).
		Str(s.entityLogExtraKey(), entityID).
		Str("storage_type", "persistence").
		Int("deleted_keys", deletedCount).
		Msgf("%s 检查点已清除", s.entityLabel)
	return nil
}

// Exists 检查 KVStore 中是否存在会话状态。
func (s *basePersistenceStorage) Exists(ctx context.Context, session interfaces.InnerSession) (bool, error) {
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

// Save 保存工作流状态到 KVStore。
// 对应 Python: WorkflowStorage.save()
func (ws *PersistenceWorkflowStorage) Save(ctx context.Context, session interfaces.InnerSession) error {
	workflowID := getWorkflowID(session)
	sessionID := session.SessionID()

	// 防御性检查：session.State() 为 nil 时直接返回，避免后续操作 panic
	if session.State() == nil {
		return nil
	}

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
func (ws *PersistenceWorkflowStorage) Recover(ctx context.Context, session interfaces.InnerSession, inputs any) error {
	workflowID := getWorkflowID(session)
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
			if st, ok := loadedState.(map[string]any); ok && session.State() != nil {
				session.State().SetState(st)
			}
		}
	}

	// 处理交互输入
	// 对齐 Python: if inputs is not None: self._process_interactive_inputs(session, inputs)
	if ii, ok := inputs.(*interaction.InteractiveInput); ok {
		ws.processInteractiveInputs(session, ii)
	}

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
	deletedCount, err := ws.kvStore.BatchDelete(ctx, keys, 0)
	if err != nil {
		return err
	}
	logger.Debug(logComponent).
		Str("event_type", "checkpoint_clear").
		Str("session_id", sessionID).
		Str("workflow_id", workflowID).
		Str("storage_type", "persistence").
		Int("deleted_keys", deletedCount).
		Msg("工作流检查点已清除")
	return nil
}

// Exists 检查工作流状态是否存在。
func (ws *PersistenceWorkflowStorage) Exists(ctx context.Context, session interfaces.InnerSession) (bool, error) {
	workflowID := getWorkflowID(session)
	sessionID := session.SessionID()

	pipeline := ws.kvStore.Pipeline(ctx)
	stateDumpTypeKey := BuildKeyWithNamespace(sessionID, SessionNamespaceWorkflow, workflowID, wfStateBlobsDumpType)
	stateBlobKey := BuildKeyWithNamespace(sessionID, SessionNamespaceWorkflow, workflowID, wfStateBlobs)
	updateDumpTypeKey := BuildKeyWithNamespace(sessionID, SessionNamespaceWorkflow, workflowID, wfUpdateBlobsDumpType)
	updateBlobKey := BuildKeyWithNamespace(sessionID, SessionNamespaceWorkflow, workflowID, wfUpdateBlobs)
	_ = pipeline.Exists(ctx, stateDumpTypeKey)
	_ = pipeline.Exists(ctx, stateBlobKey)
	_ = pipeline.Exists(ctx, updateDumpTypeKey)
	_ = pipeline.Exists(ctx, updateBlobKey)
	results, err := pipeline.Execute(ctx)
	if err != nil {
		return false, err
	}

	// 对齐 Python _KEY_NUMS = 4（state×2 + updates×2）
	if len(results) != wfKeyNums {
		return false, nil
	}
	// Python: 只要求 state key 存在，updates key 为可选
	exists0, _ := pipelineExistsResult(results, 0)
	exists1, _ := pipelineExistsResult(results, 1)
	return exists0 && exists1, nil
}

// PreAgentExecute Agent 执行前恢复状态。
// 对应 Python: PersistenceCheckpointer.pre_agent_execute()
func (cp *PersistenceCheckpointer) PreAgentExecute(ctx context.Context, session interfaces.InnerSession, inputs any) error {
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
			if err := st.Update(map[string]any{constants.InteractiveInputKey: []any{inputs}}); err != nil {
				logger.Warn(logComponent).Err(err).
					Str("session_id", session.SessionID()).
					Msg("设置交互输入到 session state 失败")
			}
		}
	}
	return nil
}

// PreAgentTeamExecute AgentTeam 执行前恢复状态。
// 对应 Python: PersistenceCheckpointer.pre_agent_team_execute()
func (cp *PersistenceCheckpointer) PreAgentTeamExecute(ctx context.Context, session interfaces.InnerSession, inputs any) error {
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
			st.UpdateGlobal(map[string]any{constants.InteractiveInputKey: []any{inputs}})
		}
	}
	return nil
}

// InterruptAgentExecute Agent 中断时保存检查点。
// 对应 Python: PersistenceCheckpointer.interrupt_agent_execute()
func (cp *PersistenceCheckpointer) InterruptAgentExecute(ctx context.Context, session interfaces.InnerSession) error {
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
func (cp *PersistenceCheckpointer) PostAgentExecute(ctx context.Context, session interfaces.InnerSession) error {
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
func (cp *PersistenceCheckpointer) PostAgentTeamExecute(ctx context.Context, session interfaces.InnerSession) error {
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
func (cp *PersistenceCheckpointer) PreWorkflowExecute(ctx context.Context, session interfaces.InnerSession, inputs any) error {
	workflowID := getWorkflowID(session)
	sessionID := session.SessionID()

	logger.Info(logComponent).
		Str("action", "pre_workflow_execute").
		Str("event_type", "checkpoint_process").
		Str("session_id", sessionID).
		Str("workflow_id", workflowID).
		Str("storage_type", "persistence").
		Msg("开始处理工作流执行前检查点")

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
		if forceDel, ok := GetConfigEnv(session, constants.ForceDelWorkflowStateKey, false); ok {
			if forceDelBool, _ := forceDel.(bool); forceDelBool {
				logger.Info(logComponent).
					Str("action", "pre_workflow_execute").
					Str("event_type", "checkpoint_clear").
					Str("session_id", sessionID).
					Str("workflow_id", workflowID).
					Str("storage_type", "persistence").
					Msg("强制清除当前工作流所有检查点")

				// 对齐 Python: if workflow_id is None: logger.warning(...) return
				if workflowID == "" {
					logger.Warn(logComponent).
						Str("event_type", "checkpoint_error").
						Str("session_id", sessionID).
						Str("storage_type", "persistence").
						Msg("Workflow ID 为空，跳过状态清理")
					return nil
				}

				// ⤵️ 8.7 回填：Graph Store 实现后添加 graphStore.Delete(sessionID, workflowID)

				if err := cp.workflowStorage.Clear(ctx, workflowID, sessionID); err != nil {
					return err
				}
				return nil
			}
		}
		return fmt.Errorf("检查点器工作流执行前错误: session_id=%s, workflow=%s, 原因=工作流状态已存在但非交互输入且未启用清理",
			sessionID, workflowID)
	}
	return nil
}

// PostWorkflowExecute 工作流执行后处理检查点。
// 对应 Python: PersistenceCheckpointer.post_workflow_execute()
func (cp *PersistenceCheckpointer) PostWorkflowExecute(ctx context.Context, session interfaces.InnerSession, result any, exception error) error {
	sessionID := session.SessionID()
	workflowID := getWorkflowID(session)

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
// agentID 非空时仅释放指定 Agent 的持久化检查点（支持多个，循环清除）；为空时释放整个会话。
func (cp *PersistenceCheckpointer) Release(ctx context.Context, sessionID string, agentID ...string) error {
	if len(agentID) > 0 {
		// 循环清除每个指定 Agent 的持久化检查点
		logger.Info(logComponent).
			Str("action", "release").
			Str("event_type", "checkpoint_clear").
			Str("session_id", sessionID).
			Str("storage_type", "persistence").
			Strs("agent_ids", agentID).
			Msg("开始清除指定 Agent 的检查点")
		var firstErr error
		for _, aid := range agentID {
			if err := cp.agentStorage.Clear(ctx, aid, sessionID); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	}
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

// Create 创建 Persistence 检查点器。
// 对应 Python: PersistenceCheckpointerProvider.create()
//
// 配置项（对齐 Python conf 字典）：
//   - db_type:   存储后端类型，当前仅支持 "sqlite"（默认 "sqlite"），Python 额外支持 "shelve"
//   - db_path:   数据库文件路径（默认 "checkpointer"）
//   - db_client: 预配置的 *gorm.DB 实例（可选，提供时跳过自动创建）
//   - db_timeout: SQLite 锁等待秒数（默认 5，对齐 Python 默认 30 秒）
//   - db_enable_wal: 是否启用 SQLite WAL 模式（默认 true）
func (p *persistenceProvider) Create(ctx context.Context, conf map[string]any) (Checkpointer, error) {
	// db_type：当前仅支持 sqlite
	dbType := "sqlite"
	if v, ok := conf["db_type"]; ok {
		if s, ok := v.(string); ok && s != "" {
			dbType = s
		}
	}
	if dbType != "sqlite" {
		return nil, fmt.Errorf("不支持的数据库类型: %s（当前仅支持 sqlite）", dbType)
	}

	// db_client：优先使用预配置的 *gorm.DB
	if v, ok := conf["db_client"]; ok && v != nil {
		if db, ok := v.(*gorm.DB); ok {
			logger.Info(logComponent).
				Str("action", "persistence_provider_create").
				Str("db_type", dbType).
				Bool("db_client_provided", true).
				Msg("使用预配置的数据库客户端创建检查点器")
			kvStore := kv.NewDbBasedKVStore(db)
			return NewPersistenceCheckpointer(kvStore), nil
		}
	}

	// db_path：数据库文件路径
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

	// db_timeout：SQLite 锁等待秒数（默认 5）
	dbTimeout := 5
	if v, ok := conf["db_timeout"]; ok {
		switch t := v.(type) {
		case int:
			dbTimeout = t
		case float64:
			dbTimeout = int(t)
		}
	}

	// db_enable_wal：是否启用 WAL 模式（默认 true）
	dbEnableWAL := true
	if v, ok := conf["db_enable_wal"]; ok {
		if b, ok := v.(bool); ok {
			dbEnableWAL = b
		}
	}

	// 使用 GORM + SQLite 创建 DbBasedKVStore
	dsn := fmt.Sprintf("%s?_busy_timeout=%d", dbPath, dbTimeout*1000)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// 启用 WAL 模式
	if dbEnableWAL {
		sqlDB, err := db.DB()
		if err == nil {
			_, _ = sqlDB.Exec("PRAGMA journal_mode=WAL")
		}
	}

	logger.Info(logComponent).
		Str("action", "persistence_provider_create").
		Str("db_type", dbType).
		Str("db_path", dbPath).
		Int("db_timeout", dbTimeout).
		Bool("db_enable_wal", dbEnableWAL).
		Msg("创建持久化检查点器")

	kvStore := kv.NewDbBasedKVStore(db)
	return NewPersistenceCheckpointer(kvStore), nil
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

// serializeState 序列化状态，返回 serdeTuple。
func (ws *PersistenceWorkflowStorage) serializeState(st any) *serdeTuple {
	formatTag, data, err := ws.serde.DumpsTyped(st)
	if err != nil {
		return nil
	}
	return &serdeTuple{FormatTag: formatTag, Data: data}
}

// processInteractiveInputs 处理交互输入并更新工作流状态。
// 委托给公共函数 processInteractiveInputs，消除代码重复（CP-25）。
func (ws *PersistenceWorkflowStorage) processInteractiveInputs(session interfaces.InnerSession, inputs *interaction.InteractiveInput) {
	processInteractiveInputs(session, inputs)
}
