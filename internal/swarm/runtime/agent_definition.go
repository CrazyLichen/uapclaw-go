package runtime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"sort"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RuntimeAgentDefinition 共享 Runtime 所拥有的一份 root Agent 定义（已校验）。
// Python: agent_definition.py:151-238 RuntimeAgentDefinition
//
// Tools 为 "*" 表示沿用 Runtime 已配置的工具集；显式白名单在 rail 启动后
// 于 Agent 能力边界强制。
type RuntimeAgentDefinition struct {
	// Name 名称（去空白后须匹配 [A-Za-z0-9_-]{3,50}）
	Name string
	// Instructions 指令（校验非空，值原样保留）
	Instructions string
	// Description 可选描述
	Description *string
	// Model 可选模型标识
	Model *string
	// ToolsWildcard 是否使用 "*"（Python tools="*"）
	ToolsWildcard bool
	// Tools 显式工具名列表（ToolsWildcard 为真时忽略）
	Tools []string
	// Skills 技能名列表（不得重复）
	Skills []string
	// MaxIterations 可选最大迭代次数（须为正整数）
	MaxIterations *int
}

// RuntimeAgentExecution 一次受支持的自定义 root-Agent 执行handoff（已校验）。
// Python: agent_definition.py:256-287 RuntimeAgentExecution
//
// 这是能力契约，不是第二个执行器：AgentRuntime 把它翻译成既有的
// AgentManager 与 Agent Adapter 输入。
type RuntimeAgentExecution struct {
	// Definition 已校验的定义
	Definition RuntimeAgentDefinition
	// Mode canonical 模式
	Mode string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// agentNamePattern 合法 agent 名（上游用 \Z 锚定，Go 用 \z 等价）
	// Python: agent_definition.py:21 _AGENT_NAME_PATTERN
	agentNamePattern = `^[A-Za-z0-9_-]{3,50}$`

	// AgentDefinitionSchema fingerprint 的 schema 版本标记
	// Python: agent_definition.py:22 _DEFINITION_SCHEMA
	AgentDefinitionSchema = "jiuwenswarm.runtime.agent-definition.v1"

	// RuntimeAgentModeWorkNormal agent-work 常态模式
	// Python: agent_definition.py:70 RuntimeAgentMode.WORK_NORMAL
	RuntimeAgentModeWorkNormal = "agent.work.normal"
	// RuntimeAgentModeWorkPlan agent-work 规划模式
	// Python: agent_definition.py:71 RuntimeAgentMode.WORK_PLAN
	RuntimeAgentModeWorkPlan = "agent.work.plan"
	// RuntimeAgentModeCodeNormal agent-code 常态模式
	// Python: agent_definition.py:72 RuntimeAgentMode.CODE_NORMAL
	RuntimeAgentModeCodeNormal = "agent.code.normal"
	// RuntimeAgentModeCodePlan agent-code 规划模式
	// Python: agent_definition.py:73 RuntimeAgentMode.CODE_PLAN
	RuntimeAgentModeCodePlan = "agent.code.plan"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrAgentDefinitionInvalidDefinition 定义本身非法
	// Python: :40 RuntimeAgentDefinitionErrorCode.INVALID_DEFINITION
	ErrAgentDefinitionInvalidDefinition = errors.New("AGENT_DEFINITION_INVALID")
	// ErrAgentDefinitionInvalidRequest 请求形态非法
	// Python: :41 INVALID_REQUEST
	ErrAgentDefinitionInvalidRequest = errors.New("AGENT_DEFINITION_REQUEST_INVALID")
	// ErrAgentDefinitionInvalidMode 模式不合法
	// Python: :42 INVALID_MODE
	ErrAgentDefinitionInvalidMode = errors.New("AGENT_DEFINITION_MODE_INVALID")
	// ErrAgentDefinitionUnsupportedMode 模式合法但不受支持
	// Python: :43 UNSUPPORTED_MODE
	ErrAgentDefinitionUnsupportedMode = errors.New("AGENT_DEFINITION_MODE_UNSUPPORTED")
	// ErrAgentDefinitionModelNotFound 配置的模型不存在
	// Python: :44 MODEL_NOT_FOUND
	ErrAgentDefinitionModelNotFound = errors.New("AGENT_DEFINITION_MODEL_NOT_FOUND")
	// ErrAgentDefinitionToolAllowlistUnsupported 工具白名单不被支持
	// Python: :45 TOOL_ALLOWLIST_UNSUPPORTED
	ErrAgentDefinitionToolAllowlistUnsupported = errors.New("AGENT_DEFINITION_TOOL_ALLOWLIST_UNSUPPORTED")
	// ErrAgentExecutionSessionConflict 会话已绑定另一个定义
	// Python: :46 SESSION_CONFLICT
	ErrAgentExecutionSessionConflict = errors.New("AGENT_DEFINITION_SESSION_CONFLICT")
)

