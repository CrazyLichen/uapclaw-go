// runtime_evidence.go 提供案例本地调用证据的有界净化拷贝。
// Python: openjiuwen/rsi/harness_rsi/evaluation_result_analyzer/runtime_evidence.py（全量对齐）。

package evaluationresultanalyzer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// configFields 运行时配置白名单。
// Python: runtime_evidence.py:12-19 _CONFIG_FIELDS
var configFields = map[string]bool{
	"max_iterations": true, "on_max_iterations": true, "solver_max_iterations": true, "solver_on_max_iterations": true,
	"max_iterations_hard_limit": true, "host_max_iterations": true, "hard_max_iterations": true,
	"solver_hard_max_iterations": true, "host_hard_limit": true,
	"max_output_tokens": true, "timeout": true, "timeout_seconds": true, "role": true, "case_id": true, "evaluation_id": true,
	"task_id": true, "invocation_id": true, "status": true, "source": true, "effective": true, "limits": true, "solver": true, "runtime": true,
	"allowed_config_fields": true, "candidate_editable": true, "host_limits": true, "effective_config": true, "runtime_config": true,
}

// terminationFields 终止信息白名单。
// Python: runtime_evidence.py:20-31 _TERMINATION_FIELDS
var terminationFields = map[string]bool{
	"truncated_by_max_iterations": true, "termination_reason": true, "result_type": true,
	"max_iterations": true, "iterations_used": true, "used_iterations": true,
	"iteration_count": true, "on_max_iterations": true, "budget_exhausted": true, "finalization_attempted": true,
	"finalization_succeeded": true, "finalization_status": true, "finalization_error": true, "final_answer_available": true,
	"final_output_available": true, "artifacts_available": true, "output_available": true, "artifact_count": true,
	"model_call_count": true, "loop_model_calls": true, "finalization_model_calls": true, "elapsed_seconds": true,
	"token_usage": true, "input_tokens": true, "output_tokens": true, "total_tokens": true, "status": true, "reason": true, "role": true,
	"case_id": true, "task_id": true, "evaluation_id": true, "invocation_id": true, "error": true, "execution_termination": true,
	"finalization": true, "attempted": true, "succeeded": true, "success": true, "effective_max_iterations": true,
	"effective_on_max_iterations": true, "actual_iterations": true, "actual_model_calls": true, "completion_status": true,
}

// secretKeyRE 键级敏感信息匹配。
// Python: runtime_evidence.py:32-36 _SECRET_KEY
var secretKeyRE = regexp.MustCompile(
	`(?i)api.?key|authorization|password|secret|credential|access.?token|` +
		`(?:^|[_-])token$|environment|headers|prompt|messages|request_body|model_config`)

// secretTextRE 文本级敏感信息匹配（分组 1/2 保留前缀）。
// Python: runtime_evidence.py:37-41 _SECRET_TEXT
var secretTextRE = regexp.MustCompile(
	`(?i)(bearer\s+)[^\s,;"']+|` +
		`((?:api[_-]?key|access[_-]?token|token|password|secret|authorization)["']?\s*[:=]\s*)` +
		`(?:"[^"]*"|'[^']*'|[^\s,;}]+)|\bsk-[A-Za-z0-9_-]{8,}`)

// payloadTextRE 结构化载荷标记匹配。
// Python: runtime_evidence.py:43 _PAYLOAD_TEXT
var payloadTextRE = regexp.MustCompile(`(?i)(?:prompt|messages|request_body|response_body|model_config)["']?\s*[:=]`)

// terminationPattern 初始日志检索的终止关键词。
// Python: runtime_evidence.py:295-297 _TERMINATION_PATTERN
var terminationPattern = regexp.MustCompile(
	`(?i)Traceback|exception|timeout|timed out|max_iterations|termination_reason|failed|invocation_end|finalization`)

