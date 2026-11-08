# 9.90b 设计：权限决策事实层 + PermissionEngine A/B/C 三段对齐

> 对应 Python：
> - `jiuwenswarm/agents/harness/common/rails/permissions/`（8 个文件，2,173 行）
> - `jiuwenswarm/common/permission_tools.py`（105 行）
> - `openjiuwen/harness/security/permission_engine/`（4,176 行，engine 侧）
> - `openjiuwen/harness/resources/builtin_rules.yaml`（246 行）
>
> 目标 Go 包：
> - `internal/common/permission_tools/`（新建）
> - `internal/agentcore/harness/security/`（重构 + 新建 fileguard/netguard 子树）
> - `internal/agentcore/harness/resources/`（扩结构体 + 补 YAML）
> - `internal/swarm/agents/harness/common/rails/permissions/`（9.90b 八文件）
>
> 制定日期：2026-11-08
> 遵循规则：2（计划同步）、8（删除残留）、9（结构忠实）、10（接口归属）、11（CI 不主动关注）、12（不改层次/步骤/命名）、13（主动盘点缺失项）

---

## 一、定位与作用

### 1.1 在 agent 会话中的流程位置

```
用户提问
   │
   ▼
DeepAgent.runSuperstep → LLM 返回一批 tool_calls（可并行 5 个）
   │
   ├─ 逐个 BeforeToolCall 回调
   │    │
   │    ├─【9.90a ✅】数据契约（ToolInvocation / PermissionHandlingResult
   │    │              / PermissionInterruptRequest）+ 实现身份背书 ResolveToolBinding
   │    │
   │    ├─【PermissionEngine.check_permission】规则判定 —— 本批重构为 A/B/C 三段
   │    │      │
   │    │      ├─ Pipeline A：evaluate_tiered_policy（工具白黑名单 + 参数规则
   │    │      │              + approval_overrides + baseline + defaults）
   │    │      ├─ Pipeline B：FileGuardChecker.evaluate（路径防护 + 敏感路径）
   │    │      ├─ Pipeline C：NetGuardChecker.evaluate（fetch 类 URL 白黑名单）
   │    │      └─ strictest(A, B, C)
   │    │            ├─ ALLOW → 放行
   │    │            ├─ DENY  → 拒绝
   │    │            └─ ASK   → 需要用户确认  ◀── 权限体系在此介入
   │    │
   │    ├─【9.90b ★ 本批】决策事实层 —— 把 args 翻译成「事实」
   │    │      ├─ tool_capabilities        工具名 → 能力分类（category /
   │    │      │                          operation_family / risk_tier / 脱敏键）
   │    │      ├─ tool_decision_facts      一次调用的权威事实包（读写路径 /
   │    │      │                          command / workspace_root / accesses_known）
   │    │      ├─ network_scope            host 归一化 / IDNA / 内网 IP 拒绝 /
   │    │      │                          query 密钥检测
   │    │      ├─ policy_eval              不弹框的影子评估（scene hook → engine
   │    │      │                          → send_file 路径守卫，取最严）
   │    │      ├─ permissions_layers       Global/User/Session 三层覆盖读写 + 锁
   │    │      ├─ execution_provider_contract  哪个工具必须在 JiuwenBox 内跑
   │    │      ├─ sandbox_profile          沙箱根目录投影（2 字段）
   │    │      └─ native_path_context      Smart 场景原生路径访问投影
   │    │
   │    ├─【9.90c ✅】root 五文件（串行队列 + 意图窗口 + 中断扩字段）
   │    ├─【9.90d ☐】自动审查层（auto_reviewer / deterministic / before_tool）
   │    └─【9.90e ☐】护栏与审计（url_safety / persistent_audit / send_file_path_guard）
```

### 1.2 一句话作用

**本批不产生任何审批决策，也不调用 LLM。** 它做两件事：

1. **把引擎的判定管线补全并修正**——Go 侧 `PermissionEngine` 目前只有 A 段（工具策略），缺 B 段（路径防护）与 C 段（网络防护），导致参数级的路径越权与黑名单 URL 访问拦不住。本批按 Python `core.py` 的 `check_permission` 一比一补齐三段取最严。
2. **为 9.90d 的自动审查层准备客观事实**——9.90d 要让 LLM 判断「这次调用能否自动放行」，它需要的是结构化事实（什么工具、什么类别、碰了哪些路径、访问了哪个 host、风险等级），而不是裸的工具名和参数字典。

### 1.3 为什么 B 段（路径防护）是本批的核心

Python `PermissionEngine.check_permission` 的判定管线：

```python
result_A = evaluate_tiered_policy(...)      # 工具权限（始终）
result_B = FileGuardChecker.evaluate(...)   # 路径防护（file_guard.enabled）
result_C = NetGuardChecker.evaluate(...)    # 网络防护（net_guard.enabled）
return strictest(result_A, result_B, result_C)
```

Go 侧的 `ExternalDirectoryChecker`（`checker.go`，303 行）是当初自研的替代品，它只做「workspace 外的路径 → ASK/DENY」这一件事，与 Python 的 `FileGuardChecker`（870 行）不是同一棵树上的东西：

| 能力 | Python `FileGuardChecker` | Go `ExternalDirectoryChecker` |
|---|---|---|
| 双模式 | `native` / `legacy` | 无此概念 |
| 三轴判定 | read / write / exec 分别配置 | 无轴，统一一个 level |
| 规则形态 | `prefix` + `glob` 两种匹配 | 只有前缀 |
| 敏感路径 | `builtin_rules.yaml` 的 `sensitive_paths` 注入 | 无 |
| `collect_ask_accesses` | 有（供「总是允许」按轴落盘） | 无 |
| `trusted_dirs` | 参与编译成 allow 前缀 | 无 |

**这就是本批要连引擎一起对齐的原因**：`policy_eval` 要复算「规则本来会给什么结论」作为 9.90d 的比对基准，如果引擎本身只有 A 段，这个基准就是错的。

---

## 二、Go 侧现状盘点（规则 13.4）

### 2.1 上游文件清单与 Go 现状对照

#### 2.1.1 engine 侧：`openjiuwen/harness/security/permission_engine/`

| # | Python 文件 | 行数 | Go 现状 | 落点 | 判定 |
|---|---|---|---|---|---|
| 1 | `models.py` | 194 | 269 | `security/models.go` | ✅ 已对齐 |
| 2 | `host.py` | 123 | — | `security/models.go` 内 `ToolPermissionHost` | ⚠️ 需逐字段核 |
| 3 | `core.py` | 445 | 375 | `security/permission_engine.go` | 🔴 **重写**（补 B/C 段 + `prepare_permissions_for_engine` + `update_trusted_dirs`） |
| 4 | `toolguard/tool_policy.py` | 910 | 960 | `security/tiered_policy.go` | ✅ 已对齐（A 段） |
| 5 | `toolguard/shell_ast.py` | 386 | 516 | `security/shell_ast.go` | ✅ 已对齐 |
| 6 | `toolguard/pattern_matchers.py` | 163 | — | `security/patterns.go` 的 matcher 部分 | ✅ 已对齐 |
| 7 | `toolguard/builtin_rules.py` | 151 | **0** | — | 🔴 **新建**（`inline_package_command_rules` 等） |
| 8 | `toolguard/severity_mapping.py` | 87 | — | `SeverityToDecision` | ✅ 已对齐 |
| 9 | `toolguard/command_canonicalize.py` | 134 | **0** | — | 🔴 **新建**（M6，见 §12.1） |
| 10 | `toolguard/tool_categories.py` | 51 | — | `toolCategory` | ✅ 已对齐 |
| 11 | `fileguard/file_guard.py` | 870 | **0** | — | 🔴 **新建（B 段）** |
| 12 | `fileguard/path_extract.py` | 524 | **0** | — | 🔴 **新建** |
| 13 | `fileguard/sensitive_paths.py` | 193 | **0** | — | 🔴 **新建** |
| 14 | `fileguard/file_tool_specs.py` | 63 | **0** | — | 🔴 **新建** |
| 15 | `netguard/net_guard.py` | 163 | **0** | — | 🔴 **新建（C 段）** |
| 16 | `netguard/net_urls.py` | 152 | **0** | — | 🔴 **新建** |
| 17 | `approve/*.py`（3 文件） | — | — | — | 属 9.90e，不在本批 |
| — | — | — | 303 | `security/checker.go` | 🔴 **删除**（规则 8） |

#### 2.1.2 资源侧：`openjiuwen/harness/resources/builtin_rules.yaml`

| 段 | Python | Go | 判定 |
|---|---|---|---|
| `rules` 条数 | 17（L11-143） | 10（L16-84） | 🔴 **缺 7 条** |
| `rules[].action` | 17 条全有 | 仅 1 条有 | 🔴 **缺 16 个 action** |
| `sensitive_paths` | 17 条（L148-241，94 行） | 无 | 🔴 **整段缺失** |
| `net_urls` | 4 条（L242-246） | 无 | 🔴 **整段缺失** |

缺失的 7 条 `rules`：

- `shell_ps_recursive_or_forced_delete`（PowerShell 递归/强制删除）
- `shell_registry_delete`（注册表删除）
- `shell_chmod_world_writable`（chmod 777）
- `shell_ld_preload_hijack`（LD_PRELOAD 劫持）
- `shell_clear_audit_history`（清除审计历史）
- `shell_disable_firewall`（关闭防火墙）
- `shell_docker_privileged`（docker --privileged）

#### 2.1.3 9.90b 本体的 8 个文件

| # | Python 文件 | 行数 | Go 现状 |
|---|---|---|---|
| 1 | `tool_capabilities.py` | 615 | 🔴 缺失 |
| 2 | `tool_decision_facts.py` | 182 | 🔴 缺失 |
| 3 | `network_scope.py` | 175 | 🔴 缺失 |
| 4 | `policy_eval.py` | 375 | 🔴 缺失 |
| 5 | `permissions_layers.py` | 676 | 🔴 缺失 |
| 6 | `execution_provider_contract.py` | 37 | 🔴 缺失 |
| 7 | `sandbox_profile.py` | 48 | 🔴 缺失 |
| 8 | `native_path_context.py` | 65 | 🔴 缺失 |

全部缺失，需新建。合计 2,173 行。

### 2.2 旧逻辑清单（将被替代）

| 旧实现 | 位置 | 行数 | 替代者 | 处置 |
|---|---|---|---|---|
| `ExternalDirectoryChecker` | `security/checker.go` | 303 | `FileGuardChecker` | **删除**（含 `checker_test.go` 7 用例） |
| `PermissionEngine.SetWorkspaceRoot` | `security/permission_engine.go:119` | 6 | `UpdateTrustedDirs` | **删除**（零生产调用方，仅测试引用） |
| `PermissionEngine.externalChecker` 字段 | 同上 :34 | 1 | `fileGuard *FileGuardChecker` + `netGuard *NetGuardChecker` | 改类型 |
| `evaluateGlobalPolicyWithSnapshot` 的 `includeExternalDirectory` 分支 | 同上 :345 | 15 | B 段独立 | 删除分支，B 段由 `CheckPermission` 调用 |
| `tool_security_rail.collectExternalDirectoryPersistPaths` | `rails/security/tool_security_rail.go:693` | 32 | `collectFileGuardPersistAccesses`（对齐 `_collect_file_guard_persist_accesses`） | **重写**（返回类型 `[]string` → `[]persistAccess`） |
| `MergeExternalDirectoryAllowIntoPermissions` 的调用 | `tool_security_rail.go:740` | 5 | `MergeFileGuardAccessAllows`（对齐 `merge_file_guard_access_allows`） | **改调用** |
| `tool_security_rail.toolNameAliases` 缺 `web_fetch_webpage` | 同上 :73 | 6 | 补一项 | **补齐** |
| `EvaluateTieredPolicy` 自行调 `getBuiltinSecurityRules()` | `tiered_policy.go:150` | 1 | 从配置按 `layer=="builtin"` 切分 | **改**（Python 判定路径不 load YAML，见 §12.5.3-4） |
| 无 `canon_cmd` / `canon_args` 脱壳 | `tiered_policy.go:147` | — | `CanonicalizeShellCommandForPermission` | **新增**（§12.5.1） |
| 无 `override_hits` 两轮（原始→脱壳） | `tiered_policy.go` | — | 对齐 `tool_policy.py:827-832` | **新增** |
| 无 `_evaluate_param_rules_only` 整串复核 | `tiered_policy.go` | — | 对齐 `tool_policy.py:855-869` | **新增** |
| 无 `_has_interpreter_sink` / `check_sink` | `tiered_policy.go` | — | 对齐 `tool_policy.py:871-878` | **新增** |
| 无 `_allow_overrides_unless_denied`（外层 override 兜底） | `tiered_policy.go` 的 `EvaluateTieredPolicy` | — | 对齐 `tool_policy.py:610-619`，在子命令聚合**之后**再套一次 | **新增** |

**关于最后一项的精确差异**（核实后修正）：`evaluateSingleInvocation` 第 3 步「override 命中直接返回 ALLOW」两边**一致**，不是缺陷。真正的差异在**外层**：

- Python `evaluate_tiered_policy` 在子命令聚合、整串复核、interpreter_sink 之后，**再调一次** `_allow_overrides_unless_denied(permission, matched_rule, override_hits)`
- 作用：子命令聚合出的 ASK 可被 override 放宽为 ALLOW，但聚合出的 DENY 不可放宽

Go 现状没有这层外层兜底，因此「整条命令命中 override，但某个子命令判定为 ASK」的场景，Go 会停在中途的 ALLOW（因为单次调用里 override 已短路），而 Python 会走到聚合再兜底——**结果可能相同，但 `matched_rule` 不同，且 DENY 场景下的行为差异需实测确认**。Task 3a 需为此写对拍用例。

### 2.3 缺失项清单（规则 13.2，必须讨论后决策）

