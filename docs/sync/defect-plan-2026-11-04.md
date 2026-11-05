# 上游同步 · Go 缺陷计划（2026-11-04）

> 依据：[`README.md`](./README.md)（2026-11-03 上游快照）、[`agent-core-commits.md`](./agent-core-commits.md)、[`jiuwenswarm-commits.md`](./jiuwenswarm-commits.md)
> 基线：jiuwenswarm `1ea114cf` → `a2abceccf`；agent-core `a889d9fcc` → `5a35c39f`
> 范围：上游 **2713 条非合并缺陷修复**（jiuwenswarm 1902 + agent-core 811）中，落在 Go 已迁移子系统上的部分。
> 新功能（含未迁移新模块）另见 [IMPLEMENTATION_PLAN.md](../../IMPLEMENTATION_PLAN.md) 领域十五~二十九。

---

## 0. 摘要

1. **不能逐条照搬**：2713 条修复里，259 条带 `web`/`tui`/`designer` scope，另有 606 条无 scope 提交中约半数为前端修复；这些属于 Go 未迁移的前端产物，不构成本项目缺陷。真正落在 Go 已迁移包上的，聚合后为 **14 组、181 个缺陷行**。
2. **不是"上游修了 Go 就一定有"**：Go 迁移时基线早于这批修复，但部分防护在迁移时就已按当时的 Python 行为实现（例：技能包解压 zip-slip 防护 Go 已有 `filepath.Rel` 校验）。因此每个缺陷族必须先在 Go 侧核验再动手。
3. **优先看 P0**：数据丢失 / 崩溃 / 安全边界三类共 **30 行**（见 §4 批次一），其余为行为偏差与可观测性偏差。
4. **核验进度**：181 行中已确认 1 行（D-01）、已对齐 2 行（D-07、C-15）、归新功能 2 行（H-16、L-09），其余 176 行为待核验。
5. **高价值系统性缺陷**集中在四处：① ReAct 循环终止语义（force_finish / 无进展 / 迭代上限）② 上下文压缩的取消与预算守卫 ③ AgentTeams 任务与 worktree 生命周期 ④ Gateway 会话路由与资源释放。

### 状态图例

| 标记 | 含义 |
| --- | --- |
| ✅ | 已在 Go 侧确认存在该缺陷（已核验代码路径） |
| 🔍 | 待核验：上游修复证据明确，Go 落点已定位，但尚未逐行确认是否同构 |
| ➖ | 已对齐：Go 已具备等价防护，或 Go 结构不同构（如 Python 专属的 event-loop/FD 问题） |
| ⤵️ | 归新功能：Go 尚无该模块，转入实现计划对应领域 |

### 优先级

- **P0**：数据丢失 / 崩溃 panic / 安全越界（路径穿越、越权、凭据泄漏）/ 全栈停机
- **P1**：功能行为与上游不一致（会导致对话失败、任务卡死、结果错误）
- **P2**：可观测性、日志、性能、提示词与体验偏差

---

## 1. 判定口径

### 1.1 入表条件（三条同时满足）

1. 上游修复触及的 Python 文件，在 Go 有对应包（映射见各行"Go 落点"）；
2. Go 该包的实现是**按基线语义**迁移的，未在迁移时吸收后续修复；
3. 缺陷在 Go 语言模型下可复现（不是 Python 专属机制，如 `asyncio` event-loop、`loguru` sink、`sys.setrecursionlimit`）。

### 1.2 出表（转入新功能）的典型

- `symphony` / `rsi` / `cua_engine` / `harness_protocol` / `harness_providers` / `process-cli` / `gateway` 独立包的**新文件**——Go 完全没有对应包，其内部的 fix 一律随功能实现落地（计划领域十五~二十一）。
- `channels/web/frontend`、`channels/tui/frontend`、`channels/desktop`、`designer`——Go 侧无前端源码。
- 纯依赖升级（`pyproject.toml`/`uv.lock`）与 Python 打包问题。

### 1.3 核验方法（每个族的固定动作）

```bash
# 1) 取上游修复的完整 diff —— 用带 .git 的分析克隆（/tmp 被清理时按 README §6 重新 clone）
git -C /tmp/upstream/agent-core show <sha> --stat
git -C /tmp/upstream/jiuwenswarm show <sha> --stat
#    注意：/home/opensource/jiuwenswarm-develop 是无 .git 的源码包，只能看文件内容，不能 git show
# 2) 在 Go 侧定位等价代码
rg -n "<Python 符号>" internal/
# 3) 判定：缺实现 / 实现但无防护 / 已对齐，并在本表把 🔍 改为 ✅ / ➖
# 4) 修复后补单测（>=85% 包覆盖）+ 集成用例（跨 >=3 个 internal 二级子系统）
```

---

## 2. 总览

| 组 | 主题 | 行数 | P0 | P1 | P2 | Go 主要落点 |
| --- | --- | --- | --- | --- | --- | --- |
| A | 核心 ReAct 循环与中断恢复 | 12 | 2 | 8 | 2 | `internal/agentcore/single_agent/agents` |
| B | 上下文引擎与压缩 | 16 | 3 | 9 | 4 | `internal/agentcore/context_engine` |
| C | LLM 客户端与流式 | 15 | 0 | 10 | 5 | `internal/agentcore/foundation/llm` |
| D | 工具系统 / MCP / 提示词模板 | 17 | 5 | 11 | 1 | `agentcore/sys_operation`、`foundation/tool`、`foundation/prompt` |
| E | 记忆 | 13 | 2 | 8 | 3 | `internal/agentcore/memory`、`internal/agent_teams/memory` |
| F | 会话 / 状态 / 检查点 / VCS | 9 | 2 | 7 | 0 | `internal/agentcore/session` |
| G | 工作流与图引擎 | 7 | 0 | 5 | 2 | `internal/agentcore/workflow`、`agentcore/graph` |
| H | Harness / DeepAgent | 17 | 3 | 13 | 1 | `internal/agentcore/harness`、`internal/swarm/agents/harness` |
| I | AgentTeams / Swarm | 19 | 3 | 13 | 3 | `internal/agent_teams`、`internal/swarm` |
| J | AgentServer / Gateway / 渠道 | 21 | 3 | 14 | 4 | `internal/swarm/{server,gateway,channels}` |
| K | 轨迹与可观测性 | 10 | 0 | 4 | 6 | `internal/evolving/trajectory`、`agent_teams/observability` |
| L | 权限与安全 | 10 | 4 | 6 | 0 | `internal/agentcore/harness/rails/security`、`common/security` |
| M | 工程性 / 资源 / 性能 | 6 | 1 | 3 | 2 | 跨包 |
| N | 定时任务与心跳 | 9 | 2 | 7 | 0 | `internal/swarm/gateway/{cron,heartbeat}` |
| **合计** | | **181** | **30** | **118** | **33** | |

