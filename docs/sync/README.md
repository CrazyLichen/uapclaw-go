# 上游差异分析报告（jiuwenswarm / openjiuwen-agent-core）

> 生成时间：2026-11-04（基于 2026-11-03 上游 `develop` 分支快照）
> 对比阶段未修改本地源码，所有分析均在临时克隆目录 `/tmp/upstream/*` 中完成。
>
> **更新状态：已完成。** 2026-11-04 将本地两个项目更新到上表目标提交（jiuwenswarm `a2abceccf` / agent-core `5a35c39f`），更新前备份位于 `/home/opensource/backup-20261104/`；本地 `.clawbench/` 与原有本地文档已保留。

---

## 0. 结论速览

| 项目 | 本地版本 | 远端版本 | 落后提交（含合并） | 落后提交（不含合并） | develop 上跨过的标签 |
| --- | --- | --- | --- | --- | --- |
| jiuwenswarm（现名 workswarm） | 0.2.0 | 0.2.5.beta1 | **2805** | 2760 | dev0.2.1 → dev0.2.1.final → JiuwenSwarm0.2.2 → 0.2.3.beta1 → release_0.2.3 → dev_0.2.4.b1 → 0.2.4.beta1 |
| openjiuwen / agent-core | 0.1.13 | 0.1.20 | **1611** | 1572 | v0.1.14 → v0.1.14.post2 → v0.1.15(.post1~3) → v0.1.16.post1~5 → v0.1.20 |

> 说明：`v0.1.16`、`v0.1.17`、`v0.1.18`、`v0.1.19` 等标签位于发布分支，并非 `develop` 的祖先；上表只统计 `develop` 祖先链上跨过的标签。jiuwenswarm 的 `0.2.5.beta1` 标签同样在发布分支，`develop` 的 `pyproject.toml` 只是提前写成了该版本号。

三条最需要注意的结论：

1. **jiuwenswarm 已改名为 `workswarm`**（`pyproject.toml` 的 `name`、描述同步变更），包内目录仍是 `jiuwenswarm/`，但发布名变了，打包/安装脚本需要跟着改。
2. **上游 jiuwenswarm 不再跟随 `openjiuwen@develop`，改为钉死 `c8c621b2`**（2026-10-31，比本地基线新 1528 个提交），而 agent-core 的 `develop` 头 `5a35c39f` 又比这个钉死点再新 **83** 个提交。也就是说：只升 jiuwenswarm 会拿到 `c8c621b2`；若两个仓库都直接更到 develop 头，jiuwenswarm 并未在该组合上验证过。
3. **变更体量巨大**：jiuwenswarm 6835 个文件 `+1,652,248/-101,630`，agent-core 4428 个文件 `+981,093/-71,293`；包含多处架构级新增模块（`symphony`、`rsi`、`cua_engine`、独立 `gateway`、`harness_protocol`）和 9 处显式破坏性变更（agent-core 6 处、jiuwenswarm 3 处 `!` 提交），不建议一次性整包替换。

---

## 1. 数据来源与基线识别方法

### 1.1 访问方式

| 站点 | 结果 | 采用 |
| --- | --- | --- |
| github.com | 连接超时（15s 无响应），本地 agent-core 的 `origin` 指向 GitHub，无法 fetch | 否 |
| gitee.com | 可访问，但 `openJiuwen/jiuwenswarm`、`openJiuwen/agent-core` 均为 404 | 否 |
| gitcode.com | 可访问，两个仓库均 200，`git ls-remote` 正常 | **是** |

`jiuwenswarm/pyproject.toml` 本身就把依赖声明为 `git+https://gitcode.com/openJiuwen/agent-core.git`，因此 gitcode 即官方源，用它作为“远端库”基准。两个仓库以 `--filter=blob:none` 方式完整克隆到 `/tmp/upstream/`，保留全部分支与历史。

### 1.2 本地基线 commit 的确定

