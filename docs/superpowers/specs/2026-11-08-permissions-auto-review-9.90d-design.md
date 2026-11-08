# 9.90d 设计：权限自动审查层（Auto Permission）

> 对应 Python（本批范围内。该目录共 37 个 .py：9.90a/b/c 已完成 20 个、本批 15 个、9.90e 留 2 个）：
> - `jiuwenswarm/agents/harness/common/rails/permissions/`（本批 15 个：9.90d-1 占 7、9.90d-2 占 8，其中 `auto_permission_rail.py` 落子包其余入顶层）
> - `jiuwenswarm/agents/harness/common/rails/permissions/_auto_permission/`（21 文件）
> - `jiuwenswarm/agents/harness/common/rails/interrupt/permission_interrupt_rail.py`（417 行）
> - `jiuwenswarm/common/tool_display.py`（87）、`jiuwenswarm/common/workspace_paths.py`（518）
> - `jiuwenswarm/common/tools/command_runtime.py`（98）、`jiuwenswarm/common/tools/pdf_tools.py`（275）
> - `jiuwenswarm/runtime/run_permissions.py`（54）、`jiuwenswarm/server/runtime/sandbox_no_host_fallback.py`（38）
>
> 目标 Go 包：
> - `internal/swarm/agents/harness/common/rails/permissions/`（新增 10 文件：7 个属 9.90d-2、2 个属 9.90d-1，另有 `doc.go`/`permissions_layers.go` 等既有文件改造）
> - `internal/swarm/agents/harness/common/rails/permissions/auto_permission/`（共 22 文件 = 20 个新建 + `models.go`/`tool_binding.go` 扩展；另新增 `auto_permission_rail.go`、`base_rail.go` 已计入）
> - `internal/common/tool_display/`（新建）
> - `internal/swarm/agents/harness/common/workspace_paths.go`
> - `internal/swarm/agents/harness/common/tools/{command_runtime,pdf_tools}.go`
> - `internal/swarm/runtime/run_permissions.go`
> - `internal/swarm/server/runtime/sandbox_no_host_fallback.go`
> - `internal/agentcore/harness/rails/base.go`（新增 `MarkTrusted()`）
>
> 制定日期：2026-11-08
> 遵循规则：2（计划同步）、8（删除残留）、9（结构忠实）、10（接口归属）、11（CI 不主动关注）、12（不改层次/步骤/命名）、13（主动盘点缺失项）

---

## 一、定位与作用

### 1.1 在 agent 会话中的流程位置

```
agent 启动期
  internal/swarm/server/adapter/deep_adapter_rails.go · code_adapter.go
    └─ harnesssecurity.BuildPermissionInterruptRail(...)              ← Go 已有（9.19-23）
         └─ secrail.NewPermissionInterruptRail(config, engine, ...)   ← 基础权限轨（A/B/C 引擎）
         └─ 【9.90d-1 ★】permissions.JiuwenSwarmPermissionInterruptRail 包装层（417 行）
         └─ 【9.90d-2 ★】enableAutoPermission 分支：
              └─ auto_permission.AutoPermissionRail(baseRail = 上面的 rail, ...)

单次工具调用（BeforeToolCall）
  PermissionInterruptRail.BeforeToolCall
    └─ resolvePermissionInterrupt
         ├ 1. PermissionSceneHook 短路
         ├ 2. engine.CheckPermission → A/B/C 三段取最严               ← 9.90b 已落地
         │     ALLOW → 放行
         │     DENY  → 拒绝
         │     ASK   → ◀── 【9.90d 从这里接手】
         ├ 3.【9.90d-2 ★】auto 判定七步
         │     a. 提取 ToolInvocation（参数修复 / 命令归一 / 原生路径归一）
         │     b. ToolDecisionFacts + OpenJiuwenPolicyEvaluator 影子复算
         │     c. deterministic_*_route  确定性硬规则 → 直接 allow / deny
         │     d. post_policy_route + reviewer_route → route = deny / manual / semantic
         │     e. semantic → AutoReviewer（隔离 LLM + 超时 + redaction）
         │     f. reviewer_override_gate / consume（用户覆盖 token）
         │     g. 组装 PermissionHandlingResult + UI 元数据 + stream 元数据
         ├ 4. 三终点：allow / deny_response / 回 base 轨抛权限卡
         └ 5. audit  ← ⤵️ 9.90e（本批 no-op 桩）
```

### 1.2 一句话作用

**补上 `ASK` 之后的第二层判定**：先用确定性规则挡掉一批明显安全的调用，再让隔离 LLM 审查器基于「原始用户意图窗口 + 脱敏后的工具参数 + 决策事实」裁决，只有真正需要人拍板的才弹权限卡。

在此之前（截至 9.90c），Go 侧的行为是**规则说 ASK 就一律弹框问用户**——而 Python 在 `check_permission` 返回 ASK 之后还有整整一层 `auto_authorize`（确定性规则 → 意图硬规则 → LLM 审查）。本批把这层补齐。

### 1.3 同时回填 9.90c 的"管道就位、未接水源"

9.90c 落地的 `RootDecisionContext`（意图窗口）目前只被两个 root rail 读写，**没有任何判定逻辑消费它**。Python 侧的三处消费方全在本批：

| 消费方 | Python | 本批落地位置 |
|---|---|---|
| `_intent_view` | `auto_reviewer.py:548` | `auto_reviewer.go` |
| `original_user_intent` / `file_delivery_constraint_text` | `_auto_permission/before_tool.py` | `before_tool.go` |
| `has_valid_ask_user_clarification` | `url_safety.py:217` | `url_safety.go`（9.90d-1） |

`root_context.go:753/770/789` 的三处 `⤵️ 消费方待 9.90d` 注释在本批删除（规则 8）。

### 1.4 为什么 A 类 5 文件必须提前（原在 9.90e）

9.90d 的四个文件**硬 import** `url_safety`：

```python
# auto_decision.py:21
from ...url_safety import (RecentUrlSource, absolute_uri_hard_block_reason,
                           evaluate_reviewable_url_scope, inspect_network_scope)
# auto_reviewer.py:44 / reviewer_route.py:26 / artifact_authorization.py:19 同样引入
```

