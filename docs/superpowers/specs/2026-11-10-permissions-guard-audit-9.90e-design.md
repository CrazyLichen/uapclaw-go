# 9.90e permissions 护栏与审计对齐设计

> 制定日期：2026-11-10
> 上游：`jiuwenswarm/agents/harness/common/rails/permissions/`（5 文件）+ `jiuwenswarm/common/config.py`（11 函数）+ `openjiuwen/harness/security/permission_engine/approve/persist_rule_merge.py`（1 函数）+ `jiuwenswarm/agents/harness/common/rails/stream_event_rail.py`（3 处）
> 前置批次：9.90a ✅ / 9.90b ✅ / 9.90c ✅ / 9.90d-1 ✅ / 9.90d-2（已落地主体，本批回填其 17 处桩）
> 规则约束：规则 8（禁兼容壳）、规则 9/12（结构忠实、禁私自改架构层次与命名）、规则 10.1（接口扩张同改动内改完实现者）、规则 13.2（缺失项必须提出讨论）

---

## 一、本批在权限链路中的位置

```
用户发起工具调用
      ↓
PermissionEngine 分层规则判定（纯规则，零 LLM）              ← 9.90a / 9.90b
      ↓  ASK
┌── 自动审查层 ────────────────────────────────────────────┐
│  9.90c  root 队列串行化 + 意图窗口 + 中断所有权              │
│  9.90d  deterministic → route → LLM reviewer → 决策        │
│         每个决策点调 _emit_audit  ★ 本批接管               │
└──────────────────────────────────────────────────────────┘
      ↓
┌── 本批（9.90e）三项收口 ──────────────────────────────────┐
│  ① 审计旁路   audit.py → audit.go                          │
│     observe-only，异常全吞，绝不改变权限判定结果             │
│  ② 落盘补全   permissions_persist.py 缺失 5 函数            │
│     「总是允许」要把规则写进 YAML，下次直接命中 allow         │
│  ③ 中断分流   permission_interaction.py                     │
│     流事件消息里有没有权限交互 → 决定走撤销还是普通投影        │
│     撤销失败 → quarantine_session 永久隔离该会话              │
└──────────────────────────────────────────────────────────┘
      ↓
  前端 / 下一次工具调用
```

**三项作用的白话解释**

| 项 | 作用 | 为什么需要 |
|---|---|---|
| 审计 | 把每次权限决策清洗后记一行 JSONL | 事后追责；原始工具参数绝不入库，只留白名单字段的脱敏摘要 |
| 落盘 | 把「这次允许」固化成静态规则 | 下次同类调用直接命中 allow，不再走 LLM 审查，省钱也省用户打扰。**注意不是跳过判定**，是判定依据多了一条 |
| 中断分流 | 判断流消息里是否携带权限请求 | queue 缺失时权限中断没人接管，必须撤销前端卡片；否则用户面对一张永远等不到结果的审批卡 |

---

## 二、批次范围与体量

原计划 9.90e 约 700 行。实际盘点后为 **约 1,300-1,500 行**，用户 2026-11-10 决策 **不拆批，一批做完**。

| 序号 | 工作项 | Python 源 | 行数 | Go 交付物 |
|---|---|---|---|---|
| 1 | 审计事件发出 | `audit.py`（65） | 65 | 新建 `permissions/audit.go` + `audit_test.go` |
| 2 | 权限交互判据 | `permission_interaction.py`（54） | 54 | 新建 `permissions/permission_interaction.go` + 测试 |
| 3 | 会话隔离 + 中断分流 | `stream_event_rail.py` L675-701 / L811-820 | ~80 | `rails/stream_event_rail.go` 扩 2 方法 + 2 调用点 + 分流改写 |
| 4 | 审计桩回填（17 处） | — | ~100 | `auto_permission/*.go` 的 `⤵️ 9.90e` 桩全部换实调用 |
| 5 | persist 5 函数 + 1 异常 | `permissions_persist.py` L81/99/151/155/195/300 | ~340 | `permissions/permissions_persist.go` 补 |
| 6 | file guard 路径合并（**隐藏前置件**） | `persist_rule_merge.py:259`（74） | 74 | `agentcore/harness/security/patterns.go` 补 `MergeFileGuardPathRule` |
| 7 | config 层 11 函数 | `common/config.py` L1199-1500 | ~300 | `common/config/` 新建 `permissions.go` + 测试 |
| 8 | RPC 分发重构 | `permissions_config_rpc.py`（221） | 221 | `permissions/permissions_config_rpc.go` 从 23 函数收敛为 1 入口 + 3 私有 |
| 9 | owner_scopes 命名与逻辑对齐 | `owner_scopes.py` L61/99/104 | ~120 | `swarm/schema/permission.go` + `permissions/owner_scopes.go` |

---

## 三、Python 与 Go 逐文件对照（盘点，规则 13.4）

### 3.1 完全缺失（本批新建）

