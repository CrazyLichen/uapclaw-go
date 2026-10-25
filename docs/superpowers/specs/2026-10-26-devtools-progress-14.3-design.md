# 14.3 进度上报设计（devtools agent_builder utils/progress）

- 日期：2026-10-26
- 状态：已批准（brainstorming 会话定稿）
- Python 参考：`openjiuwen/dev_tools/agent_builder/utils/progress.py`（512 行：3 dataclass/class + 1 manager + 1 模块级单例）
- Python 测试：`tests/unit_tests/dev_tools/agent_builder/utils/test_progress.py`（15 用例，3 类：Step 4 / BuildProgress 3 / Reporter 8）+ `tests/system_tests/dev_tools/agent_builder/utils/test_utils_integration.py` 的 `TestProgressIntegration`（4 用例）
- 前置：14.1/14.2 已完成（同包，基线覆盖率 97.2%）

## 0. 已锁定决策（brainstorming 会话）

| 决策点 | 结论 |
|---|---|
| 锁粒度 | **方案 A**：ProgressReporter 单把 `sync.Mutex` 方法级锁，所有变更方法+notify 在锁内执行；GetProgress 返回内部指针（对齐 Python 同对象引用）；ProgressCallback doc 注明"回调内禁止调用同一 reporter 方法"（防死锁） |
| 回调移除 | **方案 A**：`AddCallback(cb) (cancel func())` 返回取消函数（语言特性级等价物，延续 14.2 ParseXxx 收口先例；Go 函数值不可 `==` 比较，Python remove_callback 的函数身份比对不可移植）；cancel 幂等 |
| 单例形态 | **方案 B**：导出 `GetProgressManager() *ProgressManager`（内部 `sync.Once` 惰性初始化 + 非导出 `getGlobalProgressManager`，harness/registry.go 先例）；对齐 Python `from progress import progress_manager` 对象导入语义 |
| 回调签名 | **方案 A**：命名类型 `ProgressCallback func(progress *BuildProgress)`（指针=Python 对象引用；仓库先例 SessionMemoryTaskCallback 等命名回调类型） |
| to_dict 形态 | **方案 A**：`ToDict() map[string]any` + timestamp 用 `time.RFC3339Nano` 字符串（对齐 isoformat 带微秒；仓库 ToDict 5 处先例 + RFC3339 系先例） |
| 默认值构造 | **方案 A**：`NewProgressStep`/`NewBuildProgress` 构造器收口默认值（Details 空 map 非 nil、Timestamp 恒 now 不可传、Duration/Error 指针 nil=Python None）+ Step/Progress 前缀专属选项（`WithStepDetails` 等，规避与 exception.WithDetails 阅读混淆） |
| 章节顺序 | **方案 A**：维持原序立即实施 14.3（依赖零缺口：仅依赖已完成的 enums；14.4/14.5/14.6 是其消费方） |

## 1. 流程位置与作用

```
开发者/前端（REST/WebSocket 轮询 get_progress）
        │
        ▼
AgentBuilder 门面 (14.9 main.py)
        │ build_agent()                       get_progress(session_id)
        ▼                                          │
AgentBuilderExecutor (14.5 executor.py) ───────────┤
        │ 构造器内 progress_manager.create_reporter│
        │ (enable_progress=true 默认开)             ▼
        ▼                                   GetProgressManager()（单例）
BaseAgentBuilder.execute (14.6 base.py) ──→ session_id → ProgressReporter
        │ 模板方法骨架 4 上报点：                   │ callbacks[]（观察者）
        │  · start_stage(INITIALIZING)             │   ↓ notifyLocked
        │  · start_stage(RESOURCE_RETRIEVING)      │  WebSocket/REST 推送
        │  · complete("Build completed")           │
        │  · fail_stage(异常分支)                   │
        ▼
Clarifier/Generator/DLGenerator (14.7/14.8)
        · update_stage / warn_stage / start/complete_stage 细粒度上报
```

