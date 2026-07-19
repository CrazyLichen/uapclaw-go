package schema

import "testing"

// ──────────────────────────── TaskOpResult 测试 ────────────────────────────

// TestTaskOpResult_Success 验证 Success 创建成功的 TaskOpResult
func TestTaskOpResult_Success(t *testing.T) {
	r := TaskOpResult{}.Success()
	if !r.OK {
		t.Error("Success() 期望 OK=true, 实际 false")
	}
	if r.Reason != "" {
		t.Errorf("Success() 期望 Reason 为空, 实际 %q", r.Reason)
	}
}

// TestTaskOpResult_Fail 验证 Fail 创建失败的 TaskOpResult
func TestTaskOpResult_Fail(t *testing.T) {
	r := TaskOpResult{}.Fail("权限不足")
	if r.OK {
		t.Error("Fail() 期望 OK=false, 实际 true")
	}
	if r.Reason != "权限不足" {
		t.Errorf("Fail() 期望 Reason=%q, 实际 %q", "权限不足", r.Reason)
	}
}

// TestTaskOpResult_Fail_空原因 验证 Fail 传空原因
func TestTaskOpResult_Fail_空原因(t *testing.T) {
	r := TaskOpResult{}.Fail("")
	if r.OK {
		t.Error("Fail() 期望 OK=false, 实际 true")
	}
	if r.Reason != "" {
		t.Errorf("Fail() 期望 Reason 为空, 实际 %q", r.Reason)
	}
}

// ──────────────────────────── TaskCreateResult 测试 ────────────────────────────

// TestTaskCreateResult_OK_成功 验证 OK 方法在 Task 不为 nil 时返回 true
func TestTaskCreateResult_OK_成功(t *testing.T) {
	r := TaskCreateResult{Task: "fake_task"}
	if !r.OK() {
		t.Error("OK() 期望 true, 实际 false")
	}
}

// TestTaskCreateResult_OK_失败 验证 OK 方法在 Task 为 nil 时返回 false
func TestTaskCreateResult_OK_失败(t *testing.T) {
	r := TaskCreateResult{Reason: "任务已满"}
	if r.OK() {
		t.Error("OK() 期望 false, 实际 true")
	}
}

// TestTaskCreateResult_CreateSuccess 验证 CreateSuccess 创建成功的 TaskCreateResult
func TestTaskCreateResult_CreateSuccess(t *testing.T) {
	task := map[string]string{"id": "t1"}
	r := TaskCreateResult{}.CreateSuccess(task)
	if r.Task == nil {
		t.Error("CreateSuccess() 期望 Task 不为 nil")
	}
	if r.Reason != "" {
		t.Errorf("CreateSuccess() 期望 Reason 为空, 实际 %q", r.Reason)
	}
}

// TestTaskCreateResult_CreateFail 验证 CreateFail 创建失败的 TaskCreateResult
func TestTaskCreateResult_CreateFail(t *testing.T) {
	r := TaskCreateResult{}.CreateFail("任务已存在")
	if r.Task != nil {
		t.Error("CreateFail() 期望 Task 为 nil")
	}
	if r.Reason != "任务已存在" {
		t.Errorf("CreateFail() 期望 Reason=%q, 实际 %q", "任务已存在", r.Reason)
	}
}

// ──────────────────────────── GraphMutationResult 测试 ────────────────────────────

// TestGraphMutationResult_GraphSuccess 验证 GraphSuccess 创建成功的 GraphMutationResult
func TestGraphMutationResult_GraphSuccess(t *testing.T) {
	r := GraphMutationResult{}.GraphSuccess("task_a", "task_b")
	if !r.OK {
		t.Error("GraphSuccess() 期望 OK=true, 实际 false")
	}
	if len(r.RefreshedTasks) != 2 {
		t.Errorf("GraphSuccess() 期望 RefreshedTasks 长度 2, 实际 %d", len(r.RefreshedTasks))
	}
	if r.Reason != "" {
		t.Errorf("GraphSuccess() 期望 Reason 为空, 实际 %q", r.Reason)
	}
}

// TestGraphMutationResult_GraphSuccess_无刷新任务 验证 GraphSuccess 无刷新任务
func TestGraphMutationResult_GraphSuccess_无刷新任务(t *testing.T) {
	r := GraphMutationResult{}.GraphSuccess()
	if !r.OK {
		t.Error("GraphSuccess() 期望 OK=true, 实际 false")
	}
	if len(r.RefreshedTasks) != 0 {
		t.Errorf("GraphSuccess() 期望 RefreshedTasks 长度 0, 实际 %d", len(r.RefreshedTasks))
	}
}

// TestGraphMutationResult_GraphFail 验证 GraphFail 创建失败的 GraphMutationResult
func TestGraphMutationResult_GraphFail(t *testing.T) {
	r := GraphMutationResult{}.GraphFail("检测到环路")
	if r.OK {
		t.Error("GraphFail() 期望 OK=false, 实际 true")
	}
	if r.Reason != "检测到环路" {
		t.Errorf("GraphFail() 期望 Reason=%q, 实际 %q", "检测到环路", r.Reason)
	}
	if r.RefreshedTasks != nil {
		t.Errorf("GraphFail() 期望 RefreshedTasks 为 nil, 实际 %v", r.RefreshedTasks)
	}
}
