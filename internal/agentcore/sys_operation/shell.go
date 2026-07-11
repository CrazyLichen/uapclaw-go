package sys_operation

import (
	"context"
	"fmt"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ShellOperation Shell 操作接口，定义命令执行等操作。
// 对齐 Python BaseShellOperation：execute_cmd, execute_cmd_stream, execute_cmd_background,
// 对齐 Python 方法：write_stdin, kill_process, list_processes, list_tools。
type ShellOperation interface {
	// ExecuteCmd 执行 Shell 命令
	ExecuteCmd(ctx context.Context, command string, opts ...ShellOption) (*result.ExecuteCmdResult, error)
	// ExecuteCmdStream 流式执行 Shell 命令，返回命令输出块通道
	ExecuteCmdStream(ctx context.Context, command string, opts ...ShellOption) (<-chan result.ExecuteCmdStreamResult, error)
	// ExecuteCmdBackground 后台执行 Shell 命令，立即返回进程 PID
	ExecuteCmdBackground(ctx context.Context, command string, opts ...ShellOption) (*result.ExecuteCmdBackgroundResult, error)
	// WriteStdin 向后台进程写入标准输入。
	// 对齐 Python ShellOperation.write_stdin。
	WriteStdin(ctx context.Context, sessionID string, data string, opts ...ShellOption) (*result.ExecuteCmdResult, error)
	// KillProcess 终止指定后台进程。
	// 对齐 Python ShellOperation.kill_process。
	KillProcess(ctx context.Context, sessionID string, opts ...ShellOption) (*result.ExecuteCmdResult, error)
	// ListProcesses 列出所有后台进程。
	// 对齐 Python ShellOperation.list_processes。
	ListProcesses(ctx context.Context, opts ...ShellOption) (*result.ExecuteCmdResult, error)
	// ListTools 返回 Shell 操作的工具卡片列表
	ListTools() []*tool.ToolCard
}

// ShellOption Shell 操作选项函数
type ShellOption func(*ShellOptions)

// ShellOptions Shell 操作选项。
// 对齐 Python execute_cmd 签名：command, cwd, timeout, environment, options, shell_type。
type ShellOptions struct {
	// Cwd 工作目录
	Cwd string
	// Timeout 超时时间（秒）
	Timeout int
	// Environment 环境变量
	Environment map[string]string
	// ShellType Shell 类型
	ShellType ShellType
	// Options 扩展配置选项
	Options map[string]any
}

// BaseShellOperation ShellOperation 的空操作桩实现
type BaseShellOperation struct {
	BaseOperation
}

// ──────────────────────────── 枚举 ────────────────────────────

// ShellType Shell 类型枚举
type ShellType int

const (
	// ShellTypeAuto 自动检测
	ShellTypeAuto ShellType = 0
	// ShellTypeCmd Windows 命令提示符
	ShellTypeCmd ShellType = 1
	// ShellTypePowerShell Windows PowerShell 命令行
	ShellTypePowerShell ShellType = 2
	// ShellTypeBash Bash 命令行
	ShellTypeBash ShellType = 3
	// ShellTypeSh POSIX 命令行
	ShellTypeSh ShellType = 4
)

// ──────────────────────────── 导出函数 ────────────────────────────

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

// ParseShellType 将字符串解析为 ShellType。
// 对齐 Python ShellType.from_str。
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

// NewShellOptions 从选项列表构造 ShellOptions
func NewShellOptions(opts ...ShellOption) *ShellOptions {
	o := &ShellOptions{Timeout: 300}
	for _, opt := range opts {
		opt(o)
	}
	return o
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

// WithShellOptions 设置扩展配置选项
func WithShellOptions(options map[string]any) ShellOption {
	return func(o *ShellOptions) { o.Options = options }
}

// ExecuteCmd 执行命令（BaseShellOperation 空实现）
func (b *BaseShellOperation) ExecuteCmd(_ context.Context, _ string, _ ...ShellOption) (*result.ExecuteCmdResult, error) {
	return nil, fmt.Errorf("未实现: ExecuteCmd")
}

// ExecuteCmdStream 流式执行命令（BaseShellOperation 空实现）
func (b *BaseShellOperation) ExecuteCmdStream(_ context.Context, _ string, _ ...ShellOption) (<-chan result.ExecuteCmdStreamResult, error) {
	return nil, fmt.Errorf("未实现: ExecuteCmdStream")
}

// ExecuteCmdBackground 后台执行命令（BaseShellOperation 空实现）
func (b *BaseShellOperation) ExecuteCmdBackground(_ context.Context, _ string, _ ...ShellOption) (*result.ExecuteCmdBackgroundResult, error) {
	return nil, fmt.Errorf("未实现: ExecuteCmdBackground")
}

// ListTools 返回工具卡片列表（BaseShellOperation 空实现）
func (b *BaseShellOperation) ListTools() []*tool.ToolCard { return nil }

// WriteStdin 向后台进程写入标准输入（BaseShellOperation 空实现）
func (b *BaseShellOperation) WriteStdin(_ context.Context, _ string, _ string, _ ...ShellOption) (*result.ExecuteCmdResult, error) {
	return nil, fmt.Errorf("未实现: WriteStdin")
}

// KillProcess 终止指定后台进程（BaseShellOperation 空实现）
func (b *BaseShellOperation) KillProcess(_ context.Context, _ string, _ ...ShellOption) (*result.ExecuteCmdResult, error) {
	return nil, fmt.Errorf("未实现: KillProcess")
}

// ListProcesses 列出所有后台进程（BaseShellOperation 空实现）
func (b *BaseShellOperation) ListProcesses(_ context.Context, _ ...ShellOption) (*result.ExecuteCmdResult, error) {
	return nil, fmt.Errorf("未实现: ListProcesses")
}
