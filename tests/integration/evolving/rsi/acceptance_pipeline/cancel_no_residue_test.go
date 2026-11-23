//go:build integration

// 17.10 验收点④：取消后不残留（双用例）。
//
// 用例①服务域全链：17.9 真服务入口（rsi.task.create → rsi.training.start →
// rsi.training.terminate）→ 真 singleharness 引擎运行中被取消（慢 fake LLM
// 制造取消窗口）→ TERMINATED 状态落库 + 事件流终止于取消事件 + 终态后无
// 新推进事件。
//
// 用例②引擎直调：Run(ctx cancel) 验证清理面（状态 terminated、无发布工件、
// ErrTaskCancelled/ctx 取消分类）。与 17.9 域测试不重叠：那条管服务域传递
// （fake 引擎），本条管引擎清理。
//
// 跨子系统 ≥3：server/rsi 分发层 + 服务域（agents/harness/common/rsi）+
// 真引擎（singleharness）+ evaluator 真链 + transport（E2A wire）。

package rsi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	configpkg "github.com/uapclaw/uapclaw-go/internal/common/config"
	rsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/singleharness"
	svcRsi "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rsi"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// accResultField 定向读取 wire 帧 body.result.<key>（复刻 chain3ResultField）。
func accResultField(frame map[string]any, key string) any {
	body, _ := frame["body"].(map[string]any)
	if body == nil {
		return nil
	}
	result, _ := body["result"].(map[string]any)
	if result == nil {
		return nil
	}
	return result[key]
}

// accParseFrame 解析 wire 帧 JSON（失败返回错误承载 map，复刻 chain3ParseFrame）。
func accParseFrame(frame string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(frame), &m); err != nil {
		return map[string]any{"__parse_error__": err.Error(), "__frame__": frame}
	}
	m["__frame__"] = frame
	return m
}

// CancelNoResidueSuite 取消残留套件。
type CancelNoResidueSuite struct{ suite.Suite }

// TestCancelNoResidueSuite 入口。
func TestCancelNoResidueSuite(t *testing.T) { suite.Run(t, new(CancelNoResidueSuite)) }

