# SysOpBuilder 完整实现设计

> 目标：完整实现 10.3.7-11 中 SysOpBuilder 组件，包含 Card 构建、FilesystemPolicy 组装、SysOperation 实例化注册、展示辅助。
> 一比一对齐 Python `jiuwenswarm/server/runtime/agent_adapter/sysop_builder.py`（~1100 行）。

---

## 1. 组件定位与流程位置

```
DeepAdapter.CreateInstance
  └── 步骤 17: createSysOperation(configBase)
        ├── resolveOperationMode(configBase) → Local / Sandbox
        ├── if Sandbox → CreateSandboxSysOpCard(...)
        │     └── BuildFilesystemPolicy(filesRuntime, projectDir, isCodeAgent)
        │           ├── collectIntrinsicTargets() → 5 个 rw 文件 + daily_memory + config.yaml(ro)
        │           ├── resolveAgentSkillsDir()
        │           ├── resolveProjectDir(projectDir) [仅 isCodeAgent]
        │           └── 处理 files.allow / files.deny
        ├── if Local → CreateLocalSysOpCard()
        └── CreateSysOperationFromCard(card)
              ├── sysop.NewSysOperation(card) → LocalSysOperation
              ├── 隔离键复用检查 → GetSysOperationByIsolationKey
              └── ResourceMgr.AddSysOperation(card.ID, instance) + 自动注册工具
```

**作用**：SysOpBuilder 是 Agent 实例化的最后一步（CreateInstance 步骤 17），决定 Agent 的操作环境——本地直接执行还是沙箱隔离执行。同时提供沙箱路径的展示/查询辅助（`/sandbox status`、`/sandbox files list`）。

---

## 2. 文件组织

```
internal/swarm/server/sysop_builder/
├── doc.go          # 包文档
├── builder.go      # Card 构建 + SysOperation 实例化 + resolveOperationMode
├── policy.go       # BuildFilesystemPolicy + 6 个非导出辅助函数
└── display.go      # ListAutoManagedSandboxPaths + ListEffectiveSandboxFiles + FindAutoManagedMatch + 3 个非导出辅助
```

非导出辅助函数包内共享：`collectIntrinsicTargets`、`resolveProjectDir`、`normalizeFSEntry` 等被 policy.go 和 display.go 共同使用。

---

## 3. 函数签名（一比一对齐 Python）

### 3.1 builder.go — Card 构建 + 实例化

```go
// ──────────────────────────── 常量 ────────────────────────────
// preserveFileSharingMode 固有文件共享模式，当前只支持 "mount"。
// 对齐 Python: PreserveFileSharingMode = Literal["mount"]
const preserveFileSharingMode = "mount"

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateLocalSysOpCard 创建本地模式 SysOperationCard。
// 对齐 Python: create_local_sysop_card() → SysOperationCard(mode=LOCAL, work_config=LocalWorkConfig(shell_allowlist=None))
func CreateLocalSysOpCard() *sysop.SysOperationCard

// CreateSandboxSysOpCard 创建沙箱模式 SysOperationCard。
// 对齐 Python: create_sandbox_sysop_card(sandbox_url, sandbox_type, *, files_runtime, excluded_commands,
//              idle_ttl_seconds, idle_check_interval, project_dir, is_code_agent)
// 失败时返回 nil（Python 返回 None），异常被捕获记 warning。
func CreateSandboxSysOpCard(
    sandboxURL string,
    sandboxType string,
    filesRuntime map[string]any,
    excludedCommands []string,
    idleTTLSecs *int,
    idleCheckInterval *int,
    projectDir string,
    isCodeAgent bool,
) *sysop.SysOperationCard

// CreateSysOperationFromCard 从 SysOperationCard 创建 SysOperation 实例并注册到 ResourceMgr。
// 对齐 Python: Runner.resource_mgr.add_sys_operation(card)
// 包含隔离键复用检查、注册、并发重试逻辑。
func CreateSysOperationFromCard(card *sysop.SysOperationCard) sysop.SysOperation

// ResolveOperationMode 从配置解析操作模式。
// 对齐 Python: _resolve_operation_mode(config_base) — 从 configBase["sys_operation"]["mode"] 解析
func ResolveOperationMode(configBase map[string]any) sysop.OperationMode

// ──────────────────────────── 非导出函数 ────────────────────────────

// getRegisteredSysOpByIsolationKey 按隔离键模板查找已注册的 SysOperation。
// 对齐 Python: _get_registered_sys_operation_by_isolation_key()
func getRegisteredSysOpByIsolationKey(key string) sysop.SysOperation
```

