# 9.32-9.33 SysOperation 接口 + LocalSysOperation 完整设计方案

## 1. 概述

### 1.1 目标

完整对齐 Python `openjiuwen/core/sys_operation/` 的文件组织与逻辑，实现：
- **9.32**：SysOperation 接口（系统操作抽象层补全）
- **9.33**：LocalSysOperation（本地执行实现）

sandbox/（9.34）延后，本次不涉及。

### 1.2 流程位置

```
Agent 启动
  │
  ▼
ResourceManager.AddSysOperation(card)
  │
  ├── 1. 创建 SysOperation 实例（9.32 调度器，lazy 实例化子操作）
  │
  └── 2. registerSysOperationTools()
        │
        └── SysOperationToolAdapter.ExtractTools(card, instance)
              │
              └── 遍历 fs/shell/code 子操作：
                    ListTools() → ToolCard 列表
                    NewTool(fn, WithToolCard(card)) → tool.Tool
                    注册到 ResourceMgr

Agent 运行
  │
  ├── Context Engine: writeOffloadToFile → sysOp.Fs().WriteFile()  （第 1 层程序化调用）
  └── Harness 工具 (9.38-49): BashTool → sysOp.Shell().ExecuteCmd() （第 2 层包装调用）
```

### 1.3 已实现 vs 本次新建/修改

| 组件 | 已有 | 本次动作 |
|---|---|---|
| CWD (`cwd/cwd.go`) | ✅ 完整 | 不动 |
| ShellProcessRegistry | ✅ 完整 | 不动 |
| SysOperation 接口 | ⚠️ 缺 stream/background/upload/download | **补全** |
| SysOperationCard | ✅ 基本对齐 | 小幅补全（SandboxRoot → []string） |
| Result 类型 | ❌ 仅简化版 | **重建** |
| OperationRegistry | ❌ 无 | **新建** |
| BaseOperation | ❌ 无 | **新建** |
| SysOperation 调度器 | ❌ 无 | **新建** |
| SysOperationToolAdapter | ❌ 无 | **新建** |
| Local Shell/FS/Code | ❌ 无 | **新建** |
| AsyncProcessHandler (utils) | ❌ 无 | **新建** |
| ResourceManager 回填 | ❌ 占位桩 | **回填** |
| builder/factory 回填 | ❌ 占位桩 | **回填** |
| ContextEngine SysOperation 注入 | ❌ 断裂 | **回填** |

---

## 2. 文件组织（一比一复刻 Python）

```
internal/agentcore/sys_operation/
├── doc.go                              # 包文档（更新）
├── base.go                             # NEW: BaseOperation + OperationMode（从 sys_operation.go 拆出）
├── config.go                           # NEW: LocalWorkConfig/SandboxGatewayConfig（从 sys_operation_card.go 拆出）
├── sys_operation.go                    # REWRITE: SysOperation 调度器（lazy 实例化 + 动态调度）
├── sys_operation_card.go               # MODIFY: SysOperationCard + ToolIdProxy + generateIsolationKeyTemplate
├── registry.go                         # NEW: OperationRegistry + OperationDef + @operation 装饰器等价
├── shell.go                            # NEW: BaseShellOperation + ShellType（从 sys_operation.go 拆出）
├── fs.go                               # NEW: BaseFsOperation + 常量（从 sys_operation.go 拆出）
├── code.go                             # NEW: BaseCodeOperation（从 sys_operation.go 拆出）
├── cwd/                                # ✅ 已有，不动
│   ├── cwd.go
│   └── cwd_test.go
├── tool_adapter.go                     # NEW: SysOperationToolAdapter
├── shell_process_registry.go           # ✅ 已有，不动
├── shell_process_registry_test.go      # ✅ 已有，不动
├── result/                             # NEW 子包
│   ├── base_result.go                  # BaseResult[T] + BuildOperationErrorResult
│   ├── shell_operation_result.go       # ExecuteCmdData/Result + ChunkData/StreamResult + BackgroundData/Result
│   ├── fs_operation_result.go          # ReadFileData/Result + ChunkData/StreamResult + WriteFile + Upload/Download + FileSystemItem + FileSystemData + SearchFilesData/Result
│   └── code_operation_result.go        # ExecuteCodeData/Result + ChunkData/StreamResult
├── local/                              # NEW 子包
│   ├── shell_operation.go              # ShellOperation（本地 Shell 执行）
│   ├── fs_operation.go                 # FsOperation（本地文件系统操作）
│   ├── code_operation.go               # CodeOperation（本地代码执行）
│   └── utils.go                        # AsyncProcessHandler + OperationUtils + StreamEvent/StreamEventType
└── sys_operation_test.go               # 更新测试
```

### 2.1 包间依赖（无循环）

