package web

import "fmt"

// ──────────────────────────── 枚举 ────────────────────────────

// mappedGroup 表驱动路由的 catalog 分组。
// Python: mapped.py SETTINGS_ROUTES / WORKSPACE_ROUTES 二组。
type mappedGroup string

const (
	// mappedGroupSettings settings 组（config/a2a/models/locale/cron/im.hosting）。
	mappedGroupSettings mappedGroup = "settings"
	// mappedGroupWorkspace workspace 组（permissions/session 生命周期/projects/skills/harness/文件）。
	mappedGroupWorkspace mappedGroup = "workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// webHTTPMappedRoute 一条表驱动 Web HTTP 路由（HTTP verb+path → WebChannel method）。
// Python: http_routes/mapped.py:16-41 WebHttpMappedRoute（frozen dataclass）。
// 校验收敛在 newWebHTTPMappedRoute（对齐 __post_init__）。
type webHTTPMappedRoute struct {
	// httpMethod HTTP 动词（GET/POST/PUT/PATCH/DELETE）
	httpMethod string
	// path 归一化路径（{param} 占位）
	path string
	// rpcMethod 目标 WebChannel method
	rpcMethod string
	// tag OpenAPI tag
	tag string
	// summary OpenAPI summary
	summary string
	// pathToParam 路径参数 → params 键映射
	pathToParam map[string]string
	// queryKeys 透传 query 键
	queryKeys []string
	// acceptBody 是否接受 JSON body
	acceptBody bool
	// bindSessionParam 是否把 session_id 注入 params
	bindSessionParam bool
	// created 成功时回 201
	created bool
	// extraParams 额外固定 params
	extraParams map[string]string
	// group catalog 分组（Go 侧派生字段，对齐 catalog_entries 的 group）
	group mappedGroup
}

// ──────────────────────────── 全局变量 ────────────────────────────

// mappedRoutes 全量表驱动路由，行序对齐 Python MAPPED_ROUTES = SETTINGS + WORKSPACE。
// Python: mapped.py:849-852（由 mapped.py 机器对拍生成，130 行）。
var mappedRoutes = buildMappedRoutes()

// coreRouteCatalog CORE 手写路由对照（SSE/history 等在 web_http_routes.go 手写）。
// Python: mapped.py:870-883 CORE_ROUTE_CATALOG。
var coreRouteCatalog = [][4]string{
	{"GET", "/health", "", "探活（非 RPC method）"},
	{"GET", "/connection/status", "connection.status", "Agent 连接状态"},
	{"GET", "/sessions", "session.list", "列会话"},
	{"POST", "/sessions", "session.create", "建会话"},
	{"GET", "/sessions/{session_id}", "session.get_metadata", "会话元数据"},
	{"PATCH", "/sessions/{session_id}", "session.rename|session.pin", "重命名 / 置顶"},
	{"DELETE", "/sessions/{session_id}", "session.delete", "删除会话"},
	{"GET", "/sessions/{session_id}/history", "history.get", "JSON；Accept:text/event-stream 时 SSE"},
	{"POST", "/chat/completions", "chat.send", "发消息 SSE（主路径）"},
	{"POST", "/chat/resume", "chat.resume", "恢复中断对话"},
	{"POST", "/chat/{session_id}/actions/interrupt", "chat.interrupt", "中断生成"},
	{"POST", "/chat/{session_id}/actions/user_answer", "chat.user_answer", "回答 Agent 追问"},
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildMappedRoutes 构造并校验全量表（构造期 fail-fast，对齐模块加载时
// validate_mapped_routes()，mapped.py:965）。
func buildMappedRoutes() []webHTTPMappedRoute {
	routes := make([]webHTTPMappedRoute, 0, 130)
	add := func(httpMethod, path, rpcMethod, tag, summary string, pathToParam map[string]string, queryKeys []string, acceptBody, bindSessionParam, created bool, extraParams map[string]string, grp mappedGroup) {
		routes = append(routes, *newWebHTTPMappedRoute(httpMethod, path, rpcMethod, tag, summary, pathToParam, queryKeys, acceptBody, bindSessionParam, created, extraParams, grp))
	}
	add("GET", "/config", "config.get", "config", "读配置（企业只读；写权威在 Claw Manager）", nil, []string{}, false, false, false, nil, mappedGroupSettings)
	add("GET", "/a2a/ingress", "a2a.ingress.get", "a2a", "读取 A2A 入站服务状态", nil, []string{}, false, false, false, nil, mappedGroupSettings)
	add("GET", "/a2a/ingress/history", "a2a.ingress.history", "a2a", "读取 A2A 入站请求处理历史", nil, []string{"limit"}, false, false, false, nil, mappedGroupSettings)
	add("PATCH", "/a2a/ingress", "a2a.ingress.update", "a2a", "保存 A2A 入站配置", nil, []string{}, true, false, false, nil, mappedGroupSettings)
	add("POST", "/a2a/ingress:enable", "a2a.ingress.enable", "a2a", "启用 A2A 入站服务", nil, []string{}, true, false, false, nil, mappedGroupSettings)
	add("POST", "/a2a/ingress:disable", "a2a.ingress.disable", "a2a", "停用 A2A 入站服务", nil, []string{}, true, false, false, nil, mappedGroupSettings)
	add("POST", "/a2a/ingress:reload", "a2a.ingress.reload", "a2a", "重载 A2A 入站服务", nil, []string{}, true, false, false, nil, mappedGroupSettings)
	add("GET", "/models", "models.list", "models", "模型列表（企业聊天下拉只读）", nil, []string{}, false, false, false, nil, mappedGroupSettings)
	add("GET", "/locale", "locale.get_conf", "locale", "读 preferred_language", nil, []string{}, false, false, false, nil, mappedGroupSettings)
	add("PUT", "/locale", "locale.set_conf", "locale", "写 preferred_language（body.preferred_language=zh|en）", nil, []string{}, true, false, false, nil, mappedGroupSettings)
	add("GET", "/cron/jobs", "cron.job.list", "cron", "列定时任务", nil, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id", "project_id", "session_id", "count"}, false, false, false, nil, mappedGroupSettings)
	add("POST", "/cron/jobs", "cron.job.create", "cron", "创建定时任务（body 字段与 RPC params 一致）", nil, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id"}, true, true, false, nil, mappedGroupSettings)
	add("GET", "/cron/jobs/meta", "cron.job.meta", "cron", "Cron 元数据（modes/timeout 等）", nil, []string{}, false, false, false, nil, mappedGroupSettings)
	add("GET", "/cron/jobs/{id}", "cron.job.get", "cron", "获取单个任务", map[string]string{"id": "id"}, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id", "project_id", "session_id", "count"}, false, false, false, nil, mappedGroupSettings)
	add("PATCH", "/cron/jobs/{id}", "cron.job.update", "cron", "更新任务（body.patch 对象，与 RPC params 一致）", map[string]string{"id": "id"}, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id"}, true, false, false, nil, mappedGroupSettings)
	add("DELETE", "/cron/jobs/{id}", "cron.job.delete", "cron", "删除任务", map[string]string{"id": "id"}, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id"}, false, false, false, nil, mappedGroupSettings)
	add("POST", "/cron/jobs/{id}/actions/toggle", "cron.job.toggle", "cron", "启停任务（body.enabled）", map[string]string{"id": "id"}, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id"}, true, false, false, nil, mappedGroupSettings)
	add("POST", "/cron/jobs/{id}/actions/preview", "cron.job.preview", "cron", "预览下次运行（可选 body/query count）", map[string]string{"id": "id"}, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id", "project_id", "session_id", "count"}, true, false, false, nil, mappedGroupSettings)
	add("POST", "/cron/jobs/{id}/actions/run-now", "cron.job.run_now", "cron", "立即执行一次", map[string]string{"id": "id"}, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id"}, true, false, false, nil, mappedGroupSettings)
	add("GET", "/im/hosting/status", "im.hosting.status", "im-hosting", "数字分身通道与轮询状态", nil, []string{}, false, false, false, nil, mappedGroupSettings)
	add("GET", "/im/hosting/discover", "im.hosting.discover", "im-hosting", "发现近期会话", nil, []string{"channel_id", "query_count"}, false, false, false, nil, mappedGroupSettings)
	add("GET", "/im/hosting/targets", "im.hosting.targets.list", "im-hosting", "托管会话列表", nil, []string{"channel_id"}, false, false, false, nil, mappedGroupSettings)
	add("POST", "/im/hosting/targets", "im.hosting.targets.add", "im-hosting", "手选加入托管", nil, []string{}, true, false, true, nil, mappedGroupSettings)
	add("PATCH", "/im/hosting/targets/{id}", "im.hosting.targets.patch", "im-hosting", "改托管开关或覆盖字段", map[string]string{"id": "id"}, []string{}, true, false, false, nil, mappedGroupSettings)
	add("DELETE", "/im/hosting/targets/{id}", "im.hosting.targets.delete", "im-hosting", "移除托管会话", map[string]string{"id": "id"}, []string{}, false, false, false, nil, mappedGroupSettings)
	add("GET", "/im/hosting/policy", "im.hosting.policy.get", "im-hosting", "读全局托管策略", nil, []string{}, false, false, false, nil, mappedGroupSettings)
	add("PATCH", "/im/hosting/policy", "im.hosting.policy.patch", "im-hosting", "改全局托管策略", nil, []string{}, true, false, false, nil, mappedGroupSettings)
	add("POST", "/im/hosting/actions/poll-now", "im.hosting.poll_now", "im-hosting", "立即拉一轮", nil, []string{}, false, false, false, nil, mappedGroupSettings)
	add("GET", "/im/hosting/targets/{id}/history", "im.hosting.history", "im-hosting", "托管代回 history", map[string]string{"id": "id"}, []string{}, false, false, false, nil, mappedGroupSettings)
	add("POST", "/im/hosting/login/start", "im.hosting.login.start", "im-hosting", "发起渠道关联（应用配置+设备码授权）", nil, []string{}, true, false, false, nil, mappedGroupSettings)
	add("GET", "/im/hosting/login/status", "im.hosting.login.status", "im-hosting", "查询渠道关联会话状态", nil, []string{"channel_id"}, false, false, false, nil, mappedGroupSettings)
	add("GET", "/permissions/owner-scopes", "permissions.owner_scopes.get", "permissions", "数字分身 owner_scopes", nil, []string{}, false, false, false, nil, mappedGroupWorkspace)
	add("PUT", "/permissions/owner-scopes", "permissions.owner_scopes.set", "permissions", "写入 owner_scopes", nil, []string{}, true, false, false, nil, mappedGroupWorkspace)
	add("GET", "/permissions/tools", "permissions.tools.get", "permissions", "工具级 allow/ask/deny", nil, []string{}, false, false, false, nil, mappedGroupWorkspace)
	add("PUT", "/permissions/tools", "permissions.tools.set", "permissions", "整表替换 permissions.tools", nil, []string{}, true, false, false, nil, mappedGroupWorkspace)
	add("POST", "/permissions/tools/actions/update", "permissions.tools.update", "permissions", "更新单条工具级别（body.tool + level；适合含特殊字符的工具名）", nil, []string{}, true, false, false, nil, mappedGroupWorkspace)
	add("PATCH", "/permissions/tools/{tool}", "permissions.tools.update", "permissions", "更新单条工具级别", map[string]string{"tool": "tool"}, []string{}, true, false, false, nil, mappedGroupWorkspace)
	add("DELETE", "/permissions/tools/{tool}", "permissions.tools.delete", "permissions", "删除单条工具级别", map[string]string{"tool": "tool"}, []string{}, false, false, false, nil, mappedGroupWorkspace)
	add("GET", "/permissions/rules", "permissions.rules.get", "permissions", "列出参数级规则", nil, []string{}, false, false, false, nil, mappedGroupWorkspace)
	add("POST", "/permissions/rules", "permissions.rules.create", "permissions", "创建规则（body.rule）", nil, []string{}, true, false, true, nil, mappedGroupWorkspace)
	add("PATCH", "/permissions/rules/{id}", "permissions.rules.update", "permissions", "更新规则（body.patch）", map[string]string{"id": "id"}, []string{}, true, false, false, nil, mappedGroupWorkspace)
	add("DELETE", "/permissions/rules/{id}", "permissions.rules.delete", "permissions", "删除规则", map[string]string{"id": "id"}, []string{}, false, false, false, nil, mappedGroupWorkspace)
	add("GET", "/permissions/approval-overrides", "permissions.approval_overrides.get", "permissions", "列出「总是允许」覆盖项", nil, []string{}, false, false, false, nil, mappedGroupWorkspace)
	add("DELETE", "/permissions/approval-overrides/{id}", "permissions.approval_overrides.delete", "permissions", "删除一条覆盖项", map[string]string{"id": "id"}, []string{}, false, false, false, nil, mappedGroupWorkspace)
	add("POST", "/sessions/actions/archive", "session.archive", "sessions", "归档会话（body.session_ids）", nil, []string{}, true, false, false, nil, mappedGroupWorkspace)
	add("POST", "/sessions/actions/unarchive", "session.unarchive", "sessions", "取消归档（body.session_ids）", nil, []string{}, true, false, false, nil, mappedGroupWorkspace)
	add("GET", "/archived-sessions", "session.archived.list", "sessions", "归档会话列表", nil, []string{"work_mode", "project_id", "keyword", "limit", "offset"}, false, false, false, nil, mappedGroupWorkspace)
	add("POST", "/projects/{project_id}/sessions/actions/archive", "project.sessions.archive", "projects", "项目下批量归档会话", map[string]string{"project_id": "project_id"}, []string{}, true, false, false, nil, mappedGroupWorkspace)
	add("POST", "/projects/{project_id}/sessions/actions/delete-archived", "project.sessions.delete_archived", "projects", "项目下批量删除已归档会话", map[string]string{"project_id": "project_id"}, []string{}, true, false, false, nil, mappedGroupWorkspace)
	add("GET", "/projects", "project.list", "projects", "列出项目（含默认项目与统计）", nil, []string{"filter", "work_mode", "include_hidden", "limit", "cron_id"}, false, false, false, nil, mappedGroupWorkspace)
	add("GET", "/projects/pinned-sessions", "project.pinned_sessions", "projects", "全部置顶会话", nil, []string{}, false, false, false, nil, mappedGroupWorkspace)
	add("GET", "/projects/{project_id}", "project.info", "projects", "项目详情（统计口径同 project.list）", map[string]string{"project_id": "project_id"}, []string{}, false, false, false, nil, mappedGroupWorkspace)
	add("GET", "/projects/{project_id}/sessions", "project.get_sessions", "projects", "项目下非置顶普通会话列表", map[string]string{"project_id": "project_id"}, []string{"limit"}, false, false, false, nil, mappedGroupWorkspace)
	add("GET", "/projects/{project_id}/cron-sessions", "project.get_cron_sessions", "projects", "项目下定时任务触发会话列表", map[string]string{"project_id": "project_id"}, []string{"cron_id"}, false, false, false, nil, mappedGroupWorkspace)
	add("POST", "/projects", "project.create", "projects", "创建项目（name/project_dir/work_mode 均可选）", nil, []string{}, true, false, false, nil, mappedGroupWorkspace)
	add("POST", "/projects/actions/restore", "project.restore", "projects", "恢复已软删除的项目（body.project_id）", nil, []string{}, true, false, false, nil, mappedGroupWorkspace)
	add("PATCH", "/projects/{project_id}", "project.rename", "projects", "重命名项目（body.name）", map[string]string{"project_id": "project_id"}, []string{}, true, false, false, nil, mappedGroupWorkspace)
	add("POST", "/projects/{project_id}/actions/pin", "project.pin", "projects", "置顶/取消置顶项目（body.pinned）", map[string]string{"project_id": "project_id"}, []string{}, true, false, false, nil, mappedGroupWorkspace)
	add("DELETE", "/projects/{project_id}", "project.remove", "projects", "移除项目（软删除）", map[string]string{"project_id": "project_id"}, []string{}, false, false, false, nil, mappedGroupWorkspace)
	add("GET", "/skills", "skills.list", "skills", "技能列表（个人/本机目录）", nil, []string{"with_installed", "refresh_marketplaces", "session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/installed", "skills.installed", "skills", "已安装技能", nil, []string{"session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/enterprise", "skills.enterprise.list", "skills", "企业租户已装技能（Gateway 本地表）", nil, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id"}, false, false, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/enterprise/actions/install", "skills.enterprise.install", "skills", "企业租户安装", nil, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id"}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/enterprise/actions/uninstall", "skills.enterprise.uninstall", "skills", "企业租户卸载", nil, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id"}, true, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/marketplace", "skills.marketplace.list", "skills", "技能市场列表", nil, []string{"session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/marketplace", "skills.marketplace.add", "skills", "添加市场源", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/marketplace/actions/remove", "skills.marketplace.remove", "skills", "移除市场源", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/marketplace/actions/toggle", "skills.marketplace.toggle", "skills", "启停市场源", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/clawhub/token", "skills.clawhub.get_token", "skills", "ClawHub token", nil, []string{"session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("PUT", "/skills/clawhub/token", "skills.clawhub.set_token", "skills", "保存 ClawHub token", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/clawhub/search", "skills.clawhub.search", "skills", "搜索 ClawHub（query.q）", nil, []string{"q", "limit", "session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/clawhub/actions/search", "skills.clawhub.search", "skills", "搜索 ClawHub", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/clawhub/actions/download", "skills.clawhub.download", "skills", "从 ClawHub 安装", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/teamskillshub", "skills.teamskillshub.info", "skills", "TeamSkills Hub 信息", nil, []string{"session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/teamskillshub/actions/init", "skills.teamskillshub.init", "skills", "TeamSkills Hub 初始化", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/teamskillshub/actions/validate", "skills.teamskillshub.validate", "skills", "校验待发布技能", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/teamskillshub/actions/pack", "skills.teamskillshub.pack", "skills", "打包技能", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/teamskillshub/actions/search", "skills.teamskillshub.search", "skills", "搜索 TeamSkills Hub", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/teamskillshub/actions/install", "skills.teamskillshub.install", "skills", "从 TeamSkills Hub 安装", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/teamskillshub/actions/publish", "skills.teamskillshub.publish", "skills", "发布到 TeamSkills Hub", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/teamskillshub/actions/delete", "skills.teamskillshub.delete", "skills", "删除 TeamSkills Hub 版本", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/sources", "skills.source.providers", "skills", "列出已配置技能源", nil, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id", "session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/sources/search", "skills.source.search", "skills", "搜索技能源", nil, []string{"source_id", "q", "page", "page_size", "group_id", "bot_id", "user_id", "service_id", "agent_id", "session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/sources/actions/search", "skills.source.search", "skills", "搜索技能源（含扩展筛选）", nil, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id"}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/sources/actions/install", "skills.source.install", "skills", "从技能源安装精确版本", nil, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id"}, true, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/updates", "skills.updates.check", "skills", "检查已安装技能更新", nil, []string{"source_id", "group_id", "bot_id", "user_id", "service_id", "agent_id", "session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/actions/update", "skills.update", "skills", "更新已安装技能", nil, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id"}, true, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/enterprise/sources", "skills.enterprise.source.providers", "skills", "列出企业技能源", nil, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id", "session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/enterprise/sources/search", "skills.enterprise.source.search", "skills", "搜索企业技能源", nil, []string{"source_id", "q", "page", "page_size", "group_id", "bot_id", "user_id", "service_id", "agent_id", "session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/enterprise/sources/actions/search", "skills.enterprise.source.search", "skills", "搜索企业技能源（含扩展筛选）", nil, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id"}, true, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/retrieval/status", "skills.retrieval.status", "skills", "技能检索索引状态", nil, []string{"session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/retrieval/tree", "skills.retrieval.tree", "skills", "技能检索树", nil, []string{"session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/retrieval/actions/index-build", "skills.retrieval.index_build", "skills", "构建检索索引", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/retrieval/actions/index-cancel", "skills.retrieval.index_cancel", "skills", "取消索引构建", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/retrieval/actions/search", "skills.retrieval.search", "skills", "检索技能", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/evolution/status", "skills.evolution.status", "skills", "技能进化状态（query: session_id, name）", nil, []string{"session_id", "name"}, false, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/evolution", "skills.evolution.get", "skills", "当前进化配置（query: session_id, name）", nil, []string{"session_id", "name"}, false, true, false, nil, mappedGroupWorkspace)
	add("PUT", "/skills/evolution", "skills.evolution.save", "skills", "保存进化配置", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/evolution/archives", "skills.evolution.archives", "skills", "进化归档（query: session_id, name）", nil, []string{"session_id", "name"}, false, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/evolution/actions/rollback", "skills.evolution.rollback", "skills", "进化回滚", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/evolution/actions/rebuild", "skills.evolution.rebuild", "skills", "重建进化产物", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/skillnet/actions/search", "skills.skillnet.search", "skills", "SkillNet 搜索", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/skillnet/actions/install", "skills.skillnet.install", "skills", "SkillNet 安装", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/skillnet/install-status", "skills.skillnet.install_status", "skills", "SkillNet 安装状态（query: session_id, install_id）", nil, []string{"session_id", "install_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/skillnet/actions/evaluate", "skills.skillnet.evaluate", "skills", "SkillNet 评估", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/online-search/actions/search", "skills.online_search.search", "skills", "在线搜索技能", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/actions/install", "skills.install", "skills", "安装技能（body.spec）", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/actions/uninstall", "skills.uninstall", "skills", "卸载技能（body.name）", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/actions/toggle", "skills.toggle", "skills", "启用/停用技能", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/skills/actions/import-local", "skills.import_local", "skills", "从本地路径导入", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/{name}/files", "skills.files.list", "skills", "技能文件列表（工作副本）", map[string]string{"name": "name"}, []string{"session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/{name}/files/content", "skills.files.get", "skills", "技能文件预览（工作副本）", map[string]string{"name": "name"}, []string{"path", "session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/{name}/versions", "skills.versions.list", "skills", "技能本地版本列表", map[string]string{"name": "name"}, []string{"session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("GET", "/skills/{name}", "skills.get", "skills", "技能详情", map[string]string{"name": "name"}, []string{"origin", "session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("GET", "/harness/packages", "harness.packages", "harness", "列出 harness 包", nil, []string{"session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("POST", "/harness/packages/actions/scan", "harness.packages.scan", "harness", "扫描并刷新包列表", nil, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/harness/packages/actions/import", "harness.import", "harness", "导入 zip（JSON file_content=base64；multipart 见专用入口）", nil, []string{}, true, false, false, nil, mappedGroupWorkspace)
	add("POST", "/harness/packages/{package_id}/actions/activate", "harness.activate", "harness", "激活包", map[string]string{"package_id": "package_id"}, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/harness/packages/{package_id}/actions/deactivate", "harness.deactivate", "harness", "停用包", map[string]string{"package_id": "package_id"}, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("GET", "/harness/packages/{package_id}/export", "harness.export", "harness", "导出包（返回 download_url / token，文件走既有 HTTP 下载）", map[string]string{"package_id": "package_id"}, []string{"session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("DELETE", "/harness/packages/{package_id}", "harness.delete", "harness", "删除包", map[string]string{"package_id": "package_id"}, []string{"session_id"}, false, true, false, nil, mappedGroupWorkspace)
	add("POST", "/sessions/{session_id}/media", "media.persist", "files", "保存会话图片", map[string]string{"session_id": "session_id"}, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("DELETE", "/sessions/{session_id}/media", "media.discard", "files", "删除未发送附件", map[string]string{"session_id": "session_id"}, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("POST", "/sessions/{session_id}/documents", "document.persist", "files", "保存会话文档", map[string]string{"session_id": "session_id"}, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("GET", "/documents/formats", "document.formats", "files", "读取禁止上传的格式", nil, []string{}, false, false, false, nil, mappedGroupWorkspace)
	add("POST", "/sessions/{session_id}/files/import", "file.import_url", "files", "导入对象存储附件", map[string]string{"session_id": "session_id"}, []string{}, true, true, false, nil, mappedGroupWorkspace)
	add("GET", "/workspace/tree", "workspace.tree", "workspace", "列工作区目录（相对租户根；含 zone / deletable）", nil, []string{"relative_path", "group_id", "bot_id", "user_id", "service_id", "agent_id"}, false, false, false, nil, mappedGroupWorkspace)
	add("GET", "/workspace/usage", "workspace.usage", "workspace", "查询本人工作区用量与配额状态", nil, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id"}, false, false, false, nil, mappedGroupWorkspace)
	add("DELETE", "/workspace/entries", "workspace.entries.delete", "workspace", "删除可清理文件或目录（body.relative_paths）", nil, []string{"group_id", "bot_id", "user_id", "service_id", "agent_id"}, true, false, false, nil, mappedGroupWorkspace)
	add("GET", "/workspace/preview", "workspace.preview", "workspace", "预览文件正文（二进制 content=null；目录 400）", nil, []string{"relative_path", "max_bytes", "group_id", "bot_id", "user_id", "service_id", "agent_id"}, false, false, false, nil, mappedGroupWorkspace)
	validateMappedRoutes(routes)
	return routes
}

// newWebHTTPMappedRoute 构造并校验一条路由（对齐 __post_init__，mapped.py:32-41）。
func newWebHTTPMappedRoute(httpMethod, path, rpcMethod, tag, summary string, pathToParam map[string]string, queryKeys []string, acceptBody, bindSessionParam, created bool, extraParams map[string]string, grp mappedGroup) *webHTTPMappedRoute {
	if len(path) == 0 || path[0] != '/' {
		panic(fmt.Sprintf("path must start with /: %s", path))
	}
	method := httpMethod
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		panic(fmt.Sprintf("unsupported HTTP method: %s", method))
	}
	if rpcMethod == "" {
		panic("rpc_method is required")
	}
	return &webHTTPMappedRoute{
		httpMethod: method, path: path, rpcMethod: rpcMethod, tag: tag, summary: summary,
		pathToParam: pathToParam, queryKeys: queryKeys, acceptBody: acceptBody,
		bindSessionParam: bindSessionParam, created: created, extraParams: extraParams, group: grp,
	}
}

// validateMappedRoutes fail-fast 检查重复 HTTP 面（忽略路径参数名差异）。
// Python: mapped.py:886-903 validate_mapped_routes + _normalize_path_key。
func validateMappedRoutes(routes []webHTTPMappedRoute) {
	seen := map[string]bool{}
	for _, route := range routes {
		key := route.httpMethod + " " + normalizePathKey(route.path)
		if seen[key] {
			panic(fmt.Sprintf("duplicate Web HTTP route %s", key))
		}
		seen[key] = true
	}
}

// normalizePathKey 归一路径参数名为统一占位。
// Python: mapped.py:896-903 _normalize_path_key。
func normalizePathKey(path string) string {
	key := ""
	for _, part := range splitPath(path) {
		if len(part) >= 2 && part[0] == '{' && part[len(part)-1] == '}' {
			part = "{}"
		}
		key += "/" + part
	}
	return key
}

// splitPath 按 / 切分路径段（空段丢弃）。
func splitPath(path string) []string {
	parts := []string{}
	start := 0
	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == '/' {
			if i > start {
				parts = append(parts, path[start:i])
			}
			start = i + 1
		}
	}
	return parts
}

// catalogRow 一条 catalog 对照行。
type catalogRow struct {
	HTTPMethod string `json:"http_method"`
	Path       string `json:"path"`
	RPCMethod  string `json:"rpc_method"`
	Group      string `json:"group"`
	Tag        string `json:"tag,omitempty"`
	Summary    string `json:"summary,omitempty"`
	Note       string `json:"note,omitempty"`
	BindParam  *bool  `json:"bind_session_param,omitempty"`
}

// catalogEntries 生成 GET /api/v1/catalog 的机器可读对照表（含 CORE 行与
// multipart 专行）。Python: mapped.py:918-962 catalog_entries。
func catalogEntries() []catalogRow {
	rows := []catalogRow{}
	for _, core := range coreRouteCatalog {
		rows = append(rows, catalogRow{HTTPMethod: core[0], Path: "/api/v1" + core[1], RPCMethod: core[2], Group: "core", Note: core[3]})
	}
	for i := range mappedRoutes {
		route := &mappedRoutes[i]
		bind := route.bindSessionParam
		rows = append(rows, catalogRow{HTTPMethod: route.httpMethod, Path: "/api/v1" + route.path,
			RPCMethod: route.rpcMethod, Group: string(route.group), Tag: route.tag,
			Summary: route.summary, BindParam: &bind})
	}
	// multipart 专行（Python: mapped.py:953-961）
	noBind := false
	rows = append(rows, catalogRow{HTTPMethod: "POST", Path: "/api/v1/harness/packages/actions/import-file",
		RPCMethod: "harness.import", Group: "workspace", Tag: "harness",
		Summary: "multipart 字段 file → harness.import", BindParam: &noBind})
	return rows
}

// mappedRoutesForEdition 按版本取路由表；Go 恒个人版表
// （不挂 northbound_routes、保留 a2a ingress 组），偏差 D-B2-5。
// Python: mapped.py:855-867 mapped_routes_for_edition。
func mappedRoutesForEdition() []webHTTPMappedRoute {
	return mappedRoutes
}
