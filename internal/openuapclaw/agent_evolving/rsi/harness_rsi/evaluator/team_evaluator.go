// team_evaluator.go Team Skill + member harness 集合评估门面。
// Python: openjiuwen/rsi/harness_rsi/evaluator/team_evaluator.py（501 行）。
// 17.6c 真身：替换 17.4f 桩（回填件①哨兵已删、②真载入、③any 槽收窄、
// ④无旧签名兼容分支）。方法内步骤逐条对齐 Python 原文（规则 9/12）。

package evaluator

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/config"
	dataloader "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/data_loader"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator/judger"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	sourceEvidence "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/source_evidence"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvaluationStageEvent 单用例阶段进度事件。
//
// Python: team_evaluator.py:148-157 report_case 的 payload dict（on_case_stage
// 回调参数）收窄为具体结构体（spec §14 拍板 2，回填件③）。CompletedCases 仅在
// caseConcurrency > 1 时填充（对齐 Python 条件字段 completed_cases）。
type EvaluationStageEvent struct {
	// CaseIndex 用例序号（1 起，对齐 enumerate(cases, start=1)）
	CaseIndex int
	// TotalCases 用例总数
	TotalCases int
	// CaseID 用例标识（缺省 fallback "case_%03d"）
	CaseID string
	// Status 阶段状态：running 或 case_ref.Status（对齐 payload["status"]）
	Status string
	// Score 分数引用（进行中/未评分为 nil，对齐 payload["score"]=None）
	Score *float64
	// CompletedCases 已完成数（仅并发 >1 时填充，nil 对齐条件字段缺席）
	CompletedCases *int
}

// TeamEvaluator Team Skill + member harness 集合对数据集的评估门面。
//
// 对齐 Python team_evaluator.py:44 TeamEvaluator：评估一个 Team Skill 加
// member harness 集合对数据集的表现。
type TeamEvaluator struct {
	// config 评估器配置（对齐 Python self.config）
	config config.EvaluatorConfig
	// metricsCollector 指标收集器（对齐 Python self.metrics_collector）
	metricsCollector *MetricsCollector
	// caseRunner 用例监考（对齐 Python self.case_runner）
	caseRunner *CaseRunner
}

// evalRefPayload eval_ref.yaml 落盘结构（键序对齐 Python payload 字典字面顺序）。
type evalRefPayload struct {
	// EvalID 评估标识（取 eval_dir 末三级目录拼接）
	EvalID string `yaml:"eval_id"`
	// CreatedAt 创建时间（本地时区 ISO 格式）
	CreatedAt string `yaml:"created_at"`
	// TeamName 评估名称
	TeamName string `yaml:"team_name"`
	// TeamSkillRefPath 团队技能引用路径
	TeamSkillRefPath string `yaml:"team_skill_ref_path"`
	// HarnessRefsPath harness 引用文件路径
	HarnessRefsPath string `yaml:"harness_refs_path"`
	// Dataset 数据集工件（可空）
	Dataset *evalRefDataset `yaml:"dataset"`
	// EvalDir 评估目录
	EvalDir string `yaml:"eval_dir"`
	// CaseResultsDir 用例结果目录
	CaseResultsDir string `yaml:"case_results_dir"`
	// CaseTracesDir 用例轨迹目录（与 CaseResultsDir 同目录，Python 同值双键）
	CaseTracesDir string `yaml:"case_traces_dir"`
	// SummaryPath 汇总文件路径
	SummaryPath string `yaml:"summary_path"`
	// Cases 用例引用清单
	Cases []evalRefCaseEntry `yaml:"cases"`
}

// evalRefDataset eval_ref.yaml 的 dataset 段。
type evalRefDataset struct {
	// DatasetID 数据集唯一标识
	DatasetID string `yaml:"dataset_id"`
	// DatasetDir 数据集目录
	DatasetDir string `yaml:"dataset_dir"`
	// DatasetFiles 数据集文件清单
	DatasetFiles []string `yaml:"dataset_files"`
}