| ID | 缺失项 | 影响面 | 本批决策 |
|---|---|---|---|
| M1 | `fileguard/` 整棵子树（4 文件 1,650 行） | 引擎 B 段完全缺失，路径越权拦不住 | **本批补齐**（用户拍板方案 A + A+B+C） |
| M2 | `netguard/` 整棵子树（2 文件 315 行） | 引擎 C 段缺失，黑名单 URL 不拦 | **本批补齐** |
| M3 | `toolguard/builtin_rules.py`（151 行） | 内置规则不走 ingest 注入，依赖 YAML 写死 action | **本批补齐** |
| M4 | `builtin_rules.yaml` 缺 7 条 rules + 16 个 action + 2 段 | A 段少 7 条安全底线；B/C 段无内置规则可用 | **本批补齐**（用户拍板拷贝 Python 两段并扩展结构体） |
| M5 | `resources.BuiltinRules` 缺 `SensitivePaths` / `NetUrls` 字段 | 无法解析 YAML 新段 | **本批扩展** |
| M6 | `toolguard/command_canonicalize.py`（134 行） | 未知，需核实 Go 侧是否有对应 | **本批核实**，若缺失则单独登记到 §十二 |
| M7 | `common/permission_tools.py`（105 行） | `tool_capabilities.classify_tool` 的硬依赖 | **本批新建**于 `internal/common/permission_tools/` |
| M8 | `PermissionEngine.UpdateTrustedDirs` / `TrustedDirs()` | B 段 `trusted_dirs` 入参恒空 | **本批补齐** |
| M9 | `PermissionInterruptRail.SetTrustedDirs` | 请求级 trusted_dirs 无注入点 | **本批补齐** |
| M10 | `merge_file_guard_access_allows`（Python `security/patterns.py`） | 「总是允许」写入位置与 B 段读取位置不一致 | **本批补齐**（用户拍板一并改掉） |
| M11 | `host.py` 的 `set_trusted_dirs` 链路 | 同 M9 | 并入 M9 |

### 2.4 本批总量

| 类别 | 文件 | 行数 |
|---|---|---|
| 新建：`permission_tools` | 1 + 1 测试 | 105 + ~150 |
| 新建：`fileguard/` | 4 + 4 测试 | 1,650 + ~1,400 |
| 新建：`netguard/` | 2 + 2 测试 | 315 + ~280 |
| 新建：`toolguard/builtin_rules.go` | 1 + 1 测试 | 151 + ~180 |
| 新建：9.90b 八文件 | 8 + 8 测试 | 2,173 + ~1,900 |
| 扩：`resources/` | 1 + YAML | 30 + 162 |
| 重写：`permission_engine.go` | 1 | 375 → ~520 |
| 改造：`tool_security_rail.go` | 1 | ~+200 |
| 删除：`checker.go` + 测试 | 2 | -303 -338 |
| **合计** | | **约 6,150 行实现 + 约 4,300 行测试** |

---

## 三、结构对照（规则 9.4 / 12.4 方法清单）

### 3.1 `permission_tools.py` → `internal/common/permission_tools/permission_tools.go`

**包名决策**（用户拍板）：目录名 `permission_tools`，`package permission_tools`——目录与包名同名带下划线，与 Python 文件一一对应，grep 可追溯。

| # | Python 符号 | 类型 | Go 对应 | 说明 |
|---|---|---|---|---|
| 1 | `PERMISSION_TOOL_ALIASES` | dict（4 项） | `PermissionToolAliases map[string]string` | 逐项照抄 |
| 2 | `PERMISSION_LEVEL_RANK` | dict（3 项） | `PermissionLevelRank map[string]int` | allow=0 / ask=1 / deny=2 |
| 3 | `PermissionToolNameResolution` | `@dataclass(frozen=True)` | `PermissionToolNameResolution struct` | 4 字段 + `Conflict bool` |
| 4 | `resolve_permission_tool_name(tool_name, *, canonical_names)` | 函数 | `ResolvePermissionToolName(toolName string, canonicalNames []string) PermissionToolNameResolution` | 见 §3.1.1 冲突语义 |
| 5 | `normalize_permission_tool_name(tool_name)` | 函数 | `NormalizePermissionToolName(toolName any) string` | 返回 `canonical_name` |
| 6 | `stricter_permission_level(left, right)` | 函数 | `StricterPermissionLevel(left, right string) string` | 未知输入按 ask 兜底 |

#### 3.1.1 `resolve_permission_tool_name` 的冲突语义（不可简化）

Python 原文：

```python
canonical_name = PERMISSION_TOOL_ALIASES.get(registered_name, registered_name)
alias_targets = frozenset(PERMISSION_TOOL_ALIASES.values())
conflict = (
    (registered_name in canonical_names
     and registered_name in PERMISSION_TOOL_ALIASES
     and canonical_name != registered_name)
    or (registered_name in alias_targets
        and registered_name in PERMISSION_TOOL_ALIASES
        and canonical_name != registered_name)
    or (canonical_name in PERMISSION_TOOL_ALIASES
        and PERMISSION_TOOL_ALIASES[canonical_name] != canonical_name)
)
if conflict:
    canonical_name = ""
    aliases = ()
else:
    aliases = tuple(sorted(
        alias for alias, target in PERMISSION_TOOL_ALIASES.items()
        if target == canonical_name and alias != canonical_name
    ))
```

三个 or 分支必须逐条照抄，不可合并——它们检查的是「同一个名字既是别名又是某个集合的成员」的不同组合。`conflict=True` 时 `canonical_name=""`，调用方 `classify_tool` 据此返回 `unknown`/`high`/`high_flex=True`（**fail-closed**）。

另有两处异常语义（Go 无异常，需等价）：

- `str(tool_name or "")` 抛异常 → 返回 `registered_name=""`、`conflict=True`（Python 注释：`A caller-controlled name must not escape fail-closed lookup.`）
- `aliases` 排序：`tuple(sorted(...))`——Go 需 `sort.Strings`

### 3.2 `fileguard/file_tool_specs.py` → `security/fileguard/file_tool_specs.go`

| # | Python 符号 | Go 对应 | 说明 |
|---|---|---|---|
| 1 | `FileAction = Literal["read","write","exec"]` | `type FileAction string` + 3 常量 | |
| 2 | `FileToolSpec` dataclass | `FileToolSpec struct` | 3 字段 |
| 3 | `_FILE_TOOL_REGISTRY` | `fileToolRegistry map[string][]FileToolSpec` | 全局变量 |
| 4 | `register_file_tool(spec)` | `RegisterFileTool(spec FileToolSpec)` | 同 `arg_name` 替换 |
| 5 | `lookup_file_tool_specs(tool_name)` | `LookupFileToolSpecs(toolName string) []FileToolSpec` | 无则返回 nil |
| 6 | `_bootstrap_builtin_specs()` | `init()` | 17 条内置 spec 逐项照抄 |

**注意**：`_bootstrap_builtin_specs` 用 `if _FILE_TOOL_REGISTRY: return` 守卫，Go 用 `sync.Once` 或直接放 `init()`。Python 的注册表是**可变全局**（9.90e 的 `send_file_path_guard` 会注册原生 schema），Go 需 `sync.RWMutex` 保护。

### 3.3 `fileguard/path_extract.py` → `security/fileguard/path_extract.go`

| # | Python 符号 | 行 | Go 对应 |
|---|---|---|---|
| 1 | `_is_windows_abs_text` | 79 | `isWindowsAbsText` |
| 2 | `_resolve_extract_path` | 83 | `resolveExtractPath` |
| 3 | `_nt_cmd_exe_switch_token` | 90 | `ntCmdExeSwitchToken` |
| 4 | `_looks_like_path` | 109 | `looksLikePath`（**与 `checker.go` 同名函数不同实现**） |
| 5 | `_join_home_rest` | 122 | `joinHomeRest` |
| 6 | `_unglue_home_hidden` | 128 | `unglueHomeHidden` |
| 7 | `_expand_home_token` | 138 | `expandHomeToken` |
| 8 | `_looks_like_redirect_target` | 154 | `looksLikeRedirectTarget` |
| 9 | `_quoted_spans` | 163 | `quotedSpans` |
| 10 | `_index_in_quoted_span` | 167 | `indexInQuotedSpan` |
| 11 | `_strip_separator_residue` | 171 | `stripSeparatorResidue` |
| 12 | `_ampersand_is_redirect` | 186 | `ampersandIsRedirect` |
| 13 | `_split_unquoted_command_segments` | 193 | `splitUnquotedCommandSegments` |
| 14 | `_collect_ps_path_tokens` | 227 | `collectPsPathTokens` |
| 15 | `_is_shell_flag_token` | 250 | `isShellFlagToken` |
| 16 | `_basename_lower` | 259 | `basenameLower` |
| 17 | `_segments_for_extract` | 266 | `segmentsForExtract` |
| 18 | `_path_aware_one_segment` | 282 | `pathAwareOneSegment`（含内部闭包 `_append_path_token`） |
| 19 | `extract_path_aware_command_accesses` | 366 | `ExtractPathAwareCommandAccesses` |
| 20 | `extract_shell_path_accesses` | 387 | `ExtractShellPathAccesses` |
| 21 | `_resolve_path_str` | 452 | `resolvePathStr` |
| 22 | `_specs_for_tool` | 467 | `specsForTool` |
| 23 | `extract_accesses_native` | 471 | `ExtractAccessesNative` |

**关键约束**：`extract_accesses_native` 的三个分支顺序不可调换：

1. shell 工具 → `extract_shell_path_accesses`（根据 `workdir` 解析基准目录，绝对路径 `workdir` 优先）
2. 有注册 spec → 按 spec 的 `arg_name` / `action` 逐项解析
3. `tool_name in _PATH_TOOLS` → 遍历 `_iter_path_strings`，读写由 `_WRITE_PATH_TOOLS` 决定

第 3 分支的 `_PATH_TOOLS` 与 `_iter_path_strings` 来自 `toolguard/tool_policy.py`，Go 侧已是 `tiered_policy.go` 的**非导出**符号。**跨文件引用非导出符号在 Go 中不可能**，因此需在 `path_extract.go` 内重新声明这两项，或在 `tiered_policy.go` 中导出。见 §十一 偏差 D3。

### 3.4 `fileguard/file_guard.go` → `security/fileguard/file_guard.go`

| # | Python 符号 | 行 | Go 对应 | 说明 |
|---|---|---|---|---|
| 1 | `FileGuardMode` | — | `type FileGuardMode string` | "legacy" / "native" |
| 2 | `FileGuardMatch` | — | `type FileGuardMatch string` | "prefix" / "glob" |
| 3 | `_WRITE_PATH_TOOLS` | — | `writePathTools` | 5 项 frozenset |
| 4 | `_PATH_AWARE_COMMANDS` | — | `pathAwareCommands` | 24 项 frozenset |
| 5 | `_LEVEL_ORDER` | — | `levelOrder` | deny=0/ask=1/allow=2 |
| 6 | `FileGuardAxisDefaults` | 59 | `FileGuardAxisDefaults struct` | read/write/exec |
| 7 | `FileGuardPathRule` | 66 | `FileGuardPathRule struct` | path/read/write/exec/match |
| 8 | `EffectiveFileGuardConfig` | 75 | `EffectiveFileGuardConfig struct` | enabled/mode/defaults/paths/workspace_root |
| 9 | `_parse_level` | — | `parseFileGuardLevel` | 默认 ASK |
| 10 | `_axis_from_star` | — | `axisFromStar` | |
| 11 | `_posix_norm` | — | `posixNorm` | 失败回退 `str(p).replace("\","/").rstrip("/")` |
| 12 | `_expand_raw` | — | `expandRaw` | `expandvars` + `expanduser` |
| 13 | `_apply_implications` | — | `applyImplications` | Write⇒Read / Exec⇒Read，显式更严 read 优先并 WARN |
| 14 | `_compile_path_entry` | 133 | `compilePathEntry` | 见 §3.4.1 |
| 15 | `_has_axis_dict` | — | `hasAxisDict` | |
| 16 | `_has_native_file_guard` | 167 | `hasNativeFileGuard` | 见 §3.4.2 |
| 17 | `_compile_workspace_axis_rule` | 190 | `compileWorkspaceAxisRule` | 无 root 时 WARN 并跳过 |
| 18 | `_explicit_enabled` | 220 | `explicitEnabled` | 返回 `*bool`（Python `bool | None`） |
| 19 | `normalize_path_guard_config` | 226 | `NormalizePathGuardConfig` | 见 §3.4.3 |
| 20 | `_normalize_legacy` | 276 | `normalizeLegacy` | 见 §3.4.4 |
| 21 | `_normalize_native` | 351 | `normalizeNative` | 见 §3.4.5 |
| 22 | `_looks_like_path`（legacy 版） | 465 | `looksLikePathLegacy` | **与 path_extract 同名但不同实现，必须分开** |
| 23 | `_extract_paths_from_command` | 473 | `extractPathsFromCommand` | legacy 专用 |
| 24 | `extract_paths_legacy` | 502 | `ExtractPathsLegacy` | |
| 25 | `_tool_default_action` | 532 | `toolDefaultAction` | shell 也返回 read |
| 26 | `_match_glob` | 541 | `matchFileGuardGlob` | 手写 `**`/`*`/`?` 转正则 |
| 27 | `_level_for_action` | 571 | `levelForAction` | |
| 28 | `FileGuardChecker` | 576 | `FileGuardChecker struct` | |
| 29 | `.enabled` / `.mode` | 588/592 | `Enabled()` / `Mode()` | |
| 30 | `.evaluate(tool_name, tool_args)` | 595 | `Evaluate(toolName string, toolArgs map[string]any) *PermissionResult` | 见 §3.4.6 |
| 31 | `.collect_ask_accesses(...)` | 664 | `CollectAskAccesses(...) []AskAccess` | 见 §3.4.7 |
| 32 | `._resolve_one(path, action)` | 706 | `resolveOne(path string, action FileAction) (PermissionLevel, string)` | 见 §3.4.8 |
| 33 | `build_file_guard_checker(...)` | 752 | `BuildFileGuardChecker(...) *FileGuardChecker` | enabled=false 返回 nil |
| 34 | `_PATH_CLASS_TOOLS` | 771 | `pathClassTools` | 14 项 |
| 35 | `_LOAD_WARNED_RULE_IDS` | 779 | `loadWarnedRuleIDs` + mutex | 全局去重缓存 |
| 36 | `_looks_like_path_pattern` | 782 | `looksLikePathPattern` | |
| 37 | `report_legacy_path_rules_at_load` | 795 | `ReportLegacyPathRulesAtLoad` | 返回 flagged id 列表 |

#### 3.4.1 `_compile_path_entry` 的过短前缀跳过（易漏）

```python
if match == "prefix":
    norm = path_s.replace("\\", "/").rstrip("/")
    if "/" not in norm:
        return None          # 过短前缀（无 /）跳过
    try:
        path_s = _posix_norm(_expand_raw(path_s))
    except (OSError, RuntimeError, ValueError):
        path_s = norm
    if "/" not in path_s.rstrip("/"):
        return None          # 归一化后仍无 /，再跳一次
return FileGuardPathRule(...)
```

**两次判断**，中间隔着归一化。第一次针对原始串，第二次针对归一化结果。目的：避免 `"C:"` 误匹配整盘。不可合并为一次。

#### 3.4.2 `_has_native_file_guard` 的判定顺序

```python
if _has_axis_dict(fg.get("defaults")) or _has_axis_dict(fg.get("workspace")):
    return True
paths = fg.get("paths")
if not isinstance(paths, list) or not paths:
    return False
if any(p.get("match") == "glob" for p in paths):
    return True
ext = permissions.get("external_directory")
if isinstance(ext, (dict, str)) and bool(ext):
    return False      # 仍有 external_directory → 保持 Legacy
return True
```

