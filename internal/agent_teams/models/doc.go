// Package models 提供团队模型池、路由和分配相关类型。
//
// 核心概念：
//   - ModelPoolEntry：单个 LLM 端点的完整描述（模型名、凭证、Provider、元数据），
//     可物化为 TeamModelConfig 供 Allocator 分配给团队成员
//   - ModelRouterConfig：单端点路由器配置，将一个 api_base_url 展开为多个 ModelPoolEntry
//   - InheritPoolIDs：跨池版本的 model_id 继承，按 bit-exact 签名匹配旧条目
//   - TeamModelConfig：可序列化的团队模型配置，供分配使用
//   - Allocator：模型分配器，将池中端点分配给团队成员
//
// 文件目录：
//
//	models/
//	├── doc.go              # 包文档
//	├── pool.go             # ModelPoolEntry、ModelRouterConfig、InheritPoolIDs
//	├── allocator.go        # Allocator 模型分配器
//	└── team_model_config.go # TeamModelConfig 团队模型配置
//
// 对应 Python 代码：openjiuwen/agent_teams/models/
package models