| 仓库 | 本地形态 | 基线判定 | 基线 commit |
| --- | --- | --- | --- |
| jiuwenswarm | **不是 git 仓库**（无 `.git`），纯源码包，1219 个文件 | 对上游 2026-05-29～06-05 的全部提交做**全树 blob 哈希比对**，找差异最小者：唯一命中的最小差异为 1 个文档内容差异 + 1 个本地新增文档 | `1ea114cf1446a8ef32eac98ae51a41db68cdc789`（2026-05-30 17:44:29，`feat: add necessary packages`） |
| openjiuwen | 有 `.git`，HEAD 即基线 | HEAD 提交本身 + 抽查 40 个 `openjiuwen/**/*.py` 的 blob 哈希，全部与基线一致 | `a889d9fcc05a4cd45a81776012d3c6b7f414e5ee`（2026-05-30 22:45:02，`fix(context): fix the message_summary_offloader bug`） |

基线可信度说明：

- jiuwenswarm 本地目录 mtime 为 `2026-05-30 17:44`，与基线提交时间 `17:44:29` 完全吻合；全树 1218 个文件中仅 `docs/zh/SUMMARY.md` 内容有差异，另有本地多出的 `docs/zh/知识库RAG设计与WeKnora对标.md`（应为打包前手工添加的文档，未进入上游提交）。
- agent-core 本地为**裁剪后的源码包**：`git status` 显示 3689 个受版本控制的路径在上传时缺失、30 个未跟踪文件；但版本号 `0.1.13` 与 HEAD 提交一致，抽查的源码文件哈希全部相同，故仍以 `a889d9f` 作为基线。

### 1.3 统计口径

- 提交数为 `git rev-list --count <基线>..origin/develop`，默认分支为 `develop`（两仓库 `origin/HEAD -> develop`）。
- “含合并”为全部提交；“不含合并”剔除 merge commit，附录清单只列非合并提交（2760 + 1572 = 4332 条）。
- 日期采用 author date，因此少数被合并进来的分支提交日期早于基线日期。
- 上游大量提交带 `[mirror] xxx#NNN:` 前缀（从 GitHub PR 镜像过来），分类时已剥离该前缀再判断类型。

---

## 2. 总览

| 指标 | jiuwenswarm | agent-core |
| --- | --- | --- |
| 上游仓库 | https://gitcode.com/openJiuwen/jiuwenswarm | https://gitcode.com/openJiuwen/agent-core |
| 基线 commit | `1ea114cf1446a8ef32eac98ae51a41db68cdc789` | `a889d9fcc05a4cd45a81776012d3c6b7f414e5ee` |
| 上游 HEAD commit | `a2abceccfba89ab25a518568916152865fa28acd` | `5a35c39f0163a4e6190800df53ad7e24848756eb` |
| HEAD 说明 | `feat(swarmflow): group workflow traces and show operator details` | `feat(swarmflow): open the swarmflow tool to in-process teammates` |
| HEAD 时间 | 2026-11-03 20:26 | 2026-11-03 20:45 |
| 落后提交（含合并） | 2805（其中 merge 45） | 1611（其中 merge 39） |
| 文件变更 | 6835 个，+1,652,248 / -101,630 | 4428 个，+981,093 / -71,293 |
| 参与作者 | 152 | 101 |
| 提交最活跃作者 | openJiuwen-bot 302、alan_cheng 232、kdgmpw 141 | alan_cheng 654、openJiuwen-bot 108、wncbdj 46 |

提交节奏（非合并提交）：

| 月份 | jiuwenswarm | agent-core |
| --- | --- | --- |
| 2026-05 | — | 25 |
| 2026-06 | 413 | 282 |
| 2026-07 | 442 | 280 |
| 2026-08 | 453 | 265 |
| 2026-09 | 695 | 339 |
| 2026-10 | 679 | 351 |
| 2026-11（截至 3 日） | 78 | 30 |

---

## 3. jiuwenswarm → workswarm

### 3.1 版本与依赖演进

`pyproject.toml` 关键变化：

| 项 | 本地 0.2.0 | 上游 0.2.5.beta1 |
| --- | --- | --- |
| `name` | `jiuwenswarm` | **`workswarm`** |
| `description` | JiuwenSwarm | WorkSwarm |
| `requires-python` | `>=3.11,<3.14` | `>=3.11,<3.15` |
| 基础依赖条数 | 30 | **63** |
| `openjiuwen` 来源 | `@develop`（浮动） | `@c8c621b22f1ad5da6acee0ceee33778007c06efb`（钉死） |

新增依赖（按用途归类）：

