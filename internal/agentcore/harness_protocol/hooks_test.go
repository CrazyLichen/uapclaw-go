package harness_protocol

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// TestToolDecisionKind_值对齐Python 验证决策种类取值与上游逐字一致
// Python: hooks.py:20-28 ToolDecisionKind
func TestToolDecisionKind_值对齐Python(t *testing.T) {
	cases := []struct {
		name string
		got  ToolDecisionKind
		want string
	}{
		{"allow", ToolDecisionKindAllow, "allow"},
		{"deny", ToolDecisionKindDeny, "deny"},
		{"rewrite", ToolDecisionKindRewrite, "rewrite"},
		{"ask", ToolDecisionKindAsk, "ask"},
		{"provider_policy", ToolDecisionKindProviderPolicy, "provider_policy"},
	}
	if len(cases) != 5 {
		t.Fatalf("ToolDecisionKind 成员数 = %d，上游为 5", len(cases))
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if string(c.got) != c.want {
				t.Errorf("取值 = %q，期望 %q（协议字符串不得本地化）", string(c.got), c.want)
			}
		})
	}
}

// TestNewBeforePromptContext_字段透传 验证无 __post_init__ 的类型只做组装
// Python: hooks.py:30-38 BeforePromptContext（无 __post_init__）
func TestNewBeforePromptContext_字段透传(t *testing.T) {
	sid, turn := "sess-1", "turn-1"
	got := NewBeforePromptContext("member-a", &sid, &turn, "hello")
	if got.AgentName != "member-a" || got.Prompt != "hello" {
		t.Errorf("必填字段透传失败: %+v", got)
	}
	if got.ProviderSessionID == nil || *got.ProviderSessionID != "sess-1" {
		t.Errorf("ProviderSessionID = %v，期望 sess-1", got.ProviderSessionID)
	}
	if got.TurnID == nil || *got.TurnID != "turn-1" {
		t.Errorf("TurnID = %v，期望 turn-1", got.TurnID)
	}
}

// TestNewBeforePromptContext_可选指针为nil 验证 nil 与空串在结构层可区分（Q7）
func TestNewBeforePromptContext_可选指针为nil(t *testing.T) {
	got := NewBeforePromptContext("member-a", nil, nil, "hello")
	if got.ProviderSessionID != nil || got.TurnID != nil {
		t.Errorf("nil 未保持为 nil: %+v", got)
	}
}

// TestNewBeforePromptResult_继续执行为默认 验证 Options 零值语义
// Python: hooks.py:40-48 BeforePromptResult（continue_execution 默认 True）
func TestNewBeforePromptResult_继续执行为默认(t *testing.T) {
	got, err := NewBeforePromptResult("rewritten prompt", BeforePromptResultOptions{})
	if err != nil {
		t.Fatalf("零值 Options 不应报错: %v", err)
	}
	if got.Prompt != "rewritten prompt" {
		t.Errorf("Prompt = %q", got.Prompt)
	}
	// Go 侧 Options 零值 bool 为 false，必须由构造器填成 Python 的默认值 true
	if !got.ContinueExecution {
		t.Error("ContinueExecution 应为 true（Python 默认值），Go 需在构造器内填默认")
	}
	if got.Reason != nil || got.AdditionalContext != nil {
		t.Errorf("可选字段应为 nil: %+v", got)
	}
}

// TestNewBeforeToolContext_冻结参数 验证 JsonObject 冻结语义
// Python: hooks.py:61-62 BeforeToolContext.__post_init__
func TestNewBeforeToolContext_冻结参数(t *testing.T) {
	args := JsonObject{"command": "pwd"}
	got, err := NewBeforeToolContext("member-a", nil, nil, "call-1", "shell", args)
	if err != nil {
		t.Fatalf("合法输入不应报错: %v", err)
	}
	if got.CallID != "call-1" || got.ToolName != "shell" {
		t.Errorf("必填字段透传失败: %+v", got)
	}
	// 冻结必须是深拷贝：改原 map 不应影响结果
	args["command"] = "rm -rf /"
	if got.Arguments["command"] != "pwd" {
		t.Error("Arguments 未深拷贝，原 map 修改污染了结果")
	}
}

