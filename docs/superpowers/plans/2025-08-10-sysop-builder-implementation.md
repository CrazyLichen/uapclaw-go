# SysOpBuilder 完整实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完整实现 SysOpBuilder（Card 构建 + FilesystemPolicy + SysOperation 实例化注册 + 展示辅助），一比一对齐 Python `sysop_builder.py`

**Architecture:** sysop_builder 包内拆 3 文件（builder/policy/display），非导出辅助函数包内共享。DeepAdapter.createSysOperation 回填完整流程，CreateInstance 步骤 17 接通。SysOperationMgr 新增 GetSysOperationByIsolationKey 方法支持隔离复用。

**Tech Stack:** Go 1.22+, 标准库 os/path/filepath, 内部依赖 workspace(路径函数), sys_operation(类型), runner/resources_manager(注册), logger(日志)

---

## 文件结构

| 操作 | 路径 | 职责 |
|------|------|------|
| **重写** | `internal/swarm/server/sysop_builder/builder.go` | Card 构建 + SysOperation 实例化 + ResolveOperationMode |
| **新建** | `internal/swarm/server/sysop_builder/policy.go` | BuildFilesystemPolicy + collectIntrinsicTargets + resolveProjectDir + 辅助函数 |
| **新建** | `internal/swarm/server/sysop_builder/display.go` | ListAutoManagedSandboxPaths + ListEffectiveSandboxFiles + FindAutoManagedMatch + 辅助函数 |
| **新建** | `internal/swarm/server/sysop_builder/builder_test.go` | builder.go 单元测试 |
| **新建** | `internal/swarm/server/sysop_builder/policy_test.go` | policy.go 单元测试 |
| **新建** | `internal/swarm/server/sysop_builder/display_test.go` | display.go 单元测试 |
| **修改** | `internal/swarm/server/sysop_builder/doc.go` | 更新文件目录 |
| **修改** | `internal/swarm/server/adapter/deep_adapter_config.go` | 回填 createSysOperation + 新增 resolveProjectDirForSandbox + getSandboxRuntime |
| **修改** | `internal/swarm/server/adapter/deep_adapter.go` | 回填 CreateInstance 步骤 17 |
| **修改** | `internal/swarm/server/adapter/code_adapter.go` | 回填 CreateInstance 步骤 17 |
| **修改** | `internal/agentcore/runner/resources_manager/sys_operation_manager.go` | 新增 GetSysOperationByIsolationKey |
| **修改** | `IMPLEMENTATION_PLAN.md` | 更新 10.3.7-11 状态 |

---

### Task 1: policy.go — 非导出辅助函数

**Files:**
- Create: `internal/swarm/server/sysop_builder/policy.go`
- Create: `internal/swarm/server/sysop_builder/policy_test.go`

- [ ] **Step 1: 编写 policy.go 基础结构和非导出辅助函数**