// evalRefCaseEntry eval_ref.yaml 的逐题引用条目。
type evalRefCaseEntry struct {
	// CaseID 用例标识
	CaseID string `yaml:"case_id"`
	// CasePath 用例文件路径
	CasePath string `yaml:"case_path"`
	// TracePath 轨迹文件路径
	TracePath string `yaml:"trace_path"`
	// ResultPath 结果文件路径
	ResultPath string `yaml:"result_path"`
	// Status 评估状态
	Status string `yaml:"status"`
	// Score 分数（nil 表示未评分）
	Score *float64 `yaml:"score,omitempty"`
	// Metadata 附加元数据
	Metadata map[string]any `yaml:"metadata"`
}

// caseRetryRecord 瞬态重试历史条目（Python retry_history append 的 dict）。
type caseRetryRecord struct {
	// Attempt 尝试序号（1 起）
	Attempt int `json:"attempt"`
	// Error 瞬态错误原文
	Error string `json:"error"`
}

// ──────────────────────────── 常量 ────────────────────────────

// CaseResultsDirName 用例结果目录名（Python: CASE_RESULTS_DIR_NAME）。
const CaseResultsDirName = "cases"

// evaluationInputFile 评估输入指纹清单文件名（Python: _EVALUATION_INPUT_FILE）。
const evaluationInputFile = "evaluation_input.json"

// ──────────────────────────── 全局变量 ────────────────────────────

