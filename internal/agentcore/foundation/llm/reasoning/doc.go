// Package reasoning 提供厂商中立的思考/推理档位解析与线上参数编码。
//
// 上游把「用户选的档位」（off/on/low/medium/high/xhigh/max）翻译成各厂商不同的
// 请求字段：OpenAI 用 reasoning_effort、Anthropic 用 thinking.type+budget_tokens、
// DashScope 用 enable_thinking+thinking_budget、OpenRouter 用 reasoning{effort,...}、
// DeepSeek 用 thinking.type+reasoning_effort 等。本包只负责翻译，不负责发送。
//
// 文件目录：
//
//	reasoning/
//	├── doc.go       # 包文档
//	├── profiles.go  # 42 条 provider profile + 23 条 model fallback + 能力查询
//	└── protocol.go  # 协议标识常量
//
// 对应 Python 代码：openjiuwen/core/foundation/llm/reasoning_profiles.py
package reasoning
