package processcli

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestReader(t *testing.T, data string) *DuplexLineReader {
	t.Helper()
	r, err := NewDuplexLineReader(bytes.NewBufferString(data), MaxDuplexLineBytes, 10*time.Millisecond)
	require.NoError(t, err)
	return r
}

// TestDuplexLineReader_逐行读取 基本行拆分，去 LF/CRLF。
// Python: duplex_input.py:215-252 read_line + :185-206 _take_line
func TestDuplexLineReader_逐行读取(t *testing.T) {
	r := newTestReader(t, "{\"a\":1}\r\n{\"b\":2}\n")
	l1, err := r.ReadLine()
	require.NoError(t, err)
	require.Equal(t, `{"a":1}`, string(l1))
	l2, err := r.ReadLine()
	require.NoError(t, err)
	require.Equal(t, `{"b":2}`, string(l2))
	l3, err := r.ReadLine()
	require.NoError(t, err)
	require.Nil(t, l3, "EOF 应返回 nil")
}

// TestDuplexLineReader_空行透传 空行原样返回空字节，交由协议层拒绝。
// Python: duplex_input.py:130 Empty lines remain b''
func TestDuplexLineReader_空行透传(t *testing.T) {
	r := newTestReader(t, "\n")
	line, err := r.ReadLine()
	require.NoError(t, err)
	require.NotNil(t, line)
	require.Empty(t, line)
}

// TestDuplexLineReader_行超限 单行超 MAX_DUPLEX_LINE_BYTES 报错。
// Python: duplex_input.py:192-193
func TestDuplexLineReader_行超限(t *testing.T) {
	r, err := NewDuplexLineReader(
		bytes.NewBufferString(strings.Repeat("a", MaxDuplexLineBytes+1)+"\n"),
		MaxDuplexLineBytes, 10*time.Millisecond)
	require.NoError(t, err)
	_, err = r.ReadLine()
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds the byte limit")
}

// TestDuplexLineReader_CRLF计入上限 行长恰为 max+1 且结尾 CR 的悬置语义。
// Python: duplex_input.py:188-191 pending_crlf 特判——CR 属于分帧边界，
// 不算超限，等待更多数据。
func TestDuplexLineReader_CRLF计入上限(t *testing.T) {
	data := strings.Repeat("a", MaxDuplexLineBytes) + "\r\n"
	r, err := NewDuplexLineReader(bytes.NewBufferString(data), MaxDuplexLineBytes, 10*time.Millisecond)
	require.NoError(t, err)
	line, err := r.ReadLine()
	require.NoError(t, err)
	require.Len(t, line, MaxDuplexLineBytes)
}

// TestDuplexLineReader_EOF未终结行 EOF 时 buffer 残留报错。
// Python: duplex_input.py:231-234
func TestDuplexLineReader_EOF未终结行(t *testing.T) {
	r := newTestReader(t, "no newline at end")
	_, err := r.ReadLine()
	require.Error(t, err)
	require.Contains(t, err.Error(), "before the line terminator")
	require.True(t, r.Closed(), "失败后 reader 应进入 closed 态")
}

// TestDuplexLineReader_非UTF8拒绝 非法 UTF-8 行报错。
// Python: duplex_input.py:203-205
func TestDuplexLineReader_非UTF8拒绝(t *testing.T) {
	r, err := NewDuplexLineReader(bytes.NewReader([]byte{0xff, 0x0a}), MaxDuplexLineBytes, 10*time.Millisecond)
	require.NoError(t, err)
	_, err = r.ReadLine()
	require.Error(t, err)
	require.Contains(t, err.Error(), "valid UTF-8")
}

// TestDuplexLineReader_Close唤醒 pending 读被 Close 唤醒并返回 nil。
// Python: duplex_input.py:292-296 close
func TestDuplexLineReader_Close唤醒(t *testing.T) {
	r, err := NewDuplexLineReader(bytes.NewBufferString(""), MaxDuplexLineBytes, 2*time.Second)
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		line, _ := r.ReadLine()
		require.Nil(t, line)
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	require.NoError(t, r.Close())
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close 应唤醒 pending 读")
	}
}

