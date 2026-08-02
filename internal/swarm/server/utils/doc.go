// Package utils 提供 AgentServer 工具函数。
//
// 包含流式 chunk 解析、请求参数提取、turn-based diff 查询等功能。
// 对齐 Python: jiuwenswarm/server/utils/
//
// 文件目录：
//
//	utils/
//	├── doc.go              # 包文档
//	├── stream_utils.go     # ParseStreamChunk + UsageAccumulator + InteractionConverterFunc + 10 helper 纯函数
//	├── utils.go            # GetChatID + IsTeamParams
//	├── diff_service.go     # DiffService + GetTurnDiffs/GetFilesToRestore + 12 helper
//
// 对应 Python 代码：jiuwenswarm/server/utils/
package utils