| Python 符号 | 文件:行 | Go 现状 | 说明 |
|---|---|---|---|
| `emit_permission_audit` | `audit.py:21-65` | 无（`runtime_bridge.go` 有 no-op 桩） | 本批落地 |
| `contains_permission_interaction` | `permission_interaction.py:15-51` | 无 | 本批落地 |
| `PERMISSION_RUNTIME_QUARANTINED_KEY` | `permission_interaction.py:10-12` | 无 | 字符串字面量，照抄不改 |
| `quarantine_session` | `stream_event_rail.py:675` | 无 | 本批落地 |
| `raise_if_quarantined` | `stream_event_rail.py:686` | 无 | 本批落地 |
| `persist_exact_permission_allow_rule` | `permissions_persist.py:195` | 无 | 106 行，本批最复杂 |
| `_check_approval_accesses` | `permissions_persist.py:155` | 无 | 42 行 |
| `_merge_file_guard_path_into_permissions` | `permissions_persist.py:81` | 无 | 18 行 |
| `build_command_allow_pattern` | `permissions_persist.py:99` | 无 | 3 行 |
| `_normalize_exact_accesses` | `permissions_persist.py:300` | 无 | 22 行 |
| `PermissionApprovalInvalidated` | `permissions_persist.py:151` | 无 | Go 用 `error` 哨兵值 |
| `merge_file_guard_path_rule` | `persist_rule_merge.py:259` | ⚠️ **已实现但未导出**（`patterns.go:953` 的 `mergeFileGuardPathRule`，仅同包 `MergeFileGuardAccessAllows` 调用，零测试） | **只需导出 + 补测试**，不需重写 |
| `setup_permission_context` | `owner_scopes.py:61` | 名字被改为 `NewPermissionContextFromRequest` 且逻辑简化 | 本批改正 |
| `cleanup_permission_context` | `owner_scopes.py:99` | 无 | 本批新增 |
| `current_permission_owner_scope` | `owner_scopes.py:104` | 名字被改（拆成 `PermissionContextFromCtx` + `OwnerScopeKey`） | 本批改正 |
| `dispatch_permissions_config_request` | `permissions_config_rpc.py:58` | 拆成 23 函数 | 本批收敛 |

### 3.2 部分对齐

| Python | Go 现状 | 差异 |
|---|---|---|
| `permissions_persist.py`（468 行） | `permissions_persist.go` 约 280 行 | 缺上表 5 函数 + 1 异常；已有 `PersistPermissionAllowRule` / `PersistExternalDirectoryAllow` / `PersistCliTrustedDirectoryWithOverrides` / `normalizeToolArgs` / `ensurePermissionsDict` / `ensureExternalDirectoryDict` / `ensureApprovalOverridesList` / `hasOverrideID` / `appendOverrideIfMissing` / `expandAndResolvePath` / `sortedSet` |
| `permissions_config_rpc.py`（221 行） | `permissions_config_rpc.go`（745 行 / 23 函数） | 业务逻辑下沉，见 §五 |

### 3.3 Go 侧多出（规则 8 要求删除）

| 符号 | 位置 | 判定依据 | 处置 |
|---|---|---|---|
| `GetPermissionsTools` / `ReplacePermissionsToolsInConfig` / `UpdatePermissionsToolInConfig` / `DeletePermissionsToolInConfig` / `GetPermissionsRules` / `GetPermissionsApprovalOverrides` / `CreatePermissionsRuleInConfig` / `UpdatePermissionsRuleInConfig` / `DeletePermissionsRuleInConfig` / `DeletePermissionsApprovalOverrideInConfig` / `UpdatePermissionsOwnerScopesInConfig` | `permissions_config_rpc.go` | Python 同名函数全在 `common/config.py` | 迁到 config 层后删除 |
| `loadLiveConfig` / `readYAMLData` / `writeYAMLData` | `permissions_config_rpc.go` | Python 配置读写全在 `common/config` | 迁到 config 层后删除 |
| `WithAgentPermissionContext` + `AgentRequest.PermissionContext` 字段 | `schema/agent.go:45,190` | **全仓零调用**（只有定义） | 删除 |
| `req.PermissionContext` 的 `else if` 分支 | `deep_adapter.go:1059,1354` | Python 无 | 删除 |

### 3.4 明确保留（经查证 Python 有对应）

| Go 符号 | Python 对应 | 结论 |
|---|---|---|
| `root_context_rail.go:279-281` / `:326-331` 的 `WithPermissionContextValue(ctx, owner/nil)` | `root_context_rail.py:231,272` `TOOL_PERMISSION_CONTEXT.set(owner if context else None)` | **保留**，有对应 |
| `root_context_rail.go:195` 附近的 reset 等价 | `root_context_rail.py:195` `TOOL_PERMISSION_CONTEXT.reset(tokens.owner)` | 保留（Go 用空闭包） |
| `WithPermissionContextValue` | Python `.set()` | 保留，作为底层注入手段 |
| `CheckAvatarPermission` | `check_avatar_permission`（`owner_scopes.py`） | 保留，命名已对齐 |

---

## 四、工作项设计

### 4.1 工作项 1：`permissions/audit.go`（65 行 Python）

**Python 原文结构**

```python
def emit_permission_audit(facts, *, decision, reason, degraded,
                          grant_id="", grant_reason="", extra=None,
                          persistent_writer=None) -> Any | None:
    try:
        event = build_sanitized_audit_fields(...)
        logger.info("permission_audit %s", json.dumps(event, sort_keys=True, separators=(",",":")))
    except Exception:
        pass
    if persistent_writer is None:
        return None
    try:
        return persistent_writer.write(...)
    except Exception:
        return None
```

**Go 签名**

```go
// AuditOptions 审计记录的决策事实。
// Python: emit_permission_audit 的关键字参数
type AuditOptions struct {
	// Decision 决策结果（allow / ask / deny）
	Decision string
	// Reason 决策原因码
	Reason string
	// Degraded 是否处于降级状态
	Degraded bool
	// GrantID 授权标识，空串不写入记录
	GrantID string
	// GrantReason 授权原因，空串不写入记录
	GrantReason string
	// Extra 白名单附加字段，非白名单键被丢弃
	Extra map[string]any
}

// EmitPermissionAudit 发出自动权限审计事件，不含原始工具参数。
// Python: emit_permission_audit (audit.py L21-65)
//
// 两段独立的异常边界：日志段与持久化段互不牵连，任一段失败都返回 nil，
// 绝不让审计改变权限判定结果（observe-only）。
func EmitPermissionAudit(
	facts ToolDecisionFacts,
	opts AuditOptions,
	persistentWriter *PersistentAuditWriter,
) *PersistentAuditWriteResult
```

