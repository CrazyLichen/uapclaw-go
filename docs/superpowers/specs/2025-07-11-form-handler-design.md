# Form Handler 设计文档

> 对应实现计划：3.10 Form Handler — 表单数据处理
> Python 源码：`openjiuwen/core/foundation/tool/form_handler/`

## 1. 概述

Form Handler 是 RESTful API 工具中处理 `multipart/form-data` 请求的子系统。当 API 参数的 `location` 标记为 `form` 时，参数需要通过 Form Handler 处理后写入 multipart 请求体，而非作为 JSON body 发送。

当前 Go 实现中，form 参数被 fallback 合并到 JSON body（`restful_api.go` 第 367-379 行），需要替换为完整的 multipart/form-data 构建。

## 2. 架构

### 2.1 包结构

独立子包 `form_handler/`，对齐 Python 目录结构：

```
internal/agentcore/foundation/tool/form_handler/
├── doc.go                  # 包文档
├── form_handler.go         # FormHandler 接口 + DefaultFormHandler + FormHandlerManager + 全局单例
└── form_handler_test.go    # 单元测试
```

### 2.2 调用链

```
RestfulApi.Invoke
  → APIParamMapper.Map → 识别 location=form 参数，存储 {form_handler_type, value}
  → RestfulApi.doRequest
    → RestfulApi.processFormData
      → FormHandlerManager.GetHandler(handlerType) → 获取 FormHandler
      → FormHandler.Handle(writer, paramName, value) → 写入 multipart.Writer
      → body_params 以 application/json content-type 追加
    → prepareHeadersForFormData → 移除手动 Content-Type
    → 设置 multipart content-type（含 boundary）
    → 发送 HTTP 请求
```

## 3. 核心类型

### 3.1 FormHandler 接口

```go
// FormHandler 表单数据处理器接口。
//
// 实现此接口可自定义不同类型表单字段的处理方式（如文件上传、二进制数据等）。
// 默认实现为 DefaultFormHandler，将值转为字符串写入 multipart Writer。
//
// 对应 Python: FormHandler (ABC)
type FormHandler interface {
    // Handle 处理表单数据，将 formName=value 写入 multipart Writer。
    Handle(ctx context.Context, writer *multipart.Writer, formName string, value any) error
}
```

**设计决策**：

- 接收 `*multipart.Writer` 而非返回 FormData 对象：Go 中 `multipart.Writer` 直接写入 buffer，无 Python `aiohttp.FormData` 那样的可累积对象
- 增加 `ctx context.Context`：Go 惯例，预留取消和超时传播
- 处理单个字段（formName + value）而非整个 map：与 Python 调用方式对齐，Python 每次 `handler.handle(form, {param_name: value})` 也是单字段调用

### 3.2 DefaultFormHandler

```go
// DefaultFormHandler 默认表单处理器，将值转为字符串写入 multipart Writer。
//
// 对应 Python: DefaultFormHandler
type DefaultFormHandler struct{}

func (DefaultFormHandler) Handle(_ context.Context, writer *multipart.Writer, formName string, value any) error {
    if value == nil {
        return nil
    }
    return writer.WriteField(formName, fmt.Sprintf("%v", value))
}
```

### 3.3 FormHandlerManager

```go
// FormHandlerManager 表单处理器注册表，单例模式。
//
// 对应 Python: FormHandlerManager（Singleton 元类）
type FormHandlerManager struct {
    mu             sync.RWMutex
    handlerMap     map[string]FormHandler
    defaultHandler FormHandler
}
```

**线程安全**：`sync.RWMutex` 保护 handlerMap 和 defaultHandler。`GetHandler` 用读锁，`Register` / `RegisterDefaultHandler` 用写锁。

**与 Python 的差异**：

| Python | Go | 原因 |
|--------|-----|------|
| `register` 接收 `Type[FormHandler]`（类），`get_handler` 时实例化 | `Register` 接收 `FormHandler` 实例 | Go 没有元类/类对象概念 |
| `isinstance(handler_class, type) and issubclass(...)` 校验 | `handler == nil` 校验 | Go 接口编译期已保证类型 |
| 无并发保护 | `sync.RWMutex` | Go 并发场景下注册表需要线程安全 |

**方法**：

- `Register(handlerType string, handler FormHandler)` — 注册处理器，无效参数记录错误日志并忽略
- `RegisterDefaultHandler(handler FormHandler)` — 注册默认处理器
- `GetHandler(handlerType string) FormHandler` — 获取处理器，未注册则返回默认

**全局单例**：

```go
var formHandlerManagerInst = utils.Singleton[FormHandlerManager]{}

func GetFormHandlerManager() *FormHandlerManager {
    return formHandlerManagerInst.Get(func() *FormHandlerManager {
        mgr := &FormHandlerManager{
            handlerMap:     make(map[string]FormHandler),
            defaultHandler: DefaultFormHandler{},
        }
        mgr.handlerMap["default"] = DefaultFormHandler{}
        return mgr
    })
}
```

## 4. service_api 集成

### 4.1 processFormData

```go
func (r *RestfulApi) processFormData(
    ctx context.Context,
    formParams map[string]any,
    bodyParams map[string]any,
) (bodyBytes []byte, contentType string, err error)
```

流程（对齐 Python `RestfulApi._process_form_data`）：

1. 创建 `bytes.Buffer` + `multipart.NewWriter(&buf)`
2. 遍历 formParams，每个参数：
   - 从 `paramInfo.(map[string]any)` 提取 `form_handler_type` 和 `value`
   - `FormHandlerManager.GetHandler(handlerType)` 获取处理器
   - `handler.Handle(ctx, writer, paramName, value)` 写入字段
