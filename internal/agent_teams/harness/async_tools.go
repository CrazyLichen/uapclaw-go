package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AsyncToolRecord 一个已启动异步任务的业务视图。
// Python: AsyncToolRecord (async_tools.py:145-157)
type AsyncToolRecord struct {
	// TaskID 任务标识
	TaskID string
	// ToolName 启动工具名
	ToolName string
	// Description 人类可读任务描述
	Description string
	// Status running | completed | error
	Status string
	// Result 工具返回值（完成时写入）
	Result any
	// Error 失败原因
	Error string
	// OutputFile 溢出落盘路径（完成且超阈值时写入）
	OutputFile string
	// FormatCompleted 自定义完成文案
	FormatCompleted func(any) string
	// FormatFailed 自定义失败文案
	FormatFailed func(error) string
}

// asyncTask 在飞任务句柄。
type asyncTask struct {
	// cancel 取消后台任务
	cancel context.CancelFunc
	// done 终态信号（sync.Once close）
	done chan struct{}
	// once 保证 done 只关一次
	once sync.Once
}

// AsyncToolRuntime 每个 harness 一份的异步任务注册表与完成注入器。
// Python: AsyncToolRuntime (async_tools.py:160-435)
type AsyncToolRuntime struct {
	// inject 完成文本回灌回调（NativeHarness.Send(immediate=false)）
	inject func(context.Context, string) error
	// mu 保护 registry/order/tasks/outputDirResolver/spillThreshold
	mu sync.Mutex
	// registry task_id → 记录
	registry map[string]*AsyncToolRecord
	// order 启动顺序（ListAll 保序）
	order []string
	// tasks task_id → 在飞句柄
	tasks map[string]*asyncTask
	// outputDirResolver 溢出目录解析器（rail init 注入）
	outputDirResolver func() string
	// spillThreshold 溢出阈值（字符数）
	spillThreshold int
}

// AsyncToolRuntimeOption runtime 可选配置。
type AsyncToolRuntimeOption func(*AsyncToolRuntime)

// AsyncToolWorker 子类实现的后台执行体。
type AsyncToolWorker interface {
	// RunBackground 在后台执行并返回完整结果
	RunBackground(ctx context.Context, taskID string, inputs map[string]any) (any, error)
}

// AsyncTool 两阶段异步工具基类。
// Python: AsyncTool (async_tools.py:438-496)
type AsyncTool struct {
	// TeamTool 团队工具基类（Card/GetCallTimeout/MapResult 默认面）
	tools.TeamTool
	// parent 宿主能力（launch + session id）
	parent tools.AsyncToolHost
	// language 模型可见文案语言
	language atschema.Language
	// worker 子类自身（构造期注入，避免 Go 嵌入虚派发陷阱）
	worker AsyncToolWorker
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// spillSummaryChars 溢出摘要保留的字符数（按 rune）
	spillSummaryChars = 1024
	// defaultSpillThreshold 默认溢出阈值（字符数）
	defaultSpillThreshold = 32768
	// AsyncToolStatusRunning 任务运行中
	AsyncToolStatusRunning = "running"
	// AsyncToolStatusCompleted 任务已完成
	AsyncToolStatusCompleted = "completed"
	// AsyncToolStatusError 任务失败/取消
	AsyncToolStatusError = "error"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAsyncToolRuntime 创建 runtime。
// Python: AsyncToolRuntime(inject=...) (async_tools.py:160)
func NewAsyncToolRuntime(inject func(context.Context, string) error, opts ...AsyncToolRuntimeOption) *AsyncToolRuntime {
	rt := &AsyncToolRuntime{
		inject:         inject,
		registry:       make(map[string]*AsyncToolRecord),
		tasks:          make(map[string]*asyncTask),
		spillThreshold: defaultSpillThreshold,
	}
	for _, opt := range opts {
		opt(rt)
	}
	return rt
}

// WithSpillThreshold 设置溢出阈值。
func WithSpillThreshold(n int) AsyncToolRuntimeOption {
	return func(rt *AsyncToolRuntime) { rt.spillThreshold = n }
}

// WithOutputDirResolver 设置溢出目录解析器。
func WithOutputDirResolver(fn func() string) AsyncToolRuntimeOption {
	return func(rt *AsyncToolRuntime) { rt.outputDirResolver = fn }
}

// RenderResultText 把异步工具返回值渲染为模型可见文本（全量，不截断）。
// Python: render_result_text (async_tools.py:111-136)
func RenderResultText(result any) string {
	if result == nil {
		return ""
	}
	if s, ok := result.(string); ok {
		return s
	}
	rv := reflect.ValueOf(result)
	switch rv.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array:
		if text, err := marshalIndent(result); err == nil {
			return text
		}
	}
	return fmt.Sprint(result)
}

