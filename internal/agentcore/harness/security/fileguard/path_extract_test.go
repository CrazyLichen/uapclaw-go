package fileguard

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestExtractAccessesNative_注册spec分支 测试按 spec 的 arg_name 抽路径
func TestExtractAccessesNative_注册spec分支(t *testing.T) {
	ws := t.TempDir()
	args := map[string]any{"file_path": filepath.Join(ws, "a.txt")}

	got := ExtractAccessesNative("read_file", args, ws, nil)
	if len(got) == 0 {
		t.Fatal("未抽出任何访问")
	}
	if got[0].Action != FileActionRead {
		t.Errorf("Action = %v, want read", got[0].Action)
	}
	if got[0].Source != "tool_arg" {
		t.Errorf("Source = %q, want tool_arg", got[0].Source)
	}
	if got[0].Path != filepath.Join(ws, "a.txt") {
		t.Errorf("Path = %q, want %q", got[0].Path, filepath.Join(ws, "a.txt"))
	}
}

// TestExtractAccessesNative_写工具 测试写工具的 action 是 write
func TestExtractAccessesNative_写工具(t *testing.T) {
	ws := t.TempDir()
	args := map[string]any{"file_path": filepath.Join(ws, "b.txt")}

	got := ExtractAccessesNative("write_file", args, ws, nil)
	if len(got) == 0 {
		t.Fatal("未抽出任何访问")
	}
	if got[0].Action != FileActionWrite {
		t.Errorf("Action = %v, want write", got[0].Action)
	}
}

// TestExtractAccessesNative_无spec的路径工具 测试走 PathTools 兜底分支
//
// Python 的 _PATH_TOOLS 只有 13 项且全部有注册 spec，因此 PathTools 兜底
// 分支在当前内置配置下不可达。本用例通过注销一个 spec 来验证该分支：
// 先注册一个假工具名到 PathTools 无法做到（PathTools 是只读导出），
// 改为直接验证「有 spec 优先于 PathTools」与「PathTools 判断本身」。
func TestExtractAccessesNative_无spec的路径工具(t *testing.T) {
	ws := t.TempDir()
	// apply_patch 不在 Python 的 _PATH_TOOLS 里，抽取应为空
	args := map[string]any{"file_path": filepath.Join(ws, "c.txt")}
	if got := ExtractAccessesNative("apply_patch", args, ws, nil); len(got) != 0 {
		t.Errorf("apply_patch 不在 _PATH_TOOLS，应抽出 0 条，实际 %+v", got)
	}
	// search_replace 有 spec（3 个参数），验证 spec 分支优先生效
	args2 := map[string]any{"target_file": filepath.Join(ws, "d.txt")}
	got := ExtractAccessesNative("search_replace", args2, ws, nil)
	if len(got) != 1 {
		t.Fatalf("search_replace target_file 应抽出 1 条，实际 %d 条", len(got))
	}
	if got[0].Action != FileActionWrite {
		t.Errorf("Action = %v, want write", got[0].Action)
	}
}

// TestExtractAccessesNative_shell分支 测试 shell 工具走 shlex 抽取
func TestExtractAccessesNative_shell分支(t *testing.T) {
	ws := t.TempDir()
	target := filepath.Join(ws, "x.txt")
	args := map[string]any{"command": "cat " + target}

	got := ExtractAccessesNative("bash", args, ws, nil)
	if len(got) == 0 {
		t.Fatal("未抽出任何访问")
	}
	if got[0].Source != "shlex" {
		t.Errorf("Source = %q, want shlex", got[0].Source)
	}
}

// TestExtractAccessesNative_未知工具 测试无 spec 且非 path/shell 返回空
func TestExtractAccessesNative_未知工具(t *testing.T) {
	ws := t.TempDir()
	got := ExtractAccessesNative("some_unknown_tool", map[string]any{"x": "y"}, ws, nil)
	if len(got) != 0 {
		t.Errorf("got %d 条访问, want 0", len(got))
	}
}

// TestExtractAccessesNative_多个spec参数 测试 read_file 的两个参数都抽
func TestExtractAccessesNative_多个spec参数(t *testing.T) {
	ws := t.TempDir()
	args := map[string]any{
		"file_path": filepath.Join(ws, "one.txt"),
		"path":      filepath.Join(ws, "two.txt"),
	}
	got := ExtractAccessesNative("read_file", args, ws, nil)
	if len(got) != 2 {
		t.Fatalf("got %d 条访问, want 2（read_file 有 file_path 与 path 两个 spec）", len(got))
	}
}

