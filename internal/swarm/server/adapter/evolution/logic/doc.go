// Package logic 提供技能演进（Skill Evolution）的纯逻辑辅助函数。
//
// 本包是 evolution/ 的逻辑子层，不含任何外部依赖（不依赖 gateway_push 等），
// 所有函数均为无状态纯函数或简单数据结构。
// adapter 包可安全导入本包，不会产生循环依赖。
//
// 核心功能：
//   - 事件分类：将 SDK 内部演进事件分为 approval/outcome/progress/stream 四类
//   - 状态提取：从原始事件中提取 request_id、stage、terminal 等字段
//   - Noop 检测：根据消息内容识别"无演进信号"场景，映射到细粒度 noop 阶段
//   - 审批分组：按 request_id 聚合同一审批的多个事件
//   - 状态构建：构建 EvolutionStatusUpdate、EvolutionProgressStatus 等结构体
//
// 依赖方向：
//
//	evolution/logic/  ← 纯逻辑，零外部依赖
//	  ↑
//	evolution/        ← 推送层，依赖 gateway_push + evolution/logic
//	  ↑ (不可逆)
//	adapter/          ← import evolution/logic（不 import evolution/）
//
// 文件目录：
//
//	evolution/logic/
//	├── doc.go              # 包文档
//	└── helpers.go          # 3 结构体 + 常量/变量 + 18 导出函数 + 5 非导出函数
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/evolution_helpers.py
package logic
