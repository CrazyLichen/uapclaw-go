## UapClaw v0.3.3

自 v0.3.2 以来的 23 个提交，无破坏性改动，可直接覆盖升级。本版两条主线：**Web 控制台整体换成上游最新前端**，以及**动态提示词附件（Prompt Attachment）地基落地**——后者是与上游对齐的架构改造第一步，本版只建地基、不改现有提示词行为。

### 一、Web 控制台：整体升级到上游 0.2.5 前端

- 前端源码由旧基线（134 文件）整体替换为上游最新版本（1292 文件），界面从单会话版升级为上游重构版：多会话运行时、设置面板、轨迹面板、设计器、项目维度侧边栏、插件/技能市场等
- 构建产物 `dist` 与源码一并入库，`go:embed` 方式不变；`git clone` 后直接 `go build` 即得带前端的二进制（CI 仍会 `npm ci && npm run build` 重建，产物可复现）
- 上游 0.2.5 已把品牌改为 WorkSwarm，本仓库统一重放为 **UapClaw**：界面文案、`localStorage`/`sessionStorage` 键、自定义事件名、`~/.uapclaw` 路径提示、`UAPCLAW_*` 环境变量、分享图文件名与仓库链接
- 后端契约字段与遥测键**保持原样**（`jiuwenclaw_id`/`jiuwenclaw_name`/`initialized_by_jiuwenswarm`、`openjiuwen.*` 轨迹属性），避免与后端/轨迹协议脱钩
- 剥离 Python 仓库依赖：删除 `predev`/`prebuild`（`generate-agent-folders.js`）与两个引用 `../extensions/video_duplex` 的测试脚本

**Go 侧此前的前端补丁处置（逐项对照上游源码核实）**

| 本地补丁 | 上游现状 | 处置 |
| --- | --- | --- |
| 思考过程块渲染（`reasoning_content`） | 已实现且更完整（多会话运行时 + 思考分段 + 时间线泳道） | 删除本地实现，采用上游 |
| 消息 id 同毫秒撞车修复 | 上游以 `rsn-<时间戳>-<序号>-<随机>` 覆盖同一场景 | 删除本地实现 |
| 模型输入/输出上限输入框 | 上游无此字段 | **重新合入**：模型对话框新增两项输入 + 非负整数校验，写入 `model_client_config.max_input_tokens`/`max_output_tokens`（0 或留空 = 未配置） |

### 二、Prompt Attachment 地基（S1）

上游把动态提示词（plan 指引、todo 提醒、专家角色、技能披露规则）从「每轮改写 system prompt」改成「作为会话历史消息投递」，带内容哈希做增量与恢复。本版在 Go 侧建成与上游等价的地基：

- **会话级 section 存储**：增删改查、按来源清空、过期剔除、稳定排序、跨会话查询、并发安全
- **三态渲染**：`snapshot` / `delta` / `removed`，`<system-reminder>` 外壳，中英文案逐字对齐上游；两级截断（单节 12000 字符、整体 48000 字符）并保证 CJK 安全
- **内容哈希**：规范化 JSON + sha256，只覆盖模型可见内容（优先级/来源/时间戳不参与），内容未变不产生新消息
- **两个装订点**：写用户消息之前（保证 `[附件变更][用户消息]` 顺序）、构建模型窗口之前（覆盖「本轮无新用户消息但 rail 改了附件」）；`Sync` 幂等，失败只告警不阻断模型调用
- **窗口投影**：`ContextWindow` 增加 mutator 链，attachment mutator 按 provider 决定投递角色（百炼/百炼兼容走 System，Anthropic 与其它保持 User）；投影只作用于本次请求，磁盘历史始终是 UserMessage
- **metadata 契约**：消息四键（身份标记 / `snapshot|delta` / `session_id` / `{section: hash}` 账本），供后续 S2/S3 与轨迹、观测共用
- **测试**：7 例集成测试（全链路、顺序契约、压缩回退、重启恢复、投影矩阵、隔离、跨轮插入）+ 各环节单测；导出方法统一 `recover`，panic 转 error 并打 `event_type=prompt_attachment_panic`

> 说明：S1 明确**不迁移任何消费方**，本版提示词行为与 v0.3.2 一致；rail 改投递、模式合并等在 S2/S3。

### 三、文档与计划

- 上游同步扩充章节：领域十五~二十九（harness 协议、symphony、rsi、cua、AgentOS、process-cli、权限模式、可观测性、LLM/多模态、context/memory、team/swarm、渠道、Web 后端契约、TUI/技能、交付运维，共 156 步）
- 新增 Prompt Attachment 地基设计、S2 rail/模式合并设计与实施计划、S3 跨层契约设计

### 升级提示

- 无配置项改名或删除，旧 `config.yaml` 可直接使用；S1 地基不改变现有提示词行为
- **Web 控制台后端契约尚未补齐（重要）**：新前端调用 163 个 RPC，Go 后端目前实现约 75 个；而 `models.*` / `cron.job.*` / `heartbeat.job.*` 等命名已随上游变化。聊天主链路的请求方法（`chat.send` / `history.get` / `session.list`）与核心事件（`chat.delta` / `chat.reasoning` / `chat.final` / `chat.processing_status` / `connection.ack`）都在，但上游新增的 `runtime.accepted` / `chat.output_phase` 等时序事件尚未发出。结果：**对话可用，配置保存、项目管理、设计器、轨迹面板等会报「方法未注册」，部分时序展示（输入盖章时间、输出阶段）会降级**。对应补齐工作是 IMPLEMENTATION_PLAN 领域二十七，完成前不建议把本版 Web 控制台当作完整形态对外分发
- 需要回到 v0.3.2 的旧界面时无需回退二进制：`git checkout v0.3.2` 后在该版本的前端目录执行 `npm ci && npm run build`，用 `--dist <该 dist 目录>` 启动，或把产物放到 `<数据根>/channels/web/frontend/dist`（工作区产物优先于内置产物）
- 发布包体积基本持平：前端 dist 虽由约 23MB 增至 30MB，但新版构建产物压缩率更好，Linux/Windows 包各约 49MB（较 v0.3.2 略小约 0.9MB）
