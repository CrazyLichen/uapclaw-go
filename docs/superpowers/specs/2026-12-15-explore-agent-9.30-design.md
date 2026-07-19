# 9.30 ExploreAgent 实现设计

## 概述

ExploreAgent 是只读代码库导航子代理，定位、读取并汇报代码信息，禁止任何写操作。

## Python 对齐要点

1. ExploreAgent **不设 `factory_name`**，在 `deep_agent.create_subagent` 中走通用 `create_deep_agent()` 路径
2. Python 中 `build_explore_agent_config` 和 `create_explore_agent` 的默认 Rails 不同（build 用 `read_only=True`，create 用无参数），Go 统一为 `SysOperationRail(WithReadOnly(true))`
3. 工具完全通过 `SysOperationRail(read_only=True)` 注册：`read_file, glob, list_dir, grep, bash(只读)`
4. 不依赖 9.38-49 Harness 工具集

## PlanAgent 偏差修正

Go 中 PlanAgent 错误地设了 `cfg.FactoryName` 和 `deep_agent.go` 专用分支，Python 中 plan_agent 不设 `factory_name`，走通用路径。需修正：
- 删除 `subagents/plan_agent.go` 中 `cfg.FactoryName = PlanAgentFactoryName`
- 删除 `deep_agent.go` 中 `case "plan_agent"` 分支
- 修正 `plan_agent_test.go` 中 FactoryName 断言

## 实现范围

### 新增文件

1. **`internal/agentcore/harness/subagents/explore_agent.go`** — 配置层
   - 常量 `ExploreAgentFactoryName = "explore_agent"`（仅用于 AgentCard.name）
   - 双语系统提示词（一比一复刻 Python 原文）
   - 双语描述（一比一复刻 Python 原文）
   - `BuildExploreAgentConfig(model, params)` 返回 `*SubAgentConfig`
   - `DefaultExploreAgentSystemPrompt(language)` 辅助函数
   - `DefaultExploreAgentDescription(language)` 辅助函数

2. **`internal/agentcore/harness/explore_agent_factory.go`** — 运行时层
   - `CreateExploreAgent(ctx, params)` 返回 `(*DeepAgent, error)`
   - Full override rule：默认 `SysOperationRail(WithReadOnly(true))`

3. **`internal/agentcore/harness/subagents/explore_agent_test.go`** — 单元测试

### 修改文件

4. **`internal/agentcore/harness/subagents/plan_agent.go`** — 删除 `cfg.FactoryName` 赋值
5. **`internal/agentcore/harness/deep_agent.go`** — 删除 `case "plan_agent"` 分支
6. **`internal/agentcore/harness/subagents/plan_agent_test.go`** — 修正 FactoryName 断言

## 关键配置参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| FactoryName | 不设 | 对齐 Python，走通用路径 |
| MaxIterations | 15 | Python 默认值 |
| RestrictToWorkDir | false | Python `restrict_to_work_dir=False` |
| 默认 Rails | `SysOperationRail(WithReadOnly(true))` | Go 统一增强 |
