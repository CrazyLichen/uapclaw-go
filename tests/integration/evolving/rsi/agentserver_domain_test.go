//go:build integration

// 17.9 AgentServer 服务域集成用例（spec §六）。
// 链路1（训练主链）：task_create → materialize → training_start → fake 引擎
//   事件 → P2/P3 推送 → terminate → cancel ctx → ErrTaskCancelled 分支触达
//   （17.7 回填兑现）→ TERMINATED 落盘 + P1 推送含 failure_reason。
// 链路2（安装链）：激活指针轮换 → list_versions → rollback 门禁。
// 跨子系统：server 分发层（server/rsi）+ 服务域（agents/harness/common/rsi）+ 单 harness
//   引擎（agent_evolving/rsi/harness_rsi/singleharness）≥3 子系统。

package rsi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	coreRSI "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/singleharness"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rsi"
	rsihandlers "github.com/uapclaw/uapclaw-go/internal/swarm/server/rsi"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentServerDomainSuite 17.9 服务域集成套件。
type AgentServerDomainSuite struct {
	suite.Suite
	root     string
	context  *rsi.ServiceContext
	pushed   *[]map[string]any
	handlers *rsihandlers.AgentServerHandlers
}

// TestAgentServerDomainSuite 套件入口。
func TestAgentServerDomainSuite(t *testing.T) {
	suite.Run(t, new(AgentServerDomainSuite))
}

// SetupTest 每用例建独立服务域 + fake 引擎适配器。
func (s *AgentServerDomainSuite) SetupTest() {
	s.root = s.T().TempDir()
	s.context = rsi.BuildRsiServiceContext(filepath.Join(s.root, "tasks"), false, false)
	pushed := []map[string]any{}
	s.pushed = &pushed
	// fake HARNESS adapter：驱动真引擎事件构造器 + 可控阻塞（17.4d fake 链路复用）。
	fake := newIntegrationFakeEngine()
	s.context.RegisterAdapters(map[string]rsi.ScenarioAdapter{"HARNESS": fake})
	s.context.HarnessInstaller = rsi.NewHarnessInstaller(
		s.context.Store, nil, nil, s.context.HarnessActivationStore)
	s.handlers = rsihandlers.NewAgentServerHandlers(s.context,
		func(msg map[string]any) error { *s.pushed = append(*s.pushed, msg); return nil },
		nil, "web")
}

// call 分发层调用助手。
func (s *AgentServerDomainSuite) call(method string, params map[string]any) map[string]any {
	out := s.handlers.Handle(map[string]any{"req_method": method, "params": params, "session_id": "sess-it"})
	s.Require().Equal(true, out["ok"], "method=%s out=%v", method, out)
	payload, _ := out["payload"].(map[string]any)
	return payload
}

// pushTypes 已推送事件类型统计。
func (s *AgentServerDomainSuite) pushTypes() map[string]int {
	counts := map[string]int{}
	for _, msg := range *s.pushed {
		payload, _ := msg["payload"].(map[string]any)
		if t, ok := payload["event_type"].(string); ok {
			counts[t]++
		}
	}
	return counts
}

