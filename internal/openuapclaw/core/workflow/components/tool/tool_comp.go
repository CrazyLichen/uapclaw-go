package tool

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool/service_api"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner"
	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
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

// ──────────────────────────── 结构体（续）────────────────────────────

// ToolExecutable 工具组件执行体（Python ToolExecutable :43-109 一比一）。
// 挂接方式（spec 拍板 #2 槽优先）：复用 8.17 WorkflowComponent 槽位模式，
// invoke 逻辑经 InvokeFn 槽接入（8.18 LLMExecutable 同款遮蔽模式）。
type ToolExecutable struct {
	// WorkflowComponent 8.17 组件基座（四槽 + 组合面）
	components.WorkflowComponent
	// cfg 组件配置（Python self._config）
	cfg *ToolComponentConfig
	// toolObj 已绑定工具（Python self._tool: Union[Tool, None]；字段名避开接口名）
	toolObj tool.Tool

	// baseInvoke 基座钩子引用（方法遮蔽后转调用；D-17-1 同款遮蔽模式）
	baseInvoke func(ctx context.Context, inputs map[string]any, sess session.BaseSession, kwargs map[string]any) (map[string]any, error)
}

// ──────────────────────────── 导出函数（续）────────────────────────────

// NewToolExecutable 构造执行体（Python __init__ :45-48 一比一：装配 config，
// tool 为 None）
func NewToolExecutable(componentConfig *ToolComponentConfig) *ToolExecutable {
	e := &ToolExecutable{cfg: componentConfig}
	// 保存基座钩子（方法遮蔽后转调）+ 四槽装配：仅 invoke 槽
	//（Python ToolExecutable 无 stream/collect/transform）
	e.baseInvoke = e.WorkflowComponent.OnInvoke
	e.InvokeFn = e.invoke
	return e
}

// SetTool 绑定工具（Python set_tool :83-85 一比一，自返回）
func (e *ToolExecutable) SetTool(t tool.Tool) *ToolExecutable {
	e.toolObj = t
	return e
}

// invoke 执行链（Python invoke :61-81 五步一比一，顺序不改）：
// ① tool nil → 102000；② 校验输入；③ tool.Invoke(skip_inputs_validate=False,
// skip_none_value=True)；④ 错误吞没（BaseError 取 code/message，否则 182012
// msg 格式化 card+reason）；⑤ postProcess → createOutput
func (e *ToolExecutable) invoke(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade) (map[string]any, error) {
	// ① tool 未初始化
	if e.toolObj == nil {
		return nil, exception.BuildError(
			exception.StatusComponentToolExecutionError,
			exception.WithParam("error_msg", "tool is not initialized"),
		)
	}
	// ② 输入校验（Python extra='allow' 弱校验，D-21-3）
	toolInputs, err := validateToolInputs(inputs)
	if err != nil {
		return nil, err
	}
	// ③ 调工具（skip_inputs_validate=False, skip_none_value=True 一比一）
	var response map[string]any
	raw, invokeErr := e.toolObj.Invoke(ctx, toolInputs,
		tool.WithSkipInputsValidate(false), tool.WithSkipNoneValue(true))
	if invokeErr != nil {
		// ④ 错误吞没（Python except Exception :72-80 一比一）
		var be *exception.BaseError
		if errors.As(invokeErr, &be) {
			response = map[string]any{errMessageKey: be.Message(), errCodeKey: be.Code()}
		} else {
			reason := invokeErr.Error()
			if reason == "" {
				reason = "unknown exception"
			}
			response = map[string]any{
				errMessageKey: fmt.Sprintf("tool execution error, too card=%v, reason=%s", e.toolObj.Card(), reason),
				errCodeKey:    exception.StatusToolExecutionError.Code(),
			}
		}
	} else {
		// ④' 正常路径同样过 postProcess（Python try 内 :70-71）
		response = postProcessToolResult(e.toolObj, raw)
	}
	// ⑤ 输出信封
	return createOutput(response), nil
}