const (
	// maxRecordBytes 单条记录字节上限。Python: _MAX_RECORD_BYTES
	maxRecordBytes = 8192
	// maxLogFileBytes 单日志字节上限（4 MiB）。Python: _MAX_LOG_FILE_BYTES
	maxLogFileBytes = 4 * 1024 * 1024
	// maxLogTotalBytes 日志总字节上限（16 MiB）。Python: _MAX_LOG_TOTAL_BYTES
	maxLogTotalBytes = 16 * 1024 * 1024
	// maxStructuredBytes 结构化文件字节上限。Python: _MAX_STRUCTURED_BYTES
	maxStructuredBytes = 65536
	// maxLogFiles 日志文件数上限。Python: manifest["limits"]["max_log_files"]
	maxLogFiles = 32
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ReadExecutionContext 读取白名单、有界的案例本地结构化执行事实。
// 返回 mapping：runtime_config / execution_termination → {status, facts, source}。
// Python: runtime_evidence.py:80-126 read_execution_context
func ReadExecutionContext(caseDir string, execution map[string]any, recordedTerminationReason string) map[string]any {
	result := map[string]any{}
	// Python: for name, default, fields in ((runtime_config, ...), (execution_termination, ...))
	type fieldSpec struct {
		name   string
		def    string
		fields map[string]bool
	}
	specs := []fieldSpec{
		{"runtime_config", "runtime_config.json", configFields},
		{"execution_termination", "termination.json", terminationFields},
	}
	for _, spec := range specs {
		source := localSource(caseDir, execution[spec.name], spec.def)
		status := "unknown"
		value := map[string]any{}
		if source != "" && isFile(source) {
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						status = "unreadable"
					}
				}()
				info, statErr := os.Stat(source)
				if statErr != nil {
					status = "unreadable"
					return
				}
				// Python: if source.stat().st_size > _MAX_STRUCTURED_BYTES: oversized
				if info.Size() > maxStructuredBytes {
					status = "oversized"
					return
				}
				data, err := readJSONAny(source)
				if err != nil {
					// Python: except (OSError, ValueError): unreadable
					status = "unreadable"
					return
				}
				if dataMap, isMap := data.(map[string]any); isMap {
					projected := projectValue(dataMap, spec.fields)
					if projMap, isProj := projected.(map[string]any); isProj {
						value = projMap
						status = "available"
					}
				} else {
					// Python: else: status = "invalid"
					status = "invalid"
				}
			}()
		} else if inline, isMap := execution[spec.name].(map[string]any); isMap {
			// Python: elif isinstance(execution.get(name), dict): 无 path/file 键才内联投影
			_, hasPath := inline["path"]
			_, hasFile := inline["file"]
			if !hasPath && !hasFile {
				projected := projectValue(inline, spec.fields)
				if projMap, isProj := projected.(map[string]any); isProj {
					value = projMap
					if len(value) > 0 {
						status = "available"
					} else {
						status = "unknown"
					}
				}
			}
		}
		// Python: provenance = 相对路径或 outside_case_rejected
		provenance := "outside_case_rejected"
		if source != "" {
			if rel, err := filepath.Rel(resolvePath(caseDir), source); err == nil && !strings.HasPrefix(rel, "..") {
				provenance = filepath.ToSlash(rel)
			}
		}
		// Python: execution_termination 记录回填
		if spec.name == "execution_termination" && recordedTerminationReason != "" && value["termination_reason"] == nil {
			value["termination_reason"] = recordedTerminationReason
			if status != "available" {
				status = "partial"
			}
			provenance += "; termination_reason from recorded case response/metadata"
		}
		result[spec.name] = map[string]any{"status": status, "facts": value, "source": provenance}
	}
	return result
}