`persistent_audit` 被 `audit.py:17` 的 `build_sanitized_audit_fields` 直接依赖；`trusted_search_urls` 是 `lifecycle.py:24` 的构造器入参类型；`send_file_path_guard` 是 `policy_eval.py:23` 的 `path_result`（Go 侧当前恒 nil，语义失真）；`generated_artifact_delivery` 被 `invocation_context` / `artifact_authorization` / `before_tool` 三处依赖。

缺任何一个都无法编译或语义不完整。性质与 9.90b 必补 `command_canonicalize.py` 相同——不是"可选功能"，是前置条件。

---

## 二、Go 侧现状盘点（规则 13.4）

### 2.1 上游文件清单与 Go 现状对照

| Python 文件 | 行数 | Go 现状 | 归属 |
|---|---|---|---|
| `auto_decision.py` | 639 | ❌ 0 | 9.90d-2 |
| `auto_reviewer.py` | 888 | ❌ 0 | 9.90d-2 |
| `auto_config.py` | 315 | ❌ 0 | 9.90d-2 |
| `auto_permission_rail.py` | 75 | ❌ 0 | 9.90d-2 |
| `permission_compose.py` | 362 | ⚠️ **最小实现**（`permissions_layers.go:1419`） | 9.90d-2 |
| `reviewer_redaction.py` | 689 | ❌ 0 | 9.90d-2 |
| `reviewer_route.py` | 330 | ❌ 0 | 9.90d-2 |
| `reviewer_stream_metadata.py` | 57 | ❌ 0 | 9.90d-2 |
| `openjiuwen_contract.py` | 183 | ❌ 0 | **9.90d-1** |
| `permission_interrupt_rail.py` | 417 | ❌ 0 | **9.90d-1** |
| `url_safety.py` | 541 | ❌ 0 | **9.90d-1**（A 类） |
| `persistent_audit.py` | 296 | ❌ 0 | **9.90d-1**（A 类） |
| `trusted_search_urls.py` | 236 | ❌ 0 | **9.90d-1**（A 类） |
| `send_file_path_guard.py` | 315 | ❌ 0（仅 `SendFilePathGuardResult` 空壳） | **9.90d-1**（A 类） |
| `generated_artifact_delivery.py` | 175 | ❌ 0 | **9.90d-1**（A 类） |
| `_auto_permission/` 21 文件 | 4,568 | 仅 `models.go`(186) + `tool_binding.go`(166) | 9.90d-2 |
| `common/tool_display.py` | 87 | ❌ 0 | **9.90d-1**（B 类） |
| `common/workspace_paths.py` | 518 | ❌ 0 | **9.90d-1**（B 类） |
| `common/tools/command_runtime.py` | 98 | ❌ 0 | **9.90d-1**（B 类） |
| `common/tools/pdf_tools.py` | 275 | ❌ 0 | **9.90d-1**（B 类） |
| `runtime/run_permissions.py` | 54 | ❌ 0 | **9.90d-1**（B 类） |
| `server/runtime/sandbox_no_host_fallback.py` | 38 | ❌ 0 | **9.90d-1**（B 类） |
| `audit.py` | 65 | ❌ 0 | ⤵️ **9.90e** |
| `permission_interaction.py` | 54 | ❌ 0 | ⤵️ **9.90e** |

### 2.2 旧逻辑清单（将被替代，规则 13.1）

| 对象 | 位置 | 处置 |
|---|---|---|
| `ComposeHostEffectivePermissions` 最小实现体（约 90 行） | `permissions_layers.go:1419` | 函数体替换为 `permission_compose.go` 完整实现，**签名保留** |
| 该函数的 `⤴️ 9.90d 回填` 注释 | `permissions_layers.go:1406-1418` | 删除 |
| `doc.go` 第 1、2 条已知缺口 | `permissions/doc.go:26-41` | 删除 |
| `root_context.go` 三处 `⤵️ 消费方待 9.90d` | `root_context.go:753/770/789` | 删除 |
| `policy_eval.go:234` 的 `⤵️ 9.90e 回填` | `policy_eval.go` | 删除 |
| `auto_permission/doc.go` 4 常量未启用说明 | `auto_permission/doc.go` | 删除 |
| 该文件 11 常量"消费方在 9.90d" | 同上 | 删除 |

### 2.3 缺失项清单（规则 13.2，已讨论决策）

| 编号 | 缺口 | 规模 | 决策 |
|---|---|---|---|
| **A1** | `url_safety.py`（原排 9.90e） | 541 | 提前进 9.90d-1 |
| **A2** | `persistent_audit.py`（原排 9.90e） | 296 | 提前进 9.90d-1 |
| **A3** | `trusted_search_urls.py`（原排 9.90e） | 236 | 提前进 9.90d-1 |
| **A4** | `send_file_path_guard.py`（原排 9.90e） | 315 | 提前进 9.90d-1 |
| **A5** | `generated_artifact_delivery.py`（原排 9.90e） | 175 | 提前进 9.90d-1 |
| **B1** | `common/tool_display.py`（计划表 0 章节） | 87 | 提前进 9.90d-1，落 `internal/common/tool_display/` |
| **B2** | `runtime/run_permissions.py`（0 章节） | 54 | 提前进 9.90d-1 |
| **B3** | `server/runtime/sandbox_no_host_fallback.py`（0 章节） | 38 | 提前进 9.90d-1 |
| **B4** | `common/workspace_paths.py`（0 章节） | 518 | 提前进 9.90d-1 全量 |
| **B5** | `common/tools/command_runtime.py`（0 章节） | 98 | 提前进 9.90d-1 |
| **B6** | `common/tools/pdf_tools.py`（0 章节） | 275 | 提前进 9.90d-1 全量（含 `read_pdf`） |
| **B7** | `permission_interrupt_rail.py`（417，**计划表 0 章节**） | 417 | 提前进 9.90d-1 完整落 |

### 2.4 本批总量

| 批次 | 文件数 | 行数 | 构成 |
|---|---|---|---|
| **9.90d-1** | 13 个 Python 文件（permissions 包 7 + B 类 6）+ `rails/base.go` 改造 + `base_rail.go` 接口 | **3,333** | A 类 1,563（含在 permissions 包 7 内）+ B 类 1,070 + `openjiuwen_contract` 183 + `permission_interrupt_rail` 417 + `MarkTrusted()` 约 40 + `BasePermissionRail` 接口约 60 |
| **9.90d-2** | 29（顶层 7 + 子包 22 = 21 个新建/扩展 + `auto_permission_rail.go`） | **7,923** | 顶层 7 文件 3,280 + 子包 21 文件 4,568 + `auto_permission_rail.go` 75 |
| 合计 | 42 + 2（改造/新增接口） | **11,256** | |

