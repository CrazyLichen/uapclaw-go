package sys_operation

import (
	"context"
	"encoding/json"
	"fmt"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 结构体 ────────────────────────────

// FsOperation 文件系统操作接口，定义读取、写入、列表、搜索等文件系统操作。
type FsOperation interface {
	// ReadFile 读取文件内容
	ReadFile(ctx context.Context, path string, opts ...FsOption) (*ReadFileResult, error)
	// WriteFile 写入文件内容
	WriteFile(ctx context.Context, path string, content string, opts ...FsOption) (*WriteFileResult, error)
	// ListFiles 列出目录下文件
	ListFiles(ctx context.Context, path string, opts ...FsOption) (*ListFilesResult, error)
	// ListDirectories 列出目录下子目录
	ListDirectories(ctx context.Context, path string, opts ...FsOption) (*ListDirsResult, error)
	// SearchFiles 搜索匹配模式的文件
	SearchFiles(ctx context.Context, path string, pattern string, opts ...FsOption) (*SearchFilesResult, error)
	// ListTools 返回文件系统操作的工具卡片列表
	ListTools() []*tool.ToolCard
}

// ShellOperation Shell 操作接口，定义命令执行等操作。
type ShellOperation interface {
	// ExecuteCmd 执行 Shell 命令
	ExecuteCmd(ctx context.Context, command string, opts ...ShellOption) (*ExecuteCmdResult, error)
	// ListTools 返回 Shell 操作的工具卡片列表
	ListTools() []*tool.ToolCard
}

// CodeOperation 代码执行接口，定义代码执行操作。
type CodeOperation interface {
	// ExecuteCode 执行代码
	ExecuteCode(ctx context.Context, code string, opts ...CodeOption) (*ExecuteCodeResult, error)
	// ListTools 返回代码执行的工具卡片列表
	ListTools() []*tool.ToolCard
}

// SysOperation 系统操作主接口，编排文件系统、Shell、代码执行等子操作。
type SysOperation interface {
	// Card 返回系统操作配置卡片
	Card() *SysOperationCard
	// Fs 返回文件系统操作实例
	Fs() FsOperation
	// Shell 返回 Shell 操作实例
	Shell() ShellOperation
	// Code 返回代码执行实例
	Code() CodeOperation
	// IsolationKeyTemplate 返回隔离键模板
	IsolationKeyTemplate() string
}

// BaseSysOperation SysOperation 的空操作桩实现，所有方法返回 nil 或未实现错误。
type BaseSysOperation struct{}

// BaseFsOperation FsOperation 的空操作桩实现，所有方法返回 nil 或未实现错误。
type BaseFsOperation struct{}

// BaseShellOperation ShellOperation 的空操作桩实现，所有方法返回 nil 或未实现错误。
type BaseShellOperation struct{}

// BaseCodeOperation CodeOperation 的空操作桩实现，所有方法返回 nil 或未实现错误。
type BaseCodeOperation struct{}

// ReadFileResult 读取文件结果
type ReadFileResult struct {
	// Code 状态码
	Code int
	// Message 状态消息
	Message string
	// Data 文件内容
	Data string
}

// WriteFileResult 写入文件结果
type WriteFileResult struct {
	// Code 状态码
	Code int
	// Message 状态消息
	Message string
}

// ListFilesResult 列出文件结果
type ListFilesResult struct {
	// Code 状态码
	Code int
	// Message 状态消息
	Message string
}

// ListDirsResult 列出目录结果
type ListDirsResult struct {
	// Code 状态码
	Code int
	// Message 状态消息
	Message string
}

// SearchFilesResult 搜索文件结果
type SearchFilesResult struct {
	// Code 状态码
	Code int
	// Message 状态消息
	Message string
}

// ExecuteCmdResult 执行命令结果
type ExecuteCmdResult struct {
	// Code 状态码
	Code int
	// Message 状态消息
	Message string
	// Stdout 标准输出
	Stdout string
	// Stderr 标准错误
	Stderr string
	// ExitCode 退出码
	ExitCode int
}

// ExecuteCodeResult 执行代码结果
type ExecuteCodeResult struct {
	// Code 状态码
	Code int
	// Message 状态消息
	Message string
	// Stdout 标准输出
	Stdout string
	// Stderr 标准错误
	Stderr string
	// ExitCode 退出码
	ExitCode int
}

// FsOptions 文件系统操作选项
type FsOptions struct {
	// Mode 读取模式
	Mode string
	// Head 头部行数
	Head int
	// Tail 尾部行数
	Tail int
	// Encoding 文件编码
	Encoding string
}

// ShellOptions Shell 操作选项
type ShellOptions struct {
	// Cwd 工作目录
	Cwd string
	// Timeout 超时时间（秒）
	Timeout int
	// Environment 环境变量
	Environment map[string]string
	// ShellType Shell 类型
	ShellType ShellType
}

// CodeOptions 代码执行选项
type CodeOptions struct {
	// Language 编程语言
	Language string
	// Timeout 超时时间（秒）
	Timeout int
	// Environment 环境变量
	Environment map[string]string
	// Cwd 工作目录
	Cwd string
}

// OperationMode 操作模式枚举
type OperationMode int

// ──────────────────────────── 常量 ────────────────────────────

const (
	// OperationModeLocal 本地执行模式
	OperationModeLocal OperationMode = 0
	// OperationModeSandbox 沙箱执行模式
	OperationModeSandbox OperationMode = 1
)

// ──────────────────────────── 结构体 ────────────────────────────

// ShellType Shell 类型枚举
type ShellType int

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ShellTypeAuto 自动检测
	ShellTypeAuto ShellType = 0
	// ShellTypeCmd Windows CMD
	ShellTypeCmd ShellType = 1
	// ShellTypePowerShell Windows PowerShell
	ShellTypePowerShell ShellType = 2
	// ShellTypeBash Bash
	ShellTypeBash ShellType = 3
	// ShellTypeSh POSIX Sh
	ShellTypeSh ShellType = 4
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContainerScope 容器作用域枚举
type ContainerScope int

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ContainerScopeSystem 系统级容器
	ContainerScopeSystem ContainerScope = 0
	// ContainerScopeSession 会话级容器
	ContainerScopeSession ContainerScope = 1
	// ContainerScopeCustom 自定义容器
	ContainerScopeCustom ContainerScope = 2
)

