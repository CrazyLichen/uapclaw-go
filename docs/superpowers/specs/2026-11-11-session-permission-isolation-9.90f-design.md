# 9.90f 会话适配器权限隔离（R-4）设计

> 9.90e 的遗留项。来源：9.90d-2 在 `deep_adapter_sessions.go` 留的 `⤵️ R-4 权限隔离未完成`。

## 一、缺口是什么

Python `getOrCreateSessionAdapter` 在复用已有会话适配器前，有一组重建判据。其中 R-4 是：

```python
# interface_deep.py :4131-4143
if existing is not None and existing._permission_state.permission_isolated:
    if not existing._permission_state.permission_cleanup_complete:
        await existing._isolate_permission_instance()
    if not existing._permission_state.permission_cleanup_complete:
        raise RuntimeError("permission_session_cleanup_pending")
    self._drop_session_adapter_cache_entry(
        sid, remove_lock=False, remove_runtime_state=False,
    )
    existing = None
```

**语义**：某个会话适配器的权限运行时被判定损坏（隔离）后，**不允许再被复用**。要么严格执行一次隔离清理，要么直接报错拒绝——不能带着半损坏的权限状态继续跑。

Go 侧该判据完全缺失，缓存里已被隔离的适配器会被正常复用。

## 二、依赖盘点

### 2.1 已具备（9.90e 结束时核实）

| 能力 | Go 位置 | 状态 |
|---|---|---|
| `SessionPermissionState` 等价物 | — | ❌ 缺 |
| `_isolate_permission_instance` | — | ❌ 缺 |
| `instance.stop()` | `DeepAgent.Stop` `deep_agent.go:1826` | ✅ |
| 逐 rail 反注册 | `DeepAgent.UnregisterRail` `deep_agent.go:703`（内部已清 callback manager） | ✅ |
| `callback_manager.clear()` | `AgentCallbackManager.Clear` `callback.go:938` | ✅ |
| `ability_manager.teardown_tools()` | `AbilityManager.TeardownTools` `ability_manager.go:329` | ✅ |
| 取 instance | `AgentAdapter.Instance() *harness.DeepAgent` `interface.go:68` | ✅ |
| 列举全部 rail（`configured_rails`） | — | ❌ 缺（`pendingRails`/`registeredRails` 字段在，无导出访问器） |
| 丢弃缓存条目 | `dropSessionAdapterCacheEntry(sid, removeLock, removeRuntimeState)` `deep_adapter_sessions.go:269` | ✅ |

> **修正 9.90e 末尾的判断**：当时说「`unregister_rail` / `teardown_tools` 等 SDK 能力 Go 侧未移植」，核实后**全部已有生产实现**。真正的缺口只有 3 项：状态结构体、隔离方法本身、`ConfiguredRails` 访问器。

### 2.2 需新增

1. `SessionPermissionState`（56 行 Python → 约 80 行 Go）
2. `DeepAgent.ConfiguredRails()`（3 行）
3. `_isolate_permission_instance` 等价方法（52 行 Python → 约 90 行 Go）
4. R-4 判据分支插入 `getOrCreateSessionAdapter`

## 三、任务拆分

### Task 1：`SessionPermissionState`

**位置**：`internal/swarm/server/adapter/permission_runtime_state.go`（新文件）

**对应**：`jiuwenswarm/server/runtime/agent_adapter/permission_runtime_state.py`（56 行）

字段与方法逐一对应：

| Python | Go |
|---|---|
| `permission_epoch: str \| None` | `PermissionEpoch *string` |
| `pending_permission_epoch` | `PendingPermissionEpoch *string` |
| `pending_installed_permissions: dict \| None` | `PendingInstalledPermissions map[string]any` |
| `permission_update_in_progress: bool` | `PermissionUpdateInProgress bool` |
| `permission_isolated: bool` | `PermissionIsolated bool` |
| `permission_cleanup_complete: bool` | `PermissionCleanupComplete bool` |
| `permission_cleanup_candidates: list` | `PermissionCleanupCandidates []any` |
| `stage_pending_permission_capture()` | 同名 CamelCase |
| `clear_pending_permission()` | 同名 |
| `begin_permission_isolation()` | 同名 |
| `updating()` (contextmanager) | ⚠️ 见偏差 D-1 |
| `publish(epoch)` | 同名 |
| `track_cleanup(rails)` | 同名 |
| `finish_cleanup(*, complete)` | 同名 |

**偏差 D-1**：Python 的 `updating()` 是 `@contextmanager`（try/finally 成对置位）。Go 无 contextmanager，改为 `Updating(fn func())` 接受闭包，语义等价（进入置 true、退出置 false，含 panic 路径用 defer）。

### Task 2：`DeepAgent.ConfiguredRails()`

**位置**：`internal/agentcore/harness/deep_agent.go`

```go
// ConfiguredRails 返回全部 queued + registered rails（pending 在前）。
// Python: configured_rails (deep_agent.py:2089-2096)
func (d *DeepAgent) ConfiguredRails() []agentinterfaces.AgentRail {
    return append(append([]agentinterfaces.AgentRail{}, d.pendingRails...), d.registeredRails...)
}
```

