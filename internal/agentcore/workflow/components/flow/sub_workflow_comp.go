package flow

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// SubWorkflowStreamState 子工作流组件的流式结果缓存状态
// （Python workflow_comp.py:22-63 SubWorkflowStreamState 一比一）。
// 并发约定：Accumulate 仅由 stream 生产方调用、BuildFinalResult 由通道耗尽后的
// Vertex postStream / invoke 短路检查调用，通道序自带 happens-before，不加锁
// （spec 决策 #3；定义复用+并发 Invoke 的缓存混淆为已知限制 D-14b-3）。
type SubWorkflowStreamState struct {
	// accumulatedOutputs 累积的流帧（Python _accumulated_outputs）
	accumulatedOutputs []any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Accumulate 累积流帧（Python accumulate）
func (s *SubWorkflowStreamState) Accumulate(output any) {
	s.accumulatedOutputs = append(s.accumulatedOutputs, output)
}

// BuildFinalResult 合并累积帧为最终结果（Python build_final_result :41-63）：
// 只认 map 帧；嵌套取 output 键回退自身；同 key 相加——string 拼接、数字求和、
// 其余类型（含 map+map）对齐 Python TypeError 返回 error（spec 决策 #2）。
func (s *SubWorkflowStreamState) BuildFinalResult() (map[string]any, error) {
	if len(s.accumulatedOutputs) == 0 {
		return map[string]any{}, nil
	}
	merged := map[string]any{}
	for _, output := range s.accumulatedOutputs {
		m, ok := output.(map[string]any)
		if !ok {
			continue
		}
		inner, ok := m["output"]
		if !ok {
			inner = m
		}
		innerMap, ok := inner.(map[string]any)
		if !ok {
			continue
		}
		for key, value := range innerMap {
			if old, exists := merged[key]; exists {
				sum, err := addValues(old, value)
				if err != nil {
					return nil, err
				}
				merged[key] = sum
			} else {
				merged[key] = value
			}
		}
	}
	return merged, nil
}

// Clear 清空状态（Python clear）
func (s *SubWorkflowStreamState) Clear() {
	s.accumulatedOutputs = nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// addValues 复刻 Python merged[key] + value 的类型分派：
// string+string 拼接、数值求和（int/float 提升）、其余报错对齐 TypeError。
func addValues(a, b any) (any, error) {
	as, aok := a.(string)
	bs, bok := b.(string)
	if aok && bok {
		return as + bs, nil
	}
	af, aIsNum := toFloat64(a)
	bf, bIsNum := toFloat64(b)
	if aIsNum && bIsNum {
		return af + bf, nil
	}
	return nil, fmt.Errorf("unsupported operand type(s) for +: '%T' and '%T'", a, b)
}

// toFloat64 数值类型统一提升（int/int64/float64/float32；bool 经 int 语义天然覆盖 Python bool+bool=1+1）
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case float32:
		return float64(n), true
	default:
		return 0, false
	}
}