```go
package sysop_builder

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 常量 ────────────────────────────

// envSandboxProjectDir 沙箱项目目录环境变量。
// 对齐 Python: JIUSWARM_SANDBOX_PROJECT_DIR
const envSandboxProjectDir = "UAPCLAW_SANDBOX_PROJECT_DIR"

// ──────────────────────────── 全局变量 ────────────────────────────

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

// ──────────────────────────── 非导出函数 ────────────────────────────

// collectIntrinsicTargets 收集 deep agent 固有路径，分 rw/ro 两类返回。
// 对齐 Python: _collect_intrinsic_targets() → (rw_files, rw_dirs, ro_files)
func collectIntrinsicTargets() (rwFiles, rwDirs, roFiles []string) {
	rwFiles = make([]string, 0)
	rwDirs = make([]string, 0)
	roFiles = make([]string, 0)

	// 固有 rw 文件（AGENT.md / HEARTBEAT.md / IDENTITY.md / SOUL.md / USER.md）
	for _, fn := range intrinsicRWFilePathFuncs {
		raw := fn()
		if raw == "" {
			continue
		}
		if ensureIntrinsicFile(raw) {
			rwFiles = append(rwFiles, raw)
		}
	}

	// daily_memory 目录（仅 host 存在时加入，不自动创建）
	dailyMemoryPath := filepath.Join(workspace.AgentMemoryDir(), "daily_memory")
	if info, err := os.Stat(dailyMemoryPath); err == nil && info.IsDir() {
		rwDirs = append(rwDirs, dailyMemoryPath)
	} else {
		logger.Info(logComponent).
			Str("path", dailyMemoryPath).
			Msg("daily_memory 不存在于 host，跳过沙箱 bind 列表")
	}

	// 固有 ro 文件（config.yaml）
	for _, fn := range intrinsicROFilePathFuncs {
		raw := fn()
		if raw == "" {
			continue
		}
		resolved, err := filepath.EvalSymlinks(raw)
		if err != nil {
			logger.Warn(logComponent).
				Str("path", raw).
				Err(err).
				Msg("固有 ro 文件路径解析失败，跳过")
			continue
		}
		if _, statErr := os.Stat(resolved); statErr != nil {
			logger.Warn(logComponent).
				Str("path", resolved).
				Msg("固有 ro 文件不存在于 host，跳过沙箱 bind 列表")
			continue
		}
		roFiles = append(roFiles, resolved)
	}

	return rwFiles, rwDirs, roFiles
}

// resolveProjectDir 解析挂入沙箱的主写入根目录。
// 对齐 Python: _resolve_project_dir(override)
// 优先级：override → UAPCLAW_SANDBOX_PROJECT_DIR 环境变量 → os.Getwd()
// 拒绝挂载文件系统根 "/"。
func resolveProjectDir(override string) string {
	candidates := make([]string, 0, 3)
	if override != "" {
		candidates = append(candidates, override)
	}
	if envVal := os.Getenv(envSandboxProjectDir); envVal != "" {
		candidates = append(candidates, envVal)
	}
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		candidates = append(candidates, cwd)
	}

	for _, cand := range candidates {
		resolved, err := filepath.Abs(cand)
		if err != nil {
			logger.Debug(logComponent).
				Str("candidate", cand).
				Err(err).
				Msg("project_dir 候选路径解析失败")
			continue
		}
		info, statErr := os.Stat(resolved)
		if statErr != nil || !info.IsDir() {
			logger.Debug(logComponent).
				Str("candidate", resolved).
				Msg("project_dir 候选不是目录，跳过")
			continue
		}
		// 拒绝文件系统根
		if resolved == "/" || resolved == filepath.VolumeName(resolved)+string(os.PathSeparator) {
			logger.Warn(logComponent).
				Str("candidate", resolved).
				Msg("拒绝将文件系统根作为 rw project 目录")
			return ""
		}
		return resolved
	}
	return ""
}

// resolveAgentSkillsDir 解析内置技能目录。
// 对齐 Python: _resolve_agent_skills_dir()
func resolveAgentSkillsDir() string {
	raw := workspace.AgentSkillsDir()
	if raw == "" {
		return ""
	}
	resolved, err := filepath.Abs(raw)
	if err != nil {
		logger.Debug(logComponent).
			Str("path", raw).
			Err(err).
			Msg("内置技能目录路径解析失败")
		return ""
	}
	info, statErr := os.Stat(resolved)
	if statErr != nil || !info.IsDir() {
		logger.Debug(logComponent).
			Str("path", resolved).
			Msg("内置技能目录不存在，跳过")
		return ""
	}
	return resolved
}

// ensureIntrinsicFile 确保固有文件存在，不存在则 touch 空文件。
// 对齐 Python: _ensure_intrinsic_file(path) → bool
func ensureIntrinsicFile(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	// 创建父目录
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Warn(logComponent).
			Str("path", path).
			Err(err).
			Msg("确保固有文件失败：创建父目录失败")
		return false
	}
	// touch 空文件
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		logger.Warn(logComponent).
			Str("path", path).
			Err(err).
			Msg("确保固有文件失败：创建文件失败")
		return false
	}
	f.Close()
	logger.Info(logComponent).
		Str("path", path).
		Msg("创建空固有文件")
	return true
}

// normalizeFSEntry 归一化 {path, permissions} 项，接受 string 或 map[string]any。
// 对齐 Python: _normalize_fs_entry(entry, default_permissions)
func normalizeFSEntry(entry any, defaultPermissions string) map[string]any {
	if entry == nil {
		return nil
	}
	switch v := entry.(type) {
	case string:
		path := strings.TrimSpace(v)
		if path == "" {
			return nil
		}
		return map[string]any{"path": path, "permissions": defaultPermissions}
	case map[string]any:
		rawPath, _ := v["path"]
		path := strings.TrimSpace(fmt.Sprintf("%v", rawPath))
		if path == "" || path == "<nil>" {
			return nil
		}
		perm, _ := v["permissions"]
		permStr := fmt.Sprintf("%v", perm)
		if permStr == "" || permStr == "<nil>" {
			permStr = defaultPermissions
		}
		return map[string]any{"path": path, "permissions": permStr}
	default:
		return nil
	}
}
```

