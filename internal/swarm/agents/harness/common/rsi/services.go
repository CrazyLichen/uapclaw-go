package rsi

// 派生薄封装服务（内部 v3 §4.7）。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/services.py（997 行）。
// 薄封装：委托 store / projector / usage / artifact / adapter。
// I7/I8/I9 + C1 为中优先级预留：接口已落位，引擎衔接/单价算法不实现。
//
// ⚠️ D-9-1 连带：Python 的 provider_state_to_progress / provider_report_to_web
// / provider_usage_to_dict 属 artifact_adapter（ARTIFACT 轨裁剪）；HARNESS 路径
// 的 provider 报告为 dict 形态，此处以同形状最小等价实现承载。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskService 任务命令/查询（I2/I3/I4/I5）。
type TaskService struct {
	store                  *TaskStore
	adapterResolver        func(scenario, artifactType string) ScenarioAdapter
	harnessRefsProvider    func(params map[string]any) (string, error)
	harnessMaterializer    *TaskMaterializer
	modelResolver          *ModelConfigResolver
	harnessActivationStore *HarnessActivationStore
}

// NewTaskService 构造。
func NewTaskService(store *TaskStore, adapterResolver func(scenario, artifactType string) ScenarioAdapter,
	harnessMaterializer *TaskMaterializer, modelResolver *ModelConfigResolver,
	harnessActivationStore *HarnessActivationStore) *TaskService {
	return &TaskService{store: store, adapterResolver: adapterResolver,
		harnessMaterializer: harnessMaterializer, modelResolver: modelResolver,
		harnessActivationStore: harnessActivationStore}
}

