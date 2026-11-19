package singleharness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	dataloader "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/data_loader"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/improver_evolution"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// IterativeSingleHarnessRequest 单 harness 可续跑 epoch/批次优化运行的输入。
// Python: iterative.py IterativeSingleHarnessRequest（frozen dataclass）。
type IterativeSingleHarnessRequest struct {
	// DatasetFiles 数据集文件清单
	DatasetFiles []string
	// HarnessRefsPath harness 引用文件路径
	HarnessRefsPath string
	// OutputDir 输出目录
	OutputDir string
	// DatasetID 数据集标识
	DatasetID string
	// Resume 是否断点续跑
	Resume bool
	// BaselineEvalRefPath 基线评估引用路径
	BaselineEvalRefPath string
	// AutoFullBaseline 是否自动全量基线
	AutoFullBaseline bool
	// TaskID 任务标识
	TaskID string
	// MaxIteration 最大迭代轮数（nil 表示不限制）
	MaxIteration *int
}

// NewIterativeSingleHarnessRequest 构造请求并执行 __post_init__ 校验：
// 一次公开迭代是一个完整 epoch，而非 agent 单步。
func NewIterativeSingleHarnessRequest(
	datasetFiles []string, harnessRefsPath, outputDir string,
	datasetID string, resume bool, baselineEvalRefPath string,
	autoFullBaseline bool, taskID string, maxIteration *int,
) (*IterativeSingleHarnessRequest, error) {
	if maxIteration != nil {
		// Python: isinstance(bool) 排除——Go 无 bool/int 混型，无需判断
		if *maxIteration < 1 {
			return nil, errors.New("max_iteration must be an integer greater than or equal to 1")
		}
	}
	return &IterativeSingleHarnessRequest{
		DatasetFiles:        datasetFiles,
		HarnessRefsPath:     harnessRefsPath,
		OutputDir:           outputDir,
		DatasetID:           datasetID,
		Resume:              resume,
		BaselineEvalRefPath: baselineEvalRefPath,
		AutoFullBaseline:    autoFullBaseline,
		TaskID:              taskID,
		MaxIteration:        maxIteration,
	}, nil
}

// IterativeSingleHarnessResult 单 harness 迭代运行的最终产物。
// Python: iterative.py IterativeSingleHarnessResult（frozen dataclass）。
type IterativeSingleHarnessResult struct {
	// StatePath 状态文件路径
	StatePath string
	// ReportPath 报告文件路径
	ReportPath string
	// CurrentHarnessRefsPath 当前 harness 引用路径
	CurrentHarnessRefsPath string
	// BestHarnessRefsPath 最佳 harness 引用路径
	BestHarnessRefsPath string
	// PublishedHarnessRefsPath 已发布 harness 引用路径
	PublishedHarnessRefsPath string
	// BestScore 最佳分数（nil 表示无）
	BestScore *float64
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// allowedActionGroups 允许的动作组（Python: _ALLOWED_ACTION_GROUPS）
	allowedActionGroups = "prompt|skill|tool|rail|config"
	// allowedPromptSurfaces 允许的提示词面（Python: _ALLOWED_PROMPT_SURFACES）
	allowedPromptSurfaces = "prompt_section"
)

// allowedActionGroupsList 允许的动作组清单（保持 Python 列表顺序）。
func allowedActionGroupsList() []string {
	return []string{"prompt", "skill", "tool", "rail", "config"}
}

// allowedPromptSurfacesList 允许的提示词面清单（保持 Python 列表顺序）。
func allowedPromptSurfacesList() []string {
	return []string{"prompt_section"}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadCases 载入并校验本引擎接受的数据集用例。
// Python: iterative.py load_cases——注入 case_path/case_index，拒绝空与重复 case_id。
func LoadCases(datasetFiles []string) ([]map[string]any, error) {
	cases := []map[string]any{}
	for _, datasetFile := range datasetFiles {
		path, err := resolvePath(datasetFile)
		if err != nil {
			return nil, err
		}
		loaded, err := dataloader.LoadJSONCases(path)
		if err != nil {
			return nil, err
		}
		for index, singleCase := range loaded {
			caseWith := map[string]any{}
			for key, value := range singleCase {
				caseWith[key] = value
			}
			caseWith["case_path"] = path
			caseWith["case_index"] = index + 1
			cases = append(cases, caseWith)
		}
	}
	caseIDs := make([]string, 0, len(cases))
	for _, singleCase := range cases {
		caseIDs = append(caseIDs, strings.TrimSpace(toString(singleCase["case_id"])))
	}
	for _, caseID := range caseIDs {
		if caseID == "" {
			return nil, errors.New("single-harness dataset cases must have non-empty case_id values")
		}
	}
	seenCaseIDs := map[string]struct{}{}
	duplicates := map[string]struct{}{}
	for _, caseID := range caseIDs {
		if _, ok := seenCaseIDs[caseID]; ok {
			duplicates[caseID] = struct{}{}
		}
		seenCaseIDs[caseID] = struct{}{}
	}
	if len(duplicates) > 0 {
		dupList := sortedStringSet(duplicates)
		return nil, fmt.Errorf("single-harness dataset contains duplicate case ids: %v", dupList)
	}
	return cases, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// raiseIfCancelled 协作式中止运行中的编排器（任务被取消时）。
// Python: iterative.py _raise_if_cancelled——asyncio 取消 → CancelledError；
// Go 等价：ctx 取消 → 17.4f 桩返回错误（⤴️17.7 回填协作式终止后删桩）。
func raiseIfCancelled(ctx context.Context) error {
	return harnessrsi.RaiseIfCancelled(ctx)
}

// llmJudgeProtocol LLM judge 协议标识（Python: _llm_judge_protocol）。
// 内嵌延迟 import 按原位置内联：judge_protocol_identity 属 17.6 judger 范围，
// 真身未落地前该分支返回空映射不报错——偏差 D-d3（⤴️17.6 回填 judge_protocol_identity）。
func llmJudgeProtocol(evaluatorConfig any) map[string]any {
	cfg, ok := evaluatorConfig.(interface {
		GetEvaluationMethod() string
		GetScoreMode() string
		GetJudgeBaselineComparison() any
	})
	if !ok {
		return map[string]any{}
	}
	method := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(cfg.GetEvaluationMethod())), "-", "_")
	if method != "llm_as_judge" {
		return map[string]any{}
	}
	// Python: judge_protocol_identity(score_mode, judge_baseline_comparison)，
	// TypeError 时回退无参调用。Go 侧真身属 17.6，此处按"协议不可知"返回空。
	_ = cfg.GetJudgeBaselineComparison()
	var contract string
	if cfg.GetScoreMode() == "binary" {
		contract = "threshold_binary_v1"
	} else {
		contract = "continuous"
	}
	return map[string]any{"llm_judge_score_contract": contract}
}

// datasetArtifact 由请求构造数据集工件（Python: _dataset_artifact）。
// 要求全部数据集文件同目录。
func datasetArtifact(request *IterativeSingleHarnessRequest) (harnessrsi.DatasetArtifact, error) {
	files := make([]string, 0, len(request.DatasetFiles))
	for _, path := range request.DatasetFiles {
		resolved, err := resolvePath(path)
		if err != nil {
			return harnessrsi.DatasetArtifact{}, err
		}
		files = append(files, resolved)
	}
	dirs := map[string]struct{}{}
	for _, file := range files {
		dirs[filepath.Dir(file)] = struct{}{}
	}
	if len(dirs) != 1 {
		return harnessrsi.DatasetArtifact{}, errors.New("single-harness dataset files must share one directory")
	}
	datasetDir := ""
	for dir := range dirs {
		datasetDir = dir
	}
	loaded, err := LoadCases(files)
	if err != nil {
		return harnessrsi.DatasetArtifact{}, err
	}
	caseCount := len(loaded)
	return harnessrsi.DatasetArtifact{
		DatasetID:    request.DatasetID,
		DatasetDir:   datasetDir,
		DatasetFiles: files,
		Cases:        &caseCount,
	}, nil
}

// loadCasesPrivate load_cases 的兼容别名（Python: _load_cases）。
func loadCasesPrivate(datasetFiles []string) ([]map[string]any, error) {
	return LoadCases(datasetFiles)
}