```
sys_operation (根包)
  → sys_operation/result    （shell.py/fs.py/code.py 引用 result 类型）
  → sys_operation/cwd       （本地操作的 CWD 解析）

sys_operation/local
  → sys_operation           （import base/shell/fs/code/registry）
  → sys_operation/result    （import 具体结果类型）
  → sys_operation/cwd       （CWD 解析）

sys_operation/result
  → 无外部依赖              （纯数据类型）
```

sandbox/ 延后，sys_operation.go 中对 SandboxRunConfig 的引用用 `// ⤵️ 9.34` 占位。

---

## 3. 核心类型设计

### 3.1 result/ — Result/Data 类型（对齐 Python result/）

Go 没有 Python 的 `BaseResult[Generic[T]]`，用具体 struct 代替：

```go
// result/base_result.go

// BaseResult 操作结果基类
type BaseResult struct {
    // Code 状态码：0 = 成功，非 0 = 失败
    Code int
    // Message 状态消息
    Message string
}

// BuildOperationErrorResult 构造标准化错误结果
func BuildOperationErrorResult(errorCode int, errMsg string, data any) BaseResult
```

每个具体 Result 组合 BaseResult + 具体 Data：

```go
// result/shell_operation_result.go

// ExecuteCmdData 执行命令结果数据
type ExecuteCmdData struct {
    Command  string `json:"command"`
    Cwd      string `json:"cwd"`
    ExitCode *int   `json:"exit_code"`
    Stdout   string `json:"stdout"`
    Stderr   string `json:"stderr"`
}

// ExecuteCmdResult 执行命令结果
type ExecuteCmdResult struct {
    BaseResult
    Data *ExecuteCmdData `json:"data"`
}

// ExecuteCmdChunkData 执行命令流式块数据
type ExecuteCmdChunkData struct {
    Text       string  `json:"text"`
    Type       *string `json:"type"`       // "stdout" | "stderr"
    ChunkIndex int     `json:"chunk_index"`
    ExitCode   *int    `json:"exit_code"`
    Metadata   map[string]any `json:"metadata,omitempty"`
}

// ExecuteCmdStreamResult 执行命令流式结果
type ExecuteCmdStreamResult struct {
    BaseResult
    Data *ExecuteCmdChunkData `json:"data"`
}

// ExecuteCmdBackgroundData 后台执行命令结果数据
type ExecuteCmdBackgroundData struct {
    Command string `json:"command"`
    Cwd     string `json:"cwd"`
    Pid     *int   `json:"pid"`
}

// ExecuteCmdBackgroundResult 后台执行命令结果
type ExecuteCmdBackgroundResult struct {
    BaseResult
    Data *ExecuteCmdBackgroundData `json:"data"`
}
```

FS 和 Code 的 Result/Data 类型同理，完整字段对齐 Python（见附录 A）。

### 3.2 base.go — BaseOperation

```go
// base.go

// BaseOperation 操作基类，所有子操作（fs/shell/code）的公共父类
type BaseOperation struct {
    // name 操作名称（如 "fs", "shell", "code"）
    name string
    // mode 操作模式
    mode OperationMode
    // description 操作描述
    description string
    // runConfig 运行配置（LocalWorkConfig 或 SandboxGatewayConfig）
    runConfig any
}

// NewBaseOperation 创建 BaseOperation 实例
func NewBaseOperation(name string, mode OperationMode, description string, runConfig any) BaseOperation

// Name 返回操作名称
func (b *BaseOperation) Name() string

// Mode 返回操作模式
func (b *BaseOperation) Mode() OperationMode

// ListTools 返回工具卡片列表（由子类实现）
func (b *BaseOperation) ListTools() []*tool.ToolCard

// CreateSysOperationEvent 创建系统操作日志事件
func (b *BaseOperation) CreateSysOperationEvent(...) SysOperationEvent
```

### 3.3 registry.go — OperationRegistry

```go
// registry.go

// OperationDef 操作定义，包含类型信息和工厂方法
type OperationDef struct {
    // NewFunc 工厂函数：从 runConfig 创建 BaseOperation 实例
    NewFunc func(runConfig any) BaseOperation
    // Name 操作名称
    Name string
    // Mode 操作模式
    Mode OperationMode
    // Description 操作描述
    Description string
}

// OperationRegistry 操作注册表
type OperationRegistry struct {
    // repository mode → name → OperationDef
    repository map[OperationMode]map[string]OperationDef
}

// 全局实例
var GlobalRegistry = NewOperationRegistry()

// Register 注册操作定义
func (r *OperationRegistry) Register(def OperationDef)

// GetOperationInfo 获取操作定义
func (r *OperationRegistry) GetOperationInfo(name string, mode OperationMode) (OperationDef, bool)

// GetSupportedOperations 获取指定模式下所有已注册操作名称
func (r *OperationRegistry) GetSupportedOperations(mode OperationMode) []string
```

