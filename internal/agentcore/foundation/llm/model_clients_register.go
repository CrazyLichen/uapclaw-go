// 集中导入所有内置 model_client 子包，触发 init() 注册到 ClientRegistry。
//
// 使用方只需 import llm 包，即可通过 InitModel 使用所有已注册的 provider。
// 对应 Python: _builtin_model_client() 中局部 import 触发 __init_subclass__ 注册。
//
// 2.14/2.15 回填点：blank import 各 model_client 包触发 init() 注册。
//
// 机制说明：
//
//	Go 的 blank import（import _ "xxx"）会执行被导入包的 init() 函数。
//	各 model_client 子包的 init() 调用 registry.Register() 注册工厂到全局注册表。
//	此文件将所有子包的 blank import 集中管理，任何 import llm 包的代码自动获得所有 provider。
package llm

import (
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/dashscope"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/deepseek"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/inference_affinity"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/intellirouter"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/openai"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/siliconflow"
)