### 3.2 policy.go — FilesystemPolicy 组装

```go
// ──────────────────────────── 导出函数 ────────────────────────────

// BuildFilesystemPolicy 组装沙箱 filesystem policy。
// 对齐 Python: build_filesystem_policy(files_runtime, *, project_dir=None, is_code_agent=False)
//   → (policy_dict, upload_list)
//
// 参数：
//   - filesRuntime: config.yaml::sandbox.files 字典，含 allow/deny 列表
//   - projectDir: 工程目录覆盖值（仅 isCodeAgent 时消费）
//   - isCodeAgent: 是否 code-agent 形态
//
// 返回：
//   - policyDict: {"filesystem_policy": {"files": [], "directories": [], "bind_mounts": [], "read_write": [], "read_only": []}}
//   - uploadList: 当前始终为空列表（mount 模式下走 bind_mounts）
//   - error: files.allow/deny 中 path 不存在时返回 os.ErrNotExist（对齐 Python FileNotFoundError）
func BuildFilesystemPolicy(
    filesRuntime map[string]any,
    projectDir string,
    isCodeAgent bool,
) (policyDict map[string]any, uploadList []map[string]string, err error)

// ──────────────────────────── 非导出函数 ────────────────────────────

// collectIntrinsicTargets 收集 deep agent 固有路径，分 rw/ro 两类返回。
// 对齐 Python: _collect_intrinsic_targets() → (rw_files, rw_dirs, ro_files)
func collectIntrinsicTargets() (rwFiles, rwDirs, roFiles []string)

// resolveProjectDir 解析挂入沙箱的主写入根目录。
// 对齐 Python: _resolve_project_dir(override)
// 优先级：override → UAPCLAW_SANDBOX_PROJECT_DIR 环境变量 → os.Getwd()
// 拒绝挂载文件系统根 "/"。
func resolveProjectDir(override string) string

// resolveAgentSkillsDir 解析内置技能目录。
// 对齐 Python: _resolve_agent_skills_dir()
func resolveAgentSkillsDir() string

// ensureIntrinsicFile 确保固有文件存在，不存在则 touch 空文件。
// 对齐 Python: _ensure_intrinsic_file(path) → bool
func ensureIntrinsicFile(path string) bool

// normalizeFSEntry 归一化 {path, permissions} 项，接受 string 或 map[string]any。
// 对齐 Python: _normalize_fs_entry(entry, default_permissions)
func normalizeFSEntry(entry any, defaultPermissions string) map[string]any
```

### 3.3 display.go — 展示辅助

```go
// ──────────────────────────── 导出函数 ────────────────────────────

// ListAutoManagedSandboxPaths 列出自动管理的沙箱路径。
// 对齐 Python: list_auto_managed_sandbox_paths(project_dir, *, is_code_agent)
// 返回 {"allow_write": [...], "deny_write": [...]}，每项为 {"path": str, "permissions": str, "kind": str}
func ListAutoManagedSandboxPaths(
    projectDir string,
    isCodeAgent bool,
) map[string][]map[string]string

// ListEffectiveSandboxFiles 只读视图：auto + 用户条目合并。
// 对齐 Python: list_effective_sandbox_files(files_runtime, *, project_dir, is_code_agent)
func ListEffectiveSandboxFiles(
    filesRuntime map[string]any,
    projectDir string,
    isCodeAgent bool,
) map[string][]map[string]string

// FindAutoManagedMatch 判断路径是否已被 auto 管理。
// 对齐 Python: find_auto_managed_match(path, *, project_dir, is_code_agent)
// 返回 (bucket, canonicalPath, found)，bucket 为 "allow_write" 或 "deny_write"
func FindAutoManagedMatch(
    path string,
    projectDir string,
    isCodeAgent bool,
) (bucket, canonicalPath string, found bool)

// ──────────────────────────── 非导出函数 ────────────────────────────

// appendUnique 去重追加（按 path 比较）。
// 对齐 Python: _append_unique(target, entry)
func appendUnique(target []map[string]string, entry map[string]string) []map[string]string

// classifyHostKind 判定 path 是 "directory" 还是 "file"。
// 对齐 Python: _classify_host_kind(path)
func classifyHostKind(path string) string

// resolveDisplayPath 解析为绝对路径用于展示/比较。
// 对齐 Python: _resolve_display_path(raw)
func resolveDisplayPath(raw string) string
```

