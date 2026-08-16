// Package update 提供记忆冲突检查器。
//
// 本包实现 MemUpdateChecker，用于检测新记忆与旧记忆之间的冗余和冲突。
// 当前为 stub 实现，直接返回所有新记忆为 ADD；7.8 实现时替换为 LLM 驱动的冲突检查。
//
// 文件目录：
//
//	update/
//	├── doc.go             # 包文档
//	└── update_checker.go  # MemUpdateChecker 记忆冲突检查器（⤵️ 回填: 7.8 stub）
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/update/
package update
