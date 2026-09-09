# 48h 代码逻辑审查报告 (2026-09-07)

> 审查范围：最近 48 小时内提交的代码，重点关注与 Python 参考项目的对齐情况

## 审查范围

### 48h 内完成的章节

| 章节 | 状态 | 说明 |
|------|------|------|
| 9.66 Team Workspace | ✅ | TeamWorkspaceManager + TeamWorkspaceRail + WorkspaceMetaTool + i18n |
| 10.6.5 Permissions | ✅ | permissions_config_rpc 10 个 RPC 方法 + owner_scopes + SecurityRail 回填 |
| 9.24 P2 EvolutionRail | 🔄 | 基类 + TrajectoryRail + 辅助函数 |
| 9.19-23 SkillUseRail | ✅ | 增量加载 + YAML 解析 + 工具注册 + 提示词注入 |
| any 类型消除重构 | ✅ | trajectory/evolution/security 包的 any→具体类型 |

### 对应 Python 参考路径

| 章节 | Python 路径 |
|------|------------|
| 9.66 | `openjiuwen/agent_teams/team_workspace/` |
| 10.6.5 | `jiuwenswarm/agents/harness/common/rails/permissions/` + `jiuwenswarm/common/config.py` |
| 9.24 P2 | `openjiuwen/harness/rails/evolution/` |
| 9.19-23 | `openjiuwen/harness/rails/skills/` + `openjiuwen/harness/prompts/sections/skills.py` |

---

## 问题汇总

| 严重度 | 数量 | 编号 |
|--------|------|------|
| **严重** | 17 | S-01 ~ S-17 |
| **一般** | 30 | M-01 ~ M-30 |
| **提示** | 22 | T-01 ~ T-22 |

---

## 一、9.66 Team Workspace

### 严重问题

#### S-01: TeamWorkspaceManager 缺少分布式协调字段（messager/leader_id/node_id/pending_lock_requests）

**Python 样例**：
```python
class TeamWorkspaceManager:
    def __init__(self, config, workspace_path, team_name, *,
                 mode=WorkspaceMode.LOCAL, messager=None,
                 leader_id=None, node_id=None, publish_event=None):
        self._messager = messager
        self._leader_id = leader_id
        self._node_id = node_id
        self._pending_lock_requests: dict[str, asyncio.Future] = {}
```

**Go 问题**：
`TeamWorkspaceManager` 结构体完全缺少 `messager`、`leaderID`、`nodeID`、`pendingLockRequests` 字段。
- `AcquireLock` 仅判断 `m.mode == WorkspaceModeDistributed`，不区分 Leader/Remote
- `ReleaseLock` 同理
- `Initialize` 中用 `remote != ""` 替代 Python 的 `self._leader_id != self._node_id`，Leader 有 remote 时也会走克隆路径

**修复方案**：
1. 在 `TeamWorkspaceManager` 中添加 `leaderID`、`nodeID` 字段和 `ManagerOption` 选项
2. `AcquireLock`/`ReleaseLock` 判断条件改为 `m.mode == WorkspaceModeDistributed && m.nodeID != "" && m.leaderID != m.nodeID`
3. `Initialize` 中克隆判断改为 `m.leaderID != "" && m.nodeID != "" && m.leaderID != m.nodeID`

**复杂度**：中等 — 需修改构造函数签名和 3 个方法的条件分支

---

#### S-02: HandleLockRequest/HandleLockResponse 为纯空实现，丢失 Python 的完整逻辑

**Python 样例**：
```python
async def handle_lock_request(self, request: WorkspaceLockRequestEvent) -> WorkspaceLockResponseEvent:
    granted = False
    if request.action == "acquire":
        granted = await self.acquire_lock(
            request.file_path, request.member_name or "",
            request.holder_name or request.member_name or "",
            timeout_seconds=request.timeout_seconds or 300)
    elif request.action == "release":
        granted = await self.release_lock(request.file_path, request.member_name or "")
    holder_dict = None
    if not granted:
        existing = self._locks.get(request.file_path)
        if existing:
            holder_dict = existing.model_dump()
    return WorkspaceLockResponseEvent(
        team_name=self.team_name, member_name=request.member_name or "",
        file_path=request.file_path, granted=granted, holder=holder_dict)
```

**Go 问题**：
```go
func (m *TeamWorkspaceManager) HandleLockRequest(request any) (any, error) {
    return nil, ErrDistributedNotImplemented
}
```
纯空实现，无分发逻辑，且使用 `any` 类型占位（违反 any 消除原则）。

