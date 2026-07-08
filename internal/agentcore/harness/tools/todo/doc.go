// Package todo 提供待办事项（Todo）工具集，包含创建（TodoCreateTool）、
// 列表（TodoListTool）、查询详情（TodoGetTool）和修改（TodoModifyTool）四个工具。
//
// 待办事项按会话隔离，持久化到 {workspace}/{sessionID}/todo.json 文件，
// 通过 TodoLockManager 为每个会话分配独立互斥锁，保证并发安全。
//
// 对齐 Python: openjiuwen/harness/tools/todo/todo.py
//
// 文件目录：
//
//	todo/
//	├── doc.go      # 包文档
//	└── todo.go     # TodoLockManager + TodoTool 基类 + 四个工具工厂函数
//
// 对应 Python 代码：openjiuwen/harness/tools/todo/
package todo
