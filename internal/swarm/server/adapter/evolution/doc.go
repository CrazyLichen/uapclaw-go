// Package evolution 提供技能演进（Skill Evolution）事件分类、状态提取和推送的共享辅助工具。
//
// 本包是纯工具模块，不含外部依赖，所有函数均为无状态纯函数或简单数据结构。
// 消费者通过 EvolutionPushContext 注入推送传输和回调函数来使用推送能力。
//
// 核心功能：
//   - 事件分类：将 SDK 内部演进事件分为 approval/outcome/progress/stream 四类
//   - 状态提取：从原始事件中提取 request_id、stage、terminal 等字段
//   - Noop 检测：根据消息内容识别"无演进信号"场景，映射到细粒度 noop 阶段
//   - 推送桥接：通过 EvolutionPushContext 将演进状态推送到 Gateway 侧
//   - 审批分组：按 request_id 聚合同一审批的多个事件
//
// 文件目录：
//
//	evolution/
//	├── doc.go            # 包文档
//	├── helpers.go        # 3 结构体 + 常量/变量 + ~22 导出函数 + 1 非导出函数
//	└── helpers_test.go   # 单元测试
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/evolution_helpers.py
package evolution
