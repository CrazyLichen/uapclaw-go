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
	"unicode"

	hresources "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	dataloader "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/data_loader"
	evaluatorpkg "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/evaluator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/improver_evolution"
	memberoptimizer "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer"
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

// readJSONFile 读 JSON 文件为映射（Python: _read_json——不存在/解析失败返回空映射）。
func readJSONFile(path string) map[string]any {
	target := expandUser(path)
	if !fileExists(target) {
		return map[string]any{}
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return map[string]any{}
	}
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return map[string]any{}
	}
	if result, ok := parsed.(map[string]any); ok {
		return result
	}
	return map[string]any{}
}

// mappingKeys 映射键集合。
func mappingKeys(value map[string]any) map[string]struct{} {
	keys := map[string]struct{}{}
	for key := range value {
		keys[key] = struct{}{}
	}
	return keys
}

// mappingStringKeys 浮点映射键集合。
func mappingStringKeys(value map[string]float64) map[string]struct{} {
	keys := map[string]struct{}{}
	for key := range value {
		keys[key] = struct{}{}
	}
	return keys
}

// stringSetIntersection 集合交集。
func stringSetIntersection(left, right map[string]struct{}) map[string]struct{} {
	out := map[string]struct{}{}
	for value := range left {
		if _, ok := right[value]; ok {
			out[value] = struct{}{}
		}
	}
	return out
}

// stringSetKeyDiffToSlice 与映射键集求差并排序（failed_targets − first_edit_steps 场景）。
func stringSetKeyDiffToSlice(minuend map[string]struct{}, subtrahendKeys map[string]int) []string {
	subtrahend := map[string]struct{}{}
	for key := range subtrahendKeys {
		subtrahend[key] = struct{}{}
	}
	return sortedStringSetKeyDiff(minuend, subtrahend)
}

// toKeySet 字符串切片转键集。
func toKeySet(values []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

// collectTrajectoryDirUsage 收集成员轨迹目录用量（Python: _collect_trajectory_dir_usage）。
func collectTrajectoryDirUsage(trace map[string]any, toolNames, skillNames map[string]struct{}) {
	trajectoryDir := expandUser(toString(trace["trajectory_dir"]))
	if info, err := os.Stat(trajectoryDir); err != nil || !info.IsDir() {
		return
	}
	for _, memberTracePath := range evaluatorpkg.TrajectoryGlobJSONL(trajectoryDir) {
		evaluatorpkg.CollectJSONLSuccessfulUsage(memberTracePath, toolNames, skillNames)
	}
}

// failedMachineEvidence 失败的机器证据（Python: _failed_machine_evidence）。
func failedMachineEvidence(evalRefPath string) ([]string, error) {
	failures := []string{}
	payload, err := readYAMLFile(evalRefPath)
	if err != nil {
		return nil, err
	}
	for _, rawCase := range toAnySlice(payload["cases"]) {
		caseMap, ok := rawCase.(map[string]any)
		if !ok {
			continue
		}
		result := readJSONFile(toString(caseMap["result_path"]))
		metadata := mappingOrNil(mappingOrNil(result["evaluation"])["metadata"])
		evidence := mappingOrNil(metadata["artifact_runtime_evidence"])
		for _, rawObservation := range toAnySlice(evidence["observations"]) {
			observation, ok := rawObservation.(map[string]any)
			if !ok {
				continue
			}
			status := strings.ToLower(toString(observation["status"]))
			if status == "failed" || status == "error" {
				caseID := defaultString(caseMap["case_id"], "unknown")
				failures = append(failures, fmt.Sprintf("%s:%s", caseID, toString(observation["type"])))
			}
		}
	}
	sort.Strings(failures)
	deduped := failures[:0]
	seen := map[string]struct{}{}
	for _, failure := range failures {
		if _, ok := seen[failure]; !ok {
			seen[failure] = struct{}{}
			deduped = append(deduped, failure)
		}
	}
	return deduped, nil
}

// machineEvidenceCaseIDs 机器证据用例集（Python: _machine_evidence_case_ids）。
func machineEvidenceCaseIDs(failures []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, failure := range failures {
		parts := strings.SplitN(failure, ":", 2)
		if parts[0] != "" {
			out[parts[0]] = struct{}{}
		}
	}
	return out
}

// toolNamesMatch 工具名匹配（Python: _tool_names_match——小写/中划线转下划线/去 _tool 后缀）。
func toolNamesMatch(planned, invoked string) bool {
	canonical := func(value string) string {
		value = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "_")
		return strings.TrimSuffix(value, "_tool")
	}
	return canonical(planned) != "" && canonical(planned) == canonical(invoked)
}

// skillNamesMatch 技能名匹配（Python: _skill_names_match——不去 _tool 后缀）。
func skillNamesMatch(planned, invoked string) bool {
	canonical := func(value string) string {
		return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "_")
	}
	return canonical(planned) != "" && canonical(planned) == canonical(invoked)
}

// ──────────────────────────── 检查点过滤物化段（Python 3614-4138） ────────────────────────────

// materializeCheckpointFilteredHarness 把保留改动组合到 epoch 的安全起点 Harness 上。
// Python: _materialize_checkpoint_filtered_harness。
func materializeCheckpointFilteredHarness(
	outputDir string,
	epoch int,
	baseHarnessRefsPath, replayedHarnessRefsPath string,
	retainedGates, removedGates []map[string]any,
	fullEvalRefPath string,
) (string, error) {
	baseRefs, err := resolvePath(baseHarnessRefsPath)
	if err != nil {
		return "", err
	}
	replayedRefs, err := resolvePath(replayedHarnessRefsPath)
	if err != nil {
		return "", err
	}
	basePayload, err := readYAMLFile(baseRefs)
	if err != nil {
		return "", err
	}
	baseRoles, _ := basePayload["harness_refs"].(map[string]any)
	if len(baseRoles) == 0 {
		return "", errors.New("Cannot filter epoch checkpoint: base refs contain no harness_refs")
	}

	selectionRoot := filepath.Join(outputDir, "epoch_selections", fmt.Sprintf("e%03d", epoch))
	stagingRoot := filepath.Join(filepath.Dir(selectionRoot), filepath.Base(selectionRoot)+".filter_tmp")
	if _, err := os.Stat(stagingRoot); err == nil {
		if err := os.RemoveAll(stagingRoot); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(stagingRoot, 0o755); err != nil {
		return "", err
	}
	stagedRoles := map[string]string{}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(stagingRoot)
		}
	}()
	rolesSorted := make([]string, 0, len(baseRoles))
	for role := range baseRoles {
		rolesSorted = append(rolesSorted, role)
	}
	sort.Strings(rolesSorted)
	for _, role := range rolesSorted {
		source, err := resolvePath(toString(baseRoles[role]))
		if err != nil {
			return "", err
		}
		if info, err := os.Stat(source); err != nil || !info.IsDir() {
			return "", fmt.Errorf(
				"Cannot filter epoch checkpoint role '%s': source package does not exist: %s", role, source)
		}
		roleDir := filepath.Join(stagingRoot, "harnesses", checkpointRoleDirName(role))
		if err := copyTreePreserve(source, roleDir); err != nil {
			return "", err
		}
		// 组合前先播种 H0 声明；否则同步 delta-only 注册表会擦除基线能力。
		if err := memberoptimizer.PreparePluginRegistries(roleDir); err != nil {
			return "", err
		}
		stagedRoles[role] = roleDir
	}

	retainedRecords := []map[string]string{}
	for _, gate := range retainedGates {
		candidateRoles := checkpointHarnessRolePaths(toString(gate["candidate_harness_refs_path"]))
		for _, rawCapability := range toAnySlice(gate["capabilities"]) {
			capability, ok := rawCapability.(map[string]any)
			if !ok {
				continue
			}
			role := toString(capability["role"])
			if _, staged := stagedRoles[role]; !staged && len(stagedRoles) == 1 {
				for onlyRole := range stagedRoles {
					role = onlyRole
					break
				}
			}
			targetRel := checkpointRelativePath(toString(capability["target_path"]))
			if _, staged := stagedRoles[role]; !staged || targetRel == "" {
				return "", fmt.Errorf(
					"Cannot filter epoch checkpoint capability without a "+
						"resolvable role and target path: %v", capability)
			}
			operation := toString(capability["operation"])
			actionGroup := toString(capability["action_group"])
			runtimeName := toString(capability["runtime_name"])
			candidateRole := candidateRoles[role]
			if candidateRole == "" && len(candidateRoles) == 1 {
				for _, only := range candidateRoles {
					candidateRole = only
					break
				}
			}
			if operation == "add" || operation == "modify" {
				if candidateRole == "" {
					return "", fmt.Errorf(
						"Cannot compose retained capability without its candidate role package: %s:%s",
						role, targetRel)
				}
				if err := checkpointCopyCapability(
					stagedRoles[role], candidateRole,
					actionGroup, targetRel, runtimeName); err != nil {
					return "", err
				}
			} else if operation == "remove" {
				if err := checkpointRemoveAddedCapability(
					stagedRoles[role], actionGroup, targetRel, runtimeName); err != nil {
					return "", err
				}
			} else {
				return "", fmt.Errorf(
					"Checkpoint filtering supports retained add/modify/remove capabilities, got %s:%s",
					actionGroup, operation)
			}
			retainedRecords = append(retainedRecords, map[string]string{
				"action_id":    toString(capability["action_id"]),
				"role":         role,
				"action_group": actionGroup,
				"operation":    operation,
				"target_path":  targetRel,
			})
		}
	}

	for _, roleDir := range stagedRoles {
		if err := memberoptimizer.SynchronizePluginManifest(roleDir); err != nil {
			return "", err
		}
		manifest := filepath.Join(roleDir, "manifest.json")
		if fileExists(manifest) {
			if _, err := hresources.LoadPluginPackage(manifest); err != nil {
				return "", err
			}
		}
	}

	if _, err := os.Stat(selectionRoot); err == nil {
		if err := os.RemoveAll(selectionRoot); err != nil {
			return "", err
		}
	}
	if err := os.Rename(stagingRoot, selectionRoot); err != nil {
		return "", err
	}
	success = true

	removedRecords := []map[string]string{}
	for _, gate := range removedGates {
		for _, rawCapability := range toAnySlice(gate["capabilities"]) {
			capability, ok := rawCapability.(map[string]any)
			if !ok {
				continue
			}
			removedRecords = append(removedRecords, map[string]string{
				"action_id":    toString(capability["action_id"]),
				"role":         toString(capability["role"]),
				"action_group": toString(capability["action_group"]),
				"operation":    toString(capability["operation"]),
				"target_path":  checkpointRelativePath(toString(capability["target_path"])),
			})
		}
	}
	selectedRoles := map[string]any{}
	roleOrder := make([]string, 0, len(stagedRoles))
	for role := range stagedRoles {
		roleOrder = append(roleOrder, role)
	}
	sort.Strings(roleOrder)
	for _, role := range roleOrder {
		selectedRoles[role] = filepath.Join(selectionRoot, "harnesses", filepath.Base(stagedRoles[role]))
	}
	selectedPayload := map[string]any{}
	for key, value := range basePayload {
		selectedPayload[key] = value
	}
	roleResults := map[string]any{}
	for _, role := range roleOrder {
		roleResults[role] = map[string]any{
			"status":                  "checkpoint_filtered",
			"before_harness_ref_path": toString(baseRoles[role]),
			"after_harness_ref_path":  selectedRoles[role],
		}
	}
	roleIDentities := []any{}
	for _, rawRole := range toAnySlice(basePayload["roles"]) {
		rawRoleMap, ok := rawRole.(map[string]any)
		if !ok {
			continue
		}
		identity := map[string]any{}
		for key, value := range rawRoleMap {
			identity[key] = value
		}
		roleName := defaultString(identity["member_name"], toString(identity["role"]))
		if selected, ok := selectedRoles[roleName]; ok {
			identity["harness_ref_path"] = selected
		}
		roleIDentities = append(roleIDentities, identity)
	}
	retainedActionIDs := []any{}
	for _, id := range capabilityActionIDs(retainedGates) {
		retainedActionIDs = append(retainedActionIDs, id)
	}
	retainedActionsAny := []any{}
	for _, record := range retainedRecords {
		retainedActionsAny = append(retainedActionsAny, record)
	}
	removedActionsAny := []any{}
	for _, record := range removedRecords {
		removedActionsAny = append(removedActionsAny, record)
	}
	selectedPayload["source_harness_refs_path"] = baseRefs
	selectedPayload["harness_refs"] = selectedRoles
	selectedPayload["promotion_status"] = "checkpoint_filtered"
	selectedPayload["role_results"] = roleResults
	selectedPayload["checkpoint_filter"] = map[string]any{
		"full_eval_ref_path":               fullEvalRefPath,
		"replayed_harness_refs_path":       replayedRefs,
		"post_checkpoint_replay_performed": false,
		"retained_action_ids":              retainedActionIDs,
		"retained_actions":                 retainedActionsAny,
		"removed_actions":                  removedActionsAny,
	}
	if len(roleIDentities) > 0 {
		selectedPayload["roles"] = roleIDentities
	}
	selectedRefPath := filepath.Join(selectionRoot, "harness_refs.yaml")
	if err := writeYAMLAtomic(selectedRefPath, selectedPayload); err != nil {
		return "", err
	}
	if err := validateSingleHarnessRefs(selectedRefPath); err != nil {
		return "", err
	}
	return selectedRefPath, nil
}

