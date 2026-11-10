package permissions

import (
	"context"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sysoperation "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	sschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// newTestCallbackContext 构造一个不含 session 的测试回调上下文。
// session 传 nil：本批测试只覆盖与 extra 交互的部分，不涉及会话解析。
func newTestCallbackContext() *agentinterfaces.AgentCallbackContext {
	return agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
}

// ──────────────────────────── 绑定三态测试 ────────────────────────────

// TestBindRootPermissionRequest_三态 验证未绑定 / 绑定禁用 / 绑定启用三种状态
func TestBindRootPermissionRequest_三态(t *testing.T) {
	// 1. 未绑定
	base := context.Background()
	if _, err := CurrentRootPermissionQueue(base); err == nil {
		t.Fatalf("未绑定应报错")
	} else if !errIsRootQueue(err, "root_permission_request_unbound") {
		t.Fatalf("错误码不符: %v", err)
	}
	if OptionalRootPermissionQueue(base) != nil {
		t.Fatalf("未绑定时 optional 应返回 nil")
	}

	// 2. 绑定明确禁用
	disabled, cleanup, err := BindRootPermissionRequest(base, "", "", false, nil)
	if err != nil {
		t.Fatalf("绑定失败: %v", err)
	}
	defer cleanup()
	queue, err := CurrentRootPermissionQueue(disabled)
	if err != nil {
		t.Fatalf("绑定禁用不应报错: %v", err)
	}
	if queue != nil {
		t.Fatalf("绑定禁用应返回 nil 队列")
	}
	if OptionalRootPermissionQueue(disabled) != nil {
		t.Fatalf("绑定禁用时 optional 应返回 nil")
	}

	// 3. 绑定启用
	q := newTestQueue()
	enabled, cleanup2, err := BindRootPermissionRequest(base, "sess", "req", true, q)
	if err != nil {
		t.Fatalf("绑定失败: %v", err)
	}
	defer cleanup2()
	got, err := CurrentRootPermissionQueue(enabled)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got != q {
		t.Fatalf("队列不符")
	}
	if OptionalRootPermissionQueue(enabled) != q {
		t.Fatalf("optional 应返回队列")
	}
}

// TestBindRootPermissionRequest_字段非法 验证会话或请求 ID 为空时报错
func TestBindRootPermissionRequest_字段非法(t *testing.T) {
	if _, _, err := BindRootPermissionRequest(context.Background(), "", "req", true, nil); err == nil {
		t.Fatalf("空会话应报错")
	}
	if _, _, err := BindRootPermissionRequest(context.Background(), "sess", "", true, nil); err == nil {
		t.Fatalf("空请求应报错")
	}
}

// TestBindRootPermissionRequest_派生隔离 验证派生 context 不影响父 context
func TestBindRootPermissionRequest_派生隔离(t *testing.T) {
	base := context.Background()
	q := newTestQueue()
	ctx, cleanup, err := BindRootPermissionRequest(base, "s", "r", true, q)
	if err != nil {
		t.Fatalf("绑定失败: %v", err)
	}
	defer cleanup()

	if OptionalRootPermissionQueue(ctx) != q {
		t.Fatalf("子 context 应有队列")
	}
	if OptionalRootPermissionQueue(base) != nil {
		t.Fatalf("父 context 不应受影响")
	}
}

// TestResetRootPermissionRequest 验证 reset 为 no-op（Go 语义）
func TestResetRootPermissionRequest(t *testing.T) {
	ResetRootPermissionRequest(context.Background())
}

// ──────────────────────────── PutRootNonPermissionResumeInInputs 测试 ────────────────────────────

// TestPutRootNonPermissionResumeInInputs_写入与清除 验证写入续接与包装器，nil 时清除
func TestPutRootNonPermissionResumeInInputs_写入与清除(t *testing.T) {
	resume := &RootNonPermissionResume{RootSessionID: "s", ToolCallID: "c", ToolName: "tool"}
	wrapper := &RootPermissionWrapperResume{Consumed: true}

	out := PutRootNonPermissionResumeInInputs(map[string]any{}, resume, []*RootPermissionWrapperResume{wrapper})
	extra := out["run"].(map[string]any)["context"].(map[string]any)["extra"].(map[string]any)
	if extra[RootNonPermissionResumeDTOKey] != resume {
		t.Fatalf("应写入续接")
	}
	if wrappers, ok := extra[RootPermissionWrappersKey].([]*RootPermissionWrapperResume); !ok || len(wrappers) != 1 {
		t.Fatalf("应写入包装器")
	}

	// 清除
	cleared := PutRootNonPermissionResumeInInputs(out, nil, nil)
	extra2 := cleared["run"].(map[string]any)["context"].(map[string]any)["extra"].(map[string]any)
	if _, exists := extra2[RootNonPermissionResumeDTOKey]; exists {
		t.Fatalf("应清除续接")
	}
	if _, exists := extra2[RootPermissionWrappersKey]; exists {
		t.Fatalf("应清除包装器")
	}
}

// ──────────────────────────── 中断标记测试 ────────────────────────────

// TestMarkPermissionInterruptRequest_正常与冲突 验证标记与冲突检测
func TestMarkPermissionInterruptRequest_正常与冲突(t *testing.T) {
	ctx := newTestCallbackContext()
	request := &saschema.InterruptRequest{Message: "确认"}

	if err := MarkPermissionInterruptRequest(ctx, request); err != nil {
		t.Fatalf("首次标记应成功: %v", err)
	}
	// 同一请求重复标记 → 成功
	if err := MarkPermissionInterruptRequest(ctx, request); err != nil {
		t.Fatalf("重复标记同一请求应成功: %v", err)
	}
	// 不同请求 → 冲突
	other := &saschema.InterruptRequest{Message: "另一个"}
	if err := MarkPermissionInterruptRequest(ctx, other); err == nil {
		t.Fatalf("不同请求应冲突")
	}
}

// TestMarkPermissionInterruptRequest_nil 验证 nil 上下文报错
func TestMarkPermissionInterruptRequest_nil(t *testing.T) {
	if err := MarkPermissionInterruptRequest(nil, &saschema.InterruptRequest{}); err == nil {
		t.Fatalf("nil 上下文应报错")
	}
}

// TestIsMarkedPermissionInterrupt 验证只有同一请求才判为已标记
func TestIsMarkedPermissionInterrupt(t *testing.T) {
	ctx := newTestCallbackContext()
	request := &saschema.InterruptRequest{Message: "确认"}
	exc := &saschema.ToolInterruptException{Request: request}

	// 未标记 → false
	if IsMarkedPermissionInterrupt(ctx, exc) {
		t.Fatalf("未标记应为 false")
	}

	if err := MarkPermissionInterruptRequest(ctx, request); err != nil {
		t.Fatalf("标记失败: %v", err)
	}
	if !IsMarkedPermissionInterrupt(ctx, exc) {
		t.Fatalf("应判为已标记")
	}

	// 不同请求的异常 → false
	otherExc := &saschema.ToolInterruptException{Request: &saschema.InterruptRequest{}}
	if IsMarkedPermissionInterrupt(ctx, otherExc) {
		t.Fatalf("不同请求应为 false")
	}

	// 无异常 / nil 上下文 → false
	if IsMarkedPermissionInterrupt(ctx, nil) {
		t.Fatalf("无异常应为 false")
	}
	if IsMarkedPermissionInterrupt(nil, exc) {
		t.Fatalf("nil 上下文应为 false")
	}
}

// TestConsumePermissionWrapperResume 验证只消费已消费标记的包装器
func TestConsumePermissionWrapperResume(t *testing.T) {
	ctx := newTestCallbackContext()

	// 无标记 → false
	if ConsumePermissionWrapperResume(ctx) {
		t.Fatalf("无标记应为 false")
	}

	// 标记已消费 → true，且标记被删除
	ctx.SetExtra(RootPermissionWrapperAttribute, &RootPermissionWrapperResume{Consumed: true})
	if !ConsumePermissionWrapperResume(ctx) {
		t.Fatalf("应返回 true")
	}
	if _, still := ctx.GetExtra(RootPermissionWrapperAttribute); still {
		t.Fatalf("标记应被删除")
	}

	// 标记未消费 → false
	ctx.SetExtra(RootPermissionWrapperAttribute, &RootPermissionWrapperResume{Consumed: false})
	if ConsumePermissionWrapperResume(ctx) {
		t.Fatalf("未消费应返回 false")
	}

	// 非包装器类型 → false
	ctx.SetExtra(RootPermissionWrapperAttribute, "string")
	if ConsumePermissionWrapperResume(ctx) {
		t.Fatalf("类型不符应返回 false")
	}

	// nil 上下文 → false
	if ConsumePermissionWrapperResume(nil) {
		t.Fatalf("nil 应为 false")
	}
}

// ──────────────────────────── 上下文取值测试 ────────────────────────────

// TestToolInvocationKeyFromContext 验证号码牌读取
func TestToolInvocationKeyFromContext(t *testing.T) {
	ctx := newTestCallbackContext()
	if ToolInvocationKeyFromContext(ctx) != nil {
		t.Fatalf("未设置应返回 nil")
	}

	key, _ := NewToolInvocationKeyV1("i", "s", "r", ExecutorKindAgent, "e", "c", ToolInvocationKeyVersion)
	ctx.SetExtra(ToolInvocationContextAttribute, key)
	if got := ToolInvocationKeyFromContext(ctx); got != key {
		t.Fatalf("号码牌不符")
	}

	// 类型不符
	ctx.SetExtra(ToolInvocationContextAttribute, "string")
	if ToolInvocationKeyFromContext(ctx) != nil {
		t.Fatalf("类型不符应返回 nil")
	}
	if ToolInvocationKeyFromContext(nil) != nil {
		t.Fatalf("nil 应返回 nil")
	}
}

// TestRootPermissionResumeFromContext 验证续接状态读取
func TestRootPermissionResumeFromContext(t *testing.T) {
	ctx := newTestCallbackContext()
	if RootPermissionResumeFromContext(ctx) != nil {
		t.Fatalf("未设置应返回 nil")
	}

	resume := &RootPermissionResume{UserInput: "answer"}
	ctx.SetExtra(RootPermissionResumeAttribute, resume)
	if got := RootPermissionResumeFromContext(ctx); got != resume {
		t.Fatalf("续接不符")
	}

	ctx.SetExtra(RootPermissionResumeAttribute, "string")
	if RootPermissionResumeFromContext(ctx) != nil {
		t.Fatalf("类型不符应返回 nil")
	}
	if RootPermissionResumeFromContext(nil) != nil {
		t.Fatalf("nil 应返回 nil")
	}
}

// TestRootNonPermissionResumeFromContext 验证非权限续接读取
func TestRootNonPermissionResumeFromContext(t *testing.T) {
	ctx := newTestCallbackContext()
	if RootNonPermissionResumeFromContext(ctx) != nil {
		t.Fatalf("未设置应返回 nil")
	}

	resume := &RootNonPermissionResume{RootSessionID: "s", ToolCallID: "c", ToolName: "t"}
	ctx.SetExtra(RootNonPermissionResumeAttribute, resume)
	if got := RootNonPermissionResumeFromContext(ctx); got != resume {
		t.Fatalf("续接不符")
	}

	ctx.SetExtra(RootNonPermissionResumeAttribute, "string")
	if RootNonPermissionResumeFromContext(ctx) != nil {
		t.Fatalf("类型不符应返回 nil")
	}
	if RootNonPermissionResumeFromContext(nil) != nil {
		t.Fatalf("nil 应返回 nil")
	}
}

// ──────────────────────────── rail 构造与优先级测试 ────────────────────────────

// TestNewRootPermissionQueueRail 验证构造与优先级
func TestNewRootPermissionQueueRail(t *testing.T) {
	q := newTestQueue()
	r := NewRootPermissionQueueRail(q, nil)
	if r == nil || r.queue != q {
		t.Fatalf("构造失败")
	}
	if r.Priority() != RootPermissionQueuePriority {
		t.Fatalf("优先级不符: %d", r.Priority())
	}
}

// TestNewRootPermissionCompletionRail 验证构造与优先级
func TestNewRootPermissionCompletionRail(t *testing.T) {
	q := newTestQueue()
	r := NewRootPermissionCompletionRail(q)
	if r == nil || r.queue != q {
		t.Fatalf("构造失败")
	}
	if r.Priority() != RootPermissionCompletionPriority {
		t.Fatalf("优先级不符: %d", r.Priority())
	}
}

// TestNewRootContextRail_校验 验证队列必填与优先级
func TestNewRootContextRail_校验(t *testing.T) {
	if _, err := NewRootContextRail(nil, nil, nil, nil); err == nil {
		t.Fatalf("nil 队列应报错")
	}

	q := newTestQueue()
	sandboxed := true
	r, err := NewRootContextRail(q, "sysop", nil, &sandboxed)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if r.Priority() != RootContextRailPriority {
		t.Fatalf("优先级不符: %d", r.Priority())
	}
	if !r.resolveSandboxed("anything") {
		t.Fatalf("显式 sandboxed 应优先")
	}
}

// TestResolveSandboxed_推导 验证构造未给时按 mode 推导
func TestResolveSandboxed_推导(t *testing.T) {
	q := newTestQueue()
	r, _ := NewRootContextRail(q, nil, nil, nil)

	if r.resolveSandboxed("plain") {
		t.Fatalf("无 mode 应为 false")
	}

	type fakeOperation struct{}
	if r.resolveSandboxed(fakeOperation{}) {
		t.Fatalf("无 Card 方法应为 false")
	}

	if !r.resolveSandboxed(fakeOperationWithCard{mode: sysoperation.OperationModeSandbox}) {
		t.Fatalf("Mode=sandbox 应为 true")
	}
	if r.resolveSandboxed(fakeOperationWithCard{mode: sysoperation.OperationModeLocal}) {
		t.Fatalf("Mode=local 应为 false")
	}
	// Card() 返回 nil 时不 panic
	if r.resolveSandboxed(fakeOperationNilCard{}) {
		t.Fatalf("nil card 应为 false")
	}
}

// fakeOperationWithCard 测试用带 Card 的对象
type fakeOperationWithCard struct {
	mode sysoperation.OperationMode
}

// Card 实现 cardProvider
func (f fakeOperationWithCard) Card() *sysoperation.SysOperationCard {
	return &sysoperation.SysOperationCard{Mode: f.mode}
}

// fakeOperationNilCard 测试用 Card 返回 nil 的对象
type fakeOperationNilCard struct{}

// Card 实现 cardProvider，返回 nil
func (f fakeOperationNilCard) Card() *sysoperation.SysOperationCard { return nil }

// TestResolvedSysOperation 验证 sysOperation 优先、回退 commandSysOperation
func TestResolvedSysOperation(t *testing.T) {
	q := newTestQueue()
	r, _ := NewRootContextRail(q, "primary", "fallback", nil)
	if got := r.resolvedSysOperation(); got != "primary" {
		t.Fatalf("应优先返回 primary，实际 %v", got)
	}

	r2, _ := NewRootContextRail(q, nil, "fallback", nil)
	if got := r2.resolvedSysOperation(); got != "fallback" {
		t.Fatalf("应回退 fallback，实际 %v", got)
	}

	r3, _ := NewRootContextRail(q, nil, nil, nil)
	if got := r3.resolvedSysOperation(); got != nil {
		t.Fatalf("都为空应返回 nil")
	}
}

// ──────────────────────────── PutPermissionOwnerInInputs 测试 ────────────────────────────

// TestPutPermissionOwnerInInputs_写入与清除 验证 owner 写入与 nil 清除
func TestPutPermissionOwnerInInputs_写入与清除(t *testing.T) {
	owner := &sschema.PermissionContext{
		PrincipalUserID:     "u1",
		TriggeringUserID:    "u2",
		GroupDigitalAvatar:  true,
		EnableMemory:        false,
		AvatarPrincipalName: "张三",
		AvatarMode:          true,
	}
	out := PutPermissionOwnerInInputs(map[string]any{}, owner)
	extra := out["run"].(map[string]any)["context"].(map[string]any)["extra"].(map[string]any)
	stored, ok := extra[RootPermissionOwnerKey].(map[string]any)
	if !ok {
		t.Fatalf("应写入 owner")
	}
	if stored["principal_user_id"] != "u1" || stored["avatar_principal_name"] != "张三" {
		t.Fatalf("owner 内容不符: %v", stored)
	}

	cleared := PutPermissionOwnerInInputs(out, nil)
	extra2 := cleared["run"].(map[string]any)["context"].(map[string]any)["extra"].(map[string]any)
	if _, exists := extra2[RootPermissionOwnerKey]; exists {
		t.Fatalf("应清除")
	}
}

// ──────────────────────────── ownerFromExtra 测试 ────────────────────────────

// validOwnerMap 构造一个合法的 owner 映射
func validOwnerMap() map[string]any {
	return map[string]any{
		"principal_user_id":     "u1",
		"triggering_user_id":    "u2",
		"group_digital_avatar":  true,
		"enable_memory":         false,
		"avatar_principal_name": "张三",
		"avatar_mode":           true,
	}
}

// TestOwnerFromExtra_正常与缺省 验证正常还原与缺失返回 nil
func TestOwnerFromExtra_正常与缺省(t *testing.T) {
	if got, err := ownerFromExtra(nil, "web"); err != nil || got != nil {
		t.Fatalf("nil extra 应返回 nil nil")
	}
	if got, err := ownerFromExtra(map[string]any{}, "web"); err != nil || got != nil {
		t.Fatalf("无键应返回 nil nil")
	}
	if got, err := ownerFromExtra(map[string]any{RootPermissionOwnerKey: nil}, "web"); err != nil || got != nil {
		t.Fatalf("值为 nil 应返回 nil nil")
	}

	got, err := ownerFromExtra(map[string]any{RootPermissionOwnerKey: validOwnerMap()}, "web")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got.ChannelID != "web" || got.PrincipalUserID != "u1" || !got.AvatarMode {
		t.Fatalf("owner 不符: %+v", got)
	}
}

// TestOwnerFromExtra_非法 验证键集合与类型不符时返回错误
func TestOwnerFromExtra_非法(t *testing.T) {
	// 非映射
	if _, err := ownerFromExtra(map[string]any{RootPermissionOwnerKey: "str"}, "web"); err == nil {
		t.Fatalf("非映射应报错")
	}
	// 键数不符
	short := validOwnerMap()
	delete(short, "avatar_mode")
	if _, err := ownerFromExtra(map[string]any{RootPermissionOwnerKey: short}, "web"); err == nil {
		t.Fatalf("缺键应报错")
	}
	// 多键
	extra := validOwnerMap()
	extra["unknown"] = 1
	if _, err := ownerFromExtra(map[string]any{RootPermissionOwnerKey: extra}, "web"); err == nil {
		t.Fatalf("多键应报错")
	}
	// 布尔字段类型错
	for _, name := range []string{"group_digital_avatar", "enable_memory", "avatar_mode"} {
		bad := validOwnerMap()
		bad[name] = "yes"
		if _, err := ownerFromExtra(map[string]any{RootPermissionOwnerKey: bad}, "web"); err == nil {
			t.Fatalf("%s 类型错应报错", name)
		}
	}
	// 文本字段类型错
	for _, name := range []string{"principal_user_id", "triggering_user_id", "avatar_principal_name"} {
		bad := validOwnerMap()
		bad[name] = 1
		if _, err := ownerFromExtra(map[string]any{RootPermissionOwnerKey: bad}, "web"); err == nil {
			t.Fatalf("%s 类型错应报错", name)
		}
	}
}

// ──────────────────────────── RootContextFailureFromContext 测试 ────────────────────────────

// TestRootContextFailureFromContext 验证合法与非法原因
func TestRootContextFailureFromContext(t *testing.T) {
	ctx := newTestCallbackContext()
	if got := RootContextFailureFromContext(ctx); got != "" {
		t.Fatalf("未设置应返回空")
	}

	ctx.SetExtra(RootContextFailureAttribute, rootContextFailureMissing)
	if got := RootContextFailureFromContext(ctx); got != rootContextFailureMissing {
		t.Fatalf("应返回设置值，实际 %q", got)
	}

	ctx.SetExtra(RootContextFailureAttribute, "unknown-reason")
	if got := RootContextFailureFromContext(ctx); got != "" {
		t.Fatalf("非法原因应返回空")
	}

	ctx.SetExtra(RootContextFailureAttribute, 123)
	if got := RootContextFailureFromContext(ctx); got != "" {
		t.Fatalf("非字符串应返回空")
	}

	if RootContextFailureFromContext(nil) != "" {
		t.Fatalf("nil 应返回空")
	}
}

// TestRootContextFailureReasons_完整 验证六个合法原因都在集合里
func TestRootContextFailureReasons_完整(t *testing.T) {
	expected := []string{
		rootContextFailureInvalid,
		rootContextFailureMissing,
		rootContextFailureSessionMismatch,
		rootContextFailureCommandUnavailable,
		rootContextFailureResumeInvalid,
		rootContextFailureAskAnswerInvalid,
	}
	if len(rootContextFailureReasons) != len(expected) {
		t.Fatalf("集合大小不符: %d", len(rootContextFailureReasons))
	}
	for _, reason := range expected {
		if _, ok := rootContextFailureReasons[reason]; !ok {
			t.Fatalf("缺 %q", reason)
		}
	}
}

// ──────────────────────────── 工具辅助函数测试 ────────────────────────────

// TestToolNameOfInputs 验证工具名取值优先级
func TestToolNameOfInputs(t *testing.T) {
	if toolNameOfInputs(nil) != "" {
		t.Fatalf("nil 应返回空")
	}
}

// TestToolCallIDOfInputs 验证调用 ID 取值
func TestToolCallIDOfInputs(t *testing.T) {
	if toolCallIDOfInputs(nil) != "" {
		t.Fatalf("nil 应返回空")
	}
}

// TestNilIfReason 验证失败时置 nil
func TestNilIfReason(t *testing.T) {
	dc, _ := NewRootDecisionContext("s", "r", "", nil, "")
	if nilIfReason(dc, "") != dc {
		t.Fatalf("无原因应原样返回")
	}
	if nilIfReason(dc, "reason") != nil {
		t.Fatalf("有原因应返回 nil")
	}
}

// TestNilIfReasonOwner 验证失败时置 nil owner
func TestNilIfReasonOwner(t *testing.T) {
	owner := &sschema.PermissionContext{}
	if nilIfReasonOwner(owner, "") != owner {
		t.Fatalf("无原因应原样返回")
	}
	if nilIfReasonOwner(owner, "reason") != nil {
		t.Fatalf("有原因应返回 nil")
	}
}

// TestCallbackSessionID 验证会话 ID 读取
func TestCallbackSessionID(t *testing.T) {
	if callbackSessionID(nil) != "" {
		t.Fatalf("nil 应返回空")
	}
}

// TestPopAskResume与Discard 验证反问续接的取出与丢弃
func TestPopAskResume与Discard(t *testing.T) {
	ctx := newTestCallbackContext()
	if popAskResume(ctx) != nil {
		t.Fatalf("无 DTO 应返回 nil")
	}

	// 直接往 run extra 里塞
	runExtra := runExtraOf(ctx)
	if runExtra == nil {
		// 该测试上下文没有 run_context，跳过塞值分支，仅验证 nil 安全
		discardAskResume(ctx)
		return
	}
	prepared := &RootAskUserResume{}
	runExtra[AskUserResumeDTOKey] = prepared
	if got := popAskResume(ctx); got != prepared {
		t.Fatalf("应取出 prepared")
	}
	if _, exists := runExtra[AskUserResumeDTOKey]; exists {
		t.Fatalf("应已删除")
	}
	discardAskResume(ctx)
}

// ──────────────────────────── 钩子路径测试（带 session 与 inputs） ────────────────────────────

// fakeSession 测试用会话门面，只实现本批需要的 GetSessionID 与 GetState。
type fakeSession struct {
	sessionID string
	state     map[string]any
}

// GetSessionID 实现 SessionFacade
func (f *fakeSession) GetSessionID() string { return f.sessionID }

// UpdateState 实现 SessionFacade（本批未使用）
func (f *fakeSession) UpdateState(map[string]any) {}

// GetState 实现 SessionFacade
func (f *fakeSession) GetState(key sessionstate.StateKey) (any, error) {
	if f.state == nil {
		return nil, nil
	}
	value := f.state[key.String()]
	return value, nil
}

// DumpState 实现 SessionFacade（本批未使用）
func (f *fakeSession) DumpState() map[string]any { return nil }

// WriteStream 实现 SessionFacade（本批未使用）
func (f *fakeSession) WriteStream(context.Context, any) error { return nil }

// WriteCustomStream 实现 SessionFacade（本批未使用）
func (f *fakeSession) WriteCustomStream(context.Context, any) error { return nil }

// GetEnv 实现 SessionFacade（本批未使用）
func (f *fakeSession) GetEnv(string, ...any) any { return nil }

// Interact 实现 SessionFacade（本批未使用）
func (f *fakeSession) Interact(context.Context, any) error { return nil }

// ClearSession 实现 SessionFacade（本批未使用）
func (f *fakeSession) ClearSession(context.Context) {}

// _ 编译期断言
var _ sessioninterfaces.SessionFacade = (*fakeSession)(nil)

// newSessionCallback 构造带 session 与工具输入的回调上下文
func newSessionCallback(t *testing.T, sessionID, toolName, toolCallID string) *agentinterfaces.AgentCallbackContext {
	t.Helper()
	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: toolName,
		ToolArgs: map[string]any{},
	}
	inputs.ToolCall = &llmschema.ToolCall{ID: toolCallID, Name: toolName}
	return agentinterfaces.NewAgentCallbackContext(nil, inputs, &fakeSession{sessionID: sessionID})
}

