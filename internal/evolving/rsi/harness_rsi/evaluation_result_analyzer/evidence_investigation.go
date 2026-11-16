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
	"archive/zip"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	docxlib "github.com/fumiama/go-docx"
	"github.com/ledongthuc/pdf"
	"github.com/xuri/excelize/v2"
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

// ──────────────────────────── Task 7：执行分发与 trace/数值/check 族 ────────────────────────────

// ExecuteCausalInvestigation 执行调查计划中控制器拥有的只读证据操作（Python execute_causal_investigation）
func ExecuteCausalInvestigation(
	caseInput *CaseAnalysisInput,
	investigation map[string]any,
	priorCandidateFeedback map[string]any,
	evidenceRoot string,
) map[string]any {
	// Python: trace_data = _read_json(Path(case.result_path).parent / "judge" / "normalized_trace.json")
	traceData := readJSONInv(filepath.Join(filepath.Dir(caseInput.ResultPath), "judge", "normalized_trace.json"))
	events := traceEvents(traceData)
	repositoryDir := repositoryDirInv(evidenceRoot)
	discoveredRepositoryPaths := map[string]bool{}
	artifactTextCache := map[string]string{}
	var results []map[string]any
	// Python: for request in investigation.get("evidence_requests", [])
	requestsAny, _ := investigation["evidence_requests"].([]any)
	for _, reqAny := range requestsAny {
		request, ok := reqAny.(map[string]any)
		if !ok {
			continue
		}
		operation := stringValue(request["operation"])
		var evidence map[string]any
		switch operation {
		case "check_relation":
			evidence = checkRelation(request)
		case "compare_numeric_change":
			evidence = compareNumericChange(request)
		case "search_trace":
			evidence = searchTrace(events, request)
		case "read_event":
			evidence = readEvent(events, request)
		case "inspect_artifact":
			evidence = inspectArtifact(caseInput, request, artifactTextCache)
		case "read_artifact_window":
			evidence = readArtifactWindow(caseInput, request, artifactTextCache)
		case "inspect_evaluation":
			evidence = inspectEvaluation(caseInput, request)
		case "search_repository":
			evidence = searchRepository(repositoryDir, request)
			for _, item := range invStringList2(evidence["files"]) {
				if p := stringValue(item["relative_path"]); p != "" {
					discoveredRepositoryPaths[p] = true
				}
			}
		case "read_repository_file":
			evidence = readRepositoryFile(repositoryDir, request, discoveredRepositoryPaths)
		case "compare_runs":
			evidence = compareRuns(priorCandidateFeedback, request)
		default:
			continue
		}
		result := map[string]any{
			"request_id":       stringValue(request["request_id"]),
			"hypothesis_ids":   invStringList(request["hypothesis_ids"]),
			"operation":        operation,
			"purpose":          stringValue(request["purpose"]),
			"proof_obligation": stringValue(request["proof_obligation"]),
		}
		for key, value := range evidence {
			result[key] = value
		}
		results = append(results, result)
	}
	// Python: 自动窗口闭合
	automaticRequests, closureResults, closure := closeIncompleteArtifactEvidence(
		caseInput, investigation, results, artifactTextCache)
	results = append(results, closureResults...)
	modelRequestCount := 0
	if v, ok := investigation["evidence_requests"].([]any); ok {
		modelRequestCount = len(v)
	}
	return map[string]any{
		"schema_version": 1,
		"policy": map[string]any{
			"controller_owned":               true,
			"read_only":                      true,
			"arbitrary_shell_or_path_access": false,
			"display_omissions_are_not_task_agent_observations": true,
		},
		"hypotheses":                toAnySlice(investigation["hypotheses"]),
		"request_count":             modelRequestCount + len(automaticRequests),
		"model_request_count":       modelRequestCount,
		"automatic_request_count":   len(automaticRequests),
		"automatic_requests":        automaticRequests,
		"completed_request_count":   len(results),
		"artifact_evidence_closure": closure,
		"results":                   results,
	}
}

