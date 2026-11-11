package processcli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli/protocol"
)

// 本文件是 channels/process_cli/control_commands.py 的 Go 对应物。
//
// Python: channels/process_cli/control_commands.py（222 行）
//
// 职责：REPL 的只读 Runtime 查询与展示。每次查询 fork 自己的进程跑
// --query-json，保证「查状态」既不污染执行中的会话，也不延长主体生命周期（P-6）。

// ──────────────────────────── 结构体 ────────────────────────────

// ControlQueryError 查询 worker 失败且不暴露其诊断流。
// Python: control_commands.py:20-21 ControlQueryError
type ControlQueryError struct {
	// Message 用户可见文案
	Message string
}

// Error 实现 error 接口。
func (e *ControlQueryError) Error() string { return e.Message }

// QueryRuntimeOptions QueryRuntime 的入参。
type QueryRuntimeOptions struct {
	// Cwd 工作目录
	Cwd string
	// Params 查询参数
	Params map[string]any
	// TimeoutSeconds 查询超时（秒）
	TimeoutSeconds float64
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultQueryTimeoutSeconds 默认查询超时
	// Python: control_commands.py:34 timeout: float = 30.0
	defaultQueryTimeoutSeconds = 30.0
	// queryTimeoutGraceSeconds 子进程超时余量
	// Python: control_commands.py:57 timeout=timeout + 5
	queryTimeoutGraceSeconds = 5.0
)

// ──────────────────────────── 导出函数 ────────────────────────────

// QueryRuntime 在独立进程中跑一次只读 Runtime 查询。
// Python: control_commands.py:24-66 query_runtime
//
// 六步：组请求 JSON → fork 自身 → 写 stdin → 读 stdout → 校验 → 返回 data。
func QueryRuntime(operation string, options QueryRuntimeOptions) (map[string]any, error) {
	params := options.Params
	if params == nil {
		params = map[string]any{}
	}
	timeout := options.TimeoutSeconds
	if timeout <= 0 {
		timeout = defaultQueryTimeoutSeconds
	}

	request := map[string]any{
		"schema_version":  protocol.CurrentSchemaVersion,
		"type":            "query",
		"operation":       operation,
		"params":          params,
		"workspace":       map[string]any{"cwd": options.Cwd},
		"timeout_seconds": timeout,
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, &ControlQueryError{Message: "Runtime 查询请求构造失败"}
	}

	executable, err := os.Executable()
	if err != nil {
		return nil, &ControlQueryError{Message: "Runtime 查询无法定位可执行文件"}
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration((timeout+queryTimeoutGraceSeconds)*float64(time.Second)),
	)
	defer cancel()

	command := exec.CommandContext(ctx, executable, "--query-json", "-")
	command.Stdin = strings.NewReader(string(payload))
	command.Env = append(os.Environ(), "UAPCLAW_PROCESS_IOENCODING=utf-8")

	output, err := command.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, &ControlQueryError{Message: "Runtime 查询超时"}
		}
		return nil, &ControlQueryError{Message: "Runtime 查询未返回有效结果"}
	}

	var result map[string]any
	if unmarshalErr := json.Unmarshal(output, &result); unmarshalErr != nil {
		return nil, &ControlQueryError{Message: "Runtime 查询未返回有效结果"}
	}
	if validateErr := validateQueryResult(result, operation); validateErr != nil {
		return nil, validateErr
	}
	data, ok := result["data"].(map[string]any)
	if !ok {
		return nil, &ControlQueryError{Message: "Runtime 查询结果缺少数据"}
	}
	return data, nil
}