作用：**构建进度的单一事实源 + 观察者推送通道 + 跨模块状态中转站**。`BuildProgress`（session 级聚合：当前阶段/状态/消息 + steps[] 时间线 + overall_progress 0-100）被前端轮询（14.9 get_progress → to_dict）与回调推送共用；`ProgressManager` 单例被 executor（创建）、main 门面（查询）、REST/WS（消费）三方共享——devtools"进程内直接调用、不经会话链路"设计的核心落地点。

Python 消费点（已核实）：executor.py L113-118（create_reporter）、main.py L226（get_progress 查询）、base.py L130/139/152/167/196（模板方法 5 上报点含 fail 分支）、llm_agent/builder.py 9 处、workflow/builder.py 3 处（含 warn_stage×2）。

## 2. 文件与声明结构

同包续建零新包：`internal/agentcore/devtools/agentbuilder/utils/`。

```
utils/
├── doc.go              # 目录树补 progress.go / progress_test.go 条目 + 流程位置段落更新
├── progress.go         # 新增：ProgressCallback 类型 + 4 结构体 + 2 选项类型 + 2 进度映射 var + 单例 + 方法群（~350 行）
├── progress_test.go    # 新增：15 unit 1:1 + 4 system 回填 + Go 并发增强（~500 行）
└── (constants/enums/utils 及其测试 6 文件不动)
```

progress.go 声明顺序（规范 2）：结构体区块（ProgressCallback 命名回调类型 → ProgressStep → BuildProgress → ProgressReporter → ProgressManager）→ 枚举区块（ProgressStepOption/BuildProgressOption/StartStageOption/UpdateStageOption/CompleteStageOption/FailStageOption/WarnStageOption 7 个选项类型）→ 常量区块（无）→ 全局变量区块（llmAgentProgressMap/workflowProgressMap 2 张进度映射表 + globalProgressManager + globalProgressManagerOnce）→ 导出函数区块（NewProgressStep/NewBuildProgress/NewProgressReporter/NewProgressManager/GetProgressManager + With 系列选项构造器）→ 非导出函数区块（getGlobalProgressManager/endCurrentStage/calculateProgress/notifyLocked）。

## 3. 组件细节

### 3.1 ProgressStep / BuildProgress（值类型）

```go
// ProgressStep 构建步骤快照（对齐 Python dataclass ProgressStep）
type ProgressStep struct {
    // Stage 构建阶段
    Stage ProgressStage
    // Status 步骤状态
    Status ProgressStatus
    // Message 步骤描述消息
    Message string
    // Details 细节字典（构造器收口为空 map，非 nil）
    Details map[string]any
    // Timestamp 步骤时间戳（构造器恒 now()，对齐 default_factory 不可传参）
    Timestamp time.Time
    // Duration 步骤时长秒（nil=Python None）
    Duration *float64
    // Error 错误消息（nil=Python None）
    Error *string
}

// BuildProgress 构建进度聚合（对齐 Python dataclass BuildProgress）
type BuildProgress struct {
    // SessionID 会话标识
    SessionID string
    // AgentType Agent 类型
    AgentType string
    // CurrentStage 当前阶段
    CurrentStage ProgressStage
    // CurrentStatus 当前状态
    CurrentStatus ProgressStatus
    // CurrentMessage 当前消息
    CurrentMessage string
    // Steps 步骤列表（构造器收口为空切片）
    Steps []ProgressStep
    // OverallProgress 总体进度 0-100
    OverallProgress float64
    // StartTime 开始时间（构造器恒 now()）
    StartTime time.Time
    // LastUpdateTime 最后更新时间（构造器恒 now()）
    LastUpdateTime time.Time
    // Error 错误消息（nil=Python None）
    Error *string
}
```

构造器与选项：