// NewAsyncTool 构造异步工具基类；worker 必须传子类自身。
// Python: AsyncTool.__init__(card, parent_agent, language)
func NewAsyncTool(card *tool.ToolCard, parent tools.AsyncToolHost, lang atschema.Language, worker AsyncToolWorker) AsyncTool {
	return AsyncTool{
		TeamTool: tools.NewTeamTool(card),
		parent:   parent,
		language: lang,
		worker:   worker,
	}
}

// HasRunning 返回某工具是否有在飞任务。
// Python: AsyncToolRuntime.has_running (async_tools.py:186-191)
func (rt *AsyncToolRuntime) HasRunning(toolName string) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for _, rec := range rt.registry {
		if rec.Status == AsyncToolStatusRunning && rec.ToolName == toolName {
			return true
		}
	}
	return false
}

// Launch 启动后台任务并登记。
// Python: AsyncToolRuntime.launch (async_tools.py:193-225)
func (rt *AsyncToolRuntime) Launch(taskID string, factory func(context.Context) (any, error), opts tools.AsyncToolLaunchOptions) {
	ctx, cancel := context.WithCancel(context.Background())
	task := &asyncTask{cancel: cancel, done: make(chan struct{})}
	rt.mu.Lock()
	if _, exists := rt.registry[taskID]; !exists {
		rt.order = append(rt.order, taskID)
	}
	rt.registry[taskID] = &AsyncToolRecord{
		TaskID:          taskID,
		ToolName:        opts.ToolName,
		Description:     opts.Description,
		Status:          AsyncToolStatusRunning,
		FormatCompleted: opts.FormatCompleted,
		FormatFailed:    opts.FormatFailed,
	}
	rt.tasks[taskID] = task
	rt.mu.Unlock()
	go rt.run(ctx, taskID, factory, opts.ToolName)
}

// Get 返回记录快照；未知返回 nil。调用方可安全读取，不与后台写入竞态。
func (rt *AsyncToolRuntime) Get(taskID string) *AsyncToolRecord {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rec := rt.registry[taskID]
	if rec == nil {
		return nil
	}
	snapshot := *rec
	return &snapshot
}

// ListAll 按启动顺序返回全部记录快照。
// Python: AsyncToolRuntime.list_all (async_tools.py:371-373)
func (rt *AsyncToolRuntime) ListAll() []*AsyncToolRecord {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	out := make([]*AsyncToolRecord, 0, len(rt.order))
	for _, id := range rt.order {
		if rec := rt.registry[id]; rec != nil {
			snapshot := *rec
			out = append(out, &snapshot)
		}
	}
	return out
}

// Cancel 取消一个任务；未知返回 false。
// Python: AsyncToolRuntime.cancel (async_tools.py:375-393)
func (rt *AsyncToolRuntime) Cancel(taskID string) bool {
	rt.mu.Lock()
	task := rt.tasks[taskID]
	rec := rt.registry[taskID]
	if task == nil || rec == nil {
		rt.mu.Unlock()
		return false
	}
	if rec.Status == AsyncToolStatusRunning {
		rec.Status = AsyncToolStatusError
		rec.Error = "cancelled"
	}
	rt.mu.Unlock()
	task.cancel()
	rt.signal(taskID)
	return true
}

