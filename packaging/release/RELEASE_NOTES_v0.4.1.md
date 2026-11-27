# uapclaw-go v0.4.1 发布说明

v0.4.0 的补丁版本，共 **9 个提交**，聚焦桌面/自托管体验修复与两处用户可见 UI 调整。

## 修复

1. **"无法通过 WebSocket 获取后端配置"**：config/permissions 类 RPC 经网关转发时 request_id 固定为空串，页面加载并发 `config.get` + `models.list` 时第二个请求被防重守卫拒绝（4c4ee41f4）
2. **模型设置页 "unknown method: models.get"**：23.4d 模型目录 10 个 web method（models.get/upsert/delete/references、model_groups.*、session.selection.set、vendors.*）AgentServer 侧已实现但网关漏注册（01f3ffa15）
3. **校验模型报 "[181002] 模型客户端配置 client_id 为空"**：探活用 ModelClientConfig 漏 client_id，探测预算同步对齐 Python（timeout 25s / max_retries 0）（2534ec111）
4. **RSI 优化平台页 "unknown method: rsi.task.list" 等**：rsi.* 18 个 web method 补 WS 注册（1c16f370e）
5. **"@name 引用素材" unknown method**：session.assets.list/register/rename/remove 四方法整域补移植（516e5e755，对齐 Python session_assets.py：client_id 幂等/generated 路径去重/name_history 占名）
6. **新建任务页看不到历史任务**：project.* 项目控制面只读族（project.list/info/get_sessions/get_cron_sessions/pinned_sessions）整域补移植（2da1c9d14 + ef40e097b，对齐 Python project_repository/project_queries/project_store 读路径；AllReqMethods 查找表补十常量）。已知挂账：project.create/rename/pin/remove/restore、sessions.archive 族、project.git.* 待后续版本

## 变更

5. **默认监听地址改为 0.0.0.0**（19000 Web 界面与 19001 Gateway）：出厂即局域网可访问；环境变量 `UAPCLAW_WEB_HOST` / `UAPCLAW_GATEWAY_HOST` 或 `--host` 参数仍可覆盖（130930bbd）
6. **界面调整**（1c16f370e）：
   - 移除"更新"侧边栏页签
   - 对话中助手显示名 Jiuwen → UapClaw
   - 移除欢迎区悬浮提示气泡（含"活动"限时免费推广）

## 已知限制

- 智能体设计页的 agent_templates.*/agent_groups.* 系列方法（模板/分组管理）后端尚未实现，相关操作暂不可用，将随后续版本补齐。

## 资产

- `uapclaw-0.4.1-linux-amd64.tar.gz`（内嵌 Web 前端，解压即用）
- `uapclaw-0.4.1-windows-amd64.zip`
- `sha256sums.txt`