// ShowSessions 渲染 Runtime 边界返回的 Process CLI 会话。
// Python: control_commands.py:69-120 show_sessions
func ShowSessions(ui *ProcessCliUI, data map[string]any, current, search string) ([]string, error) {
	rawSessions, ok := data["sessions"].([]any)
	if !ok {
		return nil, &ControlQueryError{Message: "会话列表格式无效"}
	}
	heading := fmt.Sprintf("共 %d 个会话；当前页偏移 %d", intOr(data["total"], 0), intOr(data["offset"], 0))
	if search != "" {
		heading += "；搜索：" + search
	}
	lines := []string{heading}
	sessionIDs := make([]string, 0, len(rawSessions))

	for _, item := range rawSessions {
		mapping, ok := item.(map[string]any)
		if !ok {
			continue
		}
		sessionID := stringOr(mapping["session_id"])
		if sessionID == "" {
			continue
		}
		sessionIDs = append(sessionIDs, sessionID)
		marker := " "
		if sessionID == current {
			marker = "*"
		}
		title := stringOr(mapping["title"])
		if title == "" {
			title = "未命名"
		}
		mode := stringOr(mapping["mode"])
		if mode == "" {
			mode = "未知模式"
		}
		project := stringOr(mapping["project_dir"])
		if project == "" {
			project = stringOr(mapping["project_id"])
		}
		if project == "" {
			project = "未记录"
		}
		activity := "未知"
		if timestamp, ok := toFloat(mapping["last_message_at"]); ok && timestamp > 0 {
			activity = time.Unix(int64(timestamp), 0).Local().Format("2006-01-02 15:04")
		}
		lines = append(lines,
			fmt.Sprintf("%d. %s %s · %s · %s", len(sessionIDs), marker, sessionID, title, mode))
		lines = append(lines, fmt.Sprintf("   项目：%s · 最近活动：%s", project, activity))
	}
	if len(sessionIDs) == 0 {
		if search != "" {
			lines = append(lines, "没有匹配的进程式 CLI 会话")
		} else {
			lines = append(lines, "暂无进程式 CLI 会话")
		}
	}
	ui.Details("会话", lines)
	return sessionIDs, nil
}

// ShowModels 渲染模型列表。
// Python: control_commands.py:123-146 show_models
func ShowModels(ui *ProcessCliUI, data map[string]any, selected string) error {
	rawModels, ok := data["models"].([]any)
	if !ok {
		return &ControlQueryError{Message: "模型列表格式无效"}
	}
	lines := make([]string, 0, len(rawModels)+1)
	for _, item := range rawModels {
		mapping, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key := stringOr(mapping["selection_key"])
		marker := " "
		isCurrent, _ := mapping["is_current"].(bool)
		if key == selected || (selected == "" && isCurrent) {
			marker = "*"
		}
		name := stringOr(mapping["display_name"])
		if name == "" {
			name = stringOr(mapping["model_name"])
		}
		if name == "" {
			name = key
		}
		provider := stringOr(mapping["provider"])
		lines = append(lines, strings.TrimRight(fmt.Sprintf("%s %s [%s] %s", marker, name, key, provider), " "))
	}
	if len(lines) == 0 {
		lines = append(lines, "暂无可用模型")
	}
	lines = append(lines, "使用 /model <名称或选择键> 切换下一轮模型")
	ui.Details("模型", lines)
	return nil
}

// ShowStatus 渲染当前状态。
// Python: control_commands.py:149-176 show_status
func ShowStatus(
	ui *ProcessCliUI, sessionID string, session map[string]any,
	nextMode, nextModel, cwd string,
) error {
	current := "尚未创建"
	if sessionID != "" {
		current = sessionID
	}
	lines := []string{fmt.Sprintf("当前会话：%s", current)}
	if session != nil {
		mode := stringOr(session["mode"])
		if mode == "" {
			mode = "未记录"
		}
		model := stringOr(session["model"])
		if model == "" {
			model = "未记录"
		}
		projectDir := stringOr(session["project_dir"])
		if projectDir == "" {
			projectDir = cwd
		}
		lines = append(lines,
			fmt.Sprintf("会话模式：%s", mode),
			fmt.Sprintf("会话模型：%s", model),
			fmt.Sprintf("消息数：%d", intOr(session["message_count"], 0)),
			fmt.Sprintf("项目目录：%s", projectDir),
		)
	}
	lines = append(lines,
		fmt.Sprintf("下一轮模式：%s", nextMode),
		fmt.Sprintf("下一轮模型：%s", nextModel),
		fmt.Sprintf("工作目录：%s", cwd),
	)
	ui.Details("状态", lines)
	return nil
}

