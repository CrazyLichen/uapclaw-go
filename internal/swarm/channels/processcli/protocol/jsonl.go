package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// MaterializeRecords 把任意序列物化为 one-shot 记录切片。
// Python: jsonl.py:20-34 _materialize_records
func MaterializeRecords(records []any) ([]any, error) {
	if len(records) == 0 {
		return nil, fmt.Errorf("record stream must contain a terminal result")
	}
	for index, record := range records {
		switch record.(type) {
		case *OneShotEvent, *OneShotRunResult:
		default:
			return nil, fmt.Errorf("records[%d] has an unsupported record type", index)
		}
	}
	return records, nil
}

// ValidateRecordIdentity 校验记录身份一致性。
// Python: jsonl.py:37-56 _validate_record_identity
func ValidateRecordIdentity(records []any) error {
	if len(records) == 0 {
		return fmt.Errorf("record stream must contain a terminal result")
	}
	first, err := recordRequestID(records[0])
	if err != nil {
		return err
	}
	firstVersion, verr := recordSchemaVersion(records[0])
	if verr != nil {
		return verr
	}
	var observedSessionID *string
	for index, record := range records {
		seq, serr := recordSequence(record)
		if serr != nil {
			return serr
		}
		// Python: :41-42 if record.sequence != index: raise ValueError
		if seq != index {
			return fmt.Errorf("record sequence must start at zero and be contiguous")
		}
		// Python: :43-44 if record.request_id != first.request_id: raise ValueError
		rid, rerr := recordRequestID(record)
		if rerr != nil {
			return rerr
		}
		if rid != first {
			return fmt.Errorf("all records must use the same request_id")
		}
		// Python: :45-46 if record.schema_version != first.schema_version
		version, verr2 := recordSchemaVersion(record)
		if verr2 != nil {
			return verr2
		}
		if version != firstVersion {
			return fmt.Errorf("all records must use the same schema_version")
		}
		// Python: :47-52 非 nil session_id 必须一致
		sid, sok := recordSessionID(record)
		if !sok || sid == nil {
			continue
		}
		if observedSessionID == nil {
			observedSessionID = sid
		} else if *observedSessionID != *sid {
			return fmt.Errorf("all non-null session_id values must match")
		}
	}
	// Python: :54-56 最后一条 result 必须带已观察到的 session_id
	last := records[len(records)-1]
	result, ok := last.(*OneShotRunResult)
	if !ok {
		return fmt.Errorf("record stream must end with exactly one result")
	}
	if observedSessionID != nil {
		if result.SessionID == nil || *result.SessionID != *observedSessionID {
			return fmt.Errorf("terminal result must carry the resolved session_id")
		}
	}
	return nil
}

// ValidateOneShotRecords 校验「有序事件 + 恰好一条终态 result」。
// Python: jsonl.py:59-74 validate_one_shot_records
func ValidateOneShotRecords(records []any) ([]any, error) {
	materialized, err := MaterializeRecords(records)
	if err != nil {
		return nil, err
	}
	// Python: :66-72 result 必须恰好是最后一条
	resultIndexes := make([]int, 0, 1)
	for index, record := range materialized {
		if _, ok := record.(*OneShotRunResult); ok {
			resultIndexes = append(resultIndexes, index)
		}
	}
	lastIndex := len(materialized) - 1
	if len(resultIndexes) != 1 || resultIndexes[0] != lastIndex {
		return nil, fmt.Errorf("record stream must end with exactly one result")
	}
	if err := ValidateRecordIdentity(materialized); err != nil {
		return nil, err
	}
	return materialized, nil
}

// EncodeJSONL 编码一份完整、已校验的 NDJSON 文档。
// Python: jsonl.py:77-86 encode_jsonl
func EncodeJSONL(records []any) (string, error) {
	validated, err := ValidateOneShotRecords(records)
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	for _, record := range validated {
		line, lerr := EncodeJSONLRecord(record)
		if lerr != nil {
			return "", lerr
		}
		buf.WriteString(line)
	}
	return buf.String(), nil
}