> 本表数字由脚本按行统计（`^\\| ([A-N])-\\d+ \\|`）生成，修改明细后需重新核对。

---

## 3. 分组明细

### A. 核心 ReAct 循环与中断恢复

| 编号 | 缺陷（Go 语义） | 上游证据 | Go 落点 | 级别 | 状态 |
| --- | --- | --- | --- | --- | --- |
| A-01 | 循环迭代耗尽时未按 boundary 的 `force_finish` 收尾，应产出最终答案而不是继续重试或直接报错 | `43d18a45` | `single_agent/agents/react_invoke.go`、`react_helpers.go` | P1 | ✅ |
| A-02 | 工具调用**之前**也要处理 force finish（否则仍会执行被拒绝后的工具） | `537ebc4c` | 同上（tool 前置检查） | P1 | ✅ |
| A-03 | 流式异常时必须把流消费干净，否则 goroutine/连接泄漏、上下文不落盘 | `aeceda1c` | `react_model_call.go` | P0 | ✅ |
| A-04 | 普通查询恢复时应定位"首个被中断的组件"续跑，而非从头重放 | `e2f492c8` | ⤵️ 领域二十三（application 层 LLMController 未迁移） | P1 | ⤵️ |
| A-05 | 内层 ReAct 必须遵守配置的 `max_iterations`；未配置时应视为**不限**（不是默认 10） | `d4e8f81e`、`980ba342` | `single_agent/agents`、`single_agent/interfaces` | P1 | ✅ |
| A-06 | 输出上限在"还没有任何答案"时命中，应判定失败而非静默空回包 | `5f017e34` | 循环终止判定 | P1 | ✅ |
| A-07 | 一轮既无答案也无工具调用 → 重试一次，仍无则失败（防静默空轮） | `ad668f63` | 同上 | P1 | ✅ |
| A-08 | 无进展停止 + 外层轮次上限 + 视觉降级（防 agent 挂死） | `fee4e34c` | 循环守卫 | P0 | ✅ |
| A-09 | 只有**连续相同**的工具调用（工具集+参数）才计为 loop，不能跨轮累计 | `589da12a`（`1c4f5603` 无效已删除） | ⤵️ 领域九 9.30 ModelAnomalyDetectionRail | P1 | ⤵️ |
| A-10 | 中断后的 ReAct context 需持久化，否则恢复丢上下文 | `bf0a3eb2` | 会话状态提交点 | P1 | ✅ |
| A-11 | system prompt 为空时仍要保留 skills section | `051ae9c8` | `react_prompt.go` | P2 | ✅ |
| A-12 | Agent 工具（agent-as-tool）HTTP 请求需超时与响应状态校验 | `b8ceb049` | 技能远程下载已具备超时+状态校验；wikipedia 工具未迁移 | P2 | ➖ |

### B. 上下文引擎与压缩

| 编号 | 缺陷（Go 语义） | 上游证据 | Go 落点 | 级别 | 状态 |
| --- | --- | --- | --- | --- | --- |
| B-01 | 工具结果窗口化（`ToolResultWindowProcessor`）未接入处理器预设，长工具结果直接进上下文 | `ecb7e5ce` | `context_engine/processor/` | P0 | 🔍 |
| B-02 | 压缩预算缺 guard：预算 ≤0 / 极小时仍执行压缩导致死循环或空压缩 | `d0adebac` | 压缩器预算校验 | P1 | 🔍 |
| B-03 | **取消时未保存消息历史**：用户中断后本轮消息丢失 | `1b28ec45` | 取消路径的状态提交 | P0 | 🔍 |
| B-04 | 取消时应保存"清洗后"的上下文（而非原始），否则恢复后重复压缩 | `4ba66bf1` | 同上 | P0 | 🔍 |
| B-05 | base64 图片载荷被当作文本计入 token，导致过早压缩 | `660e9c57` | token 计数 | P1 | 🔍 |
| B-06 | usage token 基线在 append-only 消息增长后失效 / 未跨轮保留 | `cad85ec`、`c6659f10` | usage 统计 | P2 | 🔍 |
| B-07 | 流式过程中不允许上下文恢复（应允许），否则溢出后无法自救 | `1254a180` | 溢出恢复 | P1 | 🔍 |
| B-08 | 连续调用同一工具会触发压缩并抛异常 | `31c5733b` | 压缩触发条件 | P1 | 🔍 |
| B-09 | 百炼 `Range of input length` 错误应识别为上下文溢出并触发压缩 | `384d1466` | 溢出错误分类 | P1 | 🔍 |
| B-10 | 手动 compact 后 usage 上报不准确（应发布压缩后的用量） | `be7dadd2` | usage 发布 | P2 | 🔍 |
| B-11 | 被按路径读取过的源文件不应参与"规则压缩"（会破坏后续 edit） | `82748f69` | 规则压缩排除集 | P1 | 🔍 |
| B-12 | token 估算降级按「3 字符 ≈ 1 token」而非按字节 | `7215321b` | token 估算 | P2 | 🔍 |
| B-13 | `message_summary_offload` 参数冗余清理；**带 tool_calls 的 assistant 消息不得卸载** | `85c1e6f5` | offloader | P1 | 🔍 |
| B-14 | 模型上下文窗口取最大值算法错误 / 需可显式配置（含 `context_window` 配置项） | `df67c97f`、`bb799210` | `common/config`、`llm/schema` | P1 | 🔍 |
| B-15 | 压缩召回 query 需从**结构化 user content** 中抽取文本（多模态消息取不到） | `bbf5a80c` | 压缩召回 | P1 | 🔍 |
| B-16 | 压缩状态需上报 modified messages 与 offload handle（前端/轨迹消费） | `feat(context)` 系列 | 压缩状态结构 | P2 | 🔍 |
| B-17 | 动态提示词仍靠 system prompt 改写，无附件投递层（上游已迁移为历史消息 + 哈希增量） | `958fd614`、`c6852696`、`ee92d08b` | `harness/prompts/attachment_*.go`（S1 已落地） | P1 | ✅ |

