// Package harness core 侧图片附件 turn ctx 键与助手。
//
// Python 的本轮图片附件经 contextvars「隐形袋子」（interface_deep.py:18412
// set_current_multimodal_image_files → current_multimodal_image_files）从
// 适配器入口传到 before_model_call rail。Go 侧 chat.send 经 SendInput 队列
// 解耦：适配器 goroutine 写 request ctx 无法到达交互消费 goroutine 的
// rail 回调（D-23.8-1 的原始假设在该链路不成立）——对齐 Python 语义改为
// 「inputs 随队列过河 + 消费侧 pop 写 turn ctx」（interface_deep.py:18412
// inputs.pop("_multimodal_image_files") 的等价实现）。
//
// 键与原始载荷（[]map[string]any）定义在 core 侧，swarm 侧 prompt 包按
// swarm→core 方向引用读取并转换为强类型清单。
package turnctx

import (
	"context"
)

// turnMultimodalImageFilesKey 本轮图片附件 ctx 键（raw dict 列表）。
type turnMultimodalImageFilesKey struct{}

// WithTurnMultimodalImageFiles 把本轮图片附件 raw 清单写入 turn ctx。
// Python: set_current_multimodal_image_files(files)（interface_deep.py:18409）。
func WithTurnMultimodalImageFiles(ctx context.Context, files []map[string]any) context.Context {
	return context.WithValue(ctx, turnMultimodalImageFilesKey{}, files)
}

// TurnMultimodalImageFilesFromContext 读回本轮图片附件 raw 清单；无则 nil。
// Python: current_multimodal_image_files()（interface_deep.py:18410）。
func TurnMultimodalImageFilesFromContext(ctx context.Context) []map[string]any {
	if ctx == nil {
		return nil
	}
	files, _ := ctx.Value(turnMultimodalImageFilesKey{}).([]map[string]any)
	return files
}

// PopTurnMultimodalImageFiles 从 inputs 弹出图片附件清单（存在性无关）。
// Python: inputs.pop("_multimodal_image_files", None)（interface_deep.py:18412）。
// 返回 raw dict 列表；inputs 中可能出现 []map[string]any（适配器直塞）或
// []any（JSON 往返），两种形态统一归一。
func PopTurnMultimodalImageFiles(inputs map[string]any) []map[string]any {
	raw, ok := inputs["_multimodal_image_files"]
	if !ok || raw == nil {
		return nil
	}
	delete(inputs, "_multimodal_image_files")
	switch v := raw.(type) {
	case []map[string]any:
		return v
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}
