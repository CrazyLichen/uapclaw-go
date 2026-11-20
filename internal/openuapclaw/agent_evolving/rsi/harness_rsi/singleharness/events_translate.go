// 把 epoch 级 Harness 版本投影进共享 RSI 事件词汇（events_translate.py 一比一对齐）。
package singleharness

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi"
)

// provisionalStatuses 临时态集合（Python _PROVISIONAL_STATUSES）。
var provisionalStatuses = map[string]bool{"provisional": true}

// ──────────────────────────── 导出函数 ────────────────────────────

// SourceReuseStagePayload 报告证据来源，不是新评估或模型用量事件（Python source_reuse_stage_payload）。
func SourceReuseStagePayload(batchIndex, totalCases int, score *float64, evalRefPath string, provenance map[string]any) map[string]any {
	reusedCaseIDs, _ := provenance["reused_case_ids"].([]any)
	evaluatedCaseIDs, _ := provenance["evaluated_case_ids"].([]any)
	payload := map[string]any{
		"id":                   "source.reuse",
		"name":                 fmt.Sprintf("Batch %d: reused %d/%d case results", batchIndex, len(reusedCaseIDs), totalCases),
		"status":               "done",
		"batch_index":          batchIndex,
		"total_cases":          totalCases,
		"reused_case_count":    len(reusedCaseIDs),
		"evaluated_case_count": len(evaluatedCaseIDs),
		"eval_ref_path":        evalRefPath,
	}
	if score != nil {
		payload["score"] = *score
	} else {
		payload["score"] = nil
	}
	for key, value := range provenance {
		payload[key] = value
	}
	return payload
}

// CaseStagePayload 构建单 harness 逐用例评估阶段载荷（Python case_stage_payload）。
func CaseStagePayload(caseIndex, totalCases int, status string, caseID string, score *float64, completedCases *int) map[string]any {
	label := fmt.Sprintf("Case %d/%d", caseIndex, totalCases)
	normalizedStatus := status
	if normalizedStatus == "" {
		normalizedStatus = "running"
	}
	var name string
	switch normalizedStatus {
	case "passed":
		name = fmt.Sprintf("%s passed", label)
	case "failed":
		name = fmt.Sprintf("%s failed", label)
	case "error":
		name = fmt.Sprintf("%s error", label)
	case "skipped":
		name = fmt.Sprintf("%s skipped", label)
	default:
		normalizedStatus = "running"
		name = fmt.Sprintf("%s evaluating", label)
	}
	if score != nil && (normalizedStatus == "passed" || normalizedStatus == "failed") {
		name = fmt.Sprintf("%s · score %s", name, formatCaseScore(*score))
	}
	payload := map[string]any{
		"id":          fmt.Sprintf("evaluate.case.%d", caseIndex),
		"name":        name,
		"status":      normalizedStatus,
		"case_index":  caseIndex,
		"total_cases": totalCases,
	}
	if caseID != "" {
		payload["case_id"] = caseID
	}
	if score != nil {
		payload["score"] = *score
	}
	if completedCases != nil {
		payload["id"] = "evaluate.parallel"
		payload["name"] = fmt.Sprintf("Cases %d/%d completed", *completedCases, totalCases)
		if *completedCases == totalCases {
			payload["status"] = "done"
		} else {
			payload["status"] = "running"
		}
		payload["completed_cases"] = *completedCases
	}
	return payload
}

// GenerateStagePayload 构建候选生成阶段载荷（Python generate_stage_payload）。
func GenerateStagePayload(candidateIndex, candidateCount int, status string, errorMsg string) map[string]any {
	normalizedStatus := status
	if normalizedStatus == "" {
		normalizedStatus = "running"
	}
	var name string
	switch normalizedStatus {
	case "done":
		name = fmt.Sprintf("Candidate %d/%d generated", candidateIndex, candidateCount)
	case "error":
		name = fmt.Sprintf("Candidate %d/%d generation failed", candidateIndex, candidateCount)
	default:
		normalizedStatus = "running"
		name = fmt.Sprintf("Generating candidate %d/%d", candidateIndex, candidateCount)
	}
	payload := map[string]any{
		"id":               "generate.candidate",
		"name":             name,
		"status":           normalizedStatus,
		"candidate_index":  candidateIndex,
		"total_candidates": candidateCount,
	}
	if errorMsg != "" {
		payload["error"] = errorMsg
	}
	return payload
}