### C. LLM 客户端与流式

| 编号 | 缺陷（Go 语义） | 上游证据 | Go 落点 | 级别 | 状态 |
| --- | --- | --- | --- | --- | --- |
| C-01 | 流式 chunk 卡住（长时间无数据）无处理：应超时退出并保留已产出内容 | `55116d61` | `model_clients/openai` SSE | P1 | 🔍 |
| C-02 | chunk 入队重试耗尽后必须报错，不能静默丢 chunk | `65dc4b13` | 流式管道 | P1 | 🔍 |
| C-03 | `finish_reason` 被覆盖/丢失：应保留真实值（含流式增量） | `687be26c`、`ec26a306` | chunk 解析 | P1 | 🔍 |
| C-04 | `cache_tokens` / cache 读写用量字段未解析，成本统计偏低 | `ce761415`、`df7c717` | usage 解析 | P2 | 🔍 |
| C-05 | `reasoning_content` 抽取：OpenRouter `reasoning` 字段、DeepSeek 按模型名补字段 | `3585aaf1`、`01a08c13` | reasoning 抽取 | P2 | 🔍 |
| C-06 | Responses API 的 `reasoning_tokens` 未从 usage 读取 | `92eb535b` | usage 解析 | P2 | 🔍 |
| C-07 | 不同 provider 的凭据必填项差异未区分（缺 key 时才报错/该报错时未报） | `f38e0d96` | 客户端校验 | P1 | 🔍 |
| C-08 | `api_base` 被硬编码补 `/v1`，对已带版本段的网关产生双段 | `38975d79` | base URL 归一 | P1 | 🔍 |
| C-09 | `top_p`/`temperature` 不应有默认值（未设置就不下发），Anthropic 设了 temperature 要丢 top_p | `a586d48a`、`69ce2182`、`fc9f9f9a` | 请求参数构建 | P1 | 🔍 |
| C-10 | 请求编码错误需分类并保留错误日志（当前被吞成通用错误） | `6fe3c353` | 错误映射 | P2 | 🔍 |
| C-11 | 模型异常需按状态码规范抛（401/429/5xx 语义不同） | `f9cb58e7` | `WrapError`/`HandleHTTPError` | P1 | 🔍 |
| C-12 | 可见输出前的瞬时失败应重试（当前一次失败即整轮失败） | `73e8255d` | 重试策略 | P1 | 🔍 |
| C-13 | IntelliRouter 路由缓存需在 event-loop/运行时变化后重建（Go：配置热更新后重建） | `e3e19a95`、`fe4eb216` | `intelli_router` | P2 | 🔍 |
| C-14 | HTTP client 复用：每请求新建 transport 导致连接泄漏 | `115583b0`、`4cc900ca` | 客户端构造 | P1 | 🔍 |
| C-15 | 流式 tool_call 被拆成两条（`type` 空串覆盖）导致后续请求 400 | `streaming-toolcall`（11-02 已修） | `mergeToolCalls` | P1 | ➖ |

### D. 工具系统 / MCP / 提示词模板

| 编号 | 缺陷（Go 语义） | 上游证据 | Go 落点 | 级别 | 状态 |
| --- | --- | --- | --- | --- | --- |
| D-01 | Shell 工具输出**无上限**，单条命令可撑爆上下文 | `b4a5a967` | `sys_operation/shell.go`（`ShellOptions` 无 `max_output`） | P0 | ✅ |
| D-02 | Bash 超时未跨执行层传递（harness → sys_operation → 进程），超时被忽略 | `55b9c571`、`c2d1544` | `harness/tools`、`sys_operation/local` | P1 | 🔍 |
| D-03 | 工具执行缺少"串行执行"选项（默认并行会破坏有状态工具） | `6eba3e6` | `ability` 调度 | P2 | 🔍 |
| D-04 | 工具调用整体无超时（模型卡住时无法中断） | `fe99da2` | `ability`/`react` 执行点 | P0 | 🔍 |
| D-05 | worktree 路径穿越：`gitdir` 内容拼接后未校验是否逃出仓库 | `e868918b` | `harness/tools/worktree/git.go` | P0 | 🔍 |
| D-06 | SkillTool 的 `relative_file_path` 未校验（可读任意文件） | `710b2b09` | `harness/tools`（skill_tool） | P0 | 🔍 |
| D-07 | 技能包解压路径穿越（zip/tar slip） | `c993823`、`8c5b537b` | `server/runtime/skill/remote_import.go` | P0 | ➖ |
| D-08 | teamskillshub 初始化路径穿越 | `92ab155f` | 同上（hub 分支） | P1 | 🔍 |
| D-09 | 技能卸载用 slug 而非不安全 name（含 `..`/斜杠） | `910af579` | `skill_manager.go` | P1 | 🔍 |
| D-10 | PDF/notebook 读取超 token 预算未截断 | `810bb071` | fs 工具读取 | P1 | 🔍 |
| D-11 | 流式子进程输出需增量解码（多字节字符跨 chunk 被切碎） | `ce2aedda` | `sys_operation/local` 流式读 | P1 | 🔍 |
| D-12 | Windows shell 自动检测错误 / 后台输出捕获缺失 | `af85e33d` | `sys_operation/local/proc_windows.go` | P1 | 🔍 |
| D-13 | MCP 参数 int 被解析成 float（工具校验失败） | `92a04257` | `tool/mcp` 参数解码 | P1 | 🔍 |
| D-14 | MCP SSE/streamable-http 连接被重置后不重连 | `1bd6360c`、`d90a442` | `tool/mcp/client` | P1 | 🔍 |
| D-15 | MCP 工具结果 content blocks 被丢弃（多模态/多段结果丢失） | `5a5d7c61` | 结果解析 | P1 | 🔍 |
| D-16 | MCP 错误状态在结果抽取中丢失（失败被当成成功） | `5d2eef7f` | 结果解析 | P1 | 🔍 |
| D-17 | MCP 跨 task context 退出导致 CPU 100% / 资源泄漏 | `0988e98c` | 客户端生命周期 | P2 | ➖（Go 无 task 概念，但需核验连接释放） |
| D-18 | `tool_search` 延迟工具机制缺失：deferred 工具、精确名优先、参数变更重建 BM25、保留 deferred 中断；`todos` 省略数组应拒绝；提示词空占位符需容忍 | `27a2e99b`、`1d257566`、`7915973`、`0af325eb`、`6e7f3bd9`、`c1395370`、`d8c226fa` | `ability`、`foundation/prompt` | P1 | ✅（9.11a 处置：exposure 策略 + BM25 精确名优先 + 参数变更重建 + deferred 中断透传，见 docs/superpowers/specs/2026-11-05-progressive-deferred-tool-catalog-design.md） |

