
package runtime

import (
	"fmt"
	"os"
)

// qaProbe 临时探针（调试问答闭环，验证后删除）。
func qaProbe(step string, kv ...any) {
	f, err := os.OpenFile("/tmp/pqa-go.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[runtime] %s %v\n", step, kv)
}
