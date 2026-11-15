// case_reader.go 提供评估工件的文件系统读取层。
// Python: openjiuwen/rsi/harness_rsi/evaluation_result_analyzer/case_reader.py（全量对齐）。

package evaluationresultanalyzer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvaluationSummaryInput 分析器上下文使用的聚合评估汇总。
// Python: case_reader.py:16-22 EvaluationSummaryInput（frozen dataclass → Go struct，偏差 D-5）。
type EvaluationSummaryInput struct {
	// TotalCases 案例总数
	TotalCases int
	// PassedCount 通过数
	PassedCount int
	// FailedCount 失败数
	FailedCount int
	// AverageScore 平均分
	AverageScore float64
	// EvaluationMethod 评估方法
	EvaluationMethod string
}

// DeterministicSignals 评估案例输出的零 LLM 预提取信号。
// Python: case_reader.py:25-31 DeterministicSignals。
type DeterministicSignals struct {
	// Method 评估方法
	Method string
	// ExecFailures 执行失败案例 ID 清单
	ExecFailures []string
	// JudgeFailures 评估未通过案例 ID 清单
	JudgeFailures []string
	// ErrorClusters 错误聚类（元素含 fingerprint/cases，偏差 D-6）
	ErrorClusters []map[string]any
	// MethodSpecific 方法专属信号
	MethodSpecific map[string]any
}

