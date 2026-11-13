//go:build integration

package processcli

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"os"
	"path/filepath"
	"strings"
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
		"UAPCLAW_DATA_DIR":     dataDir,
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
