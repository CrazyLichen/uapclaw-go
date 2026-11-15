package evaluationresultanalyzer

// 确定性调查计划执行器（RSI 因果诊断的有界只读证据获取）。
//
// 对齐 Python openjiuwen/rsi/harness_rsi/evaluation_result_analyzer/evidence_investigation.py。
// NormalizeCausalInvestigation 校验并裁剪模型提议的调查计划（最多 6 假设/12 请求），
// ExecuteCausalInvestigation 以纯代码逐步执行只读查证——文本 AI 不直接触碰文件系统。
// 注意：Python 侧本模块无生产调用方（仅单元测试消费），照常一比一复刻（spec D-12），
// 消费方 ⤴️ 17.4d/后续章节。
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/evaluation_result_analyzer/evidence_investigation.py

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxHypotheses 假设上限（Python _MAX_HYPOTHESES）
	maxHypotheses = 6
	// maxRequests 请求上限（Python _MAX_REQUESTS）
	maxRequests = 12
	// maxSearchResults 搜索结果上限（Python _MAX_SEARCH_RESULTS）
	maxSearchResults = 5
	// maxEventChars 事件字符预算（Python _MAX_EVENT_CHARS）
	maxEventChars = 12000
	// maxArtifactFileChars 单 artifact 字符预算（Python _MAX_ARTIFACT_FILE_CHARS）
	maxArtifactFileChars = 200000
	// maxArtifactFiles artifact 文件数上限（Python _MAX_ARTIFACT_FILES）
	maxArtifactFiles = 100
	// maxArtifactWindowChars 窗口字符预算（Python _MAX_ARTIFACT_WINDOW_CHARS）
	maxArtifactWindowChars = 12000
	// maxAutomaticArtifactWindows 自动窗口上限（Python _MAX_AUTOMATIC_ARTIFACT_WINDOWS）
	maxAutomaticArtifactWindows = 12
	// maxAutomaticArtifactSources 自动窗口来源上限（Python _MAX_AUTOMATIC_ARTIFACT_SOURCES）
	maxAutomaticArtifactSources = 4
	// maxAutomaticArtifactChars 自动窗口字符预算（Python _MAX_AUTOMATIC_ARTIFACT_CHARS）
	maxAutomaticArtifactChars = 96000
	// maxStructuredArtifactsPerRequest 单请求结构化 artifact 上限（Python _MAX_STRUCTURED_ARTIFACTS_PER_REQUEST）
	maxStructuredArtifactsPerRequest = 16
	// maxRepositoryFiles 仓库文件数上限（Python _MAX_REPOSITORY_FILES）
	maxRepositoryFiles = 2000
	// maxStructuredCells 结构化单元格上限（Python _MAX_STRUCTURED_CELLS）
	maxStructuredCells = 20000
	// maxStructuredPages 结构化页数上限（Python _MAX_STRUCTURED_PAGES）
	maxStructuredPages = 100
)

// textArtifactSuffixes 文本类 artifact 后缀集（Python _TEXT_ARTIFACT_SUFFIXES）
var textArtifactSuffixes = map[string]bool{
	".csv": true, ".diff": true, ".json": true, ".log": true, ".md": true,
	".patch": true, ".txt": true, ".xml": true, ".yaml": true, ".yml": true,
}

// structuredArtifactSuffixes 结构化类 artifact 后缀集（Python _STRUCTURED_ARTIFACT_SUFFIXES）
var structuredArtifactSuffixes = map[string]bool{
	".docx": true, ".pdf": true, ".pptx": true, ".xlsx": true,
}

// structuredQuerySuffixHints 结构化查询提示词（Python _STRUCTURED_QUERY_SUFFIX_HINTS，逐字保留）
var structuredQuerySuffixHints = map[string][]string{
	".docx": {"docx", "word", "paragraph", "document", "文档", "段落"},
	".pdf":  {"pdf", "page", "document", "文档", "页面"},
	".pptx": {"pptx", "powerpoint", "slide", "deck", "presentation", "幻灯片", "演示"},
	".xlsx": {"xlsx", "excel", "workbook", "worksheet", "spreadsheet", "cell", "formula",
		"工作簿", "工作表", "单元格", "公式"},
}