// AnalysisStagePayload 为当前树根构建失败分析阶段载荷（Python analysis_stage_payload）。
func AnalysisStagePayload(status string, failedCaseCount *int, errorMsg string) map[string]any {
	normalizedStatus := status
	if normalizedStatus == "" {
		normalizedStatus = "running"
	}
	var name string
	switch normalizedStatus {
	case "done":
		name = "Failure analysis completed"
	case "error":
		name = "Failure analysis failed"
	default:
		normalizedStatus = "running"
		name = "Analyzing failed cases"
	}
	payload := map[string]any{
		"id":     "analyze.failures",
		"name":   name,
		"status": normalizedStatus,
	}
	if failedCaseCount != nil {
		payload["failed_case_count"] = *failedCaseCount
	}
	if errorMsg != "" {
		payload["error"] = errorMsg
	}
	return payload
}

// ProgressEvent 统计已完成 epoch；本地候选尝试不推进进度（Python progress_event）。
func ProgressEvent(state map[string]any, totalIterations int) *rsi.EventProgress {
	return rsi.NewEventProgress(
		len(mappingItems(state["epoch_checkpoints"])),
		maxInt(0, totalIterations),
		optionalNumber(state["best_score"]),
		optionalNumber(state["baseline_score"]),
		rsi.UsageSnapshot(state["usage"]),
	)
}

// RootNodeEvent 暴露初始 Harness，无需额外基线运行（Python root_node_event）。
func RootNodeEvent(state map[string]any) *rsi.EventNode {
	refsPath := stringOrEmpty(state["source_harness_refs_path"])
	extra := map[string]any{"artifact_path": refsPath, "iteration_unit": "epoch"}
	return rsi.NewEventNode(rsi.NewRsiTreeNode(
		"h0", 0, nil, "ROOT", true,
		optionalNumber(state["baseline_score"]),
		stringPtr("Initial Harness"), nil, nil, nil,
		[]rsi.RsiChange{}, extra,
	), HarnessArtifacts(refsPath))
}

// EpochNodeEvent 每个 epoch 暴露一个最终 Harness，而非每个本地修复候选（Python epoch_node_event）。
func EpochNodeEvent(state map[string]any, checkpoint map[string]any) *rsi.EventNode {
	epoch := intOf(checkpoint["epoch"], 0)
	selected := stringOrEmpty(checkpoint["selected_harness_refs_path"])
	evaluated := stringOrEmpty(checkpoint["harness_refs_path"])
	parentID := epochParentID(state, checkpoint)
	adopted, _ := checkpoint["promotion_applied"].(bool)
	running := stringOrEmpty(checkpoint["status"]) == "running"
	rejected := stringOrEmpty(checkpoint["status"]) == "rejected"
	fullEvaluationSkippedReason := stringOrEmpty(checkpoint["full_evaluation_skipped_reason"])
	var changes []rsi.RsiChange
	if adopted {
		for _, candidate := range mappingItems(state["candidate_gates"]) {
			if intOf(candidate["epoch"], 0) == epoch && stringOrEmpty(candidate["status"]) == "accepted" {
				changes = append(changes, changesFrom(candidate["capabilities"])...)
			}
		}
	}
	// 被过滤或回滚的 Harness 不是全量重放的那个
	var score *float64
	if fullEvaluationSkippedReason == "" && selected != "" && selected == evaluated {
		score = optionalNumber(checkpoint["score"])
	}
	var reason *string
	if !running && !adopted {
		text := fullEvaluationSkippedReason
		if text == "" {
			text = stringOrEmpty(checkpoint["promotion_reason"])
		}
		if text == "" {
			text = "No Harness change passed the acceptance checks"
		}
		reason = &text
	}
	sourceEvidence := []any{}
	completedBatches, _ := state["completed_batches"].(map[string]any)
	batchKeys := make([]string, 0, len(completedBatches))
	for key := range completedBatches {
		batchKeys = append(batchKeys, key)
	}
	sort.Strings(batchKeys)
	for _, key := range batchKeys {
		batch, ok := completedBatches[key].(map[string]any)
		if !ok {
			continue
		}
		if intOf(batch["epoch"], 0) != epoch {
			continue
		}
		batchEvidence, hasEvidence := batch["source_evidence"].(map[string]any)
		if !hasEvidence {
			continue
		}
		entry := map[string]any{
			"batch_index":   batch["batch_index"],
			"eval_ref_path": batch["source_eval_ref_path"],
		}
		for evidenceKey, evidenceValue := range batchEvidence {
			entry[evidenceKey] = evidenceValue
		}
		sourceEvidence = append(sourceEvidence, entry)
	}
	extra := map[string]any{
		"artifact_path":   selected,
		"iteration_unit":  "epoch",
		"source_evidence": sourceEvidence,
	}
	if fullEvaluationSkippedReason != "" {
		extra["full_evaluation_skipped_reason"] = fullEvaluationSkippedReason
	}
	promotionPolicy, _ := state["promotion_policy"].(map[string]any)
	if promotionPolicy != nil {
		if policyVersion, _ := promotionPolicy["policy_version"].(string); policyVersion == "continuous_margin_v1" || policyVersion == "continuous_two_stage_v2" {
			extra["score_mode"] = "continuous"
			extra["best_reference_score"] = state["best_reference_score"]
			extra["latest_evaluated_score"] = checkpoint["score"]
			extra["promotion_policy"] = state["promotion_policy"]
		}
	}
	nodeType := "UNCHANGED"
	summaryText := "No retained Harness change"
	if running {
		nodeType = "RUNNING"
		summaryText = "Optimizing Harness"
	} else if adopted {
		nodeType = "ADOPTED"
	} else if rejected {
		nodeType = "REJECTED"
	}
	return rsi.NewEventNode(rsi.NewRsiTreeNode(
		fmt.Sprintf("epoch-%03d", epoch), epoch, &parentID, nodeType, adopted,
		score, summaryPtr(changes, &summaryText), nil, reason, nil,
		changes, extra,
	), harnessArtifactsOrNil(selected, running))
}