- **模型/协议**：`a2ui-agent-sdk==0.2.1`、`a4p`（agent-protocol V0.2.0）、`openjiuwen-protocol`、`anthropic>=0.40,<1.0.0`、`google-genai`、`google-adk`、`JiuwenMemory>=0.2.0`
- **渠道接入**：`python-telegram-bot`、`discord.py`、`slack-bolt`、`dingtalk-stream`、`wecom-aibot-sdk`、`lark-oapi`、`python-socks`
- **服务/存储**：`fastapi`、`uvicorn`、`starlette`、`minio`、`asyncpg`、`pgvector`、`aiosqlite`、`sqlite-vec`、`faiss-cpu`
- **工具链**：`fastmcp`、`mcp`、`prompt-toolkit`、`playwright`、`tree-sitter(-bash)`、`tiktoken`、`uv`、`gitcode-api`、`gitcode-cli`、`skillnet-ai`、`imageio-ffmpeg`、`mutagen`
- **可观测**：`opentelemetry-api/sdk`、`otlp-proto-grpc`、`otlp-proto-http`
- **企业运行时**：`openjiuwen-runtime-foundation`（agent-runtime 仓库 `foundation` 子目录）
- **新增 extra**：`[harmony]`（鸿蒙精简依赖集）

依赖安全下限（注释中明示的 CVE 修复）：

- `lxml>=6.1.0` ← CVE-2026-41066（XXE）
- `pillow>=12.2.0` ← CVE-2026-40192（FITS gzip bomb DoS）
- `python-multipart>=0.0.31` ← CVE-2026-42561（MultipartParser DoS）
- `filelock>=3.25,<4`：规避 openjiuwen 文件锁跨 task 释放导致的死锁
- `anthropic<1.0.0`：1.0.0 迁到 httpx2 fork，自定义 `http_client` 会被类型校验拒绝

主仓库 `uv.lock` 从 ~797 KB 增长，依赖树整体重算，升级时**必须重新 lock**，不能沿用本地 lock 文件。

### 3.2 目录与模块结构变化

新增顶层条目：`gateway/`（从 `jiuwenswarm/gateway` 提升为独立可发布包）、`connectors/`、`sdks/`、`third_party/`、`Dockerfile.claw`、`Makefile`、`setup.py`、`AGENTS.md`、`CODEREVIEWER/`、`COMMITTER/`、`.agents/`、`.claude/`、`.gitcode/`、`.doc_project_maintainer/`、`USER.json`。

`jiuwenswarm/` 包内新增子模块：`cli/`、`clouddoc/`、`cua_engine/`、`diagnosis/`、`enterprise/`、`infrastructure/`、`observability/`、`runtime/`、`tests/`，以及若干运行时补丁文件（`llm_sse_patch.py`、`llm_provider_compat_patch.py`、`openjiuwen_fs_rwlock_patch.py`、`debug_launcher.py`）。同时 `gateway/` 从包内移出。

### 3.3 变更规模（按目录，前 12）

| 目录 | 文件数 | 新增行 | 删除行 |
| --- | --- | --- | --- |
| `jiuwenswarm/channels` | 1919 | +414,430 | -28,837 |
| `tests/unit_tests` | 1139 | +397,954 | -4,131 |
| `jiuwenswarm/server` | 408 | +190,277 | -10,979 |
| `jiuwenswarm/agents` | 467 | +142,151 | -26,263 |
| `jiuwenswarm/resources` | 394 | +80,770 | -2,523 |
| `jiuwenswarm/common` | 121 | +28,271 | -3,849 |
| `jiuwenswarm/cua_engine` | 259 | +30,883 | 0 |
| `gateway/channel_manager` | 60 | +29,479 | 0 |
| `jiuwenswarm/extensions` | 116 | +26,554 | -531 |
| `docs/zh` | 100 | +21,292 | -2,964 |
| `jiuwenswarm/runtime` | 55 | +20,453 | 0 |
| `gateway/common` | 94 | +19,663 | 0 |

`channels/web` 单目录新增 1508 个文件，是本次体量最大的部分（前端重构/扩张）。

### 3.4 提交分类

| 类别 | 提交数 | 占比 |
| --- | --- | --- |
| 缺陷修复 | 1904 | 69.0% |
| 新功能 | 453 | 16.4% |
| 重构 | 101 | 3.7% |
| 文档 | 90 | 3.3% |
| 测试 | 38 | 1.4% |
| 工程/依赖/构建 | 33 | 1.2% |
| 优化改进 | 32 | 1.2% |
| 回滚 | 8 | 0.3% |
| 安全加固 | 1 | 0.0% |
| 其他（含无规范前缀） | 100 | 3.6% |