关键：**`paths` 非空且存在 `external_directory` 时返回 False（Legacy）**。这是为了防止 `/add-dir` 写入的 `file_guard.paths` 导致 workspace 隐式放行被误取消。

#### 3.4.3 `normalize_path_guard_config` 的 enabled 推导

```python
explicit = _explicit_enabled(fg)
if explicit is not None:
    enabled = explicit
elif has_ext or has_trusted:      # external_directory 非空 或 trusted_dirs 非空
    enabled = True
else:
    enabled = False
```

**`trusted_dirs` 非空会隐式开启 file_guard**。这是 M8/M9 必须本批补齐的直接理由——传空 trusted_dirs 会导致 file_guard 被关掉。

#### 3.4.4 `_normalize_legacy` 的规则追加顺序

1. workspace 隐式全放行（`read=allow, write=allow, exec=allow, prefix`）
2. 每个 trusted_dir → `read=allow, write=allow, exec=ask, prefix`
3. `external_directory` 具名键 → allow 走三轴 allow（exec 为 ask），其余三轴同 action
4. `file_guard.paths` 中**无 glob** 的项并入（`/add-dir` 写入）

顺序即优先级（`_resolve_one` 取最长前缀），不可调整。

#### 3.4.5 `_normalize_native` 的规则追加顺序

1. `file_guard.paths` 逐项（prefix 或 glob）
2. `file_guard.workspace` → 绑定 `workspace_root` 的前缀规则
3. 每个 trusted_dir → `read=allow, write=allow, exec=ask, prefix`
4. `external_directory` 具名键作**迁移源**（不覆盖已有 paths，逐条 WARN）

defaults 的推导：若 `raw_defaults` 无任何 read/write/exec 键，从 `external_directory["*"]` 或 `external_directory`（字符串）迁移；否则按 raw_defaults 解析。

#### 3.4.6 `evaluate` 的判定流程

```
enabled=false → nil
workspace==nil → nil（debug 日志）
mode=="native" → extract_accesses_native → [(path, action)]
mode=="legacy" → extract_paths_legacy → [(path, action=_tool_default_action)]
accesses 为空 → nil
overall = ALLOW; hit_external=[]; matched_bits=[]
for (p, action) in accesses:
    level, rule_id = _resolve_one(p, action)
    overall = strictest(overall, level)
    if level != ALLOW:
        hit_external.append(p.as_posix())
        if rule_id: matched_bits.append(rule_id)
overall == ALLOW → nil
mode=="legacy":
    reason 用 accesses[0][0] 作 sample，matched = "external_directory.*"
mode=="native":
    reason 用 hit_external[0] 或 accesses[0][0] 作 hint，matched = "|".join(matched_bits) or "file_guard"
返回 PermissionResult(permission=overall, reason, matched_rule=matched, external_paths=hit_external or None)
```

**`hit_external or None`** 的语义：空列表要返回 `None`（Go 用 nil 切片），不是空切片——Python 的 `PermissionResult.external_paths` 默认 `None`，下游 `policy_eval._validated_external_paths` 对 `None` 与 `[]` 处理不同。

#### 3.4.7 `collect_ask_accesses` 的去重维度

```python
seen: set[tuple[str, FileGuardAction]] = set()
for p, action in accesses:
    level, _rule_id = self._resolve_one(p, action)
    if level != PermissionLevel.ASK: continue
    key = (p.as_posix(), action)
    if key in seen: continue
    seen.add(key)
    out.append(key)
```

去重键是 **`(path, action)` 二元组**，不是单纯 path。同一路径在不同轴上可能一个 ASK 一个 ALLOW。且**只收 ASK**，不含 DENY/ALLOW。

#### 3.4.8 `_resolve_one` 的 prefix 与 glob 竞争

```python
best_prefix = None            # (prefix_len, rule)
glob_hits = []
for rule in paths:
    if rule.match == "glob":
        if _match_glob(rule.path.replace("\\","/"), path_posix):
            glob_hits.append(_level_for_action(rule, action))
        continue
    prefix = rule.path.rstrip("/")
    if path_posix == prefix or path_posix.startswith(prefix + "/"):
        if contains_path(prefix, path) or path_posix == prefix or path_posix.startswith(prefix + "/"):
            ln = len(prefix)
            if best_prefix is None or ln > best_prefix[0]:
                best_prefix = (ln, rule)
candidates = []
rule_id = None
if best_prefix is not None:
    candidates.append(_level_for_action(best_prefix[1], action))
    rule_id = f"file_guard:prefix:{best_prefix[1].path}"
candidates.extend(glob_hits)
if glob_hits and rule_id is None:
    rule_id = "file_guard:glob"
if candidates:
    level = min(candidates, key=lambda lv: _LEVEL_ORDER[lv])   # deny > ask > allow
    return level, rule_id
defaults = self._effective.defaults
if action == "write": return defaults.write, "file_guard:defaults"
if action == "exec":  return defaults.exec,  "file_guard:defaults"
return defaults.read, "file_guard:defaults"
```

三处易错：

1. prefix 判定有**两层**：`startswith` 之外还要 `contains_path(prefix, path)`——后者处理 Windows 盘符怪例
2. `best_prefix` 取**最长**前缀（`ln > best_prefix[0]`），不是最先匹配
3. glob 与 prefix 的 level 一起取 `min`（严格者胜），但 `rule_id` 优先 prefix

### 3.5 `fileguard/sensitive_paths.go`

| # | Python 符号 | Go 对应 |
|---|---|---|
| 1 | `_SENSITIVE_PATHS_CACHE` | `sensitivePathsCache` + mutex |
| 2 | `_HOME_PREFIX = "~/"` | `homePrefix` |
| 3 | `_VALID_ACTIONS` | `validSensitiveActions` |
| 4 | `_AXIS_RANK` | `axisRank`（deny=0/ask=1/allow=2） |
| 5 | `_package_rules_yaml_path()` | 复用 `resources.ResolveBuiltinRulesYAMLPath()` |
| 6 | `_load_package_yaml()` | 复用 `resources.ParseBuiltinRulesFromFile` |
| 7 | `_expand_home_path` | `expandHomePath` |
| 8 | `_yaml_action(item)` | `yamlAction(item map[string]any) string` | 空 → WARN 返回 ""；非法 → WARN；allow → WARN 且跳过 |
| 9 | `load_package_sensitive_paths()` | `LoadPackageSensitivePaths() []map[string]any` |
| 10 | `_stricter_axis` | `stricterAxis` | 未知按 ask |
| 11 | `_merge_sensitive_paths` | `mergeSensitivePaths` |
| 12 | `merge_package_sensitive_paths` | `MergePackageSensitivePaths(permissions map[string]any, inject *bool) map[string]any` |

**输出条目的固定字段**：`path`（已展开 `~`）、`match`（默认 "glob"）、`read`/`write`/`exec`（都等于 action）、`layer="builtin"`。

**合并语义**（`_merge_sensitive_paths`）：

- overlay 与 packaged 同 path → 三轴逐个取严；overlay 是 allw 而 floor 不是 → 用 floor；overlay 空 → 用 floor；`layer` 强制 "builtin"
- packaged 独有的 path → 追加到末尾
- overlay 独有的 path → 原样保留

### 3.6 `netguard/net_guard.go` + `netguard/net_urls.go`

`net_guard.py`（163 行）：

| # | Python 符号 | Go 对应 |
|---|---|---|
| 1 | `_FETCH_TOOLS` | `fetchTools`（`mcp_fetch_webpage` / `fetch_webpage` / `web_fetch_webpage`） |
| 2 | `_URL_ARG_KEYS` | `urlArgKeys`（6 项，**顺序敏感**） |
| 3 | `_VALID_ACTIONS` | `validNetActions`（仅 allow/deny） |
| 4 | `_URL_WILDCARD_CHARS` | `urlWildcardChars` |
| 5 | `_parse_action` | `parseNetAction` |
| 6 | `extract_fetch_url` | `ExtractFetchURL` |
| 7 | `_hostname` | `urlHostname` |
| 8 | `_match_url_glob` | `matchURLGlob` |
| 9 | `match_net_pattern` | `MatchNetPattern` |
| 10 | `NetGuardChecker` | `NetGuardChecker struct` |
| 11 | `.evaluate` | `Evaluate` |
| 12 | `build_net_guard_checker` | `BuildNetGuardChecker` |

`net_urls.py`（152 行）：`load_package_net_urls` / `_has_package_net_urls` / `_stricter` / `merge_package_net_urls` 及 `_package_rules_yaml_path` / `_parse_action`。

**注意**：`net_guard._parse_action` 与 `net_urls._parse_action` 是**两个同名但不同实现**的函数（前者支持 `{"action":...}` / `{"fetch":...}` 两种包装，后者同样），二者在 Python 里各写一遍且逻辑一致。Go 侧按规则 9.1 保持两个文件各有自己的实现。

`_has_package_net_urls` 语义特殊：`set(packaged).issubset(urls)`——packaged 的**所有键**都在 urls 里才算「已有内置」。空 packaged 返回 False。

### 3.7 `toolguard/builtin_rules.go`（新建）

| # | Python 符号 | Go 对应 |
|---|---|---|
| 1 | `_COMMAND_RULES_CACHE` | `commandRulesCache` + mutex |
| 2 | `_VALID_ACTIONS` | `validCommandRuleActions` |
| 3 | `_FALSE_STRINGS` / `_TRUE_STRINGS` | `falseStrings` / `trueStrings` |
| 4 | `package_builtin_rules_enabled` | `PackageBuiltinRulesEnabled(permissions map[string]any) bool` |
| 5 | `_package_rules_yaml_path` | 复用 `resources.ResolveBuiltinRulesYAMLPath()` |
| 6 | `_load_package_yaml` | 复用 `resources.ParseBuiltinRulesFromFile` |
| 7 | `_yaml_action` | `yamlCommandRuleAction` | 同 sensitive_paths 语义，日志前缀不同 |
| 8 | `load_package_command_rules` | `LoadPackageCommandRules() []map[string]any` |
| 9 | `inline_package_command_rules` | `InlinePackageCommandRules(permissions map[string]any) map[string]any` |

`PackageBuiltinRulesEnabled` 语义：`permissions["package_builtin_rules"]` 缺失 → true；bool → 原值；字符串按 false/0/no/off 与 true/1/yes/on 判定；其他 → true。

`InlinePackageCommandRules` 语义：先剔除已有 `layer=="builtin"` 的项，把 packaged 规则（带 `layer=builtin` + `action`）放前面，用户规则追加在后。

### 3.8 `resources/resources.go` 扩展

新增两个结构体与两个字段：

```go
// SensitivePathRule 内置敏感路径规则（file_guard 路径层底线）
type SensitivePathRule struct {
    ID       string `yaml:"id"`
    Path     string `yaml:"path"`
    Match    string `yaml:"match"`
    Severity string `yaml:"severity"`
    Action   string `yaml:"action"`
}

// BuiltinRules 内置安全规则集合（扩两个字段）
type BuiltinRules struct {
    Rules          []SecurityRule        `yaml:"rules"`
    SensitivePaths []SensitivePathRule   `yaml:"sensitive_paths"`
    NetUrls        map[string]any        `yaml:"net_urls"`   // 值可能是 "deny" 或 {"action":"deny"}
}
```

`NetUrls` 用 `map[string]any` 而非 `map[string]string`：Python 的 `_parse_action` 接受 `{"action":...}` / `{"fetch":...}` 两种包装形式，YAML 里当前是纯字符串，但结构上不封闭。用 `any` 保留解析弹性（见 §十一 偏差 D6）。

`SensitivePaths` 用 `[]SensitivePathRule` 会丢失未在结构体声明的字段；Python 是 `dict[str, Any]` 全保留。Go 侧若需完整保真，应改为 `[]map[string]any`。**本 spec 选 `[]map[string]any`**，与 `NetUrls` 一致，避免字段丢失。

### 3.9 `core.py` → `security/permission_engine.go`（重写）

| # | Python 符号 | Go 对应 | 说明 |
|---|---|---|---|
| 1 | `_has_builtin_layer` | `hasBuiltinLayer` | 判定 list 里是否有 `layer=="builtin"` |
| 2 | `_fill_legacy_host_rule_actions` | `fillLegacyHostRuleActions` | severity + permission_mode 补 action |
| 3 | `prepare_permissions_for_engine` | `PreparePermissionsForEngine` | 见 §3.9.1 |
| 4 | `PermissionEngine.__init__` | `NewPermissionEngine` | 构造 fileGuard + netGuard |
| 5 | `_rebuild_file_guard` | `rebuildGuards` | 同时重建 fileGuard 与 netGuard |
| 6 | `update_config` | `UpdateConfig` | 走 `PreparePermissionsForEngine` |
| 7 | `update_trusted_dirs` | `UpdateTrustedDirs` | **新增** |
| 8 | `trusted_dirs`（property） | `TrustedDirs()` | **新增** |
| 9 | `update_llm` | `UpdateLLM` | 已有 |
| 10 | `enabled` | `Enabled()` | 已有 |
| 11 | `set_permission_checks_active` | `SetPermissionChecksActive` | 已有 |
| 12 | `check_tool_permission_directly` | `CheckToolPermissionDirectly` | 已有 |
| 13 | `evaluate_global_policy_directly` | `EvaluateGlobalPolicyDirectly` | 内部改调 fileGuard |
| 14 | `check_permission` | `CheckPermission` | A+B+C 三段 |
| 15 | `_get_reason` | `getReason` | 已有 |
| 16 | `build_permission_interrupt_rail` | `BuildPermissionInterruptRail`（`factory.go`） | 需核 workspace_root 注入 |
| — | `set_scene_hook`（Go 独有） | `SetSceneHook` | ⚠️ Python 无此方法，见 §十一 偏差 D1 |
| — | `set_workspace_root`（Go 独有） | `SetWorkspaceRoot` | 🔴 **删除** |

#### 3.9.1 `prepare_permissions_for_engine` 的注入顺序

```python
cfg = dict(config or {})
if not package_builtin_rules_enabled(cfg):
    return _fill_legacy_host_rule_actions(cfg)
if not _has_builtin_layer(cfg.get("rules")):
    cfg = inline_package_command_rules(cfg)
fg = cfg.get("file_guard")
if isinstance(fg, dict) and fg.get("enabled") and not _has_builtin_layer(fg.get("paths")):
    cfg = merge_package_sensitive_paths(cfg)
ng = cfg.get("net_guard")
if isinstance(ng, dict) and ng.get("enabled") and not _has_package_net_urls(ng.get("urls")):
    cfg = merge_package_net_urls(cfg)
cfg = _fill_legacy_host_rule_actions(cfg)
return cfg
```

四个短路条件各自独立：

