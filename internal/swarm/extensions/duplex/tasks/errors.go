package tasks

import "errors"

// QueueVersionConflict 队列版本冲突（重排时队列已变化）。
// 对齐 tasks/errors.py:4-5 QueueVersionConflict。
var QueueVersionConflict = errors.New("queue version conflict")

// TaskRevisionConflict 任务修订版本冲突。
// 对齐 tasks/errors.py:8-9 TaskRevisionConflict。
var TaskRevisionConflict = errors.New("task revision conflict")

// ExecutionUncertain 传输丢失了最终执行事实；绝不自动重放。
// Python 定义于 tasks/service.py:26-28 ExecutionUncertain；
// 归属偏差 O-4：Go 集中到 errors.go（execute_core_agent 与 service 双向依赖，
// 集中定义避免环），spec §已知偏差登记。
type ExecutionUncertain struct{ Msg string }

func (e *ExecutionUncertain) Error() string { return e.Msg }

// InteractionPending Agent 在观察到的交互处结束本轮流（而非结果）。
// Python 定义于 tasks/service.py:30-31 InteractionPending；归属偏差 O-4 同上。
type InteractionPending struct{}

func (e *InteractionPending) Error() string { return "interaction pending" }