// BindTaskService 注入 refs 提供方/物化器/解析器（对齐 Python bind_task_service）。
func (s *TaskService) BindTaskService(harnessRefsProvider func(params map[string]any) (string, error)) {
	s.harnessRefsProvider = harnessRefsProvider
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Create rsi.task.create（web §6.1 三分支校验）。返回 {task_id, status=CREATED}。
func (s *TaskService) Create(params map[string]any) (map[string]any, error) {
	scenarioRaw := upper(strings.TrimSpace(valueToString(params["scenario"])))
	var artifactTypeRaw *string
	if v := upper(strings.TrimSpace(valueToString(params["artifact_type"]))); v != "" {
		artifactTypeRaw = &v
	}
	scenario, artifactType, err := ValidateScenario(scenarioRaw, artifactTypeRaw, scenarioRaw == "ARTIFACT")
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(valueToString(params["name"]))
	if name == "" || len(name) > 100 {
		return nil, NewBadRequest("实验名称必填且 1–100 字符", nil)
	}
	modelRefs, _ := params["model_refs"].(map[string]any)
	if modelRefs == nil {
		return nil, NewBadRequest("model_refs 必填", nil)
	}
	optimizer := strings.TrimSpace(valueToString(modelRefs["optimizer"]))
	if optimizer == "" {
		return nil, NewBadRequest("model_refs.optimizer 必填", nil)
	}
	if *scenario == ScenarioHarness {
		if strings.TrimSpace(valueToString(modelRefs["tester"])) == "" {
			return nil, NewBadRequest("harness 优化必填 model_refs.tester", nil)
		}
	}
	requestedMaxIterations := params["max_iterations"]
	// search_width 已废弃；保留引擎缺省。
	searchWidth := 1
	var harnessProfileOptions map[string]any
	if *scenario == ScenarioHarness {
		opts, err := harnessProfileOptionsOf(params)
		if err != nil {
			return nil, err
		}
		harnessProfileOptions = opts
	}
	var optimizationInstruction *string
	if params["optimization_instruction"] != nil {
		v := strings.TrimSpace(valueToString(params["optimization_instruction"]))
		if v != "" {
			optimizationInstruction = &v
		}
	}
	webProxy := normalizeWebProxy(params["web_proxy"])
	if webProxy != nil && !(*scenario == ScenarioArtifact && *artifactType == ArtifactTypePaper) {
		return nil, NewBadRequest("web_proxy 仅支持论文优化", nil)
	}
	// 场景字段校验（web §6.1 规则②③）。
	var inputFile, artifactPath *string
	if *scenario == ScenarioHarness {
		v := strings.TrimSpace(firstNonEmpty(valueToString(params["input_file"]), valueToString(params["dataset_path"])))
		if v == "" {
			return nil, NewBadRequest("harness 优化必填 input_file（数据集）", nil)
		}
		inputFile = &v
		if params["artifact_path"] != nil {
			return nil, NewBadRequest("harness 分支不接受 artifact_path", nil)
		}
	} else {
		v := strings.TrimSpace(firstNonEmpty(valueToString(params["artifact_path"]), valueToString(params["input_file"])))
		if v != "" {
			artifactPath = &v
		}
		if *artifactType == ArtifactTypeProgram && artifactPath == nil {
			return nil, NewBadRequest("PROGRAM 必填 artifact_path", nil)
		}
		if *artifactType == ArtifactTypePaper {
			if params["input_file"] != nil {
				vv := strings.TrimSpace(valueToString(params["input_file"]))
				inputFile = &vv
			}
			if artifactPath == nil && optimizationInstruction == nil {
				return nil, NewBadRequest("PAPER 至少需要 artifact_path 或 optimization_instruction", nil)
			}
		}
	}
	// 优化指令仅产物·论文可填。
	if *scenario == ScenarioHarness && optimizationInstruction != nil {
		return nil, NewBadRequest("harness 优化不接受 optimization_instruction", nil)
	}
	if *scenario == ScenarioArtifact && *artifactType == ArtifactTypeProgram && optimizationInstruction != nil {
		return nil, NewBadRequest("PROGRAM 优化不接受 optimization_instruction", nil)
	}
	selectedAdapter := s.adapterFor(*scenario, artifactType)
	if *scenario == ScenarioArtifact && selectedAdapter != nil {
		if err := ensureProviderValid(selectedAdapter.ValidateInput(artifactPath, string(*scenario), stringPtrEnum(artifactType))); err != nil {
			return nil, err
		}
	}
	// Program 任务束自带搜索预算（D-9-1 裁剪：PROGRAM 轨随 17.3 回填）。
	if requestedMaxIterations == nil && *scenario == ScenarioArtifact && *artifactType == ArtifactTypeProgram {
		requestedMaxIterations = programManifestMaxIterations(artifactPath)
	}
	maxIterations, err := positiveInt(requestedMaxIterations, 1, "max_iterations")
	if err != nil {
		return nil, err
	}
	taskID := GenerateTaskID()
	runDir := filepath.Join(s.store.TasksRoot(), taskID, "run")
	var harnessRefsPath *string
	var materialization *TaskMaterialization
	if *scenario == ScenarioHarness && s.materializationEnabled() {
		sourceHarness, err := s.resolveHarnessSource(params)
		if err != nil {
			return nil, err
		}
		if sourceHarness == nil && !s.harnessMaterializer.AllowMissingHarness() {
			return nil, &RsiError{Code: "INVALID_HARNESS", Message: "当前没有可用的活动 Harness 配置"}
		}
		var validator func(*string) (RsiDatasetResult, error)
		if selectedAdapter != nil {
			validator = func(path *string) (RsiDatasetResult, error) {
				return selectedAdapter.ValidateInput(path, "HARNESS", nil)
			}
		}
		materialization, err = s.harnessMaterializer.Materialize(taskID, *inputFile, sourceHarness,
			map[string]string{"optimizer": optimizer, "tester": valueToString(modelRefs["tester"])},
			s.modelResolver, valueToString(harnessProfileOptions["domain"]), harnessProfileOptions,
			validator, maxIterations, searchWidth)
		if err != nil {
			// 物化先于 task.json 落盘：拒绝的 create 请求不得遗留私有模型/配置文件。
			removeUncommittedTaskDir(s.store.TasksRoot(), taskID)
			return nil, err
		}
		p := valueToString(materialization.Dataset["path"])
		inputFile = &p
		if rp := strings.TrimSpace(valueToString(materialization.Harness["path"])); rp != "" {
			harnessRefsPath = &rp
		}
	} else if *scenario == ScenarioHarness && s.harnessRefsProvider != nil {
		// 物化前/mock 契约：轻量 provider 直接暴露激活 refs 路径。
		sourceHarness, err := s.resolveHarnessSource(params)
		if err != nil {
			return nil, err
		}
		harnessRefsPath = sourceHarness
	}
	config := map[string]any{
		"harness_refs_path":        nullableAny(harnessRefsPath),
		"artifact_path":            nullableAny(artifactPath),
		"optimization_instruction": nullableAny(optimizationInstruction),
		"artifact_type":            stringOrEmpty(artifactType),
		"results":                  map[string]any{},
		// 物化 refs 是任务私有不可变输入而非发布/激活产物：不得阻断终态任务删除。
		"active_ref_released": materialization != nil,
	}
	if webProxy != nil {
		// URL 任务私有：公开投影只暴露布尔，凭据不泄漏到 UI。
		config["web_proxy"] = webProxy
	}
	if materialization != nil {
		config["orchestrator_config_path"] = materialization.Profile["path"]
		config["dataset_id"] = firstNonEmpty(valueToString(materialization.Dataset["dataset_id"]), "single_harness_benchmark")
		config["rsi_training_options"] = harnessProfileOptions
		config["rsi_materials"] = materialization.ToManifest()
	}
	// WebSocket 会话是传输元数据但必须随任务留存，供异步事件回源创建页。
	rsiSessionID := strings.TrimSpace(firstNonEmpty(valueToString(params["_rsi_session_id"]), valueToString(params["session_id"])))
	if rsiSessionID != "" {
		config["rsi_session_id"] = rsiSessionID
	}
	task := RsiTask{
		TaskID: taskID, Name: name, Scenario: string(*scenario),
		ArtifactType: stringPtrFromEnum(artifactType), InputFile: inputFile,
		ModelRefs:     map[string]any{"optimizer": optimizer, "tester": firstNonEmpty(valueToString(modelRefs["tester"]))},
		MaxIterations: maxIterations, SearchWidth: searchWidth,
		OptimizationInstruction: optimizationInstruction, ArtifactPath: artifactPath,
		Config: config, RunDir: runDir, Status: string(TaskStatusCreated), CreatedAt: UtcnowISO(),
	}
	// run_dir 建好（引擎产出落点）。
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, err
	}
	if _, err := s.store.Create(task); err != nil {
		return nil, err
	}
	return map[string]any{"task_id": taskID, "status": string(TaskStatusCreated)}, nil
}