// checkpointHarnessRolePaths 引用文件的 role→路径映射（Python: _checkpoint_harness_role_paths）。
func checkpointHarnessRolePaths(refsPath string) map[string]string {
	result := map[string]string{}
	payload, err := readYAMLFile(refsPath)
	if err != nil {
		return result
	}
	refs, ok := payload["harness_refs"].(map[string]any)
	if !ok {
		return result
	}
	for role, rawPath := range refs {
		roleStr := toString(role)
		pathStr := toString(rawPath)
		if roleStr == "" || pathStr == "" {
			continue
		}
		if resolved, err := resolvePath(pathStr); err == nil {
			result[roleStr] = resolved
		}
	}
	return result
}

// checkpointRoleDirName 角色目录名（Python: _checkpoint_role_dir_name——字母数字保留小写、其余转下划线、去首尾下划线）。
func checkpointRoleDirName(role string) string {
	var normalized strings.Builder
	for _, char := range role {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			normalized.WriteRune(unicode.ToLower(char))
		} else {
			normalized.WriteRune('_')
		}
	}
	normalizedStr := strings.Trim(normalized.String(), "_")
	digest := sha256.Sum256([]byte(role))
	return fmt.Sprintf("%s_%s", defaultString(normalizedStr, "role"), hex.EncodeToString(digest[:])[:8])
}

// checkpointRelativePath 相对路径规范化（Python: _checkpoint_relative_path）。
func checkpointRelativePath(rawPath string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(rawPath), "\\", "/")
	if normalized == "" || path.IsAbs(normalized) {
		return ""
	}
	parts := strings.Split(normalized, "/")
	for _, part := range parts {
		if part == ".." {
			return ""
		}
	}
	return path.Join(parts...)
}

// checkpointRemoveAddedCapability 移除新增能力（Python: _checkpoint_remove_added_capability）。
func checkpointRemoveAddedCapability(
	harnessRoot string, actionGroup, targetRel, runtimeName string,
) error {
	if actionGroup == "config" {
		return checkpointRemovePath(harnessRoot, "runtime.yaml")
	}
	if actionGroup == "skill" {
		parts := strings.Split(targetRel, "/")
		skillRoot := targetRel
		if len(parts) >= 2 && parts[0] == "skills" {
			skillRoot = path.Join(parts[0], parts[1])
		}
		if err := checkpointRemovePath(harnessRoot, skillRoot); err != nil {
			return err
		}
		return checkpointRemoveManifestEntries(
			filepath.Join(harnessRoot, "skills", "skills.yaml"),
			"skills", skillRoot, runtimeName, true)
	}
	if actionGroup == "prompt" {
		if err := checkpointRemovePath(harnessRoot, targetRel); err != nil {
			return err
		}
		return checkpointRemovePromptEntry(
			filepath.Join(harnessRoot, "prompt_sections", "sections.yaml"),
			targetRel, runtimeName)
	}
	if actionGroup == "tool" || actionGroup == "rail" {
		registry := actionGroup + "s"
		manifestName := registry + "/" + registry + ".yaml"
		if targetRel == manifestName {
			return fmt.Errorf("Cannot remove an added %s whose target is only %s", actionGroup, manifestName)
		}
		if err := checkpointRemovePath(harnessRoot, targetRel); err != nil {
			return err
		}
		return checkpointRemoveManifestEntries(
			filepath.Join(harnessRoot, manifestName),
			registry, targetRel, runtimeName, false)
	}
	return fmt.Errorf("Checkpoint filtering does not support action_group=%q", actionGroup)
}

// checkpointCopyCapability 复制保留能力（Python: _checkpoint_copy_capability）。
func checkpointCopyCapability(
	harnessRoot, candidateHarnessRoot, actionGroup, targetRel, runtimeName string,
) error {
	if actionGroup == "config" {
		return checkpointCopyPath(harnessRoot, candidateHarnessRoot, "runtime.yaml")
	}
	copyRel := targetRel
	manifestName := ""
	manifestKey := ""
	switch actionGroup {
	case "skill":
		parts := strings.Split(targetRel, "/")
		if len(parts) >= 2 && parts[0] == "skills" {
			copyRel = path.Join(parts[0], parts[1])
		}
		manifestName = "skills/skills.yaml"
		manifestKey = "skills"
	case "prompt":
		manifestName = "prompt_sections/sections.yaml"
		manifestKey = "sections"
	case "tool", "rail":
		manifestKey = actionGroup + "s"
		manifestName = manifestKey + "/" + manifestKey + ".yaml"
	default:
		return fmt.Errorf("Checkpoint filtering does not support action_group=%q", actionGroup)
	}
	if targetRel != manifestName {
		if err := checkpointCopyPath(harnessRoot, candidateHarnessRoot, copyRel); err != nil {
			return err
		}
	}
	return checkpointCopyManifestEntries(
		filepath.Join(harnessRoot, manifestName),
		filepath.Join(candidateHarnessRoot, manifestName),
		manifestKey, copyRel, runtimeName)
}

// checkpointCopyPath 复制路径（Python: _checkpoint_copy_path）。
func checkpointCopyPath(harnessRoot, sourceHarnessRoot, targetRel string) error {
	destination := filepath.Join(harnessRoot, targetRel)
	source := filepath.Join(sourceHarnessRoot, targetRel)
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("Cannot compose retained capability: source path is missing: %s", source)
	}
	if err := checkpointRemovePath(harnessRoot, targetRel); err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if info.IsDir() {
		return copyTreePreserve(source, destination)
	}
	return copyFilePreserve(source, destination)
}

// checkpointCopyManifestEntries 复制 manifest 条目（Python: _checkpoint_copy_manifest_entries）。
func checkpointCopyManifestEntries(
	destination, source string,
	listKey, targetRel, runtimeName string,
) error {
	nativeManifest := filepath.Join(filepath.Dir(filepath.Dir(source)), "manifest.json")
	var sourceEntries []any
	if fileExists(nativeManifest) {
		// 已发布的原生候选无需携带 RSI 可编辑 sidecar。
		raw, err := os.ReadFile(nativeManifest)
		if err != nil {
			return err
		}
		var nativeData map[string]any
		if err := json.Unmarshal(raw, &nativeData); err != nil {
			return err
		}
		nativeKey := listKey
		if listKey == "sections" {
			nativeKey = "prompt_sections"
		}
		sourceEntries = toAnySlice(nativeData[nativeKey])
	} else if fileExists(source) {
		raw, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		var sourceData any
		if err := yaml.Unmarshal(raw, &sourceData); err != nil {
			return err
		}
		if sourceMap, ok := sourceData.(map[string]any); ok {
			sourceEntries = toAnySlice(sourceMap[listKey])
		} else if sourceList, ok := sourceData.([]any); ok {
			sourceEntries = sourceList
		}
	} else {
		return nil
	}
	if sourceEntries == nil {
		sourceEntries = []any{nil}
		if len(sourceEntries) == 1 && sourceEntries[0] == nil {
			sourceEntries = []any{}
		}
	}

	matches := func(entry any) bool {
		if entryStr, ok := entry.(string); ok {
			return checkpointManifestPathMatches(entryStr, targetRel, true)
		}
		entryMap, ok := entry.(map[string]any)
		if !ok {
			return false
		}
		if runtimeName != "" && toString(entryMap["name"]) == runtimeName {
			return true
		}
		for _, key := range []string{"file", "file_path", "path", "dir"} {
			if checkpointManifestPathMatches(toString(entryMap[key]), targetRel, true) {
				return true
			}
		}
		return false
	}

	selected := []any{}
	for _, entry := range sourceEntries {
		if matches(entry) {
			selected = append(selected, entry)
		}
	}
	if len(selected) == 0 {
		display := runtimeName
		if display == "" {
			display = targetRel
		}
		return fmt.Errorf(
			"Cannot compose retained capability: candidate manifest has no matching entry for %s", display)
	}

	var destinationData any
	if fileExists(destination) {
		raw, err := os.ReadFile(destination)
		if err != nil {
			return err
		}
		if err := yaml.Unmarshal(raw, &destinationData); err != nil {
			return err
		}
	} else {
		destinationData = map[string]any{listKey: []any{}}
	}
	var destinationEntries []any
	dataMap, isMap := destinationData.(map[string]any)
	if isMap {
		destinationEntries = toAnySlice(dataMap[listKey])
	} else if dataList, isList := destinationData.([]any); isList {
		destinationEntries = dataList
		dataMap = map[string]any{listKey: destinationEntries}
		isMap = true
	} else {
		destinationEntries = []any{}
		dataMap = map[string]any{listKey: destinationEntries}
		isMap = true
	}
	if _, isNil := dataMap[listKey]; !isNil && destinationEntries == nil {
		destinationEntries = []any{}
	}
	if destinationEntries == nil {
		destinationEntries = []any{}
	}
	position := len(destinationEntries)
	for index, entry := range destinationEntries {
		if matches(entry) {
			position = index
			break
		}
	}
	kept := []any{}
	for _, entry := range destinationEntries {
		if !matches(entry) {
			kept = append(kept, entry)
		}
	}
	// Python: destination_entries[position:position] = selected——位置插入
	newEntries := make([]any, 0, len(kept)+len(selected))
	if position > len(kept) {
		position = len(kept)
	}
	newEntries = append(newEntries, kept[:position]...)
	newEntries = append(newEntries, selected...)
	newEntries = append(newEntries, kept[position:]...)
	dataMap[listKey] = newEntries
	return writeYAMLAtomic(destination, dataMap)
}

