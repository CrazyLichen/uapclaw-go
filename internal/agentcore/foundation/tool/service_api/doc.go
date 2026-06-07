// Package service_api 提供 RESTful API 工具实现，将输入参数映射到 HTTP 请求的
// path/query/header/body/form 位置，发送 HTTP 请求并解析响应。
//
// 核心组件：
//   - RestfulApiCard：RESTful API 工具配置卡片，扩展 ToolCard，使用原始 JSON Schema map
//     （InputSchema）替代 []*Param，以支持 location 扩展属性
//   - RestfulApi：HTTP REST 工具，实现 Tool 接口，参数映射 → HTTP 请求 → 响应解析
//   - APIParamMapper：参数位置映射器，根据 schema 中的 location 字段分配参数到各 HTTP 位置
//   - ParserRegistry：响应解析器注册表，支持 JSON/Text 解析和 Gzip/Deflate 解压
//
// 设计决策：
//
//	RestfulApiCard 使用 InputSchema（map[string]any）而非 ToolCard.InputParams（[]*Param），
//	因为 Python 中 input_params 是原始 JSON Schema map，properties 中每个参数可带
//	location 扩展属性（path/query/header/body/form），这在 Go 的 []*Param 结构化列表中无法表达。
//	RestfulApiCard 覆写 ToolInfo() 方法，直接将 InputSchema 作为 parameters 传给 LLM。
//
//	Form 参数处理使用 form_handler 子包的策略模式：
//	location=form 的参数通过 FormHandlerManager 获取对应处理器，
//	由处理器将字段写入 multipart.Writer 构建 multipart/form-data 请求体。
//	body_params 以 application/json content-type 追加到 multipart form 中。
//
// 文件目录：
//
//	service_api/
//	├── doc.go                # 包文档
//	├── restful_api.go        # RestfulApiCard + RestfulApi + processFormData + prepareHeadersForFormData
//	├── api_param_mapper.go   # APIParamLocation 枚举 + APIParamMapper
//	└── response_parser.go    # BaseResponseParser + JSON/Text 解析器 + 解压器 + ParserRegistry
//
// 对应 Python 代码：openjiuwen/core/foundation/tool/service_api/
package service_api