// buildBackendFn / buildJudgerFn 构造函数测试缝（函数值承载，反转缝同款惯例）：
// 生产路径指向 BuildBackend / judger.BuildJudger；真身单测替换为 fake 构造器。
var (
	buildBackendFn = BuildBackend
	buildJudgerFn  = judger.BuildJudger
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamEvaluator 构造评估门面（对齐 Python __init__：config + MetricsCollector
// + _make_case_runner）。
func NewTeamEvaluator(cfg config.EvaluatorConfig) (*TeamEvaluator, error) {
	te := &TeamEvaluator{config: cfg}
	runner, err := te.makeCaseRunner()
	if err != nil {
		return nil, err
	}
	te.caseRunner = runner
	te.metricsCollector = NewMetricsCollector()
	return te, nil
}

// Evaluate 评估数据集全部用例并返回 eval_ref.yaml 路径。
//
// Python: team_evaluator.py:66-80 evaluate——_load_dataset_cases 载入后转调
// evaluate_batch（回填件②：桩期 nil 占位换为真载入产物）。
func (te *TeamEvaluator) Evaluate(ctx context.Context, dataset harnessrsi.DatasetArtifact,
	teamSkillRefPath, harnessRefsPath, outputDir string) (string, error) {
	cases, err := loadDatasetCases(dataset.DatasetFiles)
	if err != nil {
		return "", err
	}
	return te.EvaluateBatch(ctx, cases, teamSkillRefPath, harnessRefsPath, outputDir,
		&dataset, nil, 1, nil)
}

// EvaluateBatch 按输入顺序持久化用例结果；并发为按次评估可选项。
//
// Python: team_evaluator.py:82-254 evaluate_batch。只按新签名实现，无
// inspect.signature 旧签名兼容分支（回填件④，17.4d 拍板）；Python 的
// isinstance bool 检查在 Go 类型层不可能（bool ≠ int），属已知类型层偏差（偏差①）。
func (te *TeamEvaluator) EvaluateBatch(ctx context.Context, cases []map[string]any,
	teamSkillRefPath, harnessRefsPath, outputDir string,
	dataset *harnessrsi.DatasetArtifact,
	onCaseStage func(EvaluationStageEvent) error,
	caseConcurrency int,
	baselineContexts map[string]*BaselineJudgeContext) (string, error) {
	// 守卫序照 Python：case_concurrency 先于一切（守卫顺序不得调换）。
	if caseConcurrency < 1 {
		return "", errors.New("case_concurrency must be a positive integer")
	}
	// 基线公平性校验：候选评估缺同题基线即整场失败。
	if baselineContexts != nil {
		for _, caseMapping := range cases {
			context := baselineContexts[evalToString(caseMapping["case_id"])]
			if context == nil {
				return "", NewEvaluationInfrastructureError(
					"Candidate evaluation is missing its same-case baseline")
			}
			if _, err := context.Validate(caseMapping); err != nil {
				return "", err
			}
		}
	}
	// 判分前校验：仅 single_harness 门面带判分器时逐题校验字段与判分契约。
	if te.caseRunner != nil {
		if _, isSingleHarness := te.caseRunner.backend.(*SingleHarnessExecutionBackend); isSingleHarness &&
			te.caseRunner.judger != nil {
			for _, caseMapping := range cases {
				if err := dataloader.ValidateCaseFields(caseMapping, true); err != nil {
					return "", err
				}
				if err := te.caseRunner.judger.ValidateCase(caseMapping); err != nil {
					return "", err
				}
			}
		}
	}
	evalDir, err := prepareEvalDir(outputDir)
	if err != nil {
		return "", err
	}
	caseResultsDir := filepath.Join(evalDir, CaseResultsDirName)

	// harness 引用载入：文件缺失/不可读分别告警后原样上抛（Python L112-120）。
	refsPath := strings.TrimSpace(harnessRefsPath)
	var harnessRefs map[string]string
	if refsPath != "" {
		harnessRefs, err = loadHarnessRefs(refsPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				logger.Warn(logComponent).Str("refs_path", refsPath).
					Msg("Harness refs file is missing")
			} else {
				logger.Warn(logComponent).Str("refs_path", refsPath).
					Msg("Harness refs file is unreadable or invalid")
			}
			return "", err
		}
	}

	// 评估输入指纹：写入 manifest 后与上次比对决定是否断点续考。
	inputFingerprint, err := evaluationInputFingerprint(fingerprintParams{
		cases:            cases,
		teamSkillRefPath: teamSkillRefPath,
		harnessRefsPath:  harnessRefsPath,
		evaluatorConfig:  &te.config,
		baselineContexts: baselineContexts,
		harnessRefs:      harnessRefs,
	})
	if err != nil {
		return "", err
	}
	manifestPath := filepath.Join(evalDir, evaluationInputFile)
	// 断点续考判定先于 manifest 覆写（Python L131-135：先比对旧指纹再写新指纹）。
	canResumeCases := storedEvaluationFingerprint(manifestPath) == inputFingerprint
	if err := writeJSONFile(manifestPath, map[string]any{"fingerprint": inputFingerprint}); err != nil {
		return "", err
	}
	totalCases := len(cases)
	completedCases := 0
	var stageLock sync.Mutex

	reportCase := func(caseIndex int, caseID string, caseRef *harnessrsi.EvaluationCaseTraceRef) error {
		stageLock.Lock()
		if caseRef != nil {
			completedCases++
		}
		if onCaseStage != nil {
			event := EvaluationStageEvent{
				CaseIndex:  caseIndex,
				TotalCases: totalCases,
				CaseID:     caseID,
				Status:     "running",
			}
			if caseRef != nil {
				event.Status = caseRef.Status
				event.Score = caseRef.Score
			}
			// completed_cases 条件字段：仅并发 >1 时携带（Python L155-156）。
			if caseConcurrency > 1 {
				completed := completedCases
				event.CompletedCases = &completed
			}
			stageLock.Unlock()
			return onCaseStage(event)
		}
		stageLock.Unlock()
		return nil
	}

	runCase := func(caseIndex int, caseMapping map[string]any) (*harnessrsi.EvaluationCaseTraceRef, error) {
		caseID := evalToString(caseMapping["case_id"])
		if caseID == "" {
			caseID = fmt.Sprintf("case_%03d", caseIndex)
		}
		caseOutputDir := filepath.Join(caseResultsDir, caseDirName(caseID, caseIndex))

		// 断点续考：指纹匹配且上次已完成（非 execution-only）直接翻旧账。
		if canResumeCases {
			completedRef := loadCompletedCaseRef(caseOutputDir, caseID)
			if completedRef != nil {
				if caseConcurrency > 1 {
					if err := reportCase(caseIndex, caseID, completedRef); err != nil {
						return nil, err
					}
				}
				return completedRef, nil
			}
		}

		// 并发 >1 时逐题新造监考，避免共享监考的订阅叠加（Python L170）。
		runner := te.caseRunner
		if caseConcurrency != 1 {
			reused, err := te.makeCaseRunner()
			if err != nil {
				return nil, err
			}
			runner = reused
		}
		retryHistory := []caseRetryRecord{}
		retryLimit := te.config.TransientCaseRetryLimit
		if retryLimit < 0 {
			retryLimit = 0
		}
		if err := reportCase(caseIndex, caseID, nil); err != nil {
			return nil, err
		}
		var caseRef *harnessrsi.EvaluationCaseTraceRef
		for attempt := 0; attempt <= retryLimit; attempt++ {
			// 考题与 harness 引用深拷贝后交监考（Python deepcopy 对齐）。
			casePayload := utils.DeepCopyMap(caseMapping)
			casePayload["case_id"] = caseID
			refsCopy := utils.DeepCopyMap(toAnyMap(harnessRefs))
			runnerParams := CaseRunnerParams{
				Case:             casePayload,
				OutputDir:        caseOutputDir,
				TeamSkillRefPath: teamSkillRefPath,
				HarnessRefs:      toStringMap(refsCopy),
			}
			if baselineContexts != nil {
				runnerParams.BaselineContext = baselineContexts[caseID]
			}
			executed, err := runner.Execute(ctx, runnerParams)
			if err != nil {
				// 基建失败：仅瞬态传输错误且未到上限才重考（Python L187-199）。
				var infraErr *EvaluationInfrastructureError
				if !errors.As(err, &infraErr) {
					return nil, err
				}
				transient := TransientTransportError(infraErr.Msg)
				if transient == "" || attempt >= retryLimit {
					return nil, err
				}
				retryHistory = append(retryHistory,
					caseRetryRecord{Attempt: attempt + 1, Error: transient})
				os.RemoveAll(caseOutputDir)
				time.Sleep(backoffDelay(attempt))
				continue
			}
			caseRef = executed
			// 结果级瞬态错误：status=error 且错误文本可重试（Python L200-210）。
			transient := transientCaseError(caseRef)
			if transient == "" || attempt >= retryLimit {
				break
			}
			retryHistory = append(retryHistory,
				caseRetryRecord{Attempt: attempt + 1, Error: transient})
			os.RemoveAll(caseOutputDir)
			time.Sleep(backoffDelay(attempt))
		}
		// 重试历史落盘（Python L211-216）。
		if len(retryHistory) > 0 {
			historyPath := filepath.Join(caseResultsDir,
				caseOutputDir[len(caseResultsDir)+1:]+"_transient_retries.json")
			records := make([]map[string]any, 0, len(retryHistory))
			for _, record := range retryHistory {
				records = append(records, map[string]any{
					"attempt": record.Attempt, "error": record.Error,
				})
			}
			if err := writeJSONFile(historyPath, records); err != nil {
				return nil, err
			}
		}
		if err := reportCase(caseIndex, caseID, caseRef); err != nil {
			return nil, err
		}
		return caseRef, nil
	}

	caseRefs := make([]*harnessrsi.EvaluationCaseTraceRef, 0, totalCases)
	if caseConcurrency == 1 {
		// 串行：按输入顺序逐题执行（Python L220-222）。
		for index, caseMapping := range cases {
			caseRef, err := runCase(index+1, caseMapping)
			if err != nil {
				return "", err
			}
			caseRefs = append(caseRefs, caseRef)
		}
	} else {
		// 并发：信号量限流 + errgroup 首错取消全体（asyncio 任务池，偏差②）。
		var group errgroup.Group
		group.SetLimit(caseConcurrency)
		results := make([]*harnessrsi.EvaluationCaseTraceRef, totalCases)
		for index, caseMapping := range cases {
			index, caseMapping := index, caseMapping
			group.Go(func() error {
				caseRef, err := runCase(index+1, caseMapping)
				if err != nil {
					return err
				}
				results[index] = caseRef
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			return "", err
		}
		// Gather 保序：结果按数据集输入序归位（Python L232-233）。
		caseRefs = append(caseRefs, results...)
	}

	// 汇总与成绩单落盘（Python L241-254）。
	summaryPath, err := te.metricsCollector.Collect(caseResultsDir,
		filepath.Join(evalDir, "summary.json"))
	if err != nil {
		return "", err
	}
	return writeEvalRef(evalRefParams{
		evalDir:          evalDir,
		teamName:         evaluationName(teamSkillRefPath),
		teamSkillRefPath: teamSkillRefPath,
		harnessRefsPath:  harnessRefsPath,
		dataset:          dataset,
		caseRefs:         caseRefs,
		summaryPath:      summaryPath,
	})
}

// makeCaseRunner 构造监考并按需复用轨迹 processor。
//
// Python: team_evaluator.py:52-64 _make_case_runner——backend 经 build_backend
// 构造；若新旧 backend 均为 SingleHarnessExecutionBackend，则复用既有 case_runner
// 的 trajectory processor 注册（processor 已按异步上下文隔离订阅，避免逐 case
// 叠加注册）。Go 侧复用点为 SingleHarnessExecutionBackend.ReuseTrajectoryProcessor
// （case_backend_main.go）。
func (te *TeamEvaluator) makeCaseRunner() (*CaseRunner, error) {
	backend, err := buildBackendFn(&te.config)
	if err != nil {
		return nil, err
	}
	judgerImpl, err := buildJudgerFn(&te.config)
	if err != nil {
		return nil, err
	}
	// 复用判断：新 backend 与既有监考 backend 同为 single_harness 时共享
	// trajectory processor 注册。
	existing := te.caseRunner
	if newBackend, ok := backend.(*SingleHarnessExecutionBackend); ok && existing != nil {
		if existingBackend, ok := existing.backend.(*SingleHarnessExecutionBackend); ok {
			newBackend.ReuseTrajectoryProcessor(existingBackend)
		}
	}
	return NewCaseRunner(backend, judgerImpl)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// prepareEvalDir 展开用户目录并创建评估根目录。
// Python: team_evaluator.py:262-265 _prepare_eval_dir。
func prepareEvalDir(outputDir string) (string, error) {
	evalDir, err := filepath.Abs(pathutil.ExpandHome(outputDir))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(evalDir, 0o755); err != nil {
		return "", err
	}
	return evalDir, nil
}

// evaluationName 由技能引用路径推断评估名称。
// Python: team_evaluator.py:268-272 _evaluation_name。
func evaluationName(skillRefPath string) string {
	if strings.TrimSpace(skillRefPath) == "" {
		return "single_harness"
	}
	path := pathutil.ExpandHome(skillRefPath)
	if strings.ToLower(filepath.Base(path)) == "skill.md" {
		return filepath.Base(filepath.Dir(path))
	}
	return filepath.Base(path)
}

// fingerprintParams 指纹计算入参包（Python 关键字参数）。
type fingerprintParams struct {
	cases            []map[string]any
	teamSkillRefPath string
	harnessRefsPath  string
	evaluatorConfig  *config.EvaluatorConfig
	baselineContexts map[string]*BaselineJudgeContext
	harnessRefs      map[string]string
}

// evaluationInputFingerprint 评估输入指纹：考题/引用/配置全部入哈希。
// Python: team_evaluator.py:275-321 _evaluation_input_fingerprint。
func evaluationInputFingerprint(params fingerprintParams) (string, error) {
	datasetFiles := make([]any, 0, len(params.cases))
	for _, caseMapping := range params.cases {
		fingerprint, err := dataloader.FileFingerprint(caseMapping)
		if err != nil {
			return "", err
		}
		datasetFiles = append(datasetFiles, fingerprint)
	}
	payload := map[string]any{
		"cases":         params.cases,
		"dataset_files": datasetFiles,
		"team_skill":    pathIdentity(params.teamSkillRefPath),
		"harness_refs":  pathIdentity(params.harnessRefsPath),
	}
	if params.baselineContexts != nil {
		contexts := map[string]any{}
		for key, value := range params.baselineContexts {
			contexts[key] = value.Fingerprint()
		}
		payload["baseline_contexts"] = contexts
	}
	packages := map[string]any{}
	refsPath := strings.TrimSpace(params.harnessRefsPath)
	if refsPath != "" {
		loadedRefs := params.harnessRefs
		if loadedRefs == nil {
			var err error
			loadedRefs, err = loadHarnessRefs(refsPath)
			if err != nil {
				return "", err
			}
		}
		for role, packagePath := range loadedRefs {
			packageAbs, err := filepath.Abs(pathutil.ExpandHome(packagePath))
			if err != nil {
				return "", err
			}
			if isFile(packageAbs) {
				packageAbs = filepath.Dir(packageAbs)
			}
			packages[role] = pathIdentity(packageAbs)
		}
	}
	payload["harness_packages"] = packages
	method := ""
	if params.evaluatorConfig != nil {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			cwd = "."
		}
		payload["evaluator"] = sourceEvidence.MaterialIdentity(
			config.EvaluatorProtocolConfig(*params.evaluatorConfig), cwd, nil)
		method = strings.ToLower(strings.ReplaceAll(
			strings.TrimSpace(params.evaluatorConfig.EvaluationMethod), "-", "_"))
	}
	if params.evaluatorConfig != nil && method == "llm_as_judge" {
		if params.evaluatorConfig.ScoreMode == "binary" {
			payload["llm_judge_score_contract"] = "threshold_binary_v1"
		} else {
			payload["llm_judge_score_contract"] = "continuous"
		}
		protocol, err := judger.JudgeProtocolIdentity(
			params.evaluatorConfig.ScoreMode,
			params.evaluatorConfig.JudgeBaselineComparison)
		if err != nil {
			return "", err
		}
		payload["judge_protocol"] = protocol
	}
	return sha256JSON(payload), nil
}

// pathIdentity 路径身份：文件取内容哈希，目录取相对路径+内容按序哈希。
// Python: team_evaluator.py:324-337 _path_identity。
func pathIdentity(pathValue string) map[string]string {
	if strings.TrimSpace(pathValue) == "" {
		return map[string]string{"path": "", "sha256": ""}
	}
	path, err := filepath.Abs(pathutil.ExpandHome(pathValue))
	if err != nil {
		return map[string]string{"path": pathValue, "sha256": ""}
	}
	digest := ""
	if isFile(path) {
		data, err := os.ReadFile(path)
		if err == nil {
			sum := sha256.Sum256(data)
			digest = hex.EncodeToString(sum[:])
		}
	} else if isDir(path) {
		hasher := sha256.New()
		var children []string
		filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() {
				children = append(children, p)
			}
			return nil
		})
		sort.Strings(children)
		for _, child := range children {
			rel, relErr := filepath.Rel(path, child)
			if relErr != nil {
				continue
			}
			hasher.Write([]byte(filepath.ToSlash(rel)))
			if data, err := os.ReadFile(child); err == nil {
				hasher.Write(data)
			}
		}
		digest = hex.EncodeToString(hasher.Sum(nil))
	}
	return map[string]string{"path": path, "sha256": digest}
}

