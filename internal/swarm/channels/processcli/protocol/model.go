// Package protocol 提供 Process CLI 一次性执行协议的机器面契约。
//
// 与上游 jiuwenswarm/channels/process_cli/protocol/ 一对一：
// version / model / jsonl / contracts / query / schema / capabilities / schema.json。
//
// 这些类型描述机器面适配器，刻意不实现常驻服务、JSON-RPC 方法或会话控制面。
// Process CLI 仍是传输持有者，把共享的 Runtime 事件流转换成这些记录。
package protocol

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"

	"encoding/json"
)

// ──────────────────────────── 结构体 ────────────────────────────

// JsonObject JSON 对象。
// Python: model.py:32 JsonObject: TypeAlias = Mapping[str, JsonValue]
type JsonObject = map[string]any

// AgentSpec 共享 Runtime 中一个 root Agent 的声明式引用。
// Python: model.py:190-276 AgentSpec
//
// tools 沿用既有 Agent 定义语义：("*",) 请求配置好的工具集，
// 显式元组是白名单（空元组禁用全部工具）。Runtime 策略可进一步收紧。
// 本契约排除 Team/Workflow 入口模式；选中的 Agent 仍可使用 Runtime 管理的内部能力。
type AgentSpec struct {
	// Name Agent 名（匹配 [A-Za-z0-9_-]{3,50}）
	Name string
	// Instructions 指令（保留原文空白，但不得为空白）
	Instructions string
	// Description 可选描述
	Description *string
	// Model 可选模型标识
	Model *string
	// Tools 工具名列表；("*",) 表示请求全部
	Tools []string
	// Skills 技能名列表
	Skills []string
	// MaxIterations 可选最大迭代次数（须为正整数）
	MaxIterations *int
}

// HostToolSpec 一个进程本地工具，实现住在 SDK 宿主里。
// Python: model.py:279-317 HostToolSpec
type HostToolSpec struct {
	// Name 工具名（ASCII 标识符）
	Name string
	// Description 描述
	Description string
	// InputSchema 入参 schema（必须描述 object）
	InputSchema JsonObject
}

// WorkspaceSpec 传给既有 Runtime 安全边界的文件系统上下文。
// Python: model.py:320-363 WorkspaceSpec
type WorkspaceSpec struct {
	// Cwd 工作目录
	Cwd *string
	// ProjectDir 项目目录
	ProjectDir *string
	// TrustedDirs 可信目录
	TrustedDirs []string
}

// OneShotRunInput 一个命令进程、恰好一个 Runtime 生命周期的输入。
// Python: model.py:366-568 OneShotRunInput
//
// session_id 请求复用该 Runtime 会话身份与其持久化状态（存在时）。
// 省略则创建会话；省略的 mode 随后解析为 agent.code.normal。
// 对已存在会话，省略的 mode 与 workspace 继承持久化的会话绑定。
// 对新会话，省略的 workspace 使用命令进程工作目录，项目目录沿用既有
// Process CLI 默认。agent 可选，让调用方使用配置好的默认 Agent；
// 它永远不会把 Team / Workflow / AutoHarness 选为 root 入口模式。
type OneShotRunInput struct {
	// SchemaVersion 协议版本
	SchemaVersion string
	// RequestID 请求标识（可选）
	RequestID *string
	// SessionID 会话标识（可选）
	SessionID *string
	// Input 用户输入（保留原文，不得为空白）
	Input string
	// Agent 声明式 root Agent（可选）
	Agent *AgentSpec
	// Mode 运行模式（可选；新建会话时默认 agent.code.normal）
	Mode *string
	// Model 模型标识（可选）
	Model *string
	// Skills 技能名列表（可选）
	Skills []string
	// Mcp MCP 引用列表（可选）
	Mcp []string
	// Permissions 运行级工具权限（可选）
	Permissions JsonObject
	// OutputSchema 结构化输出 schema（可选）
	OutputSchema JsonObject
	// MaxTurns 最大轮次（可选，1..1000）
	MaxTurns *int
	// MaxBudgetUSD 最大预算（可选，正数）
	MaxBudgetUSD *float64
	// HostTools 宿主工具列表
	HostTools []HostToolSpec
	// Workspace 工作区（可选）
	Workspace *WorkspaceSpec
	// TimeoutSeconds 执行超时秒数（可选，正数）
	TimeoutSeconds *float64
}

// RuntimeErrorInfo 传输安全的错误详情；未知的未来错误码仍可解码。
// Python: model.py:571-616 RuntimeErrorInfo
type RuntimeErrorInfo struct {
	// Code 错误码
	Code string
	// Message 错误消息（保留原文）
	Message string
	// Retryable 是否可重试
	Retryable bool
	// Details 附加详情
	Details JsonObject
}

// OneShotEvent 一条共享 Runtime 事件的只读 JSONL 观测。
// Python: model.py:619-741 OneShotEvent
//
// event_type 保持开放，让兼容的 SDK 能忽略新的观测事件。协议修订 1 为关键
// 事件类型定义了公开字段；其余 payload 字段仍是 Runtime 拥有的观测。
// 传输与内部 Runtime 元数据刻意省略。只有 OneShotRunResult 表示命令进程的结果。
type OneShotEvent struct {
	// SchemaVersion 协议版本
	SchemaVersion string
	// Sequence 序号（从 0 连续递增）
	Sequence int
	// RequestID 请求标识
	RequestID string
	// SessionID 会话标识（可选）
	SessionID *string
	// EventType 事件类型
	EventType string
	// Payload 事件负载
	Payload JsonObject
}

// OneShotRunResult 进程退出前发出的唯一终态记录。
// Python: model.py:744-885 OneShotRunResult
//
// exit_code 是真实进程状态。除成功运行外，调用方应使用结构化错误，
// 而不是从数字值推导语义。
type OneShotRunResult struct {
	// SchemaVersion 协议版本
	SchemaVersion string
	// Sequence 序号
	Sequence int
	// RequestID 请求标识
	RequestID string
	// SessionID 会话标识（completed 时必填）
	SessionID *string
	// Status 终态
	Status string
	// ExitCode 退出码
	ExitCode int
	// Output 完整文本输出
	Output *string
	// OutputJSON 结构化输出
	OutputJSON JsonObject
	// Error 错误详情（非 completed 时必填）
	Error *RuntimeErrorInfo
	// Usage 用量快照
	Usage JsonObject
}