// checkpointRemovePath 删除路径（Python: _checkpoint_remove_path）。
func checkpointRemovePath(harnessRoot, targetRel string) error {
	target := filepath.Join(harnessRoot, targetRel)
	info, err := os.Stat(target)
	if err != nil {
		return nil
	}
	if info.IsDir() {
		return os.RemoveAll(target)
	}
	return os.Remove(target)
}

// checkpointRemovePromptEntry 移除提示词条目（Python: _checkpoint_remove_prompt_entry）。
func checkpointRemovePromptEntry(manifestPath string, targetRel, sectionName string) error {
	if !fileExists(manifestPath) {
		return nil
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var parsed any
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return err
	}
	var data map[string]any
	var entries []any
	key := "prompt_sections"
	if dataMap, ok := parsed.(map[string]any); ok {
		data = dataMap
		if _, hasSections := dataMap["sections"]; hasSections {
			key = "sections"
		}
		entries = toAnySlice(dataMap[key])
	} else if dataList, ok := parsed.([]any); ok {
		key = "sections"
		entries = dataList
		data = map[string]any{key: entries}
	} else {
		return nil
	}
	if entries == nil {
		entries = []any{data[key]}
	}
	kept := []any{}
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			kept = append(kept, entry)
			continue
		}
		nameMatches := sectionName != "" && toString(entryMap["name"]) == sectionName
		pathValue := toString(entryMap["file"])
		if pathValue == "" {
			pathValue = toString(entryMap["path"])
		}
		pathMatches := checkpointManifestPathMatches(pathValue, targetRel, true)
		if !nameMatches && !pathMatches {
			kept = append(kept, entry)
		}
	}
	data[key] = kept
	return writeYAMLAtomic(manifestPath, data)
}

// checkpointRemoveManifestEntries 移除 manifest 条目（Python: _checkpoint_remove_manifest_entries）。
func checkpointRemoveManifestEntries(
	manifestPath string, listKey, targetRel, runtimeName string, exactOnly bool,
) error {
	if !fileExists(manifestPath) {
		return nil
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var parsed any
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return err
	}
	var data map[string]any
	var entries []any
	if dataMap, ok := parsed.(map[string]any); ok {
		data = dataMap
		entries = toAnySlice(dataMap[listKey])
	} else if dataList, ok := parsed.([]any); ok {
		entries = dataList
		data = map[string]any{listKey: entries}
	} else {
		return nil
	}
	if entries == nil {
		entries = []any{data[listKey]}
	}
	matches := func(entry any) bool {
		if entryStr, ok := entry.(string); ok {
			return checkpointManifestPathMatches(entryStr, targetRel, exactOnly)
		}
		entryMap, ok := entry.(map[string]any)
		if !ok {
			return false
		}
		if runtimeName != "" && toString(entryMap["name"]) == runtimeName {
			return true
		}
		for _, key := range []string{"file", "file_path", "path", "dir"} {
			if checkpointManifestPathMatches(toString(entryMap[key]), targetRel, exactOnly) {
				return true
			}
		}
		return false
	}
	kept := []any{}
	for _, entry := range entries {
		if !matches(entry) {
			kept = append(kept, entry)
		}
	}
	data[listKey] = kept
	return writeYAMLAtomic(manifestPath, data)
}

// checkpointManifestPathMatches manifest 路径匹配（Python: _checkpoint_manifest_path_matches）。
func checkpointManifestPathMatches(rawPath, targetRel string, exactOnly bool) bool {
	normalized := checkpointRelativePath(rawPath)
	target := checkpointRelativePath(targetRel)
	if normalized == "" || target == "" {
		return false
	}
	if normalized == target {
		return true
	}
	if exactOnly {
		return false
	}
	return strings.HasPrefix(normalized, target+"/") || strings.HasPrefix(target, normalized+"/")
}

// missingCapabilityInvocations 缺失能力调用（Python: _missing_capability_invocations）。
func missingCapabilityInvocations(
	capabilities []map[string]any,
	actionGroup string,
	invokedNamesByCase map[string]map[string]struct{},
	fallbackTargetCaseIDs map[string]struct{},
	namesMatch func(planned, invoked string) bool,
	laterEditNamesByCase map[string]map[string]struct{},
) []map[string]string {
	missing := []map[string]string{}
	for _, capability := range capabilities {
		runtimeName := toString(capability["runtime_name"])
		operation := toString(capability["operation"])
		if toString(capability["action_group"]) != actionGroup ||
			(operation != "add" && operation != "modify") ||
			runtimeName == "" {
			continue
		}
		targetCaseIDs := map[string]struct{}{}
		for _, rawCaseID := range toAnySlice(capability["target_case_ids"]) {
			if caseID := toString(rawCaseID); caseID != "" {
				targetCaseIDs[caseID] = struct{}{}
			}
		}
		if len(targetCaseIDs) == 0 {
			for caseID := range fallbackTargetCaseIDs {
				targetCaseIDs[caseID] = struct{}{}
			}
		}
		eligibleNames := invokedNamesByCase
		activationPhase := toString(capability["activation_phase"])
		if activationPhase == "post_diagnosis" || activationPhase == "pre_submission" {
			eligibleNames = laterEditNamesByCase
			if eligibleNames == nil {
				eligibleNames = map[string]map[string]struct{}{}
			}
		}
		for _, caseID := range sortedStringSet(targetCaseIDs) {
			invokedNames := eligibleNames[caseID]
			matched := false
			for invoked := range invokedNames {
				if namesMatch(runtimeName, invoked) {
					matched = true
					break
				}
			}
			if !matched {
				missing = append(missing, map[string]string{
					"runtime_name":   runtimeName,
					"target_case_id": caseID,
				})
			}
		}
	}
	return missing
}

// missingInvocationsWereLate 缺失调用是否发生在激活窗口之外（Python: _missing_invocations_were_late）。
func missingInvocationsWereLate(
	missingInvocations []map[string]string,
	invokedNamesByCase map[string]map[string]struct{},
	namesMatch func(planned, invoked string) bool,
) bool {
	for _, item := range missingInvocations {
		for invoked := range invokedNamesByCase[item["target_case_id"]] {
			if namesMatch(item["runtime_name"], invoked) {
				return true
			}
		}
	}
	return false
}

// copyTreePreserve 递归复制目录（Python: shutil.copytree——copyTree 为 Task 2 既有实现）。
func copyTreePreserve(source, destination string) error {
	return copyTree(source, destination)
}

// copyFilePreserve 复制文件（Python: shutil.copy2——保留 mtime）。
func copyFilePreserve(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.WriteFile(destination, data, 0o644); err != nil {
		return err
	}
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return err
	}
	return os.Chtimes(destination, sourceInfo.ModTime(), sourceInfo.ModTime())
}

// ──────────────────────────── 报告与最终发布段（Python 4209-4405） ────────────────────────────

// buildReport 构建运行报告（Python: _build_report）。
func buildReport(state map[string]any, dataset harnessrsi.DatasetArtifact) map[string]any {
	accepted := []any{}
	analysisFailures := []any{}
	for _, rawFailure := range toAnySlice(state["analysis_failures"]) {
		analysisFailures = append(analysisFailures, rawFailure)
	}
	gates, _ := state["candidate_gates"].([]any)
	for _, rawGate := range gates {
		gate, ok := rawGate.(map[string]any)
		if !ok {
			continue
		}
		if truthy(gate["accepted"]) && toString(gate["status"]) == "accepted" {
			accepted = append(accepted, gate)
		}
		if toString(gate["candidate_failure_analysis_status"]) == "unavailable" {
			failure := map[string]any{}
			if errMap, ok := gate["candidate_failure_analysis_error"].(map[string]any); ok {
				for key, value := range errMap {
					failure[key] = value
				}
			}
			failure["epoch"] = gate["epoch"]
			failure["batch_index"] = gate["batch_index"]
			failure["batch_attempt_index"] = gate["batch_attempt_index"]
			failure["candidate_eval_ref_path"] = defaultString(gate["candidate_eval_ref_path"], "")
			failure["target_case_ids"] = toAnySlice(gate["target_case_ids"])
			analysisFailures = append(analysisFailures, failure)
		}
	}
	completedBatches := mappingOrNil(state["completed_batches"])
	improvementInstances := mappingOrNil(state["improvement_instances"])
	epochCheckpoints, _ := state["epoch_checkpoints"].([]any)
	iteration := len(epochCheckpoints)
	usage := state["usage"]
	promotionPolicy := mappingOrNil(state["promotion_policy"])
	leverScoreboard := mappingOrNil(state["lever_scoreboard"])
	return map[string]any{
		"task_id":                        defaultString(state["task_id"], ""),
		"mode":                           "single_harness_benchmark",
		"status":                         state["status"],
		"max_iteration":                  state["max_iteration"],
		"iteration":                      iteration,
		"usage":                          usage,
		"model_calls_path":               defaultString(state["model_calls_path"], ""),
		"dataset_id":                     dataset.DatasetID,
		"dataset_cases":                  dataset.Cases,
		"allowed_action_groups":          state["allowed_action_groups"],
		"allowed_prompt_surfaces":        state["allowed_prompt_surfaces"],
		"completed_batch_count":          len(completedBatches),
		"candidate_count":                len(gates),
		"accepted_candidate_count":       len(accepted),
		"analysis_unavailable_count":     len(analysisFailures),
		"analysis_failures":              analysisFailures,
		"baseline_score":                 state["baseline_score"],
		"baseline_eval_ref_path":         defaultString(state["baseline_eval_ref_path"], ""),
		"best_score":                     state["best_score"],
		"best_reference_score":           state["best_reference_score"],
		"best_reference_eval_ref_path":   state["best_reference_eval_ref_path"],
		"historical_peak_score":          state["historical_peak_score"],
		"promotion_policy":               promotionPolicy,
		"best_improvement_proof":         state["best_improvement_proof"],
		"best_eval_ref_path":             defaultString(state["best_eval_ref_path"], ""),
		"best_harness_refs_path":         state["best_harness_refs_path"],
		"published_harness_refs_path":    state["published_harness_refs_path"],
		"publication_status":             defaultString(state["publication_status"], "not_published"),
		"current_harness_refs_path":      state["current_harness_refs_path"],
		"working_harness_refs_path":      state["working_harness_refs_path"],
		"retained_case_ids":              state["retained_case_ids"],
		"epoch_checkpoints":              state["epoch_checkpoints"],
		"candidate_gates":                state["candidate_gates"],
		"improvement_instances":          improvementInstances,
		"candidate_feedback_ledger_path": defaultString(state["candidate_feedback_ledger_path"], ""),
		"improver_policy":                state["improver_policy"],
		"optimization_journal_path":      defaultString(state["optimization_journal_path"], ""),
		"lever_scoreboard_path":          defaultString(state["lever_scoreboard_path"], ""),
		"lever_scoreboard":               leverScoreboard,
	}
}

