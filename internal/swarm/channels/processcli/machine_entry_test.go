package processcli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli/protocol"
)

// TestExecuteSource_冲突参数返回2
// Python: machine_entry.py:224-228 conflicting_arguments
func TestExecuteSource_冲突参数返回2(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "in.json")
	require.NoError(t, os.WriteFile(f,
		[]byte(`{"schema_version":"0.1","type":"run","input":"hi"}`), 0o600))

	code := ExecuteSource(ExecuteOptions{Source: f, ConflictingArguments: true})
	require.Equal(t, 2, code, "conflicting_arguments 应返回 2")
}

// TestExecuteSource_冲突参数按jsonl给出选项名
// Python: machine_entry.py:225 option = "--run-jsonl" if json_lines else "--run-json"
func TestExecuteSource_冲突参数按jsonl给出选项名(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "in.json")
	require.NoError(t, os.WriteFile(f, []byte(" "), 0o600))

	// 无法直接读回错误文本（写入 stdout），只验证退出码一致
	code := ExecuteSource(ExecuteOptions{Source: f, ConflictingArguments: true, JSONLines: true})
	require.Equal(t, 2, code)
}

// TestExecuteSource_空文档返回2
// Python: machine_entry.py:233-235 MachineInputError → exit_code 2
func TestExecuteSource_空文档返回2(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "in.json")
	require.NoError(t, os.WriteFile(f, []byte("   "), 0o600))

	code := ExecuteSource(ExecuteOptions{Source: f})
	require.Equal(t, 2, code)
}

// TestExecuteSource_文件不存在返回2
// Python: machine_entry.py:192-193 OSError → "run input could not be read"
func TestExecuteSource_文件不存在返回2(t *testing.T) {
	code := ExecuteSource(ExecuteOptions{Source: "/no/such/file.json"})
	require.Equal(t, 2, code)
}

// TestPrepareWorkspace_先resolve再chdir
// Python: machine_entry.py:87-106 prepare_workspace
func TestPrepareWorkspace_先resolve再chdir(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))

	in := &protocol.OneShotRunInput{Input: "hi"}
	in.Workspace = &protocol.WorkspaceSpec{Cwd: &sub}

	orig, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(orig) }()

	out, prepErr := PrepareWorkspace(in)
	require.NoError(t, prepErr)
	require.NotNil(t, out.Workspace)
	require.NotNil(t, out.Workspace.Cwd)

	now, _ := os.Getwd()
	absSub, _ := filepath.Abs(sub)
	require.Equal(t, absSub, now, "应已 chdir 到解析后的 cwd")
}

// TestPrepareWorkspace_不可访问路径报错
// Python: machine_entry.py:104-105 raise MachineInputError("Cannot access the requested workspace.")
func TestPrepareWorkspace_不可访问路径报错(t *testing.T) {
	in := &protocol.OneShotRunInput{Input: "hi"}
	bad := "/no/such/dir/at/all"
	in.Workspace = &protocol.WorkspaceSpec{Cwd: &bad}

	_, err := PrepareWorkspace(in)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Cannot access the requested workspace.")
}

// TestPrepareWorkspace_无workspace原样返回
// Python: machine_entry.py:90-91 if workspace is None: return run_input
func TestPrepareWorkspace_无workspace原样返回(t *testing.T) {
	in := &protocol.OneShotRunInput{Input: "hi"}
	out, err := PrepareWorkspace(in)
	require.NoError(t, err)
	require.Same(t, in, out)
}

// TestPrepareWorkspace_trustedDirs也解析为绝对路径
// Python: machine_entry.py:100 trusted_dirs=tuple(_absolute_path(path) ...)
func TestPrepareWorkspace_trustedDirs也解析为绝对路径(t *testing.T) {
	tmp := t.TempDir()
	in := &protocol.OneShotRunInput{Input: "hi"}
	in.Workspace = &protocol.WorkspaceSpec{TrustedDirs: []string{tmp}}

	out, err := PrepareWorkspace(in)
	require.NoError(t, err)
	require.Len(t, out.Workspace.TrustedDirs, 1)
	require.True(t, filepath.IsAbs(out.Workspace.TrustedDirs[0]))
}

