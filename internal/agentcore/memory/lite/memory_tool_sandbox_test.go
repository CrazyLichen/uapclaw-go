package lite

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestMemoryTools_沙箱拒绝不得假成功 回归：Fs 失败以 BaseResult.Code 表达
// （err 恒为 nil），记忆工具必须显式检查，否则会向模型返回 success=true。
//
// 历史缺陷（CI 40987848552）：沙箱根为空导致写入被拒 → 工具仍报成功 →
// 模型「写入成功→读取为空→编辑读失败」反复排查直到 max_iterations 打满。
func TestMemoryTools_沙箱拒绝不得假成功(t *testing.T) {
	root := t.TempDir()
	ws := workspace.NewWorkspace(root, "cn")
	if nodePath := ws.GetNodePath("memory"); nodePath != nil {
		if err := os.MkdirAll(*nodePath, 0o755); err != nil {
			t.Fatalf("创建 memory 目录失败: %v", err)
		}
	}

	cfg := &sysop.LocalWorkConfig{RestrictToSandbox: true}
	sysOp, err := sysop.NewSysOperation(sysop.NewSysOperationCard(
		sysop.WithSysOpMode(sysop.OperationModeLocal),
		sysop.WithSysOpWorkConfig(cfg),
	))
	if err != nil {
		t.Fatalf("创建 SysOperation 失败: %v", err)
	}

	toolCtx := NewMemoryToolContext().WithWorkspace(ws).WithSysOperation(sysOp).WithAgentID("sandbox-probe")
	toolCtx.Settings = CreateMemorySettings(root, nil)
	toolCtx.NodeName = "memory"

	// 无 CwdState：沙箱根为空 → 写入必须失败（不得假成功）
	ctx := context.Background()
	writeRes := WriteMemoryWithContext(ctx, toolCtx, "USER.md", "手机号: 13800138000\n", false)
	if writeRes.Success {
		t.Fatalf("沙箱拒绝时 write_memory 不应报成功: %+v", writeRes)
	}
	if writeRes.Error == "" {
		t.Fatal("失败结果应带错误信息")
	}

	readRes := ReadMemoryWithContext(ctx, toolCtx, "USER.md", nil, nil)
	if readRes.Success {
		t.Fatalf("沙箱拒绝时 read_memory 不应报成功: %+v", readRes)
	}

	editRes := EditMemoryWithContext(ctx, toolCtx, "USER.md", "13800138000", "13900139000")
	if editRes.Success {
		t.Fatalf("沙箱拒绝时 edit_memory 不应报成功: %+v", editRes)
	}
}

// TestMemoryTools_Ctx工作区下正常落盘 回归：ctx 携带 CwdState(workspace) 时
// 沙箱根=工作区，写/读/编辑全链路应真实生效。
func TestMemoryTools_Ctx工作区下正常落盘(t *testing.T) {
	root := t.TempDir()
	ws := workspace.NewWorkspace(root, "cn")
	if nodePath := ws.GetNodePath("memory"); nodePath != nil {
		if err := os.MkdirAll(*nodePath, 0o755); err != nil {
			t.Fatalf("创建 memory 目录失败: %v", err)
		}
	}

	sysOp, err := sysop.NewSysOperation(sysop.NewSysOperationCard(
		sysop.WithSysOpMode(sysop.OperationModeLocal),
		sysop.WithSysOpWorkConfig(&sysop.LocalWorkConfig{RestrictToSandbox: true}),
	))
	if err != nil {
		t.Fatalf("创建 SysOperation 失败: %v", err)
	}

	toolCtx := NewMemoryToolContext().WithWorkspace(ws).WithSysOperation(sysOp).WithAgentID("sandbox-ok")
	toolCtx.Settings = CreateMemorySettings(root, nil)
	toolCtx.NodeName = "memory"

	ctx := cwd.WithCwdState(context.Background(), cwd.InitCwd(root, cwd.WithWorkspace(root)))

	writeRes := WriteMemoryWithContext(ctx, toolCtx, "USER.md", "手机号: 13800138000\n", false)
	if !writeRes.Success {
		t.Fatalf("write_memory 应成功，实际: %+v", writeRes)
	}
	if _, statErr := os.Stat(writeRes.Path); statErr != nil {
		t.Fatalf("文件未落盘: %s err=%v", writeRes.Path, statErr)
	}

	readRes := ReadMemoryWithContext(ctx, toolCtx, "USER.md", nil, nil)
	if !readRes.Success || readRes.Content == "" {
		t.Fatalf("read_memory 应读到内容，实际: %+v", readRes)
	}

	editRes := EditMemoryWithContext(ctx, toolCtx, "USER.md", "13800138000", "13900139000")
	if !editRes.Success {
		t.Fatalf("edit_memory 应成功，实际: %+v", editRes)
	}
	data, readErr := os.ReadFile(filepath.Join(root, "USER.md"))
	if readErr != nil {
		t.Fatalf("读取落盘文件失败: %v", readErr)
	}
	if !containsText(string(data), "13900139000") {
		t.Fatalf("编辑未落盘，文件内容: %q", string(data))
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// containsText 判断 s 是否包含 sub。
func containsText(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

// indexOf 返回 sub 在 s 中的起始下标，未找到返回 -1。
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
