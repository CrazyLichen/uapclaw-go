package tool

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolComponentConfig 工具组件配置（Python ToolComponentConfig :22-24 一比一）
type ToolComponentConfig struct {
	// components.ComponentConfig 组件基座配置（Python dataclass 继承）
	components.ComponentConfig
	// ToolID 工具标识（Python tool_id: Optional[str]；nil ≙ None，等 BindTool 绑定）
	ToolID *string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultExceptionErrorCode 非 RestfulApi code 整数化失败回退值
	// （Python DEFAULT_EXCEPTION_ERROR_CODE :19）
	defaultExceptionErrorCode = -1

	// errCodeKey / errMessageKey / restfulDataKey RestFul Res 信封键
	// （Python ERR_CODE/ERR_MESSAGE/RESTFUL_DATA :32-34；出驼峰为偏差 D-21-1）
	errCodeKey     = "errCode"
	errMessageKey  = "errMessage"
	restfulDataKey = "data"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// validateToolInputs 输入校验（Python ToolComponentInput extra='allow' +
// _validate_inputs :50-59 一比一；pydantic 零字段定义 ≙ 只要求 dict 形状，
// 偏差 D-21-3——非 map → 102001；map 原样透传）
func validateToolInputs(inputs any) (map[string]any, error) {
	m, ok := inputs.(map[string]any)
	if !ok {
		return nil, exception.BuildError(
			exception.StatusComponentToolInputParamError,
			exception.WithParam("error_msg",
				fmt.Sprintf("tool_input must be an object, got %T", inputs)),
		)
	}
	return m, nil
}

// createOutput 信封整形（Python _create_output :87-88 一比一；出键恒驼峰，
// 偏差 D-21-1：Python model_dump 出蛇形，Go 统一出驼峰）
func createOutput(response map[string]any) map[string]any {
	out := map[string]any{
		errCodeKey:     0,
		errMessageKey:  "",
		restfulDataKey: "",
	}
	if v, ok := response[errCodeKey]; ok {
		out[errCodeKey] = v
	}
	if v, ok := response[errMessageKey]; ok {
		out[errMessageKey] = v
	}
	if v, ok := response[restfulDataKey]; ok {
		out[restfulDataKey] = v
	}
	return out
}
