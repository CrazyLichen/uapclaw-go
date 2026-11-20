package trajectory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	obs "github.com/uapclaw/uapclaw-go/internal/agent_teams/observability"
)

// windows.go 轨迹 v2 事件状态所回放的上下文窗口。
// 对齐 Python: agent_evolving/trajectory/windows.py（全文，2026-12-04 快照）。
// 每次模型请求经 context.window.commit 提交它发送的窗口；只有 epoch 基线
// 陈述完整窗口，之后的每次提交陈述其相对前一窗口的变更，且消息在其留在
// 上下文期间保持同一 message_id。回放该链回答"模型实际读到了什么"。
// 全部为 span 的纯函数。

// ──────────────────────────── 常量 ────────────────────────────

// ContextWindowCommit 上下文窗口提交事件类别。
// Python: windows.py CONTEXT_WINDOW_COMMIT。
const ContextWindowCommit = "context.window.commit"

// CompactionCompleted 压缩完成事件类别。
// Python: windows.py COMPACTION_COMPLETED。
const CompactionCompleted = "compaction.completed"

// EpochBaseline / TrimBaseline 基线类型。
// Python: windows.py EPOCH_BASELINE / TRIM_BASELINE。
const (
	EpochBaseline = "epoch_baseline"
	TrimBaseline  = "trim_baseline"
)

// baselineReasons 事件类别 → 基线原因。
// Python: windows.py _BASELINE_REASONS。
var baselineReasons = map[string]string{EpochBaseline: "runtime_epoch_start", TrimBaseline: "trimmed_window"}

// deltaOperations / messageOrigins 闭集。
// Python: windows.py _DELTA_OPERATIONS / _MESSAGE_ORIGINS。
var deltaOperations = map[string]struct{}{"insert": {}, "remove": {}, "move": {}, "replace": {}}
var messageOrigins = map[string]struct{}{"external_user": {}, "harness_internal": {}}

// ──────────────────────────── 结构体 ────────────────────────────

// TrajectoryEvent 从 span 读出的一个 v2 轨迹事件。
// Python: windows.py TrajectoryEvent(frozen dataclass)。
type TrajectoryEvent struct {
	Span          map[string]any
	EventID       string
	EventKind     string
	SubjectID     string
	SequenceEpoch string
	Sequence      int
	RecordedAt    int64
	Payload       map[string]any
}

// SpanID 事件所属 span 的 ID。
func (e *TrajectoryEvent) SpanID() string { return fmt.Sprint(e.Span["spanId"]) }

// ParentSpanID 事件所属 span 的父 ID。
func (e *TrajectoryEvent) ParentSpanID() string { return fmt.Sprint(e.Span["parentSpanId"]) }

// WindowReplay 轨迹提交链回放结果。
// Python: windows.py WindowReplay(frozen dataclass)。
type WindowReplay struct {
	// Windows subject_id -> window_id -> 窗口消息序列。
	Windows map[string]map[string][]map[string]any
	// ByInference 推理 span ID → 该请求读到的窗口；压缩提交不在此。
	ByInference map[string][]map[string]any
	// Issues 按回放序排列的链缺陷。
	Issues []map[string]any
}

// replayIssue 构造 issue 映射。
// Python: windows.py _issue。
func replayIssue(code, message string, event *TrajectoryEvent) map[string]any {
	data := map[string]any{"code": code, "message": message}
	if event != nil {
		data["subject_id"] = event.SubjectID
		data["event_id"] = event.EventID
		data["sequence"] = event.Sequence
	}
	return data
}

// positiveInt 属性值转非负 int（非法返 -1 表示 None）。
// Python: windows.py _positive_int。
func positiveInt(value any) int64 {
	switch v := value.(type) {
	case int:
		if v >= 0 {
			return int64(v)
		}
	case int64:
		if v >= 0 {
			return v
		}
	case float64:
		if v >= 0 {
			return int64(v)
		}
	case string:
		var parsed int64
		if _, err := fmt.Sscanf(v, "%d", &parsed); err == nil && parsed >= 0 {
			return parsed
		}
	}
	return -1
}

