// 请求级命令执行归属绑定（对齐 Python command_execution_context.py）。
// 每个请求由所属会话适配器选定一个确切的 sys operation，绑到当前任务树；
// 工具内部通过 binding 取用，避免依赖全局状态或跨请求串联。
// 对应 Python 代码：jiuwenswarm/agents/harness/common/tools/command_execution_context.py
package tools

import (
	"context"
	"errors"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CommandExecutionBinding 由所属会话适配器选定的确切 sys operation。
// SysOperation 用 any 承载：本包位于 swarm 层，而 sys operation 实现可能在
// agentcore 或沙箱 provider 中，直接用具体类型会引入跨层依赖。
// Python 侧同为 Any。
// Python: @dataclass(frozen=True, slots=True) CommandExecutionBinding
type CommandExecutionBinding struct {
	// SysOperation 本次请求使用的系统操作实例
	SysOperation any
	// Sandboxed 是否运行在沙箱中
	Sandboxed bool
}

// commandExecutionKey 命令执行绑定的 context key
type commandExecutionKey struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCommandExecutionBinding 构造并校验命令执行绑定。
// sysOperation 为 nil 或 sandboxed 非布尔值时由调用方以 bool 已类型化的参数
// 保证；此处只做 nil 校验，与 Python __post_init__ 一致。
// Python: CommandExecutionBinding(sys_operation=..., sandboxed=...)
func NewCommandExecutionBinding(sysOperation any, sandboxed bool) (*CommandExecutionBinding, error) {
	if sysOperation == nil {
		return nil, errors.New("command execution sys_operation is required")
	}
	return &CommandExecutionBinding{
		SysOperation: sysOperation,
		Sandboxed:    sandboxed,
	}, nil
}

// BindCommandExecution 把适配器持有的 sys operation 绑到当前任务树。
// 返回的清理函数等价于 Python 的 reset_command_execution(token)：
// Go 的 context 不可变，丢弃派生 ctx 即恢复上一层绑定。
// Python: bind_command_execution(sys_operation, *, sandboxed)
func BindCommandExecution(ctx context.Context, sysOperation any, sandboxed bool) (context.Context, func(), error) {
	binding, err := NewCommandExecutionBinding(sysOperation, sandboxed)
	if err != nil {
		return ctx, func() {}, err
	}
	return context.WithValue(ctx, commandExecutionKey{}, binding), func() {}, nil
}

// BindNoCommandExecution 显式屏蔽从上层继承来的命令绑定。
// 用于需要清空绑定的场景（Python 的 set(None)）。与"从未绑定"区分开：
// 本函数产生"已绑定但为 nil"状态。
// Python: bind_no_command_execution()
func BindNoCommandExecution(ctx context.Context) (context.Context, func()) {
	return context.WithValue(ctx, commandExecutionKey{}, (*CommandExecutionBinding)(nil)), func() {}
}

// CurrentCommandExecution 返回当前请求级命令执行绑定，未绑定时为 nil。
// Python: current_command_execution()
func CurrentCommandExecution(ctx context.Context) *CommandExecutionBinding {
	if v, ok := ctx.Value(commandExecutionKey{}).(*CommandExecutionBinding); ok {
		return v
	}
	return nil
}
