package infra

import (
	"context"
	"os/exec"
	"strings"
	"unicode"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CommandResult 命令执行结果（对齐 Python (returncode, output) 二元组）
type CommandResult struct {
	// ExitCode 退出码（成功为 0）
	ExitCode int
	// Output 合并 stdout/stderr 后的输出（utf-8 errors=replace 语义）
	Output string
}

// CommandRunner 统一命令执行注入缝（Go 侧新增件，拍板 #3）。
//
// Python 侧直接 spawn 子进程无此抽象；Go 侧收敛所有外部命令
// （git/gh/python/uv 等）出口，测试注入 fake 不跑真命令。
type CommandRunner interface {
	// Run 在 dir 下以 env 执行 name args；env 为完整环境（对齐
	// Python create_subprocess_exec(env=...) 语义：调用方负责构造
	// 全量环境，runner 不做追加）。stderr 合并进 output
	// （对齐 stderr=subprocess.STDOUT）。
	Run(ctx context.Context, dir string, env []string, name string, args ...string) (CommandResult, error)
}

// execCommandRunner 默认实现：exec.CommandContext
type execCommandRunner struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExecCommandRunner 构造真实子进程执行器
func NewExecCommandRunner() CommandRunner {
	return &execCommandRunner{}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Run 执行命令并返回合并输出（对齐 asyncio.create_subprocess_exec 语义）
func (r *execCommandRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) (CommandResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	result := CommandResult{
		ExitCode: 0,
		Output:   decodeLooseUTF8(out),
	}
	if err != nil {
		// 退出码优先（对齐 proc.returncode；启动失败（如未安装）保留 error）
		var exitErr *exec.ExitError
		if ok := asExitError(err, &exitErr); ok {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return result, err
	}
	return result, nil
}

// decodeLooseUTF8 解码输出，非法字节替换（对齐 .decode("utf-8", errors="replace")）
func decodeLooseUTF8(b []byte) string {
	return strings.ToValidUTF8(string(b), "\uFFFD")
}

// rstrip 去除尾部空白（对齐 Python str.rstrip()）
func rstrip(s string) string {
	return strings.TrimRightFunc(s, unicode.IsSpace)
}

// asExitError 类型断言 helper
func asExitError(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}