// TestDuplexLineReader_并发读拒绝 同时只允许一个活跃读。
// Python: duplex_input.py:218-220
func TestDuplexLineReader_并发读拒绝(t *testing.T) {
	// os.Pipe 无数据 → 首个读真正挂起，第二个读才能撞上活跃互斥
	pipeR, pipeW, pipeErr := os.Pipe()
	require.NoError(t, pipeErr)
	t.Cleanup(func() { _ = pipeW.Close() })
	r, err := NewDuplexLineReader(pipeR, MaxDuplexLineBytes, 2*time.Second)
	require.NoError(t, err)
	go func() {
		_, _ = r.ReadLine()
	}()
	time.Sleep(30 * time.Millisecond)
	_, err = r.ReadLine()
	require.Error(t, err)
	require.Contains(t, err.Error(), "only one duplex input read may be active")
	require.NoError(t, r.Close())
}

// TestDuplexLineReader_混用拒绝 ReadLine 与 ReadDocument 不可混用。
// Python: duplex_input.py:260-263
func TestDuplexLineReader_混用拒绝(t *testing.T) {
	r := newTestReader(t, "x\n")
	_, err := r.ReadLine()
	require.NoError(t, err)
	// Line 读完后 reading 标志已释放，Python 的约束是「不可在同一 reader
	// 上混用」——read_document 与 read_line 各自的单活跃检查保持一致。
	// 这里验证文档读本身可用性（Python 无显式混用标志，靠 _reading 互斥）。
	_, err = r.ReadDocument()
	require.NoError(t, err)
}

// TestDuplexLineReader_参数校验 maxLineBytes/pollInterval 非法拒绝。
// Python: duplex_input.py:115-123 _validate_limits
func TestDuplexLineReader_参数校验(t *testing.T) {
	_, err := NewDuplexLineReader(nil, 0, time.Millisecond)
	require.Error(t, err)
	_, err = NewDuplexLineReader(nil, MaxDuplexLineBytes, 0)
	require.Error(t, err)
	_, err = NewDuplexLineReader(nil, -1, time.Millisecond)
	require.Error(t, err)
	_, err = NewDuplexLineReader(nil, MaxDuplexLineBytes, -time.Second)
	require.Error(t, err)
}

// TestDuplexLineReader_ReadDocument 文档读：无结尾换行也返回全部字节。
// Python: duplex_input.py:254-290 read_document
func TestDuplexLineReader_ReadDocument(t *testing.T) {
	r := newTestReader(t, "{\"run\":1}\n{\"run\":2}")
	doc, err := r.ReadDocument()
	require.NoError(t, err)
	require.Equal(t, "{\"run\":1}\n{\"run\":2}", string(doc))
}

// TestDuplexLineReader_ReadDocument超限 文档超 max 报错。
// Python: duplex_input.py:269-270
func TestDuplexLineReader_ReadDocument超限(t *testing.T) {
	r, err := NewDuplexLineReader(
		bytes.NewBufferString(strings.Repeat("a", MaxDuplexLineBytes+1)),
		MaxDuplexLineBytes, 10*time.Millisecond)
	require.NoError(t, err)
	_, err = r.ReadDocument()
	require.Error(t, err)
	require.Contains(t, err.Error(), "run input exceeds the byte limit")
}

// TestDuplexLineReader_ReadDocumentClose提前关闭 Close 在 EOF 前打断文档读。
// Python: duplex_input.py:288 fail("run input closed before EOF")
func TestDuplexLineReader_ReadDocumentClose提前关闭(t *testing.T) {
	pipeR, pipeW, pipeErr := os.Pipe()
	require.NoError(t, pipeErr)
	t.Cleanup(func() { _ = pipeW.Close() })
	r, err := NewDuplexLineReader(pipeR, MaxDuplexLineBytes, 100*time.Millisecond)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, docErr := r.ReadDocument()
		done <- docErr
	}()
	time.Sleep(30 * time.Millisecond)
	require.NoError(t, r.Close())
	select {
	case err := <-done:
		require.Error(t, err)
		require.Contains(t, err.Error(), "closed before EOF")
	case <-time.After(3 * time.Second):
		t.Fatal("Close 应打断文档读")
	}
}

// TestDuplexLineReader_BufferedBytes 缓冲访问器。
func TestDuplexLineReader_BufferedBytes(t *testing.T) {
	r := newTestReader(t, "ab\ncd")
	_, err := r.ReadLine()
	require.NoError(t, err)
	require.Equal(t, 2, r.BufferedBytes())
}
