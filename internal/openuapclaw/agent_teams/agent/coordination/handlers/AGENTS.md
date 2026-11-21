# Handlers — 场景 handler 的策略逻辑

> 改编自上游 `agent_teams/agent/coordination/handlers/AGENTS.md`。Go 结构见 `doc.go`。

coordination 唤醒层的**决策末梢**。EventDispatcher 只做"是否该 trigger"的粗筛；**"收到这个事件该唤醒
谁、怎么唤醒、要不要唤醒"全部落在本目录**。

## 类契约

一个 handler 一个业务域。每个 handler 声明 event→method 映射并经 `GetCallbacks()`
（对应 Python `get_callbacks()`，沿用 AgentRail 约定）注册给 dispatcher 持有的回调框架。

- **inner event 的 key 用 `InnerEventType.X`；transport event 用 `TeamEvent.X` 裸串**——混用会静默
  注册出一个永不匹配的 key。
- handler 不持有原始 host 引用；依赖分五个 narrow 字段（round/lifecycle/poll/blueprint/infra）。

## 五个决策维度

每个 handler 的策略都是这五问的组合：

| 维度 | 问题 | 典型取值 |
|---|---|---|
| 1. 角色 | 这件事该谁反应？ | leader-only / 非 leader / 角色无关 |
| 2. 自身 vs 他人 | 事件指向我，还是旁观？ | self 分支定向 steer；他人 fall-through 看板或忽略 |
| 3. 投递方式 | 打断当前 round 吗？ | use_steer=True 打断（改派/取消/返工——**再干下去就是白干**）；False 追加（催促/看板巡视——**只是提醒**） |
| 4. 前置闸门 | 有没有"根本不该投"的状态？ | harness_input_blocked / idle 阈值+节流 / 上升沿去重 / round-in-flight |
| 5. 调度模式 | 自主与调度同义吗？ | 不同义就**装配期换类**（Scheduled* 子类），**绝不在方法体里判 dispatch_mode** |

维度 3 最易错：判据不是"消息重不重要"，而是"不打断的话它接下来做的事会不会作废"。

## 各 handler 策略要点（细节以代码注释 `Python: task_board.py L…` 为准）

### AgentLifecycleHandler（5 事件，无状态）
USER_INPUT 直接 deliver_input（路由上游已做完，不再解析 mention）；STANDBY pause_polls；CLEANED 非
leader shutdown_self（**leader 恒 no-op**——persistent leader 必须活过 clean_team）；审批/plan 结果
恢复中断。

### MemberHandler（7 事件，无状态）
leader 分支：6 类 MEMBER_* 纯观测 + 喂活动登记（F_74），**不再跨进程催成员**（F_53 砍掉）；**不自动
clean_team**（把状态当意图读是错的，F_81）。非 leader：MEMBER_CANCELED→cancel；MEMBER_SHUTDOWN 只
处理 force。名册变动触发外部成员 `announce_team_context()`（F_70）。

### MessageHandler（4 事件，无状态）
三条投递路径统一先过 `_harness_input_blocked`（**状态驱动**——状态从 DB 读，不信事件 payload）：
SHUTDOWN 丢弃；SHUTDOWN_REQUESTED+无 in-flight 直接 shutdown_self（不为告别烧一轮 LLM）；
SHUTDOWN_REQUESTED+round 在飞放行 steer。leader 两件事：ack user 伪成员 + 通知 human-agent 回调。
未读按发送时间升序投递（B_04）。投递前 `_expand`（F_63 投递时刻渲染，四个消费点都必须展开）。
投递后 `observeShownRevision` 记 seen 版本（F_129）。

### TaskBoardHandler（15 事件，无状态）

- **A. 定向所有权**（CLAIMED/REVOKED/CANCELLED/UPDATED）：指向自己 steer；UPDATED 例外——只触发
  `announce_task_revisions()`（F_129），handler 自己不出文案。改派经 DAO 原子 CAS，**不发 RELEASED**。
- **B. verify 闸三事件**（F_59）：SUBMITTED_FOR_REVIEW 看 reviewer 列含自己→steer 去 verify_task；
  REVISION_REQUESTED/VERIFIED 的 member_name 是 author→steer 返工/放行找新活。
