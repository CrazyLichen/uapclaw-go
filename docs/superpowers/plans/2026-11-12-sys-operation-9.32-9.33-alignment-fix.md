# 9.32/9.33 sys_operation Python 对齐修复计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 9.32/9.33 实现与 Python 的 13 项对齐差异，使 Go 的 sys_operation 完全对齐 Python `openjiuwen/core/sys_operation/`。

**Architecture:** 按严重程度排序：先修复结构冲突（sys_operation.go 重构），再修复类型定义（SandboxGatewayConfig 扁平化、SysOperationCard 补字段），再修复接口方法（ShellOperation 补 3 方法），最后补齐辅助函数和细节。

**Tech Stack:** Go 1.22+, os/exec, context.Context, sync.RWMutex, regexp

---

## File Structure

### 修改文件

| 文件 | 修改内容 |
|------|---------|
| `internal/agentcore/sys_operation/sys_operation.go` | 重构：删除所有与独立文件重复的定义，只保留 SysOperation 接口 + BaseSysOperation + LocalSysOperation + SysSubOperation + NewSysOperation 工厂 |
| `internal/agentcore/sys_operation/sys_operation_card.go` | SandboxGatewayConfig 扁平化、删除 SandboxIsolationConfig/SandboxLauncherConfig、SysOperationCard 补 IsolationPrefix/ContainerScope/CustomID、LocalWorkConfig 默认值修正 |
| `internal/agentcore/sys_operation/shell.go` | ShellOperation 接口补 WriteStdin/KillProcess/ListProcesses + BaseShellOperation 空实现 + ShellOptions 补 Cwd/Options 字段 |
| `internal/agentcore/sys_operation/shell_process_registry.go` | 补 Set/Get/Reset/ResolveShellSessionID |
| `internal/agentcore/sys_operation/tool_adapter.go` | 补 GetToolIDPrefix 方法 |
| `internal/agentcore/sys_operation/local/shell_operation.go` | 补齐 ~20 个 Shell 辅助函数 + WriteStdin/KillProcess/ListProcesses 实现 |
| `internal/agentcore/sys_operation/local/fs_operation.go` | 补 UploadFileStream/DownloadFileStream 实现 + WriteFile 补 prepend_newline/append_newline/encoding/permissions |
| `internal/agentcore/sys_operation/local/code_operation.go` | 补齐命令长度限制/force_file/encoding/-u/FileNotFoundError |
| `internal/agentcore/sys_operation/config.go` | 可能需要调整（如果 SandboxGatewayConfig 移到此处更合理） |
| `internal/agentcore/sys_operation/doc.go` | 更新文件目录 |

---

## Task 1: sys_operation.go 重构为纯主入口

**问题：** sys_operation.go 是大杂烩，包含与 fs.go/shell.go/code.go/base.go/config.go 重复的所有类型定义（接口、枚举、Base 实现、Result 类型、Options），且缺少 LocalSysOperation。

**Files:**
- Modify: `internal/agentcore/sys_operation/sys_operation.go`
- Test: `internal/agentcore/sys_operation/sys_operation_test.go`

- [x] **Step 1: 备份当前 sys_operation.go 内容**

Run: `cp internal/agentcore/sys_operation/sys_operation.go internal/agentcore/sys_operation/sys_operation.go.bak`

- [x] **Step 2: 重写 sys_operation.go**

只保留以下内容，删除所有与独立文件重复的定义：