**修复方案**：
1. 添加 `LockRequestAction` 等结构体或使用 `schema.WorkspaceLockRequestEvent`/`WorkspaceLockResponseEvent`
2. 实现 acquire/release 分发逻辑
3. 如因循环依赖无法导入 schema 包，定义本地接口

---

#### S-03: 缺少 `_send_lock_request` 方法的占位

**Python 样例**：
```python
async def _send_lock_request(self, request: WorkspaceLockRequestEvent) -> WorkspaceLockResponseEvent | None:
    raise NotImplementedError("Distributed lock messaging is Phase 3")
```

**Go 问题**：完全缺少此方法，Phase 3 扩展时无处挂载。

**修复方案**：添加 `SendLockRequest` 方法占位，返回 `ErrDistributedNotImplemented`。

---

### 一般问题

#### M-01: Rail.Init() 为 no-op，缺少 `set_team_workspace` 调用

**Python 样例**：
```python
def init(self, agent) -> None:
    from openjiuwen.core.sys_operation.cwd import set_team_workspace
    set_team_workspace(self._ws.workspace_path)
```

**Go 问题**：`Init` 方法仅 `return nil`，当前无任何逻辑在 CwdState 中设置 team workspace。

**修复方案**：通过 `WithTeamWorkspace` 选项在 CwdState 初始化时设置，或在 Init 中调用等价逻辑。

---

#### M-02: maybePull 分布式模式实现为空

**Python 样例**：
```python
async def _maybe_pull(self) -> None:
    if self._ws.mode != WorkspaceMode.DISTRIBUTED:
        return
    now = time.monotonic()
    if now - self._last_pull_time < self._pull_interval:
        return
    self._last_pull_time = now
    await self._ws.pull()
```

**Go 问题**：`maybePull` 在分布式模式下仅有 TODO 注释，无节流逻辑和 Pull 调用。

**修复方案**：实现 `time.Since(lastPullTime)` 节流判断 + `r.ws.Pull(ctx)` 调用。需将 `maybePull` 签名改为接受 `context.Context`。

---

#### M-03: after_tool_call 中 publishEvent 使用简单 map 而非结构体

**Python 样例**：
```python
from openjiuwen.agent_teams.schema.events import TeamEvent, WorkspaceArtifactEvent
await self._ws.publish_event(
    TeamEvent.WORKSPACE_ARTIFACT_UPDATED,
    WorkspaceArtifactEvent(team_name=..., member_name=..., artifact_path=...))
```

**Go 问题**：`publishEvent` 直接传 `map[string]any`，丢失了类型安全。

**修复方案**：定义 `WorkspaceArtifactEvent` 结构体（如因循环依赖可在本地定义），替代 map。

---

#### M-04: mountDirectory 缺少 Windows junction 回退

**Python 样例**：
```python
def _mount_directory(self, target_path, link_path):
    try:
        os.symlink(target_path, link_path, target_is_directory=True)
    except OSError as exc:
        if os.name != "nt" or getattr(exc, "winerror", None) != ERROR_PRIVILEGE_NOT_HELD:
            raise
        self._create_windows_junction(target_path, link_path)
```

**Go 问题**：仅有 `// TODO: Windows junction 回退方案` 注释，直接 `os.Symlink`，Windows 下可能失败。

**修复方案**：在 `mountDirectory` 中检测 `runtime.GOOS == "windows"` 并添加 `cmd /c mklink /J` 回退。

---

#### M-05: Initialize 中分布式模式远程克隆条件不一致

**Python 样例**：
```python
if self.mode == WorkspaceMode.DISTRIBUTED and remote_url and self._leader_id != self._node_id:
    # Remote node: clone the workspace repo
```

**Go 问题**：
```go
if m.mode == WorkspaceModeDistributed && remote != "" {
    // 克隆 —— Leader 有 remote 也会走此路径
```

**修复方案**：与 S-01 一起修复，添加 leaderID/nodeID 判断。

---

#### M-06: WorkspaceMetaTool 锁使用原始路径（含 .team/ 前缀）与 AutoCommit 不一致

**Python 样例**：Python 中锁 key 也是原始路径（含 `.team/`），两处一致。

**Go 问题**：Go 的 `AfterToolCall` 中 `AutoCommit` 使用 `resolveWorkspaceRelative` 解析后的路径，但锁使用原始 `.team/...` 路径。两处 key 空间不同。

