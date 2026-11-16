package tasks

import "errors"

// QueueVersionConflict 队列版本冲突（重排时队列已变化）。
// 对齐 tasks/errors.py:4-5 QueueVersionConflict。
var QueueVersionConflict = errors.New("queue version conflict")

// TaskRevisionConflict 任务修订版本冲突。
// 对齐 tasks/errors.py:8-9 TaskRevisionConflict。
var TaskRevisionConflict = errors.New("task revision conflict")
