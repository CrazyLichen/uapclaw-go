package permissions

import (
	"testing"

	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
)

// ──────────────────────────── NativePathGuardProjection 接口方法（9.90d-1 Task 13） ────────────────────────────

// fakeFileGuard 测试用路径判定器替身。
type fakeFileGuard struct {
	enabled        bool
	mode           string
	evalResult     *harnesssecurity.PermissionResult
	askAccesses    []harnesssecurity.PathAction
	lastToolName   string
	lastToolArgs   map[string]any
	evaluateCalled int
	collectCalled  int
}

// Enabled 实现 FileGuardEvaluator
func (f *fakeFileGuard) Enabled() bool { return f.enabled }

// Mode 实现 FileGuardEvaluator
func (f *fakeFileGuard) Mode() string { return f.mode }

// Evaluate 实现 FileGuardEvaluator
func (f *fakeFileGuard) Evaluate(toolName string, toolArgs map[string]any) *harnesssecurity.PermissionResult {
	f.evaluateCalled++
	f.lastToolName = toolName
	f.lastToolArgs = toolArgs
	return f.evalResult
}

// CollectAskAccesses 实现 FileGuardEvaluator
func (f *fakeFileGuard) CollectAskAccesses(toolName string, toolArgs map[string]any) []harnesssecurity.PathAction {
	f.collectCalled++
	f.lastToolName = toolName
	f.lastToolArgs = toolArgs
	return f.askAccesses
}

// TestNativePathGuardProjection_Enabled透传 验证 Enabled 转发内层。
func TestNativePathGuardProjection_Enabled透传(t *testing.T) {
	for _, want := range []bool{true, false} {
		p := NewNativePathGuardProjection(&fakeFileGuard{enabled: want})
		if got := p.Enabled(); got != want {
			t.Errorf("Enabled() = %v, want %v", got, want)
		}
	}
	// nil 安全
	var nilProj *NativePathGuardProjection
	if nilProj.Enabled() {
		t.Error("nil projection should return false")
	}
	if NewNativePathGuardProjection(nil).Enabled() {
		t.Error("nil checker should return false")
	}
}

// TestNativePathGuardProjection_Mode透传 验证 Mode 转发内层。
func TestNativePathGuardProjection_Mode透传(t *testing.T) {
	for _, want := range []string{"native", "legacy", ""} {
		p := NewNativePathGuardProjection(&fakeFileGuard{mode: want})
		if got := p.Mode(); got != want {
			t.Errorf("Mode() = %q, want %q", got, want)
		}
	}
	var nilProj *NativePathGuardProjection
	if nilProj.Mode() != "" {
		t.Error("nil projection should return empty mode")
	}
}

// TestNativePathGuardProjection_CollectAskAccesses透传 验证无投影时透传。
func TestNativePathGuardProjection_CollectAskAccesses透传(t *testing.T) {
	want := []harnesssecurity.PathAction{{Path: "/a", Action: "read"}}
	inner := &fakeFileGuard{askAccesses: want}
	p := NewNativePathGuardProjection(inner)

	got := p.CollectAskAccesses("read_file", map[string]any{"file_path": "/a"})
	if len(got) != 1 || got[0].Path != "/a" {
		t.Errorf("got %v", got)
	}
	if inner.lastToolName != "read_file" {
		t.Errorf("inner should receive tool name, got %q", inner.lastToolName)
	}
}

// TestNativePathGuardProjection_CollectAskAccesses用投影 验证有投影时改派 guard 工具。
func TestNativePathGuardProjection_CollectAskAccesses用投影(t *testing.T) {
	args := map[string]any{"file_path": "/x"}
	access := &NativePathAccess{
		ToolName:      "write_file",
		ArgumentsJSON: NativeArgumentsJSON(args),
		Path:          "/x",
		Action:        "write",
	}
	SetNativePathAccess(access)
	defer SetNativePathAccess(nil)

	inner := &fakeFileGuard{askAccesses: []harnesssecurity.PathAction{{Path: "/x", Action: "write"}}}
	p := NewNativePathGuardProjection(inner)
	got := p.CollectAskAccesses("write_file", args)

	if len(got) != 1 {
		t.Errorf("got %v", got)
	}
	// 投影后应改用 guard 工具（write → write_file）与 guard 参数
	if inner.lastToolName != "write_file" {
		t.Errorf("should dispatch to guard tool, got %q", inner.lastToolName)
	}
	if inner.lastToolArgs["file_path"] != "/x" {
		t.Errorf("guard args = %v", inner.lastToolArgs)
	}
}

// TestNativePathGuardProjection_nil安全 验证 nil 投影与 nil checker 不 panic。
func TestNativePathGuardProjection_nil安全(t *testing.T) {
	var nilProj *NativePathGuardProjection
	if got := nilProj.CollectAskAccesses("t", nil); got != nil {
		t.Errorf("nil projection -> %v", got)
	}
	if got := NewNativePathGuardProjection(nil).CollectAskAccesses("t", nil); got != nil {
		t.Errorf("nil checker -> %v", got)
	}
}