// validateAndFilterPlannedBatches 拒绝缺失/重复的请求用例并忽略邻近数据集。
// Python: _validate_and_filter_planned_batches。
func validateAndFilterPlannedBatches(
	batches [][]map[string]any,
	expectedCaseIDs map[string]struct{},
) ([][]map[string]any, error) {
	filtered := [][]map[string]any{}
	seen := map[string]struct{}{}
	for _, batch := range batches {
		selected := []map[string]any{}
		for _, singleCase := range batch {
			caseID := strings.TrimSpace(toString(singleCase["case_id"]))
			if _, ok := expectedCaseIDs[caseID]; !ok {
				continue
			}
			if _, ok := seen[caseID]; ok {
				return nil, fmt.Errorf("data loader returned duplicate requested case id: %s", caseID)
			}
			seen[caseID] = struct{}{}
			selected = append(selected, singleCase)
		}
		if len(selected) > 0 {
			filtered = append(filtered, selected)
		}
	}
	missing := sortedStringSetKeyDiff(expectedCaseIDs, seen)
	if len(missing) > 0 {
		return nil, fmt.Errorf("data loader omitted requested case ids: %v", missing)
	}
	return filtered, nil
}

// validateSingleHarnessRefs 校验 harness 引用文件必须恰好一个 ref。
// Python: _validate_single_harness_refs。
func validateSingleHarnessRefs(harnessRefsPath string) error {
	path, err := resolvePath(harnessRefsPath)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var loaded any
	if err := yaml.Unmarshal(raw, &loaded); err != nil {
		return err
	}
	if loaded == nil {
		loaded = map[string]any{}
	}
	data, ok := loaded.(map[string]any)
	if !ok {
		return fmt.Errorf("harness refs must be a mapping: %s", path)
	}
	normalized := map[string]string{}
	if refs, ok := data["harness_refs"].(map[string]any); ok {
		for role, ref := range refs {
			if strings.TrimSpace(toString(ref)) != "" {
				normalized[toString(role)] = toString(ref)
			}
		}
	} else {
		excluded := map[string]struct{}{"version": {}, "source_harness_refs_path": {}}
		for role, ref := range data {
			if _, skip := excluded[role]; skip {
				continue
			}
			if refStr, ok := ref.(string); ok {
				normalized[role] = refStr
			}
		}
	}
	if len(normalized) != 1 {
		return fmt.Errorf(
			"single harness optimization requires exactly one harness ref; got %d from %s",
			len(normalized), harnessRefsPath,
		)
	}
	return nil
}

// requestFingerprint 请求指纹（Python: _request_fingerprint）。
func requestFingerprint(
	request *IterativeSingleHarnessRequest,
	sourceHarnessRefsPath string,
	siblingCandidateCount int,
	improverPolicyDigest string,
) (map[string]any, error) {
	datasetFiles := make([]string, 0, len(request.DatasetFiles))
	datasetSHA := make([]string, 0, len(request.DatasetFiles))
	for _, path := range request.DatasetFiles {
		resolved, err := resolvePath(path)
		if err != nil {
			return nil, err
		}
		datasetFiles = append(datasetFiles, resolved)
		sha, err := fileSHA256(resolved)
		if err != nil {
			return nil, err
		}
		datasetSHA = append(datasetSHA, sha)
	}
	baselineEvalRefPath := ""
	baselineEvalRefSHA := ""
	if request.BaselineEvalRefPath != "" {
		resolved, err := resolvePath(request.BaselineEvalRefPath)
		if err != nil {
			return nil, err
		}
		baselineEvalRefPath = resolved
		sha, err := fileSHA256(request.BaselineEvalRefPath)
		if err != nil {
			return nil, err
		}
		baselineEvalRefSHA = sha
	}
	return map[string]any{
		"optimization_chain_version": 20,
		"dataset_files":              datasetFiles,
		"dataset_sha256":             datasetSHA,
		"source_harness_refs_path":   sourceHarnessRefsPath,
		"baseline_eval_ref_path":     baselineEvalRefPath,
		"baseline_eval_ref_sha256":   baselineEvalRefSHA,
		"auto_full_baseline":         request.AutoFullBaseline,
		"sibling_candidate_count":    siblingCandidateCount,
		"improver_policy_digest":     improverPolicyDigest,
	}, nil
}