---

## 4. BuildFilesystemPolicy 核心逻辑

对齐 Python `build_filesystem_policy`（L350-648），完整实现以下流程：

```
1. filesRuntime = filesRuntime or {}
2. 初始化容器：allow_files, allow_dirs, bind_mounts, upload_list, writable_paths,
   read_write_promote, read_only_promote
3. 定义闭包：
   - recordRWBind(host, sandbox, isDir, permissions) → bind_mounts + writable_paths
   - recordUserDenyBind(host, sandbox) → bind_mounts(mode=rw) + read_only_promote
   - recordROResourceBind(host, sandbox) → bind_mounts(mode=ro) + read_only_promote
4. 收集 intrinsic 目标：
   rw_files, rw_dirs, ro_files = collectIntrinsicTargets()
5. 注册 rw_files → recordRWBind
6. 注册 rw_dirs → recordRWBind
7. 注册 ro_files → recordROResourceBind
8. agent_skills → resolveAgentSkillsDir() → recordRWBind
9. [仅 isCodeAgent] project_dir → resolveProjectDir(projectDir) → recordRWBind
10. 遍历 files_runtime["allow"] → normalizeFSEntry → 校验存在 → recordRWBind + read_write_promote
11. 遍历 files_runtime["deny"] → normalizeFSEntry → 校验存在 → recordUserDenyBind
12. 组装 fs_policy dict + 可选字段 (bind_mounts / read_write / read_only)
13. 返回 {"filesystem_policy": fs_policy}, uploadList, nil
```

### 4.1 闭包实现

Go 没有闭包修改外层变量的习惯，改用结构体+方法：

```go
// policyBuilder filesystem policy 构建器，封装可变状态。
type policyBuilder struct {
    allowFiles       []map[string]any
    allowDirs        []map[string]any
    bindMounts       []map[string]any
    uploadList       []map[string]string
    writablePaths    []string
    readWritePromote []string
    readOnlyPromote  []string
}

func (b *policyBuilder) recordRWBind(hostPath, sandboxPath string, isDir bool, permissions string)
func (b *policyBuilder) recordUserDenyBind(hostPath, sandboxPath string)
func (b *policyBuilder) recordROResourceBind(hostPath, sandboxPath string)
func (b *policyBuilder) build() (map[string]any, []map[string]string)
```

### 4.2 固有路径函数映射

对齐 Python `_INTRINSIC_RW_FILE_PATH_FUNCS` 和 `_INTRINSIC_RO_FILE_PATH_FUNCS`：

```go
// intrinsicRWFilePathFuncs 固有 rw 文件路径函数列表。
// 对齐 Python: _INTRINSIC_RW_FILE_PATH_FUNCS
var intrinsicRWFilePathFuncs = []func() string{
    workspace.DeepAgentAgentMDPath,
    workspace.DeepAgentHeartbeatPath,
    workspace.DeepAgentIdentityMDPath,
    workspace.DeepAgentSoulMDPath,
    workspace.DeepAgentUserMDPath,
}

// intrinsicROFilePathFuncs 固有 ro 文件路径函数列表。
// 对齐 Python: _INTRINSIC_RO_FILE_PATH_FUNCS
var intrinsicROFilePathFuncs = []func() string{
    workspace.ConfigFile,
}
```

---

## 5. CreateSandboxSysOpCard 完整逻辑

对齐 Python `create_sandbox_sysop_card`（L651-767）：