// TestSessionIDOf 验证会话 ID 读取与缺 session 报错
func TestSessionIDOf(t *testing.T) {
	cbc := newSessionCallback(t, "sess-1", "tool", "call-1")
	got, err := sessionIDOf(cbc)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got != "sess-1" {
		t.Fatalf("会话 ID 不符: %q", got)
	}

	if _, err := sessionIDOf(nil); err == nil {
		t.Fatalf("nil 应报错")
	}
	if _, err := sessionIDOf(newTestCallbackContext()); err == nil {
		t.Fatalf("缺 session 应报错")
	}
}

// TestToolCallIDOf与ToolNameOf 验证工具身份读取与非法
func TestToolCallIDOf与ToolNameOf(t *testing.T) {
	cbc := newSessionCallback(t, "s", "my-tool", "call-1")
	inputs := cbc.Inputs().(*agentinterfaces.ToolCallInputs)

	if got, err := toolCallIDOf(inputs); err != nil || got != "call-1" {
		t.Fatalf("call id 不符: %q %v", got, err)
	}
	if got, err := toolNameOf(inputs); err != nil || got != "my-tool" {
		t.Fatalf("tool name 不符: %q %v", got, err)
	}

	if _, err := toolCallIDOf(nil); err == nil {
		t.Fatalf("nil inputs 应报错")
	}
	if _, err := toolNameOf(nil); err == nil {
		t.Fatalf("nil inputs 应报错")
	}
}

