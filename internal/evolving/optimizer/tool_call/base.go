package tool_call

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolOptimizerBase 工具维度优化器基类。
// 固定 domain="tool"，默认优化目标为 ["tool_description"]。
// 核心入口是 OptimizeTool()，Backward/Step 对齐 Python 空实现。
//
// 对应 Python: ToolOptimizerBase
type ToolOptimizerBase struct {
	optimizer.BaseOptimizerMixin
	// maxTurns 最大迭代轮数
	maxTurns int
	// llmAPIKey LLM API 密钥
	llmAPIKey string
	// configEg Example Stage 配置
	configEg map[string]any
	// configDesc Description Stage 配置
	configDesc map[string]any
	// pathSaveDir 结果保存目录
	pathSaveDir string
	// model LLM 模型客户端
	model *llm.Model
}

// ToolOptimizerBaseOption ToolOptimizerBase 构造选项函数。
type ToolOptimizerBaseOption func(*ToolOptimizerBase)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolOptimizerBase 创建 ToolOptimizerBase 实例。
//
// 对齐 Python: ToolOptimizerBase.__init__(self, **kwargs)
//
//	self.max_turns = kwargs.get("max_turns", 5)
//	self.llm_api_key = kwargs.get("llm_api_key", "")
//	self.config_eg = kwargs.get("config_eg", default_config_eg)
//	self.config_desc = kwargs.get("config_desc", default_config_desc)
//	self.path_save_dir = kwargs.get("path_save_dir", "./tool_optimizer_results")
//	self.config_eg['save_dir'] = os.path.join(self.path_save_dir, "examples")
//	self.config_desc['save_dir'] = os.path.join(self.path_save_dir, "descriptions")
//	self.config_desc['examples_dir'] = self.config_eg['save_dir']
//	self.config_desc['neg_ex_input_path'] = os.path.join(self.path_save_dir, f"{kwargs.get('tool_name','tool')}.json")
func NewToolOptimizerBase(model *llm.Model, opts ...ToolOptimizerBaseOption) *ToolOptimizerBase {
	// 对齐 Python: 使用默认配置
	configEg := copyMap(DefaultConfigEg)
	configDesc := copyMap(DefaultConfigDesc)

	o := &ToolOptimizerBase{
		maxTurns:    5,
		llmAPIKey:   "",
		configEg:    configEg,
		configDesc:  configDesc,
		pathSaveDir: "./tool_optimizer_results",
		model:       model,
	}

	for _, opt := range opts {
		opt(o)
	}

	// 对齐 Python: 路径拼接
	o.configEg["save_dir"] = filepath.Join(o.pathSaveDir, "examples")
	o.configDesc["save_dir"] = filepath.Join(o.pathSaveDir, "descriptions")
	o.configDesc["examples_dir"] = o.configEg["save_dir"]

	toolName := "tool"
	o.configDesc["neg_ex_input_path"] = filepath.Join(o.pathSaveDir, toolName+".json")

	// 对齐 Python: llm_api_key 传递给 config
	o.configEg["llm_api_key"] = o.llmAPIKey
	o.configDesc["llm_api_key"] = o.llmAPIKey

	return o
}

// Domain 返回优化器域 "tool"。
//
// 对齐 Python: ToolOptimizerBase.domain = "tool"
func (b *ToolOptimizerBase) Domain() string {
	return "tool"
}

// DefaultTargets 返回默认优化目标列表。
//
// 对齐 Python: ToolOptimizerBase.default_targets() → ["tool_description"]
func (b *ToolOptimizerBase) DefaultTargets() []string {
	return []string{"tool_description"}
}

// RequiresForwardData 返回 false，ToolOptimizer 是黑盒优化器，
// 内部自己生成/执行/评估，不依赖框架前向推理。
func (b *ToolOptimizerBase) RequiresForwardData() bool {
	return false
}