Go 没有 Python 的 `@operation` 装饰器 + 包扫描自动发现。改用 `init()` 显式注册：

```go
// local/shell_operation.go
func init() {
    sysop.GlobalRegistry.Register(sysop.OperationDef{
        Name:        "shell",
        Mode:        sysop.OperationModeLocal,
        Description: "local shell operation",
        NewFunc:     newShellOperation,
    })
}
```

### 3.4 sys_operation.go — SysOperation 调度器

```go
// sys_operation.go

// SysOperation 系统操作主接口（保持现有接口不变）
type SysOperation interface {
    Card() *SysOperationCard
    Fs() FsOperation
    Shell() ShellOperation
    Code() CodeOperation
    IsolationKeyTemplate() string
}

// LocalSysOperation 本地系统操作实现
type LocalSysOperation struct {
    // card 配置卡片
    card *SysOperationCard
    // instances 懒实例化的子操作缓存 name → BaseOperation
    instances map[string]BaseOperation
    // mu 保护 instances
    mu sync.RWMutex
}

// NewLocalSysOperation 创建本地系统操作
func NewLocalSysOperation(card *SysOperationCard) *LocalSysOperation

// Fs 返回文件系统操作实例（lazy 实例化）
func (s *LocalSysOperation) Fs() FsOperation

// Shell 返回 Shell 操作实例（lazy 实例化）
func (s *LocalSysOperation) Shell() ShellOperation

// Code 返回代码执行实例（lazy 实例化）
func (s *LocalSysOperation) Code() CodeOperation

// getOperation 通用 lazy 实例化逻辑
func (s *LocalSysOperation) getOperation(name string) (BaseOperation, bool)
```

### 3.5 shell.go / fs.go / code.go — 抽象基类

从现有 `sys_operation.go` 拆出，补全方法签名：

```go
// shell.go

// ShellOperation Shell 操作接口（补全 stream + background）
type ShellOperation interface {
    ExecuteCmd(ctx context.Context, command string, opts ...ShellOption) (*result.ExecuteCmdResult, error)
    ExecuteCmdStream(ctx context.Context, command string, opts ...ShellOption) (<-chan result.ExecuteCmdStreamResult, error)
    ExecuteCmdBackground(ctx context.Context, command string, opts ...ShellOption) (*result.ExecuteCmdBackgroundResult, error)
    ListTools() []*tool.ToolCard
}

// BaseShellOperation Shell 操作基类
type BaseShellOperation struct {
    BaseOperation
}
```

```go
// fs.go

// FsOperation 文件系统操作接口（补全 stream + upload/download）
type FsOperation interface {
    ReadFile(ctx context.Context, path string, opts ...FsOption) (*result.ReadFileResult, error)
    ReadFileStream(ctx context.Context, path string, opts ...FsOption) (<-chan result.ReadFileStreamResult, error)
    WriteFile(ctx context.Context, path string, content string, opts ...FsOption) (*result.WriteFileResult, error)
    UploadFile(ctx context.Context, localPath string, targetPath string, opts ...FsOption) (*result.UploadFileResult, error)
    UploadFileStream(ctx context.Context, localPath string, targetPath string, opts ...FsOption) (<-chan result.UploadFileStreamResult, error)
    DownloadFile(ctx context.Context, sourcePath string, localPath string, opts ...FsOption) (*result.DownloadFileResult, error)
    DownloadFileStream(ctx context.Context, sourcePath string, localPath string, opts ...FsOption) (<-chan result.DownloadFileStreamResult, error)
    ListFiles(ctx context.Context, path string, opts ...FsOption) (*result.ListFilesResult, error)
    ListDirectories(ctx context.Context, path string, opts ...FsOption) (*result.ListDirsResult, error)
    SearchFiles(ctx context.Context, path string, pattern string, opts ...FsOption) (*result.SearchFilesResult, error)
    ListTools() []*tool.ToolCard
}
```

```go
// code.go

// CodeOperation 代码执行接口（补全 stream）
type CodeOperation interface {
    ExecuteCode(ctx context.Context, code string, opts ...CodeOption) (*result.ExecuteCodeResult, error)
    ExecuteCodeStream(ctx context.Context, code string, opts ...CodeOption) (<-chan result.ExecuteCodeStreamResult, error)
    ListTools() []*tool.ToolCard
}
```

**Go 流式返回**：Python 用 `AsyncIterator[T]`，Go 用 `<-chan T`（和项目已有的 `StreamFunction` 模式一致）。

### 3.5.1 session_id 通过 context.Context 传递

