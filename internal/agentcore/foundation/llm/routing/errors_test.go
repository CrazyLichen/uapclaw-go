package routing

import (
	"strings"
	"testing"
	"time"

	intellirouter "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter"
	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// fakeState 对齐 LocalRouterState 翻译层触碰的字段（上游 _FakeState）。
// 直接复用真实 LocalRouterState，注入失败走 OnFailure/ResetDeployment。
type fakeRouter struct {
	deployments []*intelliroutercore.Deployment
	groupID     string
	pinnedTag   string
	state       *intelliroutercore.LocalRouterState
}

func newFakeRouter(deps []fakeDep, pinnedTag string) *fakeRouter {
	ResetFailureRecords()
	state := intelliroutercore.NewLocalRouterState()
	r := &fakeRouter{groupID: "g1", pinnedTag: pinnedTag, state: state}
	for _, d := range deps {
		dep := &intelliroutercore.Deployment{
			ID: d.id, ModelName: d.name, Provider: "openai",
			APIBase: d.apiBase, FallbackTag: strPtrOf(tagOf(d)),
		}
		r.deployments = append(r.deployments, dep)
		state.DeploymentStatuses[d.id] = intelliroutercore.DeploymentStatusHealthy
	}
	return r
}

func (r *fakeRouter) fail(id string, cooldown float64, consecutive int) {
	// 镜像 LocalRouterState.on_failure：冷却 + 失败连击
	r.state.OnFailure(id, intellirouter.NewDeploymentAuthError(id, 401), cooldown)
	r.state.ConsecutiveFailures[id] = consecutive
}

func (r *fakeRouter) Deployments() []*intelliroutercore.Deployment { return r.deployments }
func (r *fakeRouter) ModelGroupID() string                         { return r.groupID }
func (r *fakeRouter) PinnedRouteTag() string                       { return r.pinnedTag }
func (r *fakeRouter) State() *intelliroutercore.LocalRouterState   { return r.state }

type fakeDep struct {
	id, name, apiBase, tag string
}

func tagOf(d fakeDep) string {
	if d.tag != "" {
		return d.tag
	}
	return d.name
}

func strPtrOf(s string) *string { return &s }

func translate(t *testing.T, exc error, router *fakeRouter, modelName string, outputEmitted bool) *exception.BaseError {
	t.Helper()
	if modelName == "" {
		modelName = "*"
	}
	return ToModelError(exc, router, modelName, outputEmitted)
}

// badRequest 对齐上游 _bad_request：泛 DeploymentError + 400 + 响应体。
func badRequest(depID string) error {
	body := `{"error":{"message":"maximum context length is 32768 tokens","type":"invalid_request_error"}}`
	return intellirouter.NewDeploymentGenericError(depID, 400, body)
}

func dep(id, name string) fakeDep {
	return fakeDep{id: id, name: name, apiBase: "https://api.a.com/v1"}
}
func depB(id, name string) fakeDep { return fakeDep{id: id, name: name, apiBase: "https://b.com"} }

// ──────────────────────────── switchable ────────────────────────────

// 对齐 TestSwitchable::test_auth_error_with_usable_deployment_asks_for_immediate_retry。
func TestSwitchable_AuthError_ImmediateRetry(t *testing.T) {
	router := newFakeRouter([]fakeDep{dep("dpl_c", "C"), dep("dpl_d", "D")}, "")
	router.fail("dpl_c", 60.0, 1)

	e := translate(t, intellirouter.NewDeploymentAuthError("dpl_c", 401), router, "*", false)
	d := e.Details().(map[string]any)
	if e.Status().Code() != exception.StatusModelCallFailed.Code() {
		t.Fatalf("status 应为 181001，得 %s", e.Status().Name())
	}
	if d["route_case"] != "switchable" || d["reason"] != "deterministic" {
		t.Fatalf("route_case/reason 错误：%v/%v", d["route_case"], d["reason"])
	}
	if d["should_retry"] != true || d["user_action_required"] != false {
		t.Fatalf("should_retry/user_action 错误：%v/%v", d["should_retry"], d["user_action_required"])
	}
	if v, ok := d["retry_after_seconds"].(float64); !ok || v != SwitchRetryDelaySeconds {
		t.Fatalf("retry_after_seconds 错误：%v", d["retry_after_seconds"])
	}
	usable := d["usable"].([]map[string]any)
	if len(usable) != 1 || usable[0]["model_name"] != "D" || usable[0]["endpoint"] != "api.a.com" {
		t.Fatalf("usable 错误：%v", usable)
	}
	if pos, ok := usable[0]["position"].(int); !ok || pos != 2 {
		t.Fatalf("usable position 错误：%v", usable[0]["position"])
	}
	failures := d["failures"].([]map[string]any)
	if pos, ok := failures[0]["position"].(int); !ok || pos != 1 {
		t.Fatalf("failures[0].position 错误：%v", failures[0]["position"])
	}
	want := "[181001] The model 'C' (openai, api.a.com) rejected the API key (HTTP 401). " +
		"Switching to another model in the group."
	if e.Error() != want {
		t.Fatalf("消息漂移：\n得 %s\nwant %s", e.Error(), want)
	}
	assertReadable(t, e.Error())
}

// 对齐 TestSwitchable::test_pinned_model_switches_between_same_name_deployments。
func TestSwitchable_PinnedModel_SwitchesBetweenSameName(t *testing.T) {
	router := newFakeRouter([]fakeDep{dep("dpl_c1", "C"), depB("dpl_c2", "C")}, "C")
	router.fail("dpl_c1", 60.0, 1)

	e := translate(t, intellirouter.NewDeploymentAuthError("dpl_c1", 401), router, "*", false)
	d := e.Details().(map[string]any)
	if d["route_case"] != "switchable" || d["pinned_model"] != "C" {
		t.Fatalf("route_case/pinned_model 错误：%v/%v", d["route_case"], d["pinned_model"])
	}
}

// 对齐 TestSwitchable::test_rejected_request_tries_another_model。
func TestSwitchable_RejectedRequest_TriesAnotherModel(t *testing.T) {
	router := newFakeRouter([]fakeDep{dep("dpl_c", "C"), dep("dpl_d", "D")}, "")
	router.fail("dpl_c", 60.0, 1)

	e := translate(t, badRequest("dpl_c"), router, "*", false)
	d := e.Details().(map[string]any)
	if d["route_case"] != "switchable" || d["reason"] != "request_rejected" {
		t.Fatalf("route_case/reason 错误：%v/%v", d["route_case"], d["reason"])
	}
	if d["should_retry"] != true {
		t.Fatal("should_retry 应为 true")
	}
	if !strings.Contains(e.Error(),
		"rejected the request (HTTP 400): maximum context length is 32768 tokens") {
		t.Fatalf("消息缺 provider 文本：%s", e.Error())
	}
	assertReadable(t, e.Error())
}

// 对齐 TestSwitchable::test_interrupted_answer_with_usable_deployment_reruns_round。
func TestSwitchable_InterruptedAnswer_RerunsRound(t *testing.T) {
	router := newFakeRouter([]fakeDep{dep("dpl_c", "C"), dep("dpl_d", "D")}, "")
	router.fail("dpl_c", 60.0, 1)

	e := translate(t, intellirouter.NewDeploymentNetworkError("dpl_c", "closed"), router, "*", true)
	d := e.Details().(map[string]any)
	if e.Status().Code() != exception.StatusModelCallFailed.Code() {
		t.Fatalf("status 应为 181001，得 %s", e.Status().Name())
	}
	if d["reason"] != "stream_interrupted" || d["should_retry"] != false {
		t.Fatalf("reason/should_retry 错误：%v/%v", d["reason"], d["should_retry"])
	}
	if !strings.Contains(e.Error(), "stopped in the middle of its answer (could not be reached)") ||
		!strings.Contains(e.Error(), "Retrying with another model in the group.") {
		t.Fatalf("消息漂移：%s", e.Error())
	}
	assertReadable(t, e.Error())
}

// ──────────────────────────── exhausted ────────────────────────────

// 对齐 TestExhausted::test_pinned_single_deployment_auth_error_requires_user。
func TestExhausted_PinnedSingleAuthError_RequiresUser(t *testing.T) {
	router := newFakeRouter([]fakeDep{dep("dpl_c", "C")}, "C")
	router.fail("dpl_c", 60.0, 1)

	e := translate(t, intellirouter.NewDeploymentAuthError("dpl_c", 401), router, "*", false)
	d := e.Details().(map[string]any)
	if e.Status().Code() != exception.StatusModelGroupNoAvailableRoute.Code() {
		t.Fatalf("status 应为 181010，得 %s", e.Status().Name())
	}
	if d["route_case"] != "exhausted_user_action" || d["reason"] != "all_deterministic" {
		t.Fatalf("route_case/reason 错误：%v/%v", d["route_case"], d["reason"])
	}
	if d["user_action_required"] != true || d["should_retry"] != false {
		t.Fatalf("user_action/should_retry 错误：%v/%v", d["user_action_required"], d["should_retry"])
	}
	want := "[181010] The model 'C' (openai, api.a.com) rejected the API key (HTTP 401). " +
		"Please check the API key configured for this model. " +
		"This member is set to use 'C' only, so no other model was tried."
	if e.Error() != want {
		t.Fatalf("消息漂移：\n得 %s\nwant %s", e.Error(), want)
	}
	assertReadable(t, e.Error())
}

// 对齐 TestExhausted::test_cooldown_error_reports_the_original_reason。
func TestExhausted_CooldownError_ReportsOriginalReason(t *testing.T) {
	router := newFakeRouter([]fakeDep{dep("dpl_c", "C")}, "C")
	router.fail("dpl_c", 60.0, 1)
	translate(t, intellirouter.NewDeploymentAuthError("dpl_c", 401), router, "*", false)

	e := translate(t, intellirouter.NewNoDeploymentAvailable("*", "No deployment matched routing strategy"),
		router, "*", false)
	d := e.Details().(map[string]any)
	if d["reason"] != "all_deterministic" {
		t.Fatalf("reason 错误：%v", d["reason"])
	}
	if !strings.Contains(e.Error(), "rejected the API key (HTTP 401)") {
		t.Fatalf("应报原始原因：%s", e.Error())
	}
	assertReadable(t, e.Error())
}

// 对齐 TestExhausted::test_all_same_name_deployments_listed。
func TestExhausted_AllSameNameDeployments_Listed(t *testing.T) {
	router := newFakeRouter([]fakeDep{dep("dpl_c1", "C"), depB("dpl_c2", "C")}, "C")
	router.fail("dpl_c1", 60.0, 1)
	translate(t, intellirouter.NewDeploymentAuthError("dpl_c1", 401), router, "*", false)
	router.fail("dpl_c2", 60.0, 1)

	e := translate(t, intellirouter.NewDeploymentAuthError("dpl_c2", 403), router, "*", false)
	text := e.Error()
	if !strings.HasPrefix(text, "[181010] No model 'C' is available: ") {
		t.Fatalf("前缀漂移：%s", text)
	}
	if !strings.Contains(text, "'C' (openai, api.a.com) rejected the API key (HTTP 401)") ||
		!strings.Contains(text, "'C' (openai, b.com) rejected the API key (HTTP 403)") {
		t.Fatalf("应列出全部 deployment：%s", text)
	}
	assertReadable(t, text)
}

// 对齐 TestExhausted::test_mixed_reasons_wait_for_the_transient_deployment。
func TestExhausted_MixedReasons_WaitForTransient(t *testing.T) {
	router := newFakeRouter([]fakeDep{dep("dpl_c1", "C"), dep("dpl_c2", "C")}, "C")
	router.fail("dpl_c1", 60.0, 1)
	translate(t, intellirouter.NewDeploymentAuthError("dpl_c1", 401), router, "*", false)
	router.fail("dpl_c2", 30.0, 1)

	e := translate(t, intellirouter.NewAllDeploymentsFailed("*", []any{
		[]any{"dpl_c2", "Deployment 'dpl_c2' returned HTTP 502 (server error)"},
	}), router, "*", false)
	d := e.Details().(map[string]any)
	if d["route_case"] != "exhausted_retryable" || d["should_retry"] != true {
		t.Fatalf("route_case/should_retry 错误：%v/%v", d["route_case"], d["should_retry"])
	}
	wait, ok := d["retry_after_seconds"].(float64)
	if !ok || wait <= 29.0 || wait > 31.0 {
		t.Fatalf("retry_after_seconds 应约 30，得 %v", d["retry_after_seconds"])
	}
	if failures, ok := d["failures"].([]map[string]any); !ok || len(failures) != 2 {
		t.Fatalf("failures 应为 2 条：%v", d["failures"])
	}
	if !strings.Contains(e.Error(), "Retrying automatically in 30 seconds.") {
		t.Fatalf("消息漂移：%s", e.Error())
	}
}

// 对齐 TestExhausted::test_first_transient_failure_retries_after_cooldown。
func TestExhausted_FirstTransientFailure_RetriesAfterCooldown(t *testing.T) {
	router := newFakeRouter([]fakeDep{dep("dpl_c", "C")}, "C")
	router.fail("dpl_c", 60.0, 1)

	e := translate(t, intellirouter.NewAllDeploymentsFailed("*", []any{
		[]any{"dpl_c", "Deployment 'dpl_c' returned HTTP 502 (server error)"},
	}), router, "*", false)
	d := e.Details().(map[string]any)
	if d["route_case"] != "exhausted_retryable" {
		t.Fatalf("route_case 错误：%v", d["route_case"])
	}
	if v, ok := d["retry_after_seconds"].(float64); !ok || v != 60.0 {
		t.Fatalf("retry_after_seconds 应为 60.0，得 %v", d["retry_after_seconds"])
	}
	if !strings.Contains(e.Error(), "returned a server error (HTTP 502)") {
		t.Fatalf("消息漂移：%s", e.Error())
	}
	assertReadable(t, e.Error())
}

// 对齐 TestExhausted::test_repeated_transient_failure_requires_user。
func TestExhausted_RepeatedTransientFailure_RequiresUser(t *testing.T) {
	router := newFakeRouter([]fakeDep{dep("dpl_c", "C")}, "C")
	router.fail("dpl_c", 120.0, 2)

	e := translate(t, intellirouter.NewAllDeploymentsFailed("*", []any{
		[]any{"dpl_c", "Deployment 'dpl_c' returned HTTP 502 (server error)"},
	}), router, "*", false)
	d := e.Details().(map[string]any)
	if d["reason"] != "transient_repeated" || d["user_action_required"] != true {
		t.Fatalf("reason/user_action 错误：%v/%v", d["reason"], d["user_action_required"])
	}
	if !strings.Contains(e.Error(), "Still unavailable after retrying.") {
		t.Fatalf("消息漂移：%s", e.Error())
	}
}

// 对齐 TestExhausted::test_interrupted_answer_without_alternative_requires_user。
func TestExhausted_InterruptedWithoutAlternative_RequiresUser(t *testing.T) {
	router := newFakeRouter([]fakeDep{dep("dpl_c", "C")}, "C")
	router.fail("dpl_c", 60.0, 1)

	e := translate(t, intellirouter.NewDeploymentNetworkError("dpl_c", "closed"), router, "*", true)
	d := e.Details().(map[string]any)
	if d["route_case"] != "exhausted_user_action" || d["reason"] != "stream_interrupted" {
		t.Fatalf("route_case/reason 错误：%v/%v", d["route_case"], d["reason"])
	}
	if !strings.Contains(e.Error(), "This member is set to use 'C' only") {
		t.Fatalf("消息漂移：%s", e.Error())
	}
	assertReadable(t, e.Error())
}

// 对齐 TestExhausted::test_model_missing_from_group。
func TestExhausted_ModelMissingFromGroup(t *testing.T) {
	router := newFakeRouter([]fakeDep{dep("dpl_a", "A")}, "C")

	e := translate(t, intellirouter.NewNoDeploymentAvailable("*", "No deployment matched routing strategy"),
		router, "*", false)
	d := e.Details().(map[string]any)
	if d["reason"] != "tag_not_found" {
		t.Fatalf("reason 错误：%v", d["reason"])
	}
	want := "[181010] The model 'C' is not in the selected model group. " +
		"Please choose another model or add it back to the model group."
	if e.Error() != want {
		t.Fatalf("消息漂移：\n得 %s\nwant %s", e.Error(), want)
	}
}

// 对齐 TestExhausted::test_unrecorded_cooling_deployment_counts_as_transient。
func TestExhausted_UnrecordedCoolingDeployment_CountsAsTransient(t *testing.T) {
	router := newFakeRouter([]fakeDep{dep("dpl_c", "C")}, "C")
	router.fail("dpl_c", 60.0, 1)

	e := translate(t, intellirouter.NewNoDeploymentAvailable("*", "No deployment matched routing strategy"),
		router, "*", false)
	d := e.Details().(map[string]any)
	if d["route_case"] != "exhausted_retryable" {
		t.Fatalf("route_case 错误：%v", d["route_case"])
	}
	if !strings.Contains(e.Error(), "is temporarily unavailable") {
		t.Fatalf("消息漂移：%s", e.Error())
	}
}

// ──────────────────────────── request_rejected ────────────────────────────

// 对齐 TestRequestRejected::test_every_model_rejecting_the_request_is_not_escalated。
func TestRequestRejected_NotEscalated(t *testing.T) {
	router := newFakeRouter([]fakeDep{dep("dpl_c", "C")}, "")
	router.fail("dpl_c", 60.0, 1)

	e := translate(t, badRequest("dpl_c"), router, "*", false)
	d := e.Details().(map[string]any)
	if d["route_case"] != "exhausted_request_rejected" {
		t.Fatalf("route_case 错误：%v", d["route_case"])
	}
	if d["user_action_required"] != false || d["should_retry"] != false {
		t.Fatalf("user_action/should_retry 错误：%v/%v", d["user_action_required"], d["should_retry"])
	}
	want := "[181010] The model 'C' (openai, api.a.com) rejected the request (HTTP 400): " +
		"maximum context length is 32768 tokens."
	if e.Error() != want {
		t.Fatalf("消息漂移：\n得 %s\nwant %s", e.Error(), want)
	}
}

// 对齐 TestRequestRejected::test_rejecting_models_are_released_for_a_smaller_retry。
func TestRequestRejected_ReleasingModels(t *testing.T) {
	router := newFakeRouter([]fakeDep{dep("dpl_c", "C")}, "")
	router.fail("dpl_c", 60.0, 1)

	translate(t, badRequest("dpl_c"), router, "*", false)

	if router.state.DeploymentStatuses["dpl_c"] != intelliroutercore.DeploymentStatusHealthy {
		t.Fatalf("被拒方冷却应被释放：%v", router.state.DeploymentStatuses["dpl_c"])
	}
}

// 对齐 TestRequestRejected::test_context_overflow_text_stays_detectable。
func TestRequestRejected_ContextOverflowTextStaysDetectable(t *testing.T) {
	router := newFakeRouter([]fakeDep{dep("dpl_c", "C")}, "")
	router.fail("dpl_c", 60.0, 1)

	e := translate(t, badRequest("dpl_c"), router, "*", false)

	// ContextEngine.is_context_overflow_error 的 Go 等价判定（短语匹配）：
	// Task 9 集成测试接入真实 ContextEngine，此处对齐上游的短语可见性断言。
	if !strings.Contains(e.Error(), "maximum context length") {
		t.Fatalf("溢出短语应留在错误文本中：%s", e.Error())
	}
}

// ──────────────────────────── failure order ────────────────────────────

// 对齐 TestFailureOrder::test_triggering_deployment_is_reported_first。
func TestFailureOrder_TriggeringDeploymentReportedFirst(t *testing.T) {
	router := newFakeRouter([]fakeDep{dep("dpl_a", "A"), dep("dpl_c", "C"), dep("dpl_d", "D")}, "")
	router.fail("dpl_a", 60.0, 1)
	router.fail("dpl_c", 60.0, 1)

	e := translate(t, intellirouter.NewDeploymentAuthError("dpl_c", 401), router, "*", false)
	failures := e.Details().(map[string]any)["failures"].([]map[string]any)
	if failures[0]["deployment_id"] != "dpl_c" {
		t.Fatalf("触发方应排最前：%v", failures[0]["deployment_id"])
	}
	if failures[0]["position"] != 2 || failures[1]["position"] != 1 {
		t.Fatalf("position 错误：%v/%v", failures[0]["position"], failures[1]["position"])
	}
}

// ──────────────────────────── 分类/解析单测 ────────────────────────────

// 对齐 _classify 全矩阵（intelli_router_errors.py:152-159）。
func TestClassify_全矩阵(t *testing.T) {
	n := func(v int) *int { return &v }
	cases := []struct {
		errType string
		status  *int
		want    string
	}{
		{"DeploymentServerError", n(502), CategoryTransient},
		{"DeploymentRateLimitError", n(429), CategoryTransient},
		{"DeploymentTimeoutError", nil, CategoryTransient},
		{"DeploymentNetworkError", nil, CategoryTransient},
		{"DeploymentAuthError", n(401), CategoryDeterministic},
		{"DeploymentAuthError", n(403), CategoryDeterministic},
		{"DeploymentError", n(404), CategoryDeterministic},
		{"DeploymentError", n(400), CategoryRequestRejected},
		{"DeploymentError", n(418), CategoryRequestRejected},
		{"DeploymentError", n(429), CategoryTransient},
		{"DeploymentError", nil, CategoryTransient},
	}
	for _, c := range cases {
		if got := classify(c.errType, c.status); got != c.want {
			t.Fatalf("classify(%s,%v)=%s want %s", c.errType, c.status, got, c.want)
		}
	}
}

// 对齐 _describe 八分支文案逐字（intelli_router_errors.py:184-201）。
func TestDescribe_文案逐字(t *testing.T) {
	n := func(v int) *int { return &v }
	cases := []struct {
		errType string
		status  *int
		timeout any
		detail  string
		want    string
	}{
		{"DeploymentTimeoutError", nil, "30", "", "did not respond within 30s"},
		{"DeploymentTimeoutError", nil, nil, "", "did not respond in time"},
		{"DeploymentNetworkError", nil, nil, "", "could not be reached"},
		{"DeploymentRateLimitError", nil, nil, "", "is rate limited (HTTP 429)"},
		{"DeploymentError", n(429), nil, "", "is rate limited (HTTP 429)"},
		{"DeploymentError", n(401), nil, "", "rejected the API key (HTTP 401)"},
		{"DeploymentError", n(404), nil, "", "was not found at its endpoint (HTTP 404)"},
		{"DeploymentError", n(502), nil, "", "returned a server error (HTTP 502)"},
		{"DeploymentError", n(400), nil, "ctx too long", "rejected the request (HTTP 400): ctx too long"},
		{"DeploymentError", n(400), nil, "", "rejected the request (HTTP 400)"},
		{"DeploymentError", nil, nil, "", "failed"},
	}
	for _, c := range cases {
		if got := describe(c.errType, c.status, c.timeout, c.detail); got != c.want {
			t.Fatalf("describe(%s,%v)=%q want %q", c.errType, c.status, got, c.want)
		}
	}
}

// 对齐 _provider_detail（intelli_router_errors.py:162-181）。
func TestProviderDetail_提取与截断(t *testing.T) {
	if got := providerDetail(`{"error":{"message":"  boom   boom "}}`); got != "boom boom" {
		t.Fatalf("JSON 提取失败：%q", got)
	}
	if got := providerDetail(`{"message":"plain"}`); got != "plain" {
		t.Fatalf("message 键提取失败：%q", got)
	}
	long := strings.Repeat("x", 300)
	got := providerDetail(long)
	if len(got) != detailMaxChars+3 || !strings.HasSuffix(got, "...") {
		t.Fatalf("截断失败：%d %q", len(got), got)
	}
	if got := providerDetail(nil); got != "" {
		t.Fatalf("nil 应为空串：%q", got)
	}
}

// 对齐 _parse_error_text 六模式（intelli_router_errors.py:204-219）。
func TestParseErrorText_六模式(t *testing.T) {
	ty, sc, _ := parseErrorText("Deployment 'x' returned HTTP 401 (auth failed)")
	if ty != "DeploymentAuthError" || sc == nil || *sc != 401 {
		t.Fatalf("auth failed 解析错误：%s %v", ty, sc)
	}
	ty, sc, _ = parseErrorText("returned HTTP 502 (server error)")
	if ty != "DeploymentServerError" || *sc != 502 {
		t.Fatalf("server error 解析错误：%s %v", ty, sc)
	}
	ty, sc, _ = parseErrorText("Deployment 'x' rate limited")
	if ty != "DeploymentRateLimitError" || *sc != 429 {
		t.Fatalf("rate limited 解析错误：%s %v", ty, sc)
	}
	ty, sc, timeout := parseErrorText("timed out after 3.5s")
	if ty != "DeploymentTimeoutError" || sc != nil || timeout != "3.5" {
		t.Fatalf("timeout 解析错误：%s %v %v", ty, sc, timeout)
	}
	ty, _, _ = parseErrorText("network error: closed")
	if ty != "DeploymentNetworkError" {
		t.Fatalf("network error 解析错误：%s", ty)
	}
	ty, sc, _ = parseErrorText("returned HTTP 418")
	if ty != "DeploymentError" || *sc != 418 {
		t.Fatalf("默认解析错误：%s %v", ty, sc)
	}
}

// 对齐 record_routing_event 事件同步（intelli_router_errors.py:244-267）。
func TestRecordRoutingEvent_事件同步(t *testing.T) {
	ResetFailureRecords()
	t.Cleanup(ResetFailureRecords)

	// 失败事件存记录
	RecordRoutingEvent(&fakeEvent{
		EventType: "request_retried", DeploymentID: "dpl_c",
		ErrorType:    "DeploymentServerError",
		ErrorMessage: "Deployment 'dpl_c' returned HTTP 503 (server error)",
		Extra:        map[string]any{"model_group_id": "g1", "route_id": "dpl_c"},
	})
	if f := loadFailure("g1", "dpl_c"); f == nil || f.StatusCode == nil || *f.StatusCode != 503 {
		t.Fatalf("失败事件未记录：%v", f)
	}
	// 成功事件清记录
	RecordRoutingEvent(&fakeEvent{
		EventType: "request_succeeded", DeploymentID: "dpl_c",
		Extra: map[string]any{"model_group_id": "g1", "route_id": "dpl_c"},
	})
	if f := loadFailure("g1", "dpl_c"); f != nil {
		t.Fatalf("成功事件应清记录：%v", f)
	}
	// deployment_id 缺失走 extra.route_id
	RecordRoutingEvent(&fakeEvent{
		EventType: "request_retried", ErrorMessage: "returned HTTP 429",
		Extra: map[string]any{"model_group_id": "g1", "route_id": "dpl_x"},
	})
	if f := loadFailure("g1", "dpl_x"); f == nil {
		t.Fatal("route_id 兜底失败")
	}
}

type fakeEvent struct {
	EventType    string
	DeploymentID string
	ErrorType    string
	ErrorMessage string
	Extra        map[string]any
}

// 对齐 _record_exception 的 AllDeploymentsFailed 路径（:296-307）。
func TestRecordException_AllDeploymentsFailed(t *testing.T) {
	ResetFailureRecords()
	t.Cleanup(ResetFailureRecords)

	exc := intellirouter.NewAllDeploymentsFailed("*", []any{
		[]any{"dpl_a", "returned HTTP 502 (server error)"},
		[]any{"dpl_b", "rate limited"},
	})
	lastID, last := recordException("g1", exc, false)
	if lastID != "dpl_b" || last == nil {
		t.Fatalf("last 错误：%s %v", lastID, last)
	}
	if f := loadFailure("g1", "dpl_a"); f == nil || f.Category != CategoryTransient {
		t.Fatalf("dpl_a 记录错误：%v", f)
	}
	if f := loadFailure("g1", "dpl_b"); f == nil || f.StatusCode == nil || *f.StatusCode != 429 {
		t.Fatalf("dpl_b 记录错误：%v", f)
	}
}

// assertReadable 内部标识与术语绝不出现在用户可见文本
// （上游 _FORBIDDEN_TEXT + _assert_readable）。
func assertReadable(t *testing.T, text string) {
	t.Helper()
	for _, forbidden := range []string{
		"'*'", "dpl_", "deployment", "Deployment", "route", "pinned", "cooldown",
		"LLM stream timeout", "stream frame timeout",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("%q 泄漏进 %q", forbidden, text)
		}
	}
}

// 防止 time 导入被移除（RouteFailure.Timestamp 使用）。
var _ = time.Now