// TestAttachKey 验证 metadata 挂载与非法入参
func TestAttachKey(t *testing.T) {
	request := &saschema.InterruptRequest{}
	key, _ := NewToolInvocationKeyV1("i", "s", "r", ExecutorKindAgent, "e", "c", ToolInvocationKeyVersion)

	if err := attachKey(request, key); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	wire, ok := request.Metadata["tool_invocation_key"].(map[string]any)
	if !ok || wire["invocation_id"] != "i" {
		t.Fatalf("metadata 未写入: %v", request.Metadata)
	}

	// 已有 metadata 时保留其它键
	request2 := &saschema.InterruptRequest{Metadata: map[string]any{"keep": 1}}
	if err := attachKey(request2, key); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if request2.Metadata["keep"] != 1 {
		t.Fatalf("应保留原有键")
	}

	if err := attachKey(nil, key); err == nil {
		t.Fatalf("nil request 应报错")
	}
	if err := attachKey(request, nil); err == nil {
		t.Fatalf("nil key 应报错")
	}
}

// TestToolInterruptOf 验证从异常链取中断
func TestToolInterruptOf(t *testing.T) {
	if toolInterruptOf(nil) != nil {
		t.Fatalf("nil 应返回 nil")
	}
	plain := context.Canceled
	if toolInterruptOf(plain) != nil {
		t.Fatalf("普通错误应返回 nil")
	}

	exc := &saschema.ToolInterruptException{Request: &saschema.InterruptRequest{}}
	if toolInterruptOf(exc) != exc {
		t.Fatalf("应取到中断")
	}
}

