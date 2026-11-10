package auto_permission

import (
	"context"
	"path/filepath"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
	swarmschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 用例：NormalizeInvocationToolArgs ───────────────────────────────

// TestNormalizeInvocationToolArgs_map输入 验证直接透传。
func TestNormalizeInvocationToolArgs_map输入(t *testing.T) {
	got := NormalizeInvocationToolArgs("read_file", map[string]any{"file_path": "/a"})
	if got["file_path"] != "/a" {
		t.Errorf("got %v", got)
	}
}

// TestNormalizeInvocationToolArgs_JSON字符串 验证解析。
func TestNormalizeInvocationToolArgs_JSON字符串(t *testing.T) {
	got := NormalizeInvocationToolArgs("read_file", `{"file_path":"/b"}`)
	if got["file_path"] != "/b" {
		t.Errorf("got %v", got)
	}
}

// TestNormalizeInvocationToolArgs_损坏JSON修复 验证修复路径。
func TestNormalizeInvocationToolArgs_损坏JSON修复(t *testing.T) {
	got := NormalizeInvocationToolArgs("read_file", `{"file_path":"/c",}`)
	if got["file_path"] != "/c" {
		t.Errorf("got %v", got)
	}
}

// TestNormalizeInvocationToolArgs_非法输入 验证返回空 map。
func TestNormalizeInvocationToolArgs_非法输入(t *testing.T) {
	for _, value := range []any{nil, 42, "not json at all", []any{1}} {
		if got := NormalizeInvocationToolArgs("read_file", value); len(got) != 0 {
			t.Errorf("%v -> %v, want empty", value, got)
		}
	}
}

// TestNormalizeInvocationToolArgs_sendFile归一 验证两个特殊字段。
func TestNormalizeInvocationToolArgs_sendFile归一(t *testing.T) {
	got := NormalizeInvocationToolArgs(SendFileToolName, map[string]any{
		"abs_file_path_list": []any{"/a", "/b"},
		"target_channels":    []any{"web"},
	})
	if _, ok := got["abs_file_path_list"].([]string); !ok {
		t.Errorf("abs_file_path_list = %T", got["abs_file_path_list"])
	}
	if _, ok := got["target_channels"].([]string); !ok {
		t.Errorf("target_channels = %T", got["target_channels"])
	}
}

// ──────────────────────────── 用例：RepairMalformedToolArguments ───────────────────────────────

// TestRepairMalformedToolArguments 验证修复与失败返回 nil。
func TestRepairMalformedToolArguments(t *testing.T) {
	// 尾逗号
	if got := RepairMalformedToolArguments(`{"a":1,}`); got["a"] == nil {
		t.Errorf("trailing comma not repaired: %v", got)
	}
	// 单引号
	if got := RepairMalformedToolArguments(`{'a':1}`); got["a"] == nil {
		t.Errorf("single quote not repaired: %v", got)
	}
	// 数组尾逗号
	if got := RepairMalformedToolArguments(`{"a":[1,]}`); got == nil {
		t.Error("array trailing comma not repaired")
	}
	// 无法修复
	if got := RepairMalformedToolArguments("not json"); got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if got := RepairMalformedToolArguments(""); got != nil {
		t.Errorf("empty -> %v", got)
	}
	if got := RepairMalformedToolArguments(`[1,2]`); got != nil {
		t.Errorf("array -> %v, want nil（非对象）", got)
	}
}

// TestRepairJSONText 验证文本级修复。
func TestRepairJSONText(t *testing.T) {
	if got := repairJSONText(""); got != "" {
		t.Errorf("empty -> %q", got)
	}
	if got := repairJSONText("plain"); got != "" {
		t.Errorf("non-json -> %q", got)
	}
	if got := repairJSONText(`{"a":1,}`); got != `{"a":1}` {
		t.Errorf("got %q", got)
	}
	if got := repairJSONText(`[1,]`); got != `[1]` {
		t.Errorf("got %q", got)
	}
}

// ──────────────────────────── ToolCallValue / 标识符 ────────────────────────────

// TestToolCallValue_map形态 验证 map 与嵌套 function 段。
func TestToolCallValue_map形态(t *testing.T) {
	call := map[string]any{"name": "bash", "arguments": `{"command":"ls"}`}
	if got := ToolCallValue(call, "name"); got != "bash" {
		t.Errorf("name = %v", got)
	}
	if got := ToolCallValue(call, "arguments"); got != `{"command":"ls"}` {
		t.Errorf("arguments = %v", got)
	}
	// 候选键 args
	if got := ToolCallValue(map[string]any{"args": "x"}, "arguments"); got != "x" {
		t.Errorf("args -> %v", got)
	}
	// 嵌套 function
	nested := map[string]any{"function": map[string]any{"name": "grep"}}
	if got := ToolCallValue(nested, "name"); got != "grep" {
		t.Errorf("nested -> %v", got)
	}
	// 缺失
	if got := ToolCallValue(map[string]any{}, "name"); got != nil {
		t.Errorf("missing -> %v", got)
	}
	// 非 map 非 ToolCall
	if got := ToolCallValue("string", "name"); got != nil {
		t.Errorf("string -> %v", got)
	}
}

// TestNormalizedIdentifier 验证标识符归一。
func TestNormalizedIdentifier(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"  abc  ", "abc"},
		{"", ""},
		{nil, ""},
		{42, ""},
		{[]string{"a"}, ""},
	}
	for _, c := range cases {
		if got := NormalizedIdentifier(c.in); got != c.want {
			t.Errorf("NormalizedIdentifier(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestNormalizedChannelIdentifier 验证通道标识符小写化。
func TestNormalizedChannelIdentifier(t *testing.T) {
	if got := NormalizedChannelIdentifier("  WEB  "); got != "web" {
		t.Errorf("got %q", got)
	}
	if got := NormalizedChannelIdentifier(nil); got != "" {
		t.Errorf("nil -> %q", got)
	}
}

// TestHostToolCallID 验证工具调用 ID 提取。
func TestHostToolCallID(t *testing.T) {
	if got := HostToolCallID(map[string]any{"id": " c1 "}); got != "c1" {
		t.Errorf("got %q", got)
	}
	if got := HostToolCallID(map[string]any{"tool_call_id": "c2"}); got != "c2" {
		t.Errorf("got %q", got)
	}
	if got := HostToolCallID(map[string]any{"call_id": "c3"}); got != "c3" {
		t.Errorf("got %q", got)
	}
	if got := HostToolCallID(map[string]any{}); got != "" {
		t.Errorf("got %q", got)
	}
	if got := HostToolCallID(nil); got != "" {
		t.Errorf("nil -> %q", got)
	}
}

// ──────────────────────────── 用例：ExtractInvocation ───────────────────────────────

// TestExtractInvocation_从Inputs取 验证无 toolCall 时从 cbc 取。
func TestExtractInvocation_从Inputs取(t *testing.T) {
	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "read_file",
		ToolArgs: map[string]any{"file_path": "/a"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	got := ExtractInvocation(cbc, nil, map[string]any{})
	if got.ToolName != "read_file" {
		t.Errorf("tool_name = %q", got.ToolName)
	}
	if got.ToolArgs["file_path"] != "/a" {
		t.Errorf("tool_args = %v", got.ToolArgs)
	}
	if got.Ctx != cbc {
		t.Error("ctx should be set")
	}
}

// TestExtractInvocation_从toolCall取 验证回落到 tool_call。
func TestExtractInvocation_从toolCall取(t *testing.T) {
	call := map[string]any{"name": "bash", "arguments": map[string]any{"command": "ls"}}
	got := ExtractInvocation(nil, call, map[string]any{})
	if got.ToolName != "bash" {
		t.Errorf("tool_name = %q", got.ToolName)
	}
}

// TestExtractInvocation_全空 验证兜底。
func TestExtractInvocation_全空(t *testing.T) {
	got := ExtractInvocation(nil, nil, map[string]any{})
	if got.ToolName != "" {
		t.Errorf("tool_name = %q", got.ToolName)
	}
	if got.ToolArgs == nil {
		t.Error("tool_args should be empty map, not nil")
	}
	if got.ToolCall == nil {
		t.Error("tool_call should be synthesized")
	}
}

// ──────────────────────────── 用例：HostSessionID ───────────────────────────────

// TestHostSessionID_nil安全 验证 nil 返回空。
func TestHostSessionID_nil安全(t *testing.T) {
	if got := HostSessionID(nil); got != "" {
		t.Errorf("nil -> %q", got)
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	if got := HostSessionID(cbc); got != "" {
		t.Errorf("nil session -> %q", got)
	}
}

// ──────────────────────────── 用例：CompatibilitySendMatchesHost ───────────────────────────────

// TestCompatibilitySendMatchesHost_完全一致 验证通过。
func TestCompatibilitySendMatchesHost_完全一致(t *testing.T) {
	identity := &TrustedSendIdentity{
		SessionID:   "s1",
		RequestID:   "r1",
		ToolCallID:  "c1",
		ChannelKind: "web",
		ToolName:    SendFileToolName,
		NormalizedToolArgs: NormalizeInvocationToolArgs(SendFileToolName,
			map[string]any{"abs_file_path_list": []any{"/a"}}),
	}
	invocation := ToolInvocation{
		ToolName: SendFileToolName,
		ToolArgs: map[string]any{"abs_file_path_list": []any{"/a"}},
	}
	if !CompatibilitySendMatchesHost(map[string]any{}, invocation, identity) {
		t.Error("should match")
	}
}

// TestCompatibilitySendMatchesHost_工具名不符 验证拒绝。
func TestCompatibilitySendMatchesHost_工具名不符(t *testing.T) {
	identity := &TrustedSendIdentity{ToolName: SendFileToolName, NormalizedToolArgs: map[string]any{}}
	invocation := ToolInvocation{ToolName: "other", ToolArgs: map[string]any{}}
	if CompatibilitySendMatchesHost(map[string]any{}, invocation, identity) {
		t.Error("different tool name should not match")
	}
}

// TestCompatibilitySendMatchesHost_kwargs声明字段不符 验证拒绝。
func TestCompatibilitySendMatchesHost_kwargs声明字段不符(t *testing.T) {
	identity := &TrustedSendIdentity{
		SessionID: "s1", RequestID: "r1", ToolCallID: "c1",
		ChannelKind: "web", ToolName: SendFileToolName,
		NormalizedToolArgs: NormalizeInvocationToolArgs(SendFileToolName, map[string]any{}),
	}
	invocation := ToolInvocation{ToolName: SendFileToolName, ToolArgs: map[string]any{}}
	// kwargs 声明了 session_id 但值不符
	if CompatibilitySendMatchesHost(map[string]any{"session_id": "other"}, invocation, identity) {
		t.Error("mismatched session_id should fail")
	}
	// channel_kind 走小写归一
	if !CompatibilitySendMatchesHost(map[string]any{"channel_kind": "WEB"}, invocation, identity) {
		t.Error("channel_kind should be lowercased")
	}
}

// TestCompatibilitySendMatchesHost_toolCall不符 验证 tool_call 校验。
func TestCompatibilitySendMatchesHost_toolCall不符(t *testing.T) {
	identity := &TrustedSendIdentity{
		SessionID: "s1", RequestID: "r1", ToolCallID: "c1",
		ChannelKind: "web", ToolName: SendFileToolName,
		NormalizedToolArgs: NormalizeInvocationToolArgs(SendFileToolName, map[string]any{}),
	}
	invocation := ToolInvocation{ToolName: SendFileToolName, ToolArgs: map[string]any{}}
	badCall := map[string]any{"id": "c2", "name": SendFileToolName}
	if CompatibilitySendMatchesHost(map[string]any{"tool_call": badCall}, invocation, identity) {
		t.Error("mismatched tool_call id should fail")
	}
	// 必须带 arguments：Python 对 None 参数归一为 {}，与 identity（从 dict 算出、
	// 含 send_file 两个归一字段）不等，会判失败
	goodCall := map[string]any{"id": "c1", "name": SendFileToolName, "arguments": map[string]any{}}
	if !CompatibilitySendMatchesHost(map[string]any{"tool_call": goodCall}, invocation, identity) {
		t.Error("matching tool_call should pass")
	}
}

// ──────────────────────────── 用例：validateSmartGlobPattern ───────────────────────────────

// TestValidateSmartGlobPattern_各拒绝分支 验证限制。
func TestValidateSmartGlobPattern_各拒绝分支(t *testing.T) {
	cases := []struct {
		name    string
		pattern any
		want    string
	}{
		{"非字符串", 42, "native_glob_expansion_limit"},
		{"空串", "", "native_glob_expansion_limit"},
		{"超长", string(make([]byte, 3000)), "native_glob_expansion_limit"},
		// 上限是 6：括号需 7 个、逗号需 7 个才触发
		{"括号过多", "{a}{b}{c}{d}{e}{f}{g}", "native_glob_expansion_limit"},
		{"逗号过多", "{1,2,3,4,5,6,7,8}", "native_glob_expansion_limit"},
		{"绝对路径", "/etc/passwd", "native_glob_scope_invalid:put_parent_directory_in_path"},
		{"父目录", "../secret", "native_glob_scope_invalid:put_parent_directory_in_path"},
		{"正常", "*.go", ""},
		{"花括号正常", "{a,b}/*.go", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := validateSmartGlobPattern(c.pattern); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// ──────────────────────────── 用例：SetNativePathExecutors ───────────────────────────────

// TestSetNativePathExecutors_登记与读取 验证注入点。
func TestSetNativePathExecutors_登记与读取(t *testing.T) {
	SetNativePathExecutors(nil)
	if got := registeredNativeExecutor("read_file"); got != nil {
		t.Errorf("got %v", got)
	}
	// 传 nil 实例应被忽略
	SetNativePathExecutors(map[string]tool.Tool{"read_file": nil})
	if got := registeredNativeExecutor("read_file"); got != nil {
		t.Errorf("nil instance should be skipped, got %v", got)
	}
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// TestArgsWereValidJSONObject 验证合法性判定。
func TestArgsWereValidJSONObject(t *testing.T) {
	if !argsWereValidJSONObject(map[string]any{"a": 1}) {
		t.Error("map should be valid")
	}
	if !argsWereValidJSONObject(`{"a":1}`) {
		t.Error("json object string should be valid")
	}
	if argsWereValidJSONObject(`[1]`) {
		t.Error("json array should not be valid")
	}
	if argsWereValidJSONObject("not json") {
		t.Error("invalid should be false")
	}
	if argsWereValidJSONObject(42) {
		t.Error("number should be false")
	}
}

// TestRepairInvocationArgsForExecution 验证幂等直返。
func TestRepairInvocationArgsForExecution(t *testing.T) {
	invocation := ToolInvocation{ToolName: "x", ToolArgs: map[string]any{"a": 1}}
	got := repairInvocationArgsForExecution(invocation)
	if got.ToolArgs["a"] != 1 {
		t.Errorf("got %v", got.ToolArgs)
	}
	// nil 参数 → 空 map
	empty := repairInvocationArgsForExecution(ToolInvocation{})
	if empty.ToolArgs == nil {
		t.Error("nil tool_args should become empty map")
	}
}

// TestNormalizeChannelKind 验证通道类型归一。
func TestNormalizeChannelKind(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"WEB", "web"},
		{"  ", "unknown"},
		{nil, "unknown"},
		{"cli", "cli"},
	}
	for _, c := range cases {
		if got := normalizeChannelKind(c.in); got != c.want {
			t.Errorf("normalizeChannelKind(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestResolveChannelKind_kwargs优先 验证 kwargs 优先。
func TestResolveChannelKind_kwargs优先(t *testing.T) {
	got := resolveChannelKind(context.Background(), nil, map[string]any{"channel_kind": "CLI"})
	if got != "cli" {
		t.Errorf("got %q", got)
	}
}

// TestResolveSessionID_kwargs优先 验证 kwargs 优先。
func TestResolveSessionID_kwargs优先(t *testing.T) {
	got := resolveSessionID(nil, map[string]any{"session_id": " s1 "})
	if got != "s1" {
		t.Errorf("got %q", got)
	}
	if got := resolveSessionID(nil, map[string]any{}); got != "" {
		t.Errorf("nil ctx -> %q", got)
	}
}

// TestResolveToolCallID 验证工具调用 ID 解析。
func TestResolveToolCallID(t *testing.T) {
	if got := resolveToolCallID(nil, map[string]any{"tool_call_id": " c1 "}); got != "c1" {
		t.Errorf("got %q", got)
	}
	if got := resolveToolCallID(map[string]any{"id": "c2"}, map[string]any{}); got != "c2" {
		t.Errorf("got %q", got)
	}
	if got := resolveToolCallID(nil, map[string]any{}); got != "" {
		t.Errorf("got %q", got)
	}
}

// TestResolveUserInput_kwargs优先 验证 kwargs 优先。
func TestResolveUserInput_kwargs优先(t *testing.T) {
	got := resolveUserInput(nil, "c1", map[string]any{"user_input": "yes"})
	if got != "yes" {
		t.Errorf("got %v", got)
	}
	if got := resolveUserInput(nil, "", map[string]any{}); got != nil {
		t.Errorf("got %v", got)
	}
}

// TestResolveRequestID_kwargs优先 验证 kwargs 优先。
func TestResolveRequestID_kwargs优先(t *testing.T) {
	got := resolveRequestID(context.Background(), nil, map[string]any{"request_id": " r1 "})
	if got != "r1" {
		t.Errorf("got %q", got)
	}
}

// TestHasWindowsDrive 验证盘符判定。
func TestHasWindowsDrive(t *testing.T) {
	if !hasWindowsDrive("C:/x") {
		t.Error("C:/x should have drive")
	}
	if hasWindowsDrive("/x") {
		t.Error("/x should not have drive")
	}
	if hasWindowsDrive("x") {
		t.Error("x should not have drive")
	}
}

// ──────────────────────────── 执行前归一化 ────────────────────────────

// TestNormalizeNativePathInvocationForExecution_未登记直返 验证未登记时不冻结。
func TestNormalizeNativePathInvocationForExecution_未登记直返(t *testing.T) {
	SetNativePathExecutors(nil)
	invocation := ToolInvocation{ToolName: "read_file", ToolArgs: map[string]any{"file_path": "/a"}}
	got, reason := NormalizeNativePathInvocationForExecution(context.Background(), invocation, map[string]any{}, "/ws")
	if reason != "" {
		t.Errorf("reason = %q, want empty", reason)
	}
	if got.ToolArgs["file_path"] != "/a" {
		t.Errorf("args should be unchanged, got %v", got.ToolArgs)
	}
}

// TestNormalizeNativePathInvocationForExecution_非原生工具 验证直接返回。
func TestNormalizeNativePathInvocationForExecution_非原生工具(t *testing.T) {
	invocation := ToolInvocation{ToolName: "bash", ToolArgs: map[string]any{}}
	got, reason := NormalizeNativePathInvocationForExecution(context.Background(), invocation, map[string]any{}, "/ws")
	if reason != "" || got.ToolName != "bash" {
		t.Errorf("got (%v, %q)", got, reason)
	}
}

// TestNormalizeCommandInvocationForExecution_非execCommand 验证直接返回。
func TestNormalizeCommandInvocationForExecution_非execCommand(t *testing.T) {
	invocation := ToolInvocation{ToolName: "bash", ToolArgs: map[string]any{}}
	got, reason := NormalizeCommandInvocationForExecution(context.Background(), invocation, map[string]any{})
	if reason != "" || got.ToolName != "bash" {
		t.Errorf("got (%v, %q)", got, reason)
	}
}

// TestNormalizeCommandInvocationForExecution_空参数 验证契约校验。
func TestNormalizeCommandInvocationForExecution_空参数(t *testing.T) {
	invocation := ToolInvocation{ToolName: execCommandToolName, ToolArgs: map[string]any{}}
	_, reason := NormalizeCommandInvocationForExecution(context.Background(), invocation, map[string]any{})
	if reason != "command_workdir_contract_invalid" {
		t.Errorf("reason = %q", reason)
	}
}

// TestNormalizeCommandInvocationForExecution_已有cwd 验证拒绝。
func TestNormalizeCommandInvocationForExecution_已有cwd(t *testing.T) {
	invocation := ToolInvocation{
		ToolName: execCommandToolName,
		ToolArgs: map[string]any{"cwd": "/a"},
	}
	_, reason := NormalizeCommandInvocationForExecution(context.Background(), invocation, map[string]any{})
	if reason != "command_workdir_contract_invalid" {
		t.Errorf("reason = %q", reason)
	}
}

// TestResolveNativePathArgument_无cwdState 验证缺 cwd state 时的报错。
//
// 注意：context.Background() 下 GetCwd 会回落进程 cwd（非空），
// 因此先命中 workspace 缺失分支而非 cwd 缺失分支。
func TestResolveNativePathArgument_无cwdState(t *testing.T) {
	_, _, reason := resolveNativePathArgument("read_file", map[string]any{"file_path": "/a"}, "/ws", context.Background())
	if reason != "native_path_runtime_workspace_mismatch" {
		t.Errorf("reason = %q", reason)
	}
}

// TestResolveNativePathArgument_workspace不符 验证 workspace 校验。
func TestResolveNativePathArgument_workspace不符(t *testing.T) {
	ctx := cwd.WithCwdState(context.Background(), cwd.InitCwd("/ws", cwd.WithWorkspace("/other")))
	_, _, reason := resolveNativePathArgument("read_file", map[string]any{"file_path": "/a"}, "/ws", ctx)
	if reason != "native_path_runtime_workspace_mismatch" {
		t.Errorf("reason = %q", reason)
	}
}

// TestResolveNativePathArgument_相对路径 验证基于 cwd 解析。
func TestResolveNativePathArgument_相对路径(t *testing.T) {
	ctx := cwd.WithCwdState(context.Background(), cwd.InitCwd("/ws", cwd.WithWorkspace("/ws")))
	resolved, field, reason := resolveNativePathArgument(
		"read_file", map[string]any{"file_path": "a.txt"}, "/ws", ctx)
	if reason != "" {
		t.Fatalf("reason = %q", reason)
	}
	if field != "file_path" {
		t.Errorf("field = %q", field)
	}
	if resolved != "/ws/a.txt" {
		t.Errorf("resolved = %q, want /ws/a.txt", resolved)
	}
}

// TestResolveNativePathArgument_各工具字段 验证字段映射。
func TestResolveNativePathArgument_各工具字段(t *testing.T) {
	ctx := cwd.WithCwdState(context.Background(), cwd.InitCwd("/ws", cwd.WithWorkspace("/ws")))
	cases := []struct {
		tool  string
		args  map[string]any
		field string
	}{
		{"read_pdf", map[string]any{"pdf_path": "/a.pdf"}, "pdf_path"},
		{"glob", map[string]any{"pattern": "*.go"}, "path"},
		{"grep", map[string]any{"pattern": "x"}, "path"},
		{"list_files", map[string]any{}, "path"},
		{"write_file", map[string]any{"file_path": "/b"}, "file_path"},
	}
	for _, c := range cases {
		_, field, reason := resolveNativePathArgument(c.tool, c.args, "/ws", ctx)
		if reason != "" {
			t.Errorf("%s: reason = %q", c.tool, reason)
			continue
		}
		if field != c.field {
			t.Errorf("%s: field = %q, want %q", c.tool, field, c.field)
		}
	}
}

// TestResolveNativePathArgument_非法参数 验证空路径与 NUL。
func TestResolveNativePathArgument_非法参数(t *testing.T) {
	ctx := cwd.WithCwdState(context.Background(), cwd.InitCwd("/ws", cwd.WithWorkspace("/ws")))
	for _, args := range []map[string]any{
		{"file_path": ""},
		{"file_path": "a\x00b"},
		{},
	} {
		if _, _, reason := resolveNativePathArgument("read_file", args, "/ws", ctx); reason != "native_path_argument_invalid" {
			t.Errorf("args=%v reason = %q", args, reason)
		}
	}
}

// TestResolveNativePathArgument_glob非法模式 验证 glob 校验前置。
func TestResolveNativePathArgument_glob非法模式(t *testing.T) {
	ctx := cwd.WithCwdState(context.Background(), cwd.InitCwd("/ws", cwd.WithWorkspace("/ws")))
	_, _, reason := resolveNativePathArgument("glob", map[string]any{"pattern": "/etc/*"}, "/ws", ctx)
	if reason != "native_glob_scope_invalid:put_parent_directory_in_path" {
		t.Errorf("reason = %q", reason)
	}
}

// ──────────────────────────── 用例：writeInvocationToolArgs ───────────────────────────────

// TestWriteInvocationToolArgs_写回Inputs 验证 SetInputs 替换。
func TestWriteInvocationToolArgs_写回Inputs(t *testing.T) {
	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "read_file",
		ToolArgs: map[string]any{"file_path": "a.txt"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	invocation := ToolInvocation{Ctx: cbc, ToolName: "read_file", ToolArgs: inputs.ToolArgs}

	kwargs := map[string]any{"tool_args": "old"}
	err := writeInvocationToolArgs(invocation, kwargs, map[string]any{"file_path": "/ws/a.txt"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	updated, _ := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if updated.ToolArgs["file_path"] != "/ws/a.txt" {
		t.Errorf("inputs not written back: %v", updated.ToolArgs)
	}
	if kwargs["tool_args"] == "old" {
		t.Error("kwargs should be updated")
	}
}

// TestWriteInvocationToolArgs_写回map型ToolCall 验证 map 分支。
func TestWriteInvocationToolArgs_写回map型ToolCall(t *testing.T) {
	call := map[string]any{"name": "read_file"}
	invocation := ToolInvocation{ToolCall: call, ToolName: "read_file"}
	if err := writeInvocationToolArgs(invocation, map[string]any{}, map[string]any{"file_path": "/a"}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if call["arguments"] == nil {
		t.Error("map tool_call should be written back")
	}
}

// TestReplaceInvocationToolArgs 验证快照替换。
func TestReplaceInvocationToolArgs(t *testing.T) {
	original := ToolInvocation{ToolName: "x", ToolArgs: map[string]any{"a": 1}}
	got := replaceInvocationToolArgs(original, map[string]any{"b": 2})
	if got.ToolArgs["b"] != 2 {
		t.Errorf("got %v", got.ToolArgs)
	}
	if got.ToolName != "x" {
		t.Errorf("tool_name = %q", got.ToolName)
	}
}

// ──────────────────────────── 用例：ResolveTrustedSendIdentity ───────────────────────────────

// TestResolveTrustedSendIdentity_非send工具 验证返回 nil。
func TestResolveTrustedSendIdentity_非send工具(t *testing.T) {
	got := ResolveTrustedSendIdentity(context.Background(), nil, map[string]any{},
		ToolInvocation{ToolName: "read_file"})
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// TestResolveTrustedSendIdentity_缺上下文 验证 fail-closed。
func TestResolveTrustedSendIdentity_缺上下文(t *testing.T) {
	got := ResolveTrustedSendIdentity(context.Background(), nil, map[string]any{},
		ToolInvocation{ToolName: SendFileToolName})
	if got == nil || got.Error != "send_file_authorization_context_missing" {
		t.Errorf("got %+v", got)
	}
}

// TestResolveTrustedSendIdentity_工具名不匹配 验证 mismatch。
func TestResolveTrustedSendIdentity_工具名不匹配(t *testing.T) {
	inputs := &agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "c1", Name: SendFileToolName},
		ToolName: SendFileToolName,
		ToolArgs: map[string]any{},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	// 兼容调用声称是别的工具
	got := ResolveTrustedSendIdentity(context.Background(), cbc, map[string]any{},
		ToolInvocation{ToolName: "other_tool"})
	if got == nil || got.Error != "send_file_authorization_context_mismatch" {
		t.Errorf("got %+v", got)
	}
}

// TestResolveTrustedSendIdentity_缺requestID 验证上下文缺失。
func TestResolveTrustedSendIdentity_缺requestID(t *testing.T) {
	inputs := &agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "c1", Name: SendFileToolName},
		ToolName: SendFileToolName,
		ToolArgs: map[string]any{},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	// ctx 里没有 requestID/channelID，session 为 nil
	got := ResolveTrustedSendIdentity(context.Background(), cbc, map[string]any{},
		ToolInvocation{ToolName: SendFileToolName})
	if got == nil || got.Error != "send_file_authorization_context_missing" {
		t.Errorf("got %+v", got)
	}
}

// ──────────────────────────── resolve* 的 Extra 分支 ────────────────────────────

// TestResolveUserInput_从Extra取 验证三种 extra 形态。
func TestResolveUserInput_从Extra取(t *testing.T) {
	// 形态 1：user_inputs 子 map
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	cbc.SetExtra(permissions.RootPermissionQueueUserInputKey, map[string]any{
		"user_inputs": map[string]any{"c1": "answer1"},
	})
	if got := resolveUserInput(cbc, "c1", map[string]any{}); got != "answer1" {
		t.Errorf("user_inputs form -> %v", got)
	}

	// 形态 2：直接以 toolCallID 为键
	cbc2 := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	cbc2.SetExtra(permissions.RootPermissionQueueUserInputKey, map[string]any{"c2": "answer2"})
	if got := resolveUserInput(cbc2, "c2", map[string]any{}); got != "answer2" {
		t.Errorf("direct key form -> %v", got)
	}

	// 形态 3：整体返回
	cbc3 := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	cbc3.SetExtra(permissions.RootPermissionQueueUserInputKey, "raw-answer")
	if got := resolveUserInput(cbc3, "c3", map[string]any{}); got != "raw-answer" {
		t.Errorf("raw form -> %v", got)
	}

	// 无 extra
	cbc4 := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	if got := resolveUserInput(cbc4, "c4", map[string]any{}); got != nil {
		t.Errorf("no extra -> %v", got)
	}
}

// TestResolveRequestID_从Extra取 验证 extra 分支。
func TestResolveRequestID_从Extra取(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	cbc.SetExtra("request_id", " r-extra ")
	if got := resolveRequestID(context.Background(), cbc, map[string]any{}); got != "r-extra" {
		t.Errorf("got %q", got)
	}
	// 空 extra → 回落 ctx（background 无值）→ 空
	cbc2 := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	if got := resolveRequestID(context.Background(), cbc2, map[string]any{}); got != "" {
		t.Errorf("got %q", got)
	}
}

// TestResolveChannelKind_从Extra取 验证 extra 分支。
func TestResolveChannelKind_从Extra取(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	cbc.SetExtra("channel_kind", " WEB ")
	if got := resolveChannelKind(context.Background(), cbc, map[string]any{}); got != "web" {
		t.Errorf("got %q", got)
	}
	// 无任何来源 → unknown
	cbc2 := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	if got := resolveChannelKind(context.Background(), cbc2, map[string]any{}); got != "unknown" {
		t.Errorf("got %q", got)
	}
}

// TestResolveRequestID_从ctx取 验证 ctx 里的 requestID。
func TestResolveRequestID_从ctx取(t *testing.T) {
	ctx := swarmschema.WithToolPermissionRequestID(context.Background(), "r-ctx")
	if got := resolveRequestID(ctx, nil, map[string]any{}); got != "r-ctx" {
		t.Errorf("got %q", got)
	}
}

// TestResolveChannelKind_从ctx取 验证 ctx 里的 channelID。
func TestResolveChannelKind_从ctx取(t *testing.T) {
	ctx := swarmschema.WithToolPermissionChannelID(context.Background(), "CLI")
	if got := resolveChannelKind(ctx, nil, map[string]any{}); got != "cli" {
		t.Errorf("got %q", got)
	}
}

// ──────────────────────────── ToolCallValue ToolCall 分支 ────────────────────────────

// TestToolCallValue_ToolCall结构体 验证 *llmschema.ToolCall 分支。
func TestToolCallValue_ToolCall结构体(t *testing.T) {
	call := &llmschema.ToolCall{ID: "c1", Name: "bash", Arguments: `{"a":1}`}
	if got := ToolCallValue(call, "name"); got != "bash" {
		t.Errorf("name = %v", got)
	}
	if got := ToolCallValue(call, "arguments"); got != `{"a":1}` {
		t.Errorf("arguments = %v", got)
	}
	if got := ToolCallValue(call, "id"); got != "c1" {
		t.Errorf("id = %v", got)
	}
	if got := ToolCallValue(call, "unknown"); got != nil {
		t.Errorf("unknown -> %v", got)
	}
	var nilCall *llmschema.ToolCall
	if got := ToolCallValue(nilCall, "name"); got != nil {
		t.Errorf("nil -> %v", got)
	}
}

// TestHostToolCallID_ToolCall结构体 验证结构体形态取 ID。
func TestHostToolCallID_ToolCall结构体(t *testing.T) {
	call := &llmschema.ToolCall{ID: "c9"}
	if got := HostToolCallID(call); got != "c9" {
		t.Errorf("got %q", got)
	}
}

// ──────────────────────────── 归一化的错误分支 ────────────────────────────

// TestNormalizeNativePathInvocationForExecution_未登记不冻结 验证 nil 登记直返。
func TestNormalizeNativePathInvocationForExecution_未登记不冻结(t *testing.T) {
	SetNativePathExecutors(nil)
	for _, name := range []string{"read_file", "write_file", "edit_file", "glob", "grep", "list_files", "read_pdf"} {
		invocation := ToolInvocation{ToolName: name, ToolArgs: map[string]any{"file_path": "/a"}}
		got, reason := NormalizeNativePathInvocationForExecution(context.Background(), invocation, map[string]any{}, "/ws")
		if reason != "" {
			t.Errorf("%s: reason = %q", name, reason)
		}
		if got.ToolName != name {
			t.Errorf("%s: tool name changed", name)
		}
	}
}

// ──────────────────────────── 完整归一化路径（fake 执行器） ────────────────────────────

// fakeNativeTool 最小 tool.Tool 实现，用于身份登记测试。
type fakeNativeTool struct {
	card *tool.ToolCard
}

// Card 实现 tool.Tool
func (f *fakeNativeTool) Card() *tool.ToolCard { return f.card }

// Invoke 实现 tool.Tool
func (f *fakeNativeTool) Invoke(context.Context, map[string]any, ...tool.ToolOption) (map[string]any, error) {
	return nil, nil
}

// Stream 实现 tool.Tool
func (f *fakeNativeTool) Stream(context.Context, map[string]any, ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, nil
}

// GetCallTimeout 实现 tool.Tool
func (f *fakeNativeTool) GetCallTimeout(map[string]any, float64) float64 { return 0 }

// TestNormalizeNativePathInvocationForExecution_完整路径 验证登记后冻结路径。
func TestNormalizeNativePathInvocationForExecution_完整路径(t *testing.T) {
	workspace := t.TempDir()
	instance := &fakeNativeTool{
		card: tool.NewToolCardWithID("tool-read-file-1", "read_file", "read", nil, nil),
	}
	resourceManager := runner.GetResourceMgr()
	if resourceManager == nil {
		t.Skip("no global resource manager")
	}
	if err := resourceManager.AddTool(instance); err != nil {
		t.Skipf("cannot register tool: %v", err)
	}
	// 清理全局 ResourceMgr，避免污染其他测试
	defer func() { _, _ = resourceManager.RemoveTool([]string{"tool-read-file-1"}) }()
	defer SetNativePathExecutors(nil)
	SetNativePathExecutors(map[string]tool.Tool{"read_file": instance})

	ctx := cwd.WithCwdState(context.Background(), cwd.InitCwd(workspace, cwd.WithWorkspace(workspace)))
	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "read_file",
		ToolArgs: map[string]any{"file_path": "a.txt"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	invocation := ToolInvocation{
		Ctx:      cbc,
		ToolName: "read_file",
		ToolArgs: map[string]any{"file_path": "a.txt"},
	}

	got, reason := NormalizeNativePathInvocationForExecution(ctx, invocation, map[string]any{}, workspace)
	if reason != "" {
		t.Fatalf("reason = %q（期望冻结成功）", reason)
	}
	frozen, _ := got.ToolArgs["file_path"].(string)
	if frozen == "a.txt" {
		t.Error("path should be frozen to absolute")
	}
	if !filepath.IsAbs(frozen) {
		t.Errorf("frozen path not absolute: %q", frozen)
	}
}

// TestNormalizeNativePathInvocationForExecution_未注册到ResourceMgr 验证身份不符。
func TestNormalizeNativePathInvocationForExecution_未注册到ResourceMgr(t *testing.T) {
	workspace := t.TempDir()
	// 登记了实例，但 ResourceMgr 里没有 → 身份校验失败
	instance := &fakeNativeTool{
		card: tool.NewToolCardWithID("tool-never-registered", "read_file", "read", nil, nil),
	}
	defer SetNativePathExecutors(nil)
	SetNativePathExecutors(map[string]tool.Tool{"read_file": instance})

	ctx := cwd.WithCwdState(context.Background(), cwd.InitCwd(workspace, cwd.WithWorkspace(workspace)))
	inputs := &agentinterfaces.ToolCallInputs{ToolName: "read_file", ToolArgs: map[string]any{"file_path": "a.txt"}}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	invocation := ToolInvocation{Ctx: cbc, ToolName: "read_file", ToolArgs: map[string]any{"file_path": "a.txt"}}

	_, reason := NormalizeNativePathInvocationForExecution(ctx, invocation, map[string]any{}, workspace)
	if reason != "native_path_binding_unavailable" {
		t.Errorf("reason = %q", reason)
	}
}
