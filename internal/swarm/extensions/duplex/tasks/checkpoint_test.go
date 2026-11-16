package tasks

import (
	"errors"
	"strings"
	"testing"
)

// checkpointStubs 装配层注入桩。
type checkpointStubs struct {
	endpointCalls []map[string]any
	statusResult  map[string]any
	claimResult   map[string]any
	added         []string
	observed      []string
	runExtra      func() map[string]any
}

func (s *checkpointStubs) checkpoint(taskID, requestID string) *TaskCheckpoint {
	return &TaskCheckpoint{
		endpoint:  nil,
		taskID:    taskID,
		requestID: requestID,
		AddUserMessage: func(content string) error {
			s.added = append(s.added, content)
			return nil
		},
		ObservedMessages: func() []string { return s.observed },
		CallEndpoint: func(command map[string]any) (map[string]any, error) {
			s.endpointCalls = append(s.endpointCalls, command)
			switch command["action"] {
			case "status":
				return s.statusResult, nil
			case "claim":
				return s.claimResult, nil
			default:
				return map[string]any{}, nil
			}
		},
		RunExtra: s.runExtra,
	}
}

func TestTaskCheckpoint_Check_绑定校验(t *testing.T) {
	stubs := &checkpointStubs{
		statusResult: map[string]any{"status": "running"},
		runExtra: func() map[string]any {
			return map[string]any{"run_context": map[string]any{
				"extra": map[string]any{"managed_task_request": "rq1"}}}
		},
	}
	cp := stubs.checkpoint("t1", "rq1")
	if err := cp.Check(); err != nil {
		t.Fatalf("running 状态应通过，得到 %v", err)
	}
	// 绑定不匹配 → TASK_EXECUTION_BINDING_STALE
	stubs2 := &checkpointStubs{
		statusResult: map[string]any{"status": "running"},
		runExtra: func() map[string]any {
			return map[string]any{"run_context": map[string]any{
				"extra": map[string]any{"managed_task_request": "other"}}}
		},
	}
	err := stubs2.checkpoint("t1", "rq1").Check()
	if err == nil || !strings.Contains(err.Error(), "TASK_EXECUTION_BINDING_STALE") {
		t.Fatalf("绑定不匹配应报 stale，得到 %v", err)
	}
	// 终态 → TASK_EXECUTION_NOT_RUNNING
	stubs3 := &checkpointStubs{statusResult: map[string]any{"status": "completed"},
		runExtra: func() map[string]any {
			return map[string]any{"run_context": map[string]any{
				"extra": map[string]any{"managed_task_request": "rq1"}}}
		}}
	err = stubs3.checkpoint("t1", "rq1").Check()
	if err == nil || !strings.Contains(err.Error(), "TASK_EXECUTION_NOT_RUNNING") {
		t.Fatalf("completed 应报 not running，得到 %v", err)
	}
	// closed → stale
	stubs4 := &checkpointStubs{statusResult: map[string]any{"status": "running"}}
	cp4 := stubs4.checkpoint("t1", "rq1")
	cp4.closed = true
	if err := cp4.Check(); err == nil || !strings.Contains(err.Error(), "TASK_EXECUTION_BINDING_STALE") {
		t.Fatalf("closed 应报 stale，得到 %v", err)
	}
}

func TestTaskCheckpoint_BeforeModel_变更注入(t *testing.T) {
	stubs := &checkpointStubs{
		statusResult: map[string]any{"status": "waiting_user"},
		claimResult: map[string]any{"changes": []any{
			map[string]any{"id": "ch1", "instruction": "改成英文"},
			map[string]any{"id": "ch2", "instruction": "加图表"},
		}},
		runExtra: func() map[string]any {
			return map[string]any{"run_context": map[string]any{
				"extra": map[string]any{"managed_task_request": "rq1"}}}
		},
	}
	cp := stubs.checkpoint("t1", "rq1")
	if err := cp.BeforeModel(); err != nil {
		t.Fatalf("BeforeModel 不应报错，得到 %v", err)
	}
	if len(stubs.added) != 2 {
		t.Fatalf("应注入 2 条 UserMessage，得到 %d", len(stubs.added))
	}
	if !strings.Contains(stubs.added[0], `"managed_task_change":"ch1"`) ||
		!strings.Contains(stubs.added[0], `"instruction":"改成英文"`) {
		t.Fatalf("注入载荷应含变更 ID 与指令，得到 %q", stubs.added[0])
	}
	// 动作序列：status→claim→context_written×2→status（末次 Check）
	actions := []string{}
	for _, call := range stubs.endpointCalls {
		actions = append(actions, call["action"].(string))
	}
	want := []string{"status", "claim", "context_written", "context_written", "status"}
	if len(actions) != len(want) {
		t.Fatalf("动作序列不符，得到 %v", actions)
	}
	for i, a := range want {
		if actions[i] != a {
			t.Fatalf("动作序列第 %d 步应为 %s，得到 %s", i, a, actions[i])
		}
	}
	// endpoint 报错透传
	stubsErr := &checkpointStubs{runExtra: func() map[string]any { return map[string]any{} }}
	cpErr := stubsErr.checkpoint("t1", "rq1")
	cpErr.CallEndpoint = func(map[string]any) (map[string]any, error) { return nil, errors.New("down") }
	if err := cpErr.BeforeModel(); err == nil {
		t.Fatal("端点错误应透传")
	}
}

func TestTaskCheckpoint_AfterModel_回执观察(t *testing.T) {
	stubs := &checkpointStubs{
		statusResult: map[string]any{"status": "running"},
		observed: []string{
			`{"managed_task_change":"ch1"}`,
			`{"managed_task_change":"ch1"}`, // 重复去重
			`not-json`,                      // 不可解析跳过
			`{"managed_task_change":""}`,    // 空 ID 跳过
		},
		runExtra: func() map[string]any {
			return map[string]any{"run_context": map[string]any{
				"extra": map[string]any{"managed_task_request": "rq1"}}}
		},
	}
	cp := stubs.checkpoint("t1", "rq1")
	if err := cp.AfterModel(); err != nil {
		t.Fatalf("AfterModel 不应报错，得到 %v", err)
	}
	if len(stubs.endpointCalls) != 2 || stubs.endpointCalls[1]["action"] != "observed" {
		t.Fatalf("应为 status(Check)+observed 两次调用，得到 %v", stubs.endpointCalls)
	}
	ids := stubs.endpointCalls[1]["change_ids"].([]any)
	if len(ids) != 1 || ids[0] != "ch1" {
		t.Fatalf("observed 应去重为 [ch1]，得到 %v", ids)
	}
	// 无回执则不调用端点
	stubsNone := &checkpointStubs{statusResult: map[string]any{"status": "running"},
		runExtra: func() map[string]any {
			return map[string]any{"run_context": map[string]any{
				"extra": map[string]any{"managed_task_request": "rq1"}}}
		}}
	if err := stubsNone.checkpoint("t1", "rq1").AfterModel(); err != nil {
		t.Fatalf("无回执不应报错，得到 %v", err)
	}
	if len(stubsNone.endpointCalls) != 1 { // 仅 Check 的 status 调用
		t.Fatalf("无回执不应产生 observed 调用，得到 %v", stubsNone.endpointCalls)
	}
	if stubsNone.endpointCalls[0]["action"] != "status" {
		t.Fatalf("唯一调用应为 status，得到 %v", stubsNone.endpointCalls[0])
	}
}