// CaseAnalysisInput 单个评估案例的结构化输入（result + trace 组装）。
// Python: case_reader.py:34-54 CaseAnalysisInput。
type CaseAnalysisInput struct {
	// CaseID 案例 ID
	CaseID string
	// Status 执行状态
	Status string
	// Score 分数
	Score float64
	// Input 案例输入
	Input string
	// Expected 期望输出（可缺失）
	Expected *string
	// Response 响应
	Response string
	// Error 错误
	Error string
	// EvaluationMethod 评估方法
	EvaluationMethod string
	// EvaluationPassed 是否通过
	EvaluationPassed bool
	// EvaluationReason 评估理由
	EvaluationReason string
	// EvaluationMetadata 评估元数据
	EvaluationMetadata map[string]any
	// TracePath 轨迹文件路径
	TracePath string
	// ResultPath 结果文件路径
	ResultPath string
	// TrajectoryWindowSummary 有界轨迹窗口摘要
	TrajectoryWindowSummary map[string]any
	// NormalizedTraceSummary 有界归一化轨迹摘要
	NormalizedTraceSummary map[string]any
	// TrainingSignal 训练信号
	TrainingSignal map[string]any
	// BenchmarkTestContract 基准测试契约
	BenchmarkTestContract map[string]any
	// TerminationReason 终止原因
	TerminationReason string
	// ExecutionMetadata 执行元数据
	ExecutionMetadata map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCaseReader 创建评估工件读取器。
// Python: case_reader.py:56 CaseReader（静态方法容器 → Go 空 struct 方法，偏差 D-1'）。
func NewCaseReader() *CaseReader { return &CaseReader{} }

// CaseReader 评估工件读取器（Python 静态方法容器）。
type CaseReader struct{}

// ReadEvalRef 载入 eval_ref YAML 文件并返回 mapping。
// Python: case_reader.py:59-70 CaseReader.read_eval_ref
func (r *CaseReader) ReadEvalRef(path string) (map[string]any, error) {
	// Python: if not p.exists(): raise ValueError(...)
	if _, statErr := os.Stat(path); statErr != nil {
		return nil, errEvalRefPathNotFound(path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := yaml.Unmarshal(content, &data); err != nil {
		return nil, err
	}
	if data == nil {
		data = map[string]any{}
	}
	return data, nil
}

// ReadSummary 载入 summary.json 并映射为 EvaluationSummaryInput。
// G4 字段映射：passed_cases → passed_count、failed_cases → failed_count；
// path 为空或文件不存在时返回零值汇总。
// Python: case_reader.py:72-101 CaseReader.read_summary
func (r *CaseReader) ReadSummary(path string) EvaluationSummaryInput {
	if path == "" {
		return EvaluationSummaryInput{}
	}
	// Python: if not p.exists(): return EvaluationSummaryInput()
	content, err := os.ReadFile(path)
	if err != nil {
		return EvaluationSummaryInput{}
	}
	var data map[string]any
	if err := json.Unmarshal(content, &data); err != nil {
		return EvaluationSummaryInput{}
	}
	return EvaluationSummaryInput{
		// Python: int(data.get("total_cases", 0))
		TotalCases:  intField(data, "total_cases", 0),
		PassedCount: intField(data, "passed_cases", intField(data, "passed_count", 0)),
		FailedCount: intField(data, "failed_cases", intField(data, "failed_count", 0)),
		// Python: float(data.get("average_score", 0.0))
		AverageScore:     floatField(data, "average_score", 0.0),
		EvaluationMethod: stringField(data, "evaluation_method", ""),
	}
}

// ReadCaseInputs 扫描案例结果目录并构建 CaseAnalysisInput 列表。
// 对目录下每个 */result.json 同时读取同级 trace.json 获取 input/response（G3）。
// 目录不存在返回空列表；按案例目录名排序。
// Python: case_reader.py:103-184 CaseReader.read_case_inputs
func (r *CaseReader) ReadCaseInputs(directory string) []CaseAnalysisInput {
	// Python: if not case_results_dir.is_dir(): return []
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}

	// Python: sorted(case_results_dir.glob("*/result.json"))
	caseDirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			caseDirs = append(caseDirs, filepath.Join(directory, entry.Name()))
		}
	}
	sort.Strings(caseDirs)

	results := make([]CaseAnalysisInput, 0, len(caseDirs))
	// Python: dataset_case_cache: dict[Path, dict[str, dict[str, Any]]] = {}
	datasetCaseCache := map[string]map[string]map[string]any{}

	for _, caseDir := range caseDirs {
		resultPath := filepath.Join(caseDir, "result.json")
		tracePath := filepath.Join(caseDir, "trace.json")

		// Python: result_data = json.loads(result_path.read_text(...))
		resultData, err := readJSONMapping(resultPath)
		if err != nil {
			continue
		}
		// Python: trace_data = {} if not exists else json.loads(...)
		traceData := map[string]any{}
		if traceContent, traceErr := os.ReadFile(tracePath); traceErr == nil {
			_ = json.Unmarshal(traceContent, &traceData)
		}

		// Python: evaluation = result_data.get("evaluation") or {}
		evaluation := mapField(resultData, "evaluation")
		// Python: eval_metadata = evaluation.get("metadata") or {}
		evalMetadata := mapField(evaluation, "metadata")
		// Python: result_metadata = result_data.get("metadata") or {}
		resultMetadata := mapField(resultData, "metadata")
		// Python: training_signal = result_metadata.get("training_signal") if isinstance(dict) else {}
		trainingSignal := mapField(resultMetadata, "training_signal")
		// Python: case_id = str(result_data.get("case_id", case_dir.name))
		caseID := stringField(resultData, "case_id", filepath.Base(caseDir))

		// Python: benchmark_test_contract = _read_benchmark_test_contract(...)
		benchmarkTestContract := readBenchmarkTestContract(caseID, resultMetadata, datasetCaseCache)

		// Python: score=float(result_data.get("score") or 0.0)
		score := floatField(resultData, "score", 0.0)
		// Python: input=str(trace_data.get("input", ""))
		caseInput := stringField(traceData, "input", "")
		// Python: response=str(trace_data.get("response", result_data.get("result", "")))
		response := stringField(traceData, "response", stringField(resultData, "result", ""))
		// Python: error=str(result_data.get("error") or "")
		errorStr := stringField(resultData, "error", "")

		// Python: termination_reason=_read_termination_reason(trace_data, result_data)
		terminationReason := readTerminationReason(traceData, resultData)

		// Python: execution_metadata 条件取值
		executionMetadata := map[string]any{}
		if exec, ok := resultMetadata["execution"].(map[string]any); ok {
			executionMetadata = exec
		}

		// Python: expected=None（恒为 None，见 Python 153-181 字面量）
		results = append(results, CaseAnalysisInput{
			CaseID:                  caseID,
			Status:                  stringField(resultData, "status", ""),
			Score:                   score,
			Input:                   caseInput,
			Expected:                nil,
			Response:                response,
			Error:                   errorStr,
			EvaluationMethod:        stringField(evaluation, "method", ""),
			EvaluationPassed:        boolField(evaluation, "passed", false),
			EvaluationReason:        stringField(evaluation, "reason", ""),
			EvaluationMetadata:      evalMetadata,
			TracePath:               tracePath,
			ResultPath:              resultPath,
			TrajectoryWindowSummary: boundedTrajectoryWindowSummary(mapField(traceData, "behavior_trace")),
			NormalizedTraceSummary:  boundedNormalizedTraceSummary(mapField(traceData, "behavior_trace"), caseDir),
			TrainingSignal:          trainingSignal,
			BenchmarkTestContract:   benchmarkTestContract,
			TerminationReason:       terminationReason,
			ExecutionMetadata:       executionMetadata,
		})
	}

	return results
}