### E. 记忆

| 编号 | 缺陷（Go 语义） | 上游证据 | Go 落点 | 级别 | 状态 |
| --- | --- | --- | --- | --- | --- |
| E-01 | 消息创建时间时区不一致导致记忆消息**排序错乱** | `5ba83785` | `memory` 消息存储/排序 | P0 | 🔍 |
| E-02 | embedding 配置变更后索引无法重建/热更新 | `0d62218`、`5dcd1357` | `memory` 索引管理 | P1 | 🔍 |
| E-03 | 未配置 embedding 时未降级为 FTS5-only 检索（直接失败） | `0fc618ad` | 混合检索 | P1 | 🔍 |
| E-04 | BM25 分词器缺陷（CJK/标点处理） | `1df6d247` | BM25 实现 | P1 | 🔍 |
| E-05 | summary 记忆内容为空串时 embedding 报错 | `1fced429` | 索引写入 | P2 | 🔍 |
| E-06 | `mem2_search` 的 `top_k` 为 null 时崩溃 | `02cc37e9` | 外部 provider 调用 | P1 | 🔍 |
| E-07 | LongTermMemory 对 `db_store` 强依赖（无 db 时不可用） | `117b1290` | 记忆装配 | P1 | 🔍 |
| E-08 | 迁移前必须备份每条消息，且**不得删除未备份的记录** | `adce3cda` | 记忆迁移 | P0 | 🔍 |
| E-09 | JiuwenMemory provider 服务端与 `/v1` API 契约漂移（三元组字段） | `df67f86b`、`86b58bc` | provider 适配 | P1 | 🔍 |
| E-10 | team 模式下 memory 工具初始化失败 / 团队记忆生成失败 | `06def473` | `agent_teams/memory` | P1 | 🔍 |
| E-11 | episodic memory 时间抽取不准（"昨天/上周"锚点） | `688b38f5`、`e343eed` | 记忆抽取 | P2 | 🔍 |
| E-12 | graph-memory 关系去重应按 `fact` 字段（当前按实体对，丢关系） | `600b468f` | 图记忆写入 | P1 | 🔍 |
| E-13 | 成员释放时未关闭 memory index manager（句柄泄漏） | `339d64df` | `agent_teams` dispose | P2 | 🔍 |

### F. 会话 / 状态 / 检查点 / VCS

| 编号 | 缺陷（Go 语义） | 上游证据 | Go 落点 | 级别 | 状态 |
| --- | --- | --- | --- | --- | --- |
| F-01 | checkpointer 保存时未释放深拷贝，长会话内存持续增长 | `97020649` | `session/checkpointer` | P1 | 🔍 |
| F-02 | 取消传播时丢失 checkpoint（应保留可恢复点） | `9efe425` | 取消路径 | P0 | 🔍 |
| F-03 | `InteractiveInput` 恢复失败被静默吞掉（应显式报错） | `9efe425` | 恢复分支 | P1 | 🔍 |
| F-04 | 流式 send 在取消时不可中断（goroutine 卡死） | `0e79fb1` | `session` 流写 | P0 | 🔍 |
| F-05 | VCS/rewind：越 head 的 event ref 未拒绝；CRC 应由 JSON dump 计算；应用快照需清除 stale context；状态 delta 路径需转义 `.` | `1ab6a410`、`503d0cdf`、`1db52758`、`3b8b3dd3` | `session/vcs`（Go 若无则 ⤵️） | P1 | 🔍 |
| F-06 | 会话链：`GroupUserSubject` id 中 `:` 需编码；下游 id 需持久化 | `7859059`、`f4e6e9fc` | `session`/`agent_teams/sessionctx` | P1 | 🔍 |
| F-07 | 内存会话释放按前缀匹配导致误释放 | `fbeaeeef` | 会话注册表 | P1 | 🔍 |
| F-08 | `BackgroundTask.wait` 不应取消工作 | `0f5b7008` | `common`/后台任务 | P1 | 🔍 |
| F-09 | checkpointer 并发写覆盖（同一 state 两路写） | `73b80e4c` | checkpointer 锁 | P1 | 🔍 |

### G. 工作流与图引擎

