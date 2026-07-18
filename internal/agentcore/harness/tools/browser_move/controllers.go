package browser_move

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BrowserRuntime 浏览器运行时接口，提供 RunBrowserTask 方法。
//
// 对齐 Python: ActionController.bind_runtime 中隐含的运行时协议
type BrowserRuntime interface {
	// RunBrowserTask 执行浏览器任务
	RunBrowserTask(ctx context.Context, task string, sessionID string, requestID string, timeoutS *int) (map[string]any, error)
}

// BaseController 基础控制器接口，定义动作调度器的抽象契约。
//
// 对齐 Python: BaseController (controllers/base.py L11-69)
type BaseController interface {
	// BindRuntime 绑定运行时对象，供运行时支持的动作使用。
	// 对齐 Python: BaseController.bind_runtime
	BindRuntime(runtime BrowserRuntime) error

	// BindRuntimeRunner 绑定运行时运行器。
	// 对齐 Python: BaseController.bind_runtime_runner
	BindRuntimeRunner(runner RuntimeRunner)

	// ClearRuntimeRunner 清除已绑定的运行时运行器。
	// 对齐 Python: BaseController.clear_runtime_runner
	ClearRuntimeRunner()

	// BindCodeExecutor 绑定直接代码执行器。
	// 对齐 Python: BaseController.bind_code_executor
	BindCodeExecutor(executor CodeExecutorFunc)

	// ClearCodeExecutor 清除已绑定的代码执行器。
	// 对齐 Python: BaseController.clear_code_executor
	ClearCodeExecutor()

	// RegisterAction 注册动作处理器。
	// 对齐 Python: BaseController.register_action
	RegisterAction(name string, handler ActionHandler, overwrite bool) error

	// RegisterActionSpec 注册动作元数据。
	// 对齐 Python: BaseController.register_action_spec
	RegisterActionSpec(name string, spec ActionSpec)

	// ListActions 列出已注册的动作名称。
	// 对齐 Python: BaseController.list_actions
	ListActions() []string

	// DescribeActions 返回已注册动作的元数据。
	// 对齐 Python: BaseController.describe_actions
	DescribeActions() map[string]ActionSpec

	// RunAction 执行已注册的动作。
	// 对齐 Python: BaseController.run_action
	RunAction(ctx context.Context, action string, sessionID string, requestID string, kwargs map[string]any) ActionResult
}

// ActionController 实例级动作注册和调度器。
//
// 对齐 Python: ActionController (controllers/action.py L70-268)
type ActionController struct {
	actions      map[string]ActionHandler
	actionSpecs  map[string]ActionSpec
	runtimeRunner RuntimeRunner
	codeExecutor  CodeExecutorFunc
	mu           sync.Mutex
}

// ActionHandler 动作处理器函数类型。
// 对齐 Python: ActionHandler = Callable[..., Awaitable[Any] | Any]
type ActionHandler func(ctx context.Context, sessionID string, requestID string, kwargs map[string]any) ActionResult

// RuntimeRunner 运行时运行器函数类型。
// 对齐 Python: RuntimeRunner
type RuntimeRunner func(ctx context.Context, task string, sessionID string, requestID string, timeoutS *int) ActionResult

// ActionResult 动作执行结果。
// 对齐 Python: ActionResult = dict[str, Any]
type ActionResult = map[string]any

// ActionSpec 动作元数据。
//
// 对齐 Python: register_action_spec 参数
type ActionSpec struct {
	// Summary 动作摘要
	Summary string `json:"summary"`
	// WhenToUse 使用场景
	WhenToUse string `json:"when_to_use"`
	// Params 参数说明
	Params map[string]string `json:"params"`
}

// ──────────────────────────── 全局变量 ────────────────────────────

// recursiveBrowserActions 禁止递归调用的浏览器动作集合。
// 对齐 Python: _RECURSIVE_BROWSER_ACTIONS
var recursiveBrowserActions = map[string]bool{
	"browser_task":      true,
	"run_browser_task":  true,
}

// browserWorkerActionCtxKey 上下文键，标记是否在 browser worker 动作中执行
type browserWorkerActionKey struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewActionController 创建动作控制器实例。
//
// 对齐 Python: ActionController.__init__
func NewActionController() *ActionController {
	return &ActionController{
		actions:     make(map[string]ActionHandler),
		actionSpecs: make(map[string]ActionSpec),
	}
}

// RuntimeRunner 返回运行时运行器。
func (c *ActionController) RuntimeRunner() RuntimeRunner {
	return c.runtimeRunner
}

// CodeExecutor 返回代码执行器。
func (c *ActionController) CodeExecutor() CodeExecutorFunc {
	return c.codeExecutor
}

// BindRuntime 绑定运行时对象。
//
// 对齐 Python: ActionController.bind_runtime
func (c *ActionController) BindRuntime(runtime BrowserRuntime) error {
	runner := func(ctx context.Context, task string, sessionID string, requestID string, timeoutS *int) ActionResult {
		result, err := runtime.RunBrowserTask(ctx, task, sessionID, requestID, timeoutS)
		if err != nil {
			return ActionResult{
				"ok":    false,
				"error": err.Error(),
			}
		}
		if result == nil {
			return ActionResult{"ok": false, "error": "empty result"}
		}
		return result
	}
	c.BindRuntimeRunner(runner)
	return nil
}

// BindRuntimeRunner 绑定运行时运行器。
//
// 对齐 Python: ActionController.bind_runtime_runner
func (c *ActionController) BindRuntimeRunner(runner RuntimeRunner) {
	c.runtimeRunner = runner
}

// ClearRuntimeRunner 清除运行时运行器。
//
// 对齐 Python: ActionController.clear_runtime_runner
func (c *ActionController) ClearRuntimeRunner() {
	c.BindRuntimeRunner(nil)
}

// BindCodeExecutor 绑定代码执行器。
//
// 对齐 Python: ActionController.bind_code_executor
func (c *ActionController) BindCodeExecutor(executor CodeExecutorFunc) {
	c.codeExecutor = executor
}

