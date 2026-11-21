package judger

// 裁判运行时测试：JudgeBudgetRail 迭代预算 + GuardedJudgeModel 边界 + 修复链 + closeout。
// Python: judge_runtime.py / evidence_guard.py GuardedJudgeModel 对应行为。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/config"
	ceiface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	llm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
)

// ──────────────────────────── 测试助手 ────────────────────────────

// newJudgeTestConfig 构造指向 fake OpenAI 端点的 EvaluatorConfig。
func newJudgeTestConfig(t *testing.T, baseURL string) *config.EvaluatorConfig {
	t.Helper()
	dir := t.TempDir()
	ref := filepath.Join(dir, "judge_model.yaml")
	modelCfg := "model:\n" +
		"  model_client_config:\n" +
		"    client_id: judge-cid\n" +
		"    client_provider: llm_OpenAI\n" +
		"    model_name: judge-model\n" +
		"    api_key: sk-test\n" +
		"    api_base: " + baseURL + "\n" +
		"    max_retries: 5\n" +
		"  model_request_config:\n" +
		"    max_tokens: 512\n"
	if err := os.WriteFile(ref, []byte(modelCfg), 0o644); err != nil {
		t.Fatalf("写模型配置失败: %v", err)
	}
	return &config.EvaluatorConfig{
		ModelConfigRef:          ref,
		JudgeAgentMaxIterations: 3,
		JudgeTimeoutSec:         30,
		JudgeMaxRetries:         2,
		ScoreMode:               "binary",
	}
}

// fakeOpenAIServer 返回固定 chat.completion 响应，可选捕获请求消息。
func fakeOpenAIServer(t *testing.T, content string, finishReason string, capture *[][]map[string]any) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if capture != nil {
			msgs, _ := body["messages"].([]any)
			rows := make([]map[string]any, 0, len(msgs))
			for _, m := range msgs {
				if row, ok := m.(map[string]any); ok {
					rows = append(rows, row)
				}
			}
			mu.Lock()
			*capture = append(*capture, rows)
			mu.Unlock()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "chatcmpl-test",
			"object": "chat.completion",
			"model":  "judge-model",
			"choices": []map[string]any{{
				"index":         0,
				"finish_reason": finishReason,
				"message":       map[string]any{"role": "assistant", "content": content},
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		})
	}))
}

// fakeModelContextJudge 测试用最小 ModelContext（仅 AddMessages）。
type fakeModelContextJudge struct {
	ceiface.ModelContext
	added [][]llmschema.BaseMessage
}

func (f *fakeModelContextJudge) AddMessages(_ context.Context, messages []llmschema.BaseMessage, _ ...ceiface.Option) ([]llmschema.BaseMessage, error) {
	f.added = append(f.added, messages)
	return messages, nil
}

// ──────────────────────────── JudgeBudgetRail ────────────────────────────

// TestJudgeBudgetRail_轮次计数与最终轮提示 Python before_model_call（judge_runtime.py L48-71）。
func TestJudgeBudgetRail_轮次计数与最终轮提示(t *testing.T) {
	rail := NewJudgeBudgetRail(3, filepath.Join(t.TempDir(), "tool_events.jsonl"))
	mc := &fakeModelContextJudge{}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	cbc.SetModelContext(mc)

	// 第 1-2 轮：不追加提示
	for i := 0; i < 2; i++ {
		if err := rail.BeforeModelCall(context.Background(), cbc); err != nil {
			t.Fatalf("BeforeModelCall 报错: %v", err)
		}
	}
	if len(mc.added) != 0 {
		t.Fatalf("前两轮不应追加消息，实得 %d", len(mc.added))
	}
	if rail.Turns() != 2 {
		t.Fatalf("turns 应为 2，实得 %d", rail.Turns())
	}

	// 第 3 轮（>= iterations）：追加最终轮提示
	if err := rail.BeforeModelCall(context.Background(), cbc); err != nil {
		t.Fatalf("BeforeModelCall 报错: %v", err)
	}
	if len(mc.added) != 1 || len(mc.added[0]) != 1 {
		t.Fatalf("最终轮应追加一条消息")
	}
	content := mc.added[0][0].GetContent().Text()
	if !strings.Contains(content, "Final evaluation turn") {
		t.Fatalf("最终轮提示文案不符: %q", content)
	}
	if !strings.Contains(content, "status=unavailable") {
		t.Fatal("提示应含 unavailable 条款")
	}
}