Python 用 `ContextVar` 隐式传递 `shell_session_id`，Go 对等方案是用 `context.Context`：

```go
// shell_process_registry.go 新增

type sessionIDKey struct{}

// WithSessionID 注入 session_id 到 context
func WithSessionID(ctx context.Context, sessionID string) context.Context {
    return context.WithValue(ctx, sessionIDKey{}, sessionID)
}

// SessionIDFromCtx 从 context 获取 session_id
func SessionIDFromCtx(ctx context.Context) string {
    if v, ok := ctx.Value(sessionIDKey{}).(string); ok {
        return v
    }
    return ""
}

// ResolveShellSessionID 解析当前 Shell 会话 ID（context → logger fallback）
func ResolveShellSessionID(ctx context.Context) string {
    sid := SessionIDFromCtx(ctx)
    if sid != "" {
        return sid
    }
    // fallback: 从 logger context 获取
    return logger.GetSessionID(ctx)
}
```

和 CWD 模式完全一致：`WithCwdState` / `CwdStateFromCtx` / `GetCwd` — 都通过 `context.Context` 隐式传递，不需要方法签名显式声明参数。

DeepAgent 调用工具时，session_id 已通过 `WithSessionID` 注入到 ctx，local/shell_operation 内部直接 `ResolveShellSessionID(ctx)` 获取。

### 3.6 Options 补全

```go
// shell.go 中 ShellOptions 补全
type ShellOptions struct {
    Cwd         string
    Timeout     int
    Environment map[string]string
    ShellType   ShellType
    Options     map[string]any   // NEW: 对齐 Python options
}

// fs.go 中 FsOptions 补全
type FsOptions struct {
    Mode             string   // "text" | "bytes"
    Head             int
    Tail             int
    LineRange        [2]int   // NEW: (start, end)
    Encoding         string
    ChunkSize        int
    Options          map[string]any  // NEW
    // Write 专用
    Content          string   // WriteFile 时使用
    PrependNewline   *bool    // NEW: 默认 true
    AppendNewline    *bool    // NEW: 默认 false
    Append           bool     // NEW
    CreateIfNotExist bool     // NEW: 默认 true
    Permissions      string   // NEW: 默认 "644"
    // Upload/Download 专用
    LocalPath        string
    TargetPath       string
    SourcePath       string
    Overwrite        bool
    CreateParentDirs bool
    PreservePerms    bool
    // List 专用
    Recursive       bool
    MaxDepth        int
    SortBy          string   // "name" | "modified_time" | "size"
    SortDescending  bool
    FileTypes       []string
    ExcludePatterns []string
}

// code.go 中 CodeOptions 补全
type CodeOptions struct {
    Language    string
    Timeout     int
    Environment map[string]string
    Cwd         string
    Options     map[string]any  // NEW
}
```

### 3.7 local/ — 本地实现

#### local/utils.go — AsyncProcessHandler

```go
// local/utils.go

// StreamEventType 流式事件类型枚举
type StreamEventType int
const (
    StreamEventTypeStdout StreamEventType = iota
    StreamEventTypeStderr
    StreamEventTypeExit
    StreamEventTypeError
)

// StreamEvent 流式事件
type StreamEvent struct {
    Type      StreamEventType
    Data      string  // stdout/stderr 文本 或 error 消息
    ExitCode  int     // 仅 Exit 事件有效
    Timestamp time.Time
}

// AsyncProcessHandler 异步进程处理器
type AsyncProcessHandler struct {
    process        *os.Process
    chunkSize      int
    encoding       string
    overallTimeout int
    isExecuted     bool
}

// NewAsyncProcessHandler 创建处理器
func NewAsyncProcessHandler(process *os.Process, chunkSize int, encoding string, timeout int) *AsyncProcessHandler

// Invoke 一次性执行，收集完整输出
func (h *AsyncProcessHandler) Invoke(ctx context.Context) (*InvokeData, error)

// Stream 流式执行，通过 channel 逐块返回
func (h *AsyncProcessHandler) Stream(ctx context.Context) (<-chan StreamEvent, error)

// Background 后台执行，等待 grace 秒检测早期失败
func (h *AsyncProcessHandler) Background(grace float64) (pid int, err error)

// InvokeData 一次性执行结果
type InvokeData struct {
    Stdout   string
    Stderr   string
    ExitCode int
    Exception error
}

// OperationUtils 操作工具类
type OperationUtils struct{}

// PrepareEnvironment 合并环境变量
func (OperationUtils) PrepareEnvironment(customEnv map[string]string) map[string]string

// CreateTmpFile 创建临时文件
func (OperationUtils) CreateTmpFile(content string, suffix string) (string, error)

// DeleteTmpFile 删除临时文件
func (OperationUtils) DeleteTmpFile(path string) error
```