// PrepareRuntimeEvidence 拷贝有界调用日志；绝不从共享日志/时间推断隔离。
// Python: runtime_evidence.py:129-193 prepare_runtime_evidence
func PrepareRuntimeEvidence(caseDir string, execution map[string]any, runtimeDir string, recordedTerminationReason string) map[string]any {
	destination := filepath.Join(runtimeDir, "runtime_evidence")
	if err := os.Mkdir(destination, 0o755); err != nil {
		// Python: destination.mkdir() 失败抛异常；Go 等价返回含错误状态的 manifest 不现实——
		// 沿用 Python 行为：调用方传入的 runtime_dir 必须可写，mkdir 失败视为调用契约违约。
		panic(fmt.Sprintf("runtime evidence dir create failed: %v", err))
	}
	caseDirResolved := resolvePath(caseDir)
	context := ReadExecutionContext(caseDir, execution, recordedTerminationReason)

	manifest := map[string]any{
		"scope": "case-local invocation files only; identities come from records, never timestamps",
		"limits": map[string]any{
			"single_record_bytes": maxRecordBytes, "per_log_bytes": maxLogFileBytes,
			"total_log_bytes": maxLogTotalBytes, "total_source_bytes": maxLogTotalBytes,
			"max_log_files": maxLogFiles,
		},
		"copy_policy": "Complete files within 4 MiB; larger files retain tail and head within input/output budgets. " +
			"Tail byte offsets are exact; original tail line numbers " +
			"remain unknown without scanning the omitted middle.",
		"structured": map[string]any{}, "logs": []any{}, "missing": []any{},
	}

	// Python: 结构化事实落盘 + manifest 登记
	for _, pair := range []struct{ name, filename string }{
		{"runtime_config", "runtime_config.json"},
		{"execution_termination", "termination.json"},
	} {
		entry := context[pair.name].(map[string]any)
		content, _ := json.MarshalIndent(entry, "", "  ")
		target := filepath.Join(destination, pair.filename)
		_ = os.WriteFile(target, content, 0o644)
		rel, _ := filepath.Rel(runtimeDir, target)
		merged := copyMap(entry)
		merged["path"] = filepath.ToSlash(rel)
		manifest["structured"].(map[string]any)[pair.name] = merged
		if entry["status"] != "available" {
			manifest["missing"] = append(manifest["missing"].([]any), pair.name)
		}
	}

	// Python: raw_logs 归一化（dict → paths/files → [dict]；str → [str]）
	rawLogsValue := execution["runtime_logs"]
	var rawLogs []any
	if logsMap, isMap := rawLogsValue.(map[string]any); isMap {
		if paths, ok := logsMap["paths"].([]any); ok {
			rawLogs = paths
		} else if files, ok := logsMap["files"].([]any); ok {
			rawLogs = files
		} else {
			rawLogs = []any{logsMap}
		}
	} else if str, isStr := rawLogsValue.(string); isStr {
		rawLogs = []any{str}
	}
	// Python: declared = isinstance(raw_logs, list) and bool(raw_logs)
	declared := rawLogs != nil && len(rawLogs) > 0

	var sources []string
	if declared {
		for _, item := range rawLogs {
			sources = append(sources, localSource(caseDir, item, "logs"))
		}
	} else {
		// Python: sorted((case_dir / "logs").rglob("*.jsonl"))
		sources = rglobJSONL(filepath.Join(caseDir, "logs"))
	}

	// Python: 目录展开为子文件清单
	var expanded []string
	for _, source := range sources {
		if source != "" && isDir(source) {
			expanded = append(expanded, rglobJSONL(source)...)
		} else {
			expanded = append(expanded, source)
		}
	}

	// Python: dict.fromkeys(expanded) 保序去重；[:32]
	deduped := dedupeOrder(expanded)
	if len(deduped) > maxLogFiles {
		deduped = deduped[:maxLogFiles]
	}

	remaining := maxLogTotalBytes
	remainingSource := maxLogTotalBytes
	logs := manifest["logs"].([]any)
	for index, source := range deduped {
		sourceLabel := source
		if source == "" {
			sourceLabel = "outside_case_rejected"
		}
		row := map[string]any{"source": sourceLabel, "status": "unknown"}
		logs = append(logs, row)
		// Python: source None / 越界 / 非 file → continue
		if source == "" || !withinBase(resolvePath(source), caseDirResolved) || !isFile(source) {
			continue
		}
		target := filepath.Join(destination, fmt.Sprintf("invocation_%03d.jsonl", index+1))
		copied, copyErr := copyLog(source, target, minInt(maxLogFileBytes, remainingSource), remaining)
		if copyErr != nil {
			// Python: except OSError: row["status"] = "unreadable"
			row["status"] = "unreadable"
			continue
		}
		remainingSource -= intField(copied, "bytes_examined", 0)
		remaining -= intField(copied, "copied_bytes", 0)
		for key, value := range copied {
			row[key] = value
		}
		rel, _ := filepath.Rel(runtimeDir, target)
		row["path"] = filepath.ToSlash(rel)
	}
	manifest["logs"] = logs

	// Python: if not manifest["logs"]: missing += case_scoped_runtime_logs
	if len(logs) == 0 {
		manifest["missing"] = append(manifest["missing"].([]any), "case_scoped_runtime_logs")
	}
	// Python: manifest["log_files_omitted"] = max(0, len(expanded) - 32)
	omittedFiles := len(expanded) - maxLogFiles
	if omittedFiles < 0 {
		omittedFiles = 0
	}
	manifest["log_files_omitted"] = omittedFiles

	// Python: manifest["initial_search"] = _initial_log_search(runtime_dir, manifest)
	manifest["initial_search"] = initialLogSearch(runtimeDir, manifest)

	// Python: path = runtime_dir / "evidence_manifest.json"; write_text(...)
	manifestContent, _ := json.MarshalIndent(manifest, "", "  ")
	_ = os.WriteFile(filepath.Join(runtimeDir, "evidence_manifest.json"), manifestContent, 0o644)
	return manifest
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// redactValue 递归净化敏感信息。
// Python: runtime_evidence.py:50-61 _redact
func redactValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			if secretKeyRE.MatchString(key) {
				out[key] = "[REDACTED]"
			} else {
				out[key] = redactValue(item)
			}
		}
		return out
	case []any:
		limit := len(v)
		if limit > 50 {
			limit = 50
		}
		result := make([]any, 0, limit+1)
		for _, item := range v[:limit] {
			result = append(result, redactValue(item))
		}
		if len(v) > 50 {
			result = append(result, map[string]any{"truncated_items": len(v) - 50})
		}
		return result
	case string:
		// Python: if _PAYLOAD_TEXT.search(value): return "[Structured prompt/model payload omitted]"
		if payloadTextRE.MatchString(v) {
			return "[Structured prompt/model payload omitted]"
		}
		// Python: _SECRET_TEXT.sub(lambda m: (m.group(1) or m.group(2) or "") + "[REDACTED]", value)
		return secretTextRE.ReplaceAllStringFunc(v, func(match string) string {
			groups := secretTextRE.FindStringSubmatch(match)
			prefix := groups[1]
			if prefix == "" {
				prefix = groups[2]
			}
			return prefix + "[REDACTED]"
		})
	default:
		return value
	}
}