// ProjectExecutionHistory 保留记录的观察与引用，剔除 provider/配置元数据。
// Python: case_reader.py:385-413 project_execution_history
func ProjectExecutionHistory(traceData map[string]any) map[string]any {
	// Python: traces = trace_data.get("traces", []) if isinstance(trace_data, dict) else []
	traces, _ := traceData["traces"].([]any)
	history := make([]map[string]any, 0, len(traces))
	for _, rawTrace := range traces {
		trace, isMap := rawTrace.(map[string]any)
		if !isMap {
			continue
		}
		messagesRaw, messagesIsList := trace["messages"].([]any)
		if !messagesIsList {
			continue
		}

		messages := make([]map[string]any, 0, len(messagesRaw))
		for _, rawMessage := range messagesRaw {
			message, isMap := rawMessage.(map[string]any)
			if !isMap {
				continue
			}
			// Python: if message.get("role") in ("system", "developer"): continue
			role := stringField(message, "role", "")
			if role == "system" || role == "developer" {
				continue
			}
			// Python: row = {key: message[key] for key in message_fields if key in message}
			row := map[string]any{}
			for _, key := range []string{"role", "content", "message_index", "step_pointer"} {
				if value, ok := message[key]; ok {
					row[key] = value
				}
			}
			// Python: row["tool_calls"] = [...]（call_fields 过滤）
			calls, _ := message["tool_calls"].([]any)
			filteredCalls := make([]map[string]any, 0, len(calls))
			for _, rawCall := range calls {
				call, isMap := rawCall.(map[string]any)
				if !isMap {
					continue
				}
				filtered := map[string]any{}
				for _, key := range []string{"name", "input", "output", "error", "step_pointer"} {
					if value, ok := call[key]; ok {
						filtered[key] = value
					}
				}
				filteredCalls = append(filteredCalls, filtered)
			}
			row["tool_calls"] = filteredCalls
			messages = append(messages, row)
		}

		// Python: "role": trace.get("member_role", trace.get("role", ""))
		traceRole := stringField(trace, "member_role", stringField(trace, "role", ""))
		history = append(history, map[string]any{
			"trace_id": stringField(trace, "trace_id", ""),
			"role":     traceRole,
			"messages": messages,
		})
	}
	return map[string]any{"traces": history}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// errEvalRefPathNotFound 构造 Python ValueError 等价错误。
// Python: case_reader.py:68 raise ValueError(f"eval_ref path not found: {path}")
func errEvalRefPathNotFound(path string) error {
	return &evalRefPathError{path: path}
}

// evalRefPathError eval_ref 路径不存在错误（Python ValueError 等价）。
type evalRefPathError struct {
	path string
}

func (e *evalRefPathError) Error() string {
	return "eval_ref path not found: " + e.path
}

// readBenchmarkTestContract 读取当前案例的验收测试，不暴露其 solution patch。
// Python: case_reader.py:187-229 _read_benchmark_test_contract
func readBenchmarkTestContract(
	caseID string,
	resultMetadata map[string]any,
	datasetCaseCache map[string]map[string]map[string]any,
) map[string]any {
	// Python: raw_case_path = result_metadata.get("case_path")
	rawCasePath, isStr := resultMetadata["case_path"].(string)
	if !isStr || strings.TrimSpace(rawCasePath) == "" {
		return map[string]any{}
	}

	// Python: dataset_path 相对则基于 cwd；resolve()
	datasetPath, err := expandUserPath(rawCasePath)
	if err != nil {
		return map[string]any{}
	}
	if !filepath.IsAbs(datasetPath) {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			return map[string]any{}
		}
		datasetPath = filepath.Join(cwd, datasetPath)
	}
	if absPath, absErr := filepath.Abs(datasetPath); absErr == nil {
		datasetPath = absPath
	}

	// Python: cases_by_id 缓存
	casesByID, cached := datasetCaseCache[datasetPath]
	if !cached {
		casesByID = readDatasetCases(datasetPath)
		datasetCaseCache[datasetPath] = casesByID
	}

	caseData, exists := casesByID[caseID]
	if !exists {
		return map[string]any{}
	}
	// Python: contract = case_data.get("verification_contract")
	contract, isMap := caseData["verification_contract"].(map[string]any)
	if !isMap {
		return map[string]any{}
	}

	// Python: fail_to_pass = _test_id_list(contract.get("fail_to_pass", contract.get("must_pass")))
	failToPass := testIDList(valueOr(contract, "fail_to_pass", contract["must_pass"]))
	passToPass := testIDList(valueOr(contract, "pass_to_pass", contract["regression"]))
	// Python: test_patch = contract.get("test_patch", contract.get("acceptance_probe"))
	testPatch, _ := valueOr(contract, "test_patch", contract["acceptance_probe"]).(string)
	if !failToPassHasItems(failToPass) && !passToPassHasItems(passToPass) && testPatch == "" {
		return map[string]any{}
	}

	return map[string]any{
		"provenance":   fmtProvenance(datasetPath, caseID),
		"fail_to_pass": failToPass,
		"pass_to_pass": passToPass,
		"test_patch":   testPatch,
	}
}

