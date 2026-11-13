package processcli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DuplexInputError 输入分帧的安全诊断（不回显请求内容与文件路径）。
// Python: duplex_input.py:29-32 DuplexInputError（code = "INVALID_INPUT"）
type DuplexInputError struct {
	// Message 安全诊断消息
	Message string
}

// Error 实现 error 接口。
func (e *DuplexInputError) Error() string { return e.Message }

// Code 错误码（常量方法，对齐 Python 类属性）。
// Python: duplex_input.py:31 code = "INVALID_INPUT"
func (e *DuplexInputError) Code() string { return "INVALID_INPUT" }

// duplexSource 非阻塞读源接口。
// Python: duplex_input.py:35-112 _MemorySource/_DescriptorSource/_WindowsPipe
// 的共同协议面。readAvailable 的三态约定：
//   - (data, nil)：读到数据
//   - (nil, nil)：暂无数据（非 EOF）
//   - (空切片, nil)：EOF
type duplexSource interface {
	readAvailable(size int) ([]byte, error)
}

// memorySource 内存测试流适配（不超出每次请求的字节数）。
// Python: duplex_input.py:35-54 _MemorySource
type memorySource struct {
	// stream 底层内存流
	stream io.Reader
	// pending 临时编码缓冲（Python 的 BytesIO/TextIO 双态缓冲）
	pending []byte
}

// blockingFileSource 常规文件的同步读源。
// Python: duplex_input.py:105-106 常规文件分支（os.read 直接读）
type blockingFileSource struct {
	fd uintptr
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// MaxDuplexLineBytes 单行字节上限。
	// Python: duplex_input.py:24 MAX_DUPLEX_LINE_BYTES = 1024 * 1024
	MaxDuplexLineBytes = 1024 * 1024
	// duplexReadChunkBytes 单次读取块大小。
	// Python: duplex_input.py:25 _READ_CHUNK_BYTES = 64 * 1024
	duplexReadChunkBytes = 64 * 1024
	// duplexPollInterval 默认轮询间隔。
	// Python: duplex_input.py:144 poll_interval: float = 0.01
	duplexPollInterval = 10 * time.Millisecond
)

// ──────────────────────────── 导出函数 ────────────────────────────

// DuplexLineReader 不等 stdin EOF 的有界 JSONL 帧读取器。
// Python: duplex_input.py:126-161 DuplexLineReader
//
// ReadLine 返回去除 LF/CRLF 的合法 UTF-8 行；干净 EOF/Close 后返回
// (nil, nil)。空行保持空字节交给协议解码器拒绝。EOF 带未终结行恒为
// 输入错误。同一时刻只允许一个活跃读，且任何组件不得并发读同一流。
// Close 唤醒 pending 读并释放全部缓冲字节，但绝不关闭借来的流或描述符。
// 活跃分帧缓冲至多 maxLineBytes + 2 字节。
type DuplexLineReader struct {
	// source 非阻塞读源
	source duplexSource
	// maxLineBytes 单行字节上限
	maxLineBytes int
	// pollInterval 无数据时的轮询间隔
	pollInterval time.Duration
	// mu 保护下列全部可变状态
	mu sync.Mutex
	// buffer 活跃分帧缓冲
	buffer []byte
	// closed 是否已显式关闭或被拒
	closed bool
	// eof 是否已读到流 EOF
	eof bool
	// reading 是否有活跃读（Python :161 self._reading——互斥标志，
	// 独立于锁：waitLocked 睡眠窗口放开锁但 reading 仍为 true，
	// 保证同一时刻只有一个活跃读）
	reading bool
}

// NewDuplexLineReader 构造读取器；stream 为 nil 时借 os.Stdin。
// Python: duplex_input.py:139-161 __init__（_validate_limits + source 适配）
func NewDuplexLineReader(stream io.Reader, maxLineBytes int, pollInterval time.Duration) (*DuplexLineReader, error) {
	// Python: :146 _validate_limits
	if maxLineBytes <= 0 {
		return nil, &DuplexInputError{Message: "max_line_bytes must be positive"}
	}
	if pollInterval <= 0 {
		return nil, &DuplexInputError{Message: "poll_interval must be finite and positive"}
	}
	// Python: :147 source = sys.stdin if stream is None else stream
	if stream == nil {
		stream = os.Stdin
	}
	// Python: :149-155 BytesIO/StringIO → _MemorySource；其余 → _DescriptorSource
	var source duplexSource
	switch s := stream.(type) {
	case *bytes.Buffer, *bytes.Reader, *strings.Reader:
		source = &memorySource{stream: s}
	default:
		file, isFile := s.(*os.File)
		if !isFile {
			return nil, &DuplexInputError{Message: "duplex input stream is not readable"}
		}
		var err error
		source, err = newDescriptorSource(file)
		if err != nil {
			return nil, &DuplexInputError{Message: "duplex input stream is not readable"}
		}
	}
	return &DuplexLineReader{
		source:       source,
		maxLineBytes: maxLineBytes,
		pollInterval: pollInterval,
	}, nil
}

