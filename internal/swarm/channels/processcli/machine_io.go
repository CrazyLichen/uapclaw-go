package processcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli/protocol"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MachineInputError 被拒机器文档的安全诊断（在 Runtime 启动之前）。
// Python: machine_io.py:52-66 MachineInputError
//
// 只暴露「哪一类问题」，不回显字段值或未知字段名。
type MachineInputError struct {
	// Message 安全诊断消息
	Message string
	// RequestID 已能解析出的请求标识（可选）
	RequestID *string
	// Details 字段级定位（来自 schema 诊断）
	Details map[string]any
}

// OutputError 机器输出流写入失败（对齐 Python 的 OSError）。
// Python: machine_io.py:229 raise OSError("machine output write was incomplete")
//
// 单独成类型的原因：run_machine 用 `except OSError` 把它分流成 OUTPUT_CLOSED；
// Go 侧若直接返回 fmt.Errorf，errors.As 无法识别，会掉进兜底的 RUNTIME_FAILED。
type OutputError struct {
	// Err 底层错误
	Err error
}

// OneShotWriter 立即写出每条事件，其后最多一条终态结果。
// Python: machine_io.py:197-266 OneShotWriter
//
// 执行器在解析出 Runtime 会话后才赋值 session_id。
// Runtime 事件自身的身份永远不替换外部命令身份。
// 一旦写出失败，broken 禁止后续写入或重试——记录可能已部分投递。
type OneShotWriter struct {
	// stdout 输出目标
	stdout io.Writer
	// RequestID 外部命令身份（已 strip）
	RequestID string
	// Sequence 下一条记录的序号
	Sequence int
	// SessionID 解析出的会话标识（可空）
	SessionID *string
	// Broken 输出流是否已损坏
	Broken bool
	// FinalWritten 终态结果是否已写出
	FinalWritten bool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// readChunkBytes 单次读取块大小。
	// Python: machine_io.py:28 _READ_CHUNK_BYTES = 8192
	readChunkBytes = 8192
)

// ──────────────────────────── 全局变量 ────────────────────────────

// schemaFields 用于 _safe_schema_reason 的字段名前缀表。
// Python: machine_io.py:29-49 _SCHEMA_FIELDS
var schemaFields = []string{
	"schema_version",
	"type",
	"request_id",
	"session_id",
	"input",
	"agent",
	"name",
	"instructions",
	"description",
	"model",
	"tools",
	"skills",
	"max_iterations",
	"mode",
	"workspace",
	"cwd",
	"project_dir",
	"trusted_dirs",
	"timeout_seconds",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *OutputError) Error() string {
	if e == nil || e.Err == nil {
		return "machine output write failed"
	}
	return e.Err.Error()
}