// readDatasetCases 载入数据集文件并按 case_id 索引。
// Python: case_reader.py:232-247 _read_dataset_cases
func readDatasetCases(datasetPath string) map[string]map[string]any {
	cases := map[string]map[string]any{}
	content, err := os.ReadFile(datasetPath)
	if err != nil {
		return cases
	}
	var dataset map[string]any
	if jsonErr := json.Unmarshal(content, &dataset); jsonErr != nil {
		return cases
	}
	// Python: raw_cases = dataset.get("cases") if isinstance(dataset, dict) else dataset
	rawCases, isList := dataset["cases"].([]any)
	if !isList {
		return cases
	}
	for _, rawCase := range rawCases {
		caseData, isMap := rawCase.(map[string]any)
		if !isMap {
			continue
		}
		// Python: case_id = str(case.get("case_id", case.get("instance_id", ""))).strip()
		caseID := strings.TrimSpace(stringField(caseData, "case_id", stringField(caseData, "instance_id", "")))
		if caseID != "" {
			cases[caseID] = caseData
		}
	}
	return cases
}

// testIDList 归一化测试 ID 清单（字符串 JSON 解码或单元素包装）。
// Python: case_reader.py:250-259 _test_id_list
func testIDList(value any) []string {
	if str, isStr := value.(string); isStr {
		var decoded any
		if json.Unmarshal([]byte(str), &decoded) == nil {
			if list, isList := decoded.([]any); isList {
				value = list
			}
		}
		// Python: value = decoded if isinstance(decoded, list) else [value]
		// （解码失败/非清单时包装为单元素清单）
		if _, stillStr := value.(string); stillStr {
			value = []any{str}
		}
	}
	list, isList := value.([]any)
	if !isList {
		return nil
	}
	var result []string
	for _, item := range list {
		if str := toDisplayString(item); strings.TrimSpace(str) != "" {
			result = append(result, str)
		}
	}
	return result
}