// TestNewBeforeToolContext_nilMap等价空对象 JsonObject 的 nil 在 Go 语义上是空 dict
// （不是 Python 的 None）：FreezeJsonObject(JsonObject(nil)) 返回空 map、无错误，
// 与 Python 侧传 {} 一致。真正被拒绝的是「无类型 nil」（见 FreezeJsonObject 之
// frozen == nil 分支），调用方无法从 JsonObject 类型形参传进无类型 nil。
// Python: hooks.py:50-63 BeforeToolContext.arguments（必填，调用方须给 {} 或对象）
func TestNewBeforeToolContext_nilMap等价空对象(t *testing.T) {
	var nilObj JsonObject
	got, err := NewBeforeToolContext("member-a", nil, nil, "call-1", "shell", nilObj)
	if err != nil {
		t.Fatalf("nil map 应等价空 dict，不应报错: %v", err)
	}
	if got.Arguments == nil {
		t.Error("Arguments 应被归一化为非 nil 空 map（对齐 Python {} 语义）")
	}
	if len(got.Arguments) != 0 {
		t.Errorf("Arguments = %v，期望空", got.Arguments)
	}
}

// TestNewToolDecision_REWRITE必须带参数 Q9 决策 A 的第二条校验
// Python: hooks.py:77-78
func TestNewToolDecision_REWRITE必须带参数(t *testing.T) {
	_, err := NewToolDecision(ToolDecisionKindRewrite, ToolDecisionOptions{})
	if err == nil {
		t.Fatal("REWRITE 缺少 updated_arguments 应被拒绝")
	}
	if !strings.Contains(err.Error(), "rewrite tool decision requires updated_arguments") {
		t.Errorf("错误文案不符: %v", err)
	}
}

// TestNewToolDecision_非REWRITE不得带参数 Q9 决策 A 的第三条校验
// Python: hooks.py:79-80
func TestNewToolDecision_非REWRITE不得带参数(t *testing.T) {
	_, err := NewToolDecision(ToolDecisionKindAllow, ToolDecisionOptions{
		UpdatedArguments: JsonObject{"command": "pwd"},
	})
	if err == nil {
		t.Fatal("非 REWRITE 携带 updated_arguments 应被拒绝")
	}
	if !strings.Contains(err.Error(), "only valid for a rewrite tool decision") {
		t.Errorf("错误文案不符: %v", err)
	}
}

// TestNewToolDecision_单值决策可构造 覆盖 5 个枚举取值中的 4 个
// Python: hooks.py:69-80
func TestNewToolDecision_单值决策可构造(t *testing.T) {
	for _, kind := range []ToolDecisionKind{
		ToolDecisionKindAllow, ToolDecisionKindDeny,
		ToolDecisionKindAsk, ToolDecisionKindProviderPolicy,
	} {
		t.Run(string(kind), func(t *testing.T) {
			got, err := NewToolDecision(kind, ToolDecisionOptions{})
			if err != nil {
				t.Fatalf("%s 不应报错: %v", kind, err)
			}
			if got.Decision != kind {
				t.Errorf("Decision = %q，期望 %q", got.Decision, kind)
			}
		})
	}
	// REWRITE 正例
	got, err := NewToolDecision(ToolDecisionKindRewrite, ToolDecisionOptions{
		UpdatedArguments: JsonObject{"command": "pwd"},
	})
	if err != nil {
		t.Fatalf("REWRITE 带参数不应报错: %v", err)
	}
	if got.UpdatedArguments["command"] != "pwd" {
		t.Errorf("UpdatedArguments = %v", got.UpdatedArguments)
	}
}

