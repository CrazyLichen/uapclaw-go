package recommendation

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeNativeRunner 可注入的假搜索执行器。
func fakeNativeRunner(engine string, rows []map[string]string, err error) SearchRunner {
	return func(string, int, int) (string, []map[string]string, error) {
		return engine, rows, err
	}
}

// TestOpenJiuwenNativeSearch_契约拒绝 请求契约硬校验（对齐 :203-215）。
func TestOpenJiuwenNativeSearch_契约拒绝(t *testing.T) {
	s := NewOpenJiuwenNativeSearch(fakeNativeRunner("bing", nil, nil), func() string { return "req-1" })
	cases := []struct {
		query      string
		maxResults int
		timeout    int
	}{
		{"", 5, 5}, {"   ", 5, 5}, {"q", 0, 5}, {"q", 21, 5}, {"q", 5, 0},
	}
	for _, c := range cases {
		_, err := s.Search(context.Background(), c.query, c.maxResults, c.timeout)
		var contract *OpenJiuwenNativeSearchContractError
		if !errors.As(err, &contract) || contract.Reason != "search_request_contract_failed" ||
			contract.HostRequestID != "req-1" {
			t.Fatalf("case %+v: err=%v", c, err)
		}
	}
}

// TestOpenJiuwenNativeSearch_成功 成功路径：白名单引擎、小票字段、调用小票追加
// （对齐 :216-289）。
func TestOpenJiuwenNativeSearch_成功(t *testing.T) {
	rows := []map[string]string{
		{"url": "https://a.example.com/1", "title": "A"},
		{"url": "https://b.example.com/2", "title": "B"},
	}
	s := NewOpenJiuwenNativeSearch(fakeNativeRunner("bing", rows, nil), func() string { return "req-2" })
	result, err := s.Search(context.Background(), "query", 5, 5)
	if err != nil {
		t.Fatal(err)
	}
	if result.Engine != "bing" || len(result.Rows) != 2 {
		t.Fatalf("result=%+v", result)
	}
	receipt := result.Receipt
	for key, want := range map[string]any{
		"receipt_version": OpenjiuwenNativeSearchReceiptVersion,
		"provider_id":     OpenjiuwenNativeSearchProviderId,
		"host_request_id": "req-2",
		"requested_query": "query",
		"engine":          "bing",
		"result_count":    2,
	} {
		got, _ := receipt[key]
		if got != want {
			t.Fatalf("receipt[%q]=%v want %v", key, got, want)
		}
	}
	if len(s.invocationReceipts) != 1 {
		t.Fatalf("invocationReceipts=%d", len(s.invocationReceipts))
	}
}

// TestOpenJiuwenNativeSearch_厂商失败 失败小票分类（对齐 :96-147）。
func TestOpenJiuwenNativeSearch_厂商失败(t *testing.T) {
	cases := []struct {
		err  error
		kind string
		reason string
	}{
		{&nativeProviderAPIError{Code: openjiuwenWebSearchAllEnginesFailedCode}, "all_engines_failed", "provider_all_engines_failed"},
		{&nativeProviderAPIError{Code: openjiuwenWebSearchEngineErrorCode,
			Params: map[string]any{"engine": "duckduckgo", "reason": "anti-bot challenge page returned"}},
			"challenge", "provider_challenge"},
		{context.DeadlineExceeded, "timeout", "provider_timeout"},
		{&providerStatusError{StatusCode: 503}, "http_status", "provider_http_status"},
		{errors.New("dial tcp: connection refused"), "transport", "provider_transport"},
	}
	for _, c := range cases {
		s := NewOpenJiuwenNativeSearch(fakeNativeRunner("", nil, c.err), func() string { return "req-3" })
		_, err := s.Search(context.Background(), "q", 5, 5)
		provider, ok := err.(*OpenJiuwenNativeSearchProviderError)
		if !ok {
			t.Fatalf("kind=%s: err=%v", c.kind, err)
		}
		if provider.Error() != c.reason || provider.HostRequestID() != "req-3" {
			t.Fatalf("kind=%s: reason=%q", c.kind, provider.Error())
		}
		if provider.Receipt["failure_kind"] != c.kind {
			t.Fatalf("kind=%s: receipt=%v", c.kind, provider.Receipt)
		}
	}
}

