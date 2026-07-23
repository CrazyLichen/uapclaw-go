package tool_call

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// invalidStageError 无效阶段错误。
type invalidStageError struct {
	stage string
}

// pipelineError 流水线错误。
type pipelineError struct {
	// msg 错误消息
	msg string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// errFnCallPathNotImplemented fn_call_path 未实现错误
	errFnCallPathNotImplemented = &pipelineError{msg: "基于配置的 API 包装器尚未实现"}
	// errToolCallableRequired 缺少 tool_callable 错误
	errToolCallableRequired = &pipelineError{msg: "必须提供 config 或 tool_callable。"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CustomizedPipeline 运行优化流水线。
// 根据阶段（example/description）选择对应方法，创建 BeamSearch 执行搜索。
//
// 对齐 Python: customized_pipeline(stage, tool, config, tool_callable=None)
//
//  1. 创建 SimpleAPIWrapperFromCallable
//  2. 创建 SimpleEval
//  3. 根据 stage 创建 APICallToExampleMethod 或 ToolDescriptionMethod
//  4. 创建 BeamSearch 并调用 Search
//  5. 保存结果到 JSON 文件
func CustomizedPipeline(
	ctx context.Context,
	stage string,
	tool map[string]any,
	config map[string]any,
	toolCallable APIWrapperFunc,
	model *llm.Model,
) ([][]any, error) {
	// 对齐 Python: if "fn_call_path" in config: raise NotImplementedError
	if _, ok := config["fn_call_path"]; ok {
		return nil, errFnCallPathNotImplemented
	}

	// 对齐 Python: elif tool_callable is not None:
	var callAPIFn APIWrapperFunc
	if toolCallable != nil {
		toolName := getToolName(tool)
		callAPIFn = NewSimpleAPIWrapperFromCallable(toolCallable, toolName).Call
	} else {
		// 对齐 Python: else: raise ValueError("Either config or tool_callable must be provided.")
		return nil, errToolCallableRequired
	}

	// 对齐 Python: eval_fn = SimpleEval(api_wrapper=call_api_fn, config=config)
	evalFn := NewSimpleEval(callAPIFn, config, 0.4, 0.6, model)

	var method BeamSearchMethod

	// 对齐 Python: if stage == "example":
	switch stage {
	case "example":
		// 对齐 Python: method = APICallToExampleMethod(config, call_api_fn, eval_fn, api_keys=None, non_opt_params=[])
		method = NewAPICallToExampleMethod(config, model, callAPIFn, evalFn, nil, nil)
	case "description":
		// 对齐 Python: elif stage == "description": method = ToolDescriptionMethod(config, eval_fn)
		method = NewToolDescriptionMethod(config, model, evalFn)
	default:
		// 对齐 Python: else: raise ValueError(f"wrong stage: {stage}")
		return nil, &invalidStageError{stage: stage}
	}

	logger.Info(logComponent).
		Str("method", "CustomizedPipeline").
		Str("stage", stage).
		Msg("=== Starting SingleRoundSearch ===")

	// 对齐 Python: single_search = BeamSearch(method=method, beam_width=..., expand_num=..., ...)
	search := NewBeamSearch(method,
		WithBeamWidth(getConfigInt(config, "beam_width")),
		WithExpandNum(getConfigInt(config, "expand_num")),
		WithMaxDepth(getConfigInt(config, "max_depth")),
		WithNumWorkers(getConfigInt(config, "num_workers")),
		WithVerbose(getConfigInt(config, "verbose") > 0),
		WithEarlyStop(true),
		WithCheckValid(true),
		WithMaxScore(3.0),
		WithTopK(getConfigInt(config, "top_k")),
	)

	// 对齐 Python: result = single_search.search(tool)
	result, err := search.Search(ctx, tool)
	if err != nil {
		return nil, err
	}

	// 对齐 Python: save results
	if saveDir, ok := config["save_dir"].(string); ok && saveDir != "" {
		toolName := getToolName(tool)
		saveFilename := toolName + ".json"
		savePath := filepath.Join(saveDir, saveFilename)

		// 对齐 Python: os.makedirs(config["save_dir"], exist_ok=True)
		if mkdirErr := os.MkdirAll(saveDir, 0o755); mkdirErr != nil {
			logger.Warn(logComponent).
				Str("method", "CustomizedPipeline").
				Str("save_dir", saveDir).
				Err(mkdirErr).
				Msg("创建保存目录失败")
		} else {
			// 对齐 Python: if Path(save_path).exists(): merge old results
			mergedResult := result
			if existingData, readErr := os.ReadFile(savePath); readErr == nil {
				var oldResult [][]any
				if jsonErr := json.Unmarshal(existingData, &oldResult); jsonErr == nil {
					// 对齐 Python: result = json.load(f) + result
					mergedResult = append(oldResult, result...)
				}
			}

			// 对齐 Python: json.dump(result, f, indent=2, ensure_ascii=False)
			data, jsonErr := json.MarshalIndent(mergedResult, "", "  ")
			if jsonErr != nil {
				logger.Warn(logComponent).
					Str("method", "CustomizedPipeline").
					Str("save_path", savePath).
					Err(jsonErr).
					Msg("序列化结果失败")
			} else if writeErr := os.WriteFile(savePath, data, 0o644); writeErr != nil {
				logger.Warn(logComponent).
					Str("method", "CustomizedPipeline").
					Str("save_path", savePath).
					Err(writeErr).
					Msg("保存结果失败")
			}
		}
	}

	return result, nil
}

// Error 返回无效阶段错误消息。
func (e *invalidStageError) Error() string {
	return "无效阶段: " + e.stage
}

// Error 返回流水线错误消息。
func (e *pipelineError) Error() string {
	return e.msg
}

// ──────────────────────────── 非导出函数 ────────────────────────────