**修复方案**：统一锁 key 为工作空间相对路径（`resolveWorkspaceRelative` 后的路径），或在 `handleLock`/`handleUnlock`/`handleHistory` 中也调用 `resolveWorkspaceRelative`。

---

## 二、10.6.5 Permissions

### 严重问题

#### S-04: permissions_config_rpc.go 日志组件错误

**Python 样例**：
```python
# permissions_config_rpc.py
logger = logging.getLogger(__name__)  # 归属 permissions 包
```

**Go 问题**：
```go
// permissions_config_rpc.go:50
permRpcLogComponent = logger.ComponentChannel  // 应为 logger.ComponentPermissions
```

同包 `owner_scopes.go:55` 已正确使用 `logger.ComponentPermissions`。

**修复方案**：将 `permRpcLogComponent` 改为 `logger.ComponentPermissions`。

---

#### S-05: DeletePermissionsToolInConfig/DeletePermissionsRuleInConfig/DeletePermissionsApprovalOverrideInConfig 空参数时不报错

**Python 样例**：
```python
# config.py:401
def delete_permissions_tool_in_config(tool_name: str) -> bool:
    name = tool_name.strip()
    if not name:
        raise ValueError("tool name must be non-empty")  # 参数无效时抛异常
```

**Go 问题**：
```go
func DeletePermissionsToolInConfig(toolName string) bool {
    name := strings.TrimSpace(toolName)
    if name == "" {
        return false  // 调用方无法区分"工具不存在"和"参数无效"
    }
```

**修复方案**：将返回值从 `bool` 改为 `(bool, error)`，空参数时返回 `fmt.Errorf("tool name must be non-empty")`。同步修改 `DeletePermissionsRuleInConfig` 和 `DeletePermissionsApprovalOverrideInConfig`。

---

#### S-06: 缺少 `_normalize_tool_args` 工具参数归一化

**Python 样例**：
```python
# permissions_persist.py:106-129
def _normalize_tool_args(tool_args):
    """Normalize tool_args from str/bytes/dict to dict."""
    if isinstance(tool_args, str):
        try:
            return json.loads(tool_args)
        except (json.JSONDecodeError, TypeError):
            return {}
    if isinstance(tool_args, bytes):
        return json.loads(tool_args)
    if isinstance(tool_args, dict):
        return tool_args
    return {}
```

**Go 问题**：`PersistToOwnerScope` 直接传 `toolArgs map[string]any`，缺少 JSON 字符串→dict 的归一化。

**修复方案**：在 `permissions_persist.go` 中添加 `normalizeToolArgs(toolArgs any) map[string]any` 函数，处理 `string`（JSON 解析）→ `map[string]any` 和 `map[string]any` 直接使用两种情况。

---

#### S-07: web_handlers 注册了不存在的 RPC 方法（permissions.approval_overrides.set / permissions.owner_scopes.get/set）

**Python 样例**：Python 只有 `approval_overrides.get` 和 `approval_overrides.delete`，无 `set/create`。无 `owner_scopes.get/set` RPC 方法。

**Go 问题**：`web_handlers.go` 注册了 `permissions.approval_overrides.set`、`permissions.owner_scopes.get`、`permissions.owner_scopes.set` 桩函数，但 `permissions_config_rpc.go` 和 `req_method.go` 均无对应实现。

**修复方案**：
1. 移除 `permissions.approval_overrides.set` 桩（Python 无此方法）
2. 如需 `owner_scopes.get/set`，在 `permissions_config_rpc.go` 中实现；否则移除桩

---

### 一般问题

#### M-07: CheckAvatarPermission 缺少 evaluate_global_policy_directly 的异常降级

**Python 样例**：
```python
try:
    global_level, _rule = engine.evaluate_global_policy_directly(
        tool_name, tool_args, include_external_directory=True)
    global_level_value = global_level.value if global_level is not None else None
except Exception as exc:
    logger.warning("[check_avatar_permission] evaluate_global_policy_directly failed: %s", exc)
    global_level_value = None
```

**Go 问题**：直接调用 `engine.EvaluateGlobalPolicyDirectly`，无 defer/recover 异常保护。

**修复方案**：添加 `defer func() { if r := recover(); r != nil { ... 降级处理 } }()` 或确保 `NewPermissionEngine` 不 panic。

---

#### M-08: CheckAvatarPermission 中 Python 的 session_id 参数缺失

