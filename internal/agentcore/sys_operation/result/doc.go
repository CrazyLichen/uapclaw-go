// Package result 提供系统操作的结果类型定义。
//
// 所有子操作（fs/shell/code）的返回值都使用本包的 Result + Data 类型，
// 遵循 BaseResult{Code, Message} + 具体 Data 的统一模式。
//
// 文件目录：
//
//	result/
//	├── doc.go                        # 包文档
//	├── base_result.go                # BaseResult + BuildOperationErrorResult
//	├── shell_operation_result.go     # Shell 操作结果类型
//	├── fs_operation_result.go        # 文件系统操作结果类型
//	└── code_operation_result.go      # 代码执行结果类型
//
// 对应 Python 代码：openjiuwen/core/sys_operation/result/
package result
