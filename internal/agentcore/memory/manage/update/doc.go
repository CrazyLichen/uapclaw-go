// Package update 提供记忆冲突检查器。
//
// 本包实现 MemUpdateChecker，用于检测新记忆与旧记忆之间的冗余和冲突。
// 使用 LLM 驱动的提示词模板（memory_update_check.md）进行冲突分析，
// 返回 MemoryActionItem 列表指示每条记忆应执行 ADD 或 DELETE 操作。
//
// 文件目录：
//
//	update/
//	├── doc.go              # 包文档
//	└── update_checker.go   # MemUpdateChecker 记忆冲突检查器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/update/
package update