| 编号 | 缺陷（Go 语义） | 上游证据 | Go 落点 | 级别 | 状态 |
| --- | --- | --- | --- | --- | --- |
| G-01 | 嵌套分支的前驱被误判为 AND 组（多分支被要求全部满足） | `5d7cef2c` | `workflow` 条件编译 | P1 | 🔍 |
| G-02 | 条件长度为 None 时判长抛异常 | `7830bc3a` | 条件求值 | P1 | 🔍 |
| G-03 | 条件变量中的布尔子串被误替换（`true`/`false` 字符串） | `c64fc130` | 变量替换 | P1 | 🔍 |
| G-04 | 工作流执行中 deepcopy 未释放（大对象内存翻倍） | `4dbeb1af`、`dbb1e0a1` | 状态传递 | P2 | 🔍 |
| G-05 | 组件回调注册频率从实例级改类级（含两次 revert：最终形态需按上游 HEAD 确认） | `be0e5ca`、`8e8dd32`、`5d8b91e` | 组件回调 | P2 | 🔍 |
| G-06 | nullable node config 往返丢失（`null` 被丢字段） | `31c73fd7` | 工作流 schema | P1 | 🔍 |
| G-07 | pregel 边界应抛 typed error（StoreError）、补 super-step 状态码 | `e45fbeb5`、`b48e26a6` | `graph/pregel` | P1 | 🔍 |

### H. Harness / DeepAgent

| 编号 | 缺陷（Go 语义） | 上游证据 | Go 落点 | 级别 | 状态 |
| --- | --- | --- | --- | --- | --- |
| H-01 | 子代理可绕过父级沙箱限制（sandbox ctx 未继承） | `ac28d891`、`144debbd` | `harness` 子代理装配 | P0 | 🔍 |
| H-02 | plan mode rail 缺静态注入 / 写操作阻断 / 模式恢复 | `6d5491a6` | `harness/rails`（S2 已补三层防御 + plan 指引改附件投递；剩余：`plan_mode_system_note`/`plan_mode_attachment_note` 静态注入口、code 档 `_CODE_PLAN_ALLOWED_TOOLS` 白名单扩展） | P1 | 🔍 |
| H-03 | TaskPlanningRail 用队列外的直塞，消息顺序错乱 | `456ddef5` | planning rail | P1 | 🔍 |
| H-04 | 并发编辑覆盖 + 分块整读检测（write 前必须读完文件） | `73b80e4c` | fs 工具 | P0 | 🔍 |
| H-05 | `write_file` 超长文件名导致 ENAMETOOLONG 崩溃（应返回错误） | `4b3860b5` | fs 工具 | P0 | 🔍 |
| H-06 | 流取消时递归报错 / `aclose` 未取消 | `92340d0c` | 流式控制 | P1 | 🔍 |
| H-07 | task-loop 取消未级联到内层 DEEP 调度任务 | `8c5338a3` | 任务调度 | P1 | 🔍 |
| H-08 | 嵌套 skill 提示不可达 / grouping 目录下技能未被发现 | `a9513636`、`f84b65c1` | 技能发现 | P1 | 🔍 |
| H-09 | LSP 初始化放在模型路径上（阻塞对话）/ rail 初始化阻塞 | `1d144d5a`、`06c4293` | `project_lsp` | P1 | 🔍 |
| H-10 | `RailSpec.build` 遇未知 rail 类型应告警跳过（当前报错中断） | `51d19840` | rail 装配 | P1 | 🔍 |
| H-11 | system prompt section 优先级冲突需静态注册表 + 告警 + 同名排序 | `c1ae5bfa` | prompt 组装 | P2 | 🔍 |
| H-12 | 热重载后 context processor 仍绑旧模型（压缩用旧模型） | `ab1ad620`、`a3677e7c` | 热重载重绑 | P1 | 🔍 |
| H-13 | 热重载后 goal manager 未重绑，协议不再注入 | `c1daffc2` | goal 装配 | P1 | 🔍 |
| H-14 | 模态探测无超时 / 图像拒绝未识别（视觉降级失效） | `4674d6fc`、`b79d1590` | modality probe | P1 | 🔍 |
| H-15 | ReAct 迭代上限命中时应保留已有结果 | `1f33f876` | 循环收尾 | P1 | 🔍 |
| H-16 | 第三方模型请求需作为 protocol 事件上报（破坏性，与 harness_protocol 绑定） | `83aa7194` | ⤵️ 领域十五 | P1 | ⤵️ |
| H-17 | 中断按 kind 应答（审批/提问分流） | `d3ccf8a`、`e0d6ac7` | interrupt 路由 | P1 | 🔍 |

### I. AgentTeams / Swarm

| 编号 | 缺陷（Go 语义） | 上游证据 | Go 落点 | 级别 | 状态 |
| --- | --- | --- | --- | --- | --- |
| I-01 | 发布 task 事件前未清理 `running_tasks`，导致并发 ReAct 循环 | `0769818d` | `agent_teams` 任务表 | P0 | 🔍 |
| I-02 | `completion_timeout` 后未取消僵尸任务，阻塞后续轮次 | `8f3b4d12` | 任务超时 | P1 | 🔍 |
| I-03 | 轮次超时未取消调度器任务 | `60333697` | runtime 调度 | P1 | 🔍 |
| I-04 | 团队删除非幂等（重复删除报错/残留） | `755622e7` | 团队生命周期 | P1 | 🔍 |
| I-05 | worktree 生命周期：并发创建/删除未串行、未开 `core.longpaths`、未锚定项目目录、未随团队清理、未记录 session 归属 | `886654fb`、`f7dbd5d6`、`63efa098`、`cd3041d0`、`361e39d5` | `team_workspace`、worktree | P0 | 🔍 |
| I-06 | 成员注册时未校验 worktree 隔离可行性（运行期才炸） | `0d67e585` | 成员注册 | P1 | 🔍 |
| I-07 | coordination stop 时 messager 未关闭（goroutine 泄漏） | `e3bb3fe7` | `agent_teams/messager` | P1 | 🔍 |
| I-08 | 锁池耗尽超时未重试 | `e9ec6d77` | SQLite 重试 | P1 | 🔍 |
| I-09 | 广播读取未折叠为水位（每次全量扫描） | `c82d7a7d`、`078a941e` | mailbox | P2 | 🔍 |
| I-10 | TeamDatabase：同一 SQLite 文件应共享一个实例（路径写法不同会分裂）、close 需终结、DDL 需共享写锁 | `05c0e82`、`0a9a49b`、`00a6e8e`、`b98a1ca` | team DB 层 | P0 | 🔍 |
| I-11 | session bind 清理竞态（bind 中途被清理） | `b1ea803` | 绑定生命周期 | P1 | 🔍 |
| I-12 | cold resume 仅 leader 可触发；崩溃的外部 CLI 成员需标记 error | `7223d630` | 恢复 | P1 | 🔍 |
| I-13 | 外部 CLI 失败需上报且保留细节（当前只报"失败"） | `721600e`、`8105a36`、`a485d799` | 外部运行时 | P1 | 🔍 |
| I-14 | spawn 边界：校验 `display_name`、拒绝未安装 SDK 的 CLI 成员 | `47a1603`、`1c1b47cf` | spawn 工具 | P1 | 🔍 |
| I-15 | 被动人类成员/humaninbox 自动启动分支错误 | `70d1b482`、`c79d73f` | human inbox | P2 | 🔍 |
| I-16 | 模型组路由失败未处理/未命名实际路由模型 | `af7e668`、`e5dd538`、`56a1963a` | 模型组 | P1 | 🔍 |
| I-17 | 调度模式：重派未交给新 owner、任务修订未同步、内联成员校验 | `d03be0a`、`2fec261`、`8aa9940` | 调度器 | P1 | 🔍 |
| I-18 | 未读消息应按 oldest-first 投递并锚定入站时间 | `16b009f` | mailbox | P2 | 🔍 |
| I-19 | 群聊隔离需固定 runtime mode，否则状态串台 | `030187f` | 群聊 | P1 | 🔍 |

