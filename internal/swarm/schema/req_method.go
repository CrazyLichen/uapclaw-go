package schema

import "fmt"

// ──────────────────────────── 枚举 ────────────────────────────

// ReqMethod E2A 协议 RPC 方法名枚举。
// 定义 Gateway↔AgentServer 通信链路中所有合法的 RPC 方法标识，
// 用于 E2AEnvelope.method 字段和 AgentServer 方法路由分发。
// 值为点分字符串格式（如 "chat.send"），与 Python ReqMethod 枚举值一一对应。
// Python: jiuwenswarm/common/schema/message.py (ReqMethod)
type ReqMethod string

const (
	// ─── 初始化 / ACP ───

	// ReqMethodInitialize 初始化
	ReqMethodInitialize ReqMethod = "initialize"
	// ReqMethodACPToolResponse ACP 工具响应
	ReqMethodACPToolResponse ReqMethod = "acp.tool_response"

	// ─── 对话核心 ───

	// ReqMethodChatSend 发送对话消息
	ReqMethodChatSend ReqMethod = "chat.send"
	// ReqMethodChatResume 恢复对话
	ReqMethodChatResume ReqMethod = "chat.resume"
	// ReqMethodChatCancel 中断对话
	ReqMethodChatCancel ReqMethod = "chat.interrupt"
	// ReqMethodChatAnswer 用户回答 Agent 提问
	ReqMethodChatAnswer ReqMethod = "chat.user_answer"

	// ─── 媒体 / 文件域（27.8；Python: workspace_file_adapter.py:109-127 methods）───

	// ReqMethodMediaPersist 浏览器上传图片落盘会话 uploads
	ReqMethodMediaPersist ReqMethod = "media.persist"
	// ReqMethodMediaDiscard 删除未发送的会话 uploads 副本
	ReqMethodMediaDiscard ReqMethod = "media.discard"
	// ReqMethodDocumentPersist 浏览器上传文档校验落盘（扩展名黑名单）
	ReqMethodDocumentPersist ReqMethod = "document.persist"
	// ReqMethodDocumentFormats 读取禁止上传的格式列表
	ReqMethodDocumentFormats ReqMethod = "document.formats"
	// ReqMethodPathSelectDirectory workspace 内目录选择
	ReqMethodPathSelectDirectory ReqMethod = "path.select_directory"
	// ReqMethodPathSelectFiles workspace 内文件枚举（图片 ≤4MB 附 base64）
	ReqMethodPathSelectFiles ReqMethod = "path.select_files"
	// ReqMethodFileImportURL chat.send 上行外部 url 文件由 AgentServer 下载落盘
	ReqMethodFileImportURL ReqMethod = "file.import_url"
	// ReqMethodFileUploadChunk 分块上传（首块 O_EXCL 认领 + 后续 append）
	ReqMethodFileUploadChunk ReqMethod = "file.upload_chunk"
	// ReqMethodFileReadUploadChunk 读取会话 uploads 内文件的有界分块
	ReqMethodFileReadUploadChunk ReqMethod = "file.read_upload_chunk"
	// ReqMethodFileDownloadVerifiedChunk Smart Approval sealed-asset 校验分块下载
	ReqMethodFileDownloadVerifiedChunk ReqMethod = "file.download_verified_chunk"
	// ReqMethodWorkspaceTree 列工作区目录树（含 zone / deletable）
	ReqMethodWorkspaceTree ReqMethod = "workspace.tree"
	// ReqMethodWorkspacePreview 预览文件正文（二进制 content=null；目录 400）
	ReqMethodWorkspacePreview ReqMethod = "workspace.preview"
	// ReqMethodWorkspaceEntriesDelete 删除可清理文件或目录（zones 白名单）
	ReqMethodWorkspaceEntriesDelete ReqMethod = "workspace.entries.delete"
	// ReqMethodWorkspaceUsage 查询本人工作区用量（单机 du 快照）
	ReqMethodWorkspaceUsage ReqMethod = "workspace.usage"
	// 注：im.file_persist 归 IM 渠道域不注册；workspace.* 四值 27-A B2 起承接
	//（个人版面板，裁决 2026-11-25；Python 侧 _handle_workspace 个人版 adapter 也分发）。

	// ─── 命令 ───

	// ReqMethodCommandAddDir 添加目录
	ReqMethodCommandAddDir ReqMethod = "command.add_dir"
	// ReqMethodCommandChrome Chrome 控制
	ReqMethodCommandChrome ReqMethod = "command.chrome"
	// ReqMethodCommandCompact 压缩上下文
	ReqMethodCommandCompact ReqMethod = "command.compact"
	// ReqMethodCommandContext 上下文信息
	ReqMethodCommandContext ReqMethod = "command.context"
	// ReqMethodCommandGoal 持续目标命令（action=set/resume/pause/clear/get）
	// 上游协议成员，此前 Go 侧缺此常量、只用裸字符串（slash_command.go:279）。
	ReqMethodCommandGoal ReqMethod = "command.goal"
	// ReqMethodCommandRecap 回顾总结
	ReqMethodCommandRecap ReqMethod = "command.recap"
	// ReqMethodCommandDiff 查看差异
	ReqMethodCommandDiff ReqMethod = "command.diff"
	// ReqMethodCommandMCP MCP 管理
	ReqMethodCommandMCP ReqMethod = "command.mcp"
	// ReqMethodCommandModel 模型切换
	ReqMethodCommandModel ReqMethod = "command.model"
	// ReqMethodCommandResume 恢复执行
	ReqMethodCommandResume ReqMethod = "command.resume"
	// ReqMethodCommandSandbox 沙箱管理
	ReqMethodCommandSandbox ReqMethod = "command.sandbox"
	// ReqMethodCommandSession 会话命令
	ReqMethodCommandSession ReqMethod = "command.session"
	// ReqMethodCommandStatus 状态查询
	ReqMethodCommandStatus ReqMethod = "command.status"

	// ─── 配置 / 通道 ───

	// ReqMethodConfigGet 获取配置
	ReqMethodConfigGet ReqMethod = "config.get"
	// ReqMethodConfigSet 设置配置
	ReqMethodConfigSet ReqMethod = "config.set"
	// ReqMethodConfigSaveAll 全量保存配置（Python ReqMethod.CONFIG_SAVE_ALL，27.4）
	ReqMethodConfigSaveAll ReqMethod = "config.save_all"
	// ReqMethodConfigValidateModel 校验模型连通性（Python ReqMethod.CONFIG_VALIDATE_MODEL，27.4）
	ReqMethodConfigValidateModel ReqMethod = "config.validate_model"
	// ReqMethodConfigMoaStatus MOA 兼容就绪状态（Python ReqMethod.CONFIG_MOA_STATUS，27.4）
	ReqMethodConfigMoaStatus ReqMethod = "config.moa_status"
	// ReqMethodChannelGet 获取通道信息
	ReqMethodChannelGet ReqMethod = "channel.get"

	// ─── 会话 ───

	// ReqMethodVoiceTaskCheckpointAck 语音任务检查点 ACK（23.9，对齐 ReqMethod.VOICE_TASK_CHECKPOINT_ACK）
	ReqMethodVoiceTaskCheckpointAck ReqMethod = "voice.task.checkpoint_ack"
	// ReqMethodVoiceTaskFiles 语音任务产物文件查询（23.9，对齐 ReqMethod.VOICE_TASK_FILES）
	ReqMethodVoiceTaskFiles ReqMethod = "voice.task.files"
	// ReqMethodSessionList 会话列表
	ReqMethodSessionList ReqMethod = "session.list"
	// ReqMethodSessionCreate 创建会话
	ReqMethodSessionCreate ReqMethod = "session.create"
	// ReqMethodSessionSwitch 切换会话
	ReqMethodSessionSwitch ReqMethod = "session.switch"
	// ReqMethodSessionPlanStatus 只读查询会话是否处于计划模式
	ReqMethodSessionPlanStatus ReqMethod = "session.plan_status"
	// ReqMethodSessionDelete 删除会话
	ReqMethodSessionDelete ReqMethod = "session.delete"
	// ReqMethodSessionRename 重命名会话
	ReqMethodSessionRename ReqMethod = "session.rename"
	// ReqMethodSessionFork 分支会话
	ReqMethodSessionFork ReqMethod = "session.fork"
	// ReqMethodSessionRewind 回退会话
	ReqMethodSessionRewind ReqMethod = "session.rewind"
	// ReqMethodSessionRewindAndRestore 回退并恢复会话
	ReqMethodSessionRewindAndRestore ReqMethod = "session.rewind_and_restore"
	// ReqMethodSessionRewindContext 回退会话上下文
	ReqMethodSessionRewindContext ReqMethod = "session.rewind_context"
	// ReqMethodSessionRestoreFiles 恢复会话文件
	ReqMethodSessionRestoreFiles ReqMethod = "session.restore_files"

	// ReqMethodProactiveTick 触发一次主动推荐 tick
	// Python: ReqMethod.PROACTIVE_TICK（server/dispatch.py:157 映射到 handlers/ops.py）
	ReqMethodProactiveTick ReqMethod = "proactive.tick"
	// ReqMethodProactiveFeedback 记录对一条主动推荐的赞/踩
	ReqMethodProactiveFeedback ReqMethod = "proactive.feedback"
	// ReqMethodRecommendation 推荐面板 RPC（AgentServer 执行，网关仅转发）
	// Python: message.py:60 RECOMMENDATION = "recommendation.job"
	ReqMethodRecommendation ReqMethod = "recommendation.job"
	// ReqMethodPromptSuggestionCancel 取消一条正在生成的建议
	// Python: message.py:59 PROMPT_SUGGESTION_CANCEL = "prompt_suggestion.cancel"
	ReqMethodPromptSuggestionCancel ReqMethod = "prompt_suggestion.cancel"

	// ─── 模型目录（23.4d）───
	// wire 名逐字对齐上游注册名（app_web_handlers.py:5629-5660 与 vendors.* :5677-5678）。

	// ReqMethodModelsGet 读取单个模型详情
	ReqMethodModelsGet ReqMethod = "models.get"
	// ReqMethodModelsUpsert 新增/更新模型条目
	ReqMethodModelsUpsert ReqMethod = "models.upsert"
	// ReqMethodModelsDelete 删除模型条目
	ReqMethodModelsDelete ReqMethod = "models.delete"
	// ReqMethodModelsReferences 查询模型/模型组引用位置
	ReqMethodModelsReferences ReqMethod = "models.references"
	// ReqMethodModelGroupsList 模型组列表
	ReqMethodModelGroupsList ReqMethod = "model_groups.list"
	// ReqMethodModelGroupsUpsert 新增/更新模型组
	ReqMethodModelGroupsUpsert ReqMethod = "model_groups.upsert"
	// ReqMethodModelGroupsDelete 删除模型组
	ReqMethodModelGroupsDelete ReqMethod = "model_groups.delete"
	// ReqMethodModelsList 模型列表（Python ReqMethod.MODELS_LIST，27.4）
	ReqMethodModelsList ReqMethod = "models.list"
	// ReqMethodModelsValidate 校验单个模型参数（Python ReqMethod.MODELS_VALIDATE，27.4）
	ReqMethodModelsValidate ReqMethod = "models.validate"
	// ReqMethodModelsReplaceAll 全量替换模型列表（Python ReqMethod.MODELS_REPLACE_ALL，27.4）
	ReqMethodModelsReplaceAll ReqMethod = "models.replace_all"
	// ReqMethodModelsZenEntries 免费模型清单（Python ReqMethod.MODELS_ZEN_ENTRIES，27.4）
	ReqMethodModelsZenEntries ReqMethod = "models.zen.entries"
	// ReqMethodModelsZenWarm 免费模型预热（Python ReqMethod.MODELS_ZEN_WARM，27.4）
	ReqMethodModelsZenWarm ReqMethod = "models.zen.warm"
	// ReqMethodLocaleGetConf 读取界面偏好语言（Python ReqMethod.LOCALE_GET_CONF，27.4）
	ReqMethodLocaleGetConf ReqMethod = "locale.get_conf"
	// ReqMethodLocaleSetConf 写入界面偏好语言（Python ReqMethod.LOCALE_SET_CONF，27.4）
	ReqMethodLocaleSetConf ReqMethod = "locale.set_conf"
	// ReqMethodVendorsList 厂商预设分组列表
	ReqMethodVendorsList ReqMethod = "vendors.list"
	// ReqMethodVendorsFetchModels 按厂商预设拉取远端模型列表
	ReqMethodVendorsFetchModels ReqMethod = "vendors.fetch_models"
	// ReqMethodSessionSelectionSet 设置会话模型选择
	ReqMethodSessionSelectionSet ReqMethod = "session.selection.set"

	// ─── 会话素材（session_assets.py，企业版新增方法族）───

	// ReqMethodSessionAssetsList 会话素材列表（Python SESSION_ASSETS_LIST）
	ReqMethodSessionAssetsList ReqMethod = "session.assets.list"
	// ReqMethodSessionAssetsRegister 注册会话素材（Python SESSION_ASSETS_REGISTER）
	ReqMethodSessionAssetsRegister ReqMethod = "session.assets.register"
	// ReqMethodSessionAssetsRename 重命名会话素材（Python SESSION_ASSETS_RENAME）
	ReqMethodSessionAssetsRename ReqMethod = "session.assets.rename"
	// ReqMethodSessionAssetsRemove 删除会话素材条目（Python SESSION_ASSETS_REMOVE）
	ReqMethodSessionAssetsRemove ReqMethod = "session.assets.remove"

	// ─── 项目控制面（project_repository.py，Phase 1 只读）───

	// ReqMethodProjectList 项目列表含统计（Python PROJECT_LIST）
	ReqMethodProjectList ReqMethod = "project.list"
	// ReqMethodProjectInfo 项目详情（Python PROJECT_INFO）
	ReqMethodProjectInfo ReqMethod = "project.info"
	// ReqMethodProjectPinnedSessions 置顶会话列表（Python PROJECT_PINNED_SESSIONS）
	ReqMethodProjectPinnedSessions ReqMethod = "project.pinned_sessions"
	// ReqMethodProjectGetSessions 项目普通会话列表（Python PROJECT_GET_SESSIONS）
	ReqMethodProjectGetSessions ReqMethod = "project.get_sessions"
	// ReqMethodProjectGetCronSessions 项目 cron 会话列表（Python PROJECT_GET_CRON_SESSIONS）
	ReqMethodProjectGetCronSessions ReqMethod = "project.get_cron_sessions"
	// ReqMethodProjectCronResolveBinding cron 项目绑定解析（Python PROJECT_CRON_RESOLVE_BINDING）
	ReqMethodProjectCronResolveBinding ReqMethod = "project.cron.resolve_binding"

	// ─── 会话控制面（session_repository.py）───

	// ReqMethodSessionPin 置顶/取消置顶会话（Python SESSION_PIN）
	ReqMethodSessionPin ReqMethod = "session.pin"

	// ─── 扩展设备管理（extension_package_manager.py）───

	// ReqMethodAgentTemplatesList 模板列表（Python AGENT_TEMPLATES_LIST）
	ReqMethodAgentTemplatesList ReqMethod = "agent_templates.list"
	// ReqMethodAgentTemplatesShow 模板详情（Python AGENT_TEMPLATES_SHOW）
	ReqMethodAgentTemplatesShow ReqMethod = "agent_templates.show"
	// ReqMethodAgentTemplatesFileList 模板文件树（Python AGENT_TEMPLATES_FILE_LIST）
	ReqMethodAgentTemplatesFileList ReqMethod = "agent_templates.file.list"
	// ReqMethodAgentTemplatesFileRead 读模板包内文件（Python AGENT_TEMPLATES_FILE_READ）
	ReqMethodAgentTemplatesFileRead ReqMethod = "agent_templates.file.read"
	// ReqMethodAgentTemplatesCreate 创建模板包（Python AGENT_TEMPLATES_CREATE）
	ReqMethodAgentTemplatesCreate ReqMethod = "agent_templates.create"
	// ReqMethodAgentTemplatesImportLocal 本地导入模板（Python AGENT_TEMPLATES_IMPORT_LOCAL）
	ReqMethodAgentTemplatesImportLocal ReqMethod = "agent_templates.import_local"
	// ReqMethodAgentTemplatesInstall Hub 安装（Python AGENT_TEMPLATES_INSTALL；本批 D-1 拒绝）
	ReqMethodAgentTemplatesInstall ReqMethod = "agent_templates.install"
	// ReqMethodAgentTemplatesUninstall 卸载模板（Python AGENT_TEMPLATES_UNINSTALL）
	ReqMethodAgentTemplatesUninstall ReqMethod = "agent_templates.uninstall"
	// ReqMethodAgentTemplatesUpdate 更新模板（Python AGENT_TEMPLATES_UPDATE）
	ReqMethodAgentTemplatesUpdate ReqMethod = "agent_templates.update"
	// ReqMethodAgentTemplatesDelete 删除模板（Python AGENT_TEMPLATES_DELETE）
	ReqMethodAgentTemplatesDelete ReqMethod = "agent_templates.delete"
	// ReqMethodAgentGroupsList 分组列表（Python AGENT_GROUPS_LIST）
	ReqMethodAgentGroupsList ReqMethod = "agent_groups.list"
	// ReqMethodAgentGroupsShow 分组详情（Python AGENT_GROUPS_SHOW）
	ReqMethodAgentGroupsShow ReqMethod = "agent_groups.show"
	// ReqMethodAgentGroupsFileList 分组文件树（Python AGENT_GROUPS_FILE_LIST）
	ReqMethodAgentGroupsFileList ReqMethod = "agent_groups.file.list"
	// ReqMethodAgentGroupsFileRead 读分组包内文件（Python AGENT_GROUPS_FILE_READ）
	ReqMethodAgentGroupsFileRead ReqMethod = "agent_groups.file.read"
	// ReqMethodAgentGroupsCreate 创建分组（Python AGENT_GROUPS_CREATE）
	ReqMethodAgentGroupsCreate ReqMethod = "agent_groups.create"
	// ReqMethodAgentGroupsImportLocal 本地导入分组（Python AGENT_GROUPS_IMPORT_LOCAL）
	ReqMethodAgentGroupsImportLocal ReqMethod = "agent_groups.import_local"
	// ReqMethodAgentGroupsInstall Hub 安装分组（Python AGENT_GROUPS_INSTALL；本批 D-1 拒绝）
	ReqMethodAgentGroupsInstall ReqMethod = "agent_groups.install"
	// ReqMethodAgentGroupsUninstall 卸载分组（Python AGENT_GROUPS_UNINSTALL）
	ReqMethodAgentGroupsUninstall ReqMethod = "agent_groups.uninstall"
	// ReqMethodPluginPackagesInstall Hub 安装插件（Python PLUGIN_PACKAGES_INSTALL；D-1 拒绝）
	ReqMethodPluginPackagesInstall ReqMethod = "plugin_packages.install"
	// ReqMethodPluginPackagesUninstall 卸载插件（Python PLUGIN_PACKAGES_UNINSTALL）
	ReqMethodPluginPackagesUninstall ReqMethod = "plugin_packages.uninstall"

	// ─── 历史 ───

	// ReqMethodHistoryGet 获取历史
	ReqMethodHistoryGet ReqMethod = "history.get"
	// ReqMethodHistoryListTurns 列出历史轮次
	ReqMethodHistoryListTurns ReqMethod = "history.list_turns"

	// ─── 团队 ───

	// ReqMethodTeamDelete 删除团队
	ReqMethodTeamDelete ReqMethod = "team.delete"
	// ReqMethodTeamSnapshot 团队快照
	ReqMethodTeamSnapshot ReqMethod = "team.snapshot"
	// ReqMethodTeamHistoryGet 获取团队历史
	ReqMethodTeamHistoryGet ReqMethod = "team.history.get"
	// ReqMethodTeamMqPublish 外部团队事件经 Gateway 中继投递（hybrid publisher）。
	// Python: ReqMethod.TEAM_MQ_PUBLISH = "team.mq.publish"（dispatch.py:245）
	ReqMethodTeamMqPublish ReqMethod = "team.mq.publish"

	// ─── 路径 / 文件 / TTS / 内存 ───

	// ReqMethodPathGet 获取路径
	ReqMethodPathGet ReqMethod = "path.get"
	// ReqMethodPathSet 设置路径
	ReqMethodPathSet ReqMethod = "path.set"
	// ReqMethodMemoryForbiddenGet 读取记忆禁区（Python ReqMethod.MEMORY_FORBIDDEN_GET，27.4）
	ReqMethodMemoryForbiddenGet ReqMethod = "memory.forbidden.get"
	// ReqMethodMemoryForbiddenSet 写入记忆禁区（Python ReqMethod.MEMORY_FORBIDDEN_SET，27.4）
	ReqMethodMemoryForbiddenSet ReqMethod = "memory.forbidden.set"
	// ReqMethodPermissionsOwnerScopesGet 读取权限属主范围（Python ReqMethod.PERMISSIONS_OWNER_SCOPES_GET，27.4）
	ReqMethodPermissionsOwnerScopesGet ReqMethod = "permissions.owner_scopes.get"
	// ReqMethodPermissionsOwnerScopesSet 写入权限属主范围（Python ReqMethod.PERMISSIONS_OWNER_SCOPES_SET，27.4）
	ReqMethodPermissionsOwnerScopesSet ReqMethod = "permissions.owner_scopes.set"
	// ReqMethodFilesList 列出文件
	ReqMethodFilesList ReqMethod = "files.list"
	// ReqMethodFilesGet 获取文件
	ReqMethodFilesGet ReqMethod = "files.get"
	// ReqMethodTTSSynthesize 语音合成
	ReqMethodTTSSynthesize ReqMethod = "tts.synthesize"
	// ReqMethodMemoryCompute 记忆计算
	ReqMethodMemoryCompute ReqMethod = "memory.compute"

	// ─── 浏览器 ───

	// ReqMethodBrowserStart 启动浏览器
	ReqMethodBrowserStart ReqMethod = "browser.start"
	// ReqMethodBrowserRuntimeRestart 重启浏览器运行时
	ReqMethodBrowserRuntimeRestart ReqMethod = "browser.runtime_restart"

	// ─── 配置缓存 / Agent 重载 ───

	// ReqMethodConfigCacheClear 清除配置缓存
	ReqMethodConfigCacheClear ReqMethod = "config.cache_clear"
	// ReqMethodAgentReloadConfig 重载 Agent 配置
	ReqMethodAgentReloadConfig ReqMethod = "agent.reload_config"

	// ─── Agent 管理 ───

	// ReqMethodAgentsList 列出 Agent
	ReqMethodAgentsList ReqMethod = "agents.list"
	// ReqMethodAgentsGet 获取 Agent
	ReqMethodAgentsGet ReqMethod = "agents.get"
	// ReqMethodAgentsCreate 创建 Agent
	ReqMethodAgentsCreate ReqMethod = "agents.create"
	// ReqMethodAgentsUpdate 更新 Agent
	ReqMethodAgentsUpdate ReqMethod = "agents.update"
	// ReqMethodAgentsDelete 删除 Agent
	ReqMethodAgentsDelete ReqMethod = "agents.delete"
	// ReqMethodAgentsEnable 启用 Agent
	ReqMethodAgentsEnable ReqMethod = "agents.enable"
	// ReqMethodAgentsDisable 禁用 Agent
	ReqMethodAgentsDisable ReqMethod = "agents.disable"
	// ReqMethodAgentsToolsList 列出 Agent 工具
	ReqMethodAgentsToolsList ReqMethod = "agents.tools_list"

	// ─── 技能 ───

	// ReqMethodSkillsMarketplaceList 技能市场列表
	ReqMethodSkillsMarketplaceList ReqMethod = "skills.marketplace.list"
	// ReqMethodSkillsList 技能列表
	ReqMethodSkillsList ReqMethod = "skills.list"
	// ReqMethodSkillsInstalled 已安装技能
	ReqMethodSkillsInstalled ReqMethod = "skills.installed"
	// ReqMethodSkillsGet 获取技能
	ReqMethodSkillsGet ReqMethod = "skills.get"
	// ReqMethodSkillsToggle 切换技能
	ReqMethodSkillsToggle ReqMethod = "skills.toggle"
	// ReqMethodSkillsInstall 安装技能
	ReqMethodSkillsInstall ReqMethod = "skills.install"
	// ReqMethodSkillsImportLocal 导入本地技能
	ReqMethodSkillsImportLocal ReqMethod = "skills.import_local"
	// ReqMethodSkillsMarketplaceAdd 从市场添加技能
	ReqMethodSkillsMarketplaceAdd ReqMethod = "skills.marketplace.add"
	// ReqMethodSkillsMarketplaceRemove 从市场移除技能
	ReqMethodSkillsMarketplaceRemove ReqMethod = "skills.marketplace.remove"
	// ReqMethodSkillsMarketplaceToggle 切换市场技能
	ReqMethodSkillsMarketplaceToggle ReqMethod = "skills.marketplace.toggle"
	// ReqMethodSkillsUninstall 卸载技能
	ReqMethodSkillsUninstall ReqMethod = "skills.uninstall"
	// ReqMethodSkillsSkillnetSearch SkillNet 搜索
	ReqMethodSkillsSkillnetSearch ReqMethod = "skills.skillnet.search"
	// ReqMethodSkillsSkillnetInstall SkillNet 安装
	ReqMethodSkillsSkillnetInstall ReqMethod = "skills.skillnet.install"
	// ReqMethodSkillsSkillnetInstallStatus SkillNet 安装状态
	ReqMethodSkillsSkillnetInstallStatus ReqMethod = "skills.skillnet.install_status"
	// ReqMethodSkillsSkillnetEvaluate SkillNet 评估
	ReqMethodSkillsSkillnetEvaluate ReqMethod = "skills.skillnet.evaluate"
	// ReqMethodSkillsClawhubGetToken 获取 ClawHub 令牌
	ReqMethodSkillsClawhubGetToken ReqMethod = "skills.clawhub.get_token"
	// ReqMethodSkillsClawhubSetToken 设置 ClawHub 令牌
	ReqMethodSkillsClawhubSetToken ReqMethod = "skills.clawhub.set_token"
	// ReqMethodSkillsClawhubSearch ClawHub 搜索
	ReqMethodSkillsClawhubSearch ReqMethod = "skills.clawhub.search"
	// ReqMethodSkillsClawhubDownload ClawHub 下载
	ReqMethodSkillsClawhubDownload ReqMethod = "skills.clawhub.download"
	// ReqMethodSkillsTeamSkillsHubInfo TeamSkillsHub 信息
	ReqMethodSkillsTeamSkillsHubInfo ReqMethod = "skills.teamskillshub.info"
	// ReqMethodSkillsTeamSkillsHubInit TeamSkillsHub 初始化
	ReqMethodSkillsTeamSkillsHubInit ReqMethod = "skills.teamskillshub.init"
	// ReqMethodSkillsTeamSkillsHubValidate TeamSkillsHub 校验
	ReqMethodSkillsTeamSkillsHubValidate ReqMethod = "skills.teamskillshub.validate"
	// ReqMethodSkillsTeamSkillsHubPack TeamSkillsHub 打包
	ReqMethodSkillsTeamSkillsHubPack ReqMethod = "skills.teamskillshub.pack"
	// ReqMethodSkillsTeamSkillsHubSearch TeamSkillsHub 搜索
	ReqMethodSkillsTeamSkillsHubSearch ReqMethod = "skills.teamskillshub.search"
	// ReqMethodSkillsTeamSkillsHubInstall TeamSkillsHub 安装
	ReqMethodSkillsTeamSkillsHubInstall ReqMethod = "skills.teamskillshub.install"
	// ReqMethodSkillsTeamSkillsHubPublish TeamSkillsHub 发布
	ReqMethodSkillsTeamSkillsHubPublish ReqMethod = "skills.teamskillshub.publish"
	// ReqMethodSkillsTeamSkillsHubDelete TeamSkillsHub 删除
	ReqMethodSkillsTeamSkillsHubDelete ReqMethod = "skills.teamskillshub.delete"
	// ReqMethodSkillsEvolutionStatus 技能进化状态
	ReqMethodSkillsEvolutionStatus ReqMethod = "skills.evolution.status"
	// ReqMethodSkillsEvolutionGet 获取技能进化
	ReqMethodSkillsEvolutionGet ReqMethod = "skills.evolution.get"
	// ReqMethodSkillsEvolutionSave 保存技能进化
	ReqMethodSkillsEvolutionSave ReqMethod = "skills.evolution.save"

	// ─── 插件 ───

	// ReqMethodPluginsList 插件列表
	ReqMethodPluginsList ReqMethod = "plugins.list"
	// ReqMethodPluginsInstall 安装插件
	ReqMethodPluginsInstall ReqMethod = "plugins.install"
	// ReqMethodPluginsUninstall 卸载插件
	ReqMethodPluginsUninstall ReqMethod = "plugins.uninstall"
	// ReqMethodPluginsEnable 启用插件
	ReqMethodPluginsEnable ReqMethod = "plugins.enable"
	// ReqMethodPluginsDisable 禁用插件
	ReqMethodPluginsDisable ReqMethod = "plugins.disable"
	// ReqMethodPluginsReload 重载插件
	ReqMethodPluginsReload ReqMethod = "plugins.reload"

	// ─── 技能开发 ───

	// ReqMethodSkilldevStart 启动技能开发任务
	ReqMethodSkilldevStart ReqMethod = "skilldev.start"
	// ReqMethodSkilldevRespond 响应挂起点确认
	ReqMethodSkilldevRespond ReqMethod = "skilldev.respond"
	// ReqMethodSkilldevStatus 查询技能开发状态
	ReqMethodSkilldevStatus ReqMethod = "skilldev.status"
	// ReqMethodSkilldevDownload 下载技能开发产物
	ReqMethodSkilldevDownload ReqMethod = "skilldev.download"
	// ReqMethodSkilldevCancel 取消技能开发任务
	ReqMethodSkilldevCancel ReqMethod = "skilldev.cancel"
	// ReqMethodSkilldevFileList 获取工作区文件列表
	ReqMethodSkilldevFileList ReqMethod = "skilldev.file.list"
	// ReqMethodSkilldevFileRead 读取工作区文件内容
	ReqMethodSkilldevFileRead ReqMethod = "skilldev.file.read"

	// ─── 扩展 ───

	// ReqMethodExtensionsList 扩展列表
	ReqMethodExtensionsList ReqMethod = "extensions.list"
	// ReqMethodExtensionsImport 导入扩展
	ReqMethodExtensionsImport ReqMethod = "extensions.import"
	// ReqMethodExtensionsDelete 删除扩展
	ReqMethodExtensionsDelete ReqMethod = "extensions.delete"
	// ReqMethodExtensionsToggle 切换扩展
	ReqMethodExtensionsToggle ReqMethod = "extensions.toggle"

	// ─── 钩子 ───

	// ReqMethodHooksList 钩子列表
	ReqMethodHooksList ReqMethod = "hooks.list"

	// ─── 心跳 ───

	// ReqMethodHeartbeatGetConf 获取心跳配置
	ReqMethodHeartbeatGetConf ReqMethod = "heartbeat.get_conf"
	// ReqMethodHeartbeatSetConf 设置心跳配置
	ReqMethodHeartbeatSetConf ReqMethod = "heartbeat.set_conf"

	// ─── 权限 ───

	// ReqMethodPermissionsToolsGet 获取工具权限
	ReqMethodPermissionsToolsGet ReqMethod = "permissions.tools.get"
	// ReqMethodPermissionsToolsSet 设置工具权限
	ReqMethodPermissionsToolsSet ReqMethod = "permissions.tools.set"
	// ReqMethodPermissionsToolsUpdate 更新工具权限
	ReqMethodPermissionsToolsUpdate ReqMethod = "permissions.tools.update"
	// ReqMethodPermissionsToolsDelete 删除工具权限
	ReqMethodPermissionsToolsDelete ReqMethod = "permissions.tools.delete"
	// ReqMethodPermissionsRulesGet 获取权限规则
	ReqMethodPermissionsRulesGet ReqMethod = "permissions.rules.get"
	// ReqMethodPermissionsRulesCreate 创建权限规则
	ReqMethodPermissionsRulesCreate ReqMethod = "permissions.rules.create"
	// ReqMethodPermissionsRulesUpdate 更新权限规则
	ReqMethodPermissionsRulesUpdate ReqMethod = "permissions.rules.update"
	// ReqMethodPermissionsRulesDelete 删除权限规则
	ReqMethodPermissionsRulesDelete ReqMethod = "permissions.rules.delete"
	// ReqMethodPermissionsApprovalOverridesGet 获取审批覆盖
	ReqMethodPermissionsApprovalOverridesGet ReqMethod = "permissions.approval_overrides.get"
	// ReqMethodPermissionsApprovalOverridesDelete 删除审批覆盖
	ReqMethodPermissionsApprovalOverridesDelete ReqMethod = "permissions.approval_overrides.delete"

	// ─── IM 通道配置 ───

	// ReqMethodChannelFeishuGetConf 获取飞书通道配置
	ReqMethodChannelFeishuGetConf ReqMethod = "channel.feishu.get_conf"
	// ReqMethodChannelFeishuSetConf 设置飞书通道配置
	ReqMethodChannelFeishuSetConf ReqMethod = "channel.feishu.set_conf"
	// ReqMethodChannelXiaoyiGetConf 获取小艺通道配置
	ReqMethodChannelXiaoyiGetConf ReqMethod = "channel.xiaoyi.get_conf"
	// ReqMethodChannelXiaoyiSetConf 设置小艺通道配置
	ReqMethodChannelXiaoyiSetConf ReqMethod = "channel.xiaoyi.set_conf"
	// ReqMethodChannelTelegramGetConf 获取 Telegram 通道配置
	ReqMethodChannelTelegramGetConf ReqMethod = "channel.telegram.get_conf"
	// ReqMethodChannelTelegramSetConf 设置 Telegram 通道配置
	ReqMethodChannelTelegramSetConf ReqMethod = "channel.telegram.set_conf"
	// ReqMethodChannelDingtalkGetConf 获取钉钉通道配置
	ReqMethodChannelDingtalkGetConf ReqMethod = "channel.dingtalk.get_conf"
	// ReqMethodChannelDingtalkSetConf 设置钉钉通道配置
	ReqMethodChannelDingtalkSetConf ReqMethod = "channel.dingtalk.set_conf"
	// ReqMethodChannelWhatsAppGetConf 获取 WhatsApp 通道配置
	ReqMethodChannelWhatsAppGetConf ReqMethod = "channel.whatsapp.get_conf"
	// ReqMethodChannelWhatsAppSetConf 设置 WhatsApp 通道配置
	ReqMethodChannelWhatsAppSetConf ReqMethod = "channel.whatsapp.set_conf"
	// ReqMethodChannelWechatGetConf 获取微信通道配置
	ReqMethodChannelWechatGetConf ReqMethod = "channel.wechat.get_conf"
	// ReqMethodChannelWechatSetConf 设置微信通道配置
	ReqMethodChannelWechatSetConf ReqMethod = "channel.wechat.set_conf"
	// ReqMethodChannelWechatGetLoginUI 获取微信登录界面
	ReqMethodChannelWechatGetLoginUI ReqMethod = "channel.wechat.get_login_ui"
	// ReqMethodChannelWechatUnbind 解绑微信
	ReqMethodChannelWechatUnbind ReqMethod = "channel.wechat.unbind"

	// ─── 更新器 ───

	// ReqMethodUpdaterGetStatus 获取更新器状态
	ReqMethodUpdaterGetStatus ReqMethod = "updater.get_status"
	// ReqMethodUpdaterCheck 检查更新
	ReqMethodUpdaterCheck ReqMethod = "updater.check"
	// ReqMethodUpdaterDownload 下载更新
	ReqMethodUpdaterDownload ReqMethod = "updater.download"
	// ReqMethodUpdaterGetConf 获取更新器配置
	ReqMethodUpdaterGetConf ReqMethod = "updater.get_conf"
	// ReqMethodUpdaterSetConf 设置更新器配置
	ReqMethodUpdaterSetConf ReqMethod = "updater.set_conf"

	// ─── Harness 工具包 ───

	// ReqMethodHarnessPackagesGet 获取 Harness 包
	ReqMethodHarnessPackagesGet ReqMethod = "harness.packages.get"
	// ReqMethodHarnessPackagesScan 扫描 Harness 包
	ReqMethodHarnessPackagesScan ReqMethod = "harness.packages.scan"
	// ReqMethodHarnessPackagesActivate 激活 Harness 包
	ReqMethodHarnessPackagesActivate ReqMethod = "harness.packages.activate"
	// ReqMethodHarnessPackagesDeactivate 停用 Harness 包
	ReqMethodHarnessPackagesDeactivate ReqMethod = "harness.packages.deactivate"
	// ReqMethodHarnessPackagesDelete 删除 Harness 包
	ReqMethodHarnessPackagesDelete ReqMethod = "harness.packages.delete"
	// ReqMethodHarnessPackagesImport 导入 Harness 包
	ReqMethodHarnessPackagesImport ReqMethod = "harness.packages.import"
	// ReqMethodHarnessPackagesExport 导出 Harness 包
	ReqMethodHarnessPackagesExport ReqMethod = "harness.packages.export"

	// ─── 调度 ───

	// ReqMethodScheduleCheckConfig 检查调度配置
	ReqMethodScheduleCheckConfig ReqMethod = "schedule.check_config"
	// ReqMethodScheduleUpdateConfig 更新调度配置
	ReqMethodScheduleUpdateConfig ReqMethod = "schedule.update_config"
	// ReqMethodScheduleCreate 创建调度任务
	ReqMethodScheduleCreate ReqMethod = "schedule.create"
	// ReqMethodScheduleRun 运行调度任务
	ReqMethodScheduleRun ReqMethod = "schedule.run"
	// ReqMethodScheduleList 调度任务列表
	ReqMethodScheduleList ReqMethod = "schedule.list"
	// ReqMethodScheduleStatus 调度任务状态
	ReqMethodScheduleStatus ReqMethod = "schedule.status"
	// ReqMethodScheduleLogs 调度任务日志
	ReqMethodScheduleLogs ReqMethod = "schedule.logs"
	// ReqMethodScheduleCancel 取消调度任务
	ReqMethodScheduleCancel ReqMethod = "schedule.cancel"
	// ReqMethodScheduleDelete 删除调度任务
	ReqMethodScheduleDelete ReqMethod = "schedule.delete"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// reqMethodLookup 字符串值到 ReqMethod 枚举的查找表，用于 ParseReqMethod/IsValid 的 O(1) 查找。
var reqMethodLookup map[string]ReqMethod

// ──────────────────────────── 导出函数 ────────────────────────────

// AllReqMethods 返回所有 ReqMethod 枚举值。
// 用于遍历清理等场景。
func AllReqMethods() []ReqMethod {
	return []ReqMethod{
		// 初始化 / ACP
		ReqMethodInitialize,
		ReqMethodACPToolResponse,
		// 对话核心
		ReqMethodChatSend,
		ReqMethodChatResume,
		ReqMethodChatCancel,
		ReqMethodChatAnswer,
		// 媒体 / 文件域（27.8）
		ReqMethodMediaPersist,
		ReqMethodMediaDiscard,
		ReqMethodDocumentPersist,
		ReqMethodDocumentFormats,
		ReqMethodPathSelectDirectory,
		ReqMethodPathSelectFiles,
		ReqMethodFileImportURL,
		ReqMethodFileUploadChunk,
		ReqMethodFileReadUploadChunk,
		ReqMethodFileDownloadVerifiedChunk,
		// workspace 面板（27-A B2）
		ReqMethodWorkspaceTree,
		ReqMethodWorkspacePreview,
		ReqMethodWorkspaceEntriesDelete,
		ReqMethodWorkspaceUsage,
		// 命令
		ReqMethodCommandAddDir,
		ReqMethodCommandChrome,
		ReqMethodCommandCompact,
		ReqMethodCommandContext,
		ReqMethodCommandRecap,
		ReqMethodCommandDiff,
		ReqMethodCommandGoal,
		ReqMethodCommandMCP,
		ReqMethodCommandModel,
		ReqMethodCommandResume,
		ReqMethodCommandSandbox,
		ReqMethodCommandSession,
		ReqMethodCommandStatus,
		// 配置 / 通道
		ReqMethodConfigGet,
		ReqMethodConfigSet,
		ReqMethodConfigSaveAll,
		ReqMethodConfigValidateModel,
		ReqMethodConfigMoaStatus,
		ReqMethodChannelGet,
		// 模型目录（23.4d）
		ReqMethodModelsGet,
		ReqMethodModelsUpsert,
		ReqMethodModelsDelete,
		ReqMethodModelsReferences,
		ReqMethodModelGroupsList,
		ReqMethodModelGroupsUpsert,
		ReqMethodModelGroupsDelete,
		ReqMethodVendorsList,
		ReqMethodVendorsFetchModels,
		ReqMethodSessionSelectionSet,
		ReqMethodModelsList,
		ReqMethodModelsValidate,
		ReqMethodModelsReplaceAll,
		ReqMethodModelsZenEntries,
		ReqMethodModelsZenWarm,
		ReqMethodLocaleGetConf,
		ReqMethodLocaleSetConf,
		// 会话
		ReqMethodSessionList,
		ReqMethodSessionCreate,
		ReqMethodSessionSwitch,
		ReqMethodSessionDelete,
		ReqMethodSessionRename,
		ReqMethodSessionFork,
		ReqMethodSessionRewind,
		ReqMethodSessionRewindAndRestore,
		ReqMethodSessionRewindContext,
		ReqMethodSessionRestoreFiles,
		ReqMethodSessionPlanStatus,
		// 历史
		ReqMethodHistoryGet,
		ReqMethodHistoryListTurns,
		// 团队
		ReqMethodTeamDelete,
		ReqMethodTeamSnapshot,
		ReqMethodTeamHistoryGet,
		ReqMethodTeamMqPublish,
		// 路径 / 文件 / TTS / 内存
		ReqMethodPathGet,
		ReqMethodPathSet,
		ReqMethodMemoryForbiddenGet,
		ReqMethodMemoryForbiddenSet,
		ReqMethodPermissionsOwnerScopesGet,
		ReqMethodPermissionsOwnerScopesSet,
		ReqMethodFilesList,
		ReqMethodFilesGet,
		ReqMethodTTSSynthesize,
		ReqMethodMemoryCompute,
		// 浏览器
		ReqMethodBrowserStart,
		ReqMethodBrowserRuntimeRestart,
		// 配置缓存 / Agent 重载
		ReqMethodConfigCacheClear,
		ReqMethodAgentReloadConfig,
		// Agent 管理
		ReqMethodAgentsList,
		ReqMethodAgentsGet,
		ReqMethodAgentsCreate,
		ReqMethodAgentsUpdate,
		ReqMethodAgentsDelete,
		ReqMethodAgentsEnable,
		ReqMethodAgentsDisable,
		ReqMethodAgentsToolsList,
		// 技能
		ReqMethodSkillsMarketplaceList,
		ReqMethodSkillsList,
		ReqMethodSkillsInstalled,
		ReqMethodSkillsGet,
		ReqMethodSkillsToggle,
		ReqMethodSkillsInstall,
		ReqMethodSkillsImportLocal,
		ReqMethodSkillsMarketplaceAdd,
		ReqMethodSkillsMarketplaceRemove,
		ReqMethodSkillsMarketplaceToggle,
		ReqMethodSkillsUninstall,
		ReqMethodSkillsSkillnetSearch,
		ReqMethodSkillsSkillnetInstall,
		ReqMethodSkillsSkillnetInstallStatus,
		ReqMethodSkillsSkillnetEvaluate,
		ReqMethodSkillsClawhubGetToken,
		ReqMethodSkillsClawhubSetToken,
		ReqMethodSkillsClawhubSearch,
		ReqMethodSkillsClawhubDownload,
		ReqMethodSkillsTeamSkillsHubInfo,
		ReqMethodSkillsTeamSkillsHubInit,
		ReqMethodSkillsTeamSkillsHubValidate,
		ReqMethodSkillsTeamSkillsHubPack,
		ReqMethodSkillsTeamSkillsHubSearch,
		ReqMethodSkillsTeamSkillsHubInstall,
		ReqMethodSkillsTeamSkillsHubPublish,
		ReqMethodSkillsTeamSkillsHubDelete,
		ReqMethodSkillsEvolutionStatus,
		ReqMethodSkillsEvolutionGet,
		ReqMethodSkillsEvolutionSave,
		// 插件
		ReqMethodPluginsList,
		ReqMethodPluginsInstall,
		ReqMethodPluginsUninstall,
		ReqMethodPluginsEnable,
		ReqMethodPluginsDisable,
		ReqMethodPluginsReload,
		// 技能开发
		ReqMethodSkilldevStart,
		ReqMethodSkilldevRespond,
		ReqMethodSkilldevStatus,
		ReqMethodSkilldevDownload,
		ReqMethodSkilldevCancel,
		ReqMethodSkilldevFileList,
		ReqMethodSkilldevFileRead,
		// 扩展
		ReqMethodExtensionsList,
		ReqMethodExtensionsImport,
		ReqMethodExtensionsDelete,
		ReqMethodExtensionsToggle,
		// 钩子
		ReqMethodHooksList,
		// 心跳
		ReqMethodHeartbeatGetConf,
		ReqMethodHeartbeatSetConf,
		// 权限
		ReqMethodPermissionsToolsGet,
		ReqMethodPermissionsToolsSet,
		ReqMethodPermissionsToolsUpdate,
		ReqMethodPermissionsToolsDelete,
		ReqMethodPermissionsRulesGet,
		ReqMethodPermissionsRulesCreate,
		ReqMethodPermissionsRulesUpdate,
		ReqMethodPermissionsRulesDelete,
		ReqMethodPermissionsApprovalOverridesGet,
		ReqMethodPermissionsApprovalOverridesDelete,
		// IM 通道配置
		ReqMethodChannelFeishuGetConf,
		ReqMethodChannelFeishuSetConf,
		ReqMethodChannelXiaoyiGetConf,
		ReqMethodChannelXiaoyiSetConf,
		ReqMethodChannelTelegramGetConf,
		ReqMethodChannelTelegramSetConf,
		ReqMethodChannelDingtalkGetConf,
		ReqMethodChannelDingtalkSetConf,
		ReqMethodChannelWhatsAppGetConf,
		ReqMethodChannelWhatsAppSetConf,
		ReqMethodChannelWechatGetConf,
		ReqMethodChannelWechatSetConf,
		ReqMethodChannelWechatGetLoginUI,
		ReqMethodChannelWechatUnbind,
		// 更新器
		ReqMethodUpdaterGetStatus,
		ReqMethodUpdaterCheck,
		ReqMethodUpdaterDownload,
		ReqMethodUpdaterGetConf,
		ReqMethodUpdaterSetConf,
		// Harness 工具包
		ReqMethodHarnessPackagesGet,
		ReqMethodHarnessPackagesScan,
		ReqMethodHarnessPackagesActivate,
		ReqMethodHarnessPackagesDeactivate,
		ReqMethodHarnessPackagesDelete,
		ReqMethodHarnessPackagesImport,
		ReqMethodHarnessPackagesExport,
		// 调度
		ReqMethodScheduleCheckConfig,
		ReqMethodScheduleUpdateConfig,
		ReqMethodScheduleCreate,
		ReqMethodScheduleRun,
		ReqMethodScheduleList,
		ReqMethodScheduleStatus,
		ReqMethodScheduleLogs,
		ReqMethodScheduleCancel,
		ReqMethodScheduleDelete,
		// 推荐面板（10.7.2）
		ReqMethodRecommendation,
		ReqMethodPromptSuggestionCancel,
		// 会话素材（session_assets.py）
		ReqMethodSessionAssetsList,
		ReqMethodSessionAssetsRegister,
		ReqMethodSessionAssetsRename,
		ReqMethodSessionAssetsRemove,
		// 项目控制面（project_repository.py）
		ReqMethodProjectList,
		ReqMethodProjectInfo,
		ReqMethodProjectPinnedSessions,
		ReqMethodProjectGetSessions,
		ReqMethodProjectGetCronSessions,
		ReqMethodProjectCronResolveBinding,
		ReqMethodSessionPin,
		// 扩展设备管理（extension_package_manager.py 本地半区）
		ReqMethodAgentTemplatesList,
		ReqMethodAgentTemplatesShow,
		ReqMethodAgentTemplatesFileList,
		ReqMethodAgentTemplatesFileRead,
		ReqMethodAgentTemplatesCreate,
		ReqMethodAgentTemplatesImportLocal,
		ReqMethodAgentTemplatesInstall,
		ReqMethodAgentTemplatesUninstall,
		ReqMethodAgentTemplatesUpdate,
		ReqMethodAgentTemplatesDelete,
		ReqMethodAgentGroupsList,
		ReqMethodAgentGroupsShow,
		ReqMethodAgentGroupsFileList,
		ReqMethodAgentGroupsFileRead,
		ReqMethodAgentGroupsCreate,
		ReqMethodAgentGroupsImportLocal,
		ReqMethodAgentGroupsInstall,
		ReqMethodAgentGroupsUninstall,
		ReqMethodPluginPackagesInstall,
		ReqMethodPluginPackagesUninstall,
	}
}

// ParseReqMethod 从字符串解析 ReqMethod，不合法返回错误。
// 使用包级查找表实现 O(1) 查找，替代 Python 中多处重复的 _parse_req_method() 遍历逻辑。
func ParseReqMethod(s string) (ReqMethod, error) {
	if m, ok := reqMethodLookup[s]; ok {
		return m, nil
	}
	return ReqMethod(""), fmt.Errorf("不合法的 ReqMethod 值: %q", s)
}

// IsValid 判断字符串是否为合法的 ReqMethod 值。
func IsValid(s string) bool {
	_, ok := reqMethodLookup[s]
	return ok
}

// String 实现 fmt.Stringer 接口。
func (m ReqMethod) String() string {
	return string(m)
}

// GoString 实现 fmt.GoStringer 接口，返回带类型名前缀的字符串表示。
func (m ReqMethod) GoString() string {
	return fmt.Sprintf("schema.ReqMethod(%q)", string(m))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	// 构建查找表
	methods := AllReqMethods()
	reqMethodLookup = make(map[string]ReqMethod, len(methods))
	for _, m := range methods {
		reqMethodLookup[string(m)] = m
	}
}

// ──────────────────────────── 常量 ────────────────────────────

// TeamAttachOnlyParam 团队会话被动订阅参数键（chat.send 附加：只观察不提交输入）。
// Python: openjiuwen_protocol.message.TEAM_ATTACH_ONLY_PARAM（协议包常量，
// jiuwenswarm/common/schema/message.py re-export）。
const TeamAttachOnlyParam = "team_attach_only"