3. 遍历 bodyParams，非 nil 值：
   - `json.Marshal(paramValue)` 序列化
   - `writer.CreatePart()` 创建 part，设置 `Content-Type: application/json`
   - 写入 JSON 字节
4. `writer.Close()` 写入终止 boundary
5. 返回 `buf.Bytes()`, `writer.FormDataContentType()`, nil

### 4.2 prepareHeadersForFormData

对齐 Python `RestfulApi._prepare_headers_for_form_data()`：

- 移除手动设置的 `Content-Type`，因为 `multipart.Writer` 自动生成含 boundary 的正确值
- 返回处理后的 header map

### 4.3 doRequest 修改

替换当前 fallback 逻辑：

```go
// 旧代码（删除）
if len(formParams) > 0 {
    merged := make(map[string]any)
    for k, v := range bodyParams { merged[k] = v }
    for k, v := range formParams { merged[k] = v }
    bodyBytes, _ := json.Marshal(merged)
    bodyReader = bytes.NewReader(bodyBytes)
    headerMap["Content-Type"] = "application/json"
}

// 新代码
if len(formParams) > 0 {
    formBody, formContentType, formErr := r.processFormData(ctx, formParams, bodyParams)
    if formErr != nil {
        return nil, formErr
    }
    bodyReader = bytes.NewReader(formBody)
    headerMap = prepareHeadersForFormData(headerMap)
    headerMap["Content-Type"] = formContentType
}
```

## 5. 日志对齐

| Python | Go |
|--------|-----|
| `logger.error(f"register handler failed, {handler_type_value} is invalid")` | `logger.Error(ComponentAgentCore).Str("handler_type", handlerType).Msg("注册处理器失败，handler_type 无效")` |
| `logger.error(f"register handler failed, {handler_class} is not a subclass of FormHandler")` | `logger.Error(ComponentAgentCore).Str("handler_type", handlerType).Msg("注册处理器失败，handler 为 nil")` |
| `logger.info(f"register handler success, ...")` | `logger.Info(ComponentAgentCore).Str("handler_type", handlerType).Str("handler", fmt.Sprintf("%T", handler)).Msg("注册处理器成功")` |
| `logger.error(f"register default handler failed, ...")` | `logger.Error(ComponentAgentCore).Msg("注册默认处理器失败，handler 为 nil")` |
| `logger.info(f"register default handler success, ...")` | `logger.Info(ComponentAgentCore).Str("handler", fmt.Sprintf("%T", handler)).Msg("注册默认处理器成功")` |
| Python `tool_logger.debug` (prepare_headers) | `logger.Debug(ComponentAgentCore).Str("content_type", value).Msg("multipart/form-data 请求移除手动设置的 Content-Type...")` |

## 6. 测试策略

### 6.1 form_handler_test.go

| 测试函数 | 覆盖内容 |
|----------|---------|
| `TestDefaultFormHandler_Handle` | 写入普通字符串值 |
| `TestDefaultFormHandler_Handle_Nil值跳过` | value 为 nil 时不写入字段 |
| `TestDefaultFormHandler_Handle_各种类型` | int/float/bool/slice 等 fmt.Sprintf 格式化 |
| `TestFormHandlerManager_Register` | 注册自定义处理器后 GetHandler 返回正确处理器 |
| `TestFormHandlerManager_Register_空类型忽略` | handlerType 为空字符串时记录错误日志，不注册 |
| `TestFormHandlerManager_Register_Nil处理器忽略` | handler 为 nil 时记录错误日志，不注册 |
| `TestFormHandlerManager_RegisterDefaultHandler` | 注册后 GetHandler("unknown") 返回新默认处理器 |
| `TestFormHandlerManager_RegisterDefaultHandler_Nil忽略` | handler 为 nil 时忽略 |
| `TestFormHandlerManager_GetHandler_未注册返回默认` | 获取未注册类型时返回 DefaultFormHandler |
| `TestFormHandlerManager_GetHandler_Default类型` | 获取 "default" 返回 DefaultFormHandler |
| `TestGetFormHandlerManager_单例` | 多次调用返回同一实例 |

### 6.2 restful_api_test.go 新增

| 测试函数 | 覆盖内容 |
|----------|---------|
| `TestProcessFormData_默认处理器` | form_params 使用 DefaultFormHandler 写入 multipart |
| `TestProcessFormData_自定义处理器` | 注册自定义 handler，验证调用 |
| `TestProcessFormData_BodyParams追加` | body_params 以 application/json content-type 追加到 form |
| `TestProcessFormData_Nil值跳过` | body_params 中 nil 值不写入 |
| `TestPrepareHeadersForFormData_移除ContentType` | 手动 Content-Type 被移除 |
| `TestPrepareHeadersForFormData_保留其他头` | 非 Content-Type 头保留 |
| `TestPrepareHeadersForFormData_空头` | 空输入返回空 map |

覆盖率目标：≥ 85%。

## 7. 需要修改的已有文件

| 文件 | 修改内容 |
|------|---------|
| `service_api/restful_api.go` | 替换 doRequest 中 form_params fallback 逻辑；新增 processFormData、prepareHeadersForFormData 方法；新增 import form_handler 包、net/textproto |
| `service_api/doc.go` | 更新文件目录和设计决策描述 |
| `IMPLEMENTATION_PLAN.md` | 3.10 状态 ☐ → ✅ |