// ActiveEpochNodeEvent 活跃 epoch 与恢复查询共用同一活跃快照（Python active_epoch_node_event）。
func ActiveEpochNodeEvent(state map[string]any) *rsi.EventNode {
	epoch := intOf(state["active_epoch"], 0)
	if epoch == 0 {
		return nil
	}
	for _, item := range mappingItems(state["epoch_checkpoints"]) {
		if intOf(item["epoch"], 0) == epoch {
			return nil
		}
	}
	return EpochNodeEvent(state, map[string]any{
		"epoch":                    epoch,
		"status":                   "running",
		"before_harness_refs_path": stringOrEmpty(state["active_epoch_before_harness_refs_path"]),
	})
}

// HarnessArtifacts 描述 refs 文件及其插件目录，绝不包含整个 run（Python harness_artifacts）。
func HarnessArtifacts(refsPath string) []map[string]string {
	if refsPath == "" {
		return nil
	}
	artifacts := []map[string]string{{"role": "HARNESS_REFS", "path": refsPath, "format": "yaml"}}
	data, err := osReadFileYAML(refsPath)
	if err != nil {
		return artifacts
	}
	rawRefs, present := data["harness_refs"]
	if !present {
		rawRefs = data
	}
	refs, ok := rawRefs.(map[string]any)
	if !ok {
		return artifacts
	}
	keys := make([]string, 0, len(refs))
	for key := range refs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		raw, ok := refs[key].(string)
		if !ok {
			continue
		}
		pkg := expandUser(raw)
		if !filepath.IsAbs(pkg) {
			pkg = filepath.Join(filepath.Dir(refsPath), pkg)
		}
		if info, statErr := os.Stat(pkg); statErr == nil && info.IsDir() {
			if _, configErr := os.Stat(filepath.Join(pkg, "harness_config.yaml")); configErr == nil {
				resolved, _ := filepath.EvalSymlinks(pkg)
				if resolved == "" {
					resolved = pkg
				}
				artifacts = append(artifacts, map[string]string{"role": "PRIMARY", "path": resolved, "format": "dir"})
			}
		}
	}
	return artifacts
}