```go
package sys_operation

import (
	"context"
	"fmt"
	"sync"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SysSubOperation 子操作公共接口，FsOperation/ShellOperation/CodeOperation 的交集。
type SysSubOperation interface {
	ListTools() []*tool.ToolCard
}

// SysOperation 系统操作主接口，编排文件系统、Shell、代码执行等子操作。
// 对齐 Python SysOperation：fs(), shell(), code(), isolation_key_template。
type SysOperation interface {
	Card() *SysOperationCard
	Fs() FsOperation
	Shell() ShellOperation
	Code() CodeOperation
	IsolationKeyTemplate() string
}

// BaseSysOperation SysOperation 的空操作桩实现。
type BaseSysOperation struct{}

// LocalSysOperation 本地系统操作实现。
// 对齐 Python SysOperation 的 __getattr__ 动态调度逻辑，
// 使用 OperationRegistry + instances map 实现 lazy 实例化。
type LocalSysOperation struct {
	card      *SysOperationCard
	instances map[string]SysSubOperation
	mu        sync.RWMutex
}

// ──────────────────────────── 全局变量 ────────────────────────────

var _ SysOperation = (*BaseSysOperation)(nil)
var _ SysOperation = (*LocalSysOperation)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSysOperation 系统操作工厂函数，根据 card.Mode 决定构造类型。
// 对齐 Python SysOperation(card) 构造函数的 mode 分支。
func NewSysOperation(card *SysOperationCard) SysOperation {
	if card == nil {
		card = NewSysOperationCard()
	}
	if card.Mode == OperationModeSandbox {
		// sandbox 模式预留，当前 fallback 到 local
		return NewLocalSysOperation(card)
	}
	return NewLocalSysOperation(card)
}

// NewLocalSysOperation 创建本地系统操作实例。
// 对齐 Python SysOperation.__init__：根据 mode 初始化 runConfig。
func NewLocalSysOperation(card *SysOperationCard) *LocalSysOperation {
	if card == nil {
		card = NewSysOperationCard()
	}
	return &LocalSysOperation{
		card:      card,
		instances: make(map[string]SysSubOperation),
	}
}

// Card 返回系统操作配置卡片
func (s *LocalSysOperation) Card() *SysOperationCard { return s.card }

// Fs 返回文件系统操作实例（lazy 实例化）
func (s *LocalSysOperation) Fs() FsOperation {
	op := s.getOperation("fs")
	if op == nil {
		return &BaseFsOperation{}
	}
	if fsOp, ok := op.(FsOperation); ok {
		return fsOp
	}
	return &BaseFsOperation{}
}

// Shell 返回 Shell 操作实例（lazy 实例化）
func (s *LocalSysOperation) Shell() ShellOperation {
	op := s.getOperation("shell")
	if op == nil {
		return &BaseShellOperation{}
	}
	if shellOp, ok := op.(ShellOperation); ok {
		return shellOp
	}
	return &BaseShellOperation{}
}

// Code 返回代码执行实例（lazy 实例化）
func (s *LocalSysOperation) Code() CodeOperation {
	op := s.getOperation("code")
	if op == nil {
		return &BaseCodeOperation{}
	}
	if codeOp, ok := op.(CodeOperation); ok {
		return codeOp
	}
	return &BaseCodeOperation{}
}

// IsolationKeyTemplate 返回隔离键模板
func (s *LocalSysOperation) IsolationKeyTemplate() string {
	return s.card.IsolationKeyTemplate()
}

// BaseSysOperation 空实现
func (b *BaseSysOperation) Card() *SysOperationCard          { return nil }
func (b *BaseSysOperation) Fs() FsOperation                  { return nil }
func (b *BaseSysOperation) Shell() ShellOperation            { return nil }
func (b *BaseSysOperation) Code() CodeOperation              { return nil }
func (b *BaseSysOperation) IsolationKeyTemplate() string     { return "" }

// ──────────────────────────── 非导出函数 ────────────────────────────

// getOperation 通用 lazy 实例化，从 OperationRegistry 查 OperationDef，调用 NewFunc 创建实例。
// 对齐 Python SysOperation._get_operation。
func (s *LocalSysOperation) getOperation(name string) SysSubOperation {
	s.mu.RLock()
	if inst, ok := s.instances[name]; ok {
		s.mu.RUnlock()
		return inst
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// 双重检查
	if inst, ok := s.instances[name]; ok {
		return inst
	}

	def, ok := GlobalRegistry.GetOperationInfo(name, s.card.Mode)
	if !ok {
		return nil
	}

	// 构造 runConfig
	var runConfig any
	if s.card.Mode == OperationModeLocal {
		if s.card.WorkConfig != nil {
			runConfig = s.card.WorkConfig
		} else {
			runConfig = NewLocalWorkConfig()
		}
	} else {
		if s.card.GatewayConfig != nil {
			runConfig = s.card.GatewayConfig
		} else {
			runConfig = NewSandboxGatewayConfig()
		}
	}

	inst := def.NewFunc(runConfig)
	s.instances[name] = inst
	return inst
}
```