// closeIncompleteArtifactEvidence 不完整 artifact 证据的确定性闭合（Python _close_incomplete_artifact_evidence）
func closeIncompleteArtifactEvidence(
	caseInput *CaseAnalysisInput,
	investigation map[string]any,
	initialResults []map[string]any,
	textCache map[string]string,
) ([]map[string]any, []map[string]any, map[string]any) {
	existingIDs := map[string]bool{}
	for _, item := range initialResults {
		if id := stringValue(item["request_id"]); id != "" {
			existingIDs[id] = true
		}
	}
	// Python: 显式窗口计入覆盖，不重复读取
	coveredRanges := map[string][][2]int{}
	sourceCounts := map[string]int{}
	for _, item := range initialResults {
		if stringValue(item["operation"]) != "read_artifact_window" {
			continue
		}
		if stringValue(item["availability"]) != "available" {
			continue
		}
		source := stringValue(item["source"])
		startPtr := optionalNonnegativeInt(item["source_char_start"])
		endPtr := optionalNonnegativeInt(item["source_char_end"])
		countPtr := optionalNonnegativeInt(item["source_char_count"])
		if source == "" || startPtr == nil || endPtr == nil {
			continue
		}
		if *endPtr >= *startPtr {
			coveredRanges[source] = append(coveredRanges[source], [2]int{*startPtr, *endPtr})
			if countPtr != nil {
				sourceCounts[source] = *countPtr
			}
		}
	}
	var candidates []map[string]any
	candidateBySource := map[string]map[string]any{}
	for _, item := range initialResults {
		if stringValue(item["operation"]) != "inspect_artifact" {
			continue
		}
		if stringValue(item["availability"]) != "available" {
			continue
		}
		hypothesisIDs := invStringList(item["hypothesis_ids"])
		if len(hypothesisIDs) == 0 {
			continue
		}
		if stringValue(item["proof_obligation"]) == "existence" {
			// Python: 物理命中即存在性声明的完整见证，读无关尾部不能加强该义务
			continue
		}
		matches, _ := item["matches"].([]any)
		if len(matches) > 0 {
			match, ok := matches[0].(map[string]any)
			if !ok {
				continue
			}
			source := stringValue(match["source"])
			if source == "" {
				continue
			}
			rawSpans, _ := match["exact_spans"].([]any)
			incomplete := false
			for _, spanAny := range rawSpans {
				if span, ok := spanAny.(map[string]any); ok {
					if wc, ok := span["window_complete"].(bool); ok && !wc {
						incomplete = true
					}
				}
			}
			if !incomplete {
				continue
			}
			parentRequestID := stringValue(item["request_id"])
			if existing := candidateBySource[source]; existing != nil {
				existing["hypothesis_ids"] = mergeUnique(invStringList(existing["hypothesis_ids"]), hypothesisIDs)
				existing["parent_request_ids"] = mergeUnique(invStringList(existing["parent_request_ids"]), []string{parentRequestID})
				continue
			}
			logicalSource := stringValue(match["logical_source"])
			if logicalSource == "" {
				logicalSource = source
			}
			candidate := map[string]any{
				"source":             source,
				"logical_source":     logicalSource,
				"hypothesis_ids":     hypothesisIDs,
				"parent_request_id":  parentRequestID,
				"parent_request_ids": []string{},
				"purpose":            stringValue(item["purpose"]),
			}
			if parentRequestID != "" {
				candidate["parent_request_ids"] = []string{parentRequestID}
			}
			candidateBySource[source] = candidate
			candidates = append(candidates, candidate)
			if len(candidates) >= maxAutomaticArtifactSources {
				break
			}
		}
		if len(candidates) >= maxAutomaticArtifactSources {
			break
		}
	}
	var automaticRequests []map[string]any
	var closureResults []map[string]any
	var sourceRecords []map[string]any
	totalChars := 0
	windowBudget := maxAutomaticArtifactWindows
	for _, candidate := range candidates {
		source := stringValue(candidate["source"])
		cursor := continuousPrefixEnd(coveredRanges[source])
		count, hasCount := sourceCounts[source]
		completed := hasCount && cursor >= count
		sourceWindows := 0
		sourceChars := 0
		lastReason := ""
		if completed {
			lastReason = "already_covered"
		}
		for !completed && windowBudget > 0 && totalChars < maxAutomaticArtifactChars {
			remainingChars := maxAutomaticArtifactChars - totalChars
			maxChars := maxArtifactWindowChars
			if remainingChars < maxChars {
				maxChars = remainingChars
			}
			requestID := uniqueAutomaticRequestID(
				stringValue(candidate["parent_request_id"]), source, cursor, existingIDs)
			request := map[string]any{
				"request_id":        requestID,
				"hypothesis_ids":    candidate["hypothesis_ids"],
				"operation":         "read_artifact_window",
				"relative_path":     source,
				"source_char_start": cursor,
				"max_chars":         maxChars,
				"purpose": "controller-owned continuation of an incomplete artifact search; " +
					"establish continuous source coverage before reasoning about absence",
				"automatic":          true,
				"parent_request_id":  candidate["parent_request_id"],
				"parent_request_ids": candidate["parent_request_ids"],
			}
			evidence := readArtifactWindow(caseInput, request, textCache)
			result := map[string]any{}
			for key, value := range request {
				result[key] = value
			}
			for key, value := range evidence {
				result[key] = value
			}
			automaticRequests = append(automaticRequests, request)
			closureResults = append(closureResults, result)
			existingIDs[requestID] = true
			windowBudget--
			sourceWindows++
			if stringValue(evidence["availability"]) != "available" {
				lastReason = stringValue(evidence["reason"])
				if lastReason == "" {
					lastReason = stringValue(evidence["availability"])
				}
				if lastReason == "" {
					lastReason = "unavailable"
				}
				break
			}
			startPtr := optionalNonnegativeInt(evidence["source_char_start"])
			endPtr := optionalNonnegativeInt(evidence["source_char_end"])
			countPtr := optionalNonnegativeInt(evidence["source_char_count"])
			if startPtr == nil || endPtr == nil || countPtr == nil {
				lastReason = "non_contiguous_controller_window"
				break
			}
			if *startPtr != cursor || *endPtr <= *startPtr {
				lastReason = "non_contiguous_controller_window"
				break
			}
			readChars := *endPtr - *startPtr
			totalChars += readChars
			sourceChars += readChars
			coveredRanges[source] = append(coveredRanges[source], [2]int{*startPtr, *endPtr})
			sourceCounts[source] = *countPtr
			nextCursor := continuousPrefixEnd(coveredRanges[source])
			if nextCursor <= cursor {
				lastReason = "no_forward_progress"
				break
			}
			cursor = nextCursor
			completed = cursor >= *countPtr
			if completed {
				lastReason = "complete"
			} else {
				lastReason = "continuation_required"
			}
		}
		if !completed && lastReason == "" {
			lastReason = "budget_exhausted"
		}
		if !completed && (windowBudget <= 0 || totalChars >= maxAutomaticArtifactChars) {
			lastReason = "budget_exhausted"
		}
		countValue := any(nil)
		if c, ok := sourceCounts[source]; ok {
			countValue = c
		}
		sourceRecords = append(sourceRecords, map[string]any{
			"source":                     source,
			"logical_source":             candidate["logical_source"],
			"parent_request_id":          candidate["parent_request_id"],
			"continuous_source_char_end": cursor,
			"source_char_count":          countValue,
			"window_count":               sourceWindows,
			"read_char_count":            sourceChars,
			"complete":                   completed,
			"status":                     lastReason,
		})
	}
	attempted := len(candidates) > 0
	allComplete := attempted
	for _, record := range sourceRecords {
		if !record["complete"].(bool) {
			allComplete = false
		}
	}
	budgetExhausted := false
	for _, record := range sourceRecords {
		if record["status"] == "budget_exhausted" {
			budgetExhausted = true
		}
	}
	status := "not_needed"
	switch {
	case allComplete:
		status = "completed"
	case budgetExhausted:
		status = "budget_exhausted"
	case attempted:
		status = "incomplete"
	}
	completedSourceCount := 0
	for _, record := range sourceRecords {
		if record["complete"].(bool) {
			completedSourceCount++
		}
	}
	return automaticRequests, closureResults, map[string]any{
		"attempted":              attempted,
		"status":                 status,
		"candidate_source_count": len(candidates),
		"completed_source_count": completedSourceCount,
		"automatic_window_count": len(automaticRequests),
		"read_char_count":        totalChars,
		"limits": map[string]any{
			"max_sources":          maxAutomaticArtifactSources,
			"max_windows":          maxAutomaticArtifactWindows,
			"max_chars":            maxAutomaticArtifactChars,
			"max_chars_per_window": maxArtifactWindowChars,
		},
		"sources": sourceRecords,
	}
}

// continuousPrefixEnd 已覆盖区间的连续前缀终点（Python _continuous_prefix_end）
func continuousPrefixEnd(ranges [][2]int) int {
	cursor := 0
	sortedRanges := append([][2]int{}, ranges...)
	for i := 0; i < len(sortedRanges); i++ {
		for j := i + 1; j < len(sortedRanges); j++ {
			if sortedRanges[j][0] < sortedRanges[i][0] {
				sortedRanges[i], sortedRanges[j] = sortedRanges[j], sortedRanges[i]
			}
		}
	}
	for _, r := range sortedRanges {
		if r[0] > cursor {
			break
		}
		if r[1] > cursor {
			cursor = r[1]
		}
	}
	return cursor
}

// uniqueAutomaticRequestId 自动请求 id（Python _unique_automatic_request_id）
func uniqueAutomaticRequestID(parentRequestID string, source string, start int, existingIDs map[string]bool) string {
	stem := regexp.MustCompile(`[^a-zA-Z0-9_.-]+`).ReplaceAllString(parentRequestID, "_")
	stem = strings.Trim(stem, "_")
	if stem == "" {
		stem = "artifact"
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(source)))[:8]
	candidate := fmt.Sprintf("%s.auto.%s.%d", stem, digest, start)
	suffix := 2
	for existingIDs[candidate] {
		candidate = fmt.Sprintf("%s.auto.%s.%d.%d", stem, digest, start, suffix)
		suffix++
	}
	return candidate
}

// checkRelation 数值判别（Python _check_relation）
func checkRelation(request map[string]any) map[string]any {
	expression := strings.TrimSpace(stringValue(request["expression"]))
	expectedPtr := finiteNumber(request["expected"])
	operator := strings.ToLower(stringValue(request["operator"]))
	if operator == "" {
		operator = "approximately_equal"
	}
	tolerance := boundedTolerance(request["tolerance"])
	if expectedPtr == nil {
		return map[string]any{"availability": "invalid", "reason": "expected_must_be_a_finite_number"}
	}
	value, err := safeNumericExpression(expression)
	if err != nil {
		return map[string]any{
			"availability": "invalid",
			"expression":   expression,
			"reason":       "invalid_numeric_expression:" + err.Error(),
		}
	}
	expected := *expectedPtr
	closeEnough := mathClose(value, expected, tolerance)
	var holds bool
	switch operator {
	case "approximately_equal", "equal":
		holds = closeEnough
	case "not_equal":
		holds = !closeEnough
	case "less_than":
		holds = value < expected
	case "less_than_or_equal":
		holds = value <= expected || closeEnough
	case "greater_than":
		holds = value > expected
	case "greater_than_or_equal":
		holds = value >= expected || closeEnough
	default:
		return map[string]any{
			"availability": "invalid",
			"expression":   expression,
			"value":        value,
			"reason":       "unsupported_relation_operator",
		}
	}
	return map[string]any{
		"availability": "available",
		"expression":   expression,
		"value":        value,
		"operator":     operator,
		"expected":     expected,
		"tolerance":    tolerance,
		"holds":        holds,
	}
}

// compareNumericChange 计算 after-before（Python _compare_numeric_change）
func compareNumericChange(request map[string]any) map[string]any {
	beforeExpression := strings.TrimSpace(stringValue(request["before_expression"]))
	afterExpression := strings.TrimSpace(stringValue(request["after_expression"]))
	expectedDeltaPtr := finiteNumber(request["expected_delta"])
	tolerance := boundedTolerance(request["tolerance"])
	if expectedDeltaPtr == nil {
		return map[string]any{"availability": "invalid", "reason": "expected_delta_must_be_a_finite_number"}
	}
	before, errBefore := safeNumericExpression(beforeExpression)
	after, errAfter := safeNumericExpression(afterExpression)
	if errBefore != nil || errAfter != nil {
		errName := errBefore
		if errName == nil {
			errName = errAfter
		}
		return map[string]any{
			"availability":      "invalid",
			"before_expression": beforeExpression,
			"after_expression":  afterExpression,
			"reason":            "invalid_numeric_expression:" + errName.Error(),
		}
	}
	delta := after - before
	return map[string]any{
		"availability":      "available",
		"before_expression": beforeExpression,
		"after_expression":  afterExpression,
		"before_value":      before,
		"after_value":       after,
		"computed_delta":    delta,
		"expected_delta":    *expectedDeltaPtr,
		"tolerance":         tolerance,
		"holds":             mathClose(delta, *expectedDeltaPtr, tolerance),
	}
}