// ensureFinalPublication 把最佳受门 harness 复制到稳定的独立发布位置。
// Python: _ensure_final_publication。
func ensureFinalPublication(state map[string]any, outputDir string) error {
	resolvedOutputDir, err := resolvePath(outputDir)
	if err != nil {
		return err
	}
	acceptedGates := []map[string]any{}
	for _, rawGate := range toAnySlice(state["candidate_gates"]) {
		gate, ok := rawGate.(map[string]any)
		if !ok {
			continue
		}
		if truthy(gate["accepted"]) && toString(gate["status"]) == "accepted" {
			acceptedGates = append(acceptedGates, gate)
		}
	}
	policyVersion := toString(mappingOrNil(state["promotion_policy"])["policy_version"])
	continuous := policyVersion == "continuous_margin_v1" || policyVersion == "continuous_two_stage_v2"
	bestIdentity := ""
	if continuous {
		proof := mappingOrNil(state["best_improvement_proof"])
		bestIdentity, err = PackageFingerprint(toString(state["best_harness_refs_path"]))
		if err != nil {
			return err
		}
		evaluationField := "confirmation_eval_ref_path"
		if policyVersion == "continuous_two_stage_v2" {
			evaluationField = "full_eval_ref_path"
		}
		matchingGates := []map[string]any{}
		for _, gate := range acceptedGates {
			sameRefs := gate["candidate_harness_refs_path"] == state["best_harness_refs_path"]
			sameEval := gate[evaluationField] == state["best_eval_ref_path"]
			sameProof := anyEqual(gate["improvement_proof"], proof)
			qualified := proof["qualified"] == true
			samePackage := toString(proof["package_fingerprint"]) == bestIdentity
			if sameRefs && sameEval && sameProof && qualified && samePackage {
				matchingGates = append(matchingGates, gate)
			}
		}
		acceptedGates = matchingGates
		if policyVersion == "continuous_two_stage_v2" {
			engineeringGates := []map[string]any{}
			for _, gate := range acceptedGates {
				engineeringOK := gate["full_engineering_passed"] == true
				checksOK := mappingOrNil(gate["full_checks"])["accepted"] == true
				targetScope := toString(proof["scope"]) == "target_cases"
				if engineeringOK && checksOK && targetScope {
					engineeringGates = append(engineeringGates, gate)
				}
			}
			acceptedGates = engineeringGates
		}
		sourceIdentity, err := PackageFingerprint(toString(state["source_harness_refs_path"]))
		if err != nil {
			return err
		}
		if bestIdentity == sourceIdentity {
			acceptedGates = nil
		}
	}
	if len(acceptedGates) == 0 {
		state["published_harness_refs_path"] = ""
		state["publication_status"] = "not_published_no_improvement"
		return nil
	}

	existingRef := expandUser(toString(state["published_harness_refs_path"]))
	if fileExists(existingRef) {
		if err := validateSingleHarnessRefs(existingRef); err != nil {
			return err
		}
		existingPayload, err := readYAMLFile(existingRef)
		if err != nil {
			return err
		}
		existingPackages := []string{}
		for _, rawPath := range mappingOrNil(existingPayload["harness_refs"]) {
			if resolved, err := resolvePath(filepath.Join(filepath.Dir(existingRef), toString(rawPath))); err == nil {
				existingPackages = append(existingPackages, resolved)
			}
		}
		existingResolved, err := resolvePath(existingRef)
		if err != nil {
			return err
		}
		withinOutput := strings.HasPrefix(existingResolved+string(filepath.Separator), resolvedOutputDir+string(filepath.Separator)) ||
			existingResolved == resolvedOutputDir
		allPackagesWithin := len(existingPackages) > 0
		for _, pkg := range existingPackages {
			if !strings.HasPrefix(pkg+string(filepath.Separator), resolvedOutputDir+string(filepath.Separator)) {
				allPackagesWithin = false
				break
			}
		}
		if withinOutput && allPackagesWithin {
			if !continuous {
				state["publication_status"] = "published"
				return nil
			}
			existingIdentity, err := PackageFingerprint(existingRef)
			if err != nil {
				return err
			}
			if existingIdentity == bestIdentity {
				state["publication_status"] = "published"
				return nil
			}
		}
	}

	bestRefsPath, err := resolvePath(toString(state["best_harness_refs_path"]))
	if err != nil {
		return err
	}
	bestPayload, err := readYAMLFile(bestRefsPath)
	if err != nil {
		return err
	}
	harnessRefs, ok := bestPayload["harness_refs"].(map[string]any)
	if !ok || len(harnessRefs) == 0 {
		return errors.New("Cannot publish final single harness: best refs contain no harness_refs")
	}

	memberOutputRoot := filepath.Join(resolvedOutputDir, "member_optimizations")
	// 优化器 worktree 可能使用短兄弟 mh/ 目录。最终包必须留在 run/ 内以满足安装边界。
	pathLayout := memberoptimizer.NewMemberOptimizerPathLayout(memberOutputRoot)
	pathLayout.RuntimeRoot = filepath.Join(resolvedOutputDir, "published")
	publishedRefs := map[string]any{}
	rolesSorted := make([]string, 0, len(harnessRefs))
	for role := range harnessRefs {
		rolesSorted = append(rolesSorted, role)
	}
	sort.Strings(rolesSorted)
	for _, role := range rolesSorted {
		source, err := resolvePath(toString(harnessRefs[role]))
		if err != nil {
			return err
		}
		if info, err := os.Stat(source); err != nil || !info.IsDir() {
			return fmt.Errorf(
				"Cannot publish final single harness role '%s': source package does not exist: %s",
				role, source)
		}
		destination := pathLayout.CurrentHarnessDir(role)
		staging := filepath.Join(filepath.Dir(destination), filepath.Base(destination)+".publish_tmp")
		for _, target := range []string{destination, staging} {
			resolved, err := filepath.Abs(target)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(resolved+string(filepath.Separator), resolvedOutputDir+string(filepath.Separator)) {
				return errors.New("Cannot publish final single harness outside its run directory")
			}
		}
		if err := pathLayout.WriteRoleMapping(role); err != nil {
			return err
		}
		if _, err := os.Stat(staging); err == nil {
			if err := os.RemoveAll(staging); err != nil {
				return err
			}
		}
		if err := copyTreePreserve(source, staging); err != nil {
			return err
		}
		manifest := filepath.Join(staging, "manifest.json")
		if fileExists(manifest) {
			if _, err := hresources.LoadPluginPackage(manifest); err != nil {
				return err
			}
		}
		if _, err := os.Stat(destination); err == nil {
			if err := os.RemoveAll(destination); err != nil {
				return err
			}
		}
		if err := os.Rename(staging, destination); err != nil {
			return err
		}
		publishedRefs[role] = destination
	}

	publishedPayload := map[string]any{}
	for key, value := range bestPayload {
		publishedPayload[key] = value
	}
	publishedRoles := make([]string, 0, len(publishedRefs))
	for role := range publishedRefs {
		publishedRoles = append(publishedRoles, role)
	}
	sort.Strings(publishedRoles)
	publishedRolesAny := []any{}
	for _, role := range publishedRoles {
		publishedRolesAny = append(publishedRolesAny, role)
	}
	publishedRoleResults := map[string]any{}
	for _, role := range publishedRoles {
		publishedRoleResults[role] = map[string]any{
			"status":                  "published",
			"before_harness_ref_path": toString(harnessRefs[role]),
			"after_harness_ref_path":  publishedRefs[role],
		}
	}
	roleIDentities := []any{}
	for _, rawRole := range toAnySlice(bestPayload["roles"]) {
		rawRoleMap, ok := rawRole.(map[string]any)
		if !ok {
			continue
		}
		roleIdentity := map[string]any{}
		for key, value := range rawRoleMap {
			roleIdentity[key] = value
		}
		roleName := defaultString(roleIdentity["member_name"], toString(roleIdentity["role"]))
		if published, ok := publishedRefs[roleName]; ok {
			roleIdentity["harness_ref_path"] = published
		}
		roleIDentities = append(roleIDentities, roleIdentity)
	}
	publishedPayload["source_harness_refs_path"] = bestRefsPath
	publishedPayload["harness_refs"] = publishedRefs
	publishedPayload["published_roles"] = publishedRolesAny
	publishedPayload["candidate_ready_roles"] = publishedRolesAny
	publishedPayload["staged_roles"] = publishedRolesAny
	publishedPayload["verified_roles"] = publishedRolesAny
	publishedPayload["promoted_roles"] = publishedRolesAny
	publishedPayload["promotion_status"] = "published"
	publishedPayload["role_results"] = publishedRoleResults
	publishedPayload["last_attempt"] = map[string]any{
		"published_roles": publishedRolesAny,
		"failed_roles":    []any{},
		"skipped_roles":   []any{},
		"role_results":    publishedRoleResults,
	}
	publishedPayload["published_from_harness_refs_path"] = bestRefsPath
	publishedPayload["published_best_score"] = state["best_score"]
	publishedPayload["promotion_policy"] = state["promotion_policy"]
	publishedPayload["improvement_proof"] = state["best_improvement_proof"]
	if len(roleIDentities) > 0 {
		publishedPayload["roles"] = roleIDentities
	}
	publishedRefPath := filepath.Join(memberOutputRoot, "current_harness_refs.yaml")
	if err := writeYAMLAtomic(publishedRefPath, publishedPayload); err != nil {
		return err
	}
	if err := validateSingleHarnessRefs(publishedRefPath); err != nil {
		return err
	}
	state["published_harness_refs_path"] = publishedRefPath
	state["publication_status"] = "published"
	return nil
}

// ──────────────────────────── 评估信号与用量段（Python 2821-3400） ────────────────────────────

// evalCaseScores 用例分数映射（Python: _eval_case_scores）。
func evalCaseScores(evalRefPath string) (map[string]float64, error) {
	payload, err := readYAMLFile(evalRefPath)
	if err != nil {
		return nil, err
	}
	scores := map[string]float64{}
	for _, rawCase := range toAnySlice(payload["cases"]) {
		caseMap, ok := rawCase.(map[string]any)
		if !ok {
			continue
		}
		caseID := toString(caseMap["case_id"])
		score := numberOf(caseMap["score"])
		if caseID != "" && score != nil {
			scores[caseID] = *score
		}
	}
	return scores, nil
}

