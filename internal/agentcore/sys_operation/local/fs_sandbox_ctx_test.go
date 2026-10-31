package local

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
)

// TestResolvePathSandbox_使用Ctx中的工作区 回归：沙箱根必须从调用方 ctx 的
// CwdState 读取（对齐 Python get_workspace()/get_project_root() 的 contextvar 语义）。
//
// 历史缺陷：resolvePath 用 context.Background() 读 CwdState，恒为空 →
// sandboxRoots 为空 → RestrictToSandbox=true（生产默认）下所有绝对路径被拒，
// 表现为记忆工具「写入报成功但没落盘」。
func TestResolvePathSandbox_使用Ctx中的工作区(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "memory", "USER.md")

	cfg := &sysop.LocalWorkConfig{RestrictToSandbox: true}
	fsOp := NewLocalFsOperation(cfg).(*LocalFsOperation)

	// 无 CwdState：沙箱根为空 → 拒绝
	if _, err := fsOp.resolvePath(context.Background(), target, true); err == nil {
		t.Fatal("无 CwdState 时应拒绝沙箱外路径")
	}

	// 有 CwdState（workspace = root）：允许
	ctx := cwd.WithCwdState(context.Background(), cwd.InitCwd(root, cwd.WithWorkspace(root)))
	resolved, err := fsOp.resolvePath(ctx, target, true)
	if err != nil {
		t.Fatalf("CwdState.workspace 覆盖目标路径时应允许，实际报错: %v", err)
	}
	if resolved != target {
		t.Fatalf("解析路径不符：期望 %s，实际 %s", target, resolved)
	}

	// 写入可真实落盘
	res, err := fsOp.WriteFile(ctx, target, "hello", sysop.WithFsCreateIfNotExist(true))
	if err != nil {
		t.Fatalf("WriteFile 返回 error: %v", err)
	}
	if res.BaseResult.Code != 0 {
		t.Fatalf("WriteFile 结果码非 0: %+v", res.BaseResult)
	}
	if data, readErr := os.ReadFile(target); readErr != nil || string(data) != "hello" {
		t.Fatalf("文件未落盘: data=%q err=%v", string(data), readErr)
	}
}

// TestResolvePathSandbox_相对路径基准取自Ctx 回归：相对路径必须以 ctx 的 cwd 为基准
// （对齐 Python get_cwd()），而不是进程 CWD。
func TestResolvePathSandbox_相对路径基准取自Ctx(t *testing.T) {
	root := t.TempDir()
	cfg := &sysop.LocalWorkConfig{}
	fsOp := NewLocalFsOperation(cfg).(*LocalFsOperation)

	ctx := cwd.WithCwdState(context.Background(), cwd.InitCwd(root, cwd.WithWorkspace(root)))
	resolved, err := fsOp.resolvePath(ctx, "memory/USER.md", false)
	if err != nil {
		t.Fatalf("解析相对路径失败: %v", err)
	}
	expect := filepath.Join(root, "memory", "USER.md")
	if resolved != expect {
		t.Fatalf("相对路径基准不符：期望 %s，实际 %s", expect, resolved)
	}
}

// TestResolvePathSandbox_前缀相似目录越界 回归（S-02）：/ws 不得包含 /ws-evil。
//
// 历史缺陷：用 strings.HasPrefix 做包含判定，路径段边界未对齐，
// 同级前缀目录（root=/ws，target=/ws-evil/x）被误判为沙箱内。
func TestResolvePathSandbox_前缀相似目录越界(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "ws")
	sibling := filepath.Join(parent, "ws-evil")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("创建沙箱根失败: %v", err)
	}
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatalf("创建同级目录失败: %v", err)
	}

	cfg := &sysop.LocalWorkConfig{RestrictToSandbox: true, SandboxRoot: []string{root}}
	fsOp := NewLocalFsOperation(cfg).(*LocalFsOperation)

	if _, err := fsOp.resolvePath(context.Background(), filepath.Join(sibling, "x.txt"), false); err == nil {
		t.Fatal("同级前缀目录应被拒绝（HasPrefix 误判回归）")
	}
	if _, err := fsOp.resolvePath(context.Background(), filepath.Join(root, "ok.txt"), false); err != nil {
		t.Fatalf("沙箱内路径应允许，实际报错: %v", err)
	}
}

// TestResolvePathSandbox_软链逃逸被拒 回归（S-02）：工作区内软链指向外部目录时，
// 其下（尚不存在的）新文件必须被拒绝。
func TestResolvePathSandbox_软链逃逸被拒(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "ws")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("创建沙箱根失败: %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("创建外部目录失败: %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("当前平台不支持软链: %v", err)
	}

	cfg := &sysop.LocalWorkConfig{RestrictToSandbox: true, SandboxRoot: []string{root}}
	fsOp := NewLocalFsOperation(cfg).(*LocalFsOperation)

	// 软链目录下「新建」文件：整链 EvalSymlinks 必失败，必须靠祖先解析拦截
	if _, err := fsOp.resolvePath(context.Background(), filepath.Join(link, "new.txt"), false); err == nil {
		t.Fatal("软链逃逸路径应被拒绝")
	}
}

// TestResolvePathSandbox_空根条目不放行 回归（S-02）：SandboxRoot 含空串时
// 不得因前缀比较恒真而放行任意路径（fail-closed）。
func TestResolvePathSandbox_空根条目不放行(t *testing.T) {
	outside := t.TempDir()
	cfg := &sysop.LocalWorkConfig{RestrictToSandbox: true, SandboxRoot: []string{"", "   "}}
	fsOp := NewLocalFsOperation(cfg).(*LocalFsOperation)

	if _, err := fsOp.resolvePath(context.Background(), filepath.Join(outside, "x.txt"), false); err == nil {
		t.Fatal("沙箱根为空条目时应拒绝，不得 fail-open")
	}
}