// safeNumericExpression 白名单数值表达式求值（Python _safe_numeric_expression）。
// Go go/ast 白名单：Constant(int/float，非 bool)/UnaryOp(+,-)/BinOp(+,-,*,/)，
// 节点数 ≤40、长度 ≤200、结果有限且 |value| ≤ 1e100（spec D-11）。
func safeNumericExpression(expression string) (float64, error) {
	if expression == "" || len(expression) > 200 {
		return 0, errors.New("expression_length")
	}
	tree, err := parser.ParseExpr(expression)
	if err != nil {
		return 0, errors.New("unsupported_expression_node")
	}
	nodeCount := 0
	ast.Inspect(tree, func(n ast.Node) bool {
		nodeCount++
		return nodeCount <= 40
	})
	if nodeCount > 40 {
		return 0, errors.New("expression_complexity")
	}
	value, evalErr := evalNumericNode(tree)
	if evalErr != nil {
		return 0, evalErr
	}
	if math.IsInf(value, 0) || math.IsNaN(value) || math.Abs(value) > 1e100 {
		return 0, errors.New("non_finite_or_unbounded_result")
	}
	return value, nil
}

// evalNumericNode AST 白名单递归求值
func evalNumericNode(node ast.Node) (float64, error) {
	switch n := node.(type) {
	case *ast.UnaryExpr:
		operand, err := evalNumericNode(n.X)
		if err != nil {
			return 0, err
		}
		switch n.Op {
		case token.ADD:
			return operand, nil
		case token.SUB:
			return -operand, nil
		}
		return 0, errors.New("unsupported_expression_node")
	case *ast.BinaryExpr:
		left, err := evalNumericNode(n.X)
		if err != nil {
			return 0, err
		}
		right, err := evalNumericNode(n.Y)
		if err != nil {
			return 0, err
		}
		switch n.Op {
		case token.ADD:
			return left + right, nil
		case token.SUB:
			return left - right, nil
		case token.MUL:
			return left * right, nil
		case token.QUO:
			return left / right, nil
		}
		return 0, errors.New("unsupported_expression_node")
	case *ast.ParenExpr:
		return evalNumericNode(n.X)
	case *ast.BasicLit:
		if n.Kind == token.INT || n.Kind == token.FLOAT {
			value, err := strconv.ParseFloat(n.Value, 64)
			if err != nil {
				return 0, errors.New("unsupported_expression_node")
			}
			return value, nil
		}
		return 0, errors.New("unsupported_expression_node")
	default:
		return 0, errors.New("unsupported_expression_node")
	}
}

// mathClose 绝对容差近似比较（Python math.isclose(rel_tol=0, abs_tol=tolerance)）
func mathClose(a, b, tolerance float64) bool {
	return math.Abs(a-b) <= tolerance
}

// traceEvents 轨迹事件展平（Python _trace_events）
func traceEvents(traceData map[string]any) []map[string]any {
	var events []map[string]any
	traces, ok := traceData["traces"].([]any)
	if !ok {
		return events
	}
	for _, traceAny := range traces {
		trace, ok := traceAny.(map[string]any)
		if !ok {
			continue
		}
		traceID := stringValue(trace["trace_id"])
		messages, ok := trace["messages"].([]any)
		if !ok {
			continue
		}
		for sequence, messageAny := range messages {
			message, ok := messageAny.(map[string]any)
			if !ok {
				continue
			}
			messageIndex := sequence
			if ptr := optionalNonnegativeInt(message["message_index"]); ptr != nil {
				messageIndex = *ptr
			} else if raw := stringValue(message["message_index"]); raw != "" {
				// Python: raw_index 非法时回退 sequence
				messageIndex = sequence
			}
			var toolCalls []map[string]any
			if calls, ok := message["tool_calls"].([]any); ok {
				for _, callAny := range calls {
					if call, ok := callAny.(map[string]any); ok {
						toolCalls = append(toolCalls, call)
					}
				}
			}
			events = append(events, map[string]any{
				"trace_id":      traceID,
				"message_index": messageIndex,
				"step_pointer":  stringValue(trace["step_pointer"]),
				"role":          stringValue(message["role"]),
				"content":       stringValue(message["content"]),
				"tool_calls":    toolCalls,
			})
		}
	}
	return events
}

// searchTrace 轨迹搜索（Python _search_trace）
func searchTrace(events []map[string]any, request map[string]any) map[string]any {
	query := stringValue(request["query"])
	terms := queryTerms(query)
	type ranked struct {
		score int
		seq   int
		event map[string]any
	}
	var rankedEvents []ranked
	for sequence, event := range events {
		data, _ := json.Marshal(event)
		searchable := strings.ToLower(string(data))
		var matched []string
		for _, term := range terms {
			if strings.Contains(searchable, term) {
				matched = append(matched, term)
			}
		}
		if len(matched) > 0 {
			unique := map[string]bool{}
			for _, m := range matched {
				unique[m] = true
			}
			rankedEvents = append(rankedEvents, ranked{score: len(unique), seq: sequence, event: event})
		}
	}
	limit := positiveInt(request["max_results"], 3)
	if limit < 1 {
		limit = 1
	}
	if limit > maxSearchResults {
		limit = maxSearchResults
	}
	// Python: sorted(key=(-score, seq))[:limit]
	for i := 0; i < len(rankedEvents); i++ {
		for j := i + 1; j < len(rankedEvents); j++ {
			if rankedEvents[j].score > rankedEvents[i].score ||
				(rankedEvents[j].score == rankedEvents[i].score && rankedEvents[j].seq < rankedEvents[i].seq) {
				rankedEvents[i], rankedEvents[j] = rankedEvents[j], rankedEvents[i]
			}
		}
	}
	if len(rankedEvents) > limit {
		rankedEvents = rankedEvents[:limit]
	}
	availability := "not_found"
	if len(rankedEvents) > 0 {
		availability = "available"
	}
	var eventViews []map[string]any
	for _, r := range rankedEvents {
		eventViews = append(eventViews, searchEventView(r.event, terms))
	}
	return map[string]any{
		"availability":        availability,
		"query":               query,
		"matched_event_count": len(rankedEvents),
		"events":              eventViews,
	}
}

// readEvent 读单事件（Python _read_event）
func readEvent(events []map[string]any, request map[string]any) map[string]any {
	traceID := stringValue(request["trace_id"])
	messageIndexPtr := optionalNonnegativeInt(request["message_index"])
	var matches []map[string]any
	for _, event := range events {
		if event["message_index"] == any(messageIndexPtr) || messageIndexPtr == nil {
			if messageIndexPtr != nil && event["message_index"] == *messageIndexPtr ||
				(messageIndexPtr == nil && event["message_index"] == nil) {
				// 匹配 message_index
			}
		}
	}
	matches = nil
	for _, event := range events {
		if messageIndexPtr == nil {
			continue
		}
		idx, ok := event["message_index"].(int)
		if !ok || idx != *messageIndexPtr {
			continue
		}
		if traceID == "" || event["trace_id"] == traceID {
			matches = append(matches, event)
		}
	}
	if len(matches) == 0 {
		var indexValue any
		if messageIndexPtr != nil {
			indexValue = *messageIndexPtr
		}
		return map[string]any{
			"availability":  "not_found",
			"trace_id":      traceID,
			"message_index": indexValue,
		}
	}
	if traceID == "" && len(matches) > 1 {
		candidateSet := map[string]bool{}
		for _, item := range matches {
			candidateSet[stringValue(item["trace_id"])] = true
		}
		var candidates []string
		for id := range candidateSet {
			candidates = append(candidates, id)
		}
		for i := 0; i < len(candidates); i++ {
			for j := i + 1; j < len(candidates); j++ {
				if candidates[j] < candidates[i] {
					candidates[i], candidates[j] = candidates[j], candidates[i]
				}
			}
		}
		return map[string]any{
			"availability":        "ambiguous",
			"reason":              "trace_id_required_for_non_unique_message_index",
			"message_index":       *messageIndexPtr,
			"candidate_trace_ids": toAnySlice(candidates),
		}
	}
	event := matches[0]
	var calls []map[string]any
	if event["tool_calls"] != nil {
		calls = event["tool_calls"].([]map[string]any)
	}
	if toolCallIndexPtr := optionalNonnegativeInt(request["tool_call_index"]); toolCallIndexPtr != nil {
		if *toolCallIndexPtr < len(calls) {
			calls = []map[string]any{calls[*toolCallIndexPtr]}
		} else {
			calls = nil
		}
	}
	var callViews []map[string]any
	for index, call := range calls {
		callViews = append(callViews, map[string]any{
			"tool_call_index": index,
			"name":            stringValue(call["name"]),
			"input":           boundedExactText(stringValue(call["input"]), maxEventChars),
			"output":          boundedExactText(stringValue(call["output"]), maxEventChars),
			"error":           boundedExactText(stringValue(call["error"]), 4000),
		})
	}
	return map[string]any{
		"availability": "available",
		"event": map[string]any{
			"trace_id":      event["trace_id"],
			"message_index": event["message_index"],
			"step_pointer":  event["step_pointer"],
			"role":          event["role"],
			"content":       boundedExactText(stringValue(event["content"]), maxEventChars),
			"tool_calls":    callViews,
		},
	}
}

