# ReqMethod 枚举实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 E2A 协议 RPC 方法名枚举 ReqMethod，全量定义 82 个常量，提供 ParseReqMethod/IsValid/AllReqMethods 等辅助方法。

**Architecture:** 采用 `type ReqMethod string` 字符串枚举，与项目已有 AgentCallbackEvent 模式一致。82 个常量按功能分组注释分隔在单文件 req_method.go 中，包级 map 查找表实现 O(1) 解析。

**Tech Stack:** Go 1.26, encoding/json, fmt, strconv, sync（查找表初始化）

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 创建 | `internal/swarm/schema/doc.go` | 包文档，描述 swarm/schema 包功能 |
| 创建 | `internal/swarm/schema/req_method.go` | ReqMethod 类型 + 82 个常量 + 查找表 + 辅助方法 |
| 创建 | `internal/swarm/schema/req_method_test.go` | 单元测试（8 个测试函数） |
| 修改 | `IMPLEMENTATION_PLAN.md` | 10.1.1 状态 ☐ → ✅ |

---

### Task 1: 创建目录结构 + doc.go

**Files:**
- Create: `internal/swarm/schema/doc.go`

- [ ] **Step 1: 创建 swarm/schema 目录**

```bash
mkdir -p internal/swarm/schema
```

- [ ] **Step 2: 创建 doc.go**

```go
// Package schema 提供 E2A 协议和 Gateway/AgentServer 通信所需的全部类型定义。
//
// 本包定义了 E2A 协议的核心数据模型，包括 RPC 方法名枚举（ReqMethod）、
// 事件类型枚举（EventType）、运行模式枚举（Mode）、消息模型（Message）、
// Agent 请求/响应模型等，作为 swarm 层的类型基础。
//
// 文件目录：
//
//	schema/
//	├── doc.go           # 包文档
//	└── req_method.go    # ReqMethod 枚举（~82 个 RPC 方法名）
//
// 对应 Python 代码：jiuwenswarm/common/schema/
package schema
```

- [ ] **Step 3: 验证编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/schema/`
Expected: 编译通过，无错误

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/schema/doc.go
git commit -m "feat(swarm/schema): 添加包文档 doc.go"
```

---

### Task 2: 实现 req_method.go — 枚举类型 + 全量常量

**Files:**
- Create: `internal/swarm/schema/req_method.go`

- [ ] **Step 1: 创建 req_method.go，包含完整的枚举类型和 82 个常量定义**

```go
package schema

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ReqMethod E2A 协议 RPC 方法名枚举。
//
// 定义 Gateway↔AgentServer 通信链路中所有合法的 RPC 方法标识，
// 用于 E2AEnvelope.method 字段和 AgentServer 方法路由分发。
// 值为点分字符串格式（如 "chat.send"），与 Python ReqMethod 枚举值一一对应。
//
// 对应 Python: jiuwenswarm/common/schema/message.py (ReqMethod)
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

	// ─── 命令 ───

	// ReqMethodCommandAddDir 添加目录
	ReqMethodCommandAddDir ReqMethod = "command.add_dir"
	// ReqMethodCommandChrome Chrome 控制
	ReqMethodCommandChrome ReqMethod = "command.chrome"
	// ReqMethodCommandCompact 压缩上下文
	ReqMethodCommandCompact ReqMethod = "command.compact"
	// ReqMethodCommandContext 上下文信息
	ReqMethodCommandContext ReqMethod = "command.context"
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
	// ReqMethodChannelGet 获取通道信息
	ReqMethodChannelGet ReqMethod = "channel.get"

	// ─── 会话 ───

	// ReqMethodSessionList 会话列表
	ReqMethodSessionList ReqMethod = "session.list"
	// ReqMethodSessionCreate 创建会话
	ReqMethodSessionCreate ReqMethod = "session.create"
	// ReqMethodSessionSwitch 切换会话
	ReqMethodSessionSwitch ReqMethod = "session.switch"
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

	// ─── 路径 / 文件 / TTS / 内存 ───

	// ReqMethodPathGet 获取路径
	ReqMethodPathGet ReqMethod = "path.get"
	// ReqMethodPathSet 设置路径
	ReqMethodPathSet ReqMethod = "path.set"
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

	// ─── Harness ───

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

// ──────────────────────────── 常量 ────────────────────────────

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
		// 命令
		ReqMethodCommandAddDir,
		ReqMethodCommandChrome,
		ReqMethodCommandCompact,
		ReqMethodCommandContext,
		ReqMethodCommandRecap,
		ReqMethodCommandDiff,
		ReqMethodCommandMCP,
		ReqMethodCommandModel,
		ReqMethodCommandResume,
		ReqMethodCommandSandbox,
		ReqMethodCommandSession,
		ReqMethodCommandStatus,
		// 配置 / 通道
		ReqMethodConfigGet,
		ReqMethodConfigSet,
		ReqMethodChannelGet,
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
		// 历史
		ReqMethodHistoryGet,
		ReqMethodHistoryListTurns,
		// 团队
		ReqMethodTeamDelete,
		ReqMethodTeamSnapshot,
		ReqMethodTeamHistoryGet,
		// 路径 / 文件 / TTS / 内存
		ReqMethodPathGet,
		ReqMethodPathSet,
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
		// Harness
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
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/schema/`
Expected: 编译通过，无错误