1. `package_builtin_rules=false` → 全部跳过，只补 action
2. `rules` 已有 builtin 层 → 不重复注入命令规则
3. `file_guard.enabled` 为假 → 不注入敏感路径
4. `net_guard.enabled` 为假 **或** urls 已含全部内置 → 不注入 net_urls

**最后统一 `_fill_legacy_host_rule_actions`**，即使前面提前 return 的分支也调了。

#### 3.9.2 `check_permission` 的三段管线（本批核心）

```python
# 1. Pipeline A
permission, matched_rule = self.evaluate_global_policy_directly(
    tool_name, tool_args, include_external_directory=False)
if permission is None:
    permission = ASK; matched_rule = "default"

# 2. Pipeline B
if self._file_guard is not None:
    path_result = self._file_guard.evaluate(tool_name, tool_args)
    if path_result is not None:
        path_rule = path_result.matched_rule or "file_guard"
        permission = strictest(permission, path_result.permission)
        matched_rule = f"{matched_rule}|{path_rule}"
        external_paths = path_result.external_paths
    else:
        # 日志：checked=true permission=none
else:
    # 日志：checked=false reason=disabled

# 3. Pipeline C（仅 fetch 工具；只有 allow/deny，无 ask）
if self._net_guard is not None:
    net_result = self._net_guard.evaluate(tool_name, tool_args)
    if net_result is not None:
        net_rule = net_result.matched_rule or "net_guard"
        permission = strictest(permission, net_result.permission)
        matched_rule = f"{matched_rule}|{net_rule}"
    else:
        # 日志：checked=true permission=none
else:
    # 日志：checked=false reason=disabled
```

**关键语义**：

- B 段返回 `None` 时不改动 permission/matched_rule/external_paths，但仍要打日志
- C 段**不写 external_paths**
- `matched_rule` 恒用 `f"{old}|{new}"` 拼接，**不判空**（Python 此处不像 `evaluate_global_policy_directly` 那样判空）——B/C 段是在 A 段之后，A 段已保证非空
- `external_paths` 只有 B 段能写，初值 `None`

#### 3.9.3 `evaluate_global_policy_directly` 的 include_external_directory

```python
if include_external_directory:
    path_result = self._file_guard.evaluate(tool_name, tool_args) if self._file_guard else None
    if path_result is not None:
        if permission is None:
            permission = path_result.permission
            matched_rule = path_result.matched_rule
        else:
            permission = strictest(permission, path_result.permission)
            matched_rule = f"{matched_rule}|{path_result.matched_rule}"
    # net_guard 同理，见 core.py L248-262
```

Go 现在的 `evaluateGlobalPolicyWithSnapshot` 用的是 `externalChecker.CheckExternalPaths`，本批改为 `fileGuard.Evaluate`，并补 netguard 分支。

### 3.10 `permissions/tool_capabilities.go`（9.90b）

| # | Python 符号 | 行 | Go 对应 |
|---|---|---|---|
| 1 | `TOOL_CAPABILITY_FACTS_VERSION = "2"` | 17 | `ToolCapabilityFactsVersion` |
| 2 | `HOST_STATIC_FACTS` / `NAME_CLASSIFICATION_HINT` / `UNKNOWN_FACTS` | 18-20 | 三常量 |
| 3 | `_is_exact_subagent_runtime_control` | 23 | `isExactSubagentRuntimeControl` |
| 4 | `ToolCapability`（14 字段） | 39 | `ToolCapability struct` |
| 5 | 30+ 个工具名集合 | 62-175 | 30+ 个包级变量 |
| 6 | `_STATIC_CANONICAL_TOOL_NAMES` | 176 | `staticCanonicalToolNames` |
| 7 | `_TRUSTED_BROWSER_MCP_SERVER_PREFIXES` | 197 | 2 项 tuple |
| 8 | `_TRUSTED_BROWSER_MCP_TOOL_SUFFIXES` | 200 | 25 项 frozenset |
| 9 | `normalize_tool_name` | 230 | `NormalizeToolName` |
| 10 | `classify_tool` | 235 | `ClassifyTool` |
| 11 | `_capability` | 520 | `newCapability` |
| 12 | `_redaction_key` | 561 | `redactionKey` |
| 13 | `_is_path_write_tool` | 578 | `isPathWriteTool` |
| 14 | `_is_trusted_browser_mcp_tool` | 582 | `isTrustedBrowserMCPTool` |
| 15 | `shell_tool_names` | 64 | `ShellToolNames()` |

**`classify_tool` 的 22 个分支顺序不可调换**（Python L242-518）。顺序即优先级：