注意：policy.go 需要导入 `"fmt"` 用于 `normalizeFSEntry`。

- [ ] **Step 2: 编写 policy_test.go 辅助函数测试**

测试 `normalizeFSEntry`、`ensureIntrinsicFile`、`resolveProjectDir` 的核心场景，使用 `t.TempDir()` 隔离。

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/sysop_builder/... -run "TestNormalizeFSEntry|TestEnsureIntrinsicFile|TestResolveProjectDir" -v -count=1`

- [ ] **Step 4: Commit**

```
feat(sysop_builder): 新增 policy.go 辅助函数（collectIntrinsicTargets/resolveProjectDir/normalizeFSEntry 等）
```

---

### Task 2: policy.go — BuildFilesystemPolicy 核心实现

**Files:**
- Modify: `internal/swarm/server/sysop_builder/policy.go`
- Modify: `internal/swarm/server/sysop_builder/policy_test.go`

- [ ] **Step 1: 编写 BuildFilesystemPolicy 及 policyBuilder 结构体**

在 policy.go 中添加导出函数 `BuildFilesystemPolicy` 和 `policyBuilder` 结构体（封装闭包状态），完整实现对齐 Python `build_filesystem_policy` L350-648 的逻辑：

- policyBuilder 结构体含 allowFiles/allowDirs/bindMounts/uploadList/writablePaths/readWritePromote/readOnlyPromote
- policyBuilder.recordRWBind — 对齐 Python `_record_rw_bind`
- policyBuilder.recordUserDenyBind — 对齐 Python `_record_user_deny_bind`
- policyBuilder.recordROResourceBind — 对齐 Python `_record_ro_resource_bind`
- policyBuilder.build — 组装最终 policy dict
- BuildFilesystemPolicy 遍历 intrinsic → agent_skills → project_dir → files.allow → files.deny，组装返回

- [ ] **Step 2: 编写 BuildFilesystemPolicy 测试**

覆盖场景：
- 空输入返回默认结构
- isCodeAgent=true 时 project_dir 被 bind
- isCodeAgent=false 时 project_dir 被忽略
- files.allow 中不存在的路径返回 error
- files.deny 中不存在的路径返回 error
- files.allow + files.deny 混合

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/sysop_builder/... -run "TestBuildFilesystemPolicy" -v -count=1`

- [ ] **Step 4: Commit**

```
feat(sysop_builder): 实现 BuildFilesystemPolicy（一比一对齐 Python build_filesystem_policy）
```

---

### Task 3: builder.go — Card 构建重写

**Files:**
- Rewrite: `internal/swarm/server/sysop_builder/builder.go`
- Create: `internal/swarm/server/sysop_builder/builder_test.go`

- [ ] **Step 1: 重写 builder.go**

重写 `CreateLocalSysOpCard`、`CreateSandboxSysOpCard`、`CreateSysOperationFromCard`、`ResolveOperationMode`，移除所有 `⤵️ 10.3.7-11` 标记。

`CreateLocalSysOpCard` 签名：`() *sysop.SysOperationCard`（去掉 3 个参数，对齐 Python 无参数）

`CreateSandboxSysOpCard` 签名：
```go
func CreateSandboxSysOpCard(
    sandboxURL string, sandboxType string,
    filesRuntime map[string]any, excludedCommands []string,
    idleTTLSecs *int, idleCheckInterval *int,
    projectDir string, isCodeAgent bool,
) *sysop.SysOperationCard
```
内部调用 `BuildFilesystemPolicy`，构建 `SandboxGatewayConfig` + `PreDeployLauncherConfig`，组装 `SysOperationCard`。失败时返回 nil。

