package permissions

import (
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────── DecisionRoute ────────────────────────────

// TestDecisionRoute_六种source 测试六个 source 对应的谓词
func TestDecisionRoute_六种source(t *testing.T) {
	cases := []struct {
		source    string
		hardBlock bool
		manual    bool
		reviewer  bool
		detAllow  bool
	}{
		{"hard_guard", true, false, false, false},
		{"manual_only", false, true, false, false},
		{"semantic_reviewer", false, false, true, false},
		{"recent_search_result", false, false, false, true},
		{"baseline", false, false, false, false},
		{"", false, false, false, false},
	}
	for _, c := range cases {
		r := DecisionRoute{Source: c.source}
		if r.IsHardBlock() != c.hardBlock {
			t.Errorf("%q: IsHardBlock = %v, want %v", c.source, r.IsHardBlock(), c.hardBlock)
		}
		if r.RequiresManual() != c.manual {
			t.Errorf("%q: RequiresManual = %v, want %v", c.source, r.RequiresManual(), c.manual)
		}
		if r.RequiresReviewer() != c.reviewer {
			t.Errorf("%q: RequiresReviewer = %v, want %v", c.source, r.RequiresReviewer(), c.reviewer)
		}
		if r.IsDeterministicAllow() != c.detAllow {
			t.Errorf("%q: IsDeterministicAllow = %v, want %v", c.source, r.IsDeterministicAllow(), c.detAllow)
		}
	}
}

// TestDecisionRoute_Accepted 测试 accepted 是 hard_block 的取反
func TestDecisionRoute_Accepted(t *testing.T) {
	if (DecisionRoute{Source: "hard_guard"}).Accepted() {
		t.Error("hard_guard 的 Accepted 应为 false")
	}
	if !(DecisionRoute{Source: "manual_only"}).Accepted() {
		t.Error("manual_only 的 Accepted 应为 true")
	}
}

// TestDecisionRoute_AllowedOutcomes 测试三种结果集
func TestDecisionRoute_AllowedOutcomes(t *testing.T) {
	// reviewer / deterministic allow → 三选项
	for _, src := range []string{"semantic_reviewer", "recent_search_result"} {
		got := (DecisionRoute{Source: src}).AllowedOutcomes()
		want := []string{"allow_once", "manual", "deny"}
		if len(got) != len(want) {
			t.Fatalf("%q: AllowedOutcomes = %v, want %v", src, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%q: AllowedOutcomes[%d] = %q, want %q", src, i, got[i], want[i])
			}
		}
	}
	// manual → 两选项
	got := (DecisionRoute{Source: "manual_only"}).AllowedOutcomes()
	if len(got) != 2 || got[0] != "manual" || got[1] != "deny" {
		t.Errorf("manual_only: AllowedOutcomes = %v, want [manual deny]", got)
	}
	// 其他 → 空
	if got := (DecisionRoute{Source: "baseline"}).AllowedOutcomes(); len(got) != 0 {
		t.Errorf("baseline: AllowedOutcomes = %v, want 空", got)
	}
	if got := (DecisionRoute{Source: "hard_guard"}).AllowedOutcomes(); len(got) != 0 {
		t.Errorf("hard_guard: AllowedOutcomes = %v, want 空", got)
	}
}

// TestDecisionRoute_NoAutoAllowReason 测试只有 manual_only 给出原因
func TestDecisionRoute_NoAutoAllowReason(t *testing.T) {
	if got := (DecisionRoute{Reason: "需人工", Source: "manual_only"}).NoAutoAllowReason(); got != "需人工" {
		t.Errorf("manual_only: NoAutoAllowReason = %q, want 需人工", got)
	}
	if got := (DecisionRoute{Reason: "x", Source: "baseline"}).NoAutoAllowReason(); got != "" {
		t.Errorf("baseline: NoAutoAllowReason = %q, want 空", got)
	}
}

// ──────────────────────────── ToolDecisionFacts ────────────────────────────

// TestBuildToolDecisionFacts_基本字段 测试基本投影
func TestBuildToolDecisionFacts_基本字段(t *testing.T) {
	ws := t.TempDir()
	facts := BuildToolDecisionFacts("bash", map[string]any{"command": "ls -la"}, ws, "", true, nil, nil)

	if facts.ToolName() != "bash" {
		t.Errorf("ToolName = %q, want bash", facts.ToolName())
	}
	if facts.ToolCategory() != "shell" {
		t.Errorf("ToolCategory = %q, want shell", facts.ToolCategory())
	}
	if facts.Command != "ls -la" {
		t.Errorf("Command = %q, want ls -la", facts.Command)
	}
	if !facts.ArgumentsValidObject {
		t.Error("ArgumentsValidObject = false, want true")
	}
	if facts.WorkspaceRoot != ws {
		t.Errorf("WorkspaceRoot = %q, want %q", facts.WorkspaceRoot, ws)
	}
}

// TestBuildToolDecisionFacts_paths去重 测试 paths 去重且保序
func TestBuildToolDecisionFacts_paths去重(t *testing.T) {
	facts := ToolDecisionFacts{
		ReadPaths:  []string{"/a", "/b"},
		WritePaths: []string{"/b", "/c"},
	}
	got := facts.Paths()
	want := []string{"/a", "/b", "/c"}
	if len(got) != len(want) {
		t.Fatalf("Paths() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Paths()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestBuildToolDecisionFacts_read工具抽读路径 测试 read_file 的路径进入 ReadPaths
func TestBuildToolDecisionFacts_read工具抽读路径(t *testing.T) {
	ws := t.TempDir()
	target := filepath.Join(ws, "a.txt")
	facts := BuildToolDecisionFacts("read_file", map[string]any{"file_path": target}, ws, "", true, nil, nil)

	if len(facts.ReadPaths) != 1 || facts.ReadPaths[0] != target {
		t.Errorf("ReadPaths = %v, want [%s]", facts.ReadPaths, target)
	}
	if len(facts.WritePaths) != 0 {
		t.Errorf("WritePaths = %v, want 空", facts.WritePaths)
	}
	if !facts.AccessesKnown {
		t.Error("AccessesKnown = false, want true")
	}
}

// TestBuildToolDecisionFacts_write工具抽写路径 测试 write_file 的路径进入 WritePaths
func TestBuildToolDecisionFacts_write工具抽写路径(t *testing.T) {
	ws := t.TempDir()
	target := filepath.Join(ws, "b.txt")
	facts := BuildToolDecisionFacts("write_file", map[string]any{"file_path": target}, ws, "", true, nil, nil)

	if len(facts.WritePaths) != 1 || facts.WritePaths[0] != target {
		t.Errorf("WritePaths = %v, want [%s]", facts.WritePaths, target)
	}
	if len(facts.ReadPaths) != 0 {
		t.Errorf("ReadPaths = %v, want 空", facts.ReadPaths)
	}
}

// TestBuildToolDecisionFacts_非路径类返回空且known 测试网络/记忆类不出路径但 known=true
func TestBuildToolDecisionFacts_非路径类返回空且known(t *testing.T) {
	ws := t.TempDir()
	facts := BuildToolDecisionFacts("mcp_fetch_webpage", map[string]any{"url": "https://x.com"}, ws, "", true, nil, nil)

	if len(facts.ReadPaths) != 0 || len(facts.WritePaths) != 0 {
		t.Errorf("非路径类不应抽路径，got read=%v write=%v", facts.ReadPaths, facts.WritePaths)
	}
	if !facts.AccessesKnown {
		t.Error("非路径类的 AccessesKnown 应为 true（确定无访问）")
	}
}

// TestBuildToolDecisionFacts_无root时unknown 测试无 workspaceRoot 时 accesses_known=false
func TestBuildToolDecisionFacts_无root时unknown(t *testing.T) {
	facts := BuildToolDecisionFacts("read_file", map[string]any{"file_path": "/x"}, "", "", true, nil, nil)
	if facts.AccessesKnown {
		t.Error("无 root 时 AccessesKnown 应为 false")
	}
	if facts.WorkspaceRoot != "" {
		t.Errorf("WorkspaceRoot = %q, want 空", facts.WorkspaceRoot)
	}
}

// TestBuildToolDecisionFacts_sendFile用sendPaths 测试 send_file_to_user 用 send_paths
func TestBuildToolDecisionFacts_sendFile用sendPaths(t *testing.T) {
	ws := t.TempDir()
	sendPaths := []string{"/a/file1", "", "/b/file2", "/a/file1"}
	facts := BuildToolDecisionFacts("send_file_to_user", map[string]any{}, ws, "", true, nil, sendPaths)

	if len(facts.ReadPaths) != 2 {
		t.Fatalf("ReadPaths = %v, want 2 项（去重去空）", facts.ReadPaths)
	}
	if facts.ReadPaths[0] != "/a/file1" || facts.ReadPaths[1] != "/b/file2" {
		t.Errorf("ReadPaths = %v, 顺序或内容错", facts.ReadPaths)
	}
	if !facts.AccessesKnown {
		t.Error("有 send_paths 时 AccessesKnown 应为 true")
	}
	// 无 send_paths 时 unknown
	empty := BuildToolDecisionFacts("send_file_to_user", map[string]any{}, ws, "", true, nil, nil)
	if empty.AccessesKnown {
		t.Error("无 send_paths 时 AccessesKnown 应为 false")
	}
}

// TestBuildToolDecisionFacts_externalPaths去重 测试外部路径去重
func TestBuildToolDecisionFacts_externalPaths去重(t *testing.T) {
	ws := t.TempDir()
	facts := BuildToolDecisionFacts("read_file", map[string]any{}, ws, "", true,
		[]string{"/e1", "/e2", "/e1"}, nil)
	if len(facts.ExternalPaths) != 2 {
		t.Errorf("ExternalPaths = %v, want 2 项去重", facts.ExternalPaths)
	}
}

// TestBuildToolDecisionFacts_command仅shell 测试只有 shell 类才取 command
func TestBuildToolDecisionFacts_command仅shell(t *testing.T) {
	ws := t.TempDir()
	// 非 shell 类即使有 command 字段也不取
	facts := BuildToolDecisionFacts("read_file", map[string]any{"command": "ls"}, ws, "", true, nil, nil)
	if facts.Command != "" {
		t.Errorf("非 shell 类的 Command = %q, want 空", facts.Command)
	}
	// shell 类取 command
	shell := BuildToolDecisionFacts("bash", map[string]any{"command": "  ls  "}, ws, "", true, nil, nil)
	if shell.Command != "ls" {
		t.Errorf("shell 的 Command = %q, want ls（trimmed）", shell.Command)
	}
	// shell 类回退到 cmd
	cmd := BuildToolDecisionFacts("bash", map[string]any{"cmd": "dir"}, ws, "", true, nil, nil)
	if cmd.Command != "dir" {
		t.Errorf("shell 的 Command = %q, want dir（回退 cmd）", cmd.Command)
	}
}

// TestBuildToolDecisionFacts_lsp_workspaceSymbol 测试 lsp 的 workspaceSymbol 分支
//
// lsp 在 tool_capabilities 的 _PATH_TOOLS 里（category=path），但不在
// fileguard 的 file_tool_specs 里，也不在 security._PATH_TOOLS 里，
// 因此 extract_accesses_native 对它返回空。Python 侧实际行为同样为空。
// 这里验证的是：lsp 走通了 path 分支（不返回 unknown），且结果与 Python 一致。
func TestBuildToolDecisionFacts_lsp_workspaceSymbol(t *testing.T) {
	ws := t.TempDir()
	facts := BuildToolDecisionFacts("lsp",
		map[string]any{"operation": "workspaceSymbol"}, ws, "", true, nil, nil)
	// 无抽到路径 → known=false（Python: bool(reads or writes) 为假）
	if facts.AccessesKnown {
		t.Error("lsp 无路径可抽时 AccessesKnown 应为 false（对齐 Python）")
	}
	if len(facts.ReadPaths) != 0 || len(facts.WritePaths) != 0 {
		t.Errorf("lsp 不应抽出路径，got read=%v write=%v", facts.ReadPaths, facts.WritePaths)
	}
}

// TestBuildToolDecisionFacts_platformTrustedRoot 测试 platform_trusted_root 投影
func TestBuildToolDecisionFacts_platformTrustedRoot(t *testing.T) {
	ws := t.TempDir()
	trusted := t.TempDir()
	facts := BuildToolDecisionFacts("read_file", map[string]any{}, ws, trusted, true, nil, nil)
	if facts.PlatformTrustedRoot != trusted {
		t.Errorf("PlatformTrustedRoot = %q, want %q", facts.PlatformTrustedRoot, trusted)
	}
}

// TestWorkspaceRootOf 测试空串与非法输入
func TestWorkspaceRootOf(t *testing.T) {
	if got := workspaceRootOf(""); got != "" {
		t.Errorf("空串 → %q, want 空", got)
	}
	if got := workspaceRootOf("   "); got != "" {
		t.Errorf("空白 → %q, want 空", got)
	}
	abs := t.TempDir()
	if got := workspaceRootOf(abs); got != abs {
		t.Errorf("绝对路径 → %q, want %q", got, abs)
	}
}

// TestBuildToolDecisionFacts_untrustedArgs不被修改 测试 untrusted_args 是副本
func TestBuildToolDecisionFacts_untrustedArgs不被修改(t *testing.T) {
	ws := t.TempDir()
	args := map[string]any{"file_path": "/x"}
	facts := BuildToolDecisionFacts("read_file", args, ws, "", true, nil, nil)
	facts.UntrustedArgs["injected"] = true
	if _, has := args["injected"]; has {
		t.Error("改 UntrustedArgs 影响了原始入参")
	}
}

// TestWorkspaceRootOf_波浪号展开 测试 ~ 前缀被展开
func TestWorkspaceRootOf_波浪号展开(t *testing.T) {
	home := userHomeDir()
	if home == "" {
		t.Skip("无 home 目录")
	}
	got := workspaceRootOf("~/mydir")
	if strings.Contains(got, "~") {
		t.Errorf("未展开 ~：%q", got)
	}
	if !strings.HasPrefix(got, filepath.ToSlash(home)) {
		t.Errorf("got %q，应以 home %q 开头", got, home)
	}
	// 单独的 ~
	if got := workspaceRootOf("~"); got != filepath.ToSlash(home) {
		t.Errorf("workspaceRootOf(~) = %q, want %q", got, filepath.ToSlash(home))
	}
}

// TestCoreAccesses_native投影优先 测试 native 投影命中时短路
func TestCoreAccesses_native投影优先(t *testing.T) {
	ws := t.TempDir()
	target := filepath.Join(ws, "native.txt")
	args := map[string]any{"file_path": target}
	SetNativePathAccess(&NativePathAccess{
		ToolName:      "read_file",
		ArgumentsJSON: NativeArgumentsJSON(args),
		Path:          target,
		Action:        "read",
	})
	defer SetNativePathAccess(nil)

	capability := ClassifyTool("read_file")
	reads, writes, known := coreAccesses(capability, args, ws, nil)
	if len(reads) != 1 || reads[0] != target {
		t.Errorf("reads = %v, want [%s]", reads, target)
	}
	if len(writes) != 0 {
		t.Errorf("writes = %v, want 空", writes)
	}
	if !known {
		t.Error("known = false, want true")
	}

	// write 投影进 writes
	SetNativePathAccess(&NativePathAccess{
		ToolName:      "write_file",
		ArgumentsJSON: NativeArgumentsJSON(args),
		Path:          target,
		Action:        "write",
	})
	capW := ClassifyTool("write_file")
	reads2, writes2, known2 := coreAccesses(capW, args, ws, nil)
	if len(reads2) != 0 || len(writes2) != 1 || writes2[0] != target {
		t.Errorf("write 投影：reads=%v writes=%v, want 空 + [%s]", reads2, writes2, target)
	}
	if !known2 {
		t.Error("known2 = false, want true")
	}
}

// TestCoreAccesses_shell恒known 测试 shell 类无路径时 known 仍为 true
func TestCoreAccesses_shell恒known(t *testing.T) {
	ws := t.TempDir()
	capability := ClassifyTool("bash")
	// echo 不是 pathAwareCommand，抽不到路径
	reads, writes, known := coreAccesses(capability, map[string]any{"command": "echo hi"}, ws, nil)
	if len(reads) != 0 || len(writes) != 0 {
		t.Errorf("echo 不应抽路径，got read=%v write=%v", reads, writes)
	}
	if !known {
		t.Error("shell 类的 known 应恒为 true（对齐 Python）")
	}
}

// TestCoreAccesses_exec进writes 测试 exec 动作归入写路径
func TestCoreAccesses_exec进writes(t *testing.T) {
	ws := t.TempDir()
	script := filepath.Join(ws, "run.sh")
	capability := ClassifyTool("bash")
	_, writes, _ := coreAccesses(capability,
		map[string]any{"command": "bash " + script}, ws, nil)
	found := false
	for _, w := range writes {
		if w == script {
			found = true
		}
	}
	if !found {
		t.Errorf("exec 路径应进 writes，got %v", writes)
	}
}
