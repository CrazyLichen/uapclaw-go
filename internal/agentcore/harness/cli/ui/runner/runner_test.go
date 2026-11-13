package runner

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/agent"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// captureStdout 截获 stdout。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = original
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// TestOutputJSON_累计与兜底 对齐 runner.py:86-110。
func TestOutputJSON_累计与兜底(t *testing.T) {
	cfg := agent.CLIConfig{Model: "m1"}
	chunks := []stream.OutputSchema{
		{Type: "llm_output", Payload: map[string]any{"content": "你好"}},
		{Type: "llm_output", Payload: map[string]any{"content": "世界"}},
		{Type: "answer", Payload: map[string]any{"content": "不应计入"}},
	}
	got := captureStdout(t, func() {
		require.Equal(t, 0, outputJSON(chunks, cfg))
	})
	assert.Contains(t, got, `"result": "你好世界"`)
	assert.Contains(t, got, `"chunks": 3`)
	assert.Contains(t, got, `"model": "m1"`)
	// 无 llm_output 时 answer 兜底
	got = captureStdout(t, func() {
		require.Equal(t, 0, outputJSON([]stream.OutputSchema{
			{Type: "answer", Payload: map[string]any{"content": "兜底"}},
		}, cfg))
	})
	assert.Contains(t, got, `"result": "兜底"`)
}

// TestOutputStreamJSON_逐行 对齐 runner.py:112-128。
func TestOutputStreamJSON_逐行(t *testing.T) {
	chunks := []stream.OutputSchema{
		{Type: "llm_output", Index: 0, Payload: map[string]any{"content": "a"}},
		{Type: "message", Index: 1, Payload: "字符串载荷"},
		{Type: "tool_call", Index: 2, Payload: 42}, // 非串非 map → str
	}
	got := captureStdout(t, func() {
		require.Equal(t, 0, outputStreamJSON(chunks))
	})
	lines := strings.Split(strings.TrimSpace(got), "\n")
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], `"type":"llm_output"`)
	assert.Contains(t, lines[1], `"payload":"字符串载荷"`)
	assert.Contains(t, lines[2], `"payload":"42"`)
}

// TestPrintError_分类提示 对齐 runner.py:130-150。
func TestPrintError_分类提示(t *testing.T) {
	cases := []struct {
		message string
		want    string
	}{
		{"rate_limit exceeded", "Rate limited"},
		{"HTTP 429", "Rate limited"},
		{"authentication failed", "API Key invalid"},
		{"HTTP 401", "API Key invalid"},
		{"prompt too long", "Context too long"},
		{"request timeout", "Request timed out"},
		{"mystery failure", "Error: mystery failure"},
	}
	for _, testCase := range cases {
		got := captureStderr(t, func() { PrintError(assertErrorOf(testCase.message)) })
		assert.Contains(t, got, testCase.want, testCase.message)
		assert.Contains(t, got, "✗")
	}
}

// captureStderr 截获 stderr。
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	fn()
	_ = w.Close()
	os.Stderr = original
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// errOf 构造 error。
func assertErrorOf(message string) error { return errorStub{message} }

type errorStub struct{ message string }

func (e errorStub) Error() string { return e.message }

// TestRunOnce_未知格式 对齐 runner.py:70-73。
func TestRunOnce_未知格式(t *testing.T) {
	code := captureStderrCode(t, func() int {
		return RunOnce(agent.CLIConfig{}, "查询", "yaml")
	})
	assert.Equal(t, 1, code)
}

// captureStderrCode 截获 stderr 并取返回值。
func captureStderrCode(t *testing.T, fn func() int) int {
	t.Helper()
	original := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	code := fn()
	_ = w.Close()
	os.Stderr = original
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	assert.Contains(t, buf.String(), "Unknown output format")
	return code
}