`CreateSysOperationFromCard` 内部调用 `sysop.NewSysOperation(card)`，隔离键复用检查，注册到 `ResourceMgr`。

`ResolveOperationMode` 从 `configBase["sys_operation"]["mode"]` 解析，默认 `OperationModeLocal`。

- [ ] **Step 2: 编写 builder_test.go**

覆盖：
- `CreateLocalSysOpCard` 返回 mode=LOCAL + WorkConfig
- `CreateSandboxSysOpCard` 返回 mode=SANDBOX + GatewayConfig + Policy
- `CreateSandboxSysOpCard` BuildFilesystemPolicy 失败时返回 nil
- `CreateSysOperationFromCard` card=nil 返回 nil
- `CreateSysOperationFromCard` 成功注册
- `ResolveOperationMode` 各种输入

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/sysop_builder/... -v -count=1`

- [ ] **Step 4: Commit**

```
feat(sysop_builder): 重写 builder.go — CreateLocalSysOpCard/CreateSandboxSysOpCard/CreateSysOperationFromCard 完整实现
```

---

### Task 4: display.go — 展示辅助实现

**Files:**
- Create: `internal/swarm/server/sysop_builder/display.go`
- Create: `internal/swarm/server/sysop_builder/display_test.go`

- [ ] **Step 1: 编写 display.go**

实现 `ListAutoManagedSandboxPaths`、`ListEffectiveSandboxFiles`、`FindAutoManagedMatch` 及辅助函数 `appendUnique`、`classifyHostKind`、`resolveDisplayPath`。

对齐 Python L836-1091。复用 `collectIntrinsicTargets`、`resolveProjectDir`、`resolveAgentSkillsDir`、`normalizeFSEntry`（均在 policy.go 中定义，同包可访问）。

- [ ] **Step 2: 编写 display_test.go**

覆盖：
- `ListAutoManagedSandboxPaths` isCodeAgent=true/false
- `ListEffectiveSandboxFiles` 空/有 files_runtime
- `FindAutoManagedMatch` 命中/未命中
- `classifyHostKind` file/dir
- `appendUnique` 去重

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/sysop_builder/... -v -count=1`

- [ ] **Step 4: Commit**

```
feat(sysop_builder): 实现 display.go — ListAutoManagedSandboxPaths/ListEffectiveSandboxFiles/FindAutoManagedMatch
```

---

### Task 5: doc.go 更新

**Files:**
- Modify: `internal/swarm/server/sysop_builder/doc.go`

- [ ] **Step 1: 更新包文档文件目录**

```go
// Package sysop_builder 提供系统操作（SysOperation）卡片和实例的构建器。
//
// 根据运行模式（local/sandbox）创建对应的 SysOperationCard 和 SysOperation 实例，
// 供 DeepAdapter._create_sys_operation 调用。同时提供沙箱 filesystem policy 组装
// 和路径展示辅助。
//
// 文件目录：
//
//	sysop_builder/
//	├── doc.go       # 包文档
//	├── builder.go   # Card 构建 + SysOperation 实例化 + ResolveOperationMode
//	├── policy.go    # BuildFilesystemPolicy + 固有路径收集 + 辅助函数
//	└── display.go   # 展示辅助：ListAutoManaged/FindAutoManaged + 辅助函数
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/sysop_builder.py
package sysop_builder
```

- [ ] **Step 2: Commit**

```
docs(sysop_builder): 更新 doc.go 文件目录
```

---

### Task 6: SysOperationMgr.GetSysOperationByIsolationKey

**Files:**
- Modify: `internal/agentcore/runner/resources_manager/sys_operation_manager.go`
- Modify: `internal/agentcore/runner/resources_manager/sys_operation_manager_test.go`

- [ ] **Step 1: 新增 GetSysOperationByIsolationKey 方法**

```go
// GetSysOperationByIsolationKey 按隔离键模板查找已注册的 SysOperation。
// 对齐 Python: SysOperationMgr._sandbox_key_owner_map[key] → get_sys_operation(op_id)
func (m *SysOperationMgr) GetSysOperationByIsolationKey(key string) (sysop.SysOperation, error) {
	if key == "" {
		return nil, fmt.Errorf("隔离键模板为空")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	opID, ok := m.sandboxKeyOwnerMap[key]
	if !ok {
		return nil, fmt.Errorf("未找到隔离键 %q 对应的 SysOperation", key)
	}
	return m.sysOperations.Get(opID)
}
```