// compareRuns 候选反馈对比（Python _compare_runs）
func compareRuns(priorCandidateFeedback map[string]any, request map[string]any) map[string]any {
	if len(priorCandidateFeedback) == 0 {
		return map[string]any{"availability": "not_available", "reason": "no_paired_candidate_feedback"}
	}
	// Python: json.dumps(..., ensure_ascii=False, indent=2)
	rendered, _ := json.MarshalIndent(priorCandidateFeedback, "", "  ")
	query := stringValue(request["query"])
	var terms []string
	if query != "" {
		terms = queryTerms(query)
	}
	spanResult := any(boundedExactText(string(rendered), maxEventChars))
	if len(terms) > 0 {
		spanResult = map[string]any{"exact_spans": exactMatchSpans(string(rendered), terms, 5)}
	}
	return map[string]any{
		"availability":    "available",
		"query":           query,
		"paired_feedback": spanResult,
	}
}

// searchEventView 搜索结果事件视图（Python _search_event_view）
func searchEventView(event map[string]any, terms []string) map[string]any {
	var callViews []map[string]any
	if calls, ok := event["tool_calls"].([]map[string]any); ok {
		for index, call := range calls {
			callViews = append(callViews, map[string]any{
				"tool_call_index": index,
				"name":            stringValue(call["name"]),
				"input_spans":     exactMatchSpans(stringValue(call["input"]), terms, 2),
				"output_spans":    exactMatchSpans(stringValue(call["output"]), terms, 3),
				"error_spans":     exactMatchSpans(stringValue(call["error"]), terms, 2),
			})
		}
	}
	return map[string]any{
		"trace_id":      event["trace_id"],
		"message_index": event["message_index"],
		"step_pointer":  event["step_pointer"],
		"role":          event["role"],
		"content_spans": exactMatchSpans(stringValue(event["content"]), terms, 2),
		"tool_calls":    callViews,
	}
}

// exactMatchSpans 精确匹配 span 窗口（Python _exact_match_spans）
func exactMatchSpans(text string, terms []string, maxSpans int) []map[string]any {
	const spanChars = 2000
	if text == "" || len(terms) == 0 {
		return nil
	}
	lowered := strings.ToLower(text)
	type candidate struct {
		score        float64
		start, end   int
		matchedTerms []string
	}
	var candidates []candidate
	for _, term := range terms {
		startAt := 0
		for i := 0; i < 16; i++ {
			position := strings.Index(lowered[startAt:], term)
			if position < 0 {
				break
			}
			position += startAt
			start := position - spanChars/3
			if start < 0 {
				start = 0
			}
			end := start + spanChars
			if end > len(text) {
				end = len(text)
			}
			if end-spanChars > start {
				start = end - spanChars
			}
			if start < 0 {
				start = 0
			}
			window := lowered[start:end]
			var matched []string
			seenTerm := map[string]bool{}
			for _, c := range terms {
				if strings.Contains(window, c) && !seenTerm[c] {
					seenTerm[c] = true
					matched = append(matched, c)
				}
			}
			score := 0.0
			for m := range seenTerm {
				score += termWeight(m)
			}
			ratio := float64(len(matched)) / 10.0
			if ratio > 1.0 {
				ratio = 1.0
			}
			score += ratio
			candidates = append(candidates, candidate{score: score, start: start, end: end, matchedTerms: matched})
			startAt = position + len(term)
			if startAt < position+1 {
				startAt = position + 1
			}
		}
	}
	// Python: sorted(key=(-score, start))
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].score > candidates[i].score ||
				(candidates[j].score == candidates[i].score && candidates[j].start < candidates[i].start) {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}
	var spans []map[string]any
	type span [2]int
	var occupied []span
	for _, c := range candidates {
		overlap := false
		for _, o := range occupied {
			if c.start < o[1] && c.end > o[0] {
				overlap = true
				break
			}
		}
		if overlap {
			continue
		}
		occupied = append(occupied, span{c.start, c.end})
		matchedUnique := mergeUnique(nil, c.matchedTerms)
		matchedFirst := ""
		if len(matchedUnique) > 0 {
			matchedFirst = matchedUnique[0]
		}
		omissionOrigin := "none"
		if c.start != 0 || c.end < len(text) {
			omissionOrigin = "controller_search_window"
		}
		spans = append(spans, map[string]any{
			"source_char_start": c.start,
			"source_char_end":   c.end,
			"source_char_count": len([]rune(text)),
			"matched_term":      matchedFirst,
			"matched_terms":     matchedUnique,
			"text":              clipStr(text, c.end)[:minRunes(text, c.start, c.end)],
			"window_complete":   c.start == 0 && c.end == len(text),
			"omission_origin":   omissionOrigin,
		})
		if len(spans) >= maxSpans {
			break
		}
	}
	return spans
}

// minRunes 取 text[start:end] 的 rune 长度辅助
func minRunes(text string, start, end int) int {
	return len([]rune(text[start:end]))
}

// leadingTextSpan 首部窗口 span（Python _leading_text_span）
func leadingTextSpan(text string) []map[string]any {
	const spanChars = 2000
	if text == "" {
		return nil
	}
	end := len(text)
	if end > spanChars {
		end = spanChars
	}
	omissionOrigin := "none"
	if end < len(text) {
		omissionOrigin = "controller_search_window"
	}
	return []map[string]any{{
		"source_char_start": 0,
		"source_char_end":   end,
		"source_char_count": len(text),
		"matched_term":      "logical_source",
		"matched_terms":     []string{"logical_source"},
		"text":              text[:end],
		"window_complete":   end == len(text),
		"omission_origin":   omissionOrigin,
	}}
}

// queryMatchScore 查询匹配得分（Python _query_match_score）
func queryMatchScore(text string, terms []string) float64 {
	lowered := strings.ToLower(text)
	seen := map[string]bool{}
	score := 0.0
	for _, term := range terms {
		if !seen[term] && strings.Contains(lowered, term) {
			seen[term] = true
			score += termWeight(term)
		}
	}
	return score
}

// termWeight 词权重（Python _term_weight）
func termWeight(term string) float64 {
	weight := 1.0 + float64(minInt(len(term), 24))/24.0
	for _, ch := range term {
		if ch >= '0' && ch <= '9' {
			weight += 1.5
			break
		}
	}
	if strings.ContainsAny(term, "_./:-") {
		weight += 0.5
	}
	return weight
}

// boundedExactText 有界精确文本（Python _bounded_exact_text）
func boundedExactText(text string, limit int) map[string]any {
	if len(text) <= limit {
		return map[string]any{
			"text":              text,
			"source_char_count": len(text),
			"complete":          true,
			"omission_origin":   "none",
		}
	}
	return map[string]any{
		"text":                 text[:limit],
		"source_char_count":    len(text),
		"complete":             false,
		"omission_origin":      "controller_bound",
		"omitted_source_chars": len(text) - limit,
	}
}

