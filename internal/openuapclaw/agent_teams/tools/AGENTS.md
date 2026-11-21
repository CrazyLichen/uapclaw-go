# Agent Team Tools

> 改编自上游 `agent_teams/tools/AGENTS.md`（Go 对齐版）。Go 侧结构见本目录 `doc.go`。

## 模块地图

| Go 文件 | 负责 |
|---|---|
| `team_tool.go` | `TeamTool` 基类 + 权限集合（`LeaderOnlyTools`/`MemberTools`/`HumanAgentToolsStr` 等）+ ToolCard ID 前缀 `team.{name}` |
| `team_tools.go` | 全部工具实现（Go 按上游合并形态落在一个文件；上游按领域拆 tool_task/tool_message/tool_team/tool_member 的映射见函数注释 `Python: xxx`） |
| `tool_member.go` / `tool_expert.go` / `spawn_expert.go` | 成员类工具（spawn/shutdown/set_member_model/approve…）与专家目录工具（25.9 部分） |
| `task_manager.go` | `TeamTaskManager`：add_graph 原子批量图创建 / claim / complete（评审门）/ reassign（原子 CAS）/ reset / cancel / approve_plan / 事件发布 / 计划索引（index.json，plansDir 必填守卫） |
| `message_manager.go` | `TeamMessageManager`：点对点 + 广播 + 已读 |
| `team_backend.go` | `TeamBackend`：spawn/shutdown/clean/approve、名册查询、`AutostartUnstarted`/`StartupMember` CAS 链、`on_team_built`/`on_team_cleaned` 回调 |
| `database/` | `TeamDatabase` + SQL/内存双实现 DAO（`sql_task_dao.go` 等）；SQL 层跑 sqlite（CGO sqlite_fts5），测试用内存实现 |
| `async_tool_host.go` | async 管控工具宿主（25.1d；⤴️ spill 目录接线 + `async_tasks_*` 控制工具随 25.12） |
| `locales/` | desc 文案（cn/en，从上游复制的 `descs/<lang>/<domain>/<tool>.md`）+ i18n STRINGS |
| `index.json` | （已删除）曾为测试产物污染，见下文守卫说明 |

工具从不直接伸手进 `TeamDatabase`——一律经 `TeamBackend` 或某个 manager，事件发布与状态流转保持集中。

## 数据库并发（SQLite 写串行化）

- 读写引擎分离：文件后端 SQLite 跑单写者池 + 读者池；写在应用级锁上串行；`:memory:` 共享单连接。
- `database/sql_engine.go` 持进程级写锁；**写锁不可重入**——写委托给写时只有最内层 opener 取锁。
- PRAGMA：WAL + synchronous=NORMAL + wal_autocheckpoint；写者大 cache / 读者小 cache。
- 批量写：`MarkMessagesRead` 单事务排空整个 mailbox（一次 fsync）。
- `retry_on_locked` 有界退避，sleep 在 session 块之外。
- 索引按查询驱动、压低 INSERT 写放大；动态表不建 `team_name` 索引（按 session 分区基数≈1）。
- 高并发写部署应用 PostgreSQL/MySQL，不用 SQLite。

## 读取只取所需，校验落在边界

1. **读取粒度对齐消费方**：存在性用 EXISTS / 窄投影，不为轻字段拉全行。成员读取三档：
   `member_exists` / `list_member_roster` 投影 / `get_member` 全行。
2. **不可信输入的校验放边界层**：LLM 传入的名字/ID 在工具边界校验一次，不下推进共享热路径
   （如 `SendMessageTool` 的收件人校验）。

## 工具目录与角色过滤（要点）

`create_team_tools`（Go：`team_tools.go` 的 `NewTeamTools`/`CreateTeamTools(TeamToolOptions)`）是唯一入口。
完整工具权限表与每个工具的行为契约（build_team 幂等接管 F_76 / create_task add_graph / update_task 五合一
/ claim_task one-active / member_complete_task 评审门+完成闸 / verify_task 双模式语义 / send_message
双形态+MAX_CONTENT_CHARS / view_task action 分档）**逐字对齐上游**，此处不重复——以 Python `tool_task.py`
/ `tool_message.py` 与本仓各工具 `Invoke` 注释为准。Go 已交付的关键差异点：

