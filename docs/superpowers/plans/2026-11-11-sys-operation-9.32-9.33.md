# 9.32-9.33 SysOperation 接口 + LocalSysOperation 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完整对齐 Python `openjiuwen/core/sys_operation/` 的接口抽象层（9.32）和本地执行实现（9.33），包括 OperationRegistry、Result 类型、Local Shell/FS/Code 操作、SysOperationToolAdapter、回填所有占位桩。

**Architecture:** 一比一复刻 Python 文件组织：`sys_operation/` 根包放接口+注册+调度+适配器，`sys_operation/result/` 子包放所有 Result/Data 类型，`sys_operation/local/` 子包放本地实现。sandbox/ 延后（9.34）。ListTools() 硬编码 ToolCard，description 和提示词严格对齐 Python 英文原文不翻译。session_id 通过 context.Context 传递（和 CWD 模式一致）。

**Tech Stack:** Go 1.22+, os/exec（子进程管理）, context.Context（CWD/session 传递）, sync.RWMutex（并发安全）, encoding/json（序列化）

---

## File Structure

### 新建文件

| 文件 | 职责 |
|---|---|
| `internal/agentcore/sys_operation/result/base_result.go` | BaseResult + BuildOperationErrorResult |
| `internal/agentcore/sys_operation/result/shell_operation_result.go` | ExecuteCmdData/Result + ChunkData/StreamResult + BackgroundData/Result |
| `internal/agentcore/sys_operation/result/fs_operation_result.go` | ReadFile/WriteFile/Upload/Download Data/Result + FileSystemItem + FileSystemData + SearchFilesData/Result |
| `internal/agentcore/sys_operation/result/code_operation_result.go` | ExecuteCodeData/Result + ChunkData/StreamResult |
| `internal/agentcore/sys_operation/result/doc.go` | result 子包包文档 |
| `internal/agentcore/sys_operation/base.go` | BaseOperation + OperationMode（从 sys_operation.go 拆出） |
| `internal/agentcore/sys_operation/config.go` | LocalWorkConfig/SandboxGatewayConfig 等（从 sys_operation_card.go 拆出） |
| `internal/agentcore/sys_operation/registry.go` | OperationRegistry + OperationDef |
| `internal/agentcore/sys_operation/shell.go` | BaseShellOperation + ShellType + ShellOptions（从 sys_operation.go 拆出补全） |
| `internal/agentcore/sys_operation/fs.go` | BaseFsOperation + FsOptions（从 sys_operation.go 拆出补全） |
| `internal/agentcore/sys_operation/code.go` | BaseCodeOperation + CodeOptions（从 sys_operation.go 拆出补全） |
| `internal/agentcore/sys_operation/tool_adapter.go` | SysOperationToolAdapter |
| `internal/agentcore/sys_operation/local/utils.go` | AsyncProcessHandler + OperationUtils + StreamEvent/StreamEventType |
| `internal/agentcore/sys_operation/local/shell_operation.go` | 本地 Shell 操作实现 |
| `internal/agentcore/sys_operation/local/fs_operation.go` | 本地文件系统操作实现 |
| `internal/agentcore/sys_operation/local/code_operation.go` | 本地代码执行实现 |
| `internal/agentcore/sys_operation/local/doc.go` | local 子包包文档 |

### 修改文件

| 文件 | 修改内容 |
|---|---|
| `internal/agentcore/sys_operation/sys_operation.go` | 重写：SysOperation 调度器（LocalSysOperation lazy 实例化） |
| `internal/agentcore/sys_operation/sys_operation_card.go` | 小改：SandboxRoot → []string，拆出 config 类型 |
| `internal/agentcore/sys_operation/shell_process_registry.go` | 新增 WithSessionID/SessionIDFromCtx/ResolveShellSessionID |
| `internal/agentcore/sys_operation/doc.go` | 更新文件目录 |
| `internal/agentcore/runner/resources_manager/resource_manager.go` | 回填 registerSysOperationTools/RemoveSysOperation/GetSysOpToolCards |
| `internal/agentcore/harness/harness_config/builder.go` | 回填 createSysOperation |
| `internal/agentcore/harness/factory.go` | 回填 buildSysOperation |
| `internal/agentcore/harness/deep_agent.go` | 回填 ContextEngine SysOperation 注入 |
| `internal/agentcore/context_engine/processor/offload.go` | 无需修改（已实现，只是注入断裂） |