// Wait 等任务到终态或超时；返回当前记录（可能仍 running）。
// Python: AsyncToolRuntime.wait (async_tools.py:395-422)
func (rt *AsyncToolRuntime) Wait(ctx context.Context, taskID string, timeout time.Duration) *AsyncToolRecord {
	rec := rt.Get(taskID)
	if rec == nil {
		return nil
	}
	if rec.Status != AsyncToolStatusRunning {
		return rec
	}
	rt.mu.Lock()
	task := rt.tasks[taskID]
	rt.mu.Unlock()
	if task == nil {
		return rec
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-task.done:
	case <-timer.C:
	case <-ctx.Done():
	}
	return rt.Get(taskID)
}

// CancelAll 取消全部在飞任务（停机用）。
// Python: AsyncToolRuntime.cancel_all (async_tools.py:431-435)
func (rt *AsyncToolRuntime) CancelAll() {
	rt.mu.Lock()
	tasks := make([]*asyncTask, 0, len(rt.tasks))
	for _, task := range rt.tasks {
		tasks = append(tasks, task)
	}
	rt.mu.Unlock()
	for _, task := range tasks {
		task.cancel()
	}
}

// Invoke 启动后台任务并立即返回 launched。
// Python: AsyncTool.invoke (async_tools.py:479-491)
func (t *AsyncTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	taskID := agentteams.GenerateID(t.Card().Name)
	desc := t.Card().Name
	if d, ok := t.worker.(interface{ LaunchedDescription(map[string]any) string }); ok {
		desc = d.LaunchedDescription(inputs)
	}
	err := t.parent.LaunchAsyncTool(taskID, func(ctx context.Context) (any, error) {
		return t.worker.RunBackground(ctx, taskID, inputs)
	}, tools.AsyncToolLaunchOptions{ToolName: t.Card().Name, Description: desc})
	if err != nil {
		return map[string]any{"success": false, "error": "Internal error: " + err.Error()}, nil
	}
	return map[string]any{
		"success": true,
		"data":    map[string]any{"status": "launched", "task_id": taskID},
	}, nil
}