// allowedOperations 允许的调查操作集（Python _ALLOWED_OPERATIONS）
var allowedOperations = map[string]bool{
	"check_relation":         true,
	"compare_numeric_change": true,
	"compare_runs":           true,
	"inspect_artifact":       true,
	"inspect_evaluation":     true,
	"read_artifact_window":   true,
	"read_repository_file":   true,
	"read_event":             true,
	"search_repository":      true,
	"search_trace":           true,
}

// stopwords 搜索停用词（Python _STOPWORDS，逐字对齐）
var stopwords = map[string]bool{
	"about": true, "after": true, "agent": true, "before": true, "case": true,
	"complete": true, "content": true, "document": true, "evidence": true,
	"failure": true, "file": true, "from": true, "full": true, "inspect": true,
	"into": true, "missing": true, "output": true, "result": true, "read": true,
	"show": true, "should": true, "source": true, "task": true, "that": true,
	"the": true, "this": true, "tool": true, "trace": true, "with": true,
}

// 正则族（RE2 核对：无回溯引用，语义等价）
var (
	// termPattern 搜索分词（Python _TERM_PATTERN）
	termPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_.:/-]{1,}|\d+(?:\.\d+)?|[\x{4e00}-\x{9fff}]{2,8}`)
	// explicitNumericDeltaPattern 显式数值对比声明（Python _EXPLICIT_NUMERIC_DELTA_PATTERN）
	explicitNumericDeltaPattern = regexp.MustCompile(
		`(?i)(?:\b(?:before[-\s]*(?:vs\.?|versus|and)[-\s]*after|numeric\s+(?:change|delta)|` +
			`formula\s+(?:change|delta))\b|(?:数值|公式)(?:变化|差值)|前后(?:数值|公式))`)
	// implicitNumericDeltaPattern 隐式数值语义（Python _IMPLICIT_NUMERIC_DELTA_PATTERN）
	implicitNumericDeltaPattern = regexp.MustCompile(
		`(?i)(?:[%％]|\b(?:percentage\s+points?|percent|delta|difference|increase[sd]?|decrease[sd]?|` +
			`subtract(?:ed|ing)?|add(?:ed|ing)?|formula|ratio|rate)\b|(?:百分点|百分比|增(?:加|长)|` +
			`减少|差值|公式|比率|比例))`)
	// absenceClaimPattern 缺席声明（Python _ABSENCE_CLAIM_PATTERN）
	absenceClaimPattern = regexp.MustCompile(
		`(?i)\b(?:absent|absence|missing|lacks?|without|does\s+not\s+(?:contain|include|show)|` +
			`no\s+(?:evidence|record|field|entry|occurrence))\b|(?:缺少|缺失|不存在|未包含|没有)`)
	// existenceClaimPattern 存在声明（Python _EXISTENCE_CLAIM_PATTERN）
	existenceClaimPattern = regexp.MustCompile(
		`(?i)\b(?:contains?|includes?|present|exists?|records?|shows?|states?)\b|(?:包含|存在|记录|显示|说明)`)
)

// ──────────────────────────── 结构体 ────────────────────────────

// NormalizeOptions 归一化约束（Python normalize_causal_investigation 关键字参数）
type NormalizeOptions struct {
	// FailedRequirementIDs 需求白名单（Python failed_requirement_ids）
	FailedRequirementIDs []string
	// MaxRequests 请求上限（Python max_requests）
	MaxRequests int
	// MaxHypotheses 假设上限（Python max_hypotheses）
	MaxHypotheses int
	// MinHypotheses 最少假设（Python min_hypotheses）
	MinHypotheses int
	// MinHypothesesPerRequirement 每需求最少备选假设（Python min_hypotheses_per_requirement）
	MinHypothesesPerRequirement int
	// RequireEvidencePerHypothesis 每假设须有证据请求（Python require_evidence_per_hypothesis）
	RequireEvidencePerHypothesis bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CausalHypothesisSemanticId 单条因果陈述的稳定语义指纹（Python causal_hypothesis_semantic_id）
func CausalHypothesisSemanticId(claim string, falsifiedIf string) string {
	// Python: json.dumps({...}, ensure_ascii=True, sort_keys=True, separators=(",", ":"))
	canonical, _ := json.Marshal(map[string]string{
		"claim":        normalizedSemantics(claim),
		"falsified_if": normalizedSemantics(falsifiedIf),
	})
	sum := sha256.Sum256(canonical)
	return fmt.Sprintf("chs:%x", sum[:])[:4+24]
}

// NormalizeCausalInvestigation 校验并裁剪模型提议的调查计划（Python normalize_causal_investigation）。
// 返回 nil 表示计划不可用（缺假设/覆盖不足等）。
func NormalizeCausalInvestigation(value any, options ...NormalizeOptions) map[string]any {
	opts := NormalizeOptions{MaxRequests: maxRequests, MaxHypotheses: maxHypotheses, MinHypotheses: 1}
	if len(options) > 0 {
		given := options[0]
		if given.MaxRequests != 0 {
			opts.MaxRequests = given.MaxRequests
		}
		if given.MaxHypotheses != 0 {
			opts.MaxHypotheses = given.MaxHypotheses
		}
		if given.MinHypotheses != 0 {
			opts.MinHypotheses = given.MinHypotheses
		}
		opts.MinHypothesesPerRequirement = given.MinHypothesesPerRequirement
		opts.RequireEvidencePerHypothesis = given.RequireEvidencePerHypothesis
		opts.FailedRequirementIDs = given.FailedRequirementIDs
	}
	// Python: if not isinstance(value, Mapping): return None
	raw, ok := value.(map[string]any)
	if !ok {
		if m, isMap := value.(map[string]any); isMap {
			raw = m
		} else {
			return nil
		}
	}
	if raw == nil {
		return nil
	}
	// Python: raw = value.get("causal_investigation") → value.get("investigation") → value
	if v, exists := raw["causal_investigation"]; exists {
		if m, isMap := v.(map[string]any); isMap {
			raw = m
		}
	}
	if v, exists := raw["investigation"]; exists {
		if m, isMap := v.(map[string]any); isMap {
			raw = m
		}
	}
	// Python: raw = value.get("investigation") 后仍要求 Mapping
	if _, isMap := rawRaw(raw); !isMap {
		return nil
	}
	return normalizePlanBody(raw, &opts)
}

// ExecuteCausalInvestigation 执行调查计划（Python execute_causal_investigation，Task 7 落地）

// ──────────────────────────── 非导出函数 ────────────────────────────

// rawRaw 类型守卫辅助：确认 map 非空类型（Python isinstance(raw, Mapping) 尾检）
func rawRaw(v map[string]any) (map[string]any, bool) {
	return v, v != nil
}

// normalizePlanBody 计划体归一化主链（Python normalize_causal_investigation 函数体）
func normalizePlanBody(raw map[string]any, opts *NormalizeOptions) map[string]any {
	// Python: hypothesis_limit = max(1, min(_MAX_HYPOTHESES, int(max_hypotheses)))
	hypothesisLimit := opts.MaxHypotheses
	if hypothesisLimit > maxHypotheses {
		hypothesisLimit = maxHypotheses
	}
	if hypothesisLimit < 1 {
		hypothesisLimit = 1
	}
	// Python: raw_hypotheses = raw.get("hypotheses"); 须为非空 list
	rawHypotheses, _ := raw["hypotheses"].([]any)
	if len(rawHypotheses) == 0 {
		return nil
	}
	// Python: allowed_requirements = {str(item) for item in failed_requirement_ids if str(item)}
	allowedRequirements := map[string]bool{}
	for _, item := range opts.FailedRequirementIDs {
		if item != "" {
			allowedRequirements[item] = true
		}
	}
	var hypotheses []map[string]any
	hypothesisIDs := map[string]bool{}
	hypothesisSemantics := map[string]bool{}
	type rawRequest struct {
		hypothesisID string
		request      map[string]any
	}
	var rawRequests []rawRequest
	// Python: for index, item in enumerate(raw_hypotheses[:hypothesis_limit], start=1)
	for index, itemAny := range rawHypotheses[:invMinInt(hypothesisLimit, len(rawHypotheses))] {
		item, ok := itemAny.(map[string]any)
		if !ok {
			continue
		}
		claim := strings.TrimSpace(stringValue(item["claim"]))
		falsifiedIf := strings.TrimSpace(stringValue(item["falsified_if"]))
		if claim == "" || falsifiedIf == "" {
			continue
		}
		// Python: semantics 去重
		semanticsKey := normalizedSemantics(claim) + "\x00" + normalizedSemantics(falsifiedIf)
		if hypothesisSemantics[semanticsKey] {
			continue
		}
		hypothesisSemantics[semanticsKey] = true
		hypothesisID := strings.TrimSpace(stringValue(item["hypothesis_id"]))
		if hypothesisID == "" || hypothesisIDs[hypothesisID] {
			hypothesisID = fmt.Sprintf("h%d", index+1)
		}
		hypothesisIDs[hypothesisID] = true
		explains := invStringList(item["explains_requirement_ids"])
		if len(allowedRequirements) > 0 {
			filtered := explains[:0]
			for _, requirementID := range explains {
				if allowedRequirements[requirementID] {
					filtered = append(filtered, requirementID)
				}
			}
			explains = filtered
		}
		requestsAny, _ := item["evidence_requests"].([]any)
		declaredNumericCheck, _ := item["numeric_change_check_required"].(bool)
		// Python: 显式 false 不能关闭控制器检测到的数值前后声明（控制面决策）
		hypothesisText := claim + "\n" + falsifiedIf
		numericChangeCheckRequired := declaredNumericCheck ||
			explicitNumericDeltaPattern.MatchString(hypothesisText) ||
			implicitNumericDeltaPattern.MatchString(hypothesisText)
		for _, reqAny := range requestsAny {
			if req, ok := reqAny.(map[string]any); ok && stringValue(req["operation"]) == "compare_numeric_change" {
				numericChangeCheckRequired = true
			}
		}
		currentSupport := invStringList(item["current_support"])
		if len(currentSupport) > 8 {
			currentSupport = currentSupport[:8]
		}
		hypotheses = append(hypotheses, map[string]any{
			"hypothesis_id":                 hypothesisID,
			"hypothesis_semantic_id":        CausalHypothesisSemanticId(claim, falsifiedIf),
			"claim":                         claim,
			"explains_requirement_ids":      explains,
			"current_support":               currentSupport,
			"falsified_if":                  falsifiedIf,
			"numeric_change_check_required": numericChangeCheckRequired,
		})
		for _, reqAny := range requestsAny {
			if req, ok := reqAny.(map[string]any); ok {
				rawRequests = append(rawRequests, rawRequest{hypothesisID: hypothesisID, request: req})
			}
		}
	}
	// Python: top_level_requests
	if topAny, exists := raw["evidence_requests"]; exists {
		if top, ok := topAny.([]any); ok {
			for _, reqAny := range top {
				if req, ok := reqAny.(map[string]any); ok {
					rawRequests = append(rawRequests, rawRequest{hypothesisID: "", request: req})
				}
			}
		}
	}
	// Python: if len(hypotheses) < max(1, min(hypothesis_limit, int(min_hypotheses))): return None
	minRequired := opts.MinHypotheses
	if minRequired > hypothesisLimit {
		minRequired = hypothesisLimit
	}
	if minRequired < 1 {
		minRequired = 1
	}
	if len(hypotheses) < minRequired {
		return nil
	}
	// Python: 覆盖校验（required_alternatives）
	requiredAlternatives := opts.MinHypothesesPerRequirement
	if requiredAlternatives > hypothesisLimit {
		requiredAlternatives = hypothesisLimit
	}
	if requiredAlternatives < 0 {
		requiredAlternatives = 0
	}
	if len(allowedRequirements) > 0 && requiredAlternatives > 0 {
		coverage := map[string]int{}
		for requirementID := range allowedRequirements {
			coverage[requirementID] = 0
		}
		for _, hypothesis := range hypotheses {
			for _, requirementID := range hypothesis["explains_requirement_ids"].([]string) {
				if _, ok := coverage[requirementID]; ok {
					coverage[requirementID]++
				}
			}
		}
		for _, count := range coverage {
			if count < requiredAlternatives {
				return nil
			}
		}
	}
	// Python: request_limit = min(_MAX_REQUESTS, max(0, int(max_requests)))
	requestLimit := opts.MaxRequests
	if requestLimit > maxRequests {
		requestLimit = maxRequests
	}
	if requestLimit < 0 {
		requestLimit = 0
	}
	var requests []map[string]any
	requestIndexByFingerprint := map[string]int{}
	usedRequestIDs := map[string]bool{}
	// Python: for default_hypothesis_id, request in raw_requests
	for _, rr := range rawRequests {
		normalized := normalizeRequest(rr.request, rr.hypothesisID, hypothesisIDs, len(requests)+1)
		if normalized == nil {
			continue
		}
		fingerprint := requestExecutionFingerprint(normalized)
		if duplicateIndex, exists := requestIndexByFingerprint[fingerprint]; exists {
			// Python: 合并 hypothesis_ids 到既有共享探针
			existing := requests[duplicateIndex]
			merged := invStringList(existing["hypothesis_ids"])
			for _, id := range invStringList(normalized["hypothesis_ids"]) {
				if !containsString(merged, id) {
					merged = append(merged, id)
				}
			}
			existing["hypothesis_ids"] = merged
			continue
		}
		if requestLimit == 0 || len(requests) >= requestLimit {
			// Python: 继续扫描，让后续声明仍可绑定已保留的共享探针
			continue
		}
		requestID := strings.TrimSpace(stringValue(normalized["request_id"]))
		if requestID == "" {
			requestID = fmt.Sprintf("q%d", len(requests)+1)
		}
		if usedRequestIDs[requestID] {
			stem := requestID
			suffix := 2
			for usedRequestIDs[requestID] {
				requestID = fmt.Sprintf("%s_%d", stem, suffix)
				suffix++
			}
			normalized["request_id"] = requestID
		}
		usedRequestIDs[requestID] = true
		requestIndexByFingerprint[fingerprint] = len(requests)
		requests = append(requests, normalized)
	}
	// Python: require_evidence_per_hypothesis 覆盖校验
	if opts.RequireEvidencePerHypothesis {
		covered := map[string]bool{}
		for _, request := range requests {
			for _, id := range invStringList(request["hypothesis_ids"]) {
				covered[id] = true
			}
		}
		for id := range hypothesisIDs {
			if !covered[id] {
				return nil
			}
		}
	}
	// Python: inspect_artifact 无 proof_obligation 时按假设文本判定
	hypothesesByID := map[string]map[string]any{}
	for _, hypothesis := range hypotheses {
		hypothesesByID[hypothesis["hypothesis_id"].(string)] = hypothesis
	}
	for _, request := range requests {
		if stringValue(request["operation"]) != "inspect_artifact" || stringValue(request["proof_obligation"]) != "" {
			continue
		}
		var claimParts []string
		for _, id := range invStringList(request["hypothesis_ids"]) {
			if hypothesis, ok := hypothesesByID[id]; ok {
				claimParts = append(claimParts, stringValue(hypothesis["claim"]))
			}
		}
		claimText := strings.Join(claimParts, " ")
		switch {
		case absenceClaimPattern.MatchString(claimText):
			request["proof_obligation"] = "absence"
		case claimText != "" && existenceClaimPattern.MatchString(claimText):
			request["proof_obligation"] = "existence"
		default:
			request["proof_obligation"] = "coverage"
		}
	}
	readyWithoutMoreEvidence := false
	if v, ok := raw["ready_without_more_evidence"].(bool); ok {
		readyWithoutMoreEvidence = v
	}
	return map[string]any{
		"schema_version":              1,
		"hypotheses":                  hypotheses,
		"evidence_requests":           requests,
		"ready_without_more_evidence": readyWithoutMoreEvidence && len(requests) == 0,
	}
}

// requestExecutionFingerprint 执行语义指纹（Python _request_execution_fingerprint）
func requestExecutionFingerprint(request map[string]any) string {
	// Python: {k: v for k, v in request.items() if k not in {"request_id", "hypothesis_ids", "purpose"}}
	execution := map[string]any{}
	for key, value := range request {
		switch key {
		case "request_id", "hypothesis_ids", "purpose":
			continue
		}
		execution[key] = value
	}
	data, _ := json.Marshal(execution)
	return string(data)
}

// normalizeRequest 单请求归一化（Python _normalize_request, evidence_investigation.py:644-751）
func normalizeRequest(request map[string]any, defaultHypothesisID string, knownHypothesisIDs map[string]bool, index int) map[string]any {
	// Python: operation casefold；不在白名单 → nil
	operation := strings.ToLower(strings.TrimSpace(stringValue(request["operation"])))
	if !allowedOperations[operation] {
		return nil
	}
	// Python: query 必须非空的操作集
	query := strings.TrimSpace(stringValue(request["query"]))
	switch operation {
	case "search_trace", "inspect_artifact", "inspect_evaluation", "search_repository":
		if query == "" {
			return nil
		}
	}
	// Python: requested_path，read_artifact_window 校验失败回落 source
	requestedPath := stringValue(request["relative_path"])
	if operation == "read_artifact_window" && safeRelativePath(requestedPath) == "" {
		// Python: 接受控制器下发的 source 身份回喂
		requestedPath = stringValue(request["source"])
	}
	// Python: read_artifact_window/read_repository_file 须合法相对路径
	if operation == "read_artifact_window" || operation == "read_repository_file" {
		if safeRelativePath(requestedPath) == "" {
			return nil
		}
	}
	// Python: check_relation 须 expression
	if operation == "check_relation" && strings.TrimSpace(stringValue(request["expression"])) == "" {
		return nil
	}
	// Python: compare_numeric_change 须 before/after 双表达式
	if operation == "compare_numeric_change" {
		before := strings.TrimSpace(stringValue(request["before_expression"]))
		after := strings.TrimSpace(stringValue(request["after_expression"]))
		if before == "" || after == "" {
			return nil
		}
	}
	// Python: hypothesis_ids + default 回填 + 白名单过滤 + 保序去重
	hypothesisIDs := invStringList(request["hypothesis_ids"])
	if defaultHypothesisID != "" {
		hypothesisIDs = append(hypothesisIDs, defaultHypothesisID)
	}
	var filteredIDs []string
	seen := map[string]bool{}
	for _, id := range hypothesisIDs {
		if knownHypothesisIDs[id] && !seen[id] {
			seen[id] = true
			filteredIDs = append(filteredIDs, id)
		}
	}
	hypothesisIDs = filteredIDs
	normalized := map[string]any{
		"request_id":     strings.TrimSpace(stringValue(request["request_id"])),
		"hypothesis_ids": hypothesisIDs,
		"operation":      operation,
		"purpose":        strings.TrimSpace(stringValue(request["purpose"])),
	}
	if normalized["request_id"] == "" {
		normalized["request_id"] = fmt.Sprintf("q%d", index)
	}
	if query != "" {
		// Python: query[:1_000]
		if len(query) > 1000 {
			query = query[:1000]
		}
		normalized["query"] = query
	}
	if operation == "inspect_artifact" {
		// Python: relative_path or source 提示
		artifactHint := safeRelativePath(stringValue(request["relative_path"]))
		if artifactHint == "" {
			artifactHint = safeRelativePath(stringValue(request["source"]))
		}
		if artifactHint != "" {
			normalized["relative_path"] = artifactHint
		}
		proofObligation := strings.ToLower(strings.TrimSpace(stringValue(request["proof_obligation"])))
		switch proofObligation {
		case "existence", "absence", "coverage":
			normalized["proof_obligation"] = proofObligation
		}
	}
	if operation == "check_relation" {
		// Python: expected 必须可解析为有限数
		expected := finiteNumber(request["expected"])
		if expected == nil {
			return nil
		}
		operator := strings.ToLower(strings.TrimSpace(stringValue(request["operator"])))
		if operator == "" {
			operator = "approximately_equal"
		}
		normalized["expression"] = clipStr(strings.TrimSpace(stringValue(request["expression"])), 200)
		normalized["operator"] = operator
		normalized["expected"] = *expected
		normalized["tolerance"] = boundedTolerance(request["tolerance"])
	}
	if operation == "compare_numeric_change" {
		expectedDelta := finiteNumber(request["expected_delta"])
		if expectedDelta == nil {
			return nil
		}
		normalized["before_expression"] = clipStr(strings.TrimSpace(stringValue(request["before_expression"])), 200)
		normalized["after_expression"] = clipStr(strings.TrimSpace(stringValue(request["after_expression"])), 200)
		normalized["expected_delta"] = *expectedDelta
		normalized["tolerance"] = boundedTolerance(request["tolerance"])
	}
	if operation == "search_trace" || operation == "search_repository" {
		// Python: max_results = min(_MAX_SEARCH_RESULTS, max(1, _positive_int(..., default=3)))
		maxResults := positiveInt(request["max_results"], 3)
		if maxResults < 1 {
			maxResults = 1
		}
		if maxResults > maxSearchResults {
			maxResults = maxSearchResults
		}
		normalized["max_results"] = maxResults
	}
	if operation == "read_repository_file" {
		normalized["relative_path"] = safeRelativePath(request["relative_path"])
	}
	if operation == "read_artifact_window" {
		normalized["relative_path"] = safeRelativePath(requestedPath)
		sourceCharStart := 0
		if v := optionalNonnegativeInt(request["source_char_start"]); v != nil {
			sourceCharStart = *v
		}
		var sourceCharEnd *int
		sourceCharEnd = optionalNonnegativeInt(request["source_char_end"])
		requestedMaxCharsAny, hasRequested := request["max_chars"]
		// Python: max_chars 缺省时 source_char_end > start → 差值
		if !hasRequested || requestedMaxCharsAny == nil {
			if sourceCharEnd != nil && *sourceCharEnd > sourceCharStart {
				requestedMaxCharsAny = *sourceCharEnd - sourceCharStart
			}
		}
		requestedMaxChars := maxArtifactWindowChars
		if requestedMaxCharsAny != nil {
			requestedMaxChars = positiveInt(requestedMaxCharsAny, maxArtifactWindowChars)
		}
		if requestedMaxChars < 1 {
			requestedMaxChars = 1
		}
		if requestedMaxChars > maxArtifactWindowChars {
			requestedMaxChars = maxArtifactWindowChars
		}
		normalized["source_char_start"] = sourceCharStart
		normalized["max_chars"] = requestedMaxChars
	}
	if operation == "read_event" {
		// Python: message_index 必须存在
		messageIndex := optionalNonnegativeInt(request["message_index"])
		if messageIndex == nil {
			return nil
		}
		normalized["trace_id"] = strings.TrimSpace(stringValue(request["trace_id"]))
		normalized["message_index"] = *messageIndex
		if toolCallIndex := optionalNonnegativeInt(request["tool_call_index"]); toolCallIndex != nil {
			normalized["tool_call_index"] = *toolCallIndex
		}
	}
	return normalized
}

// clipStr 按 rune 截断（Python text[:limit]）
func clipStr(text string, limit int) string {
	r := []rune(text)
	if len(r) > limit {
		r = r[:limit]
	}
	return string(r)
}

// safeRelativePath 相对路径校验（Python _safe_relative_path, evidence_investigation.py:1362）
func safeRelativePath(value any) string {
	text := strings.ReplaceAll(strings.TrimSpace(stringValue(value)), "\\", "/")
	if text == "" || strings.HasPrefix(text, "/") || filepath.IsAbs(text) ||
		len(text) >= 2 && text[1] == ':' && text[0] >= 'A' && text[0] <= 'z' {
		return ""
	}
	var parts []string
	for _, part := range strings.Split(text, "/") {
		switch part {
		case "", ".":
			continue
		case "..":
			return ""
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "/")
}

// normalizedSemantics 归一化语义（Python _normalized_semantics）
func normalizedSemantics(value string) string {
	// Python: " ".join(re.findall(r"[a-z0-9_]+|[\u4e00-\u9fff]", value.casefold()))
	matches := termTokenPattern.FindAllString(strings.ToLower(value), -1)
	return strings.Join(matches, " ")
}

// termTokenPattern Python re.findall(r"[a-z0-9_]+|[\u4e00-\u9fff]")（小写后分词）
var termTokenPattern = regexp.MustCompile(`[a-z0-9_]+|[\x{4e00}-\x{9fff}]`)

// positiveInt 正整数解析，非法/非正回退默认（Python _positive_int）
func positiveInt(value any, defaultValue int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(stringValue(value)))
	if err != nil {
		return defaultValue
	}
	if parsed > 0 {
		return parsed
	}
	return defaultValue
}

// optionalNonnegativeInt 非负整数解析，非法/负数返 nil（Python _optional_nonnegative_int）
func optionalNonnegativeInt(value any) *int {
	parsed, err := strconv.Atoi(strings.TrimSpace(stringValue(value)))
	if err != nil || parsed < 0 {
		return nil
	}
	return &parsed
}

// stringValue 任意值 → 字符串（Python str(value or "") 语义）
func stringValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		if v == "<nil>" {
			return ""
		}
		return v
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprintf("%v", v)
	}
}

// stringList 字符串列表提取（Python _string_list 语义）
func invStringList(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	var result []string
	for _, item := range items {
		if s := stringValue(item); s != "" {
			result = append(result, s)
		}
	}
	return result
}

// containsString 列表包含判断
func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

// minInt 取小
func invMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// finiteNumber 有限数提取（Python _finite_number）：bool 与非数值返 nil
func finiteNumber(value any) *float64 {
	switch v := value.(type) {
	case bool:
		return nil
	case int:
		f := float64(v)
		return &f
	case int64:
		f := float64(v)
		return &f
	case float64:
		f := v
		return &f
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return nil
		}
		return &f
	default:
		return nil
	}
}

// boundedTolerance 容差钳制到 [0,1]，非法回退 1e-9（Python _bounded_tolerance）
func boundedTolerance(value any) float64 {
	number := finiteNumber(value)
	if number == nil {
		return 1e-9
	}
	if *number < 0.0 {
		return 0.0
	}
	if *number > 1.0 {
		return 1.0
	}
	return *number
}