### J. AgentServer / Gateway / 渠道

| 编号 | 缺陷（Go 语义） | 上游证据 | Go 落点 | 级别 | 状态 |
| --- | --- | --- | --- | --- | --- |
| J-01 | Gateway WS 连接 CLOSE-WAIT 泄漏，连带 SIGKILL 全栈停机 | `75afcd1f` | `swarm/gateway` 连接管理 | P0 | 🔍 |
| J-02 | 归档会话时 `request_id` 撞号导致请求被拒 | `eefb003c` | 请求 id 分配 | P1 | 🔍 |
| J-03 | 同会话复用 Web 连接时跨请求事件串扰、`chat.final` 内容为空 | `00344b99` | 会话事件路由 | P1 | 🔍 |
| J-04 | `before_chat` 钩子改写的 query/model 未实时回推前端 | `454b983f` | forward loop | P2 | 🔍 |
| J-05 | 断线重连未补发在途事件（前端丢消息） | `f408e552` | 事件缓冲 | P1 | 🔍 |
| J-06 | 非字符串 `mode` 未在 Web 路由前拒绝（后续 panic） | gateway fix | 入参校验 | P1 | 🔍 |
| J-07 | 完成后 unary `request_id` 不允许复用（长期运行后 id 耗尽） | #4940 | 请求表 | P2 | 🔍 |
| J-08 | 超大 ID 未拒绝 + 日志脱敏正则回溯（CPU/DoS 面） | gateway fix | 校验/日志 | P1 | 🔍 |
| J-09 | 一次性会话（心跳/cron）processor 未回收 → 内存持续增长 | `c2840818`、`6bdc0cfe` | `server/runtime/session` | P0 | 🔍 |
| J-10 | history jsonl 不持久/不兼容/不可逆（需原子写+可回滚） | `0648b1fa` | 历史存储 | P1 | 🔍 |
| J-11 | 重启后历史会话丢 history | `a2418265` | 历史加载 | P1 | 🔍 |
| J-12 | 批量归档删除性能差 + 扫描竞态导致列表偶发加载失败 | `c07c57b1` | 归档扫描 | P2 | 🔍 |
| J-13 | 常驻 team 流阻塞 `session.delete` | `24d1474b` | 删除路径 | P1 | 🔍 |
| J-14 | 普通会话误用项目目录（#5753）/ 上传目录名可越出 sessions 根 | `67b8c033`、`common` fix | 目录解析 | P0 | 🔍 |
| J-15 | 团队流结束时 `request_queue` 未 drain，丢 `team.completed` | `97ce2fda` | team 流 | P1 | 🔍 |
| J-16 | `chat.final` 早于 `seen_team_events` 就复位 `isProcessing` | `6d5cdb5d` | 流收尾 | P1 | 🔍 |
| J-17 | pause 后团队流未退出/暂停态未在生命周期变化时重置 | `af779aa6`、`b5c95ec7`、`3e97637a` | 团队暂停 | P1 | 🔍 |
| J-18 | `permission` 场景出现 "Gateway forwarding queue is full" | `387735f1` | 转发队列背压 | P1 | 🔍 |
| J-19 | 模型配置变更未广播 reload 到所有 channel | `e68a5a15` | 配置热更新 | P2 | 🔍 |
| J-20 | 清理时未释放 adapter 持有的 SysOperations / 工具注册未按 adapter 作用域 | `147433c0` | 适配器生命周期 | P1 | 🔍 |
| J-21 | MCP 相关路由与生命周期：`mcp.*` 未转发 AgentServer、reload 并发叠加 OOM、单 MCP 失败未隔离、prewarm 缺失 | `ea98a452`、`40ce444a` | `server`/`mcp` 桥 | P1 | 🔍 |

### K. 轨迹与可观测性