---

## Task 1: result/ 子包 — Shell Operation Result 类型

**Files:**
- Create: `internal/agentcore/sys_operation/result/base_result.go`
- Create: `internal/agentcore/sys_operation/result/shell_operation_result.go`
- Create: `internal/agentcore/sys_operation/result/doc.go`
- Test: `internal/agentcore/sys_operation/result/shell_operation_result_test.go`

- [ ] **Step 1: 创建 result/base_result.go**

```go
package result

// ──────────────────────────── 结构体 ────────────────────────────

// BaseResult 操作结果基类
type BaseResult struct {
	// Code 状态码：0 = 成功，非 0 = 失败
	Code int
	// Message 状态消息
	Message string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildOperationErrorResult 构造标准化错误结果
func BuildOperationErrorResult(errorCode int, errMsg string) BaseResult {
	return BaseResult{Code: errorCode, Message: errMsg}
}

// IsSuccess 判断结果是否成功
func (r BaseResult) IsSuccess() bool {
	return r.Code == 0
}
```

- [ ] **Step 2: 创建 result/shell_operation_result.go**

严格对齐 Python `result/shell_operation_result.py` 的所有字段。

```go
package result

// ──────────────────────────── 结构体 ────────────────────────────

// ExecuteCmdData 执行命令结果数据
type ExecuteCmdData struct {
	// Command 执行的命令
	Command string `json:"command"`
	// Cwd 工作目录
	Cwd string `json:"cwd"`
	// ExitCode 退出码
	ExitCode *int `json:"exit_code"`
	// Stdout 标准输出
	Stdout string `json:"stdout"`
	// Stderr 标准错误
	Stderr string `json:"stderr"`
}

// ExecuteCmdResult 执行命令结果
type ExecuteCmdResult struct {
	BaseResult
	// Data 结果数据
	Data *ExecuteCmdData `json:"data"`
}

// ExecuteCmdChunkData 执行命令流式块数据
type ExecuteCmdChunkData struct {
	// Text 输出块内容
	Text string `json:"text"`
	// Type 输出类型：stdout / stderr
	Type *string `json:"type"`
	// ChunkIndex 块索引
	ChunkIndex int `json:"chunk_index"`
	// ExitCode 退出码
	ExitCode *int `json:"exit_code"`
	// Metadata 附加元数据
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ExecuteCmdStreamResult 执行命令流式结果
type ExecuteCmdStreamResult struct {
	BaseResult
	// Data 流式块数据
	Data *ExecuteCmdChunkData `json:"data"`
}

// ExecuteCmdBackgroundData 后台执行命令结果数据
type ExecuteCmdBackgroundData struct {
	// Command 执行的命令
	Command string `json:"command"`
	// Cwd 工作目录
	Cwd string `json:"cwd"`
	// Pid 进程 ID
	Pid *int `json:"pid"`
}

// ExecuteCmdBackgroundResult 后台执行命令结果
type ExecuteCmdBackgroundResult struct {
	BaseResult
	// Data 后台执行数据
	Data *ExecuteCmdBackgroundData `json:"data"`
}
```

- [ ] **Step 3: 创建 result/doc.go**

```go
// Package result 提供系统操作的结果类型定义。
//
// 所有子操作（fs/shell/code）的返回值都使用本包的 Result + Data 类型，
// 遵循 BaseResult{Code, Message} + 具体 Data 的统一模式。
//
// 文件目录：
//
//	result/
//	├── doc.go                        # 包文档
//	├── base_result.go                # BaseResult + BuildOperationErrorResult
//	├── shell_operation_result.go     # Shell 操作结果类型
//	├── fs_operation_result.go        # 文件系统操作结果类型
//	└── code_operation_result.go      # 代码执行结果类型
//
// 对应 Python 代码：openjiuwen/core/sys_operation/result/
package result
```