// projectValue 白名单投影递归。
// Python: runtime_evidence.py:64-69 _project
func projectValue(value any, fields map[string]bool) any {
	if m, isMap := value.(map[string]any); isMap {
		out := map[string]any{}
		for key, item := range m {
			if fields[key] {
				out[key] = projectValue(item, fields)
			}
		}
		return out
	}
	if list, isList := value.([]any); isList {
		limit := len(list)
		if limit > 50 {
			limit = 50
		}
		result := make([]any, 0, limit)
		for _, item := range list[:limit] {
			result = append(result, projectValue(item, fields))
		}
		return result
	}
	return redactValue(value)
}

// localSource 解析案例本地来源路径；越界返回空串。
// Python: runtime_evidence.py:72-77 _local_source
func localSource(caseDir string, value any, fallback string) string {
	// Python: if isinstance(value, dict): value = value.get("path", value.get("file", default))
	if m, isMap := value.(map[string]any); isMap {
		if path, ok := m["path"]; ok {
			value = path
		} else if file, ok := m["file"]; ok {
			value = file
		} else {
			value = fallback
		}
	}
	str, isStr := value.(string)
	if !isStr || str == "" {
		str = fallback
	}
	// Python: source 相对则拼 case_dir；resolve()
	source := str
	if !filepath.IsAbs(source) {
		source = filepath.Join(caseDir, source)
	}
	source = resolvePath(source)
	// Python: return source if source.is_relative_to(case_dir.resolve()) else None
	if withinBase(source, resolvePath(caseDir)) {
		return source
	}
	return ""
}