// TestNewAfterToolContext_与BeforeToolContext形参对齐 Q6 决策的核心守护：
// 两个姊妹类型的前 6 个必填字段必须一一对应，避免消费者记两套调用形状
// Python: hooks.py:50-63 / 83-98
func TestNewAfterToolContext_与BeforeToolContext形参对齐(t *testing.T) {
	args := JsonObject{"command": "pwd"}
	before, err := NewBeforeToolContext("member-a", nil, nil, "call-1", "shell", args)
	if err != nil {
		t.Fatalf("BeforeToolContext: %v", err)
	}
	after, err := NewAfterToolContext("member-a", nil, nil, "call-1", "shell", args, AfterToolContextOptions{})
	if err != nil {
		t.Fatalf("AfterToolContext: %v", err)
	}
	if before.AgentName != after.AgentName ||
		before.CallID != after.CallID ||
		before.ToolName != after.ToolName ||
		before.ProviderSessionID != after.ProviderSessionID ||
		before.TurnID != after.TurnID {
		t.Errorf("姊妹类型必填字段不对齐: before=%+v after=%+v", before, after)
	}
}

// TestNewAfterToolContext_冻结结果与参数
// Python: hooks.py:96-98
func TestNewAfterToolContext_冻结结果(t *testing.T) {
	got, err := NewAfterToolContext("member-a", nil, nil, "call-1", "shell",
		JsonObject{"command": "pwd"},
		AfterToolContextOptions{Result: map[string]any{"stdout": "/home"}})
	if err != nil {
		t.Fatalf("合法输入不应报错: %v", err)
	}
	m, ok := got.Result.(map[string]any)
	if !ok || m["stdout"] != "/home" {
		t.Errorf("Result = %v", got.Result)
	}
}

// TestNewAfterToolResult_形参0空构造 Q6 特例：Python 可 AfterToolResult()
func TestNewAfterToolResult_形参0空构造(t *testing.T) {
	got, err := NewAfterToolResult(AfterToolResultOptions{})
	if err != nil {
		t.Fatalf("空 Options 不应报错: %v", err)
	}
	if got.ReplaceResult {
		t.Error("ReplaceResult 默认应为 false")
	}
}

// TestNewStopHookContext_零值Metadata不报错 守护 default_factory=dict 归一化
// Python: hooks.py:121 field(default_factory=dict)
func TestNewStopHookContext_零值Metadata不报错(t *testing.T) {
	got, err := NewStopHookContext("member-a", nil, nil, StopHookContextOptions{})
	if err != nil {
		t.Fatalf("零值 Options 不应报错（Python 侧 metadata 默认空 dict）: %v", err)
	}
	if got.Metadata == nil {
		t.Error("Metadata 应被归一化为空 JsonObject，而不是 nil")
	}
	if len(got.Metadata) != 0 {
		t.Errorf("Metadata = %v，期望空", got.Metadata)
	}
}

// TestNewStopHookContext_冻结Metadata
func TestNewStopHookContext_冻结Metadata(t *testing.T) {
	meta := JsonObject{"k": "v"}
	got, err := NewStopHookContext("member-a", nil, nil, StopHookContextOptions{Metadata: meta})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	meta["k"] = "changed"
	if got.Metadata["k"] != "v" {
		t.Error("Metadata 未深拷贝")
	}
}

// fakeHookDispatcher 测试替身：验证接口成员集完整（4 个钩子方法）
type fakeHookDispatcher struct {
	prompts []BeforePromptContext
	tools   []BeforeToolContext
	afters  []AfterToolContext
	stops   []StopHookContext
	// rewriteCommand 非空时 before_tool 返回 REWRITE
	rewriteCommand string
	// denyTool 非空时该工具被 DENY
	denyTool string
}

// BeforePrompt 记录调用并原样返回提示词。
func (f *fakeHookDispatcher) BeforePrompt(_ context.Context, hookCtx BeforePromptContext) (BeforePromptResult, error) {
	f.prompts = append(f.prompts, hookCtx)
	return BeforePromptResult{Prompt: hookCtx.Prompt, ContinueExecution: true}, nil
}