// ShowPermissions 渲染权限快照（只读）。
// Python: control_commands.py:179-215 show_permissions
func ShowPermissions(ui *ProcessCliUI, data map[string]any) error {
	effective, ok := data["effective"].(map[string]any)
	if !ok {
		return &ControlQueryError{Message: "权限快照格式无效"}
	}
	scope := "全局"
	if stringOr(data["scope"]) == "session" {
		scope = "当前会话"
	}
	enabled, hasEnabled := effective["enabled"].(bool)
	state := "未指定"
	if hasEnabled {
		if enabled {
			state = "启用"
		} else {
			state = "关闭"
		}
	}
	lines := []string{
		fmt.Sprintf("范围：%s", scope),
		fmt.Sprintf("权限开关：%s", state),
	}

	if tools, ok := effective["tools"].([]any); ok {
		counts := map[string]int{"allow": 0, "ask": 0, "deny": 0}
		for _, item := range tools {
			mapping, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if _, known := counts[stringOr(mapping["level"])]; known {
				counts[stringOr(mapping["level"])]++
			}
		}
		lines = append(lines, fmt.Sprintf("工具配置：allow %d、ask %d、deny %d",
			counts["allow"], counts["ask"], counts["deny"]))
		for _, item := range tools {
			mapping, ok := item.(map[string]any)
			if !ok {
				continue
			}
			level := stringOr(mapping["level"])
			if level != "ask" && level != "deny" {
				continue
			}
			lines = append(lines, fmt.Sprintf("%s: %s", stringOr(mapping["name"]), level))
		}
	}

	if rules, ok := effective["rules"].([]any); ok {
		lines = append(lines, fmt.Sprintf("规则配置：%d 条", len(rules)))
		for _, rule := range rules {
			mapping, ok := rule.(map[string]any)
			if !ok {
				continue
			}
			names := make([]string, 0, 2)
			if rawTools, ok := mapping["tools"].([]any); ok {
				for _, name := range rawTools {
					names = append(names, stringOr(name))
				}
			}
			id := stringOr(mapping["id"])
			if id == "" {
				id = "规则"
			}
			action := stringOr(mapping["action"])
			if action == "" {
				action = "未指定"
			}
			lines = append(lines, fmt.Sprintf("%s · %s · %s", id, action, strings.Join(names, ", ")))
		}
	}

	if hasEnabled && !enabled {
		lines = append(lines, "当前权限检查已关闭；以上为配置快照。")
	}
	ui.Details("权限（只读）", lines)
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateQueryResult 校验查询 worker 的返回结构。
// Python: control_commands.py:58-64（operation / status / data 三项检查）
func validateQueryResult(result map[string]any, operation string) error {
	if stringOr(result["operation"]) != operation {
		return &ControlQueryError{Message: "Runtime 查询结果与请求不匹配"}
	}
	if stringOr(result["status"]) != string(protocol.StatusCompleted) {
		code := "QUERY_FAILED"
		if errorInfo, ok := result["error"].(map[string]any); ok {
			if raw := stringOr(errorInfo["code"]); raw != "" {
				code = raw
			}
		}
		return &ControlQueryError{Message: fmt.Sprintf("Runtime 查询失败：%s", code)}
	}
	if _, ok := result["data"].(map[string]any); !ok {
		return &ControlQueryError{Message: "Runtime 查询结果缺少数据"}
	}
	return nil
}

// intOr 取整数或默认值。
func intOr(value any, fallback int) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return fallback
	}
}

// toFloat 取浮点或 false。
func toFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}