// agentNameRegexp 编译后的名称校验正则。
// Python: agent_definition.py:21 _AGENT_NAME_PATTERN = re.compile(r"[A-Za-z0-9_-]{3,50}\Z")
var agentNameRegexp = regexp.MustCompile(agentNamePattern)

// agentDefinitionFields 定义允许的键集合。
// Python: agent_definition.py:23-33 _DEFINITION_FIELDS
var agentDefinitionFields = map[string]struct{}{
	"name": {}, "instructions": {}, "description": {},
	"model": {}, "tools": {}, "skills": {}, "max_iterations": {},
}

// agentDefinitionRequiredFields 必填键集合。
// Python: agent_definition.py:34 _REQUIRED_DEFINITION_FIELDS
var agentDefinitionRequiredFields = map[string]struct{}{
	"name": {}, "instructions": {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRuntimeAgentDefinition 校验并归一化一份定义。
// Python: agent_definition.py:167-194 __post_init__
func NewRuntimeAgentDefinition(in RuntimeAgentDefinition) (RuntimeAgentDefinition, error) {
	out := in

	// Python:168-171 name 先去空白校验非空，再匹配模式，最后写回去空白值
	name, err := requiredText("name", in.Name, false)
	if err != nil {
		return RuntimeAgentDefinition{}, err
	}
	if !agentNameRegexp.MatchString(name) {
		return RuntimeAgentDefinition{}, invalidField("name", "name must match [A-Za-z0-9_-]{3,50}")
	}
	out.Name = name

	// Python:172-176 instructions 校验非空但保留原值（preserve=True）
	instructions, err := requiredText("instructions", in.Instructions, true)
	if err != nil {
		return RuntimeAgentDefinition{}, err
	}
	out.Instructions = instructions

	// Python:177-181 description 可选文本
	if out.Description, err = optionalText("description", in.Description); err != nil {
		return RuntimeAgentDefinition{}, err
	}
	// Python:182 model 可选文本
	if out.Model, err = optionalText("model", in.Model); err != nil {
		return RuntimeAgentDefinition{}, err
	}

	// Python:183 tools 归一
	tools, wildcard, err := configuredTools(in.ToolsWildcard, in.Tools)
	if err != nil {
		return RuntimeAgentDefinition{}, err
	}
	out.Tools, out.ToolsWildcard = tools, wildcard

	// Python:184 skills 归一（非空且不重复）
	if out.Skills, err = stringTuple("skills", in.Skills); err != nil {
		return RuntimeAgentDefinition{}, err
	}

	// Python:185-194 max_iterations 须为 > 0 的整数
	if in.MaxIterations != nil && *in.MaxIterations <= 0 {
		return RuntimeAgentDefinition{}, invalidField("max_iterations",
			"max_iterations must be greater than zero")
	}
	return out, nil
}

// RuntimeAgentDefinitionFromMapping 解析一份严格的、传输无关的定义映射。
// Python: agent_definition.py:196-209 from_mapping + :130-148 _strict_definition_mapping
//
// 注意：本函数拒绝未知键，与项目内 pyjson 的"未知字段保留"策略相反。
func RuntimeAgentDefinitionFromMapping(value any) (RuntimeAgentDefinition, error) {
	raw, err := strictDefinitionMapping(value)
	if err != nil {
		return RuntimeAgentDefinition{}, err
	}

	out := RuntimeAgentDefinition{}
	// Python:202 name=data["name"]
	nameValue, ok := raw["name"]
	if !ok {
		nameValue = ""
	}
	if nameValueString, ok := nameValue.(string); ok {
		out.Name = nameValueString
	} else {
		return RuntimeAgentDefinition{}, invalidField("name", "name must be a string")
	}
	// Python:203 instructions=data["instructions"]
	instructionsValue, ok := raw["instructions"]
	if !ok {
		instructionsValue = ""
	}
	if instructionsString, ok := instructionsValue.(string); ok {
		out.Instructions = instructionsString
	} else {
		return RuntimeAgentDefinition{}, invalidField("instructions", "instructions must be a string")
	}

	// Python:204 description=data.get("description")
	if out.Description, err = optionalTextFromAny("description", raw["description"]); err != nil {
		return RuntimeAgentDefinition{}, err
	}
	// Python:205 model=data.get("model")
	if out.Model, err = optionalTextFromAny("model", raw["model"]); err != nil {
		return RuntimeAgentDefinition{}, err
	}
	// Python:206 tools=data.get("tools", "*")
	out.Tools, out.ToolsWildcard, err = configuredToolsFromAny(raw["tools"])
	if err != nil {
		return RuntimeAgentDefinition{}, err
	}
	// Python:207 skills=data.get("skills", ())
	if out.Skills, err = stringTupleFromAny("skills", raw["skills"]); err != nil {
		return RuntimeAgentDefinition{}, err
	}
	// Python:208 max_iterations=data.get("max_iterations")
	if out.MaxIterations, err = optionalInt("max_iterations", raw["max_iterations"]); err != nil {
		return RuntimeAgentDefinition{}, err
	}
	return NewRuntimeAgentDefinition(out)
}

// RuntimeAgentDefinitionToDict 返回规范的 JSON 兼容表示。
// Python: agent_definition.py:211-222 to_dict
func RuntimeAgentDefinitionToDict(d RuntimeAgentDefinition) map[string]any {
	var tools any
	if d.ToolsWildcard {
		tools = "*"
	} else {
		tools = append([]string{}, d.Tools...)
	}
	skills := d.Skills
	if skills == nil {
		skills = []string{}
	}
	return map[string]any{
		"name":           d.Name,
		"instructions":   d.Instructions,
		"description":    d.Description,
		"model":          d.Model,
		"tools":          tools,
		"skills":         append([]string{}, skills...),
		"max_iterations": d.MaxIterations,
	}
}

// RuntimeAgentDefinitionFingerprint 返回规范的 SHA-256 指纹。
// Python: agent_definition.py:224-238 fingerprint
//
// 逐字节等价三要素：sort_keys（map 交给 json.Marshal 自动有序）、
// separators=(",",":")（json.Marshal 无空格）、ensure_ascii=False
// （SetEscapeHTML(false)）。json.Encoder 会追加 '\n'，必须裁掉。
func RuntimeAgentDefinitionFingerprint(d RuntimeAgentDefinition) (string, error) {
	payload := map[string]any{
		"schema":     AgentDefinitionSchema,
		"definition": RuntimeAgentDefinitionToDict(d),
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return "", err
	}
	canonical := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// PrepareAgentExecution 校验并描述一次当前受支持的自定义 Agent 执行。
// Python: agent_definition.py:289-301 prepare_agent_execution
func PrepareAgentExecution(definition any, mode string) (*RuntimeAgentExecution, error) {
	var parsed RuntimeAgentDefinition
	switch value := definition.(type) {
	case RuntimeAgentDefinition:
		// Python:296-300 isinstance 分支：已是定义对象则直接使用（不再重复校验）
		parsed = value
	case map[string]any:
		mapped, err := RuntimeAgentDefinitionFromMapping(value)
		if err != nil {
			return nil, err
		}
		parsed = mapped
	default:
		mapped, err := RuntimeAgentDefinitionFromMapping(definition)
		if err != nil {
			return nil, err
		}
		parsed = mapped
	}
	// Python:301 RuntimeAgentExecution(definition=parsed, mode=mode)
	return NewRuntimeAgentExecution(parsed, mode)
}

// NewRuntimeAgentExecution 构造执行体并校验模式。
// Python: agent_definition.py:267-271 RuntimeAgentExecution.__post_init__
func NewRuntimeAgentExecution(def RuntimeAgentDefinition, mode string) (*RuntimeAgentExecution, error) {
	// Python:268-269 定义类型校验（Go 静态类型保证，此处仅保留语义注释）
	// Python:270 mode = _runtime_agent_mode(self.mode)
	canonical, err := runtimeAgentModeValue(mode)
	if err != nil {
		return nil, err
	}
	return &RuntimeAgentExecution{Definition: def, Mode: canonical}, nil
}

// RuntimeAgentExecutionFingerprint 返回执行体所用定义的指纹。
// Python: agent_definition.py:273-277 fingerprint
func RuntimeAgentExecutionFingerprint(e *RuntimeAgentExecution) (string, error) {
	return RuntimeAgentDefinitionFingerprint(e.Definition)
}

// RuntimeAgentExecutionToDict 返回 JSON 兼容的执行描述。
// Python: agent_definition.py:279-286 to_dict
func RuntimeAgentExecutionToDict(e *RuntimeAgentExecution) (map[string]any, error) {
	mode, err := runtimeAgentModeValue(e.Mode)
	if err != nil {
		return nil, err
	}
	fingerprint, err := RuntimeAgentExecutionFingerprint(e)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"agent":           RuntimeAgentDefinitionToDict(e.Definition),
		"agent_fingerprint": fingerprint,
		"mode":            mode,
	}, nil
}

// AgentDefinitionErrorField 取错误的 field（非本类型返回空串）。
// Python: agent_definition.py:54-64 self.field
func AgentDefinitionErrorField(err error) string {
	var target *agentDefinitionError
	if errors.As(err, &target) {
		if target.field == nil {
			return ""
		}
		return *target.field
	}
	return ""
}

// AgentDefinitionErrorCode 取错误的 code（非本类型返回空串）。
// Python: agent_definition.py:63 self.code = code.value
func AgentDefinitionErrorCode(err error) string {
	var target *agentDefinitionError
	if errors.As(err, &target) {
		return target.sentinel.Error()
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// agentDefinitionError 带 code + field + message 的定义错误。
// Python: agent_definition.py:49-64 RuntimeAgentDefinitionError
type agentDefinitionError struct {
	// sentinel 稳定错误码
	sentinel error
	// field 出错字段（上游可为 None）
	field *string
	// message 人类可读说明
	message string
}

// Error 实现 error 接口。
func (e *agentDefinitionError) Error() string {
	if e.field == nil {
		return e.message
	}
	return *e.field + ": " + e.message
}

// Unwrap 支持 errors.Is 判定稳定错误码。
func (e *agentDefinitionError) Unwrap() error { return e.sentinel }

// invalid 构造 INVALID_DEFINITION 错误。
// Python: agent_definition.py:76-81 _invalid
func invalid(field *string, message string) error {
	return &agentDefinitionError{
		sentinel: ErrAgentDefinitionInvalidDefinition,
		field:    field,
		message:  message,
	}
}

// invalidField 构造带字段名的 INVALID_DEFINITION 错误。
func invalidField(field, message string) error {
	return invalid(&field, message)
}

// invalidNoField 构造不带字段名的 INVALID_DEFINITION 错误（上游 field=None）。
func invalidNoField(message string) error {
	return invalid(nil, message)
}

// requiredText 必填文本校验。
// Python: agent_definition.py:84-94 _required_text
func requiredText(field, value string, preserve bool) (string, error) {
	if !preserve && strings.TrimSpace(value) == "" {
		return "", invalidField(field, field+" must not be empty")
	}
	if preserve && strings.TrimSpace(value) == "" {
		return "", invalidField(field, field+" must not be empty")
	}
	if preserve {
		return value, nil
	}
	return strings.TrimSpace(value), nil
}

// optionalText 可选文本，nil 视为未提供。
// Python: agent_definition.py:97-100 _optional_text
func optionalText(field string, value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed, err := requiredText(field, *value, false)
	if err != nil {
		return nil, err
	}
	return &trimmed, nil
}

// optionalTextFromAny 可选文本（从 any 解出），nil/缺省视为未提供。
// Python: agent_definition.py:97-100 _optional_text
func optionalTextFromAny(field string, value any) (*string, error) {
	if value == nil {
		return nil, nil
	}
	s, ok := value.(string)
	if !ok {
		return nil, invalidField(field, field+" must be a string")
	}
	return optionalText(field, &s)
}

// stringTuple 字符串序列归一：逐项非空且不得重复。
// Python: agent_definition.py:103-111 _string_tuple
func stringTuple(field string, value []string) ([]string, error) {
	out := make([]string, 0, len(value))
	seen := make(map[string]struct{}, len(value))
	for index, item := range value {
		itemField := field + "[" + strconv.Itoa(index) + "]"
		trimmed, err := requiredText(itemField, item, false)
		if err != nil {
			// 上游用 field[index] 命名，但错误仍归到序列字段上
			return nil, invalidField(field, field+" must be a sequence of strings")
		}
		if _, duplicated := seen[trimmed]; duplicated {
			return nil, invalidField(field, field+" must not contain duplicates")
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out, nil
}

// stringTupleFromAny 从 any 解出字符串序列。
// Python: agent_definition.py:103-111 _string_tuple
func stringTupleFromAny(field string, value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	list, ok := value.([]any)
	if !ok {
		if direct, ok := value.([]string); ok {
			return stringTuple(field, direct)
		}
		return nil, invalidField(field, field+" must be a sequence of strings")
	}
	converted := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil, invalidField(field, field+" must be a sequence of strings")
		}
		converted = append(converted, s)
	}
	return stringTuple(field, converted)
}

// configuredTools tools 归一。
// Python: agent_definition.py:114-127 _configured_tools
func configuredTools(wildcard bool, tools []string) ([]string, bool, error) {
	if wildcard {
		return nil, true, nil
	}
	normalized, err := stringTuple("tools", tools)
	if err != nil {
		return nil, false, err
	}
	// Python:119-120 tools == ("*",) → "*"
	if len(normalized) == 1 && normalized[0] == "*" {
		return nil, true, nil
	}
	// Python:121-122 "*" 不可与其他工具名混用
	for _, name := range normalized {
		if name == "*" {
			return nil, false, invalidField("tools", "'*' cannot be combined with tool names")
		}
	}
	return normalized, false, nil
}

// configuredToolsFromAny 从 any 解出 tools。
// Python: agent_definition.py:114-127 _configured_tools
func configuredToolsFromAny(value any) ([]string, bool, error) {
	if value == nil {
		// Python:206 data.get("tools", "*")
		return nil, true, nil
	}
	if s, ok := value.(string); ok {
		if s == "*" {
			return nil, true, nil
		}
		// Python:124-126 单字符串非 "*" → 报错
		if _, err := requiredText("tools", s, false); err != nil {
			return nil, false, err
		}
		return nil, false, invalidField("tools", "tools must be '*' or a sequence of strings")
	}
	tools, err := stringTupleFromAny("tools", value)
	if err != nil {
		return nil, false, err
	}
	return configuredTools(false, tools)
}

// optionalInt 可选整数（拒绝浮点与小数字符串）。
// Python: agent_definition.py:185-189 max_iterations 必须为 int（bool 除外）
func optionalInt(field string, value any) (*int, error) {
	if value == nil {
		return nil, nil
	}
	switch v := value.(type) {
	case int:
		return &v, nil
	case int64:
		n := int(v)
		return &n, nil
	case float64:
		// JSON 解出的数字是 float64；非整数报错
		n := int(v)
		if float64(n) != v {
			return nil, invalidField(field, field+" must be an integer")
		}
		return &n, nil
	case bool:
		return nil, invalidField(field, field+" must be an integer")
	}
	return nil, invalidField(field, field+" must be an integer")
}

// strictDefinitionMapping 严格解析定义映射。
// Python: agent_definition.py:130-148 _strict_definition_mapping
func strictDefinitionMapping(value any) (map[string]any, error) {
	if value == nil {
		return nil, invalidNoField("agent definition must be an object")
	}
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, invalidNoField("agent definition must be an object")
	}

	// Python:136-141 未知键（field=None）
	unknown := make([]string, 0)
	for key := range raw {
		if _, known := agentDefinitionFields[key]; !known {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, invalidNoField(
			"agent definition contains unknown fields: " + strings.Join(unknown, ", "))
	}

	// Python:142-147 缺必填键（field=None）
	missing := make([]string, 0)
	for key := range agentDefinitionRequiredFields {
		if _, ok := raw[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, invalidNoField(
			"agent definition is missing required fields: " + strings.Join(missing, ", "))
	}
	return raw, nil
}

// runtimeAgentModeValue 解析 canonical 模式值。
// Python: agent_definition.py:241-253 _runtime_agent_mode
func runtimeAgentModeValue(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	switch trimmed {
	case RuntimeAgentModeWorkNormal, RuntimeAgentModeWorkPlan,
		RuntimeAgentModeCodeNormal, RuntimeAgentModeCodePlan:
		return trimmed, nil
	}
	return "", &agentDefinitionError{
		sentinel: ErrAgentDefinitionInvalidMode,
		field:    strPtr("mode"),
		message:  "unsupported single-Agent mode",
	}
}

// strPtr 取字符串指针。
func strPtr(value string) *string { return &value }
