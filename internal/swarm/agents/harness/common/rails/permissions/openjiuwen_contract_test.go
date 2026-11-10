package permissions

import (
	"reflect"
	"testing"

	secrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
)

// ──────────────────────────── 常量 ────────────────────────────

func TestOpenJiuwenContract_常量(t *testing.T) {
	if ManualApprovalResponseID != "auto_permission_manual_approval_required" {
		t.Errorf("ManualApprovalResponseID = %q", ManualApprovalResponseID)
	}
	if ManualApprovalToolName != "auto_permission" {
		t.Errorf("ManualApprovalToolName = %q", ManualApprovalToolName)
	}
	if PermissionDeniedPrefix != "[PERMISSION_DENIED] " {
		t.Errorf("PermissionDeniedPrefix = %q", PermissionDeniedPrefix)
	}
	if PermissionRejectedPrefix != "[PERMISSION_REJECTED] " {
		t.Errorf("PermissionRejectedPrefix = %q", PermissionRejectedPrefix)
	}
	if PermissionResultAllow != "allow" || PermissionResultDenied != "denied" ||
		PermissionResultInterrupt != "interrupt" || PermissionResultUserRejection != "user_rejection" {
		t.Error("result kind constants mismatch")
	}
}

// ──────────────────────────── 用例：build * response ───────────────────────────────

func TestBuildDeniedPermissionResponse(t *testing.T) {
	got := BuildDeniedPermissionResponse("路径越界")
	if got.Approved {
		t.Error("approved should be false")
	}
	if got.AutoConfirm {
		t.Error("auto_confirm should be false")
	}
	if got.Feedback != "[PERMISSION_DENIED] 路径越界" {
		t.Errorf("feedback = %q", got.Feedback)
	}
}

func TestBuildRejectedPermissionResponse(t *testing.T) {
	got := BuildRejectedPermissionResponse("用户拒绝")
	if got.Approved || got.AutoConfirm {
		t.Error("approved/auto_confirm should be false")
	}
	if got.Feedback != "[PERMISSION_REJECTED] 用户拒绝" {
		t.Errorf("feedback = %q", got.Feedback)
	}
}

func TestBuildManualApprovalRequiredResponse_基础(t *testing.T) {
	got := BuildManualApprovalRequiredResponse("需要人工确认", nil, nil)

	if got["id"] != ManualApprovalResponseID {
		t.Errorf("id = %v", got["id"])
	}
	if got["interrupt"] != true {
		t.Errorf("interrupt = %v", got["interrupt"])
	}
	value, ok := got["value"].(map[string]any)
	if !ok {
		t.Fatalf("value = %T", got["value"])
	}
	if value["message"] != "需要人工确认" {
		t.Errorf("message = %v", value["message"])
	}
	if value["tool_name"] != ManualApprovalToolName {
		t.Errorf("tool_name = %v", value["tool_name"])
	}
	if _, exists := got["metadata"]; exists {
		t.Error("metadata should be absent when nil")
	}
}

func TestBuildManualApprovalRequiredResponse_带metadata(t *testing.T) {
	meta := map[string]any{"k": "v"}
	got := BuildManualApprovalRequiredResponse("r", meta, nil)

	if _, exists := got["metadata"]; !exists {
		t.Fatal("metadata should be present")
	}
	gotMeta := got["metadata"].(map[string]any)
	if gotMeta["k"] != "v" {
		t.Errorf("metadata = %v", gotMeta)
	}
	// metadata 也会并入 value
	value := got["value"].(map[string]any)
	if value["k"] != "v" {
		t.Errorf("value should merge metadata, got %v", value)
	}
}

func TestBuildManualApprovalRequiredResponse_metadata是副本(t *testing.T) {
	meta := map[string]any{"k": "v"}
	got := BuildManualApprovalRequiredResponse("r", meta, nil)
	// 修改返回的 metadata 不应影响入参（Python 用 dict(metadata) 拷贝）
	got["metadata"].(map[string]any)["k"] = "changed"
	if meta["k"] != "v" {
		t.Error("metadata should be a copy, not the same map")
	}
}

