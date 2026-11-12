package recommendation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// fakeChatClient 可编程响应/错误的客户端替身（对齐 Python 测试的 client_factory 注入点）。
type fakeChatClient struct {
	resp    map[string]any
	err     error
	request map[string]any
	closed  int
}

func (f *fakeChatClient) CreateChatCompletion(_ context.Context, request map[string]any) (map[string]any, error) {
	f.request = request
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func (f *fakeChatClient) Close() error { f.closed++; return nil }

// fakeChatResponse 构造 OpenAI 兼容响应体。
func fakeChatResponse(content string, model string) map[string]any {
	return map[string]any{
		"id":     "resp-1",
		"object": "chat.completion",
		"model":  model,
		"usage":  map[string]any{"total_tokens": 10},
		"choices": []any{map[string]any{
			"finish_reason": "stop",
			"message":       map[string]any{"content": content},
		}},
	}
}

func newTestInvoker(t *testing.T, client *fakeChatClient, opts ...func(*ArkStageInvoker)) *ArkStageInvoker {
	t.Helper()
	inv := BuildArkStageInvoker(
		&ArkBackgroundConfig{APIKey: "k", Model: "m", BaseURL: "http://ark", Temperature: 0, TopP: 1},
		func(string, string) (ChatClient, error) { return client, nil },
		nil, false, nil, nil, nil, nil, false,
	)
	for _, opt := range opts {
		opt(inv)
	}
	return inv
}

func TestBuildArkStageInvoker_thinkingExtraBody固定注入(t *testing.T) {
	client := &fakeChatClient{resp: fakeChatResponse(`{"ok":1}`, "m")}
	inv := newTestInvoker(t, client)
	result, err := inv.Invoke(t.Context(), "stage_a", "sys", `{"request_id":"r1"}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	extra := client.request["extra_body"].(map[string]any)
	if extra["enable_thinking"] != false {
		t.Fatal("extra_body 必须关 thinking（对齐 :247）")
	}
	if m, ok := extra["thinking"].(map[string]any); !ok || m["type"] != "disabled" {
		t.Fatal("thinking.type 必须为 disabled（对齐 :20-23）")
	}
	if _, ok := client.request["reasoning_effort"]; ok {
		t.Fatal("reasoning_effort 不允许进请求（对齐 :19 注释）")
	}
	if result == nil {
		t.Fatal("结果为空")
	}
}

func TestBuildArkStageInvoker_jsonSchema响应格式(t *testing.T) {
	client := &fakeChatClient{resp: fakeChatResponse(`{"label":"MATCH"}`, "m")}
	inv := newTestInvoker(t, client, func(inv *ArkStageInvoker) {
		inv.responseSchemaForStage = func(stage, payload string) map[string]any {
			return map[string]any{"type": "object", "properties": map[string]any{
				"label": map[string]any{"type": "string"},
			}}
		}
	})
	if _, err := inv.Invoke(t.Context(), "rid-pair-match", "sys", "{}"); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	rf := client.request["response_format"].(map[string]any)
	if rf["type"] != "json_schema" {
		t.Fatalf("schema 存在时必须用 json_schema，got %v", rf["type"])
	}
	js := rf["json_schema"].(map[string]any)
	if js["name"] != "rid_pair_match" || js["strict"] != true {
		t.Fatalf("name/strict 不符: %v", js)
	}
}

func TestBuildArkStageInvoker_状态机小票流转(t *testing.T) {
	client := &fakeChatClient{resp: fakeChatResponse(`{"a":1}`, "m")}
	var statuses []string
	inv := newTestInvoker(t, client, func(inv *ArkStageInvoker) {
		inv.receiptSink = func(record map[string]any) {
			statuses = append(statuses, record["status"].(string))
		}
	})
	if _, err := inv.Invoke(t.Context(), "s", "sys", "{}"); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	// 对齐 :288(started) → :308(received) → :351(parsed)
	want := []string{"started", "received", "parsed"}
	if len(statuses) < 3 || statuses[0] != want[0] || statuses[1] != want[1] || statuses[len(statuses)-1] != "parsed" {
		t.Fatalf("状态机流转不符: %v", statuses)
	}
	if inv.InvocationReceipts()[0]["structured_output_mode"] != "json_object" {
		t.Fatal("无 schema 时输出模式必须为 json_object")
	}
}

func TestBuildArkStageInvoker_请求失败归因(t *testing.T) {
	client := &fakeChatClient{err: &ProviderFailureError{Msg: "boom", HTTPStatus: 429}}
	var sinkRecords []map[string]any
	inv := newTestInvoker(t, client, func(inv *ArkStageInvoker) {
		inv.receiptSink = func(record map[string]any) { sinkRecords = append(sinkRecords, record) }
	})
	if _, err := inv.Invoke(t.Context(), "s", "sys", "{}"); err == nil {
		t.Fatal("必须报错")
	}
	last := sinkRecords[len(sinkRecords)-1]
	if last["status"] != "request_failed" || last["failure_reason"] != "rate_limited" {
		t.Fatalf("失败归因不符: status=%v reason=%v", last["status"], last["failure_reason"])
	}
}

func TestBuildArkStageInvoker_响应元数据契约(t *testing.T) {
	client := &fakeChatClient{resp: fakeChatResponse(`{"a":1}`, "wrong-model")}
	inv := newTestInvoker(t, client, func(inv *ArkStageInvoker) {
		inv.requireResponseMetadata = true
	})
	_, err := inv.Invoke(t.Context(), "s", "sys", "{}")
	if err == nil || err.Error() != "Ark response metadata contract failed" {
		t.Fatalf("actual_model 不符必须报元数据契约错（对齐 :314-327）: %v", err)
	}
}

func TestBuildArkStageInvoker_未消费回执上限64(t *testing.T) {
	client := &fakeChatClient{resp: fakeChatResponse(`{"a":1}`, "m")}
	sunk := 0
	inv := newTestInvoker(t, client, func(inv *ArkStageInvoker) {
		inv.receiptSink = func(map[string]any) { sunk++ }
	})
	for i := 0; i < _MAX_PENDING_VALIDATION_RECEIPTS+10; i++ {
		if _, err := inv.Invoke(t.Context(), "s", "sys", "{}"); err != nil {
			t.Fatalf("invoke %d: %v", i, err)
		}
	}
	if len(inv.validationReceipts) != _MAX_PENDING_VALIDATION_RECEIPTS {
		t.Fatalf("未消费回执必须封顶 64（对齐 :357-358），got %d", len(inv.validationReceipts))
	}
}

func TestArkStageInvoker_RecordValidation消费(t *testing.T) {
	client := &fakeChatClient{resp: fakeChatResponse(`{"a":1}`, "m")}
	var outcomes []string
	inv := newTestInvoker(t, client, func(inv *ArkStageInvoker) {
		inv.receiptSink = func(record map[string]any) {
			if s, ok := record["status"].(string); ok && (s == "business_validated" || s == "business_validation_failed") {
				outcomes = append(outcomes, s)
			}
		}
	})
	result, err := inv.Invoke(t.Context(), "s", "sys", "{}")
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	inv.RecordValidation(result, "business_validated")
	if len(outcomes) != 1 || outcomes[0] != "business_validated" {
		t.Fatalf("消费回执未落库: %v", outcomes)
	}
	inv.RecordValidation(result, "business_validated") // 已弹出：幂等
	if len(outcomes) != 1 {
		t.Fatal("重复登记不允许二次落库")
	}
}

func TestArkStageInvoker_Aclose清理(t *testing.T) {
	client := &fakeChatClient{resp: fakeChatResponse(`{"a":1}`, "m")}
	inv := newTestInvoker(t, client)
	if _, err := inv.Invoke(t.Context(), "s", "sys", "{}"); err != nil {
		t.Fatal(err)
	}
	if err := inv.Aclose(); err != nil {
		t.Fatal(err)
	}
	if len(inv.validationReceipts) != 0 {
		t.Fatal("aclose 必须清空未消费回执（对齐 :384）")
	}
	if client.closed != 1 {
		t.Fatal("aclose 必须关闭客户端")
	}
}

func TestProviderFailureReason_归因矩阵(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&ProviderFailureError{CodeStr: "AccountOverdueError"}, "account_overdue"},
		{&ProviderFailureError{CodeStr: "InvalidEndpoint.ClosedEndpoint"}, "model_unavailable"},
		{&ProviderFailureError{HTTPStatus: 400, ParamName: "response_format"}, "model_unavailable"},
		{&ProviderFailureError{HTTPStatus: 401}, "invalid_key"},
		{&ProviderFailureError{HTTPStatus: 403}, "access_denied"},
		{&ProviderFailureError{HTTPStatus: 404}, "model_unavailable"},
		{&ProviderFailureError{HTTPStatus: 429}, "rate_limited"},
		{errors.New("plain"), "provider_request_failed"},
	}
	for _, c := range cases {
		if got := ProviderFailureReason(c.err); got != c.want {
			t.Fatalf("ProviderFailureReason(%v)=%q want %q", c.err, got, c.want)
		}
	}
}

func TestFromEnviron_配置解析(t *testing.T) {
	if FromEnviron(map[string]string{"ARK_API_KEY": "k"}) != nil {
		t.Fatal("缺 BASE_URL 不臆造端点（对齐 :60-64）")
	}
	cfg := FromEnviron(map[string]string{
		"ARK_API_KEY": " k ", "ARK_MODEL": "m", "ARK_BASE_URL": "u",
		"ARK_CONTEXT_WINDOW_TOKENS": "128k", "ARK_TEMPERATURE": "0.2",
	})
	if cfg != nil {
		t.Fatal("context_window 非整数必须返回 nil（对齐 :74-79）")
	}
	cfg = FromEnviron(map[string]string{
		"ARK_API_KEY": "k", "ARK_MODEL": "m", "ARK_BASE_URL": "u",
		"ARK_CONTEXT_WINDOW_TOKENS": "128", "ARK_TEMPERATURE": "0.2", "ARK_TOP_P": "0.9",
	})
	if cfg == nil || cfg.Temperature != 0.2 || cfg.TopP != 0.9 || cfg.ContextWindowTokens != 128 {
		t.Fatalf("解析不符: %+v", cfg)
	}
}

func TestFromEnviron_SafeProvenance不泄密(t *testing.T) {
	cfg := FromEnviron(map[string]string{
		"ARK_API_KEY": "secret", "ARK_MODEL": "m", "ARK_BASE_URL": "u",
		"ARK_CONTEXT_WINDOW_TOKENS": "128", "ARK_EXPECTED_MODEL": "em",
	})
	raw, _ := json.Marshal(cfg.SafeProvenance())
	if string(raw) == "" || len(raw) == 0 {
		t.Fatal("provenance 空")
	}
	if cfg.SafeProvenance()["expected_model"] != "em" {
		t.Fatal("expected_model 必须进 provenance（对齐 :113-114）")
	}
}

func TestBackgroundConfigurationStatus_状态机(t *testing.T) {
	full := map[string]string{
		"ARK_API_KEY": "k", "ARK_MODEL": "m", "ARK_BASE_URL": "u",
		"ARK_CONTEXT_WINDOW_TOKENS": "128",
	}
	// 对齐 :118-162：未设 FREE_SEARCH_* 开关 → native_search_unconfigured（搜索通道未配置）
	st := BackgroundConfigurationStatus(FromEnviron(full), full)
	if st["configured"] != true || st["status"] != "native_search_unconfigured" {
		t.Fatalf("全配置但无搜索开关应 native_search_unconfigured: %v", st["status"])
	}
	withSearch := map[string]string{
		"ARK_API_KEY": "k", "ARK_MODEL": "m", "ARK_BASE_URL": "u",
		"ARK_CONTEXT_WINDOW_TOKENS": "128",
		"FREE_SEARCH_DDG_ENABLED":   "true", "FREE_SEARCH_BING_ENABLED": "true",
	}
	st = BackgroundConfigurationStatus(FromEnviron(withSearch), withSearch)
	if st["status"] != "ready" {
		t.Fatalf("全配置+搜索开关应 ready: %v", st["status"])
	}
	st = BackgroundConfigurationStatus(nil, map[string]string{})
	if st["status"] != "missing_configuration" {
		t.Fatalf("缺配置应 missing_configuration: %v", st["status"])
	}
}