**必须对齐的细节**

1. **两段独立 `try/except`**，不是一个大 try。第一段只包「构造 + 记日志」，第二段只包「写盘」。
2. 日志格式：`permission_audit ` + `json.dumps(event, sort_keys=True, separators=(",",":"))`。Go 用 `json.Marshal` 后需保证键有序。**注意**：Go 的 `encoding/json` 对 `map[string]any` 会按键名字典序输出，与 `sort_keys=True` 天然一致；但 `sanitize_audit_field` 返回的 `side_effects` 是 tuple，Go 侧是 `[]string`，需确认序列化为数组。
3. 异常吞掉时**不留日志**（Python 的 `except: pass`）—— 这是刻意的，因为审计失败本身不应产生噪声。
4. `persistent_writer is None` 时**直接返回 nil**，不进第二段。

**依赖**：`BuildSanitizedAuditFields`（9.90d-1 已落 `persistent_audit.go`）、`PersistentAuditWriter`（同）。

### 4.2 工作项 2：`permissions/permission_interaction.go`（54 行 Python）

**Python 原文结构**

```python
PERMISSION_RUNTIME_QUARANTINED_KEY = "_jiuwenswarm_permission_runtime_quarantined_v1"

def contains_permission_interaction(value: Any) -> bool:
    pending = [value]
    seen: set[int] = set()
    for _ in range(128):
        if not pending:
            return False
        item = pending.pop()
        identity = id(item)
        if identity in seen:
            continue
        seen.add(identity)
        if isinstance(item, Mapping):
            metadata = item.get("metadata")
            if isinstance(metadata, Mapping) and (
                "tool_invocation_key" in metadata
                or metadata.get("manual_approval_supported") is True
            ):
                return True
            if "tool_invocation_key" in item:
                return True
            pending.extend(item.values())
        elif isinstance(item, (list, tuple)):
            pending.extend(item)
        else:
            metadata = getattr(item, "metadata", None)
            if isinstance(metadata, Mapping) and (...):
                return True
            for name in ("payload", "value", "state"):
                nested = getattr(item, name, None)
                if nested is not None:
                    pending.append(nested)
    return bool(pending)
```

**Go 签名**（用户 2026-11-10 决策：严格照 Python，入参 `any`）

```go
// PermissionRuntimeQuarantinedKey 会话隔离标记在 session state 中的键。
// Python: PERMISSION_RUNTIME_QUARANTINED_KEY (permission_interaction.py L10-12)
//
// 键名含 v1 后缀，用于将来语义变更时平滑迁移；字面量必须与 Python 一字不差，
// 否则跨语言共享的会话状态读不到。
const PermissionRuntimeQuarantinedKey = "_jiuwenswarm_permission_runtime_quarantined_v1"

// ContainsPermissionInteraction 判断一个嵌套值是否携带宿主权限定位信息。
// Python: contains_permission_interaction (permission_interaction.py L15-51)
func ContainsPermissionInteraction(value any) bool
```

**必须对齐的细节**

1. **三条判据的短路顺序不可调换**：先查 `metadata.tool_invocation_key`，再查 `metadata.manual_approval_supported is True`，再查顶层 `tool_invocation_key`。
2. **`manual_approval_supported is True` 的 `is` 语义**：Python `is` 是同一性比较，`1` 不算（虽然 `1 == True`）。Go 需 `v, ok := m["manual_approval_supported"].(bool); ok && v` —— **不能**接受数字 1。
3. **128 层上限**：循环次数硬上限，不是递归深度。
4. **扫满 128 层返回 `len(pending) > 0`，不是 `false`**：fail-safe 方向——没扫完就无法确认干净，按「有权限交互」处理。
5. **`id()` 去重的 Go 等价**（见 §六 偏差 D-1）。
6. **类型分支顺序**：`Mapping` → `list`/`tuple` → 其他对象。Go 中 `map[string]any` 与 `[]any` 分别对应前两支；其他对象走反射取 `metadata` / `payload` / `value` / `state`。

**Go 侧的类型分派**

```go
switch typed := item.(type) {
case map[string]any:
	// 判据 1/2：查 metadata
	// 判据 3：查顶层 tool_invocation_key
	// 未命中 → 所有 value 入栈
case []any:
	// 所有元素入栈
default:
	// 反射：查 Metadata() 方法或 metadata 字段
	// 依次取 Payload() / Value() / State()
}
```

### 4.3 工作项 3：`stream_event_rail.go` 会话隔离与中断分流

**新增字段**

```go
// quarantinedSessions 已隔离会话集合（进程内快速判定）。
// Python: _quarantined_sessions (stream_event_rail.py L317)
quarantinedSessions map[string]struct{}
```

**新增方法**

```go
// QuarantineSession 永久隔离已损坏的运行时，直到所属会话被销毁。
// Python: quarantine_session (stream_event_rail.py L675-684)
func (r *JiuClawStreamEventRail) QuarantineSession(
	sessionID string,
	session sessioninterfaces.SessionFacade,
)

// RaiseIfQuarantined 在 fail-closed 清理失败后拒绝新工作。
// Python: raise_if_quarantined (stream_event_rail.py L686-701)
func (r *JiuClawStreamEventRail) RaiseIfQuarantined(
	sessionID string,
	session sessioninterfaces.SessionFacade,
) error
```

**双写语义**（用户 2026-11-10 决策：完全照 Python）

```
QuarantineSession:
  ① quarantinedSessions[normalizeSID(sessionID)] = struct{}{}     ← 进程内
  ② session.UpdateState(PermissionRuntimeQuarantinedKey, true)     ← 持久化
  ③ abort(sid)                                                    ← 真断任务

RaiseIfQuarantined:
  persisted = session.GetState(PermissionRuntimeQuarantinedKey) == true
  if _, inMemory := quarantinedSessions[sid]; inMemory || persisted {
      quarantinedSessions[sid] = struct{}{}      ← 从持久化回填内存
      return 隔离错误
  }
  return nil
```