// evalCaseNativeSignals 读评估器通用搜索信号，回退严格分（Python: _eval_case_native_signals）。
func evalCaseNativeSignals(evalRefPath string) (map[string]map[string]any, error) {
	payload, err := readYAMLFile(evalRefPath)
	if err != nil {
		return nil, err
	}
	signals := map[string]map[string]any{}
	for _, rawCase := range toAnySlice(payload["cases"]) {
		caseMap, ok := rawCase.(map[string]any)
		if !ok {
			continue
		}
		caseID := toString(caseMap["case_id"])
		fallbackScore := numberOf(caseMap["score"])
		if caseID == "" {
			continue
		}
		result := readJSONFile(toString(caseMap["result_path"]))
		evaluation := mappingOrNil(result["evaluation"])
		metadata := mappingOrNil(evaluation["metadata"])
		contract := evaluatorpkg.EvaluationOptimizationSignals(metadata)
		continuous := mappingOrNil(contract["continuous_score"])
		var nativeScore *float64
		if toString(continuous["availability"]) == "available" {
			nativeScore = numberOf(continuous["value"])
		}
		source := toString(continuous["source"])
		dimensions := map[string]any{}
		for name, rawDimension := range mappingOrNil(contract["dimensions"]) {
			dimension, ok := rawDimension.(map[string]any)
			if !ok || toString(dimension["availability"]) != "available" {
				continue
			}
			if score := numberOf(dimension["value"]); score != nil {
				dimensions[toString(name)] = *score
			}
		}
		if nativeScore == nil {
			nativeScore = fallbackScore
			source = "eval_ref_score_fallback"
		}
		if nativeScore == nil {
			continue
		}
		signals[caseID] = map[string]any{
			"score":      *nativeScore,
			"source":     source,
			"dimensions": dimensions,
		}
	}
	return signals, nil
}

// nativeCaseScores 原生分映射（Python: _native_case_scores）。
func nativeCaseScores(signals map[string]map[string]any) map[string]float64 {
	scores := map[string]float64{}
	for caseID, signal := range signals {
		if score := numberOf(signal["score"]); score != nil {
			scores[caseID] = *score
		}
	}
	return scores
}

// pairedScoreDelta 配对分差（Python: _paired_score_delta）。
func pairedScoreDelta(sourceScores, candidateScores map[string]float64, caseID string) *float64 {
	sourceRaw, hasSource := sourceScores[caseID]
	candidateRaw, hasCandidate := candidateScores[caseID]
	sourceScore := numberOf(numberToAny(sourceRaw, hasSource))
	candidateScore := numberOf(numberToAny(candidateRaw, hasCandidate))
	if sourceScore == nil || candidateScore == nil {
		return nil
	}
	delta := *candidateScore - *sourceScore
	return &delta
}

// numberToAny 有值转 any、缺失转 nil（配合 numberOf 的缺失语义）。
func numberToAny(value float64, present bool) any {
	if !present {
		return nil
	}
	return value
}

// nativeDimensionDeltas 原生维度差（Python: _native_dimension_deltas）。
func nativeDimensionDeltas(
	sourceSignals, candidateSignals map[string]map[string]any,
	targetCaseIDSet map[string]struct{},
) (map[string]map[string]float64, *float64) {
	byCase := map[string]map[string]float64{}
	var allDeltas []float64
	for _, caseID := range sortedStringSet(targetCaseIDSet) {
		sourceDimensions := mappingOrNil(sourceSignals[caseID]["dimensions"])
		candidateDimensions := mappingOrNil(candidateSignals[caseID]["dimensions"])
		caseDeltas := map[string]float64{}
		names := sortedStringSet(stringSetIntersection(mappingKeys(sourceDimensions), mappingKeys(candidateDimensions)))
		for _, name := range names {
			sourceScore := numberOf(sourceDimensions[name])
			candidateScore := numberOf(candidateDimensions[name])
			if sourceScore == nil || candidateScore == nil {
				continue
			}
			delta := *candidateScore - *sourceScore
			caseDeltas[name] = delta
			allDeltas = append(allDeltas, delta)
		}
		if len(caseDeltas) > 0 {
			byCase[caseID] = caseDeltas
		}
	}
	var aggregate *float64
	if len(allDeltas) > 0 {
		total := 0.0
		for _, delta := range allDeltas {
			total += delta
		}
		mean := total / float64(len(allDeltas))
		aggregate = &mean
	}
	return byCase, aggregate
}

// evalOfficialMetrics 官方指标（Python: _eval_official_metrics）。
func evalOfficialMetrics(evalRefPath string) (map[string]any, error) {
	payload, err := readYAMLFile(evalRefPath)
	if err != nil {
		return nil, err
	}
	if metrics, ok := payload["official_metrics"].(map[string]any); ok {
		return metrics, nil
	}
	return map[string]any{}, nil
}

// epochFullSourceEvalRef 找覆盖全部冻结用例的 epoch 源评估（Python: _epoch_full_source_eval_ref）。
func epochFullSourceEvalRef(
	state map[string]any,
	epoch int,
	expectedCaseIDs map[string]struct{},
) string {
	completedBatches := mappingOrNil(state["completed_batches"])
	prefix := fmt.Sprintf("epoch_%03d:", epoch)
	keys := make([]string, 0, len(completedBatches))
	for key := range completedBatches {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		record, ok := completedBatches[key].(map[string]any)
		if !ok {
			continue
		}
		candidate := toString(record["source_eval_ref_path"])
		if candidate == "" {
			continue
		}
		caseScores, err := evalCaseScores(candidate)
		if err != nil {
			continue
		}
		if stringSetEqual(mappingStringKeys(caseScores), expectedCaseIDs) {
			return candidate
		}
	}
	return ""
}

// evalVerifierStatuses 读数据集中立的逐需求结果（Python: _eval_verifier_statuses）。
func evalVerifierStatuses(evalRefPath string) (map[string]map[string]any, error) {
	payload, err := readYAMLFile(evalRefPath)
	if err != nil {
		return nil, err
	}
	statuses := map[string]map[string]any{}
	for _, rawCase := range toAnySlice(payload["cases"]) {
		caseMap, ok := rawCase.(map[string]any)
		if !ok {
			continue
		}
		caseID := toString(caseMap["case_id"])
		if caseID == "" {
			continue
		}
		result := readJSONFile(toString(caseMap["result_path"]))
		evaluation := mappingOrNil(result["evaluation"])
		metadata := mappingOrNil(evaluation["metadata"])
		requirements := evaluatorpkg.EvaluationRequirementResults(metadata, caseID)
		instanceReport := mappingOrNil(metadata["instance_report"])
		report := mappingOrNil(instanceReport[caseID])
		if len(report) == 0 && len(instanceReport) == 1 {
			for _, only := range instanceReport {
				report = mappingOrNil(only)
				break
			}
		}
		statuses[caseID] = map[string]any{
			"patch_successfully_applied": report["patch_successfully_applied"],
			"resolved":                   report["resolved"],
			"empty_patch":                metadata["empty_patch"],
			"requirement_results":        requirements,
		}
	}
	return statuses, nil
}

// verifierDeltasByCase 配对官方测试操作逐用例对比（Python: _verifier_deltas_by_case）。
func verifierDeltasByCase(
	sourceEvalRef, candidateEvalRef string,
	targetCaseIDSet map[string]struct{},
) (map[string]map[string]any, error) {
	source, err := evalVerifierStatuses(sourceEvalRef)
	if err != nil {
		return nil, err
	}
	candidate, err := evalVerifierStatuses(candidateEvalRef)
	if err != nil {
		return nil, err
	}
	deltas := map[string]map[string]any{}
	for _, caseID := range sortedStringSet(targetCaseIDSet) {
		sourceStatus := mappingOrNil(source[caseID])
		candidateStatus := mappingOrNil(candidate[caseID])
		sourceRequirements := requirementResultIndex(sourceStatus["requirement_results"])
		candidateRequirements := requirementResultIndex(candidateStatus["requirement_results"])
		sourcePassed := requirementKeysWithPassed(sourceRequirements, true)
		candidatePassed := requirementKeysWithPassed(candidateRequirements, true)
		candidateFailed := requirementKeysWithPassed(candidateRequirements, false)
		newlyPassedKeys := keySetDiff(candidatePassed, sourcePassed)
		regressedKeys := keySetIntersection(sourcePassed, candidateFailed)

		sourceF2PSuccess := requirementIDsWithGroup(sourcePassed, evaluatorpkg.FailToPassGroup)
		candidateF2PSuccess := requirementIDsWithGroup(candidatePassed, evaluatorpkg.FailToPassGroup)
		candidateF2PFailure := requirementIDsWithGroup(candidateFailed, evaluatorpkg.FailToPassGroup)
		newlyPassed := sortedStringSetKeyDiff(candidateF2PSuccess, sourceF2PSuccess)
		regressedF2P := sortedStringSetKeyDiff(sourceF2PSuccess, candidateF2PSuccess)
		sourceP2PSuccess := requirementIDsWithGroup(sourcePassed, evaluatorpkg.PassToPassGroup)
		candidateP2PFailure := requirementIDsWithGroup(candidateFailed, evaluatorpkg.PassToPassGroup)
		regressedP2P := sortedStringSet(stringSetIntersection(sourceP2PSuccess, candidateP2PFailure))
		sourceAtomicSuccess := requirementIDsWithGroup(sourcePassed, evaluatorpkg.AtomicCheckGroup)
		candidateAtomicSuccess := requirementIDsWithGroup(candidatePassed, evaluatorpkg.AtomicCheckGroup)
		candidateAtomicFailure := requirementIDsWithGroup(candidateFailed, evaluatorpkg.AtomicCheckGroup)
		newlyPassedAtomic := sortedStringSetKeyDiff(candidateAtomicSuccess, sourceAtomicSuccess)
		regressedAtomic := sortedStringSetKeyDiff(sourceAtomicSuccess, candidateAtomicSuccess)
		newlyPassedRequirements := requirementSnapshots(candidateRequirements, newlyPassedKeys)
		remainingFailedRequirements := requirementSnapshots(candidateRequirements, candidateFailed)
		regressedRequirements := requirementSnapshots(candidateRequirements, regressedKeys)
		improvedScoreKeys := map[evaluatorpkg.RequirementKey]struct{}{}
		regressedScoreKeys := map[evaluatorpkg.RequirementKey]struct{}{}
		for key := range sourceRequirements {
			if _, both := candidateRequirements[key]; !both {
				continue
			}
			before := numberOf(sourceRequirements[key]["score"])
			after := numberOf(candidateRequirements[key]["score"])
			if before == nil || after == nil {
				continue
			}
			if *after < *before {
				regressedScoreKeys[key] = struct{}{}
			} else if *after > *before && strings.TrimSpace(toString(candidateRequirements[key]["evidence"])) != "" {
				improvedScoreKeys[key] = struct{}{}
			}
		}
		improvedRequirementScores := requirementSnapshots(candidateRequirements, improvedScoreKeys)
		regressedRequirementScores := requirementSnapshots(candidateRequirements, regressedScoreKeys)
		partialProgress := (len(newlyPassedRequirements) > 0 || len(improvedRequirementScores) > 0) &&
			len(remainingFailedRequirements) > 0 &&
			len(regressedRequirements) == 0 &&
			len(regressedRequirementScores) == 0
		requirementValues := func(index map[evaluatorpkg.RequirementKey]map[string]any) []any {
			out := []any{}
			for _, key := range requirementKeyList(index) {
				out = append(out, index[key])
			}
			return out
		}
		deltas[caseID] = map[string]any{
			"source_requirement_results":           requirementValues(sourceRequirements),
			"candidate_requirement_results":        requirementValues(candidateRequirements),
			"newly_passed_requirements":            newlyPassedRequirements,
			"remaining_failed_requirements":        remainingFailedRequirements,
			"regressed_requirements":               regressedRequirements,
			"improved_requirement_scores":          improvedRequirementScores,
			"regressed_requirement_scores":         regressedRequirementScores,
			"source_fail_to_pass_success":          sortedStringSet(sourceF2PSuccess),
			"candidate_fail_to_pass_success":       sortedStringSet(candidateF2PSuccess),
			"newly_passed_fail_to_pass":            newlyPassed,
			"remaining_failed_fail_to_pass":        sortedStringSet(candidateF2PFailure),
			"regressed_fail_to_pass":               regressedF2P,
			"regressed_pass_to_pass":               regressedP2P,
			"source_passed_atomic_checks":          sortedStringSet(sourceAtomicSuccess),
			"candidate_passed_atomic_checks":       sortedStringSet(candidateAtomicSuccess),
			"newly_passed_atomic_checks":           newlyPassedAtomic,
			"remaining_failed_atomic_checks":       sortedStringSet(candidateAtomicFailure),
			"regressed_atomic_checks":              regressedAtomic,
			"partial_progress":                     partialProgress,
			"source_patch_successfully_applied":    sourceStatus["patch_successfully_applied"],
			"candidate_patch_successfully_applied": candidateStatus["patch_successfully_applied"],
			"candidate_empty_patch":                candidateStatus["empty_patch"],
		}
	}
	return deltas, nil
}