#### local/shell_operation.go — 本地 Shell 操作

一比一复刻 Python `local/shell_operation.py`，核心方法：

```go
// local/shell_operation.go

// ShellOperation 本地 Shell 操作
type ShellOperation struct {
    sysop.BaseShellOperation
    // dangerousPatterns 危险命令正则
    dangerousPatterns []DangerousPattern
    // tuiCommandPatterns TUI 命令检测正则
    tuiCommandPatterns []TUICommandPattern
}

// DangerousPattern 危险命令模式
type DangerousPattern struct {
    Pattern *regexp.Regexp
    Label   string
}

// TUICommandPattern TUI 命令模式
type TUICommandPattern struct {
    Pattern  *regexp.Regexp
    Desc     string
    AutoEnv  map[string]string  // 自动注入的环境变量
}

// ExecuteCmd 执行 Shell 命令
func (s *ShellOperation) ExecuteCmd(ctx context.Context, command string, opts ...sysop.ShellOption) (*result.ExecuteCmdResult, error)

// ExecuteCmdStream 流式执行 Shell 命令
func (s *ShellOperation) ExecuteCmdStream(ctx context.Context, command string, opts ...sysop.ShellOption) (<-chan result.ExecuteCmdStreamResult, error)

// ExecuteCmdBackground 后台执行 Shell 命令
func (s *ShellOperation) ExecuteCmdBackground(ctx context.Context, command string, opts ...sysop.ShellOption) (*result.ExecuteCmdBackgroundResult, error)

// ListTools 返回 Shell 操作的工具卡片列表（硬编码）
func (s *ShellOperation) ListTools() []*tool.ToolCard
```

安全机制一比一复刻 Python：
- `_checkCommandSafety`：危险模式检测（rm -rf, shutdown, reboot 等）
- `_checkAllowlist`：白名单校验
- `_detectAndMitigateTUI`：TUI 命令检测（playwright test, vitest --watch 等）
- `_resolveExecutionPlan`：Shell 类型解析（Auto/PowerShell/Bash/Sh/Cmd）
- `_createSubprocess`：子进程创建（进程组隔离 + start_new_session）
- `_resolveCwd`：CWD 解析（调用 `cwd.GetCwd(ctx)`，从 context 获取，不走显式传参）
- Shell 进程跟踪：通过 `ResolveShellSessionID(ctx)` 获取 session_id，自动 register/unregister ShellProcessRegistry

**ShellProcessRegistry 的用途**：每次 fork 子进程时自动注册，执行完/超时/取消时自动注销。当用户中断 session 时，`KillShellProcessesForSession(sessionID)` 批量 kill 所有该 session 的子进程，避免孤儿进程。

#### local/fs_operation.go — 本地文件系统操作

一比一复刻 Python `local/fs_operation.py`，核心方法：

```go
// local/fs_operation.go

// FsOperation 本地文件系统操作
type FsOperation struct {
    sysop.BaseFsOperation
}

// ReadFile 读取文件
func (f *FsOperation) ReadFile(ctx context.Context, path string, opts ...sysop.FsOption) (*result.ReadFileResult, error)

// ReadFileStream 流式读取文件
func (f *FsOperation) ReadFileStream(ctx context.Context, path string, opts ...sysop.FsOption) (<-chan result.ReadFileStreamResult, error)

// WriteFile 写入文件
func (f *FsOperation) WriteFile(ctx context.Context, path string, content string, opts ...sysop.FsOption) (*result.WriteFileResult, error)

// UploadFile 上传文件
func (f *FsOperation) UploadFile(ctx context.Context, localPath string, targetPath string, opts ...sysop.FsOption) (*result.UploadFileResult, error)

// UploadFileStream 流式上传文件
func (f *FsOperation) UploadFileStream(ctx context.Context, localPath string, targetPath string, opts ...sysop.FsOption) (<-chan result.UploadFileStreamResult, error)

// DownloadFile 下载文件
func (f *FsOperation) DownloadFile(ctx context.Context, sourcePath string, localPath string, opts ...sysop.FsOption) (*result.DownloadFileResult, error)

// DownloadFileStream 流式下载文件
func (f *FsOperation) DownloadFileStream(ctx context.Context, sourcePath string, localPath string, opts ...sysop.FsOption) (<-chan result.DownloadFileStreamResult, error)

// ListFiles 列出文件
func (f *FsOperation) ListFiles(ctx context.Context, path string, opts ...sysop.FsOption) (*result.ListFilesResult, error)

// ListDirectories 列出目录
func (f *FsOperation) ListDirectories(ctx context.Context, path string, opts ...sysop.FsOption) (*result.ListDirsResult, error)

// SearchFiles 搜索文件
func (f *FsOperation) SearchFiles(ctx context.Context, path string, pattern string, opts ...sysop.FsOption) (*result.SearchFilesResult, error)

// ListTools 返回文件系统操作的工具卡片列表（硬编码）
func (f *FsOperation) ListTools() []*tool.ToolCard
```