func TestBuildManualApprovalRequiredResponse_带promptPayload(t *testing.T) {
	payload := map[string]any{"prompt": "确认？"}
	got := BuildManualApprovalRequiredResponse("r", nil, payload)
	value := got["value"].(map[string]any)
	if value["prompt"] != "确认？" {
		t.Errorf("prompt_payload should merge into value, got %v", value)
	}
	if _, exists := got["metadata"]; exists {
		t.Error("metadata should be absent")
	}
}

func TestBuildManualApprovalRequiredResponse_空map不合并(t *testing.T) {
	// Python 用 `if metadata:`（空 dict 为假，不合并）
	got := BuildManualApprovalRequiredResponse("r", map[string]any{}, map[string]any{})
	if _, exists := got["metadata"]; exists {
		t.Error("empty metadata should not be attached")
	}
	value := got["value"].(map[string]any)
	if len(value) != 2 {
		t.Errorf("empty payloads should not merge, got %v", value)
	}
}

func TestBuildManualApprovalRequiredResponse_优先级(t *testing.T) {
	// prompt_payload 先合并，metadata 后合并 → metadata 覆盖 payload
	payload := map[string]any{"message": "from-payload"}
	meta := map[string]any{"message": "from-meta"}
	got := BuildManualApprovalRequiredResponse("reason", meta, payload)
	value := got["value"].(map[string]any)
	if value["message"] != "from-meta" {
		t.Errorf("metadata should override prompt_payload, got %v", value["message"])
	}
}

// ──────────────────────────── 用例：classify permission result ───────────────────────────────

func TestClassifyPermissionResult_nil是allow(t *testing.T) {
	if got := ClassifyPermissionResult(nil); got != PermissionResultAllow {
		t.Errorf("got %q, want allow", got)
	}
}

func TestClassifyPermissionResult_approved真值(t *testing.T) {
	got := ClassifyPermissionResult(&security.PermissionConfirmResponse{Approved: true})
	if got != PermissionResultAllow {
		t.Errorf("got %q, want allow", got)
	}
}

func TestClassifyPermissionResult_approved假值(t *testing.T) {
	got := ClassifyPermissionResult(&security.PermissionConfirmResponse{Approved: false})
	if got != PermissionResultDenied {
		t.Errorf("got %q, want denied", got)
	}
}

func TestClassifyPermissionResult_feedback拒绝标记优先(t *testing.T) {
	// Feedback 含 [PERMISSION_REJECTED] 时，即使 Approved=true 也判 user_rejection
	got := ClassifyPermissionResult(&security.PermissionConfirmResponse{
		Approved: true,
		Feedback: "[PERMISSION_REJECTED] 用户取消",
	})
	if got != PermissionResultUserRejection {
		t.Errorf("got %q, want user_rejection（feedback 判定先于 approved）", got)
	}
}

func TestClassifyPermissionResult_feedback拒绝标记(t *testing.T) {
	got := ClassifyPermissionResult(&security.PermissionConfirmResponse{
		Feedback: "[PERMISSION_DENIED] 路径越界",
	})
	if got != PermissionResultDenied {
		t.Errorf("got %q, want denied", got)
	}
}

func TestClassifyPermissionResult_feedback取消标记(t *testing.T) {
	for _, feedback := range []string{"cancelled by user", "request canceled"} {
		got := ClassifyPermissionResult(&security.PermissionConfirmResponse{Feedback: feedback})
		if got != PermissionResultUserRejection {
			t.Errorf("%q -> %q, want user_rejection", feedback, got)
		}
	}
}

func TestClassifyPermissionResult_字符串拒绝标记(t *testing.T) {
	got := ClassifyPermissionResult("[permission_rejected] x")
	if got != PermissionResultUserRejection {
		t.Errorf("got %q", got)
	}
}

func TestClassifyPermissionResult_字符串denied标记(t *testing.T) {
	got := ClassifyPermissionResult("[permission_denied] x")
	if got != PermissionResultDenied {
		t.Errorf("got %q", got)
	}
}

func TestClassifyPermissionResult_字符串cancelled不是驳回(t *testing.T) {
	// Python: 字符串分支在 [permission_rejected] 检查之后直接 return False，
	// 不再看 cancelled 标记。
	got := ClassifyPermissionResult("cancelled")
	if got == PermissionResultUserRejection {
		t.Errorf("bare string cancelled should NOT be user_rejection, got %q", got)
	}
}