- `NewProgressStep(stage ProgressStage, status ProgressStatus, message string, opts ...ProgressStepOption) ProgressStep`——默认 Details=`map[string]any{}`、Timestamp=time.Now()、Duration/Error=nil；选项 `WithStepDetails(map[string]any)`/`WithStepDuration(float64)`/`WithStepError(string)`
- `NewBuildProgress(sessionID, agentType string, stage ProgressStage, status ProgressStatus, message string, opts ...BuildProgressOption) BuildProgress`——默认 Steps=`[]ProgressStep{}`、OverallProgress=0.0、StartTime/LastUpdateTime=time.Now()、Error=nil；选项 `WithProgressSteps([]ProgressStep)`

ToDict 键名 1:1 对齐 Python to_dict：

- ProgressStep：`stage/status/message/details/timestamp/duration/error`；timestamp=`Format(time.RFC3339Nano)`；duration/error 指针解引用（nil 保持 nil，对齐 Python None）
- BuildProgress：`session_id/agent_type/current_stage/current_status/current_message/steps/overall_progress/start_time/last_update_time/error`；steps 逐项 ToDict

### 3.2 ProgressReporter（观察者核心）

```go
// ProgressReporter 进度上报器（对齐 Python ProgressReporter）
type ProgressReporter struct {
    // sessionID 会话标识
    sessionID string
    // agentType Agent 类型
    agentType string
    // progress 进度聚合（值语义，锁内读写）
    progress BuildProgress
    // callbacks 回调列表（有序切片，锁内增删遍历）
    callbacks []ProgressCallback
    // stepStartTimes 各阶段墙钟起始秒（对齐 Python time.time()）
    stepStartTimes map[ProgressStage]float64
    // mu 方法级锁（方案 A）
    mu sync.Mutex
}
```

- `NewProgressReporter(sessionID, agentType string) *ProgressReporter`——progress 初始为 NewBuildProgress(sid, type, INITIALIZING, PENDING, "Initializing...")（对齐 Python 构造器字面量，注意 Python 此处是 `"Initializing..."` 消息）
- `AddCallback(cb ProgressCallback) (cancel func())`——追加切片，返回 cancel 闭包；cancel 拿锁后按索引移除自身（闭包内 bool 幂等标记），二次调用安全
- `StartStage(stage ProgressStage, message string, opts ...StartStageOption)`——①当前阶段非 INITIALIZING 先 endCurrentStage(SUCCESS)（隐性自动收尾，Python L193-195）②stepStartTimes[stage]=墙钟秒 ③更新 current_stage/status=RUNNING/message/last_update_time ④显式 progress 选项则用，否则 calculateProgress(stage) ⑤append NewProgressStep(stage, RUNNING, message, WithStepDetails(details)) ⑥Info 日志（session_id/stage/message 三字段）⑦notifyLocked
- `UpdateStage(opts ...UpdateStageOption)`——message 选项则改 current_message；details 选项且 steps 非空则 `steps[len-1].Details` 逐键合并（对齐 Python dict.update）；progress 选项则改 overall_progress；恒改 last_update_time；notifyLocked。**不建新 step**
- `CompleteStage(opts ...CompleteStageOption)`——endCurrentStage(SUCCESS, message, details)
- `FailStage(errMsg string, opts ...FailStageOption)`——先 `progress.Error=&errMsg` 再 endCurrentStage(FAILED, message, details, errMsg)
- `WarnStage(warning string, opts ...WarnStageOption)`——message 选项则改 current_message；steps 非空则 `steps[len-1].Status=WARNING` + `steps[len-1].Details["warning"]=warning` + details 选项逐键合并；current_status=WARNING；last_update_time；Warn 日志（session_id/stage/warning 三字段）；notifyLocked。**不建新 step**
- `GetProgress() *BuildProgress`——锁内返回 `&r.progress` 指针（对齐 Python 返回同一对象；**doc 注明：跨 goroutine 读取须走 ToDict 或在回调内读**）
- `Complete(message ...string)`——变参收默认 "Build completed"；current_stage=COMPLETED/status=SUCCESS/message/overall_progress=100.0/last_update_time；steps 非空再 endCurrentStage(SUCCESS, message)；Info 日志（session_id/agent_type 两字段）；notifyLocked