- [ ] **Step 2: 编写测试**

覆盖：key 为空、key 不存在、key 存在返回实例。

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/runner/resources_manager/... -run "TestSysOperationMgr_GetByIsolationKey" -v -count=1`

- [ ] **Step 4: Commit**

```
feat(resources_manager): 新增 SysOperationMgr.GetSysOperationByIsolationKey
```

---

### Task 7: DeepAdapter 回填 — createSysOperation + 辅助方法

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_config.go`

- [ ] **Step 1: 新增 resolveProjectDirForSandbox 和 getSandboxRuntime 辅助方法**

```go
// resolveProjectDirForSandbox 解析沙箱挂载用的项目目录。
// 对齐 Python: _resolve_project_dir_for_sandbox()
func (d *DeepAdapter) resolveProjectDirForSandbox() string {
	if d.projectDir != "" {
		return d.projectDir
	}
	return ""
}

// getSandboxRuntime 从配置获取沙箱运行时参数。
// 对齐 Python: get_sandbox_runtime() + get_config()["sandbox"]
func (d *DeepAdapter) getSandboxRuntime(configBase map[string]any) (url, typ string, runtime map[string]any) {
	sandbox, _ := configBase["sandbox"].(map[string]any)
	if sandbox == nil {
		sandbox = make(map[string]any)
	}
	url, _ = sandbox["url"].(string)
	typ, _ = sandbox["type"].(string)
	runtime = sandbox
	return
}
```

- [ ] **Step 2: 重写 createSysOperation 方法**

替换当前骨架实现，调用新签名的 `sysop_builder.CreateSandboxSysOpCard`（8 参数）和 `sysop_builder.CreateLocalSysOpCard`（0 参数），包含完整的 sandbox 配置提取和隔离复用逻辑。

- [ ] **Step 3: Commit**

```
feat(adapter): 回填 DeepAdapter.createSysOperation 完整实现
```

---

### Task 8: CreateInstance 步骤 17 回填

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go`
- Modify: `internal/swarm/server/adapter/code_adapter.go`

- [ ] **Step 1: 回填 deep_adapter.go 步骤 17**

将第 393-425 行的占位注释替换为：

```go
// 步骤 17: sys_operation = _create_sys_operation()
sysOp, _ := d.createSysOperation(configBase)
if sysOp != nil {
	params.SysOperation = sysOp
}
```

移除 `⤵️ 10.3.7-11` 标记。

- [ ] **Step 2: 回填 code_adapter.go 步骤 17**

同 deep_adapter.go，调用 `d.createSysOperation(configBase)` 并赋值 `params.SysOperation`。

- [ ] **Step 3: 运行编译确认无语法错误**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/...`

- [ ] **Step 4: Commit**

```
feat(adapter): 回填 CreateInstance 步骤 17 — sys_operation 接通
```

---

### Task 9: IMPLEMENTATION_PLAN.md 状态更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 10.3.7-11 SysOpBuilder 部分状态**

在 10.3.7-11 行描述中标注 SysOpBuilder 已完成（在已有的 RecapPrompts✅ + handleCommand✅ 之后追加 SysOpBuilder✅）。

- [ ] **Step 2: Commit**

```
docs: 更新 IMPLEMENTATION_PLAN.md — 10.3.7-11 SysOpBuilder 完成
```

---

### Task 10: 全量测试 + 覆盖率检查

**Files:**
- All test files

- [ ] **Step 1: 运行 sysop_builder 包全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/sysop_builder/... -v -count=1 -cover`

- [ ] **Step 2: 运行 resources_manager 包测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/runner/resources_manager/... -v -count=1 -cover`

- [ ] **Step 3: 运行 adapter 包编译（不跑测试，因为依赖外部）**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/...`

- [ ] **Step 4: 确认覆盖率 ≥ 85%**

如果 sysop_builder 包覆盖率不足，补充测试用例。

- [ ] **Step 5: 最终 Commit**

```
test(sysop_builder): 确保全量测试通过，覆盖率 ≥ 85%
```