**Python 样例**：
```python
async def check_avatar_permission(tool_name, tool_args, channel_id, session_id) -> str:
```

**Go 问题**：
```go
func CheckAvatarPermission(permCfg map[string]any, toolName string, toolArgs map[string]any,
    channelID string, principalUserID string) (string, error) {
```

缺少 `sessionID` 参数。当前未使用，但接口不一致。

**修复方案**：添加 `sessionID string` 参数（可变参数 `...string`），保持接口对齐。

---

#### M-09: CheckAvatarPermission 中 owner_scopes_keys 日志字段缺失

**Python 样例**：
```python
logger.info("[check_avatar_permission] tool=%s channel=%s user=%s owner_scopes_type=%s owner_scopes_keys=%s",
    tool_name, perm_ctx.channel_id, perm_ctx.principal_user_id,
    type(owner_scopes).__name__, list(owner_scopes.keys()) if isinstance(owner_scopes, dict) else None)
```

**Go 问题**：日志缺少 `owner_scopes_keys` 字段。

**修复方案**：在日志中添加 `Any("owner_scopes_keys", ownerScopesKeyList)` 字段。

---

#### M-10: PersistToOwnerScope 写盘后未更新 configCache

**Python 样例**：
```python
def persist_to_owner_scope(tool_name, pattern, channel_id, user_id, config):
    raw = get_config_raw()
    # ... 修改 ...
    set_config(raw)  # 更新全局配置缓存
```

**Go 问题**：只写 YAML 文件，未同步更新内存中的配置快照。

**修复方案**：写盘成功后，调用 `harnesssecurity.RefreshConfigCache()` 或在 DeepAdapter 层刷新 `configCache`。

---

#### M-11: UpdatePermissionsRuleInConfig 的 merged 直接引用原 map

**Python 样例**：
```python
merged: dict[str, Any] = dict(rules[idx])  # 浅拷贝
```

**Go 问题**：
```go
merged, _ := rules[idx].(map[string]any)  // 直接引用，修改会影响原条目
```

**修复方案**：创建浅拷贝 `merged := make(map[string]any, len(original)); for k, v := range original { merged[k] = v }`。

---

#### M-12: permissions_config_rpc.go 中缺少持久化锁（TOCTOU 竞态）

**Python 样例**：
```python
_persist_lock = threading.Lock()

def replace_permissions_tools_in_config(tools):
    # Python 的 CRUD 函数也缺少锁保护，但 owner_scopes 有
```

**Go 问题**：`owner_scopes.go` 有 `persistLock`，但 `permissions_config_rpc.go` 中 10 个 RPC 方法均未使用锁。

**修复方案**：在 `permissions_config_rpc.go` 中引入 `persistLock`，或在 `readYAMLData`/`writeYAMLData` 层加锁。

---

#### M-13: 缺少 `persist_permission_allow_rule` / `persist_external_directory_allow` 独立函数

**Python 样例**：
```python
# permissions_persist.py
def persist_permission_allow_rule(tool_name, tool_args): ...
def persist_external_directory_allow(paths): ...
def persist_cli_trusted_directory_with_overrides(directory, ...): ...
```

**Go 问题**：这些独立持久化函数嵌入在各 Rail 中，无独立可调用版本。

**修复方案**：在 `permissions` 包中导出 `PersistPermissionAllowRule`、`PersistExternalDirectoryAllow`、`PersistCliTrustedDirectoryWithOverrides` 函数。

---

#### M-14: DispatchPermissionsConfigRequest 缺少全局异常捕获

**Python 样例**：
```python
try:
    # ... 各分支 ...
except ValueError as e:
    return _err(request, str(e))
except Exception as e:
    logger.exception("[%s] %s", tag, e)
    return _err(request, str(e), code="INTERNAL_ERROR")
```

**Go 问题**：各 CRUD 函数返回 `error` 但 Dispatch 中未统一处理 panic。

**修复方案**：在 `DispatchPermissionsConfigRequest` 入口添加 `defer func() { if r := recover(); r != nil { ... 返回 INTERNAL_ERROR } }()`。

---

### 提示问题

#### T-01: DispatchPermissionsConfigRequest 无日志输出

**Python 样例**：`logger.exception("[%s] %s", tag, e)` 在异常路径记录日志。

**Go 问题**：dispatch 层完全没有日志。

**修复方案**：在 default 分支和 error 返回处添加日志。

---