测试代码预计 **约 7,000 行**（实现:测试 ≈ 1:0.63，与 9.90b/9.90c 惯例接近）。

---

## 三、结构对照（规则 9.4 / 12.4）

### 3.1 `permissions/` 顶层文件（9.90d-2 的 7 个 + 9.90d-1 的 2 个，另 `auto_permission_rail.go` 落子包）

| Python | Go | 方法数 |
|---|---|---|
| `auto_config.py` | `auto_config.go` | 5 导出 + 15 私有 |
| `permission_compose.py` | `permission_compose.go` | 1 导出 + 13 私有 |
| `openjiuwen_contract.py`（9.90d-1） | `openjiuwen_contract.go` | 1 类 + 12 函数 |
| `permission_interrupt_rail.py`（9.90d-1） | `permission_interrupt_rail.go` | 1 类 + 2 函数 |
| `reviewer_redaction.py` | `reviewer_redaction.go` | 13 导出 + 11 私有 + 11 常量 |
| `reviewer_route.py` | `reviewer_route.go` | 2 导出 + 11 私有 |
| `reviewer_stream_metadata.py` | `reviewer_stream_metadata.go` | 3 函数 + 1 常量 |
| `auto_decision.py` | `auto_decision.go` | 5 导出 + 13 私有 |
| `auto_reviewer.py` | `auto_reviewer.go` | 3 类 + 3 协议 + 22 函数 |
| `auto_permission_rail.py` | `auto_permission/auto_permission_rail.go` | 1 类 + 2 文件级导出 |

#### `permission_compose.go`（替代最小实现）

14 个函数逐个对齐：

| Python | Go |
|---|---|
| `_package_builtin_rules_enabled` | `packageBuiltinRulesEnabled` |
| `_as_dict` | `asDict` |
| `_scalar_level` | `scalarLevel` |
| `_named_levels` | `namedLevels` |
| `_pick_level` | `pickLevel` |
| `_merge_list_of_dicts` | `mergeListOfDicts` |
| `_apply_product_p1_rule_actions` | `applyProductP1RuleActions` |
| `_net_action` | `netAction` |
| `_merge_net_urls` | `mergeNetUrls` |
| `_as_shell_guard_flag` | `asShellGuardFlag` |
| `_compose_shell_guard` | `composeShellGuard` |
| `_compose_net_guard` | `composeNetGuard` |
| `_fail_closed` | `failClosed` |
| `compose_host_effective_permissions` | `ComposeHostEffectivePermissions`（签名不变） |

**关键**：`auto_mode=True` 分支依赖 `normalize_permissions_for_runtime`（`auto_config.py:79`）—— 这是 `permission_compose.go` 与 `auto_config.go` 的循环调用点，Go 同为同包函数调用，无环问题。

#### `permission_interrupt_rail.go`（9.90d-1，417 行，计划表此前 0 章节）

`JiuwenSwarmPermissionInterruptRail(PermissionInterruptRail)` 是 **JiuwenSwarm 侧子类**，包装 `openjiuwen` 父类。Go 落点：

```go
// Python: class JiuwenSwarmPermissionInterruptRail(PermissionInterruptRail)
type JiuwenSwarmPermissionInterruptRail struct {
    *secrail.PermissionInterruptRail   // 嵌入父类（Go 的"继承"）
    // ... 子类独有字段
}
```

**22 个方法/函数**逐个对齐：

| Python | 行 | 说明 |
|---|---|---|
| `mark_plan_hard_reject` | 38 | 标记 plan 硬拒 |
| `plan_hard_rejected` | 42 | 判定 plan 硬拒 |
| `_ExactPersistAttempt` | 47 | 精确持久化尝试类型 |
| `__init__` | 68 | 构造 |
| `_project_native_path_guard` | 89 | 原生路径守卫投影 |
| `_sync_engine_workspace_root` | 97 | 同步 engine 工作区根 |
| `update_config` | 131 | 继承转发 |
| `set_trusted_dirs` | 136 | 继承转发 |
| `_collect_file_guard_persist_accesses` | 140 | 收集 file_guard 持久化 |
| `installed_permission_config` | 163 | **9.90d-2 消费** |
| `get_permission_snapshot` | 166 | **9.90d-2 消费** |
| `permission_check_context` | 187 | **9.90d-2 消费** |
| `_get_user_input` | 201 | 取用户输入 |
| `_approval_arguments_digest` | 208 | 审批参数摘要 |
| `bind_approval_request` | 213 | **9.90d-2 消费** |
| `_invoke_permissions_hook` | 231 | 权限钩子 |
| `resolve_interrupt` | 252 | 中断解析 |
| `_resolve_interrupt_with_run_policy` | 292 | 带 run policy 解析 |
| `_persist_allow_always` | 355 | 持久化 always allow |
| `_persist_session_allow` | 377 | 持久化 session allow |
| `_persist_merged_allow` | 384 | 持久化合并 allow |
| `_remove_exact_auto_confirm` | 390 | 移除精确 auto confirm |
| `before_tool_call` | 402 | 入口（`plan_hard_rejected` 短路 + 父类调用 + AbortError 绑定） |

### 3.2 `auto_permission/` 子包（21 文件）