// NodeEvent 从一条持久化候选记录构建通用节点快照（Python node_event）。
func NodeEvent(candidate map[string]any, iteration int, parentID *string) *rsi.EventNode {
	status := strings.ToLower(strings.TrimSpace(stringOrEmpty(candidate["status"])))
	accepted, _ := candidate["accepted"].(bool)
	adopted := accepted && status == "accepted"
	nodeType := "REJECTED"
	if adopted {
		nodeType = "ADOPTED"
	} else if provisionalStatuses[status] {
		nodeType = "PROVISIONAL"
	}
	changes := changesFrom(candidate["capabilities"])
	reasonText := publicText(strings.TrimSpace(stringOrEmpty(candidate["reason"])))
	var reason *string
	if reasonText != "" {
		reason = &reasonText
	}
	artifactPath := strings.TrimSpace(stringOrEmpty(candidate["candidate_harness_refs_path"]))
	var failureClass *string
	failureText := strings.TrimSpace(stringOrEmpty(candidate["failure_class"]))
	if failureText == "" {
		failureText = strings.TrimSpace(stringOrEmpty(candidate["causal_failure_class"]))
	}
	if failureText != "" {
		failureClass = &failureText
	}
	var summary *string
	adoptedReasonNil := reason
	if adopted {
		adoptedReasonNil = nil
	}
	if len(changes) > 0 || reasonText != "" {
		summary = summaryPtr(changes, reason)
	}
	var extra map[string]any
	if artifactPath != "" {
		extra = map[string]any{"artifact_path": artifactPath}
	} else {
		extra = map[string]any{}
	}
	return rsi.NewEventNode(rsi.NewRsiTreeNode(
		NodeID(candidate, iteration), iteration, parentID, nodeType, adopted,
		optionalNumber(candidate["candidate_score"]),
		summary, nil, adoptedReasonNil, failureClass,
		changes, extra,
	), HarnessArtifacts(artifactPath))
}

