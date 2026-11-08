package permissions

import (
	"path/filepath"
	"strings"
	"testing"

	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
)

// ──────────────────────────── sandbox_profile ────────────────────────────

// TestBuildSandboxProfile_三种形态 测试 nil / map / struct 三种输入
func TestBuildSandboxProfile_三种形态(t *testing.T) {
	// nil
	if got := BuildSandboxProfile(nil); got != (SandboxDescriptor{}) {
		t.Errorf("nil 应返回空描述符，got %+v", got)
	}

	// map 形态
	got := BuildSandboxProfile(map[string]any{
		"sandbox_execution_workspace_root": "/ws",
		"sandbox_python_package_venv_root": "/venv",
	})
	if got.ExecutionWorkspaceRoot != "/ws" {
		t.Errorf("ExecutionWorkspaceRoot = %q, want /ws", got.ExecutionWorkspaceRoot)
	}
	if got.PythonPackageVenvRoot != "/venv" {
		t.Errorf("PythonPackageVenvRoot = %q, want /venv", got.PythonPackageVenvRoot)
	}

	// struct 形态（按 json tag 匹配）
	type fakeSysOp struct {
		Root string `json:"sandbox_execution_workspace_root"`
	}
	got2 := BuildSandboxProfile(fakeSysOp{Root: "/s"})
	if got2.ExecutionWorkspaceRoot != "/s" {
		t.Errorf("struct 形态 ExecutionWorkspaceRoot = %q, want /s", got2.ExecutionWorkspaceRoot)
	}
}

// TestBuildSandboxProfile_空白与非字符串回落 测试空白与非字符串返回空串
func TestBuildSandboxProfile_空白与非字符串回落(t *testing.T) {
	got := BuildSandboxProfile(map[string]any{
		"sandbox_execution_workspace_root": "   ",
		"sandbox_python_package_venv_root": 123,
	})
	if got.ExecutionWorkspaceRoot != "" {
		t.Errorf("空白应回落空串，got %q", got.ExecutionWorkspaceRoot)
	}
	if got.PythonPackageVenvRoot != "" {
		t.Errorf("非字符串应回落空串，got %q", got.PythonPackageVenvRoot)
	}
}

// TestValueGet_各形态 测试 _value_get 的三种输入
func TestValueGet_各形态(t *testing.T) {
	if got := valueGet(nil, "x"); got != nil {
		t.Errorf("nil source 应返回 nil，got %v", got)
	}
	if got := valueGet(map[string]any{"x": 1}, "x"); got != 1 {
		t.Errorf("map 取值 = %v, want 1", got)
	}
	if got := valueGet(map[string]any{}, "missing"); got != nil {
		t.Errorf("缺失键应返回 nil，got %v", got)
	}
	// 非 struct 非 map
	if got := valueGet("string", "x"); got != nil {
		t.Errorf("字符串 source 应返回 nil，got %v", got)
	}
	// 指针 nil
	var p *struct{ X int }
	if got := valueGet(p, "X"); got != nil {
		t.Errorf("nil 指针应返回 nil，got %v", got)
	}
}

// ──────────────────────────── native_path_context ────────────────────────────

// TestNativeArgumentsJSON_ASCII转义 测试非 ASCII 被转义（对齐 ensure_ascii=True）
//
// Python 的 json.dumps(ensure_ascii=True) 会把中文转成 \uXXXX，
// Go 的 json.Marshal 默认输出 UTF-8。字符串比较依赖二者一致，必须转义。
func TestNativeArgumentsJSON_ASCII转义(t *testing.T) {
	got := NativeArgumentsJSON(map[string]any{"path": "/tmp/中文.txt"})
	if !containsASCIIEscape(got) {
		t.Errorf("NativeArgumentsJSON 未做 ASCII 转义: %q", got)
	}
	// 纯 ASCII 不应被转义
	plain := NativeArgumentsJSON(map[string]any{"path": "/tmp/a.txt"})
	if containsASCIIEscape(plain) {
		t.Errorf("纯 ASCII 不应被转义: %q", plain)
	}
}

// TestNativeArgumentsJSON_键排序 测试 sort_keys=True
func TestNativeArgumentsJSON_键排序(t *testing.T) {
	a := NativeArgumentsJSON(map[string]any{"b": 1, "a": 2})
	b := NativeArgumentsJSON(map[string]any{"a": 2, "b": 1})
	if a != b {
		t.Errorf("键顺序不同的同内容应产生相同 JSON: %q vs %q", a, b)
	}
	if a != `{"a":2,"b":1}` {
		t.Errorf("got %q, want {\"a\":2,\"b\":1}", a)
	}
}