// EncodeJSONLRecord 编码一条记录，供一次性进程立即写入并 flush。
// Python: jsonl.py:89-101 encode_jsonl_record
//
// Python: json.dumps(record.to_dict(), ensure_ascii=False, separators=(",", ":")) + "\n"
// ensure_ascii=False 对应 Go 的 SetEscapeHTML(false)——否则 < > & 会被转义成
// \u003c 等，与上游字节不一致，SDK 侧比对失败。
func EncodeJSONLRecord(record any) (string, error) {
	dict, ok := recordDict(record)
	if !ok {
		return "", fmt.Errorf("record has an unsupported record type")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(dict); err != nil {
		return "", err
	}
	// json.Encoder.Encode 已追加换行，与上游 "+ \n" 等价
	return buf.String(), nil
}

// DecodeJSONL 解码并校验一份完整的 NDJSON 文档。
// Python: jsonl.py:104-128 decode_jsonl
func DecodeJSONL(document string) ([]any, error) {
	lines := splitLines(document)
	if len(lines) == 0 {
		return nil, fmt.Errorf("document must contain non-empty JSON records")
	}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			return nil, fmt.Errorf("document must contain non-empty JSON records")
		}
	}
	records := make([]any, 0, len(lines))
	for index, line := range lines {
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("record %d is not valid JSON", index)
		}
		recordType := asString(raw["type"])
		switch recordType {
		case "event":
			ev, evErr := OneShotEventFromDict(raw)
			if evErr != nil {
				return nil, fmt.Errorf("record %d: %w", index, evErr)
			}
			records = append(records, ev)
		case "result":
			res, resErr := OneShotRunResultFromDict(raw)
			if resErr != nil {
				return nil, fmt.Errorf("record %d: %w", index, resErr)
			}
			records = append(records, res)
		default:
			return nil, fmt.Errorf("record %d has unsupported type: %s", index, recordType)
		}
	}
	return ValidateOneShotRecords(records)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// recordDict 把记录转成待编码的 map。
func recordDict(record any) (map[string]any, bool) {
	switch v := record.(type) {
	case *OneShotEvent:
		return v.ToDict(), true
	case *OneShotRunResult:
		return v.ToDict(), true
	case *OneShotQueryResult:
		// Python: query_entry.py:112-140 逐条写出的 query_result 也是 JSONL 记录
		return v.ToDict(), true
	default:
		return nil, false
	}
}

// recordSequence 取记录序号。
func recordSequence(record any) (int, error) {
	switch v := record.(type) {
	case *OneShotEvent:
		return v.Sequence, nil
	case *OneShotRunResult:
		return v.Sequence, nil
	default:
		return 0, fmt.Errorf("record has an unsupported record type")
	}
}

// recordRequestID 取记录 request_id。
func recordRequestID(record any) (string, error) {
	switch v := record.(type) {
	case *OneShotEvent:
		return v.RequestID, nil
	case *OneShotRunResult:
		return v.RequestID, nil
	default:
		return "", fmt.Errorf("record has an unsupported record type")
	}
}

// recordSchemaVersion 取记录 schema_version。
func recordSchemaVersion(record any) (string, error) {
	switch v := record.(type) {
	case *OneShotEvent:
		return v.SchemaVersion, nil
	case *OneShotRunResult:
		return v.SchemaVersion, nil
	default:
		return "", fmt.Errorf("record has an unsupported record type")
	}
}

// recordSessionID 取记录 session_id（第二个返回值表示该类型是否带该字段）。
func recordSessionID(record any) (*string, bool) {
	switch v := record.(type) {
	case *OneShotEvent:
		return v.SessionID, true
	case *OneShotRunResult:
		return v.SessionID, true
	default:
		return nil, false
	}
}

// splitLines 按 Python str.splitlines() 语义切行（\n / \r\n / \r）。
func splitLines(document string) []string {
	if document == "" {
		return nil
	}
	normalized := strings.ReplaceAll(document, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")
	// 末尾换行产生的空串丢弃（splitlines 不含行终止后的空行）
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
