# 10.3.19-20 审查偏差修复设计

## 背景

10.3.19-20 (SkillManager Server + SkillDev 管道) 实现审查发现 3 个与 Python 的行为偏差，本文档记录修复方案。

## 偏差 1：BenchmarkRun.RunNumber 默认值

### 问题

Python `BenchmarkRun` 是 dataclass，`run_number: int = 1`；Go 的 `RunNumber int` 零值为 `0`。`aggregateBenchmark` 构建 `BenchmarkRun` 时未显式设置 `RunNumber`，序列化后 `run_number` 为 0 而非 1。

### 方案

在 `evaluate_stage.go` 的 `aggregateBenchmark` 方法中，构造 `BenchmarkRun` 时显式加 `RunNumber: 1`。

### 改动

- 文件：`internal/swarm/server/runtime/skill/skilldev/stages/evaluate_stage.go`
- 位置：`aggregateBenchmark` 中 `run := skilldev.BenchmarkRun{...}` 构造处
- 改动：加 `RunNumber: 1,`

## 偏差 2：Pipeline handler 未找到时的调用链对齐

### 问题

Python 调用链：Pipeline `raise RuntimeError` → 穿过 Service → UapClaw `try/except` 兜底返回 `ok=False`。

Go 调用链：Pipeline goroutine 内 `emit(ERROR) + return` → Service range 正常结束 → UapClaw 返回 `ok=true`（错误藏在 events 里）。

Go 缺少"异常传播到 UapClaw 层兜底"的等价机制。

### 方案

在 `SkillDevPipeline` 上加 `runErr error` 字段。goroutine 内 handler 未找到时赋值 `p.runErr` 后退出。Service 在 range 完 channel 后检查 `pipeline.runErr`，非 nil 时推 errorChunk + logger.Error（对齐 Python UapClaw 的 `except` 兜底）。

channel close 提供 happens-before 保证，`runErr` 的写入先于 channel close，读取在 range 结束后，无数据竞争。

### 改动

1. **`pipeline.go`**：
   - `SkillDevPipeline` 结构体加 `runErr error` 字段
   - handler 未找到分支：去掉 `emit(ERROR)`，改为 `p.runErr = fmt.Errorf("阶段 %s 没有对应的处理器", stage)` + return
   - Run() 签名保持 `(<-chan SkillDevEvent, error)` 不变

2. **`service.go`**：
   - `handleStart` goroutine：range 完后检查 `pipeline.runErr`，非 nil 时推 errorChunk + logger.Error + return（不发 suspended）
   - `handleRespond` goroutine：同理加 `pipeline.runErr` 检查

## 偏差 3：SkillNet Install pending 条件判断

### 问题

Python 在 `_handle_skills_request` 中运行时检查：`handler_name == "handle_skills_skillnet_install" and payload.get("pending")` 时跳过 `create_instance()`。

Go 的 `NeedsRebuild()` 只做静态方法名匹配，`ReqMethodSkillsSkillnetInstall` 始终返回 true，即使安装处于 pending 状态也触发重建。

### 方案

保留 `NeedsRebuild()` 函数，在 `handleSkillsRequest` 调用侧加 pending 覆盖逻辑：`NeedsRebuild` 返回 true 后，若方法是 `skillnet_install` 且 `result["pending"]` 为 true，跳过 `CreateInstance()`。

### 改动

- 文件：`internal/swarm/server/runtime/uapclaw.go`
- 位置：`handleSkillsRequest` 中 `NeedsRebuild` 检查处
- 改动：`if skill.NeedsRebuild(request.ReqMethod)` 内部加 pending override 分支