// TestResumeInputOf_无答案 验证未找到答案时返回 false
func TestResumeInputOf_无答案(t *testing.T) {
	cbc := newTestCallbackContext()
	if _, ok := resumeInputOf(cbc, "call-1"); ok {
		t.Fatalf("无输入应返回 false")
	}
	if _, ok := resumeInputOf(nil, "call-1"); ok {
		t.Fatalf("nil 应返回 false")
	}
}

// TestResumeInputOf_从恢复输入取 验证从 RESUME_USER_INPUT_KEY 取答案
func TestResumeInputOf_从恢复输入取(t *testing.T) {
	cbc := newTestCallbackContext()
	cbc.SetExtra(saschema.ResumeUserInputKey, map[string]any{"call-1": "answer"})
	got, ok := resumeInputOf(cbc, "call-1")
	if !ok || got != "answer" {
		t.Fatalf("应取到 answer，实际 %v %v", got, ok)
	}
}

// TestResumeInputOf_从新鲜答案取 验证优先取 non-carried 答案
func TestResumeInputOf_从新鲜答案取(t *testing.T) {
	cbc := newTestCallbackContext()
	cbc.SetExtra(saschema.ResumeInterruptAnswersKey, map[string]map[string]*saschema.InterruptAnswer{
		"call-1": {
			"owner-a": {Owner: "owner-a", Value: "fresh", Carried: false},
			"owner-b": {Owner: "owner-b", Value: "carried", Carried: true},
		},
	})
	got, ok := resumeInputOf(cbc, "call-1")
	if !ok || got != "fresh" {
		t.Fatalf("应取到 fresh，实际 %v", got)
	}
}

