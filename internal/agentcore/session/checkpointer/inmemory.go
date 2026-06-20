package checkpointer

import (
	"context"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InMemoryCheckpointer 内存检查点器，所有状态存储在进程内存中。
// 对应 Python: openjiuwen/core/session/checkpointer/inmemory.py (InMemoryCheckpointer)
type InMemoryCheckpointer struct {
	// mu 并发读写锁
	mu sync.RWMutex
	// agentStores Agent 状态存储，key=sessionID
	agentStores map[string]*AgentStorage
	// agentTeamStores AgentTeam 状态存储，key=sessionID
	agentTeamStores map[string]*AgentTeamStorage
	// workflowStores Workflow 状态存储，key=sessionID
	workflowStores map[string]*WorkflowStorage
	// sessionToWorkflowIDs 会话到工作流 ID 集合的映射，key=sessionID
	sessionToWorkflowIDs map[string]map[string]bool
	// graphStore 图状态存储
	// ⤵️ 8.7 回填：Graph Store 实现后替换为 Store 实例
	graphStore any
}

// baseSingleStateStorage 单实体状态存储基类，提供基于 serde 的序列化存储。
// 对应 Python: openjiuwen/core/session/checkpointer/inmemory.py (BaseSingleStateStorage)
//
// 设计说明：Python 中 BaseSingleStateStorage 定义了 save/recover/clear/exists 固定骨架，
// 子类只需实现 _get_entity_id/_get_state_to_save/_restore_state 三个钩子方法。
// Go 的 struct embedding 不支持虚方法分派（嵌入方法永远调基类版本），
// 当前子类直接实现全部接口方法 + 基类仅提供 setBlob/getBlob 等辅助方法。
// 5.9 PersistenceCheckpointer 的 Storage 体系应采用接口注入钩子模式
// （baseSingleStateStorage 持有 entityHooks 接口，构造时注入），与 Python 模板方法对齐。
// 本 InMemory 版的 Storage 是独立实现，5.9 不需要回填此处。
type baseSingleStateStorage struct {
	// mu 并发读写锁
	mu sync.RWMutex
	// stateBlobs 状态序列化数据，key=entityID
	stateBlobs map[string]serdeTuple
	// serde 序列化器
	serde Serializer
}

// AgentStorage Agent 状态存储。
// 对应 Python: openjiuwen/core/session/checkpointer/inmemory.py (AgentStorage)
type AgentStorage struct {
	baseSingleStateStorage
}

// AgentTeamStorage AgentTeam 状态存储。
// 对应 Python: openjiuwen/core/session/checkpointer/inmemory.py (AgentTeamStorage)
type AgentTeamStorage struct {
	baseSingleStateStorage
}

// WorkflowStorage Workflow 状态存储，独立于 baseSingleStateStorage，
// 因为需要同时保存 state 和 updates 两类数据。
// 对应 Python: openjiuwen/core/session/checkpointer/inmemory.py (WorkflowStorage)
type WorkflowStorage struct {
	// mu 并发读写锁
	mu sync.RWMutex
	// serde 序列化器
	serde Serializer
	// stateBlobs 工作流状态序列化数据，key=workflowID
	stateBlobs map[string]serdeTuple
	// stateUpdatesBlobs 工作流状态更新序列化数据，key=workflowID
	stateUpdatesBlobs map[string]serdeTuple
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ForceDelWorkflowStateKey 强制删除工作流状态的环境变量键。
	// 对应 Python: openjiuwen/core/session/constants.py (FORCE_DEL_WORKFLOW_STATE_KEY)
	// 5.13 实现 session/constants 包后，此常量应迁移到该包中。
	ForceDelWorkflowStateKey = "_force_del_workflow_state"

	// InteractiveInputKey 交互输入在 session state 中的键。
	// 对应 Python: openjiuwen/core/common/constants/constant.py (INTERACTIVE_INPUT)
	// 此常量与 interaction 包中的 InteractiveInputKey 值一致，
	// 5.13 实现 session/constants 包后，两处引用应统一到该包中。
	InteractiveInputKey = "__interactive_input__"

	// emptyFormatTag 空状态标记，用于 WorkflowStorage.exists 判断
	emptyFormatTag = "empty"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件标识
var logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryCheckpointer 创建内存检查点器实例。
func NewInMemoryCheckpointer() *InMemoryCheckpointer {
	logger.Info(logComponent).
		Str("action", "new_inmemory_checkpointer").
		Msg("创建内存检查点器")
	return &InMemoryCheckpointer{
		agentStores:          make(map[string]*AgentStorage),
		agentTeamStores:      make(map[string]*AgentTeamStorage),
		workflowStores:       make(map[string]*WorkflowStorage),
		sessionToWorkflowIDs: make(map[string]map[string]bool),
		graphStore:           nil, // ⤵️ 8.7 回填
	}
}

// PreWorkflowExecute 工作流执行前保存检查点。
// 对应 Python: InMemoryCheckpointer.pre_workflow_execute()
func (cp *InMemoryCheckpointer) PreWorkflowExecute(ctx context.Context, session interfaces.BaseSession, inputs any) error {
	sessionID := session.SessionID()
	workflowID := getWorkflowID(session)

	cp.mu.Lock()
	isNewWorkflowStore := sessionID != "" && cp.workflowStores[sessionID] == nil
	workflowStore, exists := cp.workflowStores[sessionID]
	if !exists {
		workflowStore = newWorkflowStorage()
		cp.workflowStores[sessionID] = workflowStore
	}
	if _, ok := cp.sessionToWorkflowIDs[sessionID]; !ok {
		cp.sessionToWorkflowIDs[sessionID] = make(map[string]bool)
	}
	cp.mu.Unlock()

	if isNewWorkflowStore {
		logger.Info(logComponent).
			Str("action", "pre_workflow_execute").
			Str("event_type", "checkpointer_store_add").
			Str("session_id", sessionID).
			Str("workflow_id", workflowID).
			Str("storage_type", "inmemory").
			Msg("创建新的工作流检查点存储")
	}

	// 判断 inputs 是否为交互输入（非 nil 且非空 map）
	isInteractiveInput := isInteractiveInput(inputs)

	if isInteractiveInput {
		logger.Info(logComponent).
			Str("action", "pre_workflow_execute").
			Str("event_type", "checkpoint_restore").
			Str("session_id", sessionID).
			Str("workflow_id", workflowID).
			Str("storage_type", "inmemory").
			Msg("开始恢复工作流会话")

		if err := workflowStore.Recover(ctx, session, inputs); err != nil {
			logger.Error(logComponent).
				Err(err).
				Str("action", "pre_workflow_execute").
				Str("session_id", sessionID).
				Str("workflow_id", workflowID).
				Msg("恢复工作流会话失败")
			return err
		}

		logger.Info(logComponent).
			Str("action", "pre_workflow_execute").
			Str("event_type", "checkpoint_restore").
			Str("session_id", sessionID).
			Str("workflow_id", workflowID).
			Str("storage_type", "inmemory").
			Msg("成功恢复工作流会话")
	} else {
		exists, err := workflowStore.Exists(ctx, session)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		// 检查是否强制删除工作流状态
		if forceDel, ok := GetConfigEnv(session, ForceDelWorkflowStateKey, false); ok {
			if forceDelBool, _ := forceDel.(bool); forceDelBool {
				logger.Info(logComponent).
					Str("action", "pre_workflow_execute").
					Str("event_type", "checkpoint_clear").
					Str("session_id", sessionID).
					Str("workflow_id", workflowID).
					Str("storage_type", "inmemory").
					Msg("强制清除当前工作流所有检查点")

				// ⤵️ 8.7 回填：Graph Store 实现后添加 graphStore.Delete(sessionID, workflowID)
				func() {
					// 预留 graph store delete 逻辑
				}()

				// 无论 graph store 是否成功，都清除 workflow store
				if err := workflowStore.Clear(ctx, workflowID, sessionID); err != nil {
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
// 对应 Python: InMemoryCheckpointer.post_workflow_execute()
func (cp *InMemoryCheckpointer) PostWorkflowExecute(ctx context.Context, session interfaces.BaseSession, result any, exception error) error {
	sessionID := session.SessionID()
	workflowID := getWorkflowID(session)

	cp.mu.RLock()
	workflowStore := cp.workflowStores[sessionID]
	cp.mu.RUnlock()

	if exception != nil {
		if workflowStore == nil {
			return fmt.Errorf("检查点器工作流执行后错误: workflow=%s, 原因=工作流存储未找到", workflowID)
		}
		if err := cp.innerSaveWorkflowCheckpoint(ctx, workflowID, sessionID, session, fmt.Sprintf("workflow exception %v", exception)); err != nil {
			return err
		}
		return exception
	}

	// 检查结果中是否有中断标记
	// Python: result.get(TASK_STATUS_INTERRUPT) is None
	isInterrupted := isWorkflowInterrupted(result)

	if !isInterrupted {
		// 工作流正常完成，清除检查点
		// 清理和删除在同一锁区间内完成，避免中间状态被并发访问
		cp.mu.Lock()
		// 清除工作流会话（使用不加锁版本，因为外层已持锁）
		if err := cp.innerClearWorkflowSessionLocked(ctx, workflowID, sessionID, "workflow execute completion"); err != nil {
			logger.Error(logComponent).Err(err).
				Str("session_id", sessionID).
				Str("workflow_id", workflowID).
				Msg("清除工作流会话失败")
		}

		// 如果没有父会话，移除 workflow store
		// 对齐 Python: if not isinstance(session.parent(), AgentSession)
		// 有 parent → 保留 store；无 parent → 删除 store
		hasParent := false
		if pp, ok := session.(ParentProvider); ok && pp.Parent() != nil {
			hasParent = true
		}
		if !hasParent {
			delete(cp.workflowStores, sessionID)
			delete(cp.sessionToWorkflowIDs, sessionID)
			logger.Info(logComponent).
				Str("action", "post_workflow_execute").
				Str("event_type", "checkpointer_store_remove").
				Str("session_id", sessionID).
				Str("workflow_id", workflowID).
				Str("storage_type", "inmemory").
				Msg("移除工作流检查点存储")
		}
		cp.mu.Unlock()
	} else {
		// 工作流中断，保存检查点
		if workflowStore == nil {
			return fmt.Errorf("检查点器工作流执行后错误: workflow=%s, 原因=工作流存储未找到", workflowID)
		}
		if err := cp.innerSaveWorkflowCheckpoint(ctx, workflowID, sessionID, session, "workflow interruption"); err != nil {
			return err
		}
	}
	return nil
}

// PreAgentExecute Agent 执行前恢复状态。
// 对应 Python: InMemoryCheckpointer.pre_agent_execute()
func (cp *InMemoryCheckpointer) PreAgentExecute(ctx context.Context, session interfaces.BaseSession, inputs any) error {
	agentID := GetAgentID(session)
	sessionID := session.SessionID()

	cp.mu.Lock()
	isNewAgentStore := cp.agentStores[sessionID] == nil
	agentStore, exists := cp.agentStores[sessionID]
	if !exists {
		agentStore = newAgentStorage()
		cp.agentStores[sessionID] = agentStore
	}
	cp.mu.Unlock()

	if isNewAgentStore {
		logger.Info(logComponent).
			Str("action", "pre_agent_execute").
			Str("event_type", "checkpointer_store_add").
			Str("session_id", sessionID).
			Str("agent_id", agentID).
			Str("storage_type", "inmemory").
			Msg("创建新的 Agent 检查点存储")
	}

	logger.Info(logComponent).
		Str("action", "pre_agent_execute").
		Str("event_type", "checkpoint_restore").
		Str("session_id", sessionID).
		Str("agent_id", agentID).
		Str("storage_type", "inmemory").
		Msg("开始恢复 Agent 会话")

	if err := agentStore.Recover(ctx, session, nil); err != nil {
		logger.Error(logComponent).Err(err).
			Str("session_id", sessionID).
			Str("agent_id", agentID).
			Msg("恢复 Agent 会话失败")
		return err
	}

	logger.Info(logComponent).
		Str("action", "pre_agent_execute").
		Str("event_type", "checkpoint_restore").
		Str("session_id", sessionID).
		Str("agent_id", agentID).
		Str("storage_type", "inmemory").
		Msg("成功恢复 Agent 会话")

	// 如果有交互输入，设置到 session state
	if inputs != nil {
		if st := session.State(); st != nil {
			if err := st.Update(map[string]any{InteractiveInputKey: []any{inputs}}); err != nil {
				logger.Warn(logComponent).Err(err).
					Str("session_id", session.SessionID()).
					Msg("设置交互输入到 session state 失败")
			}
		}
	}
	return nil
}

// PreAgentTeamExecute AgentTeam 执行前恢复状态。
// 对应 Python: InMemoryCheckpointer.pre_agent_team_execute()
func (cp *InMemoryCheckpointer) PreAgentTeamExecute(ctx context.Context, session interfaces.BaseSession, inputs any) error {
	teamID := GetTeamID(session)
	sessionID := session.SessionID()

	cp.mu.Lock()
	isNewTeamStore := cp.agentTeamStores[sessionID] == nil
	teamStore, exists := cp.agentTeamStores[sessionID]
	if !exists {
		teamStore = newAgentTeamStorage()
		cp.agentTeamStores[sessionID] = teamStore
	}
	cp.mu.Unlock()

	if isNewTeamStore {
		logger.Info(logComponent).
			Str("action", "pre_agent_team_execute").
			Str("event_type", "checkpointer_store_add").
			Str("session_id", sessionID).
			Str("workflow_id", teamID).
			Str("storage_type", "inmemory").
			Msg("创建新的 AgentTeam 检查点存储")
	}

	logger.Info(logComponent).
		Str("action", "pre_agent_team_execute").
		Str("event_type", "checkpoint_restore").
		Str("session_id", sessionID).
		Str("workflow_id", teamID).
		Str("storage_type", "inmemory").
		Msg("开始恢复 AgentTeam 会话")

	if err := teamStore.Recover(ctx, session, nil); err != nil {
		logger.Error(logComponent).Err(err).
			Str("session_id", sessionID).
			Str("workflow_id", teamID).
			Msg("恢复 AgentTeam 会话失败")
		return err
	}

	logger.Info(logComponent).
		Str("action", "pre_agent_team_execute").
		Str("event_type", "checkpoint_restore").
		Str("session_id", sessionID).
		Str("workflow_id", teamID).
		Str("storage_type", "inmemory").
		Msg("成功恢复 AgentTeam 会话")

	// 如果有交互输入，更新全局状态
	if inputs != nil {
		if st := session.State(); st != nil {
			st.UpdateGlobal(map[string]any{InteractiveInputKey: []any{inputs}})
		}
	}
	return nil
}

// InterruptAgentExecute Agent 中断时保存检查点。
// 对应 Python: InMemoryCheckpointer.interrupt_agent_execute()
func (cp *InMemoryCheckpointer) InterruptAgentExecute(ctx context.Context, session interfaces.BaseSession) error {
	agentID := GetAgentID(session)
	sessionID := session.SessionID()

	cp.mu.RLock()
	agentStore := cp.agentStores[sessionID]
	cp.mu.RUnlock()

	if agentStore == nil {
		return fmt.Errorf("检查点器中断 Agent 错误: agent=%s, 原因=Agent 存储未找到", agentID)
	}

	logger.Info(logComponent).
		Str("action", "interrupt_agent_execute").
		Str("event_type", "checkpoint_save").
		Str("session_id", sessionID).
		Str("agent_id", agentID).
		Str("storage_type", "inmemory").
		Msg("Agent 中断时开始保存检查点")

	if err := agentStore.Save(ctx, session); err != nil {
		logger.Error(logComponent).Err(err).
			Str("action", "interrupt_agent_execute").
			Str("event_type", "checkpoint_save").
			Str("session_id", sessionID).
			Str("agent_id", agentID).
			Str("storage_type", "inmemory").
			Msg("Agent 中断时保存检查点失败")
		return err
	}

	logger.Info(logComponent).
		Str("action", "interrupt_agent_execute").
		Str("event_type", "checkpoint_save").
		Str("session_id", sessionID).
		Str("agent_id", agentID).
		Str("storage_type", "inmemory").
		Msg("Agent 中断时成功保存检查点")
	return nil
}

// PostAgentExecute Agent 执行后保存检查点。
// 对应 Python: InMemoryCheckpointer.post_agent_execute()
func (cp *InMemoryCheckpointer) PostAgentExecute(ctx context.Context, session interfaces.BaseSession) error {
	agentID := GetAgentID(session)
	sessionID := session.SessionID()

	cp.mu.RLock()
	agentStore := cp.agentStores[sessionID]
	cp.mu.RUnlock()

	if agentStore == nil {
		return fmt.Errorf("检查点器 Agent 执行后错误: agent=%s, 原因=Agent 存储未找到", agentID)
	}

	logger.Info(logComponent).
		Str("action", "post_agent_execute").
		Str("event_type", "checkpoint_save").
		Str("session_id", sessionID).
		Str("agent_id", agentID).
		Str("storage_type", "inmemory").
		Msg("Agent 执行完成后开始保存检查点")

	if err := agentStore.Save(ctx, session); err != nil {
		logger.Error(logComponent).Err(err).
			Str("action", "post_agent_execute").
			Str("event_type", "checkpoint_save").
			Str("session_id", sessionID).
			Str("agent_id", agentID).
			Str("storage_type", "inmemory").
			Msg("Agent 执行完成后保存检查点失败")
		return err
	}

	logger.Info(logComponent).
		Str("action", "post_agent_execute").
		Str("event_type", "checkpoint_save").
		Str("session_id", sessionID).
		Str("agent_id", agentID).
		Str("storage_type", "inmemory").
		Msg("Agent 执行完成后成功保存检查点")
	return nil
}

// PostAgentTeamExecute AgentTeam 执行后保存检查点。
// 对应 Python: InMemoryCheckpointer.post_agent_team_execute()
func (cp *InMemoryCheckpointer) PostAgentTeamExecute(ctx context.Context, session interfaces.BaseSession) error {
	teamID := GetTeamID(session)
	sessionID := session.SessionID()

	cp.mu.RLock()
	teamStore := cp.agentTeamStores[sessionID]
	cp.mu.RUnlock()

	if teamStore == nil {
		return fmt.Errorf("检查点器 Agent 执行后错误: agent=%s, 原因=AgentTeam 存储未找到", teamID)
	}

	logger.Info(logComponent).
		Str("action", "post_agent_team_execute").
		Str("event_type", "checkpoint_save").
		Str("session_id", sessionID).
		Str("workflow_id", teamID).
		Str("storage_type", "inmemory").
		Msg("AgentTeam 执行完成后开始保存检查点")

	if err := teamStore.Save(ctx, session); err != nil {
		logger.Error(logComponent).Err(err).
			Str("action", "post_agent_team_execute").
			Str("event_type", "checkpoint_save").
			Str("session_id", sessionID).
			Str("workflow_id", teamID).
			Str("storage_type", "inmemory").
			Msg("AgentTeam 执行完成后保存检查点失败")
		return err
	}

	logger.Info(logComponent).
		Str("action", "post_agent_team_execute").
		Str("event_type", "checkpoint_save").
		Str("session_id", sessionID).
		Str("workflow_id", teamID).
		Str("storage_type", "inmemory").
		Msg("AgentTeam 执行完成后成功保存检查点")
	return nil
}

// SessionExists 检查会话是否存在。
// 对应 Python: InMemoryCheckpointer.session_exists()
func (cp *InMemoryCheckpointer) SessionExists(ctx context.Context, sessionID string) (bool, error) {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	_, hasAgent := cp.agentStores[sessionID]
	_, hasTeam := cp.agentTeamStores[sessionID]
	_, hasWorkflow := cp.workflowStores[sessionID]
	return hasAgent || hasTeam || hasWorkflow, nil
}

// Release 释放会话资源。
// 对应 Python: InMemoryCheckpointer.release()
// agentID 非空时仅释放指定 Agent 的状态（支持多个，循环清除）；为空时释放整个会话的全部状态。
func (cp *InMemoryCheckpointer) Release(ctx context.Context, sessionID string, agentID ...string) error {
	if len(agentID) > 0 {
		// 循环清除每个指定 Agent 的检查点
		logger.Info(logComponent).
			Str("action", "release").
			Str("event_type", "checkpoint_clear").
			Str("session_id", sessionID).
			Str("storage_type", "inmemory").
			Strs("agent_ids", agentID).
			Msg("开始清除指定 Agent 的检查点")
		cp.mu.RLock()
		agentStore, ok := cp.agentStores[sessionID]
		cp.mu.RUnlock()
		if !ok {
			return nil
		}
		var firstErr error
		for _, aid := range agentID {
			if err := agentStore.Clear(ctx, aid, sessionID); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	}
	cp.mu.Lock()
	defer cp.mu.Unlock()

	// 释放全量会话
	workflowIDs := cp.sessionToWorkflowIDs[sessionID]
	logger.Info(logComponent).
		Str("action", "release").
		Str("event_type", "checkpoint_clear").
		Str("session_id", sessionID).
		Str("workflow_ids", fmt.Sprintf("%v", workflowIDs)).
		Str("storage_type", "inmemory").
		Msg("开始清除会话的所有工作流检查点")

	// 清除 workflow 相关的 graph store 和 workflow store
	for wid := range workflowIDs {
		// ⤵️ 8.7 回填：Graph Store 实现后添加 graphStore.Delete(sessionID, wid)
		_ = wid // 占位
	}
	delete(cp.sessionToWorkflowIDs, sessionID)

	logger.Info(logComponent).
		Str("action", "release").
		Str("event_type", "checkpoint_clear").
		Str("session_id", sessionID).
		Str("storage_type", "inmemory").
		Msg("成功清除会话的所有工作流检查点")

	// 移除 workflow store
	if _, exists := cp.workflowStores[sessionID]; exists {
		delete(cp.workflowStores, sessionID)
		logger.Info(logComponent).
			Str("action", "release").
			Str("event_type", "checkpointer_store_remove").
			Str("session_id", sessionID).
			Str("storage_type", "inmemory").
			Msg("移除工作流检查点存储")
	}

	// 移除匹配的 agent store（前缀匹配，对齐 Python sid.startswith(session_id)）
	for sid := range cp.agentStores {
		if sid == sessionID || (len(sid) > len(sessionID) && sid[:len(sessionID)] == sessionID) {
			delete(cp.agentStores, sid)
			logger.Info(logComponent).
				Str("action", "release").
				Str("event_type", "checkpointer_store_remove").
				Str("session_id", sid).
				Str("storage_type", "inmemory").
				Msg("移除 Agent 检查点存储")
		}
	}

	// 移除 agent team store
	if _, exists := cp.agentTeamStores[sessionID]; exists {
		delete(cp.agentTeamStores, sessionID)
		logger.Info(logComponent).
			Str("action", "release").
			Str("event_type", "checkpointer_store_remove").
			Str("session_id", sessionID).
			Str("storage_type", "inmemory").
			Msg("移除 AgentTeam 检查点存储")
	}

	return nil
}

// GraphStore 获取图状态存储。
// ⤵️ 8.7 回填：Graph Store 实现后返回 Store 实例
func (cp *InMemoryCheckpointer) GraphStore() any {
	return cp.graphStore
}

// Save 保存 Agent 状态。
// 对应 Python: AgentStorage.save() → BaseSingleStateStorage.save()
func (s *AgentStorage) Save(ctx context.Context, session interfaces.BaseSession) error {
	entityID := GetAgentID(session)
	if session.State() == nil {
		return nil
	}
	stateToSave := session.State().GetState()
	if stateToSave == nil {
		return nil
	}
	formatTag, data, err := s.serde.DumpsTyped(stateToSave)
	if err != nil {
		return fmt.Errorf("序列化状态失败: %w", err)
	}
	s.setBlob(entityID, formatTag, data)
	return nil
}

// Recover 恢复 Agent 状态。
// 对应 Python: AgentStorage.recover() → BaseSingleStateStorage.recover()
func (s *AgentStorage) Recover(ctx context.Context, session interfaces.BaseSession, inputs any) error {
	entityID := GetAgentID(session)
	stateBlob, exists := s.getBlob(entityID)
	if !exists {
		return nil
	}
	loadedState, err := s.serde.LoadsTyped(stateBlob.FormatTag, stateBlob.Data)
	if err != nil {
		return fmt.Errorf("反序列化状态失败: %w", err)
	}
	if session.State() == nil || loadedState == nil {
		return nil
	}
	if st, ok := loadedState.(map[string]any); ok {
		session.State().SetState(st)
	}
	return nil
}

// Clear 清除 Agent 状态。
func (s *AgentStorage) Clear(ctx context.Context, entityID, _ string) error {
	s.deleteBlob(entityID)
	return nil
}

// Exists 检查 Agent 状态是否存在。
func (s *AgentStorage) Exists(ctx context.Context, session interfaces.BaseSession) (bool, error) {
	entityID := GetAgentID(session)
	return s.hasBlob(entityID), nil
}

// Save 保存 AgentTeam 状态。
// 对应 Python: AgentTeamStorage.save() → BaseSingleStateStorage.save()
func (s *AgentTeamStorage) Save(ctx context.Context, session interfaces.BaseSession) error {
	entityID := GetTeamID(session)
	if session.State() == nil {
		return nil
	}
	// 对齐 Python: session.state().get_global(None) → 只保存 globalState
	stateToSave := session.State().GetGlobal(state.AllStateKey)
	if stateToSave == nil {
		return nil
	}
	formatTag, data, err := s.serde.DumpsTyped(stateToSave)
	if err != nil {
		return fmt.Errorf("序列化状态失败: %w", err)
	}
	s.setBlob(entityID, formatTag, data)
	return nil
}

// Recover 恢复 AgentTeam 状态。
// 对应 Python: AgentTeamStorage.recover() → BaseSingleStateStorage.recover()
func (s *AgentTeamStorage) Recover(ctx context.Context, session interfaces.BaseSession, inputs any) error {
	entityID := GetTeamID(session)
	stateBlob, exists := s.getBlob(entityID)
	if !exists {
		return nil
	}
	loadedState, err := s.serde.LoadsTyped(stateBlob.FormatTag, stateBlob.Data)
	if err != nil {
		return fmt.Errorf("反序列化状态失败: %w", err)
	}
	if session.State() == nil || loadedState == nil {
		return nil
	}
	// 对齐 Python: session.state().global_state.set_state(state) → 只恢复 globalState
	if st, ok := loadedState.(map[string]any); ok {
		session.State().SetGlobal(st)
	}
	return nil
}

// Clear 清除 AgentTeam 状态。
func (s *AgentTeamStorage) Clear(ctx context.Context, entityID, _ string) error {
	s.deleteBlob(entityID)
	return nil
}

// Exists 检查 AgentTeam 状态是否存在。
func (s *AgentTeamStorage) Exists(ctx context.Context, session interfaces.BaseSession) (bool, error) {
	entityID := GetTeamID(session)
	return s.hasBlob(entityID), nil
}

// Save 保存工作流状态和更新。
// 对应 Python: WorkflowStorage.save()
func (ws *WorkflowStorage) Save(ctx context.Context, session interfaces.BaseSession) error {
	workflowID := getWorkflowID(session)

	// 通过类型断言获取 WorkflowState 接口
	wfState, ok := session.State().(state.WorkflowState)
	if !ok || wfState == nil {
		return nil
	}

	// 保存主状态
	// 对齐 Python: state = session.state().get_state()
	// WorkflowState 接口没有 GetState，但 session.State() 返回 SessionState，
	// 而 SessionState 嵌入了 RecoverableStateLike（包含 GetState）
	mainState := session.State().GetState()
	if mainState != nil {
		formatTag, data, err := ws.serde.DumpsTyped(mainState)
		if err != nil {
			return fmt.Errorf("序列化工作流状态失败: %w", err)
		}
		ws.mu.Lock()
		ws.stateBlobs[workflowID] = serdeTuple{FormatTag: formatTag, Data: data}
		ws.mu.Unlock()
	}

	// 保存状态更新
	// 对齐 Python: updates = session.state().get_updates()
	// GetUpdates/SetUpdates 在 WorkflowCommitState 上，不在 WorkflowState 接口上，
	// 需要类型断言为 *state.WorkflowCommitState
	if commitState, ok := session.State().(*state.WorkflowCommitState); ok {
		updates := commitState.GetUpdates()
		if updates != nil {
			formatTag, data, err := ws.serde.DumpsTyped(updates)
			if err != nil {
				return fmt.Errorf("序列化工作流状态更新失败: %w", err)
			}
			ws.mu.Lock()
			ws.stateUpdatesBlobs[workflowID] = serdeTuple{FormatTag: formatTag, Data: data}
			ws.mu.Unlock()
		}
	}
	return nil
}

// Recover 恢复工作流状态。
// 对应 Python: WorkflowStorage.recover()
func (ws *WorkflowStorage) Recover(ctx context.Context, session interfaces.BaseSession, inputs any) error {
	workflowID := getWorkflowID(session)

	// 恢复主状态
	ws.mu.RLock()
	stateBlob, stateExists := ws.stateBlobs[workflowID]
	ws.mu.RUnlock()

	if stateExists && stateBlob.FormatTag != emptyFormatTag {
		loadedState, err := ws.serde.LoadsTyped(stateBlob.FormatTag, stateBlob.Data)
		if err != nil {
			return fmt.Errorf("反序列化工作流状态失败: %w", err)
		}
		if st, ok := loadedState.(map[string]any); ok {
			session.State().SetState(st)
		}
	}

	// 处理交互输入
	// 对齐 Python: if inputs is not None: self._process_interactive_inputs(session, inputs)
	if ii, ok := inputs.(*interaction.InteractiveInput); ok {
		ws.processInteractiveInputs(session, ii)
	}

	// 恢复状态更新
	// GetUpdates/SetUpdates 在 WorkflowCommitState 上，需要类型断言
	// 对齐 Python: session.state().set_updates(state_updates) 后无需额外 commit，
	// updates 会在下次 workflow 执行时通过 commit 合并到 state。
	if commitState, ok := session.State().(*state.WorkflowCommitState); ok {
		ws.mu.RLock()
		updatesBlob, updatesExists := ws.stateUpdatesBlobs[workflowID]
		ws.mu.RUnlock()

		if updatesExists {
			loadedUpdates, err := ws.serde.LoadsTyped(updatesBlob.FormatTag, updatesBlob.Data)
			if err != nil {
				return fmt.Errorf("反序列化工作流状态更新失败: %w", err)
			}
			if updates, ok := loadedUpdates.(map[string]any); ok {
				commitState.SetUpdates(updates)
			}
		}
	}
	return nil
}

// Clear 清除工作流状态。
// 对应 Python: WorkflowStorage.clear()
func (ws *WorkflowStorage) Clear(ctx context.Context, workflowID, _ string) error {
	ws.mu.Lock()
	delete(ws.stateBlobs, workflowID)
	delete(ws.stateUpdatesBlobs, workflowID)
	ws.mu.Unlock()
	return nil
}

// Exists 检查工作流状态是否存在。
// 对应 Python: WorkflowStorage.exists()
func (ws *WorkflowStorage) Exists(ctx context.Context, session interfaces.BaseSession) (bool, error) {
	workflowID := getWorkflowID(session)
	ws.mu.RLock()
	stateBlob, exists := ws.stateBlobs[workflowID]
	ws.mu.RUnlock()
	if exists && stateBlob.FormatTag != emptyFormatTag {
		return true, nil
	}
	return false, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newAgentStorage 创建 Agent 状态存储。
func newAgentStorage() *AgentStorage {
	return &AgentStorage{
		baseSingleStateStorage: baseSingleStateStorage{
			stateBlobs: make(map[string]serdeTuple),
			serde:      NewJSONSerializer(),
		},
	}
}

// newAgentTeamStorage 创建 AgentTeam 状态存储。
func newAgentTeamStorage() *AgentTeamStorage {
	return &AgentTeamStorage{
		baseSingleStateStorage: baseSingleStateStorage{
			stateBlobs: make(map[string]serdeTuple),
			serde:      NewJSONSerializer(),
		},
	}
}

// newWorkflowStorage 创建 Workflow 状态存储。
func newWorkflowStorage() *WorkflowStorage {
	return &WorkflowStorage{
		serde:             NewJSONSerializer(),
		stateBlobs:        make(map[string]serdeTuple),
		stateUpdatesBlobs: make(map[string]serdeTuple),
	}
}

// innerSaveWorkflowCheckpoint 内部方法：保存工作流检查点。
// 对应 Python: InMemoryCheckpointer._inner_save_workflow_checkpoint()
func (cp *InMemoryCheckpointer) innerSaveWorkflowCheckpoint(ctx context.Context, workflowID, sessionID string, session interfaces.BaseSession, reason string) error {
	cp.mu.RLock()
	workflowStore := cp.workflowStores[sessionID]
	workflowIDs := cp.sessionToWorkflowIDs[sessionID]
	cp.mu.RUnlock()

	logger.Info(logComponent).
		Str("action", "inner_save_workflow_checkpoint").
		Str("event_type", "checkpoint_save").
		Str("session_id", sessionID).
		Str("workflow_id", workflowID).
		Str("reason", reason).
		Str("storage_type", "inmemory").
		Msg("开始保存工作流检查点")

	if err := workflowStore.Save(ctx, session); err != nil {
		logger.Error(logComponent).Err(err).
			Str("session_id", sessionID).
			Str("workflow_id", workflowID).
			Msg("保存工作流检查点失败")
		return err
	}

	// 记录 workflow ID
	cp.mu.Lock()
	if workflowIDs != nil {
		workflowIDs[workflowID] = true
	}
	cp.mu.Unlock()

	logger.Info(logComponent).
		Str("action", "inner_save_workflow_checkpoint").
		Str("event_type", "checkpoint_save").
		Str("session_id", sessionID).
		Str("workflow_id", workflowID).
		Str("reason", reason).
		Str("storage_type", "inmemory").
		Msg("成功保存工作流检查点")
	return nil
}

// innerClearWorkflowSession 内部方法：清除工作流会话。
// 对应 Python: InMemoryCheckpointer._inner_clear_workflow_session()
func (cp *InMemoryCheckpointer) innerClearWorkflowSession(ctx context.Context, workflowID, sessionID string, reason string) error {
	cp.mu.RLock()
	workflowStore := cp.workflowStores[sessionID]
	workflowIDs := cp.sessionToWorkflowIDs[sessionID]
	cp.mu.RUnlock()

	logger.Info(logComponent).
		Str("action", "inner_clear_workflow_session").
		Str("event_type", "checkpoint_clear").
		Str("session_id", sessionID).
		Str("workflow_id", workflowID).
		Str("reason", reason).
		Str("storage_type", "inmemory").
		Msg("开始清除工作流所有检查点")

	isSucceed := false

	// ⤵️ 8.7 回填：Graph Store 实现后添加 graphStore.Delete(sessionID, workflowID)
	// 暂时标记为成功（因为没有 graph store 操作）
	isSucceed = true

	// 清除 workflow store
	if workflowStore != nil {
		cp.mu.Lock()
		if workflowIDs != nil {
			delete(workflowIDs, workflowID)
		}
		cp.mu.Unlock()

		if err := workflowStore.Clear(ctx, workflowID, sessionID); err != nil {
			if !isSucceed {
				logger.Error(logComponent).Err(err).
					Str("action", "inner_clear_workflow_session").
					Str("event_type", "checkpoint_clear").
					Str("session_id", sessionID).
					Str("workflow_id", workflowID).
					Str("storage_type", "inmemory").
					Msg("清除工作流检查点失败")
			}
			return err
		}
	}

	if isSucceed {
		logger.Info(logComponent).
			Str("action", "inner_clear_workflow_session").
			Str("event_type", "checkpoint_clear").
			Str("session_id", sessionID).
			Str("workflow_id", workflowID).
			Str("reason", reason).
			Str("storage_type", "inmemory").
			Msg("成功清除工作流所有检查点")
	}
	return nil
}

// innerClearWorkflowSessionLocked 清除工作流会话（不加锁版本，调用方已持锁）。
// 用于 PostWorkflowExecute 中清理与删除需要在同一锁区间完成的场景。
func (cp *InMemoryCheckpointer) innerClearWorkflowSessionLocked(ctx context.Context, workflowID, sessionID string, reason string) error {
	workflowStore := cp.workflowStores[sessionID]
	workflowIDs := cp.sessionToWorkflowIDs[sessionID]

	logger.Info(logComponent).
		Str("action", "inner_clear_workflow_session").
		Str("event_type", "checkpoint_clear").
		Str("session_id", sessionID).
		Str("workflow_id", workflowID).
		Str("reason", reason).
		Str("storage_type", "inmemory").
		Msg("开始清除工作流所有检查点")

	isSucceed := false

	// ⤵️ 8.7 回填：Graph Store 实现后添加 graphStore.Delete(sessionID, workflowID)
	isSucceed = true

	// 清除 workflow store（不需要再加锁，调用方已持锁）
	if workflowStore != nil {
		if workflowIDs != nil {
			delete(workflowIDs, workflowID)
		}
		// workflowStore.Clear 自身有独立的锁保护
		if err := workflowStore.Clear(ctx, workflowID, sessionID); err != nil {
			if !isSucceed {
				logger.Error(logComponent).Err(err).
					Str("action", "inner_clear_workflow_session").
					Str("event_type", "checkpoint_clear").
					Str("session_id", sessionID).
					Str("workflow_id", workflowID).
					Str("storage_type", "inmemory").
					Msg("清除工作流检查点失败")
			}
			return err
		}
	}

	if isSucceed {
		logger.Info(logComponent).
			Str("action", "inner_clear_workflow_session").
			Str("event_type", "checkpoint_clear").
			Str("session_id", sessionID).
			Str("workflow_id", workflowID).
			Str("reason", reason).
			Str("storage_type", "inmemory").
			Msg("成功清除工作流所有检查点")
	}
	return nil
}

// setBlob 设置序列化数据。
func (s *baseSingleStateStorage) setBlob(entityID, formatTag string, data []byte) {
	s.mu.Lock()
	s.stateBlobs[entityID] = serdeTuple{FormatTag: formatTag, Data: data}
	s.mu.Unlock()
}

// getBlob 获取序列化数据。
func (s *baseSingleStateStorage) getBlob(entityID string) (serdeTuple, bool) {
	s.mu.RLock()
	blob, exists := s.stateBlobs[entityID]
	s.mu.RUnlock()
	return blob, exists
}

// deleteBlob 删除序列化数据。
func (s *baseSingleStateStorage) deleteBlob(entityID string) {
	s.mu.Lock()
	delete(s.stateBlobs, entityID)
	s.mu.Unlock()
}

// hasBlob 检查序列化数据是否存在。
func (s *baseSingleStateStorage) hasBlob(entityID string) bool {
	s.mu.RLock()
	_, exists := s.stateBlobs[entityID]
	s.mu.RUnlock()
	return exists
}

// processInteractiveInputs 处理交互输入并更新工作流状态。
// 委托给公共函数 processInteractiveInputs，消除代码重复（CP-25）。
func (ws *WorkflowStorage) processInteractiveInputs(session interfaces.BaseSession, inputs *interaction.InteractiveInput) {
	processInteractiveInputs(session, inputs)
}

// isInteractiveInput 判断输入是否为交互输入。
// 对齐 Python: isinstance(inputs, InteractiveInput)
func isInteractiveInput(inputs any) bool {
	if inputs == nil {
		return false
	}
	_, ok := inputs.(*interaction.InteractiveInput)
	return ok
}

// isWorkflowInterrupted 检查工作流结果是否为中断状态。
// Python: result.get(TASK_STATUS_INTERRUPT) is None
// Go 版本简化处理：检查 result 中的 interrupt 标记。
func isWorkflowInterrupted(result any) bool {
	if result == nil {
		return false
	}
	if m, ok := result.(map[string]any); ok {
		// 对齐 Python: TASK_STATUS_INTERRUPT 键
		if interruptVal, exists := m["__interrupt__"]; exists && interruptVal != nil {
			return true
		}
	}
	return false
}