// TestExtractAccessesNative_spec参数为空跳过 测试 spec 参数缺失或空白时跳过
func TestExtractAccessesNative_spec参数为空跳过(t *testing.T) {
	ws := t.TempDir()
	args := map[string]any{"file_path": "   ", "path": filepath.Join(ws, "ok.txt")}

	got := ExtractAccessesNative("read_file", args, ws, nil)
	if len(got) != 1 {
		t.Fatalf("got %d 条访问, want 1（空白参数应跳过）", len(got))
	}
	if got[0].Path != filepath.Join(ws, "ok.txt") {
		t.Errorf("Path = %q, want %q", got[0].Path, filepath.Join(ws, "ok.txt"))
	}
}

// TestExtractAccessesNative_shell脱壳后抽取 测试 bash -c 包裹的路径仍能抽出
func TestExtractAccessesNative_shell脱壳后抽取(t *testing.T) {
	ws := t.TempDir()
	target := filepath.Join(ws, "y.txt")
	args := map[string]any{"command": `bash -c "cat ` + target + `"`}

	got := ExtractAccessesNative("bash", args, ws, nil)
	if len(got) == 0 {
		t.Fatal("脱壳后未抽出任何访问")
	}
	found := false
	for _, acc := range got {
		if acc.Path == target {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("未抽到 %q，实际 %+v", target, got)
	}
}

// TestExtractAccessesNative_相对路径 测试相对路径按 workspace 解析
func TestExtractAccessesNative_相对路径(t *testing.T) {
	ws := t.TempDir()
	args := map[string]any{"file_path": "sub/rel.txt"}

	got := ExtractAccessesNative("read_file", args, ws, nil)
	if len(got) == 0 {
		t.Fatal("未抽出任何访问")
	}
	want := filepath.Join(ws, "sub/rel.txt")
	if got[0].Path != want {
		t.Errorf("Path = %q, want %q", got[0].Path, want)
	}
}

// TestPathIsAbs 测试 Windows 盘符形式在非 Windows 平台也判为绝对路径
func TestPathIsAbs(t *testing.T) {
	if !pathIsAbs("/abs/path") {
		t.Error("/abs/path 应为绝对路径")
	}
	if pathIsAbs("rel/path") {
		t.Error("rel/path 不应为绝对路径")
	}
	// Windows 盘符：在 Linux 上 filepath.IsAbs 返回 false，但语义上是绝对的
	if !pathIsAbs(`C:\Users\x`) {
		t.Error(`C:\Users\x 应判为绝对路径（Windows 盘符）`)
	}
	if runtime.GOOS == "windows" {
		t.Log("当前在 Windows 上运行")
	}
}

// TestExpandHomeToken 测试 ~ 与 Windows home 环境变量展开
func TestExpandHomeToken(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("无法取 home")
	}
	if got := expandHomeToken("~/a/b"); got != filepath.Join(home, "a", "b") {
		t.Errorf("expandHomeToken(~/a/b) = %q", got)
	}
	if got := expandHomeToken("~"); got != home {
		t.Errorf("expandHomeToken(~) = %q, want %q", got, home)
	}
	// 非 ~ 开头原样返回
	if got := expandHomeToken("/abs/path"); got != "/abs/path" {
		t.Errorf("expandHomeToken(/abs/path) = %q", got)
	}
	if got := expandHomeToken("%USERPROFILE%/x"); got != filepath.ToSlash(home)+"/x" {
		t.Errorf("expandHomeToken(%%USERPROFILE%%/x) = %q", got)
	}
}