```
1. policy, uploadList, err := BuildFilesystemPolicy(filesRuntime, projectDir, isCodeAgent)
   if err != nil { logger.Warn(...); return nil }
2. 构建 extraParams:
   - "policy": policy
   - "policy_mode": "append"
   - "excluded_commands": excludedCommands or []
   - "preserve_file_sharing_mode": "mount"
   - "preserve_files_upload": uploadList
   - [可选] "idle_check_interval": idleCheckInterval
3. 构建 SandboxGatewayConfig:
   - Isolation: SandboxIsolationConfig{ContainerScope: System}
   - LauncherConfig: PreDeployLauncherConfig{
       BaseURL: sandboxURL,
       SandboxType: sandboxType,
       IdleTTLSeconds: idleTTLSecs,
       ExtraParams: extraParams,
     }
4. 构建 SysOperationCard:
   - Mode: OperationModeSandbox
   - WorkConfig: NewLocalWorkConfig()（shell_allowlist=nil）
   - GatewayConfig: 上述 config
5. 日志输出完整 policy 详情（对齐 Python L730-763）
6. return card
```

---

## 6. CreateSysOperationFromCard 实例化流程

对齐 Python `_create_sys_operation`（L1762-1833）：

```
1. if card == nil { return nil }
2. instance, err := sysop.NewSysOperation(card)
   if err != nil { logger.Warn; return nil }
3. isolationKey := instance.IsolationKeyTemplate()
4. if isolationKey != "" {
     existing := getRegisteredSysOpByIsolationKey(isolationKey)
     if existing != nil { logger.Info("复用"); return existing }
   }
5. rm := runner.GetResourceMgr()
   if rm != nil {
     if addErr := rm.AddSysOperation(card.ID, instance); addErr != nil {
       // 注册失败，再查一次（并发场景）
       existing := getRegisteredSysOpByIsolationKey(isolationKey)
       if existing != nil { return existing }
       logger.Warn("注册失败"); return nil
     }
     logger.Info("已注册到资源管理器")
   }
6. return instance
```

### 6.1 需新增的 SysOperationMgr 方法

```go
// GetSysOperationByIsolationKey 按隔离键模板查找已注册的 SysOperation。
// 对齐 Python: SysOperationMgr._sandbox_key_owner_map[key] → get_sys_operation(op_id)
func (m *SysOperationMgr) GetSysOperationByIsolationKey(key string) sysop.SysOperation
```

实现逻辑：读锁 → `sandboxKeyOwnerMap[key]` → `sysOperations.Get(opID)` → 返回。

---

## 7. DeepAdapter 回填

### 7.1 createSysOperation 签名变更

当前：
```go
func (d *DeepAdapter) createSysOperation(configBase map[string]any) (sysop.SysOperation, *sysop.SysOperationCard)
```

回填后完整实现：
```go
func (d *DeepAdapter) createSysOperation(configBase map[string]any) (sysop.SysOperation, *sysop.SysOperationCard) {
    mode := sysop_builder.ResolveOperationMode(configBase)

    var card *sysop.SysOperationCard
    switch mode {
    case sysop.OperationModeSandbox:
        sandboxURL, sandboxType, runtime := d.getSandboxRuntime(configBase)
        card = sysop_builder.CreateSandboxSysOpCard(
            sandboxURL, sandboxType,
            runtime["files"].(map[string]any),
            getStrSlice(runtime, "excluded_commands"),
            getIntPtr(runtime, "idle_ttl_seconds"),
            getIntPtr(runtime, "idle_check_interval"),
            d.resolveProjectDirForSandbox(),
            d.isCodeAgent,
        )
    default:
        card = sysop_builder.CreateLocalSysOpCard()
    }

    instance := sysop_builder.CreateSysOperationFromCard(card)
    d.sysOperation = instance
    d.sysOperationCard = card
    return instance, card
}
```

### 7.2 新增辅助方法