// ──────────────────────────── 结构体 ────────────────────────────

// FsOption 文件系统操作选项函数
type FsOption func(*FsOptions)

// ShellOption Shell 操作选项函数
type ShellOption func(*ShellOptions)

// CodeOption 代码执行选项函数
type CodeOption func(*CodeOptions)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 BaseSysOperation 满足 SysOperation 接口
var _ SysOperation = (*BaseSysOperation)(nil)

// 编译时验证 BaseFsOperation 满足 FsOperation 接口
var _ FsOperation = (*BaseFsOperation)(nil)

// 编译时验证 BaseShellOperation 满足 ShellOperation 接口
var _ ShellOperation = (*BaseShellOperation)(nil)

// 编译时验证 BaseCodeOperation 满足 CodeOperation 接口
var _ CodeOperation = (*BaseCodeOperation)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// String 返回操作模式的字符串表示
func (m OperationMode) String() string {
	switch m {
	case OperationModeLocal:
		return "local"
	case OperationModeSandbox:
		return "sandbox"
	default:
		return fmt.Sprintf("unknown(%d)", int(m))
	}
}

// MarshalJSON 实现 json.Marshaler 接口
func (m OperationMode) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.String())
}

// UnmarshalJSON 实现 json.Unmarshaler 接口
func (m *OperationMode) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	switch s {
	case "local":
		*m = OperationModeLocal
	case "sandbox":
		*m = OperationModeSandbox
	default:
		return fmt.Errorf("未知的操作模式: %s", s)
	}
	return nil
}

