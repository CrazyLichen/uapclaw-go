package rsi

// Append-only 任务级原始引擎事件日志。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/event_journal.py（94 行）。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EventJournal 每条收到的引擎事件按任务落一行 JSON。
type EventJournal struct {
	tasksRoot string
	// lock 实例级互斥（对齐 Python threading.Lock）。
	lock sync.Mutex
}

// NewEventJournal 构造 journal（tasksRoot 解析为绝对路径）。
func NewEventJournal(tasksRoot string) *EventJournal {
	abs, err := filepath.Abs(tasksRoot)
	if err != nil {
		abs = tasksRoot
	}
	return &EventJournal{tasksRoot: abs}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Append 追加一条事件记录；返回落盘文件路径。
func (j *EventJournal) Append(taskID string, event map[string]any) (string, error) {
	task := strings.TrimSpace(taskID)
	if task == "" || filepath.Base(task) != task || task == "." || task == ".." {
		return "", fmt.Errorf("task_id 非法: %s", taskID)
	}
	taskDir := filepath.Join(j.tasksRoot, task)
	if !strings.HasPrefix(taskDir, j.tasksRoot) {
		return "", fmt.Errorf("task_id 超出 RSI 根目录: %s", taskID)
	}
	record := map[string]any{
		"schema_version": 1,
		"recorded_at":    time.Now().UTC().Format("2006-01-02T15:04:05.000+00:00"),
		"task_id":        task,
		"event_type":     journalEventType(event),
		"event":          event,
	}
	line, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	target := filepath.Join(taskDir, "events.jsonl")
	j.lock.Lock()
	defer j.lock.Unlock()
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return "", err
	}
	f, err := os.OpenFile(target, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return "", err
	}
	return target, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// journalEventType 事件类型归一：顶层 event_type → family.kind → UNKNOWN。
// （Go 侧事件统一为 dict 形态，Python 的 dataclass/attrs 分支不适用。）
func journalEventType(event map[string]any) string {
	if t := strings.TrimSpace(valueToString(event["event_type"])); t != "" {
		return t
	}
	family := strings.TrimSpace(valueToString(event["family"]))
	kind := strings.TrimSpace(valueToString(event["kind"]))
	if family != "" && kind != "" {
		return family + "." + kind
	}
	return "UNKNOWN"
}
