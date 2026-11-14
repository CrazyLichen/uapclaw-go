//go:build integration

package processcli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DuplexJSONLSuite --run-jsonl 双工通道端到端套件（真二进制 + 真 stdin/stdout）。
//
// 范围说明：本套件覆盖 duplex 链路里**确定性**可断言的边界——
// 行读取器、run 记录解码、cancel 控制帧的整条子进程路径
// （executeDuplex ↔ DuplexLineReader ↔ DuplexController ↔ machine 取消）。
// 交互问答闭环（interaction.requested → answer → answer 流）需要真实模型
// 产出 ask_user_question 事件，真二进制子进程无法注入 MockLLM；
// 该闭环由 duplex_control_test.go 的 fake answerStreamer 单测覆盖
// （TestDuplexController_交互登记与answer回填 等 9 例）。
type DuplexJSONLSuite struct {
	CLISuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDuplexJSONLSuite 运行本套件（复用 CLISuite 的二进制构建）。
func TestDuplexJSONLSuite(t *testing.T) {
	suite.Run(t, new(DuplexJSONLSuite))
}

// ──────────────────────────── 用例 ────────────────────────────

// TestRunJsonl_首行前EOF stdin 立即 EOF：exit 2 + 固定文案。
// Python: machine_entry.py:130-135 read_line() is None →
// "A run record is required before control input."
func (s *DuplexJSONLSuite) TestRunJsonl_首行前EOF() {
	res := s.run(strings.NewReader(""), "--run-jsonl")

	s.Equal(2, res.exitCode, "EOF 无 run 记录应退出 2")
	records := decodeJSONLines(s.T(), res.stdout)
	s.Require().Len(records, 1)
	s.Equal("result", records[0]["type"])
	s.Equal("INVALID_INPUT", digString(records[0], "error", "code"))
	s.Equal("A run record is required before control input.",
		digString(records[0], "error", "message"))
}

// TestRunJsonl_首行非法 首行不是合法 run 记录：行读取器 → 解码拒绝。
// Python: machine_entry.py:133-135 read_run_input 失败 → INVALID_INPUT。
func (s *DuplexJSONLSuite) TestRunJsonl_首行非法() {
	res := s.run(strings.NewReader("{not json}\n"), "--run-jsonl")

	s.Equal(2, res.exitCode, "非法 run 记录应退出 2")
	records := decodeJSONLines(s.T(), res.stdout)
	s.Require().NotEmpty(records)
	s.Equal("INVALID_INPUT", digString(records[0], "error", "code"))
}

// TestRunJsonl_cancel取消 run 记录 + cancel 帧同批写入：控制路由终止，
// 退出码 130、status=cancelled。
// Python: machine_entry.py:140-143（attended controller）+
// duplex_control.py:128-134（cancel → CANCELLED/130）。
//
// 确定性手段：本用例单独 dataDir + config.yaml，默认模型指向一个阻塞的
// 本地 HTTP 服务——Runtime 流被模型调用挂起数分钟，cancel 帧（毫秒级
// 已缓冲在 stdin）必然先于任何流事件被控制路由处理。
func (s *DuplexJSONLSuite) TestRunJsonl_cancel取消() {
	// 阻塞的假模型服务：收到请求后挂起（不响应）
	release := make(chan struct{})
	defer close(release)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-time.After(120 * time.Second):
		}
	}))
	defer server.Close()

	dataDir := s.T().TempDir()
	// 新格式 defaults 列表（registry 装配）+ 旧格式 default 兜底
	//（deep_adapter.go defaultModelClientConfig 直读 models.default.model_client_config）
	configYaml := "models:\n" +
		"  defaults:\n" +
		"    - name: \"test-model\"\n" +
		"      model_client_config:\n" +
		"        client_provider: \"OpenAI\"\n" +
		"        api_base: \"" + server.URL + "\"\n" +
		"        api_key: \"test-key\"\n" +
		"        model_name: \"test-model\"\n" +
		"        verify_ssl: false\n" +
		"      is_default: true\n" +
		"  default:\n" +
		"    model_client_config:\n" +
		"      client_provider: \"OpenAI\"\n" +
		"      api_base: \"" + server.URL + "\"\n" +
		"      api_key: \"test-key\"\n" +
		"      model_name: \"test-model\"\n" +
		"      verify_ssl: false\n"
	// ConfigDir = <dataDir>/config/（paths.go ConfigDir：已初始化 → ~/.uapclaw/config 同构）
	s.Require().NoError(os.MkdirAll(filepath.Join(dataDir, "config"), 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(dataDir, "config", "config.yaml"), []byte(configYaml), 0o600))

	input := `{"schema_version":"0.1","type":"run","input":"hi"}` + "\n" +
		`{"schema_version":"0.1","type":"cancel","request_id":"x"}` + "\n"

	// 与 s.run 相同的进程装配，但 dataDir 指向带模型配置的隔离目录
	res := s.runWithEnv(map[string]string{
		"UAPCLAW_DATA_DIR":    dataDir,
		"UAPCLAW_CONFIG_YAML": filepath.Join(dataDir, "config", "config.yaml"),
	}, strings.NewReader(input), "--run-jsonl")

	s.Equal(130, res.exitCode, "cancel 控制帧应使进程退出 130")
	records := decodeJSONLines(s.T(), res.stdout)
	s.Require().NotEmpty(records)
	last := records[len(records)-1]
	s.Equal("result", last["type"])
	s.Equal("cancelled", last["status"], "status 应为 cancelled")
	s.Equal("CANCELLED", digString(last, "error", "code"))
}