**方法选项构造器**（导出函数区块，对齐 Python Optional 形参——`StartStage(stage, message, details=None, progress=None)` 等可选形参）：

**最终裁定**（按 Python 形参实际可选项生成，`With<方法名><字段名>` 全限定命名无歧义）：

- `StartStageOption`：`WithStartStageDetails` / `WithStartStageProgress`（message 是必填首参，不需要选项）
- `UpdateStageOption`：`WithUpdateStageMessage` / `WithUpdateStageDetails` / `WithUpdateStageProgress`
- `CompleteStageOption`：`WithCompleteStageMessage` / `WithCompleteStageDetails`
- `FailStageOption`：`WithFailStageMessage` / `WithFailStageDetails`
- `WarnStageOption`：`WithWarnStageMessage` / `WithWarnStageDetails`（warning 是必填首参）

共 11 个薄构造器，各自返回对应方法专属选项类型（Go 无联合类型，5 个选项类型独立定义）。

- `notifyLocked()`——锁内遍历 callbacks；每回调 `defer func(){ recover → logger.Error(error/callback/session_id 三字段) }()`（对齐 Python except Exception 吞异常只记日志，L164-175）
- `endCurrentStage(status, message *string, details map[string]any, errMsg *string)`——steps 空→直接 return（Python L330-331）；末 step.Status=status；message 非空则改 step.message+current_message；details 逐键合并；errMsg 非空则 step.Error+progress.Error；stage 在 stepStartTimes 则算墙钟差写 step.Duration 并**删键**（Python L347-351）；current_status=status；last_update_time
- `calculateProgress(stage) float64`——agentType=="llm_agent" 查 llmAgentProgressMap、=="workflow" 查 workflowProgressMap，未命中键返回 0.0；未知 agentType 返回 0.0（Python L392-397）

### 3.3 ProgressManager + 单例

```go
// ProgressManager 进度管理器（对齐 Python ProgressManager）
type ProgressManager struct {
    // mu 读写锁
    mu sync.RWMutex
    // reporters 会话→上报器注册表
    reporters map[string]*ProgressReporter
}
```

- `NewProgressManager() *ProgressManager`——独立实例构造（测试隔离用，Python system 测试同款 `ProgressManager()` 直构）
- `GetReporter(sessionID string) *ProgressReporter`——nil=Python None
- `CreateReporter(sessionID, agentType string) *ProgressReporter`——已存在返回既有实例（Python L480-481 幂等语义）
- `RemoveReporter(sessionID string)`——存在则删
- `GetProgress(sessionID string) *BuildProgress`——reporter 存在则转发其 GetProgress，否则 nil。**两锁不嵌套**：manager 锁内只取 reporter 指针，锁释放后再调 reporter 方法（防锁序死锁）
- 包级 `GetProgressManager() *ProgressManager`——`getGlobalProgressManager()` sync.Once 惰性初始化（globalProgressManager/globalProgressManagerOnce 非导出全局变量，harness/registry.go L51-55+L197-206 先例）

### 3.4 进度映射表（全局变量区块）

```go
// llmAgentProgressMap LLM Agent 阶段进度映射（Python L370-377）
var llmAgentProgressMap = map[ProgressStage]float64{
    ProgressStageInitializing: 0, ProgressStageClarifying: 20,
    ProgressStageResourceRetrieving: 40, ProgressStageGeneratingConfig: 60,
    ProgressStageTransformingDSL: 80, ProgressStageCompleted: 100,
}

// workflowProgressMap Workflow Agent 阶段进度映射（Python L380-390）
var workflowProgressMap = map[ProgressStage]float64{
    ProgressStageInitializing: 0, ProgressStageDetectingIntention: 10,
    ProgressStageGeneratingWorkflowDesign: 25, ProgressStageGeneratingDL: 45,
    ProgressStageValidatingDL: 60, ProgressStageRefiningDL: 70,
    ProgressStageTransformingMermaid: 85, ProgressStageTransformingWorkflowDSL: 95,
    ProgressStageCompleted: 100,
}
```