// TestRunExtraOf与PopNonPermission 验证 run extra 读取与弹出
func TestRunExtraOf与PopNonPermission(t *testing.T) {
	cbc := newTestCallbackContext()
	cbc.SetExtra("run_context", map[string]any{
		"extra": map[string]any{RootNonPermissionResumeDTOKey: "marker"},
	})

	extra := runExtraOf(cbc)
	if extra == nil || extra[RootNonPermissionResumeDTOKey] != "marker" {
		t.Fatalf("应读到 run extra")
	}

	present, value := popNonPermissionResume(cbc)
	if !present || value != "marker" {
		t.Fatalf("应弹出 marker，实际 %v %v", present, value)
	}
	if _, exists := extra[RootNonPermissionResumeDTOKey]; exists {
		t.Fatalf("应已删除")
	}

	// 再弹一次 → false
	if present, _ := popNonPermissionResume(cbc); present {
		t.Fatalf("应返回 false")
	}
	if popNonPermissionResume(nil); false {
		t.Fatalf("unreachable")
	}
}

// TestFinishOnToolException与Completion 验证两个 rail 的结束语义
func TestFinishOnToolException与Completion(t *testing.T) {
	q := newTestQueue()
	cbc := newSessionCallback(t, "s", "tool", "call-1")

	card, err := q.Begin("s", "r", "s", "call-1", "tool")
	if err != nil {
		t.Fatalf("Begin 失败: %v", err)
	}
	cbc.SetExtra(ToolInvocationContextAttribute, card.Key)

	finishOnToolException(NewRootPermissionQueueRail(q, nil), cbc)
	if _, ok := q.Get(card.Key); ok {
		t.Fatalf("异常路径应结束卡片")
	}

	// completion rail 完成护栏
	card2, _ := q.Begin("s", "r2", "s", "call-2", "tool")
	cbc.SetExtra(ToolInvocationContextAttribute, card2.Key)
	finishOnCompletion(NewRootPermissionCompletionRail(q), cbc)
	if _, ok := q.Get(card2.Key); ok {
		t.Fatalf("完成路径应结束卡片")
	}

	// 无号码牌时不应 panic
	cbc2 := newTestCallbackContext()
	finishOnToolException(NewRootPermissionQueueRail(q, nil), cbc2)
	finishOnCompletion(NewRootPermissionCompletionRail(q), cbc2)
}