**调用点 1**：`beforeInvoke`（对应 Python L771）。在解析出 sid 后、处理新输入前调用 `RaiseIfQuarantined`。

**调用点 2**：`cancelInterruptionSnapshot` 的异常出口（对应 Python L916）。Python 用 `except BaseException` 覆盖 `CancelledError`；**Go 需同时覆盖 error 返回与 panic**（见 §六 偏差 D-2）。

**中断分流改写**（对应 Python L811-820 的 `_project_complete_interruption`）

```go
queue := r.RootPermissionQueue()
if queue == nil {
	if permissions.ContainsPermissionInteraction(cbc.Inputs().Result()) {
		r.cancelInterruptionSnapshot(cbc, nil, "permission_queue_unavailable")
	} else if r.projectInterruptionSnapshots {
		r.projectOrdinaryInterruption(cbc)
	}
}
```

### 4.4 工作项 4：审计桩回填（17 处）

`⤵️ 9.90e 回填` 全量清单（`grep -rn "⤵️ 9.90e" internal/` 得）：

| 文件 | 处数 | 回填动作 |
|---|---|---|
| `auto_permission/runtime_bridge.go` | 2 | `emitAudit` 桩体换 `EmitPermissionAudit`；`emitPermissionResultObservation` 按 `permission_terminal` / `permission_pending` 两类记录 |
| `auto_permission/domain_grant_audit.go` | 3 | 同上 |
| `auto_permission/reviewer_execution.go` | 4 | 同上 |
| `auto_permission/reviewer_override_gate.go` | 1 | 同上 |
| `auto_permission/reviewer_override_consume.go` | 1 | 同上 |
| `auto_permission/deny_response.go` | 1 | 同上 |
| `auto_permission/manual_authorization.go` | 1 | 同上 |
| `auto_permission/deterministic.go` | 1 | 同上 |
| `auto_permission/artifact_tracking_capture.go` | 1 | 派生 ctx 回传给调用方（偏差 V-16） |
| `auto_permission/lifecycle.go` | 1 | `persistentAuditWriter any` → `*permissions.PersistentAuditWriter` |
| `permissions/policy_eval.go` | 1 | `send_file` 路径守卫接线（`SendFilePathGuardEvaluator` 已在 9.90d-1 落地，直接接） |
| `swarm/server/adapter/*.go` | 1 | `auditWriter any` → 具体类型 |

**完成信号**：`grep -rn "⤵️ 9.90e" --include=*.go internal/` 输出为空。

**接口扩张约束（规则 10.1）**：`lifecycle.go` 的 `PersistentAuditWriter any` 改具体类型属字段类型变更，不是接口扩张，但需同改动内改完所有构造点与测试。

### 4.5 工作项 5：`permissions_persist.go` 补 5 函数 + 1 异常

| Go | Python | 行 | 要点 |
|---|---|---|---|
| `ErrPermissionApprovalInvalidated` | `PermissionApprovalInvalidated` | — | Go 用 `var ErrXxx = errors.New(...)` 哨兵。Python 是 `ValueError` 子类，带 message；Go 需包装具体原因（`approval_accesses_changed` 等） |
| `MergeFileGuardPathIntoPermissions` | `_merge_file_guard_path_into_permissions` | 18 | 依赖 `MergeFileGuardPathRule`（工作项 6）。**Python 用 `clear()` + `update()` 原地改引用**，Go 侧 map 引用语义天然一致 |
| `BuildCommandAllowPattern` | `build_command_allow_pattern` | 3 | `cmd.strip() + " *"`。注意 Python 是尾部加 `" *"` 不是 `"*"` |
| `NormalizeExactAccesses` | `_normalize_exact_accesses` | 22 | 校验 `(path, action)` 二元组；action 必须 ∈ {read, write, exec}；path 反斜杠转正斜杠 + 去尾斜杠；去重保序。**任一不合格返回 nil（整体失败）**，不是跳过该项 |
| `CheckApprovalAccesses` | `_check_approval_accesses` | 42 | 见下 |
| `PersistExactPermissionAllowRule` | `persist_exact_permission_allow_rule` | 106 | 见下 |

**`CheckApprovalAccesses` 的 7 步**

1. 构造 `PermissionEngine(permissions, workspace_root=root, trusted_dirs=())`
2. `current_native_path_access(name, args)`
3. `evaluate_global_policy_directly(name, args, include_external_directory=(access is None))`
4. `build_file_guard_checker` → 有 access 则用 `access.guard_tool` / `guard_args`，否则用原名原参
5. access 非空且有 checker → `checker.evaluate` → 取 `path_result.permission`，与 level 取 `strictest`
6. `checker.collect_ask_accesses(guard_name, guard_args)`
7. 若 `name == SEND_FILE_TOOL_NAME` → `SendFilePathGuardEvaluator().evaluate(...)`；`status == "unevaluable"` **或** `level == "deny"` 都抛 `PermissionApprovalInvalidated(result.reason)`；否则 `asks.update(result.ask_accesses)`
8. 最后 `level.value == "deny"` → 抛 `current_policy_denied`

**`PersistExactPermissionAllowRule` 的 14 步**（Python L195-300）

关键步骤：