// TestLooksLikeRedirectTarget 测试重定向目标判定
func TestLooksLikeRedirectTarget(t *testing.T) {
	// 注意：Python 的 _FD_ALIAS_RE 只匹配 "&N" 形式，2>&1 / >&2 不算 fd 别名，
	// 因此它们会被当作合法重定向目标（真值）。这是 Python 原行为。
	yes := []string{"/tmp/out.txt", "a.txt", "./rel.txt", "2>&1", ">&2"}
	no := []string{"", "&1", "$VAR", "${VAR}", "$env:FOO"}
	for _, s := range yes {
		if !looksLikeRedirectTarget(s) {
			t.Errorf("looksLikeRedirectTarget(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if looksLikeRedirectTarget(s) {
			t.Errorf("looksLikeRedirectTarget(%q) = true, want false", s)
		}
	}
}

// TestIsShellFlagToken 测试命令开关判定
func TestIsShellFlagToken(t *testing.T) {
	if !isShellFlagToken("-rf", "rm") {
		t.Error("-rf 应为开关")
	}
	if !isShellFlagToken("--force", "rm") {
		t.Error("--force 应为开关")
	}
	if isShellFlagToken("-", "cat") {
		t.Error("单独的 - 不是开关（stdin 占位）")
	}
	if isShellFlagToken("/tmp/x", "cat") {
		t.Error("/tmp/x 不是开关（cat 不属 cmd 内建）")
	}
	// cmd 内建命令的 /b 是开关
	if !isShellFlagToken("/b", "dir") {
		t.Error("dir /b 中的 /b 应为开关")
	}
}

// TestStripSeparatorResidue 测试分隔符残留去除
func TestStripSeparatorResidue(t *testing.T) {
	cases := []struct{ in, want string }{
		{"file;", "file"},
		{"path|", "path"},
		{"x&&", "x"},
		{"'quoted;'", "quoted;"}, // 引号内保留
		{`a\;`, `a;`},            // 反斜杠转义保留
		{"plain", "plain"},
	}
	for _, c := range cases {
		if got := stripSeparatorResidue(c.in); got != c.want {
			t.Errorf("stripSeparatorResidue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestSplitUnquotedCommandSegments 测试按未引用的分隔符分段
func TestSplitUnquotedCommandSegments(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a; b|c", []string{"a", "b", "c"}},
		{`echo "a;b"`, []string{`echo "a;b"`}}, // 引号内不分段
		{"a && b", []string{"a", "b"}},
		{"a || b", []string{"a", "b"}},
		{`a \; b`, []string{`a \; b`}},     // 转义的分号不分段
		{"ls &>out", []string{"ls &>out"}}, // &> 是重定向不是分隔符
		{"", nil},
	}
	for _, c := range cases {
		got := splitUnquotedCommandSegments(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitUnquotedCommandSegments(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitUnquotedCommandSegments(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

// TestExtractShellPathAccesses_重定向 测试重定向目标被抽取
func TestExtractShellPathAccesses_重定向(t *testing.T) {
	ws := t.TempDir()
	out := filepath.Join(ws, "out.txt")
	got := extractShellPathAccesses("echo hi > "+out, ws, true)

	foundWrite := false
	for _, acc := range got {
		if acc.Path == out && acc.Action == FileActionWrite {
			foundWrite = true
		}
	}
	if !foundWrite {
		t.Errorf("未抽到重定向写入 %q，实际 %+v", out, got)
	}
}

// TestExtractShellPathAccesses_解释器脚本 测试解释器首参按 exec 记
func TestExtractShellPathAccesses_解释器脚本(t *testing.T) {
	ws := t.TempDir()
	script := filepath.Join(ws, "run.sh")
	got := extractShellPathAccesses("bash "+script, ws, true)

	foundExec := false
	for _, acc := range got {
		if acc.Path == script && acc.Action == FileActionExec {
			foundExec = true
		}
	}
	if !foundExec {
		t.Errorf("未抽到 exec %q（解释器首参），实际 %+v", script, got)
	}
}

// TestExtractShellPathAccesses_cd切换工作目录 测试 cd 后的相对路径按新 cwd 解析
//
// 注意：Python 只把「含 / 或 \ 的 token」当路径（见 _looks_like_path），
// 因此 `cat a.txt` 里的裸文件名 a.txt 不会被抽取。要验证 cwd 切换，
// 必须用带路径分隔符的相对路径。
func TestExtractShellPathAccesses_cd切换工作目录(t *testing.T) {
	ws := t.TempDir()
	sub := filepath.Join(ws, "sub")
	got := extractShellPathAccesses("cd "+sub+" && cat ./a.txt", ws, true)

	wantPath := filepath.Join(sub, "a.txt")
	found := false
	for _, acc := range got {
		if acc.Path == wantPath {
			found = true
		}
	}
	if !found {
		t.Errorf("cd 后未按新 cwd 解析 %q，实际 %+v", wantPath, got)
	}
}

// TestExtractShellPathAccesses_传输命令 测试 cp 首参读其余写
func TestExtractShellPathAccesses_传输命令(t *testing.T) {
	ws := t.TempDir()
	src := filepath.Join(ws, "src.txt")
	dst := filepath.Join(ws, "dst.txt")
	got := extractShellPathAccesses("cp "+src+" "+dst, ws, true)

	actions := map[string]FileAction{}
	for _, acc := range got {
		actions[acc.Path] = acc.Action
	}
	if actions[src] != FileActionRead {
		t.Errorf("src Action = %v, want read", actions[src])
	}
	if actions[dst] != FileActionWrite {
		t.Errorf("dst Action = %v, want write", actions[dst])
	}
}

// TestExtractShellPathAccesses_中文路径 测试非 ASCII 路径不被截断
//
// ⚠️ 回归测试：regexp2 的 rune 索引若被当字节索引用，中文路径会错位。
func TestExtractShellPathAccesses_中文路径(t *testing.T) {
	ws := t.TempDir()
	got := extractShellPathAccesses(`bash -c "cat /tmp/中文/文件.txt"`, ws, true)
	if len(got) == 0 {
		t.Fatal("未抽出任何访问")
	}
	if got[0].Path != "/tmp/中文/文件.txt" {
		t.Errorf("Path = %q, want /tmp/中文/文件.txt", got[0].Path)
	}
}

// TestCollectPSPathTokens 测试 PowerShell 的 -Path 系列参数收集
func TestCollectPSPathTokens(t *testing.T) {
	tokens := []string{"get-content", "-Path", `C:\a\b.txt`, "-LiteralPath", `C:\c.txt`}
	got := collectPSPathTokens(tokens)
	if len(got) != 2 {
		t.Fatalf("got %d 个 token, want 2: %v", len(got), got)
	}
	if got[0] != `C:\a\b.txt` || got[1] != `C:\c.txt` {
		t.Errorf("got %v", got)
	}
	// 非路径开关不收集
	if got := collectPSPathTokens([]string{"copy-item", "-Force", "x"}); len(got) != 0 {
		t.Errorf("非 -Path 开关不应收集，got %v", got)
	}
}

// TestExtractShellPathAccesses_PowerShell路径参数 测试 get-content -Path 抽到路径
func TestExtractShellPathAccesses_PowerShell路径参数(t *testing.T) {
	ws := t.TempDir()
	target := filepath.Join(ws, "ps.txt")
	got := extractShellPathAccesses("get-content -Path "+target, ws, true)

	found := false
	for _, acc := range got {
		if acc.Path == target {
			found = true
		}
	}
	if !found {
		t.Errorf("未抽到 -Path 参数 %q，实际 %+v", target, got)
	}
}

// TestResolveShellWorkdir 测试 workdir 解析的三种形态
func TestResolveShellWorkdir(t *testing.T) {
	ws := t.TempDir()
	// 无 workdir → workspace
	if got := resolveShellWorkdir(map[string]any{}, ws); got != ws {
		t.Errorf("无 workdir 时应返回 workspace %q，got %q", ws, got)
	}
	// 绝对 workdir 直接用
	abs := filepath.Join(ws, "abs")
	if got := resolveShellWorkdir(map[string]any{"workdir": abs}, ws); got != abs {
		t.Errorf("绝对 workdir 应原样返回 %q，got %q", abs, got)
	}
	// 相对 workdir 拼 workspace
	if got := resolveShellWorkdir(map[string]any{"workdir": "rel"}, ws); got != filepath.Join(ws, "rel") {
		t.Errorf("相对 workdir 应拼 workspace，got %q", got)
	}
}

// TestCommandFromArgs 测试命令文本提取（command 优先于 cmd）
func TestCommandFromArgs(t *testing.T) {
	if got := commandFromArgs(map[string]any{"command": "ls", "cmd": "dir"}); got != "ls" {
		t.Errorf("应优先 command，got %q", got)
	}
	if got := commandFromArgs(map[string]any{"cmd": "dir"}); got != "dir" {
		t.Errorf("无 command 时用 cmd，got %q", got)
	}
	if got := commandFromArgs(map[string]any{}); got != "" {
		t.Errorf("都缺失时返回空串，got %q", got)
	}
}

// TestIsShellTool_宿主覆盖 测试 categories.shell 覆盖默认集合
func TestIsShellTool_宿主覆盖(t *testing.T) {
	if !isShellTool("bash", nil) {
		t.Error("默认应认得 bash")
	}
	if isShellTool("not_shell", nil) {
		t.Error("未知工具不应判为 shell")
	}
	// 宿主显式指定 categories.shell 时以它为准
	cfg := map[string]any{"categories": map[string]any{"shell": []any{"my_shell"}}}
	if !isShellTool("my_shell", cfg) {
		t.Error("应认得宿主声明的 my_shell")
	}
	if isShellTool("bash", cfg) {
		t.Error("宿主覆盖后 bash 不应再算 shell")
	}
}