// TestCurrentNativePathAccess_匹配 测试工具名与参数都匹配时返回访问
func TestCurrentNativePathAccess_匹配(t *testing.T) {
	target := "/tmp/native.txt"
	args := map[string]any{"file_path": target}
	SetNativePathAccess(&NativePathAccess{
		ToolName:      "read_file",
		ArgumentsJSON: NativeArgumentsJSON(args),
		Path:          target,
		Action:        "read",
	})
	defer SetNativePathAccess(nil)

	got := CurrentNativePathAccess("read_file", args)
	if got == nil {
		t.Fatal("应返回匹配的访问")
	}
	if got.Path != target {
		t.Errorf("Path = %q, want %q", got.Path, target)
	}
}

// TestCurrentNativePathAccess_工具名不匹配 测试工具名不同时返回 nil
func TestCurrentNativePathAccess_工具名不匹配(t *testing.T) {
	args := map[string]any{"file_path": "/x"}
	SetNativePathAccess(&NativePathAccess{
		ToolName:      "read_file",
		ArgumentsJSON: NativeArgumentsJSON(args),
		Path:          "/x",
		Action:        "read",
	})
	defer SetNativePathAccess(nil)

	if got := CurrentNativePathAccess("write_file", args); got != nil {
		t.Errorf("工具名不匹配应返回 nil，got %+v", got)
	}
}

// TestCurrentNativePathAccess_参数不匹配 测试参数不同时返回 nil
func TestCurrentNativePathAccess_参数不匹配(t *testing.T) {
	SetNativePathAccess(&NativePathAccess{
		ToolName:      "read_file",
		ArgumentsJSON: NativeArgumentsJSON(map[string]any{"file_path": "/a"}),
		Path:          "/a",
		Action:        "read",
	})
	defer SetNativePathAccess(nil)

	got := CurrentNativePathAccess("read_file", map[string]any{"file_path": "/b"})
	if got != nil {
		t.Errorf("参数不匹配应返回 nil，got %+v", got)
	}
}

// TestCurrentNativePathAccess_未设置 测试未设置时返回 nil
func TestCurrentNativePathAccess_未设置(t *testing.T) {
	SetNativePathAccess(nil)
	if got := CurrentNativePathAccess("read_file", map[string]any{"file_path": "/x"}); got != nil {
		t.Errorf("未设置时应返回 nil，got %+v", got)
	}
}

// TestNativePathAccess_GuardTool 测试 guard_tool 派生
func TestNativePathAccess_GuardTool(t *testing.T) {
	w := &NativePathAccess{Action: "write"}
	if got := w.GuardTool(); got != "write_file" {
		t.Errorf("write → %q, want write_file", got)
	}
	r := &NativePathAccess{Action: "read"}
	if got := r.GuardTool(); got != "read_file" {
		t.Errorf("read → %q, want read_file", got)
	}
	e := &NativePathAccess{Action: "exec"}
	if got := e.GuardTool(); got != "read_file" {
		t.Errorf("exec → %q, want read_file（非 write 都按 read）", got)
	}
}

// TestNativePathAccess_GuardArgs 测试 guard_args 只含 file_path
func TestNativePathAccess_GuardArgs(t *testing.T) {
	a := &NativePathAccess{Path: "/p"}
	got := a.GuardArgs()
	if len(got) != 1 || got["file_path"] != "/p" {
		t.Errorf("GuardArgs() = %v, want {file_path: /p}", got)
	}
}

// TestNativePathGuardProjection_有投影 测试有投影时用 guard 工具与参数调用检查器
func TestNativePathGuardProjection_有投影(t *testing.T) {
	target := filepath.Join(t.TempDir(), "proj.txt")
	args := map[string]any{"file_path": target}
	SetNativePathAccess(&NativePathAccess{
		ToolName:      "read_file",
		ArgumentsJSON: NativeArgumentsJSON(args),
		Path:          target,
		Action:        "read",
	})
	defer SetNativePathAccess(nil)

	var calledTool string
	var calledArgs map[string]any
	fake := fakeEvaluator{
		onEvaluate: func(toolName string, toolArgs map[string]any) *harnesssecurity.PermissionResult {
			calledTool = toolName
			calledArgs = toolArgs
			return nil
		},
	}
	proj := NewNativePathGuardProjection(fake)
	_ = proj.Evaluate("read_file", args)

	if calledTool != "read_file" {
		t.Errorf("tool = %q, want read_file（guard_tool）", calledTool)
	}
	if calledArgs["file_path"] != target {
		t.Errorf("args = %v, want {file_path: %s}", calledArgs, target)
	}
}

