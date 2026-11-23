//go:build integration

// 17.10 验收点①：引擎事件序。真编排器 + 真 TeamEvaluator（fake LLM HTTP
// 边界）跑一轮完整演进，收集 rsi.OnEvent 事件流，核对剧本顺序：
// 首事件为 ROOT node、进度事件 epoch 计数不回退、epoch 节点事件序号递增。
// 事件形状对齐 Python tests/unit_tests/rsi/test_harness_engine_events.py
// （progress_event 记已完成 epoch 数、root/epoch node_event 形态）。
//
// 跨子系统 ≥3：singleharness（真编排器）+ evaluator（真 TeamEvaluator）+
// data_loader（真载题）+ model 调用链（fake HTTP 边界）。

package rsi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	rsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi"
	harnessconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/config"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator"
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/singleharness"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EngineEventOrderSuite 引擎事件序套件。
type EngineEventOrderSuite struct{ suite.Suite }

// eventCollector 并发安全事件收集器。
type eventCollector struct {
	mu     sync.Mutex
	events []rsi.EngineEvent
}

func (c *eventCollector) add(_ context.Context, e rsi.EngineEvent) error {
	c.mu.Lock()
	c.events = append(c.events, e)
	c.mu.Unlock()
	return nil
}

func (c *eventCollector) snapshot() []rsi.EngineEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]rsi.EngineEvent(nil), c.events...)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestEngineEventOrderSuite 入口。
func TestEngineEventOrderSuite(t *testing.T) { suite.Run(t, new(EngineEventOrderSuite)) }

// accNewOrchestrator 构造真编排器（真 evaluator + fake LLM 边界）。
func (s *EngineEventOrderSuite) accNewOrchestrator(apiBase string, root string) (
	*singleharness.SingleHarnessIterativeOptimizationOrchestrator, *accRealEvalRequest) {
	s.T().Helper()
	modelConfig := accWriteModelConfig(s.T(), apiBase)
	return newAccOrchestratorWithModel(s.T(), modelConfig, root)
}

// TestEngineEventOrder_一轮演进事件剧本 fake LLM 驱动完整演进（solved 提前
// 停止），断言事件剧本：首事件 ROOT node、进度事件计数不回退、epoch 节点
// 事件序号递增、无状态异常事件。
func (s *EngineEventOrderSuite) TestEngineEventOrder_一轮演进事件剧本() {
	llm := &accFakeLLM{replies: []string{"REPLY", "REPLY", "REPLY"}}
	server := llm.server()
	s.T().Cleanup(server.Close)

	root := s.T().TempDir()
	orchestrator, pack := s.accNewOrchestrator(server.URL, root)

	collector := &eventCollector{}

	var resultErr error
	_, resultErr = orchestrator.Run(s.T().Context(), pack.req, collector.add)
	s.Require().NoError(resultErr)

	events := collector.snapshot()

	// 断言 1：事件流非空，首事件为 ROOT 节点（h0，node_type=ROOT，
	// 对齐 Python root_node_event 形态）。
	s.Require().NotEmpty(events)
	first, ok := events[0].(*rsi.EventNode)
	s.Require().True(ok, "首事件应为 node 类型，得到 %T", events[0])
	s.Require().Equal("h0", first.Node().NodeID())
	s.Require().Equal("ROOT", first.Node().Type())
	s.Require().True(first.Node().Adopted())

	// 断言 2：progress 事件的已完成 epoch 计数沿时间线不回退
	// （Python progress_event：统计已完成 epoch，本地候选尝试不推进）。
	lastProgress := -1
	progressSeen := 0
	for _, e := range events {
		if p, ok := e.(*rsi.EventProgress); ok {
			s.Require().GreaterOrEqual(p.Iteration(), lastProgress,
				"进度事件 epoch 计数回退：当前 %d，之前 %d", p.Iteration(), lastProgress)
			lastProgress = p.Iteration()
			progressSeen++
		}
	}
	s.Require().Greater(progressSeen, 0, "事件流中无 progress 事件")

	// 断言 3：epoch 节点事件（iteration_unit=epoch）序号不回退、集合无缺口。
	// 同一 epoch 会先后发 running 态与终态两次（iterative.go:4467/4805），
	// 对齐 Python epoch_node_event 的重发形态。
	lastEpoch := -1
	epochSeen := map[int]bool{}
	for _, e := range events {
		n, ok := e.(*rsi.EventNode)
		if !ok {
			continue
		}
		if unit, _ := n.Node().Extra()["iteration_unit"].(string); unit == "epoch" && n.Node().Type() != "ROOT" {
			s.Require().GreaterOrEqual(n.Node().Iteration(), lastEpoch,
				"epoch 节点事件序号回退：当前 %d，之前 %d", n.Node().Iteration(), lastEpoch)
			lastEpoch = n.Node().Iteration()
			epochSeen[n.Node().Iteration()] = true
		}
	}
	s.Require().NotEmpty(epochSeen, "无 epoch 节点事件")
	for idx := range epochSeen {
		s.Require().True(idx >= 1 && idx <= accOrchestratorConfig().MaxEpochs,
			"epoch 序号越界：%d", idx)
	}

	// 断言 4：全流程无 status 失败/终止事件（成功路径只发 terminal 之外的
	// 事件；status 仅在 cancelled/failed 收尾段出现，见 iterative.go:3464）。
	for _, e := range events {
		if st, ok := e.(*rsi.EventStatus); ok {
			s.Require().NotEqual(rsi.RsiStatusFailed, st.Status())
			s.Require().NotEqual(rsi.RsiStatusTerminated, st.Status())
		}
	}
}
// accRealEvalRequest 打包直调编排器所需的请求与路径。
type accRealEvalRequest struct {
	req      *singleharness.IterativeSingleHarnessRequest
	refsPath string
	root     string
}