// Unwrap 暴露底层错误。
func (e *OutputError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Error 实现 error 接口。
func (e *MachineInputError) Error() string { return e.Message }

// Code 错误码（常量方法，对齐 Python 类属性）。
// Python: machine_io.py:55 code = "INVALID_INPUT"
func (e *MachineInputError) Code() string { return "INVALID_INPUT" }

// ReadRunInputBytes 读取有界的严格 UTF-8 文档字节内容。
// Python: machine_io.py:69-80 _read_limited
//
// 读到 EOF 为止；上限是 protocol.MaxRunInputBytes。
func ReadRunInputBytes(reader io.Reader) (string, error) {
	content := make([]byte, 0, 4096)
	chunk := make([]byte, readChunkBytes)
	for {
		// Python: remaining = MAX_RUN_INPUT_BYTES - len(content) + 1
		remaining := protocol.MaxRunInputBytes - len(content) + 1
		if remaining <= 0 {
			return "", &MachineInputError{Message: "run input exceeds the byte limit"}
		}
		readSize := readChunkBytes
		if remaining < readSize {
			readSize = remaining
		}
		n, err := reader.Read(chunk[:readSize])
		if n > 0 {
			// Python: :77-78 if len(content) + len(encoded) > MAX: raise
			if len(content)+n > protocol.MaxRunInputBytes {
				return "", &MachineInputError{Message: "run input exceeds the byte limit"}
			}
			content = append(content, chunk[:n]...)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", &MachineInputError{Message: "run input could not be read"}
		}
		if n == 0 {
			break
		}
	}
	return string(content), nil
}

// DecodeMachineDocument 严格解码一个机器 JSON 对象，不回显不安全的值。
// Python: machine_io.py:90-131 decode_machine_document
func DecodeMachineDocument(document string) (map[string]any, error) {
	// Python: :92-93 if not document.strip(): raise
	if strings.TrimSpace(document) == "" {
		return nil, &MachineInputError{Message: "run input must not be empty"}
	}

	// Python: :94-108 object_pairs_hook=collect_pairs / parse_constant=reject_constant
	//
	// Go 的 encoding/json 不认 NaN / Infinity / -Infinity 字面量（直接报语法错），
	// 而 Python 的 parse_constant 能接住它们。先预检这三个字面量，
	// 命中即按 non_finite_number 处理，语义与上游一致。
	value, stats, err := decodeStrict(document)
	if err != nil {
		if hasNonFiniteLiteral(document) {
			return nil, &MachineInputError{
				Message: "run input must contain only finite JSON numbers",
			}
		}
		// Python: :116-119 except (ValueError, RecursionError)
		return nil, &MachineInputError{
			Message: "run input must contain one valid JSON document",
		}
	}
	// Python: :120-121 if not isinstance(value, dict): raise
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, &MachineInputError{Message: "run input must be a JSON object"}
	}
	requestID := requestIDOf(obj)
	// Python: :123-126 if duplicate_key: raise
	if stats.duplicateKey {
		return nil, &MachineInputError{
			Message:   "run input contains duplicate JSON keys",
			RequestID: requestID,
		}
	}
	// Python: :127-130 if non_finite_number: raise
	if stats.nonFinite {
		return nil, &MachineInputError{
			Message:   "run input must contain only finite JSON numbers",
			RequestID: requestID,
		}
	}
	return obj, nil
}

// ReadMachineDocument 在导入任何 Runtime 之前读取有界严格 UTF-8 文档。
// Python: machine_io.py:176-194 read_machine_document
//
// source == "-" 表示从 stdin 读（读到 EOF）。
func ReadMachineDocument(source string, stdin io.Reader) (map[string]any, error) {
	var document string
	var err error
	if source == "-" {
		// Python: :181-184 input_stream = sys.stdin if stdin is None else stdin
		//            stream = getattr(input_stream, "buffer", input_stream)
		stream := stdin
		if stream == nil {
			stream = os.Stdin
		}
		document, err = ReadRunInputBytes(stream)
	} else {
		// Python: :185-187 with open(source, "rb") as stream
		file, openErr := os.Open(source)
		if openErr != nil {
			// Python: :192-193 except (OSError, ValueError) → "could not be read"
			return nil, &MachineInputError{Message: "run input could not be read"}
		}
		defer func() { _ = file.Close() }()
		document, err = ReadRunInputBytes(file)
	}
	if err != nil {
		// Python: :188-193 MachineInputError 原样抛；OSError/ValueError 转 "could not be read"
		var inputErr *MachineInputError
		if errors.As(err, &inputErr) {
			return nil, inputErr
		}
		return nil, &MachineInputError{Message: "run input could not be read"}
	}
	return DecodeMachineDocument(document)
}

// ReadRunInput 从文件或 stdin 读到 EOF，返回一条一次性运行输入。
// Python: machine_io.py:153-173 read_run_input
func ReadRunInput(source string, stdin io.Reader) (*protocol.OneShotRunInput, error) {
	value, err := ReadMachineDocument(source, stdin)
	if err != nil {
		return nil, err
	}
	input, convErr := protocol.OneShotRunInputFromDict(value)
	if convErr != nil {
		// Python: :166-173 raise MachineInputError(_safe_schema_reason(error), ...)
		return nil, &MachineInputError{
			Message:   safeSchemaReason(convErr),
			RequestID: requestIDOf(value),
			Details:   protocol.InputErrorDetails(value, "run"),
		}
	}
	return input, nil
}

// NewOneShotWriter 构造输出写入器。
// Python: machine_io.py:206-216 OneShotWriter.__init__
//
// Python 在 request_id 非字符串/空白时 raise；Go 用 error 第二返回值表达。
func NewOneShotWriter(stdout io.Writer, requestID string) (*OneShotWriter, error) {
	// Python: :207-208 if not isinstance(request_id, str): raise TypeError
	// 只接受字符串（Go 签名已保证）
	// Python: :209-210 if not request_id.strip(): raise ValueError
	trimmed := strings.TrimSpace(requestID)
	if trimmed == "" {
		return nil, fmt.Errorf("request_id must not be empty")
	}
	return &OneShotWriter{
		stdout:    stdout,
		RequestID: trimmed,
	}, nil
}