// fileSHA256 文件 sha256（1MiB 分块，Python: _file_sha256）。
func fileSHA256(path string) (string, error) {
	resolved, err := resolvePath(path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

var (
	// skKeyPattern API key 脱敏（Python: _safe_candidate_error 的三条正则）
	skKeyPattern      = regexp.MustCompile(`(?i)\bsk-[A-Za-z0-9._-]{8,}`)
	apiKeyPattern     = regexp.MustCompile(`(?i)(api[_-]?key\s*[=:]\s*)\S+`)
	authBearerPattern = regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)\S+`)
)

// safeCandidateError 候选失败信息脱敏截断（Python: _safe_candidate_error）。
func safeCandidateError(err error) map[string]string {
	message := strings.TrimSpace(err.Error())
	message = skKeyPattern.ReplaceAllString(message, "[redacted]")
	message = apiKeyPattern.ReplaceAllString(message, "${1}[redacted]")
	message = authBearerPattern.ReplaceAllString(message, "${1}[redacted]")
	if len(message) > 2_000 {
		message = message[:2_000]
	}
	return map[string]string{
		"error_type": errorTypeName(err),
		"message":    message,
	}
}

// resultFromState 从状态恢复结果（Python: _result_from_state）。
func resultFromState(
	state map[string]any,
	statePath string,
	reportPath string,
) IterativeSingleHarnessResult {
	return IterativeSingleHarnessResult{
		StatePath:                statePath,
		ReportPath:               reportPath,
		CurrentHarnessRefsPath:   toString(state["current_harness_refs_path"]),
		BestHarnessRefsPath:      toString(state["best_harness_refs_path"]),
		PublishedHarnessRefsPath: toString(state["published_harness_refs_path"]),
		BestScore:                numberOf(state["best_score"]),
	}
}

// priorEvalRefsFromState 从状态提取既有评估引用（Python: _prior_eval_refs_from_state）。
func priorEvalRefsFromState(state map[string]any) []string {
	refs := []string{toString(state["baseline_eval_ref_path"])}
	checkpoints, _ := state["epoch_checkpoints"].([]any)
	for _, item := range checkpoints {
		if checkpoint, ok := item.(map[string]any); ok {
			refs = append(refs, toString(checkpoint["eval_ref_path"]))
		}
	}
	filtered := []string{}
	for _, ref := range refs {
		if ref != "" {
			filtered = append(filtered, ref)
		}
	}
	return filtered
}

// loadOrCreateState 加载或初始化状态（Python: _load_or_create_state）。
func loadOrCreateState(
	statePath string,
	resume bool,
	fingerprint map[string]any,
	sourceHarnessRefsPath string,
	dataset harnessrsi.DatasetArtifact,
) (map[string]any, error) {
	if resume && fileExists(statePath) {
		state, err := readYAMLFile(statePath)
		if err != nil {
			return nil, err
		}
		storedFingerprint, _ := state["fingerprint"].(map[string]any)
		if !resumeFingerprintMatches(storedFingerprint, fingerprint) {
			return nil, errors.New("resume inputs do not match single-harness state")
		}
		if !anyEqual(storedFingerprint, fingerprint) {
			state["fingerprint"] = fingerprint
		}
		setDefaultString(state, "working_harness_refs_path",
			defaultString(state["current_harness_refs_path"], sourceHarnessRefsPath))
		setDefault(state, "retained_case_ids", []any{})
		setDefaultString(state, "best_eval_ref_path", "")
		setDefaultString(state, "baseline_eval_ref_path", "")
		setDefault(state, "baseline_score", nil)
		setDefaultString(state, "publication_status", "not_published")
		setDefault(state, "optimization_journal", []any{})
		setDefault(state, "lever_scoreboard", map[string]any{})
		setDefault(state, "improvement_instances", map[string]any{})
		setDefaultString(state, "candidate_feedback_ledger_path", "")
		return state, nil
	}
	datasetDict, err := yamlToAnyMap(dataset)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"version":                        7,
		"status":                         "running",
		"mode":                           "single_harness_benchmark",
		"fingerprint":                    fingerprint,
		"dataset":                        datasetDict,
		"allowed_action_groups":          toAnySlice(allowedActionGroupsList()),
		"allowed_prompt_surfaces":        toAnySlice(allowedPromptSurfacesList()),
		"source_harness_refs_path":       sourceHarnessRefsPath,
		"current_harness_refs_path":      sourceHarnessRefsPath,
		"working_harness_refs_path":      sourceHarnessRefsPath,
		"best_harness_refs_path":         sourceHarnessRefsPath,
		"published_harness_refs_path":    "",
		"publication_status":             "not_published",
		"best_score":                     nil,
		"best_eval_ref_path":             "",
		"baseline_eval_ref_path":         "",
		"baseline_score":                 nil,
		"retained_case_ids":              []any{},
		"batch_plan_paths":               map[string]any{},
		"completed_batches":              map[string]any{},
		"candidate_gates":                []any{},
		"improvement_instances":          map[string]any{},
		"epoch_checkpoints":              []any{},
		"optimization_journal":           []any{},
		"lever_scoreboard":               map[string]any{},
		"candidate_feedback_ledger_path": "",
	}, nil
}

// initializeFrozenBaseline 播种全局对比状态（不改每批执行）。
// Python: _initialize_frozen_baseline。
func initializeFrozenBaseline(
	state map[string]any,
	baselineEvalRefPath string,
	sourceHarnessRefsPath string,
	expectedCaseIDs map[string]struct{},
) error {
	if baselineEvalRefPath == "" {
		return nil
	}

	baselinePath, err := resolvePath(baselineEvalRefPath)
	if err != nil {
		return err
	}
	complete, err := evalRefComplete(baselinePath)
	if err != nil {
		return err
	}
	if !complete {
		return fmt.Errorf("baseline eval ref is incomplete: %s", baselinePath)
	}
	baseline, err := readYAMLFile(baselinePath)
	if err != nil {
		return err
	}

	rawRefsPath := strings.TrimSpace(toString(baseline["harness_refs_path"]))
	if rawRefsPath == "" {
		return fmt.Errorf("baseline eval ref has no harness_refs_path: %s", baselinePath)
	}
	recordedRefsPath := expandUser(rawRefsPath)
	if !filepath.IsAbs(recordedRefsPath) {
		recordedRefsPath = filepath.Join(filepath.Dir(baselinePath), recordedRefsPath)
	}
	sourceResolved, err := resolvePath(sourceHarnessRefsPath)
	if err != nil {
		return err
	}
	if filepath.Clean(recordedRefsPath) != sourceResolved {
		return errors.New("baseline eval ref was not produced by the source Harness")
	}

	rawCases, ok := baseline["cases"].([]any)
	if !ok {
		return fmt.Errorf("baseline eval ref has no cases: %s", baselinePath)
	}
	baselineCaseIDs := []string{}
	for _, rawCase := range rawCases {
		if caseMap, ok := rawCase.(map[string]any); ok {
			baselineCaseIDs = append(baselineCaseIDs, toString(caseMap["case_id"]))
		}
	}
	seenBaseline := map[string]struct{}{}
	for _, caseID := range baselineCaseIDs {
		if caseID == "" {
			return fmt.Errorf("baseline eval ref contains missing or duplicate case ids: %s", baselinePath)
		}
		if _, dup := seenBaseline[caseID]; dup {
			return fmt.Errorf("baseline eval ref contains missing or duplicate case ids: %s", baselinePath)
		}
		seenBaseline[caseID] = struct{}{}
	}
	if !stringSetEqual(seenBaseline, expectedCaseIDs) {
		missing := sortedStringSetKeyDiff(expectedCaseIDs, seenBaseline)
		extra := sortedStringSetKeyDiff(seenBaseline, expectedCaseIDs)
		return fmt.Errorf(
			"baseline eval ref does not match the frozen dataset: missing=%v, extra=%v", missing, extra,
		)
	}

	existingBaseline := toString(state["baseline_eval_ref_path"])
	if existingBaseline != "" {
		existingResolved, err := resolvePath(existingBaseline)
		if err != nil {
			return err
		}
		if existingResolved != baselinePath {
			return errors.New("resume baseline eval ref does not match single-harness state")
		}
	}
	state["baseline_eval_ref_path"] = baselinePath
	baselineScore, err := evalScore(baselinePath)
	if err != nil {
		return err
	}
	state["baseline_score"] = baselineScore
	if toString(state["best_eval_ref_path"]) == "" {
		state["best_eval_ref_path"] = baselinePath
		state["best_score"] = baselineScore
		passing, err := passingCaseIDs(baselinePath)
		if err != nil {
			return err
		}
		state["retained_case_ids"] = sortedStringSetToAnySlice(passing)
	}
	return nil
}

// resumeFingerprintMatches 续跑指纹匹配（含 12→13 遗留迁移分支，逐字复刻）。
// Python: _resume_fingerprint_matches——注意 Python 侧 _request_fingerprint 写
// version 20，此处 12/13 分支仅对旧状态生效（上游原样，勿"修正"）。
func resumeFingerprintMatches(stored, requested map[string]any) bool {
	if anyEqual(stored, requested) {
		return true
	}
	if stored == nil {
		return false
	}
	if !numberEqual(stored["optimization_chain_version"], 12) {
		return false
	}
	if !numberEqual(requested["optimization_chain_version"], 13) {
		return false
	}
	requestedDigest, _ := requested["improver_policy_digest"].(string)
	if requestedDigest != improver_evolution.DefaultImproverPolicy().CanonicalDigest() {
		return false
	}
	migrated := map[string]any{}
	for key, value := range stored {
		migrated[key] = value
	}
	migrated["optimization_chain_version"] = 13
	migrated["improver_policy_digest"] = requested["improver_policy_digest"]
	return anyEqual(migrated, requested)
}

// refreshOptimizationExperience 从已判定的实验重建跨轮日志与 lever 记分板。
// Python: _refresh_optimization_experience。
func refreshOptimizationExperience(
	state map[string]any,
	outputDir string,
) error {
	journal := []any{}
	gates, _ := state["candidate_gates"].([]any)
	for _, rawGate := range gates {
		gate, ok := rawGate.(map[string]any)
		if !ok {
			continue
		}
		if toString(gate["status"]) == "inconclusive" ||
			toString(gate["failure_class"]) == "infrastructure_or_evidence_failure" {
			continue
		}
		capabilities := []map[string]any{}
		rawCapabilities, _ := gate["capabilities"].([]any)
		for _, rawCapability := range rawCapabilities {
			if capability, ok := rawCapability.(map[string]any); ok {
				capabilities = append(capabilities, capability)
			}
		}
		if len(capabilities) == 0 {
			capabilities = []map[string]any{{}}
		}
		for _, capability := range capabilities {
			decision := map[string]any{}
			if rawDecision, ok := capability["lever_decision"].(map[string]any); ok {
				decision = rawDecision
			}
			epochCheckpoint, _ := gate["epoch_checkpoint_outcome"].(map[string]any)
			if epochCheckpoint == nil {
				epochCheckpoint = map[string]any{}
			}
			journal = append(journal, map[string]any{
				"experiment_id": fmt.Sprintf(
					"e%03d-b%03d-%s",
					toInt(gate["epoch"]), toInt(gate["batch_index"]),
					defaultString(capability["action_id"], "no_action"),
				),
				"epoch":                               toInt(gate["epoch"]),
				"batch_index":                         toInt(gate["batch_index"]),
				"action_id":                           toString(capability["action_id"]),
				"surface":                             toString(capability["action_group"]),
				"lever":                               defaultString(decision["selected_lever"], "unresolved"),
				"lever_decision":                      decision,
				"target_case_ids":                     toAnySlice(gate["target_case_ids"]),
				"source_target_score":                 numberOfValue(gate["source_target_score"]),
				"candidate_target_score":              numberOfValue(gate["candidate_target_score"]),
				"target_score_delta":                  numberOfValue(gate["target_score_delta"]),
				"non_target_score_delta":              numberOfValue(gate["non_target_score_delta"]),
				"status":                              toString(gate["status"]),
				"reason":                              toString(gate["reason"]),
				"failure_class":                       toString(gate["failure_class"]),
				"outcome":                             experimentOutcome(gate),
				"source_eval_ref_path":                toString(gate["paired_source_eval_ref_path"]),
				"candidate_eval_ref_path":             toString(gate["candidate_eval_ref_path"]),
				"candidate_failure_analysis_ref_path": toString(gate["candidate_failure_analysis_ref_path"]),
				"verifier_deltas_by_case":             mappingOrNil(gate["verifier_deltas_by_case"]),
				"candidate_patch_excerpts_by_case":    mappingOrNil(gate["candidate_patch_excerpts_by_case"]),
				"candidate_failure_diagnoses":         mappingOrNil(gate["candidate_failure_diagnoses"]),
				"candidate_behavior_by_case":          mappingOrNil(gate["candidate_behavior_by_case"]),
				"epoch_checkpoint": map[string]any{
					"status":                 toString(epochCheckpoint["status"]),
					"eval_ref_path":          toString(epochCheckpoint["eval_ref_path"]),
					"failed_target_case_ids": toAnySlice(epochCheckpoint["failed_target_case_ids"]),
				},
			})
		}
	}

	scoreboard := map[string]any{}
	for _, rawRecord := range journal {
		record := rawRecord.(map[string]any)
		lever := defaultString(record["lever"], "unresolved")
		entry, ok := scoreboard[lever].(map[string]any)
		if !ok {
			entry = map[string]any{
				"attempts":                  0,
				"accepted":                  0,
				"rejected":                  0,
				"target_improvements":       0,
				"partial_contract_progress": 0,
				"regressions":               0,
				"total_target_score_delta":  0.0,
				"surfaces":                  map[string]any{},
				"last_reason":               "",
			}
			scoreboard[lever] = entry
		}
		entry["attempts"] = toInt(entry["attempts"]) + 1
		accepted := record["status"] == "accepted"
		if accepted {
			entry["accepted"] = toInt(entry["accepted"]) + 1
		} else {
			entry["rejected"] = toInt(entry["rejected"]) + 1
		}
		targetDelta := 0.0
		if delta := numberOf(record["target_score_delta"]); delta != nil {
			targetDelta = *delta
		}
		entry["total_target_score_delta"] = entry["total_target_score_delta"].(float64) + targetDelta
		if targetDelta > 0 {
			entry["target_improvements"] = toInt(entry["target_improvements"]) + 1
		}
		if record["outcome"] == "partial_contract_progress" {
			entry["partial_contract_progress"] = toInt(entry["partial_contract_progress"]) + 1
		}
		if record["outcome"] == "regressed" {
			entry["regressions"] = toInt(entry["regressions"]) + 1
		}
		surface := defaultString(record["surface"], "unknown")
		surfaces := entry["surfaces"].(map[string]any)
		surfaces[surface] = toInt(surfaces[surface]) + 1
		entry["last_reason"] = toString(record["reason"])
	}
	for _, rawEntry := range scoreboard {
		entry := rawEntry.(map[string]any)
		attempts := toInt(entry["attempts"])
		total := entry["total_target_score_delta"].(float64)
		delete(entry, "total_target_score_delta")
		if attempts > 0 {
			entry["average_target_score_delta"] = total / float64(attempts)
		} else {
			entry["average_target_score_delta"] = 0.0
		}
	}

	state["optimization_journal"] = journal
	state["lever_scoreboard"] = scoreboard
	journalPath := filepath.Join(outputDir, "optimization_journal.yaml")
	scoreboardPath := filepath.Join(outputDir, "lever_scoreboard.yaml")
	if err := writeYAMLAtomic(journalPath, map[string]any{"version": 1, "experiments": journal}); err != nil {
		return err
	}
	if err := writeYAMLAtomic(scoreboardPath, map[string]any{"version": 1, "levers": scoreboard}); err != nil {
		return err
	}
	state["optimization_journal_path"] = journalPath
	state["lever_scoreboard_path"] = scoreboardPath
	return nil
}

// experimentOutcome 实验结果分类（Python: _experiment_outcome）。
func experimentOutcome(gate map[string]any) string {
	if anySliceNonEmpty(gate["regressed_target_case_ids"]) ||
		anySliceNonEmpty(gate["regressed_non_target_case_ids"]) {
		return "regressed"
	}
	targetDelta := 0.0
	if delta := numberOf(gate["target_score_delta"]); delta != nil {
		targetDelta = *delta
	}
	if targetDelta > 0 {
		if gate["status"] == "accepted" {
			return "flipped"
		}
		return "improved_but_rejected"
	}
	if verifierDeltas, ok := gate["verifier_deltas_by_case"].(map[string]any); ok {
		for _, rawDelta := range verifierDeltas {
			delta, ok := rawDelta.(map[string]any)
			if !ok {
				continue
			}
			if truthy(delta["newly_passed_fail_to_pass"]) {
				return "partial_contract_progress"
			}
		}
	}
	return "still_failed"
}

// candidateCapabilities 从成员优化信息提取能力清单（Python: _candidate_capabilities）。
func candidateCapabilities(memberInfo map[string]any) ([]map[string]any, error) {
	plan, err := readYAMLFile(toString(memberInfo["plan_path"]))
	if err != nil {
		return nil, err
	}
	issueCaseIDs, targetCaseIDsByRole, err := targetCaseIDs(plan, memberInfo)
	if err != nil {
		return nil, err
	}
	capabilities := []map[string]any{}
	rawActions, _ := plan["actions"].([]any)
	for _, rawAction := range rawActions {
		action, ok := rawAction.(map[string]any)
		if !ok {
			continue
		}
		targetPath := toString(action["target_path"])
		constraints := map[string]any{}
		if rawConstraints, ok := action["constraints"].(map[string]any); ok {
			constraints = rawConstraints
		}
		optimizationContracts := []map[string]any{}
		rawContracts, _ := constraints["optimization_contracts"].([]any)
		for _, rawContract := range rawContracts {
			if contract, ok := rawContract.(map[string]any); ok {
				optimizationContracts = append(optimizationContracts, contract)
			}
		}
		phases := map[string]struct{}{}
		for _, contract := range optimizationContracts {
			if decisionContract, ok := contract["decision_contract"].(map[string]any); ok {
				phases[toString(decisionContract["activation_phase"])] = struct{}{}
			} else {
				phases[""] = struct{}{}
			}
		}
		activationPhase := ""
		if len(phases) == 1 {
			for phase := range phases {
				activationPhase = phase
			}
		}
		decisionContracts := []any{}
		for _, contract := range optimizationContracts {
			if decisionContract, ok := contract["decision_contract"].(map[string]any); ok {
				decisionContracts = append(decisionContracts, decisionContract)
			} else {
				decisionContracts = append(decisionContracts, map[string]any{})
			}
		}
		attributedIssueIDs := []string{}
		for _, item := range toAnySlice(action["attributed_issue_ids"]) {
			attributedIssueIDs = append(attributedIssueIDs, toString(item))
		}
		analyzerPredictions := []string{}
		for _, item := range toAnySlice(constraints["analyzer_counterfactual_predictions"]) {
			analyzerPredictions = append(analyzerPredictions, toString(item))
		}
		leverDecision := map[string]any{}
		if rawLever, ok := constraints["lever_decision"].(map[string]any); ok {
			leverDecision = rawLever
		}
		hypothesisIDs := []string{}
		for _, contract := range optimizationContracts {
			if id := toString(contract["hypothesis_id"]); id != "" {
				hypothesisIDs = append(hypothesisIDs, id)
			}
		}
		sourceHypothesisIDs := []string{}
		for _, item := range toAnySlice(constraints["source_causal_hypothesis_ids"]) {
			sourceHypothesisIDs = append(sourceHypothesisIDs, toString(item))
		}
		sourceSemanticIDs := []string{}
		for _, item := range toAnySlice(constraints["source_causal_hypothesis_semantic_ids"]) {
			sourceSemanticIDs = append(sourceSemanticIDs, toString(item))
		}
		contractSHA := []string{}
		for _, contract := range optimizationContracts {
			if sha := toString(contract["content_sha256"]); sha != "" {
				contractSHA = append(contractSHA, sha)
			}
		}
		actionTargetIDs := sortedStringSet(
			actionTargetCaseIDs(action, issueCaseIDs, targetCaseIDsByRole),
		)
		capabilities = append(capabilities, map[string]any{
			"action_id":                             toString(action["action_id"]),
			"role":                                  toString(action["role"]),
			"action_group":                          toString(action["action_group"]),
			"operation":                             toString(action["operation"]),
			"description":                           toString(action["description"]),
			"rationale":                             toString(action["rationale"]),
			"intervention":                          toString(action["intervention"]),
			"target_path":                           targetPath,
			"runtime_name":                          capabilityRuntimeName(toString(action["action_group"]), targetPath),
			"expected_effect":                       toString(action["expected_effect"]),
			"activation_phase":                      activationPhase,
			"decision_contracts":                    decisionContracts,
			"attributed_issue_ids":                  attributedIssueIDs,
			"analyzer_counterfactual_predictions":   analyzerPredictions,
			"lever_decision":                        leverDecision,
			"optimization_hypothesis_ids":           hypothesisIDs,
			"source_causal_hypothesis_id":           toString(constraints["source_causal_hypothesis_id"]),
			"source_causal_hypothesis_ids":          sourceHypothesisIDs,
			"source_causal_hypothesis_semantic_id":  toString(constraints["source_causal_hypothesis_semantic_id"]),
			"source_causal_hypothesis_semantic_ids": sourceSemanticIDs,
			"optimization_contract_sha256":          contractSHA,
			"target_case_ids":                       actionTargetIDs,
		})
	}
	return capabilities, nil
}

// memberCompositionMode 候选包组合契约（Python: _member_composition_mode）。
func memberCompositionMode(memberInfo map[string]any) string {
	metadata := map[string]any{}
	if raw, ok := memberInfo["metadata"].(map[string]any); ok {
		metadata = raw
	}
	return defaultString(
		memberInfo["composition_mode"],
		toString(metadata["composition_mode"]),
	)
}

// targetCaseIDs 计算 issue→用例与 role→用例映射（Python: _target_case_ids）。
func targetCaseIDs(
	plan map[string]any,
	memberInfo map[string]any,
) (map[string]map[string]struct{}, map[string]map[string]struct{}, error) {
	metadata := map[string]any{}
	if raw, ok := memberInfo["metadata"].(map[string]any); ok {
		metadata = raw
	}
	issueCaseIDs, err := analysisIssueCaseIDs(toString(metadata["analysis_result_path"]))
	if err != nil {
		return nil, nil, err
	}

	result := map[string]map[string]struct{}{}
	rawTargets, _ := plan["targets"].([]any)
	for _, rawTarget := range rawTargets {
		target, ok := rawTarget.(map[string]any)
		if !ok {
			continue
		}
		role := defaultString(target["role"], toString(target["member_name"]))
		if role == "" {
			continue
		}
		roleCaseIDs, ok := result[role]
		if !ok {
			roleCaseIDs = map[string]struct{}{}
			result[role] = roleCaseIDs
		}
		for _, issueID := range toAnySlice(target["attributed_issue_ids"]) {
			if cases, ok := issueCaseIDs[toString(issueID)]; ok {
				for caseID := range cases {
					roleCaseIDs[caseID] = struct{}{}
				}
			}
		}
		for _, rawEvidence := range toAnySlice(target["evidence_refs"]) {
			evidence, ok := rawEvidence.(map[string]any)
			if !ok {
				continue
			}
			if caseID := toString(evidence["case_id"]); caseID != "" {
				roleCaseIDs[caseID] = struct{}{}
			}
			if issueID := toString(evidence["issue_id"]); issueID != "" {
				if cases, ok := issueCaseIDs[issueID]; ok {
					for caseID := range cases {
						roleCaseIDs[caseID] = struct{}{}
					}
				}
			}
		}
	}
	return issueCaseIDs, result, nil
}

// analysisIssueCaseIDs 解析诊断件的 issue→用例映射（Python: _analysis_issue_case_ids）。
func analysisIssueCaseIDs(analysisRef string) (map[string]map[string]struct{}, error) {
	analysis, err := readYAMLFile(analysisRef)
	if err != nil {
		return nil, err
	}
	issueCaseIDs := map[string]map[string]struct{}{}
	rawIssues, _ := analysis["issues"].([]any)
	for _, rawIssue := range rawIssues {
		issue, ok := rawIssue.(map[string]any)
		if !ok {
			continue
		}
		issueID := toString(issue["issue_id"])
		if issueID == "" {
			continue
		}
		caseIDs := map[string]struct{}{}
		for _, rawCaseID := range toAnySlice(issue["affected_cases"]) {
			if caseID := toString(rawCaseID); caseID != "" {
				caseIDs[caseID] = struct{}{}
			}
		}
		for _, rawEvidence := range toAnySlice(issue["evidence"]) {
			if evidence, ok := rawEvidence.(map[string]any); ok {
				if caseID := toString(evidence["case_id"]); caseID != "" {
					caseIDs[caseID] = struct{}{}
				}
			}
		}
		issueCaseIDs[issueID] = caseIDs
	}
	return issueCaseIDs, nil
}

// mustPreserveBudgetForSiblings 深挖失败路线前保留兄弟候选可达（Python: _must_preserve_budget_for_siblings）。
func mustPreserveBudgetForSiblings(
	remainingAttemptBudget *int,
	remainingSiblingCount int,
) bool {
	return remainingAttemptBudget != nil &&
		remainingSiblingCount > 0 &&
		*remainingAttemptBudget <= remainingSiblingCount
}

// analysisIssueSignatures 稳定语义键，防止残留循环重试同一 issue（Python: _analysis_issue_signatures）。
func analysisIssueSignatures(analysisRef string) (map[string]string, error) {
	analysis, err := readYAMLFile(analysisRef)
	if err != nil {
		return nil, err
	}
	signatures := map[string]string{}
	rawIssues, _ := analysis["issues"].([]any)
	for _, rawIssue := range rawIssues {
		issue, ok := rawIssue.(map[string]any)
		if !ok {
			continue
		}
		issueID := toString(issue["issue_id"])
		if issueID == "" {
			continue
		}
		metadata := map[string]any{}
		if raw, ok := issue["metadata"].(map[string]any); ok {
			metadata = raw
		}
		attribution := map[string]any{}
		if raw, ok := metadata["attribution"].(map[string]any); ok {
			attribution = raw
		}
		targetRef := attribution["target_ref"]
		if targetRef == nil || toString(targetRef) == "" {
			targetRef = issue["target_ref"]
		}
		affectedCases := []string{}
		for _, rawCaseID := range toAnySlice(issue["affected_cases"]) {
			if caseID := toString(rawCaseID); caseID != "" {
				affectedCases = append(affectedCases, caseID)
			}
		}
		sort.Strings(affectedCases)
		semanticPayload := map[string]any{
			"category":       strings.ToLower(strings.TrimSpace(toString(issue["category"]))),
			"summary":        strings.Join(strings.Fields(strings.ToLower(toString(issue["summary"]))), " "),
			"recommendation": strings.Join(strings.Fields(strings.ToLower(toString(issue["recommendation"]))), " "),
			"failure_mode":   strings.Join(strings.Fields(strings.ToLower(toString(issue["failure_mode"]))), " "),
			"target_ref":     strings.ToLower(strings.TrimSpace(toString(targetRef))),
			"affected_cases": affectedCases,
		}
		hasSemantic := false
		for key, value := range semanticPayload {
			if key != "affected_cases" && toString(value) != "" {
				hasSemantic = true
				break
			}
		}
		if !hasSemantic {
			signatures[issueID] = issueID
			continue
		}
		encoded := canonicalJSONString(semanticPayload)
		digest := sha256.Sum256([]byte(encoded))
		signatures[issueID] = hex.EncodeToString(digest[:])
	}
	return signatures, nil
}

// casesWithIDs 按标识保留源顺序选取活跃用例（Python: _cases_with_ids）。
func casesWithIDs(cases []map[string]any, caseIDs map[string]struct{}) []map[string]any {
	selected := []map[string]any{}
	for _, singleCase := range cases {
		if _, ok := caseIDs[strings.TrimSpace(toString(singleCase["case_id"]))]; ok {
			selected = append(selected, singleCase)
		}
	}
	return selected
}

// syncRetainedCaseIDs 以一次新评估观察到的用例整体替换保留状态（Python: _sync_retained_case_ids）。
// retained 由调用方持 map 传址修改（Python set 语义）。
func syncRetainedCaseIDs(
	retained map[string]struct{},
	evaluatedCaseScores map[string]float64,
	passingCaseIDsSet map[string]struct{},
) {
	for caseID := range evaluatedCaseScores {
		delete(retained, caseID)
	}
	if passingCaseIDsSet == nil {
		passingCaseIDsSet = map[string]struct{}{}
		for caseID, score := range evaluatedCaseScores {
			if score >= 1.0 {
				passingCaseIDsSet[caseID] = struct{}{}
			}
		}
	}
	for caseID := range passingCaseIDsSet {
		if _, evaluated := evaluatedCaseScores[caseID]; evaluated {
			retained[caseID] = struct{}{}
		}
	}
}

// rejectedCapabilityHistory 收集被拒候选能力历史（Python: _rejected_capability_history）。
func rejectedCapabilityHistory(candidateGates []map[string]any) []map[string]any {
	rejected := []map[string]any{}
	copiedFields := []string{
		"target_confirmation",
		"epoch_checkpoint_outcome",
		"verifier_deltas_by_case",
		"candidate_failure_diagnoses",
		"candidate_behavior_by_case",
	}
	for _, gate := range candidateGates {
		if toString(gate["status"]) != "rejected" {
			continue
		}
		for _, rawCapability := range toAnySlice(gate["capabilities"]) {
			capability, ok := rawCapability.(map[string]any)
			if !ok {
				continue
			}
			item := map[string]any{}
			for key, value := range capability {
				item[key] = value
			}
			item["rejection_reason"] = toString(gate["reason"])
			item["failure_class"] = toString(gate["failure_class"])
			for _, field := range copiedFields {
				if value, ok := gate[field].(map[string]any); ok {
					item[field] = value
				} else {
					item[field] = map[string]any{}
				}
			}
			rejected = append(rejected, item)
		}
	}
	return rejected
}

// capabilityActionIDs 收集门内能力动作标识（Python: _capability_action_ids）。
func capabilityActionIDs(gates []map[string]any) []string {
	actionIDs := map[string]struct{}{}
	for _, gate := range gates {
		for _, rawCapability := range toAnySlice(gate["capabilities"]) {
			capability, ok := rawCapability.(map[string]any)
			if !ok {
				continue
			}
			if actionID := toString(capability["action_id"]); actionID != "" {
				actionIDs[actionID] = struct{}{}
			}
		}
	}
	return sortedStringSet(actionIDs)
}

// expectedRuntimeNames 期望运行时名（Python: _expected_runtime_names）。
func expectedRuntimeNames(capabilities []map[string]any, actionGroup string) []string {
	names := map[string]struct{}{}
	for _, capability := range capabilities {
		if toString(capability["action_group"]) != actionGroup {
			continue
		}
		operation := toString(capability["operation"])
		if operation != "add" && operation != "modify" {
			continue
		}
		if runtimeName := toString(capability["runtime_name"]); runtimeName != "" {
			names[runtimeName] = struct{}{}
		}
	}
	return sortedStringSet(names)
}

// compactAnalysisDiagnoses 载入候选分析器的因果结果（Python: _compact_analysis_diagnoses）。
func compactAnalysisDiagnoses(analysisRef string) (map[string][]map[string]any, error) {
	analysis, err := readYAMLFile(analysisRef)
	if err != nil {
		return nil, err
	}
	metadata := map[string]any{}
	if raw, ok := analysis["metadata"].(map[string]any); ok {
		metadata = raw
	}
	diagnosesPath := expandUser(toString(metadata["per_case_diagnoses_path"]))
	if !fileExists(diagnosesPath) {
		return map[string][]map[string]any{}, nil
	}
	raw, err := os.ReadFile(diagnosesPath)
	if err != nil {
		return map[string][]map[string]any{}, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return map[string][]map[string]any{}, nil
	}
	compact := map[string][]map[string]any{}
	diagnosisFields := []string{
		"summary",
		"root_cause",
		"critical_mistake",
		"general_mechanism",
		"recommendation",
		"decision_contract",
		"target_ref",
		"confidence",
		"diagnosis_status",
		"analysis_failed",
	}
	for _, rawDiagnosis := range toAnySlice(payload["per_case_diagnoses"]) {
		diagnosis, ok := rawDiagnosis.(map[string]any)
		if !ok {
			continue
		}
		caseID := toString(diagnosis["case_id"])
		if caseID == "" {
			continue
		}
		entry := map[string]any{}
		for _, field := range diagnosisFields {
			if value, ok := diagnosis[field]; ok {
				entry[field] = value
			}
		}
		compact[caseID] = append(compact[caseID], entry)
	}
	return compact, nil
}

// uniformAssessmentValue 评估一致性取值（Python: _uniform_assessment_value）。
func uniformAssessmentValue(assessments []map[string]any, key string) string {
	values := map[string]struct{}{}
	for _, item := range assessments {
		value := strings.ToLower(strings.TrimSpace(toString(item[key])))
		if value != "" {
			values[value] = struct{}{}
		}
	}
	if len(values) == 1 {
		for value := range values {
			return value
		}
	}
	if len(values) > 1 {
		return "mixed"
	}
	return "unknown"
}

// causalCandidateFailureClassification 按实际生效动作路由被拒候选（Python: _causal_candidate_failure_classification）。
func causalCandidateFailureClassification(activationByCase map[string]map[string]any) string {
	states := map[string]struct{}{}
	behavioral := []map[string]any{}
	for _, item := range activationByCase {
		if item == nil {
			continue
		}
		states[strings.ToLower(strings.TrimSpace(toString(item["state"])))] = struct{}{}
		if behaviorActivation, ok := item["behavior_activation"].(map[string]any); ok {
			behavioral = append(behavioral, behaviorActivation)
		}
	}
	behaviorValues := map[string]struct{}{}
	outcomeValues := map[string]struct{}{}
	for _, item := range behavioral {
		if value := strings.ToLower(strings.TrimSpace(toString(item["predicted_behavior_occurred"]))); value != "" {
			behaviorValues[value] = struct{}{}
		}
		if value := strings.ToLower(strings.TrimSpace(toString(item["predicted_outcome_occurred"]))); value != "" {
			outcomeValues[value] = struct{}{}
		}
	}
	if _, ok := states["not_triggered"]; ok {
		return "intervention_not_activated"
	}
	if _, ok := behaviorValues["no"]; ok {
		return "intervention_not_activated"
	}
	if setEqual(behaviorValues, map[string]struct{}{"yes": {}}) {
		if _, ok := outcomeValues["no"]; ok {
			return "action_occurred_but_hypothesis_refuted"
		}
	}
	if _, ok := outcomeValues["no"]; ok {
		return "outcome_not_improved"
	}
	return ""
}

// priorCandidateFeedback 选取近期配对候选证据（Python: _prior_candidate_feedback）。
func priorCandidateFeedback(
	state map[string]any,
	cases []map[string]any,
) map[string]any {
	activeCaseIDs := map[string]struct{}{}
	for _, singleCase := range cases {
		if caseID := toString(singleCase["case_id"]); caseID != "" {
			activeCaseIDs[caseID] = struct{}{}
		}
	}
	byCase := map[string][]map[string]any{}
	for caseID := range activeCaseIDs {
		byCase[caseID] = []map[string]any{}
	}
	for _, rawRecord := range toAnySlice(state["optimization_journal"]) {
		record, ok := rawRecord.(map[string]any)
		if !ok {
			continue
		}
		deltas := mappingOrNil(record["verifier_deltas_by_case"])
		diagnoses := mappingOrNil(record["candidate_failure_diagnoses"])
		patches := mappingOrNil(record["candidate_patch_excerpts_by_case"])
		behaviorByCase := mappingOrNil(record["candidate_behavior_by_case"])
		for caseID := range activeCaseIDs {
			if _, present := deltas[caseID]; !present {
				continue
			}
			caseDiagnoses := []map[string]any{}
			switch diagnosisValue := diagnoses[caseID].(type) {
			case []any:
				for _, item := range diagnosisValue {
					if itemMap, ok := item.(map[string]any); ok {
						caseDiagnoses = append(caseDiagnoses, itemMap)
					}
				}
			case map[string]any:
				caseDiagnoses = append(caseDiagnoses, diagnosisValue)
			}
			firstDiagnosis := map[string]any{}
			if len(caseDiagnoses) > 0 {
				firstDiagnosis = caseDiagnoses[0]
			}
			candidateBehavior := map[string]any{}
			if rawBehavior, ok := behaviorByCase[caseID].(map[string]any); ok {
				candidateBehavior = rawBehavior
			}
			byCase[caseID] = append(byCase[caseID], map[string]any{
				"experiment_id":           toString(record["experiment_id"]),
				"surface":                 toString(record["surface"]),
				"outcome":                 toString(record["outcome"]),
				"failure_class":           toString(record["failure_class"]),
				"verifier_delta":          deltas[caseID],
				"candidate_patch_excerpt": toString(patches[caseID]),
				// 单数字段供旧分析器提示词使用，复数字段承载全部独立诊断。
				"candidate_failure_diagnosis": firstDiagnosis,
				"candidate_failure_diagnoses": caseDiagnoses,
				"candidate_behavior":          candidateBehavior,
			})
		}
	}
	byCaseFiltered := map[string]any{}
	for caseID, records := range byCase {
		if len(records) > 0 {
			// Python: records[-3:]——保留最近三条
			start := 0
			if len(records) > 3 {
				start = len(records) - 3
			}
			byCaseFiltered[caseID] = records[start:]
		}
	}
	return map[string]any{"by_case": byCaseFiltered}
}

// actionTargetCaseIDs 动作目标用例（Python: _action_target_case_ids）。
func actionTargetCaseIDs(
	action map[string]any,
	issueCaseIDs map[string]map[string]struct{},
	targetCaseIDsByRole map[string]map[string]struct{},
) map[string]struct{} {
	actionIssueIDs := map[string]struct{}{}
	for _, rawIssueID := range toAnySlice(action["attributed_issue_ids"]) {
		if issueID := toString(rawIssueID); issueID != "" {
			actionIssueIDs[issueID] = struct{}{}
		}
	}
	if len(actionIssueIDs) > 0 {
		result := map[string]struct{}{}
		for issueID := range actionIssueIDs {
			if cases, ok := issueCaseIDs[issueID]; ok {
				for caseID := range cases {
					result[caseID] = struct{}{}
				}
			}
		}
		return result
	}
	if cases, ok := targetCaseIDsByRole[toString(action["role"])]; ok {
		result := map[string]struct{}{}
		for caseID := range cases {
			result[caseID] = struct{}{}
		}
		return result
	}
	return map[string]struct{}{}
}

// bindTaskAcceptanceContracts 附加当前用例验证器反馈（Python: _bind_task_acceptance_contracts）。
func bindTaskAcceptanceContracts(
	cases []map[string]any,
	analysisRef string,
	targetCaseIDsSet map[string]struct{},
) ([]map[string]any, map[string]map[string]any, error) {
	if analysisRef == "" || len(targetCaseIDsSet) == 0 {
		return cloneCases(cases), map[string]map[string]any{}, nil
	}
	analysis, err := readYAMLFile(analysisRef)
	if err != nil {
		return nil, nil, err
	}
	metadata := map[string]any{}
	if raw, ok := analysis["metadata"].(map[string]any); ok {
		metadata = raw
	}
	diagnosesPath := toString(metadata["per_case_diagnoses_path"])
	if diagnosesPath == "" || !fileExists(diagnosesPath) {
		return cloneCases(cases), map[string]map[string]any{}, nil
	}
	raw, err := os.ReadFile(diagnosesPath)
	if err != nil {
		return cloneCases(cases), map[string]map[string]any{}, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return cloneCases(cases), map[string]map[string]any{}, nil
	}
	diagnoses := map[string]map[string]any{}
	for _, rawRecord := range toAnySlice(payload["per_case_diagnoses"]) {
		record, ok := rawRecord.(map[string]any)
		if !ok {
			continue
		}
		if caseID := toString(record["case_id"]); caseID != "" {
			diagnoses[caseID] = record
		}
	}
	contracts := map[string]map[string]any{}
	boundCases := []map[string]any{}
	for _, singleCase := range cases {
		cloned := cloneCase(singleCase)
		caseID := toString(singleCase["case_id"])
		diagnosis, hasDiagnosis := diagnoses[caseID]
		if _, isTarget := targetCaseIDsSet[caseID]; isTarget && hasDiagnosis {
			verifier := map[string]any{}
			if raw, ok := diagnosis["verifier_observations"].(map[string]any); ok {
				verifier = raw
			}
			contradiction := ""
			if validation, ok := diagnosis["validation_observations"].(map[string]any); ok {
				contradiction = toString(validation["contradiction_explanation"])
			}
			contract := map[string]any{
				"case_id":                         caseID,
				"failed_fail_to_pass_tests":       toAnySlice(verifier["failed_fail_to_pass_tests"]),
				"failed_pass_to_pass_tests":       toAnySlice(verifier["failed_pass_to_pass_tests"]),
				"verifier_failure_output_excerpt": toString(diagnosis["verifier_failure_output_excerpt"]),
				"semantic_invariant":              toString(diagnosis["root_cause"]),
				"required_correction":             toString(diagnosis["recommendation"]),
				"local_official_contradiction":    contradiction,
			}
			contracts[caseID] = contract
			appendTaskContract(cloned, contract)
		}
		boundCases = append(boundCases, cloned)
	}
	return boundCases, contracts, nil
}

// appendTaskContract 追加任务验收契约文本（Python: _append_task_contract）。
// 契约文本从 Python 源码逐字复制，禁止改写。
func appendTaskContract(singleCase map[string]any, contract map[string]any) {
	contractJSON, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		contractJSON = []byte("{}")
	}
	rendered := "<task-acceptance-contract>\n" +
		"This optimizer feedback is authoritative only for the current training case; " +
		"do not copy it into a reusable Skill. The named FAIL_TO_PASS operation may not " +
		"be replaced by a nearby convenience behavior. A locally available suite that " +
		"does not contain the named test is regression evidence, not proof of this contract.\n" +
		string(contractJSON) + "\n" +
		"Before completion, run the exact named test when available. If it is unavailable, " +
		"use the supplied authoritative failure output to enumerate competing semantics, " +
		"including positive and negative/order/lifecycle boundaries when relevant. A " +
		"self-authored probe may falsify a hypothesis only when its expected result is " +
		"grounded in the task, verifier output, or repository history; it cannot certify " +
		"equivalence to an unavailable official test. State unresolved verifier-only " +
		"semantics explicitly instead of choosing the probe expectation yourself.\n" +
		"</task-acceptance-contract>"
	key := "input"
	for _, name := range []string{"input", "inputs", "task_input", "query", "prompt"} {
		if _, ok := singleCase[name]; ok {
			key = name
			break
		}
	}
	value := singleCase[key]
	if key == "input" {
		if valueMap, ok := value.(map[string]any); ok && len(valueMap) == 1 {
			if _, hasUserMessage := valueMap["user_message"]; hasUserMessage {
				copied := map[string]any{}
				for k, v := range valueMap {
					copied[k] = v
				}
				copied["user_message"] = fmt.Sprintf("%s\n\n%s", toString(copied["user_message"]), rendered)
				singleCase[key] = copied
				return
			}
		}
	}
	valueStr, isStr := value.(string)
	if !isStr {
		valueJSON, _ := json.MarshalIndent(value, "", "  ")
		valueStr = string(valueJSON)
	}
	singleCase[key] = strings.TrimSpace(fmt.Sprintf("%s\n\n%s", valueStr, rendered))
}

// persistPromotion 持久化晋级结果（Python: _persist_promotion）。
func persistPromotion(
	memberRefPath string,
	candidateRefsPath string,
	gate map[string]any,
) error {
	status := toString(gate["status"])
	accepted := truthy(gate["accepted"]) && status == "accepted"
	promotionStatus := status
	if accepted {
		promotionStatus = "promoted"
	}
	candidateManifestPaths := map[string]struct{}{}
	for _, rawPath := range []string{memberRefPath, candidateRefsPath} {
		path := expandUser(rawPath)
		if !fileExists(path) {
			continue
		}
		payload, err := readYAMLFile(path)
		if err != nil {
			return err
		}
		if metadata, ok := payload["metadata"].(map[string]any); ok {
			if manifestPath := toString(metadata["candidate_manifest_path"]); manifestPath != "" {
				candidateManifestPaths[manifestPath] = struct{}{}
			}
		}
		roles, ok := payload["candidate_ready_roles"].([]any)
		if !ok {
			roles, _ = payload["published_roles"].([]any)
		}
		roleStrings := []any{}
		for _, role := range roles {
			roleStrings = append(roleStrings, toString(role))
		}
		payload["candidate_ready_roles"] = roleStrings
		if accepted {
			payload["promoted_roles"] = roleStrings
		} else {
			payload["promoted_roles"] = []any{}
		}
		payload["promotion_status"] = promotionStatus
		payload["candidate_gate"] = map[string]any{
			"status":          gate["status"],
			"reason":          gate["reason"],
			"source_score":    gate["source_score"],
			"candidate_score": gate["candidate_score"],
		}
		if err := writeYAMLAtomic(path, payload); err != nil {
			return err
		}
	}
	for rawManifestPath := range candidateManifestPaths {
		manifestPath := expandUser(rawManifestPath)
		if !fileExists(manifestPath) {
			continue
		}
		manifest, err := readYAMLFile(manifestPath)
		if err != nil {
			return err
		}
		promotionStatusValue := status
		if accepted {
			promotionStatusValue = "accepted"
		}
		manifest["status"] = promotionStatusValue
		manifest["candidate_gate"] = map[string]any{
			"status":                   gate["status"],
			"reason":                   gate["reason"],
			"source_score":             gate["source_score"],
			"candidate_score":          gate["candidate_score"],
			"score_delta":              gate["score_delta"],
			"target_case_ids":          toAnySlice(gate["target_case_ids"]),
			"target_confirmation":      gate["target_confirmation"],
			"epoch_checkpoint_outcome": gate["epoch_checkpoint_outcome"],
		}
		if err := writeYAMLAtomic(manifestPath, manifest); err != nil {
			return err
		}
	}
	return nil
}

// updateBatchAttemptRecord 使批次尝试状态与 epoch 检查点结果对齐（Python: _update_batch_attempt_record）。
func updateBatchAttemptRecord(completedBatch map[string]any, gate map[string]any) {
	memberRef := toString(gate["member_optimization_ref_path"])
	for _, rawAttempt := range toAnySlice(completedBatch["candidate_attempts"]) {
		attempt, ok := rawAttempt.(map[string]any)
		if !ok {
			continue
		}
		if toString(attempt["member_optimization_ref_path"]) != memberRef {
			continue
		}
		attempt["candidate_gate_status"] = toString(gate["status"])
		attempt["candidate_gate_reason"] = toString(gate["reason"])
	}
}

// capabilityRuntimeName 能力运行时名（Python: _capability_runtime_name，本节前置件）。
func capabilityRuntimeName(actionGroup, targetPath string) string {
	if targetPath == "" {
		return ""
	}
	normalized := filepath.ToSlash(targetPath)
	base := path.Base(normalized)
	if actionGroup == "skill" && strings.ToLower(base) == "skill.md" {
		// Python: normalized.parent.name——父目录的 base 名
		return path.Base(path.Dir(normalized))
	}
	return strings.TrimSuffix(base, path.Ext(base))
}

// evalScore 评估引用均分（Python: _eval_score，本节前置件）。
func evalScore(evalRefPath string) (float64, error) {
	payload, err := readYAMLFile(evalRefPath)
	if err != nil {
		return 0, err
	}
	total := 0.0
	count := 0
	for _, rawCase := range toAnySlice(payload["cases"]) {
		caseMap, ok := rawCase.(map[string]any)
		if !ok {
			continue
		}
		if score := numberOf(caseMap["score"]); score != nil {
			total += *score
			count++
		}
	}
	if count == 0 {
		return 0.0, nil
	}
	return total / float64(count), nil
}

// evalRefComplete 评估引用完整性（Python: _eval_ref_complete，本节前置件）。
func evalRefComplete(path string) (bool, error) {
	if !fileExists(path) {
		return false, nil
	}
	payload, err := readYAMLFile(path)
	if err != nil {
		return false, err
	}
	rawCases, _ := payload["cases"].([]any)
	if len(rawCases) == 0 {
		return false, nil
	}
	for _, rawCase := range rawCases {
		caseMap, ok := rawCase.(map[string]any)
		if !ok {
			return false, nil
		}
		if !fileExists(expandUser(toString(caseMap["result_path"]))) {
			return false, nil
		}
	}
	return true, nil
}

// llmCasePassed LLM 评委按配置阈值判定（Python: _llm_case_passed，本节前置件）。
func llmCasePassed(caseMap map[string]any) *bool {
	metadata, ok := caseMap["metadata"].(map[string]any)
	if !ok || toString(metadata["evaluation_method"]) != "llm_as_judge" {
		return nil
	}
	return evalCaseExplicitPassed(caseMap)
}

// evalCaseExplicitPassed 显式 evaluation_passed 元数据（Python: _eval_case_explicit_passed）。
func evalCaseExplicitPassed(caseMap map[string]any) *bool {
	metadata := map[string]any{}
	if raw, ok := caseMap["metadata"].(map[string]any); ok {
		metadata = raw
	}
	value, ok := metadata["evaluation_passed"].(bool)
	if !ok {
		return nil
	}
	return &value
}

// passingCaseIdsByRef 通过用例集合（Python: _passing_case_ids，本节前置件）。
func passingCaseIDs(evalRefPath string) (map[string]struct{}, error) {
	passing := map[string]struct{}{}
	payload, err := readYAMLFile(evalRefPath)
	if err != nil {
		return nil, err
	}
	for _, rawCase := range toAnySlice(payload["cases"]) {
		caseMap, ok := rawCase.(map[string]any)
		if !ok {
			continue
		}
		status := strings.ToLower(toString(caseMap["status"]))
		if status == "failed" || status == "error" || status == "skipped" {
			continue
		}
		metadata, _ := caseMap["metadata"].(map[string]any)
		if metadata != nil && metadata["infrastructure_skip"] == true {
			continue
		}
		explicit := llmCasePassed(caseMap)
		score := numberOf(caseMap["score"])
		passed := false
		if explicit != nil {
			passed = *explicit
		} else {
			passed = score != nil && *score >= 1.0
		}
		if passed && toString(caseMap["case_id"]) != "" {
			passing[toString(caseMap["case_id"])] = struct{}{}
		}
	}
	return passing, nil
}

// readYAMLFile 读 YAML 文件为映射（Python: _read_yaml，本节前置件）。
// 文件不存在或非映射时返回空映射（吞错语义一比一）。
func readYAMLFile(path string) (map[string]any, error) {
	target := expandUser(path)
	if !fileExists(target) {
		return map[string]any{}, nil
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, err
	}
	var parsed any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	if parsed == nil {
		return map[string]any{}, nil
	}
	if result, ok := parsed.(map[string]any); ok {
		return result, nil
	}
	return map[string]any{}, nil
}

// writeYAMLAtomic 原子写 YAML（Python: _write_yaml_atomic，本节前置件）。
// 偏差 D-d4：yaml.v3 对 map 按键排序输出，键序非插入序，内容等价（source_evidence 先例）。
func writeYAMLAtomic(path string, payload map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(payload)
	if err != nil {
		return err
	}
	temporary := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// numberOfValue 数值提取并落为 float64/nil（供存入 map——Python 存 float 或 None）。
func numberOfValue(value any) any {
	if f := numberOf(value); f != nil {
		return *f
	}
	return nil
}

// numberOf 数值提取（Python: _number——bool/nil/转换失败返回 nil，本节前置件）。
func numberOf(value any) *float64 {
	switch v := value.(type) {
	case bool:
		return nil
	case nil:
		return nil
	case float64:
		return &v
	case float32:
		f := float64(v)
		return &f
	case int:
		f := float64(v)
		return &f
	case int64:
		f := float64(v)
		return &f
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%g", &f); err != nil {
			return nil
		}
		return &f
	default:
		return nil
	}
}

// ──────────────────────────── 通用小助手（Go 侧工具，Python 无对应物） ────────────────────────────

// resolvePath 展开用户目录并解析为绝对路径（Python: Path(p).expanduser().resolve()）。
func resolvePath(rawPath string) (string, error) {
	return filepath.Abs(expandUser(rawPath))
}

// fileExists 文件是否存在。
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// toAnySlice 任意值转 any 切片（nil 安全）。
func toAnySlice(value any) []any {
	if slice, ok := value.([]any); ok {
		return slice
	}
	return nil
}

// toInt 任意数值转 int（Python int() 截断语义）。
func toInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
}

// defaultString 首选值空串时取回退值（Python: str(x or y) 常用形态）。
func defaultString(primary any, fallback string) string {
	if s := strings.TrimSpace(toString(primary)); s != "" {
		return s
	}
	return fallback
}

// setDefault 键不存在时写入默认值（Python: dict.setdefault）。
func setDefault(state map[string]any, key string, value any) {
	if _, ok := state[key]; !ok {
		state[key] = value
	}
}

// setDefaultString 键不存在或为空时写入默认字符串。
func setDefaultString(state map[string]any, key, value string) {
	if existing, ok := state[key]; !ok || toString(existing) == "" {
		state[key] = value
	}
}

// mappingOrNil 任意值转映射（Python: value if isinstance(value, dict) else {}）。
func mappingOrNil(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// anyEqual 深度相等（Python == 对 dict/list 的结构比较）。
func anyEqual(left, right any) bool {
	return reflect.DeepEqual(left, right)
}

// numberEqual 数值相等（int/float 形态差异免疫）。
func numberEqual(left, right any) bool {
	leftNum := numberOf(left)
	rightNum := numberOf(right)
	return leftNum != nil && rightNum != nil && *leftNum == *rightNum
}

// anySliceNonEmpty any 切片非空（Python: list(value) 判空）。
func anySliceNonEmpty(value any) bool {
	return len(toAnySlice(value)) > 0
}

// truthy Python truthiness（仅 bool 与非零数值视为真）。
func truthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case int:
		return v != 0
	case float64:
		return v != 0
	default:
		return false
	}
}

// cloneCase 浅拷贝用例映射（Python: dict(case)）。
func cloneCase(singleCase map[string]any) map[string]any {
	cloned := map[string]any{}
	for key, value := range singleCase {
		cloned[key] = value
	}
	return cloned
}

// cloneCases 批量浅拷贝用例。
func cloneCases(cases []map[string]any) []map[string]any {
	cloned := make([]map[string]any, 0, len(cases))
	for _, singleCase := range cases {
		cloned = append(cloned, cloneCase(singleCase))
	}
	return cloned
}

// sortedStringSet 集合转排序切片（Python: sorted(set)）。
func sortedStringSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// sortedStringSetKeyDiff 差集转排序切片（Python: sorted(a - b)）。
func sortedStringSetKeyDiff(minuend, subtrahend map[string]struct{}) []string {
	diff := map[string]struct{}{}
	for value := range minuend {
		if _, ok := subtrahend[value]; !ok {
			diff[value] = struct{}{}
		}
	}
	return sortedStringSet(diff)
}

// stringSetEqual 集合相等。
func stringSetEqual(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for value := range left {
		if _, ok := right[value]; !ok {
			return false
		}
	}
	return true
}

// setEqual setEqual 是 stringSetEqual 的别名（保持调用点语义可读）。
func setEqual(left, right map[string]struct{}) bool {
	return stringSetEqual(left, right)
}

// sortedStringSetToAnySlice 集合转排序 any 切片。
func sortedStringSetToAnySlice(set map[string]struct{}) []any {
	out := make([]any, 0, len(set))
	for _, value := range sortedStringSet(set) {
		out = append(out, value)
	}
	return out
}

// yamlToAnyMap 结构体转 any 映射（Python: dataclasses.asdict）。
func yamlToAnyMap(value any) (map[string]any, error) {
	data, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := yaml.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}