安全机制：
- sandbox_root 路径校验（restrict_to_sandbox）
- RWLock 并发读写保护（一比一复刻 Python 的 `_fs_lock`）
- 编码处理（UTF-8 默认 + fallback）
- 行范围读取（head/tail/line_range）

#### local/code_operation.go — 本地代码执行

一比一复刻 Python `local/code_operation.py`：

```go
// local/code_operation.go

// CodeOperation 本地代码执行
type CodeOperation struct {
    sysop.BaseCodeOperation
}

// ExecuteCode 执行代码
func (c *CodeOperation) ExecuteCode(ctx context.Context, code string, opts ...sysop.CodeOption) (*result.ExecuteCodeResult, error)

// ExecuteCodeStream 流式执行代码
func (c *CodeOperation) ExecuteCodeStream(ctx context.Context, code string, opts ...sysop.CodeOption) (<-chan result.ExecuteCodeStreamResult, error)

// ListTools 返回代码执行的工具卡片列表（硬编码）
func (c *CodeOperation) ListTools() []*tool.ToolCard
```

支持 python / javascript 两种语言，长代码自动写临时文件执行。

### 3.8 tool_adapter.go — SysOperationToolAdapter

```go
// tool_adapter.go

// ToolAdapterEntry 工具适配条目
type ToolAdapterEntry struct {
    // ToolID 工具标识（格式：{cardID}.{opType}.{methodName}）
    ToolID string
    // Tool 工具实例
    Tool tool.Tool
}

// SysOperationToolAdapter SysOperation → tool.Tool 适配器
type SysOperationToolAdapter struct{}

// ExtractTools 从 SysOperation 提取所有方法包装为 tool.Tool
func (SysOperationToolAdapter) ExtractTools(
    card *SysOperationCard,
    instance SysOperation,
    language string,
    agentID string,
) ([]ToolAdapterEntry, error)
```

实现逻辑：
1. 遍历 `OperationRegistry.GetSupportedOperations(card.Mode)` 获取 op_type 列表
2. 对每个 op_type，获取子操作实例（`instance.Fs()` / `instance.Shell()` / `instance.Code()`）
3. 调用 `sub_op.ListTools()` 获取 ToolCard 列表
4. 对每个 ToolCard，用 `NewTool(fn, WithToolCard(card))` 包装为 `tool.Tool`
   - `fn` 是绑定到具体子操作方法的闭包
5. 返回 `[]ToolAdapterEntry`

### 3.9 ListTools 硬编码方案

每个 BaseXxxOperation 的 `ListTools()` 方法内硬编码构建 ToolCard，description 一比一复刻 Python 方法的英文 docstring 原文，inputParams 从 Python 方法签名一比一复刻。

示例（shell ListTools）：

```go
func (s *ShellOperation) ListTools() []*tool.ToolCard {
    return []*tool.ToolCard{
        newToolCard("execute_cmd",
            "Asynchronously execute a command(shell mode only).",
            newExecuteCmdParams()),
        newToolCard("execute_cmd_stream",
            "Asynchronously execute a command streaming(shell mode only).",
            newExecuteCmdStreamParams()),
        newToolCard("execute_cmd_background",
            "Launch a command in the background and return immediately with its PID.",
            newExecuteCmdBackgroundParams()),
    }
}

func newExecuteCmdParams() []*schema.Param {
    return []*schema.Param{
        {Name: "command", Type: schema.ParamTypeString, Required: true,
            Description: "Command to execute."},
        {Name: "cwd", Type: schema.ParamTypeString, Nullable: true,
            Description: "Working directory for command execution (default: current directory)."},
        {Name: "timeout", Type: schema.ParamTypeInteger,
            Description: "Command execution timeout in seconds (default: 300 seconds)."},
        // ... 完整对齐 Python 方法签名
    }
}
```

---

## 4. 回填点

### 4.1 ResourceManager 回填

**`registerSysOperationTools`**（resource_manager.go line 1015）：
```go
func (m *ResourceMgr) registerSysOperationTools(cardID string, instance sysop.SysOperation, tag Tag) {
    entries, err := sysop.SysOperationToolAdapter{}.ExtractTools(card, instance, "en", "")
    if err != nil { ... }
    var toolIDs []string
    for _, entry := range entries {
        m.innerAddResource(entry.ToolID, entry.Tool, entry.Tool.Card(), tag, "tool")
        toolIDs = append(toolIDs, entry.ToolID)
    }
    m.resourceRegistry.Tool().AddSysOperationTools(cardID, toolIDs)
}
```

