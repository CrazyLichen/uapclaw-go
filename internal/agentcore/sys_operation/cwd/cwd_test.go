package cwd

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestInitCwd_基本初始化 测试 InitCwd 创建 CwdState
// 对齐 Python: init_cwd(cwd) → CwdState(cwd=resolved, original_cwd=resolved, project_root=resolved)
func TestInitCwd_基本初始化(t *testing.T) {
	state := InitCwd("/project", WithWorkspace("/project/ws"))
	assert.Equal(t, resolve("/project"), state.GetCwd())
	assert.Equal(t, resolve("/project"), state.GetOriginalCwd())
	assert.Equal(t, resolve("/project"), state.GetProjectRoot())
	assert.Equal(t, resolve("/project/ws"), state.GetWorkspace())
}

// TestInitCwd_自定义ProjectRoot 测试显式设置 project_root
// 对齐 Python: init_cwd(cwd, project_root="/project")
func TestInitCwd_自定义ProjectRoot(t *testing.T) {
	state := InitCwd("/workspace", WithProjectRoot("/project"))
	assert.Equal(t, resolve("/workspace"), state.GetCwd())
	assert.Equal(t, resolve("/project"), state.GetProjectRoot())
}

// TestInitCwd_全选项 测试所有选项同时设置
func TestInitCwd_全选项(t *testing.T) {
	state := InitCwd("/cwd",
		WithProjectRoot("/project"),
		WithWorkspace("/workspace"),
		WithTeamWorkspace("/team"),
	)
	assert.Equal(t, resolve("/cwd"), state.GetCwd())
	assert.Equal(t, resolve("/cwd"), state.GetOriginalCwd())
	assert.Equal(t, resolve("/project"), state.GetProjectRoot())
	assert.Equal(t, resolve("/workspace"), state.GetWorkspace())
	assert.Equal(t, resolve("/team"), state.GetTeamWorkspace())
}

// TestCwdState_SetCwd 测试运行时修改 CWD
// 对齐 Python: set_cwd(cwd) → _state().cwd = _resolve(cwd)
func TestCwdState_SetCwd(t *testing.T) {
	state := InitCwd("/project")
	state.SetCwd("/project/worktree")
	assert.Equal(t, resolve("/project/worktree"), state.GetCwd())
	// originalCwd 和 projectRoot 不受影响
	assert.Equal(t, resolve("/project"), state.GetOriginalCwd())
	assert.Equal(t, resolve("/project"), state.GetProjectRoot())
}

// TestCwdState_SetOriginalCwd 测试修改会话起始点
// 对齐 Python: set_original_cwd(cwd)
func TestCwdState_SetOriginalCwd(t *testing.T) {
	state := InitCwd("/project")
	state.SetOriginalCwd("/project/worktree")
	assert.Equal(t, resolve("/project/worktree"), state.GetOriginalCwd())
}

// TestCwdState_读取优先级 测试 cwd -> originalCwd -> os.Getwd() 优先级
// 对齐 Python: get_cwd() → s.cwd or s.original_cwd or os.getcwd()
func TestCwdState_读取优先级(t *testing.T) {
	state := InitCwd("/project")
	// 正常：返回 cwd
	assert.Equal(t, resolve("/project"), state.GetCwd())

	// 清空 cwd：回退到 originalCwd
	state.mu.Lock()
	state.cwd = ""
	state.mu.Unlock()
	assert.Equal(t, resolve("/project"), state.GetCwd())

	// 清空 originalCwd：回退到 os.Getwd()
	state.mu.Lock()
	state.originalCwd = ""
	state.mu.Unlock()
	wd, _ := os.Getwd()
	assert.Equal(t, wd, state.GetCwd())
}

// TestCwdState_GetProjectRoot优先级 测试 projectRoot -> originalCwd -> os.Getwd()
// 对齐 Python: get_project_root() → s.project_root or get_original_cwd()
func TestCwdState_GetProjectRoot优先级(t *testing.T) {
	state := InitCwd("/project")
	// 正常：返回 projectRoot
	assert.Equal(t, resolve("/project"), state.GetProjectRoot())

	// 清空 projectRoot：回退到 originalCwd
	state.mu.Lock()
	state.projectRoot = ""
	state.mu.Unlock()
	assert.Equal(t, resolve("/project"), state.GetProjectRoot())
}

// TestCwdState_并发安全 测试并发读写不 panic
func TestCwdState_并发安全(t *testing.T) {
	state := InitCwd("/project")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = state.GetCwd()
		}()
		go func() {
			defer wg.Done()
			state.SetCwd(filepath.Join("/tmp", "dir"))
		}()
	}
	wg.Wait()
}

// TestWithCwdState_上下文传播 测试 CwdState 通过 context 传播
// 对齐 Python: _cwd_state.set(state) → _cwd_state.get() == state
func TestWithCwdState_上下文传播(t *testing.T) {
	state := InitCwd("/project")
	ctx := WithCwdState(context.Background(), state)
	got := CwdStateFromCtx(ctx)
	assert.Equal(t, state, got)
}

