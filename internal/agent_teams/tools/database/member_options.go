package database

// 团队成员选项（TeamMember.options）的结构化读写。
//
// options 是落库的 JSON blob，承载模型引用、worktree 隔离、权限收窄、
// 退役策略、专家模板等扩展配置。全部字段可缺省；未知键必须原样保留
// （上游 pydantic `extra="allow"`），否则别的模块写进去的键会被本模块
// 的读写循环抹掉。
//
// 对应 Python 代码：openjiuwen/agent_teams/tools/member_options.py
//
// 已知偏差（D-16，见 25.1b spec §7）：上游把该模块与 team.py 并列放在
// agent_teams/tools/ 下，靠 engine.py 里的**函数内延迟 import** 规避与
// database 的循环依赖；Go 无延迟 import，只能靠包划分断环。本模块操作
// 的对象是 TeamMember.options 列，语义归属 database 层，故落在此包。

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemberModelRef 成员持有的模型池引用（轻量）。
// Python: MemberModelRef (member_options.py:17-26)
type MemberModelRef struct {
	// ModelName 模型名
	ModelName *string `json:"model_name,omitempty"`
	// ModelIndex 模型在池中的下标
	ModelIndex *int `json:"model_index,omitempty"`
	// RouteTag 把模型组成员钉到某个模型上的 fallback_tag
	RouteTag *string `json:"route_tag,omitempty"`
}

// MemberBuiltinModel 外部 CLI 成员用自己登录态运行的模型（含 effort）。
// Python: MemberBuiltinModel (member_options.py:29-33)
type MemberBuiltinModel struct {
	// Model 模型名
	Model string `json:"model"`
	// Effort 推理档位
	Effort *string `json:"effort,omitempty"`
}

// MemberServedModel 模型组把成员路由到的某个模型，从 since（毫秒时间戳）起生效。
// Python: MemberServedModel (member_options.py:36-40)
type MemberServedModel struct {
	// Model 模型名
	Model string `json:"model"`
	// Since 生效时刻（毫秒时间戳）
	Since int64 `json:"since"`
}

// MemberWorktreeOptions 成员上的 worktree 隔离选项。
// Python: MemberWorktreeOptions (member_options.py:48-57)
type MemberWorktreeOptions struct {
	// Isolation 隔离模式
	Isolation *string `json:"isolation,omitempty"`
	// Path worktree 路径
	Path *string `json:"path,omitempty"`
	// SessionID 关联会话标识
	SessionID *string `json:"session_id,omitempty"`
	// ProjectHash 项目指纹
	ProjectHash *string `json:"project_hash,omitempty"`
	// ManagedRoot 托管根目录
	ManagedRoot *string `json:"managed_root,omitempty"`
	// WorktreeBranch worktree 分支名
	WorktreeBranch *string `json:"worktree_branch,omitempty"`
	// HeadCommit 基线提交
	HeadCommit *string `json:"head_commit,omitempty"`
}

// MemberAgentTemplate 成员被 spawn 时所依据的专家模板。
//
// 落库是为了让成员只靠这一行就能重建——子进程、冷恢复、包升级或卸载之后
// 都成立。原生成员带 snapshot，外部 CLI 成员带 external_cli。
//
// Python: MemberAgentTemplate (member_options.py:60-77)
type MemberAgentTemplate struct {
	// Ref {"name":..., "group":...}（ExpertRef.to_dict()）
	Ref map[string]any `json:"ref,omitempty"`
	// Version spawn 时的模板版本；未知时为空串
	Version string `json:"version,omitempty"`
	// Snapshot 原生成员的能力快照（不含提示词 section 的 AgentTemplateSpec）
	Snapshot map[string]any `json:"snapshot,omitempty"`
	// ExternalCLI 外部 CLI 成员的合并后 ExternalCliAgentSpec JSON
	ExternalCLI map[string]any `json:"external_cli,omitempty"`
	// Extra 未知键原样保留（对齐 pydantic extra="allow"）
	Extra map[string]any `json:"-"`
}