// storedEvaluationFingerprint 读取 manifest 中的上次评估指纹。
// Python: team_evaluator.py:340-345 _stored_evaluation_fingerprint。
func storedEvaluationFingerprint(manifestPath string) string {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}
	if fingerprint, ok := payload["fingerprint"].(string); ok {
		return fingerprint
	}
	return ""
}

// loadCompletedCaseRef 断点续考翻旧账：result/trace 双件齐且非 execution-only。
// Python: team_evaluator.py:348-386 _load_completed_case_ref。
func loadCompletedCaseRef(caseOutputDir, caseID string) *harnessrsi.EvaluationCaseTraceRef {
	resultPath := filepath.Join(caseOutputDir, "result.json")
	tracePath := filepath.Join(caseOutputDir, "trace.json")
	result, resultErr := readJSONMapping(resultPath)
	trace, traceErr := readJSONMapping(tracePath)
	if resultErr != nil || traceErr != nil {
		return nil
	}
	if evalToString(result["case_id"]) != caseID || evalToString(trace["case_id"]) != caseID {
		return nil
	}
	status := evalToString(result["status"])
	if status == "" {
		return nil
	}
	evaluation, _ := result["evaluation"].(map[string]any)
	if evaluation == nil {
		evaluation = map[string]any{}
	}
	var caseScore *float64
	if score, ok := result["score"].(float64); ok {
		caseScore = &score
	}
	if judger.IsExecutionOnlyEvaluation(evaluation) {
		return nil
	}
	metadata, _ := result["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	passed, _ := evaluation["passed"].(bool)
	return &harnessrsi.EvaluationCaseTraceRef{
		CaseID:     caseID,
		CasePath:   evalToString(metadata["case_path"]),
		TracePath:  tracePath,
		ResultPath: resultPath,
		Status:     status,
		Score:      caseScore,
		Metadata: map[string]any{
			"session_id":        evalToString(result["session_id"]),
			"team_name":         evalToString(metadata["team_name"]),
			"execution_status":  evalToString(result["execution_status"]),
			"evaluation_method": evalToString(evaluation["method"]),
			"evaluation_passed": passed,
			"resumed":           true,
		},
	}
}

// transientCaseError 从结果件读可重试的传输层错误，绝不把语义失败当瞬态。
// Python: team_evaluator.py:389-398 _transient_case_error。
func transientCaseError(caseRef *harnessrsi.EvaluationCaseTraceRef) string {
	if !strings.EqualFold(caseRef.Status, "error") {
		return ""
	}
	payload, err := readJSONMapping(caseRef.ResultPath)
	if err != nil {
		return ""
	}
	return TransientTransportError(strings.TrimSpace(evalToString(payload["error"])))
}

// evalIDFromDir 取评估目录末三级目录拼接为评估标识。
// Python: team_evaluator.py:401-403 _eval_id_from_dir。
func evalIDFromDir(evalDir string) string {
	parts := strings.Split(strings.Trim(evalDir, string(os.PathSeparator)), string(os.PathSeparator))
	if len(parts) > 3 {
		parts = parts[len(parts)-3:]
	}
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	return strings.Join(filtered, "_")
}

// caseDirName 用例目录名：sha1 前 8 位短哈希，业务 case_id 不变。
// Python: team_evaluator.py:406-409 _case_dir_name。
func caseDirName(caseID string, caseIndex int) string {
	sum := sha1.Sum([]byte(caseID))
	return fmt.Sprintf("c%03d_%s", caseIndex, hex.EncodeToString(sum[:])[:8])
}

// loadDatasetCases 载入数据集全部文件并用例注入路径与序号。
// Python: team_evaluator.py:412-418 _load_dataset_cases。
func loadDatasetCases(datasetFiles []string) ([]map[string]any, error) {
	var cases []map[string]any
	for _, datasetFile := range datasetFiles {
		path, err := filepath.Abs(pathutil.ExpandHome(datasetFile))
		if err != nil {
			return nil, err
		}
		loaded, err := dataloader.LoadJSONCases(path)
		if err != nil {
			return nil, err
		}
		for index, caseMapping := range loaded {
			caseMapping["case_path"] = path
			caseMapping["case_index"] = index + 1
			cases = append(cases, caseMapping)
		}
	}
	return cases, nil
}

// loadHarnessRefs 载入运行时 {member_name: harness_path} 引用映射。
//
// Python: team_evaluator.py:421-440 _load_harness_refs——规范形态是
// MemberOptimizer 兼容的 {"harness_refs": {role: path}, "roles": [...]}；
// 兼容旧版顶层 {role: path} 映射。
func loadHarnessRefs(harnessRefsPath string) (map[string]string, error) {
	path, err := filepath.Abs(pathutil.ExpandHome(harnessRefsPath))
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var mapping map[string]any
	if err := yaml.Unmarshal(data, &mapping); err != nil {
		return nil, fmt.Errorf("harness_refs file is unreadable or invalid: %s: %w", path, err)
	}
	if mapping == nil {
		mapping = map[string]any{}
	}
	if refs, ok := mapping["harness_refs"].(map[string]any); ok {
		result := map[string]string{}
		for key, value := range refs {
			if str, ok := value.(string); ok {
				result[key] = str
			}
		}
		return result, nil
	}
	result := map[string]string{}
	excluded := map[string]bool{"version": true, "source_harness_refs_path": true}
	for key, value := range mapping {
		if str, ok := value.(string); ok && !excluded[key] {
			result[key] = str
		}
	}
	return result, nil
}

// evalRefParams eval_ref 落盘入参包（Python 关键字参数）。
type evalRefParams struct {
	evalDir          string
	teamName         string
	teamSkillRefPath string
	harnessRefsPath  string
	dataset          *harnessrsi.DatasetArtifact
	caseRefs         []*harnessrsi.EvaluationCaseTraceRef
	summaryPath      string
}

// writeEvalRef 写 eval_ref.yaml 成绩单（EvaluationCaseTraceRef 引用链）。
// Python: team_evaluator.py:443-488 _write_eval_ref。
func writeEvalRef(params evalRefParams) (string, error) {
	evalRefPath := filepath.Join(params.evalDir, "eval_ref.yaml")
	payload := evalRefPayload{
		EvalID:           evalIDFromDir(params.evalDir),
		CreatedAt:        time.Now().Format("2006-01-02T15:04:05Z07:00"),
		TeamName:         params.teamName,
		TeamSkillRefPath: params.teamSkillRefPath,
		HarnessRefsPath:  params.harnessRefsPath,
		EvalDir:          params.evalDir,
		CaseResultsDir:   filepath.Join(params.evalDir, CaseResultsDirName),
		CaseTracesDir:    filepath.Join(params.evalDir, CaseResultsDirName),
		SummaryPath:      params.summaryPath,
		Cases:            make([]evalRefCaseEntry, 0, len(params.caseRefs)),
	}
	if params.dataset != nil {
		payload.Dataset = &evalRefDataset{
			DatasetID:    params.dataset.DatasetID,
			DatasetDir:   params.dataset.DatasetDir,
			DatasetFiles: params.dataset.DatasetFiles,
		}
	}
	for _, item := range params.caseRefs {
		payload.Cases = append(payload.Cases, evalRefCaseEntry{
			CaseID:     item.CaseID,
			CasePath:   item.CasePath,
			TracePath:  item.TracePath,
			ResultPath: item.ResultPath,
			Status:     item.Status,
			Score:      item.Score,
			Metadata:   item.Metadata,
		})
	}
	data, err := yaml.Marshal(payload)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(evalRefPath, data, 0o644); err != nil {
		return "", err
	}
	return evalRefPath, nil
}

// loadSummaryFromEvalRef 从 eval_ref.yaml 读取汇总文件内容。
// Python: team_evaluator.py:491-501 _load_summary_from_eval_ref。
func loadSummaryFromEvalRef(evalRefPath string) (map[string]any, error) {
	data, err := os.ReadFile(evalRefPath)
	if err != nil {
		return nil, err
	}
	var evalRef map[string]any
	if err := yaml.Unmarshal(data, &evalRef); err != nil {
		return nil, err
	}
	if evalRef == nil {
		evalRef = map[string]any{}
	}
	summaryPath := evalToString(evalRef["summary_path"])
	if summaryPath == "" {
		return map[string]any{}, nil
	}
	summary, err := readJSONMapping(summaryPath)
	if err != nil {
		return nil, err
	}
	return summary, nil
}

// backoffDelay 瞬态重试退避：min(2^attempt, 4) 秒。
// Python: team_evaluator.py:198/210 asyncio.sleep(min(2**attempt, 4))。
func backoffDelay(attempt int) time.Duration {
	seconds := 1 << attempt
	if seconds > 4 {
		seconds = 4
	}
	return time.Duration(seconds) * time.Second
}

// toAnyMap string 映射转 any 映射（深拷贝入参需要）。
func toAnyMap(source map[string]string) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

// toStringMap any 映射回 string 映射（深拷贝出参收窄）。
func toStringMap(source map[string]any) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		if str, ok := value.(string); ok {
			result[key] = str
		}
	}
	return result
}


// isFile 判断常规文件。
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// isDir 判断目录。
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// writeJSONFile 缩进 JSON 落盘（ensure_ascii=False 等价：Go 默认原样 UTF-8）。
func writeJSONFile(path string, payload any) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// readJSONMapping 读取 JSON 映射文件。
func readJSONMapping(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// sha256JSON 规范化 JSON 指纹：map 键序排序后哈希。
// Python: json.dumps(payload, ensure_ascii=False, sort_keys=True).encode → sha256。
func sha256JSON(payload any) string {
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