- [x] **Step 3: 删除备份文件**

Run: `rm internal/agentcore/sys_operation/sys_operation.go.bak`

- [x] **Step 4: 运行编译检查**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/sys_operation/...`

- [x] **Step 5: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/sys_operation/... -v`

- [x] **Step 6: Commit**

---

## Task 2: SandboxGatewayConfig 扁平化 + 删除嵌套结构体

**问题：** Python 的 SandboxGatewayConfig 是扁平 6 字段，Go 是嵌套结构（Isolation + LauncherConfig + AuthHeaders + AuthQueryParams），且缺默认值、Timeout 不一致。

**Files:**
- Modify: `internal/agentcore/sys_operation/sys_operation_card.go`

- [x] **Step 1: 删除 SandboxIsolationConfig 和 SandboxLauncherConfig 结构体**

从 `sys_operation_card.go` 中删除这两个结构体定义。

- [x] **Step 2: 重写 SandboxGatewayConfig 为扁平结构**

```go
// SandboxGatewayConfig 沙箱网关配置。
// 对齐 Python SandboxGatewayConfig：gateway_url, gateway_token, launcher_type, sandbox_type, sandbox_image, timeout。
type SandboxGatewayConfig struct {
	// GatewayURL 网关地址
	GatewayURL string `yaml:"gateway_url" json:"gateway_url"`
	// GatewayToken 网关认证令牌
	GatewayToken string `yaml:"gateway_token" json:"gateway_token,omitempty"`
	// LauncherType 启动器类型
	LauncherType string `yaml:"launcher_type" json:"launcher_type"`
	// SandboxType 沙箱类型
	SandboxType string `yaml:"sandbox_type" json:"sandbox_type"`
	// SandboxImage 沙箱镜像
	SandboxImage string `yaml:"sandbox_image" json:"sandbox_image,omitempty"`
	// TimeoutSeconds 超时时间（秒）
	TimeoutSeconds float64 `yaml:"timeout_seconds" json:"timeout_seconds"`
}
```

- [x] **Step 3: 修正 NewSandboxGatewayConfig 默认值**

```go
func NewSandboxGatewayConfig() *SandboxGatewayConfig {
	return &SandboxGatewayConfig{
		GatewayURL:     "http://localhost:8080",
		LauncherType:   "pre_deploy",
		SandboxType:    "aio",
		TimeoutSeconds: 300.0,
	}
}
```

- [x] **Step 4: 检查所有引用 SandboxIsolationConfig/SandboxLauncherConfig 的代码并修复**

Run: `grep -rn "SandboxIsolationConfig\|SandboxLauncherConfig\|Isolation\b\|LauncherConfig\b" internal/agentcore/sys_operation/`

修复所有引用点（可能包括 tool_adapter.go 的 dispatch、shell_process_registry 等）。