| Python | 行 | Go | 说明 |
|---|---|---|---|
| `models.py` | 101 | `models.go`（**已存在，需扩**） | `ToolInvocation` / `PermissionHandlingResult` / `PermissionInterruptRequest` + 11 常量 |
| `lifecycle.py` | 165 | `lifecycle.go` | `AutoPermissionLifecycleMixin`（15 字段初始化） |
| `invocation_context.py` | 715 | `invocation_context.go` | 工具调用提取 / 参数修复 / 命令归一 |
| `runtime_result.py` | 250 | `runtime_result.go` | 结果还原（5 个函数不实现，见 V-3） |
| `runtime_bridge.py` | 316 | `runtime_bridge.go` | 三终点桥接 |
| `reviewer_metadata.py` | 401 | `reviewer_metadata.go` | UI 元数据 |
| `reviewer_execution.py` | 389 | `reviewer_execution.go` | LLM 审查执行 |
| `reviewer_audit.py` | 77 | `reviewer_audit.go` | 路由审计附加 |
| `reviewer_override_gate.py` | 67 | `reviewer_override_gate.go` | 覆盖门控 |
| `reviewer_override_consume.py` | 84 | `reviewer_override_consume.go` | 覆盖消费 |
| `deterministic.py` | 131 | `deterministic.go` | 确定性沙箱放行 |
| `manual_authorization.py` | 90 | `manual_authorization.go` | 手工授权响应 |
| `domain_manual.py` | 43 | `domain_manual.go` | 域手工响应 |
| `domain_grant_audit.py` | 162 | `domain_grant_audit.go` | 域授权审计 |
| `deny_response.py` | 86 | `deny_response.go` | 拒绝响应 |
| `artifact_authorization.py` | 132 | `artifact_authorization.go` | 文件交付授权 |
| `artifact_tracking_capture.py` | 23 | `artifact_tracking_capture.go` | after_tool_call 捕获 |
| `artifact_declaration.py` | 15 | `artifact_declaration.go` | 声明解析 |
| `code_tool_bindings.py` | 110 | `code_tool_bindings.go` | 代码工具绑定（仅 agent_mode 三工具，见 V-7） |
| `readonly_tool_bindings.py` | 320 | `readonly_tool_bindings.go` | 只读工具信任背书（仅 ProgressiveToolRail，见 V-6） |
| `before_tool.py` | 891 | `before_tool.go` | **七步编排主入口** |
| `__init__.py` | 1 | — | 无需对应 |

**已存在的两个文件**：
- `models.go` 现 186 行（`ToolInvocation` / `PermissionHandlingResult` / `PermissionInterruptRequest` / 11 常量 / `NewPermissionHandlingResult`）
- `tool_binding.go` 现 166 行（`MatchesBoundMethod` / `ResolveToolBinding` / `identityEqual` / `funcPtrEqual`）
- `tool_binding.py`（25 行）对应 Python 在**顶层** `permissions/tool_binding.py`，Go 放在子包 —— 这是 9.90a 已登记的既有布局。

### 3.3 mixin 落地方式（方案 C，D9）

Python 12 个 mixin 通过多继承组合成 `AutoPermissionInterruptRail`。Go 无多继承，采用**单结构体 + 方法按 Python 文件分文件 + `// Python: class XxxMixin` 注释标注归属**。

**共享状态 15 字段**（全部在 `Lifecycle.__init__` 建立）：

```go
// Python: AutoPermissionLifecycleMixin.__init__
type AutoPermissionRail struct {
    agentinterfaces.BaseRail            // 嵌入 AgentRail 基类
    baseRail              BasePermissionRail
    permissionConfig      map[string]any
    workspaceRoot         string
    platformTrustedRoot   string
    sandbox               *SandboxDescriptor
    sysOperation          sys_operation.SysOperation
    policyEvaluator       *OpenJiuwenPolicyEvaluator
    autoOptions           map[string]any
    autoReviewer          *AutoReviewer
    trustedSearchUrls     *SessionTrustedSearchUrls
    persistentAuditWriter any              // ⤵️ 9.90e 真实类型
    browserRuntimeSecurityProfile any
    priority              int
    ownerAgent            any
    writtenPermissions    func() map[string]any
}
```

**3 个对外钩子**：`BeforeToolCall`、`AfterToolCall`、`Init`/`Uninit`。

**12 个 mixin → 方法分文件**：

| mixin | Python 文件 | Go 文件 | 标注 |
|---|---|---|---|
| `Lifecycle` | `lifecycle.py` | `lifecycle.go` | `// Python: class AutoPermissionLifecycleMixin` |
| `BeforeTool` | `before_tool.py` | `before_tool.go` | `// Python: class AutoPermissionBeforeToolMixin` |
| `ArtifactCapture` | `artifact_tracking_capture.py` | `artifact_tracking_capture.go` | 同上 |
| `ReviewerOverrideConsume` | `reviewer_override_consume.py` | `reviewer_override_consume.go` | 同上 |
| `ReviewerOverrideGate` | `reviewer_override_gate.py` | `reviewer_override_gate.go` | 同上 |
| `ReviewerExecution` | `reviewer_execution.py` | `reviewer_execution.go` | 同上 |
| `Deterministic` | `deterministic.py` | `deterministic.go` | 同上 |
| `ArtifactAuthorization` | `artifact_authorization.py` | `artifact_authorization.go` | 同上 |
| `DenyResponse` | `deny_response.py` | `deny_response.go` | 同上 |
| `ManualAuthorization` | `manual_authorization.py` | `manual_authorization.go` | 同上 |
| `DomainGrantAudit` | `domain_grant_audit.py` | `domain_grant_audit.go` | 同上 |
| `RuntimeBridge` | `runtime_bridge.py` | `runtime_bridge.go` | 同上 |

MRO 顺序（`auto_permission_rail.py:52`）在 Go 中无运行时意义，但**注释中保留**供核对。

---

## 四、接口与装配改造

### 4.1 依赖方向核实

| 包 | 现 import 的 internal 包 | 是否 import `permissions` |
|---|---|---|
| `agentcore/harness/rails/security` | `harness/rails`、`harness/rails/interrupt`、`harness/security` + fileguard/netguard、`common/logger` … | ❌ 否 |
| `swarm/.../rails/permissions` | `agentcore/harness/security`（**不含** `rails/security`）、`swarm/.../rails`、`common/tools` … | — |
| `permissions/auto_permission`（子包） | 仅 `agentcore/*` 基础包 | ❌ 否 |

**新增 `permissions` → `agentcore/harness/rails/security` 单向依赖，不成环。**

### 4.2 `base_rail` 的类型选择

Python 用鸭子类型 `getattr(self.base_rail, "before_tool_call", None)` + `callable()`。Go 有两个选择，本批采用**接口化**：

```go
// permissions/base_rail.go（新增，或用现成接口）
// BasePermissionRail 自动审查层包装的权限轨协作面。
// 依赖倒置：DeepAgent（使用方）依赖别人的能力 → 允许窄接口（规则 10.2）。
type BasePermissionRail interface {
    BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error
    AfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error
    // JiuwenSwarm 子类独有成员（9.90d-1 落地）
    PermissionCheckContext(sessionID string) (func(), bool)
    GetPermissionSnapshot(sessionID string) map[string]any
    InstalledPermissionConfig() map[string]any
    ReplaceInstalledGlobalPermissions(cfg map[string]any)
    SetTrustedDirs(dirs []string)
    UpdateConfig(cfg map[string]any, toolNames []string)
    BindApprovalRequest(request any, toolCall *llmschema.ToolCall)
}
```