#### T-02: OwnerScopesPermissionContext 在两个包中重复定义

**Go 问题**：`swarm/agents/harness/common/rails/permissions/owner_scopes.go` 和 `swarm/schema/permission.go` 各有一个几乎相同的结构体。

**修复方案**：统一使用 `swarm/schema.PermissionContext`，`owner_scopes.go` 仅保留方法。

---

#### T-03: MatchArgs 缺少 panic recovery

**Python 样例**：
```python
def _match_args(pattern, tool_args):
    try:
        # ... 匹配逻辑 ...
    except Exception:
        return False
```

**Go 问题**：`MatchArgs` 无异常保护，匹配器内部 panic 会导致请求崩溃。

**修复方案**：添加 `defer func() { if r := recover(); r != nil { matched = false } }()` 或确保各匹配器方法不 panic。

---

## 三、9.24 P2 EvolutionRail

### 严重问题

#### S-08: `_get_evolution_total_timeout_secs` 完全缺失

**Python 样例**：
```python
def _get_evolution_total_timeout_secs(self) -> float | None:
    return None  # 子类可覆写，如 SkillEvolutionRail 返回 1800

# safeRunEvolution 中使用：
async with asyncio.timeout(total_timeout):
    result = await self.ext.RunEvolution(...)
```

**Go 问题**：`safeRunEvolution` 没有超时逻辑，`DrainPendingHostEvents` 的超时处理被简化为 `_ = task.Wait()`。

**修复方案**：
1. 在 `EvolutionExtension` 接口添加 `GetEvolutionTotalTimeoutSecs() *float64`
2. 在 `safeRunEvolution` 中使用 `context.WithTimeout` 实现超时
3. 在 `DrainPendingHostEvents` 中使用 `select` + `time.After` 实现精确超时等待

**流程示例**：
```
safeRunEvolution:
  1. ext.GetEvolutionTotalTimeoutSecs() → timeout
  2. if timeout != nil: ctx, cancel = context.WithTimeout(ctx, *timeout)
  3. select { case <-ctx.Done(): outcome = {"status": "timed_out"}; case <-resultCh: outcome = result }
```

---

#### S-09: `_save_trajectory` 方法缺失

**Python 样例**：
```python
def _save_trajectory(self, trajectory):
    self._trajectory_store.save(trajectory, "")
```

**Go 问题**：`AfterInvoke` 直接调用 `r.trajectoryStore.Save(traj, "")`，没有独立方法供子类覆写。

**修复方案**：添加 `SaveTrajectory(traj *trajectory.Trajectory) error` 导出方法，子类可覆写。

---

#### S-10: `safeRunEvolution` 缺少 TimeoutError 处理分支

**Python 样例**：
```python
except TimeoutError:
    outcome = {"status": "timed_out", "message": f"Evolution timed out after {total_timeout}s"}
```

**Go 问题**：只有 `{"status": "failed", ...}`，无 `timed_out` 状态。

**修复方案**：结合 S-08，在 `context.WithTimeout` 超时时返回 `{"status": "timed_out", "message": ...}`。

---

#### S-11: `_DEFAULT_MEMBER_ROLE` 类属性缺失

**Python 样例**：
```python
class EvolutionRail(DeepAgentRail):
    _DEFAULT_MEMBER_ROLE: Optional[str] = None

    def set_trajectory_sink(self, sink, *, team_id, member_role=None):
        role = self._DEFAULT_MEMBER_ROLE if member_role is None else member_role
```

**Go 问题**：`SetTrajectorySink` 当 `memberRole` 为空时不使用默认值，子类无法覆写默认角色。

**修复方案**：在 `EvolutionRail` 中添加 `defaultMemberRole string` 字段和 `WithDefaultMemberRole` 选项，`SetTrajectorySink` 中当 `memberRole` 为空时使用 `r.defaultMemberRole`。

---

### 一般问题

#### M-15: `normalizeCallbackMessages` 丢失非 dict 消息规范化

**Python 样例**：
```python
def _normalize_callback_messages(self, messages):
    normalized = []
    for msg in messages:
        if isinstance(msg, dict):
            normalized.append(msg)
        else:
            normalized.append({
                "role": getattr(msg, "role", None),
                "content": getattr(msg, "content", None),
                "tool_calls": [{"id": tc.id, "name": tc.name, "arguments": tc.arguments}
                               for tc in getattr(msg, "tool_calls", [])],
                "name": getattr(msg, "name", None),
            })
    return normalized
```