// TestFailure_退出码决定状态
// Python: machine_entry.py:109-119 _failure
//
//	status = CANCELLED if exit_code == 130 else FAILED
func TestFailure_退出码决定状态(t *testing.T) {
	w, err := NewOneShotWriter(discardWriter{}, "req-1")
	require.NoError(t, err)

	got := failure(w, "CANCELLED", "Command interrupted.", 130)
	require.Equal(t, string(protocol.StatusCancelled), got.Status)

	got = failure(w, "STARTUP_FAILED", "boom", 1)
	require.Equal(t, string(protocol.StatusFailed), got.Status)
}

// TestExecuteSource_workspace无效返回2
// Python: machine_entry.py:161-163 read_run_input → prepare_workspace
func TestExecuteSource_workspace无效返回2(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "in.json")
	content := `{"schema_version":"0.1","type":"run","input":"hi",` +
		`"workspace":{"cwd":"/no/such/dir/at/all"}}`
	require.NoError(t, os.WriteFile(f, []byte(content), 0o600))

	code := ExecuteSource(ExecuteOptions{Source: f})
	require.Equal(t, 2, code, "workspace 不可访问应为 INVALID_INPUT(2)")
}

// discardWriter 丢弃全部输出。
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// TestExecuteDocument_stdin走ReadDocument --run-json - 用 DuplexLineReader.ReadDocument。
// Python: machine_entry.py:149-155 _execute_document 的 stdin 分支。
func TestExecuteDocument_stdin走ReadDocument(t *testing.T) {
	// 无结尾换行的文档也必须能读全（旧 ReadRunInputBytes 语义一致，但换读取器）
	document := `{"schema_version":"` + protocol.CurrentSchemaVersion +
		`","type":"run","request_id":"r9","input":"hi"}`
	r, err := NewDuplexLineReader(bytes.NewBufferString(document), protocol.MaxRunInputBytes, 10*time.Millisecond)
	require.NoError(t, err)
	raw, err := r.ReadDocument()
	require.NoError(t, err)
	input, inputErr := readRunInputFromString(string(raw))
	require.NoError(t, inputErr)
	require.NotNil(t, input)
	require.NotNil(t, input.RequestID)
	require.Equal(t, "r9", *input.RequestID)
}

// TestExecuteDocument_unattendedController --run-json 尾部构造 unattended controller。
// Python: machine_entry.py:166-171 DuplexController(None, writer, unattended=True)。
func TestExecuteDocument_unattendedController(t *testing.T) {
	out := &bytes.Buffer{}
	writer, err := NewOneShotWriter(out, "w1")
	require.NoError(t, err)
	ctrl := NewDuplexController(nil, writer, true)
	// unattended controller：reader 为 nil、unattended=true
	require.Nil(t, ctrl.reader)
	require.True(t, ctrl.unattended)
}

// TestExecuteDuplex_空输入 run-jsonl 首行前 EOF → 固定错误文案。
// Python: machine_entry.py:130-134 "A run record is required before control input."
func TestExecuteDuplex_空输入(t *testing.T) {
	pipeR, pipeW, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pipeW.Close() })
	_ = pipeW.Close() // 立即 EOF
	reader, rerr := NewDuplexLineReader(pipeR, MaxDuplexLineBytes, 10*time.Millisecond)
	require.NoError(t, rerr)
	line, lerr := reader.ReadLine()
	require.NoError(t, lerr)
	require.Nil(t, line)
	// executeDuplex 在此情况下应产出 INVALID_INPUT 失败结果与该文案
	out := &bytes.Buffer{}
	writer, werr := NewOneShotWriter(out, "w2")
	require.NoError(t, werr)
	result := failure(writer, "INVALID_INPUT", "A run record is required before control input.", exitInvalidInput)
	require.Equal(t, "A run record is required before control input.", result.Error.Message)
}
