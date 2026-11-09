// 命令工作目录的共享运行时契约。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/tools/command_runtime.py（98 行）
//
// 语言差异：Python 用 ContextVar 隐式传递 CwdState（`openjiuwen.core.sys_operation.cwd`
// 的 get_cwd/get_project_root/get_workspace 无参调用），Go 侧这些函数收
// context.Context（见 `agentcore/sys_operation/cwd`）。因此本文件所有函数
// 首个参数为 ctx。这是 Go 惯用法转换，非语义偏差。
//
// 历史沿革：mcp_command.go 曾私有内联实现同一套逻辑（contextCwd /
// contextProjectRoot / contextWorkspaceRoot / resolvePathString / isRelativeTo /
// resolveCommandWorkdir）。本文件把其提升为公开契约（含 CommandRuntimePaths
// 结构体）后，私有版已在同一改动内删除（规则 8）。

package tools

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	upath "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CommandRuntimePaths 命令执行使用的主机自有路径锚点。
//
// Python: @dataclass(frozen=True) CommandRuntimePaths
type CommandRuntimePaths struct {
	// CurrentCwd 当前工作目录
	CurrentCwd string
	// ProjectRoot 项目根目录（沙箱边界依据）
	ProjectRoot string
	// WorkspaceRoot 工作区根目录，空串表示未设置（Python None）
	WorkspaceRoot string
	// AgentWorkspaceRoot Agent 工作区根目录
	AgentWorkspaceRoot string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CurrentCommandRuntimePaths 读取一次当前命令路径上下文。
//
// requireRuntimeCwd 为 true 时，若读不到 cwd 返回 error 而非回退。
//
// Python: current_command_runtime_paths(*, require_runtime_cwd=False)
func CurrentCommandRuntimePaths(
	ctx context.Context,
	requireRuntimeCwd bool,
) (CommandRuntimePaths, error) {
	currentCwd, err := currentCwdPath(ctx, requireRuntimeCwd)
	if err != nil {
		return CommandRuntimePaths{}, err
	}
	return CommandRuntimePaths{
		CurrentCwd:         currentCwd,
		ProjectRoot:        currentProjectRoot(ctx, currentCwd),
		WorkspaceRoot:      currentWorkspaceRoot(ctx),
		AgentWorkspaceRoot: agentWorkspaceDir(),
	}, nil
}

// ResolveCommandWorkdir 按工具既有的根目录契约解析一个命令工作目录。
//
// 白名单三根：ProjectRoot / WorkspaceRoot（非空时）/ AgentWorkspaceRoot。
// 越界或类型错误返回 error。
//
// Python: resolve_command_workdir(workdir, *, runtime_paths)
func ResolveCommandWorkdir(workdir any, paths CommandRuntimePaths) (string, error) {
	var candidate string

	switch v := workdir.(type) {
	case nil:
		candidate = paths.CurrentCwd
	case string:
		if v == "" {
			candidate = paths.CurrentCwd
		} else {
			candidate = v
		}
	default:
		return "", fmt.Errorf("workdir must be a string")
	}

	// 相对路径基于当前 cwd 拼接
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(paths.CurrentCwd, candidate)
	}
	candidate = resolvePath(candidate)

	// 三根白名单
	allowedRoots := []string{paths.ProjectRoot}
	if paths.WorkspaceRoot != "" {
		allowedRoots = append(allowedRoots, paths.WorkspaceRoot)
	}
	allowedRoots = append(allowedRoots, paths.AgentWorkspaceRoot)

	for _, root := range allowedRoots {
		if upath.IsPathWithin(root, candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("workdir is outside project workspace")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// currentCwdPath 读当前 cwd；异常时按 requireRuntimeCwd 决定报错或回退。
//
// Python: _current_cwd(*, require_runtime_cwd)
//
//	try:
//	    from ...cwd import get_cwd
//	    return Path(get_cwd()).resolve()
//	except Exception:
//	    if require_runtime_cwd:
//	        raise ValueError("command runtime cwd is unavailable") from None
//	    return get_agent_workspace_dir().resolve()
//
// 一比一对应：
//   - cwd.GetCwd(ctx) 对应 get_cwd()（含 os.getcwd() 可能抛的 OSError）
//   - resolvePath(...) 对应 Path(...).resolve()
//   - if err != nil 对应 except Exception
//   - require 分支对应 raise ValueError
//   - agentWorkspaceDir() 对应 get_agent_workspace_dir().resolve()
//
// 注：cwd 包现已删除吞异常的兼容壳，GetCwd 是唯一入口并返回
// (string, error)，与 Python get_cwd() 的异常语义一致。
func currentCwdPath(ctx context.Context, requireRuntimeCwd bool) (string, error) {
	got, err := cwd.GetCwd(ctx)
	if err == nil {
		return resolvePath(got), nil
	}
	if requireRuntimeCwd {
		return "", fmt.Errorf("command runtime cwd is unavailable")
	}
	return agentWorkspaceDir(), nil
}

// currentProjectRoot 读项目根；异常时回退到 cwd。
//
// Python: _current_project_root(current_cwd)
//
//	try:
//	    from ...cwd import get_project_root
//	    return Path(get_project_root()).resolve()
//	except Exception:
//	    return current_cwd
//
// 一比一对应：cwd.GetProjectRoot(ctx) 的 err != nil 对应 except Exception。
func currentProjectRoot(ctx context.Context, currentCwd string) string {
	got, err := cwd.GetProjectRoot(ctx)
	if err == nil {
		return resolvePath(got)
	}
	return currentCwd
}

// currentWorkspaceRoot 读工作区根；未设置返回空串（Python None）。
//
// Python: _current_workspace_root()
func currentWorkspaceRoot(ctx context.Context) string {
	got := cwd.GetWorkspace(ctx)
	if got == "" {
		return ""
	}
	return resolvePath(got)
}

// agentWorkspaceDir 返回 Agent 工作区目录的绝对路径。
//
// Python: get_agent_workspace_dir().resolve()
func agentWorkspaceDir() string {
	return resolvePath(workspace.AgentWorkspaceDir())
}

// resolvePath 路径规整（对齐 Python Path.resolve(strict=False) 语义）。
//
// 用 upath.ResolveSymlinksLenient：ExpandHome + Abs + EvalSymlinks，
// 失败时回退到最近的已存在祖先。Python 的 resolve(strict=False) 同样会
// 解析 symlink 并容忍路径不存在。
func resolvePath(p string) string {
	return upath.ResolveSymlinksLenient(p)
}