## 4. 显式偏差记录（Go↔Python）

1. **remove_callback → cancel 函数**：Go 函数值不可 `==` 比较（编译错误），`reflect.ValueOf(f).Pointer()` 仅代码指针（闭包误判相等/方法值误判不等）。取消函数是语言特性级等价物，语义对齐 Python "add 后移除不再通知"。Python 生产代码 add/remove_callback 实际零消费（仅 docstring 示例+测试消费），属死代码全量对齐范畴
2. **datetime.isoformat() → RFC3339Nano**：Python isoformat 默认微秒精度（2026-10-26T12:34:56.789012+00:00）；Go RFC3339Nano 纳秒自适应（尾零截断）。字符串形态有亚微秒级差异，语义（ISO8601 UTC 带亚秒精度）等价。**时区（2026-10-26 审查差异 1，修复后已消除）**：Python 8 处 `datetime.now(timezone.utc)` 为 UTC aware，Go 初版 8 处 `time.Now()` 漏 `.UTC()`（本地时间，ToDict 输出本地偏移）——已修复为 `time.Now().UTC()` 全 8 处（NewProgressStep.Timestamp、NewBuildProgress.StartTime/LastUpdateTime、5 处方法 LastUpdateTime 更新点），ToDict 现输出 `Z`/`+00:00` 对齐 isoformat 的 `+00:00` 形态；验证用例 TestProgressUTC_时间戳语义。`nowWallClock()`（UnixNano 墙钟秒）时区无关，对齐 Python `time.time()` 保持不动
3. **time.time() 墙钟 → time.Now().Unix()**：Python 用墙钟算 duration（非单调钟，系统改时会跳变——Python 原始缺陷原样保留，不"顺手优化"为单调钟；对齐原则：Python 有意/无意行为都是契约）
4. **回调异常 → panic recover**：Python except Exception 捕获回调异常记日志继续；Go 回调 error 返回值无法强制，panic recover 是等价防御。注：Go 回调若只是返回 error 而不 panic，则静默忽略（Python 异常=Go panic 的等价映射下，返回 error 的回调约定不检查）
5. **details dict.update → 逐键合并**：Python `d.update(other)` 原地覆盖合并；Go 等价逐键 for-range 赋值（nil map 守卫：Details 恒非 nil 由构造器收口保证）
6. **`InitProgressStep` 不设**：Python dataclass 字段直接可写（progress.steps[-1].status = ...）；Go 值语义结构体经 `&r.progress.Steps[len-1]` 直接字段写，无需 setter 辅助

## 4b. 隐性语义保真点（易丢，逐条核对自 Python 源码）

1. **start_stage 自动收尾**（L193-195）：当前 stage 非 INITIALIZING 时先 endCurrentStage(SUCCESS)——首个 start_stage(INITIALIZING) 不触发（首步无前序）；StartStage(RESOURCE_RETRIEVING) 会先把 INITIALIZING 收尾为 SUCCESS
2. **stepStartTimes 删键**（L347-351）：endCurrentStage 算完 duration 必删键——同 stage 二次进入重新计时
3. **update_stage 只改末 step**（L247-248）：details 合并进 steps[-1].details，不建新 step、不改 current_status
4. **warn_stage 不建 step**（L304-308）：直接改末 step 的 status=WARNING 和 details["warning"]
5. **fail_stage 双写 error**（L284-285）：progress.error 先写，endCurrentStage 内再写 step.error（同一值两处）
6. **complete 变参默认**（L408）：`Complete()` 无参 → "Build completed"；steps 空时不 endCurrentStage（L422-423 if 守卫）
7. **create_reporter 幂等**（L480-481）：同 sessionID 返回既有实例（agentType 参数被忽略）
8. **_calculate_progress 双兜底**（L392-397）：agentType 不匹配两 map 键 → 0.0；未知 agentType → 0.0
9. **_notify 遍历序**（L166-175）：按 add 顺序同步遍历；单回调异常不中断后续回调
10. **BuildProgress 构造的 "Initializing..." 消息**（L140）：Python 字面量三点省略号，Go 保持同串