// ClearCodeExecutor 清除代码执行器。
//
// 对齐 Python: ActionController.clear_code_executor
func (c *ActionController) ClearCodeExecutor() {
	c.codeExecutor = nil
}

// RegisterAction 注册动作处理器。
//
// 对齐 Python: ActionController.register_action
func (c *ActionController) RegisterAction(name string, handler ActionHandler, overwrite bool) error {
	actionName := normalizeActionName(name)
	if actionName == "" {
		return fmt.Errorf("action name must be non-empty")
	}
	if handler == nil {
		return fmt.Errorf("handler must be non-nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !overwrite {
		if _, exists := c.actions[actionName]; exists {
			return fmt.Errorf("action already exists: %s", actionName)
		}
	}
	c.actions[actionName] = handler
	return nil
}

// RegisterActionSpec 注册动作元数据。
//
// 对齐 Python: ActionController.register_action_spec
func (c *ActionController) RegisterActionSpec(name string, spec ActionSpec) {
	actionName := normalizeActionName(name)
	if actionName == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	spec.Summary = strings.TrimSpace(spec.Summary)
	spec.WhenToUse = strings.TrimSpace(spec.WhenToUse)
	if spec.Params == nil {
		spec.Params = make(map[string]string)
	}
	c.actionSpecs[actionName] = spec
}

// ListActions 列出已注册的动作名称。
//
// 对齐 Python: ActionController.list_actions
func (c *ActionController) ListActions() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]string, 0, len(c.actions))
	for name := range c.actions {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// DescribeActions 返回已注册动作的元数据。
//
// 对齐 Python: ActionController.describe_actions
func (c *ActionController) DescribeActions() map[string]ActionSpec {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make(map[string]ActionSpec, len(c.actions))
	for name := range c.actions {
		spec := c.actionSpecs[name]
		result[name] = ActionSpec{
			Summary:   spec.Summary,
			WhenToUse: spec.WhenToUse,
			Params:    spec.Params,
		}
	}
	return result
}

// RunAction 执行已注册的动作。
//
// 对齐 Python: ActionController.run_action
func (c *ActionController) RunAction(ctx context.Context, action string, sessionID string, requestID string, kwargs map[string]any) ActionResult {
	actionName := normalizeActionName(action)
	sid := strings.TrimSpace(sessionID)
	rid := strings.TrimSpace(requestID)

	paramKeys := ""
	if kwargs != nil {
		keys := make([]string, 0, len(kwargs))
		for k := range kwargs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		paramKeys = strings.Join(keys, ",")
	}

	logger.Info(logComponentBR).
		Str("event_type", "CONTROLLER_ACTION").
		Str("action", actionName).
		Str("session_id", orEmpty(sid)).
		Str("request_id", orEmpty(rid)).
		Str("param_keys", orEmpty(paramKeys)).
		Msg("start")

	// 检查递归调用
	if IsBrowserWorkerAction(ctx) && recursiveBrowserActions[actionName] {
		error := "recursive_browser_task_blocked: browser workers must not invoke " +
			"browser_task/run_browser_task via browser_custom_action; return a JSON error instead"
		logger.Warn(logComponentBR).
			Str("event_type", "CONTROLLER_ACTION_BLOCKED").
			Str("action", actionName).
			Str("session_id", orEmpty(sid)).
			Str("request_id", orEmpty(rid)).
			Str("error", error).
			Msg("blocked")
		return ActionResult{
			"ok":         false,
			"action":     actionName,
			"session_id": sid,
			"request_id": rid,
			"error":      error,
		}
	}

	c.mu.Lock()
	handler, exists := c.actions[actionName]
	c.mu.Unlock()

	if !exists {
		logger.Warn(logComponentBR).
			Str("event_type", "CONTROLLER_ACTION_UNKNOWN").
			Str("action", actionName).
			Str("session_id", orEmpty(sid)).
			Str("request_id", orEmpty(rid)).
			Msg("unknown action")
		return ActionResult{
			"ok":         false,
			"action":     actionName,
			"session_id": sid,
			"request_id": rid,
			"error":      fmt.Sprintf("unknown action: %s", actionName),
		}
	}

	result := handler(ctx, sid, rid, kwargs)

	// 确保 result 中包含默认字段
	if result == nil {
		result = ActionResult{}
	}
	if _, ok := result["ok"]; !ok {
		result["ok"] = true
	}
	if _, ok := result["action"]; !ok {
		result["action"] = actionName
	}
	if _, ok := result["session_id"]; !ok {
		result["session_id"] = sid
	}
	if _, ok := result["request_id"]; !ok {
		result["request_id"] = rid
	}
	if _, ok := result["error"]; !ok {
		result["error"] = nil
	}

	_ok, _ := result["ok"].(bool)
	if _ok {
		result["error"] = nil
	}
	logger.Info(logComponentBR).
		Str("event_type", "CONTROLLER_ACTION").
		Str("action", actionName).
		Str("session_id", orEmpty(sid)).
		Str("request_id", orEmpty(rid)).
		Bool("ok", _ok).
		Msg("end")

	return result
}

// Snapshot 返回控制器快照。
// 对齐 Python: ActionController.snapshot
func (c *ActionController) Snapshot() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	actions := make(map[string]ActionHandler, len(c.actions))
	for k, v := range c.actions {
		actions[k] = v
	}
	return map[string]any{
		"actions":        actions,
		"action_specs":   c.actionSpecs,
		"runtime_runner": c.runtimeRunner,
		"code_executor":  c.codeExecutor,
	}
}