// List rsi.task.list（web §6.2 六字段投影 + 可选过滤）。
func (s *TaskService) List(params map[string]any) ([]map[string]any, error) {
	var scenarioEnum *Scenario
	var artifactEnum *ArtifactType
	if params["scenario"] != nil && valueToString(params["scenario"]) != "" {
		scenarioValue := upper(strings.TrimSpace(valueToString(params["scenario"])))
		var artifactValue *string
		if valueToString(params["artifact_type"]) != "" {
			v := upper(strings.TrimSpace(valueToString(params["artifact_type"])))
			artifactValue = &v
		}
		sc, at, err := ValidateScenario(scenarioValue, artifactValue, false)
		if err != nil {
			return nil, err
		}
		scenarioEnum, artifactEnum = sc, at
		if *sc == ScenarioHarness && *at != "" {
			return nil, NewBadRequest("harness 过滤不接受 artifact_type", nil)
		}
	} else if valueToString(params["artifact_type"]) != "" {
		v := upper(strings.TrimSpace(valueToString(params["artifact_type"])))
		if v != "PAPER" && v != "PROGRAM" {
			return nil, NewBadRequest(fmt.Sprintf("artifact_type 非法: %s", valueToString(params["artifact_type"])), nil)
		}
		ae := ArtifactType(v)
		artifactEnum = &ae
	}
	scenarioFilter := ""
	if scenarioEnum != nil {
		scenarioFilter = string(*scenarioEnum)
	}
	artifactFilter := ""
	if artifactEnum != nil && *artifactEnum != "" {
		artifactFilter = string(*artifactEnum)
	}
	tasks := s.store.List(scenarioFilter, artifactFilter)
	out := []map[string]any{}
	for i := range tasks {
		out = append(out, tasks[i].ListProjection())
	}
	return out, nil
}

// Get rsi.task.get（web §6.3：config + progress + best_artifact + usage）。
func (s *TaskService) Get(params map[string]any, projector *Projector, usageRecorder *UsageRecorder,
	artifactService *ArtifactService, adapter ScenarioAdapter) (map[string]any, error) {
	taskID := strings.TrimSpace(valueToString(params["task_id"]))
	if taskID == "" {
		return nil, NewBadRequest("task_id 必填", nil)
	}
	task, err := s.store.Get(taskID)
	if err != nil {
		return nil, err
	}
	if adapter == nil && s.adapterResolver != nil {
		adapter = s.adapterResolver(task.Scenario, ptrStr(task.ArtifactType))
	}
	if adapter != nil {
		state := readProviderSnapshotDict(adapter.ReadState, taskID)
		report := readProviderSnapshotDict(adapter.ReadReport, taskID)
		if len(state) > 0 || len(report) > 0 {
			var progress map[string]any
			if len(state) > 0 {
				progress = providerStateToProgress(state)
			}
			var usage map[string]any
			if len(state) > 0 {
				usage = providerUsageToDict(state["usage"])
			}
			if usage == nil && len(report) > 0 {
				usage = providerUsageToDict(report["usage"])
			}
			bestArtifact := providerBestArtifact(report)
			if bestArtifact == nil {
				bestArtifact = artifactService.BestArtifact(taskID)
			}
			return task.GetProjection(progress, usage, bestArtifact), nil
		}
		// CREATED 任务在 Provider 首个快照前合法无快照：走服务侧投影。
	}
	var progress map[string]any
	if p, err := projector.DeriveProgress(taskID); err == nil {
		progress = p
	}
	usage := usageRecorder.UsageSummary(taskID)
	bestArtifact := artifactService.BestArtifact(taskID)
	return task.GetProjection(progress, usage, bestArtifact), nil
}