// WriteEvent 渲染一条观测，不改动 Runtime 拥有的数据。
// Python: machine_io.py:236-253 write_event
func (w *OneShotWriter) WriteEvent(event *protocol.OneShotEvent) error {
	// Python: :239 self._check_writable()
	if err := w.checkWritable(); err != nil {
		return err
	}
	// Python: :244-252 record = OneShotEvent(sequence=..., event_type=...,
	//                                          payload=public_event_payload(...))
	eventType := event.EventType
	if eventType == "" {
		eventType = "runtime.event"
	}
	record := &protocol.OneShotEvent{
		SchemaVersion: protocol.CurrentSchemaVersion,
		Sequence:      w.Sequence,
		RequestID:     w.RequestID,
		SessionID:     w.SessionID,
		EventType:     eventType,
	}
	// Python: payload=public_event_payload(event_type, event.payload)
	if payload, ok := protocol.PublicEventPayload(eventType, event.Payload).(map[string]any); ok {
		record.Payload = payload
	}
	return w.write(record)
}

// WriteResult 用本命令的序号与身份渲染终态。
// Python: machine_io.py:255-266 write_result
func (w *OneShotWriter) WriteResult(result *protocol.OneShotRunResult) error {
	// Python: :258 self._check_writable()
	if err := w.checkWritable(); err != nil {
		return err
	}
	// Python: :259-264 replace(result, sequence=..., request_id=..., session_id=...)
	// replace 生成新对象，不改动调用方持有的 result
	record := *result
	record.Sequence = w.Sequence
	record.RequestID = w.RequestID
	record.SessionID = w.SessionID
	if err := w.write(&record); err != nil {
		return err
	}
	// Python: :266 self.final_written = True
	w.FinalWritten = true
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// decodeStats 严格解码的统计结果。
type decodeStats struct {
	// duplicateKey 是否出现重复 key（任一层）
	duplicateKey bool
	// nonFinite 是否出现非有限数
	nonFinite bool
}

// decodeStrict 用 Decoder.Token 逐 token 解析，检测重复 key 与非有限数。
// Python: machine_io.py:97-115 object_pairs_hook=collect_pairs、parse_constant=reject_constant
//
// D-10：Go 的 json.Unmarshal 对重复 key 是后者覆盖前者、不报错，
// 必须手工遍历 Token 才能与上游一致。
// 另：1e999 这类溢出会在 Unmarshal 时报错（Go 拒绝超范围 float），
// 需要单独识别成「非有限数」而非语法错，故用 UseNumber 先取原始字面量再判定。
func decodeStrict(document string) (any, decodeStats, error) {
	decoder := json.NewDecoder(strings.NewReader(document))
	decoder.UseNumber()
	stats := decodeStats{}
	value, err := decodeValue(decoder, &stats)
	if err != nil {
		return nil, stats, err
	}
	// 文档必须只有一个 JSON 值（Python json.loads 同上）
	if _, tailErr := decoder.Token(); tailErr != io.EOF {
		return nil, stats, fmt.Errorf("trailing content after JSON document")
	}
	return value, stats, nil
}

// decodeValue 递归解析一个 JSON 值。
func decodeValue(decoder *json.Decoder, stats *decodeStats) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch tok := token.(type) {
	case json.Delim:
		switch tok {
		case '{':
			// Python: :97-104 collect_pairs 逐对检查重复 key
			result := make(map[string]any)
			for decoder.More() {
				keyToken, keyErr := decoder.Token()
				if keyErr != nil {
					return nil, keyErr
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, fmt.Errorf("object key must be a string")
				}
				if _, exists := result[key]; exists {
					stats.duplicateKey = true
				}
				item, itemErr := decodeValue(decoder, stats)
				if itemErr != nil {
					return nil, itemErr
				}
				result[key] = item
			}
			// 消费收尾 '}'
			if _, endErr := decoder.Token(); endErr != nil {
				return nil, endErr
			}
			return result, nil
		case '[':
			result := make([]any, 0)
			for decoder.More() {
				item, itemErr := decodeValue(decoder, stats)
				if itemErr != nil {
					return nil, itemErr
				}
				result = append(result, item)
			}
			if _, endErr := decoder.Token(); endErr != nil {
				return nil, endErr
			}
			return result, nil
		}
		return nil, fmt.Errorf("unexpected delimiter %v", tok)
	case json.Number:
		// Python: parse_constant 拦 NaN/Infinity；parse_float 拦 1e999 → inf
		f, convErr := tok.Float64()
		if convErr != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			stats.nonFinite = true
			return nil, nil
		}
		return f, nil
	case string, bool, nil:
		return tok, nil
	default:
		return nil, fmt.Errorf("unexpected token %T", token)
	}
}