**规则 10.2 判定**：这个接口是「DeepAgent 用来调用别人的」→ 允许窄接口，不进 `DeepAgentInterface`。

### 4.3 `AutoPermissionRail` 的落点（D-方案1，V-1）

`permissions` 包与 `permissions/auto_permission` 子包的循环依赖：

```
permissions/auto_permission  → permissions（要 ToolDecisionFacts 等类型）      ← 子包回引父包，Go 允许
permissions → permissions/auto_permission（要 AutoPermissionRail）             ← 父包引子包，Go 允许
两者同时存在 → 循环，编译失败
```

**解法（已拍板）**：`AutoPermissionRail` 类型及全部 21 个方法**放子包**，`permissions` 包不引用它，改由装配层（`swarm/server/adapter`）直接引用子包。

`auto_permission_rail.py` → `permissions/auto_permission/auto_permission_rail.go`（**目录归属漂移，登记为 V-1**）。

### 4.4 `MarkTrusted()` 机制（D5 / C3）

Python 的 `_closure` / `_methods_match` 做**运行时反射级可执行身份校验**：

```python
def _closure(func, factory, name):
    code = factory.__code__
    for part in name.split("."):
        code = next(c for c in code.co_consts if isinstance(c, CodeType) and c.co_name == part)
    exact_function = type(func) is FunctionType
    if not exact_function or func.__code__ is not code or func.__globals__ is not factory.__globals__:
        raise ValueError("builtin callable mismatch")
    return dict(zip(code.co_freevars, (c.cell_contents for c in func.__closure__ or ()), strict=True))
```

原注释明确：`Proxies can spoof __class__; isinstance alone does not prove executable identity.` —— 这是**安全关键**层。

Go 无 `__code__.co_freevars` / `__closure__` 反射，采用**分层替代**：

| 层次 | Python | Go 等价 | 保证强度 |
|---|---|---|---|
| 1. 类型精确匹配 | `type(resource) is expected_type` | `reflect.TypeOf(resource) != expectedType` → nil | ✅ 完全等价（9.90a 已实现） |
| 2. 卡片指针同一 | `resource.card is card` | `resource.Card() != cardPtr` → nil | ✅ 完全等价（9.90a 已实现） |
| 3. owner 同一性 | `matches_bound_method(...)` 的 `__self__ is expected_owner` | rail 实例指针同一 | ✅ 等价 |
| 4. 方法未被替换 | `_methods_match` 的 `__func__ is getattr(expected, name)` | **构造期 `MarkTrusted()` 标记** | ⚠️ 强度降级 |
| 5. 闭包链未篡改 | `_closure` 剥装饰器验 `co_freevars` | **不实现** | ❌ 无等价 |

**`MarkTrusted()` 设计**：

```go
// agentcore/harness/rails/base.go
// MarkTrusted 标记该 rail 实例由受信任构造函数创建。
// 权限审查层据此判定该 rail 的只读工具可免于 LLM 审查。
//
// 替代 Python readonly_tool_bindings._closure / _methods_match 的反射校验
// （见 9.90d spec 已知偏差 V-4/V-5）。
func (r *DeepAgentRail) MarkTrusted() {
    atomic.StoreUint32(&r.trusted, 1)
}

// IsTrusted 返回该 rail 是否已由受信任构造函数标记。
func (r *DeepAgentRail) IsTrusted() bool {
    return atomic.LoadUint32(&r.trusted) == 1
}
```

**各受信任 rail 构造时调用**：`NewProgressiveToolRail(...)` 内部末尾 `r.MarkTrusted()`。

**残余风险（登记）**：Go 无法检测「构造函数被绕过、直接 `&ProgressiveToolRail{}` 后手工调 `MarkTrusted()`」的攻击。缓解：`MarkTrusted` 只在包内可调（不导出到外部包）时应改为包内可见 —— 但 `ProgressiveToolRail` 在 `agentcore/harness/rails` 包，`checkbind` 在 `permissions/auto_permission` 包，跨包无法限制调用方。**结论**：残余风险接受，登记为 V-4。

---

## 五、已知偏差（规则 9.6 / 12.3 / 13.2，全部经用户确认）

| 编号 | 偏差 | Python 位置 | Go 位置 | 性质 | 理由 | 回收条件 | 计划表 ⤴️ |
|---|---|---|---|---|---|---|---|
| **V-1** | `auto_permission_rail.go` 目录归属漂移 | `permissions/auto_permission_rail.py` | `permissions/auto_permission/auto_permission_rail.go` | 目录层级 | 避免 `permissions` ↔ `auto_permission` 循环依赖 | Go 支持子包回引父包类型时回收 | 9.90d-2 行内说明 |
| **V-2** | 12 个 mixin 合并为单结构体 | 12 个 `*Mixin` 类 | `AutoPermissionRail` 单类型 + 方法分文件 | 类型归属 | Go 无多继承；12 mixin 共享同一 `self` 状态 | 不回收（语言限制） | 9.90d-2 行内说明 |
| **V-3** | 5 个反射适配函数不实现 | `runtime_result.py:20/46/79/96/132` | 无 | 方法缺失 | Go 签名静态固定，无 `inspect.signature` 需求 | 不回收（语言限制） | 9.90d-2 行内说明 |
| **V-4** | `_closure` 闭包链校验 → `MarkTrusted()` | `readonly_tool_bindings.py:44` | `rails/base.go` `MarkTrusted()` | 校验机制 | Go 无 `__code__.co_freevars` / `__closure__` 反射 | 不回收（语言限制），残余风险登记 | 9.90d-2 行内说明 |
| **V-5** | `_methods_match` 反射方法归属 → 接口断言 | `readonly_tool_bindings.py:65` | 同上 | 校验机制 | Go 方法值无法还原 `__self__`/`__func__` | 同上 | 9.90d-2 行内说明 |
| **V-6** | `readonly_tool_bindings` 只覆盖 `ProgressiveToolRail` | 覆盖 5 类 rail（`ProgressiveToolRail` / `ToolRetrievalPromptRail`+`ToolRetrievalToolkit` / `SkillRetrievalPromptRail`+`SkillRetrievalToolkit` / `HeartbeatRuntimeBridge` / `_RestrictedCronBackend`） | 仅 `ProgressiveToolRail` | 功能范围 | 4 类 rail/toolkit 在 Go 侧不存在 | **9.11b 落地后回填** | **🔴 新增 `9.11b` 行，标 ☐ + ⤴️ 指向 9.90d-2** |
| **V-7** | `code_tool_bindings` 不含 LSP | 含 `LspRail` + `lsp_tool/*`（约 1,239 行） | 仅 `agent_mode` 三工具 | 功能范围 | Go 侧 LSP 整层未实现 | **9.17 LSPRail 落地后回填** | **✅ 9.17 行追加 ⤴️** |
| **V-8** | `_emit_audit` / `_emit_permission_result_observation` 空实现 | `runtime_bridge.py:62/69` | no-op 桩（保留完整签名与 31 个调用点） | 功能缺失 | `audit.py` 留 9.90e | **9.90e 回填** | **✅ 9.90e 行追加 ⤴️** |
| **V-9** | `NATIVE_PATH_ACCESS` 由 task-local 降级为全局单例 | `ContextVar`（按 asyncio task 隔离） | `native_path_context.go:130` 全局变量 + `sync.Mutex` | 并发语义 | Go 无 task-local 存储；9.90b 落地时即采用全局变量式 | 改用 ctx 派生式重写 `native_path_context.go` 时回收 | 9.90d-2 行内说明（本批是首个并发消费者） |

