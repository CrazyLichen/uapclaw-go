package evolution

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/interrupt"
)

// newEvolutionTestContext 构造带工具输入与 session 的回调上下文。
func newEvolutionTestContext(t *testing.T, toolName string, toolArgs map[string]any, sess *fakeSessionFacade) *agentinterfaces.AgentCallbackContext {
	t.Helper()
	return agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{
		ToolName: toolName,
		ToolArgs: toolArgs,
	}, sess)
}

// stubSubmissionService 测试用提交服务桩（15.24 真实实现回填前的占位）。
type stubSubmissionService struct{}

func (s *stubSubmissionService) PrepareEvolveSubmission(_ *review.EvolutionReviewRuntime, _ string, _ map[string]any, _ []string, _ string) (*review.EvolutionProposalSelection, error) {
	return nil, nil
}

func (s *stubSubmissionService) PrepareSimplifySubmission(_ map[string]any, _ []map[string]any) error {
	return nil
}

// newConfiguredRail 构造已配置依赖的 rail（Python preflight 要求已配置才能走到后续分支）。
func newConfiguredRail() *EvolutionInterruptRail {
	rail := NewEvolutionInterruptRail()
	rail.Configure(&review.EvolutionReviewRuntime{}, &stubSubmissionService{}, false, "cn")
	return rail
}

func evolveToolArgs() map[string]any {
	return map[string]any{
		"subject":               map[string]any{"kind": "skill", "name": "demo"},
		"evolution_review_ref":  "evrr_x",
		"selected_proposal_ids": []any{"p1"},
	}
}

// TestEvolutionInterruptRail_Construction 测试构造与映射。
// Python: _ACTIVE_EVOLUTION_TOOL_OPERATIONS / priority=89 / interrupt_kind (evolution_interrupt_rail.py:29-51)
func TestEvolutionInterruptRail_Construction(t *testing.T) {
	rail := NewEvolutionInterruptRail()
	if rail.Priority() != 89 {
		t.Fatalf("期望 priority 89, got %d", rail.Priority())
	}
	if rail.InterruptKind() != saschema.InterruptKindSkillEvolutionApproval {
		t.Fatalf("期望 skill_evolution_approval, got %s", rail.InterruptKind())
	}
	tools := rail.GetTools()
	if len(tools) != 2 {
		t.Fatalf("期望拦截两个工具, got %v", tools)
	}
	seen := map[string]bool{}
	for _, name := range tools {
		seen[name] = true
	}
	if !seen["evolve_skill_experiences"] || !seen["simplify_skill_experiences"] {
		t.Fatalf("工具映射缺失: %v", tools)
	}
	if rail.AutoSave() {
		t.Fatalf("默认 auto_save 期望 false")
	}
	if rail.Language() != "cn" {
		t.Fatalf("默认语言期望 cn, got %s", rail.Language())
	}
}

// TestEvolutionInterruptRail_Configure 测试依赖绑定与 auto_save/language 存取。
// Python: configure (evolution_interrupt_rail.py:55-68) + auto_save (:70-76) + language
func TestEvolutionInterruptRail_Configure(t *testing.T) {
	rail := NewEvolutionInterruptRail()
	rail.SetAutoSave(true)
	if !rail.AutoSave() {
		t.Fatalf("SetAutoSave 未生效")
	}
	rail.SetAutoSave(false)
	if rail.AutoSave() {
		t.Fatalf("auto_save false 未生效")
	}
	rail.SetLanguage("en")
	if !rail.IsEnglish() {
		t.Fatalf("SetLanguage(en) 后 IsEnglish 应为真")
	}
	rail.SetLanguage("CN")
	if rail.IsEnglish() {
		t.Fatalf("语言判定应大小写不敏感")
	}
}