| 编号 | 缺陷（Go 语义） | 上游证据 | Go 落点 | 级别 | 状态 |
| --- | --- | --- | --- | --- | --- |
| K-01 | span-level trajectory schema 需统一版本（破坏性，旧消费端需适配） | `6758808e` | `evolving/trajectory` | P1 | 🔍 |
| K-02 | 移除废弃的 backend selector（破坏性） | `6100c35f` | 导出配置 | P2 | 🔍 |
| K-03 | TTFT/TTFB 语义：`ttft_seconds` 与 `first_token_time` 区分，仅在带 token 的 chunk 上计时 | `046a869`、`50e9770` | 指标采集 | P2 | 🔍 |
| K-04 | reasoning / cache token 未记录，成本与时长偏差 | `e57fd71`、`df7c717` | usage 属性 | P2 | 🔍 |
| K-05 | 子代理 span 未嵌套在派发它的工具调用下 / 未隔离根 span | `cb26cd0`、`e3048c6`、`7903269` | span 层级 | P1 | 🔍 |
| K-06 | 每次工具调用应只产生一个 `execute_tool` span（当前重复）+ 去重探测失败要 fail-open | `3bb2ede`、`8f101e9` | tool span | P2 | 🔍 |
| K-07 | OTLP 枚举需编码为整数（标准消费端兼容） | `d2525b6` | OTLP 导出 | P2 | 🔍 |
| K-08 | compaction 归属应按 step 而非 request id，且压缩提示词不得进对话链 | `5d7ba67`、`26726e2` | 轨迹重建 | P1 | 🔍 |
| K-09 | 超出保留期的会话轨迹库需清理；Team/休眠会话也需清理 | trajectory fix 系列 | 保留策略 | P1 | 🔍 |
| K-10 | 导入归档需校验并还原导出时的 mode，非法归档要显式报错 | trajectory fix 系列 | 导入导出 | P2 | 🔍 |

### L. 权限与安全

| 编号 | 缺陷（Go 语义） | 上游证据 | Go 落点 | 级别 | 状态 |
| --- | --- | --- | --- | --- | --- |
| L-01 | 内置安全规则不生效（规则集未加载/规则 id 不匹配） | `74fa610` | `harness/rails/security` | P0 | 🔍 |
| L-02 | 安全规则通配符无法匹配非 ASCII（中文命令绕过） | `5ea6772` | 规则匹配 | P0 | 🔍 |
| L-03 | bash 场景规则匹配错误（复合命令/包装命令绕过） | `28405e7`、`44b5edc` | 命令解析 | P0 | 🔍 |
| L-04 | 命中黑名单时审批弹窗未显示完整命令 | `13d226d` | 审批展示 | P1 | 🔍 |
| L-05 | 权限审批 title/body 不一致 + 规则因路径抽取错误重复落盘 | `07d5c4b`、`acfc070` | 审批落盘 | P1 | 🔍 |
| L-06 | 权限规则作用域：全局/用户/会话三层与"仅本次生效"语义 | jiuwenswarm permissions fix 系列 | `permissions` | P1 | 🔍 |
| L-07 | SSL 校验开启但无自定义证书时应允许（当前强制要求证书） | `f9a3cbe` | `common/security/ssl_utils.go` | P1 | 🔍 |
| L-08 | 沙箱 id 跨 agent 隔离 / 沙箱释放失败降级 | `1e3a5c7`、`5cf75bd` | `sys_operation/sandbox` | P1 | 🔍 |
| L-09 | process-cli shell 可绕过文件访问授权（⤵️ 领域二十随功能实现） | process-cli fix | ⤵️ | P1 | ⤵️ |
| L-10 | 工具护栏通配/命令拼接绕过（合并 L-02/L-03 回归用例） | `28405e7` | 测试补齐 | P0 | 🔍 |

### M. 工程性 / 资源 / 性能

| 编号 | 缺陷（Go 语义） | 上游证据 | Go 落点 | 级别 | 状态 |
| --- | --- | --- | --- | --- | --- |
| M-01 | 文件读写锁内存泄漏（fd/lock 表无界增长） | `b7e5d29` | `sys_operation/local/file_lock.go` | P0 | 🔍 |
| M-02 | MQ 会话清理/stop 时未真正摘除订阅（订阅泄漏） | `24a9e52` | `agentcore/message_queue` | P1 | 🔍 |
| M-03 | 回调：无订阅者时应跳过 trigger；函数签名检查需缓存；正则需模块级预编译 | `8d12b4b`、`671f83c`、`64aa426` | `runner/callback` | P2 | 🔍 |
| M-04 | 同步阻塞调用（sleep/同步 IO）阻塞异步链路 | `7824332` | 各 await 点 | P2 | ➖（Go 无 event loop，但需核验串行阻塞） |
| M-05 | worktree 并发创建/删除需串行 + `core.longpaths` | `886654fb` | worktree | P1 | 🔍 |
| M-06 | 日志 sink 去重 / 关闭 enqueue 防 FD 耗尽 | `43e7775`、`f21315e` | `common/logger` | P2 | ➖（zerolog 轮转无此机制） |
| M-07 | 大结果落盘：消息/任务内容溢出到 session 文件，扫描时跳过 | `spill` 系列 | `agent_teams` | P2 | 🔍 |
| M-08 | 快照/状态分页保留需清理超期数据（磁盘无界增长） | retention 系列 | 会话/轨迹保留 | P1 | 🔍 |

### N. 定时任务与心跳

| 编号 | 缺陷（Go 语义） | 上游证据 | Go 落点 | 级别 | 状态 |
| --- | --- | --- | --- | --- | --- |
| N-01 | cron 任务提前 5 分钟触发（时区/`wake_offset` 语义） | cron fix 系列、`b698f77f` | `gateway/cron` | P0 | 🔍 |
| N-02 | reload 丢弃已到点未消费的 wake/push → 循环任务整轮静默丢失 | `e7871c41` | 事件堆重载 | P0 | 🔍 |
| N-03 | 文件版 `update_job` 非原子，并发写回互覆 | `e6e44b4e` | JobStore 写 | P1 | 🔍 |
| N-04 | 多个一次性 cron 同时执行调度冲突 / 一次性标志位未联动编辑 | `82401efc` | 调度器 | P1 | 🔍 |
| N-05 | 定时任务禁止嵌套创建（agent 内再建 cron） | `#5116` | cron 工具 | P1 | 🔍 |
| N-06 | cron 子代理需继承主 agent 模型配置；401 需透传 | cron fix | 子代理装配 | P1 | 🔍 |
| N-07 | 心跳 ask_user resume 准入死锁 + 执行超时抢占 | `83a1d85a` | `gateway/heartbeat` | P1 | 🔍 |
| N-08 | 心跳按 `event_type` 判终态错误（失败轮被记成功） | heartbeat fix | 结果判定 | P1 | 🔍 |
| N-09 | 会话删除前需准备心跳清理，避免资源占用导致删除失败 | heartbeat fix | 删除联动 | P1 | 🔍 |