### 5.1 V-6 的前置件规模（新增 9.11b）

| 前置件 | 行数 | Go 现状 |
|---|---|---|
| `common/rails/tool_retrieval_prompt_rail.py` | 1,086 | ❌ 0 |
| `common/rails/skill_retrieval_prompt_rail.py` | 582 | ❌ 0 |
| `common/tools/tool_retrieval_toolkits.py` | 1,030 | ❌ 0 |
| `common/tools/skill_retrieval_toolkits.py` | 651 | ❌ 0 |
| `code/rails/heartbeat/tools.py` 的 `HeartbeatRuntimeBridge` | — | ❌ 0（Go 的 `HeartbeatRail` 在 `agentcore/harness/rails/heartbeat.go`，非同一物） |
| `common/tools/cron/cron_runtime.py` 的 `_CronToolsCronBackend` / `_RestrictedCronBackend` | 937+ 行 | ⚠️ Go `cron_runtime.go` 有 `cronToolsBackend` / `CronRuntimeBridge`，需核对等价性 |
| **V-6 前置件合计** | **≥4,286** | 全部 0 命中 |

### 5.2 V-7 的前置件规模（9.17）

| 文件 | 行数 |
|---|---|
| `openjiuwen/harness/rails/lsp_rail.py` | 414 |
| `openjiuwen/harness/tools/lsp_tool/_tool.py` | 462 |
| `openjiuwen/harness/tools/lsp_tool/_formatter.py` | 235 |
| `openjiuwen/harness/tools/lsp_tool/_schemas.py` | 128 |
| 合计 | **1,239** |

---

## 六、删除清单（规则 8.3）

| # | 对象 | 位置 | 验收锚点 |
|---|---|---|---|
| D-1 | `ComposeHostEffectivePermissions` 最小实现体（约 90 行） | `permissions_layers.go:1419` | 函数体已替换为 `ComposeHostEffectivePermissions(...)` 调用 |
| D-2 | 该函数上的 `⤴️ 9.90d 回填` 注释 | `permissions_layers.go:1406-1418` | `grep "⤴️ 9.90d"` = 0 |
| D-3 | `doc.go` 第 1、2 条已知缺口 | `permissions/doc.go:26-41` | `grep "9.90b 遗留"` = 0 |
| D-4 | `root_context.go` 三处 `⤵️ 消费方待 9.90d` | `root_context.go:753/770/789` | `grep "⤵️ 消费方待 9.90d"` = 0 |
| D-5 | `policy_eval.go:234` 的 `⤵️ 9.90e 回填` | `policy_eval.go` | `grep "⤵️ 9.90e"` in `policy_eval.go` = 0 |
| D-6 | `auto_permission/doc.go` 4 常量未启用说明 | `auto_permission/doc.go` | `grep "全仓零消费"` = 0 |
| D-7 | 该文件 11 常量"消费方在 9.90d" | 同上 | 同上 |

**验证**：`grep -rn "⤵️ 9.90d\|⤴️ 9.90d" internal/` 应为 0 命中（新增文件的注释除外）。

---

## 七、测试策略

### 7.1 每条决策点的测试覆盖映射

