// Package context_engineer 提供 DeepAgent 上下文引擎护栏实现。
//
// 包含两个核心 Rail：
//   - ContextProcessorRail：配置和注入上下文引擎处理器，修复不完整的 tool_call/ToolMessage 配对，
//     刷新任务状态，注入 offload 提示节
//   - ContextAssembleRail：注入工作空间目录结构和工具列表到系统提示词构建器
//
// 辅助函数：
//   - MergeConfigWithOverrides：反射实现字段级配置合并（snake_case → PascalCase）
//   - MergeProcessors：合并 preset 和 user processor 列表
//   - RefreshTaskStateRuntime：同步运行时状态到会话顶层状态
//   - FixIncompleteToolContext：验证并修复不完整的 tool_call/ToolMessage 配对
//   - EnsureJSONArguments：确保 tool call arguments 是合法 JSON
//
// 文件目录：
//
//	context_engineer/
//	├── doc.go                       # 包文档
//	├── merge_config.go              # 反射配置合并工具
//	├── merge_config_test.go         # 配置合并测试
//	├── refresh_task_state.go        # 任务状态刷新
//	├── refresh_task_state_test.go   # 任务状态刷新测试
//	├── fix_incomplete_tool_context.go     # 不完整工具上下文修复
//	├── fix_incomplete_tool_context_test.go # 工具上下文修复测试
//	├── context_processor_rail.go    # ContextProcessorRail 实现
//	├── context_assemble_rail.go     # ContextAssembleRail 实现
//
// 对应 Python 代码：openjiuwen/harness/rails/context_engineer/
package context_engineer