// keySetDiff 需求键差集。
func keySetDiff(minuend, subtrahend map[evaluatorpkg.RequirementKey]struct{}) map[evaluatorpkg.RequirementKey]struct{} {
	out := map[evaluatorpkg.RequirementKey]struct{}{}
	for key := range minuend {
		if _, ok := subtrahend[key]; !ok {
			out[key] = struct{}{}
		}
	}
	return out
}

// keySetIntersection 需求键交集。
func keySetIntersection(left, right map[evaluatorpkg.RequirementKey]struct{}) map[evaluatorpkg.RequirementKey]struct{} {
	out := map[evaluatorpkg.RequirementKey]struct{}{}
	for key := range left {
		if _, ok := right[key]; ok {
			out[key] = struct{}{}
		}
	}
	return out
}

// stringSetKeyDiff 字符串集合差集（保留集合形态，排序由调用方 sortedStringSet 完成）。
func stringSetKeyDiff(minuend, subtrahend map[string]struct{}) map[string]struct{} {
	out := map[string]struct{}{}
	for value := range minuend {
		if _, ok := subtrahend[value]; !ok {
			out[value] = struct{}{}
		}
	}
	return out
}

// requirementKeyList 索引键排序列表。
func requirementKeyList(index map[evaluatorpkg.RequirementKey]map[string]any) []evaluatorpkg.RequirementKey {
	keys := make([]evaluatorpkg.RequirementKey, 0, len(index))
	for key := range index {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Group != keys[j].Group {
			return keys[i].Group < keys[j].Group
		}
		return keys[i].RequirementID < keys[j].RequirementID
	})
	return keys
}

// requirementResultIndex 需求结果索引（Python: _requirement_result_index）。
func requirementResultIndex(value any) map[evaluatorpkg.RequirementKey]map[string]any {
	index := map[evaluatorpkg.RequirementKey]map[string]any{}
	var rawList []any
	switch typed := value.(type) {
	case []any:
		rawList = typed
	case []map[string]any:
		rawList = make([]any, 0, len(typed))
		for _, item := range typed {
			rawList = append(rawList, item)
		}
	default:
		return index
	}
	for _, raw := range rawList {
		rawMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		requirementID := strings.TrimSpace(toString(rawMap["requirement_id"]))
		group := strings.TrimSpace(defaultString(rawMap["group"], "requirement"))
		if group == "" {
			group = "requirement"
		}
		if requirementID != "" {
			cloned := map[string]any{}
			for k, v := range rawMap {
				cloned[k] = v
			}
			index[evaluatorpkg.RequirementKey{Group: group, RequirementID: requirementID}] = cloned
		}
	}
	return index
}

// requirementIDsWithGroup 组内需求 ID（Python: _requirement_ids）。
func requirementIDsWithGroup(
	keys map[evaluatorpkg.RequirementKey]struct{}, group string,
) map[string]struct{} {
	ids := map[string]struct{}{}
	for key := range keys {
		if key.Group == group {
			ids[key.RequirementID] = struct{}{}
		}
	}
	return ids
}

// requirementKeysWithPassed 按 passed 过滤键集。
func requirementKeysWithPassed(
	index map[evaluatorpkg.RequirementKey]map[string]any, passed bool,
) map[evaluatorpkg.RequirementKey]struct{} {
	keys := map[evaluatorpkg.RequirementKey]struct{}{}
	for key, item := range index {
		if item["passed"] == passed {
			keys[key] = struct{}{}
		}
	}
	return keys
}

// requirementSnapshots 键集快照（Python: _requirement_snapshots——排序键序）。
func requirementSnapshots(
	index map[evaluatorpkg.RequirementKey]map[string]any,
	keys map[evaluatorpkg.RequirementKey]struct{},
) []map[string]any {
	sortedKeys := sortRequirementKeys(keys)
	result := []map[string]any{}
	for _, key := range sortedKeys {
		if item, ok := index[key]; ok {
			cloned := map[string]any{}
			for k, v := range item {
				cloned[k] = v
			}
			result = append(result, cloned)
		}
	}
	return result
}

// sortRequirementKeys 需求键排序。
func sortRequirementKeys(keys map[evaluatorpkg.RequirementKey]struct{}) []evaluatorpkg.RequirementKey {
	out := make([]evaluatorpkg.RequirementKey, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].RequirementID < out[j].RequirementID
	})
	return out
}

// candidatePatchExcerptsByCase 候选补丁摘要（Python: _candidate_patch_excerpts_by_case）。
func candidatePatchExcerptsByCase(
	evalRefPath string,
	targetCaseIDSet map[string]struct{},
) (map[string]string, error) {
	payload, err := readYAMLFile(evalRefPath)
	if err != nil {
		return nil, err
	}
	excerpts := map[string]string{}
	for _, rawCase := range toAnySlice(payload["cases"]) {
		caseMap, ok := rawCase.(map[string]any)
		if !ok {
			continue
		}
		caseID := toString(caseMap["case_id"])
		if _, isTarget := targetCaseIDSet[caseID]; !isTarget {
			continue
		}
		result := readJSONFile(toString(caseMap["result_path"]))
		evaluation := mappingOrNil(result["evaluation"])
		metadata := mappingOrNil(evaluation["metadata"])
		patchPath := expandUser(toString(metadata["model_patch_path"]))
		if !fileExists(patchPath) {
			excerpts[caseID] = ""
			continue
		}
		raw, err := os.ReadFile(patchPath)
		patch := ""
		if err == nil {
			patch = string(raw)
		}
		excerpts[caseID] = boundedCandidateText(patch)
	}
	return excerpts, nil
}

// candidateInterventionExcerptsByCase 候选干预摘要（Python: _candidate_intervention_excerpts_by_case）。
func candidateInterventionExcerptsByCase(
	capabilities []map[string]any,
	targetCaseIDSet map[string]struct{},
) map[string]string {
	excerpts := map[string][]string{}
	for caseID := range targetCaseIDSet {
		excerpts[caseID] = []string{}
	}
	for _, capability := range capabilities {
		if capability == nil {
			continue
		}
		intervention := strings.TrimSpace(toString(capability["intervention"]))
		if intervention == "" {
			continue
		}
		capabilityCaseIDs := map[string]struct{}{}
		for _, rawCaseID := range toAnySlice(capability["target_case_ids"]) {
			if caseID := toString(rawCaseID); caseID != "" {
				capabilityCaseIDs[caseID] = struct{}{}
			}
		}
		applicable := capabilityCaseIDs
		if len(applicable) == 0 {
			applicable = targetCaseIDSet
		}
		applicable = stringSetIntersection(applicable, targetCaseIDSet)
		labelParts := []string{
			toString(capability["action_group"]),
			toString(capability["target_path"]),
		}
		nonEmpty := []string{}
		for _, part := range labelParts {
			if part != "" {
				nonEmpty = append(nonEmpty, part)
			}
		}
		label := strings.Join(nonEmpty, ":")
		block := intervention
		if label != "" {
			block = fmt.Sprintf("[%s]\n%s", label, intervention)
		}
		for caseID := range applicable {
			excerpts[caseID] = append(excerpts[caseID], block)
		}
	}
	result := map[string]string{}
	for caseID, blocks := range excerpts {
		if len(blocks) > 0 {
			result[caseID] = boundedCandidateText(strings.Join(blocks, "\n\n"))
		}
	}
	return result
}

// boundedCandidateText 截断候选文本（Python: _bounded_candidate_text，limit 默认 6000）。
func boundedCandidateText(value string, limits ...int) string {
	limit := 6000
	if len(limits) > 0 {
		limit = limits[0]
	}
	if len(value) <= limit {
		return value
	}
	half := limit / 2
	return fmt.Sprintf("%s\n... candidate patch omitted ...\n%s", value[:half], value[len(value)-half:])
}