- [ ] **Step 4: 编写测试 result/shell_operation_result_test.go**

测试所有 Shell Result 类型的构造、JSON 序列化、IsSuccess 判断。

- [ ] **Step 5: 运行测试，确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/sys_operation/result/... -v`

- [ ] **Step 6: Commit**

---

## Task 2: result/ 子包 — FS Operation Result 类型

**Files:**
- Create: `internal/agentcore/sys_operation/result/fs_operation_result.go`
- Test: `internal/agentcore/sys_operation/result/fs_operation_result_test.go`

- [ ] **Step 1: 创建 result/fs_operation_result.go**

严格对齐 Python `result/fs_operation_result.py`。包含：ReadFileData/Result, ReadFileChunkData/StreamResult, WriteFileData/Result, UploadFileData/Result, UploadFileChunkData/StreamResult, DownloadFileData/Result, DownloadFileChunkData/StreamResult, FileSystemItem, FileSystemData, SearchFilesData/Result, ListFilesResult, ListDirsResult。每个类型的字段完整对应 Python，参考设计方案附录 A。

- [ ] **Step 2: 编写测试**

测试所有 FS Result 类型的构造、JSON 序列化、FileSystemItem 字段。

- [ ] **Step 3: 运行测试**

- [ ] **Step 4: Commit**

---

## Task 3: result/ 子包 — Code Operation Result 类型

**Files:**
- Create: `internal/agentcore/sys_operation/result/code_operation_result.go`
- Test: `internal/agentcore/sys_operation/result/code_operation_result_test.go`

- [ ] **Step 1: 创建 result/code_operation_result.go**

严格对齐 Python `result/code_operation_result.py`。包含：ExecuteCodeData/Result, ExecuteCodeChunkData/StreamResult。

- [ ] **Step 2: 编写测试**

- [ ] **Step 3: 运行测试**

- [ ] **Step 4: Commit**

---

## Task 4: 拆分 base.go + config.go — 从现有文件拆出

**Files:**
- Create: `internal/agentcore/sys_operation/base.go`
- Create: `internal/agentcore/sys_operation/config.go`
- Modify: `internal/agentcore/sys_operation/sys_operation_card.go`（移出 LocalWorkConfig/SandboxGatewayConfig 等）
- Test: `internal/agentcore/sys_operation/base_test.go`

- [ ] **Step 1: 创建 base.go**

从 `sys_operation.go` 拆出 OperationMode 枚举及其 String/MarshalJSON/UnmarshalJSON 方法。新增 BaseOperation 结构体（name, mode, description, runConfig 字段）和 ListTools/CreateSysOperationEvent 方法。

- [ ] **Step 2: 创建 config.go**

从 `sys_operation_card.go` 拆出 LocalWorkConfig, SandboxIsolationConfig, SandboxLauncherConfig, SandboxGatewayConfig, ContainerScope 枚举及其方法。修正 LocalWorkConfig.SandboxRoot 从 `string` 改为 `[]string` 对齐 Python。

- [ ] **Step 3: 修改 sys_operation_card.go**

移除已拆到 config.go 的类型。sys_operation_card.go 只保留 SysOperationCard, ToolIdProxy, SysOperationCardOption, generateIsolationKeyTemplate。

- [ ] **Step 4: 更新所有 import**

确保引用 LocalWorkConfig/SandboxGatewayConfig/ContainerScope 的文件 import sys_operation 包（无需改路径，因为在同一包内）。

- [ ] **Step 5: 运行全量测试确认无破坏**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/sys_operation/... -v`

- [ ] **Step 6: Commit**

---

## Task 5: 拆分 shell.go + fs.go + code.go — 接口补全