- [x] **Step 5: 运行编译+测试**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/sys_operation/... && go test ./internal/agentcore/sys_operation/... -v`

- [x] **Step 6: Commit**

---

## Task 3: SysOperationCard 补充字段 + LocalWorkConfig 默认值修正

**问题：** SysOperationCard 缺少 IsolationPrefix/ContainerScope/CustomID；LocalWorkConfig 的 RestrictToSandbox 默认值应为 false，且缺 ShellAllowlist 默认列表。

**Files:**
- Modify: `internal/agentcore/sys_operation/sys_operation_card.go`

- [x] **Step 1: SysOperationCard 补充三个字段**

在 SysOperationCard 结构体中添加：
```go
type SysOperationCard struct {
	schema.BaseCard
	// Mode 操作模式
	Mode OperationMode `json:"mode"`
	// IsolationPrefix 隔离键前缀
	IsolationPrefix string `json:"isolation_prefix,omitempty"`
	// ContainerScope 容器作用域
	ContainerScope ContainerScope `json:"container_scope,omitempty"`
	// CustomID 自定义容器标识
	CustomID string `json:"custom_id,omitempty"`
	// WorkConfig 本地工作目录配置
	WorkConfig *LocalWorkConfig `json:"work_config,omitempty"`
	// GatewayConfig 沙箱网关配置
	GatewayConfig *SandboxGatewayConfig `json:"gateway_config,omitempty"`
	// isolationKeyTemplate 隔离键模板
	isolationKeyTemplate string
}
```

补充对应的 Option 函数：WithSysOpIsolationPrefix、WithSysOpContainerScope、WithSysOpCustomID。

- [x] **Step 2: 修正 LocalWorkConfig 默认值**

```go
func NewLocalWorkConfig() *LocalWorkConfig {
	return &LocalWorkConfig{
		ShellAllowlist: []string{
			"echo", "rg", "ls", "cat", "head", "tail", "find", "grep",
			"awk", "sed", "sort", "uniq", "wc", "diff", "curl", "wget",
			"git", "make", "cmake", "cargo", "go", "python3", "python",
			"node", "npm", "npx", "yarn", "pnpm", "pip", "pip3",
			"mv", "cp", "mkdir", "touch", "chmod", "chown",
			"tar", "gzip", "gunzip", "zip", "unzip",
			"docker", "kubectl", "terraform",
		},
		RestrictToSandbox: false,
	}
}
```

对齐 Python 的 `shell_allowlist` 默认值。

- [x] **Step 3: 运行编译+测试**

- [x] **Step 4: Commit**

---

## Task 4: ShellOperation 接口补充 WriteStdin/KillProcess/ListProcesses

**问题：** Python ShellOperation 有 write_stdin/kill_process/list_processes，Go 缺失。

**Files:**
- Modify: `internal/agentcore/sys_operation/shell.go`
- Modify: `internal/agentcore/sys_operation/local/shell_operation.go`

- [x] **Step 1: 在 ShellOperation 接口添加三个方法**

```go
type ShellOperation interface {
	ExecuteCmd(ctx context.Context, command string, opts ...ShellOption) (*result.ExecuteCmdResult, error)
	ExecuteCmdStream(ctx context.Context, command string, opts ...ShellOption) (<-chan result.ExecuteCmdStreamResult, error)
	ExecuteCmdBackground(ctx context.Context, command string, opts ...ShellOption) (*result.ExecuteCmdBackgroundResult, error)
	// WriteStdin 向后台进程写入标准输入
	WriteStdin(ctx context.Context, sessionID string, data string, opts ...ShellOption) (*result.ExecuteCmdResult, error)
	// KillProcess 终止指定后台进程
	KillProcess(ctx context.Context, sessionID string, opts ...ShellOption) (*result.ExecuteCmdResult, error)
	// ListProcesses 列出所有后台进程
	ListProcesses(ctx context.Context, opts ...ShellOption) (*result.ExecuteCmdResult, error)
	ListTools() []*tool.ToolCard
}
```

- [x] **Step 2: 在 BaseShellOperation 添加空实现**

```go
func (b *BaseShellOperation) WriteStdin(_ context.Context, _ string, _ string, _ ...ShellOption) (*result.ExecuteCmdResult, error) {
	return nil, fmt.Errorf("未实现: WriteStdin")
}
func (b *BaseShellOperation) KillProcess(_ context.Context, _ string, _ ...ShellOption) (*result.ExecuteCmdResult, error) {
	return nil, fmt.Errorf("未实现: KillProcess")
}
func (b *BaseShellOperation) ListProcesses(_ context.Context, _ ...ShellOption) (*result.ExecuteCmdResult, error) {
	return nil, fmt.Errorf("未实现: ListProcesses")
}
```

- [x] **Step 3: 在 local/shell_operation.go 添加本地实现**

- WriteStdin: 通过 ShellProcessRegistry 查找进程，向 stdin pipe 写入数据
- KillProcess: 通过 ShellProcessRegistry 查找并终止进程
- ListProcesses: 返回 ShellProcessRegistry 中当前所有进程信息

- [x] **Step 4: 更新 ListTools 添加三个方法的 ToolCard**

- [x] **Step 5: 更新 tool_adapter.go 的 dispatchShellMethod 添加三个 case**

- [x] **Step 6: 运行编译+测试**

- [x] **Step 7: Commit**

---

## Task 5: ShellProcessRegistry 补充 SessionID context 传递

**问题：** Python 有 set/reset/get/resolve_shell_session_id 四个 contextvars 函数，Go 缺失。

**Files:**
- Modify: `internal/agentcore/sys_operation/shell_process_registry.go`

- [x] **Step 1: 添加 context key 和四个函数**

```go
// shellSessionIDKey context key 用于传递 Shell session ID
type shellSessionIDKey struct{}