// ReadLine 读一条有界行；干净 EOF/Close 返回 (nil, nil)。
// Python: duplex_input.py:215-252 read_line
func (r *DuplexLineReader) ReadLine() ([]byte, error) {
	// Python: :218-220 同一时刻只允许一个活跃读——reading 标志即互斥，
	// Go 用 TryLock 直译：抢不到锁立即报「已有活跃读」。
	r.mu.Lock()
	// Python: :218-220 同一时刻只允许一个活跃读——reading 标志是互斥本体；
	// 锁只保护状态访问，waitLocked 睡眠期间锁可被 Close 抢走但 reading 不放。
	if r.reading {
		r.mu.Unlock()
		return nil, &DuplexInputError{Message: "only one duplex input read may be active"}
	}
	r.reading = true
	defer func() {
		// 循环全程持锁（waitLocked 睡眠窗口临时放开后又重新持回），
		// 此处直接清标志并解锁
		r.reading = false
		r.mu.Unlock()
	}()
	for !r.closed {
		// Python: :225-226 await asyncio.sleep(0)——缓冲里已积压多行时也让出，
		// 控制输入不能饿死执行或取消任务。
		// Go 在持有互斥锁的循环里等价语义是直接继续；真正的让出发生在
		// 下方无数据的 sleep 分支。
		if r.closed {
			return nil, nil
		}
		// Python: :228 line = self._take_line()
		line, err := r.takeLineLocked()
		if err != nil {
			return nil, err
		}
		if line != nil {
			return line, nil
		}
		// Python: :231-234 EOF 且缓冲有残留 → 未终结行错误
		if r.eof {
			if len(r.buffer) > 0 {
				return nil, r.failLocked("duplex input ended before the line terminator")
			}
			return nil, nil
		}
		// Python: :235 remaining = max_line_bytes + 2 - len(buffer)
		remaining := r.maxLineBytes + 2 - len(r.buffer)
		// Python: :236-243 read_available(min(chunk, remaining))；OSError/ValueError → fail
		chunk, readErr := r.source.readAvailable(min(duplexReadChunkBytes, remaining))
		if readErr != nil {
			return nil, r.failLocked("duplex input could not be read")
		}
		if chunk == nil {
			// Python: :244-245 _wait_for_data——短睡后重查 closed
			r.waitLocked()
			continue
		}
		if len(chunk) == 0 {
			// Python: :248-249 空 chunk = EOF
			r.eof = true
			continue
		}
		// Python: :246-247 追加缓冲
		r.buffer = append(r.buffer, chunk...)
	}
	return nil, nil
}

// ReadDocument 读一份到 EOF 的有界文档（--run-json - 专用）。
// Python: duplex_input.py:254-290 read_document
//
// 与 JSONL 不同：不要求结尾换行，内嵌换行计入文档字节上限。
// 调用方不得在同一 reader 上混用文档读与行读。
func (r *DuplexLineReader) ReadDocument() ([]byte, error) {
	// Python: :260-262 同一时刻只允许一个活跃机器输入读
	r.mu.Lock()
	if r.reading {
		r.mu.Unlock()
		return nil, &DuplexInputError{Message: "only one machine input read may be active"}
	}
	r.reading = true
	defer func() {
		r.reading = false
		r.mu.Unlock()
	}()
	for !r.closed {
		// Python: :266-267 让出 + closed 复查
		if r.closed {
			break
		}
		// Python: :269-270 文档超限
		if len(r.buffer) > r.maxLineBytes {
			return nil, r.failLocked("run input exceeds the byte limit")
		}
		// Python: :271-274 EOF → 返回全部缓冲
		if r.eof {
			document := r.buffer
			r.buffer = nil
			return document, nil
		}
		// Python: :275 remaining = max_line_bytes + 1 - len(buffer)
		remaining := r.maxLineBytes + 1 - len(r.buffer)
		// Python: :276-281 read_available；OSError/ValueError → fail
		chunk, readErr := r.source.readAvailable(min(duplexReadChunkBytes, remaining))
		if readErr != nil {
			return nil, r.failLocked("run input could not be read")
		}
		if chunk == nil {
			// Python: :282-283 _wait_for_data
			r.waitLocked()
			continue
		}
		if len(chunk) == 0 {
			// Python: :286-287 EOF
			r.eof = true
			continue
		}
		r.buffer = append(r.buffer, chunk...)
	}
	// Python: :288 closed 提前打断
	return nil, r.failLocked("run input closed before EOF")
}