| 决策 | 单元测试 | 集成测试 |
|---|---|---|
| **D1** A 类 5 文件 | `url_safety_test.go`（23 函数）、`persistent_audit_test.go`、`trusted_search_urls_test.go`、`send_file_path_guard_test.go`、`generated_artifact_delivery_test.go` | `tests/integration/swarm/permissions/url_safety_flow_test.go`：`auto_decision` → `url_safety` → `reviewer_route` 真实链路；`persistent_audit` 真实落盘读回 |
| **D2** B 类 6 文件 | 各包原生 `_test.go` | `tests/integration/swarm/agents/harness/common/workspace_paths_test.go`：`reviewer_metadata` → `workspace_paths` → `reviewer_redaction` 三段脱敏链 |
| **D3** C1 readonly 只覆盖 ProgressiveToolRail | `readonly_tool_bindings_test.go`：① `tool_search` + ProgressiveToolRail 真实绑定 → 通过；② 伪造 rail → fail-closed；③ 4 类缺失 rail → 各自 fail-closed | `tests/integration/swarm/permissions/readonly_binding_test.go`：真实 `ProgressiveToolRail` 注册 + `ToolSearchTool` 调用 → `before_tool` 判定 allow，跨 `rails` + `permissions` + `tools` 三子系统 |
| **D4** C2 code_tool_bindings 只落 agent_mode | `code_tool_bindings_test.go`：三工具各自 `_control_invoke_matches`；LSP 路径断言返回 false | `tests/integration/swarm/permissions/code_binding_test.go`：`AgentModeRail` + `EnterPlanModeTool` 真实注册 → `before_tool` allow |
| **D5** C3 `MarkTrusted()` | `base_rail_marktrusted_test.go`：① 未 MarkTrusted → fail-closed；② 已 Mark → 通过；③ 同类型不同实例 → 失败；④ 类型不符 → 失败 | `tests/integration/swarm/permissions/marktrusted_test.go`：真实 `ProgressiveToolRail` 构造 → `MarkTrusted` → `trusted_readonly_binding` 通过；反例：`&ProgressiveToolRail{}` → 拒绝 |
| **D6** 不拆（已改为拆两份） | — | — |
| **D7** `audit.py` no-op 桩 | `runtime_bridge_stub_test.go`：① `_emit_audit` 调用不 panic；② 31 个调用点表驱动可达；③ 桩返回 nil | `tests/integration/swarm/permissions/audit_stub_test.go`：完整 `before_tool` 跑通 5 条路径（allow/deny/manual/semantic/interrupt），确认无 panic |
| **D8** `tool_display` 落点 | `internal/common/tool_display/tool_display_test.go`：`inject_call_goal_schema` / `extract_call_goal` 双向 | 被 `invocation_context` 集成用例覆盖 |
| **D9** mixin 方案 C | `auto_permission_rail_test.go`：① 15 字段构造后全部非空；② 12 mixin 方法表驱动可调；③ 方法块注释归属可 grep 核对 | `tests/integration/swarm/permissions/auto_rail_compose_test.go`：完整 rail 装配（base_rail + auto + 真实 engine）+ 一次工具调用 |
| **D10** `permission_interrupt_rail.py` 417 行 | `permission_interrupt_rail_test.go`：5 个独有成员各独立用例 | `tests/integration/swarm/permissions/swarm_rail_wrap_test.go`：`JiuwenSwarmPermissionInterruptRail` 包装 → `AutoPermissionRail` 再包装 → 调用穿透真实 engine |
| **拆分** 9.90d-1/2 边界 | 9.90d-1 每文件独立单测；9.90d-2 依赖 9.90d-1 的接口 | `tests/integration/swarm/permissions/prefix_contract_test.go`：仅用 9.90d-1 产物构造 `AutoPermissionRail` 的 fake 形态 |

### 7.2 规则 7 集成测试合规性

| 集成目录 | 调用跨度子系统 | 是否达标 |
|---|---|---|
| `tests/integration/swarm/permissions/` | `swarm/agents/harness/common/rails/permissions` + `agentcore/harness/rails` + `agentcore/harness/security` + `agentcore/foundation/tool` | ✅ 4 个 |
| `tests/integration/swarm/agents/harness/common/` | `swarm/agents/harness/common` + `swarm/agents/harness/common/rails/permissions` + `agentcore/harness` | ✅ 3 个 |
| `tests/integration/agentcore/harness/rails/security/`（已存在） | 已有 3 个 | ✅ 追加用例 |

**验收**：`go run ./cmd/integration-audit -verify` 两层门禁全绿。

### 7.3 三个必须单独写的安全回归集成场景

1. **真实 `ProgressiveToolRail` + `ToolSearchTool` 全链** → D3/D5 allow 路径
2. **伪造 rail 绕过尝试** → D3/D5 fail-closed 路径（安全回归）
3. **`JiuwenSwarmPermissionInterruptRail` → `AutoPermissionRail` 双层包装穿透** → D10 + D9 装配正确性

### 7.4 LLM 相关测试隔离

`auto_reviewer` 依赖真实 LLM。按规则 3.2：
- 单元测试用 fake `ReviewerClient`
- 真实调用隔离到 `//go:build llm`，本地不跑

---

## 八、任务分解

### 8.1 9.90d-1（前置件，3,333 行 / 15 项）

| Task | 内容 | 行数 | 依赖 | 测试 |
|---|---|---|---|---|
| 0 | 前置核实（写入 §11） | — | — | — |
| 1 | `internal/common/tool_display/tool_display.go` | 87 | 无 | 单测 |
| 2 | `.../common/tools/command_runtime.go` | 98 | 无 | 单测 |
| 3 | `.../server/runtime/sandbox_no_host_fallback.go` | 38 | 无 | 单测 |
| 4 | `.../runtime/run_permissions.go` | 54 | 无 | 单测 |
| 5 | `.../common/workspace_paths.go` | 518 | 无 | 单测 + 集成 |
| 6 | `.../common/tools/pdf_tools.go` | 275 | 无 | 单测 |
| 7 | `permissions/persistent_audit.go` | 296 | 9.90b `ToolDecisionFacts` | 单测 + 集成 |
| 8 | `permissions/url_safety.go` | 541 | 9.90b `network_scope` + 9.90c `root_context` | 单测 + 集成 |
| 9 | `permissions/generated_artifact_delivery.go` | 175 | 无 | 单测 |
| 10 | `permissions/send_file_path_guard.go` | 315 | 9.90b `fileguard` | 单测 + 集成 |
| 11 | `permissions/trusted_search_urls.go` | 236 | Task 8 + `common/tools/search_tools` | 单测 |
| 12 | `permissions/openjiuwen_contract.go` | 183 | 无 | 单测 |
| 13 | `permissions/permission_interrupt_rail.go` | 417 | Task 7-12 | 单测 + 集成 |
| 14 | `agentcore/harness/rails/base.go` 新增 `MarkTrusted()` / `IsTrusted()` | ~40 | 无 | 单测 + 集成 |
| 15 | `permissions/base_rail.go` 定义 `BasePermissionRail` 接口 | ~60 | Task 13 | 编译验证 |
| 16 | 9.90d-1 测试补齐 | — | 全部 | — |
| 17 | `doc.go` 同步 + 计划表回填（9.90d-1 部分） | — | — | — |

### 8.2 9.90d-2（审查层本体，7,923 行 / 29 文件）