**`AddSysOperation`** 修改：注册成功后调用 `registerSysOperationTools`

**`RemoveSysOperation`** 回填：移除后清理关联工具

**`GetSysOpToolCards`** 回填：通过 OperationRegistry + ListTools 获取 ToolCard

### 4.2 builder.go 回填

**`createSysOperation`**（harness_config/builder.go）：
```go
func createSysOperation(card *sasc.AgentCard) (sysop.SysOperation, error) {
    sysOpCard := sysop.NewSysOperationCard(sysop.WithSysOpMode(sysop.OperationModeLocal))
    return sysop.NewLocalSysOperation(sysOpCard), nil
}
```

### 4.3 factory.go 回填

**`buildSysOperation`**（factory.go line 392）：
- 将 `return &sysop.BaseSysOperation{}` 替换为 `return sysop.NewLocalSysOperation(card)`

### 4.4 ContextEngine SysOperation 注入回填

**`setupTaskLoop`**（deep_agent.go）：
```go
// 当前（断裂）：
ce := context_engine.NewContextEngine(ceschema.NewContextEngineConfig())

// 修改为：
ce := context_engine.NewContextEngine(
    ceschema.NewContextEngineConfig(),
    iface.WithEngineSysOperation(d.config.SysOperation),
)
```

注：`writeOffloadToFile` 本身不需要修改——它已经优先调 `sysOp.Fs().WriteFile()` 并 fallback 到 `os.WriteFile()`。路径解析（CWD + sandbox 校验）是 `local/fs_operation.go` 内部 `_resolve_path` 的职责，9.33 实现后自动生效。当前断裂只是因为 `sysOperation` 为 nil（未注入），回填注入点即可。

### 4.5 SysOperationCard.SandboxRoot 类型修正

Python `LocalWorkConfig.sandbox_root` 是 `Optional[List[str]]`，Go 当前是 `string`。
修改为 `[]string` 以对齐。

---

## 5. 日志同步

对齐 Python 中 `sys_operation_logger` 的所有日志调用：

- 组件常量：`logger.ComponentAgentCore`
- 级别映射：`logger.debug` → `logger.Debug()`, `logger.info` → `logger.Info()`, `logger.warning` → `logger.Warn()`, `logger.error` → `logger.Error()`
- 事件类型：`SYS_OP_START`, `SYS_OP_END`, `SYS_OP_STREAM`, `SYS_OP_ERROR`（对齐 Python `LogEventType`）
- 每个方法入口记录 START，成功记录 END，异常记录 ERROR + `.Err(err)`
- 流式操作逐块记录 Debug 级别 stream 事件

---

## 6. 测试策略

### 6.1 result/ 包

- 所有 Result/Data 类型的 JSON 序列化/反序列化测试
- BuildOperationErrorResult 构造测试

### 6.2 base.go + registry.go

- BaseOperation 创建/属性访问测试
- OperationRegistry Register/Get/SupportedOperations 测试
- 重复注册幂等性测试

### 6.3 local/utils.go

- AsyncProcessHandler.Invoke 正常执行 + 超时测试
- AsyncProcessHandler.Stream 流式输出测试
- AsyncProcessHandler.Background 后台启动 + 早期失败检测
- OperationUtils PrepareEnvironment/CreateTmpFile/DeleteTmpFile

### 6.4 local/shell_operation.go

- ExecuteCmd 基本命令执行（echo, ls）
- ExecuteCmd 空命令拒绝
- ExecuteCmd 危险命令拒绝
- ExecuteCmd 超时处理
- ExecuteCmdStream 流式输出
- ExecuteCmdBackground 后台启动
- checkCommandSafety / checkAllowlist 单元测试
- resolveExecutionPlan 各 ShellType 路径测试

### 6.5 local/fs_operation.go

- ReadFile 文本读取 + head/tail/line_range
- WriteFile 写入 + append
- ListFiles / ListDirectories / SearchFiles
- sandbox_root 路径校验
- UploadFile / DownloadFile（本地=文件拷贝）

### 6.6 local/code_operation.go

- ExecuteCode python + javascript
- ExecuteCode 长代码临时文件执行
- ExecuteCodeStream 流式

### 6.7 tool_adapter.go

- ExtractTools 提取正确数量的工具
- ToolID 格式验证

### 6.8 回填验证

- AddSysOperation 后通过 GetTool 获取注册的工具
- ContextEngine writeOffloadToFile 使用 LocalSysOperation 写文件
- builder/factory 创建的 SysOperation 实例类型正确

### 6.9 Build tag