// String 返回 Shell 类型的字符串表示
func (s ShellType) String() string {
	switch s {
	case ShellTypeAuto:
		return "auto"
	case ShellTypeCmd:
		return "cmd"
	case ShellTypePowerShell:
		return "powershell"
	case ShellTypeBash:
		return "bash"
	case ShellTypeSh:
		return "sh"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// String 返回容器作用域的字符串表示
func (s ContainerScope) String() string {
	switch s {
	case ContainerScopeSystem:
		return "system"
	case ContainerScopeSession:
		return "session"
	case ContainerScopeCustom:
		return "custom"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// ParseShellType 将字符串解析为 ShellType
func ParseShellType(s string) ShellType {
	switch s {
	case "auto":
		return ShellTypeAuto
	case "cmd":
		return ShellTypeCmd
	case "powershell":
		return ShellTypePowerShell
	case "bash":
		return ShellTypeBash
	case "sh":
		return ShellTypeSh
	default:
		return ShellTypeAuto
	}
}

// WithFsMode 设置文件系统操作读取模式
func WithFsMode(mode string) FsOption {
	return func(o *FsOptions) { o.Mode = mode }
}

// WithFsHead 设置文件系统操作头部行数
func WithFsHead(head int) FsOption {
	return func(o *FsOptions) { o.Head = head }
}

// WithFsTail 设置文件系统操作尾部行数
func WithFsTail(tail int) FsOption {
	return func(o *FsOptions) { o.Tail = tail }
}

// WithFsEncoding 设置文件系统操作编码
func WithFsEncoding(encoding string) FsOption {
	return func(o *FsOptions) { o.Encoding = encoding }
}

// WithShellCwd 设置 Shell 操作工作目录
func WithShellCwd(cwd string) ShellOption {
	return func(o *ShellOptions) { o.Cwd = cwd }
}

// WithShellTimeout 设置 Shell 操作超时时间
func WithShellTimeout(timeout int) ShellOption {
	return func(o *ShellOptions) { o.Timeout = timeout }
}

// WithShellEnvironment 设置 Shell 操作环境变量
func WithShellEnvironment(env map[string]string) ShellOption {
	return func(o *ShellOptions) { o.Environment = env }
}

// WithShellType 设置 Shell 类型
func WithShellType(st ShellType) ShellOption {
	return func(o *ShellOptions) { o.ShellType = st }
}

// WithCodeLanguage 设置代码执行编程语言
func WithCodeLanguage(lang string) CodeOption {
	return func(o *CodeOptions) { o.Language = lang }
}

// WithCodeTimeout 设置代码执行超时时间
func WithCodeTimeout(timeout int) CodeOption {
	return func(o *CodeOptions) { o.Timeout = timeout }
}

// WithCodeEnvironment 设置代码执行环境变量
func WithCodeEnvironment(env map[string]string) CodeOption {
	return func(o *CodeOptions) { o.Environment = env }
}

// WithCodeCwd 设置代码执行工作目录
func WithCodeCwd(cwd string) CodeOption {
	return func(o *CodeOptions) { o.Cwd = cwd }
}

// NewFsOptions 从选项列表构造 FsOptions
func NewFsOptions(opts ...FsOption) *FsOptions {
	o := &FsOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// NewShellOptions 从选项列表构造 ShellOptions
func NewShellOptions(opts ...ShellOption) *ShellOptions {
	o := &ShellOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// NewCodeOptions 从选项列表构造 CodeOptions
func NewCodeOptions(opts ...CodeOption) *CodeOptions {
	o := &CodeOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Card 返回系统操作配置卡片（BaseSysOperation 空实现）
func (b *BaseSysOperation) Card() *SysOperationCard { return nil }

// Fs 返回文件系统操作实例（BaseSysOperation 空实现）
func (b *BaseSysOperation) Fs() FsOperation { return nil }

// Shell 返回 Shell 操作实例（BaseSysOperation 空实现）
func (b *BaseSysOperation) Shell() ShellOperation { return nil }

// Code 返回代码执行实例（BaseSysOperation 空实现）
func (b *BaseSysOperation) Code() CodeOperation { return nil }

// IsolationKeyTemplate 返回隔离键模板（BaseSysOperation 空实现）
func (b *BaseSysOperation) IsolationKeyTemplate() string { return "" }

// ReadFile 读取文件（BaseFsOperation 空实现）
func (b *BaseFsOperation) ReadFile(_ context.Context, _ string, _ ...FsOption) (*ReadFileResult, error) {
	return nil, fmt.Errorf("未实现: ReadFile")
}

// WriteFile 写入文件（BaseFsOperation 空实现）
func (b *BaseFsOperation) WriteFile(_ context.Context, _ string, _ string, _ ...FsOption) (*WriteFileResult, error) {
	return nil, fmt.Errorf("未实现: WriteFile")
}

// ListFiles 列出文件（BaseFsOperation 空实现）
func (b *BaseFsOperation) ListFiles(_ context.Context, _ string, _ ...FsOption) (*ListFilesResult, error) {
	return nil, fmt.Errorf("未实现: ListFiles")
}

// ListDirectories 列出目录（BaseFsOperation 空实现）
func (b *BaseFsOperation) ListDirectories(_ context.Context, _ string, _ ...FsOption) (*ListDirsResult, error) {
	return nil, fmt.Errorf("未实现: ListDirectories")
}

// SearchFiles 搜索文件（BaseFsOperation 空实现）
func (b *BaseFsOperation) SearchFiles(_ context.Context, _ string, _ string, _ ...FsOption) (*SearchFilesResult, error) {
	return nil, fmt.Errorf("未实现: SearchFiles")
}

// ListTools 返回工具卡片列表（BaseFsOperation 空实现）
func (b *BaseFsOperation) ListTools() []*tool.ToolCard { return nil }

// ExecuteCmd 执行命令（BaseShellOperation 空实现）
func (b *BaseShellOperation) ExecuteCmd(_ context.Context, _ string, _ ...ShellOption) (*ExecuteCmdResult, error) {
	return nil, fmt.Errorf("未实现: ExecuteCmd")
}

// ListTools 返回工具卡片列表（BaseShellOperation 空实现）
func (b *BaseShellOperation) ListTools() []*tool.ToolCard { return nil }

// ExecuteCode 执行代码（BaseCodeOperation 空实现）
func (b *BaseCodeOperation) ExecuteCode(_ context.Context, _ string, _ ...CodeOption) (*ExecuteCodeResult, error) {
	return nil, fmt.Errorf("未实现: ExecuteCode")
}

// ListTools 返回工具卡片列表（BaseCodeOperation 空实现）
func (b *BaseCodeOperation) ListTools() []*tool.ToolCard { return nil }