// ──────────────────────────── 枚举 ────────────────────────────

// SingleAgentMode 冻结进首个单 Agent 机器 schema 的规范模式。
// Python: model.py:38-44 SingleAgentMode(str, Enum)
type SingleAgentMode string

const (
	// ModeAgentWorkNormal agent.work.normal
	ModeAgentWorkNormal SingleAgentMode = "agent.work.normal"
	// ModeAgentWorkPlan agent.work.plan
	ModeAgentWorkPlan SingleAgentMode = "agent.work.plan"
	// ModeAgentCodeNormal agent.code.normal
	ModeAgentCodeNormal SingleAgentMode = "agent.code.normal"
	// ModeAgentCodePlan agent.code.plan
	ModeAgentCodePlan SingleAgentMode = "agent.code.plan"
)

// RunStatus 一个命令进程的终态。
// Python: model.py:47-53 RunStatus(str, Enum)
type RunStatus string

const (
	// StatusCompleted 成功完成
	StatusCompleted RunStatus = "completed"
	// StatusFailed 失败
	StatusFailed RunStatus = "failed"
	// StatusCancelled 被取消
	StatusCancelled RunStatus = "cancelled"
	// StatusTimedOut 超时
	StatusTimedOut RunStatus = "timed_out"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// agentNamePattern Agent 名合法字符。
	// Python: model.py:34 _AGENT_NAME = re.compile(r"[A-Za-z0-9_-]{3,50}\Z")
	agentNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{3,50}$`)
	// toolNamePattern 工具名合法字符。
	// Python: model.py:35 _TOOL_NAME = re.compile(r"[A-Za-z][A-Za-z0-9_-]{0,63}\Z")
	toolNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// FreezeJson 校验并冻结一个 JSON 值（拒绝非有限数字、非 string key、循环引用）。
// Python: model.py:134-172 _freeze_json
//
// Go 无 MappingProxyType/tuple，返回不可变快照的等价物：校验通过的深拷贝。
func FreezeJson(name string, value any) (any, error) {
	return freezeJson(name, value, map[uintptr]bool{})
}

// ThawJson 把冻结值还原为可变副本。
// Python: model.py:182-187 _thaw_json
func ThawJson(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = ThawJson(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = ThawJson(item)
		}
		return out
	case []string:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = item
		}
		return out
	default:
		return value
	}
}

// NewAgentSpec 构造并校验 AgentSpec。
// Python: AgentSpec.__post_init__（model.py:209-233）
func NewAgentSpec(name, instructions string) (*AgentSpec, error) {
	return NewAgentSpecWithTools(name, instructions, []string{"*"})
}

// NewAgentSpecWithTools 构造并校验 AgentSpec，显式给 tools。
func NewAgentSpecWithTools(name, instructions string, tools []string) (*AgentSpec, error) {
	spec := &AgentSpec{
		Name:         name,
		Instructions: instructions,
		Tools:        tools,
		Skills:       []string{},
	}
	if err := spec.validate(); err != nil {
		return nil, err
	}
	return spec, nil
}

// ValidateMaxIterations 校验 max_iterations（0 被拒）。
// Python: model.py:230-233 if value == 0: raise ValueError("max_iterations must be greater than zero")
func (s *AgentSpec) ValidateMaxIterations(value *int) error {
	if value == nil {
		return nil
	}
	if *value < 0 {
		return fmt.Errorf("max_iterations must not be negative")
	}
	if *value == 0 {
		return fmt.Errorf("max_iterations must be greater than zero")
	}
	return nil
}

// ToDict 返回稳定的 JSON 兼容表示。
// Python: model.py:235-246 AgentSpec.to_dict
func (s *AgentSpec) ToDict() map[string]any {
	if s == nil {
		return nil
	}
	out := map[string]any{
		"name":           s.Name,
		"instructions":   s.Instructions,
		"description":    optionalStringValue(s.Description),
		"model":          optionalStringValue(s.Model),
		"tools":          stringSliceAny(s.Tools),
		"skills":         stringSliceAny(s.Skills),
		"max_iterations": optionalIntValue(s.MaxIterations),
	}
	return out
}

// AgentSpecFromDict 解码一个严格的 Agent 声明。
// Python: model.py:248-276 AgentSpec.from_dict
func AgentSpecFromDict(value any) (*AgentSpec, error) {
	data, err := strictObject("agent", value,
		map[string]struct{}{
			"name": {}, "instructions": {}, "description": {}, "model": {},
			"tools": {}, "skills": {}, "max_iterations": {},
		},
		map[string]struct{}{"name": {}, "instructions": {}})
	if err != nil {
		return nil, err
	}
	spec := &AgentSpec{
		Name:         asString(data["name"]),
		Instructions: asString(data["instructions"]),
		Description:  optionalStringFromAny(data["description"]),
		Model:        optionalStringFromAny(data["model"]),
		Tools:        []string{"*"},
		Skills:       []string{},
	}
	if raw, ok := data["tools"]; ok {
		spec.Tools = stringSliceFromAny(raw)
	}
	if raw, ok := data["skills"]; ok {
		spec.Skills = stringSliceFromAny(raw)
	}
	if raw, ok := data["max_iterations"]; ok && raw != nil {
		n, convErr := nonNegativeInt("max_iterations", raw)
		if convErr != nil {
			return nil, convErr
		}
		spec.MaxIterations = &n
	}
	if err := spec.validate(); err != nil {
		return nil, err
	}
	return spec, nil
}

// ToDict 返回稳定的 JSON 兼容表示。
// Python: model.py:302-307 HostToolSpec.to_dict
func (h *HostToolSpec) ToDict() map[string]any {
	if h == nil {
		return nil
	}
	return map[string]any{
		"name":         h.Name,
		"description":  h.Description,
		"input_schema": ThawJson(h.InputSchema),
	}
}

// HostToolSpecFromDict 解码一个严格的宿主工具声明。
// Python: model.py:309-317 HostToolSpec.from_dict
func HostToolSpecFromDict(value any) (*HostToolSpec, error) {
	data, err := strictObject("host_tool", value,
		map[string]struct{}{"name": {}, "description": {}, "input_schema": {}},
		map[string]struct{}{"name": {}, "description": {}, "input_schema": {}})
	if err != nil {
		return nil, err
	}
	spec := &HostToolSpec{
		Name:        asString(data["name"]),
		Description: asString(data["description"]),
	}
	schema, freezeErr := freezeObject("host_tools.input_schema", data["input_schema"])
	if freezeErr != nil {
		return nil, freezeErr
	}
	spec.InputSchema = schema
	if err := spec.validate(); err != nil {
		return nil, err
	}
	return spec, nil
}

// ToDict 返回稳定的 JSON 兼容工作区。
// Python: model.py:341-348 WorkspaceSpec.to_dict
func (w *WorkspaceSpec) ToDict() map[string]any {
	if w == nil {
		return nil
	}
	return map[string]any{
		"cwd":          optionalStringValue(w.Cwd),
		"project_dir":  optionalStringValue(w.ProjectDir),
		"trusted_dirs": stringSliceAny(w.TrustedDirs),
	}
}

// WorkspaceSpecFromDict 解码一个严格的工作区对象。
// Python: model.py:350-363 WorkspaceSpec.from_dict
func WorkspaceSpecFromDict(value any) (*WorkspaceSpec, error) {
	data, err := strictObject("workspace", value,
		map[string]struct{}{"cwd": {}, "project_dir": {}, "trusted_dirs": {}}, nil)
	if err != nil {
		return nil, err
	}
	spec := &WorkspaceSpec{
		Cwd:        optionalStringFromAny(data["cwd"]),
		ProjectDir: optionalStringFromAny(data["project_dir"]),
		TrustedDirs: func() []string {
			if raw, ok := data["trusted_dirs"]; ok {
				return stringSliceFromAny(raw)
			}
			return []string{}
		}(),
	}
	if err := spec.validate(); err != nil {
		return nil, err
	}
	return spec, nil
}

// ToDict 返回完整的一次性输入记录。
// Python: model.py:483-510 OneShotRunInput.to_dict
func (in *OneShotRunInput) ToDict() map[string]any {
	if in == nil {
		return nil
	}
	hostTools := make([]any, 0, len(in.HostTools))
	for i := range in.HostTools {
		hostTools = append(hostTools, in.HostTools[i].ToDict())
	}
	return map[string]any{
		"schema_version":  in.SchemaVersion,
		"type":            "run",
		"request_id":      optionalStringValue(in.RequestID),
		"session_id":      optionalStringValue(in.SessionID),
		"input":           in.Input,
		"agent":           in.Agent.ToDict(),
		"mode":            optionalStringValue(in.Mode),
		"model":           optionalStringValue(in.Model),
		"skills":          optionalStringSliceValue(in.Skills),
		"mcp":             optionalStringSliceValue(in.Mcp),
		"permissions":     optionalJsonValue(in.Permissions),
		"output_schema":   optionalJsonValue(in.OutputSchema),
		"max_turns":       optionalIntValue(in.MaxTurns),
		"max_budget_usd":  optionalFloatValue(in.MaxBudgetUSD),
		"host_tools":      hostTools,
		"workspace":       in.Workspace.ToDict(),
		"timeout_seconds": optionalFloatValue(in.TimeoutSeconds),
	}
}

// OneShotRunInputFromDict 解码一个严格的 type=run 记录。
// Python: model.py:512-568 OneShotRunInput.from_dict
func OneShotRunInputFromDict(value any) (*OneShotRunInput, error) {
	data, err := strictObject("run", value,
		map[string]struct{}{
			"schema_version": {}, "type": {}, "request_id": {}, "session_id": {},
			"input": {}, "agent": {}, "mode": {}, "model": {}, "skills": {},
			"mcp": {}, "permissions": {}, "output_schema": {}, "max_turns": {},
			"max_budget_usd": {}, "host_tools": {}, "workspace": {}, "timeout_seconds": {},
		},
		map[string]struct{}{"schema_version": {}, "type": {}, "input": {}})
	if err != nil {
		return nil, err
	}
	if asString(data["type"]) != "run" {
		return nil, fmt.Errorf("run.type must be 'run'")
	}

	version, verr := RequireSupportedSchemaVersion(data["schema_version"])
	if verr != nil {
		return nil, fmt.Errorf("schema_version is not supported")
	}

	in := &OneShotRunInput{
		SchemaVersion: version,
		RequestID:     optionalStringFromAny(data["request_id"]),
		SessionID:     optionalStringFromAny(data["session_id"]),
		Input:         asString(data["input"]),
		Model:         optionalStringFromAny(data["model"]),
		HostTools:     []HostToolSpec{},
	}
	if raw, ok := data["agent"]; ok && raw != nil {
		agent, aerr := AgentSpecFromDict(raw)
		if aerr != nil {
			return nil, aerr
		}
		in.Agent = agent
	}
	if raw, ok := data["skills"]; ok && raw != nil {
		in.Skills = stringSliceFromAny(raw)
	}
	if raw, ok := data["mcp"]; ok && raw != nil {
		in.Mcp = stringSliceFromAny(raw)
	}
	if raw, ok := data["permissions"]; ok && raw != nil {
		in.Permissions = jsonObjectFromAny(raw)
	}
	if raw, ok := data["output_schema"]; ok && raw != nil {
		in.OutputSchema = jsonObjectFromAny(raw)
	}
	if raw, ok := data["max_turns"]; ok && raw != nil {
		n, convErr := nonNegativeInt("max_turns", raw)
		if convErr != nil {
			return nil, convErr
		}
		in.MaxTurns = &n
	}
	if raw, ok := data["max_budget_usd"]; ok && raw != nil {
		f, convErr := positiveNumber("max_budget_usd", raw)
		if convErr != nil {
			return nil, convErr
		}
		in.MaxBudgetUSD = &f
	}
	if raw, ok := data["host_tools"]; ok && raw != nil {
		list, listErr := anySlice("host_tools", raw)
		if listErr != nil {
			return nil, listErr
		}
		for _, item := range list {
			tool, toolErr := HostToolSpecFromDict(item)
			if toolErr != nil {
				return nil, toolErr
			}
			in.HostTools = append(in.HostTools, *tool)
		}
	}
	if raw, ok := data["mode"]; ok && raw != nil {
		in.Mode = optionalStringFromAny(raw)
	}
	if raw, ok := data["workspace"]; ok && raw != nil {
		ws, wsErr := WorkspaceSpecFromDict(raw)
		if wsErr != nil {
			return nil, wsErr
		}
		in.Workspace = ws
	}
	if raw, ok := data["timeout_seconds"]; ok && raw != nil {
		f, convErr := positiveNumber("timeout_seconds", raw)
		if convErr != nil {
			return nil, convErr
		}
		in.TimeoutSeconds = &f
	}
	if err := in.validate(); err != nil {
		return nil, err
	}
	return in, nil
}

// ToDict 返回稳定的 JSON 兼容错误。
// Python: model.py:590-598 RuntimeErrorInfo.to_dict
func (e *RuntimeErrorInfo) ToDict() map[string]any {
	if e == nil {
		return nil
	}
	details := e.Details
	if details == nil {
		details = JsonObject{}
	}
	return map[string]any{
		"code":      e.Code,
		"message":   e.Message,
		"retryable": e.Retryable,
		"details":   ThawJson(details),
	}
}

// RuntimeErrorInfoFromDict 解码一个结构化 Runtime 错误。
// Python: model.py:600-616 RuntimeErrorInfo.from_dict
func RuntimeErrorInfoFromDict(value any) (*RuntimeErrorInfo, error) {
	data, err := strictObject("error", value,
		map[string]struct{}{"code": {}, "message": {}, "retryable": {}, "details": {}},
		map[string]struct{}{"code": {}, "message": {}})
	if err != nil {
		return nil, err
	}
	info := &RuntimeErrorInfo{
		Code:    asString(data["code"]),
		Message: asString(data["message"]),
	}
	if raw, ok := data["retryable"].(bool); ok {
		info.Retryable = raw
	}
	if raw, ok := data["details"]; ok && raw != nil {
		info.Details = jsonObjectFromAny(raw)
	}
	if err := info.validate(); err != nil {
		return nil, err
	}
	return info, nil
}

// ToDict 返回一条 JSONL 兼容的事件记录。
// Python: model.py:674-685 OneShotEvent.to_dict
func (e *OneShotEvent) ToDict() map[string]any {
	if e == nil {
		return nil
	}
	var payload any
	if e.Payload != nil {
		payload = ThawJson(e.Payload)
	}
	return map[string]any{
		"schema_version": e.SchemaVersion,
		"type":           "event",
		"sequence":       e.Sequence,
		"request_id":     e.RequestID,
		"session_id":     optionalStringValue(e.SessionID),
		"event_type":     e.EventType,
		"payload":        payload,
	}
}

// OneShotEventFromDict 解码一个严格的 type=event 记录。
// Python: model.py:709-741 OneShotEvent.from_dict
func OneShotEventFromDict(value any) (*OneShotEvent, error) {
	data, err := strictObject("event", value,
		map[string]struct{}{
			"schema_version": {}, "type": {}, "sequence": {}, "request_id": {},
			"session_id": {}, "event_type": {}, "payload": {},
		},
		map[string]struct{}{
			"schema_version": {}, "type": {}, "sequence": {},
			"request_id": {}, "event_type": {},
		})
	if err != nil {
		return nil, err
	}
	if asString(data["type"]) != "event" {
		return nil, fmt.Errorf("event.type must be 'event'")
	}
	version, verr := RequireSupportedSchemaVersion(data["schema_version"])
	if verr != nil {
		return nil, fmt.Errorf("schema_version is not supported")
	}
	seq, serr := nonNegativeInt("sequence", data["sequence"])
	if serr != nil {
		return nil, serr
	}
	ev := &OneShotEvent{
		SchemaVersion: version,
		Sequence:      seq,
		RequestID:     asString(data["request_id"]),
		SessionID:     optionalStringFromAny(data["session_id"]),
		EventType:     asString(data["event_type"]),
	}
	if raw, ok := data["payload"]; ok && raw != nil {
		ev.Payload = jsonObjectFromAny(raw)
	}
	if err := ev.validate(); err != nil {
		return nil, err
	}
	return ev, nil
}

// ToDict 返回终态 JSONL 兼容的结果记录。
// Python: model.py:816-833 OneShotRunResult.to_dict
func (r *OneShotRunResult) ToDict() map[string]any {
	if r == nil {
		return nil
	}
	var outputJSON any
	if r.OutputJSON != nil {
		outputJSON = ThawJson(r.OutputJSON)
	}
	var errDict any
	if r.Error != nil {
		errDict = r.Error.ToDict()
	}
	usage := r.Usage
	if usage == nil {
		usage = JsonObject{}
	}
	return map[string]any{
		"schema_version": r.SchemaVersion,
		"type":           "result",
		"sequence":       r.Sequence,
		"request_id":     r.RequestID,
		"session_id":     optionalStringValue(r.SessionID),
		"status":         r.Status,
		"exit_code":      r.ExitCode,
		"output":         optionalStringValue(r.Output),
		"output_json":    outputJSON,
		"error":          errDict,
		"usage":          ThawJson(usage),
	}
}

// OneShotRunResultFromDict 解码一个严格的 type=result 记录。
// Python: model.py:835-885 OneShotRunResult.from_dict
func OneShotRunResultFromDict(value any) (*OneShotRunResult, error) {
	data, err := strictObject("result", value,
		map[string]struct{}{
			"schema_version": {}, "type": {}, "sequence": {}, "request_id": {},
			"session_id": {}, "status": {}, "exit_code": {}, "output": {},
			"output_json": {}, "error": {}, "usage": {},
		},
		map[string]struct{}{
			"schema_version": {}, "type": {}, "sequence": {},
			"request_id": {}, "status": {}, "exit_code": {},
		})
	if err != nil {
		return nil, err
	}
	if asString(data["type"]) != "result" {
		return nil, fmt.Errorf("result.type must be 'result'")
	}
	version, verr := RequireSupportedSchemaVersion(data["schema_version"])
	if verr != nil {
		return nil, fmt.Errorf("schema_version is not supported")
	}
	seq, serr := nonNegativeInt("sequence", data["sequence"])
	if serr != nil {
		return nil, serr
	}
	exitCode, eerr := nonNegativeInt("exit_code", data["exit_code"])
	if eerr != nil {
		return nil, eerr
	}
	res := &OneShotRunResult{
		SchemaVersion: version,
		Sequence:      seq,
		RequestID:     asString(data["request_id"]),
		SessionID:     optionalStringFromAny(data["session_id"]),
		Status:        asString(data["status"]),
		ExitCode:      exitCode,
		Output:        optionalStringFromAny(data["output"]),
	}
	if raw, ok := data["output_json"]; ok && raw != nil {
		res.OutputJSON = jsonObjectFromAny(raw)
	}
	if raw, ok := data["error"]; ok && raw != nil {
		info, ierr := RuntimeErrorInfoFromDict(raw)
		if ierr != nil {
			return nil, ierr
		}
		res.Error = info
	}
	if raw, ok := data["usage"]; ok && raw != nil {
		res.Usage = jsonObjectFromAny(raw)
	}
	if err := res.validate(); err != nil {
		return nil, err
	}
	return res, nil
}

// OneShotRunResultParams NewOneShotRunResult 的入参。
type OneShotRunResultParams struct {
	// SchemaVersion 协议版本
	SchemaVersion string
	// Sequence 序号
	Sequence int
	// RequestID 请求标识
	RequestID string
	// SessionID 会话标识
	SessionID *string
	// Status 终态
	Status string
	// ExitCode 退出码
	ExitCode int
	// Output 完整文本输出
	Output *string
	// OutputJSON 结构化输出
	OutputJSON JsonObject
	// Error 错误详情
	Error *RuntimeErrorInfo
	// Usage 用量快照
	Usage JsonObject
}

// NewOneShotRunResult 构造并校验终态结果。
// Python: OneShotRunResult.__post_init__（model.py:764-814）
func NewOneShotRunResult(p OneShotRunResultParams) (*OneShotRunResult, error) {
	version := p.SchemaVersion
	if version == "" {
		version = CurrentSchemaVersion
	}
	v, verr := RequireSupportedSchemaVersion(version)
	if verr != nil {
		return nil, verr
	}
	res := &OneShotRunResult{
		SchemaVersion: v,
		Sequence:      p.Sequence,
		RequestID:     p.RequestID,
		SessionID:     p.SessionID,
		Status:        p.Status,
		ExitCode:      p.ExitCode,
		Output:        p.Output,
		OutputJSON:    p.OutputJSON,
		Error:         p.Error,
		Usage:         p.Usage,
	}
	if err := res.validate(); err != nil {
		return nil, err
	}
	return res, nil
}

// OneShotEventParams NewOneShotEvent 的入参。
type OneShotEventParams struct {
	// SchemaVersion 协议版本
	SchemaVersion string
	// Sequence 序号
	Sequence int
	// RequestID 请求标识
	RequestID string
	// SessionID 会话标识
	SessionID *string
	// EventType 事件类型
	EventType string
	// Payload 事件负载
	Payload JsonObject
}

// NewOneShotEvent 构造并校验一条观测事件。
// Python: OneShotEvent.__post_init__（model.py:637-672）
func NewOneShotEvent(p OneShotEventParams) (*OneShotEvent, error) {
	version := p.SchemaVersion
	if version == "" {
		version = CurrentSchemaVersion
	}
	v, verr := RequireSupportedSchemaVersion(version)
	if verr != nil {
		return nil, verr
	}
	ev := &OneShotEvent{
		SchemaVersion: v,
		Sequence:      p.Sequence,
		RequestID:     p.RequestID,
		SessionID:     p.SessionID,
		EventType:     p.EventType,
		Payload:       p.Payload,
	}
	if err := ev.validate(); err != nil {
		return nil, err
	}
	return ev, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validate AgentSpec 的构造期校验（语句顺序对齐 __post_init__）。
// Python: model.py:209-233
func (s *AgentSpec) validate() error {
	// 1. name = _required_text("name", self.name)
	name, err := requiredText("name", s.Name, false)
	if err != nil {
		return err
	}
	// 2. if _AGENT_NAME.fullmatch(name) is None: raise ValueError
	if !agentNamePattern.MatchString(name) {
		return fmt.Errorf("name must match [A-Za-z0-9_-]{3,50}")
	}
	s.Name = name
	// 3. instructions = _required_text(preserve=True)
	instructions, err := requiredText("instructions", s.Instructions, true)
	if err != nil {
		return err
	}
	s.Instructions = instructions
	// 4. description / model = _optional_text
	if s.Description, err = optionalText("description", s.Description); err != nil {
		return err
	}
	if s.Model, err = optionalText("model", s.Model); err != nil {
		return err
	}
	// 5. tools = _string_tuple
	tools, err := stringTuple("tools", s.Tools)
	if err != nil {
		return err
	}
	// 6. if "*" in tools and tools != ("*",): raise ValueError
	starAlone := len(tools) == 1 && tools[0] == "*"
	if containsString(tools, "*") && !starAlone {
		return fmt.Errorf("'*' must be the only tools entry when used")
	}
	s.Tools = tools
	// 7. skills = _string_tuple
	if s.Skills, err = stringTuple("skills", s.Skills); err != nil {
		return err
	}
	// 8. max_iterations 非 nil 时校验
	return s.ValidateMaxIterations(s.MaxIterations)
}

// validate HostToolSpec 的构造期校验。
// Python: model.py:287-300
func (h *HostToolSpec) validate() error {
	// 1. host_tools.name = _required_text
	name, err := requiredText("host_tools.name", h.Name, false)
	if err != nil {
		return err
	}
	// 2. if _TOOL_NAME.fullmatch(name) is None: raise ValueError
	if !toolNamePattern.MatchString(name) {
		return fmt.Errorf("host_tools.name must be an ASCII tool identifier")
	}
	h.Name = name
	// 3. description = _required_text
	desc, err := requiredText("host_tools.description", h.Description, false)
	if err != nil {
		return err
	}
	h.Description = desc
	// 4. schema = _freeze_object("host_tools.input_schema", ...)
	frozen, err := freezeObject("host_tools.input_schema", h.InputSchema)
	if err != nil {
		return err
	}
	// 5. if schema.get("type") != "object": raise ValueError
	if asString(frozen["type"]) != "object" {
		return fmt.Errorf("host_tools.input_schema must describe an object")
	}
	h.InputSchema = frozen
	return nil
}

// validate WorkspaceSpec 的构造期校验。
// Python: model.py:328-339
func (w *WorkspaceSpec) validate() error {
	var err error
	if w.Cwd, err = optionalText("cwd", w.Cwd); err != nil {
		return err
	}
	if w.ProjectDir, err = optionalText("project_dir", w.ProjectDir); err != nil {
		return err
	}
	w.TrustedDirs, err = stringTuple("trusted_dirs", w.TrustedDirs)
	return err
}

// validate OneShotRunInput 的 15 步校验（顺序严格对齐上游）。
// Python: model.py:398-481
func (in *OneShotRunInput) validate() error {
	// 1. schema_version
	if _, err := RequireSupportedSchemaVersion(in.SchemaVersion); err != nil {
		return fmt.Errorf("schema_version is not supported")
	}
	// 2. input = _required_text(preserve=True)
	input, err := requiredText("input", in.Input, true)
	if err != nil {
		return err
	}
	in.Input = input
	// 3. request_id / session_id = _optional_text
	if in.RequestID, err = optionalText("request_id", in.RequestID); err != nil {
		return err
	}
	if in.SessionID, err = optionalText("session_id", in.SessionID); err != nil {
		return err
	}
	// 4. agent 已由调用方校验（from_dict 时）；此处只需非 nil 时重校验
	if in.Agent != nil {
		if err := in.Agent.validate(); err != nil {
			return err
		}
	}
	// 5. model = _optional_text
	if in.Model, err = optionalText("model", in.Model); err != nil {
		return err
	}
	// 6. skills / mcp 非 nil 时 _string_tuple
	if in.Skills != nil {
		if in.Skills, err = stringTuple("skills", in.Skills); err != nil {
			return err
		}
	}
	if in.Mcp != nil {
		if in.Mcp, err = stringTuple("mcp", in.Mcp); err != nil {
			return err
		}
	}
	// 7. permissions 非 nil → 只允许 tools，值为 allow/ask/deny
	if in.Permissions != nil {
		if err := validatePermissions(in.Permissions); err != nil {
			return err
		}
	}
	// 8. output_schema 非 nil → type 必须 object
	if in.OutputSchema != nil {
		frozen, ferr := freezeObject("output_schema", in.OutputSchema)
		if ferr != nil {
			return ferr
		}
		if asString(frozen["type"]) != "object" {
			return fmt.Errorf("output_schema must describe an object")
		}
		in.OutputSchema = frozen
	}
	// 9. max_turns 非 nil → 1..1000
	if in.MaxTurns != nil {
		turns, terr := nonNegativeInt("max_turns", *in.MaxTurns)
		if terr != nil {
			return terr
		}
		if turns < 1 || turns > 1000 {
			return fmt.Errorf("max_turns must be between 1 and 1000")
		}
	}
	// 10. max_budget_usd 非 nil → _positive_number
	if in.MaxBudgetUSD != nil {
		f, ferr := positiveNumber("max_budget_usd", *in.MaxBudgetUSD)
		if ferr != nil {
			return ferr
		}
		in.MaxBudgetUSD = &f
	}
	// 11. host_tools name 唯一
	seen := make(map[string]struct{}, len(in.HostTools))
	for i := range in.HostTools {
		if err := in.HostTools[i].validate(); err != nil {
			return err
		}
		if _, dup := seen[in.HostTools[i].Name]; dup {
			return fmt.Errorf("host_tools names must be unique")
		}
		seen[in.HostTools[i].Name] = struct{}{}
	}
	// 12. mode 非 nil → SingleAgentMode(mode)
	if in.Mode != nil {
		mode := strings.TrimSpace(*in.Mode)
		if !isSingleAgentMode(mode) {
			return fmt.Errorf("unsupported single-Agent mode: %s", mode)
		}
		in.Mode = &mode
	} else if in.SessionID == nil {
		// 13. mode 为 nil 且 session_id 为 nil → 默认 agent.code.normal
		def := string(ModeAgentCodeNormal)
		in.Mode = &def
	}
	// 14. workspace 非 nil 时校验
	if in.Workspace != nil {
		if err := in.Workspace.validate(); err != nil {
			return err
		}
	}
	// 15. timeout_seconds 非 nil → _positive_number
	if in.TimeoutSeconds != nil {
		f, ferr := positiveNumber("timeout_seconds", *in.TimeoutSeconds)
		if ferr != nil {
			return ferr
		}
		in.TimeoutSeconds = &f
	}
	return nil
}

// validate RuntimeErrorInfo。
// Python: model.py:580-588
func (e *RuntimeErrorInfo) validate() error {
	code, err := requiredText("code", e.Code, false)
	if err != nil {
		return err
	}
	e.Code = code
	message, err := requiredText("message", e.Message, true)
	if err != nil {
		return err
	}
	e.Message = message
	if e.Details != nil {
		frozen, ferr := freezeObject("details", e.Details)
		if ferr != nil {
			return ferr
		}
		e.Details = frozen
	}
	return nil
}

// validate OneShotEvent。
// Python: model.py:637-672
func (e *OneShotEvent) validate() error {
	if e.Sequence < 0 {
		return fmt.Errorf("sequence must not be negative")
	}
	requestID, err := requiredText("request_id", e.RequestID, false)
	if err != nil {
		return err
	}
	e.RequestID = requestID
	eventType, err := requiredText("event_type", e.EventType, false)
	if err != nil {
		return err
	}
	e.EventType = eventType
	if e.SessionID, err = optionalText("session_id", e.SessionID); err != nil {
		return err
	}
	if e.Payload != nil {
		frozen, ferr := freezeObject("payload", e.Payload)
		if ferr != nil {
			return ferr
		}
		e.Payload = frozen
		// Python: if payload.event_type is not None:
		//           必须与 top-level event_type 一致
		if raw, ok := e.Payload["event_type"]; ok && raw != nil {
			payloadType, isStr := raw.(string)
			if !isStr {
				return fmt.Errorf("payload.event_type must be a string")
			}
			if payloadType != "" && payloadType != e.EventType {
				return fmt.Errorf("payload.event_type must match the top-level event_type")
			}
		}
	}
	return nil
}

// validate OneShotRunResult 的终态一致性守卫。
// Python: model.py:764-814
func (r *OneShotRunResult) validate() error {
	if _, err := RequireSupportedSchemaVersion(r.SchemaVersion); err != nil {
		return fmt.Errorf("schema_version is not supported")
	}
	if r.Sequence < 0 {
		return fmt.Errorf("sequence must not be negative")
	}
	requestID, err := requiredText("request_id", r.RequestID, false)
	if err != nil {
		return err
	}
	r.RequestID = requestID
	if r.SessionID, err = optionalText("session_id", r.SessionID); err != nil {
		return err
	}
	// status 必须是 RunStatus 之一
	if !isRunStatus(r.Status) {
		return fmt.Errorf("unsupported run status: %s", r.Status)
	}
	if r.ExitCode < 0 {
		return fmt.Errorf("exit_code must not be negative")
	}
	if r.Output != nil && !isString(*r.Output) {
		return fmt.Errorf("output must be a string")
	}
	if r.OutputJSON != nil {
		frozen, ferr := freezeObject("output_json", r.OutputJSON)
		if ferr != nil {
			return ferr
		}
		r.OutputJSON = frozen
	}
	// 终态一致性守卫（Python: model.py:802-813）
	if r.Status == string(StatusCompleted) {
		if r.SessionID == nil {
			return fmt.Errorf("completed result must contain a session_id")
		}
		if r.ExitCode != 0 {
			return fmt.Errorf("completed result must use exit_code 0")
		}
		if r.Error != nil {
			return fmt.Errorf("completed result must not contain an error")
		}
	} else {
		if r.ExitCode == 0 {
			return fmt.Errorf("non-completed result must use a non-zero exit_code")
		}
		if r.Error == nil {
			return fmt.Errorf("non-completed result must contain an error")
		}
	}
	if r.Usage == nil {
		r.Usage = JsonObject{}
	}
	frozen, err := freezeObject("usage", r.Usage)
	if err != nil {
		return err
	}
	r.Usage = frozen
	return nil
}

// validatePermissions 校验 permissions 对象（只允许 tools，值为 allow/ask/deny）。
// Python: model.py:426-441
func validatePermissions(permissions JsonObject) error {
	data, err := strictObject("permissions", permissions,
		map[string]struct{}{"tools": {}}, nil)
	if err != nil {
		return err
	}
	rawTools, ok := data["tools"]
	if !ok {
		rawTools = map[string]any{}
	}
	tools, isMap := rawTools.(map[string]any)
	if !isMap {
		return fmt.Errorf("permissions.tools must be an object")
	}
	for name, level := range tools {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("permissions.tools names must be nonempty strings")
		}
		lv := asString(level)
		if lv != "allow" && lv != "ask" && lv != "deny" {
			return fmt.Errorf("permissions.tools levels must be allow, ask, or deny")
		}
	}
	return nil
}

// freezeJson 冻结实现，携带祖先集合做循环检测。
// Python: model.py:134-172
func freezeJson(name string, value any, ancestors map[uintptr]bool) (any, error) {
	switch v := value.(type) {
	case nil, string, bool, int, int64:
		return v, nil
	case float64:
		// Python: if not math.isfinite(value): raise ValueError
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("%s must contain only finite numbers", name)
		}
		return v, nil
	case map[string]any:
		marker := reflectPointer(v)
		// Python: if marker in active: raise ValueError("must not contain reference cycles")
		if marker != 0 && ancestors[marker] {
			return nil, fmt.Errorf("%s must not contain reference cycles", name)
		}
		if marker != 0 {
			ancestors[marker] = true
			defer delete(ancestors, marker)
		}
		out := make(map[string]any, len(v))
		for k, item := range v {
			frozen, err := freezeJson(name+"."+k, item, ancestors)
			if err != nil {
				return nil, err
			}
			out[k] = frozen
		}
		return out, nil
	case []any:
		marker := reflectPointer(v)
		if marker != 0 && ancestors[marker] {
			return nil, fmt.Errorf("%s must not contain reference cycles", name)
		}
		if marker != 0 {
			ancestors[marker] = true
			defer delete(ancestors, marker)
		}
		out := make([]any, len(v))
		for i, item := range v {
			frozen, err := freezeJson(fmt.Sprintf("%s[%d]", name, i), item, ancestors)
			if err != nil {
				return nil, err
			}
			out[i] = frozen
		}
		return out, nil
	case []string:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = item
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s must contain only JSON-compatible values", name)
	}
}

// freezeObject 冻结并要求结果为对象。
// Python: model.py:175-179 _freeze_object
func freezeObject(name string, value any) (JsonObject, error) {
	frozen, err := freezeJson(name, value, map[uintptr]bool{})
	if err != nil {
		return nil, err
	}
	obj, ok := frozen.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", name)
	}
	return obj, nil
}

// reflectPointer 取 map/slice 的底层指针做循环检测。
func reflectPointer(value any) uintptr {
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Map, reflect.Slice:
		if rv.IsNil() {
			return 0
		}
		return rv.Pointer()
	default:
		return 0
	}
}

// strictObject 严格对象校验（拒绝未知字段、要求必填字段）。
// Python: model.py:112-131 _strict_object
func strictObject(
	name string,
	value any,
	allowed map[string]struct{},
	required map[string]struct{},
) (map[string]any, error) {
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", name)
	}
	unknown := make([]string, 0)
	for key := range obj {
		if _, ok := allowed[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("%s contains unknown fields: %s",
			name, strings.Join(sortedStrings(unknown), ", "))
	}
	missing := make([]string, 0)
	for key := range required {
		if _, ok := obj[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s is missing required fields: %s",
			name, strings.Join(sortedStrings(missing), ", "))
	}
	return obj, nil
}

// requiredText 必填非空文本。
// Python: model.py:56-61 _required_text
func requiredText(name, value string, preserve bool) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s must not be empty", name)
	}
	if preserve {
		return value, nil
	}
	return strings.TrimSpace(value), nil
}

// optionalText 可选文本（nil 放行，非 nil 必须非空）。
// Python: model.py:64-67 _optional_text
func optionalText(name string, value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed, err := requiredText(name, *value, false)
	if err != nil {
		return nil, err
	}
	return &trimmed, nil
}

// stringTuple 字符串序列（拒绝重复）。
// Python: model.py:101-109 _string_tuple
func stringTuple(name string, values []string) ([]string, error) {
	if values == nil {
		return []string{}, nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for i, item := range values {
		v, err := requiredText(fmt.Sprintf("%s[%d]", name, i), item, false)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[v]; dup {
			return nil, fmt.Errorf("%s must not contain duplicates", name)
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out, nil
}

// nonNegativeInt 非负整数。
// Python: model.py:84-89 _non_negative_integer
func nonNegativeInt(name string, value any) (int, error) {
	switch v := value.(type) {
	case bool:
		return 0, fmt.Errorf("%s must be an integer", name)
	case int:
		if v < 0 {
			return 0, fmt.Errorf("%s must not be negative", name)
		}
		return v, nil
	case int64:
		if v < 0 {
			return 0, fmt.Errorf("%s must not be negative", name)
		}
		return int(v), nil
	case float64:
		if v != math.Trunc(v) {
			return 0, fmt.Errorf("%s must be an integer", name)
		}
		if v < 0 {
			return 0, fmt.Errorf("%s must not be negative", name)
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("%s must be an integer", name)
	}
}

// positiveNumber 有限正数。
// Python: model.py:92-98 _positive_number
func positiveNumber(name string, value any) (float64, error) {
	var f float64
	switch v := value.(type) {
	case bool:
		return 0, fmt.Errorf("%s must be a number", name)
	case int:
		f = float64(v)
	case int64:
		f = float64(v)
	case float64:
		f = v
	default:
		return 0, fmt.Errorf("%s must be a number", name)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 {
		return 0, fmt.Errorf("%s must be finite and greater than zero", name)
	}
	return f, nil
}

// anySlice 要求序列类型。
// Python: model.py:101-109（Sequence 判定）
func anySlice(name string, value any) ([]any, error) {
	switch v := value.(type) {
	case []any:
		return v, nil
	case []string:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = item
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s must be a sequence", name)
	}
}

// isSingleAgentMode 是否为规范模式之一。
// Python: SingleAgentMode(mode) 的构造校验
func isSingleAgentMode(mode string) bool {
	switch SingleAgentMode(mode) {
	case ModeAgentWorkNormal, ModeAgentWorkPlan, ModeAgentCodeNormal, ModeAgentCodePlan:
		return true
	}
	return false
}

// isRunStatus 是否为合法终态。
// Python: RunStatus(self.status) 的构造校验
func isRunStatus(status string) bool {
	switch RunStatus(status) {
	case StatusCompleted, StatusFailed, StatusCancelled, StatusTimedOut:
		return true
	}
	return false
}

// asString 安全取字符串。
func asString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// isString 值是否为字符串。
func isString(value any) bool {
	_, ok := value.(string)
	return ok
}

// optionalStringFromAny 可选字符串（nil 或 string）。
func optionalStringFromAny(value any) *string {
	if value == nil {
		return nil
	}
	s, ok := value.(string)
	if !ok {
		return nil
	}
	return &s
}

// optionalStringValue 可选字符串值输出（nil 输出 nil）。
func optionalStringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

// optionalIntValue 可选整数输出（nil 输出 nil）。
func optionalIntValue(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

// optionalFloatValue 可选浮点输出（nil 输出 nil）。
func optionalFloatValue(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

// optionalJsonValue 可选 JSON 输出（nil 输出 nil）。
func optionalJsonValue(value JsonObject) any {
	if value == nil {
		return nil
	}
	return ThawJson(value)
}

// optionalStringSliceValue 可选字符串列表输出（nil 输出 nil）。
func optionalStringSliceValue(value []string) any {
	if value == nil {
		return nil
	}
	return stringSliceAny(value)
}

// stringSliceAny []string → []any。
func stringSliceAny(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// stringSliceFromAny any → []string（拒绝字符串本身）。
func stringSliceFromAny(value any) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return []string{}
}

// jsonObjectFromAny any → JsonObject（深拷贝）。
func jsonObjectFromAny(value any) JsonObject {
	if obj, ok := value.(map[string]any); ok {
		if thawed, ok2 := ThawJson(obj).(map[string]any); ok2 {
			return thawed
		}
	}
	return JsonObject{}
}

// containsString 切片是否含目标值。
func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

// sortedStrings 字符串切片简单排序（保持错误信息确定性）。
func sortedStrings(values []string) []string {
	out := make([]string, len(values))
	copy(out, values)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// UnmarshalJsonObject 把 JSON 字节解析为 JsonObject（保留数字精度语义）。
func UnmarshalJsonObject(raw []byte) (JsonObject, error) {
	var out JsonObject
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