- `permission_storage_lock(session_id)` 包住全程
- `expected_global_permissions` 非空时先 `resolve_env_vars` 再比对，不等则抛 `installed_policy_changed`（`validate_only` 时不抛）
- `current_asks.issubset(accesses)` 不成立 → 抛 `approval_accesses_changed`
- `validate_only` 提前返回 true
- **durable 检查**：`session_only` 时 baseline = effective，否则 `compose(user, {})`。「user 侧授权必须在本 session 已有授权之外仍成立」
- 非 auto、非 session_only、当前已 ALLOW → 只检查 durable_level 是否 ALLOW 且无 asks，**不写盘直接返回**
- `evaluate_tiered_policy(baseline, name, args) == ASK` → `merge_permission_allow_rule_into_permissions`；失败抛 `approval_rule_unavailable`；`tools[name]` 变了才加 `allow_tools`；新增的 `approval_overrides` 追加
- `target_asks` 非空 → `merge_file_guard_access_allows(updated, sorted(target_asks))`；失败抛 `approval_paths_unavailable`
- **最终校验**：`compose(updated, {})` 后必须 `ALLOW` 且无 asks，否则抛 `approval_not_covered`
- `updated == current` → 直接返回 true（幂等）
- 写盘；`OSError` → 记日志返回 false

**异常处理**（Python 的三层）

```python
except PermissionApprovalInvalidated:
    if raise_on_invalid: raise
    return False
except Exception as exc:
    logger.exception(...)
    if raise_on_invalid: raise PermissionApprovalInvalidated("approval_validation_failed") from exc
    return False
```

Go 用 `errors.Is(err, ErrPermissionApprovalInvalidated)` 区分；非哨兵错误包装成 `approval_validation_failed`。

### 4.6 工作项 6：`MergeFileGuardPathRule`（隐藏前置件）

**Python**：`openjiuwen/harness/security/permission_engine/approve/persist_rule_merge.py:259-329`（71 行）
**Go 归属**：`internal/agentcore/harness/security/patterns.go`（与已落地的 `MergeFileGuardAccessAllows` / `MergePermissionAllowRuleIntoPermissions` 同文件）

```go
// MergeFileGuardPathRule 在 permissions 副本上合并一条 file_guard.paths 规则。
// Python: merge_file_guard_path_rule (persist_rule_merge.py L259-329)
//
// 返回 (merged, wroteAny)：merged 是深拷贝后的新 permissions，wroteAny 表示是否真有变更。
// 传入的 permissions 不被修改（Python 用 deepcopy）。
func MergeFileGuardPathRule(
	permissions map[string]any,
	path string,
	read string,
	write string,
	execAction string,
	match string,
) (map[string]any, bool)
```

**必须对齐的细节**

1. `path_norm = path.replace("\\","/").rstrip("/")`，空则返回 `deepcopy(permissions), false`
2. 先 `deepcopy`，再在副本上改
3. `fg["enabled"] = True`（**无条件设真**）
4. 遍历已有 `paths`，path_norm 相同则走「按轴升级」：
   - `_escalate_axis_toward_allow(existing, new)` 三个轴各自升级
   - `merged_match = existing.get("match","prefix") or match`
   - 全都没变 → 返回 `(perms, false)`
   - 有变 → **保留 existing 其他字段**（`**existing` 展开）再覆盖五个字段，返回 `(perms, true)`
5. 没找到 → append 新条目 `{path, read, write, exec, match}`，返回 `(perms, true)`
6. 两条路径都记 `logger.info`，字段顺序 path/read/write/exec

### 4.7 工作项 7：config 层 11 函数

**新建** `internal/common/config/permissions.go`，对齐 `jiuwenswarm/common/config.py` L1199-1500：

| Go | Python 行 | 要点 |
|---|---|---|
| `GetPermissionsTools` | 1239 | **读 `cfg["permissions"]["tools"]`**，不是 `cfg["tools"]`（修正现有 bug） |
| `ReplacePermissionsToolsInConfig` | 1248 | `_validate_tools_map` → `update_config(_mutate)` |
| `UpdatePermissionsToolInConfig` | 1261 | 合并单键，返回 `{"tools": merged}` |
| `DeletePermissionsToolInConfig` | 1294 | 键不存在返回 false |
| `GetPermissionsRules` | 1344 | **读 `cfg["permissions"]["rules"]`**（修正现有 bug） |
| `GetPermissionsApprovalOverrides` | 1353 | 读 `permissions.approval_overrides` |
| `CreatePermissionsRuleInConfig` | 1362 | 生成 id |
| `UpdatePermissionsRuleInConfig` | 1401 | 按 rule_id 打 patch |
| `DeletePermissionsRuleInConfig` | 1455 | 按 rule_id 删 |
| `DeletePermissionsApprovalOverrideInConfig` | 1480 | 按 override_id 删 |
| `UpdatePermissionsOwnerScopesInConfig` | 1199 | owner_scopes 更新 |

**同时迁入**：`loadLiveConfig` / `readYAMLData` / `writeYAMLData` / `validateToolsMap` / `normalizeRuleTools` / `normalizeRuleSeverityAction` / `randomHex` / `errPayload` / `toolNameFromParams` / `ParseParams` 中属于 config 职责的部分。

**不迁的（已确认对齐）**：`permissions_layers.go` 的 `LoadGlobalPermissions` / `LoadUserPermissions` / `LoadSessionPermissions` / `UpdateUserPermissions` / `ReadPermissionLayersLocked` / `SaveUserPermissionsLocked` / `SaveSessionPermissionsLocked` / `PermissionStorageLock` —— 它们对应 Python `rails/permissions/permissions_layers.py`，**本就在 rails 层**，位置正确。同理 `permission_compose.go` 的 `ComposeHostEffectivePermissions` 对应 `permission_compose.py`，也在 rails 层。

判断依据：Python 的 `permissions_config_rpc.py` 里 `from jiuwenswarm.common.config import (...)` 明确列了 11 个函数名，只有这些属于 config 层。其余通过 `from ...permissions.permissions_layers import (...)` 导入的，保持原位。

