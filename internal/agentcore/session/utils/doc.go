// Package utils 提供会话系统的通用工具函数，包括嵌套路径操作、引用路径解析、
// 字典操作和容器深拷贝等。
//
// 本包从 state/utils.go 中迁出，作为独立子包供 state、store、tracer、graph 等
// 多个包共享使用，避免循环依赖。
//
// 文件目录：
//
//	utils/
//	├── doc.go           # 包文档
//	├── path.go          # 嵌套路径操作（SplitNestedPath, GetValueByNestedPath, RootToPath, RootToIndex）
//	├── ref.go           # 引用路径操作（IsRefPath, ExtractOriginKey）
//	├── dict.go          # 字典操作（UpdateDict, UpdateByKey, DeleteByKey, ExpandNestedStructure）
//	├── container.go     # 容器操作（SafeExtendContainer, DeepCopyMap/Slice/Value/Updates, ConvertUpdatesFromJSON）
//	├── string.go        # 字符串辅助（ContainsChar, ContainsSubstring, SplitString, ParseListIndexes）
//	└── constants.go     # 常量（RegexMaxLength, NestedPathSplit, NestedPathListSplit）
//
// 对应 Python 代码：openjiuwen/core/session/utils.py
//
// 未移植说明：
//   - create_wrapper_class：Python 中零调用，Go 使用手动委托模式替代（见 NodeSessionFacade/RouterSessionFacade）
//   - EndFrame/Frame：Go 使用 close(ch) 替代单源场景，多源场景 ⤵️ 延后到 8.x stream_actor 回填
package utils