// Delete rsi.task.delete（web §6.4，一致性规则 §8.2）。
func (s *TaskService) Delete(params map[string]any, worker *Worker) (map[string]any, error) {
	taskID := strings.TrimSpace(valueToString(params["task_id"]))
	if taskID == "" {
		return nil, NewBadRequest("task_id 必填", nil)
	}
	task, err := s.store.Get(taskID)
	if err != nil {
		return nil, err
	}
	if s.harnessActivationStore != nil {
		for _, version := range s.harnessActivationStore.ListVersions() {
			if strings.TrimSpace(valueToString(version["task_id"])) == taskID {
				return nil, NewTaskStateConflict(fmt.Sprintf("任务 %s 持有保留的 Harness 版本，不可删除", taskID))
			}
		}
	}
	if task.Status == string(TaskStatusQueued) || task.Status == string(TaskStatusPaused) {
		if valueToString(task.Config["harness_refs_path"]) != "" && task.Config["active_ref_released"] != true {
			return nil, NewTaskStateConflict(fmt.Sprintf("任务 %s 产物仍在生效，不可删除", taskID))
		}
		if task.Status == string(TaskStatusQueued) && worker != nil {
			if _, err := worker.Cancel(taskID, "terminate"); err != nil {
				return nil, err
			}
		} else {
			if _, err := s.store.UpdateStatus(taskID, []string{task.Status}, string(TaskStatusTerminated), "delete"); err != nil {
				return nil, err
			}
		}
	}
	if err := s.store.Delete(taskID, true, true); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// Start rsi.training.start（web §7.1）。状态机 + 入队；引擎装配看 adapter。
func (s *TaskService) Start(params map[string]any, worker *Worker) (map[string]any, error) {
	taskID := strings.TrimSpace(valueToString(params["task_id"]))
	if taskID == "" {
		return nil, NewBadRequest("task_id 必填", nil)
	}
	if _, err := s.store.Get(taskID); err != nil { // 存在性校验 → TASK_NOT_FOUND
		return nil, err
	}
	status, err := worker.Enqueue(taskID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": status}, nil
}

// Pause rsi.training.pause（中优先级 I7）。
func (s *TaskService) Pause(params map[string]any, worker *Worker) (map[string]any, error) {
	taskID := strings.TrimSpace(valueToString(params["task_id"]))
	if taskID == "" {
		return nil, NewBadRequest("task_id 必填", nil)
	}
	status, err := worker.Cancel(taskID, "pause")
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": status}, nil
}

// Resume rsi.training.resume（中优先级 I8；fingerprint 校验在 provider）。
func (s *TaskService) Resume(params map[string]any, worker *Worker) (map[string]any, error) {
	taskID := strings.TrimSpace(valueToString(params["task_id"]))
	if taskID == "" {
		return nil, NewBadRequest("task_id 必填", nil)
	}
	status, err := worker.Resume(taskID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": status}, nil
}

// Terminate rsi.training.terminate（中优先级 I9）。
func (s *TaskService) Terminate(params map[string]any, worker *Worker) (map[string]any, error) {
	taskID := strings.TrimSpace(valueToString(params["task_id"]))
	if taskID == "" {
		return nil, NewBadRequest("task_id 必填", nil)
	}
	status, err := worker.Cancel(taskID, "terminate")
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": status}, nil
}

// ──────────────────────────── Dataset/Report/Tree/Usage/Download ────────────────────────────

// DatasetService rsi.dataset.validate（I1 主入口）。
type DatasetService struct {
	adapter         ScenarioAdapter
	adapterResolver func(scenario, artifactType string) ScenarioAdapter
}

// NewDatasetService 构造。
func NewDatasetService(adapter ScenarioAdapter, adapterResolver func(scenario, artifactType string) ScenarioAdapter) *DatasetService {
	return &DatasetService{adapter: adapter, adapterResolver: adapterResolver}
}

// Validate web §5.1 出参：{valid, sample_count, errors[]}。
func (s *DatasetService) Validate(params map[string]any, adapter ScenarioAdapter) (map[string]any, error) {
	inputFile := strings.TrimSpace(firstNonEmpty(valueToString(params["input_file"]), valueToString(params["dataset_path"])))
	if inputFile == "" {
		return nil, NewBadRequest("input_file 必填", nil)
	}
	scenario := upper(strings.TrimSpace(valueToString(params["scenario"])))
	var artifactType *string
	if v := upper(strings.TrimSpace(valueToString(params["artifact_type"]))); v != "" {
		artifactType = &v
	}
	if _, _, err := ValidateScenario(scenario, artifactType, scenario == "ARTIFACT"); err != nil {
		return nil, err
	}
	selected := adapter
	if selected == nil {
		selected = s.adapter
	}
	if selected == nil && s.adapterResolver != nil {
		selected = s.adapterResolver(scenario, stringOrEmptyPtr(artifactType))
	}
	if selected != nil {
		result, err := selected.ValidateInput(&inputFile, scenario, artifactType)
		if err != nil {
			return nil, err
		}
		return result.ToDict(), nil
	}
	result, err := DefaultValidateInput(inputFile)
	if err != nil {
		return nil, err
	}
	return result.ToDict(), nil
}

// ReportService rsi.report.get（I10）。
type ReportService struct {
	store           *TaskStore
	projector       *Projector
	usageRecorder   *UsageRecorder
	artifactService *ArtifactService
	adapterResolver func(scenario, artifactType string) ScenarioAdapter
}

// NewReportService 构造。
func NewReportService(store *TaskStore, projector *Projector, usageRecorder *UsageRecorder,
	artifactService *ArtifactService, adapterResolver func(scenario, artifactType string) ScenarioAdapter) *ReportService {
	return &ReportService{store: store, projector: projector, usageRecorder: usageRecorder,
		artifactService: artifactService, adapterResolver: adapterResolver}
}

// Get web §8.1 出参。未运行（无引擎 report）时提供任务级兜底投影。
func (s *ReportService) Get(params map[string]any, adapter ScenarioAdapter) (map[string]any, error) {
	taskID := strings.TrimSpace(valueToString(params["task_id"]))
	if taskID == "" {
		return nil, NewBadRequest("task_id 必填", nil)
	}
	task, err := s.store.Get(taskID)
	if err != nil {
		return nil, err
	}
	if adapter == nil && s.adapterResolver != nil {
		adapter = s.adapterResolver(task.Scenario, ptrStr(task.ArtifactType))
	}
	if adapter != nil {
		state := readProviderSnapshotDict(adapter.ReadState, taskID)
		report := readProviderSnapshotDict(adapter.ReadReport, taskID)
		if len(report) > 0 || len(state) > 0 {
			return providerReportToWeb(report, state), nil
		}
		// CREATED 任务合法地尚无 Provider report。
	}
	progress, perr := s.projector.DeriveProgress(taskID)
	if perr != nil {
		progress = map[string]any{}
	}
	usage := s.usageRecorder.UsageSummary(taskID)
	bestArtifact := s.artifactService.BestArtifact(taskID)
	results, _ := task.Config["results"].(map[string]any)
	return map[string]any{
		"status":     task.Status,
		"best_score": results["best_score"],
		"baseline":   progress["baseline"],
		"metrics": map[string]any{
			"eval_passed": 0, "eval_total": 0, "pruned_count": 0,
			"iterations": progress["iteration"],
		},
		"usage":          usage,
		"best_artifact":  bestArtifact,
		"report_summary": "",
		"markdown":       nil,
	}, nil
}

// TreeService rsi.tree.get（I13：事件投影 + Provider 快照重建）。
type TreeService struct {
	projector       *Projector
	store           *TaskStore
	adapterResolver func(scenario, artifactType string) ScenarioAdapter
}

// NewTreeService 构造。
func NewTreeService(projector *Projector, store *TaskStore,
	adapterResolver func(scenario, artifactType string) ScenarioAdapter) *TreeService {
	return &TreeService{projector: projector, store: store, adapterResolver: adapterResolver}
}

// Get 树查询：内存投影器可丢弃——先恢复持久视图再取 Provider 回填数据。
func (s *TreeService) Get(params map[string]any, adapter ScenarioAdapter) (map[string]any, error) {
	taskID := strings.TrimSpace(valueToString(params["task_id"]))
	if taskID == "" {
		return nil, NewBadRequest("task_id 必填", nil)
	}
	var task *RsiTask
	if s.store != nil {
		if t, err := s.store.Get(taskID); err == nil {
			task = &t
		}
	}
	if adapter == nil && task != nil && s.adapterResolver != nil {
		adapter = s.adapterResolver(task.Scenario, ptrStr(task.ArtifactType))
	}
	s.projector.LoadFromDisk(taskID)
	s.projector.RegisterRoot(taskID, nil, "基线")
	// D-9-1 连带：_is_completed_program_artifact 属 PROGRAM 轨（随 17.3），
	// HARNESS 批恒跳过 threshold 停止对账。
	if adapter != nil {
		tree := readProviderSnapshotDict(adapter.GetTree, taskID)
		if len(tree) > 0 {
			return s.projector.MergeProviderTree(taskID, tree)
		}
	}
	return s.projector.DeriveTree(taskID)
}

// UsageService rsi.usage.get（I11；C1 单价预留）。
type UsageService struct {
	usageRecorder   *UsageRecorder
	store           *TaskStore
	adapterResolver func(scenario, artifactType string) ScenarioAdapter
}

// NewUsageService 构造。
func NewUsageService(usageRecorder *UsageRecorder, store *TaskStore,
	adapterResolver func(scenario, artifactType string) ScenarioAdapter) *UsageService {
	return &UsageService{usageRecorder: usageRecorder, store: store, adapterResolver: adapterResolver}
}

// Get 运行中事件消费视角最富；重启后回退 Provider 持久快照。
func (s *UsageService) Get(params map[string]any, adapter ScenarioAdapter) (map[string]any, error) {
	taskID := strings.TrimSpace(valueToString(params["task_id"]))
	if taskID == "" {
		return nil, NewBadRequest("task_id 必填", nil)
	}
	var task *RsiTask
	if s.store != nil {
		if t, err := s.store.Get(taskID); err == nil {
			task = &t
		}
	}
	if adapter == nil && task != nil && s.adapterResolver != nil {
		adapter = s.adapterResolver(task.Scenario, ptrStr(task.ArtifactType))
	}
	if adapter != nil {
		if out, err := s.usageRecorder.Get(taskID); err == nil {
			return out, nil
		}
		state := readProviderSnapshotDict(adapter.ReadState, taskID)
		report := readProviderSnapshotDict(adapter.ReadReport, taskID)
		var usage map[string]any
		if len(report) > 0 {
			usage = providerUsageToDict(report["usage"])
		}
		if usage == nil && len(state) > 0 {
			usage = providerUsageToDict(state["usage"])
		}
		if usage != nil {
			return map[string]any{"usage": usage, "per_iteration": []any{}, "usage_by_node": map[string]any{}}, nil
		}
	}
	return s.usageRecorder.Get(taskID)
}

// ArtifactDownloadService rsi.artifact.download 定位文件或产物目录。
type ArtifactDownloadService struct {
	artifactService *ArtifactService
	store           *TaskStore
	adapterResolver func(scenario, artifactType string) ScenarioAdapter
}

// NewArtifactDownloadService 构造。
func NewArtifactDownloadService(artifactService *ArtifactService, store *TaskStore,
	adapterResolver func(scenario, artifactType string) ScenarioAdapter) *ArtifactDownloadService {
	return &ArtifactDownloadService{artifactService: artifactService, store: store, adapterResolver: adapterResolver}
}

// Locate 返回文件/目录信息；HTTP 文件流由通道层完成。
func (s *ArtifactDownloadService) Locate(params map[string]any, adapter ScenarioAdapter) (map[string]any, error) {
	taskID := strings.TrimSpace(valueToString(params["task_id"]))
	if taskID == "" {
		return nil, NewBadRequest("task_id 必填", nil)
	}
	task, err := s.store.Get(taskID)
	if err != nil {
		return nil, err
	}
	if adapter == nil && s.adapterResolver != nil {
		adapter = s.adapterResolver(task.Scenario, ptrStr(task.ArtifactType))
	}
	var artifactID *string
	if v := strings.TrimSpace(valueToString(params["artifact_id"])); v != "" {
		artifactID = &v
	}
	if adapter != nil {
		return s.locateProvider(taskID, artifactID, adapter, task.Scenario, params)
	}
	artifact, err := s.artifactService.Locate(taskID, artifactID)
	if err != nil {
		return nil, err
	}
	// 消费成功 → 放行后续 delete（下载即消费）。
	if artifactID == nil || artifact.IsBest {
		_ = s.store.MarkActiveRefReleased(taskID)
	}
	result := map[string]any{
		"path": artifact.Path, "kind": artifact.Kind, "is_best": artifact.IsBest,
		"filename":     filepath.Base(artifact.Path),
		"is_directory": dirExists(artifact.Path),
	}
	if result["is_directory"] == false {
		for k, v := range downloadFields(artifact.Path, params) {
			result[k] = v
		}
	}
	return result, nil
}

// locateProvider Provider 产物定位（路径封闭校验 + best 判定）。
func (s *ArtifactDownloadService) locateProvider(taskID string, artifactID *string,
	adapter ScenarioAdapter, scenario string, params map[string]any) (map[string]any, error) {
	raw := adapter.LocateArtifact(taskID, artifactID)
	if code := valueToString(raw["__code__"]); code != "" {
		if code == "PATH_INVALID" {
			return nil, &RsiError{Code: "ARTIFACT_NOT_FOUND", Message: valueToString(raw["__error__"])}
		}
		return nil, &RsiError{Code: "ARTIFACT_NOT_FOUND", Message: valueToString(raw["__error__"])}
	}
	if len(raw) == 0 {
		return nil, &RsiError{Code: "ARTIFACT_NOT_FOUND", Message: "Provider 返回了无法识别的产物引用"}
	}
	pathValue := valueToString(raw["path"])
	if pathValue != "" {
		candidate := expandUser(pathValue)
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(taskRunDir(s.store, taskID), candidate)
		}
		pathValue = candidate
	}
	if pathValue == "" {
		return nil, &RsiError{Code: "ARTIFACT_NOT_FOUND", Message: "Provider 返回了无法识别的产物引用"}
	}
	taskDir := expandUser(filepath.Join(s.store.TasksRoot(), taskID))
	if !strings.HasPrefix(pathValue, taskDir) {
		return nil, &RsiError{Code: "PATH_INVALID", Message: "Provider 产物路径超出任务目录"}
	}
	if !fileExists(pathValue) && !dirExists(pathValue) {
		return nil, &RsiError{Code: "ARTIFACT_NOT_FOUND", Message: fmt.Sprintf("产物路径不存在: %s", pathValue)}
	}
	isBest := artifactID == nil
	if !isBest {
		report := readProviderSnapshotDict(adapter.ReadReport, taskID)
		bestNodeID := valueToString(report["best_node_id"])
		isBest = bestNodeID != "" && valueToString(raw["node_id"]) == bestNodeID
	}
	if isBest {
		_ = s.store.MarkActiveRefReleased(taskID)
	}
	kind := "artifact_package"
	if upper(scenario) == "HARNESS" {
		kind = "harness_plugin"
	}
	result := map[string]any{
		"path": pathValue, "kind": kind, "is_best": isBest,
		"filename":     filepath.Base(pathValue),
		"is_directory": dirExists(pathValue),
	}
	if result["is_directory"] == false {
		for k, v := range downloadFields(pathValue, params) {
			result[k] = v
		}
	}
	return result, nil
}

// ──────────────────────────── 模块级助手 ────────────────────────────

// normalizeWebProxy web_proxy 归一（非法形态 → 错误）。
func normalizeWebProxy(value any) *string {
	switch v := value.(type) {
	case nil:
		return nil
	case bool:
		if !v {
			return nil
		}
		return strPtr("")
	case map[string]any:
		if len(v) == 0 {
			return nil
		}
		data, _ := json.Marshal(v)
		return strPtr(string(data))
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return strPtr(strings.TrimSpace(v))
	default:
		return nil
	}
}

// harnessProfileOptionsOf 直连 API 的 Generic Harness 训练控件校验
// （对齐 Python _harness_profile_options）。
func harnessProfileOptionsOf(params map[string]any) (map[string]any, error) {
	nested, _ := params["training_options"].(map[string]any)
	source := nested
	if source == nil {
		source = map[string]any{}
	}
	value := func(name string, def any) any {
		if v, ok := params[name]; ok {
			return v
		}
		if v, ok := source[name]; ok {
			return v
		}
		return def
	}
	domainRaw := lower(strings.TrimSpace(valueToString(value("domain", ""))))
	if domainRaw != "" && domainRaw != "general" && domainRaw != "office" {
		return nil, NewBadRequest("domain 只支持 general 或 office", nil)
	}
	executionMode := lower(strings.TrimSpace(firstNonEmpty(valueToString(value("execution_mode", "local")), "local")))
	if executionMode == "auto" {
		executionMode = "local"
	}
	if executionMode != "local" {
		return nil, &RsiError{Code: "UNSUPPORTED_PARAMETER", Message: "AgentServer Generic Harness 适配目前只支持 execution_mode=local"}
	}
	evaluationMethod := lower(strings.TrimSpace(valueToString(value("evaluation_method", "script-based"))))
	evaluationMethod = strings.ReplaceAll(evaluationMethod, "-", "_")
	if evaluationMethod != "script_based" && evaluationMethod != "exact_match" && evaluationMethod != "llm_as_judge" {
		return nil, &RsiError{Code: "UNSUPPORTED_PARAMETER", Message: "evaluation_method must be script_based, exact_match or llm_as_judge"}
	}
	siblingCandidateCount, err := positiveInt(value("sibling_candidate_count", 1), 1, "sibling_candidate_count")
	if err != nil {
		return nil, err
	}
	improverPolicyRef := strings.TrimSpace(valueToString(value("improver_policy_ref", "")))
	if siblingCandidateCount != 1 || improverPolicyRef != "" {
		return nil, &RsiError{Code: "UNSUPPORTED_PARAMETER",
			Message: "single-harness optimization requires one candidate and no improver evolution policy"}
	}
	maxEpochs, err := positiveInt(value("max_epochs", 1), 1, "max_epochs")
	if err != nil {
		return nil, err
	}
	batchSize, err := positiveInt(value("batch_size", 1), 1, "batch_size")
	if err != nil {
		return nil, err
	}
	maxIssueAttempts, err := nonNegativeInt(value("max_issue_attempts", 8), 8, "max_issue_attempts")
	if err != nil {
		return nil, err
	}
	maxRepairRounds, err := positiveInt(value("max_repair_rounds", 3), 3, "max_repair_rounds")
	if err != nil {
		return nil, err
	}
	rolloutConcurrency, err := positiveInt(value("rollout_concurrency", 1), 1, "rollout_concurrency")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"domain": domainRaw, "improver_policy_ref": improverPolicyRef,
		"execution_mode": executionMode, "evaluation_method": evaluationMethod,
		"max_epochs": maxEpochs, "batch_size": batchSize,
		"max_issue_attempts": maxIssueAttempts, "max_repair_rounds": maxRepairRounds,
		"sibling_candidate_count": siblingCandidateCount, "rollout_concurrency": rolloutConcurrency,
	}, nil
}

// positiveInt 正整数参数（对齐 Python _positive_int）。
func positiveInt(raw any, def int, field string) (int, error) {
	if raw == nil {
		return def, nil
	}
	if boolValue(raw) {
		return 0, NewBadRequest(field+" 必须是正整数", nil)
	}
	value := safeInt(raw)
	if _, ok := raw.(float64); ok && raw.(float64) != float64(int(raw.(float64))) {
		return 0, NewBadRequest(field+" 必须是正整数", nil)
	}
	if value < 1 {
		return 0, NewBadRequest(field+" 必须是正整数", nil)
	}
	return value, nil
}

// nonNegativeInt 非负整数参数。
func nonNegativeInt(raw any, def int, field string) (int, error) {
	if raw == nil {
		return def, nil
	}
	value := safeInt(raw)
	if value < 0 {
		return 0, NewBadRequest(field+" 必须是非负整数", nil)
	}
	return value, nil
}

// programManifestMaxIterations PROGRAM bundle 搜索预算（D-9-1：随 17.3 消费）。
func programManifestMaxIterations(path *string) any {
	if path == nil || *path == "" {
		return nil
	}
	candidate := expandUser(*path)
	if !dirExists(candidate) {
		return nil
	}
	manifestPath := filepath.Join(candidate, "task.json")
	if !fileExists(manifestPath) {
		return nil
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil
	}
	if manifest == nil || manifest["max_iterations"] == nil {
		return nil
	}
	v, err := positiveInt(manifest["max_iterations"], 1, "task.json.max_iterations")
	if err != nil {
		return nil
	}
	return v
}

// ensureProviderValid 校验结果 → 服务错误（对齐 Python _ensure_provider_valid）。
func ensureProviderValid(result RsiDatasetResult, err error) error {
	if err != nil {
		return err
	}
	if result.Valid {
		return nil
	}
	return NewDatasetInvalid(firstNonEmpty(firstErrorReason(result.Errors), "Provider 输入校验失败"), result.Errors)
}

// firstErrorReason 首条错误原因。
func firstErrorReason(errors []map[string]string) string {
	if len(errors) > 0 {
		return errors[0]["reason"]
	}
	return ""
}

// removeUncommittedTaskDir 清理未提交任务目录（对齐 Python _remove_uncommitted_task_dir）。
func removeUncommittedTaskDir(tasksRoot, taskID string) {
	if taskID == "" || filepath.Base(taskID) != taskID {
		return
	}
	_ = os.RemoveAll(filepath.Join(tasksRoot, taskID))
}

// resolveHarnessSource 新任务的受信 Harness 来源解析（harness_path 为
// AgentServer 侧受控后门；正常浏览器路径走组合根 provider）。
func (s *TaskService) resolveHarnessSource(params map[string]any) (*string, error) {
	explicit := strings.TrimSpace(valueToString(params["harness_path"]))
	if explicit != "" {
		return &explicit, nil
	}
	if s.harnessRefsProvider == nil {
		return nil, nil
	}
	value, err := s.harnessRefsProvider(params)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	v := strings.TrimSpace(value)
	return &v, nil
}

// adapterFor 场景/type → adapter。
func (s *TaskService) adapterFor(scenario Scenario, artifactType *ArtifactType) ScenarioAdapter {
	if s.adapterResolver == nil {
		return nil
	}
	at := ""
	if artifactType != nil {
		at = string(*artifactType)
	}
	return s.adapterResolver(string(scenario), at)
}

// materializationEnabled 物化启用判定（对齐 _harness_materialization_enabled）。
func (s *TaskService) materializationEnabled() bool {
	return s.harnessMaterializer != nil && s.modelResolver != nil
}

// stringOrEmptyPtr 枚举指针 → 值。
func stringOrEmptyPtr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// stringPtrFromEnum 枚举指针 → *string。
func stringPtrFromEnum(p *ArtifactType) *string {
	if p == nil || *p == "" {
		return nil
	}
	s := string(*p)
	return &s
}

// stringPtrEnum 枚举指针 → *string（adapter 契约形态）。
func stringPtrEnum(p *ArtifactType) *string {
	if p == nil || *p == "" {
		return nil
	}
	s := string(*p)
	return &s
}

// stringOrEmpty 枚举指针 → 空串缺省。
func stringOrEmpty(p *ArtifactType) any {
	if p == nil {
		return nil
	}
	return string(*p)
}

// taskRunDir 任务运行目录。
func taskRunDir(store *TaskStore, taskID string) string {
	return filepath.Join(store.TasksRoot(), taskID, "run")
}

// downloadFields 短时效 HTTP 下载令牌（对齐 Python _download_fields；
// 令牌签名由通道层 web_file_download 实现，此处透传参数占位——接线随 17.9c）。
func downloadFields(path string, params map[string]any) map[string]any {
	return map[string]any{
		"download_url":   "",
		"download_token": "",
		"path":           path,
	}
}

// readProviderSnapshotDict Provider 快照读取（缺失/错误回 nil，保留服务兜底）。
func readProviderSnapshotDict(reader func(string) map[string]any, taskID string) map[string]any {
	defer func() { _ = recover() }()
	state := reader(taskID)
	if state == nil {
		return nil
	}
	if code := valueToString(state["error_code"]); upper(code) == "TASK_NOT_FOUND" {
		return nil
	}
	return state
}

// providerStateToProgress EngineState dict → progress 投影（HARNESS 最小等价，
// D-9-1 连带；字段对齐 Python provider_state_to_progress 的 web §6.3 形状）。
func providerStateToProgress(state map[string]any) map[string]any {
	return map[string]any{
		"iteration":        safeInt(state["iteration"]),
		"total_iterations": safeInt(state["total_iterations"]),
		"score":            anyFloat(state["score"]),
		"baseline":         anyFloat(state["baseline"]),
	}
}

// providerUsageToDict 用量 dict 归一（nil 安全）。
func providerUsageToDict(raw any) map[string]any {
	m, _ := raw.(map[string]any)
	if m == nil {
		return nil
	}
	return m
}

// providerBestArtifact report dict → best_artifact 对象。
func providerBestArtifact(report map[string]any) map[string]any {
	if report == nil {
		return nil
	}
	bestID := valueToString(report["best_node_id"])
	if bestID == "" {
		return nil
	}
	return map[string]any{"artifact_id": bestID, "adopted": true}
}

// providerReportToWeb EngineReport/State dict → web §8.1 出参（HARNESS 最小等价）。
func providerReportToWeb(report, state map[string]any) map[string]any {
	base := report
	if len(base) == 0 {
		base = state
	}
	status := lower(firstNonEmpty(valueToString(base["status"]), valueToString(state["status"]), "created"))
	return map[string]any{
		"status":     status,
		"best_score": anyFloat(base["score"]),
		"baseline":   anyFloat(base["baseline"]),
		"metrics": map[string]any{
			"eval_passed": 0, "eval_total": 0, "pruned_count": 0,
			"iterations": safeInt(base["iteration"]),
		},
		"usage":          providerUsageToDict(base["usage"]),
		"best_artifact":  providerBestArtifact(base),
		"report_summary": valueToString(base["summary"]),
		"markdown":       nil,
	}
}

// sortedIntKeys int 键排序助手。
func sortedIntKeys(m map[int]bool) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}