- 不需要 — 所有测试可 mock 或使用本地文件系统（`t.TempDir()`）

---

## 7. 不在本次范围

| 内容 | 原因 | 对应章节 |
|---|---|---|
| sandbox/ 子包 | 用户确认延后 | 9.34 |
| SandboxRunConfig | sandbox/ 的一部分 | 9.34 |
| JiuwenBoxProvider / AioProvider | sandbox 扩展 | 9.36-9.37 |
| Harness 工具层（BashTool, ReadFileTool 等） | 第 2 层工具 | 9.38-49 |
| protocal/ 子包 | Python 中仅用于类型检查，Go 接口已覆盖 | — |
| CallableSchemaExtractor 增强 | 不需要 — 用 ListTools 硬编码 | — |

---

## 附录 A：完整 Result/Data 类型字段

### Shell Operation Results

| Data 类型 | 字段 | 类型 | 对齐 Python |
|---|---|---|---|
| ExecuteCmdData | Command | string | ✅ command |
| | Cwd | string | ✅ cwd |
| | ExitCode | *int | ✅ exit_code |
| | Stdout | string | ✅ stdout |
| | Stderr | string | ✅ stderr |
| ExecuteCmdChunkData | Text | string | ✅ text |
| | Type | *string | ✅ type |
| | ChunkIndex | int | ✅ chunk_index |
| | ExitCode | *int | ✅ exit_code |
| | Metadata | map[string]any | ✅ metadata |
| ExecuteCmdBackgroundData | Command | string | ✅ command |
| | Cwd | string | ✅ cwd |
| | Pid | *int | ✅ pid |

### FS Operation Results

| Data 类型 | 字段 | 类型 | 对齐 Python |
|---|---|---|---|
| ReadFileData | Path | string | ✅ path |
| | Content | string | ✅ content (Union[str,bytes] → string) |
| | Mode | string | ✅ mode |
| ReadFileChunkData | Path | string | ✅ path |
| | ChunkContent | string | ✅ chunk_content |
| | Mode | string | ✅ mode |
| | ChunkSize | int | ✅ chunk_size |
| | ChunkIndex | int | ✅ chunk_index |
| | IsLastChunk | bool | ✅ is_last_chunk |
| WriteFileData | Path | string | ✅ path |
| | Size | int | ✅ size |
| | Mode | string | ✅ mode |
| UploadFileData | LocalPath | string | ✅ local_path |
| | TargetPath | string | ✅ target_path |
| | Size | int | ✅ size |
| UploadFileChunkData | LocalPath | string | ✅ local_path |
| | TargetPath | string | ✅ target_path |
| | ChunkSize | int | ✅ chunk_size |
| | ChunkIndex | int | ✅ chunk_index |
| | IsLastChunk | bool | ✅ is_last_chunk |
| DownloadFileData | SourcePath | string | ✅ source_path |
| | LocalPath | string | ✅ local_path |
| | Size | int | ✅ size |
| DownloadFileChunkData | SourcePath | string | ✅ source_path |
| | LocalPath | string | ✅ local_path |
| | ChunkSize | int | ✅ chunk_size |
| | ChunkIndex | int | ✅ chunk_index |
| | IsLastChunk | bool | ✅ is_last_chunk |
| FileSystemItem | Name | string | ✅ name |
| | Path | string | ✅ path |
| | Size | int64 | ✅ size |
| | ModifiedTime | string | ✅ modified_time |
| | IsDirectory | bool | ✅ is_directory |
| | Type | string | ✅ type |
| FileSystemData | TotalCount | int | ✅ total_count |
| | ListItems | []FileSystemItem | ✅ list_items |
| | RootPath | string | ✅ root_path |
| | Recursive | bool | ✅ recursive |
| | MaxDepth | int | ✅ max_depth |
| SearchFilesData | TotalMatches | int | ✅ total_matches |
| | MatchingFiles | []FileSystemItem | ✅ matching_files |
| | SearchPath | string | ✅ search_path |
| | SearchPattern | string | ✅ search_pattern |
| | ExcludePatterns | []string | ✅ exclude_patterns |

### Code Operation Results

| Data 类型 | 字段 | 类型 | 对齐 Python |
|---|---|---|---|
| ExecuteCodeData | CodeContent | string | ✅ code_content |
| | Language | string | ✅ language |
| | ExitCode | *int | ✅ exit_code |
| | Stdout | string | ✅ stdout |
| | Stderr | string | ✅ stderr |
| ExecuteCodeChunkData | Text | string | ✅ text |
| | Type | *string | ✅ type |
| | ChunkIndex | int | ✅ chunk_index |
| | ExitCode | *int | ✅ exit_code |
| | Metadata | map[string]any | ✅ metadata |