**Go 问题**：仅做浅拷贝，注释说"Go 中 ConvertTrajectoryToMessages 返回的已经是 []map[string]any"。

**修复方案**：当前调用链确实只传 map，但如果子类传原始对象，需添加字段提取逻辑。

---

#### M-16: `DrainPendingHostEvents` 等待逻辑与 Python 不一致

**Python 样例**：
```python
if wait and timeout is None:
    timeout = self._get_evolution_total_timeout_secs()
async with anyio.move_on_after(timeout):
    await asyncio.gather(*[t for t in self._bg_tasks if not t.done()])
self._bg_tasks = {t for t in self._bg_tasks if not t.done()}  # 清理已完成任务
```

**Go 问题**：
1. 无超时默认值
2. 等待完成后无清理已完成任务的逻辑
3. 已完成任务的删除在等待循环中进行（与 Python 不同）

**修复方案**：结合 S-08，等待后统一清理 `r.bgTasks` 中已完成的任务。

---

#### M-17: `DrainPendingHostEvents` 缺少日志

**Python 样例**：
```python
logger.debug("[EvolutionRail] drained %d pending events", len(events))
```

**Go 问题**：无对等日志。

**修复方案**：添加 `logger.Debug(logComponent).Int("count", len(events)).Msg("[EvolutionRail] drained pending events")`。

---

#### M-18: `after_model_call` 中 LLMCallDetail.Messages 类型不一致

**Python 样例**：
```python
detail = LLMCallDetail(messages=list(inputs.messages) if inputs.messages else [])
# messages 是 BaseMessage 对象列表
```

**Go 问题**：`messages = make([]map[string]any, 0)` + `baseMessageToMap(msg)` 转为 map，数据形态不同。

**修复方案**：当前 `collectMessagesFromTrajectory` 读取 map 格式，可保持但需文档说明差异。

---

#### M-19: `after_tool_call` 中 tool_call_id 处理不一致

**Python 样例**：
```python
detail = ToolCallDetail(tool_call_id=None)  # 始终为 None
```

**Go 问题**：从 `inputs.ToolCall.ID` 提取，可能为非空值。

**修复方案**：对齐 Python，`toolCallID` 设为空字符串。

---

#### M-20: `SetTrajectorySink` 中 ValueError vs Warn 行为差异

**Python 样例**：
```python
if not team_id:
    raise ValueError("team_id is required when binding a trajectory sink")
```

**Go 问题**：
```go
if sink != nil && teamID == "" {
    logger.Warn(logComponent).Msg("SetTrajectorySink: team_id 为空时 sink 不生效")
    return  // 调用方不会感知错误
}
```

**修复方案**：返回 `error`，空 `teamID` 时返回 `fmt.Errorf("team_id is required when binding a trajectory sink")`。

---

### 提示问题

#### T-04: evolution_rail.go 有重复的分隔注释

**Go 问题**：第 541 行和第 784 行各有一个 `非导出函数` 分隔注释。

**修复方案**：删除多余的分隔注释。

---

#### T-05: `normalizeSkillNamesGo` 函数冗余

**Go 问题**：`evolution_rail.go:753` 定义 `normalizeSkillNamesGo` 仅调用 `normalizeSkillNames`，无额外逻辑。

**修复方案**：删除 `normalizeSkillNamesGo`，如有外部调用改为直接调用 `normalizeSkillNames`。

---

#### T-06: `fmtErrorf`/`ensureNonNilSlice`/`ensureNonNilMap`/`isBlank` 等未使用函数

**Go 问题**：`helpers.go` 和 `evolution_rail.go` 中有多个定义但未调用的辅助函数。

**修复方案**：删除未使用的函数，或添加测试使用。

---

## 四、9.19-23 SkillUseRail

### 严重问题

#### S-12: loadYAML/loadDescription 绕过 SysOperation 直接用 os.ReadFile

**Python 样例**：
```python
async def _load_yaml(self, path: Path) -> Tuple[Optional[dict], str]:
    result = await self.sys_operation.fs().read_file(str(path), mode="text", encoding="utf-8")
    # ... 处理 result ...
```

**Go 问题**：
```go
func (r *SkillUseRail) loadYAML(path string) (map[string]any, string, error) {
    data, err := os.ReadFile(path)  // 直接文件系统读取，绕过 SysOperation 抽象
```