// BeforeTool 记录调用并按配置返回 DENY / REWRITE / ALLOW。
func (f *fakeHookDispatcher) BeforeTool(_ context.Context, hookCtx BeforeToolContext) (ToolDecision, error) {
	f.tools = append(f.tools, hookCtx)
	if hookCtx.ToolName == f.denyTool {
		return ToolDecision{Decision: ToolDecisionKindDeny}, nil
	}
	if f.rewriteCommand != "" {
		return NewToolDecision(ToolDecisionKindRewrite, ToolDecisionOptions{
			UpdatedArguments: JsonObject{"command": f.rewriteCommand},
		})
	}
	return ToolDecision{Decision: ToolDecisionKindAllow}, nil
}

// AfterTool 记录调用并原样返回空结果。
func (f *fakeHookDispatcher) AfterTool(_ context.Context, hookCtx AfterToolContext) (AfterToolResult, error) {
	f.afters = append(f.afters, hookCtx)
	return AfterToolResult{}, nil
}

// OnStop 记录调用。
func (f *fakeHookDispatcher) OnStop(_ context.Context, hookCtx StopHookContext) error {
	f.stops = append(f.stops, hookCtx)
	return nil
}

// 编译期断言：fake 满足 4 方法接口（若接口成员集被改，此处编译失败）
var _ HarnessHookDispatcher = (*fakeHookDispatcher)(nil)

// TestHarnessHookDispatcher_四方法齐全 验证接口形状与上游一致
// Python: hooks.py:127-150 HarnessHookDispatcher（before_prompt/before_tool/after_tool/on_stop）
func TestHarnessHookDispatcher_四方法齐全(t *testing.T) {
	var d HarnessHookDispatcher = &fakeHookDispatcher{}
	ctx := context.Background()

	before, err := d.BeforePrompt(ctx, NewBeforePromptContext("a", nil, nil, "hi"))
	if err != nil || before.Prompt != "hi" {
		t.Fatalf("BeforePrompt: %+v %v", before, err)
	}
	decision, err := d.BeforeTool(ctx, BeforeToolContext{ToolName: "shell", Arguments: JsonObject{}})
	if err != nil || decision.Decision != ToolDecisionKindAllow {
		t.Fatalf("BeforeTool: %+v %v", decision, err)
	}
	if _, err := d.AfterTool(ctx, AfterToolContext{ToolName: "shell", Arguments: JsonObject{}}); err != nil {
		t.Fatalf("AfterTool: %v", err)
	}
	if err := d.OnStop(ctx, StopHookContext{AgentName: "a", Metadata: JsonObject{}}); err != nil {
		t.Fatalf("OnStop: %v", err)
	}
}

// TestHarnessHookDispatcher_Enabled方法已删除 Q2 决策守护：
// 15.1 自造的 Enabled() 不得复活
func TestHarnessHookDispatcher_Enabled方法已删除(t *testing.T) {
	var d HarnessHookDispatcher = &fakeHookDispatcher{}
	typ := reflect.TypeOf(d)
	if typ.NumMethod() != 4 {
		names := make([]string, 0, typ.NumMethod())
		for i := 0; i < typ.NumMethod(); i++ {
			names = append(names, typ.Method(i).Name)
		}
		t.Fatalf("方法数 = %d，期望 4（%v）", typ.NumMethod(), names)
	}
	if _, ok := typ.MethodByName("Enabled"); ok {
		t.Error("Enabled() 必须已删除（Python hooks.py 无此方法，Q2 决策 A）")
	}
	for _, want := range []string{"BeforePrompt", "BeforeTool", "AfterTool", "OnStop"} {
		if _, ok := typ.MethodByName(want); !ok {
			t.Errorf("缺少方法 %s", want)
		}
	}
}

// TestHarnessHookDispatcher_ctx取消可传播 验证 ctx 作为首参的语义
func TestHarnessHookDispatcher_ctx取消可传播(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := &fakeHookDispatcher{}
	if _, err := d.BeforeTool(ctx, BeforeToolContext{ToolName: "shell", Arguments: JsonObject{}}); err != nil {
		t.Fatalf("BeforeTool: %v", err)
	}
}