**Files:**
- Create: `internal/agentcore/sys_operation/shell.go`
- Create: `internal/agentcore/sys_operation/fs.go`
- Create: `internal/agentcore/sys_operation/code.go`
- Modify: `internal/agentcore/sys_operation/sys_operation.go`（移除已拆出的接口/枚举/Options）

- [ ] **Step 1: 创建 shell.go**

从 sys_operation.go 拆出 ShellOperation 接口（补全 ExecuteCmdStream + ExecuteCmdBackground）、BaseShellOperation、ShellType 枚举、ShellOptions、ShellOption 函数选项。所有 Options 补全字段对齐 Python（新增 Options map[string]any）。

- [ ] **Step 2: 创建 fs.go**

从 sys_operation.go 拆出 FsOperation 接口（补全 ReadFileStream/UploadFile/UploadFileStream/DownloadFile/DownloadFileStream）、BaseFsOperation、FsOptions（补全所有字段：LineRange, PrependNewline, AppendNewline, Append, CreateIfNotExist, Permissions, LocalPath, TargetPath, SourcePath, Overwrite, CreateParentDirs, PreservePerms, Recursive, MaxDepth, SortBy, SortDescending, FileTypes, ExcludePatterns, Options）。

- [ ] **Step 3: 创建 code.go**

从 sys_operation.go 拆出 CodeOperation 接口（补全 ExecuteCodeStream）、BaseCodeOperation、CodeOptions（补全 Options 字段）。

- [ ] **Step 4: 重写 sys_operation.go**

移除已拆到 shell.go/fs.go/code.go/base.go 的类型。保留：SysOperation 接口、BaseSysOperation 空桩、LocalSysOperation 实现（lazy 实例化）。LocalSysOperation 实现对齐 Python sys_operation.py 的 `__getattr__` 动态调度逻辑，使用 OperationRegistry + instances map。

- [ ] **Step 5: 运行测试确认**

- [ ] **Step 6: Commit**

---

## Task 6: registry.go — OperationRegistry

**Files:**
- Create: `internal/agentcore/sys_operation/registry.go`
- Test: `internal/agentcore/sys_operation/registry_test.go`

- [ ] **Step 1: 创建 registry.go**

实现 OperationDef 结构体（NewFunc, Name, Mode, Description）和 OperationRegistry（repository map, Register, GetOperationInfo, GetSupportedOperations）。全局实例 GlobalRegistry。对齐 Python registry.py，但不做包扫描（Go 用 init() 显式注册）。

- [ ] **Step 2: 编写测试**

测试 Register/Get/幂等性/空注册表/多模式隔离。

- [ ] **Step 3: 运行测试**

- [ ] **Step 4: Commit**

---

## Task 7: shell_process_registry.go — 补全 session_id context 传递

**Files:**
- Modify: `internal/agentcore/sys_operation/shell_process_registry.go`
- Test: `internal/agentcore/sys_operation/shell_process_registry_test.go`（追加测试）

- [ ] **Step 1: 新增 WithSessionID/SessionIDFromCtx/ResolveShellSessionID**

在 shell_process_registry.go 末尾新增：
- `type sessionIDKey struct{}`
- `func WithSessionID(ctx context.Context, sessionID string) context.Context`
- `func SessionIDFromCtx(ctx context.Context) string`
- `func ResolveShellSessionID(ctx context.Context) string`（context → logger fallback）

对齐 Python 的 `set_shell_session_id` / `resolve_shell_session_id`，但 Go 用 context.Context 替代 ContextVar。

- [ ] **Step 2: 追加测试**

- [ ] **Step 3: 运行测试**

- [ ] **Step 4: Commit**

---

## Task 8: local/utils.go — AsyncProcessHandler + OperationUtils

**Files:**
- Create: `internal/agentcore/sys_operation/local/utils.go`
- Test: `internal/agentcore/sys_operation/local/utils_test.go`

- [ ] **Step 1: 创建 local/doc.go**

- [ ] **Step 2: 创建 local/utils.go**