// TestBindLifecycle与ResetLifecycle 验证幂等绑定与重置
func TestBindLifecycle与ResetLifecycle(t *testing.T) {
	q := newTestQueue()
	sandboxed := true
	rail, _ := NewRootContextRail(q, "sysop", nil, &sandboxed)
	cbc := newSessionCallback(t, "sess-1", "tool", "call-1")

	// 无上下文 → 绑定失败理由但不应 panic
	if err := rail.bindLifecycle(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if _, bound := cbc.GetExtra(rootContextTokensAttribute); !bound {
		t.Fatalf("应写入 token")
	}

	// 幂等
	if err := rail.bindLifecycle(context.Background(), cbc); err != nil {
		t.Fatalf("重复绑定不应报错: %v", err)
	}

	rail.resetLifecycle(cbc)
	if _, bound := cbc.GetExtra(rootContextTokensAttribute); bound {
		t.Fatalf("应清除 token")
	}

	// 无 token 时 reset 不应 panic
	rail.resetLifecycle(cbc)
	rail.resetLifecycle(nil)
}

// TestBindLifecycle_nil上下文 验证 nil 回调安全
func TestBindLifecycle_nil上下文(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, nil, nil, nil)
	if err := rail.bindLifecycle(context.Background(), nil); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
}

// TestResolveSandboxed_显式为false 验证显式 false 时即使 mode 是 sandbox 也返回 false
func TestResolveSandboxed_显式为false(t *testing.T) {
	q := newTestQueue()
	sandboxed := false
	rail, _ := NewRootContextRail(q, nil, nil, &sandboxed)
	if rail.resolveSandboxed(fakeOperationWithCard{mode: sysoperation.OperationModeSandbox}) {
		t.Fatalf("显式 false 应优先")
	}
}

// ──────────────────────────── RootContextRail 钩子测试 ────────────────────────────

// boundCallback 构造一个带完整上下文绑定的回调
func boundCallback(t *testing.T, rail *RootContextRail, sessionID string) (*agentinterfaces.AgentCallbackContext, *RootDecisionContext) {
	t.Helper()
	turn, err := NewRootIntentTurn("req-1", RootIntentTurnKindFresh, "帮我改配置", nil)
	if err != nil {
		t.Fatalf("构造轮次失败: %v", err)
	}
	dc, err := NewRootDecisionContext(sessionID, "req-1", "web", []RootIntentTurn{turn}, "")
	if err != nil {
		t.Fatalf("构造上下文失败: %v", err)
	}

	cbc := newSessionCallback(t, sessionID, "some_tool", "call-1")
	owner := &sschema.PermissionContext{PrincipalUserID: "u1", AvatarPrincipalName: "张三"}

	// 写 run extra
	cbc.SetExtra("run_context", map[string]any{
		"extra": map[string]any{},
	})
	extra := runExtraOf(cbc)
	extra[RootContextKey] = dc.ToMapping()
	extra[RootPermissionOwnerKey] = validOwnerMap()

	_ = owner
	return cbc, dc
}

// TestFromCallback_正常 验证正常还原上下文与 owner
func TestFromCallback_正常(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)
	cbc, dc := boundCallback(t, rail, "sess-1")

	got, owner, reason := rail.fromCallback(cbc)
	if reason != "" {
		t.Fatalf("不应有失败原因: %q", reason)
	}
	if got == nil || got.SessionID != dc.SessionID {
		t.Fatalf("上下文不符: %+v", got)
	}
	if owner == nil || owner.PrincipalUserID != "u1" || owner.ChannelID != "web" {
		t.Fatalf("owner 不符: %+v", owner)
	}
}

// TestFromCallback_缺extra 验证无 run extra 时报 missing
func TestFromCallback_缺extra(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)
	cbc := newSessionCallback(t, "sess-1", "tool", "call-1")

	_, _, reason := rail.fromCallback(cbc)
	if reason != rootContextFailureMissing {
		t.Fatalf("应为 missing，实际 %q", reason)
	}
}

// TestFromCallback_上下文非法 验证有 extra 但缺 RootContextKey 时报 invalid
func TestFromCallback_上下文非法(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)
	cbc := newSessionCallback(t, "sess-1", "tool", "call-1")
	cbc.SetExtra("run_context", map[string]any{"extra": map[string]any{}})

	_, _, reason := rail.fromCallback(cbc)
	if reason != rootContextFailureInvalid {
		t.Fatalf("应为 invalid，实际 %q", reason)
	}
}

// TestFromCallback_owner非法 验证 owner 非法时报 invalid
func TestFromCallback_owner非法(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)
	cbc, _ := boundCallback(t, rail, "sess-1")
	runExtraOf(cbc)[RootPermissionOwnerKey] = "not-a-map"

	_, _, reason := rail.fromCallback(cbc)
	if reason != rootContextFailureInvalid {
		t.Fatalf("应为 invalid，实际 %q", reason)
	}
}

// TestFromCallback_会话不匹配 验证会话 ID 不一致时报 mismatch
func TestFromCallback_会话不匹配(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)
	cbc, _ := boundCallback(t, rail, "sess-1")

	// 换一个 sessionID 不同的回调，但 extra 里还是 sess-1 的上下文
	other := newSessionCallback(t, "other-sess", "tool", "call-1")
	other.SetExtra("run_context", map[string]any{"extra": runExtraOf(cbc)})

	_, _, reason := rail.fromCallback(other)
	if reason != rootContextFailureSessionMismatch {
		t.Fatalf("应为 mismatch，实际 %q", reason)
	}
}

