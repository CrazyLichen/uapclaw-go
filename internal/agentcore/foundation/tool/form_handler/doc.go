// Package form_handler 提供表单数据处理器的策略模式实现，用于构建 multipart/form-data 请求体。
//
// 当 RESTful API 工具的参数 location 标记为 form 时，参数需要通过 FormHandler 处理后
// 写入 multipart 请求体，而非作为 JSON body 发送。
//
// 核心组件：
//   - FormHandler：表单数据处理器接口，自定义处理器需实现此接口
//   - DefaultFormHandler：默认处理器，将值转为字符串写入 multipart Writer
//   - FormHandlerManager：处理器注册表（单例），管理 handlerType → FormHandler 映射
//
// 设计决策：
//
//	FormHandler.Handle 接收 *multipart.Writer 而非返回累积对象，
//	因为 Go 中 multipart.Writer 直接写入底层 buffer，无 Python aiohttp.FormData
//	那样的可累积对象。每次 Handle 调用处理单个字段（formName + value），
//	与 Python 调用方式对齐。
//
// 文件目录：
//
//	form_handler/
//	├── doc.go              # 包文档
//	├── form_handler.go     # FormHandler 接口 + DefaultFormHandler + FormHandlerManager + 全局单例
//	└── form_handler_test.go # 单元测试
//
// 对应 Python 代码：openjiuwen/core/foundation/tool/form_handler/
package form_handler