热点 scope（`type(scope)` 中的 scope）：`web` 156、`tui` 113、`swarm` 104、`team` 87、`gateway` 86、`designer` 76、`symphony` 72、`agentos` 63、`trajectory` 59、`cron` 53、`swarmflow` 49、`session` 37、`rsi` 33、`server` 32、`skills` 29。

### 3.5 主题级变更总结

**① Web 控制台重做与扩张（最大头，`channels/web` +1508 文件）**
支持图片附件与多模态对话、模型 API Key 长度 500→2048、代码模式前端 MVP、`/config` 交互面板、目标管理（Goal）前端、trajectory 面板（token 成本时间线、team 成员泳道）、Designer 设计/视频生成入口、A2UI、会话侧边栏与浏览器面板、大量 UI 还原与折叠面板修复。代表提交：`feat(web): add code mode frontend MVP`、`feat(trajectory): add token-cost timeline view`、`feat(goal): Web Goal streaming`。

**② TUI / CLI 渠道化**
新增 `jiuwenswarm chat` 终端入口、`/config`、`/permissions`、`/recap`、`/statusline`、`/mode`、`/rewind` 等命令体系，工具渲染与日志折叠，英文状态文案；把 CLI 从 server 内嵌改为独立 channel（`e705f0cf cli channel化 runtime/server 重构`），并新增 `process-cli` 单 Agent 一次性机器执行协议（14 个 feat）。代表提交：`feat(process-cli): 实现单 Agent 一次性机器执行入口`、`feat(TUI): tui log and fold`。

**③ Team / Swarm 与分布式形态**
`swarmflow` 从引入到成型（支持 swarmflow 能力、workflow 事件、阶段遥测、worker/phases 事件转 web 事件）；团队成员支持外部 CLI agent（claude / codex / gemini）、技能发现、工作区隔离；Team 配置与可靠性项、同会话在线播放；`agentos` 模式与元荣（YuanRong）沙箱创建/删除、registry HTTP 客户端；`403fe354 AgentOS 架构下 Gateway 去业务状态化`。

**④ Symphony（技能交响乐）新模块**
增量/全量构建、暂停按钮、Agentic 检索（先检索后编排）、技能树可视化、构建页面优化；上游 72 个 symphony scope 提交。

**⑤ RSI（递归自我改进）**
`artifact` 优化服务流、artifact 文件浏览与 UI、`rsi-program-dataset-creator` 技能改名并补规则、`search_width` 参数废弃；33 个 rsi scope 提交。

**⑥ 渠道扩展（IM / 浏览器 / IDE / 桌面）**
Telegram、Slack、Discord、企业微信、钉钉接入；浏览器扩展作为新 channel、内置 Playwright MCP 0.0.78、headless 模式；IDE channel（50 个新文件）、desktop channel、ACP 协议支持。

**⑦ Gateway / AgentOS 基础设施**
`gateway` 独立成包，新增 `auth`、`channel_manager`、`enterprise`、`cron`、`heartbeat`、`config_poll`、`storage_assembly`、`im_pipeline`、`swarmflow_reply`、`zen_query` 等；企业分支特性回合并。

**⑧ 权限与安全**
新增权限模式（Full access / Auto / Strict）、智能审批策略、隔离语义审查与安全审计、Team 权限 rail、`/permissions` 命令 CRUD；安全修复含 `92ab155f`（teamskillshub 路径穿越）、`be115f6c`（Web 流事件按 `metadata.ws_id` 寻址，避免跨会话泄漏）、`4258eacc`（SSL 校验开关与三方地址去硬编码）、`217c0400`（锁定依赖下限修 CVE）、`a46b9e04`（升级 5 个依赖至漏洞扫描要求版本）。注意权限模式曾被整体回滚（`1454fa01 Revert "add permission modes"`），后续以 `feat(permissions)` 系列重新落地，升级时需确认最终形态。

**⑨ Trajectory / 可观测**
轨迹 v2 归档（内容寻址、分页保留、JSONL zip 导出）、span schema 统一版本、错误原因还原、团队轨迹分组（上游 HEAD 提交即属此类）。