// queryTerms 查询分词去停用词（Python _query_terms，上限 24）
func queryTerms(query string) []string {
	var terms []string
	seen := map[string]bool{}
	for _, match := range termPattern.FindAllString(query, -1) {
		normalized := strings.ToLower(match)
		if stopwords[normalized] || seen[normalized] {
			continue
		}
		seen[normalized] = true
		terms = append(terms, normalized)
		if len(terms) >= 24 {
			break
		}
	}
	return terms
}

// inspectArtifact artifact 检查（Python _inspect_artifact）
// 只搜物理落盘的任务产物；评估元数据被有意排除——此前判据或裁判解释
// 可在源文档缺席时满足 inspect_artifact 请求，把重复的结果描述变成
// 表面上的因果证据。
func inspectArtifact(caseInput *CaseAnalysisInput, request map[string]any, textCache map[string]string) map[string]any {
	query := stringValue(request["query"])
	artifactHint := strings.TrimSpace(stringValue(request["relative_path"]))
	purpose := strings.TrimSpace(stringValue(request["purpose"]))
	selectionQuery := strings.TrimSpace(query + " " + artifactHint + " " + purpose)
	terms := queryTerms(query)
	identityTerms := queryTerms(strings.TrimSpace(artifactHint + " " + purpose))
	caseDir := windowsLongPath(filepath.Dir(caseInput.ResultPath))
	type artifactSource struct{ source, logicalSource, text string }
	var sources []artifactSource
	artifactsDir := filepath.Join(caseDir, "artifacts")
	if info, err := os.Stat(artifactsDir); err == nil && info.IsDir() {
		aliases := artifactPathAliases(caseInput, artifactsDir)
		// 文本类 artifact：排序后截断到 _MAX_ARTIFACT_FILES
		var textFiles []string
		_ = filepath.WalkDir(artifactsDir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // Python rglob 忽略不可读项
			}
			if d.IsDir() {
				return nil
			}
			if textArtifactSuffixes[strings.ToLower(filepath.Ext(p))] {
				textFiles = append(textFiles, p)
			}
			return nil
		})
		sort.Strings(textFiles)
		if len(textFiles) > maxArtifactFiles {
			textFiles = textFiles[:maxArtifactFiles]
		}
		for _, path := range textFiles {
			resolved, _ := filepath.Abs(path)
			if !isRelativeTo(resolved, caseDir) {
				continue
			}
			rel, _ := filepath.Rel(artifactsDir, path)
			source := "artifacts/" + filepath.ToSlash(rel)
			logical := source
			if alias, ok := aliases[resolved]; ok {
				logical = alias
			}
			sources = append(sources, artifactSource{source, logical, readTextInv(path, maxArtifactFileChars)})
		}
		// 结构化类 artifact：按请求提示词挑选后解析
		var structuredFiles []string
		_ = filepath.WalkDir(artifactsDir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if structuredArtifactSuffixes[strings.ToLower(filepath.Ext(p))] {
				structuredFiles = append(structuredFiles, p)
			}
			return nil
		})
		sort.Strings(structuredFiles)
		structuredFiles = selectStructuredArtifactFiles(structuredFiles, selectionQuery, aliases)
		for _, path := range structuredFiles {
			resolved, _ := filepath.Abs(path)
			if !isRelativeTo(resolved, caseDir) {
				continue
			}
			var text string
			if cached, ok := textCache[resolved]; ok {
				text = cached
			} else {
				text = structuredArtifactText(path)
				textCache[resolved] = text
			}
			if text == "" {
				continue
			}
			rel, _ := filepath.Rel(artifactsDir, path)
			source := "artifacts/" + filepath.ToSlash(rel)
			logical := source
			if alias, ok := aliases[resolved]; ok {
				logical = alias
			}
			sources = append(sources, artifactSource{source, logical, text})
		}
	}

	if len(sources) == 0 {
		return map[string]any{
			"availability": "not_available",
			"reason":       "physical_artifact_snapshot_not_available",
			"query":        query,
			"matches":      []any{},
		}
	}

	if artifactHint != "" {
		hintTerms := queryTerms(artifactHint)
		bestScore := 0.0
		type scoredSource struct {
			logicalSource string
			score         float64
		}
		identityScores := make([]scoredSource, 0, len(sources))
		for _, item := range sources {
			score := queryMatchScore(item.logicalSource, hintTerms)
			identityScores = append(identityScores, scoredSource{item.logicalSource, score})
			if score > bestScore {
				bestScore = score
			}
		}
		if bestScore > 0 {
			bestLogical := map[string]bool{}
			for _, s := range identityScores {
				if s.score == bestScore {
					bestLogical[s.logicalSource] = true
				}
			}
			filtered := sources[:0]
			for _, item := range sources {
				if bestLogical[item.logicalSource] {
					filtered = append(filtered, item)
				}
			}
			sources = filtered
		}
	}

	type artifactMatch struct {
		score         float64
		source        string
		logicalSource string
		text          string
	}
	var matches []artifactMatch
	for _, item := range sources {
		lowered := strings.ToLower(item.text)
		matched := false
		for _, term := range terms {
			if strings.Contains(lowered, term) {
				matched = true
				break
			}
		}
		logicalLower := strings.ToLower(item.logicalSource)
		pathMatched := false
		for _, term := range terms {
			if strings.Contains(logicalLower, term) {
				pathMatched = true
				break
			}
		}
		if matched || pathMatched {
			score := queryMatchScore(item.text, terms) +
				2.0*queryMatchScore(item.logicalSource, terms) +
				4.0*queryMatchScore(item.logicalSource, identityTerms)
			matches = append(matches, artifactMatch{score, item.source, item.logicalSource, item.text})
		}
	}
	// Python: sorted(matches, key=lambda item: (-item[0], item[1]))[:_MAX_SEARCH_RESULTS]
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].source < matches[j].source
	})
	if len(matches) > maxSearchResults {
		matches = matches[:maxSearchResults]
	}
	availability := "not_found"
	if len(matches) > 0 {
		availability = "available"
	}
	matchList := make([]map[string]any, 0, len(matches))
	for _, m := range matches {
		spans := exactMatchSpans(m.text, terms, 3)
		if len(spans) == 0 {
			spans = leadingTextSpan(m.text)
		}
		matchList = append(matchList, map[string]any{
			"source":         m.source,
			"logical_source": m.logicalSource,
			"exact_spans":    spans,
		})
	}
	return map[string]any{
		"availability": availability,
		"query":        query,
		"matches":      matchList,
	}
}

// artifactPathAliases 有界快照路径 → 原始逻辑名映射（Python _artifact_path_aliases）
func artifactPathAliases(caseInput *CaseAnalysisInput, artifactsDir string) map[string]string {
	aliases := map[string]string{}
	snapshot, _ := caseInput.EvaluationMetadata["analysis_artifact_snapshot"].(map[string]any)
	rowsAny, _ := snapshot["files"].([]any)
	for _, rowAny := range rowsAny {
		row, ok := rowAny.(map[string]any)
		if !ok {
			continue
		}
		stored := safeRelativePath(row["path"])
		logical := strings.TrimSpace(stringValue(row["source_path"]))
		if stored == "" || logical == "" {
			continue
		}
		// Python: for candidate in (artifacts_dir / stored, artifacts_dir / "workspace" / stored)
		for _, candidate := range []string{
			filepath.Join(artifactsDir, filepath.FromSlash(stored)),
			filepath.Join(artifactsDir, "workspace", filepath.FromSlash(stored)),
		} {
			resolved, _ := filepath.Abs(candidate)
			info, err := os.Stat(resolved)
			if err == nil && !info.IsDir() && isRelativeTo(resolved, artifactsDir) {
				aliases[resolved] = logical
				break
			}
		}
	}
	return aliases
}

