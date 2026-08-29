# 9.72d 回填修复设计

## 背景

9.72d（SkillExperienceOptimizer + TeamSkillExperienceOptimizer）已实现并标记 ✅，但验证发现两个问题：

1. **功能性遗漏**：`GenerateUserPatch` / `GenerateTrajectoryPatch` 未使用 `evolutionStore` 字段读取真实数据，Python 中对应方法通过 `_load_skill_content` / `_load_existing_evolutions_summary` 异步读取。
2. **文档同步**：IMPLEMENTATION_PLAN.md 9.72d 行未标注 3 项回填验证结果。

当前 Go 中所有 `NewTeamSkillExperienceOptimizer` 调用方都传 `nil` evolutionStore，原因是 `TeamSkillEvolutionRail`（10.6.3-10）尚未实现。Python 中 `TeamSkillEvolutionRail` 构造 `TeamSkillExperienceOptimizer` 时传入 `evolution_store=self._store`（真实 `EvolutionStore` 实例）。必须在 Rail 实现之前补全此逻辑缺口，否则 10.6.3-10 回填时会产生 bug。

## 修改清单

### 修改 1：EvolutionStore 接口迁移 + LoadFullEvolutionLog 签名修正

**目标**：将 `team_optimizer.go` 中的局部 `EvolutionStore` 接口迁移到 `checkpointing` 包，并将 `LoadFullEvolutionLog` 返回值改为 `(*EvolutionLog, error)` 对齐 Python 异常语义。

#### 1.1 checkpointing 包新增接口定义

在 `internal/evolving/checkpointing/evolution_store.go` 中新增接口：

```go
// EvolutionStoreReader 技能经验优化器所需的演进存储只读接口。
// 从 skill_call/team_optimizer.go 迁移至此，与 Python evolution_store.py 位置对齐。
//
// 对应 Python: EvolutionStore
type EvolutionStoreReader interface {
    // ReadSkillContent 读取技能内容
    ReadSkillContent(ctx context.Context, skillName string) (string, error)
    // LoadFullEvolutionLog 加载完整演进日志
    LoadFullEvolutionLog(ctx context.Context, skillName string) (*EvolutionLog, error)
}
```

命名 `EvolutionStoreReader` 而非 `EvolutionStore`，避免与已有 `EvolutionStore` 结构体冲突，且语义更精确（只读子集）。

#### 1.2 LoadFullEvolutionLog 签名变更

**`StoreRecordsHelper.LoadFullEvolutionLog`**（store_records.go:90）：

```go
// 变更前
func (h *StoreRecordsHelper) LoadFullEvolutionLog(ctx context.Context, name string) *EvolutionLog

// 变更后
func (h *StoreRecordsHelper) LoadFullEvolutionLog(ctx context.Context, name string) (*EvolutionLog, error)
```

所有内部错误场景（ResolveSkillDir 为空、文件不存在、读取失败、JSON 解析失败、FromDict 失败）从返回 `EmptyEvolutionLog(name)` 改为返回 `(nil, err)`。仅在正常空日志场景（首次无演进记录）返回 `(EmptyEvolutionLog(name), nil)`。

具体地：

- `skillDir == ""` → `return nil, fmt.Errorf("skill %s not found", name)`
- `!isFile(evoPath)` → `return EmptyEvolutionLog(name), nil`（首次无记录是正常情况，不是错误）
- `ReadFileText` 失败 → `return nil, err`
- `json.Unmarshal` 失败 → `return nil, fmt.Errorf("parse evolution log: %w", err)`
- `FromDictEvolutionLog` 失败 → `return nil, fmt.Errorf("decode evolution log: %w", err)`

**`EvolutionStore.LoadFullEvolutionLog`**（evolution_store.go:489）同步修改签名：

```go
func (s *EvolutionStore) LoadFullEvolutionLog(ctx context.Context, name string) (*EvolutionLog, error) {
    return s.records.LoadFullEvolutionLog(ctx, name)
}
```

#### 1.3 调用方适配