// TestFromCallback_命令不可用 验证无 sys operation 时报 command_unavailable
func TestFromCallback_命令不可用(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, nil, nil, nil)
	cbc, _ := boundCallback(t, rail, "sess-1")

	_, _, reason := rail.fromCallback(cbc)
	if reason != rootContextFailureCommandUnavailable {
		t.Fatalf("应为 command_unavailable，实际 %q", reason)
	}
}

// TestBeforeInvoke与AfterInvoke 验证生命周期钩子
func TestBeforeInvoke与AfterInvoke(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)
	cbc, _ := boundCallback(t, rail, "sess-1")

	if err := rail.BeforeInvoke(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if _, bound := cbc.GetExtra(rootContextTokensAttribute); !bound {
		t.Fatalf("应绑定 token")
	}
	if err := rail.AfterInvoke(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if _, bound := cbc.GetExtra(rootContextTokensAttribute); bound {
		t.Fatalf("应清除 token")
	}
}

// TestBeforeAfterTaskIteration 验证迭代钩子
func TestBeforeAfterTaskIteration(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)
	cbc, _ := boundCallback(t, rail, "sess-1")

	if err := rail.BeforeTaskIteration(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if err := rail.AfterTaskIteration(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
}

// TestBeforeToolCall_正常放行 验证上下文齐全时不跳过工具
func TestBeforeToolCall_正常放行(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)
	cbc, _ := boundCallback(t, rail, "sess-1")

	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if skipped, _ := cbc.GetExtra("_skip_tool"); skipped == true {
		t.Fatalf("不应跳过工具")
	}
}

// TestBeforeToolCall_缺上下文跳过 验证上下文缺失时跳过工具
func TestBeforeToolCall_缺上下文跳过(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)
	cbc := newSessionCallback(t, "sess-1", "tool", "call-1")

	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if skipped, _ := cbc.GetExtra("_skip_tool"); skipped != true {
		t.Fatalf("应跳过工具")
	}
	inputs := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	result, ok := inputs.ToolResult.(map[string]any)
	if !ok {
		t.Fatalf("应写入失败结果")
	}
	if result["blocked_reason"] != rootContextFailureMissing {
		t.Fatalf("blocked_reason 不符: %v", result["blocked_reason"])
	}
	if inputs.ToolMsg == nil {
		t.Fatalf("应写入 tool message")
	}
}

// TestBeforeToolCall_非工具输入 验证 inputs 非 ToolCallInputs 时直接返回
func TestBeforeToolCall_非工具输入(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, &fakeSession{sessionID: "s"})

	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
}

// TestBeforeToolCall_权限续接 验证恢复时采用卡里冻结的上下文
func TestBeforeToolCall_权限续接(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)
	cbc, dc := boundCallback(t, rail, "sess-1")

	// 卡里冻结一个上下文
	card := RootPermissionCard{Key: mustKey(t), RootContext: dc}
	cbc.SetExtra(RootPermissionResumeAttribute, &RootPermissionResume{Card: card, UserInput: "ans"})

	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if skipped, _ := cbc.GetExtra("_skip_tool"); skipped == true {
		t.Fatalf("不应跳过工具")
	}
}

// TestBeforeToolCall_续接上下文缺失 验证卡里无上下文时跳过
func TestBeforeToolCall_续接上下文缺失(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)
	cbc, _ := boundCallback(t, rail, "sess-1")

	card := RootPermissionCard{Key: mustKey(t), RootContext: nil}
	cbc.SetExtra(RootPermissionResumeAttribute, &RootPermissionResume{Card: card, UserInput: "ans"})

	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if skipped, _ := cbc.GetExtra("_skip_tool"); skipped != true {
		t.Fatalf("应跳过工具")
	}
}

// TestBeforeToolCall_续接会话不匹配 验证冻结上下文的会话与当前不符时跳过
func TestBeforeToolCall_续接会话不匹配(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)
	cbc, _ := boundCallback(t, rail, "sess-1")

	otherCtx, _ := NewRootDecisionContext("other-sess", "req-1", "web", nil, "")
	card := RootPermissionCard{Key: mustKey(t), RootContext: otherCtx}
	cbc.SetExtra(RootPermissionResumeAttribute, &RootPermissionResume{Card: card, UserInput: "ans"})

	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if skipped, _ := cbc.GetExtra("_skip_tool"); skipped != true {
		t.Fatalf("应跳过工具")
	}
}

// TestBeforeToolCall_反问续接正常 验证 ask_user 反问答案被追加到意图窗口
func TestBeforeToolCall_反问续接正常(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)

	// 构造 ask_user 工具回调
	cbc, dc := boundCallback(t, rail, "sess-1")
	cbc.Inputs().(*agentinterfaces.ToolCallInputs).ToolName = AskUserToolName
	cbc.Inputs().(*agentinterfaces.ToolCallInputs).ToolCall.Name = AskUserToolName

	// 构造续接 DTO
	cont := &RootAskUserContinuation{
		ToolCallID: "call-1",
		Context:    dc,
		Questions:  []AskUserQuestionReference{{Question: "哪个配置？"}},
	}
	prepared := &RootAskUserResume{
		Continuation:   cont,
		Clarifications: []RootAskUserClarification{{Question: "哪个配置？", Answers: []string{"dev"}}},
	}
	runExtraOf(cbc)[AskUserResumeDTOKey] = prepared

	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if skipped, _ := cbc.GetExtra("_skip_tool"); skipped == true {
		t.Fatalf("不应跳过工具")
	}
	// DTO 应被消费
	if _, still := runExtraOf(cbc)[AskUserResumeDTOKey]; still {
		t.Fatalf("DTO 应被消费")
	}
}

// TestBeforeToolCall_反问续接会话不匹配 验证续接会话不符时跳过
func TestBeforeToolCall_反问续接会话不匹配(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)

	cbc, dc := boundCallback(t, rail, "sess-1")
	cbc.Inputs().(*agentinterfaces.ToolCallInputs).ToolName = AskUserToolName
	cbc.Inputs().(*agentinterfaces.ToolCallInputs).ToolCall.Name = AskUserToolName

	otherCtx, _ := NewRootDecisionContext("other", "req-1", "web", nil, "")
	prepared := &RootAskUserResume{
		Continuation: &RootAskUserContinuation{
			ToolCallID: "call-1",
			Context:    otherCtx,
			Questions:  []AskUserQuestionReference{{Question: "q"}},
		},
	}
	runExtraOf(cbc)[AskUserResumeDTOKey] = prepared
	_ = dc

	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if skipped, _ := cbc.GetExtra("_skip_tool"); skipped != true {
		t.Fatalf("应跳过工具")
	}
}

