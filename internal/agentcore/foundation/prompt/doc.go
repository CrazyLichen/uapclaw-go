// Package prompt 提供可插值替换的 Prompt 模板系统，支持字符串和消息列表两种内容格式。
//
// 本包是领域二（LLM 基础层）2.17 节的实现，对应 Python 路径：
//
//	openjiuwen/core/foundation/prompt/
//
// # 核心组件
//
//	Variable           — 模板变量接口，定义 Eval/Update 协议
//	TextableVariable   — 字符串模板变量，处理 {{placeholder}} 占位符替换
//	DictableVariable   — 字典/列表模板变量，递归处理多模态内容中的占位符
//	PromptAssembler    — 模板装配器，编排变量求值和模板渲染
//	PromptTemplate     — 用户面向类，支持 Format（部分填充）和 ToMessages
//
// # 模板语法
//
//	{{variable}}           — 变量替换（前后缀可配置）
//	{{user.profile.name}}  — 嵌套属性解析（逐层 map/struct 访问）
//
// 不支持条件逻辑、循环或表达式计算，这是纯字符串替换引擎。
//
// # 使用方式
//
// 字符串模板：
//
//	tmpl := prompt.NewPromptTemplate("greeting", "Hello {{name}}!")
//	formatted, _ := tmpl.Format(map[string]any{"name": "Alice"})
//	msgs, _ := formatted.ToMessages()
//	// msgs[0] = UserMessage{Content: "Hello Alice!"}
//
// 消息列表模板：
//
//	tmpl := prompt.NewPromptTemplate("", []schema.BaseMessage{
//	    schema.NewSystemMessage("You are a {{domain}} expert."),
//	    schema.NewUserMessage("Hi, {{name}}!"),
//	})
//	formatted, _ := tmpl.Format(map[string]any{"domain": "AI", "name": "Bob"})
//	msgs, _ := formatted.ToMessages()
//
// 部分填充（链式调用）：
//
//	step1, _ := tmpl.Format(map[string]any{"domain": "AI"})       // name 保留 {{name}}
//	step2, _ := step1.Format(map[string]any{"name": "Bob"})       // 全部填充完成
//
// # 文件清单
//
//	prompt/
//	  doc.go                    — 包文档（本文件）
//	  variable.go               — Variable 接口 + baseVariable
//	  textable_variable.go      — TextableVariable
//	  dictable_variable.go      — DictableVariable
//	  assembler.go              — PromptAssembler
//	  template.go               — PromptTemplate
//	  variable_test.go
//	  textable_variable_test.go
//	  dictable_variable_test.go
//	  assembler_test.go
//	  template_test.go
//
// # Python 对应路径
//
//	openjiuwen/core/foundation/prompt/assemble/variables/variable.py   — Variable
//	openjiuwen/core/foundation/prompt/assemble/variables/textable.py   — TextableVariable
//	openjiuwen/core/foundation/prompt/assemble/variables/dictable.py   — DictableVariable
//	openjiuwen/core/foundation/prompt/assemble/assembler.py            — PromptAssembler
//	openjiuwen/core/foundation/prompt/template.py                      — PromptTemplate
package prompt