以下调用方需适配 `(*EvolutionLog, error)` 返回值：

| 文件 | 行号 | 适配方式 |
|------|------|---------|
| `store_records.go` | 148, 181, 203, 238, 267, 335 | 加 `err` 接收，错误时 logger.Warn + 返回空/零值 |
| `evolution_store.go` | 405, 442 | 加 `err` 接收，错误时 logger.Warn + 返回空/零值 |
| `store_projection.go` | 42 | 加 `err` 接收，错误时 logger.Warn + 返回空/零值 |
| `experience/manager.go` | 360 | 加 `err` 接收，错误时向上传播或 logger.Warn |
| `experience/tracker.go` | 164 | 同上 |
| `experience/common.go` | 236 | 同上 |
| `checkpointing_test.go` | 多处 | 加 `_, err :=` 或 `require.NoError` |
| `llm_mock_test.go` | 1622, 1636 | 加 `_, err :=` 或 `require.NoError` |

**关键原则**：内部调用（StoreRecordsHelper / EvolutionStore 内部方法）在 `LoadFullEvolutionLog` 返回 error 时，当前方法也应返回 error 或降级处理（logger.Warn + 空值），不吞掉错误。

#### 1.4 team_optimizer.go 删除局部接口

删除 `team_optimizer.go:27-32` 的局部 `EvolutionStore` 接口定义，改为：

```go
import "github.com/uapclaw/uap-claw-go/internal/evolving/checkpointing"
```

`TeamSkillExperienceOptimizer.evolutionStore` 字段类型从 `EvolutionStore`（局部）改为 `checkpointing.EvolutionStoreReader`。

`NewTeamSkillExperienceOptimizer` 参数签名同步更新。

#### 1.5 确保 EvolutionStore 结构体满足接口

`checkpointing.EvolutionStore` 结构体已有的 `ReadSkillContent(ctx, name) (string, error)` 和修改后的 `LoadFullEvolutionLog(ctx, name) (*EvolutionLog, error)` 满足 `EvolutionStoreReader` 接口。编译期自动满足，无需显式声明。

### 修改 2：补全 loadSkillContent / loadExistingEvolutionsSummary

在 `team_optimizer.go` 非导出函数区新增两个方法：

#### 2.1 loadSkillContent

```go
// loadSkillContent 从 evolutionStore 读取技能内容摘要。
// 对齐 Python: TeamSkillExperienceOptimizer._load_skill_content(skill_name)
func (o *TeamSkillExperienceOptimizer) loadSkillContent(
    ctx context.Context,
    skillName string,
) string {
    if o.evolutionStore == nil {
        return langDefault("无", "N/A", o.language)
    }
    content, err := o.evolutionStore.ReadSkillContent(ctx, skillName)
    if err != nil {
        logger.Warn(logComponent).
            Str("skill_name", skillName).
            Err(err).
            Msg("[TeamSkillOptimizer] loadSkillContent failed, using fallback")
        return langDefault("无", "N/A", o.language)
    }
    if strings.TrimSpace(content) == "" {
        return langDefault("无", "N/A", o.language)
    }
    return summarizeSkillContentTeam(content)
}
```

Python 行为对齐：
- `self._evolution_store is None` → `"N/A"` / `"无"`
- `await self._evolution_store.read_skill_content(skill_name)` → strip 为空 → fallback
- 有内容 → `self._summarize_skill_content(content)`

Go 中 error 场景增加 logger.Warn 降级处理（Python 中 await 抛异常会向上传播，Go 中选择降级而非传播，因为 `loadSkillContent` 用于构建提示词上下文，缺失不应阻断流程）。

#### 2.2 loadExistingEvolutionsSummary