// TestOpenJiuwenNativeSearch_结果契约 结果契约拒绝（对齐 :241-289）。
func TestOpenJiuwenNativeSearch_结果契约(t *testing.T) {
	dup := []map[string]string{
		{"url": "https://a.example.com/1"}, {"url": "https://a.example.com/1"},
	}
	cases := []struct {
		engine string
		rows   []map[string]string
	}{
		{"unknown-engine", []map[string]string{{"url": "https://a.example.com/1"}}},
		{"/x", []map[string]string{{"url": "ftp://a.example.com/1"}}},
		{"", dup},
		{"", []map[string]string{}},
	}
	for _, c := range cases {
		s := NewOpenJiuwenNativeSearch(fakeNativeRunner(c.engine, c.rows, nil), func() string { return "req-4" })
		_, err := s.Search(context.Background(), "q", 5, 5)
		var contract *OpenJiuwenNativeSearchContractError
		if !errors.As(err, &contract) || contract.Reason != "search_result_contract_failed" {
			t.Fatalf("case %+v: err=%v", c, err)
		}
	}
}

// fakeResponsesClient 可注入的假 Responses 客户端。
type fakeResponsesClient struct {
	payload  map[string]any
	statusErr error
}

func (f *fakeResponsesClient) ResponsesCreate(context.Context, map[string]any) (map[string]any, error) {
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	return f.payload, nil
}

func (f *fakeResponsesClient) Close() error { return nil }

// arkSuccessPayload 构造合法 Ark web search 响应（对齐 :519-600 合法形态）。
func arkSuccessPayload() map[string]any {
	return map[string]any{
		"id": "resp-1", "object": "response", "status": "completed", "model": "m-1",
		"usage": map[string]any{
			"input_tokens": 10, "output_tokens": 5, "total_tokens": 15,
			"tool_usage": map[string]any{"web_search": 1},
		},
		"output": []any{
			map[string]any{"type": "web_search_call", "id": "call-1", "status": "completed",
				"action": map[string]any{"type": "search", "query": "query"}},
			map[string]any{"type": "message", "status": "completed",
				"content": []any{map[string]any{"annotations": []any{
					map[string]any{"type": "url_citation", "url": "https://a.example.com/1", "title": "A"},
					map[string]any{"type": "url_citation", "url": "https://b.example.com/2"},
				}}},
			},
		},
	}
}

