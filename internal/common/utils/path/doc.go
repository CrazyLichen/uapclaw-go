// Package path 提供纯路径计算函数，无日志依赖。
//
// 本包从 workspace 包提取路径解析逻辑，作为 workspace、logger、config 的共同依赖，
// 消除 logger↔workspace 循环依赖和代码重复。
//
// 所有函数为纯计算（不含 logger 调用），workspace 在调用后自行补日志。
// 缓存使用 sync.Once 保证幂等，与 Python _resolve_paths() 的 _initialized 全局变量行为对齐。
//
// 文件目录：
//
//	path/
//	├── doc.go         # 包文档
//	├── paths.go       # 路径解析与回退逻辑、18个路径辅助函数
//	└── paths_test.go  # 路径计算测试
//
// 对应 Python 代码：jiuwenswarm/common/utils.py（路径管理、_resolve_paths、prepare_workspace）
package path
