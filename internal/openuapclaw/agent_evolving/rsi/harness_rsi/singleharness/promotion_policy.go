// 单 harness 晋级规则与授权证据身份（promotion_policy.py 一比一对齐）。
package singleharness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator"
)

// EPSILON 非回归判定的绝对容差（Python EPSILON）。
const EPSILON = 1e-9

// ──────────────────────────── 导出函数 ────────────────────────────

// Digest 对 JSON 兼容的来源数据哈希，不圆整分数（Python digest）。
func Digest(value any) string {
	encoded, err := marshalDigestJSON(value)
	if err != nil {
		// Python 侧 allow_nan=False 会在非有限值时抛错；此处等价转为 panic（调用面均为已验证数据）
		panic(err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// PackageFingerprint 以内容识别发布包字节，与发布目录无关（Python package_fingerprint）。
func PackageFingerprint(refsPath string) (string, error) {
	refs, err := filepath.Abs(refsPath)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(refs)
	if err != nil {
		return "", err
	}
	var payload map[string]any
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return "", err
	}
	rawRefs, _ := payload["harness_refs"].(map[string]any)
	if rawRefs == nil {
		rawRefs = map[string]any{}
	}
	roles := map[string]any{}
	for role, raw := range rawRefs {
		rootPath, _ := raw.(string)
		root := expandUser(rootPath)
		if !filepath.IsAbs(root) {
			root = filepath.Join(filepath.Dir(refs), root)
		}
		root = filepath.Clean(root)
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			return "", evaluator.NewEvaluationInfrastructureError("Harness package directory is unavailable")
		}
		files := map[string]any{}
		var paths []string
		walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			paths = append(paths, path)
			return nil
		})
		if walkErr != nil {
			return "", walkErr
		}
		sort.Strings(paths)
		for _, path := range paths {
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil || relative == "." {
				continue
			}
			posix := filepath.ToSlash(relative)
			if pathHasIgnoredPart(posix) {
				continue
			}
			entry := pathEntry(path)
			if entry == nil || !isRegularFile(path) {
				continue
			}
			resolved, resErr := filepath.EvalSymlinks(path)
			if resErr != nil {
				return "", resErr
			}
			if !withinDir(resolved, root) {
				return "", evaluator.NewEvaluationInfrastructureError("Harness package links outside its snapshot")
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return "", readErr
			}
			sum := sha256.Sum256(content)
			files[posix] = hex.EncodeToString(sum[:])
		}
		roles[role] = files
	}
	if len(roles) == 0 {
		return "", evaluator.NewEvaluationInfrastructureError("Harness refs contain no packages")
	}
	return Digest(roles), nil
}

// CompleteScores 绝不平均缺失、非法、跳过或遗留二元 LLM 评分（Python complete_scores）。
func CompleteScores(evalRefPath string, expectedCaseIDs map[string]bool) (map[string]float64, error) {
	source := filepath.Clean(evalRefPath)
	data, err := os.ReadFile(source)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	if payload == nil {
		payload = map[string]any{}
	}
	scores := map[string]float64{}
	rawCases, _ := payload["cases"].([]any)
	for _, rawRow := range rawCases {
		row, ok := rawRow.(map[string]any)
		if !ok {
			continue
		}
		caseID := ""
		if raw, present := row["case_id"]; present {
			caseID = fmt.Sprint(raw)
		}
		score, _ := row["score"]
		_, duplicate := scores[caseID]
		if duplicate || caseID == "" {
			return nil, evaluator.NewEvaluationInfrastructureError("Evaluation contains duplicate or missing case identities")
		}
		numeric := isJSONNumber(score) && !isBoolValue(score)
		finite := numeric && isFiniteNumber(score)
		inRange := finite && numberInRange(score, 0, 1)
		statusOK := rowStatus(row) == "passed" || rowStatus(row) == "failed"
		metadata, _ := row["metadata"].(map[string]any)
		if metadata == nil {
			metadata = map[string]any{}
		}
		skipped, _ := metadata["infrastructure_skip"].(bool)
		if !inRange || !statusOK || skipped {
			return nil, evaluator.NewEvaluationInfrastructureError(fmt.Sprintf("Invalid correctness score for %s", caseID))
		}
		resultPath := filepath.Clean(fmt.Sprint(row["result_path"]))
		if !filepath.IsAbs(resultPath) {
			resultPath = filepath.Join(filepath.Dir(source), resultPath)
		}
		rawResult, readErr := os.ReadFile(resultPath)
		if readErr != nil {
			return nil, readErr
		}
		var result map[string]any
		if jsonErr := json.Unmarshal(rawResult, &result); jsonErr != nil {
			return nil, jsonErr
		}
		if result == nil {
			result = map[string]any{}
		}
		evaluation, _ := result["evaluation"].(map[string]any)
		if evaluation == nil {
			evaluation = map[string]any{}
		}
		executionStatus, _ := result["execution_status"].(string)
		status, _ := result["status"].(string)
		if executionStatus == "error" ||
			(status != "passed" && status != "failed") ||
			!numberEquals(result["score"], score) {
			return nil, evaluator.NewEvaluationInfrastructureError(fmt.Sprintf("Inconsistent or failed execution for %s", caseID))
		}
		method, _ := evaluation["method"].(string)
		judgeMethod := method == "llm_as_judge"
		evalMetadata, _ := evaluation["metadata"].(map[string]any)
		if evalMetadata == nil {
			evalMetadata = map[string]any{}
		}
		scoreMode, _ := evalMetadata["score_mode"].(string)
		continuous := scoreMode == "continuous"
		if judgeMethod && !continuous {
			return nil, evaluator.NewEvaluationInfrastructureError("Binary LLM grades cannot enter a continuous promotion run")
		}
		scores[caseID] = asFloat(score)
	}
	if len(scores) == 0 || !sameKeys(scores, expectedCaseIDs) {
		return nil, evaluator.NewEvaluationInfrastructureError("Evaluation does not exactly cover its frozen case set")
	}
	return scores, nil
}

