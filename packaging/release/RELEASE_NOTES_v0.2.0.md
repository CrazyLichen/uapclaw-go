## UapClaw v0.2.0

自 v0.1.2 以来的 31 个提交，两大主题：**日志输出改为文本行（带调用点）**、**出厂 `config.yaml` 模板补全**，另有若干链路修复。

### 一、日志：统一文本格式，带 component 与调用点

控制台与各日志文件不再输出 JSON，改为单行文本，便于 `grep`/`tail` 直接读：

```
[2026-11-02 14:41:38.322] [INFO] [common] [extensions/loader.go:127] [ExtensionLoader] 开始搜索扩展路径 event_type=extension_loader_discover search_paths=null
```

- 字段：时间 / 级别 / component / **调用点文件:行** / 消息 / 结构化键值
- 消息内换行与制表符转义为单行；敏感字段（api_key 等）自动脱敏
- 遵循 `NO_COLOR`（非 TTY 时不输出颜色）
- 等级与各通道（common/gateway/channel/agent_server/permissions/agent_core/full）仍可分别配置

> ⚠️ **升级注意**：如果你有脚本按 JSON 解析日志，需要改成按上面的文本格式解析（`logging` 段只保留级别配置，不再有 `format` 开关）。

### 二、出厂 `config.yaml` 模板补全（+333 行）

首次安装生成的配置从「只有 server/logging/workspace」扩到完整骨架，且每段都带注释说明默认值来源：

`logging`、`gateway`、`heartbeat`、`memory`、`embed`、`models`、`react`、`agents`、`acp_agents`、`modes`、`team`、`channels`、`permissions`、`hooks`、`mcp`、`browser`、`extensions`。

同时新增**防漂移测试**（`config_yaml_parity_test`）：代码默认值与模板注释不一致时 CI 直接红，避免文档和实现再次各说各话。`logging.format` 这个 Python 模板里不存在的死键已删除。

### 三、修复

| 范围 | 内容 |
| --- | --- |
| 模型配置 | `hasValidModelConfig` 补齐 `config.yaml` 兜底（第 4 步），未配置模型时不再向占位地址发真实请求 |
| 适配器 | `resolveEnableTaskLoop` 补上 `skill_create` 强制覆盖（对齐 Python） |
| team | 回填中断恢复死链 + 共享状态加锁（S-01/S-02/S-03/M-02/M-05/M-07/M-08/T-04/T-10） |
| 流式事件 | `tool_call` 事件层改回原样透传（对齐 Python，M-01） |
| pregel | 新增 `PregelNode` / `NodeInput` / `NodeFunc` / `addRouter`；nil 目标改 fail-closed |
| web | 路径按「已存在前缀」解析符号链接；`res` 帧形状收敛 |
| 测试/CI | 新增 agentteams 中断恢复集成用例；覆盖率门禁改为「改动行」口径（`scripts/check-changed-line-coverage.sh`） |

### 升级方式

保留数据目录（Linux `~/.uapclaw/`，Windows `%USERPROFILE%\.uapclaw\`），覆盖程序目录即可。启动后 `uapclaw --version` 应为 `0.2.0`。

已有配置**不会被覆盖**；想用新模板的完整注释骨架，可对比 `resources/config.yaml` 自行合并（程序对缺失段有代码默认值，不合并也能跑）。

### 已知问题（与 v0.1.2 相同）

- 心跳每 60s 新建一个 `~/.uapclaw/agent/sessions/heartbeat_*` 目录且不清理（Go/Python 同构缺陷）。缓解：`HEARTBEAT_INTERVAL=3600` 或 `heartbeat.every: 3600`
- `chat` / `serve` 子命令仍为占位实现，Web 界面是当前唯一完整入口