| Task | 内容 | 行数 | 依赖 | 测试 |
|---|---|---|---|---|
| 18 | `auto_config.go` | 315 | 9.90d-1 | 单测 |
| 19 | `permission_compose.go`（替换最小实现 + D-1/D-2 删除） | 362 | Task 18 | 单测 |
| 20 | `reviewer_redaction.go` | 689 | 9.90d-1 `workspace_paths` | 单测 |
| 21 | `reviewer_stream_metadata.go` | 57 | 无 | 单测 |
| 22 | `auto_permission/models.go` 扩展 + `tool_binding.go` 扩展 | ~60 | 无 | 单测 |
| 23 | `auto_permission/lifecycle.go` | 165 | Task 22 | 单测 |
| 24 | `invocation_context.go` | 715 | Task 23 + 9.90d-1 | 单测 |
| 25 | `runtime_result.go` | 250（V-3 减 5 函数） | Task 24 | 单测 |
| 26 | `runtime_bridge.go`（含 D7 no-op 桩） | 316 | Task 25 | 单测 + 集成 |
| 27 | `reviewer_metadata.go` | 401 | Task 20 | 单测 |
| 28 | `reviewer_audit.go` | 77 | Task 27 | 单测 |
| 29 | `auto_decision.go` | 639 | Task 20 + 9.90d-1 `url_safety` | 单测 |
| 30 | `reviewer_route.go` | 330 | Task 29 | 单测 |
| 31 | `auto_reviewer.go` | 888 | Task 20/27/29/30 | 单测 + 集成 |
| 32 | `reviewer_execution.go` | 389 | Task 31 | 单测 + 集成 |
| 33 | `reviewer_override_gate.go` + `reviewer_override_consume.go` | 151 | Task 27/28 | 单测 |
| 34 | `deterministic.go` | 131 | Task 31/32/33 | 单测 |
| 35 | `manual_authorization.go` + `domain_manual.go` + `deny_response.go` | 219 | Task 31/33 | 单测 |
| 36 | `domain_grant_audit.go` | 162 | Task 35 | 单测 |
| 37 | `artifact_authorization.go` + `artifact_tracking_capture.go` + `artifact_declaration.go` | 170 | 9.90d-1 `generated_artifact_delivery` | 单测 |
| 38 | `code_tool_bindings.go`（D4 仅 agent_mode） | 110 | Task 22 | 单测 + 集成 |
| 39 | `readonly_tool_bindings.go`（D3/D5 仅 ProgressiveToolRail） | 320 | Task 38 + 9.90d-1 `MarkTrusted` | 单测 + 集成 |
| 40 | `auto_permission_rail.go`（子包落点，V-1） | 75 | Task 23-39 全部 | 单测 + 集成 |
| 41 | `before_tool.go`（七步编排，891，最后） | 891 | 全部 | 单测 + 集成 |
| 42 | 装配接线（`deep_adapter_rails.go` / `code_adapter.go` / `permission_mode.go`） | ~120 | Task 40/41 | 集成 |
| 43 | 9.90d-2 测试补齐 | — | 全部 | — |
| 44 | `doc.go` 同步 + 计划表回填（9.90d-2 部分 + ⤴️ 标记） | — | — | — |

---

## 九、验证方式

```bash
# 1. 单包测试（9.90d-1）
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/common/tool_display/...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/runtime/...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/runtime/...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/...

# 2. 单包测试（9.90d-2）
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/...

# 3. 覆盖率（规则 3 要求 ≥85%）
go test -coverprofile=coverage.out ./internal/swarm/agents/harness/common/rails/permissions/...
go tool cover -func=coverage.out

# 4. 集成测试
CGO_ENABLED=1 go test -tags "integration sqlite_fts5 test" ./tests/integration/swarm/permissions/...

# 5. 集成合规审计（规则 7 两层门禁）
go run ./cmd/integration-audit -verify

# 6. 全量回归
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...

# 7. 编译与静态检查
go build ./... && go vet ./...
```

**LLM 相关**：`go test -tags "llm"` 需真实 API Key，本地不跑。

---

## 十、⤵️ 预留回填点

| 位置 | 内容 | 回填章节 |
|---|---|---|
| `auto_permission/runtime_bridge.go` `_emit_audit` / `_emit_permission_result_observation` | no-op 桩 → 真实 `emit_permission_audit` 调用 | **9.90e** |
| `auto_permission/readonly_tool_bindings.go` | 4 类 rail/toolkit 的信任背书 | **9.11b**（新增章节） |
| `auto_permission/code_tool_bindings.go` | LSP 绑定校验 | **9.17** |
| `auto_permission/persistentAuditWriter` 字段类型 | `any` → 具体 `*PersistentAuditWriter` | **9.90e** |
| `permissions/permission_interaction.go` | 整个文件未落 | **9.90e** |
| `permissions/audit.go` | 整个文件未落 | **9.90e** |

---

## 十一、风险与开放问题

| # | 风险 | 缓解 |
|---|---|---|
| R1 | `before_tool.go`（891 行单方法）无法分段单测 | 通过外部行为覆盖（fake base_rail + fake reviewer + 断言 `PermissionHandlingResult`） |
| R2 | `MarkTrusted()` 无法阻止"绕过构造函数后手工标记" | 接受为残余风险，登记 V-4 |
| R3 | 9.90d-2 的 7,923 行仍偏大 | 已拆两批；9.90d-2 内部按 Task 18-44 共 27 个可独立提交步骤推进，每步完成即提交 |
| R4 | `auto_reviewer` 依赖真实 LLM，单测覆盖有限 | fake client 覆盖协议层；真实调用隔离 `-tags llm` |
| R5 | 集成测试第二层门禁（≥3 子系统各 ≥30 行）可能不达标 | 已在 §7.2 规划三子系统链路；Task 16/43 验收时实测 |

---

## 十二、Task 0 前置核实（待执行）

以下项需在实施前核实并回填本节：

1. `agentcore/harness/rails/security` 是否 import `permissions`（已核实：否）
2. `PermissionInterruptRail` 的公开方法清单（确认 5 个缺失成员：`PermissionCheckContext` / `GetPermissionSnapshot` / `InstalledPermissionConfig` / `ReplaceInstalledGlobalPermissions` / `BindApprovalRequest`）
3. `agentinterfaces.BaseRail` 是否有扩展点（`MarkTrusted()` 落点选择 `DeepAgentRail` 还是 `BaseRail`）
4. `internal/common` 新建顶层子包的可行性（`tool_display`）
5. `IsolatedModelReviewerClient` 的 LLM 注入路径（Go 侧对应 `llm.Model` 构造方式）
6. `cron_runtime.go` 的 `cronToolsBackend` 与 Python `_CronToolsCronBackend` 的等价性（V-6 回收依赖）
7. `permission_mode.py`（47 行，`is_auto_permission_enabled`）Go 侧 0 命中，确认归属 9.90d-2 装配层
