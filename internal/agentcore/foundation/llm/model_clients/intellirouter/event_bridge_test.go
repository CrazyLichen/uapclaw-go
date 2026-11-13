package intellirouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/observability"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/routing"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// 对齐上游 TestRoutingEvents::test_bridge_records_reason_of_router_internal_failover
// （test_intelli_router_errors.py 末段；路由器内部换路的失败原因经事件桥记录）。
func TestEventBridge_RecordsFailoverReason(t *testing.T) {
	routing.ResetFailureRecords()
	t.Cleanup(func() { routing.ResetFailureRecords() })

	event := observability.NewRoutingEvent(observability.EventTypeRequestRetried, "req1", "*")
	depID := "dpl_c"
	event.DeploymentID = &depID
	errorType := "DeploymentServerError"
	event.ErrorType = &errorType
	errorMessage := "Deployment 'dpl_c' returned HTTP 503 (server error)"
	event.ErrorMessage = &errorMessage
	event.Extra = map[string]any{"model_group_id": "g1", "route_id": "dpl_c"}

	bridge := &CoreRoutingEventBridge{}
	bridge.HandleEvent(event)

	f := routing.RoutingLoadFailure("g1", "dpl_c")
	if f == nil || f.StatusCode == nil || *f.StatusCode != 503 {
		t.Fatalf("失败原因未记录：%v", f)
	}
	if f.Category != routing.CategoryTransient {
		t.Fatalf("应为 transient，得 %s", f.Category)
	}
}

// 对齐上游 TestRoutingEvents::test_success_event_clears_the_old_reason。
func TestEventBridge_SuccessEventClearsOldReason(t *testing.T) {
	routing.ResetFailureRecords()
	t.Cleanup(func() { routing.ResetFailureRecords() })

	depID := "dpl_c"
	errorMessage := "Deployment 'dpl_c' returned HTTP 401 (auth failed)"

	failEvent := observability.NewRoutingEvent(observability.EventTypeRequestRetried, "req1", "*")
	failEvent.DeploymentID = &depID
	failEvent.ErrorMessage = &errorMessage
	failEvent.Extra = map[string]any{"model_group_id": "g1", "route_id": "dpl_c"}
	(&CoreRoutingEventBridge{}).HandleEvent(failEvent)
	if routing.RoutingLoadFailure("g1", "dpl_c") == nil {
		t.Fatal("失败应先记录")
	}

	successEvent := observability.NewRoutingEvent(observability.EventTypeRequestSucceeded, "req2", "*")
	successEvent.DeploymentID = &depID
	successEvent.Extra = map[string]any{"model_group_id": "g1", "route_id": "dpl_c"}
	(&CoreRoutingEventBridge{}).HandleEvent(successEvent)
	if routing.RoutingLoadFailure("g1", "dpl_c") != nil {
		t.Fatal("成功事件应清记录")
	}
}

// 对齐 _payload_from_event（Python: :406-432）。
func TestEventBridge_PayloadFromEvent(t *testing.T) {
	event := observability.NewRoutingEvent(observability.EventTypeRequestSucceeded, "req1", "*")
	event.Extra = map[string]any{
		"model_group_id": "g1", "route_id": "r1", "routed_model": "fast",
	}
	payload := payloadFromEvent(event)
	if payload == nil || payload["status"] != "selected" || payload["result_type"] != "answer" {
		t.Fatalf("selected 载荷错误：%v", payload)
	}

	exhausted := observability.NewRoutingEvent(observability.EventTypeAllDeploymentsExhausted, "req2", "*")
	exhausted.Extra = map[string]any{"model_group_id": "g1"}
	payload = payloadFromEvent(exhausted)
	if payload["status"] != "failed" || payload["result_type"] != "error" {
		t.Fatalf("failed 载荷错误：%v", payload)
	}

	// extra 无 model_group_id → nil（Python: :417-419）
	noGroup := observability.NewRoutingEvent(observability.EventTypeRequestSucceeded, "req3", "*")
	if payloadFromEvent(noGroup) != nil {
		t.Fatal("无 model_group_id 应返回 nil")
	}
	// 非选定/失败事件 → nil
	ignored := observability.NewRoutingEvent(observability.EventTypeStreamRouteSelected, "req4", "*")
	_ = ignored
	if payloadFromEvent(event) == nil {
		t.Fatal("回读失败")
	}
}