// RegisterBuiltinActions 注册所有内置动作。
//
// 对齐 Python: register_builtin_actions (controllers/action.py L691-1237)
func (c *ActionController) RegisterBuiltinActions() {
	c.registerPingAction()
	c.registerEchoAction()
	c.registerBrowserTaskAction()
	c.registerBrowserGetElementCoordinatesAction()
	c.registerBrowserDragAndDropAction()
	c.registerBrowserSetInputFilesAction()
	c.registerListUploadFilesAction()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeActionName 规范化动作名称。
// 对齐 Python: _normalize_action_name
func normalizeActionName(name string) string {
	return strings.TrimSpace(strings.ToLower(name))
}

// IsBrowserWorkerAction 检查上下文中是否在 browser worker 动作中执行。
// 对齐 Python: _ctx_browser_worker_action.get()
func IsBrowserWorkerAction(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	val := ctx.Value(browserWorkerActionKey{})
	return val != nil && val.(bool)
}

// WithBrowserWorkerAction 设置上下文标记，表示在 browser worker 动作中执行。
// 对齐 Python: browser_worker_action_context
func WithBrowserWorkerAction(ctx context.Context) context.Context {
	return context.WithValue(ctx, browserWorkerActionKey{}, true)
}

// registerPingAction 注册 ping 动作。
// 对齐 Python: register_builtin_actions 中 ping (L694-701)
func (c *ActionController) registerPingAction() {
	c.RegisterAction("ping", func(_ context.Context, sessionID string, requestID string, kwargs map[string]any) ActionResult {
		return ActionResult{
			"ok":         true,
			"pong":       true,
			"session_id": sessionID,
			"request_id": requestID,
			"meta":       kwargs,
		}
	}, true)
	// 对齐 Python: 提示词逐字符复制
	c.RegisterActionSpec("ping", ActionSpec{
		Summary:   "Health check action.",
		WhenToUse: "Use to verify controller dispatch and session/request threading.",
	})
}

// registerEchoAction 注册 echo 动作。
// 对齐 Python: register_builtin_actions 中 echo (L703-715)
func (c *ActionController) registerEchoAction() {
	c.RegisterAction("echo", func(_ context.Context, sessionID string, requestID string, kwargs map[string]any) ActionResult {
		text := ""
		if t, ok := kwargs["text"]; ok {
			text = fmt.Sprintf("%v", t)
		}
		return ActionResult{
			"ok":         true,
			"text":       text,
			"session_id": sessionID,
			"request_id": requestID,
			"meta":       kwargs,
		}
	}, true)
	// 对齐 Python: 提示词逐字符复制
	c.RegisterActionSpec("echo", ActionSpec{
		Summary:   "Echoes provided text and metadata.",
		WhenToUse: "Use for debugging payload passthrough through browser_custom_action.",
		Params:    map[string]string{"text": "string: text to echo back"},
	})
}

// registerBrowserTaskAction 注册 browser_task 和 run_browser_task 动作。
// 对齐 Python: register_builtin_actions 中 browser_task (L717-756)
func (c *ActionController) registerBrowserTaskAction() {
	handler := func(ctx context.Context, sessionID string, requestID string, kwargs map[string]any) ActionResult {
		runner := c.runtimeRunner
		if runner == nil {
			return ActionResult{
				"ok":         false,
				"error":      "runtime_not_bound: call bind_runtime(...) before browser_task",
				"session_id": sessionID,
				"request_id": requestID,
			}
		}

		taskText := ""
		if t, ok := kwargs["task"]; ok {
			taskText = strings.TrimSpace(fmt.Sprintf("%v", t))
		}
		if taskText == "" {
			return ActionResult{
				"ok":         false,
				"error":      "missing required parameter: task",
				"session_id": sessionID,
				"request_id": requestID,
			}
		}

		var timeoutS *int
		if v, ok := kwargs["timeout_s"]; ok && v != nil {
			if n, err := toIntOrNone(v); err == nil && n != nil && *n > 0 {
				timeoutS = n
			}
		}

		return runner(ctx, taskText, sessionID, requestID, timeoutS)
	}

	c.RegisterAction("browser_task", handler, true)
	// 对齐 Python: 提示词逐字符复制
	c.RegisterActionSpec("browser_task", ActionSpec{
		Summary:   "Runs a free-form browser task through runtime.run_browser_task.",
		WhenToUse: "Use for generic website tasks when no specialized custom action applies.",
		Params: map[string]string{
			"task":      "string: required task prompt for the browser worker",
			"timeout_s": "int: optional positive timeout override",
		},
	})

	c.RegisterAction("run_browser_task", handler, true)
	// 对齐 Python: 提示词逐字符复制
	c.RegisterActionSpec("run_browser_task", ActionSpec{
		Summary:   "Alias of browser_task.",
		WhenToUse: "Same behavior as browser_task.",
		Params: map[string]string{
			"task":      "string: required task prompt for the browser worker",
			"timeout_s": "int: optional positive timeout override",
		},
	})
}

// registerBrowserGetElementCoordinatesAction 注册坐标解析动作。
// 对齐 Python: register_builtin_actions 中 browser_get_element_coordinates (L758-869)
func (c *ActionController) registerBrowserGetElementCoordinatesAction() {
	c.RegisterAction("browser_get_element_coordinates", func(ctx context.Context, sessionID string, requestID string, kwargs map[string]any) ActionResult {
		payload := buildDragPayload(kwargs)
		if !hasSourceSelector(payload) && !hasCoordinateInputs(payload) {
			return ActionResult{
				"ok": false,
				"error": "Missing location inputs. Provide at least element_source (element_target is optional), " +
					"or coord_source_x/coord_source_y/coord_target_x/coord_target_y. " +
					"Aliases source/target and source_x/source_y/target_x/target_y are also supported.",
			}
		}

		jsCode := buildCoordinateScript(payload)

		// 优先使用 code executor
		if c.codeExecutor != nil {
			raw, err := c.codeExecutor(ctx, jsCode)
			if err != nil {
				return ActionResult{
					"ok":         false,
					"error":      fmt.Sprintf("browser_run_code failed: %v", err),
					"session_id": sessionID,
					"request_id": requestID,
				}
			}
			parsed := ExtractJSONObject(fmt.Sprintf("%v", raw))
			if parsed == nil {
				preview := fmt.Sprintf("%v", raw)
				if len(preview) > 400 {
					preview = preview[:400]
				}
				return ActionResult{
					"ok":         false,
					"error":      "Could not parse coordinate result JSON from browser_run_code output",
					"raw_preview": preview,
				}
			}
			return ActionResult{
				"ok":     parsed["ok"] == true,
				"source": parsed["source"],
				"target": parsed["target"],
				"error":  parsed["error"],
			}
		}

		// 回退到 LLM worker
		runner := c.runtimeRunner
		if runner == nil {
			return ActionResult{
				"ok":         false,
				"error":      "runtime_not_bound: call bind_runtime(...) before browser_get_element_coordinates",
				"session_id": sessionID,
				"request_id": requestID,
			}
		}

		taskPrompt := buildRunCodeTask(jsCode, "resolve source/target coordinates")
		runtimeResult := runner(ctx, taskPrompt, sessionID, requestID, nil)
		if runtimeResult["ok"] != true {
			errVal := "runtime error"
			if e, ok := runtimeResult["error"]; ok && e != nil {
				errVal = fmt.Sprintf("%v", e)
			}
			return ActionResult{
				"ok":      false,
				"error":   errVal,
				"runtime": runtimeResult,
			}
		}

		finalVal := fmt.Sprintf("%v", runtimeResult["final"])
		parsed := ExtractJSONObject(finalVal)
		if parsed == nil {
			preview := finalVal
			if len(preview) > 400 {
				preview = preview[:400]
			}
			return ActionResult{
				"ok":            false,
				"error":         "Could not parse coordinate result JSON from runtime final output",
				"final_preview": preview,
				"runtime":       runtimeResult,
			}
		}

		return ActionResult{
			"ok":      parsed["ok"] == true,
			"source":  parsed["source"],
			"target":  parsed["target"],
			"error":   parsed["error"],
			"runtime": runtimeResult,
		}
	}, true)
	// 对齐 Python: 提示词逐字符复制
	c.RegisterActionSpec("browser_get_element_coordinates", ActionSpec{
		Summary:   "Resolves source/target screen coordinates by selectors or explicit coordinates.",
		WhenToUse: "Use when you need coordinates for one element (element_source only) or two (source + target). element_target is optional.",
		Params: map[string]string{
			"url":                          "string: optional URL to open before resolving coordinates",
			"element_source":               "string: source selector/text alias (required for selector mode)",
			"element_target":               "string: target selector/text alias (optional)",
			"coord_source_x":               "int: source x coordinate",
			"coord_source_y":               "int: source y coordinate",
			"coord_target_x":               "int: target x coordinate",
			"coord_target_y":               "int: target y coordinate",
			"source/target":                "string aliases for element_source/element_target",
			"source_x/source_y/target_x/target_y": "int aliases for coord_* fields",
		},
	})
}

// registerBrowserDragAndDropAction 注册拖拽动作。
// 对齐 Python: register_builtin_actions 中 browser_drag_and_drop (L907-1029)
func (c *ActionController) registerBrowserDragAndDropAction() {
	c.RegisterAction("browser_drag_and_drop", func(ctx context.Context, sessionID string, requestID string, kwargs map[string]any) ActionResult {
		payload := buildDragPayload(kwargs)
		if !hasSelectorInputs(payload) && !hasCoordinateInputs(payload) {
			return ActionResult{
				"ok": false,
				"error": "Missing drag inputs. Provide either " +
					"element_source + element_target, or " +
					"coord_source_x/coord_source_y/coord_target_x/coord_target_y. " +
					"Aliases source/target and source_x/source_y/target_x/target_y are also supported.",
			}
		}

		jsCode := buildDragScript(payload)

		// 优先使用 code executor
		if c.codeExecutor != nil {
			raw, err := c.codeExecutor(ctx, jsCode)
			if err != nil {
				return ActionResult{
					"ok":         false,
					"error":      fmt.Sprintf("browser_run_code failed: %v", err),
					"session_id": sessionID,
					"request_id": requestID,
				}
			}
			parsed := ExtractJSONObject(fmt.Sprintf("%v", raw))
			if parsed == nil {
				preview := fmt.Sprintf("%v", raw)
				if len(preview) > 400 {
					preview = preview[:400]
				}
				return ActionResult{
					"ok":         false,
					"error":      "Could not parse drag result JSON from browser_run_code output",
					"raw_preview": preview,
				}
			}
			return ActionResult{
				"ok":       parsed["ok"] == true,
				"message":  parsed["message"],
				"source":   parsed["source"],
				"target":   parsed["target"],
				"steps":    parsed["steps"],
				"delay_ms": parsed["delay_ms"],
				"error":    parsed["error"],
			}
		}

		// 回退到 LLM worker
		runner := c.runtimeRunner
		if runner == nil {
			return ActionResult{
				"ok":         false,
				"error":      "runtime_not_bound: call bind_runtime(...) before browser_drag_and_drop",
				"session_id": sessionID,
				"request_id": requestID,
			}
		}

		taskPrompt := buildRunCodeTask(jsCode, "drag and drop")
		runtimeResult := runner(ctx, taskPrompt, sessionID, requestID, nil)
		if runtimeResult["ok"] != true {
			errVal := "runtime error"
			if e, ok := runtimeResult["error"]; ok && e != nil {
				errVal = fmt.Sprintf("%v", e)
			}
			return ActionResult{
				"ok":      false,
				"error":   errVal,
				"runtime": runtimeResult,
			}
		}

		finalVal := fmt.Sprintf("%v", runtimeResult["final"])
		parsed := ExtractJSONObject(finalVal)
		if parsed == nil {
			preview := finalVal
			if len(preview) > 400 {
				preview = preview[:400]
			}
			return ActionResult{
				"ok":            false,
				"error":         "Could not parse drag result JSON from runtime final output",
				"final_preview": preview,
				"runtime":       runtimeResult,
			}
		}

		return ActionResult{
			"ok":       parsed["ok"] == true,
			"message":  parsed["message"],
			"source":   parsed["source"],
			"target":   parsed["target"],
			"steps":    parsed["steps"],
			"delay_ms": parsed["delay_ms"],
			"error":    parsed["error"],
			"runtime":  runtimeResult,
		}
	}, true)
	// 对齐 Python: 提示词逐字符复制
	c.RegisterActionSpec("browser_drag_and_drop", ActionSpec{
		Summary:   "Performs drag-and-drop using selectors or explicit coordinates.",
		WhenToUse: "Use for drag-and-drop tasks instead of generic browser_run_task text-only instructions.",
		Params: map[string]string{
			"url":                          "string: optional URL to open before drag-and-drop",
			"element_source":               "string: source selector/text alias",
			"element_target":               "string: target selector/text alias",
			"coord_source_x":               "int: source x coordinate",
			"coord_source_y":               "int: source y coordinate",
			"coord_target_x":               "int: target x coordinate",
			"coord_target_y":               "int: target y coordinate",
			"steps":                        "int: optional drag interpolation steps",
			"delay_ms":                     "int: optional delay between drag steps",
			"source/target":                "string aliases for element_source/element_target",
			"source_x/source_y/target_x/target_y": "int aliases for coord_* fields",
		},
	})
}

// registerBrowserSetInputFilesAction 注册文件上传动作。
// 对齐 Python: register_builtin_actions 中 browser_set_input_files (L1031-1109)
func (c *ActionController) registerBrowserSetInputFilesAction() {
	c.RegisterAction("browser_set_input_files", func(ctx context.Context, sessionID string, requestID string, kwargs map[string]any) ActionResult {
		effectiveSelector := `input[type="file"]`
		if s, ok := kwargs["selector"]; ok {
			sStr := strings.TrimSpace(fmt.Sprintf("%v", s))
			if sStr != "" {
				effectiveSelector = sStr
			}
		}

		var effectivePaths []string
		if p, ok := kwargs["paths"]; ok {
			switch v := p.(type) {
			case []any:
				for _, item := range v {
					s := strings.TrimSpace(fmt.Sprintf("%v", item))
					if s != "" {
						effectivePaths = append(effectivePaths, s)
					}
				}
			case []string:
				effectivePaths = v
			}
		}

		if len(effectivePaths) == 0 {
			return ActionResult{
				"ok":         false,
				"error":      "paths is required and must be non-empty",
				"session_id": sessionID,
				"request_id": requestID,
			}
		}

		jsCode := buildSetInputFilesScript(effectiveSelector, effectivePaths)

		// 优先使用 code executor
		if c.codeExecutor != nil {
			raw, err := c.codeExecutor(ctx, jsCode)
			if err != nil {
				return ActionResult{
					"ok":         false,
					"error":      fmt.Sprintf("browser_run_code failed: %v", err),
					"session_id": sessionID,
					"request_id": requestID,
				}
			}
			parsed := ExtractJSONObject(fmt.Sprintf("%v", raw))
			if parsed == nil {
				preview := fmt.Sprintf("%v", raw)
				if len(preview) > 400 {
					preview = preview[:400]
				}
				return ActionResult{
					"ok":         false,
					"error":      "Could not parse set_input_files result JSON from browser_run_code output",
					"raw_preview": preview,
				}
			}
			selectorVal := effectiveSelector
			if s, ok := parsed["selector"].(string); ok {
				selectorVal = s
			}
			pathsVal := effectivePaths
			if p, ok := parsed["paths"].([]any); ok {
				pathsVal = make([]string, len(p))
				for i, item := range p {
					pathsVal[i] = fmt.Sprintf("%v", item)
				}
			}
			return ActionResult{
				"ok":       parsed["ok"] == true,
				"selector": selectorVal,
				"paths":    pathsVal,
				"error":    parsed["error"],
			}
		}

		// 回退到 LLM worker
		runner := c.runtimeRunner
		if runner == nil {
			return ActionResult{
				"ok":         false,
				"error":      "runtime_not_bound: call bind_runtime(...) before browser_set_input_files",
				"session_id": sessionID,
				"request_id": requestID,
			}
		}

		taskPrompt := buildRunCodeTask(jsCode, fmt.Sprintf("set input files on %s", effectiveSelector))
		runtimeResult := runner(ctx, taskPrompt, sessionID, requestID, nil)
		if runtimeResult["ok"] != true {
			errVal := "runtime error"
			if e, ok := runtimeResult["error"]; ok && e != nil {
				errVal = fmt.Sprintf("%v", e)
			}
			return ActionResult{
				"ok":      false,
				"error":   errVal,
				"runtime": runtimeResult,
			}
		}

		finalVal := fmt.Sprintf("%v", runtimeResult["final"])
		parsed := ExtractJSONObject(finalVal)
		if parsed == nil {
			preview := finalVal
			if len(preview) > 400 {
				preview = preview[:400]
			}
			return ActionResult{
				"ok":            false,
				"error":         "Could not parse set_input_files result JSON from runtime final output",
				"final_preview": preview,
				"runtime":       runtimeResult,
			}
		}

		selectorVal := effectiveSelector
		if s, ok := parsed["selector"].(string); ok {
			selectorVal = s
		}
		pathsVal := effectivePaths
		if p, ok := parsed["paths"].([]any); ok {
			pathsVal = make([]string, len(p))
			for i, item := range p {
				pathsVal[i] = fmt.Sprintf("%v", item)
			}
		}

		return ActionResult{
			"ok":       parsed["ok"] == true,
			"selector": selectorVal,
			"paths":    pathsVal,
			"error":    parsed["error"],
			"runtime":  runtimeResult,
		}
	}, true)
	// 对齐 Python: 提示词逐字符复制
	c.RegisterActionSpec("browser_set_input_files", ActionSpec{
		Summary: "Sets files on an <input type='file'> element. Requires prior page inspection " +
			"to select the correct input — do not call this without first reading the page snapshot.",
		WhenToUse: "Use for all file upload tasks. Does NOT require a file chooser dialog — sets files directly " +
			"on the DOM element. Call list_upload_files first to get absolute paths, then call this action. " +
			"IMPORTANT: pages may have multiple file inputs (e.g. a visible input plus a hidden Dropzone input). " +
			"Always prefer a specific selector such as '#file-upload' or 'input#id' over the generic " +
			"'input[type=\"file\"]' to avoid strict mode violations. If you have not yet inspected the page, " +
			"delegate this action to browser_run_task so the worker can read the page snapshot first.",
		Params: map[string]string{
			"selector": "string: CSS/Playwright selector targeting exactly one file input " +
				"(default: 'input[type=\"file\"]'). Use a specific ID selector like '#file-upload' " +
				"when the page has more than one file input element.",
			"paths": "list[string]: absolute file paths to set on the input (required, non-empty). " +
				"Parameter name is 'paths' — not 'files', not 'file_paths'.",
		},
	})
}

// registerListUploadFilesAction 注册列出上传文件动作。
// 对齐 Python: register_builtin_actions 中 list_upload_files (L871-905)
func (c *ActionController) registerListUploadFilesAction() {
	c.RegisterAction("list_upload_files", func(_ context.Context, sessionID string, requestID string, _ map[string]any) ActionResult {
		uploadRoot := resolveUploadRoot()
		if uploadRoot == "" {
			return ActionResult{
				"ok":         false,
				"error":      "BROWSER_UPLOAD_ROOT is not configured. Set this env var to the directory where uploadable files are stored.",
				"files":      []any{},
				"session_id": sessionID,
				"request_id": requestID,
			}
		}

		info, err := os.Stat(uploadRoot)
		if err != nil || !info.IsDir() {
			return ActionResult{
				"ok":          false,
				"error":       fmt.Sprintf("Upload root directory does not exist: %s", uploadRoot),
				"files":       []any{},
				"upload_root": uploadRoot,
				"session_id":  sessionID,
				"request_id":  requestID,
			}
		}

		files := listDirFiles(uploadRoot)
		return ActionResult{
			"ok":          true,
			"upload_root": uploadRoot,
			"files":       files,
			"session_id":  sessionID,
			"request_id":  requestID,
		}
	}, true)
	// 对齐 Python: 提示词逐字符复制
	c.RegisterActionSpec("list_upload_files", ActionSpec{
		Summary:   "Lists files available for upload from the configured BROWSER_UPLOAD_ROOT directory.",
		WhenToUse: "Call this to discover what files are available and get their exact absolute paths before calling browser_set_input_files to attach them to a file input. Returns a list of {name, path, size_bytes} entries.",
	})
}

// ─── JavaScript 脚本构建辅助函数 ───
// 对齐 Python: controllers/action.py L297-562

// buildRunCodeTask 构建 run_code 任务提示。
// 对齐 Python: _build_run_code_task (L297-305)
// 提示词逐字符复制 Python 原文
func buildRunCodeTask(jsCode string, purpose string) string {
	toolInput := map[string]any{"code": jsCode}
	toolInputJSON, _ := json.Marshal(toolInput)
	return fmt.Sprintf("Execute this browser operation: %s.\n"+
		"Call browser_run_code exactly once with this JSON input:\n"+
		"%s\n\n"+
		"Then return your required top-level response JSON. "+
		"Set its `final` field to the exact JSON result returned by browser_run_code.",
		purpose, string(toolInputJSON))
}

// buildDragPayload 构建拖拽参数载荷。
// 对齐 Python: _build_drag_payload (L510-562)
func buildDragPayload(kwargs map[string]any) map[string]any {
	sourceSelector := getStr(kwargs, "element_source")
	targetSelector := getStr(kwargs, "element_target")

	if sourceSelector == "" {
		sourceSelector = getStr(kwargs, "source")
	}
	if targetSelector == "" {
		targetSelector = getStr(kwargs, "target")
	}

	sx := toIntPtrOrNone(kwargs, "coord_source_x")
	sy := toIntPtrOrNone(kwargs, "coord_source_y")
	tx := toIntPtrOrNone(kwargs, "coord_target_x")
	ty := toIntPtrOrNone(kwargs, "coord_target_y")

	if sx == nil {
		sx = toIntPtrOrNone(kwargs, "source_x")
	}
	if sy == nil {
		sy = toIntPtrOrNone(kwargs, "source_y")
	}
	if tx == nil {
		tx = toIntPtrOrNone(kwargs, "target_x")
	}
	if ty == nil {
		ty = toIntPtrOrNone(kwargs, "target_y")
	}

	return map[string]any{
		"url":             getStr(kwargs, "url"),
		"element_source":  sourceSelector,
		"element_target":  targetSelector,
		"coord_source_x":  sx,
		"coord_source_y":  sy,
		"coord_target_x":  tx,
		"coord_target_y":  ty,
		"steps":           toIntPtrOrNone(kwargs, "steps"),
		"delay_ms":        toIntPtrOrNone(kwargs, "delay_ms"),
	}
}

// hasSelectorInputs 检查是否有选择器输入。
// 对齐 Python: _has_selector_inputs
func hasSelectorInputs(payload map[string]any) bool {
	s, _ := payload["element_source"].(string)
	t, _ := payload["element_target"].(string)
	return strings.TrimSpace(s) != "" && strings.TrimSpace(t) != ""
}

// hasSourceSelector 检查是否有源选择器。
// 对齐 Python: _has_source_selector
func hasSourceSelector(payload map[string]any) bool {
	s, _ := payload["element_source"].(string)
	return strings.TrimSpace(s) != ""
}

// hasCoordinateInputs 检查是否有坐标输入。
// 对齐 Python: _has_coordinate_inputs
func hasCoordinateInputs(payload map[string]any) bool {
	for _, k := range []string{"coord_source_x", "coord_source_y", "coord_target_x", "coord_target_y"} {
		if payload[k] == nil {
			return false
		}
	}
	return true
}

// buildCoordinateScript 构建坐标解析脚本。
// 对齐 Python: _build_coordinate_script (L473-478)
func buildCoordinateScript(payload map[string]any) string {
	payloadJSON, _ := json.Marshal(payload)
	return wrapPageScript(
		buildSelectorResolutionHelpers(string(payloadJSON)),
		buildCoordinateResolutionBody(),
	)
}

// buildDragScript 构建拖拽操作脚本。
// 对齐 Python: _build_drag_script (L481-486)
func buildDragScript(payload map[string]any) string {
	payloadJSON, _ := json.Marshal(payload)
	return wrapPageScript(
		buildSelectorResolutionHelpers(string(payloadJSON)),
		buildDragOperationBody(),
	)
}

// buildSetInputFilesScript 构建文件设置脚本。
// 对齐 Python: _build_set_input_files_script (L489-507)
func buildSetInputFilesScript(selector string, paths []string) string {
	selectorJS := "'" + strings.ReplaceAll(strings.ReplaceAll(selector, "\\", "\\\\"), "'", "\\'") + "'"
	pathsJSON, _ := json.Marshal(paths)
	return fmt.Sprintf("async (page) => {\n"+
		"  try {\n"+
		"    await page.locator(%s).setInputFiles(%s);\n"+
		"    return { ok: true, selector: %s, paths: %s };\n"+
		"  } catch (error) {\n"+
		"    const msg = String(error);\n"+
		"    if (msg.includes('strict mode violation')) {\n"+
		"      return { ok: false, error: msg, selector: %s, paths: %s,"+
		" hint: 'Multiple file inputs matched. Use a more specific selector"+
		" (e.g. an id like #file-upload) targeting the visible input.' };\n"+
		"    }\n"+
		"    return { ok: false, error: msg, selector: %s, paths: %s };\n"+
		"  }\n"+
		"}", selectorJS, string(pathsJSON), selectorJS, string(pathsJSON), selectorJS, string(pathsJSON), selectorJS, string(pathsJSON))
}

// wrapPageScript 包装页面脚本。
// 对齐 Python: _wrap_page_script (L308-309)
func wrapPageScript(parts ...string) string {
	return "async (page) => {\n" + strings.Join(parts, "") + "}"
}

// buildSelectorResolutionHelpers 构建选择器解析辅助函数。
// 对齐 Python: _build_selector_resolution_helpers (L312-379)
func buildSelectorResolutionHelpers(payloadJSON string) string {
	return fmt.Sprintf("  const params = %s;\n", payloadJSON) +
		"  if (params.url && String(params.url).trim()) {\n" +
		"    await page.goto(String(params.url).trim());\n" +
		"  }\n" +
		"  const getTextBox = async (query, role) => {\n" +
		"    const term = String(query || '').trim().toLowerCase();\n" +
		"    if (!term) return null;\n" +
		"    return await page.evaluate(({ term, role }) => {\n" +
		"      const all = Array.from(document.querySelectorAll('body *'));\n" +
		"      const score = (el) => {\n" +
		"        const text = String(el.textContent || '').trim().toLowerCase();\n" +
		"        if (!text) return -1;\n" +
		"        if (text === term) return 2;\n" +
		"        if (text.includes(term)) return 1;\n" +
		"        return -1;\n" +
		"      };\n" +
		"      const toVisibleBox = (el) => {\n" +
		"        if (!el) return null;\n" +
		"        const candidates = role === 'target' && el.parentElement ? [el.parentElement, el] : [el];\n" +
		"        for (const candidate of candidates) {\n" +
		"          const rect = candidate.getBoundingClientRect();\n" +
		"          if (rect && rect.width > 0 && rect.height > 0) {\n" +
		"            return { x: rect.x, y: rect.y, width: rect.width, height: rect.height };\n" +
		"          }\n" +
		"        }\n" +
		"        return null;\n" +
		"      };\n" +
		"      const exactMatches = all.filter((el) => score(el) === 2);\n" +
		"      for (const el of exactMatches) {\n" +
		"        const box = toVisibleBox(el);\n" +
		"        if (box) return box;\n" +
		"      }\n" +
		"      const fuzzyMatches = all.filter((el) => score(el) === 1);\n" +
		"      for (const el of fuzzyMatches) {\n" +
		"        const box = toVisibleBox(el);\n" +
		"        if (box) return box;\n" +
		"      }\n" +
		"      return null;\n" +
		"    }, { term, role });\n" +
		"  };\n" +
		"  const extractTextFromHasText = (s) => {\n" +
		"    if (!s || typeof s !== 'string') return s;\n" +
		"    const m = String(s).match(/:has-text\\s*\\(\\s*['\"]([^'\"]*)['\"]\\s*\\)/);\n" +
		"    return m ? m[1] : s;\n" +
		"  };\n" +
		"  const getPoint = async (selector, offset, role) => {\n" +
		"    let box = null;\n" +
		"    if (selector) {\n" +
		"      try {\n" +
		"        const el = await page.$(selector);\n" +
		"        if (el) box = await el.boundingBox();\n" +
		"      } catch (_err) {\n" +
		"        box = null;\n" +
		"      }\n" +
		"    }\n" +
		"    if (!box) {\n" +
		"      const textTerm = extractTextFromHasText(selector) || selector;\n" +
		"      box = await getTextBox(textTerm, role);\n" +
		"    }\n" +
		"    if (!box) return null;\n" +
		"    if (offset && Number.isFinite(offset.x) && Number.isFinite(offset.y)) {\n" +
		"      return { x: Math.trunc(box.x + offset.x), y: Math.trunc(box.y + offset.y) };\n" +
		"    }\n" +
		"    return { x: Math.trunc(box.x + box.width / 2), y: Math.trunc(box.y + box.height / 2) };\n" +
		"  };\n"
}

// buildCoordinateResolutionBody 构建坐标解析主体。
// 对齐 Python: _build_coordinate_resolution_body (L382-414)
// 提示词逐字符复制 Python 原文
func buildCoordinateResolutionBody() string {
	return "  let source = null;\n" +
		"  let target = null;\n" +
		"  if (params.element_source || params.element_target) {\n" +
		"    if (params.element_source) {\n" +
		"      source = await getPoint(params.element_source, params.element_source_offset, 'source');\n" +
		"      if (!source) {\n" +
		"        return { ok: false, error: 'Failed to determine source coordinates from selector." +
		" Use the exact visible text (e.g. \"Learn more\" not \"More information\")" +
		" or a valid CSS/Playwright selector.', source: null, target: null };\n" +
		"      }\n" +
		"    }\n" +
		"    if (params.element_target) {\n" +
		"      target = await getPoint(params.element_target, params.element_target_offset, 'target');\n" +
		"      if (!target) {\n" +
		"        return { ok: false, error: 'Failed to determine target coordinates from selector'," +
		" source, target: null };\n" +
		"      }\n" +
		"    }\n" +
		"  } else {\n" +
		"    const values = [params.coord_source_x, params.coord_source_y," +
		" params.coord_target_x, params.coord_target_y];\n" +
		"    const allFinite = values.every((v) => Number.isFinite(v));\n" +
		"    if (!allFinite) {\n" +
		"      return { ok: false, error: 'Must provide either source/target selectors" +
		" or source/target coordinates' };\n" +
		"    }\n" +
		"    source = { x: Math.trunc(params.coord_source_x), y: Math.trunc(params.coord_source_y) };\n" +
		"    target = { x: Math.trunc(params.coord_target_x), y: Math.trunc(params.coord_target_y) };\n" +
		"  }\n" +
		"  return { ok: true, source, target, error: null };\n"
}

// buildDragOperationBody 构建拖拽操作主体。
// 对齐 Python: _build_drag_operation_body (L417-470)
// 提示词逐字符复制 Python 原文
func buildDragOperationBody() string {
	return "  let source = null;\n" +
		"  let target = null;\n" +
		"  if (params.element_source && params.element_target) {\n" +
		"    source = await getPoint(params.element_source, params.element_source_offset, 'source');\n" +
		"    target = await getPoint(params.element_target, params.element_target_offset, 'target');\n" +
		"    if (!source || !target) {\n" +
		"      return { ok: false, error: 'Failed to determine source or target coordinates from selectors'," +
		" source, target };\n" +
		"    }\n" +
		"  } else {\n" +
		"    const values = [params.coord_source_x, params.coord_source_y," +
		" params.coord_target_x, params.coord_target_y];\n" +
		"    const allFinite = values.every((v) => Number.isFinite(v));\n" +
		"    if (!allFinite) {\n" +
		"      return { ok: false, error: 'Must provide either source/target selectors" +
		" or source/target coordinates' };\n" +
		"    }\n" +
		"    source = { x: Math.trunc(params.coord_source_x), y: Math.trunc(params.coord_source_y) };\n" +
		"    target = { x: Math.trunc(params.coord_target_x), y: Math.trunc(params.coord_target_y) };\n" +
		"  }\n" +
		"  const steps = Math.max(1, Number.isFinite(params.steps) ? Math.trunc(params.steps) : 10);\n" +
		"  const delayMs = Math.max(0, Number.isFinite(params.delay_ms) ? Math.trunc(params.delay_ms) : 5);\n" +
		"  try {\n" +
		"    await page.mouse.move(source.x, source.y);\n" +
		"    await page.mouse.down();\n" +
		"    for (let i = 1; i <= steps; i += 1) {\n" +
		"      const ratio = i / steps;\n" +
		"      const x = Math.trunc(source.x + (target.x - source.x) * ratio);\n" +
		"      const y = Math.trunc(source.y + (target.y - source.y) * ratio);\n" +
		"      await page.mouse.move(x, y);\n" +
		"      if (delayMs > 0) {\n" +
		"        await new Promise((resolve) => setTimeout(resolve, delayMs));\n" +
		"      }\n" +
		"    }\n" +
		"    await page.mouse.move(target.x, target.y);\n" +
		"    await page.mouse.move(target.x, target.y);\n" +
		"    await page.mouse.up();\n" +
		"  } catch (error) {\n" +
		"    return {\n" +
		"      ok: false,\n" +
		"      error: `Error during drag operation: ${String(error)}`,\n" +
		"      source,\n" +
		"      target,\n" +
		"      steps,\n" +
		"      delay_ms: delayMs,\n" +
		"    };\n" +
		"  }\n" +
		"  const message = params.element_source && params.element_target\n" +
		"    ? `Dragged element '${params.element_source}' to '${params.element_target}'`\n" +
		"    : `Dragged from (${source.x}, ${source.y}) to (${target.x}, ${target.y})`;\n" +
		"  return { ok: true, message, source, target, steps, delay_ms: delayMs, error: null };\n"
}

// ─── 通用辅助函数 ───

// toIntOrNone 尝试将 any 转为 *int。
// 对齐 Python: _to_int_or_none
func toIntOrNone(v any) (*int, error) {
	if v == nil {
		return nil, nil
	}
	switch n := v.(type) {
	case int:
		return &n, nil
	case int64:
		i := int(n)
		return &i, nil
	case float64:
		i := int(n)
		return &i, nil
	default:
		return nil, fmt.Errorf("cannot convert %T to int", v)
	}
}

// toIntPtrOrNone 从 map 中取 int 指针值。
func toIntPtrOrNone(m map[string]any, key string) *int {
	if v, ok := m[key]; ok && v != nil {
		n, err := toIntOrNone(v)
		if err != nil {
			return nil
		}
		return n
	}
	return nil
}

// getStr 从 map 中取字符串值。
func getStr(m map[string]any, key string) string {
	if v, ok := m[key]; ok && v != nil {
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	return ""
}

// resolveUploadRoot 解析上传根目录。
// 对齐 Python: resolve_upload_root (utils/env.py)
func resolveUploadRoot() string {
	for _, envKey := range []string{"BROWSER_UPLOAD_ROOT", "PLAYWRIGHT_UPLOAD_ROOT"} {
		if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
			return v
		}
	}
	return ""
}

// listDirFiles 列出目录下的文件。
// 对齐 Python: _list_dir_files (L588-601)
func listDirFiles(root string) []map[string]any {
	entries := []map[string]any{}
	items, err := os.ReadDir(root)
	if err != nil {
		return entries
	}
	for _, item := range items {
		if item.IsDir() {
			continue
		}
		info, err := item.Info()
		if err != nil {
			continue
		}
		entries = append(entries, map[string]any{
			"name":       item.Name(),
			"path":       filepath.Join(root, item.Name()),
			"size_bytes": info.Size(),
		})
	}
	return entries
}