// TestNativePathGuardProjection_无投影 测试无投影时透传原工具与参数
func TestNativePathGuardProjection_无投影(t *testing.T) {
	SetNativePathAccess(nil)
	var calledTool string
	fake := fakeEvaluator{
		onEvaluate: func(toolName string, toolArgs map[string]any) *harnesssecurity.PermissionResult {
			calledTool = toolName
			return nil
		},
	}
	proj := NewNativePathGuardProjection(fake)
	_ = proj.Evaluate("some_tool", map[string]any{"x": 1})

	if calledTool != "some_tool" {
		t.Errorf("tool = %q, want some_tool（透传）", calledTool)
	}
}

// fakeEvaluator 用于测试的路径判定器替身。
type fakeEvaluator struct {
	onEvaluate func(toolName string, toolArgs map[string]any) *harnesssecurity.PermissionResult
}

// Enabled 路径层是否启用
func (f fakeEvaluator) Enabled() bool { return true }

// Mode 判定模式
func (f fakeEvaluator) Mode() string { return "native" }

// Evaluate 返回预置结果
func (f fakeEvaluator) Evaluate(toolName string, toolArgs map[string]any) *harnesssecurity.PermissionResult {
	if f.onEvaluate != nil {
		return f.onEvaluate(toolName, toolArgs)
	}
	return nil
}

// CollectAskAccesses 返回空
func (f fakeEvaluator) CollectAskAccesses(string, map[string]any) []harnesssecurity.PathAction {
	return nil
}

// containsASCIIEscape 判定字符串是否含 \uXXXX 形式的转义。
func containsASCIIEscape(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '\\' && s[i+1] == 'u' {
			return true
		}
	}
	return false
}

// TestNativeArgumentsJSON_emoji转义 测试 BMP 外字符被转义为 ASCII
//
// strconv.QuoteToASCII 输出 \U0001f600（Go 风格），而 Python
// json.dumps(ensure_ascii=True) 输出代理对 \ud83d\ude00。本项目只做同进程内
// 相等比较（两侧都经 NativeArgumentsJSON 产出），字面差异不影响正确性。
func TestNativeArgumentsJSON_emoji转义(t *testing.T) {
	got := NativeArgumentsJSON(map[string]any{"e": "😀"})
	if containsASCIIEscape(got) {
		t.Errorf("emoji 应被转义: %q", got)
	}
	// 必须含 ASCII 转义形式，不能是原 UTF-8 字节
	if strings.Contains(got, "😀") {
		t.Errorf("emoji 未被转义: %q", got)
	}
	// 同一输入必须产生同一输出（自洽性）
	if again := NativeArgumentsJSON(map[string]any{"e": "😀"}); again != got {
		t.Errorf("同输入输出不稳定: %q vs %q", got, again)
	}
}

// TestNativeArgumentsJSON_数值与布尔 测试数值/布尔/null 的序列化
func TestNativeArgumentsJSON_数值与布尔(t *testing.T) {
	got := NativeArgumentsJSON(map[string]any{"f": 1.5, "n": nil, "t": true})
	want := `{"f":1.5,"n":null,"t":true}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestNativeArgumentsJSON_转义字符 测试引号/反斜杠/换行/制表符
func TestNativeArgumentsJSON_转义字符(t *testing.T) {
	got := NativeArgumentsJSON(map[string]any{"s": "a\"b\\c\nd\te"})
	want := `{"s":"a\"b\\c\nd\te"}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestNativeArgumentsJSON_控制字符 测试控制字符被转义为 ASCII
//
// strconv.QuoteToASCII 输出 \x01（两位十六进制），Python 输出 \u0001。差异只
// 影响字面形式，不影响相等判定。
func TestNativeArgumentsJSON_控制字符(t *testing.T) {
	got := NativeArgumentsJSON(map[string]any{"s": "\x01"})
	// 结果里不能出现原始控制字符字节
	if strings.ContainsRune(got, 0x01) {
		t.Errorf("控制字符未被转义: %q", got)
	}
	// 必须出现转义序列
	if !strings.Contains(got, `\x01`) && !strings.Contains(got, `\u0001`) {
		t.Errorf("控制字符无转义序列: %q", got)
	}
}

// TestNativeArgumentsJSON_嵌套 测试嵌套对象与数组
func TestNativeArgumentsJSON_嵌套(t *testing.T) {
	got := NativeArgumentsJSON(map[string]any{
		"z":   map[string]any{"b": 1, "a": 2},
		"arr": []any{1, 2},
	})
	want := `{"arr":[1,2],"z":{"a":2,"b":1}}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestNativeArgumentsJSON_nil与空 测试 nil 与空 map
func TestNativeArgumentsJSON_nil与空(t *testing.T) {
	if got := NativeArgumentsJSON(nil); got != "{}" {
		t.Errorf("nil → %q, want {}", got)
	}
	if got := NativeArgumentsJSON(map[string]any{}); got != "{}" {
		t.Errorf("空 map → %q, want {}", got)
	}
}
