# 5.9 PersistenceCheckpointer 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 PersistenceCheckpointer，基于 BaseKVStore 的持久化检查点器，支持进程重启后会话状态恢复。

**Architecture:** 通过 EntityHooks 接口注入实现 Python 模板方法模式；所有 Storage 操作走 Pipeline batch；统一使用 JSONSerializer 序列化；Provider 只支持 sqlite 后端。GraphStore 跳过等 8.7。

**Tech Stack:** Go 1.x, kv.BaseKVStore 接口, kv.KVPipeline, kv.DbBasedKVStore (SQLite)

**设计文档:** `docs/superpowers/specs/2026-08-29-persistence-checkpointer-design.md`

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/agentcore/session/checkpointer/base.go` | Storage.Clear 签名扩展 |
| 修改 | `internal/agentcore/session/checkpointer/inmemory.go` | 适配 Clear 新签名 |
| 修改 | `internal/agentcore/session/checkpointer/inmemory_test.go` | 适配 Clear 新签名 |
| 创建 | `internal/agentcore/session/checkpointer/persistence.go` | EntityHooks + basePersistenceStorage + 3 个 Storage + PersistenceCheckpointer + Provider |
| 创建 | `internal/agentcore/session/checkpointer/persistence_test.go` | 全部持久化 Storage + Checkpointer 测试 |
| 修改 | `internal/agentcore/session/checkpointer/factory.go` | 注册 persistence Provider |
| 修改 | `internal/agentcore/session/checkpointer/factory_test.go` | persistence Provider 注册测试 |
| 修改 | `internal/agentcore/session/checkpointer/doc.go` | 文件目录添加 persistence.go |

---

### Task 1: 扩展 Storage.Clear 签名

**Files:**
- Modify: `internal/agentcore/session/checkpointer/base.go:48`
- Modify: `internal/agentcore/session/checkpointer/inmemory.go` (3 处 Clear 方法)
- Modify: `internal/agentcore/session/checkpointer/inmemory_test.go` (2 处 Clear 调用)

- [ ] **Step 1: 修改 base.go 的 Storage.Clear 签名**

将 `Clear(ctx context.Context, entityID string) error` 改为 `Clear(ctx context.Context, entityID, sessionID string) error`。

在 `base.go` 第 48 行，将：
```go
Clear(ctx context.Context, entityID string) error
```
改为：
```go
Clear(ctx context.Context, entityID, sessionID string) error
```

- [ ] **Step 2: 适配 inmemory.go 的三个 Clear 方法签名**

在 `inmemory.go` 中修改三处：

AgentStorage.Clear（约第 825 行）：
```go
// 修改前
func (s *AgentStorage) Clear(ctx context.Context, entityID string) error {
// 修改后
func (s *AgentStorage) Clear(ctx context.Context, entityID, sessionID string) error {
```

AgentTeamStorage.Clear（约第 889 行）：
```go
// 修改前
func (s *AgentTeamStorage) Clear(ctx context.Context, entityID string) error {
// 修改后
func (s *AgentTeamStorage) Clear(ctx context.Context, entityID, sessionID string) error {
```

WorkflowStorage.Clear（约第 993 行）：
```go
// 修改前
func (ws *WorkflowStorage) Clear(ctx context.Context, workflowID string) error {
// 修改后
func (ws *WorkflowStorage) Clear(ctx context.Context, workflowID, sessionID string) error {
```

- [ ] **Step 3: 适配 inmemory_test.go 的 Clear 调用**

在 `inmemory_test.go` 中修改两处：

TestAgentStorage_Clear（约第 479 行）：
```go
// 修改前
if err := storage.Clear(ctx, "agent1"); err != nil {
// 修改后
if err := storage.Clear(ctx, "agent1", "sess1"); err != nil {
```

TestAgentTeamStorage_Clear（约第 604 行）：
```go
// 修改前
if err := storage.Clear(ctx, "team1"); err != nil {
// 修改后
if err := storage.Clear(ctx, "team1", "sess1"); err != nil {
```

TestWorkflowStorage_Clear（约第 681 行）：
```go
// 修改前
if err := storage.Clear(ctx, "wf1"); err != nil {
// 修改后
if err := storage.Clear(ctx, "wf1", "sess1"); err != nil {
```

同时需要修改 InMemoryCheckpointer 内部所有调用 Clear 的地方，在 `inmemory.go` 中搜索所有 `.Clear(ctx,` 调用，添加 sessionID 参数：

- `workflowStore.Clear(ctx, workflowID)` → `workflowStore.Clear(ctx, workflowID, sessionID)`
  出现在 PreWorkflowExecute（约第 234 行）、innerClearWorkflowSession（约第 723 行）

- [ ] **Step 4: 运行测试确认修改正确**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/checkpointer/... -v -count=1`

Expected: 所有测试 PASS

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "refactor: Storage.Clear 签名扩展为 Clear(ctx, entityID, sessionID)"
```

---

### Task 2: 创建 persistence.go — EntityHooks 接口 + basePersistenceStorage 骨架

**Files:**
- Create: `internal/agentcore/session/checkpointer/persistence.go`

- [ ] **Step 1: 创建 persistence.go 文件，包含 EntityHooks 接口和 basePersistenceStorage 结构体**

文件头部包含 package 和 import，然后按编码规范顺序定义：

1. EntityHooks 接口（接口归入结构体区块，排在结构体之前）
2. basePersistenceStorage 结构体
3. 常量
4. basePersistenceStorage 的骨架方法（Save/Recover/Clear/Exists）
5. 辅助方法（buildStateKeys/serializeState/deserializeState/decodeDumpType/logKwargs/entityLogExtra/pipelineGetResult/pipelineExistsResult）

EntityHooks 接口：
```go
// EntityHooks 单实体状态存储的钩子接口。
// 对应 Python: BaseSingleStateStorage 的 _get_entity_id/_get_state_to_save/_restore_state
// Go 不支持虚方法分派，通过接口注入实现模板方法模式。
type EntityHooks interface {
	// GetEntityID 获取实体 ID（Agent 返回 agentID，AgentTeam 返回 teamID）
	GetEntityID(session CheckpointerSession) string
	// GetStateToSave 获取需要保存的状态
	GetStateToSave(session CheckpointerSession) any
	// RestoreState 将恢复的状态设置回 session
	RestoreState(session CheckpointerSession, state any)
}
```

basePersistenceStorage 结构体：
```go
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
```

常量：
```go
const (
	// keyNums 单实体存储的 key 数量（dumpType + blob）
	keyNums = 2
)
```

骨架方法 Save — 对齐 Python BaseSingleStateStorage.save：
```go
// Save 保存会话状态到 KVStore。
// 对应 Python: BaseSingleStateStorage.save()
func (s *basePersistenceStorage) Save(ctx context.Context, session CheckpointerSession) error {
	state := s.hooks.GetStateToSave(session)
	sessionID := session.SessionID()
	entityID := s.hooks.GetEntityID(session)

	stateBlob := s.serializeState(state)
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
		Msgf("%s 状态保存成功", strings.Title(s.entityLabel))
	return nil
}
```

骨架方法 Recover — 对齐 Python BaseSingleStateStorage.recover：
```go
// Recover 从 KVStore 恢复会话状态。
// 对应 Python: BaseSingleStateStorage.recover()
func (s *basePersistenceStorage) Recover(ctx context.Context, session CheckpointerSession, inputs any) error {
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
	state := s.deserializeState(dumpTypeBytes, blob)
	if state == nil {
		logger.Debug(logComponent).
			Str("event_type", "checkpoint_restore").
			Str("session_id", sessionID).
			Str(s.entityLogExtraKey(), entityID).
			Str("storage_type", "persistence").
			Msgf("未找到 %s 状态", s.entityLabel)
		return nil
	}

	s.hooks.RestoreState(session, state)
	logger.Debug(logComponent).
		Str("event_type", "checkpoint_restore").
		Str("session_id", sessionID).
		Str(s.entityLogExtraKey(), entityID).
		Str("storage_type", "persistence").
		Msgf("%s 状态恢复成功", strings.Title(s.entityLabel))
	return nil
}
```

骨架方法 Clear：
```go
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
```

骨架方法 Exists：
```go
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
```

辅助方法 buildStateKeys：
```go
// buildStateKeys 构建 KV 存储键。
func (s *basePersistenceStorage) buildStateKeys(sessionID, entityID string) (string, string) {
	dumpTypeKey := BuildKeyWithNamespace(sessionID, s.namespace, entityID, s.stateDumpTypeKey)
	blobKey := BuildKeyWithNamespace(sessionID, s.namespace, entityID, s.stateBlobsKey)
	return dumpTypeKey, blobKey
}
```

辅助方法 serializeState / deserializeState / decodeDumpType：
```go
// serializeState 序列化状态，返回 serdeTuple。
func (s *basePersistenceStorage) serializeState(state any) *serdeTuple {
	formatTag, data, err := s.serde.DumpsTyped(state)
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
	dumpType := s.decodeDumpType(dumpTypeBytes)
	state, err := s.serde.LoadsTyped(dumpType, blob)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "checkpoint_error").
			Str("metadata_operation", "deserialize").
			Msg("反序列化状态失败")
		return nil
	}
	return state
}

// decodeDumpType 解码 dump type（[]byte → string）。
func (s *basePersistenceStorage) decodeDumpType(data []byte) string {
	if data == nil {
		return ""
	}
	return string(data)
}
```

辅助方法 entityLogExtraKey / entityLogExtra（对齐 Python _entity_log_extra）：
```go
// entityLogExtraKey 返回日志字段键名（"agent_id" 或 "workflow_id"）。
func (s *basePersistenceStorage) entityLogExtraKey() string {
	if s.entityLabel == "agent" {
		return "agent_id"
	}
	return "workflow_id"
}
```

Pipeline 结果解析辅助方法：
```go
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
```

- [ ] **Step 2: 运行编译确认无语法错误**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/session/checkpointer/...`

Expected: 编译成功（可能有一些 unused 警告，后续步骤会使用）

- [ ] **Step 3: 提交**

```bash
git add -A && git commit -m "feat(5.9): 添加 EntityHooks 接口和 basePersistenceStorage 骨架"
```

---

### Task 3: 实现 PersistenceAgentStorage + PersistenceAgentTeamStorage

**Files:**
- Modify: `internal/agentcore/session/checkpointer/persistence.go`

- [ ] **Step 1: 在 persistence.go 中添加三个具体 Storage**

在 basePersistenceStorage 之后添加：

PersistenceAgentStorage：
```go
// PersistenceAgentStorage Agent 持久化状态存储。
// 对应 Python: persistence.py (AgentStorage)
type PersistenceAgentStorage struct {
	basePersistenceStorage
}
```

PersistenceAgentTeamStorage：
```go
// PersistenceAgentTeamStorage AgentTeam 持久化状态存储。
// 对应 Python: persistence.py (AgentTeamStorage)
type PersistenceAgentTeamStorage struct {
	basePersistenceStorage
}
```

导出函数：
```go
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
			hooks: &agentEntityHooks{},
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
```

EntityHooks 实现（非导出，归入结构体区块）：
```go
// agentEntityHooks Agent 存储钩子实现。
type agentEntityHooks struct{}

func (h *agentEntityHooks) GetEntityID(session CheckpointerSession) string {
	return GetAgentID(session)
}

func (h *agentEntityHooks) GetStateToSave(session CheckpointerSession) any {
	if session.State() == nil {
		return nil
	}
	return session.State().GetState()
}

func (h *agentEntityHooks) RestoreState(session CheckpointerSession, state any) {
	if session.State() == nil || state == nil {
		return
	}
	if st, ok := state.(map[string]any); ok {
		session.State().SetState(st)
	}
}

// agentTeamEntityHooks AgentTeam 存储钩子实现。
type agentTeamEntityHooks struct{}

func (h *agentTeamEntityHooks) GetEntityID(session CheckpointerSession) string {
	return GetTeamID(session)
}

func (h *agentTeamEntityHooks) GetStateToSave(session CheckpointerSession) any {
	if session.State() == nil {
		return nil
	}
	if asc, ok := session.State().(*state.AgentStateCollection); ok {
		return asc.GetState()
	}
	return session.State().GetGlobal(state.AllStateKey)
}

func (h *agentTeamEntityHooks) RestoreState(session CheckpointerSession, state any) {
	if session.State() == nil || state == nil {
		return
	}
	if asc, ok := session.State().(*state.AgentStateCollection); ok {
		if st, ok := state.(map[string]any); ok {
			asc.SetState(st)
		}
	}
}
```

- [ ] **Step 2: 编译确认**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/session/checkpointer/...`

Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add -A && git commit -m "feat(5.9): 实现 PersistenceAgentStorage + PersistenceAgentTeamStorage"
```

---

### Task 4: 实现 PersistenceWorkflowStorage

**Files:**
- Modify: `internal/agentcore/session/checkpointer/persistence.go`

- [ ] **Step 1: 在 persistence.go 中添加 PersistenceWorkflowStorage**

```go
// PersistenceWorkflowStorage Workflow 持久化状态存储。
// 独立于 basePersistenceStorage，因为需要同时保存 state + updates 两类数据（4 个 key）。
// 对应 Python: persistence.py (WorkflowStorage)
type PersistenceWorkflowStorage struct {
	// kvStore KV 存储后端
	kvStore kv.BaseKVStore
	// serde 序列化器
	serde Serializer
}

// workflow 存储常量
const (
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
```

构造函数：
```go
// newPersistenceWorkflowStorage 创建 Workflow 持久化状态存储。
func newPersistenceWorkflowStorage(kvStore kv.BaseKVStore) *PersistenceWorkflowStorage {
	return &PersistenceWorkflowStorage{
		kvStore: kvStore,
		serde:   NewJSONSerializer(),
	}
}
```

Save 方法 — 对齐 Python WorkflowStorage.save：
```go
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
```

Recover 方法 — 对齐 Python WorkflowStorage.recover：
```go
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
		state, deserErr := ws.serde.LoadsTyped(dumpTypeStr, stateBlob)
		if deserErr != nil {
			logger.Error(logComponent).Err(deserErr).
				Str("event_type", "checkpoint_error").
				Str("session_id", sessionID).
				Str("workflow_id", workflowID).
				Str("metadata_operation", "deserialize_state").
				Msg("反序列化工作流状态失败")
		} else if state != nil {
			if st, ok := state.(map[string]any); ok {
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
```

Clear / Exists 方法 + 辅助方法 recoverFromInputs / serializeState — 参照 InMemory 版对应方法实现，将 map 操作替换为 KVStore Pipeline 操作。具体代码较长，参照设计文档中 PersistenceWorkflowStorage 的行为表实现。

- [ ] **Step 2: 编译确认**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/session/checkpointer/...`

- [ ] **Step 3: 提交**

```bash
git add -A && git commit -m "feat(5.9): 实现 PersistenceWorkflowStorage"
```

---

### Task 5: 实现 PersistenceCheckpointer + persistenceProvider

**Files:**
- Modify: `internal/agentcore/session/checkpointer/persistence.go`

- [ ] **Step 1: 在 persistence.go 中添加 PersistenceCheckpointer 和 persistenceProvider**

PersistenceCheckpointer 结构体：
```go
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
```

构造函数：
```go
// NewPersistenceCheckpointer 创建持久化检查点器实例。
func NewPersistenceCheckpointer(kvStore kv.BaseKVStore) *PersistenceCheckpointer {
	return &PersistenceCheckpointer{
		kvStore:          kvStore,
		agentStorage:     newPersistenceAgentStorage(kvStore),
		agentTeamStorage: newPersistenceAgentTeamStorage(kvStore),
		workflowStorage:  newPersistenceWorkflowStorage(kvStore),
		graphStore:       nil,
	}
}
```

所有 Checkpointer 接口方法（GetThreadID/PreAgentExecute/PostAgentExecute/PreAgentTeamExecute/PostAgentTeamExecute/InterruptAgentExecute/PreWorkflowExecute/PostWorkflowExecute/SessionExists/Release/GraphStore）— 逐一对照 Python PersistenceCheckpointer 实现，每个方法内包含完整日志。

persistenceProvider：
```go
// persistenceProvider Persistence 检查点器提供者。
// 对应 Python: PersistenceCheckpointerProvider
type persistenceProvider struct{}

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
```

注意：Provider 使用 `kv.NewDbBasedKVStore`（基于 GORM + SQLite），对齐 Python 的 `DbBasedKVStore`。GORM 的 SQLite 驱动已在项目中配置。构造 `DbBasedKVStore` 需要先创建 `gorm.DB` 实例。

- [ ] **Step 2: 编译确认**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/session/checkpointer/...`

- [ ] **Step 3: 提交**

```bash
git add -A && git commit -m "feat(5.9): 实现 PersistenceCheckpointer + persistenceProvider"
```

---

### Task 6: 注册 persistence Provider 到 Factory

**Files:**
- Modify: `internal/agentcore/session/checkpointer/factory.go`

- [ ] **Step 1: 在 NewCheckpointerFactory 中注册 persistence Provider**

修改 `NewCheckpointerFactory` 函数，在 `f.Register("in_memory", &inMemoryProvider{})` 之后添加：
```go
f.Register("persistence", &persistenceProvider{})
```

- [ ] **Step 2: 编译确认**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/session/checkpointer/...`

- [ ] **Step 3: 添加 persistence Provider 注册测试**

在 `factory_test.go` 中添加：

```go
// TestCheckpointerFactory_Create_persistence 测试创建 persistence 类型
func TestCheckpointerFactory_Create_persistence(t *testing.T) {
	f := NewCheckpointerFactory()
	ctx := context.Background()

	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test_checkpointer.db"
	cp, err := f.Create(ctx, CheckpointerFactoryConfig{
		Type: "persistence",
		Conf: map[string]any{"db_path": dbPath},
	})
	if err != nil {
		t.Fatalf("Create 返回错误：%v", err)
	}
	if cp == nil {
		t.Fatal("Create 返回 nil")
	}
}

// TestPersistenceProvider_Create 测试 Persistence Provider 创建
func TestPersistenceProvider_Create(t *testing.T) {
	provider := &persistenceProvider{}
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test_provider.db"

	cp, err := provider.Create(ctx, map[string]any{"db_path": dbPath})
	if err != nil {
		t.Fatalf("Create 返回错误：%v", err)
	}
	if cp == nil {
		t.Fatal("Create 返回 nil")
	}
}

// TestPersistenceProvider_Create_默认路径 测试使用默认 db_path
func TestPersistenceProvider_Create_默认路径(t *testing.T) {
	provider := &persistenceProvider{}
	ctx := context.Background()

	// 不指定 db_path，使用默认值
	cp, err := provider.Create(ctx, nil)
	if err != nil {
		t.Fatalf("Create 返回错误：%v", err)
	}
	if cp == nil {
		t.Fatal("Create 返回 nil")
	}

	// 清理默认路径文件
	_ = os.Remove("checkpointer.db")
}
```

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/checkpointer/... -v -run "TestPersistence|TestCheckpointerFactory_Create_persistence" -count=1`

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat(5.9): 注册 persistence Provider 到 Factory + 测试"
```

---

### Task 7: 编写 persistence_test.go — 完整测试

**Files:**
- Create: `internal/agentcore/session/checkpointer/persistence_test.go`

- [ ] **Step 1: 创建 persistence_test.go**

测试文件包含：

1. **辅助函数** — `newTestPersistenceCheckpointer(t)` 使用 `t.TempDir()` + GORM SQLite :memory: + DbBasedKVStore
2. **NewPersistenceCheckpointer 测试** — 创建实例验证非 nil
3. **PersistenceAgentStorage 测试** — Save/Recover 往返、Clear、Exists、Recover 无数据
4. **PersistenceAgentTeamStorage 测试** — Save/Recover 往返、Clear、Exists
5. **PersistenceWorkflowStorage 测试** — Save/Recover 往返、含 updates 的 Save/Recover、Clear、Exists
6. **PersistenceCheckpointer 生命周期测试** — PreAgentExecute + PostAgentExecute 状态往返、PreAgentTeamExecute + PostAgentTeamExecute、PreWorkflowExecute + PostWorkflowExecute 正常完成、PreWorkflowExecute + PostWorkflowExecute 异常、PreWorkflowExecute + PostWorkflowExecute 中断、InterruptAgentExecute、SessionExists、Release、GraphStore 返回 nil
7. **basePersistenceStorage 辅助方法测试** — buildStateKeys、serializeState/deserializeState

所有测试复用 `inmemory_test.go` 中已定义的测试辅助类型（`testSession`/`testAgentSession`/`testTeamSession`/`testWorkflowSession`/`testConfig`），因为同包测试。

- [ ] **Step 2: 运行全部 checkpointer 测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/checkpointer/... -v -count=1`

Expected: 所有测试 PASS

- [ ] **Step 3: 提交**

```bash
git add -A && git commit -m "feat(5.9): 添加 PersistenceCheckpointer 完整测试"
```

---

### Task 8: 更新 doc.go 和 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/agentcore/session/checkpointer/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 doc.go 文件目录**

在文件目录树中添加 `persistence.go`：
```
//	checkpointer/
//	├── doc.go              # 包文档
//	├── base.go             # Checkpointer/Storage 接口、命名空间常量、Key 构建函数
//	├── serializer.go       # Serializer 接口、JSONSerializer 实现
//	├── inmemory.go         # InMemoryCheckpointer、AgentStorage/AgentTeamStorage/WorkflowStorage
//	├── persistence.go      # PersistenceCheckpointer、持久化 Storage 实现、Provider
//	├── factory.go          # CheckpointerFactory、CheckpointerProvider、CheckpointerConfig
//	├── base_test.go        # 基础接口和常量测试
//	├── serializer_test.go  # Serializer 测试
//	├── inmemory_test.go    # InMemoryCheckpointer 测试
//	├── persistence_test.go # PersistenceCheckpointer 测试
//	└── factory_test.go     # CheckpointerFactory 测试
```

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

将 5.9 的状态从 `☐` 改为 `✅`：
```
| 5.9 | ✅ | PersistenceCheckpointer | 持久化实现；Storage 体系采用接口注入钩子模式；InMemory 版 Storage 不需要回填，5.9 独立实现 Persistence 版 Storage；✅ Storage.Clear 签名扩展为 Clear(ctx, entityID, sessionID) | `openjiuwen/core/session/checkpointer/persistence.py` |
```

- [ ] **Step 3: 提交**

```bash
git add -A && git commit -m "docs(5.9): 更新 doc.go 文件目录和 IMPLEMENTATION_PLAN.md 状态"
```

---

### Task 9: 全量测试验证

**Files:** 无修改

- [ ] **Step 1: 运行 checkpointer 包全部测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/checkpointer/... -v -cover -count=1`

Expected: 所有测试 PASS，覆盖率 ≥ 85%

- [ ] **Step 2: 运行依赖 checkpointer 的其他包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/... -count=1`

Expected: 所有测试 PASS（确保 Clear 签名修改没有破坏下游）

- [ ] **Step 3: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -tags test ./... -count=1 2>&1 | tail -30`

Expected: 无失败

- [ ] **Step 4: 更新记忆**

将 5.9 相关的决策更新到 memory 文件。