// TestWithCwdState_上下文无CwdState 测试无 CwdState 时返回 nil
func TestWithCwdState_上下文无CwdState(t *testing.T) {
	got := CwdStateFromCtx(context.Background())
	assert.Nil(t, got)
}

// TestGetCwd_从上下文获取 测试 GetCwd(ctx) 全局函数
// 对齐 Python: get_cwd()
func TestGetCwd_从上下文获取(t *testing.T) {
	state := InitCwd("/project")
	ctx := WithCwdState(context.Background(), state)
	assert.Equal(t, resolve("/project"), GetCwd(ctx))
}

// TestGetCwd_上下文无CwdState回退 测试 ctx 无 CwdState 时回退到 os.Getwd()
func TestGetCwd_上下文无CwdState回退(t *testing.T) {
	cwd := GetCwd(context.Background())
	wd, _ := os.Getwd()
	assert.Equal(t, wd, cwd)
}

// TestGetWorkspace_从上下文获取 测试 GetWorkspace(ctx)
// 对齐 Python: get_workspace()
func TestGetWorkspace_从上下文获取(t *testing.T) {
	state := InitCwd("/project", WithWorkspace("/workspace"))
	ctx := WithCwdState(context.Background(), state)
	assert.Equal(t, resolve("/workspace"), GetWorkspace(ctx))
}

// TestGetWorkspace_未设置返回空 测试 workspace 未设置时返回空字符串
// 对齐 Python: get_workspace() → None
func TestGetWorkspace_未设置返回空(t *testing.T) {
	state := InitCwd("/project")
	ctx := WithCwdState(context.Background(), state)
	assert.Equal(t, "", GetWorkspace(ctx))
}

// TestResolveCwd_显式绝对路径 测试显式绝对路径直接使用
// 对齐 Python: ShellOperation._resolve_cwd(cwd) — 绝对路径直接 resolve
func TestResolveCwd_显式绝对路径(t *testing.T) {
	state := InitCwd("/project")
	ctx := WithCwdState(context.Background(), state)
	assert.Equal(t, resolve("/tmp"), ResolveCwd(ctx, "/tmp"))
}

// TestResolveCwd_显式相对路径 测试相对路径基于 GetCwd 解析
// 对齐 Python: target = pathlib.Path(get_cwd()) / target
func TestResolveCwd_显式相对路径(t *testing.T) {
	state := InitCwd("/project")
	ctx := WithCwdState(context.Background(), state)
	expected := resolve(filepath.Join(resolve("/project"), "subdir"))
	assert.Equal(t, expected, ResolveCwd(ctx, "subdir"))
}

// TestResolveCwd_空值回退 测试空值使用 GetCwd
func TestResolveCwd_空值回退(t *testing.T) {
	state := InitCwd("/project")
	ctx := WithCwdState(context.Background(), state)
	assert.Equal(t, resolve("/project"), ResolveCwd(ctx, ""))
}

// TestResolvePath_相对路径 测试相对路径基于 GetCwd 解析
// 对齐 Python: base = pathlib.Path(get_cwd()); raw = base / path
func TestResolvePath_相对路径(t *testing.T) {
	state := InitCwd("/project")
	ctx := WithCwdState(context.Background(), state)
	expected := filepath.Clean(filepath.Join(resolve("/project"), "src/main.go"))
	assert.Equal(t, expected, ResolvePath(ctx, "src/main.go"))
}

// TestResolvePath_绝对路径 测试绝对路径直接使用
func TestResolvePath_绝对路径(t *testing.T) {
	state := InitCwd("/project")
	ctx := WithCwdState(context.Background(), state)
	assert.Equal(t, filepath.Clean("/tmp/file.go"), ResolvePath(ctx, "/tmp/file.go"))
}

// Test子Agent隔离 测试子 Agent 创建独立 CwdState 不影响父
// 对齐 Python: init_cwd() 创建新 CwdState，_cwd_state.set() 只影响当前 Task
func Test子Agent隔离(t *testing.T) {
	parentState := InitCwd("/project", WithWorkspace("/project"))
	parentCtx := WithCwdState(context.Background(), parentState)

	// 子 Agent 创建独立 CwdState
	subState := InitCwd("/project/.sub/xxx", WithWorkspace("/project/.sub/xxx"))
	subCtx := WithCwdState(parentCtx, subState)

	// 子 Agent 修改 CWD
	subState.SetCwd("/project/.sub/xxx/worktree")

	// 父 Agent 不受影响
	assert.Equal(t, resolve("/project"), GetCwd(parentCtx))
	// 子 Agent 看到新 CWD
	assert.Equal(t, resolve("/project/.sub/xxx/worktree"), GetCwd(subCtx))
}

// Test父改子可见 测试父修改 CWD 后同一 ctx 下的子 goroutine 可见
// 对齐 Python: 同 Task 内共享 CwdState 引用，set_cwd 后立即可见
func Test父改子可见(t *testing.T) {
	state := InitCwd("/project")
	ctx := WithCwdState(context.Background(), state)

	// 同一 CwdState 指针修改
	state.SetCwd("/project/worktree")

	// 同一 ctx 读取可见
	assert.Equal(t, resolve("/project/worktree"), GetCwd(ctx))
}