// strPtr 字符串指针。
func strPtr(s string) *string { return &s }

// newAccOrchestratorWithModel 构造真编排器（真 TeamEvaluator + fake LLM
// 边界）与直调请求（dataset/harness refs/output 均在 root 下）。
func newAccOrchestratorWithModel(t *testing.T, modelConfig, root string) (
	*singleharness.SingleHarnessIterativeOptimizationOrchestrator, *accRealEvalRequest) {
	t.Helper()
	cfg, err := harnessconfig.EvaluatorConfigFromDict(map[string]any{
		"backend":               "single_harness",
		"model_config_ref":      modelConfig,
		"evaluation_method":     "exact_match",
		"case_concurrency":      1,
		"solver_max_iterations": 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	te, err := evaluator.NewTeamEvaluator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	orchestrator, err := singleharness.NewSingleHarnessIterativeOptimizationOrchestrator(
		accOrchestratorConfig(), te, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dataset := accWriteDataset(t, filepath.Join(root, "dataset"))
	refsPath := accWriteHarnessRefs(t, root)
	maxIter := 2
	req, err := singleharness.NewIterativeSingleHarnessRequest(
		dataset.DatasetFiles, refsPath, filepath.Join(root, "output"), dataset.DatasetID,
		false, "", false, "acc-task", &maxIter)
	if err != nil {
		t.Fatal(err)
	}
	return orchestrator, &accRealEvalRequest{req: req, refsPath: refsPath, root: root}
}

// accRealEvaluatorForCancel 构造真 TeamEvaluator（服务域全链取消用例用）。
func accRealEvaluatorForCancel(t *testing.T, modelConfig string) *evaluator.TeamEvaluator {
	t.Helper()
	cfg, err := harnessconfig.EvaluatorConfigFromDict(map[string]any{
		"backend":               "single_harness",
		"model_config_ref":      modelConfig,
		"evaluation_method":     "exact_match",
		"case_concurrency":      1,
		"solver_max_iterations": 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	te, err := evaluator.NewTeamEvaluator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return te
}

// ──────────────────────────── 结构体 ────────────────────────────

// accFakeLLM 可编程 fake OpenAI 兼容端点：按请求序弹出预置回复，
// 记录请求体供断言；slowDur>0 时每次响应先 sleep（制造取消窗口）。
type accFakeLLM struct {
	// replies 预置回复序列（choices[0].message.content 文本）；超出序号回落 "REPLY"
	replies []string
	// slowDur 每次响应前的 sleep 毫秒数（0=不延迟）
	slowDur int

	mu     sync.Mutex
	calls  int
	bodies []string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// accOrchestratorConfig 通过校验的编排器配置（复刻 chain3OrchestratorConfig
// ——只取导出字段，评分模式 binary）。
func accOrchestratorConfig() harnessconfig.AutoCoordinatingHarnessConfig {
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

// server 启动 fake 端点（调用方负责 Close）。
func (f *accFakeLLM) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		i := f.calls
		f.calls++
		f.bodies = append(f.bodies, string(body))
		content := "REPLY"
		if i < len(f.replies) {
			content = f.replies[i]
		}
		slow := f.slowDur
		f.mu.Unlock()
		if slow > 0 {
			time.Sleep(time.Duration(slow) * time.Millisecond)
		}
		payload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"},
			},
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
}

// callsCount 已发生的请求数（并发安全快照）。
func (f *accFakeLLM) callsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// accWriteModelConfig 写模型配置 JSON（api_base 指向 fake 端点），
// 返回文件路径。形态复刻 ScoringPipelineSuite.SetupTest。
func accWriteModelConfig(t *testing.T, apiBase string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "model_config.yaml")
	raw, err := json.Marshal(map[string]any{
		"model": map[string]any{
			"model_client_config": map[string]any{
				"client_provider": "OpenAI", "client_id": "acc-client",
				"api_key": "test-key", "api_base": apiBase,
				"verify_ssl": false, "timeout": 30.0,
			},
			"model_request_config": map[string]any{"model": "fake-model"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// accWriteDataset 写两 case 数据集（1 答对 + 1 答错，exact_match 型），
// 形态复刻 ScoringPipelineSuite.writeDataset。
func accWriteDataset(t *testing.T, dir string) harnessrsi.DatasetArtifact {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	casesFile := filepath.Join(dir, "cases.json")
	raw, err := json.Marshal([]map[string]any{
		{
			"case_id":    "case-pass",
			"task_input": map[string]any{"query": "answer with REPLY"},
			"reference":  map[string]any{"answer": "REPLY"},
		},
		{
			"case_id":    "case-fail",
			"task_input": map[string]any{"query": "answer with REPLY"},
			"reference":  map[string]any{"answer": "DIFFERENT"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(casesFile, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return harnessrsi.DatasetArtifact{
		DatasetID:    "ds-acc",
		DatasetDir:   dir,
		DatasetFiles: []string{casesFile},
	}
}

// accWriteHarnessRefs 写最小 harness 引用（role → harness 目录路径 + 最小
// harness_config.yaml），形态复刻 ScoringPipelineSuite 全链用例。
func accWriteHarnessRefs(t *testing.T, root string) string {
	t.Helper()
	harnessDir := filepath.Join(root, "harness", "solver")
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(harnessDir, "harness_config.yaml"),
		[]byte("schema_version: harness_config.v0.1\nid: acc-solver\nname: acc-solver\nlanguage: en\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refsPath := filepath.Join(root, "harness_refs.yaml")
	if err := os.WriteFile(refsPath,
		[]byte("harness_refs:\n  solver: "+harnessDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return refsPath
}
