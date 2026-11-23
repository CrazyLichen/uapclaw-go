//go:build integration

// 17.10 acceptance_pipeline 共享夹具：fake OpenAI 兼容端点 / 模型配置 /
// 数据集 / harness 引用，提取自 evaluator_scoring_pipeline 手法（spec
// 2026-12-12-rsi-17.10-acceptance-pipeline-design §3）。父目录 rsitest 包的
// 非导出符号跨包不可见，本包按需重建。
//
// 对应 Python 代码：tests/system_tests/rsi/（e2e 形态）+
// tests/unit_tests/rsi/（夹具语义基线）。

package rsi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	harnessconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/config"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator"
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/singleharness"
)

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