func TestClassifyPermissionResult_dict带feedback取消(t *testing.T) {
	got := ClassifyPermissionResult(map[string]any{"feedback": "cancelled"})
	if got != PermissionResultUserRejection {
		t.Errorf("got %q", got)
	}
}

func TestClassifyPermissionResult_dict嵌套value的feedback(t *testing.T) {
	// feedback 缺失时回退到 value.feedback
	got := ClassifyPermissionResult(map[string]any{
		"value": map[string]any{"feedback": "[PERMISSION_REJECTED] x"},
	})
	if got != PermissionResultUserRejection {
		t.Errorf("got %q", got)
	}
}

func TestClassifyPermissionResult_dict带interrupt(t *testing.T) {
	got := ClassifyPermissionResult(map[string]any{"interrupt": true})
	if got != PermissionResultInterrupt {
		t.Errorf("got %q, want interrupt", got)
	}
}

func TestClassifyPermissionResult_dict带id与value(t *testing.T) {
	got := ClassifyPermissionResult(map[string]any{"id": "x", "value": map[string]any{}})
	if got != PermissionResultInterrupt {
		t.Errorf("got %q, want interrupt", got)
	}
}

func TestClassifyPermissionResult_dict带approved键仍是interrupt(t *testing.T) {
	// ⚠️ 上游行为：Python 的 getattr(dict, "approved") 返回 None（dict 无属性），
	// 所以 map 里的 approved 键**不参与** allow/denied 判定，最终落到 interrupt。
	// 计划的测试示例写成了 allow/denied，与上游不符，此处按上游断言。
	for _, v := range []bool{true, false} {
		got := ClassifyPermissionResult(map[string]any{"approved": v})
		if got != PermissionResultInterrupt {
			t.Errorf("map{approved:%v} -> %q, want interrupt（getattr 对 dict 无效）", v, got)
		}
	}
}

func TestClassifyPermissionResult_未知类型是interrupt(t *testing.T) {
	got := ClassifyPermissionResult(42)
	if got != PermissionResultInterrupt {
		t.Errorf("got %q, want interrupt", got)
	}
}

// ──────────────────────────── 用例：is * result ───────────────────────────────

func TestIsAllowResult(t *testing.T) {
	if !IsAllowResult(nil) {
		t.Error("nil should be allow")
	}
	if !IsAllowResult(&security.PermissionConfirmResponse{Approved: true}) {
		t.Error("approved=true should be allow")
	}
	if IsAllowResult(&security.PermissionConfirmResponse{Approved: false}) {
		t.Error("approved=false should not be allow")
	}
}

func TestIsDeniedResult(t *testing.T) {
	if !IsDeniedResult(&security.PermissionConfirmResponse{Approved: false}) {
		t.Error("approved=false should be denied")
	}
	if IsDeniedResult(nil) {
		t.Error("nil should not be denied")
	}
}

func TestIsInterruptResult(t *testing.T) {
	if !IsInterruptResult(map[string]any{"interrupt": true}) {
		t.Error("should be interrupt")
	}
	// ⚠️ 上游行为：classify 的**兜底分支**就是 interrupt，
	// 故 interrupt:false 的 map 仍归为 interrupt（不是 allow）。
	if !IsInterruptResult(map[string]any{"interrupt": false}) {
		t.Error("interrupt:false still falls through to the interrupt default")
	}
	if IsInterruptResult(nil) {
		t.Error("nil should not be interrupt")
	}
}

func TestIsUserRejectionResult(t *testing.T) {
	if !IsUserRejectionResult("[PERMISSION_REJECTED] x") {
		t.Error("should be user_rejection")
	}
	if IsUserRejectionResult(nil) {
		t.Error("nil should not be user_rejection")
	}
}

// ──────────────────────────── 用例：load openjiuwen permission contract ───────────────────────────────