```go
// loadExistingEvolutionsSummary 从 evolutionStore 加载已有演进经验摘要。
// 对齐 Python: TeamSkillExperienceOptimizer._load_existing_evolutions_summary(skill_name)
func (o *TeamSkillExperienceOptimizer) loadExistingEvolutionsSummary(
    ctx context.Context,
    skillName string,
) string {
    if o.evolutionStore == nil {
        return langDefault("无已有演进经验", "No existing evolution records", o.language)
    }
    evoLog, err := o.evolutionStore.LoadFullEvolutionLog(ctx, skillName)
    if err != nil {
        logger.Warn(logComponent).
            Str("skill_name", skillName).
            Err(err).
            Msg("[TeamSkillOptimizer] loadExistingEvolutionsSummary failed, using fallback")
        return langDefault("无已有演进经验", "No existing evolution records", o.language)
    }
    if evoLog == nil {
        return langDefault("无已有演进经验", "No existing evolution records", o.language)
    }
    return summarizeExistingEvolutions(evoLog.Entries, o.language)
}
```

Python 行为对齐：
- `self._evolution_store is None` → fallback
- `await self._evolution_store.load_full_evolution_log(skill_name)` → 取 `.entries` → `_summarize_existing_evolutions`
- Go 中 `LoadFullEvolutionLog` 返回 `(nil, error)` 时走 fallback

### 修改 3：GenerateUserPatch / GenerateTrajectoryPatch 使用新方法

#### 3.1 GenerateUserPatch 变更

**变更前**（team_optimizer.go:370-371）：
```go
skillContent := summarizeSkillContentTeamFallback(skillName, o.language)
existingEvolutions := langDefault("无已有演进经验", "No existing evolution records", o.language)
```

**变更后**：
```go
skillContent := o.loadSkillContent(ctx, skillName)
existingEvolutions := o.loadExistingEvolutionsSummary(ctx, skillName)
```

注意：`GenerateUserPatch` 已有 `ctx context.Context` 参数，无需修改签名。

同样地，retry_prompt 中的对应值也使用新方法（Python 中 retry_prompt 也使用 `_load_skill_content` 的返回值 + `_summarize_skill_content(skill_content, max_chars=2500)` 截断）。

retry_prompt 中 `skillContent` 和 `existingEvolutions` 变量的值已来自新方法（而非固定 fallback），截断逻辑 `summarizeSkillContentTeamWithMax(skillContent, 2500)` 和 `shortenExistingEvolutionsSummary(existingEvolutions, 2)` 保持不变。无需额外修改 retry_prompt 的代码——变量值自动传递。

#### 3.2 GenerateTrajectoryPatch 变更

**变更前**（team_optimizer.go:466）：
```go
existingEvolutions := langDefault("无已有演进经验", "No existing evolution records", o.language)
```

**变更后**：
```go
existingEvolutions := o.loadExistingEvolutionsSummary(ctx, skillName)
```

注意：`GenerateTrajectoryPatch` 已有 `ctx context.Context` 参数。

`currentSkillContent` 参数保持不变——Python 中也不通过 store 读取此值，它来自信号中的 `get_team_signal_skill_content(signal) or ctx.skill_content`。

### 修改 4：测试更新

#### 4.1 mockEvolutionStore 签名适配

`llm_mock_test.go` 中的 `mockEvolutionStore` 需适配 `checkpointing.EvolutionStoreReader` 接口：

```go
type mockEvolutionStore struct {
    skillContent string
    evoLog       *checkpointing.EvolutionLog
    readErr      error
    loadErr      error
}

func (m *mockEvolutionStore) ReadSkillContent(ctx context.Context, name string) (string, error) {
    return m.skillContent, m.readErr
}

func (m *mockEvolutionStore) LoadFullEvolutionLog(ctx context.Context, name string) (*checkpointing.EvolutionLog, error) {
    return m.evoLog, m.loadErr
}
```

#### 4.2 扩展已有测试

**`TestNewTeamSkillExperienceOptimizer_有EvolutionStore`**（llm_mock_test.go:1602）：
- 验证 `loadSkillContent` 在 store 返回真实内容时不走 fallback
- 验证 `loadExistingEvolutionsSummary` 在 store 返回真实 EvolutionLog 时不走 fallback

#### 4.3 新增测试