// TestEvolutionInterruptRail_ResolveInterrupt_NotConfigured 测试未配置依赖时的拒绝路径。
// Python: resolve_interrupt → _preflight 抛 ValueError → _reject_tool (:78-114/:117-122)
func TestEvolutionInterruptRail_ResolveInterrupt_NotConfigured(t *testing.T) {
	rail := NewEvolutionInterruptRail()
	cbc := newEvolutionTestContext(t, "evolve_skill_experiences", evolveToolArgs(), &fakeSessionFacade{})
	decision := rail.ResolveInterruptFn(context.Background(), cbc, nil, nil, nil)
	reject, ok := decision.(*interrupt.RejectResult)
	if !ok {
		t.Fatalf("期望拒绝决策, got %T", decision)
	}
	if !strings.Contains(rejectTextOf(reject), "EvolutionInterruptRail is not configured") {
		t.Fatalf("错误文案不符: %v", reject.ToolResult)
	}
}

// rejectTextOf 提取 RejectResult 的错误文本。
// 偏差 D-15.21-4：Python ToolOutput(success=False, error, data) 未移植，
// Go 以 map[string]any{"error", "data"} 承载（harness/tools/base_tool.py 无对应物）。
func rejectTextOf(r *interrupt.RejectResult) string {
	if m, ok := r.ToolResult.(map[string]any); ok {
		if errText, ok := m["error"].(string); ok {
			return errText
		}
	}
	if s, ok := r.ToolResult.(string); ok {
		return s
	}
	return ""
}

// TestEvolutionInterruptRail_ResolveInterrupt_AutoSaveBypass 测试 auto_save 旁路。
// Python: resolve_interrupt 中 `if self._auto_save: return self.approve()` (:105-107)
func TestEvolutionInterruptRail_ResolveInterrupt_AutoSaveBypass(t *testing.T) {
	rail := newConfiguredRail()
	rail.SetAutoSave(true)
	// auto_save 判定在 preflight 成功之后（Python :105-107）
	cbc := newEvolutionTestContext(t, "evolve_skill_experiences", evolveToolArgs(), &fakeSessionFacade{})
	decision := rail.ResolveInterruptFn(context.Background(), cbc, nil, nil, nil)
	if _, ok := decision.(*interrupt.ApproveResult); !ok {
		t.Fatalf("auto_save 期望放行, got %T", decision)
	}
}

// TestEvolutionInterruptRail_ResolveInterrupt_UserInput 测试用户输入四分支。
// Python: _resolve_user_input (evolution_interrupt_rail.py:140-166)
func TestEvolutionInterruptRail_ResolveInterrupt_UserInput(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  string
	}{
		{"allow_once 放行", map[string]any{"action": "allow_once"}, "*interrupt.ApproveResult"},
		{"allow_always 放行并写自动确认", map[string]any{"action": "allow_always"}, "*interrupt.ApproveResult"},
		{"reject 拒绝", map[string]any{"action": "reject", "feedback": "别动"}, "*interrupt.RejectResult"},
		{"reject 无反馈默认文案", map[string]any{"action": "reject"}, "*interrupt.RejectResult"},
		{"非法 action 再中断", map[string]any{"action": "wat"}, "*interrupt.InterruptResult"},
		{"非 dict 输入按空处理再中断", "garbage", "*interrupt.InterruptResult"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rail := newConfiguredRail()
			sess := &fakeSessionFacade{sessionID: "sess-1"}
			cbc := newEvolutionTestContext(t, "evolve_skill_experiences", evolveToolArgs(), sess)
			decision := rail.ResolveInterruptFn(context.Background(), cbc, nil, tc.input, nil)
			got := decisionTypeName(decision)
			if got != tc.want {
				t.Fatalf("期望 %s, got %s", tc.want, got)
			}
			if tc.name == "allow_always 放行并写自动确认" {
				raw, _ := sess.GetState(state.StringKey(saschema.InterruptAutoConfirmKey))
				cfg, ok := raw.(map[string]any)
				if !ok || cfg["evolution:evolve_skill_experiences:skill:demo"] != true {
					t.Fatalf("自动确认键未写入: %v", raw)
				}
			}
			if tc.name == "reject 拒绝" {
				reject := decision.(*interrupt.RejectResult)
				if !strings.Contains(rejectTextOf(reject), "别动") {
					t.Fatalf("反馈未透传: %v", reject.ToolResult)
				}
			}
			if tc.name == "reject 无反馈默认文案" {
				reject := decision.(*interrupt.RejectResult)
				if !strings.Contains(rejectTextOf(reject), "evolution tool call rejected by user") {
					t.Fatalf("默认文案不符: %v", reject.ToolResult)
				}
			}
		})
	}
}