- **C. 看板巡视**（on_task_board_event→nudgeIdleAgent）：
  - leader 只在可能需要决策的 board 事件上巡视全部未完成任务（F_121）：`_LEADER_QUIET_EVENTS`
    （CLAIMED 兜底/STARTED/SUBMITTED_FOR_REVIEW/REVISION_REQUESTED）与已有 assignee 的 UNBLOCKED
    只续轮询；完成/新建/释放/取消/无主解锁照常唤醒。TASK_COMPLETED/VERIFIED 触发时先列"刚完成"
    摘要（F_120），看板只列未完成任务。
  - teammate 只被 `_TEAMMATE_NUDGE_EVENTS`={CREATED, UNBLOCKED, RELEASED} 唤醒——只扩大可认领池的三类。
  - leader 看板每任务一行不带 content（正文已在其上下文）；teammate 保留 content（靠正文匹配专长）。
  - teammate 只渲染现在就能开始的任务：claimable + 归自己的 pending；筛完为空不唤醒。
  - `resume_polls()` 对每个事件都触发；**板子一律 use_steer=False**；同一批排队的板子被压掉只留最新
    （全量幂等快照，F_71）；leader 的板子不压。
- **OnInitialTaskPoll**（F_69）：成员 runtime 起来时投一次，对称首启邮箱 sweep——收停机期间指派来的
  活；空片静默；**刻意不挂周期 POLL_TASK**。

### StaleTaskHandler（1 事件，4 私有记账）
两个 sweep：成员自催名下 stalled（{PLANNING, IN_PROGRESS}，无活跃回落最早一个 PENDING(assignee=self)，
F_69）+ leader 催无主 PENDING。**停滞时长读 idle 时钟不读 updated_at**（F_65）；排除 IN_REVIEW；
leader 要求 roster 至少一个非 leader READY；投递 use_steer=False；自催连续 3 窗口无效→成员自己
send_message 上报 leader（方向成员→leader）。节流用墙钟秒、停滞判定用 idle 时钟，不混。

### TeamCompletionHandler（3 事件）
leader-only 且 round 真空闲（三条件全否）；三条件完成判定（任务全终态+成员全 settled+无未读）由
TeamBackend 判；上升沿发 TEAM_COMPLETED；persistent 额外 conclude_completed_round；rearm() 每轮独立判定。

### WorkflowHandler / ReliabilityHandler
见 coordination/AGENTS.md 索引表。

## 跨 handler fan-out 三处协作

| event_key | 参与方 | 分工契约 |
|---|---|---|
| MEMBER_SHUTDOWN | MemberHandler → MessageHandler.on_member_shutdown_drain | 先更新状态再 drain 邮箱，顺序有意义 |
| POLL_TASK | StaleTaskHandler → TeamCompletionHandler | **顺序必须**：stale 催促会让 leader 转 busy，完成判定必须看到 |
| TASK_PLAN_RESPONSE | AgentLifecycleHandler → TaskBoardHandler | 靠 `tool_call_id` 存在互斥（plan 闸中断 vs 投递通知） |

**绝不在一个 handler 内部调另一个 handler 的方法。**

## 调度模式变体（F_62）

dispatch_mode 是静态 spec 配置：dispatcher 构造期按字面量表装配 Scheduled* 子类（未知值报错），运行期
不变。调度类重写**组合点**不是逻辑分支：
- `ScheduledTaskBoardHandler`：verify 三闸 + REVIEW_VOTE + UPDATED 全路由到 on_task_board_event（重写为
  resume_polls-only，交接是调度器的 leader 邮箱消息，自反应=双投递）；OnInitialTaskPoll 覆写 no-op（调度器
  handoff 是邮箱持久行）；**继承** CLAIMED/REVOKED/CANCELLED 定向 steer（两模式同义）；覆写撤回/取消 i18n 键
  （scheduled 专属文案）。
- `ScheduledStaleTaskHandler`：只扫自己的 stale 活跃（不做 leader stale-pending 自催）；**钉住 pre-F_65 的
  updated_at 计时**（活跃集含 IN_REVIEW），调度模式行为逐字不变；pause 缺陷仍在，记 F_65 已知遗留。

## 加一个 handler / 事件之前

1. 先问该不该做：是不是应该做成 team tool 让 LLM 自己调（铁律 1）。
2. 新事件 = 对应 handler 加映射 + 方法，**不改 dispatcher 路由**。
3. 按五维度想清楚，特别是维度 3 与维度 5。
4. 跨域响应=两个 handler 注册同一 key + 写清 fan-out 顺序契约。
5. 异常自己记（框架吞普通异常）。
6. 契约变化同步 spec；本目录策略变化同步本文件。