// TeamMemberOptions 每个成员的可扩展配置 blob，落在 TeamMember.options。
// Python: TeamMemberOptions (member_options.py:80-115)
type TeamMemberOptions struct {
	// ModelRef 当前模型池引用
	ModelRef *MemberModelRef `json:"model_ref,omitempty"`
	// FallbackModelRef 认证兜底用的模型引用
	FallbackModelRef *MemberModelRef `json:"fallback_model_ref,omitempty"`
	// BuiltinModel 外部 CLI 成员自带登录态使用的内置模型
	BuiltinModel *MemberBuiltinModel `json:"builtin_model,omitempty"`
	// ServedModels 模型组路由过的模型，旧的在前，每次换模型记一条（有上限）
	ServedModels []MemberServedModel `json:"served_models,omitempty"`
	// CLIAgent 外部 CLI 后端名
	CLIAgent *string `json:"cli_agent,omitempty"`
	// Worktree worktree 隔离选项
	Worktree *MemberWorktreeOptions `json:"worktree,omitempty"`
	// Lifetime task-scoped 成员的退役策略（空 = 常驻，由 leader 显式退役）
	Lifetime *MemberLifetime `json:"lifetime,omitempty"`
	// PermissionsOverride 成员级权限收窄，扁平 {工具名: 级别}
	PermissionsOverride map[string]string `json:"permissions_override,omitempty"`
	// AgentTemplate spawn 时的专家模板
	AgentTemplate *MemberAgentTemplate `json:"agent_template,omitempty"`
	// Extra 未知键原样保留（对齐 pydantic extra="allow"）
	Extra map[string]any `json:"-"`
}

// MemberOptionsInput BuildMemberOptions 的入参（对齐上游关键字参数）。
// Python: build_member_options 的关键字参数（member_options.py:170-181）
type MemberOptionsInput struct {
	// ModelRef 当前模型池引用（map 形态，类型不符的字段丢弃）
	ModelRef map[string]any
	// FallbackModelRef 认证兜底模型引用
	FallbackModelRef map[string]any
	// BuiltinModel 内置 CLI 模型
	BuiltinModel *MemberBuiltinModel
	// CLIAgent 外部 CLI 后端名
	CLIAgent *string
	// Worktree worktree 选项（优先于下面两个平铺字段）
	Worktree *MemberWorktreeOptions
	// WorktreeIsolation worktree 隔离模式（平铺形态）
	WorktreeIsolation string
	// WorktreePath worktree 路径（平铺形态）
	WorktreePath string
	// PermissionsOverride 权限收窄表
	PermissionsOverride map[string]string
	// Lifetime 退役策略
	Lifetime *MemberLifetime
	// AgentTemplate 专家模板
	AgentTemplate *MemberAgentTemplate
}

// ──────────────────────────── 枚举 ────────────────────────────

// MemberLifetime task-scoped 成员的退役策略。
// Python: MemberLifetime = Literal["shutdown", "dormant"]
type MemberLifetime string

const (
	// MemberLifetimeShutdown 板子上没活儿后彻底关闭
	MemberLifetimeShutdown MemberLifetime = "shutdown"
	// MemberLifetimeDormant 板子上没活儿后转入休眠
	MemberLifetimeDormant MemberLifetime = "dormant"
)

// ──────────────────────────── 常量 ────────────────────────────

// maxServedModels 每个成员保留的 served_models 条数上限：
// 够回答"最近换过哪些模型"，又不会让抖动的路由把行撑爆。
// Python: MAX_SERVED_MODELS (member_options.py:44)
const maxServedModels = 5

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadMemberOptions 解析 TeamMember.options 的 JSON 串。
//
// 空串、非法 JSON、非对象、字段类型不符一律退回空配置（上游吞错语义），
// 不让一条坏数据把整个成员读崩。
//
// Python: load_member_options (member_options.py:117-131)
func LoadMemberOptions(raw string) TeamMemberOptions {
	if raw == "" {
		return TeamMemberOptions{}
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return TeamMemberOptions{}
	}
	if data == nil {
		return TeamMemberOptions{}
	}
	var opts TeamMemberOptions
	// 已知字段类型不符时整体退回空配置，对齐上游 model_validate 抛 ValueError 的分支
	if err := decodeStrict(raw, &opts); err != nil {
		return TeamMemberOptions{}
	}
	opts.Extra = collectUnknownKeys(data, knownOptionKeys)
	// agent_template 段自身也是 extra="allow"：把该段里的未知键收进模板的 Extra，
	// 否则往返一次就会丢掉别的模块写进去的模板扩展字段。
	if opts.AgentTemplate != nil {
		if tplMap, ok := data["agent_template"].(map[string]any); ok {
			opts.AgentTemplate.Extra = collectUnknownKeys(tplMap, knownTemplateKeys)
		}
	}
	return opts
}