// copyLog 拷贝完整原生日志；历史文件超预算时先采样尾部再采样头部。
// Python: runtime_evidence.py:197-293 _copy_log
func copyLog(source, target string, sourceBudget, outputBudget int) (map[string]any, error) {
	info, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	sourceSize := int(info.Size())
	var windows [][2]int
	if sourceSize <= sourceBudget {
		windows = [][2]int{{0, sourceSize}}
	} else {
		// Python: head_size = source_budget // 2; tail_size = source_budget - head_size
		// windows = [(source_size - tail_size, source_size), (0, head_size)]（尾先头后）
		headSize := sourceBudget / 2
		tailSize := sourceBudget - headSize
		windows = [][2]int{{sourceSize - tailSize, sourceSize}, {0, headSize}}
	}

	consumed := 0
	copiedBytes := 0
	emitted := 0
	truncatedRecords := 0
	var identities []map[string]any
	identityKeys := map[string]bool{}
	sourceCaptureTruncated := any("unknown")
	var ranges []map[string]any
	outputExhausted := false

	reader, err := os.Open(source)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	writer, err := os.Create(target)
	if err != nil {
		return nil, err
	}
	bufWriter := bufio.NewWriter(writer)
	defer func() { _ = writer.Close() }()

	for _, window := range windows {
		start, end := window[0], window[1]
		if start == end || outputExhausted {
			continue
		}
		if _, seekErr := reader.Seek(int64(start), 0); seekErr != nil {
			return nil, seekErr
		}
		lineNumber := 0
		skipping := start > 0
		alignmentBytes := 0
		recordTruncated := false
		windowStart := start
		for start+consumed < end {
			// Python: reader.tell() 等价 = start + consumed
			offset := start + consumed
			// Python: raw = reader.readline(min(_MAX_RECORD_BYTES + 1, end - offset))
			raw, n := readLineLimited(reader, minInt(maxRecordBytes+1, end-offset), end)
			if n == 0 {
				break
			}
			consumed += n
			// Python: if skipping: 对齐到行界
			if skipping {
				alignmentBytes += n
				skipping = raw[n-1] != '\n'
				continue
			}
			if recordTruncated {
				recordTruncated = raw[n-1] != '\n'
				continue
			}
			lineNumber++
			sourceLine := any(nil)
			if start == 0 {
				sourceLine = lineNumber
			}
			// Python: record_truncated = not raw.endswith(b"\n") and (
			//     len(raw) >= _MAX_RECORD_BYTES or (reader.tell() == end and end < source_size))
			endsWithNewline := raw[n-1] == '\n'
			recordTruncated = !endsWithNewline && (n >= maxRecordBytes || (start+consumed == end && end < sourceSize))
			// Python: text = raw[:_MAX_RECORD_BYTES].decode("utf-8", errors="replace")
			clamped := raw
			if len(clamped) > maxRecordBytes {
				clamped = clamped[:maxRecordBytes]
			}
			text := strings.ToValidUTF8(string(clamped), "\uFFFD")
			var event any
			// Python: try: event = json.loads(text) except ValueError: event = {"excerpt": text}
			if json.Unmarshal([]byte(text), &event) != nil {
				event = map[string]any{"excerpt": text}
			}
			if eventMap, isMap := event.(map[string]any); isMap {
				identity := map[string]any{}
				for _, key := range []string{
					"agent_id", "session_id", "invocation_id", "parent_invocation_id",
					"role", "trace_id", "member_id",
				} {
					if value, ok := eventMap[key]; ok {
						identity[key] = truncateStr(toDisplayString(value), 160)
					}
				}
				// Python: if identity and identity not in identities and len(identities) < 8
				if len(identity) > 0 && len(identities) < 8 {
					canonical, _ := json.Marshal(identity)
					if !identityKeys[string(canonical)] {
						identityKeys[string(canonical)] = true
						identities = append(identities, redactValue(identity).(map[string]any))
					}
				}
				// Python: if "truncated" in event: source_capture_truncated = bool(...)
				if value, ok := eventMap["truncated"]; ok {
					if b, isBool := value.(bool); isBool {
						sourceCaptureTruncated = b
					} else {
						sourceCaptureTruncated = value != nil && value != "" && value != float64(0)
					}
				}
			}
			record := map[string]any{
				"source_line":        sourceLine,
				"source_byte_offset": offset,
				"record_truncated":   recordTruncated,
				"event":              redactValue(event),
			}
			encoded, _ := json.Marshal(record)
			// Python: if len(encoded.encode("utf-8")) > _MAX_RECORD_BYTES: 精简为 excerpt
			if len(encoded) > maxRecordBytes {
				delete(record, "event")
				record["record_truncated"] = true
				redactedEvent, _ := json.Marshal(redactValue(event))
				record["excerpt"] = truncateStr(string(redactedEvent), 1500)
				encoded, _ = json.Marshal(record)
			}
			if record["record_truncated"] == true {
				truncatedRecords++
			}
			size := len(encoded) + 1
			if copiedBytes+size > outputBudget {
				outputExhausted = true
				break
			}
			_, _ = bufWriter.Write(append(encoded, '\n'))
			copiedBytes += size
			emitted++
		}
		// Python: ranges.append({...})
		lineStart := any(nil)
		lineEnd := any(nil)
		if start == 0 && lineNumber > 0 {
			lineStart = 1
			lineEnd = lineNumber
		}
		ranges = append(ranges, map[string]any{
			"byte_start":              windowStart,
			"byte_end_exclusive":      start + consumed,
			"source_line_start":       lineStart,
			"source_line_end":         lineEnd,
			"alignment_bytes_omitted": alignmentBytes,
		})
		if outputExhausted {
			break
		}
	}
	_ = bufWriter.Flush()

	status := "budget_exhausted"
	if emitted > 0 || sourceSize == 0 {
		status = "available"
	}
	scanComplete := consumed == sourceSize
	isTruncated := consumed < sourceSize || truncatedRecords > 0 || outputExhausted
	return map[string]any{
		"status":                   status,
		"source_bytes":             sourceSize,
		"bytes_examined":           consumed,
		"copied_bytes":             copiedBytes,
		"source_ranges":            ranges,
		"records_copied":           emitted,
		"identities":               identities,
		"source_capture_truncated": sourceCaptureTruncated,
		"truncated_records":        truncatedRecords,
		"output_budget_exhausted":  outputExhausted,
		"scan_complete":            scanComplete,
		"truncated":                isTruncated,
		"redacted":                 true,
	}, nil
}