## 5. 测试对齐（progress_test.go 统一收口）

### 5.1 unit 1:1（15 个，Python test_progress.py 全量映射）

| Python 类.方法 | Go 测试 | 断言要点 |
|---|---|---|
| TestProgressStep.test_progress_step_creation | TestProgressStep_创建 | Details==空map非nil、Duration/Error nil、Timestamp 非 zero |
| TestProgressStep.test_progress_step_with_details | TestProgressStep_携带细节 | Details 等值 |
| TestProgressStep.test_progress_step_to_dict | TestProgressStep_ToDict | 7 键存在、stage/status 字符串值、duration 1.5、timestamp 可解析 RFC3339 |
| TestProgressStep.test_progress_step_with_error | TestProgressStep_携带错误 | Error 解引用等值、ToDict error 键 |
| TestBuildProgress.test_build_progress_creation | TestBuildProgress_创建 | 字段等值、Steps 空切片、OverallProgress 0.0 |
| TestBuildProgress.test_build_progress_to_dict | TestBuildProgress_ToDict | 10 键存在、current_stage/completed 串、start_time/last_update_time 可解析 |
| TestBuildProgress.test_build_progress_with_steps | TestBuildProgress_携带步骤 | ToDict steps 数组 len==2 |
| TestProgressReporter.test_progress_reporter_creation | TestProgressReporter_创建 | progress.CurrentStage==INITIALIZING、CurrentStatus==PENDING |
| TestProgressReporter.test_add_callback | TestProgressReporter_添加回调 | StartStage 触发 1 次、回调收到 sessionID |
| TestProgressReporter.test_remove_callback | TestProgressReporter_取消回调（cancel 语义改写） | add→cancel→StartStage→零调用 |
| TestProgressReporter.test_start_stage | TestProgressReporter_开始阶段 | stage/status/message/overall_progress==20.0（显式选项） |
| TestProgressReporter.test_complete_stage | TestProgressReporter_完成阶段 | CurrentStatus==SUCCESS |
| TestProgressReporter.test_fail_stage | TestProgressReporter_失败阶段 | CurrentStatus==FAILED、progress.Error 解引用=="Test error" |
| TestProgressReporter.test_complete | TestProgressReporter_整体完成 | COMPLETED/SUCCESS/overall_progress==100.0 |
| TestProgressReporter.test_multiple_stages | TestProgressReporter_多阶段推进 | 三段 start→complete 状态机推进 |
| （合并入上）update_stage/warn_stage 无 Python unit 用例 | TestProgressReporter_更新阶段 / 警告阶段（增强） | Go 补齐：末 step details 合并不建新 step；warning 写入 details["warning"] |

注：Python 15 个 `def test_` 已 grep 机器核对（Step 4/Progress 3/Reporter 8）；update/warn 两个方法 Python 无 unit 覆盖，Go 增强补齐（14.1 LoadJSONFile 增强先例）。

### 5.2 system 回填（4 个，TestProgressIntegration——14.1 spec 拆分归属本节，兑现承诺）

| Python 方法 | Go 测试 | 断言要点 |
|---|---|---|
| test_progress_reporter_with_callbacks | TestProgressIntegration_回调上报 | start+complete_stage → events≥1 |
| test_build_progress_serialization | TestProgressIntegration_序列化 | ToDict 含 session_id/steps 键、session_id 等值 |
| test_progress_manager_integration | TestProgressIntegration_管理器 | Create→Get 同指针（`retrieved is reporter` 等价） |
| test_progress_reporter_complete_workflow | TestProgressIntegration_完整工作流 | 三段式→COMPLETED+100.0 |

### 5.3 Go 增强用例（并发语义，Python 无对应）