// TestArkWebSearch_成功 合法响应解析与小票字段（对齐 :519-600）。
func TestArkWebSearch_成功(t *testing.T) {
	config := &ArkBackgroundConfig{Model: "m-1"}
	s, err := NewArkResponsesWebSearch(config, func(*ArkBackgroundConfig) (ResponsesClient, error) {
		return &fakeResponsesClient{payload: arkSuccessPayload()}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Search(context.Background(), "query", 10, 5)
	if err != nil {
		t.Fatal(err)
	}
	if result.Engine != ArkWebSearchProviderID || len(result.Rows) != 2 {
		t.Fatalf("result=%+v", result)
	}
	receipt := result.Receipt
	if receipt["receipt_version"] != ArkWebSearchReceiptVersion ||
		receipt["web_search_call_id"] != "call-1" || receipt["result_count"] != 2 {
		t.Fatalf("receipt=%v", receipt)
	}
	if len(s.InvocationReceipts) != 1 {
		t.Fatalf("InvocationReceipts=%d", len(s.InvocationReceipts))
	}
}

// TestArkWebSearch_契约拒绝 响应解析逐类拒绝（对齐 :519-600 各 reject 分支）。
func TestArkWebSearch_契约拒绝(t *testing.T) {
	cases := map[string]func(map[string]any){
		"response_metadata_mismatch": func(p map[string]any) { p["status"] = "failed" },
		"missing_web_search_call": func(p map[string]any) {
			p["output"] = []any{map[string]any{"type": "message", "status": "completed", "content": []any{}}}
		},
		"multiple_web_search_calls": func(p map[string]any) {
			call := map[string]any{"type": "web_search_call", "id": "c", "status": "completed",
				"action": map[string]any{"type": "search", "query": "query"}}
			p["output"] = []any{call, call}
		},
		"search_query_mismatch": func(p map[string]any) {
			for _, item := range p["output"].([]any) {
				m := item.(map[string]any)
				if m["type"] == "web_search_call" {
					m["action"].(map[string]any)["query"] = "其他"
				}
			}
		},
		"missing_url_citation": func(p map[string]any) {
			for _, item := range p["output"].([]any) {
				m := item.(map[string]any)
				if m["type"] == "message" {
					m["content"] = []any{}
				}
			}
		},
		"response_metadata_mismatch_usage": func(p map[string]any) {
			p["usage"] = map[string]any{"tool_usage": map[string]any{"web_search": 0}}
		},
	}
	for name, mutate := range cases {
		p := arkSuccessPayload()
		mutate(p)
		s, err := NewArkResponsesWebSearch(&ArkBackgroundConfig{Model: "m-1"}, func(*ArkBackgroundConfig) (ResponsesClient, error) {
			return &fakeResponsesClient{payload: p}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		want := strings.TrimSuffix(name, "_usage")
		_, err = s.Search(context.Background(), "query", 10, 5)
		var contract *ArkWebSearchContractError
		if !errors.As(err, &contract) || contract.Reason != want {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
}

// TestArkWebSearch_请求契约 请求参数硬校验（对齐 :533-541）。
func TestArkWebSearch_请求契约(t *testing.T) {
	s, _ := NewArkResponsesWebSearch(&ArkBackgroundConfig{Model: "m-1"}, func(*ArkBackgroundConfig) (ResponsesClient, error) {
		return &fakeResponsesClient{}, nil
	})
	for _, c := range []struct {
		query      string
		maxResults int
		timeout    int
	}{{"", 5, 5}, {"q", 0, 5}, {"q", 51, 5}, {"q", 5, 0}} {
		if _, err := s.Search(context.Background(), c.query, c.maxResults, c.timeout); err == nil ||
			err.Error() != "search_request_contract_failed" {
			t.Fatalf("case %+v: err=%v", c, err)
		}
	}
}

// TestArkWebSearch_权益拒绝 ToolNotOpen → EntitlementError + 小票
// （对齐 :397-419 _tool_not_open_receipt）。
func TestArkWebSearch_权益拒绝(t *testing.T) {
	statusErr := &providerStatusError{
		StatusCode: 403, RequestID: "ark-req-1",
		Body: map[string]any{"error": map[string]any{"code": "ToolNotOpen"}},
	}
	s, _ := NewArkResponsesWebSearch(&ArkBackgroundConfig{Model: "m-1"}, func(*ArkBackgroundConfig) (ResponsesClient, error) {
		return &fakeResponsesClient{statusErr: statusErr}, nil
	})
	_, err := s.Search(context.Background(), "query", 5, 5)
	ent, ok := err.(*ArkWebSearchEntitlementError)
	if !ok || ent.Error() != "ark_web_search_entitlement_blocked" {
		t.Fatalf("err=%v", err)
	}
	receipt := ent.Receipt
	if receipt["provider_error_code"] != "ToolNotOpen" || receipt["http_status"] != 403 ||
		receipt["provider_request_id"] != "ark-req-1" || receipt["actual_model"] != "m-1" {
		t.Fatalf("receipt=%v", receipt)
	}
}

// TestArkWebSearch_非ToolNotOpen透传 非 ToolNotOpen 厂商错误必须透传
// （对齐 :556-563 if receipt is None: raise）。
func TestArkWebSearch_非ToolNotOpen透传(t *testing.T) {
	statusErr := &providerStatusError{StatusCode: 500, Body: map[string]any{"error": map[string]any{"code": "Internal"}}}
	s, _ := NewArkResponsesWebSearch(&ArkBackgroundConfig{Model: "m-1"}, func(*ArkBackgroundConfig) (ResponsesClient, error) {
		return &fakeResponsesClient{statusErr: statusErr}, nil
	})
	_, err := s.Search(context.Background(), "query", 5, 5)
	if _, isEnt := err.(*ArkWebSearchEntitlementError); isEnt {
		t.Fatalf("非 ToolNotOpen 不得包装成权益错误: %v", err)
	}
}