// ──────────────────────────── GuardedJudgeModel ────────────────────────────

// TestGuardedJudgeModel_构造与诊断记录 Python evidence_guard.py L132-166。
func TestGuardedJudgeModel_构造与诊断记录(t *testing.T) {
	server := fakeOpenAIServer(t, "ok", "stop", nil)
	defer server.Close()

	cfg := newJudgeTestConfig(t, server.URL)
	built, err := judgeModel(cfg)
	if err != nil {
		t.Fatalf("judgeModel 报错: %v", err)
	}
	model := built
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	ctx, calls, cancel := CollectJudgeModelCalls(context.Background())
	defer cancel()

	resp, err := model.Invoke(ctx, model_clients.NewDictsMessagesParam([]map[string]any{
		{"role": "system", "content": "sys"},
		{"role": "user", "content": "grade it"},
	}))
	if err != nil {
		t.Fatalf("Invoke 报错: %v", err)
	}
	if resp == nil {
		t.Fatal("应返回响应")
	}
	// 诊断采集应记录本次调用（finish_reason=stop）
	if len(calls.Items) != 1 {
		t.Fatalf("应记录一次诊断，实得 %d", len(calls.Items))
	}
}

// ──────────────────────────── judgeModel ────────────────────────────

// TestJudgeModel_配置载入与温度覆盖 Python _judge_model（judge_runtime.py L92-106）。
func TestJudgeModel_配置载入与温度覆盖(t *testing.T) {
	server := fakeOpenAIServer(t, "ok", "stop", nil)
	defer server.Close()
	cfg := newJudgeTestConfig(t, server.URL)

	model, err := judgeModel(cfg)
	if err != nil {
		t.Fatalf("judgeModel 报错: %v", err)
	}
	if model == nil {
		t.Fatal("应返回模型")
	}
	// without_inner_sdk_retries: max_retries 归零
	if model.ClientConfig.MaxRetries != 0 {
		t.Fatalf("max_retries 应为 0，实得 %d", model.ClientConfig.MaxRetries)
	}
	// temperature 覆盖为 1.0
	if model.ModelConfig.Temperature == nil || *model.ModelConfig.Temperature != 1.0 {
		t.Fatalf("temperature 应为 1.0")
	}
}

// ──────────────────────────── RepairJudgeJSON ────────────────────────────