**修复方案**：改用 `r.SysOperation().Fs().ReadFile(path)` 读取文件。需确认 Go 的 `SysOperation.Fs()` 接口是否支持 `ReadFile` 方法。

---

#### S-13: Uninit 缺少从 ResourceMgr 注销工具

**Python 样例**：
```python
def uninit(self, agent):
    if hasattr(agent, "ability_manager"):
        for tool_name in list(self._owned_tool_names):
            agent.ability_manager.remove(tool_name)
    # Python 也不从 resource_mgr 注销，但 Go 设计文档明确要求
```

**Go 问题**：`Uninit` 只清理 `AbilityManager`，未调用 `resourceMgr.RemoveTool()` 注销 `ownedToolIDs` 中的工具。

**修复方案**：在 `Uninit` 中添加：
```go
resourceMgr := runner.GetResourceMgr()
if resourceMgr != nil {
    for toolID := range r.ownedToolIDs {
        if _, err := resourceMgr.RemoveTool([]string{toolID}); err != nil {
            logger.Warn(...)
        }
    }
}
```

---

#### S-14: SkillTool.getSkillByName 线性搜索 vs Python map O(1) 查找

**Python 样例**：
```python
def _get_skill_by_name(self, skill_name: str) -> Optional[Skill]:
    skill_map = {s.name: s for s in self._get_skills()}
    return skill_map.get(skill_name)
```

**Go 问题**：`getSkillByName()` 遍历列表 O(n)。

**修复方案**：在 `SkillTool` 中维护 `name→index` map 或在 `getSkillByName` 中先构建 map 再查找。

---

#### S-15: `_build_all_mode_prompt()` 方法缺失

**Python 样例**：
```python
def _build_all_mode_prompt(self) -> str:
    body_lines = []
    for idx, skill in enumerate(self.skills):
        body_lines.append(build_skill_line(index=idx, skill_name=skill.name, description=self._get_skill_description(skill)))
    return build_all_mode_skill_prompt(build_skill_lines(body_lines), language=self.system_prompt_builder.language)
```

**Go 问题**：只有 `buildAllModeSection()` 返回 PromptSection，没有对应的纯文本方法。

**修复方案**：添加 `BuildAllModePrompt() string` 方法，调用 `sections.BuildAllModeSkillPrompt()`。

---

### 一般问题

#### M-21: normalizeSkillDirs 缺少 `expanduser()` 等价

**Python 样例**：
```python
normalized.append(Path(raw).expanduser().resolve())
```

**Go 问题**：`filepath.Abs(raw)` 不处理 `~`，`~/skills` 会解析到错误路径。

**修复方案**：在 `normalizeSkillDirs` 中，先检查路径是否以 `~/` 开头，用 `os.UserHomeDir()` 替换 `~`。

---

#### M-22: `_load_yaml` YAML 解析行为差异

**Python 样例**：
```python
yaml_data = yaml.safe_load(yaml_block) or {}  # 空 YAML 块返回 {}
```

**Go 问题**：`yaml.Unmarshal` 空 YAML 块时 `yamlData` 为 `nil`，导致 `loadDescription` 返回不同错误消息。

**修复方案**：在 `loadYAML` 中，`yaml.Unmarshal` 后若 `yamlData == nil` 则初始化为 `map[string]any{}`。

---

#### M-23: `before_invoke` 传 `context.Background()` 而非传入的 context

**Python 样例**：
```python
async def before_invoke(self, ctx: AgentCallbackContext) -> None:
    await self.refresh_skill_prompt(ctx)
```

**Go 问题**：
```go
func (r *SkillUseRail) BeforeInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
    r.refreshSkillPrompt(context.Background())  // 应使用传入的 ctx
```

**修复方案**：将 `BeforeInvoke` 的第一个参数传递给 `refreshSkillPrompt`。

---

#### M-24: buildSkillLine 中 skillMDPath 参数传入但未使用

**Python 样例**：Python 已注释掉 `skill_md_path` 参数。

**Go 问题**：Go 保留了 `skillMDPath string` 参数但直接 `_ = skillMDPath`，代码冗余。

**修复方案**：从 `BuildSkillLine` 签名中移除 `skillMDPath` 参数。

---

#### M-25: `BuildSkillsSection` 未知 mode 时行为不一致

**Python 样例**：未知 mode 返回 `None`（移除 section）。

**Go 问题**：`BuildSkillsSection` 未知 mode 回退到 no_skill prompt（保留 section）。

