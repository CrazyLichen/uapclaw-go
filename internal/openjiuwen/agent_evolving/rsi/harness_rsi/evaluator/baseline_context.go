package evaluator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	dataloader "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi/data_loader"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaselineJudgeContext 将基线评分与其实际交付物绑定到一个任务身份。
// Python: baseline_context.py BaselineJudgeContext（frozen dataclass）。
type BaselineJudgeContext struct {
	// CaseID 用例标识
	CaseID string
	// EvalRefPath 基线评估引用路径
	EvalRefPath string
	// ResultPath 用例结果路径
	ResultPath string
	// TaskIdentity 任务身份摘要
	TaskIdentity string
	// EvidenceDigest 证据摘要
	EvidenceDigest string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Fingerprint 指纹（来源与字节均入候选缓存身份）。
// Python: baseline_context.py BaselineJudgeContext.fingerprint。
func (c *BaselineJudgeContext) Fingerprint() string {
	return baselineDigest(map[string]any{
		"case_id": c.CaseID, "eval_ref": c.EvalRefPath,
		"task": c.TaskIdentity, "evidence": c.EvidenceDigest,
		"protocol": "baseline_reference_v1",
	})
}

// Validate 冻结证据或评分输入变化时关闭失败（fail closed）。
// Python: baseline_context.py BaselineJudgeContext.validate。
func (c *BaselineJudgeContext) Validate(caseMapping map[string]any) (map[string]any, error) {
	if evalToString(caseMapping["case_id"]) != c.CaseID {
		return nil, &EvaluationInfrastructureError{Msg: "Baseline task or rubric does not match the candidate"}
	}
	identity, err := CaseIdentity(caseMapping)
	if err != nil {
		return nil, err
	}
	if identity != c.TaskIdentity {
		return nil, &EvaluationInfrastructureError{Msg: "Baseline task or rubric does not match the candidate"}
	}
	digest, err := evidenceDigest(filepath.Dir(c.ResultPath))
	if err != nil {
		return nil, err
	}
	if digest != c.EvidenceDigest {
		return nil, &EvaluationInfrastructureError{Msg: "Frozen baseline evidence changed or is missing"}
	}
	raw, err := os.ReadFile(c.ResultPath)
	if err != nil {
		return nil, &EvaluationInfrastructureError{Msg: "Frozen baseline evidence changed or is missing"}
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, &EvaluationInfrastructureError{Msg: "Frozen baseline evidence changed or is missing"}
	}
	if evalToString(data["case_id"]) != c.CaseID {
		return nil, &EvaluationInfrastructureError{Msg: "Baseline result belongs to a different case"}
	}
	artifactsDir := filepath.Join(filepath.Dir(c.ResultPath), "artifacts")
	harvested := evalToAnySlice(evalMappingOrNil(evalMappingOrNil(data["artifacts"]))["harvested"])
	for _, rawName := range harvested {
		name := evalToString(rawName)
		evidence := filepath.Join(artifactsDir, name)
		resolved, err := filepath.EvalSymlinks(evidence)
		if err != nil {
			return nil, &EvaluationInfrastructureError{Msg: "A recorded baseline deliverable is missing"}
		}
		artifactsAbs, _ := filepath.Abs(artifactsDir)
		if !strings.HasPrefix(resolved, artifactsAbs+string(filepath.Separator)) || !fileExistsB(resolved) {
			return nil, &EvaluationInfrastructureError{Msg: "A recorded baseline deliverable is missing"}
		}
	}
	evaluation := evalMappingOrNil(data["evaluation"])
	metadata := evalMappingOrNil(evaluation["metadata"])
	if evalToString(metadata["score_mode"]) == "continuous" && evalToString(evaluation["method"]) == "llm_as_judge" {
		judgeName := filepath.Base(evalToString(metadata["judge_dir"]))
		manifestPath := filepath.Join(filepath.Dir(c.ResultPath), "judge", judgeName, "artifact_manifest.json")
		if !fileExistsB(manifestPath) {
			return nil, &EvaluationInfrastructureError{Msg: "Baseline is missing its original judged-artifact manifest"}
		}
		manifestRaw, err := os.ReadFile(manifestPath)
		if err != nil {
			return nil, &EvaluationInfrastructureError{Msg: "Baseline is missing its original judged-artifact manifest"}
		}
		var manifest map[string]any
		if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
			return nil, &EvaluationInfrastructureError{Msg: "Baseline is missing its original judged-artifact manifest"}
		}
		expected, _ := manifest["files"].(map[string]any)
		actual := map[string]any{}
		filepath.Walk(artifactsDir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil || info.IsDir() {
				return walkErr
			}
			rel, relErr := filepath.Rel(artifactsDir, path)
			if relErr != nil {
				return relErr
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			sum := sha256.Sum256(data)
			actual[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
			return nil
		})
		expectedJSON, _ := json.Marshal(expected)
		actualJSON, _ := json.Marshal(actual)
		if string(expectedJSON) != string(actualJSON) {
			return nil, &EvaluationInfrastructureError{Msg: "Baseline deliverables differ from the evidence originally judged"}
		}
	}
	score := data["score"]
	numeric := false
	var scoreValue float64
	switch v := score.(type) {
	case float64:
		numeric = true
		scoreValue = v
	case int:
		numeric = true
		scoreValue = float64(v)
	case int64:
		numeric = true
		scoreValue = float64(v)
	}
	finite := numeric && !math.IsNaN(scoreValue) && !math.IsInf(scoreValue, 0)
	inRange := finite && scoreValue >= 0 && scoreValue <= 1
	statusOK := evalToString(data["status"]) == "passed" || evalToString(data["status"]) == "failed"
	executionOK := evalToString(data["execution_status"]) != "error"
	if !inRange || !statusOK || !executionOK {
		return nil, &EvaluationInfrastructureError{Msg: "Baseline does not contain a valid correctness grade"}
	}
	return data, nil
}

// BaselineContexts 从对比 B 背后的全量评估创建冻结上下文。
// Python: baseline_context.py baseline_contexts。
func BaselineContexts(evalRefPath string, cases []map[string]any) (map[string]*BaselineJudgeContext, error) {
	source, err := filepath.Abs(evalRefPath)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		return nil, &EvaluationInfrastructureError{Msg: "Baseline evaluation is not a mapping"}
	}
	var parsed any
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return nil, &EvaluationInfrastructureError{Msg: "Baseline evaluation is not a mapping"}
	}
	payload, ok := parsed.(map[string]any)
	if !ok {
		return nil, &EvaluationInfrastructureError{Msg: "Baseline evaluation is not a mapping"}
	}
	rows := evalToAnySlice(payload["cases"])
	index := map[string]map[string]any{}
	for _, rawRow := range rows {
		row, ok := rawRow.(map[string]any)
		if !ok {
			continue
		}
		index[evalToString(row["case_id"])] = row
	}
	if len(index) != len(rows) {
		return nil, &EvaluationInfrastructureError{Msg: "Baseline contains duplicate case identities"}
	}
	result := map[string]*BaselineJudgeContext{}
	for _, caseMapping := range cases {
		caseID := evalToString(caseMapping["case_id"])
		row, ok := index[caseID]
		if !ok {
			return nil, &EvaluationInfrastructureError{Msg: "Baseline does not cover case " + caseID}
		}
		path := expandUserB(evalToString(row["result_path"]))
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(source), path)
		}
		path, _ = filepath.Abs(path)
		identity, err := CaseIdentity(caseMapping)
		if err != nil {
			return nil, err
		}
		digest, err := evidenceDigest(filepath.Dir(path))
		if err != nil {
			return nil, err
		}
		context := &BaselineJudgeContext{
			CaseID: caseID, EvalRefPath: source, ResultPath: path,
			TaskIdentity: identity, EvidenceDigest: digest,
		}
		if _, err := context.Validate(caseMapping); err != nil {
			return nil, err
		}
		result[caseID] = context
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// evidenceDigest 用例目录证据摘要（Python: _evidence_digest）。
func evidenceDigest(caseDir string) (string, error) {
	files := []string{filepath.Join(caseDir, "result.json")}
	artifactsDir := filepath.Join(caseDir, "artifacts")
	if info, err := os.Stat(artifactsDir); err == nil && info.IsDir() {
		var artifactFiles []string
		filepath.Walk(artifactsDir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !info.IsDir() {
				artifactFiles = append(artifactFiles, path)
			}
			return nil
		})
		sort.Strings(artifactFiles)
		files = append(files, artifactFiles...)
	}
	trace := filepath.Join(caseDir, "judge", "normalized_trace.json")
	if fileExistsB(trace) {
		files = append(files, trace)
	}
	identities := []any{}
	caseDirAbs, _ := filepath.Abs(caseDir)
	for _, path := range files {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			resolved = path
		}
		if !strings.HasPrefix(resolved, caseDirAbs+string(filepath.Separator)) {
			return "", &EvaluationInfrastructureError{Msg: "Baseline evidence escapes its case directory"}
		}
		if !fileExistsB(path) {
			return "", &EvaluationInfrastructureError{Msg: "Baseline evidence is missing: " + filepath.Base(path)}
		}
		rel, err := filepath.Rel(caseDir, path)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		identities = append(identities, []any{filepath.ToSlash(rel), hex.EncodeToString(sum[:])})
	}
	return baselineDigest(identities), nil
}