// selectStructuredArtifactFiles 限制结构化解析并优先请求指名的文件类型（Python _select_structured_artifact_files）
func selectStructuredArtifactFiles(files []string, query string, aliases map[string]string) []string {
	loweredQuery := strings.ToLower(query)
	hintedSuffixes := map[string]bool{}
	for suffix, hints := range structuredQuerySuffixHints {
		for _, hint := range hints {
			if strings.Contains(loweredQuery, hint) {
				hintedSuffixes[suffix] = true
				break
			}
		}
	}
	var candidates []string
	for _, path := range files {
		if len(hintedSuffixes) == 0 || hintedSuffixes[strings.ToLower(filepath.Ext(path))] {
			candidates = append(candidates, path)
		}
	}
	if len(candidates) <= maxStructuredArtifactsPerRequest {
		return candidates
	}
	queryTerms_ := queryTerms(query)
	type rankedFile struct {
		score float64
		size  int64
		path  string
	}
	ranked := make([]rankedFile, 0, len(candidates))
	for _, path := range candidates {
		resolved, _ := filepath.Abs(path)
		identity := ""
		if alias, ok := aliases[resolved]; ok {
			identity = alias
		}
		score := queryMatchScore(filepath.ToSlash(path)+" "+identity, queryTerms_)
		var size int64
		if info, err := os.Stat(path); err == nil {
			size = info.Size()
		}
		ranked = append(ranked, rankedFile{score, size, path})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		if ranked[i].size != ranked[j].size {
			return ranked[i].size < ranked[j].size
		}
		return filepath.ToSlash(ranked[i].path) < filepath.ToSlash(ranked[j].path)
	})
	if len(ranked) > maxStructuredArtifactsPerRequest {
		ranked = ranked[:maxStructuredArtifactsPerRequest]
	}
	out := make([]string, 0, len(ranked))
	for _, r := range ranked {
		out = append(out, r.path)
	}
	return out
}

// readArtifactWindow 从先前指名的任务 artifact 读取一个精确有界窗口（Python _read_artifact_window）
func readArtifactWindow(caseInput *CaseAnalysisInput, request map[string]any, textCache map[string]string) map[string]any {
	relativePath := safeRelativePath(request["relative_path"])
	relativePath = strings.TrimPrefix(relativePath, "artifacts/")
	if relativePath == "" {
		return map[string]any{"availability": "invalid", "reason": "invalid_relative_path"}
	}
	caseDir := windowsLongPath(filepath.Dir(caseInput.ResultPath))
	artifactsDir, _ := filepath.Abs(filepath.Join(caseDir, "artifacts"))
	aliases := artifactPathAliases(caseInput, artifactsDir)
	path, _ := filepath.Abs(filepath.Join(artifactsDir, filepath.FromSlash(relativePath)))
	logicalSource := aliases[path]
	if !isRelativeTo(path, artifactsDir) || !isRegularFile(path) {
		workspacePath, _ := filepath.Abs(filepath.Join(artifactsDir, "workspace", filepath.FromSlash(relativePath)))
		if isRelativeTo(workspacePath, artifactsDir) && isRegularFile(workspacePath) {
			path = workspacePath
			logicalSource = aliases[path]
		} else {
			requested := strings.TrimPrefix(relativePath, "workspace/")
			var logicalMatches [][2]string // [stored, logical]
			for stored, logical := range aliases {
				if strings.TrimPrefix(strings.ReplaceAll(logical, "\\", "/"), "workspace/") == requested {
					logicalMatches = append(logicalMatches, [2]string{stored, logical})
				}
			}
			if len(logicalMatches) == 0 {
				for _, pair := range unambiguousLogicalArtifactMatches(requested, aliases) {
					logicalMatches = append(logicalMatches, pair)
				}
			}
			if len(logicalMatches) != 1 {
				return map[string]any{"availability": "not_found", "relative_path": relativePath}
			}
			path = logicalMatches[0][0]
			logicalSource = logicalMatches[0][1]
		}
	}
	if !isRelativeTo(path, artifactsDir) || !isRegularFile(path) {
		return map[string]any{"availability": "not_found", "relative_path": relativePath}
	}
	suffix := strings.ToLower(filepath.Ext(path))
	var content string
	if structuredArtifactSuffixes[suffix] {
		if cached, ok := textCache[path]; ok {
			content = cached
		} else {
			content = structuredArtifactText(path)
			textCache[path] = content
		}
	} else if textArtifactSuffixes[suffix] {
		content = readTextInv(path, maxArtifactFileChars)
	} else {
		return map[string]any{"availability": "unsupported", "relative_path": relativePath}
	}
	if content == "" {
		return map[string]any{"availability": "not_available", "relative_path": relativePath}
	}
	// Python: start = min(len(content), _optional_nonnegative_int(...) or 0)——缺省 0
	start := 0
	if ptr := optionalNonnegativeInt(request["source_char_start"]); ptr != nil {
		start = *ptr
		if start > len(content) {
			start = len(content)
		}
	}
	maxChars := positiveInt(request["max_chars"], maxArtifactWindowChars)
	if maxChars > maxArtifactWindowChars {
		maxChars = maxArtifactWindowChars
	}
	if maxChars < 1 {
		maxChars = 1
	}
	end := len(content)
	if start+maxChars < end {
		end = start + maxChars
	}
	physicalRel, _ := filepath.Rel(artifactsDir, path)
	physical := "artifacts/" + filepath.ToSlash(physicalRel)
	logical := logicalSource
	if logical == "" {
		logical = aliases[path]
	}
	if logical == "" {
		logical = physical
	}
	windowComplete := start == 0 && end == len(content)
	nextStart := any(nil)
	if end < len(content) {
		nextStart = end
	}
	omissionOrigin := "none"
	if start != 0 || end < len(content) {
		omissionOrigin = "controller_read_window"
	}
	return map[string]any{
		"availability":           "available",
		"source":                 physical,
		"logical_source":         logical,
		"source_char_start":      start,
		"source_char_end":        end,
		"source_char_count":      len(content),
		"text":                   content[start:end],
		"window_complete":        windowComplete,
		"next_source_char_start": nextStart,
		"omission_origin":        omissionOrigin,
	}
}

// unambiguousLogicalArtifactMatches 演示层路径编码变化时恢复被指名的 artifact（Python _unambiguous_logical_artifact_matches）
func unambiguousLogicalArtifactMatches(requested string, aliases map[string]string) [][2]string {
	// Python: requested_terms = {term for term in _query_terms(Path(requested).name) if len(term) >= 3}
	requestedTerms := map[string]bool{}
	for _, term := range queryTerms(filepath.Base(requested)) {
		if len(term) >= 3 {
			requestedTerms[term] = true
		}
	}
	if len(requestedTerms) < 2 {
		return nil
	}
	type rankedAlias struct {
		score         int
		stored        string
		logicalSource string
	}
	var ranked []rankedAlias
	for stored, logical := range aliases {
		logicalTerms := map[string]bool{}
		for _, term := range queryTerms(filepath.Base(logical)) {
			if len(term) >= 3 {
				logicalTerms[term] = true
			}
		}
		overlap := 0
		for term := range requestedTerms {
			if logicalTerms[term] {
				overlap++
			}
		}
		if overlap > 0 {
			ranked = append(ranked, rankedAlias{overlap, stored, logical})
		}
	}
	if len(ranked) == 0 {
		return nil
	}
	best := 0
	for _, r := range ranked {
		if r.score > best {
			best = r.score
		}
	}
	minimum := (len(requestedTerms) + 1) / 2
	if minimum < 2 {
		minimum = 2
	}
	var winners [][2]string
	for _, r := range ranked {
		if r.score == best && r.score >= minimum {
			winners = append(winners, [2]string{r.stored, r.logicalSource})
		}
	}
	// Python: return winners if len(winners) == 1 else []
	if len(winners) == 1 {
		return winners
	}
	return nil
}

