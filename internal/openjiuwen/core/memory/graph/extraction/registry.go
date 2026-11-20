package extraction

// 通过空白导入触发 cn/en 语言的 init() 注册
import (
	_ "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/memory/graph/extraction/prompts/cn"
	_ "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/memory/graph/extraction/prompts/en"
)