// nonEmptyStr 非空字符串判定。
// Python: windows.py _non_empty_str。
func nonEmptyStr(value any) bool {
	text, ok := value.(string)
	return ok && text != ""
}

// hasText 有非空白内容的字符串。
// Python: windows.py _has_text。
func hasText(value any) bool {
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) != ""
}

// declaredConflict 压缩声明的窗口 ID 与陈述的是否不同。
// Python: windows.py _declared_conflict。
func declaredConflict(declared, stated any) bool {
	text, ok := declared.(string)
	return ok && text != fmt.Sprint(stated)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ReadTrajectoryEvent 返回 span 陈述的 v2 事件，其余 span 返 nil。
// 信封不完整的事件同样不可读，返回 nil。
// Python: windows.py read_trajectory_event。
func ReadTrajectoryEvent(span map[string]any) *TrajectoryEvent {
	attributes := SpanAttributes(span)
	eventKind, _ := attributes[obs.OJTrajectoryEventKind].(string)
	if eventKind == "" {
		return nil
	}
	eventID, _ := attributes[obs.OJTrajectoryEventID].(string)
	subjectID, _ := attributes[obs.OJTrajectorySubjectID].(string)
	sequenceEpoch, _ := attributes[obs.OJTrajectorySequenceEpoch].(string)
	sequence := positiveInt(attributes[obs.OJTrajectorySubjectSequence])
	recordedAt := positiveInt(attributes[obs.OJTrajectoryRecordedAtUnixNano])
	rawPayload := attributes[obs.OJTrajectoryPayload]
	var payload any
	if text, isString := rawPayload.(string); isString {
		if json.Unmarshal([]byte(text), &payload) != nil {
			payload = nil
		}
	} else {
		payload = rawPayload
	}
	if !nonEmptyStr(eventID) || !nonEmptyStr(subjectID) || !nonEmptyStr(sequenceEpoch) {
		return nil
	}
	if sequence < 0 || recordedAt < 0 {
		return nil
	}
	payloadMap, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	return &TrajectoryEvent{
		Span:          copyJSON(span).(map[string]any),
		EventID:       eventID,
		EventKind:     eventKind,
		SubjectID:     subjectID,
		SequenceEpoch: sequenceEpoch,
		Sequence:      int(sequence),
		RecordedAt:    recordedAt,
		Payload:       payloadMap,
	}
}

// IterTrajectoryEvents 返回轨迹或 span 列表中全部可读 v2 事件。
// 事件既可以是独立 span（窗口提交、压缩），也可以是记录在短命 owner span
// 上的 span event（ask_user）；后者以 owner 属性 + 自身属性合并读取。
// Python: windows.py iter_trajectory_events。
func IterTrajectoryEvents(value any) []*TrajectoryEvent {
	var spans []map[string]any
	if list, isList := value.([]map[string]any); isList {
		spans = list
	} else {
		spans = IterSpans(value)
	}
	var events []*TrajectoryEvent
	for _, span := range spans {
		event := ReadTrajectoryEvent(span)
		if event != nil {
			events = append(events, event)
			continue
		}
		spanEvents, _ := span["events"].([]any)
		for _, eventItem := range spanEvents {
			spanEvent, isMap := eventItem.(map[string]any)
			if !isMap {
				continue
			}
			mergedAttrs := SpanAttributes(span)
			for key, value := range SpanAttributes(spanEvent) {
				mergedAttrs[key] = value
			}
			synthetic := map[string]any{
				"name":       spanEvent["name"],
				"attributes": AttributesFromMap(mergedAttrs),
				"events":     []any{},
			}
			for key, value := range span {
				if _, exists := synthetic[key]; !exists {
					synthetic[key] = value
				}
			}
			logged := ReadTrajectoryEvent(synthetic)
			if logged != nil {
				events = append(events, logged)
			}
		}
	}
	return events
}

// orderedSubjectEvents 排序一个主体的事件：epoch 按首次记录时间，epoch 内按序号。
// Python: windows.py _ordered_subject_events。
func orderedSubjectEvents(events []*TrajectoryEvent) []*TrajectoryEvent {
	byEpoch := map[string][]*TrajectoryEvent{}
	var epochOrder []string
	firstRecorded := map[string]int64{}
	for _, event := range events {
		if _, exists := byEpoch[event.SequenceEpoch]; !exists {
			epochOrder = append(epochOrder, event.SequenceEpoch)
		}
		byEpoch[event.SequenceEpoch] = append(byEpoch[event.SequenceEpoch], event)
	}
	type epochKey struct {
		first int64
		epoch string
	}
	keys := make([]epochKey, 0, len(epochOrder))
	for _, epoch := range epochOrder {
		events := byEpoch[epoch]
		sort.SliceStable(events, func(i, j int) bool {
			a, b := events[i], events[j]
			if a.Sequence != b.Sequence {
				return a.Sequence < b.Sequence
			}
			if a.RecordedAt != b.RecordedAt {
				return a.RecordedAt < b.RecordedAt
			}
			return a.EventID < b.EventID
		})
		first := events[0].RecordedAt
		for _, e := range events {
			if e.RecordedAt < first {
				first = e.RecordedAt
			}
		}
		firstRecorded[epoch] = first
		keys = append(keys, epochKey{first, epoch})
	}
	sort.SliceStable(keys, func(i, j int) bool {
		if keys[i].first != keys[j].first {
			return keys[i].first < keys[j].first
		}
		return keys[i].epoch < keys[j].epoch
	})
	var result []*TrajectoryEvent
	for _, key := range keys {
		result = append(result, byEpoch[key.epoch]...)
	}
	return result
}

// contextMessage 校验窗口消息，非法返回 nil。
// Python: windows.py _context_message。
func contextMessage(value any) map[string]any {
	message, isMap := value.(map[string]any)
	if !isMap {
		return nil
	}
	origin, _ := message["origin"].(string)
	if _, validOrigin := messageOrigins[origin]; !validOrigin {
		return nil
	}
	if !hasText(message["message_id"]) || !hasText(message["role"]) {
		return nil
	}
	if sourceKind, exists := message["source_kind"]; exists && sourceKind != nil && !hasText(sourceKind) {
		return nil
	}
	return copyJSON(message).(map[string]any)
}

// contextDelta 校验窗口增量操作，非法返回 nil。
// Python: windows.py _context_delta。
func contextDelta(value any) map[string]any {
	operation, isMap := value.(map[string]any)
	if !isMap {
		return nil
	}
	op, _ := operation["op"].(string)
	if _, validOp := deltaOperations[op]; !validOp {
		return nil
	}
	messageID, _ := operation["message_id"].(string)
	if strings.TrimSpace(messageID) == "" {
		return nil
	}
	result := map[string]any{"op": op, "message_id": messageID}
	for _, key := range []string{"index", "from_index"} {
		if index := positiveInt(operation[key]); index >= 0 {
			// bool 不是有效索引。
			if _, isBool := operation[key].(bool); isBool {
				continue
			}
			result[key] = int(index)
		}
	}
	if messageValue, exists := operation["message"]; exists {
		message := contextMessage(messageValue)
		if message == nil {
			return nil
		}
		result["message"] = message
	}
	if op == "insert" || op == "replace" {
		if _, exists := result["message"]; !exists {
			return nil
		}
	}
	return result
}

// commitPayload 校验窗口提交载荷；不可回放返回 nil。
// Python: windows.py _commit_payload。
func commitPayload(payload map[string]any) map[string]any {
	windowID := payload["window_id"]
	baseWindowID := payload["base_window_id"]
	delta, deltaIsList := payload["delta"].([]any)
	if !hasText(windowID) || payload["complete"] != true || !deltaIsList {
		return nil
	}
	if baseWindowID != nil {
		if _, isString := baseWindowID.(string); !isString {
			return nil
		}
	}
	transitionKind, _ := payload["transition_kind"].(string)
	baseline := baselineReasons[transitionKind] != ""
	messages, messagesIsList := payload["messages"].([]any)
	if baseline && !messagesIsList {
		return nil
	}
	if messages != nil && !messagesIsList && payload["messages"] != nil {
		return nil
	}
	for _, key := range []string{
		"request_purpose", "baseline_reason", "correlation_kind", "transition_kind",
		"caused_by_operation_id", "output_window_id",
	} {
		if value := payload[key]; value != nil {
			if _, isString := value.(string); !isString {
				return nil
			}
		}
	}
	inputWindowID := payload["input_window_id"]
	if inputWindowID != nil {
		if _, isString := inputWindowID.(string); !isString {
			return nil
		}
	}
	var parsedMessages []map[string]any
	if payload["messages"] != nil {
		parsedMessages = make([]map[string]any, 0, len(messages))
		ids := map[string]int{}
		for _, item := range messages {
			parsed := contextMessage(item)
			if parsed == nil {
				return nil
			}
			id := fmt.Sprint(parsed["message_id"])
			if _, duplicated := ids[id]; duplicated {
				return nil
			}
			ids[id] = 1
			parsedMessages = append(parsedMessages, parsed)
		}
	}
	parsedDelta := make([]map[string]any, 0, len(delta))
	for _, item := range delta {
		parsed := contextDelta(item)
		if parsed == nil {
			return nil
		}
		parsedDelta = append(parsedDelta, parsed)
	}
	// 基线陈述自身原因；其余提交不陈述。
	statedReason := payload["baseline_reason"]
	if expected := baselineReasons[transitionKind]; (statedReason == nil) != (expected == "") || (statedReason != nil && statedReason != expected) {
		return nil
	}
	if baseline && (baseWindowID != nil || len(parsedDelta) > 0) {
		return nil
	}
	result := copyJSON(payload).(map[string]any)
	result["delta"] = parsedDelta
	if parsedMessages != nil {
		result["messages"] = parsedMessages
	}
	return result
}

// ApplyContextDelta 把一次提交的增量应用到其基于的窗口上。
// 操作按序应用，各自作用于前一个操作留下的窗口：insert 在索引处放置新
// 消息，remove 在当前位置删除，replace 原位换内容，move 拔出再插入。
// 返回重建的窗口；操作不适配窗口时返回 nil。
// Python: windows.py apply_context_delta。
func ApplyContextDelta(base []map[string]any, operations []map[string]any) []map[string]any {
	messages := make([]map[string]any, 0, len(base))
	for _, message := range base {
		messages = append(messages, copyJSON(message).(map[string]any))
	}
	for _, operation := range operations {
		messageID := fmt.Sprint(operation["message_id"])
		current := -1
		for i, message := range messages {
			if fmt.Sprint(message["message_id"]) == messageID {
				current = i
				break
			}
		}
		op, _ := operation["op"].(string)
		indexValue := positiveInt(operation["index"])
		switch op {
		case "insert":
			message, hasMessage := operation["message"].(map[string]any)
			if !hasMessage || message == nil || current != -1 {
				return nil
			}
			if indexValue < 0 || int(indexValue) > len(messages) {
				return nil
			}
			idx := int(indexValue)
			messages = append(messages, nil)
			copy(messages[idx+1:], messages[idx:])
			messages[idx] = copyJSON(message).(map[string]any)
			continue
		case "remove":
			if current == -1 {
				return nil
			}
			messages = append(messages[:current], messages[current+1:]...)
			continue
		case "replace":
			message, hasMessage := operation["message"].(map[string]any)
			if !hasMessage || message == nil || current == -1 {
				return nil
			}
			messages[current] = copyJSON(message).(map[string]any)
			continue
		case "move":
			if current == -1 || indexValue < 0 || int(indexValue) >= len(messages) {
				return nil
			}
			moved := messages[current]
			messages = append(messages[:current], messages[current+1:]...)
			idx := int(indexValue)
			messages = append(messages, nil)
			copy(messages[idx+1:], messages[idx:])
			messages[idx] = moved
			continue
		default:
			return nil
		}
	}
	return messages
}

// sameWindow 窗口内容等价判定（规范化 JSON 比较）。
// Python: windows.py _same_window。
func sameWindow(left, right []map[string]any) bool {
	canonical := func(value []map[string]any) string {
		items := make([]any, len(value))
		for i, message := range value {
			items[i] = message
		}
		data, _ := json.Marshal(toJsonCompatible(items))
		return string(data)
	}
	return canonical(left) == canonical(right)
}

// matchesCheckpoint 回放结果与提交陈述的完整窗口（如有）一致。
// Python: windows.py _matches_checkpoint。
func matchesCheckpoint(rebuilt, stated []map[string]any) bool {
	return rebuilt != nil && (stated == nil || sameWindow(rebuilt, stated))
}

// isCompactionCommit 是否压缩相关的提交。
// Python: windows.py _is_compaction_commit。
func isCompactionCommit(payload map[string]any) bool {
	transitionKind, _ := payload["transition_kind"].(string)
	correlationKind, _ := payload["correlation_kind"].(string)
	_, modelRequestsIsList := payload["model_requests"].([]any)
	return (transitionKind == "compaction" || correlationKind == "compaction") && modelRequestsIsList
}

// compactionCorrelationIssue 解释提交陈述的压缩关联为何不成立。
// Python: windows.py _compaction_correlation_issue。
func compactionCorrelationIssue(event *TrajectoryEvent, payload map[string]any, compactions map[string]*TrajectoryEvent) string {
	explicit := false
	for _, key := range []string{"caused_by_operation_id", "input_window_id", "output_window_id"} {
		if _, exists := payload[key]; exists {
			explicit = true
			break
		}
	}
	transitionKind, _ := payload["transition_kind"].(string)
	correlationKind, _ := payload["correlation_kind"].(string)
	if transitionKind == "compaction" || correlationKind == "compaction" {
		explicit = true
	}
	if !explicit {
		return ""
	}
	operationID := strings.TrimSpace(fmt.Sprint(payload["caused_by_operation_id"]))
	inputWindowID := payload["input_window_id"]
	outputWindowID := strings.TrimSpace(fmt.Sprint(payload["output_window_id"]))
	compactionKind := transitionKind == "compaction" || correlationKind == "compaction"
	missingIDs := operationID == "" || outputWindowID == ""
	blankInput := inputWindowID != nil && strings.TrimSpace(fmt.Sprint(inputWindowID)) == ""
	if !compactionKind || missingIDs || blankInput {
		return "Explicit compaction correlation is incomplete."
	}
	if fmt.Sprint(inputWindowID) != fmt.Sprint(payload["base_window_id"]) || outputWindowID != fmt.Sprint(payload["window_id"]) {
		return "Explicit compaction correlation conflicts with the context window transition."
	}
	compaction, available := compactions[operationID]
	if !available {
		return fmt.Sprintf("Compaction operation %s is not available for this output window.", operationID)
	}
	if compaction == nil {
		return fmt.Sprintf("Compaction operation %s is ambiguous.", operationID)
	}
	if compaction.Sequence >= event.Sequence {
		return fmt.Sprintf("Compaction operation %s does not precede its output window.", operationID)
	}
	declaredInput := compaction.Payload["input_window_id"]
	declaredOutput := compaction.Payload["output_window_id"]
	inputConflict := inputWindowID != nil && declaredConflict(declaredInput, inputWindowID)
	if inputConflict || declaredConflict(declaredOutput, outputWindowID) {
		return fmt.Sprintf("Compaction operation %s conflicts with its declared input/output windows.", operationID)
	}
	return ""
}

// replaySubject 回放一个主体的提交链（就地写 windows/byInference/issues）。
// Python: windows.py _replay_subject。
func replaySubject(events []*TrajectoryEvent, windows map[string][]map[string]any, byInference map[string][]map[string]any, issues *[]map[string]any) {
	seen := map[string]*TrajectoryEvent{}
	expectedByEpoch := map[string]int{}
	blockedEpochs := map[string]struct{}{}
	compactions := map[string]*TrajectoryEvent{}
	for _, event := range orderedSubjectEvents(events) {
		position := event.SequenceEpoch + "\x00" + fmt.Sprint(event.Sequence)
		if previous, exists := seen[position]; exists {
			if previous.EventID != event.EventID {
				*issues = append(*issues, replayIssue("v2.sequence_conflict",
					"Two different events use the same sequence within one epoch; the first was kept.", event))
			}
			continue
		}
		seen[position] = event
		isCommit := event.EventKind == ContextWindowCommit
		expected, hasExpected := expectedByEpoch[event.SequenceEpoch]
		if _, blocked := blockedEpochs[event.SequenceEpoch]; blocked {
			if !isCommit {
				continue
			}
			delete(blockedEpochs, event.SequenceEpoch)
			expected = event.Sequence
			hasExpected = true
		}
		if hasExpected && event.Sequence > expected {
			*issues = append(*issues, replayIssue("v2.sequence_gap",
				fmt.Sprintf("Expected subject sequence %d but received %d.", expected, event.Sequence), event))
			if !isCommit {
				blockedEpochs[event.SequenceEpoch] = struct{}{}
				continue
			}
		}
		expectedByEpoch[event.SequenceEpoch] = event.Sequence + 1

		if event.EventKind == CompactionCompleted {
			operationID := strings.TrimSpace(fmt.Sprint(event.Payload["operation_id"]))
			if operationID != "" {
				if _, exists := compactions[operationID]; exists {
					compactions[operationID] = nil
				} else {
					compactions[operationID] = event
				}
			}
			continue
		}
		if !isCommit {
			continue
		}
		restated := fmt.Sprint(event.Payload["transition_kind"]) == TrimBaseline
		if !restated && !isCompactionCommit(event.Payload) && event.ParentSpanID() == "" {
			// 请求的提交挂在发送该窗口的推理上；压缩的在其载荷中点名请求。
			*issues = append(*issues, replayIssue("v2.missing_physical_request",
				"Context commit is missing its physical inference parent.", event))
			continue
		}
		payload := commitPayload(event.Payload)
		if payload == nil {
			*issues = append(*issues, replayIssue("v2.invalid_context_commit",
				"Invalid context.window.commit payload.", event))
			continue
		}
		baseWindowID, _ := payload["base_window_id"].(string)
		var base []map[string]any
		hasBase := false
		if baseWindowID != "" {
			base, hasBase = windows[baseWindowID]
			if !hasBase {
				*issues = append(*issues, replayIssue("v2.missing_base_window",
					fmt.Sprintf("Base context window %s is not available.", baseWindowID), event))
			}
		}
		baseline := baselineReasons[fmt.Sprint(payload["transition_kind"])] != ""
		stated, _ := payload["messages"].([]map[string]any)
		hasStated := payload["messages"] != nil
		var rebuilt []map[string]any
		if hasBase && !baseline {
			rebuilt = ApplyContextDelta(base, payload["delta"].([]map[string]any))
		}
		if hasBase && !baseline && !matchesCheckpoint(rebuilt, statedIfPresent(stated, hasStated)) {
			*issues = append(*issues, replayIssue("v2.delta_checkpoint_mismatch",
				"Context delta does not reconstruct its complete checkpoint.", event))
			continue
		}
		var window []map[string]any
		if hasStated {
			window = stated
		} else {
			window = rebuilt
		}
		if window == nil {
			// 未陈述完整窗口且增量基于的窗口从未读过：链在此提交处断裂。
			continue
		}
		windows[fmt.Sprint(payload["window_id"])] = window
		correlation := compactionCorrelationIssue(event, payload, compactions)
		if correlation != "" {
			*issues = append(*issues, replayIssue("v2.invalid_compaction_correlation", correlation, event))
		}
		if !restated && !isCompactionCommit(payload) && event.ParentSpanID() != "" {
			byInference[event.ParentSpanID()] = window
		}
	}
}

// statedIfPresent 把「陈述了消息」与「消息为空列表」区分开。
func statedIfPresent(stated []map[string]any, has bool) []map[string]any {
	if !has {
		return nil
	}
	return stated
}

// ReplayWindows 回放轨迹中每个主体的提交链。
// Python: windows.py replay_windows。
func ReplayWindows(value any) *WindowReplay {
	bySubject := map[string][]*TrajectoryEvent{}
	for _, event := range IterTrajectoryEvents(value) {
		bySubject[event.SubjectID] = append(bySubject[event.SubjectID], event)
	}
	subjects := make([]string, 0, len(bySubject))
	for subject := range bySubject {
		subjects = append(subjects, subject)
	}
	sort.Strings(subjects)
	windows := map[string]map[string][]map[string]any{}
	byInference := map[string][]map[string]any{}
	var issues []map[string]any
	for _, subjectID := range subjects {
		subjectWindows := windows[subjectID]
		if subjectWindows == nil {
			subjectWindows = map[string][]map[string]any{}
			windows[subjectID] = subjectWindows
		}
		replaySubject(bySubject[subjectID], subjectWindows, byInference, &issues)
	}
	return &WindowReplay{Windows: windows, ByInference: byInference, Issues: issues}
}

// WindowForInference 返回一个推理 span 被发送的窗口（未提交返 nil）。
// Python: windows.py window_for_inference。
func WindowForInference(replay *WindowReplay, span map[string]any) []map[string]any {
	spanID := fmt.Sprint(span["spanId"])
	if spanID == "" || spanID == "<nil>" {
		return nil
	}
	return replay.ByInference[spanID]
}

// syntheticSpanID 裁剪基线合成 span 的确定性 ID。
// Python: windows.py _synthetic_span_id。
func syntheticSpanID(windowID, subjectID string) string {
	digest := sha256.Sum256([]byte("trim_baseline\x00" + subjectID + "\x00" + windowID))
	return hex.EncodeToString(digest[:])[:16]
}

// trimBaselineSpan 以基线事件重述被保留提交所基于的窗口。
// Python: windows.py _trim_baseline_span。
func trimBaselineSpan(first *TrajectoryEvent, baseWindowID string, window []map[string]any, sequence int) map[string]any {
	attributes := SpanAttributes(first.Span)
	payload := map[string]any{
		"window_id":       baseWindowID,
		"base_window_id":  nil,
		"complete":        true,
		"delta":           []any{},
		"messages":        copyJSON(window),
		"transition_kind": TrimBaseline,
		"baseline_reason": baselineReasons[TrimBaseline],
	}
	attributes[obs.OJTrajectoryEventID] = fmt.Sprintf("trim:%s:%s", first.SubjectID, baseWindowID)
	attributes[obs.OJTrajectoryEventKind] = ContextWindowCommit
	attributes[obs.OJTrajectorySubjectSequence] = sequence
	payloadJSON, _ := json.Marshal(toJsonCompatible(payload))
	attributes[obs.OJTrajectoryPayload] = string(payloadJSON)
	start := timeValue(first.Span, "startTimeUnixNano") - 1
	if start < 0 {
		start = 0
	}
	kind := first.Span["kind"]
	if kind == nil {
		kind = "SPAN_KIND_INTERNAL"
	}
	return map[string]any{
		"traceId":           first.Span["traceId"],
		"spanId":            syntheticSpanID(baseWindowID, first.SubjectID),
		"name":              ContextWindowCommit,
		"kind":              kind,
		"startTimeUnixNano": fmt.Sprint(start),
		"endTimeUnixNano":   fmt.Sprint(start),
		"attributes":        AttributesFromMap(attributes),
		"status":            map[string]any{"code": "STATUS_CODE_OK"},
	}
}

// RebaseWindowChain 给 retained 中每条提交链一个可回放的头。
// retained 是 history 的子集（如裁剪后的演进窗口）。当一个主体保留的首个
// 提交是 delta 且其基窗口由未存活的 span 构建时，从 history 回放取基窗口，
// 在其之前插入 trim_baseline 提交重述基线。其余不变；无需补头时原样返回。
// Python: windows.py rebase_window_chain。
func RebaseWindowChain(history, retained any) (*Trajectory, error) {
	retainedTrajectory, err := payloadFor(retained)
	_ = retainedTrajectory
	if err != nil {
		return nil, err
	}
	retainedObj, isTrajectory := retained.(*Trajectory)
	retainedPayload := retained
	if isTrajectory {
		retainedObj = retained.(*Trajectory)
	} else {
		retainedObj = nil
	}
	retainedEvents := IterTrajectoryEvents(retainedPayload)
	hasCommit := false
	for _, event := range retainedEvents {
		if event.EventKind == ContextWindowCommit {
			hasCommit = true
			break
		}
	}
	if !hasCommit {
		if retainedObj != nil {
			return retainedObj, nil
		}
		return trajectoryFromPayload(retainedPayload.(map[string]any))
	}
	var historyReplay *WindowReplay
	var additions []map[string]any
	bySubject := map[string][]*TrajectoryEvent{}
	for _, event := range retainedEvents {
		bySubject[event.SubjectID] = append(bySubject[event.SubjectID], event)
	}
	subjects := make([]string, 0, len(bySubject))
	for subject := range bySubject {
		subjects = append(subjects, subject)
	}
	sort.Strings(subjects)
	for _, subjectID := range subjects {
		ordered := orderedSubjectEvents(bySubject[subjectID])
		var firstCommit *TrajectoryEvent
		for _, event := range ordered {
			if event.EventKind == ContextWindowCommit {
				firstCommit = event
				break
			}
		}
		if firstCommit == nil {
			continue
		}
		baseWindowID, _ := firstCommit.Payload["base_window_id"].(string)
		if baseWindowID == "" || baselineReasons[fmt.Sprint(firstCommit.Payload["transition_kind"])] != "" {
			continue
		}
		if historyReplay == nil {
			historyReplay = ReplayWindows(history)
		}
		window := historyReplay.Windows[subjectID][baseWindowID]
		if window == nil {
			continue
		}
		minSequence := int(^uint(0) >> 1)
		for _, event := range ordered {
			if event.SequenceEpoch == firstCommit.SequenceEpoch && event.Sequence < minSequence {
				minSequence = event.Sequence
			}
		}
		additions = append(additions, trimBaselineSpan(firstCommit, baseWindowID, window, minSequence-1))
	}
	if len(additions) == 0 {
		if retainedObj != nil {
			return retainedObj, nil
		}
		return trajectoryFromPayload(retainedPayload.(map[string]any))
	}
	rawPayload, err := payloadFor(retained)
	if err != nil {
		return nil, err
	}
	payload, err := NormalizeOTLP(rawPayload)
	if err != nil {
		return nil, err
	}
	resourceSpans, _ := payload["resourceSpans"].([]any)
	if len(resourceSpans) == 0 {
		if retainedObj != nil {
			return retainedObj, nil
		}
		return trajectoryFromPayload(payload)
	}
	resourceSpan, _ := resourceSpans[0].(map[string]any)
	scopes, _ := resourceSpan["scopeSpans"].([]any)
	if len(scopes) == 0 {
		scopes = append(scopes, map[string]any{"scope": map[string]any{}, "spans": []any{}})
	}
	firstScope, _ := scopes[0].(map[string]any)
	existingSpans, _ := firstScope["spans"].([]any)
	merged := make([]any, 0, len(additions)+len(existingSpans))
	for _, span := range additions {
		merged = append(merged, span)
	}
	merged = append(merged, existingSpans...)
	sort.SliceStable(merged, func(i, j int) bool {
		a, _ := merged[i].(map[string]any)
		b, _ := merged[j].(map[string]any)
		return compareSpanSortKey(SpanSortKeyOf(a), SpanSortKeyOf(b)) < 0
	})
	firstScope["spans"] = merged
	return trajectoryFromPayload(payload)
}

// IsTrajectoryEventSpan span 是否为 v2 轨迹事件而非工作 span。
// Python: windows.py is_trajectory_event_span。
func IsTrajectoryEventSpan(span map[string]any) bool {
	attributes := SpanAttributes(span)
	if recordKind, _ := attributes[obs.OJTrajectoryRecordKind].(string); recordKind == "event" {
		return true
	}
	_, hasEventKind := attributes[obs.OJTrajectoryEventKind]
	return hasEventKind
}

// TrimTrajectoryWindow 把轨迹限定到最新 max_spans 个工作 span。
// 事件不占名额：与它们描述的工作一起保留，且回放被裁断的每条提交链都
// 获得重述基线，使有界窗口仍能回放它保留的每个请求。
// Python: windows.py trim_trajectory_window。
func TrimTrajectoryWindow(value any, maxSpans int) (*Trajectory, error) {
	trimmed, err := TrimTrajectory(value, maxSpans, nil, nil, IsTrajectoryEventSpan)
	if err != nil {
		return nil, err
	}
	return RebaseWindowChain(value, trimmed)
}