// OptimizeTool 核心入口：两阶段迭代优化工具描述。
//
// 对齐 Python: ToolOptimizerBase.optimize_tool(tool, tool_callable)
//
//  1. 保存原始描述 original_desc = tool["description"]
//  2. for i in range(self.max_turns):
//     if i > 0: tool["description"] = 最新描述
//     # Stage 1 - Example
//     result_example = customized_pipeline("example", tool, tool_callable=tool_callable, config=self.config_eg)
//     # Stage 2 - Description
//     result_desc = customized_pipeline("description", tool, tool_callable=tool_callable, config=self.config_desc)
//  3. 最终审查：ToolDescriptionReviewer.Process(output_desc, ori_tool, ["clean","cross_check","translate"])
//  4. 格式化：ToolDescriptionReviewer.Format(schema, processed)
//  5. 返回 final_desc
func (b *ToolOptimizerBase) OptimizeTool(
	ctx context.Context,
	tool map[string]any,
	toolCallable APIWrapperFunc,
) (map[string]any, error) {
	// 对齐 Python: original_desc = tool["description"]
	originalDesc, _ := tool["description"].(string)

	var resultExamples [][]any
	var resultDescs [][][]any

	// 对齐 Python: for i in range(self.max_turns):
	for i := 0; i < b.maxTurns; i++ {
		// 对齐 Python: if i > 0: latest_description = result_descs[-1][-1][0]["description"]
		if i > 0 && len(resultDescs) > 0 && len(resultDescs[len(resultDescs)-1]) > 0 {
			lastDescBatch := resultDescs[len(resultDescs)-1]
			if len(lastDescBatch) > 0 {
				lastNode := lastDescBatch[len(lastDescBatch)-1]
				if len(lastNode) > 0 {
					if lastStep, ok := lastNode[len(lastNode)-1].(map[string]any); ok {
						if desc, ok := lastStep["description"].(string); ok {
							tool["description"] = desc
						}
					}
				}
			}
		}

		// 对齐 Python: Stage 1 - Example
		// default_config_desc['llm_api_key'] = self.llm_api_key
		// default_config_eg['llm_api_key'] = self.llm_api_key
		b.configEg["llm_api_key"] = b.llmAPIKey
		b.configDesc["llm_api_key"] = b.llmAPIKey

		resultExample, err := CustomizedPipeline(ctx, "example", tool, b.configEg, toolCallable, b.model)
		if err != nil {
			logger.Error(logComponent).
				Str("method", "OptimizeTool").
				Int("iteration", i).
				Str("stage", "example").
				Err(err).
				Msg("示例阶段失败")
			// 对齐 Python: 不中断，继续
		} else {
			resultExamples = append(resultExamples, resultExample...)
			_ = resultExamples // 对齐 Python：结果记录用于调试，暂不消费
			logger.Info(logComponent).
				Str("method", "OptimizeTool").
				Int("iteration", i).
				Msg("=== 示例阶段完成 ===")
		}

		// 对齐 Python: Stage 2 - Description
		resultDesc, err := CustomizedPipeline(ctx, "description", tool, b.configDesc, toolCallable, b.model)
		if err != nil {
			logger.Error(logComponent).
				Str("method", "OptimizeTool").
				Int("iteration", i).
				Str("stage", "description").
				Err(err).
				Msg("Description stage failed")
		} else {
			resultDescs = append(resultDescs, resultDesc)
		}
	}

	// 对齐 Python: description final reviewer
	if len(resultDescs) == 0 {
		return nil, fmt.Errorf("未生成描述结果")
	}

	// 对齐 Python: output_desc = result_descs[-1][-1][-1]["description"]
	outputDesc := extractLastDescription(resultDescs)

	// 对齐 Python: eval_model_id = self.config_desc.get("eval_model_id")
	evalModelID := getConfigString(b.configDesc, "eval_model_id")

	// 对齐 Python: processor = ToolDescriptionReviewer(eval_model_id=eval_model_id, llm_api_key=self.llm_api_key)
	processor := NewToolDescriptionReviewer(evalModelID, b.llmAPIKey, b.model)

	// 对齐 Python: schema = extract_schema(original_desc)
	schema := ExtractSchemaFromJSON(originalDesc)

	// 对齐 Python: processed = processor.process(data=output_desc, ori_tool=tool["description"], steps=["clean", "cross_check", "translate"])
	// Python 的 process 接受 dict，但 output_desc 是 string。
	// 尝试将 outputDesc 解析为 JSON dict；如果失败则包装为 {"description": outputDesc}
	var dataForProcess map[string]any
	if jsonErr := json.Unmarshal([]byte(outputDesc), &dataForProcess); jsonErr != nil {
		dataForProcess = map[string]any{"description": outputDesc}
	}

	oriToolDesc, _ := tool["description"].(string)
	processed, err := processor.Process(ctx, dataForProcess, oriToolDesc, []string{"clean", "cross_check", "translate"})
	if err != nil {
		logger.Error(logComponent).
			Str("method", "OptimizeTool").
			Err(err).
			Msg("描述审查失败")
		return nil, err
	}

	// 对齐 Python: final_desc = processor.format(schema, processed, example=None)
	// Python 的 format 第二个参数是 description: str，但 processed 是 dict。
	// 将 processed 序列化为 JSON 字符串作为 description 参数
	processedDesc := toJSON(processed)
	finalDesc, err := processor.Format(ctx, schema, processedDesc, nil)
	if err != nil {
		logger.Error(logComponent).
			Str("method", "OptimizeTool").
			Err(err).
			Msg("描述格式化失败")
		return nil, err
	}

	return finalDesc, nil
}

