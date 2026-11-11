package processcli

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli/protocol"
)

// TestDecodeMachineDocument_拒空文档
// Python: machine_io.py:92-93 if not document.strip(): raise
func TestDecodeMachineDocument_拒空文档(t *testing.T) {
	_, err := DecodeMachineDocument("   ")
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")
}

// TestDecodeMachineDocument_拒重复key
// Python: machine_io.py:101-103,123-126 duplicate_key
func TestDecodeMachineDocument_拒重复key(t *testing.T) {
	_, err := DecodeMachineDocument(`{"a":1,"a":2}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate JSON keys")
}

// TestDecodeMachineDocument_拒NaN
// Python: machine_io.py:106-108,127-130 non_finite_number
func TestDecodeMachineDocument_拒NaN(t *testing.T) {
	_, err := DecodeMachineDocument(`{"a":NaN}`)
	require.Error(t, err)
	// NaN 是 parse_constant 拦下的非有限数
	require.Contains(t, err.Error(), "finite")
}

// TestDecodeMachineDocument_拒溢出的Inf
// Python: parse_float 拦下 1e999 → inf
func TestDecodeMachineDocument_拒溢出的Inf(t *testing.T) {
	_, err := DecodeMachineDocument(`{"a":1e999}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "finite")
}

// TestDecodeMachineDocument_拒非对象
// Python: machine_io.py:120-121 if not isinstance(value, dict): raise
func TestDecodeMachineDocument_拒非对象(t *testing.T) {
	_, err := DecodeMachineDocument(`[1,2]`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a JSON object")
}

// TestDecodeMachineDocument_语法错
// Python: machine_io.py:116-119
func TestDecodeMachineDocument_语法错(t *testing.T) {
	_, err := DecodeMachineDocument(`{bad json}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "one valid JSON document")
}

// TestDecodeMachineDocument_嵌套重复key也拒绝 验证递归下降覆盖嵌套对象。
// Python: object_pairs_hook 对每一层都生效
func TestDecodeMachineDocument_嵌套重复key也拒绝(t *testing.T) {
	_, err := DecodeMachineDocument(`{"outer":{"a":1,"a":2}}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate JSON keys")
}

// TestDecodeMachineDocument_嵌套数组内的对象也检查 验证数组路径。
func TestDecodeMachineDocument_嵌套数组内的对象也检查(t *testing.T) {
	_, err := DecodeMachineDocument(`{"list":[{"a":1,"a":2}]}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate JSON keys")
}

// TestDecodeMachineDocument_带request_id的错误应回填
// Python: machine_io.py:122,125,129 request_id=_request_id(value)
func TestDecodeMachineDocument_带request_id的错误应回填(t *testing.T) {
	_, err := DecodeMachineDocument(`{"request_id":"req-9","a":1,"a":2}`)
	require.Error(t, err)
	inputErr, ok := err.(*MachineInputError)
	require.True(t, ok, "应为 *MachineInputError")
	require.NotNil(t, inputErr.RequestID)
	require.Equal(t, "req-9", *inputErr.RequestID)
}

// TestReadLimited_超限报错
// Python: machine_io.py:69-80 MAX_RUN_INPUT_BYTES
func TestReadLimited_超限报错(t *testing.T) {
	big := strings.Repeat("a", protocol.MaxRunInputBytes+1)
	_, err := ReadRunInputBytes(strings.NewReader(big))
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds the byte limit")
}

// TestReadLimited_恰好等于上限可通过
// Python: machine_io.py:77 if len(content) + len(encoded) > MAX: raise（严格大于）
func TestReadLimited_恰好等于上限可通过(t *testing.T) {
	exact := strings.Repeat("a", protocol.MaxRunInputBytes)
	got, err := ReadRunInputBytes(strings.NewReader(exact))
	require.NoError(t, err)
	require.Len(t, got, protocol.MaxRunInputBytes)
}

// TestOneShotWriter_写事件后序号递增
// Python: machine_io.py:224-234
func TestOneShotWriter_写事件后序号递增(t *testing.T) {
	var buf strings.Builder
	w, werr := NewOneShotWriter(&buf, "req-1")
	require.NoError(t, werr)
	ev, err := protocol.NewOneShotEvent(protocol.OneShotEventParams{
		Sequence: 0, RequestID: "req-1", EventType: "chat.delta",
	})
	require.NoError(t, err)
	require.NoError(t, w.WriteEvent(ev))
	require.Equal(t, 1, w.Sequence)
	require.Contains(t, buf.String(), "\n")
}

// TestOneShotWriter_写result后禁止再写
// Python: machine_io.py:221-222 if self.final_written: raise ValueError
func TestOneShotWriter_写result后禁止再写(t *testing.T) {
	var buf strings.Builder
	w, werr := NewOneShotWriter(&buf, "req-1")
	require.NoError(t, werr)
	sid := "s1"
	res := &protocol.OneShotRunResult{
		Sequence: 0, RequestID: "req-1",
		Status: "completed", ExitCode: 0, SessionID: &sid,
	}
	require.NoError(t, w.WriteResult(res))
	require.True(t, w.FinalWritten)

	err := w.WriteResult(res)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already been written")
}

// TestOneShotWriter_写失败置broken
// Python: machine_io.py:231-233 except (OSError, ValueError): self.broken = True
func TestOneShotWriter_写失败置broken(t *testing.T) {
	w, werr := NewOneShotWriter(errWriter{}, "req-1")
	require.NoError(t, werr)
	ev, err := protocol.NewOneShotEvent(protocol.OneShotEventParams{
		Sequence: 0, RequestID: "req-1", EventType: "chat.delta",
	})
	require.NoError(t, err)
	require.Error(t, w.WriteEvent(ev))
	require.True(t, w.Broken)
}

// TestOneShotWriter_broken后禁止再写
// Python: machine_io.py:219-220 if self.broken: raise BrokenPipeError
func TestOneShotWriter_broken后禁止再写(t *testing.T) {
	w, werr := NewOneShotWriter(errWriter{}, "req-1")
	require.NoError(t, werr)
	ev, _ := protocol.NewOneShotEvent(protocol.OneShotEventParams{
		Sequence: 0, RequestID: "req-1", EventType: "chat.delta",
	})
	require.Error(t, w.WriteEvent(ev))

	err := w.WriteEvent(ev)
	require.Error(t, err)
	require.Contains(t, err.Error(), "broken")
}

// TestOneShotWriter_空request_id被拒
// Python: machine_io.py:209-210 if not request_id.strip(): raise ValueError
func TestOneShotWriter_空request_id被拒(t *testing.T) {
	w, err := NewOneShotWriter(&strings.Builder{}, "   ")
	require.Error(t, err)
	require.Nil(t, w)
	require.Contains(t, err.Error(), "must not be empty")
}

// TestSafeSchemaReason_映射已知字段
// Python: machine_io.py:134-150 _safe_schema_reason
func TestSafeSchemaReason_映射已知字段(t *testing.T) {
	require.Equal(t, "schema_version is not supported",
		safeSchemaReason(errString("unsupported schema_version: 9.9")))
	require.Equal(t, "mode is not supported",
		safeSchemaReason(errString("unsupported single-Agent mode: team.normal")))
	require.Equal(t, "run contains unknown fields",
		safeSchemaReason(errString("run contains unknown fields: bogus")))
	require.Equal(t, "agent is missing required fields",
		safeSchemaReason(errString("agent is missing required fields: name")))
	require.Equal(t, "input is invalid",
		safeSchemaReason(errString("input must not be empty")))
	require.Equal(t, "run input does not match the schema",
		safeSchemaReason(errString("something else entirely")))
}

// errWriter 恒失败的 writer。
type errWriter struct{}

func (errWriter) Write(p []byte) (int, error) { return 0, os.ErrClosed }

// errString 把字符串包装成 error。
type errString string

func (e errString) Error() string { return string(e) }

// TestOutputError_写入失败是OutputError
// Python: machine_io.py:229 raise OSError("machine output write was incomplete")
//
// 两条失败路径（write 报错、写入长度不足）都必须可被 errors.As 识别，
// 否则 machine.go 的 `except OSError` 等价分支会落空。
func TestOutputError_写入失败是OutputError(t *testing.T) {
	w, err := NewOneShotWriter(errWriter{}, "req-1")
	require.NoError(t, err)
	ev, err := protocol.NewOneShotEvent(protocol.OneShotEventParams{
		Sequence: 0, RequestID: "req-1", EventType: "chat.delta",
	})
	require.NoError(t, err)

	writeErr := w.WriteEvent(ev)
	require.Error(t, writeErr)
	var outputErr *OutputError
	require.ErrorAs(t, writeErr, &outputErr, "写失败必须包成 OutputError")
	require.True(t, w.Broken)
}

// shortWriter 声称写成功但长度不足。
type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }

// TestOutputError_写入长度不足也是OutputError
// Python: machine_io.py:228-229 if written != len(line): raise OSError(...)
func TestOutputError_写入长度不足也是OutputError(t *testing.T) {
	w, err := NewOneShotWriter(shortWriter{}, "req-1")
	require.NoError(t, err)
	ev, err := protocol.NewOneShotEvent(protocol.OneShotEventParams{
		Sequence: 0, RequestID: "req-1", EventType: "chat.delta",
	})
	require.NoError(t, err)

	writeErr := w.WriteEvent(ev)
	require.Error(t, writeErr)
	var outputErr *OutputError
	require.ErrorAs(t, writeErr, &outputErr, "长度不足也必须包成 OutputError")
}

// TestOutputError_Unwrap保留底层
func TestOutputError_Unwrap保留底层(t *testing.T) {
	wrapped := &OutputError{Err: os.ErrClosed}
	require.ErrorIs(t, wrapped, os.ErrClosed)
	require.NotEmpty(t, wrapped.Error())
}
