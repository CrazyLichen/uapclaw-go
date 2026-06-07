# Form Handler 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完整移植 Python FormHandler 策略模式，替换 RestfulApi 中 form 参数的 fallback 逻辑，实现 multipart/form-data 请求构建。

**Architecture:** 独立 form_handler 子包，提供 FormHandler 接口 + DefaultFormHandler + FormHandlerManager 单例注册表。service_api 包通过 import form_handler 包集成 processFormData 和 prepareHeadersForFormData 方法。

**Tech Stack:** Go 标准库 `mime/multipart`、`net/textproto`、`sync`；项目内部 `logger`、`exception` 包。

---

### Task 1: 创建 form_handler 包 — doc.go

**Files:**
- Create: `internal/agentcore/foundation/tool/form_handler/doc.go`

- [ ] **Step 1: 创建 doc.go 文件**

```go
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
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/foundation/tool/form_handler/`
Expected: 无错误

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/foundation/tool/form_handler/doc.go
git commit -m "feat(form_handler): 添加 form_handler 包文档"
```

---

### Task 2: 创建 form_handler.go — FormHandler 接口 + DefaultFormHandler + FormHandlerManager

**Files:**
- Create: `internal/agentcore/foundation/tool/form_handler/form_handler.go`

- [ ] **Step 1: 创建 form_handler.go 文件**

```go
package form_handler

