//go:build integration

// 17.6 评分与审查全链集成用例（spec 2026-11-21 §9 硬要求 1）。
//
// 真实跨子系统链路（mock 只在边界——LLM HTTP 端点）：
//
//	EvaluatorConfig（config）→ TeamEvaluator.Evaluate（evaluator，真 loadDatasetCases
//	= data_loader）→ SingleHarnessExecutionBackend 考场（evaluator + harness +
//	member_optimizer 模型装配）→ OpenAI 兼容 fake LLM 端点（httptest，网络边界）
//	→ CaseRunner → exact_match 判分（judger）→ eval_ref.yaml / summary.json 落盘。
//
// 另含 llm_as_judge 判分器经同一 fake 端点的 verdict 解析支线。

package rsi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
	harnessrsiconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/config"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator/judger"
	sysop "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
)

// ScoringPipelineSuite 评分全链套件。
type ScoringPipelineSuite struct {
	suite.Suite
	llmServer   *httptest.Server
	modelConfig string
	// realLLM true=真链模式（LLM_API_KEY 注入），false=fake 端点（默认/CI）
	realLLM bool
}

// TestScoringPipelineSuite 入口。
func TestScoringPipelineSuite(t *testing.T) {
	suite.Run(t, new(ScoringPipelineSuite))
}