// ──────────────────────────── DashScope 生成三件套 ────────────────────────────

func dashscopeTestClient(t *testing.T, srvURL string) *IntelliRouterModelClient {
	t.Helper()
	return newTestClientWithDeployments(t, []map[string]any{{
		"route_id": "r1", "model_name": "m", "provider": "dashscope",
		"api_key": "k", "api_base": srvURL,
	}})
}

// 对齐 _generate_image_dashscope 成功路径（Python: :1182-1206）。
func TestGenerateImageDashscope_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status_code":200,"output":{"choices":[{"message":{"content":[{"image":"https://img/x.png"}]}}]}}`))
	}))
	defer srv.Close()

	client := dashscopeTestClient(t, srv.URL)
	resp, err := client.GenerateImage(context.Background(),
		[]*llmschema.UserMessage{llmschema.NewUserMessage("a cat")})
	if err != nil {
		t.Fatalf("generate image: %v", err)
	}
	if len(resp.Images) != 1 || resp.Images[0] != "https://img/x.png" {
		t.Fatalf("图片 URL 错误：%v", resp.Images)
	}
	if resp.Model != "qwen-image-max" {
		t.Fatalf("缺省模型应为 qwen-image-max，得 %s", resp.Model)
	}
}

// 对齐 :1128-1131 单消息校验 + :1172-1176 空内容校验。
func TestGenerateImageDashscope_校验(t *testing.T) {
	client := dashscopeTestClient(t, "http://127.0.0.1:1")
	if _, err := client.GenerateImage(context.Background(), nil); err == nil {
		t.Fatal("零消息应报错")
	}
	empty := llmschema.NewUserMessage("")
	if _, err := client.GenerateImage(context.Background(), []*llmschema.UserMessage{empty}); err == nil {
		t.Fatal("空内容应报错")
	}
}

// 对齐 _generate_speech_dashscope 成功路径（Python: :1276-1303）。
func TestGenerateSpeechDashscope_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status_code":200,"output":{"audio":{"url":"https://audio/x.wav","data":""}}}`))
	}))
	defer srv.Close()

	client := dashscopeTestClient(t, srv.URL)
	resp, err := client.GenerateSpeech(context.Background(),
		[]*llmschema.UserMessage{llmschema.NewUserMessage("hello")})
	if err != nil {
		t.Fatalf("generate speech: %v", err)
	}
	if resp.AudioURL == nil || *resp.AudioURL != "https://audio/x.wav" {
		t.Fatalf("音频 URL 错误：%v", resp.AudioURL)
	}
	if resp.Format != "wav" {
		t.Fatalf("格式应为 wav，得 %s", resp.Format)
	}
}

// 对齐 :1297-1299 无音频报错。
func TestGenerateSpeechDashscope_NoAudio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status_code":200,"output":{}}`))
	}))
	defer srv.Close()

	client := dashscopeTestClient(t, srv.URL)
	_, err := client.GenerateSpeech(context.Background(),
		[]*llmschema.UserMessage{llmschema.NewUserMessage("hello")})
	if err == nil || !containsStr(err.Error(), "No audio returned from DashScope API.") {
		t.Fatalf("应报无音频：%v", err)
	}
}