// averageCaseScores 平均分（Python: _average_case_scores）。
func averageCaseScores(scores map[string]float64, caseIDSet map[string]struct{}) float64 {
	values := []float64{}
	for caseID := range caseIDSet {
		if score, ok := scores[caseID]; ok {
			values = append(values, score)
		}
	}
	if len(values) == 0 {
		return 0.0
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

// capabilityTargetCaseIDs 能力目标用例（Python: _capability_target_case_ids）。
func capabilityTargetCaseIDs(
	capabilities []map[string]any,
	fallbackTargetCaseIDs map[string]struct{},
) map[string]struct{} {
	targetCaseIDs := map[string]struct{}{}
	for _, capability := range capabilities {
		operation := toString(capability["operation"])
		if operation != "add" && operation != "modify" {
			continue
		}
		for _, rawCaseID := range toAnySlice(capability["target_case_ids"]) {
			if caseID := toString(rawCaseID); caseID != "" {
				targetCaseIDs[caseID] = struct{}{}
			}
		}
	}
	if len(targetCaseIDs) > 0 {
		return targetCaseIDs
	}
	result := map[string]struct{}{}
	for caseID := range fallbackTargetCaseIDs {
		result[caseID] = struct{}{}
	}
	return result
}

// errorCaseIDs 错误用例集（Python: _error_case_ids）。
func errorCaseIDs(evalRefPath string) (map[string]struct{}, error) {
	payload, err := readYAMLFile(evalRefPath)
	if err != nil {
		return nil, err
	}
	errorSet := map[string]struct{}{}
	for _, rawCase := range toAnySlice(payload["cases"]) {
		caseMap, ok := rawCase.(map[string]any)
		if !ok {
			continue
		}
		caseID := toString(caseMap["case_id"])
		if caseID != "" && strings.ToLower(toString(caseMap["status"])) == "error" {
			errorSet[caseID] = struct{}{}
		}
	}
	return errorSet, nil
}

// evalHasErrors 是否存在错误用例（Python: _eval_has_errors）。
func evalHasErrors(evalRefPath string) bool {
	errorSet, err := errorCaseIDs(evalRefPath)
	return err == nil && len(errorSet) > 0
}

// skippedCaseIDs 跳过用例集（Python: _skipped_case_ids）。
func skippedCaseIDs(evalRefPath string) (map[string]struct{}, error) {
	payload, err := readYAMLFile(evalRefPath)
	if err != nil {
		return nil, err
	}
	skipped := map[string]struct{}{}
	for _, rawCase := range toAnySlice(payload["cases"]) {
		caseMap, ok := rawCase.(map[string]any)
		if !ok {
			continue
		}
		caseID := toString(caseMap["case_id"])
		metadata := mappingOrNil(caseMap["metadata"])
		isSkipped := strings.ToLower(toString(caseMap["status"])) == "skipped" ||
			metadata["infrastructure_skip"] == true
		if caseID != "" && isSkipped {
			skipped[caseID] = struct{}{}
		}
	}
	return skipped, nil
}

// invokedToolNames 调用过的工具名全集（Python: _invoked_tool_names）。
func invokedToolNames(evalRefPath string) (map[string]struct{}, error) {
	invoked := map[string]struct{}{}
	namesByCase, err := invokedToolNamesByCase(evalRefPath)
	if err != nil {
		return nil, err
	}
	for _, names := range namesByCase {
		for name := range names {
			invoked[name] = struct{}{}
		}
	}
	return invoked, nil
}

// invokedToolNamesByCase 逐用例工具名（Python: _invoked_tool_names_by_case）。
func invokedToolNamesByCase(evalRefPath string) (map[string]map[string]struct{}, error) {
	namesByCase := map[string]map[string]struct{}{}
	payload, err := readYAMLFile(evalRefPath)
	if err != nil {
		return nil, err
	}
	for _, rawCase := range toAnySlice(payload["cases"]) {
		caseMap, ok := rawCase.(map[string]any)
		if !ok {
			continue
		}
		caseID := toString(caseMap["case_id"])
		if caseID == "" {
			continue
		}
		names, ok := namesByCase[caseID]
		if !ok {
			names = map[string]struct{}{}
			namesByCase[caseID] = names
		}
		if len(taskStartTriggeredSkillNames(caseMap)) > 0 {
			names["skill_tool"] = struct{}{}
		}
		for _, trace := range caseUsageTraces(caseMap) {
			evaluatorpkg.CollectSuccessfulToolNames(trace, names)
			collectTrajectoryDirUsage(trace, names, nil)
		}
	}
	return namesByCase, nil
}

// invokedSkillNames 调用过的技能名全集（Python: _invoked_skill_names）。
func invokedSkillNames(evalRefPath string) (map[string]struct{}, error) {
	invoked := map[string]struct{}{}
	namesByCase, err := invokedSkillNamesByCase(evalRefPath)
	if err != nil {
		return nil, err
	}
	for _, names := range namesByCase {
		for name := range names {
			invoked[name] = struct{}{}
		}
	}
	return invoked, nil
}

// invokedSkillNamesByCase 逐用例技能名（Python: _invoked_skill_names_by_case）。
func invokedSkillNamesByCase(evalRefPath string) (map[string]map[string]struct{}, error) {
	namesByCase := map[string]map[string]struct{}{}
	payload, err := readYAMLFile(evalRefPath)
	if err != nil {
		return nil, err
	}
	for _, rawCase := range toAnySlice(payload["cases"]) {
		caseMap, ok := rawCase.(map[string]any)
		if !ok {
			continue
		}
		caseID := toString(caseMap["case_id"])
		if caseID == "" {
			continue
		}
		names, ok := namesByCase[caseID]
		if !ok {
			names = map[string]struct{}{}
			namesByCase[caseID] = names
		}
		for name := range taskStartTriggeredSkillNames(caseMap) {
			names[name] = struct{}{}
		}
		for _, trace := range caseUsageTraces(caseMap) {
			evaluatorpkg.CollectSuccessfulSkillNames(trace, names)
			collectTrajectoryDirUsage(trace, nil, names)
		}
	}
	return namesByCase, nil
}

// preEditInvokedNamesByCase 首次持久编辑前完成的能力调用（Python: _pre_edit_invoked_names_by_case）。
func preEditInvokedNamesByCase(
	evalRefPath string,
	actionGroup string,
	allowLaterEdit bool,
) (map[string]map[string]struct{}, map[string]int, error) {
	namesByCase := map[string]map[string]struct{}{}
	firstEditStepsByCase := map[string]int{}
	payload, err := readYAMLFile(evalRefPath)
	if err != nil {
		return nil, nil, err
	}
	for _, rawCase := range toAnySlice(payload["cases"]) {
		caseMap, ok := rawCase.(map[string]any)
		if !ok {
			continue
		}
		caseID := toString(caseMap["case_id"])
		if caseID == "" {
			continue
		}
		names, ok := namesByCase[caseID]
		if !ok {
			names = map[string]struct{}{}
			namesByCase[caseID] = names
		}
		triggeredSkills := taskStartTriggeredSkillNames(caseMap)
		if actionGroup == "skill" && !allowLaterEdit {
			for name := range triggeredSkills {
				names[name] = struct{}{}
			}
		} else if len(triggeredSkills) > 0 && !allowLaterEdit {
			names["skill_tool"] = struct{}{}
		}
		var toolNamesArg, skillNamesArg map[string]struct{}
		if actionGroup == "tool" {
			toolNamesArg = names
		}
		if actionGroup == "skill" {
			skillNamesArg = names
		}
		editSteps := []int{}
		for _, trace := range caseUsageTraces(caseMap) {
			if step := evaluatorpkg.CollectPreEditSuccessfulUsage(trace, toolNamesArg, skillNamesArg, allowLaterEdit); step != nil {
				editSteps = append(editSteps, *step)
			}
			trajectoryDir := expandUser(toString(trace["trajectory_dir"]))
			if info, err := os.Stat(trajectoryDir); err == nil && info.IsDir() {
				for _, memberTracePath := range evaluatorpkg.TrajectoryGlobJSONL(trajectoryDir) {
					if step := evaluatorpkg.CollectJSONLPreEditSuccessfulUsage(
						memberTracePath, toolNamesArg, skillNamesArg, allowLaterEdit); step != nil {
						editSteps = append(editSteps, *step)
					}
				}
			}
		}
		if len(editSteps) > 0 {
			minStep := editSteps[0]
			for _, step := range editSteps[1:] {
				if step < minStep {
					minStep = step
				}
			}
			firstEditStepsByCase[caseID] = minStep
			if allowLaterEdit && len(triggeredSkills) > 0 {
				if actionGroup == "skill" {
					for name := range triggeredSkills {
						names[name] = struct{}{}
					}
				} else {
					names["skill_tool"] = struct{}{}
				}
			}
		}
	}
	return namesByCase, firstEditStepsByCase, nil
}

// caseUsageTraces 适配器轨迹 + 其引用的归一化轨迹（Python: _case_usage_traces）。
func caseUsageTraces(caseMap map[string]any) []map[string]any {
	trace := readJSONFile(toString(caseMap["trace_path"]))
	traces := []map[string]any{trace}
	behavior := mappingOrNil(trace["behavior_trace"])
	normalizedPath := toString(behavior["normalized_trace_path"])
	if normalizedPath != "" {
		normalized := readJSONFile(normalizedPath)
		if len(normalized) > 0 && !anyEqual(normalized, trace) {
			traces = append(traces, normalized)
		}
	}
	return traces
}

// taskStartTriggeredSkillNames 任务开始自然派发的 Skill（Python: _task_start_triggered_skill_names）。
func taskStartTriggeredSkillNames(caseMap map[string]any) map[string]struct{} {
	result := readJSONFile(toString(caseMap["result_path"]))
	metadata := mappingOrNil(result["metadata"])
	execution := mappingOrNil(metadata["execution"])
	triggers, _ := execution["skill_triggers"].([]any)
	names := map[string]struct{}{}
	for _, rawTrigger := range triggers {
		trigger, ok := rawTrigger.(map[string]any)
		if !ok || trigger["delivered"] != true {
			continue
		}
		name := strings.TrimSpace(toString(trigger["selected_skill_name"]))
		if name != "" {
			names[name] = struct{}{}
		}
	}
	return names
}

// nonpassingCaseIDs 未通过用例集（Python: _nonpassing_case_ids）。
func nonpassingCaseIDs(evalRefPath string) (map[string]struct{}, error) {
	payload, err := readYAMLFile(evalRefPath)
	if err != nil {
		return nil, err
	}
	caseIDs := map[string]struct{}{}
	for _, rawCase := range toAnySlice(payload["cases"]) {
		caseMap, ok := rawCase.(map[string]any)
		if !ok {
			continue
		}
		caseID := toString(caseMap["case_id"])
		score := numberOf(caseMap["score"])
		status := strings.ToLower(toString(caseMap["status"]))
		metadata := mappingOrNil(caseMap["metadata"])
		if status == "skipped" || metadata["infrastructure_skip"] == true {
			continue
		}
		statusFailed := status == "failed" || status == "error"
		explicitPassed := llmCasePassed(caseMap)
		if toString(metadata["score_mode"]) == "continuous" {
			explicitPassed = nil
		} else if toString(metadata["evaluation_method"]) == "llm_as_judge" {
			result := readJSONFile(toString(caseMap["result_path"]))
			resultMetadata := mappingOrNil(mappingOrNil(result["evaluation"])["metadata"])
			if toString(resultMetadata["score_mode"]) == "continuous" {
				explicitPassed = nil
			}
		}
		scoreFailed := false
		if explicitPassed != nil {
			scoreFailed = !*explicitPassed
		} else {
			scoreFailed = score != nil && *score < 1.0
		}
		if score == nil {
			scoreFailed = evalCaseExplicitPassed(caseMap) != nil && *evalCaseExplicitPassed(caseMap) == false
		}
		if caseID != "" && (statusFailed || scoreFailed) {
			caseIDs[caseID] = struct{}{}
		}
	}
	return caseIDs, nil
}

// ──────────────────────────── 门失败分类与检查点选择段（Python 3401-3613） ────────────────────────────

// promotionScoreEpsilon 晋级分容差（Python: _PROMOTION_SCORE_EPSILON）。
const promotionScoreEpsilon = 1e-9

// classifyGateFailure 分类候选失败原因（Python: _classify_gate_failure）。
func classifyGateFailure(
	accepted bool,
	reason string,
	targetCaseIDSet map[string]struct{},
	candidateCaseScores map[string]float64,
	firstEditStepsByCase map[string]int,
	missingSkillInvocations []map[string]string,
	verifierDeltas map[string]map[string]any,
) string {
	if accepted {
		return ""
	}
	if strings.Contains(reason, "inconclusive") || strings.Contains(reason, "error_cases") {
		return "infrastructure_or_evidence_failure"
	}
	if len(missingSkillInvocations) > 0 {
		return "natural_skill_activation_failure"
	}
	for _, delta := range verifierDeltas {
		if truthy(delta["newly_passed_fail_to_pass"]) {
			return "partial_contract_progress"
		}
	}
	failedTargets := map[string]struct{}{}
	for caseID := range targetCaseIDSet {
		if candidateCaseScores[caseID] < 1.0 {
			failedTargets[caseID] = struct{}{}
		}
	}
	if len(failedTargets) > 0 && len(stringSetKeyDiffToSlice(failedTargets, firstEditStepsByCase)) > 0 {
		return "execution_convergence_failure"
	}
	if len(failedTargets) > 0 {
		return "semantic_non_reproduction"
	}
	if strings.Contains(reason, "retention") {
		return "regression_or_retention_failure"
	}
	if strings.Contains(reason, "not_invoked") || strings.Contains(reason, "invoked_outside_activation_window") {
		return "capability_activation_failure"
	}
	return "score_non_improvement"
}

// classifyReplayOutcome 对比临时目标与 epoch 重放轨迹（Python: _classify_replay_outcome）。
func classifyReplayOutcome(gate map[string]any, evalRefPath string) (map[string]any, error) {
	targetCaseIDSet := map[string]struct{}{}
	for _, rawCaseID := range toAnySlice(gate["target_case_ids"]) {
		if caseID := toString(rawCaseID); caseID != "" {
			targetCaseIDSet[caseID] = struct{}{}
		}
	}
	passing, err := passingCaseIDs(evalRefPath)
	if err != nil {
		return nil, err
	}
	failedTargetCaseIDs := sortedStringSet(stringSetKeyDiff(targetCaseIDSet, passing))
	_, firstEditSteps, err := preEditInvokedNamesByCase(evalRefPath, "skill", false)
	if err != nil {
		return nil, err
	}
	firstEditSet := map[string]struct{}{}
	for caseID := range firstEditSteps {
		firstEditSet[caseID] = struct{}{}
	}
	var failureClass string
	switch {
	case len(stringSetKeyDiff(toKeySet(failedTargetCaseIDs), firstEditSet)) > 0:
		failureClass = "execution_convergence_failure"
	case len(failedTargetCaseIDs) > 0:
		failureClass = "semantic_non_reproduction"
	default:
		failureClass = "score_non_improvement"
	}
	firstEditAny := map[string]any{}
	for caseID, step := range firstEditSteps {
		firstEditAny[caseID] = step
	}
	return map[string]any{
		"status":                             "failed",
		"eval_ref_path":                      evalRefPath,
		"failed_target_case_ids":             toAnySlice(failedTargetCaseIDs),
		"first_persistent_edit_step_by_case": firstEditAny,
		"failure_class":                      failureClass,
	}, nil
}

// selectGateFromEpochCheckpoint 从 epoch 级重放中选一个临时改动（Python: _select_gate_from_epoch_checkpoint）。
//
// epoch 重放评估的是累积 Harness；选择是候选局部的：只有当它自己的目标仍通过、
// 且其运行时能力确实被用到时才存活。无关用例结果只是审计证据，不能证明该候选
// 造成了回归。epoch 是否整体可晋级由全局晋级门另行决定。
func selectGateFromEpochCheckpoint(
	gate map[string]any,
	fullEvalRef string,
	errorCaseIDSet map[string]struct{},
	machineEvidenceSet map[string]struct{},
) (map[string]any, error) {
	targetCaseIDSet := map[string]struct{}{}
	for _, rawCaseID := range toAnySlice(gate["target_case_ids"]) {
		if caseID := toString(rawCaseID); caseID != "" {
			targetCaseIDSet[caseID] = struct{}{}
		}
	}
	inconclusiveUnion := map[string]struct{}{}
	for caseID := range errorCaseIDSet {
		inconclusiveUnion[caseID] = struct{}{}
	}
	for caseID := range machineEvidenceSet {
		inconclusiveUnion[caseID] = struct{}{}
	}
	inconclusiveTargets := sortedStringSet(stringSetIntersection(targetCaseIDSet, inconclusiveUnion))
	if len(inconclusiveTargets) > 0 {
		return map[string]any{
			"retained":                       false,
			"reason":                         "candidate_target_inconclusive_at_epoch_checkpoint",
			"failure_class":                  "infrastructure_or_evidence_failure",
			"failed_target_case_ids":         []any{},
			"inconclusive_target_case_ids":   toAnySlice(inconclusiveTargets),
			"missing_runtime_invocations":    []any{},
			"correlated_regression_case_ids": []any{},
		}, nil
	}
	passing, err := passingCaseIDs(fullEvalRef)
	if err != nil {
		return nil, err
	}
	failedTargetCaseIDs := sortedStringSet(stringSetKeyDiff(targetCaseIDSet, passing))
	if len(failedTargetCaseIDs) > 0 {
		replay, err := classifyReplayOutcome(gate, fullEvalRef)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"retained":                       false,
			"reason":                         "candidate_failed_target_replay_checkpoint",
			"failure_class":                  replay["failure_class"],
			"failed_target_case_ids":         toAnySlice(failedTargetCaseIDs),
			"missing_runtime_invocations":    []any{},
			"correlated_regression_case_ids": []any{},
		}, nil
	}

	invokedSkill, err := invokedSkillNamesByCase(fullEvalRef)
	if err != nil {
		return nil, err
	}
	invokedTool, err := invokedToolNamesByCase(fullEvalRef)
	if err != nil {
		return nil, err
	}
	missingRuntimeInvocations := []map[string]string{}
	for _, rawCapability := range toAnySlice(gate["capabilities"]) {
		capability, ok := rawCapability.(map[string]any)
		if !ok {
			continue
		}
		actionGroup := toString(capability["action_group"])
		runtimeName := toString(capability["runtime_name"])
		capabilityTargets := map[string]struct{}{}
		for _, rawCaseID := range toAnySlice(capability["target_case_ids"]) {
			if caseID := toString(rawCaseID); caseID != "" {
				capabilityTargets[caseID] = struct{}{}
			}
		}
		if len(capabilityTargets) == 0 {
			capabilityTargets = targetCaseIDSet
		}
		if actionGroup == "prompt" {
			continue
		}
		if actionGroup != "skill" && actionGroup != "tool" {
			continue
		}
		if runtimeName == "" {
			continue
		}
		var namesMatch func(planned, invoked string) bool
		var invokedByCase map[string]map[string]struct{}
		if actionGroup == "skill" {
			namesMatch = skillNamesMatch
			invokedByCase = invokedSkill
		} else {
			namesMatch = toolNamesMatch
			invokedByCase = invokedTool
		}
		for _, caseID := range sortedStringSet(capabilityTargets) {
			matched := false
			for invoked := range invokedByCase[caseID] {
				if namesMatch(runtimeName, invoked) {
					matched = true
					break
				}
			}
			if !matched {
				missingRuntimeInvocations = append(missingRuntimeInvocations, map[string]string{
					"runtime_name":   runtimeName,
					"target_case_id": caseID,
				})
			}
		}
	}
	if len(missingRuntimeInvocations) > 0 {
		incompleteBool := InconclusiveCapabilityCases(fullEvalRef, missingRuntimeInvocations)
		incomplete := map[string]struct{}{}
		for caseID := range incompleteBool {
			incomplete[caseID] = struct{}{}
		}
		if len(incomplete) > 0 {
			return map[string]any{
				"retained":                       false,
				"reason":                         "candidate_target_inconclusive_at_epoch_checkpoint",
				"failure_class":                  "infrastructure_or_evidence_failure",
				"failed_target_case_ids":         []any{},
				"inconclusive_target_case_ids":   toAnySlice(sortedStringSet(incomplete)),
				"missing_runtime_invocations":    []any{},
				"correlated_regression_case_ids": []any{},
			}, nil
		}
		missingAny := []any{}
		for _, missing := range missingRuntimeInvocations {
			missingAny = append(missingAny, missing)
		}
		return map[string]any{
			"retained":                       false,
			"reason":                         "candidate_capability_not_used_at_epoch_checkpoint",
			"failure_class":                  "capability_activation_failure",
			"failed_target_case_ids":         []any{},
			"missing_runtime_invocations":    missingAny,
			"correlated_regression_case_ids": []any{},
		}, nil
	}
	return map[string]any{
		"retained":                       true,
		"reason":                         "candidate_effect_retained_after_epoch_checkpoint",
		"failure_class":                  "",
		"failed_target_case_ids":         []any{},
		"missing_runtime_invocations":    []any{},
		"correlated_regression_case_ids": []any{},
	}, nil
}

// globallyPromotable epoch 最终工件能否成为新全局最佳（Python: _globally_promotable）。
//
// 候选局部保留只证明改动仍有益于自己的目标；晋级要求最终选中工件对前一最佳
// 非回归：均分不降，且前一最佳通过的用例必须仍通过。epsilon 只吸收评委浮点噪声。
func globallyPromotable(
	selectedEvalRef, previousBestEvalRef string,
	previousBestScore *float64,
) (bool, string, error) {
	selectedScore, err := evalScore(selectedEvalRef)
	if err != nil {
		return false, "", err
	}
	if previousBestScore != nil && selectedScore < *previousBestScore-promotionScoreEpsilon {
		return false, "selected_score_regressed", nil
	}
	if previousBestEvalRef != "" {
		previousPassing, err := passingCaseIDs(previousBestEvalRef)
		if err != nil {
			return false, "", err
		}
		selectedPassing, err := passingCaseIDs(selectedEvalRef)
		if err != nil {
			return false, "", err
		}
		if len(stringSetKeyDiff(previousPassing, selectedPassing)) > 0 {
			return false, "protected_case_regressed", nil
		}
	}
	return true, "selected_global_non_regression", nil
}

// rejectMixedOpaqueSnapshotSelection 拒绝部分存活的 opaque epoch（Python: _reject_mixed_opaque_snapshot_selection）。
func rejectMixedOpaqueSnapshotSelection(
	gates, selections []map[string]any,
) (bool, error) {
	if len(gates) != len(selections) {
		return false, errors.New("opaque snapshot gates and selections must be paired")
	}
	opaque := false
	for _, gate := range gates {
		if toString(gate["composition_mode"]) == "opaque_snapshot" {
			opaque = true
			break
		}
	}
	anyRetained := false
	anyRejected := false
	for _, selection := range selections {
		if truthy(selection["retained"]) {
			anyRetained = true
		} else {
			anyRejected = true
		}
	}
	mixed := opaque && anyRetained && anyRejected
	if !mixed {
		return false, nil
	}
	// PolicyHarness 候选是累积文件系统快照。移除一个动作而保留更晚快照
	// 会保留被拒字节。在 opaque 组合器/重放出现前整体拒绝该 epoch。
	for _, selection := range selections {
		selection["retained"] = false
		selection["reason"] = "epoch_opaque_snapshot_partial_retention_unsupported"
		selection["failure_class"] = "regression_or_retention_failure"
	}
	return true, nil
}
