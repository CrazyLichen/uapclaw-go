package providers

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/workspace"
	buildctx "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/buildctx"
	sysop "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/sys_operation"

	agentteams "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	workspacepkg "github.com/uapclaw/uapclaw-go/internal/common/workspace"
	teamtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 枚举 ────────────────────────────

// providersError providers 包内共享错误类型。
type providersError string

func (e providersError) Error() string { return string(e) }

// ──────────────────────────── 常量 ────────────────────────────

const (
	// errRuntimePromptInvariant projectless 分支内部不变量违反
	// （task_workspace_root 已设而 resolve_member_work_dir 返回空）。
	// Python: RuntimeError（member_rails.py:365-369，显式 raise 保全 -O 语义）
	errRuntimePromptInvariant = providersError("resolve_member_work_dir() returned None despite task_workspace_root being set")
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// isCronExecutionSession 定时任务执行会话判定。
//
// 覆盖两种 id 形态：`cron_*` 约定（主动 tick 会话 cron_<job_id> 与旧版团队
// 每次运行 cron_<ts>_<job_id>）与 `__cron___<ts>_<rand>`（CronScheduler
// 经内部 __cron__ 渠道分配）。cron 运行没有对端操作者，交互式中断无法
// 作答，不得挂载。
// Python: is_cron_execution_session (common/cron_session.py:8-17)
func isCronExecutionSession(sessionID string) bool {
	sid := strings.TrimSpace(sessionID)
	return strings.HasPrefix(sid, "cron") || strings.HasPrefix(sid, "__cron__")
}

// heartbeatUnsupportedChannels 无心跳支持渠道集合。
// Python: HEARTBEAT_UNSUPPORTED_CHANNELS (heartbeat_policy.py:10-12)
var heartbeatUnsupportedChannels = map[string]struct{}{"xiaoyi": {}}

// isHeartbeatSupported 渠道是否支持心跳任务；渠道未知保持原行为（支持）。
// Python: is_heartbeat_supported (heartbeat_policy.py:15-20)
func isHeartbeatSupported(channelID string) bool {
	channel := strings.ToLower(strings.TrimSpace(channelID))
	if channel == "" {
		return true
	}
	head := channel
	if idx := strings.Index(channel, ":"); idx >= 0 {
		head = channel[:idx]
	}
	_, unsupported := heartbeatUnsupportedChannels[head]
	return !unsupported
}

// isSkillRetrievalEnabled Skill 检索开关（config.skill_retrieval.enabled）。
// Python: is_skill_retrieval_enabled
// (common/tools/skill_retrieval_toolkits.py)——基座文件 ⤴️16.4（skill 侧随 discovery 后移）；
// 开关键位与 config_specs.go 既有 retrievalEnabled 读取逻辑同源。
func isSkillRetrievalEnabled(config map[string]any) bool {
	if config == nil {
		return false
	}
	retrieval, _ := config["skill_retrieval"].(map[string]any)
	enabled, _ := retrieval["enabled"].(bool)
	return enabled
}

// trimSpaceOrEmpty 去空白（空串安全）。
func trimSpaceOrEmpty(s string) string {
	return strings.TrimSpace(s)
}

// stringFieldOrEmpty 串字段，空则取默认（对齐 context_field default）。
func stringFieldOrEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// boolParamOr 布尔参数，缺失取默认（对齐 param_field default）。
func boolParamOr(params map[string]any, key string, fallback bool) bool {
	if v, ok := params[key].(bool); ok {
		return v
	}
	return fallback
}

// orEmpty 首个非空串（对齐 Python `a or b` 的串语义）。
func orEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// copyMapOrEmpty 元数据拷贝（nil 安全）。
func copyMapOrEmpty(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// mapKeyTruthy 元数据键真值判定（非零值即真，对齐 Python truthiness）。
func mapKeyTruthy(m map[string]any, key string) bool {
	v, ok := m[key]
	if !ok || v == nil {
		return false
	}
	switch typed := v.(type) {
	case string:
		return typed != ""
	case bool:
		return typed
	default:
		return true
	}
}

// logHeartbeatSkip 心跳 rail 跳过日志。
// Python: logger.info("[swarm.heartbeat] skip ...")（member_rails.py:169-183）
func logHeartbeatSkip(reason, sessionID string) {
	logger.Info(logger.ComponentAgentServer).
		Str("reason", reason).
		Str("session_id", sessionID).
		Msg("[swarm.heartbeat] skip HeartbeatRail")
}

// logPersonalContextSkip 个人上下文 rail 可选构造失败日志。
// Python: logger.warning("[swarm.personal_context] optional Rail construction failed (%s)")（:209-210）
func logPersonalContextSkip(err error) {
	logger.Warn(logger.ComponentAgentServer).
		Err(err).
		Msg("[swarm.personal_context] optional Rail construction failed")
}

// ──────────────────────────── 导出函数（追加段） ────────────────────────────

// toolProviderAdapter 把具体工具工厂适配成 schema.ToolProvider 签名，
// 并在上下文非平台形态时返回空表。
func toolProviderAdapter(
	build func(params map[string]any, view *buildctx.SwarmBuildContext) []tool.Tool,
) hschema.ToolProvider {
	return func(params map[string]any, ctx hschema.BuildContextInterface) ([]any, error) {
		view := SwarmContextOf(ctx)
		if view == nil {
			return nil, nil
		}
		tools := build(params, view)
		out := make([]any, 0, len(tools))
		for _, t := range tools {
			out = append(out, t)
		}
		return out, nil
	}
}

// toolWhitelistNames swarm 可继承工具白名单（单源：schema.ToolWhitelist，
// 对齐 Python TOOL_WHITELIST；15.16f 自 team 包 toolWhitelist 收编）。
func toolWhitelistNames() map[string]bool {
	return schema.ToolWhitelist
}

// agentpathsGlobalSkillsDir 全局 Skill 库目录缺省值。
// Python: get_agent_skills_dir()（common/utils）——转调 paths.go（15.16c）。
func agentpathsGlobalSkillsDir() string {
	return agentteams.GlobalSkillsDir()
}

// agentWorkspaceDir 共享 agent 工作区目录。
// Python: get_agent_workspace_dir()（common/utils）——转调 projectless 基座。
func agentWorkspaceDir() string {
	return workspacepkg.AgentWorkspaceDir()
}

// getDecoratedTools 用户 todo 工具装饰集。
// Python: get_decorated_tools()——转调 user_todo_tool 基座。
func getDecoratedTools() []tool.Tool {
	return teamtools.GetDecoratedTools()
}

// newGenerateImageToolPtr 图片生成工具实例。
// Python: generate_image 单例——转调 image_gen 基座构造。
func newGenerateImageToolPtr() tool.Tool {
	// Python: generate_image 模块级 @tool 单例；Go 基座为包内私有
	// newGenerateImageTool()，15.16f 补导出（偏差 D-15.16f-imagegen-export）。
	return teamtools.NewGenerateImageToolPublic()
}

// readSkillVisibilityFile 读 skills-visibility.json 填充 enabled/disabled。
// Python: skills 可见性 JSON 读取段（skills.py:93-118 区段）。
func readSkillVisibilityFile(path string, enabled, disabled map[string]struct{}) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var doc struct {
		Enabled  []string `json:"enabled"`
		Disabled []string `json:"disabled"`
	}
	if jsonErr := json.Unmarshal(data, &doc); jsonErr != nil {
		return
	}
	for _, name := range doc.Enabled {
		enabled[name] = struct{}{}
	}
	for _, name := range doc.Disabled {
		disabled[name] = struct{}{}
	}
}

// intParamOr 整数参数，缺失/非法取默认。
func intParamOr(params map[string]any, key string, fallback int) int {
	switch typed := params[key].(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	default:
		return fallback
	}
}

// workspaceFromString 从 factory_kwargs["workspace_root"] 或上下文取工作区串。
func workspaceFromString(factoryKwargs map[string]any, view *buildctx.SwarmBuildContext) *workspace.Workspace {
	root := ""
	if v, ok := factoryKwargs["workspace_root"].(string); ok {
		root = v
	}
	if root == "" {
		root = orEmpty(memberWorkspaceRoot(view), "./")
	}
	if root == "" {
		return nil
	}
	return &workspace.Workspace{RootPath: root}
}

// parentSysOperation 父系统操作实例（extras 透传）。
// Python: parent_sys_operation(ctx)（harness/schema/build_context.py）。
func parentSysOperation(view *buildctx.SwarmBuildContext) sysop.SysOperation {
	if v, ok := view.GetExtras()[hschema.ParentSysOperationExtrasKey].(sysop.SysOperation); ok {
		return v
	}
	return nil
}

// cuaModelRejectsImages 父模型是否拒绝图像输入（探测）。
// Python: cua_model_rejects_images (code_subagents.py:310-313 附近)——
// 调用 image_modality_probe；Go 基座探针段 ⤴️15.20，缺省 false（不拒绝）
// 会让无视觉模型误建 cua 代理，故此处保守返回 true? 不——对齐 Python 语义
// 需真实探针；15.20 回收前按「不拒绝」放行并登记偏差。
func cuaModelRejectsImages(model *llm.Model) bool {
	_ = model
	return false
}

// memberWorkspaceRoot 成员工作区根（workspace.root_path，nil 安全）。
// Python: _workspace_root(ctx)（providers/member_rails.py:214-218）。
func memberWorkspaceRoot(view *buildctx.SwarmBuildContext) string {
	if view == nil || view.Workspace == nil {
		return ""
	}
	return view.Workspace.RootPath
}