**已确认缺陷（本批一并修）**

```go
// 现状（错）：GetPermissionsTools
tools, _ := cfg["tools"].(map[string]any)
// Python（对）：cfg.get("permissions").get("tools")

// 现状（错）：GetPermissionsRules
rules, _ := cfg["rules"].([]any)
// Python（对）：cfg.get("permissions").get("rules")
```

写路径正确（用 `data["permissions"]`），**读写不对称**，前端「权限工具列表」当前读到空数据。

### 4.8 工作项 8：RPC 分发重构

**Python 结构**：1 个 `dispatch_permissions_config_request`（L58-221），内部按 11 个 `ReqMethod` 分支，业务逻辑全部 `from jiuwenswarm.common.config import ...`。

**Go 目标结构**：

```go
// DispatchPermissionsConfigRequest 执行一条 permissions 配置 RPC。
// Python: dispatch_permissions_config_request (permissions_config_rpc.py L58)
func DispatchPermissionsConfigRequest(
	reqMethod string,
	params map[string]any,
) (ok bool, payload map[string]any)

// 3 个私有
func permissionsConfigErr(message, code string) map[string]any  // → _err
func permissionsConfigOK(payload map[string]any) map[string]any // → _ok
func permissionsConfigMethods() []string                        // → get_permissions_config_req_methods
```

**11 个分支**（Python 的 `if m == ReqMethod.XXX` 序列，Go 用 `switch`）：

| 分支 | 委托 |
|---|---|
| `PERMISSIONS_TOOLS_GET` | `compose_host_effective_permissions(load_global, load_user, load_session(None))` |
| `PERMISSIONS_TOOLS_SET` | 拆 deny/ask/allow 三组 → `update_user_permissions` |
| `PERMISSIONS_TOOLS_UPDATE` | `update_permissions_tool_in_config(tool, level)` |
| `PERMISSIONS_TOOLS_UPDATE_USER` | 同上但写 user 层 |
| `PERMISSIONS_TOOLS_DELETE` | `delete_permissions_tool_in_config` |
| `PERMISSIONS_RULES_GET` | `get_permissions_rules` |
| `PERMISSIONS_RULES_CREATE` | `create_permissions_rule_in_config` |
| `PERMISSIONS_RULES_UPDATE` | `update_permissions_rule_in_config` |
| `PERMISSIONS_RULES_DELETE` | `delete_permissions_rule_in_config` |
| `PERMISSIONS_APPROVAL_OVERRIDES_GET` | `get_permissions_approval_overrides` |
| `PERMISSIONS_APPROVAL_OVERRIDES_DELETE` | `delete_permissions_approval_override_in_config` |

**参数校验顺序必须保持**：`isinstance(params, dict)` → 字段提取 → 字段非空校验。错误文案照抄（`"params must be object"` / `"tools must be object"` / `"tool is required"` / `"level is required"`）。

### 4.9 工作项 9：`owner_scopes` 命名与逻辑对齐

**改动清单**（用户 2026-11-10 决策：全部改成 Python 名 + 删旧）

| 操作 | Python | Go 位置 |
|---|---|---|
| 新增 | `setup_permission_context(request) → Token` | `swarm/schema/permission.go`：`SetupPermissionContext(ctx context.Context, request *AgentRequest) (context.Context, func())` |
| 新增 | `cleanup_permission_context(token)` | `swarm/schema/permission.go`：`CleanupPermissionContext(cleanup func())` |
| 新增 | `current_permission_owner_scope() → str` | `swarm/schema/permission.go`：`CurrentPermissionOwnerScope(ctx context.Context) string` |
| 删除 | — | `NewPermissionContextFromRequest` |
| 删除 | — | `PermissionContextFromCtx`（调用方改用 `CurrentPermissionOwnerScope` 或直接读 ctx） |
| 删除 | — | `WithAgentPermissionContext`（零调用） |
| 删除 | — | `AgentRequest.PermissionContext` 字段 |

**逻辑补齐**（Python L61-97 的两个分支）

```python
if not avatar_mode:
    if principal_user_id or meta.get("enable_memory") is False:
        # 建上下文（principal_user_id 用于 workspace authority）
    return None          # 两者都没有才返回 None
# avatar_mode=True → 建上下文
```

Go 现状只判断 `enable_memory is False`，**漏了 `principal_user_id` 非空分支**。后果：普通 IM 场景下 `permCtx` 为 nil → `permissionSceneHook` 直接返回 → **owner_scopes 权限不生效**。

**cleanup 语义**（用户 2026-11-10 决策：`context.Context` 派生 + 空闭包）

沿用项目既有惯例（`BindRootDecisionContext` / `BindCommandExecution`）：

```go
func SetupPermissionContext(ctx context.Context, request *AgentRequest) (context.Context, func()) {
	// ...构造 permCtx，可能为 nil
	return context.WithValue(ctx, toolPermContextKey{}, permCtx), func() {}
}
```

**这个空闭包是必须登记的偏差**（Python 的 `reset(token)` 有实质作用），理由：Go 的 `context.WithValue` 派生链不可变，丢弃派生 ctx 即恢复上一层绑定，无需显式 reset。

---

## 五、架构层次说明（规则 12.1-1）

Python 的 `permissions_config_rpc.py` 只是**分发器**：22 行 import + 11 个分支各 3-8 行，业务逻辑全在 `common/config.py`。

Go 现状把 `get_permissions_tools` 等 11 个函数**实现在 permissions 包内**，等于把 config 层职责下沉到 rails 层。这违反规则 12.1-1「Python 在 A 层做的事，Go 必须还在 A 层」。

**重构方向**：业务实现迁到 `internal/common/config/permissions.go`，`permissions_config_rpc.go` 只留分发。完成后 `permissions_config_rpc.go` 从 745 行降到约 250 行。