// DumpMemberOptions 序列化选项；空配置返回空串（上游返回 None）。
// Python: dump_member_options (member_options.py:134-140)
func DumpMemberOptions(opts TeamMemberOptions) string {
	out := flattenOptions(opts)
	if len(out) == 0 {
		return ""
	}
	// 上游 json.dumps(sort_keys=True)；Go 的 map marshal 天然按键排序，
	// 两侧字节序一致，便于对拍。
	raw, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(raw)
}

// MergeLegacyMemberOptions 构造 options JSON，同时保留 legacy model-ref 列输入。
//
// 存量库里 model_ref 可能只存在于 model_ref_json 列；已有 options 里没写
// model_ref 时用它补齐，写过的则以 options 为准。
//
// Python: merge_legacy_member_options (member_options.py:157-167)
func MergeLegacyMemberOptions(options string, modelRef map[string]any, modelRefJSON string) string {
	parsed := LoadMemberOptions(options)
	if parsed.ModelRef == nil {
		if ref := modelRefFromMapping(modelRef); ref != nil {
			parsed.ModelRef = ref
		} else {
			parsed.ModelRef = modelRefFromJSON(modelRefJSON)
		}
	}
	return DumpMemberOptions(parsed)
}

// BuildMemberOptions 为新的写入构造 TeamMember.options JSON 串。
// Python: build_member_options (member_options.py:170-200)
func BuildMemberOptions(in MemberOptionsInput) string {
	parsed := TeamMemberOptions{
		AgentTemplate: in.AgentTemplate,
		Lifetime:      in.Lifetime,
		ModelRef:      modelRefFromMapping(in.ModelRef),
		FallbackModelRef: modelRefFromMapping(
			in.FallbackModelRef,
		),
		BuiltinModel: in.BuiltinModel,
		CLIAgent:     in.CLIAgent,
	}
	if in.Worktree != nil {
		parsed.Worktree = in.Worktree
	} else if in.WorktreeIsolation != "" || in.WorktreePath != "" {
		parsed.Worktree = &MemberWorktreeOptions{
			Isolation: strPtrOrNil(in.WorktreeIsolation),
			Path:      strPtrOrNil(in.WorktreePath),
		}
	}
	if len(in.PermissionsOverride) > 0 {
		parsed.PermissionsOverride = in.PermissionsOverride
	}
	return DumpMemberOptions(parsed)
}

// SetMemberWorktreeOptions 替换 options 里的 worktree 段。
// Python: set_member_worktree_options (member_options.py:203-217)
func SetMemberWorktreeOptions(raw string, worktree *MemberWorktreeOptions, isolation, worktreePath string) string {
	parsed := LoadMemberOptions(raw)
	switch {
	case worktree != nil:
		parsed.Worktree = worktree
	case isolation != "" || worktreePath != "":
		parsed.Worktree = &MemberWorktreeOptions{
			Isolation: strPtrOrNil(isolation),
			Path:      strPtrOrNil(worktreePath),
		}
	default:
		parsed.Worktree = nil
	}
	return DumpMemberOptions(parsed)
}

// SetMemberBuiltinModel 替换 options 里的 builtin_model 段。
// Python: set_member_builtin_model (member_options.py:220-225)
func SetMemberBuiltinModel(raw string, builtin *MemberBuiltinModel) string {
	parsed := LoadMemberOptions(raw)
	parsed.BuiltinModel = builtin
	return DumpMemberOptions(parsed)
}

// AppendMemberServedModel 记录模型组把成员路由到 model，从 since 起生效。
//
// 与最新一条相同不算变更，原样返回；历史只保留最新 maxServedModels 条。
//
// Python: append_member_served_model (member_options.py:228-242)
func AppendMemberServedModel(raw, model string, since int64) string {
	parsed := LoadMemberOptions(raw)
	history := parsed.ServedModels
	if n := len(history); n > 0 && history[n-1].Model == model {
		return raw
	}
	history = append(history, MemberServedModel{Model: model, Since: since})
	if len(history) > maxServedModels {
		history = history[len(history)-maxServedModels:]
	}
	parsed.ServedModels = history
	return DumpMemberOptions(parsed)
}