// boundedTrajectoryWindowSummary 从 trace.json 读取有界轨迹窗口摘要。
// Python: case_reader.py:262-285 _bounded_trajectory_window_summary
func boundedTrajectoryWindowSummary(behaviorTrace map[string]any) map[string]any {
	summary, isMap := behaviorTrace["trajectory_window_summary"].(map[string]any)
	if !isMap {
		return map[string]any{}
	}
	bounded := map[string]any{
		"window_size":        summary["window_size"],
		"event_count":        summary["event_count"],
		"failure_signatures": stringList(summary["failure_signatures"], 20),
	}
	if recentEvents, isList := summary["recent_events"].([]any); isList {
		events := make([]map[string]any, 0, len(recentEvents))
		for _, rawEvent := range recentEvents {
			event, isMap := rawEvent.(map[string]any)
			if !isMap {
				continue
			}
			events = append(events, map[string]any{
				"event_index": event["event_index"],
				"event_type":  excerptStr(toDisplayString(event["event_type"]), 80),
				"summary":     excerptStr(toDisplayString(event["summary"]), 1200),
			})
		}
		bounded["recent_events"] = events
	}
	return bounded
}

// boundedNormalizedTraceSummary 读取用于归因的有界归一化轨迹摘要。
// Python: case_reader.py:288-333 _bounded_normalized_trace_summary
func boundedNormalizedTraceSummary(behaviorTrace map[string]any, caseDir string) map[string]any {
	// Python: trace_path = _resolve_normalized_trace_path(...)
	tracePath := resolveNormalizedTracePath(behaviorTrace, caseDir)
	if tracePath == "" {
		return map[string]any{}
	}
	content, err := os.ReadFile(tracePath)
	if err != nil {
		return map[string]any{}
	}
	var data map[string]any
	if json.Unmarshal(content, &data) != nil {
		return map[string]any{}
	}

	traces, _ := data["traces"].([]any)
	boundedTraces := make([]map[string]any, 0, len(traces))
	for i, rawTrace := range traces {
		if i >= 4 {
			break
		}
		trace, isMap := rawTrace.(map[string]any)
		if !isMap {
			continue
		}
		messages, _ := trace["messages"].([]any)
		boundedMessages := make([]map[string]any, 0, len(messages))
		for j, rawMessage := range messages {
			if j >= 40 {
				break
			}
			message, isMap := rawMessage.(map[string]any)
			if !isMap {
				continue
			}
			boundedMessages = append(boundedMessages, map[string]any{
				"role":          excerptStr(toDisplayString(message["role"]), 80),
				"message_index": message["message_index"],
				"content":       excerptStr(toDisplayString(message["content"]), 1200),
				"step_pointer":  excerptStr(toDisplayString(message["step_pointer"]), 120),
				"tool_calls":    boundedToolCalls(message["tool_calls"]),
			})
		}
		boundedTraces = append(boundedTraces, map[string]any{
			"trace_id":      excerptStr(toDisplayString(trace["trace_id"]), 240),
			"member_id":     excerptStr(toDisplayString(trace["member_id"]), 160),
			"member_role":   excerptStr(toDisplayString(trace["member_role"]), 160),
			"execution_id":  excerptStr(toDisplayString(trace["execution_id"]), 160),
			"step_count":    trace["step_count"],
			"message_count": trace["message_count"],
			"messages":      boundedMessages,
		})
	}
	return map[string]any{
		"case_id": excerptStr(toDisplayString(data["case_id"]), 160),
		"traces":  boundedTraces,
	}
}

// resolveNormalizedTracePath 解析归一化轨迹文件路径（相对基于案例目录）。
// Python: case_reader.py:336-343 _resolve_normalized_trace_path
func resolveNormalizedTracePath(behaviorTrace map[string]any, caseDir string) string {
	if rawPath, isStr := behaviorTrace["normalized_trace_path"].(string); isStr && rawPath != "" {
		if filepath.IsAbs(rawPath) {
			return rawPath
		}
		return filepath.Join(caseDir, rawPath)
	}
	return filepath.Join(caseDir, "judge", "normalized_trace.json")
}

// boundedToolCalls 有界工具调用投影。
// Python: case_reader.py:346-362 _bounded_tool_calls
func boundedToolCalls(value any) []map[string]any {
	calls, isList := value.([]any)
	if !isList {
		return nil
	}
	result := make([]map[string]any, 0, len(calls))
	for i, rawCall := range calls {
		if i >= 8 {
			break
		}
		call, isMap := rawCall.(map[string]any)
		if !isMap {
			continue
		}
		result = append(result, map[string]any{
			"name":         excerptStr(toDisplayString(call["name"]), 120),
			"input":        excerptStr(toDisplayString(call["input"]), 1200),
			"output":       excerptStr(toDisplayString(call["output"]), 1200),
			"error":        excerptStr(toDisplayString(call["error"]), 1200),
			"step_pointer": excerptStr(toDisplayString(call["step_pointer"]), 120),
		})
	}
	return result
}

