//go:build integration

// 17.9 链路3（RPC → 真引擎端到端）：AgentServer.HandleRSIRequest（E2A wire
// 编解码）→ server/rsi 分发层 → 服务域 worker → HarnessProvider 真身
// → singleharness 真编排器（fake evaluator 边界：评分 1.0 → solved 阈值提前
// 停止，零 LLM 调用）→ COMPLETED + P1/P2/P3 推送 + 树/报告/state.json 可查。
// 跨子系统 ≥3：server 分发层 + 服务域（agents/harness/common/rsi）+ 真引擎
// （agent_evolving/rsi/harness_rsi/singleharness）+ transport（E2A wire）。

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

	configpkg "github.com/uapclaw/uapclaw-go/internal/common/config"
	coreRSI "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi"
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
	harnessconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/config"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/singleharness"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rsi"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 结构体 ────────────────────────────

// chain3FakeEvaluator 真编排器评估缝替身：按预置评分写真实 eval_ref 形态
// （复用 17.4d orchestrator_test 的 eval_ref/trace/result 落盘形态）。
type chain3FakeEvaluator struct {
	scores map[string]float64
	calls  int
}

func (f *chain3FakeEvaluator) Evaluate(ctx context.Context, dataset harnessrsi.DatasetArtifact,
	teamSkillRefPath, harnessRefsPath, outputDir string) (string, error) {
	cases := make([]map[string]any, 0, len(dataset.DatasetFiles))
	for _, id := range dataset.DatasetFiles {
		cases = append(cases, map[string]any{"case_id": id, "score": f.scores[id], "status": "passed"})
	}
	return chain3WriteEvalRef(outputDir, cases, harnessRefsPath), nil
}

func (f *chain3FakeEvaluator) EvaluateBatch(ctx context.Context, cases []map[string]any,
	teamSkillRefPath, harnessRefsPath, outputDir string,
	dataset *harnessrsi.DatasetArtifact,
	onCaseStage func(evaluator.EvaluationStageEvent) error,
	caseConcurrency int,
	baselineContexts map[string]*evaluator.BaselineJudgeContext) (string, error) {
	f.calls++
	_ = teamSkillRefPath
	evalCases := []map[string]any{}
	for _, single := range cases {
		caseID, _ := single["case_id"].(string)
		score, ok := f.scores[caseID]
		if !ok {
			score = 1.0
		}
		resultPath := filepath.Join(outputDir, "cases", caseID, "result.json")
		os.MkdirAll(filepath.Dir(resultPath), 0o755)
		os.WriteFile(resultPath, []byte(`{"case_id":"`+caseID+`","score":`+fmt.Sprintf("%v", score)+`,"status":"passed","execution_status":"passed","metadata":{"execution":{"execution_evidence":{"status":"complete","timing_complete":true}}}}`), 0o644)
		tracePath := filepath.Join(outputDir, "cases", caseID, "trace.json")
		os.WriteFile(tracePath, []byte(`{"steps":[]}`), 0o644)
		evalCases = append(evalCases, map[string]any{
			"case_id": caseID, "score": score, "status": "passed",
			"result_path": resultPath, "trace_path": tracePath,
		})
	}
	return chain3WriteEvalRef(outputDir, evalCases, harnessRefsPath), nil
}

// chain3WriteEvalRef 写 eval_ref.yaml（对齐 Python eval_ref 消费形态）。
func chain3WriteEvalRef(outputDir string, cases []map[string]any, harnessRefsPath string) string {
	os.MkdirAll(outputDir, 0o755)
	evalRef := filepath.Join(outputDir, "eval_ref.yaml")
	content := ""
	if harnessRefsPath != "" {
		content += "harness_refs_path: " + harnessRefsPath + "\n"
	}
	content += "cases:\n"
	for _, single := range cases {
		content += fmt.Sprintf("  - case_id: %v\n    score: %v\n    status: %v\n    result_path: %v\n    trace_path: %v\n",
			single["case_id"], single["score"], single["status"], single["result_path"], single["trace_path"])
	}
	os.WriteFile(evalRef, []byte(content), 0o644)
	return evalRef
}