// 对齐 _generate_video_dashscope 成功路径（Python: :1396-1415）。
func TestGenerateVideoDashscope_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status_code":200,"output":{"video_url":"https://video/x.mp4"},"usage":{"size":"1920x1080","duration":5}}`))
	}))
	defer srv.Close()

	client := dashscopeTestClient(t, srv.URL)
	resp, err := client.GenerateVideo(context.Background(),
		[]*llmschema.UserMessage{llmschema.NewUserMessage("a cat")})
	if err != nil {
		t.Fatalf("generate video: %v", err)
	}
	if resp.VideoURL == nil || *resp.VideoURL != "https://video/x.mp4" {
		t.Fatalf("视频 URL 错误：%v", resp.VideoURL)
	}
	if resp.Resolution != "1920x1080" || resp.Duration == nil || *resp.Duration != 5 {
		t.Fatalf("resolution/duration 错误：%s %v", resp.Resolution, resp.Duration)
	}
	if resp.Format != "mp4" {
		t.Fatalf("格式应为 mp4，得 %s", resp.Format)
	}
}

// 对齐 :1397-1399 无视频 URL 报错。
func TestGenerateVideoDashscope_NoVideo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status_code":200,"output":{}}`))
	}))
	defer srv.Close()

	client := dashscopeTestClient(t, srv.URL)
	_, err := client.GenerateVideo(context.Background(),
		[]*llmschema.UserMessage{llmschema.NewUserMessage("a cat")})
	if err == nil || !containsStr(err.Error(), "No video URL returned from DashScope API.") {
		t.Fatalf("应报无视频：%v", err)
	}
}

// 对齐 generate_image 的不支持 provider 分支（Python: :841-845）。
func TestGenerateImage_UnsupportedProvider(t *testing.T) {
	client := newTestClient(t, "http://127.0.0.1:1", "m")
	_, err := client.GenerateImage(context.Background(),
		[]*llmschema.UserMessage{llmschema.NewUserMessage("a cat")})
	if err == nil || !containsStr(err.Error(), "does not support image generation") {
		t.Fatalf("不支持 provider 应报错：%v", err)
	}
	_, err = client.TranscribeAudio(context.Background(), "x.wav")
	if err == nil {
		t.Fatal("TranscribeAudio 应不支持")
	}
}

// 对齐 _to_ow_usage_metadata 的 nil 边界 + _provider_metadata 白名单
// （Python: :990-992/:1051-1067）。
func TestToOwUsageMetadata_NilAndProviderMetadata(t *testing.T) {
	if toOwUsageMetadata(nil) != nil {
		t.Fatal("nil 用量应返回 nil")
	}
	pm := providerMetadata(map[string]any{
		"system_fingerprint": "fp1", "service_tier": "tier", "status": "ok",
		"stop_reason": "end_turn", "stop_sequence": nil, "incomplete_details": nil,
		"secret_key": "leak",
	})
	if _, leaked := pm["secret_key"]; leaked {
		t.Fatal("白名单外键泄漏")
	}
	if pm["system_fingerprint"] != "fp1" || pm["stop_reason"] != "end_turn" {
		t.Fatalf("白名单键丢失：%v", pm)
	}
	if len(providerMetadata(nil)) != 0 {
		t.Fatal("nil 元数据应为空 map")
	}
}

// newTestClientWithDeployments 指定 deployment 列表的客户端构造。
func newTestClientWithDeployments(t *testing.T, deps []map[string]any) *IntelliRouterModelClient {
	t.Helper()
	cfg := &IntelliRouterClientConfig{
		Deployments:    deps,
		Strategy:       "simple-shuffle",
		NumRetries:     1,
		Timeout:        10,
		StrategyKwargs: map[string]any{},
	}
	r, err := CreateRouter(cfg)
	if err != nil {
		t.Fatalf("create router: %v", err)
	}
	embed, err := model_clients.NewBaseClientEmbed(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("*")),
		&llmschema.ModelClientConfig{ClientProvider: "intelli_router"},
	)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	return &IntelliRouterModelClient{BaseClientEmbed: *embed, router: r}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