**⑩ 记忆 / 技能 / 定时任务 / CUA**
记忆提供方扩展、技能全量创建（skill-omni-creation）、内置 skill-creator 与 swarmskill-creator、技能分发按经验增强；cron 执行记录分页与时间显示、跨进程互斥锁；新增 `cua_engine`（桌面自动化，259 个文件）。

### 3.6 破坏性 / 高风险提交（jiuwenswarm）

| commit | 说明 | 风险 |
| --- | --- | --- |
| 包名变更 | `jiuwenswarm` → `workswarm` | 安装名、镜像、部署脚本、依赖声明全部受影响 |
| `4c705d063` | `feat(trajectory)!: turn-page retention with checkpoints and JSONL zip archives` | 轨迹存储格式升级 |
| `fbb901b8b` | `feat(trajectory)!: export trajectory archives as version 2` | 导出格式 v2，旧消费端需适配 |
| `28b1dec4b` | `feat(trajectory)!: archive v2 is content-addressed only` | 旧路径式归档失效 |
| `e3ee2dec` | `Revert "feat(a4p): integrate A4P intent authorization"` | A4P 意图鉴权未生效 |
| `f798ee38` | `revert(protocol): 临时回退 gateway-protocol 外部包依赖` | 协议包仍走内置实现 |
| `9a47ab99` | `refactor(web): 删除团队管理页签` | 前端入口消失 |
| `e705f0cf` | `cli channel化 runtime/server 重构` | CLI 启动路径变化 |
| `33e55148` | `revert(worker): roll back !9157 lifecycle changes` | worker 生命周期回退 |
| `bd18c024` | `企业分支安全护栏特性回合` | 安全护栏策略默认值变化 |

---

## 4. openjiuwen / agent-core

### 4.1 版本与依赖演进

| 项 | 本地 0.1.13 | 上游 0.1.20 |
| --- | --- | --- |
| `version` | 0.1.13 | **0.1.20** |
| 基础依赖条数 | 33 | 46 |

新增基础依赖：`agentdescent==0.4.6`（精确钉死，RSI 的 PUCT 算法依赖其内部实现）、`intelli-router==0.1.0b1`、`anthropic>=0.120.2`、`trafilatura`、`pyoxigraph`、`matplotlib`、`PyYAML`、`gitcode-api`、`opentelemetry-sdk` 及 4 个 OTel 自动埋点包（httpx / requests / aiohttp-client / fastapi）。

调整：`filelock>=3.20.1` → `>=3.25,<4`；`sqlalchemy` → `sqlalchemy[asyncio]`；`fastmcp<3.0` → `<4.0`；`sqlite` extra 去掉 `greenlet`。

新增 extras：`jiuwen-memory`、`claude`（claude-agent-sdk）、`codex`（openai-codex）、`dsh`（deepseek-harness-sdk）、`ssh`（asyncssh）、`observability`、`cua`（pyautogui 等桌面工具）。

### 4.2 目录与模块结构变化

新增顶层：`plugins/`、`scripts/`、`.semgrep/`、`except-baseline.json`、`CODEREVIEWER/`、`COMMITTER/`、`.agents/`、`USER.json`。

`openjiuwen/` 新增子包：`harness_protocol/`、`harness_providers/`、`rsi/`、`symphony/`。其中 `rsi`（+86,932 行 / 287 文件）与 `symphony`（+53,650 行 / 207 文件）是两个从零新增的大模块。

### 4.3 变更规模（按目录，前 10）

| 目录 | 文件数 | 新增行 | 删除行 |
| --- | --- | --- | --- |
| `tests/unit_tests` | 1195 | +351,699 | -19,488 |
| `openjiuwen/harness` | 398 | +112,571 | -11,870 |
| `openjiuwen/agent_teams` | 570 | +88,826 | -11,338 |
| `openjiuwen/rsi` | 287 | +86,932 | 0 |
| `openjiuwen/agent_evolving` | 294 | +49,686 | -10,440 |
| `openjiuwen/symphony` | 207 | +53,650 | 0 |
| `openjiuwen/core` | 316 | +46,364 | -6,123 |
| `.agents/agent-docs` | 266 | +51,018 | 0 |
| `docs/zh` | 136 | +19,491 | -1,271 |
| `openjiuwen/extensions` | 75 | +17,783 | -626 |