// ParentNodeID 解析输出为本候选父版本的最近候选（Python parent_node_id）。
func ParentNodeID(candidate map[string]any, persistedCandidates []map[string]any) *string {
	parentArtifact := strings.TrimSpace(stringOrEmpty(candidate["before_harness_refs_path"]))
	if parentArtifact == "" {
		return nil
	}
	for index := len(persistedCandidates) - 1; index >= 0; index-- {
		prior := persistedCandidates[index]
		priorArtifact := strings.TrimSpace(stringOrEmpty(prior["candidate_harness_refs_path"]))
		if priorArtifact != "" && priorArtifact == parentArtifact {
			id := NodeID(prior, index+1)
			return &id
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// epochParentID 被拒绝/无操作 epoch 观察一个版本但不创建它（Python _epoch_parent_id）。
func epochParentID(state map[string]any, checkpoint map[string]any) string {
	before := stringOrEmpty(checkpoint["before_harness_refs_path"])
	if before != "" && before != stringOrEmpty(state["source_harness_refs_path"]) {
		checkpoints := mappingItems(state["epoch_checkpoints"])
		sort.SliceStable(checkpoints, func(i, j int) bool {
			return intOf(checkpoints[i]["epoch"], 0) > intOf(checkpoints[j]["epoch"], 0)
		})
		for _, prior := range checkpoints {
			if intOf(prior["epoch"], 0) < intOf(checkpoint["epoch"], 0) {
				applied, _ := prior["promotion_applied"].(bool)
				if applied && stringOrEmpty(prior["selected_harness_refs_path"]) == before {
					return fmt.Sprintf("epoch-%03d", intOf(prior["epoch"], 0))
				}
			}
		}
	}
	return "h0"
}

// NodeID 候选节点标识：显式 ID 优先，否则按稳定部分哈希（Python _node_id）。
func NodeID(candidate map[string]any, iteration int) string {
	explicit := strings.TrimSpace(stringOrEmpty(candidate["candidate_id"]))
	if explicit != "" {
		return explicit
	}
	stableParts := []string{
		stringOrEmpty(candidate["member_optimization_ref_path"]),
		stringOrEmpty(candidate["candidate_harness_refs_path"]),
		strconv.Itoa(iteration),
	}
	digest := sha256.Sum256([]byte(strings.Join(stableParts, "\x00")))
	return fmt.Sprintf("candidate-%s", hex.EncodeToString(digest[:])[:16])
}

// changesFrom 解析能力清单为变更摘要（Python _changes）。
func changesFrom(rawCapabilities any) []rsi.RsiChange {
	var changes []rsi.RsiChange
	list, ok := rawCapabilities.([]any)
	if !ok {
		return changes
	}
	for _, raw := range list {
		capability, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		summaryText := strings.TrimSpace(firstText(capability, "expected_effect", "description", "rationale", "purpose"))
		var function *string
		if fn := strings.TrimSpace(stringOrEmpty(capability["function"])); fn != "" {
			function = &fn
		}
		targetText := strings.TrimSpace(firstText(capability, "target_path", "target_ref", "target"))
		var target *string
		if targetText != "" {
			target = &targetText
		}
		changes = append(changes, *rsi.NewRsiChange(
			strings.ToUpper(strings.TrimSpace(stringOrEmpty(capability["action_group"]))),
			strings.ToUpper(strings.TrimSpace(stringOrEmpty(capability["operation"]))),
			function, target,
			publicText(summaryText),
		))
	}
	return changes
}

// summaryOf 取前三个变更描述，否则回退原因（Python _summary）。
func summaryOf(changes []rsi.RsiChange, reason *string) *string {
	descriptions := make([]string, 0, len(changes))
	for _, change := range changes {
		if change.Summary() != "" {
			descriptions = append(descriptions, change.Summary())
		}
	}
	if len(descriptions) == 0 {
		return reason
	}
	if len(descriptions) > 3 {
		descriptions = descriptions[:3]
	}
	joined := strings.Join(descriptions, "; ")
	return &joined
}

// summaryPtr 汇总助手（保持调用侧简洁）。
func summaryPtr(changes []rsi.RsiChange, reason *string) *string {
	return summaryOf(changes, reason)
}

// publicText 从面向用户的事件文本中移除控制面词汇（Python _public_text）。
func publicText(value string) string {
	text := strings.ReplaceAll(value, "_", " ")
	replacements := []struct {
		pattern     *regexp.Regexp
		replacement string
	}{
		{regexp.MustCompile(`(?i)\bgates?\b`), "reviews"},
		{regexp.MustCompile(`(?i)\bepochs?\b`), "cycles"},
		{regexp.MustCompile(`(?i)\bbatches?\b`), "case groups"},
		{regexp.MustCompile(`(?i)\battempts?\b`), "proposals"},
		{regexp.MustCompile(`(?i)\bcheckpoints?\b`), "reviews"},
	}
	for _, rule := range replacements {
		text = rule.pattern.ReplaceAllString(text, rule.replacement)
	}
	return strings.Join(strings.Fields(text), " ")
}

// mappingItems 列表值过滤出映射项（Python _mapping_items）。
func mappingItems(value any) []map[string]any {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	items := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, isMap := item.(map[string]any); isMap {
			items = append(items, m)
		}
	}
	return items
}

// optionalNumber 数值投影：nil 与 bool 拒绝（Python _number）。
func optionalNumber(value any) *float64 {
	if value == nil {
		return nil
	}
	if _, isBool := value.(bool); isBool {
		return nil
	}
	switch v := value.(type) {
	case int:
		f := float64(v)
		return &f
	case int64:
		f := float64(v)
		return &f
	case float64:
		return &v
	case *float64:
		return v
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return &f
		}
	}
	return nil
}

// formatCaseScore 阶段标签用例分数格式（Python _format_case_score）。
func formatCaseScore(score float64) string {
	return fmt.Sprintf("%.2f", score)
}

// firstText 依次取首个存在的键值（对齐 Python or 链）。
func firstText(mapping map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, present := mapping[key]; present && value != nil {
			if s, ok := value.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// stringOrEmpty 字符串投影（nil 安全）。
func stringOrEmpty(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// stringPtr 字符串转指针。
func stringPtr(value string) *string { return &value }

// intOf 整数投影（带默认值）。
func intOf(value any, fallback int) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return fallback
}

// maxInt 两整数取大。
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// harnessArtifactsOrNil 运行中无 artifacts，否则描述 refs（epoch 事件辅助）。
func harnessArtifactsOrNil(selected string, running bool) []map[string]string {
	if running {
		return nil
	}
	return HarnessArtifacts(selected)
}

// osReadFileYAML 读 YAML 文件为映射（失败回退空映射）。
func osReadFileYAML(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}, err
	}
	var payload map[string]any
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return map[string]any{}, err
	}
	return payload, nil
}
