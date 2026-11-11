package protocol

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// validResult 构造一条合法 completed result。
func validResult(seq int, sid string) *OneShotRunResult {
	id := sid
	r, err := NewOneShotRunResult(OneShotRunResultParams{
		Sequence: seq, RequestID: "r1", Status: string(StatusCompleted),
		ExitCode: 0, SessionID: &id,
	})
	if err != nil {
		panic(err)
	}
	return r
}

// TestValidateOneShotRecords_必须以result结尾
// Python: jsonl.py:66-72 result_indexes 必须恰好是最后一条
func TestValidateOneShotRecords_必须以result结尾(t *testing.T) {
	_, err := ValidateOneShotRecords([]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "terminal result")
}

// TestValidateOneShotRecords_sequence必须连续
// Python: jsonl.py:41-42 if record.sequence != index: raise ValueError
func TestValidateOneShotRecords_sequence必须连续(t *testing.T) {
	ev, err := NewOneShotEvent(OneShotEventParams{
		Sequence: 0, RequestID: "r1", EventType: "chat.delta",
	})
	require.NoError(t, err)
	res := validResult(2, "s1") // 跳了 1

	_, verr := ValidateOneShotRecords([]any{ev, res})
	require.Error(t, verr)
	require.Contains(t, verr.Error(), "contiguous")
}

// TestValidateOneShotRecords_requestId必须一致
// Python: jsonl.py:43-44
func TestValidateOneShotRecords_requestId必须一致(t *testing.T) {
	ev, err := NewOneShotEvent(OneShotEventParams{
		Sequence: 0, RequestID: "r-other", EventType: "chat.delta",
	})
	require.NoError(t, err)
	_, verr := ValidateOneShotRecords([]any{ev, validResult(1, "s1")})
	require.Error(t, verr)
	require.Contains(t, verr.Error(), "same request_id")
}

// TestValidateOneShotRecords_sessionId必须一致
// Python: jsonl.py:47-52 all non-null session_id values must match
func TestValidateOneShotRecords_sessionId必须一致(t *testing.T) {
	s1 := "s1"
	ev, err := NewOneShotEvent(OneShotEventParams{
		Sequence: 0, RequestID: "r1", EventType: "chat.delta", SessionID: &s1,
	})
	require.NoError(t, err)
	_, verr := ValidateOneShotRecords([]any{ev, validResult(1, "s2")})
	require.Error(t, verr)
	require.Contains(t, verr.Error(), "session_id")
}

// TestValidateOneShotRecords_合法两记录 验证正常路径。
func TestValidateOneShotRecords_合法两记录(t *testing.T) {
	sid := "s1"
	ev, err := NewOneShotEvent(OneShotEventParams{
		Sequence: 0, RequestID: "r1", EventType: "chat.delta", SessionID: &sid,
	})
	require.NoError(t, err)
	got, verr := ValidateOneShotRecords([]any{ev, validResult(1, "s1")})
	require.NoError(t, verr)
	require.Len(t, got, 2)
}

// TestEncodeJSONLRecord_紧凑且不转义HTML 验证字节格式。
// Python: jsonl.py:94-101 ensure_ascii=False, separators=(",",":")
func TestEncodeJSONLRecord_紧凑且不转义HTML(t *testing.T) {
	ev, err := NewOneShotEvent(OneShotEventParams{
		Sequence: 0, RequestID: "r1", EventType: "chat.delta",
		Payload: map[string]any{"text": "a<b&c"},
	})
	require.NoError(t, err)
	line, lerr := EncodeJSONLRecord(ev)
	require.NoError(t, lerr)
	require.Contains(t, line, "a<b&c", "不得 HTML 转义为 \\u003c")
	require.NotContains(t, line, ", ", "必须是紧凑分隔符")
	require.True(t, len(line) > 0 && line[len(line)-1] == '\n', "必须以换行结尾")
}

// TestDecodeJSONL_往返一致 验证编解码对称。
// Python: jsonl.py:104-128
func TestDecodeJSONL_往返一致(t *testing.T) {
	res := validResult(0, "s1")
	doc, err := EncodeJSONL([]any{res})
	require.NoError(t, err)

	back, derr := DecodeJSONL(doc)
	require.NoError(t, derr)
	require.Len(t, back, 1)
	got, ok := back[0].(*OneShotRunResult)
	require.True(t, ok)
	require.Equal(t, "completed", got.Status)
}

// TestDecodeJSONL_空文档报错
// Python: jsonl.py:110-111
func TestDecodeJSONL_空文档报错(t *testing.T) {
	_, err := DecodeJSONL("")
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-empty JSON records")
}

// TestDecodeJSONL_未知type报错
// Python: jsonl.py:126-127 raise ValueError(f"record {index} has unsupported type")
func TestDecodeJSONL_未知type报错(t *testing.T) {
	_, err := DecodeJSONL(`{"type":"bogus"}` + "\n")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported type")
}

// TestDecodeJSONL_非法JSON报错
// Python: jsonl.py:117-118 raise ValueError(f"record {index} is not valid JSON")
func TestDecodeJSONL_非法JSON报错(t *testing.T) {
	_, err := DecodeJSONL("{not json}\n")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not valid JSON")
}

// TestMaterializeRecords_拒绝非记录类型
// Python: jsonl.py:31-33 raise TypeError(f"records[{index}] has an unsupported record type")
func TestMaterializeRecords_拒绝非记录类型(t *testing.T) {
	_, err := MaterializeRecords([]any{"not a record"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported record type")
}