// chain3WireCollector 收集 ChannelTransport recv 通道的 E2A wire 帧。
type chain3WireCollector struct {
	frames []string
	mu     chan struct{}
	done   chan struct{}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestRPCToRealEngine_端到端 链路3：RPC 编解码 → 分发 → 服务域 → 真引擎 → COMPLETED。
func TestRPCToRealEngine_端到端(t *testing.T) {
	t.Setenv("RSI_ENABLED", "true")
	t.Setenv("HOME", t.TempDir())
	// 工作区/配置根指到临时目录（getResolvedPaths 首次解析在本次调用内完成）。
	dataDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", dataDir)
	os.MkdirAll(filepath.Join(dataDir, "config"), 0o755)
	// models.defaults 注册 m1（材料化阶段的 model_refs 解析与角色 YAML 落盘；
	// 真引擎侧 evaluator 已注入 fake，模型不会被真实调用）。
	os.WriteFile(filepath.Join(dataDir, "config", "config.yaml"), []byte(
		"models:\n  defaults:\n    - model_client_config:\n        model_name: m1\n        client_provider: openai\n        api_key: sk-test\n        api_base: https://example.invalid\n      model_config_obj:\n        model: m1\n"), 0o644)
	homeRoot := filepath.Join(os.Getenv("HOME"), ".uapclawswarm", "rsi", "tasks")
	os.MkdirAll(homeRoot, 0o755)

	// ① 真编排器（fake evaluator 边界，零 LLM）。
	fakeEval := &chain3FakeEvaluator{scores: map[string]float64{"c1": 1.0}}
	orchestrator, err := singleharness.NewSingleHarnessIterativeOptimizationOrchestrator(
		chain3OrchestratorConfig(), fakeEval, nil, nil, nil)
	if err != nil {
		t.Fatalf("构造真编排器: %v", err)
	}
	provider := rsi.NewHarnessProvider(homeRoot, nil, rsi.WithOrchestrator(orchestrator))

	// ② AgentServer RPC 入口（真 handle_message 分发 + E2A wire 编码）。
	cfg, cfgErr := configpkg.New("")
	if cfgErr != nil {
		t.Fatalf("构造配置: %v", cfgErr)
	}
	ct := transport.NewChannelTransport()
	server := server.NewAgentServer(cfg, ct)
	server.SetRSIHarnessProvider(provider)
	recv, err := ct.Recv()
	if err != nil {
		t.Fatalf("打开 recv 通道: %v", err)
	}
	// 帧收集器：持续累积（推送与响应混流，不可丢弃）。
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
					return chain3ParseFrame(frame)
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("等待 %s 响应超时", requestID)
		return nil
	}

	// ③ 数据集 + harness 配置包（materializer 物化基线）。
	ds := filepath.Join(t.TempDir(), "ds.json")
	if err := os.WriteFile(ds, []byte(`{"cases":[{"case_id":"c1","input":"任务"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	harnessDir := filepath.Join(t.TempDir(), "harnesspkg")
	os.MkdirAll(harnessDir, 0o755)
	os.WriteFile(filepath.Join(harnessDir, "harness_config.yaml"),
		[]byte("name: chain3-demo\nversion: 1\n"), 0o644)

	callRPC := func(requestID, method string, params map[string]any) map[string]any {
		raw, _ := json.Marshal(params)
		server.HandleRSIRequest(context.Background(), &schema.AgentRequest{
			RequestID: requestID, ChannelID: "web",
			ReqMethod: schema.ReqMethod(method), SessionID: strPtr("sess-chain3"),
			Params: raw,
		})
		return waitResponse(requestID, 15*time.Second)
	}

	// ④ rsi.task.create（含 harness_path 物化基线 refs + 材料快照）。
	created := callRPC("r1", "rsi.task.create", map[string]any{
		"scenario": "HARNESS", "name": "chain3", "input_file": ds,
		"harness_path":   harnessDir,
		"model_refs":     map[string]any{"optimizer": "m1", "tester": "m1"},
		"max_iterations": 1,
	})
	taskID, _ := chain3ResultField(created, "task_id").(string)
	if taskID == "" {
		t.Fatalf("create 未返回 task_id: %v", created)
	}

	// ⑤ rsi.training.start（真引擎跑通一个 epoch：H0 评分 1.0 → solved 提前停止）。
	started := callRPC("r2", "rsi.training.start", map[string]any{"task_id": taskID})
	startFrame, _ := started["__frame__"].(string)
	if !strings.Contains(startFrame, `"status":"QUEUED"`) && !strings.Contains(startFrame, `"status":"RUNNING"`) {
		t.Fatalf("start 状态异常: %v", started)
	}

	// ⑥ 轮询至 COMPLETED（推送/树/报告断言并行收帧）。
	deadline := time.Now().Add(60 * time.Second)
	final := map[string]any{}
	for time.Now().Before(deadline) {
		got := callRPC(fmt.Sprintf("poll-%d", time.Now().UnixNano()), "rsi.task.get",
			map[string]any{"task_id": taskID})
		if st, _ := chain3ResultField(got, "status").(string); st == "COMPLETED" || st == "FAILED" {
			final = got
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	stDug, _ := chain3ResultField(final, "status").(string)
	if stDug != "COMPLETED" {
		if raw, err := os.ReadFile(filepath.Join(homeRoot, taskID, "task.json")); err == nil {
			var taskMap map[string]any
			if json.Unmarshal(raw, &taskMap) == nil {
				t.Logf("task.status=%v task.failure=%v results.keys=%v",
					taskMap["status"], taskMap["failure_reason"],
					func() []string {
						cfg, _ := taskMap["config"].(map[string]any)
						res, _ := cfg["results"].(map[string]any)
						keys := []string{}
						for k := range res {
							keys = append(keys, k)
						}
						return keys
					}())
			}
		}
		t.Fatalf("任务未完成: status=%q fake_eval_calls=%d", stDug, fakeEval.calls)
	}

	// ⑦ 推送断言：P1 状态变更 + P2 进度 + P3 树增量均经 RPC 出口到达。
	seen := map[string]bool{}
	time.Sleep(300 * time.Millisecond) // 收尾排空在途帧
	mu.Lock()
	snapshot := append([]string(nil), allFrames...)
	mu.Unlock()
	for _, frame := range snapshot {
		for _, evt := range []string{
			"rsi.training.status.changed", "rsi.training.progress", "rsi.training.tree.delta",
		} {
			if strings.Contains(frame, `"`+evt+`"`) {
				seen[evt] = true
			}
		}
	}
	for _, evt := range []string{"rsi.training.status.changed", "rsi.training.progress", "rsi.training.tree.delta"} {
		if !seen[evt] {
			t.Fatalf("缺少推送 %s（已见: %v）", evt, seen)
		}
	}

	// ⑧ 引擎侧断言：evaluator 真被调用（H0 评估走真编排器）。
	if fakeEval.calls < 1 {
		t.Fatalf("真引擎未调用评估缝")
	}

	// ⑨ 树/报告/state.json 可查（rsi.tree.get / rsi.report.get RPC）。
	tree := callRPC("r3", "rsi.tree.get", map[string]any{"task_id": taskID})
	if nodes, _ := chain3ResultField(tree, "nodes").([]any); len(nodes) < 1 {
		t.Fatalf("树查询无节点: %v", chain3ResultField(tree, "nodes"))
	}
	report := callRPC("r4", "rsi.report.get", map[string]any{"task_id": taskID})
	if chain3ResultField(report, "task_id") == nil && chain3ResultField(report, "status") == nil {
		t.Fatalf("报告查询为空: %v", chain3ResultField(report, "report_path"))
	}
	// 引擎状态文件：single_harness_state.yaml（17.4d 三件套命名，provider stateFile 常量同源）。
	statePath := filepath.Join(homeRoot, taskID, "run", "single_harness_state.yaml")
	if _, err := os.Stat(statePath); err != nil {
		found := false
		filepath.Walk(filepath.Join(homeRoot, taskID), func(path string, _ os.FileInfo, _ error) error {
			if filepath.Base(path) == "single_harness_state.yaml" {
				found = true
			}
			return nil
		})
		if !found {
			t.Fatalf("引擎状态文件未落盘: %v", err)
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// chain3OrchestratorConfig 通过校验的编排器配置（复刻 singleharness
// orchestrator_test buildTestConfig——只取导出字段，评分模式 binary）。
func chain3OrchestratorConfig() harnessconfig.AutoCoordinatingHarnessConfig {
	return harnessconfig.AutoCoordinatingHarnessConfig{
		MaxEpochs: 2,
		Evaluator: harnessconfig.EvaluatorConfig{
			Backend:                      "single_harness",
			ScoreMode:                    "binary",
			EvaluationMethod:             "script-based",
			SolverMaxIterationsHardLimit: 10,
		},
		Scheduling: harnessconfig.OrchestratorSchedulingConfig{
			EvaluationStrategy:        "hybrid",
			CoordinationStrategy:      "team_first_single_pass",
			FullEvaluationEnabled:     true,
			FullEvaluationConcurrency: 1,
		},
		MemberOptimizer: harnessconfig.MemberOptimizerConfig{
			SiblingCandidateCount:           1,
			MaxRepairRoundsPerBatch:         2,
			CandidateMinTargetBehaviorDelta: 0.1,
		},
		DataLoader: harnessconfig.DataLoaderConfig{BatchSize: 1},
		Promotion:  harnessconfig.PromotionConfig{PolicyVersion: "legacy_epoch_v1"},
	}
}

// chain3ParseFrame 解析 wire 帧 JSON（失败返回错误承载 map）。
func chain3ParseFrame(frame string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(frame), &m); err != nil {
		return map[string]any{"__parse_error__": err.Error(), "__frame__": frame}
	}
	m["__frame__"] = frame
	return m
}

// chain3ResultField 定向读取 wire 帧 body.result.<key>（E2A 包装定形）。
func chain3ResultField(frame map[string]any, key string) any {
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

// strPtr 字符串指针助手。
func strPtr(s string) *string { return &s }

// 编译期锚点：确保 wire 消费面未漂移。
var (
	_ = e2a.EncodeAgentResponseForWire
	_ coreRSI.EngineEvent
)