// MeanScore 对已验证的非空连续分数集求平均（Python mean_score）。
func MeanScore(scores map[string]float64) (float64, error) {
	if len(scores) == 0 {
		return 0, evaluator.NewEvaluationInfrastructureError("Cannot score an empty evaluation")
	}
	total := 0.0
	for _, score := range scores {
		total += score
	}
	return total / float64(len(scores)), nil
}

// FirstImproved 首次提升必须严格超过配置的绝对裕度（Python first_improved）。
func FirstImproved(score, baseline, threshold float64) bool {
	return score-baseline > threshold+EPSILON
}

// TargetsImproved 每个目标用例必须独立超过各自的冻结基线裕度（Python targets_improved）。
func TargetsImproved(scores, baseline map[string]float64, targets map[string]bool, threshold float64) bool {
	if len(targets) == 0 {
		return false
	}
	for caseID := range targets {
		score, hasScore := scores[caseID]
		base, hasBase := baseline[caseID]
		if !hasScore || !hasBase || !FirstImproved(score, base, threshold) {
			return false
		}
	}
	return true
}

// FullEvaluationDecision 决定第二阶段独立全量评估，不平均化用例失败（Python full_evaluation_decision）。
func FullEvaluationDecision(scores, baseline map[string]float64, targets map[string]bool, maxRegression float64, proof map[string]any, identity string) (map[string]any, error) {
	sameCases := len(baseline) > 0 && sameKeysFF(scores, baseline)
	targetsKnown := len(targets) > 0 && targetsSubset(targets, baseline)
	proofIdentity, _ := proof["identity"].(string)
	qualified, _ := proof["qualified"].(bool)
	proofMatches := proofIdentity == identity && qualified
	rawProofTargets, _ := proof["target_case_ids"].([]any)
	proofTargets := map[string]bool{}
	for _, raw := range rawProofTargets {
		proofTargets[fmt.Sprint(raw)] = true
	}
	targetsMatch := sameKeysBB(proofTargets, targets)
	proofDigest, _ := proof["baseline_scores_digest"].(string)
	digestMatches := proofDigest == Digest(baseline)
	if !sameCases || !targetsKnown || !proofMatches {
		return nil, evaluator.NewEvaluationInfrastructureError("Full evaluation lacks its matching target improvement proof")
	}
	if !targetsMatch || !digestMatches {
		return nil, evaluator.NewEvaluationInfrastructureError("Full evaluation lacks its matching target improvement proof")
	}
	var failedTargets []string
	for caseID := range targets {
		if scores[caseID]-baseline[caseID] <= EPSILON {
			failedTargets = append(failedTargets, caseID)
		}
	}
	sort.Strings(failedTargets)
	var regressedOthers []string
	for caseID := range baseline {
		if targets[caseID] {
			continue
		}
		if baseline[caseID]-scores[caseID] > maxRegression+EPSILON {
			regressedOthers = append(regressedOthers, caseID)
		}
	}
	sort.Strings(regressedOthers)
	baselineMean, err := MeanScore(baseline)
	if err != nil {
		return nil, err
	}
	score, err := MeanScore(scores)
	if err != nil {
		return nil, err
	}
	averageImproved := score-baselineMean > EPSILON
	reason := "full_score_not_improved"
	if len(failedTargets) > 0 {
		reason = "target_case_not_improved"
	} else if len(regressedOthers) > 0 {
		reason = "non_target_case_regressed"
	} else if !averageImproved {
		reason = "full_score_not_improved"
	} else {
		reason = "two_stage_improvement_confirmed"
	}
	deltas := map[string]float64{}
	for caseID := range scores {
		deltas[caseID] = scores[caseID] - baseline[caseID]
	}
	return map[string]any{
		"accepted":                      len(failedTargets) == 0 && len(regressedOthers) == 0 && averageImproved,
		"reason":                        reason,
		"score":                         score,
		"baseline_score":                baselineMean,
		"full_score_delta":              score - baselineMean,
		"target_cases_improved":         len(failedTargets) == 0,
		"non_target_cases_retained":     len(regressedOthers) == 0,
		"full_average_improved":         averageImproved,
		"failed_target_case_ids":        failedTargets,
		"regressed_non_target_case_ids": regressedOthers,
		"case_score_deltas":             deltas,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// marshalDigestJSON 规范 JSON：键排序、非 ASCII 不转义、非有限值拒绝（Python json.dumps 默认参数组合）。
func marshalDigestJSON(value any) ([]byte, error) {
	var buffer strings.Builder
	encoder := json.NewEncoder(stringWriter{&buffer})
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(buffer.String(), "\n")), nil
}

// stringWriter 适配 strings.Builder 为 io.Writer。
type stringWriter struct {
	builder *strings.Builder
}

// Write 实现 io.Writer。
func (w stringWriter) Write(p []byte) (int, error) { return w.builder.Write(p) }

// expandUser 展开路径中的 ~ 前缀。
func expandUser(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// pathHasIgnoredPart 路径分段含忽略目录或编译产物后缀（Python 集合交集判断）。
func pathHasIgnoredPart(posixPath string) bool {
	for _, part := range strings.Split(posixPath, "/") {
		switch part {
		case ".git", "__pycache__", ".pytest_cache":
			return true
		}
	}
	return strings.HasSuffix(posixPath, ".pyc") || strings.HasSuffix(posixPath, ".pyo")
}

// pathEntry 取目录项（Go 1.16+ os.Stat 简装；仅用于排序后的路径过滤）。
func pathEntry(path string) os.FileInfo {
	info, err := os.Lstat(path)
	if err != nil {
		return nil
	}
	return info
}

// isRegularFile 判断普通文件（Python path.is_file()）。
func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// withinDir 判断 resolved 是否位于 dir 内（Python is_relative_to）。
func withinDir(resolved, dir string) bool {
	rel, err := filepath.Rel(dir, resolved)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// rowStatus 取用例行 status（缺失为空串）。
func rowStatus(row map[string]any) string {
	s, _ := row["status"].(string)
	return s
}

// isBoolValue 判断 bool 值。
func isBoolValue(value any) bool {
	_, ok := value.(bool)
	return ok
}

// isJSONNumber 判断数值类型（int/int64/float64）。
func isJSONNumber(value any) bool {
	switch value.(type) {
	case int, int64, float64:
		return true
	}
	return false
}

// isFiniteNumber 数值有限性。
func isFiniteNumber(value any) bool {
	f, ok := value.(float64)
	if ok {
		return !math.IsInf(f, 0) && !math.IsNaN(f)
	}
	return true
}

// numberInRange 数值范围判断。
func numberInRange(value any, low, high float64) bool {
	f := asFloat(value)
	return f >= low && f <= high
}

// asFloat 数值转 float64。
func asFloat(value any) float64 {
	switch v := value.(type) {
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case float64:
		return v
	}
	return 0
}

// numberEquals 数值等值比较（对齐 Python !=）。
func numberEquals(a, b any) bool {
	if !isJSONNumber(a) || !isJSONNumber(b) {
		return false
	}
	return asFloat(a) == asFloat(b)
}

// sameKeys 键集合相等。
func sameKeys(a map[string]float64, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if !b[key] {
			return false
		}
	}
	return true
}

// sameKeysBB bool 键集合相等。
func sameKeysBB(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if !b[key] {
			return false
		}
	}
	return true
}

// targetsSubset targets 是否为 base 键集的子集。
func targetsSubset(targets map[string]bool, base map[string]float64) bool {
	for caseID := range targets {
		if _, present := base[caseID]; !present {
			return false
		}
	}
	return true
}

// sameKeysFF float64 键集合相等。
func sameKeysFF(a, b map[string]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if _, present := b[key]; !present {
			return false
		}
	}
	return true
}