// initialLogSearch 对已拷贝记录提供有界的严重级优先检索索引。
// Python: runtime_evidence.py:300-351 _initial_log_search
func initialLogSearch(runtimeDir string, manifest map[string]any) map[string]any {
	type candidate struct {
		priority int
		hit      map[string]any
	}
	var candidates []candidate
	logs, _ := manifest["logs"].([]any)
	for _, rawItem := range logs {
		item, isMap := rawItem.(map[string]any)
		if !isMap || item["status"] != "available" {
			continue
		}
		path, _ := item["path"].(string)
		file, err := os.Open(filepath.Join(runtimeDir, path))
		if err != nil {
			continue
		}
		copiedLine := 0
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, maxRecordBytes+1024), maxRecordBytes*4)
		for scanner.Scan() {
			copiedLine++
			var record map[string]any
			if json.Unmarshal(scanner.Bytes(), &record) != nil {
				continue
			}
			event, isMap := record["event"].(map[string]any)
			if !isMap {
				continue
			}
			// Python: level = str(event.get("level", "")).upper()
			level := strings.ToUpper(stringField(event, "level", ""))
			summaryParts := []string{}
			for _, key := range []string{"message", "exception", "event_type", "status", "termination_reason", "error"} {
				if value, ok := event[key]; ok && value != nil {
					str, isStr := value.(string)
					if !isStr {
						str = toDisplayString(value)
					}
					if str != "" {
						summaryParts = append(summaryParts, str)
					}
				}
			}
			summary := strings.Join(summaryParts, " ")
			priority := 1
			if level == "WARNING" || level == "ERROR" || level == "CRITICAL" {
				priority = 0
			}
			// Python: if priority and not _TERMINATION_PATTERN.search(summary): continue
			if priority == 1 && !terminationPattern.MatchString(summary) {
				continue
			}
			byteOffset := intField(record, "source_byte_offset", 0)
			candidates = append(candidates, candidate{priority: priority, hit: map[string]any{
				"path":               path,
				"copied_line":        copiedLine,
				"source_line":        record["source_line"],
				"source_byte_offset": record["source_byte_offset"],
				"level":              level,
				"summary":            truncateStr(summary, 500),
				"summary_truncated":  len(summary) > 500,
				// 排序键与去重所需的原始值
				"_source_byte_offset_raw": byteOffset,
				"_priority":               priority,
			}})
		}
		_ = file.Close()
	}

	// Python: sorted(candidates, key=lambda item: (item[0], -(item[1]["source_byte_offset"] or 0)))
	sort.SliceStable(candidates, func(i, j int) bool {
		ci, cj := candidates[i], candidates[j]
		if ci.priority != cj.priority {
			return ci.priority < cj.priority
		}
		oi, _ := ci.hit["_source_byte_offset_raw"].(int)
		oj, _ := cj.hit["_source_byte_offset_raw"].(int)
		return oi > oj
	})

	var hits []map[string]any
	selected := map[string][]int{}
	totalBytes := 0
	omitted := 0
	for _, cand := range candidates {
		hit := cand.hit
		path, _ := hit["path"].(string)
		copiedLine := intField(hit, "copied_line", 0)
		nearby := selected[path]
		adjacent := false
		for _, line := range nearby {
			diff := copiedLine - line
			if diff < 0 {
				diff = -diff
			}
			if diff <= 1 {
				adjacent = true
				break
			}
		}
		if adjacent {
			continue
		}
		// Python: size = len(json.dumps(hit, ensure_ascii=False).encode("utf-8"))
		hitJSON, _ := json.Marshal(hit)
		size := len(hitJSON)
		if len(hits) >= 50 || totalBytes+size > 30000 {
			omitted++
			continue
		}
		// 移除排序辅助键后入清单
		publicHit := map[string]any{}
		for key, value := range hit {
			if !strings.HasPrefix(key, "_") {
				publicHit[key] = value
			}
		}
		hits = append(hits, publicHit)
		selected[path] = append(nearby, copiedLine)
		totalBytes += size
	}

	result := map[string]any{
		"policy": "WARNING/ERROR/CRITICAL first, newest records first within priority; " +
			"then termination keywords including INFO; adjacent hits deduplicated",
		"hits": hits, "omitted_matches": omitted, "max_hits": 50, "max_summary_chars": 500,
		"max_total_bytes": 32768, "truncated": omitted > 0,
		"notice": "Only bounded copied records were searched. " +
			"Missing/omitted content is unknown, not negative evidence.",
	}
	resultJSON, _ := json.Marshal(result)
	_ = os.WriteFile(filepath.Join(runtimeDir, "runtime_log_search.json"), resultJSON, 0o644)
	return map[string]any{
		"path":      "runtime_log_search.json",
		"hits":      len(hits),
		"truncated": omitted > 0,
	}
}