```go
// resolveProjectDirForSandbox 解析沙箱挂载用的项目目录。
// 对齐 Python: _resolve_project_dir_for_sandbox()
func (d *DeepAdapter) resolveProjectDirForSandbox() string

// getSandboxRuntime 从配置获取沙箱运行时参数。
// 对齐 Python: get_sandbox_runtime() + get_config()["sandbox"]
func (d *DeepAdapter) getSandboxRuntime(configBase map[string]any) (url, typ string, runtime map[string]any)
```

### 7.3 CreateInstance 步骤 17 回填

```go
// 步骤 17: sys_operation = _create_sys_operation()
sysOp, _ := d.createSysOperation(configBase)
if sysOp != nil {
    params.SysOperation = sysOp
}
```

---

## 8. CodeAdapter 回填

CodeAdapter 继承 DeepAdapter，步骤 17 同样需要回填。Python 中 `JiuwenClawCodeAdapter._is_code_agent = True`，Go 中 `CodeAdapter.isCodeAgent` 已设为 `true`。

CodeAdapter 的 `CreateInstance` 中步骤 17 当前：
```go
// 步骤 17: ⤵️ 10.3.7-11: _create_sys_operation()
```

回填后同 DeepAdapter，调用 `d.createSysOperation(configBase)` 并将 `sysOp` 传入 params。

---

## 9. IMPLEMENTATION_PLAN.md 状态变更

| 步骤 | 变更 | 说明 |
|------|------|------|
| 10.3.7-11 | 🔄→✅（SysOpBuilder 部分） | BuildFilesystemPolicy + CreateSandboxSysOpCard + CreateLocalSysOpCard + 展示辅助 + 实例化注册 |
| 10.3.2 | 补充步骤 17 回填标记 | DeepAdapter.CreateInstance 步骤 17 接通 |

---

## 10. 测试要求

每个函数配备单元测试，覆盖率 ≥ 85%：

| 文件 | 测试文件 | 覆盖内容 |
|------|---------|---------|
| builder.go | builder_test.go | CreateLocalSysOpCard、CreateSandboxSysOpCard、CreateSysOperationFromCard、ResolveOperationMode |
| policy.go | policy_test.go | BuildFilesystemPolicy（含 allow/deny、intrinsic、project_dir、isCodeAgent 各场景）、collectIntrinsicTargets、resolveProjectDir、normalizeFSEntry |
| display.go | display_test.go | ListAutoManagedSandboxPaths、ListEffectiveSandboxFiles、FindAutoManagedMatch、appendUnique、classifyHostKind |

使用 `t.TempDir()` 创建临时目录，不依赖真实文件系统路径。

---

## 11. 日志对齐

对齐 Python 中 `[sysop_builder]` 前缀的所有 logger 调用，使用结构化日志：

| Python | Go |
|--------|-----|
| `logger.info("[sysop_builder] created empty intrinsic file: %s", path)` | `logger.Info(logComponent).Str("path", path).Msg("创建空固有文件")` |
| `logger.warning("[sysop_builder] could not ensure intrinsic file %s: %s", path, exc)` | `logger.Warn(logComponent).Str("path", path).Err(exc).Msg("确保固有文件失败")` |
| `logger.info("[sysop_builder] sandbox SysOperationCard created: ...")` | `logger.Info(logComponent).Str("base_url", ...).Int("bind_mounts", ...).Msg("沙箱 SysOperationCard 已创建")` |
| `logger.info("[sysop_builder] local SysOperationCard created (mode=LOCAL)")` | `logger.Info(logComponent).Msg("本地 SysOperationCard 已创建")` |

---

## 12. 不在本次范围

| 内容 | 原因 |
|------|------|
| CodeAgentRail | 依赖 AgentConfigService（10.3.13），需单独实现 |
| TeamHelpers | 依赖 TeamManager + TeamMonitorHandler（10.6.19-23），需 Team 系统先就绪 |
| EvolutionHelpers | 依赖 Team 系统 + 推送机制，需后续实现 |
| applySandboxRuntimePatch | 沙箱运行时热更新，依赖 force_recreate_jiuwenbox_sandbox，后续实现 |
| SandboxSysOperation 独立实现 | 当前 `NewSysOperation` sandbox 模式 fallback 到 LocalSysOperation，真正沙箱执行需 jiuwenbox provider |
