package callback

import (
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	sessionutils "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OutputCallback 输出收割回调（Python output.py:9 OutputCallback(LoopCallback)）：
// 每轮把循环体输出按 outputs_format 取输入累积进 round 数组；跳出时按引用路径
// 收割成最终结构——循环的"返回值组装"职责承担者。
type OutputCallback struct {
	LoopCallback
	// outputsFormat 输出格式映射（Python self._outputs_format）
	outputsFormat map[string]any
	// resultRoot 结果根（Python self._result_root，None 保留）
	resultRoot string
	// roundResultRoot 轮次结果数组键（Python self._round_result_root，空回退 "round"）
	roundResultRoot string
}

// NewOutputCallback 构造（output.py:10-15：round_result_root 空回退 "round"）。
func NewOutputCallback(outputsFormat map[string]any, roundResultRoot, resultRoot string) *OutputCallback {
	if roundResultRoot == "" {
		roundResultRoot = "round"
	}
	c := &OutputCallback{
		outputsFormat:   outputsFormat,
		resultRoot:      resultRoot,
		roundResultRoot: roundResultRoot,
	}
	c.LoopCallback = NewLoopCallback(c.firstInLoop, c.startRound, c.endRound, c.outLoop)
	return c
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// firstInLoop 初始化轮次结果数组（output.py:17-20 一比一）。
func (c *OutputCallback) firstInLoop(sess session.BaseSession) (any, error) {
	// Python: _results = []; session.state().update({root: _results}); return None
	_ = sess.State().Update(map[string]any{c.roundResultRoot: []any{}})
	return nil, nil
}

// startRound 无操作（output.py:59-60 return None）。
func (c *OutputCallback) startRound(_ session.BaseSession) (any, error) { return nil, nil }

// endRound 累积轮次输出（output.py:46-52 一比一）。
func (c *OutputCallback) endRound(sess session.BaseSession, loopTimes int) (any, error) {
	// Python: results = session.state().get(self._round_result_root)
	results, _ := sess.State().Get(state.StringKey(c.roundResultRoot)).([]any)
	// Python: if not isinstance(results, list): raise ValueError("error results in round process")
	// （Get 缺键返回 nil → 非 list → 等价报错）
	if results == nil {
		return nil, fmt.Errorf("error results in round process")
	}
	// Python: if len(results) >= loop_times: return None
	if len(results) >= loopTimes {
		return nil, nil
	}
	// Python: results.append(session.state().get_inputs(self._outputs_format))
	var inputs any
	if ws, ok := sess.State().(state.WorkflowState); ok {
		// Python: get_inputs(self._outputs_format)——dict schema 走 SchemaKey 引用解析
		inputs = ws.GetInputs(schemaKeyOf(c.outputsFormat))
	}
	appendResult(inputs, &results)
	// Python: session.state().update({root: results})
	_ = sess.State().Update(map[string]any{c.roundResultRoot: results})
	return nil, nil
}

// outLoop 收割最终输出（output.py:54-57 一比一）。
func (c *OutputCallback) outLoop(sess session.BaseSession) (any, error) {
	// Python: results = session.state().get(self._roundResultRoot)
	results, _ := sess.State().Get(state.StringKey(c.roundResultRoot)).([]any)
	return c.generateOutput(sess, results, []string{}, c.outputsFormat), nil
}

// generateOutput 递归收割（output.py:22-44 _generate_output 一比一）：
// dict 逐键递归 / 引用路径且首段==node_id 取 results[-1] 下钻 / 兜底遍历 results 下钻成数组。
func (c *OutputCallback) generateOutput(sess session.BaseSession, results []any, root []string, outputFormat any) any {
	// Python: if isinstance(output_format, dict)
	if m, ok := outputFormat.(map[string]any); ok {
		output := map[string]any{}
		for key, value := range m {
			path := append(append([]string{}, root...), key)
			output[key] = c.generateOutput(sess, results, path, value)
		}
		return output
	}
	// Python: if isinstance(output_format, str) and is_ref_path(output_format)
	if s, ok := outputFormat.(string); ok && sessionutils.IsRefPath(s) {
		refStr := sessionutils.ExtractOriginKey(s)
		path := strings.Split(refStr, sessionutils.NestedPathSplit)
		nodeID := ""
	if ep, ok := sess.(sessioninterfaces.ExecutableIDProvider); ok {
		nodeID = ep.ExecutableID() // Python: session.node_id() → Go ExecutableIDProvider 面
	}
		if path[0] == nodeID {
			// Python: if len(results) == 0: return None
			if len(results) == 0 {
				return nil
			}
			// Python: data = results[-1]; for key in root: data = data.get(key)
			data := results[len(results)-1]
			return digNested(data, root)
		}
	}
	// Python 兜底：output = []; for result in results: data = result; for key in root: data = data.get(key)
	output := []any{}
	for _, result := range results {
		output = append(output, digNested(result, root))
	}
	return output
}

// appendResult 把 get_inputs 结果 append 进 results（Python results.append 一比一；
// 无输入时 Python append None，Go 以 nil 元素承载）。
func appendResult(inputs any, results *[]any) {
	*results = append(*results, inputs)
}

// digNested 逐键下钻（Python data.get(key) 链；缺键/非 dict → nil）。
func digNested(data any, keys []string) any {
	for _, key := range keys {
		m, ok := data.(map[string]any)
		if !ok {
			return nil
		}
		data = m[key]
	}
	return data
}