`openjiuwen/__init__.py` 的公共导出**未变**（无破坏性导出删改），破坏性变更集中在子模块语义。

### 4.4 提交分类

| 类别 | 提交数 | 占比 |
| --- | --- | --- |
| 缺陷修复 | 811 | 51.6% |
| 新功能 | 331 | 21.1% |
| 文档 | 167 | 10.6% |
| 测试 | 123 | 7.8% |
| 重构 | 61 | 3.9% |
| 工程/依赖/构建 | 37 | 2.4% |
| 优化改进 | 33 | 2.1% |
| 回滚 | 4 | 0.3% |
| 其他 | 5 | 0.3% |

热点 scope：`swarm` 434、`harness` 131、`agent_teams` 112、`rsi` 62、`observability` 60、`agent-teams` 50、`attribution` 47、`llm` 36、`symphony` 34、`context` 29、`team` 25、`memory` 25、`evolving` 21。

### 4.5 主题级变更总结

**① Swarm / Agent Teams（体量最大，约 600 个提交）**
`SuperHarness` → `NativeHarness` 并迁入 `agent_teams`；team_tools 拆分为领域模块；外部 CLI runtime（Claude Code / Codex / Gemini）接入 spawn 与 configurator；`team.mq.publish` 混合事件、群组消息、成员 roster、worktree 隔离、任务依赖与恢复；一大组 SQLite 性能优化（读写分离池、CAS 抢占、批量事务、WAL checkpointer、复合索引，见 `a6154460`、`9cccd52d`、`94e798d0` 等 15+ 个 `perf(swarm)`）。

**② Harness**
新增并发安全的 `SuperHarness`/`DeepAgent` 交互、异步 aiohttp web tools 包、技能快照签名增量刷新、`harness_protocol` 与 `harness_providers` 两个新子包；`refactor(harness): route web tools via package entry, use async impl`；移除废弃的同步 `web_tools` 模块（`ff2ee73d`）。

**③ RSI（递归自我改进，全新模块）**
新增 `openjiuwen/rsi`：递归自我改进框架、证据驱动的 harness 演进、artifact 优化（PUCT 移植自 agentdescent）、单 harness 引擎事件；配套 62 个 rsi scope 提交。

**④ Symphony（全新模块）**
新增 `openjiuwen/symphony` 与 experience 模块（技能检索）；与 jiuwenswarm 的 Symphony UI 对接。

**⑤ 可观测性**
OTel 全局 instrument（httpx/requests/aiohttp/fastapi）、标准 OTLP JSON 文件 exporter、trajectory span processor、团队级观测、span-level schema 版本统一（`6758808e`，破坏性）。

**⑥ LLM / 上下文 / 记忆**
新增 anthropic 模型客户端、OpenRouter anthropic/qwen 显式缓存、`reasoning_content` 处理、Responses usage 的 reasoning_tokens；异步 session memory processor、主动上下文（GitCode 服务）、压缩状态里的消息 offload；memory 新增 lakebase provider、`jiuwen_memory` provider、今日/昨日 daily_memory 自动注入。

**⑦ 安全与权限**
权限引擎与产品权限模型解耦并重构目录（`20e35736`）、权限模式 Full/Auto/Strict（`81750868`、`cb80cea0`）、Team 权限系统与 leader 仲裁 ASK（`f639c69e`）、guardrail 统一收编命令行安全检查（`1c22b9f2`）、修复内置安全规则不生效（`74fa6109`）；另有企业分支安全护栏回合。

**⑧ Browser / 检索 / 文档**
并行浏览器隔离（headless/headed）、批量表单填写、内置 Playwright MCP 命令、浏览器页面归属与完成判定；ontology 三元组抽取器；大量 zh/en 文档同步。

### 4.6 破坏性变更（agent-core，共 6 处 `!` + 1 处废弃模块移除）

| commit | 说明 |
| --- | --- |
| `2116850a` | `feat(codex)!: append the host prompt to the CLI instructions by default` |
| `83aa7194` | `feat(harness)!: report third-party model requests as protocol events` |
| `4ddca8ff` | `feat(trajectory)!: read reasoning as reasoning_content and skip compaction turns` |
| `b40b484b` | `feat(trajectory)!: rebuild evolution messages from the context window chain` |
| `6758808e` | `feat(observability)!: one span-level trajectory schema version` |
| `6100c35f` | `fix(observability)!: remove the deprecated backend selector` |
| `ff2ee73d` | `refactor(harness): remove deprecated synchronous web_tools module` |