---

## 六、已知偏差（须用户确认，规则 9.6 / 12.3）

| 编号 | Python | Go 等价 | 性质 | 理由 | 回收条件 |
|---|---|---|---|---|---|
| **D-1** | `contains_permission_interaction` 用 `id(item)` 去重 | 用 `reflect.ValueOf(item).Pointer()`（map/slice 可取值）；无法取指针的类型跳过去重 | 语言能力缺失 | Python 所有对象都有 `id()`；Go 的 `any` 装箱后取不到原地址。128 层上限已能防住绝大部分循环 | Go 引入可比较的对象身份机制，或改用 `unsafe` 显式取值 |
| **D-2** | `except BaseException` 捕获 `CancelledError` | `defer` + `recover()` 覆盖 panic；error 返回正常传播 | 异常模型差异 | Go 无 `BaseException`/`Exception` 分层，panic 是唯一对应物 | 无（语言层限制） |
| **D-3** | `setup_permission_context` 返回 `contextvars.Token`，`cleanup_permission_context` 做 `reset(token)` | `SetupPermissionContext` 返回 `func(){}` 空闭包 | 并发模型差异 | Go 的 `context.WithValue` 派生链不可变，丢弃即恢复。项目已有先例（`BindRootDecisionContext` / `BindCommandExecution`） | 无（语言层限制） |
| **D-4** | `PermissionContext` 定义在 `owner_scopes.py` | 定义在 `swarm/schema/permission.go` | 文件归属 | 避免 `schema` ←→ `rails/permissions` 循环依赖；`schema` 是底层包，不能 import rails | 若将来 `PermissionContext` 移出 schema 且无循环依赖 |
| **D-5** | `PermissionApprovalInvalidated(ValueError)` 携带 message | `errors.New` 哨兵 + `fmt.Errorf("%w: %s", ...)` 包装原因 | 异常类型 | Go 无异常类继承；哨兵 + 包装已能 `errors.Is` 区分 | 无 |

---

## 七、删除清单（规则 8.3）

### 7.1 文件

无整文件删除。

### 7.2 符号

| 符号 | 位置 | 删除依据 |
|---|---|---|
| `GetPermissionsTools` | `permissions_config_rpc.go:210` | 迁 config 层（且修正 cfg 路径 bug） |
| `ReplacePermissionsToolsInConfig` | `:221` | 同上 |
| `UpdatePermissionsToolInConfig` | `:240` | 同上 |
| `DeletePermissionsToolInConfig` | `:271` | 同上 |
| `GetPermissionsRules` | `:306` | 同上（且修正 bug） |
| `GetPermissionsApprovalOverrides` | `:324` | 同上 |
| `UpdatePermissionsOwnerScopesInConfig` | `:359` | 同上 |
| `CreatePermissionsRuleInConfig` | `:382` | 同上 |
| `UpdatePermissionsRuleInConfig` | `:446` | 同上 |
| `DeletePermissionsRuleInConfig` | `:529` | 同上 |
| `DeletePermissionsApprovalOverrideInConfig` | `:566` | 同上 |
| `loadLiveConfig` / `readYAMLData` / `writeYAMLData` | `:617/628/638` | 迁 config 层 |
| `validateToolsMap` / `normalizeRuleTools` / `normalizeRuleSeverityAction` / `randomHex` | `:660/697/725/741` | 迁 config 层 |
| `NewPermissionContextFromRequest` | `swarm/schema/permission.go:279` | 改名 `SetupPermissionContext` |
| `PermissionContextFromCtx` | `swarm/schema/permission.go:267` | 改名为 `CurrentPermissionOwnerScope`（语义不同，见 §4.9） |
| `WithAgentPermissionContext` | `swarm/schema/agent.go:190` | 全仓零调用 |
| `AgentRequest.PermissionContext` 字段 | `swarm/schema/agent.go:45` | 同上 |
| `req.PermissionContext` 的 `else if` 分支 | `deep_adapter.go:1059,1354` | Python 无此路径 |
| `⤵️ 9.90e 回填` 注释 | 17 处 | 桩体换实调用后自然消失 |

### 7.3 注释与文档

- `permissions/doc.go` 文件树补 `audit.go` / `permission_interaction.go`
- `common/config/doc.go` 补 `permissions.go`
- `auto_permission/doc.go` 更新 `runtime_bridge.go` 描述（不再是 no-op 桩）
- `IMPLEMENTATION_PLAN.md` 9.90e 行状态回填

### 7.4 grep 锚点（验收）

```bash
grep -rn "⤵️ 9.90e" --include=*.go internal/          # 0 命中
grep -rn "NewPermissionContextFromRequest" --include=*.go internal/ tests/ cmd/   # 0 命中
grep -rn "PermissionContextFromCtx" --include=*.go internal/ tests/ cmd/          # 0 命中
grep -rn "WithAgentPermissionContext" --include=*.go internal/ tests/ cmd/        # 0 命中
grep -rn "func GetPermissionsTools" --include=*.go internal/swarm/   # 0 命中（已迁 config）
```

---

## 八、测试策略

### 8.1 单元测试