// SetShellSessionID 将 session ID 绑定到 context。
// 对齐 Python set_shell_session_id。
func SetShellSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, shellSessionIDKey{}, sessionID)
}

// GetShellSessionID 从 context 获取 session ID。
// 对齐 Python get_shell_session_id。
func GetShellSessionID(ctx context.Context) string {
	if v, ok := ctx.Value(shellSessionIDKey{}).(string); ok {
		return v
	}
	return ""
}

// ResetShellSessionID 重置 context 中的 session ID。
// 对齐 Python reset_shell_session_id（Go 中通过覆盖 WithValue 实现）。
func ResetShellSessionID(ctx context.Context) context.Context {
	return context.WithValue(ctx, shellSessionIDKey{}, "")
}

// ResolveShellSessionID 解析 session ID：先从 context 取，fallback 到 logger 的 trace_id。
// 对齐 Python resolve_shell_session_id。
func ResolveShellSessionID(ctx context.Context) string {
	sid := strings.TrimSpace(GetShellSessionID(ctx))
	if sid != "" {
		return sid
	}
	// fallback: 从 logger trace context 获取
	traceID := logger.GetTraceIDFromContext(ctx)
	traceID = strings.TrimSpace(traceID)
	if traceID != "" && traceID != "default_trace_id" {
		return traceID
	}
	return ""
}
```

- [x] **Step 2: 运行编译+测试**

- [x] **Step 3: Commit**

---

## Task 6: ToolAdapter 补充 GetToolIDPrefix

**Files:**
- Modify: `internal/agentcore/sys_operation/tool_adapter.go`

- [x] **Step 1: 添加 GetToolIDPrefix 方法**

```go
// GetToolIDPrefix 获取工具标识前缀。
// 对齐 Python SysOperationToolAdapter.get_tool_id_prefix（Deprecated 但保留）。
func (SysOperationToolAdapter) GetToolIDPrefix(sysOperationID any) any {
	switch v := sysOperationID.(type) {
	case string:
		return v + "."
	case []string:
		result := make([]string, len(v))
		for i, id := range v {
			result[i] = id + "."
		}
		return result
	default:
		return ""
	}
}
```

- [x] **Step 2: 运行编译+测试**

- [x] **Step 3: Commit**

---

## Task 7: Shell 辅助函数补齐（~20 个）

**问题：** Python shell_operation.py 有大量辅助函数（PowerShell 检测、Windows 路径归一化、Shell 可执行文件查找等），Go 缺失。resolveExecutionPlan 也需要完善。

**Files:**
- Modify: `internal/agentcore/sys_operation/local/shell_operation.go`

- [x] **Step 1: 补齐 PowerShell 检测常量和函数**

```go
// PowerShell 检测令牌，对齐 Python _POWERSHELL_TOKENS
var powershellTokens = []string{
	"powershell ", "powershell.exe ", "pwsh ", "pwsh.exe ",
	"get-childitem", "set-location", "remove-item", "test-path",
	"join-path", "select-object", "where-object", "foreach-object",
	"invoke-webrequest", "invoke-restmethod", "out-file", "start-process",
	"$env:", "$psversiontable", "$null", "$true", "$false",
}