// decisionTypeName 返回决策具体类型名。
func decisionTypeName(v any) string {
	switch v.(type) {
	case *interrupt.ApproveResult:
		return "*interrupt.ApproveResult"
	case *interrupt.RejectResult:
		return "*interrupt.RejectResult"
	case *interrupt.InterruptResult:
		return "*interrupt.InterruptResult"
	default:
		return "unknown"
	}
}

// TestEvolutionInterruptRail_ResolveInterrupt_AutoConfirmed 测试自动确认配置命中。
// Python: _active_auto_confirm_key + _is_auto_confirmed (:314-321)
func TestEvolutionInterruptRail_ResolveInterrupt_AutoConfirmed(t *testing.T) {
	rail := newConfiguredRail()
	cbc := newEvolutionTestContext(t, "evolve_skill_experiences", evolveToolArgs(), &fakeSessionFacade{})
	cfg := map[string]any{"evolution:evolve_skill_experiences:skill:demo": true}
	decision := rail.ResolveInterruptFn(context.Background(), cbc, nil, nil, cfg)
	if _, ok := decision.(*interrupt.ApproveResult); !ok {
		t.Fatalf("自动确认期望放行, got %T", decision)
	}
	// 键不匹配 → 中断
	cfg2 := map[string]any{"evolution:evolve_skill_experiences:skill:other": true}
	decision2 := rail.ResolveInterruptFn(context.Background(), cbc, nil, nil, cfg2)
	if _, ok := decision2.(*interrupt.InterruptResult); !ok {
		t.Fatalf("键不匹配期望中断, got %T", decision2)
	}
}

// TestEvolutionInterruptRail_ResolveInterrupt_InterruptRequest 测试中断请求载荷。
// Python: resolve_interrupt 末段 InterruptRequest 构造 (:109-113)
func TestEvolutionInterruptRail_ResolveInterrupt_InterruptRequest(t *testing.T) {
	rail := newConfiguredRail()
	cbc := newEvolutionTestContext(t, "evolve_skill_experiences", evolveToolArgs(), &fakeSessionFacade{})
	decision := rail.ResolveInterruptFn(context.Background(), cbc, nil, nil, nil)
	result, ok := decision.(*interrupt.InterruptResult)
	if !ok {
		t.Fatalf("期望中断, got %T", decision)
	}
	req, ok := result.Request.(*saschema.InterruptRequest)
	if !ok {
		t.Fatalf("期望 InterruptRequest, got %T", result.Request)
	}
	if !strings.Contains(req.Message, "demo") {
		t.Fatalf("中断消息应含 subject 名: %s", req.Message)
	}
	if req.AutoConfirmKey != "evolution:evolve_skill_experiences:skill:demo" {
		t.Fatalf("auto_confirm 键不符: %s", req.AutoConfirmKey)
	}
	if len(req.UIOptions) != 3 {
		t.Fatalf("期望 3 个 UI 选项, got %v", req.UIOptions)
	}
	if req.PayloadSchema == nil {
		t.Fatalf("payload_schema 不应为空")
	}
	if meta := req.Metadata; meta["source"] != "evolution_interrupt" || meta["evolution_approval"] != true {
		t.Fatalf("metadata 不符: %v", meta)
	}
	// 中断 kind 由基座 RaiseInterrupt→StampInterrupt 在真正抛出时盖章
	// （interrupt_base.go applyDecision），原始请求断言走 rail.InterruptKind()。
	if rail.InterruptKind() != saschema.InterruptKindSkillEvolutionApproval {
		t.Fatalf("中断 kind 不符: %s", rail.InterruptKind())
	}
}