| 目标 | 覆盖率要求 | 关键用例 |
|---|---|---|
| `audit.go` | ≥85% | 两段异常边界独立（日志段失败不影响落盘、落盘失败不影响日志）；`persistentWriter == nil` 直接返回；`extra` 非白名单键被丢弃 |
| `permission_interaction.go` | ≥85% | 三条判据各自命中；`manual_approval_supported` 为 `1` **不**命中；128 层不命中返回 true（fail-safe）；循环引用不死循环；`list`/`tuple`/对象三种载体 |
| `persist` 5 函数 | ≥85% | `NormalizeExactAccesses` 全失败返回 nil；`CheckApprovalAccesses` 的 8 步短路；`PersistExactPermissionAllowRule` 的 14 步（重点：durable 检查、`approval_not_covered`、幂等返回、`raise_on_invalid` 三态） |
| `MergeFileGuardPathRule` | ≥85% | 新建 vs 同 path 合并；按轴升级；`unchanged` 返回 false；保留 existing 其他字段；`enabled` 无条件设真 |
| config 层 11 函数 | ≥85% | **`GetPermissionsTools` 读到非空数据**（bug 回归）；读写对称性 |
| RPC 分发 | ≥85% | 11 分支各 1 例 + 参数校验错误文案 |
| `owner_scopes` 改名 | ≥85% | `principal_user_id` 非空时建上下文（缺口回归）；`CurrentPermissionOwnerScope` 格式 `principal:{channel}:{uid}`；cleanup 后读不到 |
| 会话隔离 | ≥85% | 双写；进程内命中；**持久化命中后回填内存**（重启场景）；cleanup 失败 → 隔离；已隔离会话的 `beforeInvoke` 被拒 |

### 8.2 集成测试

`tests/integration/swarm/permissions/`（9.90d-2 已建目录）追加 3 个场景：

1. **审计-落盘-重放**：一次 ASK → LLM 允许 → 审计 JSONL 有记录 → 规则写进 YAML → 第二次同类调用直接 ALLOW 且不经 LLM
2. **中断分流**：queue 缺失 + 结果含权限交互 → 走撤销路径；queue 缺失 + 普通问答 → 走普通投影
3. **隔离恢复**：清理失败 → 隔离 → 新请求被拒 → 模拟进程重启（清内存 map）→ 仍被拒（从 session state 恢复）

### 8.3 验证命令

```bash
export GOPROXY=https://goproxy.cn,direct
go build ./... && go vet ./...

# 单包
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" \
  ./internal/swarm/agents/harness/common/rails/permissions/... \
  ./internal/swarm/agents/harness/common/rails/... \
  ./internal/common/config/... \
  ./internal/agentcore/harness/security/... \
  ./internal/swarm/schema/...

# 覆盖率
go test -coverprofile=coverage.out ./internal/swarm/agents/harness/common/rails/permissions/...
go tool cover -func=coverage.out

# 集成
CGO_ENABLED=1 go test -tags "integration sqlite_fts5 test" ./tests/integration/swarm/permissions/...
go run ./cmd/integration-audit -verify

# 全量
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...
```

---

## 九、执行顺序

依赖链决定顺序，越靠前的越不能后做：

```
Task 1  MergeFileGuardPathRule（工作项 6）        ← persist 的硬依赖
   ↓
Task 2  permissions_persist.go 5 函数 + 异常（工作项 5）
   ↓
Task 3  config 层 11 函数（工作项 7）             ← 顺带修 2 个读函数 bug
   ↓
Task 4  RPC 分发重构 + 删 17 个旧符号（工作项 8）
   ↓
Task 5  audit.go（工作项 1）                      ← 桩回填的硬依赖
Task 6  permission_interaction.go（工作项 2）
   ↓
Task 7  17 处桩回填（工作项 4）
Task 8  policy_eval send_file 接线
   ↓
Task 9  stream_event_rail 隔离 + 分流（工作项 3）
   ↓
Task 10 owner_scopes 改名 + 逻辑补齐（工作项 9）
   ↓
Task 11 集成测试 · Task 12 doc.go/计划表回填
```

---

## 十、风险

| # | 风险 | 缓解 |
|---|---|---|
| R1 | `PersistExactPermissionAllowRule` 106 行 14 步，单测难覆盖全部分支 | 每个 `PermissionApprovalInvalidated` 原因码各 1 例；依赖件用 fake（layers / compose 可注入） |
| R2 | owner_scopes 改名波及 3 包 5 处生产 + 50 处测试 | 先改 schema 层定义 → 编译错误驱动逐处改；测试改动机械可批量 |
| R3 | `ContainsPermissionInteraction` 的 `id()` 去重无法精确等价 | D-1 已登记；128 层上限作为主要保护；补一个循环引用测试证明不死循环 |
| R4 | config 层新增 11 函数可能与既有 `configutil` 接口冲突 | 先查 `internal/common/config/config.go` 现有导出符号，重名则加 `Permissions` 前缀 |
| R5 | 删除 `AgentRequest.PermissionContext` 字段可能破坏序列化兼容 | 已确认 `WithAgentPermissionContext` 零调用；`json` tag 带 `omitempty`，字段删除后旧 JSON 反序列化只是忽略该键 |

---

## 十一、验收清单

- [ ] `grep -rn "⤵️ 9.90e" --include=*.go internal/` = 0
- [ ] `grep -rn "NewPermissionContextFromRequest\|PermissionContextFromCtx\|WithAgentPermissionContext" --include=*.go internal/ tests/ cmd/` = 0
- [ ] `func GetPermissionsTools` 只在 `internal/common/config/` 出现
- [ ] `GetPermissionsTools` 单测返回非空 tools（bug 回归）
- [ ] `principal_user_id` 非空时 `SetupPermissionContext` 返回非 nil ctx（缺口回归）
- [ ] 会话隔离双写测试通过（含重启恢复场景）
- [ ] `audit.go` / `permission_interaction.go` / config 层 / persist 新增函数 / `stream_event_rail` 隔离 覆盖率均 ≥85%
- [ ] `go build ./...` / `go vet ./...` 通过
- [ ] 全量回归 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 零 FAIL
- [ ] 集成审计 `go run ./cmd/integration-audit -verify` 通过
- [ ] `IMPLEMENTATION_PLAN.md` 9.90e 行标 ✅ 并回填摘要
- [ ] 三处 `doc.go` 文件树同步