// Close 唤醒 pending 读并丢弃缓冲输入；不关闭借来的流。
// Python: duplex_input.py:292-296 close
func (r *DuplexLineReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	r.buffer = nil
	return nil
}

// Closed 是否已显式关闭或拒绝输入。
// Python: duplex_input.py:163-167 closed property
func (r *DuplexLineReader) Closed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// BufferedBytes 当前分帧缓冲大小；输入绝不无限积压。
// Python: duplex_input.py:169-173 buffered_bytes property
func (r *DuplexLineReader) BufferedBytes() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.buffer)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// failLocked 清缓冲、进入 closed 态并返回错误。
// Python: duplex_input.py:180-183 _fail
func (r *DuplexLineReader) failLocked(message string) error {
	r.buffer = nil
	r.closed = true
	return &DuplexInputError{Message: message}
}

// takeLineLocked 从缓冲取一条完整行。
// Python: duplex_input.py:185-206 _take_line
func (r *DuplexLineReader) takeLineLocked() ([]byte, error) {
	// Python: :186 newline = buffer.find(b"\n")
	newline := bytes.IndexByte(r.buffer, '\n')
	if newline < 0 {
		length := len(r.buffer)
		// Python: :188-191 悬置 CRLF：长度恰为 max+1 且结尾是 CR——
		// CR 属于分帧边界，等待更多数据而非判超限
		pendingCRLF := length == r.maxLineBytes+1 && r.buffer[length-1] == 13
		if pendingCRLF {
			return nil, nil
		}
		// Python: :192-194 超限
		if length > r.maxLineBytes {
			return nil, r.failLocked("duplex input line exceeds the byte limit")
		}
		return nil, nil
	}
	// Python: :195-197 去掉行尾 CR
	contentEnd := newline
	if contentEnd > 0 && r.buffer[contentEnd-1] == 13 {
		contentEnd--
	}
	// Python: :198-200 内容超限
	if contentEnd > r.maxLineBytes {
		return nil, r.failLocked("duplex input line exceeds the byte limit")
	}
	line := r.buffer[:contentEnd]
	r.buffer = r.buffer[newline+1:]
	// Python: :202-205 行必须是合法 UTF-8
	if !utf8.Valid(line) {
		return nil, r.failLocked("duplex input line must be valid UTF-8")
	}
	return line, nil
}

// waitLocked 无数据时的短暂等待；期间 close 语义由 closed 标志复查承接。
// Python: duplex_input.py:208-213 _wait_for_data（asyncio.timeout 轮询）
//
// Go 的 sync.Cond 无定时等待，用短睡轮询等价实现（Python 本质也是
// poll_interval 超时后重查）。
func (r *DuplexLineReader) waitLocked() {
	r.mu.Unlock()
	time.Sleep(r.pollInterval)
	r.mu.Lock()
}

// readAvailable 常规文件同步读（裸 syscall，不经 os.File 包装——
// 重复 NewFile 同一 fd 会反复注册 netpoller 并触发终局器，读会间歇失败）。
func (s *blockingFileSource) readAvailable(size int) ([]byte, error) {
	buf := make([]byte, size)
	n, err := syscallRead(int(s.fd), buf)
	if err != nil {
		return nil, err
	}
	// Python: os.read 在 EOF 返回 b''；0 字节即 EOF 三态
	return buf[:n], nil
}

// readAvailable 内存流适配读（不超出每次请求的字节数）。
// Python: duplex_input.py:42-53 _MemorySource.read_available
func (s *memorySource) readAvailable(size int) ([]byte, error) {
	if len(s.pending) == 0 {
		// Python: :44-46 一个 Unicode 标量最多要 4 个 UTF-8 字节；
		// 测试适配器的临时编码缓冲也按一个 chunk 封顶。
		limit := size
		if max := duplexReadChunkBytes / 4; limit > max {
			limit = max
		}
		value := make([]byte, limit)
		n, err := s.stream.Read(value)
		if err != nil && err != io.EOF {
			return nil, err
		}
		s.pending = value[:n]
	}
	// Python: :48-50 截取请求的字节数，余量留待下次
	n := len(s.pending)
	if n > size {
		n = size
	}
	result := s.pending[:n]
	s.pending = s.pending[n:]
	return result, nil
}

// min 整数最小值（Go 1.21 前无内建；按仓库工具链显式提供）。
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