| 测试名 | 场景 |
|--------|------|
| `TestLoadSkillContent_无Store` | evolutionStore 为 nil，返回 fallback |
| `TestLoadSkillContent_有Store` | store 返回真实内容，返回截断摘要 |
| `TestLoadSkillContent_Store报错` | store 返回 error，降级 fallback + logger.Warn |
| `TestLoadSkillContent_空内容` | store 返回空字符串，返回 fallback |
| `TestLoadExistingEvolutionsSummary_无Store` | evolutionStore 为 nil，返回 fallback |
| `TestLoadExistingEvolutionsSummary_有Store` | store 返回真实 EvolutionLog，返回摘要 |
| `TestLoadExistingEvolutionsSummary_Store报错` | store 返回 error，降级 fallback + logger.Warn |
| `TestLoadExistingEvolutionsSummary_空日志` | store 返回 nil EvolutionLog，返回 fallback |

#### 4.4 现有 LoadFullEvolutionLog 测试适配

`checkpointing_test.go` 中约 10 处调用 `LoadFullEvolutionLog` 的地方需加 `err` 接收和 `require.NoError` 断言。

### 修改 5：更新 IMPLEMENTATION_PLAN.md 9.72d 行

**变更前**（IMPLEMENTATION_PLAN.md:620）：
```
| 9.72d | ✅ | SkillExperienceOptimizer | 技能经验优化器（LLM 生成经验草稿→EvolutionRecord）+ TeamSkillExperienceOptimizer；✅ TextualParameter.Gradients string→any 前置变更 | `openjiuwen/agent_evolving/optimizer/skill_call/` |
```

**变更后**：
```
| 9.72d | ✅ | SkillExperienceOptimizer | 技能经验优化器（LLM 生成经验草稿→EvolutionRecord）+ TeamSkillExperienceOptimizer；✅ TextualParameter.Gradients string→any 前置变更；⤴️ 9.79 orchestrator 回填验证 ✅；⤴️ optimizer/doc.go 回填验证 ✅；⤴️ Gradients any 回填验证 ✅ | `openjiuwen/agent_evolving/optimizer/skill_call/` |
```

### 修改 6：清理不再使用的 fallback 函数

`summarizeSkillContentTeamFallback`（team_optimizer.go:979-983）在 `GenerateUserPatch` 改用 `loadSkillContent` 后不再被调用，应删除。

## 影响范围

| 文件 | 变更类型 |
|------|---------|
| `checkpointing/evolution_store.go` | 新增 `EvolutionStoreReader` 接口 + `LoadFullEvolutionLog` 签名变更 |
| `checkpointing/store_records.go` | `LoadFullEvolutionLog` 签名变更 + 内部调用适配（~6 处） |
| `checkpointing/store_projection.go` | `LoadFullEvolutionLog` 调用适配（1 处） |
| `checkpointing/checkpointing_test.go` | `LoadFullEvolutionLog` 测试适配（~10 处） |
| `experience/manager.go` | `LoadFullEvolutionLog` 调用适配（1 处） |
| `experience/tracker.go` | `LoadFullEvolutionLog` 调用适配（1 处） |
| `experience/common.go` | `LoadFullEvolutionLog` 调用适配（1 处） |
| `optimizer/skill_call/team_optimizer.go` | 删除局部接口 + 字段类型改 `checkpointing.EvolutionStoreReader` + 新增 2 方法 + 2 处函数调用替换 + 删除 fallback 函数 |
| `optimizer/skill_call/llm_mock_test.go` | mock 签名适配 + 新增测试 |
| `optimizer/skill_call/team_optimizer_test.go` | 新增 loadSkillContent / loadExistingEvolutionsSummary 测试 |
| `IMPLEMENTATION_PLAN.md` | 9.72d 行追加回填验证标记 |

## 不涉及

- 不修改 `SkillExperienceOptimizer`（个体优化器不使用 evolutionStore）
- 不实现 `TeamSkillEvolutionRail`（10.6.3-10，属于后续章节）
- 不修改 `EvolutionLog.Entries` 字段类型或结构