// stringList 字符串清单投影（前 limit 个）。
// Python: case_reader.py:365-368 _string_list
func stringList(value any, limit int) []string {
	list, isList := value.([]any)
	if !isList {
		return nil
	}
	result := make([]string, 0, len(list))
	for i, item := range list {
		if i >= limit {
			break
		}
		result = append(result, toDisplayString(item))
	}
	return result
}

// excerptStr 截断文本并追加省略号。
// Python: case_reader.py:371-374 _excerpt
func excerptStr(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	return text[:maxChars-3] + "..."
}

// readTerminationReason 在响应拍平为文本前读取结构化终止原因。
// Python: case_reader.py:416-421 _read_termination_reason
func readTerminationReason(traceData, resultData map[string]any) string {
	for _, value := range []any{traceData["response"], resultData["result"], resultData["metadata"]} {
		if m, isMap := value.(map[string]any); isMap {
			if reason, ok := m["termination_reason"]; ok {
				if str, isStr := reason.(string); isStr && str != "" {
					return str
				}
			}
		}
	}
	return ""
}

// readJSONMapping 读取 JSON 文件为 mapping。
func readJSONMapping(path string) (map[string]any, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(content, &data); err != nil {
		return nil, err
	}
	return data, nil
}

// expandUserPath 等价 Python Path.expanduser。
func expandUserPath(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
			return filepath.Join(home, path[2:]), nil
		}
	}
	return path, nil
}

// intField 读取 int 字段（Python int(...) 宽转换：float/int/字符串数字）。
func intField(data map[string]any, key string, fallback int) int {
	raw, ok := data[key]
	if !ok {
		return fallback
	}
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return parsed
		}
	}
	return fallback
}

// floatField 读取 float 字段。
func floatField(data map[string]any, key string, fallback float64) float64 {
	raw, ok := data[key]
	if !ok {
		return fallback
	}
	switch v := raw.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return parsed
		}
	}
	return fallback
}

// boolField 读取 bool 字段。
func boolField(data map[string]any, key string, fallback bool) bool {
	if v, ok := data[key].(bool); ok {
		return v
	}
	return fallback
}

// valueOr 等价 Python value if key in mapping else fallback（get(k, fallback)）。
func valueOr(data map[string]any, key string, fallback any) any {
	if value, ok := data[key]; ok {
		return value
	}
	return fallback
}

// toDisplayString 等价 Python str(value) 的显示转换（JSON 兼容值）。
func toDisplayString(value any) string {
	switch v := value.(type) {
	case nil:
		return "None"
	case string:
		return v
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return "None"
		}
		return string(raw)
	}
}

// failToPassHasItems 清单非空判断。
func failToPassHasItems(list []string) bool { return len(list) > 0 }

// passToPassHasItems 清单非空判断。
func passToPassHasItems(list []string) bool { return len(list) > 0 }

// fmtProvenance 生成 provenance 字符串。
// Python: f"{dataset_path}#case_id={case_id}.verification_contract"
func fmtProvenance(datasetPath, caseID string) string {
	return datasetPath + "#case_id=" + caseID + ".verification_contract"
}

// stringField 读取 mapping 的字符串字段（缺失/类型不符取默认值）。
func stringField(data map[string]any, key, fallback string) string {
	if raw, ok := data[key]; ok {
		if str, isStr := raw.(string); isStr {
			return str
		}
	}
	return fallback
}

// mapField 读取 mapping 字段（缺失/类型不符得空 map）。
// Python: x.get("k") or {}（falsy/None 均得空 mapping）。
func mapField(data map[string]any, key string) map[string]any {
	if raw, ok := data[key]; ok {
		if m, isMap := raw.(map[string]any); isMap {
			return m
		}
	}
	return map[string]any{}
}