**注意**：必须返回新切片，不能让调用方改动内部列表。

同时加入 `interfaces.DeepAgentInterface`（规则 10：DeepAgent 能力一律进该接口）。

### Task 3：`_isolate_permission_instance`

**位置**：`internal/swarm/server/adapter/deep_adapter_permission_isolation.go`（新文件）

**对应**：`interface_deep.py:12085-12136`（52 行）

步骤严格按 Python 顺序：

1. `begin_permission_isolation()`（置 `permission_isolated=true`、`cleanup_complete=false`）
2. 若 instance 非 nil：
   - `instance.stop()`，失败记入 failures
   - `rails = configured_rails() + permission_cleanup_candidates`，按 `id()` 去重
   - 逐个 `instance.unregister_rail(rail)`，失败记入 failures
   - 对 `instance.agent_callback_manager` 与 `instance.react_agent.agent_callback_manager` 分别 `clear()`，失败记入 failures
3. 若 instance 为 nil 且有 tool_cards：用 owner-qualified AbilityManager 拆除
4. `cleanup()`；若 instance 非 nil，`ability_manager.teardown_tools()`；
   若 `configured_rails()` 仍非空 → `raise permission_cleanup_rails_remaining`
5. `finish_cleanup(complete = failures 为空)`

**⚠️ 关键语义**：`failures` 只是收集，**不中断流程**——每个失败都记下来继续往下做，最后统一判定 `cleanup_complete`。这是「尽力清理」而非「快速失败」，顺序不能改。

### Task 4：R-4 判据插入

**位置**：`internal/swarm/server/adapter/deep_adapter_sessions.go:198` 附近

把 `⤵️ R-4` 占位替换为：

```
if existing != nil && existing 的 PermissionState.PermissionIsolated {
    if !PermissionCleanupComplete {
        _ = isolatePermissionInstance(existing)
    }
    if !PermissionCleanupComplete {
        return nil, errors.New("permission_session_cleanup_pending")
    }
    dropSessionAdapterCacheEntry(sid, removeLock=false, removeRuntimeState=false)
    existing = nil
}
```

**前置**：需要给 `AgentAdapter` 加权限状态访问器（`PermissionState() *SessionPermissionState`），否则拿不到 `existing` 的状态。

## 四、已知偏差

| 编号 | Python | Go | 性质 | 理由 | 回收条件 |
|---|---|---|---|---|---|
| D-1 | `@contextmanager updating()` | `Updating(fn func())` | 语言能力缺失 | Go 无 contextmanager | 不可回收 |
| D-2 | `except BaseException` 捕获 | `recover()` | 语言机制差异 | Go 无异常，用 recover 兜 panic | 不可回收 |
| D-3 | `id(rail)` 去重 | rail 指针地址 | 等价映射 | Go 指针即身份 | 不适用 |
| D-4 | 私有方法 `_isolate_permission_instance` | 包内非导出 `isolatePermissionInstance` | 命名空间差异 | 同名 CamelCase，可见性由包决定 | 不适用 |

## 五、验收清单

- [ ] `SessionPermissionState` 全部 7 字段 + 8 方法对齐，单测覆盖 ≥85%
- [ ] `DeepAgent.ConfiguredRails()` 返回 pending+registered 且为新切片，单测验证
- [ ] `ConfiguredRails` 已加入 `DeepAgentInterface`
- [ ] `isolatePermissionInstance` 五步顺序与 Python 一致，单测覆盖各失败分支
- [ ] R-4 判据三分支：未隔离直通 / 隔离且清理成功→丢弃复用 / 隔离且清理失败→报错
- [ ] `⤵️ R-4` 注释消失，`deep_adapter_sessions.go` 无该占位
- [ ] 集成测试：构造隔离态适配器 → 复用时被拒绝或丢弃
- [ ] `go build ./...` / `go vet ./...` 通过
- [ ] 全量回归零新增 FAIL
- [ ] `IMPLEMENTATION_PLAN.md` 新增 9.90f 行

## 六、不做的事

- **不实现** `_replace_permission_group`（Python `interface_code.py:637-643`）——它属于权限分组热替换，与「隔离」是不同场景
- **不改** `_has_live_root_permission_owner`（Python `:3710-3722`）——那是空闲驱逐判据，与本判据并列但独立

## 七、对应 Python 文件

| Python | 行数 | Go 目标 |
|---|---|---|
| `permission_runtime_state.py` | 56 | `permission_runtime_state.go`（新） |
| `interface_deep.py:12085-12136` | 52 | `deep_adapter_permission_isolation.go`（新） |
| `interface_deep.py:4131-4143` | 13 | `deep_adapter_sessions.go`（改） |
| `deep_agent.py:2089-2096` | 8 | `deep_agent.go`（加访问器） |

合计约 130 行 Python → 约 200 行 Go。