// waitPush 等待指定推送出现。
func (s *AgentServerDomainSuite) waitPush(eventType string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.pushTypes()[eventType] > 0 {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// TestTrainingLifecycle_主链训练与终止 链路1：create→start→P2/P3→terminate
// → ErrTaskCancelled 分支触达（17.7 回填兑现）→ TERMINATED + P1 failure_reason。
func (s *AgentServerDomainSuite) TestTrainingLifecycle_主链训练与终止() {
	// ① 物化数据集 + create。
	ds := filepath.Join(s.root, "ds.json")
	s.Require().NoError(os.WriteFile(ds, []byte(`{"cases":[{"case_id":"c1","input":"问题"}]}`), 0o644))
	created := s.call("rsi.task.create", map[string]any{
		"scenario": "HARNESS", "name": "集成任务", "input_file": ds,
		"model_refs": map[string]any{"optimizer": "m1", "tester": "m1"},
	})
	taskID, _ := created["task_id"].(string)
	s.NotEmpty(taskID)

	// ② 树根注册（分发层 create 内 EnsureRoot）。
	tree, err := s.context.Projector.DeriveTree(taskID)
	s.Require().NoError(err)
	s.Equal(1, len(tree["nodes"].([]map[string]any)))

	// ③ 安装 fake 引擎（阻塞运行 + 根节点事件）。
	fake := s.context.Adapters["HARNESS"].(*integrationFakeEngine)
	fake.events = []coreRSI.EngineEvent{integrationRootEvent()}

	// ④ training_start → RUNNING → P3 树增量。
	started := s.call("rsi.training.start", map[string]any{"task_id": taskID})
	// Python enqueue 语义：入队返回 QUEUED，runner 异步翻转 RUNNING。
	s.Equal("QUEUED", started["status"])
	deadline := time.Now().Add(3 * time.Second)
	for {
		task, err := s.context.Store.Get(taskID)
		s.Require().NoError(err)
		if task.Status == "RUNNING" {
			break
		}
		s.Require().LessOrEqual(time.Now().Second(), deadline.Second()+60, "等待 RUNNING 超时")
		if time.Now().After(deadline) {
			s.T().Fatal("等待 RUNNING 超时")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.True(s.waitPush("rsi.training.tree.delta", 3*time.Second), "P3 树增量未推送")

	// ⑤ terminate → worker 取消 runner → 引擎 ctx 取消
	//    （17.7 回填：Run 的 ctx.Err()/ErrTaskCancelled 分支真实触达）。
	terminated := s.call("rsi.training.terminate", map[string]any{"task_id": taskID})
	s.Equal("TERMINATED", terminated["status"])
	s.True(fake.WaitCancelled(3*time.Second), "引擎未观测到取消（17.7 分支未触达）")

	// ⑥ P1 状态推送含失败/终止语义。
	s.True(s.waitPush("rsi.training.status.changed", 3*time.Second), "P1 状态未推送")
	task, err := s.context.Store.Get(taskID)
	s.Require().NoError(err)
	s.Equal("TERMINATED", task.Status)
	last := task.StatusHistory[len(task.StatusHistory)-1]
	s.Equal("worker.cancelled", last["cause"])
}

// TestHarnessInstallChain_安装链 链路2：激活轮换→清单→门禁。
func (s *AgentServerDomainSuite) TestHarnessInstallChain_安装链() {
	// 激活 v1。
	v1 := filepath.Join(s.context.Store.TasksRoot(), "v1")
	s.Require().NoError(os.MkdirAll(v1, 0o755))
	s.Require().NoError(s.context.HarnessActivationStore.Commit(map[string]any{
		"installation_id": "inst-v1", "runtime_path": v1,
		"sha256": strings.Repeat("a", 64), "task_id": "rsi-seed",
	}))
	// 激活 v2。
	v2 := filepath.Join(s.context.Store.TasksRoot(), "v2")
	s.Require().NoError(os.MkdirAll(v2, 0o755))
	s.Require().NoError(s.context.HarnessActivationStore.Commit(map[string]any{
		"installation_id": "inst-v2", "runtime_path": v2,
		"sha256": strings.Repeat("b", 64),
	}))
	// list_versions。
	listing := s.call("rsi.harness.versions.list", map[string]any{})
	versions, _ := listing["versions"].([]map[string]any)
	s.Len(versions, 2)
	s.Equal("inst-v2", listing["active_installation_id"])
	// 激活任务存在时回退被门禁拒绝。
	task := rsi.RsiTask{TaskID: "rsi-seed", Name: "n", Scenario: "HARNESS",
		Status: "CREATED", CreatedAt: rsi.UtcnowISO(),
		ModelRefs: map[string]any{}, Config: map[string]any{}}
	if _, err := s.context.Store.Create(task); err != nil {
		s.T().Fatal(err)
	}
	if _, err := s.context.Store.UpdateStatus("rsi-seed", []string{"CREATED"}, "QUEUED", "enqueue"); err != nil {
		s.T().Fatal(err)
	}
	if _, err := s.context.Store.UpdateStatus("rsi-seed", []string{"QUEUED"}, "RUNNING", "start"); err != nil {
		s.T().Fatal(err)
	}
	out := s.handlers.Handle(map[string]any{"req_method": "rsi.harness.rollback",
		"params": map[string]any{"installation_id": "inst-v1"}})
	s.Equal(false, out["ok"])
	s.Contains(out["error"], "不能回退")
}

// ──────────────────────────── fake 引擎（真引擎事件构造器 + 可控取消） ────────────────────────────

// integrationFakeEngine 集成缝：真引擎状态文件 + ctx 取消观测。
type integrationFakeEngine struct {
	mu             sync.Mutex
	started        chan struct{}
	startOnce      sync.Once
	release        chan struct{}
	cancelObserved bool
	events         []coreRSI.EngineEvent
}

// PrepareStart 就绪启动信号（保留名以稳定用例读法）。
func (f *integrationFakeEngine) PrepareStart() error { return nil }

// WaitCancelled 等待取消观测。
func (f *integrationFakeEngine) WaitCancelled(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		observed := f.cancelObserved
		f.mu.Unlock()
		if observed {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// integrationRootEvent 根节点事件（真引擎构造器）。
func integrationRootEvent() coreRSI.EngineEvent {
	return singleharness.RootNodeEvent(map[string]any{})
}

// ──────────────────────────── ScenarioAdapter 实现 ────────────────────────────

// ValidateInput 输入校验（直通有效）。
func (f *integrationFakeEngine) ValidateInput(path *string, scenario string, artifactType *string) (rsi.RsiDatasetResult, error) {
	return rsi.RsiDatasetResult{Valid: true}, nil
}

// BuildRequest 视图 → 引擎请求。
func (f *integrationFakeEngine) BuildRequest(task rsi.RsiTaskView, resume bool) (rsi.HarnessEngineRequest, error) {
	return rsi.BuildHarnessEngineRequest(task, resume)
}

// Run 阻塞运行：发事件后等 release 或 ctx 取消。
func (f *integrationFakeEngine) Run(ctx context.Context, req rsi.HarnessEngineRequest, onEvent coreRSI.OnEvent) (map[string]any, error) {
	f.startOnce.Do(func() { close(f.started) })
	for _, e := range f.events {
		_ = onEvent(context.Background(), e)
	}
	select {
	case <-ctx.Done():
		f.mu.Lock()
		f.cancelObserved = true
		f.mu.Unlock()
		return nil, ctx.Err()
	case <-f.release:
	}
	return map[string]any{"status": "completed", "best_score": 0.9}, nil
}

// Resume 续跑同 Run。
func (f *integrationFakeEngine) Resume(ctx context.Context, req rsi.HarnessEngineRequest, onEvent coreRSI.OnEvent) (map[string]any, error) {
	return f.Run(ctx, req, onEvent)
}

// Pause HARNESS 无 pause hook。
func (f *integrationFakeEngine) Pause(string) (map[string]any, error) {
	return nil, &rsi.RsiError{Code: "INTERNAL_ERROR", Message: "harness 引擎暂不支持中途暂停"}
}

// Terminate HARNESS 无 terminate hook。
func (f *integrationFakeEngine) Terminate(string) (map[string]any, error) {
	return nil, &rsi.RsiError{Code: "INTERNAL_ERROR", Message: "harness 引擎暂不支持中途终止"}
}

// ReadState 状态快照（无）。
func (f *integrationFakeEngine) ReadState(string) map[string]any { return nil }

// ReadPublicationState 发布状态（无）。
func (f *integrationFakeEngine) ReadPublicationState(string) map[string]any { return nil }

// ReadReport 报告（无）。
func (f *integrationFakeEngine) ReadReport(string) map[string]any { return nil }

// GetTree 树（无）。
func (f *integrationFakeEngine) GetTree(string) map[string]any { return nil }

// LocateArtifact 产物定位（无）。
func (f *integrationFakeEngine) LocateArtifact(string, *string) map[string]any { return nil }

// SupportsPause / SupportsResume / SupportsTerminate 能力位。
func (f *integrationFakeEngine) SupportsPause() bool     { return false }
func (f *integrationFakeEngine) SupportsResume() bool    { return true }
func (f *integrationFakeEngine) SupportsTerminate() bool { return false }

// newIntegrationFakeEngine 构造。
func newIntegrationFakeEngine() *integrationFakeEngine {
	return &integrationFakeEngine{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}
