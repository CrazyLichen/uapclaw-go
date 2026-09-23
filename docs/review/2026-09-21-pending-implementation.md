# 2026-09-21 审查待实现依赖项

本文件记录 2026-09-21 审查中因依赖缺失而无法立即实现的问题。

## 待实现依赖缺失项

| 编号 | 问题 | 依赖 |
|------|------|------|
| M-21 | StreamEventRail 类型不存在 | IMPLEMENTATION_PLAN 待实现章节 |
| M-22 | TeamHelpers 未实现（9.55-9.65） | IMPLEMENTATION_PLAN 9.55-9.65 |
| M-23 | AutoHarnessService 未实现（10.6.11-12） | IMPLEMENTATION_PLAN 10.6.11-12 |
| M-24 | loadUserRails 函数不存在 | IMPLEMENTATION_PLAN 10.6.3-10 |
| M-26 | SkillCreateRail 类型不存在 | IMPLEMENTATION_PLAN 待实现章节 |
| M-27 ExternalMemoryRail 部分 | ExternalMemoryRail 类型不存在 | IMPLEMENTATION_PLAN 待实现章节 |
| AH-S-02 | CreateInstance 缺少 load_user_rails() | RailManager 基础设施不存在，等实现后回填 |
| AH-S-06 | ReloadAgentConfig 缺少 load_user_rails() | 同 AH-S-02，依赖 RailManager |
| AH-S-03 | ProcessMessageImpl/Stream 缺少 cron 上下文绑定 | 依赖 11.10 Cron 基础设施，整体未实现 |
| AH-S-04 | StreamEventRail 完全未实现 | 需全新构建 Rail，体量大，不在修复范畴 |
| AH-S-05 | ProcessMessageStreamImpl 缺少 streamEventRail.reset_abort | 同 AH-S-04，依赖 StreamEventRail |
| AH-S-09 | ReloadAgentConfig 缺少 load_user_rails() | 同 AH-S-02，依赖 RailManager |
| AH-M-02 | CodeAdapter 缺少 _update_tools_for_mode 和 _update_session_tools | 依赖 ACP、user_todos 等尚未实现的子系统 |
| AH-M-10 | buildLspRail 返回 nil | 依赖 LSP（Language Server Protocol）功能整体未实现 |
| AH-M-12 | CodeAdapter 缺少 browser_agent 配置 | 依赖 browser 功能，⤵️ 标记 |
| AH-M-13 | CodeAdapter 缺少 _refresh_acp_runtime_tools | 依赖 ACP 子系统 |
| AH-M-14 | CodeAdapter.updateRuntimeConfig 缺少 user_todos channel_id per-request sync | 依赖 user_todos 子系统 |

## 跳过项（确认无需修复）

| 编号 | 问题 | 原因 |
|------|------|------|
| S-14 | ContextEvolutionRail 完全缺失 | 属 IMPLEMENTATION_PLAN 9.24 P6 待实现章节 |
| S-16 | ProjectMemory .jiuwen 兼容标记 | 兼容性需求优先级低 |
| M-01 | handleStream 调用链完整性 | 误判：Go 架构与 Python 完全一致 |
| M-14 | 兼容 wrapper | 暂不需要，后续有调用方再补 |
| M-15 | 兼容别名 | 暂不需要 |
| M-36 | symlink target_is_directory | os.Symlink 在实际场景中自动正确处理 |
| M-39 | CreationDurationMs None/0 语义区分 | 保持 float64，实际不会出现真正的 0ms |
| T-20 | Enter/Exit 缺 fireRail 调用点 | Python 端 _fire_rail 也只有定义没有调用点，审查误判 |
| AH-S-11 | HandleUserAnswer 缺少 skill_create_ 审批路由 | 审查误判：Python handle_user_answer 也只有 3 个前缀，无 skill_create_ |