一比一复刻 Python `local/utils.py`：
- StreamEventType 枚举（Stdout/Stderr/Exit/Error）
- StreamEvent 结构体（Type, Data, ExitCode, Timestamp）
- InvokeData 结构体（Stdout, Stderr, ExitCode, Exception）
- AsyncProcessHandler 结构体 + NewAsyncProcessHandler 构造
- Invoke(ctx) 方法：一次性执行收集完整输出，超时 kill 进程树，对齐 Python invoke() 的 drain 逻辑
- Stream(ctx) 方法：流式输出通过 channel 返回，对齐 Python stream() 的 reader 协程 + queue 逻辑
- Background(grace) 方法：后台启动 + 早期失败检测
- KillProcessTree() 方法：进程组 kill，对齐 Python _kill_process_tree
- OperationUtils：PrepareEnvironment, CreateTmpFile, DeleteTmpFile

Go 子进程使用 `os/exec.Cmd` + `cmd.Start()`（非 `exec.CommandContext` 以便精细控制超时和进程组）。

- [ ] **Step 3: 编写测试**

测试 Invoke 正常执行/超时/取消，Stream 流式输出，Background 后台启动，PrepareEnvironment 合并环境变量，CreateTmpFile/DeleteTmpFile 临时文件操作。

- [ ] **Step 4: 运行测试**

- [ ] **Step 5: Commit**

---

## Task 9: local/shell_operation.go — 本地 Shell 操作

**Files:**
- Create: `internal/agentcore/sys_operation/local/shell_operation.go`
- Test: `internal/agentcore/sys_operation/local/shell_operation_test.go`

这是 9.33 最核心的实现，对齐 Python `local/shell_operation.py`（~925 行）。

- [ ] **Step 1: 创建 shell_operation.go 骨架**

定义 ShellOperation 结构体（嵌入 BaseShellOperation）、DangerousPattern/TUICommandPattern 类型、init() 注册到 GlobalRegistry。

- [ ] **Step 2: 实现安全检查方法**

- `checkCommandSafety(command) string`：危险模式检测，对齐 Python `_DANGEROUS_PATTERNS` 全部正则（rm -rf, del /f /s /q, shutdown, reboot, pkill jiuwenswarm 等）
- `checkAllowlist(command) bool`：白名单校验，对齐 Python `_check_allowlist`
- `detectAndMitigateTUI(command, execEnv)`：TUI 命令检测 + 环境变量注入，对齐 Python `_TUI_COMMAND_PATTERNS`
- `resolveExecutionPlan(command, shellType)`：Shell 类型解析，对齐 Python `_resolve_execution_plan` 的全部分支（Windows/非Windows × Auto/Cmd/PowerShell/Bash/Sh）
- `createSubprocess(command, cwd, env, shellType, background, stream)`：子进程创建，进程组隔离（start_new_session），对齐 Python `_create_subprocess`
- `resolveCwd(ctx, cwd)`：CWD 解析，调用 `cwd.GetCwd(ctx)`

- [ ] **Step 3: 实现 ExecuteCmd**

对齐 Python `execute_cmd()` 完整逻辑：参数校验 → 安全检查 → 超时上限（JW_EXECUTE_CMD_MAX_TIMEOUT） → 环境变量准备 → TUI 检测 → 子进程创建 → Shell 进程注册 → 执行 → 注销 → 结果构造 → 日志记录（SYS_OP_START/END/ERROR）。所有日志字段对齐 Python。

- [ ] **Step 4: 实现 ExecuteCmdStream**

对齐 Python `execute_cmd_stream()` 的 AsyncIterator 逻辑，Go 用 `<-chan ExecuteCmdStreamResult` 返回。包含：流式事件转换（stdout/stderr/error/exit 四种事件处理）、chunk 索引递增、日志记录。

- [ ] **Step 5: 实现 ExecuteCmdBackground**

对齐 Python `execute_cmd_background()` 的 grace 检测逻辑。

- [ ] **Step 6: 实现 ListTools — 硬编码 ToolCard**

description 严格使用 Python 方法英文 docstring 原文，不翻译：