// Backward 反向传播：从信号计算梯度。对齐 Python 空实现。
//
// 对齐 Python: async def _backward(self, signals): pass
func (b *ToolOptimizerBase) Backward(_ context.Context, _ []*signal.EvolutionSignal) error {
	return nil
}

// Step 生成更新映射。对齐 Python 空实现。
//
// 对齐 Python: def _step(self): updates = {}; for operator in self.operators.items(): return
func (b *ToolOptimizerBase) Step() map[string]any {
	return map[string]any{}
}

// Bind 过滤并绑定可优化的 Operator，返回匹配数量。
func (b *ToolOptimizerBase) Bind(operators map[string]any, targets []string, config map[string]any) int {
	// ⤵️ 9.70: 等待 Trainer 实现后回填 Operator 类型转换
	return 0
}

// AddTrajectory 缓存 Trajectory 供 backward 阶段查询。
func (b *ToolOptimizerBase) AddTrajectory(traj *trajectory.Trajectory) {
	b.BaseOptimizerMixin.AddTrajectory(traj)
}

// GetTrajectories 返回当前缓存的轨迹列表（副本）。
func (b *ToolOptimizerBase) GetTrajectories() []*trajectory.Trajectory {
	return b.BaseOptimizerMixin.GetTrajectories()
}

// ClearTrajectories 清空轨迹缓存。
func (b *ToolOptimizerBase) ClearTrajectories() {
	b.BaseOptimizerMixin.ClearTrajectories()
}

// Parameters 返回梯度容器的副本。
func (b *ToolOptimizerBase) Parameters() map[string]*optimizer.TextualParameter {
	return b.BaseOptimizerMixin.Parameters()
}

// SelectSignals 选择此优化器可消费的信号。默认保留全部信号。
func (b *ToolOptimizerBase) SelectSignals(signals []*signal.EvolutionSignal) []*signal.EvolutionSignal {
	return b.BaseOptimizerMixin.SelectSignals(signals)
}

// WithMaxTurns 设置最大迭代轮数。
func WithMaxTurns(n int) ToolOptimizerBaseOption {
	return func(o *ToolOptimizerBase) { o.maxTurns = n }
}

// WithLLMAPIKey 设置 LLM API 密钥。
func WithLLMAPIKey(key string) ToolOptimizerBaseOption {
	return func(o *ToolOptimizerBase) { o.llmAPIKey = key }
}

// WithConfigEg 设置 Example Stage 配置。
func WithConfigEg(config map[string]any) ToolOptimizerBaseOption {
	return func(o *ToolOptimizerBase) { o.configEg = config }
}

// WithConfigDesc 设置 Description Stage 配置。
func WithConfigDesc(config map[string]any) ToolOptimizerBaseOption {
	return func(o *ToolOptimizerBase) { o.configDesc = config }
}

// WithPathSaveDir 设置结果保存目录。
func WithPathSaveDir(dir string) ToolOptimizerBaseOption {
	return func(o *ToolOptimizerBase) { o.pathSaveDir = dir }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractLastDescription 从 resultDescs 中提取最终描述字符串。
//
// 对齐 Python: output_desc = result_descs[-1][-1][-1]["description"]
func extractLastDescription(resultDescs [][][]any) string {
	if len(resultDescs) == 0 {
		return ""
	}
	lastBatch := resultDescs[len(resultDescs)-1]
	if len(lastBatch) == 0 {
		return ""
	}
	lastNode := lastBatch[len(lastBatch)-1]
	if len(lastNode) == 0 {
		return ""
	}
	lastStep, ok := lastNode[len(lastNode)-1].(map[string]any)
	if !ok {
		return ""
	}
	desc, _ := lastStep["description"].(string)
	return desc
}