- TestProgressReporter_并发阶段操作：多 goroutine StartStage/CompleteStage/UpdateStage 压测 + `-race` 验证方法级锁
- TestProgressReporter_并发回调注册：多 goroutine AddCallback/cancel 压测
- TestProgressReporter_取消幂等：cancel 两次 + cancel 后 notify 零到达
- TestProgressReporter_回调panic不中断：两个回调，第一个 panic，第二个仍被调用（对齐 Python except 语义）
- TestProgressReporter_回调内读进度：锁内安全窗口验证（回调内读 progress 字段 + ToDict）
- TestProgressManager_并发注册：多 goroutine Create/Get/Remove
- TestProgressReporter_自动进度计算：calculateProgress 三分支（llm_agent 显式映射值/workflow 映射值/未知 agentType→0.0；未映射 stage→0.0）
- TestProgressStep_ToDict_空指针形态：duration/error nil → ToDict 对应键为 nil
- TestProgressReporter_隐性自动收尾：StartStage(INITIALIZING)→StartStage(CLARIFYING)→steps[0].Status==SUCCESS（§4b-1 验证）
- TestProgressManager_幂等创建：同 sessionID 两次 CreateReporter 同指针（§4b-7 验证）
- TestGetProgressManager_单例：两次调用同指针

### 5.4 时间断言策略

duration 用例：StartStage 后 CompleteStage，断言 Duration 非 nil 且 ≥0（不做精确值断言防 flaky）；timestamp 断言可解析 RFC3339 即可。**不引入时钟注入接口**——Python 无时钟抽象，全量对齐原则下不添加（对齐 13.16/14.1 决策先例：不自行添加 Python 没有的抽象）。

### 5.5 计数汇总（全部 grep -rn "def test_" 机器核对 2026-10-26）

- Python unit test_progress.py：15（✓ 机器核对）
- Python system TestProgressIntegration：4（✓ L87/103/119/131）
- Python utils 系 system 全文件：12 方法 = TestUtilsIntegration 4 + TestProgressIntegration 4 + TestEnumIntegration 4（✓）
- **本节完成后 utils 目录测试 100% 回填**：unit 84/84（utils 36+constants 21+enums 12+progress 15）+ system 12/12 全清偿
- agent_builder 全目录实际基数（**机器核对，修正 14.10 行计划数字**）：unit **583**（utils 84/builders 287/executor 38/resource 30/skill_omni_creation 125/根 test_main+conftest 19）+ system **314**（utils 12/builders 272/executor 9/resource 9/根 test_main_integration 12）。**IMPLEMENTATION_PLAN.md 14.10 行 "36 system + 54 unit" 与实际严重不符（约只覆盖 utils 系），14.10 收口前须修正基数**
- **Scope 发现（须 14.10 前裁决）**：`tests/unit_tests/dev_tools/agent_builder/skill_omni_creation/`（125 test）在领域十四 14.1-14.14 章节规划中**零出现**——对应 Python 生产代码归属需核实（推测对应 dev_tools 其他模块或 builder 链），处置待裁

## 6. 日志同步（规则 3）

Python progress.py 4 处 logger 调用（机器核对 L170/L221/L313/L425，设计呈现时误记为 5 处已修正）：

| Python 位置 | 级别 | Go 落位 | 字段 |
|---|---|---|---|
| _notify except（L170） | error | notifyLocked recover 分支 | Error(logComponent).Str("error",...).Str("callback",...).Str("session_id",...) — Msg "Progress callback execution failed" |
| start_stage（L221） | info | StartStage | Info(logComponent).Str("session_id",...).Str("stage",...).Str("message",...) — Msg "Started build stage" |
| warn_stage（L313） | warning | WarnStage | Warn(logComponent).Str("session_id",...).Str("stage",...).Str("warning",...) — Msg "Build stage warning" |
| complete（L425） | info | Complete | Info(logComponent).Str("session_id",...).Str("agent_type",...) — Msg "Build completed" |