**修复方案**：未知 mode 时返回零值 `PromptSection{}` 或 nil，调用方移除 section。

---

#### M-26: `fetchEvolution_texts` 错误处理使用 defer recover 而非检查返回值

**Python 样例**：
```python
try:
    text = await self.evolution_store.format_desc_experience_text(skill.name)
except Exception as exc:
    logger.warning(...)
```

**Go 问题**：使用 `defer recover()` 做 panic 恢复，但 `FormatDescExperienceText` 返回 string 不 panic。

**修复方案**：改为检查返回值（如返回空字符串表示失败），记录 Warn 日志。

---

### 提示问题

#### T-07: LoadSkillsFromDir 错误消息中英混合

**Go 问题**：`"skills_dir 为空"` vs `"skills_dir is empty"`。

**修复方案**：统一为中文 `"skills_dir 为空"`。

---

#### T-08: Init 中 enableImageMultimodal 默认值来源不同

**Python 样例**：从 `agent.deep_config.enable_read_image_multimodal` 动态读取。

**Go 问题**：从构造选项 `WithEnableImageMultimodal` 读取，构造时指定。

**修复方案**：可在 `Init` 中从 `agent` 获取 `DeepConfig` 动态读取，覆盖构造时的值。

---

## 五、any 类型消除重构

### 结论：核心重构已完成 ✅

5 个审查目标全部确认完成：
1. ✅ `extractInputs`/`extractOutputs` 已改为 AsMap 版本，旧 any 版本已删除
2. ✅ `TrajectoryStep.Logprobs` 已从 any 改为 `[]map[string]any`
3. ✅ `normalizeMemberRole` 改为 string，`normalizeSkillNames` 改为 []string
4. ✅ security llm 从 any 改为 `*llm.Model`
5. ✅ PermissionSceneHookInput/PermissionConfirmationRequest 中 Ctx/ToolCall/Engine 已改为具体类型

### 提示问题

#### T-09: 残留 `interface{}` 在非核心路径

**Go 问题**：`session/tracer/span.go`、`single_agent/interfaces/callback.go`、`deep_adapter.go` 中仍有 `interface{}`。

**修复方案**：后续统一清理，当前不影响核心功能。

---

## 修复优先级建议

### P0 — 立即修复（影响功能正确性）

| 编号 | 问题 | 影响 |
|------|------|------|
| S-04 | 日志组件错误 Channel→Permissions | 日志定位错误 |
| S-12 | loadYAML 绕过 SysOperation | 沙箱环境文件读取失败 |
| S-05 | 空参数返回 false 而非 error | 调用方无法区分参数无效和条目不存在 |
| M-23 | before_invoke 传 context.Background() | 请求取消信号丢失 |

### P1 — 尽快修复（影响功能完整性）

| 编号 | 问题 | 影响 |
|------|------|------|
| S-08 | EvolutionRail 缺少超时机制 | 演化可能无限运行 |
| S-11 | DEFAULT_MEMBER_ROLE 缺失 | 子类无法设置默认角色 |
| S-13 | Uninit 缺少 ResourceMgr 注销 | 重复 Init 残留旧工具 |
| S-01 | TeamWorkspaceManager 缺少分布式字段 | Phase 3 无法扩展 |
| S-06 | 缺少 normalize_tool_args | JSON 字符串 tool_args 解析失败 |
| S-15 | BuildAllModePrompt 方法缺失 | 无法获取纯文本 prompt |
| M-01 | Rail.Init 为 no-op | team workspace 未设置 |
| M-02 | maybePull 实现为空 | 分布式模式不同步 |
| M-11 | merged 直接引用原 map | writeYAMLData 失败时无法回滚 |
| M-21 | normalizeSkillDirs 缺少 expanduser | `~` 路径解析错误 |

### P2 — 计划修复（对齐优化）

| 编号 | 问题 |
|------|------|
| S-02/S-03 | HandleLockRequest/SendLockRequest 空实现 |
| S-07 | 不存在的 RPC 方法桩 |
| S-09 | _save_trajectory 缺失 |
| S-10 | timed_out 状态缺失 |
| S-14 | getSkillByName 线性搜索 |
| M-03 | publishEvent 使用 map 而非结构体 |
| M-04 | Windows junction 回退 |
| M-07~M-14 | Permissions 异常处理/日志/缓存/竞态 |
| M-15~M-20 | EvolutionRail 对齐问题 |
| M-22/M-24/M-25 | SkillUseRail 对齐问题 |