// postProcessToolResult 结果整形（Python _post_process_tool_result :90-109
// 三分支一比一，顺序不改）
func postProcessToolResult(t tool.Tool, toolResult map[string]any) map[string]any {
	result := make(map[string]any)
	// 分支 1：RestfulApi（Python isinstance(self._tool, RestfulApi)）
	if _, ok := t.(*service_api.RestfulApi); ok {
		result[restfulDataKey] = mapDefault(toolResult, "data", "")
		codeInt := toInt(mapDefault(toolResult, "code", defaultExceptionErrorCode))
		if codeInt == nil {
			codeInt = ptrInt(defaultExceptionErrorCode)
		}
		if 200 <= *codeInt && *codeInt < 300 {
			result[errCodeKey] = exception.StatusSuccess.Code()
		} else {
			result[errCodeKey] = exception.StatusToolExecutionError.Code()
		}
		result[errMessageKey] = mapDefault(toolResult, "message", "")
		return result
	}
	// 分支 2：普通 Tool 且结果含 code/data/message 三键（Python :103-107）
	if _, hasCode := toolResult["code"]; hasCode {
		if _, hasData := toolResult["data"]; hasData {
			if _, hasMessage := toolResult["message"]; hasMessage {
				result[restfulDataKey] = mapDefault(toolResult, "data", "")
				result[errCodeKey] = mapDefault(toolResult, "code", exception.StatusToolExecutionError.Code())
				result[errMessageKey] = mapDefault(toolResult, "message", "")
				return result
			}
		}
	}
	// 分支 3：其余——整个结果塞 data（Python :108）
	result[restfulDataKey] = toolResult
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mapDefault 取键缺省（Python dict.get(k, default) 等价）
func mapDefault(m map[string]any, key string, def any) any {
	if v, ok := m[key]; ok {
		return v
	}
	return def
}

// toInt 任意数值/数字字符串 → int（Python int(code) 宽转换；失败 nil）
func toInt(v any) *int {
	switch x := v.(type) {
	case int:
		return &x
	case int64:
		i := int(x)
		return &i
	case float64:
		i := int(x)
		return &i
	case string:
		if i, err := strconv.Atoi(x); err == nil {
			return &i
		}
	}
	return nil
}

// ptrInt 取指针
func ptrInt(i int) *int { return &i }

// ──────────────────────────── 结构体（续）────────────────────────────

// ToolComponent 工具组件组合面（Python ToolComponent :112-132 一比一）
type ToolComponent struct {
	// BaseComponentComposable 组合面基座（Python ComponentComposable mixin 嵌入）
	components.BaseComponentComposable
	// cfg 组件配置（Python self._config）
	cfg *ToolComponentConfig
	// toolObj 已解析工具（Python self._tool；方案 A' 直调无注入缝，D-21-2）
	toolObj tool.Tool
}

// ──────────────────────────── 导出函数（续）────────────────────────────

// NewToolComponent 构造组件（Python __init__ :114-120 一比一：tool_id 非空则
// 构造期直调 Runner.resource_mgr.get_tool ≙ Go 全局单例 GetResourceMgr().GetTool，
// 方案 A'；查不到 → 空 slice → toolObj 保持 nil，等 to_executable 102002）
func NewToolComponent(componentConfig *ToolComponentConfig) *ToolComponent {
	c := &ToolComponent{cfg: componentConfig}
	if componentConfig.ToolID != nil {
		tools, err := runner.GetResourceMgr().GetTool([]string{*componentConfig.ToolID})
		if err == nil && len(tools) > 0 {
			c.toolObj = tools[0]
		}
	}
	// ToExecutableFn 槽注入（spec 拍板 #2 槽优先单点，102002 硬错承载于此）
	c.ToExecutableFn = c.toExecutable
	return c
}

// toExecutable 可执行体供应槽（Python to_executable :122-128 一比一：
// tool None → 102002 硬错；否则 ToolExecutable(config).set_tool(tool)）
func (c *ToolComponent) toExecutable() graphexe.GeneralExecutor {
	if c.toolObj == nil {
		panic(exception.BuildError(
			exception.StatusComponentToolInitFailed,
			exception.WithParam("error_msg", "tool component not bind a valid tool"),
		))
	}
	return NewToolExecutable(c.cfg).SetTool(c.toolObj)
}

// BindTool 编程式绑定工具（Python bind_tool :130-132 一比一，自返回）
func (c *ToolComponent) BindTool(t tool.Tool) *ToolComponent {
	c.toolObj = t
	return c
}
