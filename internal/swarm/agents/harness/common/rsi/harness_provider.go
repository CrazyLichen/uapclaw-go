package rsi

// HarnessProvider：HarnessProviderContract 的生产实现（包装 agent-core 编排器）。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/harness_provider.py（840 行）。
//
// tasks_root 为引擎 output_dir 与状态/报告读取根；编排器配置三来源二选一
// （注入编排器 > 显式配置/dict > 缺省引导模板）。

import (
	"context"
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

	"gopkg.in/yaml.v3"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/config"
	dataloader "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/data_loader"
	singleharness "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/singleharness"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// stateFile 引擎状态文件名。
	stateFile = "single_harness_state.yaml"
	// reportFile 引擎报告文件名。
	reportFile = "single_harness_report.yaml"
	// providerRunDir 运行目录名。
	providerRunDir = "run"
	// defaultConfigPath 缺省编排器配置相对路径。
	defaultConfigPath = "config/harness_orchestrator.yaml"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HarnessProvider 生产 Harness 引擎 provider。
type HarnessProvider struct {
	tasksRoot string
	// orchestrator 注入缝（测试用，优先级最高）。
	orchestrator *singleharness.SingleHarnessIterativeOptimizationOrchestrator
	// orchestratorConfig 顶层配置 dict（与配置路径二选一）。
	orchestratorConfig map[string]any
	// orchestratorConfigPath 编排器 YAML 配置路径。
	orchestratorConfigPath string
	// modelResolver 任务级模型解析（可 nil）。
	modelResolver *ModelConfigResolver
	// snapshotCache YAML 快照缓存（stamp→data，容量 32，对齐 Python OrderedDict）。
	snapshotCache map[string]snapshotEntry
	snapshotOrder []string
	snapshotLock  sync.Mutex
}

type snapshotEntry struct {
	stamp [4]int64
	data  map[string]any
}

// HarnessProviderOption provider 构造选项（对齐 Python provider_factory 的
// harness_provider 可注入语义：调用方注入实例即替换默认构造）。
type HarnessProviderOption func(*HarnessProvider)

// WithOrchestrator 注入预构造编排器（测试缝，优先级最高；Run 优先使用注入值，
// 不再经 buildConfig 走配置路径构造）。
func WithOrchestrator(o *singleharness.SingleHarnessIterativeOptimizationOrchestrator) HarnessProviderOption {
	return func(p *HarnessProvider) { p.orchestrator = o }
}

// NewHarnessProvider 构造 provider。
func NewHarnessProvider(tasksRoot string, modelResolver *ModelConfigResolver, opts ...HarnessProviderOption) *HarnessProvider {
	p := &HarnessProvider{
		tasksRoot:     tasksRoot,
		modelResolver: modelResolver,
		snapshotCache: map[string]snapshotEntry{},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// ──────────────────────────── 能力位 ────────────────────────────

// SupportsPause harness 引擎暂不支持中途暂停。
func (p *HarnessProvider) SupportsPause() bool { return false }

// SupportsResume 支持断点续跑。
func (p *HarnessProvider) SupportsResume() bool { return true }

// SupportsTerminate 无 terminate hook（worker 以取消 runner 的方式停止）。
func (p *HarnessProvider) SupportsTerminate() bool { return false }

// ──────────────────────────── 导出函数 ────────────────────────────

// EngineValidateInput 引擎 load_cases 真校验（web §5.2 共享入口）。
func EngineValidateInput(datasetPath *string) map[string]any {
	if datasetPath == nil || *datasetPath == "" {
		return map[string]any{"valid": false, "sample_count": nil,
			"errors": []any{map[string]any{"code": "DATASET_REQUIRED", "message": "input_file is required"}}}
	}
	path := expandUser(*datasetPath)
	if !fileExists(path) {
		return map[string]any{"valid": false, "sample_count": nil,
			"errors": []any{map[string]any{"code": "PATH_INVALID", "message": fmt.Sprintf("Dataset not found: %s", path)}}}
	}
	cases, err := validateDatasetPath(path)
	if err != nil {
		code := "DATASET_INVALID"
		if errors.Is(err, ErrPathInvalid) {
			code = "PATH_INVALID"
		}
		return map[string]any{"valid": false, "sample_count": nil,
			"errors": []any{map[string]any{"code": code, "message": err.Error()}}}
	}
	return map[string]any{"valid": true, "sample_count": len(cases), "errors": []any{}}
}

// ValidateInput 输入校验（静态入口对齐）。
func (p *HarnessProvider) ValidateInput(path *string) map[string]any {
	return EngineValidateInput(path)
}

// Run 全新执行。
func (p *HarnessProvider) Run(ctx context.Context, request HarnessEngineRequest, onEvent rsi.OnEvent) (map[string]any, error) {
	return p.run(ctx, request, onEvent, false)
}

// Resume 断点续跑。
func (p *HarnessProvider) Resume(ctx context.Context, request HarnessEngineRequest, onEvent rsi.OnEvent) (map[string]any, error) {
	return p.run(ctx, request, onEvent, true)
}

// Pause 中途暂停：harness 引擎暂不支持（对齐 Python RsiNotReady）。
func (p *HarnessProvider) Pause(taskID string) (map[string]any, error) {
	return nil, &RsiError{Code: "INTERNAL_ERROR", Message: "harness 引擎暂不支持中途暂停"}
}

// Terminate 中途终止：harness 引擎无 terminate hook（worker 取消 runner）。
func (p *HarnessProvider) Terminate(taskID string) (map[string]any, error) {
	return nil, &RsiError{Code: "INTERNAL_ERROR", Message: "harness 引擎暂不支持中途终止"}
}

// ReadState 引擎状态快照（对齐 Python read_state）。
func (p *HarnessProvider) ReadState(taskID string) map[string]any {
	state := p.loadYAML(taskID, stateFile)
	if state == nil {
		state = map[string]any{}
	}
	gates := gatesOf(state)
	status := strings.ToLower(firstNonEmpty(valueToString(state["status"]), "created"))
	iteration := len(gates)
	if checkpoints, ok := state["epoch_checkpoints"].([]any); ok {
		iteration = len(checkpoints)
	}
	totalIterations := safeInt(state["max_iteration"])
	if totalIterations <= 0 {
		if iteration < 1 {
			iteration = 1
		}
		totalIterations = iteration
	}
	baseline := p.baselineScore(taskID, state)
	bestID := bestNodeID(state)
	var bestPtr *string
	if bestID != nil {
		bestPtr = bestID
	}
	return map[string]any{
		"task_id":          taskID,
		"status":           status,
		"iteration":        iteration,
		"total_iterations": totalIterations,
		"best_node_id":     nullableAny(bestPtr),
		"score":            anyFloat(state["best_score"]),
		"baseline":         baseline,
		"usage":            rsi.UsageSnapshot(state["usage"]),
		"updated_at":       UtcnowISO(),
		"error_code":       nil,
		"error_message":    nil,
	}
}

// ReadPublicationState installer 使用的原始引擎状态。
func (p *HarnessProvider) ReadPublicationState(taskID string) map[string]any {
	state := p.readStateDict(taskID)
	if state == nil {
		return map[string]any{}
	}
	return state
}

// ReadReport 引擎报告（对齐 Python read_report）。
func (p *HarnessProvider) ReadReport(taskID string) map[string]any {
	state := p.loadYAML(taskID, stateFile)
	report := p.loadYAML(taskID, reportFile)
	base := report
	if base == nil {
		base = state
	}
	if base == nil {
		base = map[string]any{}
	}
	stateForBest := state
	if stateForBest == nil {
		stateForBest = report
	}
	if stateForBest == nil {
		stateForBest = map[string]any{}
	}
	usage := rsi.UsageSnapshot(base["usage"])
	if usage == nil {
		usage = rsi.UsageSnapshot(stateForBest["usage"])
	}
	bestID := bestNodeID(stateForBest)
	var bestPtr *string
	if bestID != nil {
		bestPtr = bestID
	}
	status := strings.ToLower(firstNonEmpty(valueToString(base["status"]),
		valueToString(stateForBest["status"]), "created"))
	artifacts := p.artifactIndex(taskID, stateForBest)
	out := []map[string]any{}
	for _, ref := range artifacts {
		out = append(out, refToDict(ref))
	}
	return map[string]any{
		"task_id":        taskID,
		"status":         status,
		"best_node_id":   nullableAny(bestPtr),
		"usage":          usage,
		"artifact_index": out,
		"summary":        reportSummary(base),
	}
}

// GetTree 演进树（epoch 检查点形态 / gate 形态双分支，对齐 Python get_tree）。
func (p *HarnessProvider) GetTree(taskID string) map[string]any {
	state := p.loadYAML(taskID, stateFile)
	if state == nil {
		state = map[string]any{}
	}
	if checkpoints, ok := state["epoch_checkpoints"].([]any); ok {
		nodes := []map[string]any{}
		depths := map[string]int{"h0": 0}
		maxDepth := 0
		for i, event := range epochEvents(state) {
			node := nodeEventDict(event)
			if i > 0 {
				parentID := valueToString(node["parent_id"])
				d := depths[parentID] + 1
				depths[valueToString(node["node_id"])] = d
				if d > maxDepth {
					maxDepth = d
				}
			}
			nodes = append(nodes, node)
		}
		return map[string]any{
			"nodes": nodes, "depth": maxDepth, "iteration": len(checkpoints),
		}
	}
	stateGates := gatesOf(state)
	baseline := p.baselineScore(taskID, state)
	nodes := []map[string]any{{
		"node_id": "ROOT", "iteration": 0, "parent_id": nil, "type": "ROOT",
		"adopted": true, "score": baseline, "summary": "基线",
		"snapshot_artifact_id": nil, "reason": nil, "failure_class": nil,
		"changes": []any{}, "extra": map[string]any{},
	}}
	refsToNode := map[string]string{}
	for index, gate := range stateGates {
		nodeID := gateNodeID(gate)
		if nodeID == "" {
			nodeID = fmt.Sprintf("G%03d", index+1)
		}
		beforeRefs := valueToString(gate["before_harness_refs_path"])
		candidateRefs := valueToString(gate["candidate_harness_refs_path"])
		if candidateRefs != "" {
			if _, seen := refsToNode[candidateRefs]; !seen {
				refsToNode[candidateRefs] = nodeID
			}
		}
		parentID := "ROOT"
		if beforeRefs != "" {
			if mapped, ok := refsToNode[beforeRefs]; ok {
				parentID = mapped
			}
		}
		if parentID == nodeID {
			parentID = ""
		}
		gateStatus := strings.ToLower(firstNonEmpty(valueToString(gate["status"]), "rejected"))
		adopted := boolValue(gate["accepted"]) && gateStatus == "accepted"
		var parentPtr *string
		if parentID != "" {
			parentPtr = &parentID
		}
		extra := map[string]any{
			"before_harness_refs_path":    beforeRefs,
			"candidate_harness_refs_path": candidateRefs,
		}
		nodes = append(nodes, map[string]any{
			"node_id":              nodeID,
			"iteration":            firstNonEmpty(itoa(safeInt(gate["predicted_rank"])), itoa(index+1)),
			"parent_id":            parentPtr,
			"type":                 gateStatus,
			"adopted":              adopted,
			"score":                anyFloat(gate["candidate_score"]),
			"summary":              valueToString(gate["reason"]),
			"snapshot_artifact_id": nil,
			"reason":               valueToString(gate["reason"]),
			"failure_class":        nil,
			"changes":              []any{},
			"extra":                extra,
		})
	}
	depth := 0
	depths := map[string]int{"ROOT": 0}
	for _, node := range nodes {
		id := valueToString(node["node_id"])
		if id == "ROOT" {
			depths[id] = 0
			continue
		}
		parentID := ""
		if node["parent_id"] != nil {
			parentID = valueToString(node["parent_id"])
		}
		parentDepth, ok := depths[parentID]
		if !ok {
			// 乱序/孤儿 gate：保守按前序最大深度挂载，避免崩溃。
			parentDepth = maxIntMap(depths)
		}
		nodeDepth := parentDepth + 1
		depths[id] = nodeDepth
		if nodeDepth > depth {
			depth = nodeDepth
		}
	}
	return map[string]any{"nodes": nodes, "depth": depth, "iteration": len(stateGates)}
}

// LocateArtifact 产物定位（对齐 Python locate_artifact）。
func (p *HarnessProvider) LocateArtifact(taskID string, artifactID *string) map[string]any {
	state := p.loadYAML(taskID, stateFile)
	if state == nil {
		state = map[string]any{}
	}
	index := p.artifactIndex(taskID, state)
	wanted := ""
	if artifactID != nil {
		wanted = strings.TrimSpace(*artifactID)
	}
	for _, ref := range index {
		if wanted != "" && ref.ArtifactID() == wanted {
			return refToDict(ref)
		}
	}
	if wanted != "" {
		return map[string]any{"__error__": fmt.Sprintf("artifact 不存在: %s", wanted), "__code__": "PATH_INVALID"}
	}
	if len(index) > 0 {
		return refToDict(index[len(index)-1])
	}
	published := valueToString(state["published_harness_refs_path"])
	if published != "" {
		return refToDict(rsi.NewArtifactRef("published", nil, "published_harness_refs", "harness_refs", published, nil, nil))
	}
	return map[string]any{"__error__": fmt.Sprintf("任务 %s 尚无可定位产物", taskID), "__code__": "PATH_INVALID"}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// run 执行主链（对齐 Python _run 全步骤）。
func (p *HarnessProvider) run(ctx context.Context, request HarnessEngineRequest, onEvent rsi.OnEvent, resume bool) (map[string]any, error) {
	if err := p.validateMaterializedPaths(request); err != nil {
		return nil, err
	}
	if err := p.verifyTaskMaterials(request, resume); err != nil {
		return nil, err
	}
	orchestrator := p.orchestrator
	if orchestrator == nil {
		cfg, err := p.buildConfig(request)
		if err != nil {
			return nil, err
		}
		orchestrator, err = singleharness.NewSingleHarnessIterativeOptimizationOrchestrator(cfg, nil, nil, nil, nil)
		if err != nil {
			return nil, err
		}
	}
	// 新任务先测 H0；续跑保留原协议。
	previousState := map[string]any{}
	if resume {
		previousState = p.readStateDict(taskIDString(request))
		if previousState == nil {
			previousState = map[string]any{}
		}
	}
	autoFullBaseline := true
	if resume && len(previousState) > 0 {
		fp, _ := previousState["fingerprint"].(map[string]any)
		autoFullBaseline = boolValue(fp["auto_full_baseline"])
	}
	maxIter := request.MaxIterations
	engineRequest, err := singleharness.NewIterativeSingleHarnessRequest(
		request.DatasetFiles, request.HarnessRefsPath, expandUser(request.OutputDir),
		firstNonEmpty(request.DatasetID, "single_harness_benchmark"), resume, "", autoFullBaseline,
		request.TaskID, &maxIter)
	if err != nil {
		return nil, err
	}
	_, runErr := orchestrator.Run(ctx, engineRequest, onEvent)
	if runErr != nil {
		// 引擎侧 resume fingerprint/task_id 冲突以错误上抛，映射 4xx 语义。
		if resume && isResumeMismatch(runErr) {
			return nil, &RsiError{Code: "RESUME_MISMATCH", Message: runErr.Error()}
		}
		if !resume && isResumeMismatch(runErr) {
			return nil, NewBadRequest(runErr.Error(), nil)
		}
		return nil, runErr
	}
	return p.resultFromState(request.TaskID)
}

// taskIDString 提取请求任务 ID（可读性助手）。
func taskIDString(request HarnessEngineRequest) string { return request.TaskID }

// buildConfig 编排器配置装配（对齐 Python _build_config 全分支）。
func (p *HarnessProvider) buildConfig(request HarnessEngineRequest) (config.AutoCoordinatingHarnessConfig, error) {
	configPath := ""
	if request.OrchestratorConfigPath != nil {
		configPath = *request.OrchestratorConfigPath
	}
	if configPath == "" {
		configPath = p.orchestratorConfigPath
	}
	if configPath != "" {
		// 任务 profile 已含物化模型文件路径：生产服务调用时不被公开模型 ID 覆盖。
		return config.LoadAutoCoordinatingHarnessConfig(configPath)
	}
	if p.orchestratorConfig != nil {
		cfg, err := config.AutoCoordinatingHarnessConfigFromDict(p.orchestratorConfig)
		if err != nil {
			return config.AutoCoordinatingHarnessConfig{}, err
		}
		return p.applyModelRefs(cfg, request.ModelRefs)
	}
	cfg, err := config.LoadAutoCoordinatingHarnessConfig(filepath.Join(p.tasksRoot, defaultConfigPath))
	if err != nil {
		return config.AutoCoordinatingHarnessConfig{}, err
	}
	return p.applyModelRefs(cfg, request.ModelRefs)
}

// applyModelRefs 任务 model_refs（tester=评估 / optimizer=优化）翻译进
// ModelConfigs（对齐 Python _apply_model_refs）。
func (p *HarnessProvider) applyModelRefs(cfg config.AutoCoordinatingHarnessConfig, modelRefs map[string]any) (config.AutoCoordinatingHarnessConfig, error) {
	refs := map[string]string{}
	for k, v := range modelRefs {
		if s := strings.TrimSpace(valueToString(v)); s != "" {
			refs[k] = s
		}
	}
	if len(refs) == 0 {
		return cfg, nil
	}
	if tester, ok := refs["tester"]; ok {
		cfg.ModelConfigs.Evaluation = tester
		cfg.Evaluator.ModelConfigRef = tester
	}
	if optimizer, ok := refs["optimizer"]; ok {
		cfg.ModelConfigs.Analysis = optimizer
		cfg.EvaluationResultAnalyzer.ModelConfigRef = optimizer
		cfg.ModelConfigs.MemberOptimization = optimizer
		cfg.MemberOptimizer.ModelConfigRef = optimizer
	}
	return cfg, nil
}

// validateMaterializedPaths 任务私有路径封闭校验（对齐 Python _validate_materialized_paths）。
func (p *HarnessProvider) validateMaterializedPaths(request HarnessEngineRequest) error {
	if request.OrchestratorConfigPath == nil {
		return nil
	}
	taskRoot := expandUser(filepath.Join(p.tasksRoot, request.TaskID))
	if !strings.HasPrefix(taskRoot, expandUser(p.tasksRoot)) {
		return &RsiError{Code: "PATH_NOT_ALLOWED", Message: fmt.Sprintf("任务路径超出 RSI 根目录: %s", taskRoot)}
	}
	checks := []struct {
		label string
		path  string
	}{
		{"harness_refs", request.HarnessRefsPath},
		{"output", request.OutputDir},
		{"profile", *request.OrchestratorConfigPath},
	}
	for _, c := range checks {
		if c.path == "" {
			continue
		}
		path := expandUser(c.path)
		if !strings.HasPrefix(path, taskRoot) {
			return &RsiError{Code: "PATH_NOT_ALLOWED", Message: fmt.Sprintf("%s 路径超出任务目录: %s", c.label, path)}
		}
		if c.label != "output" && !fileExists(path) {
			return &RsiError{Code: "PATH_INVALID", Message: fmt.Sprintf("%s 文件不存在: %s", c.label, path)}
		}
	}
	for _, raw := range request.DatasetFiles {
		path := expandUser(raw)
		if !strings.HasPrefix(path, taskRoot) {
			return &RsiError{Code: "PATH_NOT_ALLOWED", Message: fmt.Sprintf("dataset 路径超出任务目录: %s", path)}
		}
		if !fileExists(path) {
			return &RsiError{Code: "PATH_INVALID", Message: fmt.Sprintf("dataset 文件不存在: %s", path)}
		}
	}
	return nil
}

// verifyTaskMaterials 任务快照与 harness refs 目标校验
// （对齐 Python _verify_task_materials；resume 时全部映射 RESUME_INPUT_CHANGED）。
func (p *HarnessProvider) verifyTaskMaterials(request HarnessEngineRequest, resume bool) error {
	taskFile := filepath.Join(p.tasksRoot, request.TaskID, "task.json")
	data, err := os.ReadFile(taskFile)
	if err != nil {
		return nil
	}
	var payload struct {
		Config struct {
			RsiMaterials map[string]any `json:"rsi_materials"`
		} `json:"config"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	materials := payload.Config.RsiMaterials
	if materials == nil {
		return nil
	}
	raise := func(message string) error {
		if resume {
			return &RsiError{Code: "RESUME_INPUT_CHANGED", Message: "resume " + message}
		}
		return &RsiError{Code: "PATH_NOT_ALLOWED", Message: message}
	}
	snapshot := func(role string) map[string]any {
		m, _ := materials[role].(map[string]any)
		return m
	}
	expected := map[string]string{
		"dataset": valueToString(snapshot("input_snapshot")["sha256"]),
		"harness": valueToString(snapshot("harness_snapshot")["sha256"]),
		"profile": valueToString(snapshot("profile")["sha256"]),
	}
	datasetPath := ""
	if len(request.DatasetFiles) > 0 {
		datasetPath = request.DatasetFiles[0]
	}
	profilePath := ""
	if request.OrchestratorConfigPath != nil {
		profilePath = *request.OrchestratorConfigPath
	}
	actual := map[string]string{
		"dataset": datasetPath,
		"harness": request.HarnessRefsPath,
		"profile": profilePath,
	}
	for _, role := range []string{"dataset", "harness", "profile"} {
		expectedHash := strings.TrimSpace(expected[role])
		path := expandUser(actual[role])
		if expectedHash == "" || !fileExists(path) {
			return raise(role + " 材料不可用")
		}
		hash, err := fileSha256(path)
		if err != nil || hash != expectedHash {
			return raise(role + " 材料已变化")
		}
	}
	// 数据集附带文件清单校验。
	inputSnapshot := snapshot("input_snapshot")
	if fileHashes, ok := inputSnapshot["files"].(map[string]any); ok && len(fileHashes) > 0 {
		datasetRoot := filepath.Dir(expandUser(actual["dataset"]))
		relatives := make([]string, 0, len(fileHashes))
		for rel := range fileHashes {
			relatives = append(relatives, rel)
		}
		sort.Strings(relatives)
		for _, relative := range relatives {
			expectedHash := valueToString(fileHashes[relative])
			path := filepath.Join(datasetRoot, relative)
			matches := false
			if fileExists(path) {
				if hash, err := fileSha256(path); err == nil {
					matches = hash == expectedHash
				}
			}
			if !matches {
				return raise(fmt.Sprintf("dataset file changed or missing: %s", relative))
			}
		}
	}
	// harness 快照（源/目标 + refs 目标一致性）。
	harnessSnapshot := snapshot("harness_snapshot")
	if harnessSnapshot != nil {
		sourceHash := strings.TrimSpace(valueToString(harnessSnapshot["source_sha256"]))
		sourcePath := expandUser(valueToString(harnessSnapshot["source_path"]))
		var expectedTarget *string
		if sourceHash != "" {
			if !fileExists(sourcePath) && !dirExists(sourcePath) {
				return raise("harness 源配置已变化")
			}
			if pathSha256(sourcePath) != sourceHash {
				return raise("harness 源配置已变化")
			}
			expectedTarget = &sourcePath
		}
		targetHash := strings.TrimSpace(valueToString(harnessSnapshot["target_sha256"]))
		targetPath := expandUser(valueToString(harnessSnapshot["package_path"]))
		if targetHash != "" {
			if !fileExists(targetPath) && !dirExists(targetPath) {
				return raise("harness 目标配置已变化")
			}
			if pathSha256(targetPath) != targetHash {
				return raise("harness 目标配置已变化")
			}
			expectedTarget = &targetPath
		}
		if err := p.verifyHarnessRefsTarget(expandUser(actual["harness"]), expectedTarget, resume); err != nil {
			return err
		}
	}
	// 模型 manifest 校验。
	if modelManifests, ok := materials["models"].(map[string]any); ok {
		roles := make([]string, 0, len(modelManifests))
		for role := range modelManifests {
			roles = append(roles, role)
		}
		sort.Strings(roles)
		for _, role := range roles {
			manifest, ok := modelManifests[role].(map[string]any)
			if !ok {
				continue
			}
			expectedHash := strings.TrimSpace(valueToString(manifest["config_sha256"]))
			path := expandUser(valueToString(manifest["path"]))
			if expectedHash != "" {
				if !fileExists(path) {
					return raise(fmt.Sprintf("model(%s) 材料已变化", role))
				}
				if hash, err := fileSha256(path); err != nil || hash != expectedHash {
					return raise(fmt.Sprintf("model(%s) 材料已变化", role))
				}
			}
		}
	}
	return nil
}

// verifyHarnessRefsTarget harness refs 必须恰好一条引用且目标与快照一致。
func (p *HarnessProvider) verifyHarnessRefsTarget(refsPath string, expectedSource *string, resume bool) error {
	raise := func(message string) error {
		if resume {
			return &RsiError{Code: "RESUME_INPUT_CHANGED", Message: "resume " + message}
		}
		return &RsiError{Code: "PATH_NOT_ALLOWED", Message: message}
	}
	data, err := os.ReadFile(refsPath)
	if err != nil {
		return raise(fmt.Sprintf("harness refs 不可读: %s", refsPath))
	}
	var payload map[string]any
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return raise(fmt.Sprintf("harness refs 不可读: %s", refsPath))
	}
	refs, ok := payload["harness_refs"].(map[string]any)
	if !ok || len(refs) != 1 {
		return raise(fmt.Sprintf("harness refs 必须恰好包含一条引用: %s", refsPath))
	}
	var rawTarget string
	for _, v := range refs {
		rawTarget = valueToString(v)
	}
	target := expandUser(rawTarget)
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(refsPath), target)
	}
	if !fileExists(target) && !dirExists(target) {
		return raise(fmt.Sprintf("harness 源配置不存在: %s", target))
	}
	if expectedSource != nil && target != *expectedSource {
		return raise("harness refs 目标与创建快照不一致")
	}
	return nil
}

// resultFromState 由状态文件归一出参（对齐 Python _result_from_state）。
func (p *HarnessProvider) resultFromState(taskID string) (map[string]any, error) {
	state := p.readStateDict(taskID)
	if state == nil {
		state = map[string]any{}
	}
	status := strings.ToLower(firstNonEmpty(valueToString(state["status"]), "completed"))
	if status != "completed" && status != "failed" {
		// 引擎 run() 正常返回即本轮执行结束。
		status = "completed"
	}
	var finalNodeID *string
	if bestNodeID(state) != nil {
		finalNodeID = bestNodeID(state)
	}
	var errorCode, errorMessage *string
	if status != "completed" {
		code := "ENGINE_FAILED"
		msg := "harness 引擎状态异常结束"
		errorCode, errorMessage = &code, &msg
	}
	result := rsi.NewEngineResult(taskID, rsi.RsiStatus(status), finalNodeID, errorCode, errorMessage)
	return map[string]any{
		"task_id":                     result.TaskID(),
		"status":                      string(result.Status()),
		"final_node_id":               nullableAny(result.FinalNodeID()),
		"error_code":                  nullableAny(result.ErrorCode()),
		"error_message":               nullableAny(result.ErrorMessage()),
		"best_score":                  anyFloat(state["best_score"]),
		"state_path":                  filepath.Join(p.tasksRoot, taskID, providerRunDir, stateFile),
		"report_path":                 filepath.Join(p.tasksRoot, taskID, providerRunDir, reportFile),
		"best_harness_refs_path":      valueToString(state["best_harness_refs_path"]),
		"published_harness_refs_path": valueToString(state["published_harness_refs_path"]),
		"current_harness_refs_path":   valueToString(state["current_harness_refs_path"]),
	}, nil
}

// readStateDict 原始状态 dict。
func (p *HarnessProvider) readStateDict(taskID string) map[string]any {
	return p.loadYAML(taskID, stateFile)
}

// baselineScore 基线分：state 优先，回退 source summary（对齐 Python _baseline_score）。
func (p *HarnessProvider) baselineScore(taskID string, state map[string]any) *float64 {
	if n := numberPtr(state["baseline_score"]); n != nil {
		return n
	}
	dataset, _ := state["dataset"].(map[string]any)
	if dataset == nil {
		return nil
	}
	expectedCases, ok := dataset["cases"]
	if !ok {
		return nil
	}
	summaryPath := filepath.Join(p.tasksRoot, taskID, providerRunDir, "evaluations", "e001", "b001", "source", "summary.json")
	data, err := os.ReadFile(summaryPath)
	if err != nil {
		return nil
	}
	var summary map[string]any
	if err := json.Unmarshal(data, &summary); err != nil {
		return nil
	}
	if summary == nil {
		return nil
	}
	// total_cases 与期望数一致才可信（对齐 Python 数值比较）。
	if numberVal(summary["total_cases"]) == nil || numberVal(expectedCases) == nil {
		return nil
	}
	if *numberVal(summary["total_cases"]) != *numberVal(expectedCases) {
		return nil
	}
	return numberVal(summary["average_score"])
}

// loadYAML 带缓存的 YAML 快照读取（对齐 Python _load_yaml：stamp 校验、
// 解析期间被替换不缓存、容量 32 LRU）。
func (p *HarnessProvider) loadYAML(taskID, name string) map[string]any {
	path := filepath.Join(p.tasksRoot, taskID, providerRunDir, name)
	p.snapshotLock.Lock()
	defer p.snapshotLock.Unlock()
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		delete(p.snapshotCache, path)
		return nil
	}
	stamp := fileStamp(info)
	if cached, ok := p.snapshotCache[path]; ok && cached.stamp == stamp {
		return deepCopyMap(cached.data)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		delete(p.snapshotCache, path)
		return nil
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil || parsed == nil {
		return nil
	}
	// 解析期间被替换/修改的快照不缓存。
	if after, err := os.Stat(path); err == nil {
		afterStamp := fileStamp(after)
		if afterStamp == stamp {
			p.snapshotCache[path] = snapshotEntry{stamp: stamp, data: parsed}
			p.snapshotOrder = append(p.snapshotOrder, path)
			if len(p.snapshotOrder) > 32 {
				oldest := p.snapshotOrder[0]
				p.snapshotOrder = p.snapshotOrder[1:]
				delete(p.snapshotCache, oldest)
			}
		}
	}
	return deepCopyMap(parsed)
}

// maxIntMap 取最大值（空表 0）。
func maxIntMap(m map[string]int) int {
	max := 0
	for _, v := range m {
		if v > max {
			max = v
		}
	}
	return max
}

// artifactIndex 产物索引（epoch 形态 / gate 形态双分支，对齐 Python _artifact_index）。
func (p *HarnessProvider) artifactIndex(taskID string, state map[string]any) []*rsi.ArtifactRef {
	refs := []*rsi.ArtifactRef{}
	if _, hasEpoch := state["epoch_checkpoints"]; hasEpoch {
		for _, event := range epochEvents(state) {
			node := eventNode(event)
			if valueToString(node["node_id"]) == "h0" || valueToString(node["type"]) == "ROOT" || !boolValue(node["adopted"]) {
				continue
			}
			extra, _ := node["extra"].(map[string]any)
			path := valueToString(extra["artifact_path"])
			if path != "" {
				id := valueToString(node["node_id"])
				refs = append(refs, rsi.NewArtifactRef(id, &id, id, "harness_refs", path, nil, nil))
			}
		}
		return refs
	}
	for index, gate := range gatesOf(state) {
		candidateRefs := valueToString(gate["candidate_harness_refs_path"])
		if candidateRefs == "" {
			continue
		}
		nodeID := gateNodeID(gate)
		if nodeID == "" {
			nodeID = fmt.Sprintf("G%03d", index+1)
		}
		name := filepath.Base(candidateRefs)
		refs = append(refs, rsi.NewArtifactRef(nodeID, &nodeID, name, "harness_refs", candidateRefs, nil, nil))
	}
	return refs
}

// ──────────────────────────── 模块级助手 ────────────────────────────

// validateDatasetPath 归一 + 引擎 load_cases 校验（对齐 engine_validate_input 主体）。
func validateDatasetPath(path string) ([]map[string]any, error) {
	normalized, err := NormalizeValidationSuite(path)
	if err != nil {
		return nil, err
	}
	loadPath := path
	if normalized != nil {
		// Evo-Bench 归一化用例 → 临时 cases.json + 附件复制。
		tempDir, err := os.MkdirTemp("", "rsi-dataset-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tempDir)
		normalizedPath := filepath.Join(tempDir, "cases.json")
		payload, err := json.Marshal(normalized)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(normalizedPath, append(payload, '\n'), 0o644); err != nil {
			return nil, err
		}
		dataloader.CopyDatasetFiles(casesFromAny(normalized["cases"]), path, normalizedPath)
		loadPath = normalizedPath
	}
	cases, err := singleharness.LoadCases([]string{loadPath})
	if err != nil {
		return nil, mapDatasetLoadError(err)
	}
	for _, c := range cases {
		if err := dataloader.ValidateCaseFields(c, true); err != nil {
			return nil, mapDatasetLoadError(err)
		}
	}
	return cases, nil
}

// mapDatasetLoadError 数据集装载错误 → 服务错误码。
func mapDatasetLoadError(err error) error {
	if os.IsNotExist(err) {
		return &RsiError{Code: "PATH_INVALID", Message: err.Error()}
	}
	return &RsiError{Code: "DATASET_INVALID", Message: err.Error()}
}

// epochEvents epoch 检查点形态事件序列（root + 各 checkpoint + active）。
func epochEvents(state map[string]any) []map[string]any {
	events := []map[string]any{nodeEventDict(singleharness.RootNodeEvent(state))}
	checkpoints, _ := state["epoch_checkpoints"].([]any)
	for _, item := range checkpoints {
		cp, _ := item.(map[string]any)
		if cp == nil {
			continue
		}
		events = append(events, nodeEventDict(singleharness.EpochNodeEvent(state, cp)))
	}
	if active := singleharness.ActiveEpochNodeEvent(state); active != nil {
		events = append(events, nodeEventDict(active))
	}
	return events
}

// nodeEventDict EventNode 事件 → node dict。
func nodeEventDict(event any) map[string]any {
	m, err := structToMap(event)
	if err != nil {
		return map[string]any{}
	}
	return m
}

// eventNode 事件取 node。
func eventNode(event map[string]any) map[string]any {
	node, _ := event["node"].(map[string]any)
	if node == nil {
		return event
	}
	return node
}

// bestNodeID 最佳节点（双形态，对齐 Python _best_node_id）。
func bestNodeID(state map[string]any) *string {
	checkpoints, hasEpoch := state["epoch_checkpoints"].([]any)
	if !hasEpoch {
		if id := gateNodeID(bestGate(gatesOf(state))); id != "" {
			return &id
		}
		return nil
	}
	bestRefs := valueToString(state["best_harness_refs_path"])
	for i := len(checkpoints) - 1; i >= 0; i-- {
		cp, _ := checkpoints[i].(map[string]any)
		if cp == nil {
			continue
		}
		if boolValue(cp["promotion_applied"]) && valueToString(cp["selected_harness_refs_path"]) == bestRefs {
			epoch := fmt.Sprintf("epoch-%03d", safeInt(cp["epoch"]))
			return &epoch
		}
	}
	h0 := "h0"
	return &h0
}

// gatesOf candidate_gates 列表（畸形防御）。
func gatesOf(raw map[string]any) []map[string]any {
	items, ok := raw["candidate_gates"].([]any)
	if !ok {
		return nil
	}
	gates := []map[string]any{}
	for _, item := range items {
		if g, ok := item.(map[string]any); ok {
			gates = append(gates, g)
		}
	}
	return gates
}

// bestGate 最高分 accepted gate。
func bestGate(gates []map[string]any) map[string]any {
	var best map[string]any
	bestScore := -1.0
	for _, gate := range gates {
		if !boolValue(gate["accepted"]) || valueToString(gate["status"]) != "accepted" {
			continue
		}
		score := 0.0
		if n := numberVal(gate["candidate_score"]); n != nil {
			score = *n
		}
		if best == nil || score > bestScore {
			best = gate
			bestScore = score
		}
	}
	return best
}

// gateNodeID gate 候选 ID。
func gateNodeID(gate map[string]any) string {
	if gate == nil {
		return ""
	}
	return strings.TrimSpace(valueToString(gate["candidate_id"]))
}

// numberPtr 数值宽松转指针。
func numberPtr(value any) *float64 { return numberVal(value) }

// anyFloat 数值转 any（nil 时归一真 nil，避免 typed-nil 接口）。
func anyFloat(value any) any {
	if n := numberVal(value); n != nil {
		return *n
	}
	return nil
}

// numberVal 数值宽松转换（nil/bool → nil）。
func numberVal(value any) *float64 {
	switch v := value.(type) {
	case nil, bool:
		return nil
	case float64:
		return &v
	case int:
		f := float64(v)
		return &f
	case int64:
		f := float64(v)
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

// reportSummary 报告摘要行（对齐 Python _report_summary）。
func reportSummary(report map[string]any) string {
	return fmt.Sprintf("candidate_count=%d best_score=%v baseline_score=%v",
		len(gatesOf(report)), report["best_score"], report["baseline_score"])
}

// refToDict ArtifactRef → dict（私有字段经访问器导出）。
func refToDict(ref *rsi.ArtifactRef) map[string]any {
	if ref == nil {
		return map[string]any{}
	}
	var nodeID any
	if ref.NodeID() != nil {
		nodeID = *ref.NodeID()
	}
	return map[string]any{
		"artifact_id":  ref.ArtifactID(),
		"node_id":      nodeID,
		"name":         ref.Name(),
		"kind":         ref.Kind(),
		"path":         ref.Path(),
		"sha256":       nullableAny(ref.Sha256()),
		"download_url": nullableAny(ref.DownloadURL()),
	}
}

// deepCopyMap JSON 深拷贝（对齐 Python deepcopy 共享快照保护）。
func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

// casesFromAny any → cases 列表。
func casesFromAny(v any) []map[string]any {
	items, _ := v.([]any)
	out := []map[string]any{}
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// expandUser ~ 展开。
func expandUser(path string) string {
	if strings.HasPrefix(path, "~") {
		return filepath.Join(userHomeDir(), strings.TrimPrefix(path, "~"))
	}
	return path
}

// dirExists 目录存在性。
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// pathSha256 文件或包目录内容寻址摘要（对齐 Python _path_sha256：目录按
// 相对路径排序逐项 hash(length+relative+file hash)）。
func pathSha256(path string) string {
	if fileExists(path) {
		h, _ := fileSha256(path)
		return h
	}
	digest := sha256.New()
	var files []string
	_ = filepath.Walk(path, func(item string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		files = append(files, item)
		return nil
	})
	sort.Slice(files, func(i, j int) bool {
		rel := func(p string) string { return strings.ReplaceAll(strings.TrimPrefix(p, path+"/"), "\\", "/") }
		return rel(files[i]) < rel(files[j])
	})
	for _, item := range files {
		relative, _ := filepath.Rel(path, item)
		relative = strings.ReplaceAll(relative, "\\", "/")
		digest.Write([]byte(fmt.Sprintf("%08x", len(relative))))
		digest.Write([]byte(relative))
		h, _ := fileSha256(item)
		raw, _ := hex.DecodeString(h)
		digest.Write(raw)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// isResumeMismatch 引擎 resume fingerprint/task_id 冲突判定。
func isResumeMismatch(err error) bool {
	return err != nil && strings.Contains(err.Error(), "fingerprint")
}

// fileStamp 文件指纹（ino/mtime/size；Windows 下 ctime 不可用时退化为 0）。
func fileStamp(info os.FileInfo) [4]int64 {
	ino, ctime := fileStampParts(info)
	return [4]int64{ino, info.ModTime().UnixNano(), ctime, info.Size()}
}