// SetMemberPermissionsOverride 替换 options 里的 permissions_override 段。
// Python: set_member_permissions_override (member_options.py:245-253)
func SetMemberPermissionsOverride(raw string, override map[string]string) string {
	parsed := LoadMemberOptions(raw)
	parsed.PermissionsOverride = override
	return DumpMemberOptions(parsed)
}

// PromoteMemberFallbackModel 把成员的兜底模型提为当前模型引用。
//
// 兜底是外部端点，因此为 CLI 自身登录态选的内置模型不再适用，一并清空。
//
// Python: promote_member_fallback_model (member_options.py:275-287)
func PromoteMemberFallbackModel(raw string) string {
	parsed := LoadMemberOptions(raw)
	if parsed.FallbackModelRef == nil {
		return raw
	}
	promoted := *parsed.FallbackModelRef
	parsed.ModelRef = &promoted
	parsed.BuiltinModel = nil
	return DumpMemberOptions(parsed)
}

// GetMemberOptions 从 DB 行（*TeamMember 或 map）读出选项。
// Python: get_member_options (member_options.py:256-258)
func GetMemberOptions(record any) TeamMemberOptions {
	return LoadMemberOptions(recordValueString(record, "options"))
}

// GetMemberModelRef 返回成员当前的模型池引用。
// Python: get_member_model_ref (member_options.py:261-263)
func GetMemberModelRef(record any) *MemberModelRef {
	return GetMemberOptions(record).ModelRef
}

// GetMemberFallbackModelRef 返回成员的认证兜底模型引用。
// Python: get_member_fallback_model_ref (member_options.py:266-268)
func GetMemberFallbackModelRef(record any) *MemberModelRef {
	return GetMemberOptions(record).FallbackModelRef
}

// GetMemberBuiltinModel 返回成员选用的内置 CLI 模型。
// Python: get_member_builtin_model (member_options.py:290-292)
func GetMemberBuiltinModel(record any) *MemberBuiltinModel {
	return GetMemberOptions(record).BuiltinModel
}

// GetMemberServedModels 返回模型组路由过的模型，旧的在前。
// Python: get_member_served_models (member_options.py:295-297)
func GetMemberServedModels(record any) []MemberServedModel {
	return GetMemberOptions(record).ServedModels
}

// GetMemberCLIAgent 返回外部 CLI 后端名。
// Python: get_member_cli_agent (member_options.py:300-302)
func GetMemberCLIAgent(record any) *string {
	return GetMemberOptions(record).CLIAgent
}

// GetMemberWorktree 返回成员的 worktree 选项。
// Python: get_member_worktree (member_options.py:305-307)
func GetMemberWorktree(record any) *MemberWorktreeOptions {
	return GetMemberOptions(record).Worktree
}

// GetMemberPermissionsOverride 返回成员的权限收窄表。
//
// 扁平 {工具名: 级别}，例如 {"bash": "deny", "write_file": "ask"}；
// spawn 时没指定则为 nil。
//
// Python: get_member_permissions_override (member_options.py:310-318)
func GetMemberPermissionsOverride(record any) map[string]string {
	return GetMemberOptions(record).PermissionsOverride
}

// GetMemberAgentTemplate 返回成员被 spawn 时依据的专家模板。
// Python: get_member_agent_template (member_options.py:326-328)
func GetMemberAgentTemplate(record any) *MemberAgentTemplate {
	return GetMemberOptions(record).AgentTemplate
}

