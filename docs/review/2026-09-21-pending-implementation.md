# 审查问题待实现依赖缺失项

> 来源：docs/review/2026-09-21-48h-logic-review.md
> 记录时间：2026-09-22

以下审查问题因依赖的功能模块尚未实现，暂时无法修复。待对应模块实现后回填。

---

## M-21: ProcessInterrupt StreamEventRail 操作

- **问题**：ProcessInterrupt 中所有 pause/resume/supplement/cancel 的 StreamEventRail 调用标记为 `⤵️ 10.6.3-10`
- **依赖**：`StreamEventRail` 类型不存在，`buildStreamEventRail()` 返回 nil
- **待实现**：JiuClawStreamEventRail 类型的完整实现（对齐 Python 的 StreamEventRail）
- **位置**：`deep_adapter.go` L1203-1233, `deep_adapter_rails.go` L402-404

---

## M-22: ProcessMessageStreamImpl team 模式分流

- **问题**：`ProcessMessageStreamImpl` 缺少 team 模式分流，标记为 `⤵️ 9.55-9.65 TeamHelpers`
- **依赖**：`team_helpers` 和 `process_team_message_stream` 未实现
- **待实现**：TeamHelpers（9.55-9.65）完整实现
- **位置**：`deep_adapter.go` L891-892

---

## M-23: ProcessMessageStreamImpl auto_harness 分流

- **问题**：`ProcessMessageStreamImpl` 缺少 auto_harness 分流，标记为 `⤵️ 10.6.11-12`
- **依赖**：`AutoHarnessService` 类型不存在
- **待实现**：AutoHarness 服务（10.6.11-12）
- **位置**：`deep_adapter.go` L895

---

## M-24: CreateInstance load_user_rails

- **问题**：`CreateInstance` 中 `load_user_rails` 缺失，标记为 `⤵️ 10.6.3-10`
- **依赖**：`loadUserRails` 函数不存在
- **待实现**：读取用户 Rail 配置并实例化+注册的加载逻辑
- **位置**：`deep_adapter.go` L541

---

## M-26: updatePlanModeRails SkillCreateRail 处理

- **问题**：`updatePlanModeRails` 中 SkillCreateRail 处理标记为 `⤵️ 待回填`
- **依赖**：`SkillCreateRail` 类型不存在，`buildSkillCreateRail()` 返回 nil
- **待实现**：SkillCreateRail 类型的完整实现
- **位置**：`deep_adapter_rails.go` L702

---

## M-27 (ExternalMemoryRail 部分): updatePlanModeRails/updateAgentModeRails ExternalMemoryRail

- **问题**：ExternalMemoryRail 处理标记为 `⤵️ 待回填`
- **依赖**：`ExternalMemoryRail` 类型不存在，`buildExternalMemoryRail()` 返回 nil
- **待实现**：ExternalMemoryRail 类型的完整实现
- **位置**：`deep_adapter_rails.go` L625, L767
- **备注**：MemoryRail 部分已于本次修复中回填（handleMemoryRailByConfig 已实现）

---

## M-27 (handleExternalMemoryRailByConfig): ExternalMemoryRail 配置处理

- **问题**：`handleExternalMemoryRailByConfig` 函数不存在
- **依赖**：同上
- **待实现**：对齐 Python `_handle_external_memory_rail_by_config`