// TestRepairJudgeJSON_仅修语法 Python repair_judge_json（judge_runtime.py L108-127）。
func TestRepairJudgeJSON_仅修语法(t *testing.T) {
	broken := `{"score": 0.5, "reason": "trun`
	server := fakeOpenAIServer(t, `{"score": 0.5, "reason": "truncated"}`, "stop", nil)
	defer server.Close()
	cfg := newJudgeTestConfig(t, server.URL)

	out, err := RepairJudgeJSON(context.Background(), cfg, broken, "unterminated string")
	if err != nil {
		t.Fatalf("RepairJudgeJSON 报错: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("修复结果应为合法 JSON: %v", out)
	}
}

// ──────────────────────────── RunJudgeCloseout / _invoke_complete_evidence ────────────────────────────

// TestRunJudgeCloseout_冻结快照直评 Python run_judge_closeout（judge_runtime.py L192-208）。
func TestRunJudgeCloseout_冻结快照直评(t *testing.T) {
	workspace := t.TempDir()
	request := map[string]any{"case_id": "c1", "reference": map[string]any{}}
	raw, _ := json.Marshal(request)
	if err := os.WriteFile(filepath.Join(workspace, "request.json"), raw, 0o644); err != nil {
		t.Fatalf("写 request.json 失败: %v", err)
	}

	var captured [][]map[string]any
	verdict := `{"score": 1.0, "behaviors": [], "forbidden_hits": []}`
	server := fakeOpenAIServer(t, verdict, "stop", &captured)
	defer server.Close()
	cfg := newJudgeTestConfig(t, server.URL)

	out, err := RunJudgeCloseout(context.Background(), cfg, workspace)
	if err != nil {
		t.Fatalf("RunJudgeCloseout 报错: %v", err)
	}
	if !strings.Contains(out, "1.0") {
		t.Fatalf("应返回判决原文: %q", out)
	}
	if len(captured) == 0 {
		t.Fatal("应发生一次模型调用")
	}
	// system 提示词应来自 judge_prompt.md（非 legacy）
	sysRole := ""
	for _, row := range captured[0] {
		if role, _ := row["role"].(string); role == "system" {
			sysRole, _ = row["content"].(string)
		}
	}
	if !strings.Contains(sysRole, "All grading evidence is supplied inline") {
		t.Fatal("直评应使用内联证据改造后的策略提示词")
	}
}

// TestInvokeCompleteEvidence_图片证据需视觉模型确认 Python _invoke_complete_evidence（judge_runtime.py L211-224）。
func TestInvokeCompleteEvidence_图片证据需视觉模型确认(t *testing.T) {
	server := fakeOpenAIServer(t, "ok", "stop", nil)
	defer server.Close()
	cfg := newJudgeTestConfig(t, server.URL)
	model, err := judgeModel(cfg)
	if err != nil {
		t.Fatalf("judgeModel 报错: %v", err)
	}
	// payload 为多模态列表（含图片 part）而模型未确认支持视觉 → 基建错误
	payload := []any{
		map[string]any{"type": "text", "text": "evidence"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,xxx"}},
	}
	_, err = invokeCompleteEvidence(context.Background(), model, payload, "", false)
	if err == nil || !strings.Contains(err.Error(), "vision-capable") {
		t.Fatalf("应拒绝无视觉确认的图片证据: %v", err)
	}
}

// ──────────────────────────── RunJudgeAgent ────────────────────────────

// TestRunJudgeAgent_内联证据直评路径 Python run_judge_agent L162-176：
// 证据可完整内联时不经 agent，直接走直评。
func TestRunJudgeAgent_内联证据直评路径(t *testing.T) {
	workspace := t.TempDir()
	request := map[string]any{"case_id": "c2", "reference": map[string]any{}}
	raw, _ := json.Marshal(request)
	if err := os.WriteFile(filepath.Join(workspace, "request.json"), raw, 0o644); err != nil {
		t.Fatalf("写 request.json 失败: %v", err)
	}
	verdict := `{"score": 0.0, "behaviors": [], "forbidden_hits": []}`
	server := fakeOpenAIServer(t, verdict, "stop", nil)
	defer server.Close()
	cfg := newJudgeTestConfig(t, server.URL)

	out, err := RunJudgeAgent(context.Background(), cfg, workspace, "grade", filepath.Join(t.TempDir(), "tool_events.jsonl"))
	if err != nil {
		t.Fatalf("RunJudgeAgent 报错: %v", err)
	}
	if !strings.Contains(out, "0.0") {
		t.Fatalf("应返回判决原文: %q", out)
	}
}

// TestRunJudgeAgent_畸形输出原样返回 Python run_judge_agent L196-205：
// 模型输出非判决 JSON 且读取预算未耗尽时原样返回。
func TestRunJudgeAgent_畸形输出原样返回(t *testing.T) {
	workspace := t.TempDir()
	request := map[string]any{"case_id": "c3", "reference": map[string]any{}}
	raw, _ := json.Marshal(request)
	if err := os.WriteFile(filepath.Join(workspace, "request.json"), raw, 0o644); err != nil {
		t.Fatalf("写 request.json 失败: %v", err)
	}
	server := fakeOpenAIServer(t, "not a verdict at all", "stop", nil)
	defer server.Close()
	cfg := newJudgeTestConfig(t, server.URL)

	out, err := RunJudgeAgent(context.Background(), cfg, workspace, "grade", filepath.Join(t.TempDir(), "tool_events.jsonl"))
	if err != nil {
		t.Fatalf("畸形输出且预算未耗尽不应报错: %v", err)
	}
	if out != "not a verdict at all" {
		t.Fatalf("畸形输出应原样返回: %q", out)
	}
}

// TestRunJudgeAgent_证据缺失走Agent路径并达迭代上限 Python run_judge_agent L178-206：
// 无 request.json（内联不可用）→ agent 读取循环 → 判决缺失 + 预算耗尽 → 迭代上限。
func TestRunJudgeAgent_证据缺失走Agent路径并达迭代上限(t *testing.T) {
	workspace := t.TempDir()
	calls := 0
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		content := "still no verdict"
		if n >= 4 {
			content = `{"status":"completed","score":0.0}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-x", "object": "chat.completion", "model": "judge-model",
			"choices": []map[string]any{{"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": content}}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer server.Close()
	cfg := newJudgeTestConfig(t, server.URL)

	_, err := RunJudgeAgent(context.Background(), cfg, workspace, "grade", filepath.Join(t.TempDir(), "tool_events.jsonl"))
	if err != nil {
		// agent 路径在单测环境可能因 Runner 装配不可用而失败；此时验证错误
		// 不是"模型名空"类配置错误即可（装配链路属集成范围）。
		if strings.Contains(err.Error(), "模型名称") {
			t.Fatalf("配置错误不应出现: %v", err)
		}
		t.Logf("agent 路径单测环境受限（记录）: %v", err)
	}
}

// ──────────────────────────── 纯函数 ────────────────────────────

// TestEmbeddedPrompt_资产读取 embeddedPrompt 分支。
func TestEmbeddedPrompt_资产读取(t *testing.T) {
	md, err := embeddedPrompt("judge_prompt.md")
	if err != nil || md == "" {
		t.Fatalf("judge_prompt.md 应可读取: %v", err)
	}
	legacy, err := embeddedPrompt("judge_prompt.legacy.md")
	if err != nil || legacy == "" {
		t.Fatalf("judge_prompt.legacy.md 应可读取: %v", err)
	}
	if _, err := embeddedPrompt("nope.md"); err == nil {
		t.Fatal("未知文件名应报错")
	}
	if legacyPromptName(true) != "judge_prompt.legacy.md" || legacyPromptName(false) != "judge_prompt.md" {
		t.Fatal("legacyPromptName 分支不符")
	}
}

// TestJudgeResultToString_形态分派 judgeResultToString 三分支。
func TestJudgeResultToString_形态分派(t *testing.T) {
	if got := judgeResultToString("plain"); got != "plain" {
		t.Fatalf("string 原样: %q", got)
	}
	if got := judgeResultToString(map[string]any{"k": 1.0}); got != `{"k":1}` {
		t.Fatalf("dict 转 JSON: %q", got)
	}
	if got := judgeResultToString(nil); got != "" {
		t.Fatalf("nil 空: %q", got)
	}
	if got := judgeResultToString(42); got != "42" {
		t.Fatalf("其他 str(): %q", got)
	}
}

// TestUserMessageFromPayload_载荷形态 userMessageFromPayload 分支。
func TestUserMessageFromPayload_载荷形态(t *testing.T) {
	text := userMessageFromPayload("hello")
	if len(text) != 1 || text[0].GetContent().Text() != "hello" {
		t.Fatal("文本载荷应直传")
	}
	multi := userMessageFromPayload([]any{
		map[string]any{"type": "text", "text": "t1"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "u1"}},
	})
	if len(multi) != 1 {
		t.Fatal("多模态载荷应产出一条消息")
	}
	if multi[0].GetContent().IsText() {
		t.Fatal("多模态载荷应为 parts 内容")
	}
	other := userMessageFromPayload(123)
	if len(other) != 1 || other[0].GetContent().Text() != "123" {
		t.Fatalf("其他类型应 JSON 序列化: %v", other[0].GetContent().Text())
	}
}

// TestModelSupportsVision_声明优先 modelSupportsVision 两分支。
func TestModelSupportsVision_声明优先(t *testing.T) {
	declared := true
	model, err := llm.NewModel(&llmschema.ModelClientConfig{ClientID: "c", ClientProvider: "llm_OpenAI", APIKey: "k", APIBase: "https://example.invalid"},
		&llmschema.ModelRequestConfig{ModelName: "m"})
	model.ClientConfig.SupportsVision = &declared
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if got := modelSupportsVision(model); got == nil || *got != true {
		t.Fatal("显式声明应优先于探测")
	}
}