- [ ] **Step 3: Commit**

```bash
git add internal/swarm/schema/req_method.go
git commit -m "feat(swarm/schema): 实现 ReqMethod 枚举（82 个 RPC 方法名）"
```

---

### Task 3: 实现单元测试 req_method_test.go

**Files:**
- Create: `internal/swarm/schema/req_method_test.go`

- [ ] **Step 1: 创建 req_method_test.go**

```go
package schema

import (
	"encoding/json"
	"testing"
)

// TestAllReqMethods 验证 AllReqMethods 返回全部 82 个枚举值
func TestAllReqMethods(t *testing.T) {
	methods := AllReqMethods()
	if len(methods) != 82 {
		t.Fatalf("AllReqMethods() 返回 %d 个方法，want 82", len(methods))
	}

	// 验证无重复
	seen := make(map[ReqMethod]bool)
	for _, m := range methods {
		if seen[m] {
			t.Errorf("重复方法: %q", m)
		}
		seen[m] = true
	}

	// 验证包含关键方法
	keyMethods := []ReqMethod{
		ReqMethodInitialize,
		ReqMethodChatSend,
		ReqMethodChatResume,
		ReqMethodChatCancel,
		ReqMethodChatAnswer,
		ReqMethodHistoryGet,
		ReqMethodSessionList,
		ReqMethodConfigGet,
	}
	for _, km := range keyMethods {
		if !seen[km] {
			t.Errorf("缺少关键方法: %q", km)
		}
	}
}

// TestParseReqMethod_合法值 验证解析合法值成功
func TestParseReqMethod_合法值(t *testing.T) {
	tests := []struct {
		input string
		want  ReqMethod
	}{
		{"initialize", ReqMethodInitialize},
		{"chat.send", ReqMethodChatSend},
		{"chat.interrupt", ReqMethodChatCancel},
		{"chat.user_answer", ReqMethodChatAnswer},
		{"config.get", ReqMethodConfigGet},
		{"session.list", ReqMethodSessionList},
		{"skills.list", ReqMethodSkillsList},
		{"permissions.tools.get", ReqMethodPermissionsToolsGet},
		{"schedule.create", ReqMethodScheduleCreate},
		{"harness.packages.scan", ReqMethodHarnessPackagesScan},
	}
	for _, tt := range tests {
		got, err := ParseReqMethod(tt.input)
		if err != nil {
			t.Errorf("ParseReqMethod(%q) 返回错误: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseReqMethod(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestParseReqMethod_非法值 验证解析非法值返回错误
func TestParseReqMethod_非法值(t *testing.T) {
	invalidInputs := []string{
		"",
		"unknown.method",
		"chat.",
		".send",
		"CHAT_SEND",
		"chat/send",
		"foo.bar.baz.qux",
	}
	for _, input := range invalidInputs {
		_, err := ParseReqMethod(input)
		if err == nil {
			t.Errorf("ParseReqMethod(%q) 应返回错误，但返回 nil", input)
		}
	}
}

// TestIsValid 验证 IsValid 对合法/非法值的判断
func TestIsValid(t *testing.T) {
	// 合法值
	if !IsValid("chat.send") {
		t.Error(`IsValid("chat.send") = false, want true`)
	}
	if !IsValid("initialize") {
		t.Error(`IsValid("initialize") = false, want true`)
	}
	if !IsValid("permissions.approval_overrides.delete") {
		t.Error(`IsValid("permissions.approval_overrides.delete") = false, want true`)
	}

	// 非法值
	if IsValid("") {
		t.Error(`IsValid("") = true, want false`)
	}
	if IsValid("nonexistent.method") {
		t.Error(`IsValid("nonexistent.method") = true, want false`)
	}
}

// TestReqMethodString 验证 String() 返回原始字符串值
func TestReqMethodString(t *testing.T) {
	if got := ReqMethodChatSend.String(); got != "chat.send" {
		t.Errorf("ReqMethodChatSend.String() = %q, want %q", got, "chat.send")
	}
	if got := ReqMethodChatCancel.String(); got != "chat.interrupt" {
		t.Errorf("ReqMethodChatCancel.String() = %q, want %q", got, "chat.interrupt")
	}
}

// TestReqMethodGoString 验证 GoString() 格式
func TestReqMethodGoString(t *testing.T) {
	if got := ReqMethodChatSend.GoString(); got != `schema.ReqMethod("chat.send")` {
		t.Errorf("ReqMethodChatSend.GoString() = %q, want %q", got, `schema.ReqMethod("chat.send")`)
	}
}

// TestReqMethodJSON序列化往返 验证 JSON marshal/unmarshal 往返一致
func TestReqMethodJSON序列化往返(t *testing.T) {
	methods := []ReqMethod{
		ReqMethodInitialize,
		ReqMethodChatSend,
		ReqMethodChatCancel,
		ReqMethodSessionCreate,
		ReqMethodScheduleDelete,
	}
	for _, m := range methods {
		data, err := json.Marshal(m)
		if err != nil {
			t.Errorf("json.Marshal(%q) 错误: %v", m, err)
			continue
		}
		var got ReqMethod
		if err := json.Unmarshal(data, &got); err != nil {
			t.Errorf("json.Unmarshal(%s) 错误: %v", data, err)
			continue
		}
		if got != m {
			t.Errorf("JSON 往返: got %q, want %q", got, m)
		}
	}
}

// TestReqMethod常量值与Python对齐 验证核心常量字符串值与 Python ReqMethod 完全对齐
func TestReqMethod常量值与Python对齐(t *testing.T) {
	// 对应 Python: jiuwenswarm/common/schema/message.py (ReqMethod)
	tests := []struct {
		got  ReqMethod
		want string
	}{
		{ReqMethodInitialize, "initialize"},
		{ReqMethodACPToolResponse, "acp.tool_response"},
		{ReqMethodChatSend, "chat.send"},
		{ReqMethodChatResume, "chat.resume"},
		{ReqMethodChatCancel, "chat.interrupt"},
		{ReqMethodChatAnswer, "chat.user_answer"},
		{ReqMethodCommandAddDir, "command.add_dir"},
		{ReqMethodCommandChrome, "command.chrome"},
		{ReqMethodCommandCompact, "command.compact"},
		{ReqMethodCommandContext, "command.context"},
		{ReqMethodCommandRecap, "command.recap"},
		{ReqMethodCommandDiff, "command.diff"},
		{ReqMethodCommandMCP, "command.mcp"},
		{ReqMethodCommandModel, "command.model"},
		{ReqMethodCommandResume, "command.resume"},
		{ReqMethodCommandSandbox, "command.sandbox"},
		{ReqMethodCommandSession, "command.session"},
		{ReqMethodCommandStatus, "command.status"},
		{ReqMethodConfigGet, "config.get"},
		{ReqMethodConfigSet, "config.set"},
		{ReqMethodChannelGet, "channel.get"},
		{ReqMethodSessionList, "session.list"},
		{ReqMethodSessionCreate, "session.create"},
		{ReqMethodSessionSwitch, "session.switch"},
		{ReqMethodSessionDelete, "session.delete"},
		{ReqMethodSessionRename, "session.rename"},
		{ReqMethodSessionFork, "session.fork"},
		{ReqMethodSessionRewind, "session.rewind"},
		{ReqMethodSessionRewindAndRestore, "session.rewind_and_restore"},
		{ReqMethodSessionRewindContext, "session.rewind_context"},
		{ReqMethodSessionRestoreFiles, "session.restore_files"},
		{ReqMethodHistoryGet, "history.get"},
		{ReqMethodHistoryListTurns, "history.list_turns"},
		{ReqMethodTeamDelete, "team.delete"},
		{ReqMethodTeamSnapshot, "team.snapshot"},
		{ReqMethodTeamHistoryGet, "team.history.get"},
		{ReqMethodPathGet, "path.get"},
		{ReqMethodPathSet, "path.set"},
		{ReqMethodFilesList, "files.list"},
		{ReqMethodFilesGet, "files.get"},
		{ReqMethodTTSSynthesize, "tts.synthesize"},
		{ReqMethodMemoryCompute, "memory.compute"},
		{ReqMethodBrowserStart, "browser.start"},
		{ReqMethodBrowserRuntimeRestart, "browser.runtime_restart"},
		{ReqMethodConfigCacheClear, "config.cache_clear"},
		{ReqMethodAgentReloadConfig, "agent.reload_config"},
		{ReqMethodAgentsList, "agents.list"},
		{ReqMethodAgentsGet, "agents.get"},
		{ReqMethodAgentsCreate, "agents.create"},
		{ReqMethodAgentsUpdate, "agents.update"},
		{ReqMethodAgentsDelete, "agents.delete"},
		{ReqMethodAgentsEnable, "agents.enable"},
		{ReqMethodAgentsDisable, "agents.disable"},
		{ReqMethodAgentsToolsList, "agents.tools_list"},
		{ReqMethodSkillsMarketplaceList, "skills.marketplace.list"},
		{ReqMethodSkillsList, "skills.list"},
		{ReqMethodSkillsInstalled, "skills.installed"},
		{ReqMethodSkillsGet, "skills.get"},
		{ReqMethodSkillsToggle, "skills.toggle"},
		{ReqMethodSkillsInstall, "skills.install"},
		{ReqMethodSkillsImportLocal, "skills.import_local"},
		{ReqMethodSkillsMarketplaceAdd, "skills.marketplace.add"},
		{ReqMethodSkillsMarketplaceRemove, "skills.marketplace.remove"},
		{ReqMethodSkillsMarketplaceToggle, "skills.marketplace.toggle"},
		{ReqMethodSkillsUninstall, "skills.uninstall"},
		{ReqMethodSkillsSkillnetSearch, "skills.skillnet.search"},
		{ReqMethodSkillsSkillnetInstall, "skills.skillnet.install"},
		{ReqMethodSkillsSkillnetInstallStatus, "skills.skillnet.install_status"},
		{ReqMethodSkillsSkillnetEvaluate, "skills.skillnet.evaluate"},
		{ReqMethodSkillsClawhubGetToken, "skills.clawhub.get_token"},
		{ReqMethodSkillsClawhubSetToken, "skills.clawhub.set_token"},
		{ReqMethodSkillsClawhubSearch, "skills.clawhub.search"},
		{ReqMethodSkillsClawhubDownload, "skills.clawhub.download"},
		{ReqMethodSkillsTeamSkillsHubInfo, "skills.teamskillshub.info"},
		{ReqMethodSkillsTeamSkillsHubInit, "skills.teamskillshub.init"},
		{ReqMethodSkillsTeamSkillsHubValidate, "skills.teamskillshub.validate"},
		{ReqMethodSkillsTeamSkillsHubPack, "skills.teamskillshub.pack"},
		{ReqMethodSkillsTeamSkillsHubSearch, "skills.teamskillshub.search"},
		{ReqMethodSkillsTeamSkillsHubInstall, "skills.teamskillshub.install"},
		{ReqMethodSkillsTeamSkillsHubPublish, "skills.teamskillshub.publish"},
		{ReqMethodSkillsTeamSkillsHubDelete, "skills.teamskillshub.delete"},
		{ReqMethodSkillsEvolutionStatus, "skills.evolution.status"},
		{ReqMethodSkillsEvolutionGet, "skills.evolution.get"},
		{ReqMethodSkillsEvolutionSave, "skills.evolution.save"},
		{ReqMethodPluginsList, "plugins.list"},
		{ReqMethodPluginsInstall, "plugins.install"},
		{ReqMethodPluginsUninstall, "plugins.uninstall"},
		{ReqMethodPluginsEnable, "plugins.enable"},
		{ReqMethodPluginsDisable, "plugins.disable"},
		{ReqMethodPluginsReload, "plugins.reload"},
		{ReqMethodExtensionsList, "extensions.list"},
		{ReqMethodExtensionsImport, "extensions.import"},
		{ReqMethodExtensionsDelete, "extensions.delete"},
		{ReqMethodExtensionsToggle, "extensions.toggle"},
		{ReqMethodHooksList, "hooks.list"},
		{ReqMethodHeartbeatGetConf, "heartbeat.get_conf"},
		{ReqMethodHeartbeatSetConf, "heartbeat.set_conf"},
		{ReqMethodPermissionsToolsGet, "permissions.tools.get"},
		{ReqMethodPermissionsToolsSet, "permissions.tools.set"},
		{ReqMethodPermissionsToolsUpdate, "permissions.tools.update"},
		{ReqMethodPermissionsToolsDelete, "permissions.tools.delete"},
		{ReqMethodPermissionsRulesGet, "permissions.rules.get"},
		{ReqMethodPermissionsRulesCreate, "permissions.rules.create"},
		{ReqMethodPermissionsRulesUpdate, "permissions.rules.update"},
		{ReqMethodPermissionsRulesDelete, "permissions.rules.delete"},
		{ReqMethodPermissionsApprovalOverridesGet, "permissions.approval_overrides.get"},
		{ReqMethodPermissionsApprovalOverridesDelete, "permissions.approval_overrides.delete"},
		{ReqMethodChannelFeishuGetConf, "channel.feishu.get_conf"},
		{ReqMethodChannelFeishuSetConf, "channel.feishu.set_conf"},
		{ReqMethodChannelXiaoyiGetConf, "channel.xiaoyi.get_conf"},
		{ReqMethodChannelXiaoyiSetConf, "channel.xiaoyi.set_conf"},
		{ReqMethodChannelTelegramGetConf, "channel.telegram.get_conf"},
		{ReqMethodChannelTelegramSetConf, "channel.telegram.set_conf"},
		{ReqMethodChannelDingtalkGetConf, "channel.dingtalk.get_conf"},
		{ReqMethodChannelDingtalkSetConf, "channel.dingtalk.set_conf"},
		{ReqMethodChannelWhatsAppGetConf, "channel.whatsapp.get_conf"},
		{ReqMethodChannelWhatsAppSetConf, "channel.whatsapp.set_conf"},
		{ReqMethodChannelWechatGetConf, "channel.wechat.get_conf"},
		{ReqMethodChannelWechatSetConf, "channel.wechat.set_conf"},
		{ReqMethodChannelWechatGetLoginUI, "channel.wechat.get_login_ui"},
		{ReqMethodChannelWechatUnbind, "channel.wechat.unbind"},
		{ReqMethodUpdaterGetStatus, "updater.get_status"},
		{ReqMethodUpdaterCheck, "updater.check"},
		{ReqMethodUpdaterDownload, "updater.download"},
		{ReqMethodUpdaterGetConf, "updater.get_conf"},
		{ReqMethodUpdaterSetConf, "updater.set_conf"},
		{ReqMethodHarnessPackagesGet, "harness.packages.get"},
		{ReqMethodHarnessPackagesScan, "harness.packages.scan"},
		{ReqMethodHarnessPackagesActivate, "harness.packages.activate"},
		{ReqMethodHarnessPackagesDeactivate, "harness.packages.deactivate"},
		{ReqMethodHarnessPackagesDelete, "harness.packages.delete"},
		{ReqMethodHarnessPackagesImport, "harness.packages.import"},
		{ReqMethodHarnessPackagesExport, "harness.packages.export"},
		{ReqMethodScheduleCheckConfig, "schedule.check_config"},
		{ReqMethodScheduleUpdateConfig, "schedule.update_config"},
		{ReqMethodScheduleCreate, "schedule.create"},
		{ReqMethodScheduleRun, "schedule.run"},
		{ReqMethodScheduleList, "schedule.list"},
		{ReqMethodScheduleStatus, "schedule.status"},
		{ReqMethodScheduleLogs, "schedule.logs"},
		{ReqMethodScheduleCancel, "schedule.cancel"},
		{ReqMethodScheduleDelete, "schedule.delete"},
	}
	for _, tt := range tests {
		if string(tt.got) != tt.want {
			t.Errorf("常量值 = %q, want %q", tt.got, tt.want)
		}
	}
	// 验证测试覆盖全部 82 个常量
	if len(tests) != 82 {
		t.Errorf("Python 对齐测试覆盖 %d 个常量，want 82", len(tests))
	}
}
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test -v ./internal/swarm/schema/`
Expected: 全部 8 个测试 PASS

- [ ] **Step 3: Commit**

```bash
git add internal/swarm/schema/req_method_test.go
git commit -m "test(swarm/schema): 添加 ReqMethod 枚举单元测试（8 个测试函数）"
```

---

### Task 4: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md:616` (10.1.1 行)

- [ ] **Step 1: 将 10.1.1 状态从 ☐ 更新为 ✅**

在 IMPLEMENTATION_PLAN.md 中找到：
```
| 10.1.1 | ☐ | ReqMethod 枚举 | ~100 个 RPC 方法名 | `jiuwenswarm/common/schema/message.py` (ReqMethod) |
```
替换为：
```
| 10.1.1 | ✅ | ReqMethod 枚举 | ~100 个 RPC 方法名 | `jiuwenswarm/common/schema/message.py` (ReqMethod) |
```

- [ ] **Step 2: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 10.1.1 ReqMethod 枚举状态为 ✅"
```

---

### Task 5: 运行测试覆盖率检查

- [ ] **Step 1: 运行覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/swarm/schema/`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 2: 确认通过**

如果覆盖率不足，补充测试用例。预期覆盖率应接近 100%（所有方法和常量都有测试覆盖）。
