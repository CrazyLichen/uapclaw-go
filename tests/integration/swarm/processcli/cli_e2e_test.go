//go:build integration

package processcli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CLISuite uapclaw-process 真实二进制端到端套件。
//
// 每个用例都启动真实子进程：构建一次二进制后复用，stdin/stdout/stderr
// 全部真实管道，退出码取 ProcessState。数据目录指向 TempDir，避免污染
// 开发者本机的 ~/.uapclaw。
type CLISuite struct {
	isuite.BaseIntegrationSuite
	// binPath 构建好的 uapclaw-process 二进制路径
	binPath string
	// dataDir 本套件专用的数据目录
	dataDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCLISuite 运行本套件。
func TestCLISuite(t *testing.T) {
	suite.Run(t, new(CLISuite))
}

// SetupSuite 构建二进制并准备隔离的数据目录。
func (s *CLISuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
	s.binPath = filepath.Join(s.T().TempDir(), "uapclaw-process")
	build := exec.Command("go", "build", "-o", s.binPath, "./cmd/uapclaw-process/")
	build.Dir = repoRoot(s.T())
	if out, err := build.CombinedOutput(); err != nil {
		s.T().Fatalf("构建 uapclaw-process 失败: %v\n%s", err, out)
	}
	s.dataDir = s.T().TempDir()
}

// TestHelp 帮助信息走 exit 0，且列出关键参数。
// Python: main.py argparse 的 action="help" 语义。
func (s *CLISuite) TestHelp() {
	res := s.run(nil, "--help")
	s.Equal(0, res.exitCode, "帮助应退出 0")
	s.Contains(res.stdout, "用法：")
	s.Contains(res.stdout, "--run-json", "帮助应列出 --run-json")
	s.Contains(res.stdout, "--query-json", "帮助应列出 --query-json")
}

// TestArgError 未知参数走 exit 2 + 中文错误文案。
// Python: main.py:84-120 _translate_argparse_error 的「无法识别的参数」。
func (s *CLISuite) TestArgError() {
	res := s.run(nil, "--bogus-flag")
	s.Equal(2, res.exitCode, "参数错误应退出 2")
	s.Contains(res.stderr, "无法识别的参数：--bogus-flag")
}

// TestListSessions 空数据目录下输出「没有匹配」且退出 0。
// Python: main.py:343-350 会话查询分支。
func (s *CLISuite) TestListSessions() {
	res := s.run(nil, "--list-sessions")
	s.Equal(0, res.exitCode, "查询无结果也应退出 0")
	s.Contains(res.stdout, "没有匹配的 Process CLI 会话")
}

// TestRunJSON_未配模型 真实 run 输入走到模型检查，输出 event+result 两行。
//
// 断言的确定性来自「未配置模型」这一环境：模型检查在装配链之后，
// 因此本用例同时锁住了 P-7 的装配链（manager 未注入会在更早处报
// "agent runtime manager is nil"，而不是 chat.error）。
// Python: main.py:378-388 --run-json 分支 + machine.py 的 once 执行。
func (s *CLISuite) TestRunJSON_未配模型() {
	input := `{"schema_version":"0.1","type":"run","input":"hi"}`
	res := s.run(strings.NewReader(input), "--run-json", "-")

	s.Equal(1, res.exitCode, "运行失败应退出 1")
	records := decodeJSONLines(s.T(), res.stdout)
	// 20.4 起 --run-json 走 unattended DuplexController：consume 先发
	// run.started 通知（Python machine_entry.py:171 同款），记录数为 3。
	s.Require().Len(records, 3, "应输出 run.started + chat.error + result 三条记录")

	event := records[0]
	s.Equal("event", event["type"])
	s.Equal("run.started", event["event_type"], "unattended consume 先发 run.started")

	errEvent := records[1]
	s.Equal("event", errEvent["type"])
	s.Equal("chat.error", errEvent["event_type"], "未配模型应产出 chat.error")
	s.Contains(dumpJSON(errEvent), "模型未正确配置", "事件应带模型未配置的说明")

	result := records[2]
	s.Equal("result", result["type"])
	s.Equal("failed", result["status"])
	s.Equal(float64(1), result["exit_code"], "result 的 exit_code 应与进程退出码一致")
	s.Equal("RUNTIME_ERROR", digString(result, "error", "code"))
}

// TestRunJSON_未知字段被拒 输入带多余字段时按字段级 details 拒绝。
// Python: protocol/schema.py 的 unknown_field 校验。
func (s *CLISuite) TestRunJSON_未知字段被拒() {
	input := `{"schema_version":"0.1","type":"run","input":"hi","query":"多余字段"}`
	res := s.run(strings.NewReader(input), "--run-json", "-")

	s.Equal(2, res.exitCode, "非法输入应退出 2")
	records := decodeJSONLines(s.T(), res.stdout)
	s.Require().NotEmpty(records)
	s.Equal("INVALID_INPUT", digString(records[0], "error", "code"))
	s.Equal("unknown_field", digString(records[0], "error", "details", "reason"))
}

// TestRunJSON_缺必需字段 缺 type 时按 required 拒绝。
func (s *CLISuite) TestRunJSON_缺必需字段() {
	input := `{"schema_version":"0.1","input":"hi"}`
	res := s.run(strings.NewReader(input), "--run-json", "-")

	s.Equal(2, res.exitCode, "非法输入应退出 2")
	records := decodeJSONLines(s.T(), res.stdout)
	s.Require().NotEmpty(records)
	s.Equal("INVALID_INPUT", digString(records[0], "error", "code"))
	s.Equal("required", digString(records[0], "error", "details", "reason"))
}

// TestQueryJSON_非法输入 query_result 契约下同样按 INVALID_INPUT 拒绝。
// Python: main.py:352-366 --query-json 分支 + query.py 的执行层。
func (s *CLISuite) TestQueryJSON_非法输入() {
	dir := s.T().TempDir()
	src := filepath.Join(dir, "query.json")
	s.Require().NoError(os.WriteFile(src, []byte(`{"query":"hi"}`), 0o600))

	res := s.run(nil, "--query-json", src)
	s.Equal(2, res.exitCode, "非法输入应退出 2")
	records := decodeJSONLines(s.T(), res.stdout)
	s.Require().NotEmpty(records)
	s.Equal("query_result", records[0]["type"], "query 分支产出 query_result")
	s.Equal("INVALID_INPUT", digString(records[0], "error", "code"))
}

// TestRunJSONL 逐行读取，非法行得到各自的 result。
// Python: main.py:367-377 --run-jsonl 分支。
func (s *CLISuite) TestRunJSONL() {
	input := `{"schema_version":"0.1","input":"hi"}` + "\n"
	res := s.run(strings.NewReader(input), "--run-jsonl")

	s.Equal(2, res.exitCode, "非法输入应退出 2")
	records := decodeJSONLines(s.T(), res.stdout)
	s.Require().NotEmpty(records)
	s.Equal("INVALID_INPUT", digString(records[0], "error", "code"))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// cliResult 一次子进程执行的真实结果。
type cliResult struct {
	stdout   string
	stderr   string
	exitCode int
}

// run 以真实子进程执行命令，stdin 可为 nil。
func (s *CLISuite) run(stdin *strings.Reader, args ...string) cliResult {
	cmd := exec.Command(s.binPath, args...)
	cmd.Env = append(os.Environ(),
		"UAPCLAW_DATA_DIR="+s.dataDir,
		// 清掉可能存在的模型配置，让「未配模型」成为确定性行为
		"UAPCLAW_CONFIG=",
	)
	if stdin != nil {
		cmd.Stdin = stdin
	} else {
		cmd.Stdin = strings.NewReader("")
	}
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

// repoRoot 定位仓库根（go.mod 所在目录）。
func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		t.Fatal("不在 Go 模块内")
	}
	return filepath.Dir(gomod)
}

// decodeJSONLines 把输出按行解析为 JSON 对象，跳过空行。
func decodeJSONLines(t *testing.T, out string) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("解析 JSON 行失败: %v\n行内容: %s", err, line)
		}
		records = append(records, rec)
	}
	return records
}

// digString 按路径取嵌套字符串字段。
func digString(m map[string]any, path ...string) string {
	cur := any(m)
	for _, key := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = obj[key]
	}
	s, _ := cur.(string)
	return s
}

// dumpJSON 把对象重新编码成字符串，便于整体包含断言。
func dumpJSON(m map[string]any) string {
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}