// windowsLongPath 深层评估 artifact 树的扩展 Windows 路径（Python _windows_long_path；GOOS 判据）
func windowsLongPath(path string) string {
	resolved, err := filepath.Abs(path)
	if err != nil {
		resolved = path
	}
	if runtime.GOOS != "windows" || strings.HasPrefix(resolved, `\\?\`) {
		return resolved
	}
	if strings.HasPrefix(resolved, `\\`) {
		return `\\?\UNC\` + strings.TrimPrefix(resolved, `\\`)
	}
	return `\\?\` + resolved
}

// isRelativeTo 判断 path 是否位于 base 之下（Python Path.is_relative_to 等价）
func isRelativeTo(path, base string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// isRegularFile 判断路径是否存在且为普通文件
func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// structuredArtifactText 结构化 artifact 文本提取（Python _structured_artifact_text）
// 畸形可选 artifact 是不可用证据，异常吞掉返回空串
func structuredArtifactText(path string) string {
	defer func() {
		// Python: except Exception: return ""
		_ = recover()
	}()
	suffix := strings.ToLower(filepath.Ext(path))
	switch suffix {
	case ".xlsx":
		return xlsxText(path)
	case ".docx":
		return docxText(path)
	case ".pdf":
		return pdfText(path)
	case ".pptx":
		return pptxText(path)
	}
	return ""
}

// xlsxText xlsx 单元格文本（Python _xlsx_text；excelize 对齐 openpyxl）
func xlsxText(path string) string {
	workbook, err := excelize.OpenFile(path)
	if err != nil {
		return ""
	}
	defer workbook.Close()
	var lines []string
	totalChars := 0
	cellCount := 0
	for _, sheet := range workbook.GetSheetList() {
		lines = append(lines, "[sheet:"+sheet+"]")
		rows, err := workbook.GetRows(sheet)
		if err != nil {
			continue
		}
		// Python: max_row=min(worksheet.max_row, 2_000), max_col=min(max_column, 200)
		maxRows := len(rows)
		if maxRows > 2000 {
			maxRows = 2000
		}
		for rowIdx, row := range rows[:maxRows] {
			maxCols := len(row)
			if maxCols > 200 {
				maxCols = 200
			}
			for colIdx := 0; colIdx < maxCols; colIdx++ {
				// Python: f"{cell.coordinate}={cell.value}"（coordinate 如 A1）；
				// openpyxl data_only=False 时公式单元格显示公式串（对齐），excelize 需显式取公式
				coordinate, _ := excelize.CoordinatesToCellName(colIdx+1, rowIdx+1)
				cellValue, _ := workbook.GetCellFormula(sheet, coordinate)
				if cellValue == "" {
					cellValue = row[colIdx]
				}
				if cellValue == "" {
					continue // Python: if cell.value is not None
				}
				line := coordinate + "=" + cellValue
				lines = append(lines, line)
				totalChars += len(line)
				cellCount++
				if totalChars >= maxArtifactFileChars || cellCount >= maxStructuredCells {
					return strings.Join(lines, "\n")
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}

// docxText docx 段落与表格文本（Python _docx_text；go-docx 对齐 python-docx）
func docxText(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ""
	}
	doc, err := docxlib.Parse(file, info.Size())
	if err != nil {
		return ""
	}
	var lines []string
	totalChars := 0
	// Python: enumerate(document.tables, start=1)——按 Body 中表格出现顺序计数
	tableIndex := 0
	appendLine := func(line string) bool {
		lines = append(lines, line)
		totalChars += len(line)
		return totalChars < maxArtifactFileChars
	}
	for _, item := range doc.Document.Body.Items {
		switch o := item.(type) {
		case *docxlib.Paragraph:
			text := paragraphRunText(o)
			if text == "" {
				continue // Python: if not paragraph.text: continue
			}
			if !appendLine(text) {
				return clipRunes(strings.Join(lines, "\n"), maxArtifactFileChars)
			}
		case *docxlib.Table:
			tableIndex++
			for rowIdx, row := range o.TableRows {
				values := make([]string, 0, len(row.TableCells))
				for _, cell := range row.TableCells {
					cellTexts := make([]string, 0, len(cell.Paragraphs))
					for _, para := range cell.Paragraphs {
						cellTexts = append(cellTexts, paragraphRunText(para))
					}
					// python-docx cell.text = "\n".join(paragraph texts)
					values = append(values, strings.Join(cellTexts, "\n"))
				}
				line := fmt.Sprintf("[table:%d:row:%d] %s", tableIndex, rowIdx+1, strings.Join(values, " | "))
				if !appendLine(line) {
					return clipRunes(strings.Join(lines, "\n"), maxArtifactFileChars)
				}
			}
		}
	}
	return clipRunes(strings.Join(lines, "\n"), maxArtifactFileChars)
}

// paragraphRunText 提取段落纯文本（python-docx paragraph.text = 直接 Run 文本拼接；超链接排除）
func paragraphRunText(p *docxlib.Paragraph) string {
	var sb strings.Builder
	for _, child := range p.Children {
		run, ok := child.(*docxlib.Run)
		if !ok {
			continue
		}
		for _, rc := range run.Children {
			if text, ok := rc.(*docxlib.Text); ok {
				sb.WriteString(text.Text)
			}
		}
	}
	return sb.String()
}

// pdfText pdf 页面文本（Python _pdf_text；ledongthuc/pdf 对齐 pdfplumber extract_text）
func pdfText(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	reader, err := pdf.NewReader(file, func() int64 {
		info, statErr := file.Stat()
		if statErr != nil {
			return 0
		}
		return info.Size()
	}())
	if err != nil {
		return ""
	}
	var lines []string
	totalChars := 0
	pageCount := reader.NumPage()
	if pageCount > maxStructuredPages {
		pageCount = maxStructuredPages
	}
	for pageIdx := 1; pageIdx <= pageCount; pageIdx++ {
		lines = append(lines, fmt.Sprintf("[page:%d]", pageIdx))
		page := reader.Page(pageIdx)
		text := ""
		if !page.V.IsNull() {
			// Python: page.extract_text() or ""
			if plain, pErr := page.GetPlainText(nil); pErr == nil {
				text = plain
			}
		}
		lines = append(lines, text)
		totalChars += len(fmt.Sprintf("[page:%d]", pageIdx)) + len(text)
		if totalChars >= maxArtifactFileChars {
			break // Python: if sum(len(item) for item in lines) >= ...: break
		}
	}
	return clipRunes(strings.Join(lines, "\n"), maxArtifactFileChars)
}

// pptxText pptx 幻灯片文本（Python _pptx_text；zip+xml 手拆与 Python 同款）
func pptxText(path string) string {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return ""
	}
	defer archive.Close()
	// Python: sorted(name for name in archive.namelist()
	//     if re.fullmatch(r"ppt/slides/slide\d+\.xml", name))[:_MAX_STRUCTURED_PAGES]
	slideRe := regexp.MustCompile(`^ppt/slides/slide\d+\.xml$`)
	var slideNames []string
	for _, f := range archive.File {
		if slideRe.MatchString(f.Name) {
			slideNames = append(slideNames, f.Name)
		}
	}
	sort.Strings(slideNames)
	if len(slideNames) > maxStructuredPages {
		slideNames = slideNames[:maxStructuredPages]
	}
	var lines []string
	for _, slideName := range slideNames {
		f, err := archive.Open(slideName)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			continue
		}
		var root pptxSlide
		if xml.Unmarshal(data, &root) != nil {
			continue
		}
		// Python: texts = [element.text or "" for element in root.iter() if element.tag.endswith("}t")]
		texts := collectSlideTexts(root.asNode())
		lines = append(lines, "["+slideName+"] "+strings.Join(texts, " "))
	}
	return clipRunes(strings.Join(lines, "\n"), maxArtifactFileChars)
}

// pptxSlide pptx 幻灯片 XML 通用节点（递归收集 *t 文本，等价 ElementTree.iter()）
type pptxSlide struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Content  string     `xml:",chardata"`
	Children []pptxNode `xml:",any"`
}

// asNode 幻灯片根节点转通用节点（collectSlideTexts 入参适配）
func (s *pptxSlide) asNode() *pptxNode {
	return &pptxNode{XMLName: s.XMLName, Content: s.Content, Children: s.Children}
}

// pptxNode 通用子节点
type pptxNode struct {
	XMLName  xml.Name
	Content  string     `xml:",chardata"`
	Children []pptxNode `xml:",any"`
}

// collectSlideTexts 递归收集 tag 以 "}t" 结尾的元素文本（等价 Python root.iter() 过滤）
func collectSlideTexts(node *pptxNode) []string {
	var texts []string
	var walk func(n *pptxNode)
	walk = func(n *pptxNode) {
		// Python: element.tag.endswith("}t")——tag 形如 "{namespace}t"，即 Local 名恰为 "t"
		if n.XMLName.Local == "t" {
			texts = append(texts, n.Content)
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	// 顶层不算，从子节点开始迭代（Python root.iter() 含 root 本身，
	// 但 root 是 p:sld，不以 }t 结尾，行为等价）
	for i := range node.Children {
		walk(&node.Children[i])
	}
	return texts
}

// clipRunes 按字符数截断（Python 切片 [:_MAX_ARTIFACT_FILE_CHARS] 按字符计）
func clipRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

// inspectEvaluation 成绩单摘要（Python _inspect_evaluation）
// 搜索评估器所有的结果元数据，但不把它称作 artifact
func inspectEvaluation(caseInput *CaseAnalysisInput, request map[string]any) map[string]any {
	query := stringValue(request["query"])
	terms := queryTerms(query)
	metadataJSON, _ := json.MarshalIndent(caseInput.EvaluationMetadata, "", "  ")
	type evalSource struct{ source, content string }
	sources := []evalSource{
		{"case.evaluation_metadata", string(metadataJSON)},
		{"case.result", readTextInv(caseInput.ResultPath, maxArtifactFileChars)},
	}
	type evalMatch struct {
		distinct int
		source   string
		content  string
	}
	var matches []evalMatch
	for _, src := range sources {
		lowered := strings.ToLower(src.content)
		matchedSet := map[string]bool{}
		for _, term := range terms {
			if strings.Contains(lowered, term) {
				matchedSet[term] = true
			}
		}
		if len(matchedSet) > 0 {
			matches = append(matches, evalMatch{len(matchedSet), src.source, src.content})
		}
	}
	// Python: sorted(matches, key=lambda item: (-item[0], item[1]))[:_MAX_SEARCH_RESULTS]
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].distinct != matches[j].distinct {
			return matches[i].distinct > matches[j].distinct
		}
		return matches[i].source < matches[j].source
	})
	if len(matches) > maxSearchResults {
		matches = matches[:maxSearchResults]
	}
	availability := "not_found"
	if len(matches) > 0 {
		availability = "available"
	}
	matchList := make([]map[string]any, 0, len(matches))
	for _, m := range matches {
		matchList = append(matchList, map[string]any{
			"source":      m.source,
			"exact_spans": exactMatchSpans(m.content, terms, 3),
		})
	}
	return map[string]any{
		"availability":   availability,
		"evidence_class": "evaluation_metadata",
		"query":          query,
		"matches":        matchList,
	}
}

// searchRepository 仓库搜索（Python _search_repository）
func searchRepository(repositoryDir string, request map[string]any) map[string]any {
	if repositoryDir == "" {
		return map[string]any{"availability": "not_available", "reason": "repository_snapshot_not_available"}
	}
	query := stringValue(request["query"])
	terms := queryTerms(query)
	type repoMatch struct {
		distinct int
		relative string
		text     string
	}
	var ranked []repoMatch
	for _, path := range repositoryTextFiles(repositoryDir) {
		text := readTextInv(path, maxArtifactFileChars)
		lowered := strings.ToLower(text)
		matchedSet := map[string]bool{}
		for _, term := range terms {
			if strings.Contains(lowered, term) {
				matchedSet[term] = true
			}
		}
		if len(matchedSet) > 0 {
			rel, _ := filepath.Rel(repositoryDir, path)
			ranked = append(ranked, repoMatch{len(matchedSet), filepath.ToSlash(rel), text})
		}
	}
	limit := positiveInt(request["max_results"], 3)
	if limit > maxSearchResults {
		limit = maxSearchResults
	}
	if limit < 1 {
		limit = 1
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].distinct != ranked[j].distinct {
			return ranked[i].distinct > ranked[j].distinct
		}
		return ranked[i].relative < ranked[j].relative
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	availability := "not_found"
	if len(ranked) > 0 {
		availability = "available"
	}
	files := make([]map[string]any, 0, len(ranked))
	for _, m := range ranked {
		files = append(files, map[string]any{
			"relative_path": m.relative,
			"exact_spans":   exactMatchSpans(m.text, terms, 3),
		})
	}
	return map[string]any{
		"availability":       availability,
		"query":              query,
		"matched_file_count": len(ranked),
		"files":              files,
	}
}

// repositoryCodeSuffixes 仓库可读文本后缀集（Python _TEXT_ARTIFACT_SUFFIXES | {代码后缀}）
var repositoryCodeSuffixes = map[string]bool{
	".c": true, ".cc": true, ".cpp": true, ".go": true, ".h": true, ".hpp": true,
	".java": true, ".js": true, ".jsx": true, ".py": true, ".rs": true, ".sh": true,
	".ts": true, ".tsx": true,
}

// readRepositoryFile 仓库文件读取（Python _read_repository_file）
// 只允许读取先前 search_repository 返回过的路径
func readRepositoryFile(repositoryDir string, request map[string]any, discoveredPaths map[string]bool) map[string]any {
	if repositoryDir == "" {
		return map[string]any{"availability": "not_available", "reason": "repository_snapshot_not_available"}
	}
	relativePath := safeRelativePath(request["relative_path"])
	if relativePath == "" {
		return map[string]any{"availability": "invalid", "reason": "invalid_relative_path"}
	}
	if !discoveredPaths[relativePath] {
		return map[string]any{
			"availability":  "invalid",
			"reason":        "relative_path_not_returned_by_prior_search_repository",
			"relative_path": relativePath,
		}
	}
	path, _ := filepath.Abs(filepath.Join(repositoryDir, filepath.FromSlash(relativePath)))
	if !isRelativeTo(path, repositoryDir) || !isRegularFile(path) {
		return map[string]any{"availability": "not_found", "relative_path": relativePath}
	}
	suffix := strings.ToLower(filepath.Ext(path))
	if !textArtifactSuffixes[suffix] && !repositoryCodeSuffixes[suffix] {
		return map[string]any{"availability": "unsupported", "relative_path": relativePath}
	}
	return map[string]any{
		"availability":  "available",
		"relative_path": relativePath,
		"content":       boundedExactText(readTextInv(path, maxArtifactFileChars), maxEventChars),
	}
}

// repositoryTextFiles 仓库可读文本文件清单（Python _repository_text_files）
func repositoryTextFiles(repositoryDir string) []string {
	var files []string
	_ = filepath.WalkDir(repositoryDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Python rglob 忽略不可读项
		}
		if d.IsDir() {
			return nil
		}
		suffix := strings.ToLower(filepath.Ext(p))
		if !textArtifactSuffixes[suffix] && !repositoryCodeSuffixes[suffix] {
			return nil
		}
		resolved, _ := filepath.Abs(p)
		if !isRelativeTo(resolved, repositoryDir) {
			return nil
		}
		files = append(files, p)
		return nil
	})
	sort.Strings(files)
	if len(files) > maxRepositoryFiles {
		files = files[:maxRepositoryFiles]
	}
	return files
}

// readJSONInv JSON 文件读取，失败返空 map（Python _read_json）
func readJSONInv(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	var value map[string]any
	if json.Unmarshal(data, &value) != nil || value == nil {
		return map[string]any{}
	}
	return value
}

// readTextInv 限长文本读取（Python _read_text）
func readTextInv(path string, limit int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data[:minInt(len(data), limit)])
}

// repositoryDirInv 证据仓库根目录（Python _repository_dir）
// 返回 evidence_root/repository，须真实存在且位于根内；否则返回空串
func repositoryDirInv(evidenceRoot string) string {
	if evidenceRoot == "" {
		return ""
	}
	root, _ := filepath.Abs(expandUser(evidenceRoot))
	repository, _ := filepath.Abs(filepath.Join(root, "repository"))
	if info, err := os.Stat(repository); err == nil && info.IsDir() && isRelativeTo(repository, root) {
		return repository
	}
	return ""
}

// expandUser 展开 ~ 前缀（Python Path.expanduser 等价）
func expandUser(path string) string {
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				return home
			}
			if strings.HasPrefix(path, "~/") {
				return filepath.Join(home, path[2:])
			}
		}
	}
	return path
}

// toAnySlice 任意列表 → []any（JSON 往返辅助）
func toAnySlice(value any) []any {
	if list, ok := value.([]any); ok {
		return list
	}
	switch v := value.(type) {
	case []map[string]any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = item
		}
		return result
	case []string:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = item
		}
		return result
	default:
		return nil
	}
}

// invStringList2 map 列表提取
func invStringList2(value any) []map[string]any {
	if list, ok := value.([]map[string]any); ok {
		return list
	}
	if list, ok := value.([]any); ok {
		var result []map[string]any
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				result = append(result, m)
			}
		}
		return result
	}
	return nil
}

// mergeUnique 保序合并去重
func mergeUnique(existing []string, extra []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, item := range existing {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	for _, item := range extra {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}