```go
func (s *ShellOperation) ListTools() []*tool.ToolCard {
    return []*tool.ToolCard{
        newShellToolCard("execute_cmd",
            "Asynchronously execute a command(shell mode only).",
            newExecuteCmdParams()),
        newShellToolCard("execute_cmd_stream",
            "Asynchronously execute a command streaming(shell mode only).",
            newExecuteCmdStreamParams()),
        newShellToolCard("execute_cmd_background",
            "Launch a command in the background and return immediately with its PID.",
            newExecuteCmdBackgroundParams()),
    }
}
```

inputParams 严格对齐 Python 方法签名的参数名、类型、默认值、描述（英文原文）。

- [ ] **Step 7: 编写测试**

测试 ExecuteCmd 基本/空命令/危险命令/超时，ExecuteCmdStream，ExecuteCmdBackground，checkCommandSafety，checkAllowlist，resolveExecutionPlan。

- [ ] **Step 8: 运行测试**

- [ ] **Step 9: Commit**

---

## Task 10: local/fs_operation.go — 本地文件系统操作

**Files:**
- Create: `internal/agentcore/sys_operation/local/fs_operation.go`
- Test: `internal/agentcore/sys_operation/local/fs_operation_test.go`

对齐 Python `local/fs_operation.py`（~1755 行）。

- [ ] **Step 1: 创建 fs_operation.go 骨架**

定义 FsOperation 结构体（嵌入 BaseFsOperation）、RWLock、init() 注册到 GlobalRegistry。

- [ ] **Step 2: 实现 resolvePath + sandbox 校验**

`resolvePath(ctx, path, createParent)` 对齐 Python `_resolve_path`：基于 CWD 解析相对路径 → expanduser → resolve → sandbox_root 校验（restrict_to_sandbox 时检查路径是否在允许范围内）。

- [ ] **Step 3: 实现 ReadFile + ReadFileStream**

对齐 Python `read_file()` / `read_file_stream()` 完整逻辑：参数校验（mutually exclusive: head/tail/line_range）→ 路径解析 → 文件打开 → 按模式读取（text 行级/bytes 原始）→ head/tail/line_range 截取 → 流式分块返回。

- [ ] **Step 4: 实现 WriteFile**

对齐 Python `write_file()`：路径解析 → 目录存在检查 → create_if_not_exist → append/overwrite 模式 → prepend_newline/append_newline → 权限设置。

- [ ] **Step 5: 实现 UploadFile/UploadFileStream + DownloadFile/DownloadFileStream**

本地模式下 upload/download = 文件拷贝。对齐 Python 逻辑：路径解析 → overwrite 检查 → create_parent_dirs → 分块传输（stream 模式）→ preserve_permissions。

- [ ] **Step 6: 实现 ListFiles/ListDirectories/SearchFiles**

对齐 Python 逻辑：路径解析 → 递归遍历 → 排序（name/modified_time/size）→ file_types 过滤 → FileSystemItem 构造。

- [ ] **Step 7: 实现 ListTools — 硬编码 ToolCard**

10 个方法的 ToolCard，description 严格用 Python 英文 docstring 原文。

- [ ] **Step 8: 编写测试**

使用 `t.TempDir()` 创建临时目录，测试 ReadFile/WriteFile/ListFiles/SearchFiles/sandbox 校验/Upload/Download。

- [ ] **Step 9: 运行测试**

- [ ] **Step 10: Commit**

---

## Task 11: local/code_operation.go — 本地代码执行

**Files:**
- Create: `internal/agentcore/sys_operation/local/code_operation.go`
- Test: `internal/agentcore/sys_operation/local/code_operation_test.go`

对齐 Python `local/code_operation.py`（~393 行）。

- [ ] **Step 1: 创建 code_operation.go**

实现 CodeOperation 结构体、init() 注册、supportLanguageConfigDict（python/javascript 的 execCli/execFile/fileSuffix）、buildSubprocessCmd（短代码 CLI 模式 / 长代码临时文件模式）。

- [ ] **Step 2: 实现 ExecuteCode + ExecuteCodeStream**