日志消息保留英文（记忆规则：logger.Msg 保留英文）；组件 `logger.ComponentAgentCore`（14.1 utils.go L17 先例，agentcore 下所有包归此组件）。

## 7. 章节衔接

- **上游**：14.2 enums——ProgressStage/ProgressStatus 25 常量作字段类型直接消费（零转换成本）；14.1 无直接依赖（progress 不用工具函数）
- **下游 14.5 executor**（第一个真实消费者）：`GetProgressManager().CreateReporter(sessionID, agentType)`（enable_progress 默认 true 语义届时在 executor 构造器落地）；reporter 注入 builder
- **下游 14.6 base.py 模板方法**：execute() 骨架 5 上报点（INITIALIZING start/RESOURCE_RETRIEVING start+complete/complete/fail 分支）
- **下游 14.7/14.8 builders**：update_stage/warn_stage 细粒度上报（llm_agent 9 处、workflow 3 处含 warn×2）
- **下游 14.9 门面**：`GetProgressManager().GetProgress(sid)` → ToDict → JSON 序列化给前端
- **章节顺序维持原序**（brainstorming 复核）：14.3 仅依赖已完成 enums，零前置缺口；14.4 resource 不依赖 progress（只依赖 14.1 工具函数+LLM 基础设施）理论可互换但无收益；14.3 先行使 14.5/14.6 落地即有完整进度链路可测。**无其他章节需要提前**（14.10 测试收口节、14.11-14.14 处置待裁维持）

## 8. 回填清单（不能丢）

| # | 回填项 | 目标位置 | 时机 |
|---|--------|---------|------|
| 1 | 14.3 行 ☐→✅ + 产出摘要（4 类型+单例+进度映射+选项机制+15 unit+4 system 回填+并发增强） | IMPLEMENTATION_PLAN.md | 实施完成后 |
| 2 | doc.go 文件目录补 progress.go/progress_test.go 条目 + 流程位置段落更新（"14.5 executor 创建 reporter、14.6 模板方法埋上报点"） | utils/doc.go | 实施完成后 |
| 3 | 14.1 spec 归属拆分的 4 个 system 用例回填兑现（⤴️ 义务清偿；utils 系 system 12/12 全清） | progress_test.go §5.2 | 本节实施时 |
| 4 | 14.10 行测试基数修正：原 "36 system + 54 unit" 与实际严重不符（机器核对实际 unit 583/system 314，原数字仅覆盖 utils 系）；同时裁决 skill_omni_creation 125 测试归属 | 本 spec §5.5 已归档核对结果，14.10 实施时引用修正 | 14.10 实施时 |
| 5 | 记忆 domain-14-devtools-progress.md 更新 14.3 状态+决策+关键坑 | 记忆文件 | 实施完成后 |
| 6 | IMPLEMENTATION_PLAN.md 14.3 行原 ⤴️ 标记清除（义务清偿后） | IMPLEMENTATION_PLAN.md | 实施完成后 |

## 9. 验收标准

1. progress.go + progress_test.go + doc.go 更新落地；gofmt + 声明顺序合规（规范 2：结构体→枚举(选项类型)→常量→全局变量→导出→非导出）
2. `go test ./internal/agentcore/devtools/agentbuilder/utils/ -cover` ≥ 85%（包基线 97.2%，progress.go 预估 95%+，包级不降）
3. `go test -race ./internal/agentcore/devtools/agentbuilder/utils/` PASS（并发用例 + 增强用例全过）
4. 全部用例 PASS：15 unit 1:1 + 4 system 回填 + ~11 并发/语义增强
5. 注释全中文；doc.go 树含新条目；Python 对应路径不变
6. 规则 6/7 不适用（纯内存观察者模式无集成测试需求，单包 unit 即完成态；无 build tag 隔离需求——时钟用 time.Now() 直调，测试不做精确时间断言故无需注入）
7. §8 回填清单 1/2/3/5/6 全部执行（4 留给 14.10）
