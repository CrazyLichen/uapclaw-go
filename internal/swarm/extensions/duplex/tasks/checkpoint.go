package tasks

import (
	"encoding/json"
	"errors"
)

// TaskCheckpoint 对确切根 Agent 模型上下文应用需求变更的检查点。
// 对齐 tasks/checkpoint.py:6-71 TaskCheckpoint。
// 偏差 O-10：Python 直接操作 openjiuwen 上下文与消息对象，Go 由 endpoint 窄接口
// 与消息注入函数表达（duplex 层装配时注入，spec 登记）。
type TaskCheckpoint struct {
	endpoint  *GatewayTaskEndpoint
	taskID    string
	requestID string
	closed    bool

	// AddUserMessage 向根 Agent 上下文追加 UserMessage（装配层注入）。
	AddUserMessage func(content string) error
	// ObservedMessages 观察根 Agent 最近请求消息列表（装配层注入）。
	ObservedMessages func() []string
	// CallEndpoint 调用检查点端点命令（bridge.Execute 的传输注入）。
	CallEndpoint func(command map[string]any) (map[string]any, error)
	// RunExtra 取运行上下文 extra（对齐 ctx.extra 的 run_context）。
	RunExtra func() map[string]any
}

// NewTaskCheckpoint 构造检查点。
// 对齐 checkpoint.py:7-11 __init__。
func NewTaskCheckpoint(endpoint *GatewayTaskEndpoint, task map[string]any) *TaskCheckpoint {
	return &TaskCheckpoint{
		endpoint:  endpoint,
		taskID:    toString(task["id"]),
		requestID: toString(task["request_id"]),
	}
}

// Check 绑定与运行状态校验。
// 对齐 checkpoint.py:12-22 check。
func (c *TaskCheckpoint) Check() error {
	if c.closed {
		return errors.New("TASK_EXECUTION_BINDING_STALE")
	}
	extra := map[string]any{}
	if c.RunExtra != nil {
		extra = c.RunExtra()
	}
	run := toMap(extra["run_context"])
	runExtra := toMap(run["extra"])
	if toString(runExtra["managed_task_request"]) != c.requestID {
		return errors.New("TASK_EXECUTION_BINDING_STALE")
	}
	result, err := c.CallEndpoint(map[string]any{"action": "status", "task_id": c.taskID,
		"owner": "", "session_id": "", "request_id": c.requestID, "command_id": "check-" + c.requestID})
	if err != nil {
		return err
	}
	status := toString(result["status"])
	if status != "running" && status != "waiting_user" {
		return errors.New("TASK_EXECUTION_NOT_RUNNING")
	}
	return nil
}

// BeforeModel 模型前：认领变更并以 UserMessage 注入上下文。
// 对齐 checkpoint.py:23-49 before_model。
func (c *TaskCheckpoint) BeforeModel() error {
	if err := c.Check(); err != nil {
		return err
	}
	result, err := c.CallEndpoint(map[string]any{"action": "claim", "task_id": c.taskID,
		"request_id": c.requestID, "command_id": "claim-" + c.requestID})
	if err != nil {
		return err
	}
	changes, _ := result["changes"].([]any)
	for _, change := range changes {
		changeMap := toMap(change)
		// SQLite 与模型上下文不是一个事务：丢失的 ACK 状态未知（对齐 :31 注释）
		payload, _ := json.Marshal(map[string]any{
			"managed_task_change": changeMap["id"],
			"instruction":         changeMap["instruction"],
			"meaning": ("User changes this task's requirements; " +
				"retain other constraints and permissions."),
		})
		if c.AddUserMessage != nil {
			if err := c.AddUserMessage(string(payload)); err != nil {
				return err
			}
		}
		if _, err := c.CallEndpoint(map[string]any{
			"action": "context_written", "task_id": c.taskID,
			"request_id": c.requestID, "change_ids": []any{changeMap["id"]},
			"command_id": "ctx-" + toString(changeMap["id"]),
		}); err != nil {
			return err
		}
	}
	return c.Check()
}

// AfterModel 模型后：观察最终请求中的变更回执。
// 对齐 checkpoint.py:50-71 after_model（观察最终请求，不校验语义合规）。
func (c *TaskCheckpoint) AfterModel() error {
	if err := c.Check(); err != nil {
		return err
	}
	observed := []any{}
	seen := map[string]bool{}
	if c.ObservedMessages != nil {
		for _, content := range c.ObservedMessages() {
			var value map[string]any
			if json.Unmarshal([]byte(content), &value) != nil {
				continue
			}
			id := toString(value["managed_task_change"])
			if id != "" && !seen[id] {
				seen[id] = true
				observed = append(observed, id)
			}
		}
	}
	if len(observed) > 0 {
		if _, err := c.CallEndpoint(map[string]any{
			"action": "observed", "task_id": c.taskID, "request_id": c.requestID,
			"change_ids": observed, "command_id": "obs-" + c.requestID,
		}); err != nil {
			return err
		}
	}
	return nil
}