// TestCancelNoResidueSuite_服务域全链 全链：terminate → 真引擎取消 → 无残留。
func (s *CancelNoResidueSuite) TestCancelNoResidueSuite_服务域全链() {
	t := s.T()
	t.Setenv("RSI_ENABLED", "true")
	t.Setenv("HOME", t.TempDir())
	dataDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", dataDir)
	if err := os.MkdirAll(filepath.Join(dataDir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	// models.defaults 注册 m1（materializer model_refs 解析；真引擎走
	// 慢 fake LLM 端点制造取消窗口）。
	if err := os.WriteFile(filepath.Join(dataDir, "config", "config.yaml"), []byte(
		"models:\n  defaults:\n    - model_client_config:\n        model_name: m1\n        client_provider: openai\n        api_key: sk-test\n        api_base: https://example.invalid\n      model_config_obj:\n        model: m1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	homeRoot := filepath.Join(os.Getenv("HOME"), ".uapclawswarm", "rsi", "tasks")
	if err := os.MkdirAll(homeRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	// 真编排器：真 evaluator + 慢 fake LLM（每次响应 sleep 500ms →
	// solver 迭代期内 terminate 必落在运行窗口）。
	llm := &accFakeLLM{replies: []string{"REPLY"}, slowDur: 500}
	llmServer := llm.server()
	t.Cleanup(llmServer.Close)
	modelConfig := accWriteModelConfig(t, llmServer.URL)

	orchestrator, err := singleharness.NewSingleHarnessIterativeOptimizationOrchestrator(
		accOrchestratorConfig(),
		accRealEvaluatorForCancel(t, modelConfig), nil, nil, nil)
	if err != nil {
		t.Fatalf("构造真编排器: %v", err)
	}
	provider := svcRsi.NewHarnessProvider(homeRoot, nil, svcRsi.WithOrchestrator(orchestrator))

	// AgentServer RPC 入口（真 handle_message 分发 + E2A wire 编码）。
	cfg, cfgErr := configpkg.New("")
	if cfgErr != nil {
		t.Fatalf("构造配置: %v", cfgErr)
	}
	ct := transport.NewChannelTransport()
	srv := server.NewAgentServer(cfg, ct)
	srv.SetRSIHarnessProvider(provider)
	recv, err := ct.Recv()
	if err != nil {
		t.Fatalf("打开 recv 通道: %v", err)
	}
	var mu sync.Mutex
	var allFrames []string
	go func() {
		for {
			data, ok := <-recv
			if !ok {
				return
			}
			mu.Lock()
			allFrames = append(allFrames, string(data))
			mu.Unlock()
		}
	}()
	waitResponse := func(requestID string, timeout time.Duration) map[string]any {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			mu.Lock()
			snapshot := append([]string(nil), allFrames...)
			mu.Unlock()
			for _, frame := range snapshot {
				if strings.Contains(frame, `"request_id":"`+requestID+`"`) {
					return accParseFrame(frame)
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("等待 %s 响应超时", requestID)
		return nil
	}
	callRPC := func(requestID, method string, params map[string]any) map[string]any {
		raw, _ := json.Marshal(params)
		srv.HandleRSIRequest(context.Background(), &schema.AgentRequest{
			RequestID: requestID, ChannelID: "web",
			ReqMethod: schema.ReqMethod(method), SessionID: strPtr("sess-cancel"),
			Params: raw,
		})
		return waitResponse(requestID, 15*time.Second)
	}

	// 数据集 + harness 配置包（materializer 物化基线，复刻 realchain）。
	ds := filepath.Join(t.TempDir(), "ds.json")
	if err := os.WriteFile(ds, []byte(`{"cases":[{"case_id":"c1","task_input":{"query":"answer with REPLY"},"reference":{"answer":"REPLY"}}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	harnessDir := filepath.Join(t.TempDir(), "harnesspkg")
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(harnessDir, "harness_config.yaml"),
		[]byte("name: cancel-demo\nversion: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	created := callRPC("r1", "rsi.task.create", map[string]any{
		"scenario": "HARNESS", "name": "cancel", "input_file": ds,
		"harness_path":   harnessDir,
		"model_refs":     map[string]any{"optimizer": "m1", "tester": "m1"},
		"max_iterations": 2,
	})
	taskID, _ := accResultField(created, "task_id").(string)
	if taskID == "" {
		t.Fatalf("create 未返回 task_id: %v", created)
	}

	started := callRPC("r2", "rsi.training.start", map[string]any{"task_id": taskID})
	startFrame, _ := started["__frame__"].(string)
	if !strings.Contains(startFrame, `"status":"QUEUED"`) && !strings.Contains(startFrame, `"status":"RUNNING"`) {
		t.Fatalf("start 状态异常: %v", started)
	}

	// 等 RUNNING（慢 LLM 保证引擎进入运行窗口）。
	waitRunning := func() bool {
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			got := callRPC(fmt.Sprintf("run-%d", time.Now().UnixNano()), "rsi.task.get",
				map[string]any{"task_id": taskID})
			if st, _ := accResultField(got, "status").(string); st == "RUNNING" {
				return true
			}
			time.Sleep(100 * time.Millisecond)
		}
		return false
	}
	isRunning := waitRunning()

	// terminate（无论是否观测到 RUNNING——引擎至少已在 QUEUED 启动协程）。
	term := callRPC("r3", "rsi.training.terminate", map[string]any{"task_id": taskID})
	termFrame, _ := term["__frame__"].(string)
	if strings.Contains(termFrame, `"error"`) {
		t.Fatalf("terminate 被拒: %v", term)
	}

	// 轮询至 TERMINATED。
	deadline := time.Now().Add(60 * time.Second)
	finalStatus := ""
	for time.Now().Before(deadline) {
		got := callRPC(fmt.Sprintf("poll-%d", time.Now().UnixNano()), "rsi.task.get",
			map[string]any{"task_id": taskID})
		if st, _ := accResultField(got, "status").(string); st == "TERMINATED" || st == "FAILED" || st == "COMPLETED" {
			finalStatus = st
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if finalStatus != "TERMINATED" {
		t.Fatalf("终态应为 TERMINATED，得到 %q（RUNNING 观测=%v）", finalStatus, isRunning)
	}

	// 断言 2：事件流含 TERMINATED 状态推送，且其后无新的阶段推进事件。
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	snapshot := append([]string(nil), allFrames...)
	mu.Unlock()
	terminatedAt, progressAfter := -1, 0
	for i, frame := range snapshot {
		if strings.Contains(frame, `"rsi.training.status.changed"`) && strings.Contains(frame, "TERMINATED") {
			terminatedAt = i
		}
		if terminatedAt >= 0 && i > terminatedAt &&
			strings.Contains(frame, `"rsi.training.progress"`) {
			progressAfter++
		}
	}
	if terminatedAt < 0 {
		t.Fatalf("缺少 TERMINATED 状态推送（帧数=%d）", len(snapshot))
	}
	if progressAfter > 0 {
		t.Fatalf("终态后有 %d 条进度推进事件（残留）", progressAfter)
	}

	// 断言 3：引擎状态文件 status=terminated（Run 收尾段落盘，
	// iterative.go observer.State()["status"]="terminated"）。
	// 引擎状态文件落 terminated（Run 收尾段异步写，轮询窗口消除竞态）。
	stateStatus := ""
	deadlineState := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadlineState) {
		found := false
		_ = filepath.Walk(filepath.Join(homeRoot, taskID), func(path string, _ os.FileInfo, _ error) error {
			if filepath.Base(path) == "single_harness_state.yaml" {
				raw, err := os.ReadFile(path)
				if err == nil && strings.Contains(string(raw), "status: terminated") {
					stateStatus = "terminated"
					found = true
				}
			}
			return nil
		})
		if found {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if stateStatus != "terminated" {
		t.Fatalf("引擎状态文件未落 terminated（60s 轮询超时）")
	}

	// 断言 4：终态后排空期无新增帧（无残留活动）。
	time.Sleep(2 * time.Second)
	mu.Lock()
	after := len(allFrames)
	mu.Unlock()
	time.Sleep(500 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(allFrames) != after {
		t.Fatalf("终态后仍有活动帧：%d → %d", after, len(allFrames))
	}
}

// TestCancelNoResidueSuite_引擎直调 直调 Run(ctx cancel)：取消分类 + 状态
// terminated + 无发布工件（与 17.9 域测试分工：此条管引擎清理面）。
func (s *CancelNoResidueSuite) TestCancelNoResidueSuite_引擎直调() {
	t := s.T()
	llm := &accFakeLLM{replies: []string{"REPLY"}, slowDur: 1000}
	llmServer := llm.server()
	t.Cleanup(llmServer.Close)

	root := t.TempDir()
	modelConfig := accWriteModelConfig(t, llmServer.URL)
	orchestrator, pack := newAccOrchestratorWithModel(t, modelConfig, root)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond) // 进入首个慢 LLM 调用后取消
		cancel()
	}()
	_, err := orchestrator.Run(ctx, pack.req, func(context.Context, rsi.EngineEvent) error { return nil })
	if err == nil {
		t.Fatal("取消必须返回错误")
	}
	if ctx.Err() == nil {
		t.Fatal("ctx 应处于取消态")
	}

	// 清理面：状态文件 terminated（Run 收尾段写 observer.State）。
	statePath := filepath.Join(pack.root, "output", "single_harness_state.yaml")
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("状态文件未落盘: %v", err)
	}
	if !strings.Contains(string(raw), "status: terminated") {
		t.Fatalf("状态应 terminated：%s", raw)
	}
	// 清理面：无发布工件。
	reportPath := filepath.Join(pack.root, "output", "single_harness_report.yaml")
	if report, err := os.ReadFile(reportPath); err == nil {
		if strings.Contains(string(report), "publication_status: published") {
			t.Fatal("取消后不得发布工件")
		}
	}
}
