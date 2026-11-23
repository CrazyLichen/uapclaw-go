//go:build integration

// 17.10 验收点③：judge 解析容错。llm_as_judge 判分器经 fake 端点收畸形
// verdict：JSON 围栏包裹 / 畸形 JSON 触发 format repair / 修复仍失败 /
// 超时。恢复语义对齐 Python tests/unit_tests/rsi/test_json_format_recovery.py、
// test_judge_assessment_retry.py、test_judge_attempt_timeout.py。
//
// 跨子系统 ≥3：judger（判分器真身）+ evaluator（配置/执行视图契约）+
// model 调用链（RunJudgeAgent→模型客户端，fake HTTP 边界）+ harness_rsi
// （RunModelCallWithRetries 重试守卫）。

package rsi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	harnessrsiconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/config"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator/judger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// JudgeParseToleranceSuite judge 解析容错套件。
type JudgeParseToleranceSuite struct{ suite.Suite }

// accStubExecView judger.CaseExecutionView 最小实现（复刻父目录 stubExecView
// ——非导出符号跨包不可见；被测子系统是判分器，执行视图仅边界数据）。
type accStubExecView struct {
	status   string
	response string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestJudgeParseToleranceSuite 入口。
func TestJudgeParseToleranceSuite(t *testing.T) { suite.Run(t, new(JudgeParseToleranceSuite)) }

// ResponseView 实际回答。
func (v accStubExecView) ResponseView() any { return v.response }

// ExecutionStatusView 执行状态。
func (v accStubExecView) ExecutionStatusView() string { return v.status }

// ErrorView 错误描述。
func (v accStubExecView) ErrorView() string { return "" }

// WorkspaceDirView 执行工作区。
func (v accStubExecView) WorkspaceDirView() string { return "" }

// JudgeResultView 既有判分结果。
func (v accStubExecView) JudgeResultView() *judger.JudgeResult { return nil }

// ──────────────────────────── 非导出函数 ────────────────────────────

// accJudgeVerdict 合法 verdict（含 behaviors/forbidden_hits 全契约字段）。
func accJudgeVerdict(score float64) string {
	return fmt.Sprintf(`{"overall_reason": "all criteria met", "behaviors": [
		{"id": "rubric_001", "score": %v, "reason": "criterion met", "evidence": "answer"}
	], "forbidden_hits": []}`, score)
}

// accJudgeServer 起 fake 端点：按请求序弹出 content 序列（超额回落最后一个）。
func accJudgeServer(t *testing.T, contents []string, delayMs int) *httptest.Server {
	t.Helper()
	i := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		content := contents[len(contents)-1]
		if i < len(contents) {
			content = contents[i]
		}
		i++
		if delayMs > 0 {
			time.Sleep(time.Duration(delayMs) * time.Millisecond)
		}
		payload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			}},
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
}