// TestRunJsonl_问答闭环 链路级集成（2026-11-13 补）：脚本化假模型第一轮返回
// ask_user 工具调用 → StructuredAskUserRail 中断 → chat.ask_user_question 事件
// → duplex 路由 interaction.requested → 宿主（本测试）回 answer 帧
// → stream_interaction_answer 恢复 → 第二轮假模型返回文本 → 终态 success/0。
//
// 覆盖整条宿主↔子进程双工链路：stdin 控制帧（DuplexLineReader→DecodeControl
// →readControls→InteractionAnswerInput）↔ stdout 事件流（OneShotWriter
// ←consume/notice）↔ Runtime（StreamInteractionAnswer 恢复中断轮）。
// Python: machine_entry.py:122-146 _execute_duplex +
// duplex_control.py:315-381 consume 交互分支 + :144-154 answer 路由。
func (s *DuplexJSONLSuite) TestRunJsonl_问答闭环() {
	// 脚本化假模型：第 1 次请求返回 ask_user 工具调用，其后每次返回文本。
	var reqCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		s.Require().True(ok)
		if reqCount.Add(1) == 1 {
			askArgs := strings.ReplaceAll(`{"query":"继续吗"}`, `"`, `\"`)
			for _, frame := range []string{
				`data: {"choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
				`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-ask","type":"function","function":{"name":"ask_user","arguments":"` + askArgs + `"}}]},"finish_reason":null}]}`,
				`data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
				"data: [DONE]",
			} {
				_, _ = fmt.Fprintln(w, frame)
				flusher.Flush()
			}
			return
		}
		for _, frame := range []string{
			`data: {"choices":[{"index":0,"delta":{"content":"已回答"},"finish_reason":null}]}`,
			`data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
			"data: [DONE]",
		} {
			_, _ = fmt.Fprintln(w, frame)
			flusher.Flush()
		}
	}))
	defer server.Close()

	// 与 cancel 用例相同的隔离 dataDir + config.yaml（模型指向假服务）
	dataDir := "/tmp/qa-dbg-dataDir" // 临时调试：固定目录看 full.log
	_ = os.RemoveAll(dataDir)
	modelBlock := "    - name: \"test-model\"\n" +
		"      model_client_config:\n" +
		"        client_provider: \"OpenAI\"\n" +
		"        api_base: \"" + server.URL + "\"\n" +
		"        api_key: \"test-key\"\n" +
		"        model_name: \"test-model\"\n" +
		"        verify_ssl: false\n" +
		"      is_default: true\n"
	configYaml := "models:\n" +
		"  defaults:\n" + modelBlock +
		"  default:\n" +
		"    model_client_config:\n" +
		"      client_provider: \"OpenAI\"\n" +
		"      api_base: \"" + server.URL + "\"\n" +
		"      api_key: \"test-key\"\n" +
		"      model_name: \"test-model\"\n" +
		"      verify_ssl: false\n"
	s.Require().NoError(os.MkdirAll(filepath.Join(dataDir, "config"), 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(dataDir, "config", "config.yaml"), []byte(configYaml), 0o600))

	// 交互式运行：stdout 逐行读，见到 interaction.requested 后回 answer 帧
	cmd := exec.Command(s.binPath, "--run-jsonl")
	env := append(os.Environ(),
		"UAPCLAW_CONFIG=",
		"UAPCLAW_DATA_DIR="+dataDir,
		"UAPCLAW_CONFIG_YAML="+filepath.Join(dataDir, "config", "config.yaml"),
	)
	cmd.Env = env
	stdin, stdinErr := cmd.StdinPipe()
	s.Require().NoError(stdinErr)
	stdout, stdoutErr := cmd.StdoutPipe()
	s.Require().NoError(stdoutErr)
	var stderrBuf strings.Builder
	dbgF, _ := os.OpenFile("/tmp/pqa-child-stderr.log", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644) // 临时调试
	defer dbgF.Close()
	cmd.Stderr = io.MultiWriter(&stderrBuf, dbgF)
	s.Require().NoError(cmd.Start())

	writeLine := func(line string) {
		_, werr := io.WriteString(stdin, line+"\n")
		s.Require().NoError(werr)
	}
	writeLine(`{"schema_version":"0.1","type":"run","input":"hi","request_id":"req-qa"}`)

	// 读 stdout：找 interaction.requested（含 interaction_id 与 session_id）
	var records []map[string]any
	var rawLines []string
	var interactionID, sessionID string
	found := false
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	deadline := time.Now().Add(60 * time.Second)
	for !found && scanner.Scan() && time.Now().Before(deadline) {
		line := scanner.Text()
		rawLines = append(rawLines, line)
		if !strings.Contains(line, "interaction.requested") {
			continue
		}
		var record map[string]any
		if jsonErr := json.Unmarshal([]byte(line), &record); jsonErr != nil {
			continue
		}
		records = append(records, record)
		interactionID = digString(record, "payload", "interaction_id")
		sessionID = digString(record, "session_id")
		found = interactionID != ""
	}
	s.Require().True(found, "60s 内未收到 interaction.requested；stderr=%s；已读 stdout 行：\n%s",
		stderrBuf.String(), strings.Join(rawLines, "\n"))

	// 宿主回答：answer 帧（request_id 与 run 记录一致；session_id 取自事件流）
	// ask_user 答案契约：question 文本必须与卡片问题一致（buildII/MatchAnswers 按问题文本归属）。
	answer := fmt.Sprintf(`{"schema_version":"0.1","type":"answer","request_id":"req-qa","session_id":%q,"interaction_id":%q,"answers":[{"question":"继续吗","selected_options":["continue"]}]}`,
		sessionID, interactionID)
	writeLine(answer)

	// 等终态 result
	var last map[string]any
	for scanner.Scan() {
		var record map[string]any
		if jsonErr := json.Unmarshal(scanner.Bytes(), &record); jsonErr != nil {
			continue
		}
		records = append(records, record)
		if record["type"] == "result" {
			last = record
		}
	}
	waitErr := cmd.Wait()
	exitCode := 0
	if waitErr != nil {
		exitErr, ok := waitErr.(*exec.ExitError)
		s.Require().True(ok, "非退出码错误: %v", waitErr)
		exitCode = exitErr.ExitCode()
	}

	s.Require().NotNil(last, "未收到终态 result；stdout 记录=%d", len(records))
	// 临时调试：打印终态记录全文
	dbg, _ := json.MarshalIndent(last, "", "  ")
	fmt.Println("DEBUG final result:", string(dbg))
	fmt.Println("DEBUG stderr:", stderrBuf.String())
	s.Equal(0, exitCode, "问答闭环应正常退出 0")
	s.Equal("success", last["status"])
	s.Equal(2, int(reqCount.Load()), "假模型应被调用两次（提问轮 + 应答轮）")
}

// runWithEnv 在 CLISuite.run 基础上叠加环境变量（隔离 dataDir 用）。
func (s *DuplexJSONLSuite) runWithEnv(extra map[string]string, stdin *strings.Reader, args ...string) cliResult {
	cmd := exec.Command(s.binPath, args...)
	env := append(os.Environ(), "UAPCLAW_CONFIG=")
	for key, value := range extra {
		env = append(env, key+"="+value)
	}
	cmd.Env = env
	cmd.Stdin = stdin
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	code := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		s.Require().True(ok, "非退出码错误: %v", err)
		code = exitErr.ExitCode()
	}
	return cliResult{stdout: outBuf.String(), stderr: errBuf.String(), exitCode: code}
}