对齐 Python 完整逻辑：参数校验 → 语言支持检查 → buildSubprocessCmd → 环境变量（PYTHONIOENCODING/PYTHONUTF8/NODE_DISABLE_COLORS）→ 子进程执行 → 结果构造 → 日志记录。

- [ ] **Step 3: 实现 ListTools — 硬编码 ToolCard**

2 个方法，description 用 Python 英文 docstring 原文。

- [ ] **Step 4: 编写测试**

测试 ExecuteCode python/javascript，长代码临时文件，ExecuteCodeStream，不支持的语言拒绝。

- [ ] **Step 5: 运行测试**

- [ ] **Step 6: Commit**

---

## Task 12: tool_adapter.go — SysOperationToolAdapter

**Files:**
- Create: `internal/agentcore/sys_operation/tool_adapter.go`
- Test: `internal/agentcore/sys_operation/tool_adapter_test.go`

- [ ] **Step 1: 创建 tool_adapter.go**

实现 ToolAdapterEntry 结构体（ToolID string, Tool tool.Tool）和 SysOperationToolAdapter.ExtractTools(card, instance, language, agentID) 方法。

逻辑：遍历 OperationRegistry.GetSupportedOperations → 获取子操作实例 → ListTools → 对每个 ToolCard 构建 fn 闭包（绑定到具体子操作方法）→ NewTool(fn, WithToolCard) → 收集 ToolAdapterEntry。

- [ ] **Step 2: 编写测试**

用 mock SysOperation 测试 ExtractTools 提取正确数量的工具、ToolID 格式正确。

- [ ] **Step 3: 运行测试**

- [ ] **Step 4: Commit**

---

## Task 13: LocalSysOperation 调度器 — 重写 sys_operation.go

**Files:**
- Modify: `internal/agentcore/sys_operation/sys_operation.go`
- Test: `internal/agentcore/sys_operation/sys_operation_test.go`（更新）

- [ ] **Step 1: 重写 sys_operation.go**

实现 LocalSysOperation 结构体：
- card *SysOperationCard
- instances map[string]BaseOperation（lazy 缓存）
- mu sync.RWMutex

方法：
- NewLocalSysOperation(card)：构造，根据 mode 初始化 runConfig
- Card() / Fs() / Shell() / Code() / IsolationKeyTemplate()
- getOperation(name)：通用 lazy 实例化，从 OperationRegistry 查 OperationDef，调用 NewFunc 创建实例，缓存到 instances

保留 BaseSysOperation 空桩（给未实现时的 fallback）。

- [ ] **Step 2: 更新测试**

- [ ] **Step 3: 运行测试**

- [ ] **Step 4: Commit**

---

## Task 14: 回填 — ResourceManager

**Files:**
- Modify: `internal/agentcore/runner/resources_manager/resource_manager.go`

- [ ] **Step 1: 回填 registerSysOperationTools**

替换空桩为真实实现：调用 SysOperationToolAdapter.ExtractTools → 遍历 ToolAdapterEntry → innerAddResource 注册到 ToolMgr → AddSysOperationTools 维护关联索引。

- [ ] **Step 2: 回填 AddSysOperation**

注册成功后调用 registerSysOperationTools。

- [ ] **Step 3: 回填 RemoveSysOperation**

移除后调用 ToolMgr.RemoveSysOperationTools 获取关联工具 ID → innerRemoveResources 清理。

- [ ] **Step 4: 回填 GetSysOpToolCards**

通过 OperationRegistry + instance.ListTools() 获取 ToolCard。

- [ ] **Step 5: 运行 resource_manager 测试**

- [ ] **Step 6: Commit**

---

## Task 15: 回填 — builder.go + factory.go

**Files:**
- Modify: `internal/agentcore/harness/harness_config/builder.go`
- Modify: `internal/agentcore/harness/factory.go`

- [ ] **Step 1: 回填 createSysOperation（builder.go）**