// TestEvolutionInterruptRail_RenderApprovalMessage 测试中英双语审批消息骨架。
// Python: _render_approval_message (evolution_interrupt_rail.py:183-238)
func TestEvolutionInterruptRail_RenderApprovalMessage(t *testing.T) {
	t.Run("cn evolve", func(t *testing.T) {
		rail := NewEvolutionInterruptRail()
		msg := rail.RenderApprovalMessageForTest("evolve", "demo", "skill", 2)
		if !strings.Contains(msg, "是否批准") || !strings.Contains(msg, "2 条") {
			t.Fatalf("中文文案不符: %s", msg)
		}
	})
	t.Run("en evolve", func(t *testing.T) {
		rail := NewEvolutionInterruptRail()
		rail.SetLanguage("en")
		msg := rail.RenderApprovalMessageForTest("evolve", "demo", "skill", 2)
		if !strings.Contains(msg, "Approve 2 skill evolution experience(s)") {
			t.Fatalf("英文文案不符: %s", msg)
		}
	})
	t.Run("cn simplify", func(t *testing.T) {
		rail := NewEvolutionInterruptRail()
		msg := rail.RenderApprovalMessageForTest("simplify", "demo", "skill", 1)
		if !strings.Contains(msg, "1 项 Skill 经验精简操作") {
			t.Fatalf("中文精简文案不符: %s", msg)
		}
	})
	t.Run("en simplify", func(t *testing.T) {
		rail := NewEvolutionInterruptRail()
		rail.SetLanguage("en")
		msg := rail.RenderApprovalMessageForTest("simplify", "demo", "skill", 1)
		if !strings.Contains(msg, "Approve 1 skill experience simplification action(s)") {
			t.Fatalf("英文精简文案不符: %s", msg)
		}
	})
}

// TestEvolutionInterruptRail_ApprovalUIOptions 测试 UI 选项双语。
// Python: _approval_ui_options (evolution_interrupt_rail.py:253-269)
func TestEvolutionInterruptRail_ApprovalUIOptions(t *testing.T) {
	rail := NewEvolutionInterruptRail()
	opts := rail.ApprovalUIOptions()
	if len(opts) != 3 || opts[0]["value"] != "allow_once" || opts[1]["value"] != "allow_always" || opts[2]["value"] != "reject" {
		t.Fatalf("中文 UI 选项不符: %v", opts)
	}
	if opts[0]["label"] != "本次允许" {
		t.Fatalf("中文标签不符: %v", opts[0])
	}
	rail.SetLanguage("en")
	opts = rail.ApprovalUIOptions()
	if opts[0]["label"] != "Allow Once" {
		t.Fatalf("英文标签不符: %v", opts[0])
	}
}

// TestEvolutionInterruptRail_Preview 测试预览截断。
// Python: _preview (evolution_interrupt_rail.py:247-251)
func TestEvolutionInterruptRail_Preview(t *testing.T) {
	if got := previewText("short"); got != "short" {
		t.Fatalf("短文本不应截断: %s", got)
	}
	long := strings.Repeat("a", 300)
	got := previewText(long)
	if len([]rune(got)) != 240+3 || !strings.HasSuffix(got, "...") {
		t.Fatalf("期望 240 字符+省略号, got len=%d", len([]rune(got)))
	}
}

// TestEvolutionInterruptRail_TextOr 测试首个非空取值。
// Python: _text_or (evolution_interrupt_rail.py:239-245)
func TestEvolutionInterruptRail_TextOr(t *testing.T) {
	if got := textOr("", "  ", "second", "third"); got != "second" {
		t.Fatalf("期望首个非空, got %s", got)
	}
	if got := textOr("", ""); got != "" {
		t.Fatalf("全空期望空串, got %s", got)
	}
}