var psVariablePattern = regexp.MustCompile(`(^|[\s;(])\$[A-Za-z_][A-Za-z0-9_]*`)
var powershellExecutablePattern = regexp.MustCompile(`^\s*(?:powershell(?:\.exe)?|pwsh(?:\.exe)?)\b`)
var powershellCommandArgPattern = regexp.MustCompile(`(?is)(?:^|\s)-(?:command|c)\s+(?P<script>.+)\s*$`)
var powershellCandidates = []string{"pwsh", "powershell", "powershell.exe"}
```

实现函数：`looksLikePowerShell(command)`, `availablePowerShell()`, `unwrapPowerShellCommand(command)`

- [x] **Step 2: 补齐 POSIX 检测常量和函数**

```go
var posixCommands = map[string]bool{
	"ls": true, "grep": true, "egrep": true, "fgrep": true, "cat": true,
	"head": true, "tail": true, "find": true, "rm": true, "cp": true,
	"mv": true, "touch": true, "chmod": true, "chown": true, "sed": true,
	"awk": true, "gawk": true, "cut": true, "sort": true, "uniq": true,
	"wc": true, "du": true, "df": true, "pwd": true, "which": true, "mkdir": true,
}
```

实现函数：`splitShellSegments(command)`, `segmentBaseCommand(segment)`, `looksLikePosix(command)`

- [x] **Step 3: 补齐 Shell 可执行文件查找函数**

实现函数：`isWSLBashPath(path)`, `gitBashCandidates()`, `availableGitBash()`, `availableBash()`, `availableSh()`

- [x] **Step 4: 补齐 Windows 路径归一化**

实现函数：`stripMatchingQuotes(value)`, `normalizeWindowsPathsForBash(command)`

- [x] **Step 5: 补齐进程追踪辅助函数**

```go
func trackShellProcess(ctx context.Context, proc *os.Process) string {
	sid := ResolveShellSessionID(ctx)
	if sid != "" {
		DefaultRegistry.Register(sid, proc)
	}
	return sid
}

func untrackShellProcess(sessionID string, proc *os.Process) {
	if sessionID != "" {
		DefaultRegistry.Unregister(sessionID, proc)
	}
}
```

- [x] **Step 6: 完善 resolveExecutionPlan**

补充完整的 PowerShell/WSL/Git Bash 检测逻辑，对齐 Python `_resolve_execution_plan` 的全部分支。

- [x] **Step 7: 运行编译+测试**

- [x] **Step 8: Commit**

---

## Task 8: FsOperation 细节补齐

**问题：** UploadFileStream/DownloadFileStream 未实现；WriteFile 缺 prepend_newline/append_newline/encoding/permissions 处理。

**Files:**
- Modify: `internal/agentcore/sys_operation/local/fs_operation.go`

- [x] **Step 1: 实现 UploadFileStream**

本地模式下实现分块拷贝，对齐 Python `upload_file_stream`。

- [x] **Step 2: 实现 DownloadFileStream**

本地模式下实现分块拷贝，对齐 Python `download_file_stream`。

- [x] **Step 3: WriteFile 补齐 prepend_newline/append_newline**

```go
// text 模式下处理换行符
if o.Mode != "bytes" {
	if o.PrependNewline != nil && *o.PrependNewline {
		dataBytes = append([]byte("\n"), dataBytes...)
	}
	if o.AppendNewline != nil && *o.AppendNewline {
		dataBytes = append(dataBytes, '\n')
	}
}
```

- [x] **Step 4: WriteFile 补齐 encoding**

使用 `encoding` 参数指定字符编码读取/写入（默认 utf-8）。

- [x] **Step 5: WriteFile 补齐 permissions**

解析 `o.Permissions`（如 "644"）设置文件权限。

- [x] **Step 6: 运行编译+测试**

- [x] **Step 7: Commit**

---

## Task 9: CodeOperation 细节补齐

**问题：** 命令长度限制不一致（Go 硬编码 4000，Python 8000/100000）、缺 force_file/encoding 选项、Python 执行缺 -u 参数、缺 FileNotFoundError 处理。

**Files:**
- Modify: `internal/agentcore/sys_operation/local/code_operation.go`

- [x] **Step 1: 修正命令长度限制**

```go
const (
	windowsCmdLimit = 8000
	unixCmdLimit    = 100000
)