1. `resolution.conflict` → unknown/unknown/unknown/high/high_flex
2. `_INTERNAL_READONLY_TOOLS` 且无别名 → cron 或 task_management
3. `_SHELL_TOOLS` → shell
4. `_CODE_TOOLS` → code
5. `_PATH_TOOLS` → path（write 判定决定 side_effects 与 risk_tier）
6. `_PUBLIC_SEARCH_TOOLS` → network/public_search
7. `_PUBLIC_WEB_READ_TOOLS` → network/public_web_read
8. `_HOST_TODO_TOOLS` → task_management/task_state/low
9. `_LEGACY_TODO_TOOLS` → task_management/task_state/medium/**high_flex=True**/**authoritative=False**
10. `_SESSION_STATUS_TOOLS` → task_management/session_status
11. `_SESSION_SEND_TOOLS` → task_management/cross_session_send
12. `_SESSION_MESSAGE_MANAGEMENT_TOOLS` → task_management/cross_session_resolution
13. `_ASK_USER_TOOLS` → interaction/ask_user
14. `startswith("cron_")` → cron/schedule_change/high/**authoritative=False**
15. `_is_exact_subagent_runtime_control` → subagent/subagent_runtime_control/low
16. `_SUBAGENT_TOOLS` → subagent/subagent_control/**authoritative=(lowered=="task_tool")**
17. `_FIXED_BROWSER_TOOLS` → browser（`browser_recall_offload` 特殊）
18. `browser_` 前缀 / `xiaoyi_gui_agent` / 可信 MCP → browser/browser_action/**authoritative=False**
19. `_SKILL_DISCOVERY_TOOLS` → skill/skill_discovery
20. `_SKILL_READ_TOOLS` → skill/skill_read
21. `_SKILL_CHANGE_TOOLS` → skill/skill_change
22. `_EXTERNAL_SEND_TOOLS` → external_send
23. `_DEVICE_TOOLS` → device
24. `_MEMORY_WRITE_TOOLS` → memory/memory_write
25. `_LOCAL_MEMORY_READ_TOOLS` → memory（search/read 分叉）
26. `_REVIEWED_MEMORY_READ_TOOLS` → memory/medium/**high_flex=True**/**authoritative=False**
27. `startswith("mcp_")` → mcp/unknown_mcp/**authoritative=False**
28. 兜底 → unknown/unknown/**authoritative=False**

第 14 分支（`cron_`）在第 2 分支（`_INTERNAL_READONLY_TOOLS`）之后——`cron_list_jobs` 等会先被第 2 分支接住，只有非只读的 `cron_*` 才落到第 14 分支。**这是最容易写错的顺序。**

### 3.11 `permissions/tool_decision_facts.go`

| # | Python 符号 | Go 对应 |
|---|---|---|
| 1 | `ToolDecisionFacts`（10 字段）+ 3 property | `ToolDecisionFacts struct` + `ToolName()` / `ToolCategory()` / `Paths()` |
| 2 | `DecisionRoute`（3 字段）+ 7 property | `DecisionRoute struct` + 7 方法 |
| 3 | `build_tool_decision_facts` | `BuildToolDecisionFacts` |
| 4 | `_workspace_root` | `workspaceRootOf` |
| 5 | `_core_accesses` | `coreAccesses` |

**`_core_accesses` 的 5 个分支顺序**（Python L154-179）：

1. `current_native_path_access(...)` 非 nil → 按 `action` 决定读写（`read` → reads，否则 writes），`known=true`
2. `tool_name == "send_file_to_user"` → 用 `send_paths`，去重后作 reads
3. `category not in {path, shell}` → `((), (), True)`
4. `root is None` → `((), (), False)`
5. `extract_accesses_native(...)`，异常捕获 → `((), (), False)`；正常 → 按 `action in {write, exec}` 归 writes，否则 reads

`known` 的最终判定：`capability.category == "shell" or bool(reads or writes)`。

**`lsp` + `operation=="workspaceSymbol"` 的特殊处理**：`access_args = {**args, "file_path": str(root)}`——把 workspace 根塞进去当 file_path。这是 Python 的 workaround，Go 必须照抄。

### 3.12 `permissions/network_scope.go`

| # | Python 符号 | Go 对应 | 备注 |
|---|---|---|---|
| 1 | `SECRET_QUERY_KEYS`（14 项） | `secretQueryKeys` | |
| 2 | `SECRET_QUERY_COMPACT_KEYS`（24 项） | `secretQueryCompactKeys` | |
| 3 | `SECRET_QUERY_WORDS`（12 项） | `secretQueryWords` | |
| 4 | `SECRET_QUERY_KEY_QUALIFIERS`（8 项） | `secretQueryKeyQualifiers` | |
| 5 | `PUBLIC_SUFFIX_ONLY_HOSTS`（10 项） | `publicSuffixOnlyHosts` | |
| 6 | `normalize_network_host` | `NormalizeNetworkHost` | 见 §3.12.1 |
| 7 | `network_host_rejection_reason` | `NetworkHostRejectionReason` | 见 §3.12.2 |
| 8 | `host_matches_allowed_domain` | `HostMatchesAllowedDomain` | |
| 9 | `has_secret_query` | `HasSecretQuery` | |
| 10 | `_is_secret_query_key` | `isSecretQueryKey` | 见 §3.12.3 |

#### 3.12.1 `normalize_network_host` 的三步

```python
raw = str(host or "").strip().strip("[]").rstrip(".").lower()
if not raw: return ""
try:
    return str(ipaddress.ip_address(raw))     # IP → 规范化文本
except ValueError:
    pass
try:
    return raw.encode("idna").decode("ascii").lower()
except UnicodeError:
    return ""
```

Go 等价：

| 步骤 | Python | Go |
|---|---|---|
| 去方括号 + 尾部点 + 小写 | 字符串操作 | `strings.TrimSuffix` / `ToLower` |
| IP 规范化 | `ipaddress.ip_address()` | `netip.ParseAddr` → `.String()` |
| IDNA | `.encode("idna")` | `golang.org/x/net/idna` 的 `idna.Lookup.ToASCII`，或 `idna.ToASCII` |

**注意** `str(ipaddress.ip_address("::1"))` → `"::1"`，`"::0:1"` → `"::1"`（压缩形式）。`netip.Addr.String()` 行为一致。

#### 3.12.2 `network_host_rejection_reason` 的 IP 与域名分叉

```python
normalized = normalize_network_host(host)
if not normalized: return "network_host_missing"
try:
    ip_address = ipaddress.ip_address(normalized)
except ValueError:
    labels = normalized.split(".")
    if len(labels) < 2: return "network_single_label_host"
    if normalized in PUBLIC_SUFFIX_ONLY_HOSTS: return "network_public_suffix_host"
    return None
if not ip_address.is_global: return "network_host_not_public"
return None
```

`ip_address.is_global` 的 Go 等价需手写——Go 标准库**没有** `is_global`。CPython `is_global` 定义：不是 private、不是 loopback、不是 link-local、不是 multicast、不是 reserved、不是 unspecified、不是共享地址（100.64/10）。**必须按 CPython `ipaddress.py` 的 `is_global` 实现逐条照抄**，不要用 `IsPrivate() && !IsLoopback()` 之类近似。

### 3.13 `permissions/policy_eval.go`

| # | Python 符号 | Go 对应 |
|---|---|---|
| 1 | `ALLOW_LEVEL` / `ASK_LEVEL` / `DENY_LEVEL` | 3 常量 |
| 2 | `_MAX_EXTERNAL_PATHS` = 64 | `maxExternalPaths` |
| 3 | `_MAX_EXTERNAL_PATH_LENGTH` = 4096 | `maxExternalPathLength` |
| 4 | `_MAX_EXTERNAL_PATHS_TOTAL_LENGTH` = 16384 | `maxExternalPathsTotalLength` |
| 5 | `PolicyEvaluation` | `PolicyEvaluation struct` |
| 6 | `OpenJiuwenPolicyEvaluator` | `OpenJiuwenPolicyEvaluator struct` |
| 7 | `.evaluate(invocation, *, scene_approval_is_final)` | `Evaluate(ctx, invocation, sceneApprovalIsFinal)` |
| 8 | `._normalize_tool_name` | `normalizeToolName` |
| 9 | `._parse_tool_args` | `parseToolArgs` |
| 10 | `._evaluate_scene_hook` | `evaluateSceneHook` |
| 11 | `._effective_permission_config` | `effectivePermissionConfig` |
| 12 | `._runtime_permission_config` | `runtimePermissionConfig` |
| 13 | `._apply_config` | `applyConfig` |
| 14 | `._trusted_dirs`（static） | `trustedDirsOf` |
| 15 | `_evaluation_from_permission_result` | `evaluationFromPermissionResult` |
| 16 | `_validated_external_paths` | `validatedExternalPaths` |
| 17 | `_permission_level_value` | `permissionLevelValue` |
| 18 | `_evaluation_from_path_result` | `evaluationFromPathResult` |
| 19 | `_strictest_evaluation` | `strictestEvaluation` |

**`evaluate` 的 7 步**（Python L66-144）顺序敏感：

1. 归一化工具名 + 解析 args
2. 取 engine、effective_config；判断 `refresh_failed`
3. engine 与 config 都在 → `_apply_config`，失败置 `refresh_failed=True`
4. 调 `_evaluate_scene_hook`；若返回 DENY 或 `scene_approval_is_final` → **直接返回**
5. 选 engine_decision：`engine is None` → fail_closed；`refresh_failed` → fail_closed；否则再次 `_apply_config` 后 `check_permission`，异常 → fail_closed
6. `normalized_tool_name == SEND_FILE_TOOL_NAME` → 调 `_path_guard_evaluator.evaluate(...)`
7. `_strictest_evaluation(scene_decision, engine_decision, _evaluation_from_path_result(path_result), path_result=path_result)`

第 3 步与第 5 步**各 apply 一次 config**，注释说明：`A scene hook may await another callback. Reapply this call's captured input before the synchronous Core policy pipelines.` Go 无 await，但仍需照抄两次（`_evaluate_scene_hook` 可能产生并发修改）。

`_strictest_evaluation` 的 `max` 语义：Python `max(candidates, key=precedence)` 取**最严**（precedence deny=2）。Go 需手写循环，且 `matched_rule` 与 `external_paths` 要跨全部 candidates 去重合并（不是只取最严那条的）。

### 3.14 `permissions/permissions_layers.go`

#### 3.14.1 函数清单（24 个）

| # | Python 符号 | 行 | Go 对应 | 说明 |
|---|---|---|---|---|
| 1 | `permission_storage_lock` | 18 | `PermissionStorageLock` | `@contextmanager` → 返回 `func() error` 的 acquire/release 对；见 §3.14.2 |
| 2 | `user_permissions_path` | 51 | `UserPermissionsPath` | `get_config_dir()/user_permissions.yaml` |
| 3 | `session_permissions_path` | 57 | `SessionPermissionsPath` | `get_agent_sessions_dir()/<sid>/session_permissions.yaml`；空 sid 抛 ValueError |
| 4 | `_load_yaml_dict` | 66 | `loadYAMLDict` | `strict bool`；非 mapping 且 strict → 抛错 |
| 5 | `_dump_yaml_dict` | 83 | `dumpYAMLDict` | 失败 WARN 返回 false |
| 6 | `_as_permissions_section` | 95 | `asPermissionsSection` | 见 §3.14.3 |
| 7 | `_scalar_level` | 104 | `scalarLevel` | dict → 取 `"*"`；非 str 返回 `""`（Python None） |
| 8 | `_str_names` | 113 | `strNames` | 过滤空白项 |
| 9 | `_ordered_tool_names_at_level` | 123 | `orderedToolNamesAtLevel` | 见 §3.14.4 |
| 10 | `_tool_names_at_level` | 143 | `toolNamesAtLevel` | 返回 set |
| 11 | `_override_ids` | 147 | `overrideIDs` | 提取 `id` 字段 |
| 12 | `_normalized_tool_names` | 160 | `normalizedToolNames` | str/list/tuple/set → []string |
| 13 | `_override_key` | 174 | `overrideKey` | 返回可比较 struct |
| 14 | `_override_keys` | 184 | `overrideKeys` | |
| 15 | `_stable_override_id` | 194 | `stableOverrideID` | 见 R1 |
| 16 | `_unique_override_ids` | 205 | `uniqueOverrideIDs` | |
| 17 | `_path_key` | 225 | `pathKey` | |
| 18 | `_path_keys` | 231 | `pathKeys` | |
| 19 | `load_global_permissions` | 242 | `LoadGlobalPermissions` | 从 config 取 `permissions` 段 |
| 20 | `load_user_permissions` | 250 | `LoadUserPermissions` | |
| 21 | `load_session_permissions` | 254 | `LoadSessionPermissions` | |
| 22 | `read_permission_layers_locked` | 265 | `ReadPermissionLayersLocked` | strict 读三层 + deepcopy |
| 23 | `capture_permission_layers` | 288 | `CapturePermissionLayers` | ⤵️ 依赖 `compose_host_effective_permissions`（9.90d） |
| 24 | `overlay_from_effective` | 302 | `OverlayFromEffective` | 见 §3.14.5 |
| 25 | `_merge_overlay_into_current` | 401 | `mergeOverlayIntoCurrent` | |
| 26 | `persist_user_overlay_from_effective` | 429 | `PersistUserOverlayFromEffective` | ⤵️ 9.90d |
| 27 | `persist_session_overlay_from_effective` | 443 | `PersistSessionOverlayFromEffective` | ⤵️ 9.90d |
| 28 | `list_saved_session_ids` | 454 | `ListSavedSessionIDs` | 扫描 sessions 目录，**排序后返回** |
| 29 | `_tool_list` | 469 | `toolList` | 非 list → 空 |
| 30 | `_list_has_tool` | 474 | `listHasTool` | **`item.strip() == tool`**，不是 `item == tool` |
| 31 | `_without_tool` | 478 | `withoutTool` | 同上 |
| 32 | `_set_user_tool_level` | 482 | `setUserToolLevel` | 见 §3.14.6 |
| 33 | `_drop_session_allow` | 503 | `dropSessionAllow` | 只删 allow 轴与 `tools` 里 allow 的项 |
| 34 | `_strip_tool_overrides` | 525 | `stripToolOverrides` | 见 §3.14.7 |
| 35 | `_session_file_lock` | 558 | `sessionFileLock` | 调用方须已持 `permission_storage_lock` |
| 36 | `update_user_tool_level` | 569 | `UpdateUserToolLevel` | 见 §3.14.8 |
| 37 | `save_user_permissions` | 634 | `SaveUserPermissions` | 委托 `UpdateUserPermissions` |
| 38 | `save_user_permissions_locked` | 638 | `SaveUserPermissionsLocked` | 调用方持锁 |
| 39 | `save_session_permissions_locked` | 643 | `SaveSessionPermissionsLocked` | 调用方持锁 |
| 40 | `update_user_permissions` | 648 | `UpdateUserPermissions` | mutator 模式 |

（Python 文件共 676 行，函数 40 个，含 9 个私有辅助函数与 5 个公开导出；上表按 Python 行号顺序全部列出。）

#### 3.14.2 `permission_storage_lock` 的锁顺序与不可重入

```python
@contextmanager
def permission_storage_lock(session_id=None, *, lock_timeout=10.0):
    """Lock cooperative writers in Global, User, Session order; never reenter."""
    with config_write_lock(lock_timeout=lock_timeout), ExitStack() as stack:
        paths = [user_permissions_path()]
        if session_id and str(session_id).strip():
            paths.append(session_permissions_path(session_id))
        for path in paths:
            path.parent.mkdir(parents=True, exist_ok=True)
            stack.enter_context(portalocker.Lock(str(_config_lock_path(path)), timeout=lock_timeout))
        yield
```

三个语义点：

1. **`config_write_lock` 在最外层**，然后是 user 文件锁，最后是 session 文件锁——固定顺序防死锁
2. **`never reenter`**：同一 goroutine 不可重入，Go 用 `sync.Mutex` 会死锁；必须用「非重入」语义的文件锁（`AcquireFileLock` 是 `LOCK_EX|LOCK_NB` + 轮询，重入会超时失败——这恰好符合「不可重入」）
3. **`path.parent.mkdir(parents=True, exist_ok=True)`** 在加锁**前**执行

Go 方案：`AcquireFileLock` / `ReleaseFileLock`（`sys_operation/local`），配合 `defer` 逆序释放。

#### 3.14.3 `_as_permissions_section` 的双形态

```python
if not isinstance(raw, dict): return {}
inner = raw.get("permissions")
if isinstance(inner, dict) and not any(key in raw for key in _OVERLAY_KEYS):
    return dict(inner)
return dict(raw)
```

即：**顶层是 `{permissions: {...}}` 且自身不含 overlay 键时，取内层**；否则取自身。`_OVERLAY_KEYS` 为 9 项（`allow_tools` / `ask_tools` / `deny_tools` / `approval_overrides` / `file_guard` / `tools` / `rules` / `net_guard` / `shell_guard`）。

#### 3.14.4 `_ordered_tool_names_at_level` 的双来源合并

```python
tools = layer.get("tools")            # 先取 tools 字典里该 level 的键（保持插入序）
key = {"allow":"allow_tools", ...}[level]
for name in _str_names(layer.get(key)):  # 再追加该 level 的列表
    _add(name)
```

**顺序敏感**：`tools` 字典的键序在前，`allow_tools` 列表在后，去重保首次出现。Python dict 保插入序，Go 的 `map` 不保序——**必须改用 `yaml.Node` 或带序切片**，否则 `overlay_from_effective` 产出的 `allow_tools` 顺序会变，进而影响 diff 结果。这是本文件最容易踩的坑。

#### 3.14.5 `overlay_from_effective` 的 base 计算差异

```python
if session:
    user_layer = _as_permissions_section(...)   # 取 user
    skip_layer = {}
else:
    user_layer = {}
    skip_layer = _as_permissions_section(session_permissions or {})  # 取 session
base_allow = allow(global) | allow(user_layer) | allow(skip_layer)
base_ask   = ask(global)                     # 注意：只取 global
base_deny  = deny(global)                    # 只取 global
```

**`base_ask` / `base_deny` 只算 global 层**，而 `base_allow` 算三层。这是有意的不对称：ask/deny 不可被上层放宽，只有 allow 可以叠加。照抄时不能「顺手统一」。

另外 `session=True` 时**不输出 `ask_tools` / `deny_tools`**（Session 层不允许 ask/deny），见 §3.14.5 的 `if not session:` 分支。

#### 3.14.6 `_set_user_tool_level` 的单列表约束

```python
removed_allow = _list_has_tool(allow_tools, tool) and level != "allow"
for key, listed in (("deny_tools","deny"), ("ask_tools","ask"), ("allow_tools","allow")):
    kept = _without_tool(tool_list(layer, key), tool)
    if listed == level: kept.append(tool)
    if kept: layer[key] = kept
    else: layer.pop(key, None)
# 再从 layer["tools"] 删同名键
```

三元组**迭代顺序是 deny → ask → allow**，且目标 level 的项**追加到末尾**（不是头部）。这决定了配置文件里的顺序。

#### 3.14.7 `_strip_tool_overrides` 的部分删除

```python
names = [t.strip() for t in item["tools"] if isinstance(t, str) and t.strip()]
if tool not in names: kept.append(item); continue
changed = True; removed += 1
rest = [n for n in names if n != tool]
if not rest: continue                    # 只指向该工具的 override 整体删除
updated = dict(item); updated["tools"] = rest
kept.append(updated)                     # 共享规则只摘掉该工具名
```

返回值是**被移除的 override 条数**（不是工具名数）。多工具共享的 override 只摘名字，不整体删。

#### 3.14.8 `update_user_tool_level` 的两阶段提交

```
阶段一（持 permission_storage_lock）：
  user = load_user_permissions()
  user_allow_removed = _set_user_tool_level(user, name, lv)
  if lv == "ask":
      overrides_removed += _strip_tool_overrides(user, name)
      for session_id in list_saved_session_ids():
          with _session_file_lock(session_id):
              session = load_session_permissions(session_id)
              ov += _strip_tool_overrides(session, name)
              dropped = _drop_session_allow(session, name)
              if dropped or ov: dump(session)   # 失败 → return None
  if not save_user_permissions_locked(user): return None
  global_permissions = load_global_permissions()
  saved_user = user

阶段二（锁外）：
  effective = compose_host_effective_permissions(global, saved_user, {})
  return {tool, level, effective_level, revoked{c...}}
```

**阶段二在锁外**，且 `compose` 失败时 `effective = {}` 但仍返回结构（`effective_level` 为 nil）。

`revoked` 结构固定三键：`user_allow`（0/1）、`session_allow`（计数）、`approval_overrides`（计数）。

#### 3.14.9 与 Go 现有能力的复用关系（已核实）

| Python | Go 复用点 | 判定 |
|---|---|---|
| `portalocker.Lock` | `sys_operation/local.AcquireFileLock` / `ReleaseFileLock`（`flock` / `LockFileEx`，50ms 轮询，10s 超时） | 无偏差（见 §六 D4） |
| `config_write_lock` | **Go 无对应** | 见下方核实结论 |
| `_load_yaml_round_trip` / `_dump_yaml_round_trip` | **Go 无独立函数**，能力在 `config.Config.SetRoundTrip` | 见下方核实结论 |
| `copy.deepcopy` | `utils.DeepCopyMap` | 无偏差 |
| `_override_key` 返回值含 tuple | Go 用可序列化的复合 key | 见 §六 D5 |
| `_stable_override_id` | 需与 Python 产出**完全一致** | 见 R1 / D8 |

**核实结论一：`config` 包没有跨进程写锁。**

`internal/common/config/config.go:32` 只有进程内 `sync.RWMutex`，`saveLocked`（:245）是同进程互斥。Python 的 `config_write_lock` 是**跨进程**文件锁，用于协调多个 agent 进程同时写 config 目录。

**处置**：`PermissionStorageLock` 最外层用 `AcquireFileLock(configDirLockPath)` 实现等价语义，然后依次加 user / session 文件锁。`configDirLockPath` 的命名需与 Python 的 `_config_lock_path` 对齐（Task 16 核实 Python `config.py` 该函数）。

**核实结论二：round-trip 能力存在但形态不同。**

`internal/common/config/roundtrip.go` 提供的是方法 `(*Config).SetRoundTrip(key string, value any) error`——更新**指定路径**的值并 round-trip 写回，基于 `yaml.v3` 的 `Node` 保留注释。Python 的 `_load_yaml_round_trip(path)` / `_dump_yaml_round_trip(path, data)` 是**按任意路径整份加载/写回**，不限定于已加载的 config 单例。

**处置**：`permissions_layers` 操作的是 `user_permissions.yaml` 与 `session_permissions.yaml`——**不在 config 单例管辖范围内**（config 单例管的是 `config.yaml`）。因此：

- 若 `roundtrip.go` 内部的 Node 读写逻辑可抽出为按路径的通用函数，则复用
- 否则在 `permissions_layers.go` 内自建 `loadYAMLDict` / `dumpYAMLDict`，用 `yaml.v3` 的 `Node` 直接读写

**这是 Task 16 开工前必须先做的技术确认**（列为 Task 16a），因为选择影响 `permissions_layers.go` 的内部结构。若走向「自建」，需在 spec 登记偏差（Python 复用 config 的 round-trip，Go 自建）。

### 3.15 三个小文件

| 文件 | 函数 | Go 对应 |
|---|---|---|
| `execution_provider_contract.py` | 3 frozenset + 2 函数 | `ExecutionProviderContract` 三常量 + `RequiresNoHostFallback` / `RequiresManualExecutionProviderReview` |
| `sandbox_profile.py` | `SandboxDescriptor` + `build_sandbox_profile` + 2 私有 | `SandboxDescriptor struct` + `BuildSandboxProfile` + `rawStringAttr` / `valueGet` |
| `native_path_context.py` | `NativePathAccess` + `NATIVE_PATH_ACCESS` ContextVar + 3 函数/类 | `NativePathAccess struct` + ctx key + `NativeArgumentsJSON` / `CurrentNativePathAccess` / `NativePathGuardProjection` |

`native_path_context.py` 的 `NATIVE_PATH_ACCESS` 是 Python `ContextVar`，Go 用 `context.WithValue` + 私有 key（对齐 9.90a 的 `tool_permission_context` 落点惯例，落 `swarm/schema/permission.go` 或本包内私有 ctx 函数，见 §十一 偏差 D5）。

`native_arguments_json` 的 `json.dumps(dict(args), sort_keys=True, ensure_ascii=True, allow_nan=False)` 语义：

- `sort_keys=True` → Go `json.Marshal` 对 map 自动按 key 排序 ✅
- `ensure_ascii=True` → Go **默认输出 UTF-8 非 ASCII**，需手动转义成 `\uXXXX`
- `allow_nan=False` → NaN/Inf 抛 ValueError；Go `json.Marshal` 对 NaN/Inf 也返回错误 ✅

**`ensure_ascii=True` 的等价是 Go 侧最容易漏的地方**——字符串比较依赖二者产出一致，漏了会导致 `current_native_path_access` 永远返回 nil。

---

## 四、`tool_security_rail` 的改造

### 4.1 补齐别名（M 类缺失）

`tool_security_rail.go:73` 的 `toolNameAliases` 补 `"web_fetch_webpage": "mcp_fetch_webpage"`。

**三份别名表各自修复**（用户拍板），不合并：

| 位置 | 数据 | 处置 |
|---|---|---|
| `internal/common/permission_tools`（新建） | `PERMISSION_TOOL_ALIASES` 4 项 | 本批新建 |
| `internal/agentcore/harness/rails/security/tool_security_rail.go:73` | `TOOL_NAME_ALIASES` 5 项 | 补第 5 项 |
| `internal/swarm/e2a/acp/acp_tool_updates.go:28` | Python `_TOOL_NAME_ALIASES` | 需核实对应 Python 源；若 Python 侧也是 4 项则不改 |

### 4.2 `collectFileGuardPersistAccesses` 重写

Python（`tool_security_rail.py:261`）：

```python
def _collect_file_guard_persist_accesses(self, normalized_name, tool_args, permissions_cfg):
    from openjiuwen.harness.security.file_guard import build_file_guard_checker
    if self._host.resolve_workspace_dir is None: return []
    try:
        workspace = self._host.resolve_workspace_dir()
    except Exception:
        logger.debug(...); return []
    trusted_dirs = list(self._engine.trusted_dirs)
    try:
        checker = build_file_guard_checker(
            permissions_cfg, workspace_root=workspace, trusted_dirs=trusted_dirs)
        if checker is None: return []
        return list(checker.collect_ask_accesses(normalized_name, tool_args))
    except Exception:
        logger.warning(...); return []
```

Go 现状（`tool_security_rail.go:693`）返回 `[]string`，用 `CheckExternalPaths`。本批改为返回 `[]persistAccess{Path string; Action FileAction}`，用 `BuildFileGuardChecker(...).CollectAskAccesses(...)`。

### 4.3 `persistAllowAlways` 的合并调用改为 `MergeFileGuardAccessAllows`

Python（`tool_security_rail.py:341`）：

```python
cfg, ok_tool = merge_permission_allow_rule_into_permissions(base_cfg, normalized_name, tool_args)
accesses = self._collect_file_guard_persist_accesses(normalized_name, tool_args, cfg)
ok_ext = False
if accesses:
    cfg, ok_ext = merge_file_guard_access_allows(cfg, accesses)
```

Go 现状用 `MergeExternalDirectoryAllowIntoPermissions(cfg, extPaths)`。本批改为 `MergeFileGuardAccessAllows(cfg, accesses)`。

**`base_cfg` 的来源也要对齐**：Python 先尝试 `host.get_permissions_snapshot`（经 `_invoke_permissions_hook`），失败才 `deepcopy(self._engine.config)`。Go 现状直接 `utils.DeepCopyMap(r.engine.Config())`，缺 snapshot 尝试。

### 4.4 新增 `merge_file_guard_access_allows`

Python 在 `openjiuwen/harness/security/patterns.py`，Go 落 `security/patterns.go`。

需逐个对齐的语义（从测试反推，见 `tests/unit_tests/harness/test_file_guard_persist.py`）：

- read 访问 → 保留精确目录（不写 `/**`）
- write 访问 → 按轴升格（read/write/exec 三者取严）
- 同路径多次访问合并

### 4.5 新增 `SetTrustedDirs`

Python（`tool_security_rail.py:213`）：

```python
def set_trusted_dirs(self, trusted_dirs):
    normalized = []
    if trusted_dirs:
        for d in trusted_dirs:
            if not d: continue
            try:
                normalized.append(Path(str(d)).expanduser().resolve(strict=False))
            except (OSError, RuntimeError): continue
    self._engine.update_trusted_dirs(normalized)
    logger.info("[PermissionEngine] permission.rail.trusted_dirs_updated count=%d", len(normalized))
```

Go 落 `PermissionInterruptRail.SetTrustedDirs(dirs []string)`。

---

## 五、删除清单（规则 8.3）

| # | 对象 | 路径 | 验收锚点 |
|---|---|---|---|
| 1 | `checker.go` 整文件 | `internal/agentcore/harness/security/checker.go` | — |
| 2 | `ExternalDirectoryChecker` 类型 | 同上 | `grep -r "ExternalDirectoryChecker" internal/ tests/ cmd/` → 0 |
| 3 | `NewExternalDirectoryChecker` | 同上 | 同上 → 0 |
| 4 | `(*ExternalDirectoryChecker).CheckExternalPaths` | 同上 | 同上 → 0 |
| 5 | `checker_test.go` 整文件（7 用例） | `internal/agentcore/harness/security/checker_test.go` | — |
| 6 | `PermissionEngine.SetWorkspaceRoot` | `permission_engine.go:119` | `grep -r "SetWorkspaceRoot" internal/ tests/ cmd/` → 0 |
| 7 | `TestPermissionEngine_SetWorkspaceRoot` | `permission_engine_test.go:131` | 同上 → 0 |
| 8 | `PermissionEngine.externalChecker` 字段 | `permission_engine.go:34` | `grep "externalChecker" internal/` → 0 |
| 9 | `evaluateGlobalPolicyWithSnapshot` 的 `externalChecker` 参数 | 同上 :328 | — |
| 10 | `collectExternalDirectoryPersistPaths` | `tool_security_rail.go:693` | `grep "collectExternalDirectoryPersistPaths" internal/` → 0 |
| 11 | `doc.go` 中 `ExternalDirectoryChecker` 条目 | `security/doc.go:5,10,24` | `grep "ExternalDirectoryChecker" internal/agentcore/harness/security/doc.go` → 0 |
| 12 | `toolNameAliases` 注释中「4 项」的隐含约定 | `tool_security_rail.go:71` | — |

**注意**：`permissions_persist.go` 与 `patterns.go` 中的 `MergeExternalDirectoryAllowIntoPermissions` **不删**——它在 Python 侧对应 `merge_external_directory_allow_into_permissions`（`approve/persist_rule_merge.py`），9.90e 才处理。本批只是 `tool_security_rail` 不再调它。

---

## 六、已知偏差

| ID | Python | Go | 偏差性质 | 理由 | 回收条件 |
|---|---|---|---|---|---|
| D1 | `PermissionEngine` 无 `set_scene_hook` | `SetSceneHook` 存在且有调用方 | Go 独有 API | Python 的 scene hook 由 host 持有并在 `check_permission` 内直取；Go 用 setter 注入 | 9.90d 若要重构 host 注入方式时统一 |
| D2 | `PermissionEngine` 无 `set_workspace_root` | 本批删除 | — | 已对齐，不再是偏差 | — |
| D3 | `path_extract.py` 从 `tool_policy` 导入 `_PATH_TOOLS` / `_iter_path_strings` | 需在 `path_extract.go` 重声明或导出 | 跨文件符号可见性 | Go 包内非导出符号可跨文件访问（同包），故**不构成偏差**——两文件同属 `security/fileguard` 与 `security` 不同包，需重新评估 | 见下方说明 |
| D4 | `portalocker.Lock(path, timeout)` | 复用 `sys_operation/local.AcquireFileLock` / `ReleaseFileLock` | 无偏差（已有等价实现） | Go 侧 `file_lock_{unix,windows}.go` 已是 `flock` + `LockFileEx` + 50ms 轮询 + 10s 超时，与 Python 同款 | — |
| D5 | `ContextVar`（`NATIVE_PATH_ACCESS` / `TOOL_PERMISSION_REQUEST_ID` 等） | `context.WithValue` | 语言差异 | Go 无 ContextVar 的 set/reset token 语义 | 无（长期） |
| D6 | `net_urls` 值类型 `dict[str, str]` | `map[string]any` | 结构放宽 | Python `_parse_action` 接受 `{"action":...}` 包装，YAML 当前是纯字符串但结构不封闭 | 若 YAML 确认恒为字符串可收紧为 `map[string]string` |
| D7 | `_load_yaml_round_trip(path)` / `_dump_yaml_round_trip(path, data)` 按任意路径整份读写 | `config.Config.SetRoundTrip(key, value)` 只更新已加载单例的指定路径；权限文件不在 config 单例管辖内 | **形态不一致** | Go 的 round-trip 能力绑在 config 单例上；`user_permissions.yaml` / `session_permissions.yaml` 是独立文件 | 若 `roundtrip.go` 的 Node 逻辑可抽为按路径通用函数则消除此偏差（Task 16a 确认） |
| D8 | `_stable_override_id` 内容哈希 | 需产出与 Python 一致的 id | **需实测** | 跨端读写同一 `user_permissions.yaml` 时 id 不一致会导致 overrides 重复 | 实现后跑 Python 侧对照用例 |
| D9 | `get_permissions_snapshot` / `persist_allow_rule` / `persist_session_allow_rule` 均为 `Callable[..., ...]`（可接受 `session_id` 关键字） | Go 函数类型固定签名 | 签名退化 | Go 无关键字参数；`session_id` 只能作固定位置参数 | 若将来需要无参调用兼容，改为 `func(sessionID ...string)` 变参 |
| D10 | `_SH_LAUNCHER_RE` 用变长 look-behind `(?<=[;&|()])` | RE2 不支持 look-around，需改写为两条正则 | 正则引擎差异 | 已在 §12.1 给出改写方案 | 用 Python 测试用例对照验证后固定 |

**D3 补充说明**：`path_extract.py` 是 `openjiuwen/harness/security/permission_engine/fileguard/`，`tool_policy.py` 是 `.../toolguard/`。Go 若把 fileguard 落为 `security/fileguard/` 子包，则与 `security` 包不同包，无法访问 `security` 的非导出 `_PATH_TOOLS`。两个选择：

- **方案 A**：`fileguard/` 独立子包，`path_extract.go` 内重新声明 `pathTools` 与 `iterPathStrings`（与 `tiered_policy.go` 的两份重复）
- **方案 B**：`security/tiered_policy.go` 把 `_PATH_TOOLS` / `_iter_path_strings` 导出为 `PathTools` / `IterPathStrings`，fileguard 子包引用

Python 里它们是**同包跨模块**（都在 `permission_engine` 下的不同子目录，但 Python 无导出概念）。Go 的包边界是硬约束，**方案 B 更贴近原意**（一份数据，避免双份漂移），需在 spec 中作为 D3 的选定方案。

---

## 七、实施任务分解

任务表见 §八，依赖链见 §十四。分批原则：

- **Task 0 已完成**（§十二），M6 的核实结果把 `command_canonicalize` 从「可选」升级为「必补」
- **Task 1-3a 是底座**：`permission_tools` / `resources`+YAML / `builtin_rules` / `command_canonicalize`，彼此无依赖，可并行
- **Task 4-8 是 fileguard/netguard 子树**：严格按 4→5→6 顺序（specs → path_extract → file_guard）
- **Task 9-11 是引擎切换**：必须等 6/8 就绪；删除 `checker.go` 放在同一 Task 内（规则 8）
- **Task 12-16 是 9.90b 本体**：12 完全独立；13→14→15 串行；16 依赖 D4/D7 方案（已定为复用现有锁）

---

## 八、任务清单

### Task 0：前置核实（不改代码）

1. `command_canonicalize.py`（134 行）在 Go 侧是否有对应（M6）
2. `host.py` 的 `ToolPermissionHost` 字段逐项核（`get_permissions_snapshot` / `_invoke_permissions_hook` / `persist_allow_rule` / `persist_session_allow_rule`）
3. `acp_tool_updates.go` 的别名表对应 Python 哪一处

### Task 1：`internal/common/permission_tools/`

- `permission_tools.go`（105 行实现）
- `permission_tools_test.go`
- `doc.go`

### Task 2：`resources` 扩展 + YAML 补齐

- `resources.go` 加 `SensitivePaths` / `NetUrls`
- `builtin_rules.yaml` 重写为 246 行（对齐 Python，含 7 条缺失 rules + 16 个 action + 两段）
- 测试

### Task 3：`security/toolguard/builtin_rules.go`

- `PackageBuiltinRulesEnabled` / `LoadPackageCommandRules` / `InlinePackageCommandRules`

### Task 4：`security/fileguard/file_tool_specs.go`

### Task 5：`security/fileguard/path_extract.go`（524 行）

依赖 Task 4 + D3 方案决定

### Task 6：`security/fileguard/file_guard.go`（870 行）

依赖 Task 4/5

### Task 7：`security/fileguard/sensitive_paths.go`

依赖 Task 2

### Task 8：`security/netguard/net_guard.go` + `net_urls.go`

依赖 Task 2

### Task 9：`security/permission_engine.go` 重写 A/B/C

依赖 Task 3/6/8 + 删除 `checker.go`

### Task 10：`security/patterns.go` 加 `MergeFileGuardAccessAllows`

### Task 11：`tool_security_rail.go` 改造

- 补别名
- `collectFileGuardPersistAccesses` 重写
- `persistAllowAlways` 改调
- `SetTrustedDirs`

### Task 12：9.90b 无依赖四文件

- `permissions/execution_provider_contract.go`
- `permissions/sandbox_profile.go`
- `permissions/native_path_context.go`
- `permissions/network_scope.go`

### Task 13：`permissions/tool_capabilities.go`

依赖 Task 1

### Task 14：`permissions/tool_decision_facts.go`

依赖 Task 5/13

### Task 15：`permissions/policy_eval.go`

依赖 Task 9 + 9.90e 的 `send_file_path_guard`（⤵️ 见 §十）

### Task 16：`permissions/permissions_layers.go`

**Task 16a 必须先做**：确认 `internal/common/config/roundtrip.go` 的 Node 读写逻辑能否抽为按路径通用函数（§3.14.9 核实结论二）。结论决定本文件是复用还是自建 `loadYAMLDict` / `dumpYAMLDict`。

锁方案已定（D4）：`AcquireFileLock` 依次加 config 目录锁 → user 文件锁 → session 文件锁。**不要用 `sync.Mutex`**——Python 的 `never reenter` 语义要求非重入锁，`sync.Mutex` 重入会死锁，而 `LOCK_EX|LOCK_NB` + 轮询会超时失败（符合预期）。

### Task 17：`doc.go` 同步 + 计划表回填

---

## 九、验证方式

1. `go build ./...` + `go vet ./...`
2. `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`（规则：必须带 CI 同款标签）
3. 受影响包覆盖率 ≥ 85%：
   - `internal/common/permission_tools`
   - `internal/agentcore/harness/security`（含 `fileguard` / `netguard` / `toolguard` 子包）
   - `internal/agentcore/harness/resources`
   - `internal/swarm/agents/harness/common/rails/permissions`
   - `internal/agentcore/harness/rails/security`
4. 删除锚点为空（§五 表格逐条 grep）
5. `go run ./cmd/integration-audit -verify`（若涉及集成测试改动）

---

## 十、⤵️ 预留回填点

| # | 位置 | 内容 | 回填章节 |
|---|---|---|---|
| 1 | `policy_eval.go` 的 send_file 分支 | `SendFilePathGuardEvaluator` 未实现，`path_result` 恒 nil | **9.90e**（`send_file_path_guard.py` 315 行） |
| 2 | `policy_eval.go` 的 `_effective_permission_config` | `overlay_run_permissions`（`runtime/run_permissions.py`）未实现 | **9.90d**（`permission_compose.py`） |
| 3 | `permissions_layers.go` 的 `capture_permission_layers` | 依赖 `compose_host_effective_permissions` | **9.90d**（`permission_compose.py` 362 行） |
| 4 | `permissions_layers.go` 的 `persist_*_overlay_from_effective` | 同上 | **9.90d** |
| 5 | `tool_capabilities.go` 的 4 个零消费常量 | 待 9.90d 启用 | **9.90d** |
| 6 | `merge_file_guard_access_allows` 的完整语义 | 本批实现基础版，`/add-dir` 相关分支待 9.90e | **9.90e** |

---

## 十一、风险与开放问题

| # | 问题 | 影响 | 处置 |
|---|---|---|---|
| R1 | `_stable_override_id` 与 Python 产出的一致性 | 跨端读写 `user_permissions.yaml` 时 id 漂移 | 实现后必须与 Python 对照跑 |
| R2 | `portalocker` 的 Go 等价 | 并发写权限文件可能损坏 | Task 16 前先做技术选型小实验 |
| R3 | `ipaddress.is_global` 的手写等价 | 判错会导致内网 IP 被允许授权 | 按 CPython 源码逐条移植 + 单测覆盖全部保留段 |
| R4 | `json.dumps(ensure_ascii=True)` 的 Go 等价 | 字符串比较失配，`current_native_path_access` 恒 nil | `native_arguments_json` 必须手写 ASCII 转义 |
| R5 | `builtin_rules.yaml` 一次补 7 条 rules 会改变现有判定结果 | 之前放行的命令可能变 DENY | 需要评估现有集成测试是否受影响，必要时单独提交 |
| R6 | 删除 `checker.go` 同时改 `permission_engine.go`，单次改动量大 | 出问题难定位 | 分两个 commit：先加 fileguard 并切换，再删 checker |
| R7 | A 段加脱壳层会改变现有判定结果 | `bash -c "rm -rf /"` 之前可能放行，加脱壳后变 DENY | 这是**修正**而非回归；需评估现有集成测试是否依赖旧行为，若有则在测试里显式说明为何修正 |
| R8 | RE2 不支持 look-around，`_SH_LAUNCHER_RE` 需改写 | 改写不当会漏脱壳或误脱壳 | 必须用 Python 侧 `test_command_canonicalize.py`（300+ 行）的用例逐条对照 |
| R9 | `builtin_rules.yaml` 补齐后 7 条新规则进入 A 段 | 与 R5 叠加，判定结果变化面更大 | 单独一个 commit，便于二分定位 |
| **R10** | **`EvaluateTieredPolicy` 不再自行 `getBuiltinSecurityRules()`，改由配置切分 builtin 层** | **若有调用方未走 `PreparePermissionsForEngine`，内置规则会完全失效——所有 shell 危险命令规则、敏感路径全部静默丢失** | Task 3 必须 `grep` 盘点全部调用点逐个确认；测试里加「未经 prepare 的裸配置必须得到 fallback-ASK 而非 ALLOW」的断言兜底 |
| R11 | `canon_args` 双份评估使 A 段判定路径翻倍 | 性能敏感场景（大批并行工具调用）可能有开销 | 先测量；Python 也是双份评，行为一致性优先于性能 |
| R12 | `_allow_overrides_unless_denied` 与现有 `MatchedRuleUsesApprovalOverride` 的衔接 | override 放宽语义若接错，可能让 DENY 被放宽 | 该函数明确「DENY 直接返回不放宽」，Go 实现须先判 DENY |
| R13 | `interpreter_sink` 守卫（`curl x \| bash` → ASK）为 Python 独有 | Go 缺失期间管道注入解释器少一道防线 | 本批补齐；补齐后会新增 ASK，需评估现有集成测试是否受影响 |

---

## 十二、Task 0 核实结果（已完成）

### 12.1 M6：`command_canonicalize.py` 完全缺失 —— 结论升级为必补项

**核实结论**：Go 侧 `grep -rn "Canonicalize\|strip_fd_alias\|FD_ALIAS" internal/` **0 命中**，整个 134 行模块不存在。

**这不是可选前置件，是 A 段的既有偏差 + B 段的硬依赖**：

| 消费方 | Python 位置 | 用途 |
|---|---|---|
| A 段 `tool_policy.py:808` | `canon_cmd = canonicalize_shell_command_for_permission(original_cmd)` | 脱壳后再解析 shell AST 与匹配规则 |
| B 段 `path_extract.py:374` | `command = canonicalize_shell_command_for_permission(command)` | `extract_path_aware_command_accesses` 入口 |
| B 段 `path_extract.py:395` | 同上 | `extract_shell_path_accesses` 入口 |
| `approve/ask_presentation.py:137` | 9.90e | 展示用 |
| `approve/persist_rule_suggestions.py:13` | 9.90e | 建议生成用 |

Go 的 A 段现状（`tiered_policy.go:147`）：

```go
shellParse = ParseShellForPermission(commandText(toolArgs))
```

**直接用原始命令**，没有脱壳层。后果：`bash -c "rm -rf /"` 这类被 launcher 包裹的命令，锚定通配符规则（`rm *`）与 shell 正则前缀匹配不到内层命令，**A 段规则会被绕过**。

Python 的 `canonicalize_shell_command_for_permission` 语义（原注释）：

> Return a permission-view copy: unwrap one launcher layer.
> Recognised launchers: `cmd /c`, `pwsh`/`powershell -Command|-c` and POSIX shells (`bash`/`sh`/`zsh`/`dash`/`ash`) invoked with a `-c`-style switch. A wrapper left in the string makes anchored wildcard rules (`rm *`) and the shell regex prefixes miss the inner command, so the permission view has to look through it.
> Does not rewrite `tool_args` used for execution.

**三项必须逐字照抄的细节**：

1. **三条正则的顺序**：`_CMD_LAUNCHER_RE` → `_PS_LAUNCHER_RE` → `_SH_LAUNCHER_RE`，任一命中即返回，`count=1` 只脱一层
2. **`_launcher_inner` 的空格还原**：`if match.group(0)[:1] in (" ", "\t"): return " " + stripped`——`_SH_LAUNCHER_RE` 用 `\s*` 消耗了分隔符后的空白，不还原会粘成 `cd d &&rm x`。另两条正则锚定 word boundary 不会消耗前导空白，此分支对其为 no-op
3. **`_SH_LAUNCHER_RE` 的空白前缀拒绝**：纯空白前缀被有意排除（`echo bash -c 'rm'` 不得被当作 launcher 调用，否则无害 echo 串会命中 deny/ask 规则）。Python 用 `(?:^|(?<=[;&|()]))\s*` 显式交替实现，因为变长 look-behind 在 `re` 里不编译

**Go 实现约束**：

- 三条正则都用了 `(?ix)`（ignorecase + verbose）。Go `regexp` 支持 `(?i)`，**不支持 `(?x)`**——需手动去掉注释与空白
- `(?P<dq>...)` 命名捕获组 → Go 用 `(?P<dq>...)` 同样支持
- 变长 look-behind `(?<=[;&|()])` → **RE2 不支持 look-around**，必须改写。见下

**RE2 改写方案**（`_SH_LAUNCHER_RE`）：把 `(?:^|(?<=[;&|()]))\s*` 改为两条正则分别匹配（`^` 版与分隔符版），或改为把分隔符作为捕获组的一部分再在回调里保留。前者更简单，但会改变 `subn(count=1)` 的「首次匹配」语义。**建议**：用两条正则按顺序尝试，每条都 `count=1`，与 Python 的单正则语义在无重叠场景下等价；需在 spec 实现时用 Python 的 300+ 行测试用例对照验证。

**本批处置**：**新增 `security/toolguard/command_canonicalize.go`（134 行），为 Task 3a**。这是 A 段修正 + B 段前置依赖，不能留到 9.90e。

**由此带来的 A 段修正**：`tiered_policy.go` 需要加脱壳层。Python `tool_policy.py` 的对应片段：

```python
original_cmd = _command_text(tool_args)
canon_cmd = canonicalize_shell_command_for_permission(original_cmd)
canon_args = (
    _with_shell_command(tool_args, canon_cmd)
    if canon_cmd != original_cmd
    else tool_args
)
shell_parse = parse_shell_for_permission(canon_cmd) if _tool_category(...) == "shell" else None
```

Go 现状（`tiered_policy.go:147`）缺 `canon_cmd` / `canon_args` 两项。本批一并补，属 §二 2.2 旧逻辑清单的补充项。

### 12.2 `host.py` 字段对照

| Python `ToolPermissionHost` 字段 | Go `ToolPermissionHost` | 判定 |
|---|---|---|
| `get_permissions_snapshot` | `GetPermissionsSnapshot func() map[string]any` | ⚠️ **签名退化**：Python 为 `Callable[..., dict]`（可接受 `session_id` 关键字），Go 无参 |
| `persist_allow_rule` | `PersistAllowRule func(map[string]any) bool` | ⚠️ 同上，Python 可接受 `session_id` |
| `persist_session_allow_rule` | **缺失** | 🔴 **Go 无此字段** |
| `resolve_workspace_dir` | `ResolveWorkspaceDir func() string` | ✅ |
| `permission_yaml_path` | `PermissionYAMLPath string` | ✅ |
| `tool_permission_checks_active` | `ToolPermissionChecksActive func() bool` | ✅ |
| `request_permission_confirmation` | `RequestPermissionConfirmation` | ✅ |
| `permission_scene_hook` | `PermissionSceneHook` | ✅ |

**新增缺失项 M12**：`persist_session_allow_rule` 缺失，影响 `_persist_session_allow`（`tool_security_rail.py:315`）。Python 原文注释：

> 产品层应写入该 `session_id` 的 Session 文件。护栏会在回调接受 `session_id` 关键字时传入 `ctx.session` 解析出的 id——中断恢复时 ContextVar 可能为空，不能只靠请求上下文。

Go 侧 `persistAllowAlways` 只对应 `_persist_allow_always`，**没有 `persistSessionAllow`**。这是 9.90c 之后遗留的缺口。

**处置**：`persist_session_allow_rule` 与 `_persist_session_allow` / `_persist_merged_allow` 的重构关联到 §十 的 9.90e 回填点，但 `_persist_merged_allow` 的抽层是本批 Task 11 的一部分（因为 `_persist_allow_always` 与 `_persist_session_allow` 共用它）。本批需：

1. 加 `PersistSessionAllowRule` 字段（带 `sessionID` 参数）
2. 把 `persistAllowAlways` 重构为 `persistMergedAllow(...)` + 两个薄封装
3. `GetPermissionsSnapshot` / `PersistAllowRule` 的 `session_id` 变参问题记为偏差 D9

### 12.3 `acp_tool_updates.go` 别名表来源

**核实结论**：Go 的 `internal/swarm/e2a/acp/acp_tool_updates.go:28` 别名表 4 项与 `tool_security_rail.go:73` 完全一致（`free_search` / `paid_search` / `fetch_webpage` / `exec_command`），注释写 `Python _TOOL_NAME_ALIASES`。

Python 侧对应位于 ACP 扩展的 tool updates 模块，与 `tool_security_rail.py` 的 `TOOL_NAME_ALIASES`（5 项，多 `web_fetch_webpage`）**是两份独立数据**。

**处置**（用户拍板「各自修各自的」）：

- `permission_tools`（新建）：4 项，按 Python `PERMISSION_TOOL_ALIASES` 照抄
- `tool_security_rail.go:73`：**补 `web_fetch_webpage`**（5 项，对齐 Python `TOOL_NAME_ALIASES`）
- `acp_tool_updates.go:28`：本批**不动**——需先确认 Python ACP 侧那一份是否也只有 4 项。若不确认就补齐，会引入无依据的偏差

**新增待办**：确认 Python ACP tool updates 模块的别名表内容。这属 10.x ACP 领域，不在本批范围，记为跨批待办而非本批缺失项。

### 12.4 范围变化（含 §12.5 六项 A 段缺陷修正）

| 项 | 原估算 | 现估算 |
|---|---|---|
| `security/toolguard/command_canonicalize.go` | 0 | +134 行实现 + ~150 行测试 |
| `tiered_policy.go` 脱壳层 + 六项 A 段对齐（a-f） | 0 | **~+200 行改**（原估 15 行；含 `shell_guard` 配置解析） |
| `host.go` 加 `PersistSessionAllowRule` + `persistMergedAllow` 重构 | 0 | ~+120 行改 |
| `builtin_rules.yaml` 补齐 + `resources` 扩字段 | 0 | ~+30 行 Go + 162 行 YAML |
| **本批总量** | 约 6,150 行 | **约 6,600 行实现 + 约 4,500 行测试** |

### 12.5 §十二 三项新发现的处置（用户已拍板：全部本批修复）

用户 2026-11-08 明确：**§十二 的 ①②③ 三项全部在本批一起修复，不挂 ⤵️**。三项的落地要求如下，实施时按此执行，不得再改为延后。

#### 12.5.1 ① command_canonicalize —— 本批完整实现 + A/B 两处接线

**范围**：

- 新增 `security/toolguard/command_canonicalize.go`（134 行，含 3 条正则 + 4 个函数）
- `tiered_policy.go` 加脱壳层（A 段）
- `path_extract.go` 的两处调用点（B 段，随 Task 5 自然引入）

**⚠️ 核实纠正：`canon_args` 不是「替换」，是「原始与脱壳两份并存」**

初步判断以为 A 段只需把 `commandText(toolArgs)` 换成脱壳后的命令。**实际读 Python `tool_policy.py:807-892` 后确认语义复杂得多**，必须逐段照抄：

```python
original_cmd = _command_text(tool_args)
canon_cmd = canonicalize_shell_command_for_permission(original_cmd)
canon_args = _with_shell_command(tool_args, canon_cmd) if canon_cmd != original_cmd else tool_args

shell_parse = parse_shell_for_permission(canon_cmd)      # AST 用脱壳后的
shell_floor, shell_floor_rule = _shell_ast_floor(shell_parse, unknown_structure=check_unknown)

# ── override 命中：先查原始，空了再查脱壳 ──
override_hits = _collect_approval_override_hits(approval_overrides, tool_name, tool_args, cfg)
if not override_hits and canon_args is not tool_args:
    override_hits = _collect_approval_override_hits(approval_overrides, tool_name, canon_args, cfg)
```

**shell simple 分支（L843-880）**——三份结果取最严：

```python
# 1. 子命令逐个评估（用 subcommand.text，不是 canon）
for subcommand in shell_parse.subcommands:
    sub_args = _with_shell_command(tool_args, subcommand.text)     # 注意：基于原始 tool_args
    sub_permission, sub_rule = _evaluate_single_invocation(tool_name, sub_args, ctx)
permission, matched_rule = _aggregate_subcommand_results(subcommand_results)

# 2. 整串参数规则：原始 + 脱壳都评，取最严
full_candidates = []
orig_full = _evaluate_param_rules_only(tool_name, tool_args, ctx)     # 原始
if orig_full: full_candidates.append(orig_full)
if canon_args is not tool_args:
    canon_full = _evaluate_param_rules_only(tool_name, canon_args, ctx)  # 脱壳
    if canon_full: full_candidates.append(canon_full)
if full_candidates:
    full_level = strictest(*(lvl for lvl, _ in full_candidates))
    full_rule = next(rule for lvl, rule in full_candidates if lvl == full_level)
    permission = strictest(permission, full_level)
    if permission == full_level:
        matched_rule = full_rule        # 整串更严时串号也替换

# 3. interpreter sink（管道末段是解释器）
if check_sink and _has_interpreter_sink(shell_parse):
    permission = strictest(permission, ASK)
    if permission == ASK:
        sink_rule = f"{_MR}:shell_guard:interpreter_sink"
        matched_rule = f"{sink_rule}|{matched_rule}" if matched_rule else sink_rule

return _allow_overrides_unless_denied(*_apply_shell_ast_floor(...), override_hits)
```

**非 shell simple 分支（L884-891）**——只认一种 args：

```python
eval_args = canon_args if _tool_category(tool_name, cfg) == "shell" else tool_args
result = _evaluate_single_invocation(tool_name, eval_args, ctx)
return _allow_overrides_unless_denied(*_apply_shell_ast_floor(*result, ...), override_hits)
```

**四个必须照抄的细节**：

| # | 细节 | 说明 |
|---|---|---|
| 1 | `canon_args is not tool_args` 用**对象身份**判断，不是值相等 | `canon_cmd != original_cmd` 时才构造新 dict；Go 需显式 `different bool` 变量 |
| 2 | shell simple 分支的子命令用**原始 `tool_args`** 做 `_with_shell_command` 基底 | 不是 `canon_args`。子命令文本本身已来自脱壳后的 AST |
| 3 | 整串参数规则**原始与脱壳都评**，取最严 | 这是「双向覆盖」：脱壳能抓到被包裹的内层命令，原始能抓到 launcher 本身 |
| 4 | `_allow_overrides_unless_denied` 最后一道 | override 可放宽 ASK，不可放宽 DENY；命中时 `matched_rule` 换成 `approval_overrides:<hits>` |

**由此发现的 Go 既有缺陷（A 段，已逐项核实）**：

| # | Go 现状（`tiered_policy.go`） | Python（`tool_policy.py`） | 影响 | 核实 |
|---|---|---|---|---|
| a | `builtinRules := getBuiltinSecurityRules()`（:151） | `builtin_rules = [r for r in dict_rules if r.get("layer")=="builtin"]`（L795） | **架构层次错误**：把 ingest 职责下沉到判定阶段。Python docstring L781 明写「判定路径不再 load YAML」 | ✅ 已核 |
| b | 无 `canon_cmd` / `canon_args`（:147 直接用 `commandText(toolArgs)` 解析 AST） | L807-813 有，双份并存 | launcher 包裹的命令绕过锚定规则 | ✅ 已核 |
| c | `overrideHits` 只有单轮（:683） | L827-832 两轮：先原始，空了再查脱壳 | override 命中漏判 | ✅ 已核 |
| d | 无 `_evaluate_param_rules_only` | L685-735 有，用于子命令聚合后整串复核 | 子命令聚合后不做整串复核 | ✅ 已核 |
| e | 无 `_has_interpreter_sink`；**`shell_guard` 配置整个没读**（`grep shell_guard` 在 security 包 0 命中，`unknown_structure` 也没有） | L805 `_read_shell_guard` + L871 `_has_interpreter_sink` | ① `curl x \| bash` 少一道 ASK ② `shell_guard.unknown_structure=false` 的宿主开关无效 | ✅ 已核 |
| f | 无外层 `_allow_overrides_unless_denied` | L880/L890 在聚合后套一次 | 子命令聚合出的 ASK/`matched_rule` 与 Python 不一致 | ✅ 已核 |

**这六项使 Task 3a 的工作量从「新增 134 行 + 接线 15 行」涨到「新增 134 行 + A 段重构约 200 行」**（(e) 还额外包含 `shell_guard` 配置解析），总量相应上调（见 §12.4）。

**关于 (a) 的处置**（承 §12.5.2/3 的双机制问题）：Go `EvaluateTieredPolicy` 必须改为**只从配置切分 builtin/user**，不再自行 `getBuiltinSecurityRules()`。这要求所有调用方都先经 `PreparePermissionsForEngine`。风险在于现有调用方可能直接构造 `permissionConfig` 而不走 prepare——需在 Task 3 里盘点全部 `EvaluateTieredPolicy` 调用点，逐个确认。

**验收要求**：

- 用 Python `tests/unit_tests/harness/security/test_command_canonicalize.py`（300+ 行）用例逐条对照，覆盖三条 launcher 分支
- 覆盖 `_launcher_inner` 的空格还原（`cd d && bash -c "rm x"` → `cd d && rm x`）
- 覆盖纯空白前缀拒绝（`echo bash -c 'rm'` 不得脱壳）
- 覆盖「原始 vs 脱壳取最严」：`mv x 'bash -c "rm y"'` 这类两者命中不同规则的情形
- 端到端：`bash -c "rm -rf /"` 必须命中 `shell_fs_recursive_or_forced_delete` 的 DENY

#### 12.5.2 ② builtin_rules.yaml 与 resources 结构体 —— 本批完整补齐

**范围**：

- `resources/builtin_rules.yaml` 重写为 246 行（对齐 Python）
  - `rules` 段：10 条 → **17 条**（补 7 条）
  - 17 条全部带 `action` 字段（Go 现仅 1 条有）
  - 新增 `sensitive_paths` 段（17 条，94 行）
  - 新增 `net_urls` 段（4 条）
- `resources.BuiltinRules` 扩两个字段：

```go
type BuiltinRules struct {
    Rules          []SecurityRule     `yaml:"rules"`
    SensitivePaths []map[string]any   `yaml:"sensitive_paths"`
    NetUrls        map[string]any     `yaml:"net_urls"`
}
```

**为什么 `SensitivePaths` 用 `[]map[string]any` 而不是 `[]SensitivePathRule`**：Python 是 `list[dict[str, Any]]` 全保留，YAML 每条含 `id` / `path` / `match` / `severity` / `action` 五个键，但 `sensitive_paths.py` 的 `load_package_sensitive_paths` 会往条目里**追加** `read`/`write`/`exec`/`layer` 四个键。若用封闭结构体，`merge_sensitive_paths` 的 overlay 与 floor 合并逻辑（需要任意键的浅拷贝）无法照抄。用 `map[string]any` 保住全保真。

**`rules[].action` 的双机制问题**：Go 现状依赖 YAML 里写死的 `action`，Python 依赖 `inline_package_command_rules` 在 ingest 阶段注入。本批 Task 3 补齐 `inline_package_command_rules` 后，**两条路径都会存在**——需确认 `GetBuiltinSecurityRules()` 与 `InlinePackageCommandRules()` 的职责边界，不能让 `action` 被赋值两次或覆盖用户规则。处置见 §12.5.3。

#### 12.5.3 ③ host.persist_session_allow_rule + _persist_merged_allow —— 本批完整补齐

**范围**：

1. `ToolPermissionHost` 加字段：

```go
// PersistSessionAllowRule 自定义「会话内记住」写盘
PersistSessionAllowRule func(permissions map[string]any, sessionID string) bool
```

2. `tool_security_rail.go` 的 `persistAllowAlways` 重构为三层，对齐 Python `tool_security_rail.py:299-388`：

```
_persist_allow_always      → persistAllowAlways      （薄封装）
_persist_session_allow     → persistSessionAllow     （薄封装，Go 新增）
_persist_merged_allow      → persistMergedAllow      （共用实现，Go 新增）
```

3. `persistMergedAllow` 的步骤（**顺序不可调换**）：

```
base_cfg = host.get_permissions_snapshot() 经 _invoke_permissions_hook 调用
   → 失败或非 dict → deepcopy(engine.config)
cfg, ok_tool = merge_permission_allow_rule_into_permissions(base_cfg, name, args)
accesses = _collect_file_guard_persist_accesses(name, args, cfg)
if accesses:
    cfg, ok_ext = merge_file_guard_access_allows(cfg, accesses)
if not ok_tool and not ok_ext: return False
prev_cfg = deepcopy(engine.config)
update_config(cfg)
if persist_hook is not None:
    persisted = _invoke_permissions_hook(persist_hook, cfg, session_id=?)
    if not persisted: update_config(prev_cfg)
    return persisted
if not write_yaml_fallback: return True
if not write_permissions_section_to_agent_config_yaml(...):
    update_config(prev_cfg); return False
return True
```

**四个易漏点**：

- `base_cfg` 先试 snapshot 再回落 `engine.config`——Go 现状直接 `DeepCopyMap(r.engine.Config())`，缺 snapshot 尝试
- `prev_cfg` 必须在 `update_config(cfg)` **之前**取
- `write_yaml_fallback=false` 时（session 版）**只更新内存不写 YAML，返回 true**
- `_invoke_permissions_hook` 的 `session_id` 传递：Python 用 `inspect` 判断回调是否接受该关键字；Go 用 D9 的变参方案

4. **内置规则来源的双机制收敛**（承 §12.5.2，此处为最终结论）

**核实纠正**：我先前写的「`EvaluateTieredPolicy` 收到含 builtin 层的配置时不再叠加 `GetBuiltinSecurityRules()`」是**错的**。Python 根本没有这个判断，而是**完全不 load YAML**：

```python
# tool_policy.py:791-796
rules = permission_config.get("rules") or []
dict_rules = [r for r in rules if isinstance(r, dict)]
builtin_rules = [r for r in dict_rules if r.get("layer") == "builtin"]
user_rules    = [r for r in dict_rules if r.get("layer") != "builtin"]
```

docstring 第 781 行明写：

> 内置规则必须已内联进 `rules`（`layer: builtin`）；判定路径不再 load YAML。

**Go 的正确改法**：

```go
// 删除 tiered_policy.go:150 的 builtinRules := getBuiltinSecurityRules()
rules := listAnyField(permissionConfig, "rules")
builtinRules, userRules := splitByBuiltinLayer(rules)   // 新增，对齐 Python L795-796
```

`GetBuiltinSecurityRules()` **保留**（作为 `PreparePermissionsForEngine` 的 YAML 读取入口），但**不再被 `EvaluateTieredPolicy` 直接调用**。

**由此产生的前置条件**：所有 `EvaluateTieredPolicy` 调用方必须先经 `PreparePermissionsForEngine` 注入 builtin 层，否则内置规则完全失效。Task 3 必须：

1. `grep -rn "EvaluateTieredPolicy("` 盘点全部调用点
2. 逐个确认其 `permissionConfig` 来源是否已 prepare
3. 未 prepare 的调用点补 `PreparePermissionsForEngine`，或在测试里显式构造含 builtin 层的配置

**这是本批风险最高的一处改动**（见 R10）。若盘点发现有调用方拿不到 prepare（例如直接构造配置的测试），则需在这些点显式调用 `InlinePackageCommandRules`，否则行为回退为「无内置规则」。

---

## 十三、更新后的缺失项汇总

| ID | 缺失项 | 本批决策 |
|---|---|---|
| M1 | `fileguard/` 4 文件 1,650 行 | 本批补齐 |
| M2 | `netguard/` 2 文件 315 行 | 本批补齐 |
| M3 | `toolguard/builtin_rules.py` 151 行 | 本批补齐 |
| M4 | `builtin_rules.yaml` 缺 7 rules + 16 action + 2 段 | 本批补齐 |
| M5 | `resources.BuiltinRules` 缺两字段 | 本批扩展 |
| **M6** | **`toolguard/command_canonicalize.py` 134 行** | **本批补齐（新增 Task 3a）** |
| M7 | `common/permission_tools.py` 105 行 | 本批新建 |
| M8 | `PermissionEngine.UpdateTrustedDirs` / `TrustedDirs()` | 本批补齐 |
| M9 | `PermissionInterruptRail.SetTrustedDirs` | 本批补齐 |
| M10 | `merge_file_guard_access_allows` | 本批补齐 |
| M11 | `host.set_trusted_dirs` 链路 | 并入 M9 |
| **M12** | **`host.persist_session_allow_rule` + `_persist_merged_allow` 抽层** | **本批补齐** |

---

## 十四、更新后的任务清单

在 §八 基础上调整：

- **Task 3a（新增）**：`security/toolguard/command_canonicalize.go`（134 行）+ 测试；同时给 `tiered_policy.go` 加脱壳层
- **Task 11 扩充**：加 `PersistSessionAllowRule` 字段、`persistMergedAllow` 抽层、`persistSessionAllow` 薄封装、`SetTrustedDirs`
- **Task 9 依赖**：需 Task 3a（A 段脱壳）

依赖链更新：

```
Task 0（已完成）
  ├─ Task 1（permission_tools）────┐
  ├─ Task 2（resources + YAML）──┐ │
  ├─ Task 3（builtin_rules）─────┤ │
  ├─ Task 3a（command_canonicalize）─┐ │
  ├─ Task 4（file_tool_specs）───┤ │ │
  ├─ Task 5（path_extract）←4, 3a┤ │ │
  ├─ Task 6（file_guard）←4,5    │ │ │
  ├─ Task 7（sensitive_paths）←2 │ │ │
  ├─ Task 8（netguard）←2 ───────┤ │ │
  ├─ Task 9（engine 重写）←3,3a,6,8 ┤
  ├─ Task 10（merge_file_guard_access_allows）←6
  ├─ Task 11（rail 改造）←9,10
  ├─ Task 12（4 个无依赖文件）
  ├─ Task 13（tool_capabilities）←1
  ├─ Task 14（tool_decision_facts）←5,13
  ├─ Task 15（policy_eval）←9,⤵️9.90e
  ├─ Task 16（permissions_layers）←16a 确认结果
  └─ Task 17（doc.go + 计划表）
```