---

## 4. 执行批次

### 批次一：P0（安全 / 数据 / 崩溃）—— 30 行（D-07 已对齐，实际待办 29 行）

安全与越界：

- D-01 shell 输出上限（**已确认**，Go `ShellOptions` 无上限字段）
- D-04 工具调用超时
- D-05 worktree `gitdir` 越界
- D-06 SkillTool 相对路径
- J-14 会话目录越界 / 项目目录串用
- L-01 内置安全规则不生效
- L-02 通配符非 ASCII
- L-03 bash 复合命令绕过
- L-10 护栏回归用例补齐
- M-01 文件读写锁泄漏

数据与状态：

- A-03 流式异常需消费完流
- A-08 无进展/轮次上限守卫
- B-01 工具结果窗口化缺失
- B-03 取消时丢消息历史
- B-04 取消时保存清洗后上下文
- E-01 消息时区排序错乱
- E-08 迁移前未备份即删除
- F-02 取消丢 checkpoint
- F-04 流式 send 不可中断
- H-01 子代理绕过沙箱
- H-04 并发编辑覆盖 / 读未完整即写
- H-05 `write_file` ENAMETOOLONG 崩溃

团队与运行时：

- I-01 任务事件与 running_tasks 竞态
- I-05 worktree 生命周期
- I-10 TeamDatabase 分裂/关闭/DDL 锁
- J-01 Gateway CLOSE-WAIT 泄漏与全栈停机
- J-09 一次性会话 processor 未回收（内存增长）
- N-01 cron 提前触发
- N-02 cron reload 丢已到点事件

验收：每项补 1 条单测 + 1 条 `-race` 用例；涉及跨子系统的补集成用例；`go test -race ./...` 与 CI 四 job 全绿。

### 批次二：P1 行为对齐（118 行，按组推进）

建议顺序（依赖在前）：

1. **A 组**（ReAct 终止语义）——影响所有上层行为，先做。
2. **B 组 + C 组**（上下文与模型调用）——与 A 组共享流式路径。
3. **D 组**（工具）——依赖 A/B 稳定的执行点。
4. **F/G 组**（会话与图引擎）。
5. **H 组**（Harness）。
6. **I 组**（团队）——依赖 H 的 harness 行为。
7. **J/N 组**（服务端、cron、心跳）。
8. **E/L 组**（记忆与安全）可与上面并行。

### 批次三：P2 可观测性与性能

K 组 + M 组 + 各组的 P2 行。可与批次二交叉进行，但需在批次二收口后统一回归（轨迹 schema 变更会牵动 E2E 断言）。

---

## 5. 通用核验清单（grep 锚点）

| 关注点 | 快速核验命令 |
| --- | --- |
| 取消路径是否提交状态 | `rg -n "ctx.Done\(\)" internal/agentcore/session internal/agentcore/context_engine` |
| 工具执行超时 | `rg -n "context.WithTimeout" internal/agentcore/single_agent/ability internal/agentcore/foundation/tool` |
| shell 输出上限 | `rg -n "max_output\|MaxOutput\|OutputLimit" internal/agentcore/sys_operation` |
| 路径越界 | `rg -n "filepath.Rel\|HasPrefix(relPath" internal/swarm/server/runtime/skill internal/agentcore/harness/tools` |
| checkpoint 深拷贝 | `rg -n "DeepCopy\|Clone" internal/agentcore/session/checkpointer` |
| 消息排序 | `rg -n "ORDER BY" internal/agentcore/memory` |
| 团队任务竞态 | `rg -n "runningTasks\|running_tasks" internal/agent_teams` |
| WS 释放 | `rg -n "Close\(\)\|defer .*Close" internal/swarm/gateway/gateway_server.go` |

---

## 6. 明确不跟进（记录差异，避免反复讨论）

| 项 | 上游 | 不跟进原因 |
| --- | --- | --- |
| loguru sink 去重 / enqueue 关闭（`43e7775`、`f21315e`） | agent-core | Go 用 zerolog + lumberjack 轮转，无 sink 注册表与 FD 耗尽机制 |
| `asyncio` event-loop 修复（`7824332`、`e3e19a95`） | agent-core | Go 无 event loop；等价风险是"同步调用阻塞"，仅保留"配置热更新重建缓存"语义（C-13） |
| Python 3.11/3.13 差异修复（`0f5b7008`、`0b1371b`） | agent-core | 语言运行时差异；仅保留"后台任务 wait 不应取消工作"的语义（F-08） |
| `pydantic` 校验/序列化类修复 | 两仓 | Go 结构体 + 显式校验天然不同构；仅在 schema 契约处等价处理（G-06、I-19） |
| `web/tui/designer/desktop` 前端缺陷（259 条带 scope，另有大量无 scope） | jiuwenswarm | Go 侧无对应前端源码；后端契约相关项已并入 J 组 |
| Python 打包 / `uv.lock` / wheel 修复 | 两仓 | 不适用；Go 依赖见 §7 依赖变更 |

---

## 7. 依赖与命名变更（独立于缺陷，但影响构建）

| 项 | 上游 | Go 侧动作 |
| --- | --- | --- |
| 包重命名 `jiuwenswarm` → `workswarm` | jiuwenswarm `pyproject.toml` | Go 模块名不变；仅文档/发布名对齐（领域二十九） |
| `openjiuwen` 依赖钉死 `c8c621b2` | jiuwenswarm | 仅记录：本项目按 agent-core HEAD 语义对齐 |
| `agentdescent==0.4.6`、`intelli-router==0.1.0b1` | agent-core | Go 侧 RSI PUCT 与 IntelliRouter 为自实现，见领域十七/C-13 |
| CVE 下限（lxml/pillow/python-multipart/filelock） | jiuwenswarm | Go 无对应依赖；等价项：`filelock` → Go file_lock（M-01） |