// baselineDigest 排序键紧凑 JSON 摘要（Python: _digest）。
func baselineDigest(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// CaseIdentity 候选与批次目录无关的任务身份（Python: task_identity.py case_identity）。
func CaseIdentity(caseMapping map[string]any) (string, error) {
	normalized, err := dataloader.NormalizeGradingCase(caseMapping)
	if err != nil {
		return "", err
	}
	taskInput, err := dataloader.TaskInput(normalized)
	if err != nil {
		return "", err
	}
	fileFingerprints, err := dataloader.FileFingerprint(normalized)
	if err != nil {
		return "", err
	}
	filesAny := map[string]any{}
	for key, value := range fileFingerprints {
		filesAny[key] = value
	}
	payload := map[string]any{
		"case_id": normalized["case_id"], "input": taskInput,
		"reference": normalized["reference"], "judge_rubrics": normalized["judge_rubrics"],
		"files": filesAny,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// fileExistsB 文件存在性。
func fileExistsB(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// expandUserB 展开用户目录。
func expandUserB(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// evalToString 任意值转字符串。
// evalAnyEqual 深度相等。
func evalAnyEqual(left, right any) bool {
	return reflect.DeepEqual(left, right)
}

func evalToString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(data)
	}
}

// evalToAnySlice any 切片提取。
func evalToAnySlice(value any) []any {
	if slice, ok := value.([]any); ok {
		return slice
	}
	return nil
}

// evalMappingOrNil 映射提取。
func evalMappingOrNil(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