替换 `return nil, fmt.Errorf("createSysOperation 尚未实现")` 为创建 LocalSysOperation 实例。

- [ ] **Step 2: 回填 buildSysOperation（factory.go）**

将 `return &sysop.BaseSysOperation{}, nil` 替换为 `return sysop.NewLocalSysOperation(sysopCard), nil`。AddSysOperation 调用改用 LocalSysOperation 实例。

- [ ] **Step 3: 运行 harness 包测试**

- [ ] **Step 4: Commit**

---

## Task 16: 回填 — ContextEngine SysOperation 注入

**Files:**
- Modify: `internal/agentcore/harness/deep_agent.go`

- [ ] **Step 1: 在 setupTaskLoop 中注入 SysOperation 到 ContextEngine**

找到 `ce := context_engine.NewContextEngine(ceschema.NewContextEngineConfig())`，改为：
```go
ceOpts := []iface.ContextEngineOption{}
if d.config.SysOperation != nil {
    ceOpts = append(ceOpts, iface.WithEngineSysOperation(d.config.SysOperation))
}
ce := context_engine.NewContextEngine(ceschema.NewContextEngineConfig(), ceOpts...)
```

- [ ] **Step 2: 验证 writeOffloadToFile 使用 SysOperation**

确认 offload 测试通过（SysOperation 非 nil 时优先调 Fs().WriteFile）。

- [ ] **Step 3: Commit**

---

## Task 17: 更新 doc.go + IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/agentcore/sys_operation/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 doc.go 文件目录**

列出所有新建/修改的文件。

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

将 9.32 从 ☐ 改为 ✅，9.33 从 ☐ 改为 ✅。将 5.21/5.29/5.31 的 ⤵️ 9.32 标记确认已回填。

- [ ] **Step 3: Commit**

---

## Task 18: 全量测试 + 覆盖率检查

- [ ] **Step 1: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/sys_operation/... -v -cover`

- [ ] **Step 2: 检查覆盖率 ≥ 85%**

如果不达标，补充测试用例。

- [ ] **Step 3: 运行受影响的上下游包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/resources_manager/... -v` 和 `go test ./internal/agentcore/harness/... -v`（只跑受影响的包）。

- [ ] **Step 4: Commit**

---

## Self-Review

### Spec Coverage

| Spec 章节 | 对应 Task |
|---|---|
| 2. 文件组织 | Task 1-11 文件结构 |
| 3.1 result/ 类型 | Task 1-3 |
| 3.2 base.go | Task 4 |
| 3.3 registry.go | Task 6 |
| 3.4 sys_operation.go 调度器 | Task 13 |
| 3.5 shell/fs/code 接口补全 | Task 5 |
| 3.5.1 session_id context 传递 | Task 7 |
| 3.6 Options 补全 | Task 5 |
| 3.7 local/ 实现 | Task 8-11 |
| 3.8 tool_adapter.go | Task 12 |
| 3.9 ListTools 硬编码 | Task 9/10/11 内 |
| 4.1 ResourceManager 回填 | Task 14 |
| 4.2 builder.go 回填 | Task 15 |
| 4.3 factory.go 回填 | Task 15 |
| 4.4 ContextEngine 注入回填 | Task 16 |
| 4.5 SandboxRoot 类型修正 | Task 4 内 |
| 5. 日志同步 | Task 9/10/11 内（逐方法对齐） |
| 6. 测试策略 | Task 1-18 各步骤 |

### Placeholder Scan

无 TBD/TODO/后实现占位。所有 Task 包含具体文件路径和实现内容。

### Type Consistency

- Result 类型定义在 Task 1-3，后续 Task 9-11 引用同一包
- BaseOperation 定义在 Task 4，Task 9-11 的 ShellOperation/FsOperation/CodeOperation 嵌入 BaseShellOperation/BaseFsOperation/BaseCodeOperation
- OperationRegistry 定义在 Task 6，Task 9-11 的 init() 注册到 GlobalRegistry
- SysOperation 接口在 Task 5 定义，Task 13 的 LocalSysOperation 实现该接口