// readLineLimited 等价 Python file.readline(sizehint)：最多读 sizehint 字节，
// 遇行界即停（文件位置停在行界之后）。返回读到的字节与长度。
func readLineLimited(reader *os.File, limit, _ int) ([]byte, int) {
	if limit <= 0 {
		return nil, 0
	}
	buf := make([]byte, limit)
	n, err := reader.Read(buf)
	if err != nil && n == 0 {
		return nil, 0
	}
	// Python readline 遇 \n 停止：若块内含行界，须回退文件位置到行界之后
	if idx := indexOfByte(buf[:n], '\n'); idx >= 0 {
		consume := idx + 1
		if _, seekErr := reader.Seek(int64(consume-n), 1); seekErr == nil {
			n = consume
		}
	}
	return buf[:n], n
}

// indexOfByte 字节查找。
func indexOfByte(buf []byte, target byte) int {
	for i, b := range buf {
		if b == target {
			return i
		}
	}
	return -1
}

// rglobJSONL 递归收集 *.jsonl 文件（排序对齐 Python sorted(rglob)）。
func rglobJSONL(root string) []string {
	var out []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".jsonl") {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// dedupeOrder 保序去重（Python dict.fromkeys）。
func dedupeOrder(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

// resolvePath 等价 Python Path.resolve（Abs + 尽力 symlink 解析）。
func resolvePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// withinBase 判断路径是否位于 base 内（is_relative_to 语义）。
func withinBase(path, base string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// isFile 文件判断。
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// isDir 目录判断。
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// readJSONAny 读取 JSON 文件为 any。
func readJSONAny(path string) (any, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data any
	if err := json.Unmarshal(content, &data); err != nil {
		return nil, err
	}
	return data, nil
}

// minInt 双 int 取小。
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