func TestLoadOpenJiuwenPermissionContract_返回契约(t *testing.T) {
	got := LoadOpenJiuwenPermissionContract()
	if got == nil {
		t.Fatal("contract should not be nil")
	}
	// Go 侧 before_tool_call 是普通方法（无 async 概念），退化为常量 true
	if !got.BeforeToolCallIsAsync {
		t.Error("BeforeToolCallIsAsync should be true (Go 编译期已保证方法存在)")
	}
	// PermissionInterruptRail 有 UpdateConfig 方法 → 编译期断言成立
	if !got.SupportsUpdateConfig {
		t.Error("SupportsUpdateConfig should be true")
	}
	if !got.SupportsManualApprovalResponse {
		t.Error("SupportsManualApprovalResponse should be true")
	}
	if !got.SupportsPermissionEngineFactory {
		t.Error("SupportsPermissionEngineFactory should be true")
	}
	if got.PermissionRailClass == nil {
		t.Error("PermissionRailClass should not be nil")
	}
	if got.PermissionConfirmResponseClass == nil {
		t.Error("PermissionConfirmResponseClass should not be nil")
	}
}

func TestLoadOpenJiuwenPermissionContract_类型正确(t *testing.T) {
	got := LoadOpenJiuwenPermissionContract()
	wantRail := reflect.TypeOf(&secrail.PermissionInterruptRail{})
	if got.PermissionRailClass != wantRail {
		t.Errorf("PermissionRailClass = %v, want %v", got.PermissionRailClass, wantRail)
	}
	wantResp := reflect.TypeOf(security.PermissionConfirmResponse{})
	if got.PermissionConfirmResponseClass != wantResp {
		t.Errorf("PermissionConfirmResponseClass = %v, want %v",
			got.PermissionConfirmResponseClass, wantResp)
	}
}

// ──────────────────────────── 私有函数分支 ────────────────────────────

func TestPermissionFeedback_各类型(t *testing.T) {
	if got := permissionFeedback(nil); got != "" {
		t.Errorf("nil -> %q", got)
	}
	if got := permissionFeedback("text"); got != "text" {
		t.Errorf("string -> %q", got)
	}
	if got := permissionFeedback(map[string]any{"feedback": "fb"}); got != "fb" {
		t.Errorf("map feedback -> %q", got)
	}
	if got := permissionFeedback(map[string]any{"feedback": nil}); got != "" {
		t.Errorf("nil feedback -> %q", got)
	}
	if got := permissionFeedback(map[string]any{
		"value": map[string]any{"feedback": "nested"},
	}); got != "nested" {
		t.Errorf("nested -> %q", got)
	}
	if got := permissionFeedback(map[string]any{"other": "x"}); got != "" {
		t.Errorf("no feedback -> %q", got)
	}
	if got := permissionFeedback(&security.PermissionConfirmResponse{Feedback: "ptr"}); got != "ptr" {
		t.Errorf("ptr struct -> %q", got)
	}
	var nilResp *security.PermissionConfirmResponse
	if got := permissionFeedback(nilResp); got != "" {
		t.Errorf("nil ptr struct -> %q", got)
	}
	if got := permissionFeedback(42); got != "" {
		t.Errorf("unknown type -> %q", got)
	}
}

func TestApprovedAttribute_值类型(t *testing.T) {
	// 值类型（非指针）也应有 approved 属性
	approved, ok := approvedAttribute(security.PermissionConfirmResponse{Approved: true})
	if !ok || !approved {
		t.Errorf("value type -> (%v, %v)", approved, ok)
	}
	approved, ok = approvedAttribute(security.PermissionConfirmResponse{Approved: false})
	if !ok || approved {
		t.Errorf("value type false -> (%v, %v)", approved, ok)
	}
	// nil 指针
	var nilResp *security.PermissionConfirmResponse
	if _, ok := approvedAttribute(nilResp); ok {
		t.Error("nil ptr should have no approved attribute")
	}
	// 无该属性的类型
	if _, ok := approvedAttribute(map[string]any{"approved": true}); ok {
		t.Error("map should have no approved attribute")
	}
	if _, ok := approvedAttribute("x"); ok {
		t.Error("string should have no approved attribute")
	}
}

func TestClassifyPermissionResult_值类型结构体(t *testing.T) {
	// 值类型（非指针）也要能分类
	got := ClassifyPermissionResult(security.PermissionConfirmResponse{Approved: true})
	if got != PermissionResultAllow {
		t.Errorf("value struct approved=true -> %q, want allow", got)
	}
	got = ClassifyPermissionResult(security.PermissionConfirmResponse{Approved: false})
	if got != PermissionResultDenied {
		t.Errorf("value struct approved=false -> %q, want denied", got)
	}
}