// accBuildJudge 构造 llm_as_judge 判分器（judge_timeout_sec/judge_max_retries
// 可调，供超时场景收紧）。
func (s *JudgeParseToleranceSuite) accBuildJudge(apiBase string, timeoutSec, maxRetries int) judger.EvaluationJudger {
	dir := s.T().TempDir()
	cfgFile := filepath.Join(dir, "judge_model.yaml")
	raw, err := json.Marshal(map[string]any{
		"model": map[string]any{
			"model_client_config": map[string]any{
				"client_provider": "OpenAI", "client_id": "acc-client",
				"api_key": "k", "api_base": apiBase,
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
		"judge_timeout_sec":      timeoutSec,
		"judge_max_retries":      maxRetries,
	})
	s.Require().NoError(err)
	j, err := judger.BuildJudger(&cfg)
	s.Require().NoError(err)
	s.Require().Equal("llm_as_judge", j.MethodName())
	return j
}

// accCase 标准判分用例。
func accCase() map[string]any {
	return map[string]any{
		"case_id":    "c1",
		"task_input": map[string]any{"query": "answer the question"},
		"reference":  map[string]any{"rubric": []any{"criterion"}},
	}
}

// TestJudgeParse_围栏包裹_直接恢复 verdict 包在 Markdown 围栏里：判分器解析
// 成功（json_format_recovery），考生不被误判。
func (s *JudgeParseToleranceSuite) TestJudgeParse_围栏包裹_直接恢复() {
	fenced := "```json\n" + accJudgeVerdict(1.0) + "\n```"
	server := accJudgeServer(s.T(), []string{fenced}, 0)
	s.T().Cleanup(server.Close)

	j := s.accBuildJudge(server.URL, 30, 1)
	out := s.T().TempDir()
	result, err := j.Judge(s.T().Context(), accCase(), accStubExecView{status: "passed", response: "REPLY"}, out, nil)
	s.Require().NoError(err)
	s.Require().NotNil(result)
	s.Require().Equal(1.0, result.Score)
	s.Require().True(result.Passed)
}

// TestJudgeParse_畸形verdict_修复恢复 畸形 JSON 触发 format repair
// （第二次调用返回合法 verdict）→ 恢复成功、分数在案。
func (s *JudgeParseToleranceSuite) TestJudgeParse_畸形verdict_修复恢复() {
	malformed := `{"overall_reason": "ok", "behaviors": [{"id": "rubric_001", "score": 1.0`
	server := accJudgeServer(s.T(), []string{malformed, accJudgeVerdict(1.0)}, 0)
	s.T().Cleanup(server.Close)

	j := s.accBuildJudge(server.URL, 30, 1)
	out := s.T().TempDir()
	result, err := j.Judge(s.T().Context(), accCase(), accStubExecView{status: "passed", response: "REPLY"}, out, nil)
	s.Require().NoError(err)
	s.Require().NotNil(result)
	s.Require().Equal(1.0, result.Score)

	// 修复留痕：judge 目录下有 format_repair 产物（Python format_repair 留档）。
	matches, _ := filepath.Glob(filepath.Join(out, "judge", "*", "format_repair_*.json"))
	s.Require().NotEmpty(matches, "format repair 未留档")
}

// TestJudgeParse_修复仍失败_结构化拒绝 修复也失败 → 结构化错误（不 panic、
// 不给考生打分），validation_error 留痕。
func (s *JudgeParseToleranceSuite) TestJudgeParse_修复仍失败_结构化拒绝() {
	garbage := `not a json at all {{{`
	server := accJudgeServer(s.T(), []string{garbage}, 0)
	s.T().Cleanup(server.Close)

	j := s.accBuildJudge(server.URL, 30, 0)
	out := s.T().TempDir()
	result, err := j.Judge(s.T().Context(), accCase(), accStubExecView{status: "passed", response: "REPLY"}, out, nil)
	s.Require().Error(err, "修复仍失败必须结构化报错")
	s.Require().Nil(result, "失败时不得返回打分结果")
	s.Require().True(strings.Contains(err.Error(), "repair") || strings.Contains(err.Error(), "format") ||
		strings.Contains(err.Error(), "Invalid") || strings.Contains(err.Error(), "JSON"),
		"错误应指向格式问题：%v", err)
}

// TestJudgeParse_超时走重试收尾 judge 端点挂起 → attempt 超时 → 按重试预算
// 收敛到结构化失败（对齐 test_judge_attempt_timeout.py）。
func (s *JudgeParseToleranceSuite) TestJudgeParse_超时走重试收尾() {
	server := accJudgeServer(s.T(), []string{accJudgeVerdict(1.0)}, 3000)
	s.T().Cleanup(server.Close)

	// timeout 1s（端点 3s 才响应必超时）、重试预算 1。
	j := s.accBuildJudge(server.URL, 1, 1)
	out := s.T().TempDir()
	start := time.Now()
	result, err := j.Judge(s.T().Context(), accCase(), accStubExecView{status: "passed", response: "REPLY"}, out, nil)
	elapsed := time.Since(start)

	s.Require().Error(err, "超时必须结构化报错")
	s.Require().Nil(result)
	s.Require().Less(elapsed, 2*time.Minute, "重试预算必须收敛，不得无限等待")
}