- 改派走 `TaskManager.Reassign`（DAO 原子 CAS，状态保留，发 `TASK_REVOKED`+`TASK_CLAIMED` 不发
  `TASK_RELEASED`；`TaskReassignableStatuses={PENDING,BLOCKED,IN_PROGRESS}`，F_82）；旧的
  CancelMember+Reset+Assign 路径已删（锚点归零）。
- one-active 不变量：`GetOtherActiveTaskID`（PLANNING/IN_PROGRESS/IN_REVIEW；exclude 幂等豁免）在
  claim/assign/start_task 三入口与 update_task 工具边界强制。
- `writePlanIndex`/`loadPlanIndex` 空 plansDir 守卫：拒绝写盘（历史上有测试经 CWD 相对路径把假数据写进
  仓库 `tools/index.json`，已根治并删除该文件）。

Plan-mode 门控 / persistent 团队门控（persistent 减 `clean_team`）在工厂强制，理由同上游。

## 工具形态（tool variants，F_57）

一个**形态**保持 ToolCard id/name 不变，替换 schema、描述与行为。三正交维度：注册与否（权限集减法）、
同名不同形态（构造期查表选类）、同形态不同文案（desc_key）。**形态选择只发生在工厂构造 `all_tools`
那一刻，永远不在 Invoke 里**——每个工具实例 schema 扁平、Invoke 直线、零 role/mode 分支。
共享判据：纯数据/纯函数用模块级函数，有状态行为才上基类。双层执法：schema enum 给宿主 LLM，
Invoke 白名单给 MCP 客户端，两层都必须有。

## 工具设计原则

- **描述 = 行为契约**：用例枚举 + 行为约束 + 成本信号；入口工具（build_team）承载完整工作流。
- 反模式纠正：鼓励行动 / 消除忙等 / 正常化沉默 / 强制通道 / 标注成本。
- 参数合并（desc 与 prompt 合一）；一个工具一个关切但只差一个参数的不拆。
- 输入校验三级（必填→存在→业务规则）；错误文案约定四类。
- 成功输出带结构化 `data`；失败必须把 `result.reason` 传进 error（LLM 诊断通道）。
- Invoke 兜底捕获，永不裸 panic 上抛。

## 结果渲染：`RenderForLLM`

同步 `TeamTool` 的 LLM 可见文本唯一来源；分四档（纯文本确认 / 结构化行 / 详情 / 文本+行为引导）+
时间上下文（`timefmt.FormatTimeContext`）。async 工具两段式文本：launch 段走 `RenderForLLM`、终态段走
`format_completed/failed` 注入（见 `harness/AGENTS.md`）。外部 CLI 成员复用同一套工具与渲染（F_26）——
保持角色中立。

## i18n

- Key 约定：`tool_name._desc` / `tool_name.param` / 嵌套 `tool_name.nested.param`。
- 长描述放 `locales/descs/<lang>/<domain>/<desc_key>.md`；`desc_key` 扁平全局唯一，目录只是分组。
- `{{slot}}` 片段填充：`descs/<lang>/fragments/<slot>.md`；**capability 槽**（如 `fork_usage`、
  `update_task_verify_gate`）omit 信号必须与塑造 schema 的信号同源——能力关掉文案跟着消失；
  omit 槽渲染后 strip + 折叠 3+ 连续换行，desc/fragment 不要故意用连续空行。
- 缺 key 构造期大声失败，不静默。

## Prompt 分层

| 层 | 拥有 |
|---|---|
| 工具描述（locales/descs/） | 操作流程、调用顺序、工作流步骤、反模式 |
| 系统提示词（prompts/） | 角色身份、决策原则、状态流转 |

**不要跨层重复**；分层 ≠ 投递时刻（F_76：leader 侧 prompts 内容经 build_team 返回值下发，归属不变）。

## ToolCard ID 约定

所有团队工具 ID 用 `team.{name}`（如 `team.send_message`）；下游接线（rail、日志、UI）解析此前缀。