// safeSchemaReason 只点出已知 schema 字段名，不回显值或未知字段名。
// Python: machine_io.py:134-150 _safe_schema_reason
func safeSchemaReason(err error) string {
	if err == nil {
		return "run input does not match the schema"
	}
	message := err.Error()
	// Python: :138-139
	if strings.HasPrefix(message, "unsupported schema_version") {
		return "schema_version is not supported"
	}
	// Python: :140-141
	if strings.HasPrefix(message, "unsupported single-Agent mode") {
		return "mode is not supported"
	}
	// Python: :142-146
	for _, name := range []string{"run", "agent", "workspace"} {
		if strings.HasPrefix(message, name+" contains unknown fields") {
			return name + " contains unknown fields"
		}
		if strings.HasPrefix(message, name+" is missing required fields") {
			return name + " is missing required fields"
		}
	}
	// Python: :147-149 for name in _SCHEMA_FIELDS:
	//   if message.startswith((f"{name} ", f"{name}[", f"{name}.")): return f"{name} is invalid"
	for _, name := range schemaFields {
		if strings.HasPrefix(message, name+" ") ||
			strings.HasPrefix(message, name+"[") ||
			strings.HasPrefix(message, name+".") {
			return name + " is invalid"
		}
	}
	// Python: :150
	return "run input does not match the schema"
}

// requestIDOf 取可用的 request_id（strip 后非空才返回）。
// Python: machine_io.py:83-87 _request_id
func requestIDOf(value map[string]any) *string {
	raw, ok := value["request_id"].(string)
	if !ok {
		return nil
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// checkWritable 写入前置守卫。
// Python: machine_io.py:218-222 _check_writable
func (w *OneShotWriter) checkWritable() error {
	// Python: :219-220 if self.broken: raise BrokenPipeError
	if w.Broken {
		return fmt.Errorf("machine output stream is broken")
	}
	// Python: :221-222 if self.final_written: raise ValueError
	if w.FinalWritten {
		return fmt.Errorf("terminal result has already been written")
	}
	return nil
}

// write 编码并写出单条记录，成功后序号递增。
// Python: machine_io.py:224-234 _write
func (w *OneShotWriter) write(record any) error {
	// Python: :225 line = encode_jsonl_record(record)
	line, err := protocol.EncodeJSONLRecord(record)
	if err != nil {
		return err
	}
	// Python: :227-230 written = stdout.write(line); if written != len(line): raise
	written, writeErr := io.WriteString(w.stdout, line)
	if writeErr != nil {
		// Python: :231-233 except (OSError, ValueError): self.broken = True
		w.Broken = true
		return &OutputError{Err: writeErr}
	}
	if written != len(line) {
		w.Broken = true
		return &OutputError{Err: errors.New("machine output write was incomplete")}
	}
	// Python: :230 self._stdout.flush()
	if flusher, ok := w.stdout.(interface{ Flush() error }); ok {
		if flushErr := flusher.Flush(); flushErr != nil {
			w.Broken = true
			return &OutputError{Err: flushErr}
		}
	}
	// Python: :234 self.sequence += 1
	w.Sequence++
	return nil
}

// hasNonFiniteLiteral 文档中是否出现 NaN / Infinity / -Infinity 字面量。
// Python: machine_io.py:106-108 parse_constant=reject_constant
//
// Go 的 encoding/json 不把这些当合法 JSON，直接报语法错；
// 为与上游「非有限数」错误区分开，需要在解析失败时单独识别。
func hasNonFiniteLiteral(document string) bool {
	for _, literal := range []string{"NaN", "Infinity", "-Infinity"} {
		if strings.Contains(document, literal) {
			return true
		}
	}
	return false
}
