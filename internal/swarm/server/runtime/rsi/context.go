package rsi

// RsiServiceContext：服务域组合根（内部 v3 §7 复用清单）。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/context.py（340 行）。
// AgentServer 侧用 BuildRsiServiceContext 一次性构建，再注入推送回调与
// harness_refs 快照提供方。

import (
	"os"
	"path/filepath"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ServiceContext 服务域组件容器（组合根）。
type ServiceContext struct {
	// TasksRoot 任务根目录。
	TasksRoot string
	// Store 任务存储。
	Store *TaskStore
	// Projector 进度/树投影。
	Projector *Projector
	// UsageRecorder 用量记账。
	UsageRecorder *UsageRecorder
	// ArtifactService 产物快照。
	ArtifactService *ArtifactService
	// ArtifactFilesService 产物文件浏览。
	ArtifactFilesService *ArtifactFilesService
	// Worker 单队列 worker。
	Worker *Worker
	// Adapters 场景适配器表（C5 注入）。
	Adapters map[string]ScenarioAdapter
	// TaskService / DatasetService / TreeService / UsageService / ReportService /
	// ArtifactDownloadService 六薄服务。
	TaskService             *TaskService
	DatasetService          *DatasetService
	TreeService             *TreeService
	UsageService            *UsageService
	ReportService           *ReportService
	ArtifactDownloadService *ArtifactDownloadService
	// HarnessActivationStore / HarnessInstaller 安装链。
	HarnessActivationStore *HarnessActivationStore
	HarnessInstaller       *HarnessInstaller
	// WorkspaceRecovery 重启对账。
	workspaceRecovery *WorkspaceRecovery
	recovered         bool
	recoverySummary   map[string]any
	// HarnessMaterializer / ModelResolver 生产物化依赖（mock/test 根为 nil）。
	HarnessMaterializer *TaskMaterializer
	ModelResolver       *ModelConfigResolver
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewServiceContext 构造组合根（adapters 由 RegisterAdapters 注入）。
func NewServiceContext(tasksRoot string, workerPushCallbacks map[string]func(string, string, map[string]any) error,
	harnessMaterializer *TaskMaterializer, modelResolver *ModelConfigResolver) *ServiceContext {
	store := NewTaskStore(tasksRoot, nil)
	projector := NewProjector(tasksRoot)
	usageRecorder := NewUsageRecorder()
	artifactService := NewArtifactService(tasksRoot)
	activationStore := NewHarnessActivationStore(tasksRoot)
	artifactSink := artifactSinkAdapter{artifactService}
	worker := NewWorker(store, map[string]ScenarioAdapter{}, usageRecorder, projector, artifactSink, workerPushCallbacks)
	adapterResolver := func(scenario, artifactType string) ScenarioAdapter {
		return workerAdapterFor(worker, scenario, artifactType)
	}
	installer := NewHarnessInstaller(store, adapterResolver, nil, activationStore)
	ctx := &ServiceContext{
		TasksRoot: tasksRoot, Store: store, Projector: projector,
		UsageRecorder: usageRecorder, ArtifactService: artifactService,
		ArtifactFilesService:    NewArtifactFilesService(store, artifactService),
		Worker:                  worker,
		Adapters:                workerAdapters(worker),
		TaskService:             NewTaskService(store, adapterResolver, harnessMaterializer, modelResolver, activationStore),
		DatasetService:          NewDatasetService(nil, adapterResolver),
		TreeService:             NewTreeService(projector, store, adapterResolver),
		UsageService:            NewUsageService(usageRecorder, store, adapterResolver),
		ReportService:           NewReportService(store, projector, usageRecorder, artifactService, adapterResolver),
		ArtifactDownloadService: NewArtifactDownloadService(artifactService, store, adapterResolver),
		HarnessActivationStore:  activationStore,
		HarnessInstaller:        installer,
		HarnessMaterializer:     harnessMaterializer,
		ModelResolver:           modelResolver,
	}
	ctx.workspaceRecovery = NewWorkspaceRecovery(store, projector, providerReaderOf(ctx))
	return ctx
}

// BuildRsiServiceContext 默认组合根（tasksRoot 空 → 工作区 .uapclawswarm/rsi/tasks；
// enableHarnessMaterialization 时装配生产物化器与解析器）。
func BuildRsiServiceContext(tasksRoot string, enableHarnessMaterialization bool, allowMissingHarness bool) *ServiceContext {
	useProduction := enableHarnessMaterialization
	if tasksRoot == "" {
		tasksRoot = filepath.Join(workspaceRoot(), "rsi", "tasks")
		useProduction = true
	}
	var materializer *TaskMaterializer
	var resolver *ModelConfigResolver
	if useProduction {
		materializer = NewTaskMaterializer(tasksRoot,
			configuredRoot("RSI_DATASET_ROOT"), configuredRoot("RSI_HARNESS_ROOT"), allowMissingHarness)
		resolver = NewModelConfigResolver()
	}
	return NewServiceContext(tasksRoot, nil, materializer, resolver)
}

// RegisterAdapters 注册场景适配器。
func (c *ServiceContext) RegisterAdapters(adapters map[string]ScenarioAdapter) {
	for k, v := range adapters {
		c.Adapters[k] = v
	}
}

// RegisterHarnessProvider 在 RSI 缝注册生产 HarnessProvider。
func (c *ServiceContext) RegisterHarnessProvider(provider HarnessProviderContract) *HarnessEngineAdapter {
	adapter := NewHarnessEngineAdapter(provider)
	c.RegisterAdapters(map[string]ScenarioAdapter{"HARNESS": adapter})
	return adapter
}

// BindHarnessInstaller 绑定 AgentServer 广播（安装操作使用）。
func (c *ServiceContext) BindHarnessInstaller(broadcast BroadcastFunc) {
	c.HarnessInstaller.broadcast = broadcast
}

// BindDatasetService 绑定 dataset 校验 adapter。
func (c *ServiceContext) BindDatasetService(adapter ScenarioAdapter) {
	c.DatasetService.adapter = adapter
}

// BindTaskService 注入 harness_refs 提供方（对齐 Python bind_task_service）。
func (c *ServiceContext) BindTaskService(harnessRefsProvider func(params map[string]any) (string, error)) {
	c.TaskService.BindTaskService(harnessRefsProvider)
}

// RegisterWorkerPush 增补 worker 推送回调。
func (c *ServiceContext) RegisterWorkerPush(cb map[string]func(string, string, map[string]any) error) {
	c.Worker.RegisterPushCallbacks(cb)
}

// AdapterFor 场景/type → adapter（公开场景对）。
func (c *ServiceContext) AdapterFor(scenario, artifactType string) ScenarioAdapter {
	return workerAdapterFor(c.Worker, scenario, artifactType)
}

// AdapterForTask 按任务取 adapter。
func (c *ServiceContext) AdapterForTask(taskID string) ScenarioAdapter {
	if taskID == "" {
		return nil
	}
	task, err := c.Store.Get(taskID)
	if err != nil {
		return nil
	}
	return c.AdapterFor(task.Scenario, ptrStr(task.ArtifactType))
}

// EnsureRoot 进度树根注册（I4/tree + P2/P3 同源）。
func (c *ServiceContext) EnsureRoot(taskID string) {
	c.Projector.RegisterRoot(taskID, nil, "基线")
}

// RecoverWorkspace 一次性 AgentServer 重启对账。
func (c *ServiceContext) RecoverWorkspace() map[string]any {
	if c.recovered {
		return c.recoverySummary
	}
	summary := c.workspaceRecovery.Recover()
	c.recovered = true
	c.recoverySummary = map[string]any{
		"scanned": summary.Scanned, "recovered": summary.Recovered,
		"paused": summary.Paused, "completed": summary.Completed,
		"failed": summary.Failed, "terminated": summary.Terminated,
	}
	return c.recoverySummary
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// artifactSinkAdapter ArtifactService → 事件消费 ArtifactSink 缝。
type artifactSinkAdapter struct{ s *ArtifactService }

// MakeSnapshot 快照代理。
func (a artifactSinkAdapter) MakeSnapshot(taskID, ref, nodeID string, artifacts []RsiArtifactPath) string {
	return a.s.MakeSnapshot(taskID, ref, nodeID, artifacts)
}

// Locate 快照定位代理。
func (a artifactSinkAdapter) Locate(taskID, artifactID string) (string, bool) {
	file, err := a.s.Locate(taskID, &artifactID)
	if err != nil {
		return "", false
	}
	return file.Path, true
}

// workerAdapters worker 内部 adapter 表引用。
func workerAdapters(w *Worker) map[string]ScenarioAdapter { return w.adapters }

// workerAdapterFor worker 场景查找（对齐 Python adapter_for）。
func workerAdapterFor(w *Worker, scenario, artifactType string) ScenarioAdapter {
	scenarioKey := upper(strings.TrimSpace(scenario))
	artifactKey := upper(strings.TrimSpace(artifactType))
	var candidates []string
	if scenarioKey == "ARTIFACT" {
		if artifactKey != "" {
			candidates = append(candidates, "ARTIFACT:"+artifactKey, "artifact:"+lower(artifactKey))
		}
		candidates = append(candidates, "ARTIFACT")
	} else if scenarioKey != "" {
		candidates = append(candidates, scenarioKey, lower(scenarioKey))
	}
	for _, key := range candidates {
		if a := w.adapters[key]; a != nil {
			return a
		}
	}
	return nil
}

// providerReaderOf 恢复通道 adapter 缝（read_state）。
func providerReaderOf(c *ServiceContext) func(scenario, artifactType string) ProviderStateReader {
	return func(scenario, artifactType string) ProviderStateReader {
		adapter := c.AdapterFor(scenario, artifactType)
		if adapter == nil {
			return nil
		}
		return readerAdapter{adapter}
	}
}

// readerAdapter read_state 缝适配。
type readerAdapter struct{ a ScenarioAdapter }

// ReadState 快照读取。
func (r readerAdapter) ReadState(taskID string) map[string]any { return r.a.ReadState(taskID) }

// workspaceRoot 用户工作区（品牌映射 uapclawswarm）。
func workspaceRoot() string {
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".uapclawswarm")
	}
	return filepath.Join(".", ".uapclawswarm")
}

// configuredRoot 可选受信输入根（env 非强制）。
func configuredRoot(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return ""
	}
	if a, err := filepath.Abs(expandUser(value)); err == nil {
		return a
	}
	return value
}