func getDefaultCmdLimit() int {
	if runtime.GOOS == "windows" {
		return windowsCmdLimit
	}
	return unixCmdLimit
}
```

- [x] **Step 2: Python 执行加 -u 参数**

```go
case "python", "python3":
	if len(code) <= getDefaultCmdLimit() && !forceFile {
		return []string{"python3", "-u", "-c", code}, "", nil
	}
	// 临时文件模式
	return []string{"python3", "-u", tmpFile}, tmpFile, nil
```

- [x] **Step 3: 补充 force_file 选项支持**

从 `o.Options` 中读取 `force_file` 参数。

- [x] **Step 4: 补充 encoding 选项支持**

从 `o.Options` 中读取 `encoding` 参数，传递给 AsyncProcessHandler。

- [x] **Step 5: 补充 FileNotFoundError 友好处理**

在 ExecuteCode 中捕获 exec.ErrNotFound，返回友好提示：
```go
if errors.Is(err, exec.ErrNotFound) {
	return &result.ExecuteCodeResult{
		BaseResult: result.BuildOperationErrorResult(...,
			fmt.Sprintf("%s file not found error, please install and add it to your system environment variable PATH.", o.Language)),
	}, nil
}
```

- [x] **Step 6: 运行编译+测试**

- [x] **Step 7: Commit**

---

## Task 10: 更新 doc.go + IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/agentcore/sys_operation/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [x] **Step 1: 更新 doc.go 文件目录描述**

- [x] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

确认 9.32/9.33 状态为 ✅（已标记完成）。

- [x] **Step 3: Commit**

---

## Task 11: 全量编译 + 测试验证

- [x] **Step 1: 运行全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/sys_operation/...`

- [x] **Step 2: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/sys_operation/... -v -cover`

- [x] **Step 3: 运行受影响的上下游包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/... -v -count=1` （只跑 agentcore 相关包）

- [x] **Step 4: 检查覆盖率**

确认 sys_operation 包覆盖率 ≥ 85%。

- [x] **Step 5: 最终 Commit**

---

## Self-Review

### Spec Coverage

| 修复项 | 对应 Task |
|--------|----------|
| 问题 1: LocalWorkConfig 默认值 | Task 3 |
| 问题 2: SandboxGatewayConfig 默认值+Timeout | Task 2 |
| 问题 3: SysOperationCard 补字段 | Task 3 |
| 问题 4: 删除嵌套结构体+扁平化 | Task 2 |
| 问题 5: ShellOperation 补 3 方法 | Task 4 |
| 问题 6: ShellSessionID context | Task 5 |
| 问题 7: ToolAdapter.GetToolIDPrefix | Task 6 |
| 问题 8: sys_operation.go 重构 | Task 1 |
| 问题 9: NewSysOperation 工厂函数 | Task 1 |
| 问题 10: Shell 辅助函数补齐 | Task 7 |
| 问题 11: FsOperation Stream 实现 | Task 8 |
| 问题 12: FsOperation WriteFile 细节 | Task 8 |
| 问题 13: CodeOperation 细节 | Task 9 |

### Placeholder Scan

无 TBD/TODO。所有 Task 包含具体代码和修改内容。

### Type Consistency

- Task 1 定义 LocalSysOperation，引用 Task 2 的 SandboxGatewayConfig 和 Task 3 的 SysOperationCard
- Task 4 扩展 ShellOperation 接口，Task 7 在 local/ 中实现
- 执行顺序：Task 1 → Task 2 → Task 3 → Task 4 → Task 5 → Task 6 → Task 7 → Task 8 → Task 9 → Task 10 → Task 11