// TestBeforeToolCall_非ask_user丢弃DTO 验证非 ask_user 工具会丢弃残留 DTO
func TestBeforeToolCall_非ask_user丢弃DTO(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)
	cbc, _ := boundCallback(t, rail, "sess-1")

	runExtraOf(cbc)[AskUserResumeDTOKey] = &RootAskUserResume{}
	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if _, still := runExtraOf(cbc)[AskUserResumeDTOKey]; still {
		t.Fatalf("DTO 应被丢弃")
	}
}

// TestAfterToolCall_无中断 验证非中断异常不做处理
func TestAfterToolCall_无中断(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)
	cbc, _ := boundCallback(t, rail, "sess-1")

	if err := rail.AfterToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
}

// TestAfterToolCall_写metadata 验证 ask_user 中断时写入续接快照
func TestAfterToolCall_写metadata(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)

	cbc, dc := boundCallback(t, rail, "sess-1")
	cbc.Inputs().(*agentinterfaces.ToolCallInputs).ToolName = AskUserToolName
	cbc.Inputs().(*agentinterfaces.ToolCallInputs).ToolCall.Name = AskUserToolName
	cbc.Inputs().(*agentinterfaces.ToolCallInputs).ToolArgs = map[string]any{
		"questions": []any{map[string]any{"question": "哪个？"}},
	}

	request := &saschema.InterruptRequest{}
	cbc.SetException(&saschema.ToolInterruptException{Request: request})

	ctx, _ := BindRootDecisionContext(context.Background(), dc)
	if err := rail.AfterToolCall(ctx, cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if _, ok := request.Metadata[AskUserContinuationMetadataKey]; !ok {
		t.Fatalf("应写入续接快照，实际 %v", request.Metadata)
	}
}

// TestAfterToolCall_无上下文 验证当前上下文缺失时不处理
func TestAfterToolCall_无上下文(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)
	cbc := newSessionCallback(t, "sess-1", AskUserToolName, "call-1")
	request := &saschema.InterruptRequest{}
	cbc.SetException(&saschema.ToolInterruptException{Request: request})

	if err := rail.AfterToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if request.Metadata != nil {
		t.Fatalf("不应写入 metadata")
	}
}

// TestAfterToolCall_非ask_user 验证非 ask_user 工具不写 metadata
func TestAfterToolCall_非ask_user(t *testing.T) {
	q := newTestQueue()
	rail, _ := NewRootContextRail(q, "sysop", nil, nil)
	cbc, dc := boundCallback(t, rail, "sess-1")
	request := &saschema.InterruptRequest{}
	cbc.SetException(&saschema.ToolInterruptException{Request: request})

	ctx, _ := BindRootDecisionContext(context.Background(), dc)
	if err := rail.AfterToolCall(ctx, cbc); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if request.Metadata != nil {
		t.Fatalf("非 ask_user 不应写 metadata")
	}
}

// TestOwnerFromCallback 验证 owner 从回调还原
func TestOwnerFromCallback(t *testing.T) {
	cbc := newTestCallbackContext()
	if got, err := ownerFromCallback(context.Background(), cbc); err != nil || got != nil {
		t.Fatalf("无 owner 应返回 nil nil")
	}

	cbc.SetExtra("run_context", map[string]any{"extra": map[string]any{
		RootPermissionOwnerKey: validOwnerMap(),
	}})
	got, err := ownerFromCallback(context.Background(), cbc)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got == nil || got.PrincipalUserID != "u1" {
		t.Fatalf("owner 不符: %+v", got)
	}
}

// TestToolNameOfInputs_优先级 验证 ToolCall.Name 优先于 ToolName
func TestToolNameOfInputs_优先级(t *testing.T) {
	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: "from-inputs",
		ToolCall: &llmschema.ToolCall{Name: "from-call"},
	}
	if got := toolNameOfInputs(inputs); got != "from-call" {
		t.Fatalf("应优先 from-call，实际 %q", got)
	}

	// ToolCall.Name 为空时回退
	inputs2 := &agentinterfaces.ToolCallInputs{
		ToolName: "from-inputs",
		ToolCall: &llmschema.ToolCall{},
	}
	if got := toolNameOfInputs(inputs2); got != "from-inputs" {
		t.Fatalf("应回退 from-inputs，实际 %q", got)
	}

	// ToolCall 为 nil
	inputs3 := &agentinterfaces.ToolCallInputs{ToolName: "only"}
	if got := toolNameOfInputs(inputs3); got != "only" {
		t.Fatalf("应为 only，实际 %q", got)
	}
}

// TestToolCallIDOfInputs_正常 验证调用 ID 读取与 nil ToolCall
func TestToolCallIDOfInputs_正常(t *testing.T) {
	inputs := &agentinterfaces.ToolCallInputs{ToolCall: &llmschema.ToolCall{ID: "c1"}}
	if got := toolCallIDOfInputs(inputs); got != "c1" {
		t.Fatalf("应为 c1，实际 %q", got)
	}
	if got := toolCallIDOfInputs(&agentinterfaces.ToolCallInputs{}); got != "" {
		t.Fatalf("nil ToolCall 应返回空")
	}
}

// TestCallbackSessionID_无session 验证无 session 返回空串
func TestCallbackSessionID_无session(t *testing.T) {
	if got := callbackSessionID(newTestCallbackContext()); got != "" {
		t.Fatalf("无 session 应返回空，实际 %q", got)
	}
}

// mustKey 构造一个测试用号码牌
func mustKey(t *testing.T) *ToolInvocationKeyV1 {
	t.Helper()
	key, err := NewToolInvocationKeyV1("inv-1", "sess-1", "req-1", ExecutorKindAgent, "sess-1", "call-1", ToolInvocationKeyVersion)
	if err != nil {
		t.Fatalf("构造号码牌失败: %v", err)
	}
	return key
}

// GetCacheIdentity 返回零值缓存身份（测试替身，不参与 KV 亲和）。
// Python: SessionFacade.get_cache_identity 的测试替身实现。
func (f *fakeSession) GetCacheIdentity() kv_cache.KVCacheIdentity {
	return kv_cache.KVCacheIdentity{}
}

// GetParentSessionID 返回空父会话（测试替身）。
func (f *fakeSession) GetParentSessionID() string { return "" }
