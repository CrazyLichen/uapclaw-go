// Package file_connector 提供上下文记忆演化系统的文件持久化连接器。
//
// JSONFileConnector 处理 JSON 文件读写，自动创建父目录。
// Go 无需 safe_model_dump（Python 那个是 Pydantic 版本兼容函数），
// Go 结构体直接 json.Marshal。
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/core/file_connector/
package file_connector