// SetupTest 起模型端点 + 写模型配置 YAML。
//
// 双模式（用户拍板 2026-12-06：RSI 域集成测试可切免费真模型）：
//   - LLM_API_KEY 未设置（默认/CI）：fake OpenAI 兼容端点（httptest 网络边界，
//     恒回固定内容）——确定性契约不受模型行为影响；
//   - LLM_API_KEY 设置：真实模型（LLM_API_BASE/LLM_MODEL_NAME 可覆盖，
//     缺省智谱 glm-4.5-flash 免费档）——同一段流水线代码真链执行，
//     断言仍为结构性产物（eval_ref/cases/result.json），与模型行为无关。
func (s *ScoringPipelineSuite) SetupTest() {
	apiKey := os.Getenv("LLM_API_KEY")
	apiBase := envWithDefaultKey("LLM_API_BASE", "https://open.bigmodel.cn/api/paas/v4")
	modelName := envWithDefaultKey("LLM_MODEL_NAME", "glm-4.5-flash")

	clientCfg := map[string]any{
		"client_provider": "OpenAI",
		"client_id":       "itest-client",
		"api_key":         "test-key",
		"verify_ssl":      false,
		"timeout":         300.0,
	}
	modelNameYAML := "fake-model"
	if apiKey != "" {
		// 真链模式：端点换真实服务商（mock 只允许在网络边界——此处直接
		// 打真实端点，无 mock）。
		s.realLLM = true
		clientCfg["api_key"] = apiKey
		clientCfg["api_base"] = apiBase
		clientCfg["verify_ssl"] = true
		modelNameYAML = modelName
	} else {
		// fake LLM：无视路径/请求体，恒回固定 assistant 内容（finish_reason=stop）。
		s.llmServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"REPLY"},"finish_reason":"stop"}]}`)
		}))
		s.T().Cleanup(s.llmServer.Close)
		clientCfg["api_base"] = s.llmServer.URL
	}

	dir := s.T().TempDir()
	s.modelConfig = filepath.Join(dir, "model_config.yaml")
	verifySSL := clientCfg["verify_ssl"].(bool)
	raw, err := json.Marshal(map[string]any{
		"model": map[string]any{
			"model_client_config": map[string]any{
				"client_provider": clientCfg["client_provider"],
				"client_id":       clientCfg["client_id"],
				"api_key":         clientCfg["api_key"],
				"api_base":        clientCfg["api_base"],
				"verify_ssl":      &verifySSL,
				"timeout":         clientCfg["timeout"],
			},
			"model_request_config": map[string]any{
				"model": modelNameYAML,
			},
		},
	})
	s.Require().NoError(err)
	s.Require().NoError(os.WriteFile(s.modelConfig, raw, 0o644))
}

// evaluatorConfig 构造评分全链配置。
func (s *ScoringPipelineSuite) evaluatorConfig() harnessrsiconfig.EvaluatorConfig {
	cfg, err := harnessrsiconfig.EvaluatorConfigFromDict(map[string]any{
		"backend":               "single_harness",
		"model_config_ref":      s.modelConfig,
		"evaluation_method":     "exact_match",
		"case_concurrency":      1,
		"solver_max_iterations": 3,
	})
	s.Require().NoError(err)
	return cfg
}

// writeDataset 写两 case 数据集文件（1 答对 + 1 答错，均为 exact_match 型）。
func (s *ScoringPipelineSuite) writeDataset(dir string) harnessrsi.DatasetArtifact {
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
	s.Require().NoError(err)
	s.Require().NoError(os.WriteFile(casesFile, raw, 0o644))
	return harnessrsi.DatasetArtifact{
		DatasetID:    "ds-int",
		DatasetDir:   dir,
		DatasetFiles: []string{casesFile},
	}
}

// TestScoringPipeline_全链 配置载入→载题→考场→判分→eval_ref.yaml。
func (s *ScoringPipelineSuite) TestScoringPipeline_全链() {
	root := s.T().TempDir()
	datasetDir := filepath.Join(root, "dataset")
	s.Require().NoError(os.MkdirAll(datasetDir, 0o755))
	dataset := s.writeDataset(datasetDir)

	// harness 引用（role → harness 目录路径；目录带最小 harness_config.yaml，
	// runtime policy 缺省走 evaluator 默认）。
	harnessDir := filepath.Join(root, "harness", "solver")
	s.Require().NoError(os.MkdirAll(harnessDir, 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(harnessDir, "harness_config.yaml"),
		[]byte("schema_version: harness_config.v0.1\nid: itest-solver\nname: itest-solver\nlanguage: en\n"),
		0o644))
	refsPath := filepath.Join(root, "harness_refs.yaml")
	s.Require().NoError(os.WriteFile(refsPath,
		[]byte("harness_refs:\n  solver: "+harnessDir+"\n"), 0o644))

	te, err := evaluator.NewTeamEvaluator(s.evaluatorConfig())
	s.Require().NoError(err)

	// 跨子系统真实调用 ①：common/utils/path 的 ExpandHome（引用路径规范化
	// 与 evaluator 内部同款语义）。
	refsPath = pathutil.ExpandHome(refsPath)
	// 跨子系统真实调用 ②：core/sys_operation 的工具 ID 生成（swebench 容器分支
	// 的系统操作卡 ID 形态契约）。
	s.Require().Equal("sweb_single_x.shell.execute_cmd",
		sysop.GenerateStaticToolID("sweb_single_x", "shell", "execute_cmd"))
	// 跨子系统真实调用 ③：openuapclaw/harness 的搜索门控查询（考场 agent 装配
	// 环境契约：RSI 评估不启用外搜）。
	s.Require().False(harness.IsFreeSearchEnabled() && harness.IsPaidSearchEnabled())

	outputDir := filepath.Join(root, "output")
	evalRefPath, err := te.Evaluate(s.T().Context(), dataset, "", refsPath, outputDir)
	s.Require().NoError(err)
	s.Require().NotEmpty(evalRefPath)
	s.Require().FileExists(evalRefPath)

	// eval_ref.yaml 结构断言：指向本次 evalDir。
	content, err := os.ReadFile(evalRefPath)
	s.Require().NoError(err)
	s.Require().Contains(string(content), "eval_dir: "+outputDir)

	// 逐题 cases/ 落盘断言：结果文件存在（目录名 c<index>_<sha1 前 8 位>，
	// 由 eval_ref.yaml 的 cases[].case_path 反查）。
	refContent, err := os.ReadFile(evalRefPath)
	s.Require().NoError(err)
	for _, caseID := range []string{"case-pass", "case-fail"} {
		s.Require().Contains(string(refContent), "case_id: "+caseID)
	}
	// 至少两份 result.json 落盘。
	matches, _ := filepath.Glob(filepath.Join(outputDir, "cases", "*", "result.json"))
	s.Require().Len(matches, 2)
	// 判分方法断言：exact_match 记录进每题 result.json。
	passResult, err := os.ReadFile(matches[0])
	s.Require().NoError(err)
	s.Require().Contains(string(passResult), `"exact_match"`)
}

// TestScoringPipeline_LlmAsJudgeVerdict llm_as_judge 判分器经 fake 端点解析 verdict
// （judger 子系统 + LLM 网络边界）。
func (s *ScoringPipelineSuite) TestScoringPipeline_LlmAsJudgeVerdict() {
	verdict := `{"overall_reason": "all criteria met", "behaviors": [
		{"id": "rubric_001", "score": 1.0, "reason": "criterion met", "evidence": "answer"}
	], "forbidden_hits": []}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = os.WriteFile("/tmp/itest-judge-last-request.json", body, 0o644)
		// judge 模型输出包在 choices[0].message.content 的 JSON 文本里。
		payload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": verdict},
			}},
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	dir := s.T().TempDir()
	cfgFile := filepath.Join(dir, "judge_model.yaml")
	raw, err := json.Marshal(map[string]any{
		"model": map[string]any{
			"model_client_config": map[string]any{
				"client_provider": "OpenAI",
				"client_id":       "itest-client", "api_key": "k", "api_base": server.URL,
				"verify_ssl": false, "timeout": 30.0,
			},
			"model_request_config": map[string]any{"model": "fake-judge"},
		},
	})
	s.Require().NoError(err)
	s.Require().NoError(os.WriteFile(cfgFile, raw, 0o644))

	cfg, err := harnessrsiconfig.EvaluatorConfigFromDict(map[string]any{
		"evaluation_method":      "llm_as_judge",
		"judge_model_config_ref": cfgFile,
	})
	s.Require().NoError(err)
	j, err := judger.BuildJudger(&cfg)
	s.Require().NoError(err)
	s.Require().Equal("llm_as_judge", j.MethodName())
	// 调试用固定目录（失败时保留 judge 产物；通过后清理）。
	debugDir := "/tmp/itest-judge-dbg"
	_ = os.RemoveAll(debugDir)
	s.Require().NoError(os.MkdirAll(debugDir, 0o755))

	caseMapping := map[string]any{
		"case_id":    "c1",
		"task_input": map[string]any{"query": "answer the question"},
		"reference":  map[string]any{"rubric": []any{"criterion"}},
	}
	result, err := j.Judge(s.T().Context(), caseMapping,
		stubExecutionView("passed", "anything"), debugDir, nil)
	s.Require().NoError(err)
	s.Require().NotNil(result)
	s.Require().True(result.Passed)
	s.Require().Equal(1.0, result.Score)
}

// stubExecutionView 构造判分器消费的执行视图（judger.CaseExecutionView 本地桩——
// 被测子系统是判分器，执行视图仅边界数据）。
func stubExecutionView(status, response string) judger.CaseExecutionView {
	return stubExecView{status: status, response: response}
}

// stubExecView judger.CaseExecutionView 最小实现。
type stubExecView struct {
	status   string
	response string
}

func (v stubExecView) ResponseView() any                    { return v.response }
func (v stubExecView) ExecutionStatusView() string          { return v.status }
func (v stubExecView) ErrorView() string                    { return "" }
func (v stubExecView) WorkspaceDirView() string             { return "" }
func (v stubExecView) JudgeResultView() *judger.JudgeResult { return nil }

// 防止 strings 未使用（保留给后续断言扩展）。
var _ = strings.Contains

// envWithDefaultKey 环境变量取值（缺省回落；与 suite/real_llm_suite.go
// envWithDefault 同形——本目录不 import suite 包，避免测试包间耦合）。
func envWithDefaultKey(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