// TestEvolutionInterruptRail_UnknownTool 测试未知工具名防御性拒绝。
// Python: _ACTIVE_EVOLUTION_TOOL_OPERATIONS[tool_name] KeyError（:81-82，异常上抛）；
// Go ResolveInterruptFn 无 error 通道，改为拒绝决策（偏差 D-15.21-6，基座
// toolNames 过滤下运行期不可达，仅防御）。
func TestEvolutionInterruptRail_UnknownTool(t *testing.T) {
	rail := NewEvolutionInterruptRail()
	cbc := newEvolutionTestContext(t, "unrelated_tool", evolveToolArgs(), &fakeSessionFacade{})
	decision := rail.ResolveInterruptFn(context.Background(), cbc, nil, nil, nil)
	if _, ok := decision.(*interrupt.RejectResult); !ok {
		t.Fatalf("未知工具期望拒绝, got %T", decision)
	}
}

// TestEvolutionInterruptRail_BadSubject 测试 subject 归一化失败拒绝。
// Python: _normalize_subject → normalize_subject 异常 → _reject_tool (:92-95/:347-350)
func TestEvolutionInterruptRail_BadSubject(t *testing.T) {
	rail := newConfiguredRail()
	cbc := newEvolutionTestContext(t, "evolve_skill_experiences", map[string]any{
		"subject": "not-a-dict",
	}, &fakeSessionFacade{})
	decision := rail.ResolveInterruptFn(context.Background(), cbc, nil, nil, nil)
	reject, ok := decision.(*interrupt.RejectResult)
	if !ok {
		t.Fatalf("坏 subject 期望拒绝, got %T", decision)
	}
	if !strings.Contains(rejectTextOf(reject), "subject must be an object") {
		t.Fatalf("错误文案不符: %v", reject.ToolResult)
	}
}

// TestEvolutionInterruptRail_ErrorData 测试错误数据载荷。
// Python: _error_data (evolution_interrupt_rail.py:352-359)
func TestEvolutionInterruptRail_ErrorData(t *testing.T) {
	rail := NewEvolutionInterruptRail()
	cbc := newEvolutionTestContext(t, "evolve_skill_experiences", map[string]any{
		"subject":               map[string]any{"kind": "skill", "name": "demo"},
		"evolution_review_ref":  "evrr_x",
		"selected_proposal_ids": []any{"p1"},
	}, &fakeSessionFacade{})
	decision := rail.ResolveInterruptFn(context.Background(), cbc, nil, nil, nil)
	reject := decision.(*interrupt.RejectResult)
	data, ok := reject.ToolResult.(map[string]any)["data"].(map[string]any)
	if !ok {
		t.Fatalf("期望 data 载荷: %v", reject.ToolResult)
	}
	if data["tool_name"] != "evolve_skill_experiences" || data["evolution_review_ref"] != "evrr_x" {
		t.Fatalf("data 字段不符: %v", data)
	}
	ids, _ := data["selected_proposal_ids"].([]string)
	if len(ids) != 1 || ids[0] != "p1" {
		t.Fatalf("selected_proposal_ids 不符: %v", data["selected_proposal_ids"])
	}
}

// TestEvolutionInterruptRail_ActionPayloadSchema 测试动作载荷 schema。
// Python: _action_payload_schema (evolution_interrupt_rail.py:271-280)
func TestEvolutionInterruptRail_ActionPayloadSchema(t *testing.T) {
	schema := actionPayloadSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties 缺失: %v", schema)
	}
	action, ok := props["action"].(map[string]any)
	if !ok {
		t.Fatalf("action 定义缺失: %v", props)
	}
	if action["type"] != "string" {
		t.Fatalf("action.type 期望 string: %v", action)
	}
	required, _ := schema["required"].([]string)
	if len(required) != 1 || required[0] != "action" {
		t.Fatalf("required 不符: %v", schema["required"])
	}
}

// 编译期断言：rail 实现 AgentRail。
var _ agentinterfaces.AgentRail = (*EvolutionInterruptRail)(nil)