---

## 5. 两仓库联动关系（升级必须成对考虑）

```
本地：jiuwenswarm 0.2.0  ──依赖──▶  openjiuwen @develop = a889d9f (0.1.13)
                                                    │
                                        1528 个提交 │
                                                    ▼
上游：jiuwenswarm HEAD ──钉死──▶  openjiuwen c8c621b2 (2026-10-31, v0.1.16.post5-806)
                                                    │
                                         83 个提交  │
                                                    ▼
                                   agent-core develop HEAD 5a35c39f (0.1.20)
```

| 组合 | 状态 |
| --- | --- |
| 本地现状（0.2.0 + a889d9f） | 当前可用基线 |
| 只升 jiuwenswarm 到 HEAD（自动取 c8c621b2） | **上游自身锁定的组合**，推荐作为第一阶段 |
| 两个都升到各自 develop HEAD（0.2.5.beta1 + 5a35c39f） | `c8c621b2` 之后 83 个提交中没有显式 `!` 破坏性变更，但上游未声明验证过该组合，需自行回归 |
| 只升 agent-core 到 5a35c39f（jiuwenswarm 仍 0.2.0） | 高风险：jiuwenswarm 0.2.0 无 `openjiuwen_fs_rwlock_patch` 等适配层，且依赖 API 已变 |

另需注意：agent-core `develop` 版本 0.1.20 与 `release/v0.1.20.post1` 并行存在，`dev-stable` 停在 0.1.16；jiuwenswarm 多个 `releases/*` 分支的 `pyproject.toml` 版本号仍写 `0.2.5.beta1`（上游未随分支更新版本号），不要用分支名推断版本。

---

## 6. 完整提交清单

| 文件 | 内容 | 条数 |
| --- | --- | --- |
| [`jiuwenswarm-commits.md`](./jiuwenswarm-commits.md) | jiuwenswarm 全部落后提交，按类别分组、组内按时间排序，含完整 40 位 SHA | 2760 |
| [`agent-core-commits.md`](./agent-core-commits.md) | agent-core 全部落后提交，同上 | 1572 |

两份清单的表格列为：序号 / commit（完整 SHA）/ 日期 / 作者 / 提交说明，可直接 `git show <sha>` 查详情。

### 复现命令

```bash
# 1) 克隆上游（GitHub 不可达时使用 gitcode）
git clone --filter=blob:none --no-single-branch https://gitcode.com/openJiuwen/jiuwenswarm.git
git clone --filter=blob:none --no-single-branch https://gitcode.com/openJiuwen/agent-core.git

# 2) 统计落后提交
git -C jiuwenswarm rev-list --count 1ea114cf1446a8ef32eac98ae51a41db68cdc789..origin/develop   # 2805
git -C agent-core  rev-list --count a889d9fcc05a4cd45a81776012d3c6b7f414e5ee..origin/develop    # 1611

# 3) 查看单条提交
git -C jiuwenswarm show <sha>
```

---

## 7. 建议的关注点（本次不执行更新）

1. **先确认包名切换**：`workswarm` 是否已在下游部署/镜像/CI 中生效，`import jiuwenswarm` 与 `pip install jiuwenswarm` 的兼容策略。
2. **按“第一阶段只升 jiuwenswarm 到 HEAD（含 c8c621b2 钉死）”推进**，再评估是否追 agent-core 的 83 个增量。
3. **重点回归面**：Web 控制台（1508 个新文件）、Team/Swarm 分布式与外部 CLI agent、Symphony、RSI、权限模式（曾被回滚后重做）、Trajectory v2 归档格式、Gateway 独立包与 AgentOS 部署形态。
4. **必须重新 lock 依赖**：两仓库的 `uv.lock`、`pyproject.toml` 都大改，且含 CVE 下限与精确钉死项（`agentdescent==0.4.6`、`intelli-router==0.1.0b1`、`skillnet-ai==0.0.16`）。
5. **数据兼容**：trajectory 归档 v1→v2、会话/任务 SQLite 索引与 schema 变更（含 15+ 个 `perf(swarm)` 迁移），升级前备份工作区数据库。