import (
	"context"
	"fmt"
	"mime/multipart"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 接口 ────────────────────────────

// FormHandler 表单数据处理器接口。
//
// 实现此接口可自定义不同类型表单字段的处理方式（如文件上传、二进制数据等）。
// 默认实现为 DefaultFormHandler，将值转为字符串写入 multipart Writer。
//
// 对应 Python: openjiuwen/core/foundation/tool/form_handler/form_handler_manager.py (FormHandler)
type FormHandler interface {
	// Handle 处理表单数据，将 formName=value 写入 multipart Writer。
	//
	// 参数：
	//   - ctx: 上下文，预留取消和超时传播
	//   - writer: multipart Writer，由调用方创建和管理生命周期
	//   - formName: 表单字段名
	//   - value: 表单字段值
	Handle(ctx context.Context, writer *multipart.Writer, formName string, value any) error
}

// ──────────────────────────── 结构体 ────────────────────────────

// DefaultFormHandler 默认表单处理器，将值转为字符串写入 multipart Writer。
//
// 遍历传入的值，跳过 nil，将其余值通过 fmt.Sprintf 转为字符串后
// 调用 writer.WriteField 写入。
//
// 对应 Python: DefaultFormHandler
type DefaultFormHandler struct{}

// FormHandlerManager 表单处理器注册表，单例模式。
//
// 维护 handlerType → FormHandler 的映射，支持注册自定义处理器和默认处理器。
// 获取处理器时，若指定类型未注册则返回默认处理器。
//
// 对应 Python: FormHandlerManager（Singleton 元类）
type FormHandlerManager struct {
	mu             sync.RWMutex
	handlerMap     map[string]FormHandler
	defaultHandler FormHandler
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	handlerManagerOnce sync.Once
	handlerManagerInst *FormHandlerManager
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetFormHandlerManager 获取全局 FormHandlerManager 单例。
//
// 首次调用时初始化注册表，注册 DefaultFormHandler 作为默认处理器，
// 同时将 "default" 类型也映射到 DefaultFormHandler。
func GetFormHandlerManager() *FormHandlerManager {
	handlerManagerOnce.Do(func() {
		handlerManagerInst = &FormHandlerManager{
			handlerMap:     make(map[string]FormHandler),
			defaultHandler: DefaultFormHandler{},
		}
		handlerManagerInst.handlerMap["default"] = DefaultFormHandler{}
	})
	return handlerManagerInst
}

// Register 注册表单处理器。
//
// handlerType 为处理器类型标识（对应 schema 中的 form_handler_type 字段），
// handler 为处理器实例。若 handlerType 无效或 handler 为 nil，记录错误日志并忽略。
//
// 对应 Python: FormHandlerManager.register()
func (m *FormHandlerManager) Register(handlerType string, handler FormHandler) {
	if handlerType == "" {
		logger.Error(logger.ComponentAgentCore).
			Str("handler_type", handlerType).
			Msg("注册处理器失败，handler_type 无效")
		return
	}
	if handler == nil {
		logger.Error(logger.ComponentAgentCore).
			Str("handler_type", handlerType).
			Msg("注册处理器失败，handler 为 nil")
		return
	}
	m.mu.Lock()
	m.handlerMap[handlerType] = handler
	m.mu.Unlock()
	logger.Info(logger.ComponentAgentCore).
		Str("handler_type", handlerType).
		Str("handler", fmt.Sprintf("%T", handler)).
		Msg("注册处理器成功")
}

// RegisterDefaultHandler 注册默认表单处理器。
//
// 若 handler 为 nil，记录错误日志并忽略。
//
// 对应 Python: FormHandlerManager.register_default_handler()
func (m *FormHandlerManager) RegisterDefaultHandler(handler FormHandler) {
	if handler == nil {
		logger.Error(logger.ComponentAgentCore).
			Msg("注册默认处理器失败，handler 为 nil")
		return
	}
	m.mu.Lock()
	m.defaultHandler = handler
	m.mu.Unlock()
	logger.Info(logger.ComponentAgentCore).
		Str("handler", fmt.Sprintf("%T", handler)).
		Msg("注册默认处理器成功")
}

// GetHandler 获取表单处理器。
//
// 若 handlerType 未注册，返回默认处理器。
//
// 对应 Python: FormHandlerManager.get_handler()
func (m *FormHandlerManager) GetHandler(handlerType string) FormHandler {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if h, ok := m.handlerMap[handlerType]; ok {
		return h
	}
	return m.defaultHandler
}

// Handle 默认表单处理器实现，将值转为字符串写入 multipart Writer。
//
// 跳过 nil 值，将其余值通过 fmt.Sprintf 转为字符串后写入。
//
// 对应 Python: DefaultFormHandler.handle()
func (DefaultFormHandler) Handle(_ context.Context, writer *multipart.Writer, formName string, value any) error {
	if value == nil {
		return nil
	}
	return writer.WriteField(formName, fmt.Sprintf("%v", value))
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/foundation/tool/form_handler/`
Expected: 无错误

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/foundation/tool/form_handler/form_handler.go
git commit -m "feat(form_handler): 实现 FormHandler 接口 + DefaultFormHandler + FormHandlerManager"
```

---

### Task 3: 创建 form_handler_test.go — 单元测试

**Files:**
- Create: `internal/agentcore/foundation/tool/form_handler/form_handler_test.go`

- [ ] **Step 1: 创建 form_handler_test.go 文件**

```go
package form_handler

import (
	"bytes"
	"context"
	"mime/multipart"
	"strings"
	"testing"
)

// ──────────────────────────── DefaultFormHandler 测试 ────────────────────────────

// TestDefaultFormHandler_Handle 测试写入普通字符串值
func TestDefaultFormHandler_Handle(t *testing.T) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	handler := DefaultFormHandler{}

	err := handler.Handle(context.Background(), writer, "name", "Alice")
	if err != nil {
		t.Fatalf("Handle 失败: %v", err)
	}
	writer.Close()

	if !strings.Contains(buf.String(), "name") {
		t.Error("输出应包含字段名 name")
	}
	if !strings.Contains(buf.String(), "Alice") {
		t.Error("输出应包含字段值 Alice")
	}
}

// TestDefaultFormHandler_Handle_Nil值跳过 测试 nil 值不写入字段
func TestDefaultFormHandler_Handle_Nil值跳过(t *testing.T) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	handler := DefaultFormHandler{}

	err := handler.Handle(context.Background(), writer, "nil_field", nil)
	if err != nil {
		t.Fatalf("Handle nil 不应返回错误: %v", err)
	}
	writer.Close()

	// multipart.Writer 写了 boundary，但不应该有 nil_field 内容
	if strings.Contains(buf.String(), "nil_field") {
		t.Error("nil 值不应写入字段")
	}
}

// TestDefaultFormHandler_Handle_各种类型 测试 int/float/bool/slice 等类型
func TestDefaultFormHandler_Handle_各种类型(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		contains string
	}{
		{"整数", 42, "42"},
		{"浮点数", 3.14, "3.14"},
		{"布尔值", true, "true"},
		{"切片", []int{1, 2, 3}, "[1 2 3]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			writer := multipart.NewWriter(&buf)
			handler := DefaultFormHandler{}

			err := handler.Handle(context.Background(), writer, "field", tt.value)
			if err != nil {
				t.Fatalf("Handle 失败: %v", err)
			}
			writer.Close()

			if !strings.Contains(buf.String(), tt.contains) {
				t.Errorf("输出应包含 %q，实际: %s", tt.contains, buf.String())
			}
		})
	}
}

// ──────────────────────────── FormHandlerManager 测试 ────────────────────────────

// testHandler 测试用自定义处理器
type testHandler struct {
	called bool
}

func (h *testHandler) Handle(_ context.Context, writer *multipart.Writer, formName string, value any) error {
	h.called = true
	return writer.WriteField(formName, "custom:"+fmt.Sprintf("%v", value))
}

// TestFormHandlerManager_Register 测试注册自定义处理器
func TestFormHandlerManager_Register(t *testing.T) {
	mgr := &FormHandlerManager{
		handlerMap:     make(map[string]FormHandler),
		defaultHandler: DefaultFormHandler{},
	}
	handler := &testHandler{}
	mgr.Register("custom", handler)

	got := mgr.GetHandler("custom")
	if got != handler {
		t.Error("GetHandler 应返回注册的自定义处理器")
	}
}

// TestFormHandlerManager_Register_空类型忽略 测试空 handlerType 忽略
func TestFormHandlerManager_Register_空类型忽略(t *testing.T) {
	mgr := &FormHandlerManager{
		handlerMap:     make(map[string]FormHandler),
		defaultHandler: DefaultFormHandler{},
	}
	mgr.Register("", &testHandler{})

	// 空 handlerType 不应注册
	if len(mgr.handlerMap) != 0 {
		t.Error("空 handlerType 不应注册到 handlerMap")
	}
}

// TestFormHandlerManager_Register_Nil处理器忽略 测试 nil handler 忽略
func TestFormHandlerManager_Register_Nil处理器忽略(t *testing.T) {
	mgr := &FormHandlerManager{
		handlerMap:     make(map[string]FormHandler),
		defaultHandler: DefaultFormHandler{},
	}
	mgr.Register("nil_type", nil)

	got := mgr.GetHandler("nil_type")
	// 应返回默认处理器而非 nil
	if got == nil {
		t.Error("GetHandler 不应返回 nil")
	}
	_, ok := got.(DefaultFormHandler)
	if !ok {
		t.Error("未注册类型应返回 DefaultFormHandler")
	}
}

// TestFormHandlerManager_RegisterDefaultHandler 测试注册默认处理器
func TestFormHandlerManager_RegisterDefaultHandler(t *testing.T) {
	mgr := &FormHandlerManager{
		handlerMap:     make(map[string]FormHandler),
		defaultHandler: DefaultFormHandler{},
	}
	custom := &testHandler{}
	mgr.RegisterDefaultHandler(custom)

	got := mgr.GetHandler("unknown_type")
	if got != custom {
		t.Error("注册默认处理器后，GetHandler 未注册类型应返回新默认处理器")
	}
}

// TestFormHandlerManager_RegisterDefaultHandler_Nil忽略 测试 nil 默认处理器忽略
func TestFormHandlerManager_RegisterDefaultHandler_Nil忽略(t *testing.T) {
	mgr := &FormHandlerManager{
		handlerMap:     make(map[string]FormHandler),
		defaultHandler: DefaultFormHandler{},
	}
	mgr.RegisterDefaultHandler(nil)

	// 默认处理器不应被替换为 nil
	got := mgr.GetHandler("unknown_type")
	if got == nil {
		t.Error("默认处理器不应为 nil")
	}
	_, ok := got.(DefaultFormHandler)
	if !ok {
		t.Error("nil 注册不应替换默认处理器")
	}
}

// TestFormHandlerManager_GetHandler_未注册返回默认 测试未注册类型返回默认处理器
func TestFormHandlerManager_GetHandler_未注册返回默认(t *testing.T) {
	mgr := &FormHandlerManager{
		handlerMap:     make(map[string]FormHandler),
		defaultHandler: DefaultFormHandler{},
	}
	got := mgr.GetHandler("nonexistent")
	_, ok := got.(DefaultFormHandler)
	if !ok {
		t.Error("未注册类型应返回 DefaultFormHandler")
	}
}

// TestFormHandlerManager_GetHandler_Default类型 测试 "default" 类型返回 DefaultFormHandler
func TestFormHandlerManager_GetHandler_Default类型(t *testing.T) {
	mgr := &FormHandlerManager{
		handlerMap:     make(map[string]FormHandler),
		defaultHandler: DefaultFormHandler{},
	}
	mgr.handlerMap["default"] = DefaultFormHandler{}

	got := mgr.GetHandler("default")
	_, ok := got.(DefaultFormHandler)
	if !ok {
		t.Error("\"default\" 类型应返回 DefaultFormHandler")
	}
}

// TestGetFormHandlerManager_单例 测试多次调用返回同一实例
func TestGetFormHandlerManager_单例(t *testing.T) {
	// 注意：由于 sync.Once 的特性，此测试验证全局单侧行为
	// 在同一进程内多次调用应返回同一实例
	mgr1 := GetFormHandlerManager()
	mgr2 := GetFormHandlerManager()
	if mgr1 != mgr2 {
		t.Error("GetFormHandlerManager 应返回同一实例")
	}
}
```

- [ ] **Step 2: 修复编译 — testHandler 中缺少的 fmt 导入**

testHandler 的 Handle 方法使用了 `fmt.Sprintf`，需要在 import 中添加 `"fmt"`：

```go
import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"strings"
	"testing"
)
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/form_handler/ -v`
Expected: 所有测试通过

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/foundation/tool/form_handler/form_handler_test.go
git commit -m "test(form_handler): 添加 FormHandler/DefaultFormHandler/FormHandlerManager 单元测试"
```

---

### Task 4: 修改 restful_api.go — 添加 processFormData 和 prepareHeadersForFormData 方法

**Files:**
- Modify: `internal/agentcore/foundation/tool/service_api/restful_api.go`

- [ ] **Step 1: 添加 import**

在 `restful_api.go` 的 import 块中添加以下包：

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/form_handler"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)
```

- [ ] **Step 2: 替换 doRequest 中的 form_params fallback 逻辑**

将以下旧代码（约第 367-379 行）：

```go
	if len(formParams) > 0 {
		// ⤵️ 预留：form_params 暂 fallback 到 body（3.10 回填 processFormData）
		// 将 form 参数合并到 body 中
		merged := make(map[string]any)
		for k, v := range bodyParams {
			merged[k] = v
		}
		for k, v := range formParams {
			merged[k] = v
		}
		bodyBytes, _ := json.Marshal(merged)
		bodyReader = bytes.NewReader(bodyBytes)
		headerMap["Content-Type"] = "application/json"
	}
```

替换为：

```go
	if len(formParams) > 0 {
		// 使用 FormHandlerManager 处理 form 参数，构建 multipart/form-data 请求体
		formBody, formContentType, formErr := r.processFormData(ctx, formParams, bodyParams)
		if formErr != nil {
			return nil, formErr
		}
		bodyReader = bytes.NewReader(formBody)
		headerMap = prepareHeadersForFormData(headerMap)
		headerMap["Content-Type"] = formContentType
	}
```

- [ ] **Step 3: 在非导出函数区块添加 processFormData 方法**

在 `restful_api.go` 文件末尾（`validatePathParams` 函数之后）添加：

```go
// processFormData 使用 FormHandlerManager 处理表单参数，构建 multipart/form-data 请求体。
//
// 流程（对齐 Python RestfulApi._process_form_data）：
//  1. 创建 bytes.Buffer + multipart.Writer
//  2. 遍历 formParams，每个参数根据 form_handler_type 获取对应处理器
//  3. 调用 handler.Handle() 将字段写入 multipart Writer
//  4. 遍历 bodyParams，非 nil 值以 application/json content_type 写入
//  5. 关闭 Writer，返回 buffer 字节和 multipart content-type（含 boundary）
//
// 对应 Python: RestfulApi._process_form_data()
func (r *RestfulApi) processFormData(
	ctx context.Context,
	formParams map[string]any,
	bodyParams map[string]any,
) (bodyBytes []byte, contentType string, err error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	mgr := form_handler.GetFormHandlerManager()

	// 处理 form_params
	for paramName, paramInfo := range formParams {
		info, ok := paramInfo.(map[string]any)
		if !ok {
			continue
		}
		handlerType := "default"
		if ht, ok := info["form_handler_type"].(string); ok && ht != "" {
			handlerType = ht
		}
		value := info["value"]

		handler := mgr.GetHandler(handlerType)
		if handleErr := handler.Handle(ctx, writer, paramName, value); handleErr != nil {
			return nil, "", exception.BuildError(
				exception.StatusToolRestfulApiExecutionError,
				exception.WithParam("method", r.card.Method),
				exception.WithParam("reason", fmt.Sprintf("表单字段 %q 处理失败: %s", paramName, handleErr.Error())),
				exception.WithCause(handleErr),
			)
		}
	}

	// 处理 body_params：以 application/json content-type 追加到 form
	for paramName, paramValue := range bodyParams {
		if paramValue == nil {
			continue
		}
		jsonBytes, marshalErr := json.Marshal(paramValue)
		if marshalErr != nil {
			continue
		}
		part, createErr := writer.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": {fmt.Sprintf(`form-data; name="%s"`, paramName)},
			"Content-Type":        {"application/json"},
		})
		if createErr != nil {
			continue
		}
		if _, writeErr := part.Write(jsonBytes); writeErr != nil {
			continue
		}
	}

	// 关闭 Writer（必须，写入 terminating boundary）
	writer.Close()

	return buf.Bytes(), writer.FormDataContentType(), nil
}

// prepareHeadersForFormData 为 multipart/form-data 请求准备请求头。
//
// 移除手动设置的 Content-Type，因为 multipart.Writer 会自动生成
// 包含 boundary 的正确 Content-Type。手动设置会导致请求失败。
//
// 对应 Python: RestfulApi._prepare_headers_for_form_data()
func prepareHeadersForFormData(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return make(map[string]string)
	}
	processed := make(map[string]string, len(headers))
	for key, value := range headers {
		if strings.ToLower(key) == "content-type" {
			logger.Debug(logger.ComponentAgentCore).
				Str("content_type", value).
				Msg("multipart/form-data 请求移除手动设置的 Content-Type，将自动设置含 boundary 的正确值")
			continue
		}
		processed[key] = value
	}
	return processed
}
```

- [ ] **Step 4: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/foundation/tool/service_api/`
Expected: 无错误

- [ ] **Step 5: 运行现有测试确保无回归**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/service_api/ -v`
Expected: 所有现有测试通过（TestRestfulApi_Invoke_Form参数Fallback 的行为会改变，需在 Task 5 更新）

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/foundation/tool/service_api/restful_api.go
git commit -m "feat(service_api): 集成 form_handler 包，替换 form_params fallback 为 multipart/form-data"
```

---

### Task 5: 更新 restful_api_test.go — 修改 fallback 测试 + 新增 form 集成测试

**Files:**
- Modify: `internal/agentcore/foundation/tool/service_api/restful_api_test.go`

- [ ] **Step 1: 替换 TestRestfulApi_Invoke_Form参数Fallback 测试**

将现有 `TestRestfulApi_Invoke_Form参数Fallback` 函数替换为 multipart/form-data 验证版本：

```go
// TestRestfulApi_Invoke_Form参数Multipart 测试 form 参数构建 multipart/form-data 请求
func TestRestfulApi_Invoke_Form参数Multipart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证 Content-Type 为 multipart/form-data
		contentType := r.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "multipart/form-data") {
			t.Errorf("Content-Type 应以 multipart/form-data 开头，实际: %s", contentType)
		}
		// 解析 multipart form
		err := r.ParseMultipartForm(10 << 20)
		if err != nil {
			t.Fatalf("解析 multipart form 失败: %v", err)
		}
		// 验证 form 字段
		if r.FormValue("file") != "data.txt" {
			t.Errorf("form file: 期望 data.txt，实际 %s", r.FormValue("file"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer server.Close()

	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file": map[string]any{"type": "string", "location": "form"},
		},
	}
	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/upload", "POST", inputSchema)
	api, _ := NewRestfulApi(card)

	result, err := api.Invoke(context.Background(), map[string]any{"file": "data.txt"})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	code, _ := result["code"].(int)
	if code != 200 {
		t.Errorf("code: 期望 200，实际 %d", code)
	}
}
```

- [ ] **Step 2: 添加 import strings**

在 `restful_api_test.go` 的 import 中添加 `"strings"`：

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)
```

- [ ] **Step 3: 新增 TestRestfulApi_Invoke_Form参数含BodyParams 测试**

在 `TestRestfulApi_Invoke_Form参数Multipart` 之后添加：

```go
// TestRestfulApi_Invoke_Form参数含BodyParams 测试 form 参数 + body 参数组合
func TestRestfulApi_Invoke_Form参数含BodyParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType := r.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "multipart/form-data") {
			t.Errorf("Content-Type 应以 multipart/form-data 开头，实际: %s", contentType)
		}
		err := r.ParseMultipartForm(10 << 20)
		if err != nil {
			t.Fatalf("解析 multipart form 失败: %v", err)
		}
		// 验证 form 字段
		if r.FormValue("file") != "data.txt" {
			t.Errorf("form file: 期望 data.txt，实际 %s", r.FormValue("file"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer server.Close()

	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file":    map[string]any{"type": "string", "location": "form"},
			"comment": map[string]any{"type": "string"}, // body 参数
		},
	}
	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/upload", "POST", inputSchema)
	api, _ := NewRestfulApi(card)

	result, err := api.Invoke(context.Background(), map[string]any{"file": "data.txt", "comment": "test"})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	code, _ := result["code"].(int)
	if code != 200 {
		t.Errorf("code: 期望 200，实际 %d", code)
	}
}
```

- [ ] **Step 4: 新增 prepareHeadersForFormData 测试**

在文件末尾添加：

```go
// TestPrepareHeadersForFormData_移除ContentType 测试手动 Content-Type 被移除
func TestPrepareHeadersForFormData_移除ContentType(t *testing.T) {
	headers := map[string]string{
		"Content-Type": "application/json",
		"Authorization": "Bearer token",
	}
	result := prepareHeadersForFormData(headers)
	if _, ok := result["Content-Type"]; ok {
		t.Error("Content-Type 应被移除")
	}
	if result["Authorization"] != "Bearer token" {
		t.Error("Authorization 应保留")
	}
}

// TestPrepareHeadersForFormData_保留其他头 测试非 Content-Type 头保留
func TestPrepareHeadersForFormData_保留其他头(t *testing.T) {
	headers := map[string]string{
		"X-Custom": "value",
		"Accept":   "application/json",
	}
	result := prepareHeadersForFormData(headers)
	if len(result) != 2 {
		t.Errorf("期望 2 个头，实际 %d", len(result))
	}
	if result["X-Custom"] != "value" {
		t.Error("X-Custom 应保留")
	}
	if result["Accept"] != "application/json" {
		t.Error("Accept 应保留")
	}
}

// TestPrepareHeadersForFormData_空头 测试空输入
func TestPrepareHeadersForFormData_空头(t *testing.T) {
	result := prepareHeadersForFormData(nil)
	if result == nil {
		t.Error("不应返回 nil")
	}
	if len(result) != 0 {
		t.Errorf("期望空 map，实际 %d 个元素", len(result))
	}

	result2 := prepareHeadersForFormData(map[string]string{})
	if len(result2) != 0 {
		t.Errorf("期望空 map，实际 %d 个元素", len(result2))
	}
}

// TestPrepareHeadersForFormData_大小写不敏感 测试 Content-Type 大小写不敏感
func TestPrepareHeadersForFormData_大小写不敏感(t *testing.T) {
	headers := map[string]string{
		"content-type": "text/plain",
		"CONTENT-TYPE": "text/html",
	}
	// 注意：map 的 key 是唯一的，所以只能有一个 content-type 变体
	// 测试小写 content-type 被移除
	result := prepareHeadersForFormData(headers)
	if len(result) != 0 {
		t.Errorf("content-type（任何大小写）都应被移除，实际 %d 个元素", len(result))
	}
}
```

- [ ] **Step 5: 新增 processFormData 测试**

```go
// TestProcessFormData_默认处理器 测试 processFormData 使用默认处理器
func TestProcessFormData_默认处理器(t *testing.T) {
	card, _ := NewRestfulApiCard("test-api", "测试API", "https://api.example.com/upload", "POST", nil)
	api, _ := NewRestfulApi(card)

	formParams := map[string]any{
		"file": map[string]any{
			"form_handler_type": "default",
			"value":             "data.txt",
		},
	}
	bodyParams := map[string]any{}

	bodyBytes, contentType, err := api.processFormData(context.Background(), formParams, bodyParams)
	if err != nil {
		t.Fatalf("processFormData 失败: %v", err)
	}
	if len(bodyBytes) == 0 {
		t.Error("bodyBytes 不应为空")
	}
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		t.Errorf("contentType 应以 multipart/form-data 开头，实际: %s", contentType)
	}
}

// TestProcessFormData_BodyParams追加 测试 body_params 以 application/json 追加
func TestProcessFormData_BodyParams追加(t *testing.T) {
	card, _ := NewRestfulApiCard("test-api", "测试API", "https://api.example.com/upload", "POST", nil)
	api, _ := NewRestfulApi(card)

	formParams := map[string]any{
		"file": map[string]any{
			"form_handler_type": "default",
			"value":             "data.txt",
		},
	}
	bodyParams := map[string]any{
		"metadata": map[string]any{"key": "value"},
	}

	bodyBytes, _, err := api.processFormData(context.Background(), formParams, bodyParams)
	if err != nil {
		t.Fatalf("processFormData 失败: %v", err)
	}
	bodyStr := string(bodyBytes)
	if !strings.Contains(bodyStr, "metadata") {
		t.Error("输出应包含 body 参数 metadata")
	}
	if !strings.Contains(bodyStr, "application/json") {
		t.Error("body 参数应以 application/json content-type 写入")
	}
}

// TestProcessFormData_Nil值跳过 测试 body_params 中 nil 值不写入
func TestProcessFormData_Nil值跳过(t *testing.T) {
	card, _ := NewRestfulApiCard("test-api", "测试API", "https://api.example.com/upload", "POST", nil)
	api, _ := NewRestfulApi(card)

	formParams := map[string]any{
		"file": map[string]any{
			"form_handler_type": "default",
			"value":             "data.txt",
		},
	}
	bodyParams := map[string]any{
		"nil_field": nil,
	}

	bodyBytes, _, err := api.processFormData(context.Background(), formParams, bodyParams)
	if err != nil {
		t.Fatalf("processFormData 失败: %v", err)
	}
	bodyStr := string(bodyBytes)
	if strings.Contains(bodyStr, "nil_field") {
		t.Error("nil body 参数不应写入")
	}
}
```

- [ ] **Step 6: 运行所有测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/service_api/ -v -run "Form|Multipart|Prepare|ProcessForm"`
Expected: 所有新增和修改的测试通过

- [ ] **Step 7: 运行全部 service_api 测试确认无回归**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/service_api/ -v`
Expected: 所有测试通过

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/foundation/tool/service_api/restful_api_test.go
git commit -m "test(service_api): 更新 form 参数测试为 multipart/form-data，新增 processFormData 和 prepareHeadersForFormData 测试"
```

---

### Task 6: 更新 service_api/doc.go — 文件目录和设计决策

**Files:**
- Modify: `internal/agentcore/foundation/tool/service_api/doc.go`

- [ ] **Step 1: 更新 doc.go**

将文件内容更新为：

```go
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
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/foundation/tool/service_api/`
Expected: 无错误

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/foundation/tool/service_api/doc.go
git commit -m "docs(service_api): 更新 doc.go，添加 form_handler 集成说明"
```

---

### Task 7: 更新 tool/doc.go — 添加 form_handler 子包

**Files:**
- Modify: `internal/agentcore/foundation/tool/doc.go`

- [ ] **Step 1: 在文件目录树中添加 form_handler 子包**

在 `tool/doc.go` 的文件目录树中，在 `service_api/` 条目前添加 `form_handler/` 条目。找到以下内容：

```
//	├── service_api/
```

在其前面插入：

```
//	├── form_handler/
//	│   ├── doc.go              # 表单处理器子包文档
//	│   └── form_handler.go     # FormHandler 接口 + DefaultFormHandler + FormHandlerManager
//	├── service_api/
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/foundation/tool/`
Expected: 无错误

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/foundation/tool/doc.go
git commit -m "docs(tool): 在 doc.go 文件目录中添加 form_handler 子包"
```

---

### Task 8: 运行全量测试 + 覆盖率检查 + 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 运行 form_handler 包覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/foundation/tool/form_handler/`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 2: 运行 service_api 包覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/foundation/tool/service_api/`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 运行全量测试确认无回归**

Run: `cd /home/opensource/uap-claw-go && go test ./... 2>&1 | tail -30`
Expected: 所有测试通过

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md**

将 3.10 行的状态从 `☐` 改为 `✅`：

找到：
```
| 3.10 | ☐ | Form Handler | 表单数据处理 | `openjiuwen/core/foundation/tool/form_handler/` |
```

替换为：
```
| 3.10 | ✅ | Form Handler | 表单数据处理 | `openjiuwen/core/foundation/tool/form_handler/` |
```

- [ ] **Step 5: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "chore: 标记 3.10 Form Handler 为已完成"
```