// GetMemberLifetime 返回成员的退役策略（nil = 常驻）。
// Python: get_member_lifetime (member_options.py:326-328)
func GetMemberLifetime(record any) *MemberLifetime {
	return GetMemberOptions(record).Lifetime
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// knownOptionKeys TeamMemberOptions 的已知 JSON 键，用于收集未知键。
var knownOptionKeys = map[string]struct{}{
	"model_ref":            {},
	"fallback_model_ref":   {},
	"builtin_model":        {},
	"served_models":        {},
	"cli_agent":            {},
	"worktree":             {},
	"lifetime":             {},
	"permissions_override": {},
	"agent_template":       {},
}

// knownTemplateKeys MemberAgentTemplate 的已知 JSON 键。
var knownTemplateKeys = map[string]struct{}{
	"ref":          {},
	"version":      {},
	"snapshot":     {},
	"external_cli": {},
}

// decodeStrict 严格解码：已知字段类型不符即返回错误（对齐上游 model_validate）。
func decodeStrict(raw string, out *TeamMemberOptions) error {
	dec := json.NewDecoder(strings.NewReader(raw))
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode member options: %w", err)
	}
	return nil
}

// flattenOptions 把结构体与 Extra 合成一个 map（Extra 在前，已知字段覆盖）。
//
// agent_template 段自身也是 extra="allow"，需单独把模板的未知键并回该段。
func flattenOptions(opts TeamMemberOptions) map[string]any {
	out := make(map[string]any, len(opts.Extra)+len(knownOptionKeys))
	for k, v := range opts.Extra {
		out[k] = v
	}
	raw, err := json.Marshal(structWithoutExtra(opts))
	if err != nil {
		return out
	}
	var known map[string]any
	if err := json.Unmarshal(raw, &known); err != nil {
		return out
	}
	for k, v := range known {
		out[k] = v
	}
	// 模板段的未知键并回该段
	if opts.AgentTemplate != nil && len(opts.AgentTemplate.Extra) > 0 {
		tpl, ok := out["agent_template"].(map[string]any)
		if !ok {
			tpl = make(map[string]any, len(opts.AgentTemplate.Extra))
			out["agent_template"] = tpl
		}
		for k, v := range opts.AgentTemplate.Extra {
			tpl[k] = v
		}
	}
	return out
}

// structWithoutExtra 返回一个 Extra 被清空的副本，便于只序列化已知字段。
func structWithoutExtra(opts TeamMemberOptions) TeamMemberOptions {
	opts.Extra = nil
	if opts.AgentTemplate != nil {
		tpl := *opts.AgentTemplate
		tpl.Extra = nil
		opts.AgentTemplate = &tpl
	}
	return opts
}

// collectUnknownKeys 收集 data 中不在 known 里的键。
func collectUnknownKeys(data map[string]any, known map[string]struct{}) map[string]any {
	var extra map[string]any
	for k, v := range data {
		if _, ok := known[k]; ok {
			continue
		}
		if extra == nil {
			extra = make(map[string]any)
		}
		extra[k] = v
	}
	return extra
}

// modelRefFromMapping 从 map 构造模型引用（类型不符的字段丢弃）。
// Python: _model_ref_from_mapping (member_options.py:143-154)
func modelRefFromMapping(value map[string]any) *MemberModelRef {
	if len(value) == 0 {
		return nil
	}
	ref := &MemberModelRef{}
	if v, ok := value["model_name"].(string); ok {
		ref.ModelName = &v
	}
	if v, ok := value["model_index"].(int); ok {
		ref.ModelIndex = &v
	}
	if v, ok := value["route_tag"].(string); ok {
		ref.RouteTag = &v
	}
	return ref
}

// modelRefFromJSON 从 model_ref_json 串构造模型引用。
// Python: _model_ref_from_json (member_options.py:157-167)
func modelRefFromJSON(raw string) *MemberModelRef {
	if raw == "" {
		return nil
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil
	}
	return modelRefFromMapping(data)
}

// recordValueString 从 *TeamMember / map[string]any 里取字符串字段。
// Python: _record_value (member_options.py:321-324)
func recordValueString(record any, key string) string {
	if record == nil {
		return ""
	}
	if m, ok := record.(map[string]any); ok {
		v, _ := m[key].(string)
		return v
	}
	rv := reflect.ValueOf(record)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return ""
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return ""
	}
	field := rv.FieldByName(capitalizeFirst(key))
	if !field.IsValid() || field.Kind() != reflect.String {
		return ""
	}
	return field.String()
}

// capitalizeFirst 把首字母大写（recordValueString 的字段名映射用）。
func capitalizeFirst(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// strPtrOrNil 空串转 nil，便于 omitempty 生效。
func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