// Stream 实现 Tool 接口（不支持流式）。
func (t *AsyncTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// MapResult 把 launched 结果渲染为模型可见文本。
// Python: AsyncTool.render_for_llm (async_tools.py:493-496)
func (t *AsyncTool) MapResult(output map[string]any) string {
	success, _ := output["success"].(bool)
	if !success {
		if e, _ := output["error"].(string); e != "" {
			return e
		}
		return "Failed to launch async tool"
	}
	data, _ := output["data"].(map[string]any)
	taskID, _ := data["task_id"].(string)
	text, err := atschema.T("async_tool.launched", map[string]any{"tool": t.Card().Name, "task_id": taskID})
	if err != nil {
		return "[Background task] " + t.Card().Name + " started (task_id=" + taskID + ")"
	}
	return text
}

// Parent 返回宿主（子类需要自定义 formatter 时使用）。
func (t *AsyncTool) Parent() tools.AsyncToolHost { return t.parent }

// Language 返回模型可见文案语言（子类自定义文案时使用）。
func (t *AsyncTool) Language() atschema.Language { return t.language }

// ──────────────────────────── 非导出函数 ────────────────────────────

// getRecord 返回内部活指针（仅 runtime 内部写路径使用）。
func (rt *AsyncToolRuntime) getRecord(taskID string) *AsyncToolRecord {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.registry[taskID]
}

// run 执行后台任务并结算。
// Python: AsyncToolRuntime._run (async_tools.py:227-264)
func (rt *AsyncToolRuntime) run(ctx context.Context, taskID string, factory func(context.Context) (any, error), toolName string) {
	rec := rt.getRecord(taskID)
	defer func() {
		if r := recover(); r != nil {
			rt.reportFailure(ctx, taskID, rec, toolName, fmt.Errorf("panic: %v", r))
		}
	}()
	result, err := factory(ctx)
	switch {
	case errors.Is(err, context.Canceled):
		rt.markError(taskID, rec, "cancelled")
	case err == nil && ctx.Err() != nil:
		// factory 忽略了取消仍返回成功：以取消为准，不注入。
		rt.markError(taskID, rec, "cancelled")
	case err != nil:
		rt.reportFailure(ctx, taskID, rec, toolName, err)
	default:
		rt.reportCompleted(ctx, taskID, rec, toolName, result)
	}
}

// markError 标记终态错误并唤醒等待者。
// Python: AsyncToolRuntime._mark_error (async_tools.py:266-271)
func (rt *AsyncToolRuntime) markError(taskID string, rec *AsyncToolRecord, msg string) {
	rt.mu.Lock()
	if rec != nil {
		rec.Status = AsyncToolStatusError
		rec.Error = msg
	}
	rt.mu.Unlock()
	rt.signal(taskID)
}

// reportFailure 上报失败并注入失败文案。
// Python: AsyncToolRuntime._report_failure (async_tools.py:273-293)
func (rt *AsyncToolRuntime) reportFailure(ctx context.Context, taskID string, rec *AsyncToolRecord, toolName string, err error) {
	rt.markError(taskID, rec, err.Error())
	text := ""
	if rec != nil && rec.FormatFailed != nil {
		text = rec.FormatFailed(err)
	}
	if text == "" {
		text, _ = atschema.T("async_tool.failed", map[string]any{"tool": toolName, "error": err.Error()})
	}
	rt.injectText(ctx, text)
}

// reportCompleted 上报完成（含溢出）并注入完成文案。
// Python: AsyncToolRuntime._report_completed (async_tools.py:295-316)
func (rt *AsyncToolRuntime) reportCompleted(ctx context.Context, taskID string, rec *AsyncToolRecord, toolName string, result any) {
	resultText := RenderResultText(result)
	if rec != nil {
		resultText = rt.maybeSpill(taskID, rec, resultText)
		rt.mu.Lock()
		rec.Status = AsyncToolStatusCompleted
		rec.Result = result
		rt.mu.Unlock()
	}
	rt.signal(taskID)
	text := ""
	if rec != nil && rec.FormatCompleted != nil {
		text = rec.FormatCompleted(result)
	}
	if text == "" {
		text, _ = atschema.T("async_tool.completed", map[string]any{"tool": toolName, "result": resultText})
	}
	rt.injectText(ctx, text)
}

// signal 唤醒等待者（幂等）。
// Python: AsyncToolRuntime._signal (async_tools.py:318-322)
func (rt *AsyncToolRuntime) signal(taskID string) {
	rt.mu.Lock()
	task := rt.tasks[taskID]
	rt.mu.Unlock()
	if task == nil {
		return
	}
	task.once.Do(func() { close(task.done) })
}

// injectText 尽力注入完成文本；停机竞态只记 Debug。
// Python: AsyncToolRuntime._inject (async_tools.py:424-429)
func (rt *AsyncToolRuntime) injectText(ctx context.Context, text string) {
	if rt.inject == nil {
		return
	}
	if err := rt.inject(ctx, text); err != nil {
		logger.Debug(logComponent).Err(err).Msg("[AsyncToolRuntime] completion injection skipped")
	}
}

// maybeSpill 超阈值结果落盘，返回模型可见文本（摘要 + 取回指针）。
// Python: AsyncToolRuntime._maybe_spill (async_tools.py:324-365)
func (rt *AsyncToolRuntime) maybeSpill(taskID string, rec *AsyncToolRecord, text string) string {
	rt.mu.Lock()
	threshold := rt.spillThreshold
	resolver := rt.outputDirResolver
	rt.mu.Unlock()
	if len([]rune(text)) <= threshold || resolver == nil {
		return text
	}
	dir := resolver()
	if dir == "" {
		return text
	}
	path := filepath.Join(dir, taskID+".output")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logger.Warn(logComponent).Err(err).Str("task_id", taskID).Msg("[AsyncToolRuntime] spill mkdir failed")
		return text
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		logger.Warn(logComponent).Err(err).Str("task_id", taskID).Msg("[AsyncToolRuntime] spill failed; inlining full result")
		return text
	}
	rt.mu.Lock()
	rec.OutputFile = path
	rt.mu.Unlock()
	runes := []rune(text)
	if len(runes) > spillSummaryChars {
		runes = runes[:spillSummaryChars]
	}
	notice, _ := atschema.T("async_tool.spilled_notice", map[string]any{"path": path, "task_id": taskID})
	return string(runes) + "\n\n" + notice
}

// marshalIndent 美化 JSON（不转义 HTML、不转义非 ASCII）。
func marshalIndent(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}
