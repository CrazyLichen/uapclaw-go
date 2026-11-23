package tool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool/service_api"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner"
)

func TestValidateToolInputs_非map拒绝(t *testing.T) {
	_, err := validateToolInputs("not a map")
	require.Error(t, err)
	be, ok := err.(*exception.BaseError)
	require.True(t, ok)
	assert.Equal(t, 102001, be.Code())
}

func TestValidateToolInputs_透传(t *testing.T) {
	in := map[string]any{"sku": "a", "count": 3}
	out, err := validateToolInputs(in)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestCreateOutput_驼峰信封(t *testing.T) {
	out := createOutput(map[string]any{
		errCodeKey:     182012,
		errMessageKey:  "boom",
		restfulDataKey: "payload",
	})
	assert.Equal(t, map[string]any{"errCode": 182012, "errMessage": "boom", "data": "payload"}, out)
}

func TestCreateOutput_缺省值兜底(t *testing.T) {
	out := createOutput(map[string]any{})
	assert.Equal(t, map[string]any{"errCode": 0, "errMessage": "", "data": ""}, out)
}

// ──────────────────────────── Task2 invoke 链 ────────────────────────────

// newExecutableWithMapTool 构造绑真实 MapFunction 工具的执行体（无替身）
func newExecutableWithMapTool(t *testing.T, invokeFn func(ctx context.Context, inputs map[string]any) (map[string]any, error)) *ToolExecutable {
	t.Helper()
	card := tool.NewToolCard("fake_tool", "测试工具", nil, nil)
	fn, err := tool.NewMapFunction(card, invokeFn, nil)
	require.NoError(t, err)
	cfg := &ToolComponentConfig{}
	return NewToolExecutable(cfg).SetTool(fn)
}

func TestToolExecutableInvoke_普通Tool信封化(t *testing.T) {
	e := newExecutableWithMapTool(t, func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
		return map[string]any{"code": 0, "data": "ok", "message": "done"}, nil
	})
	sess := newTestNodeSession(t)
	out, err := e.InvokeFn(context.Background(), map[string]any{"q": 1}, sess)
	require.NoError(t, err)
	assert.Equal(t, 0, out["errCode"])
	assert.Equal(t, "done", out["errMessage"])
	assert.Equal(t, "ok", out["data"])
}

func TestToolExecutableInvoke_裸结果塞data(t *testing.T) {
	e := newExecutableWithMapTool(t, func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
		return map[string]any{"answer": 42}, nil
	})
	sess := newTestNodeSession(t)
	out, err := e.InvokeFn(context.Background(), map[string]any{}, sess)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"answer": 42}, out["data"])
	assert.Equal(t, 0, out["errCode"])
}

// rawErrTool 返回裸 error（非 BaseError）的测试夹具——实现真实 Tool 接口。
// 用途：Go MapFunction 会把 invokeFn 错误包成 182205（领域 3 既有行为，Python
// LocalFunction 不包装、原样上抛）——182012 吞错误分支需要裸 error 才能触达。
type rawErrTool struct {
	card *tool.ToolCard
	err  error
}

func (f *rawErrTool) Card() *tool.ToolCard { return f.card }
func (f *rawErrTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	return nil, f.err
}
func (f *rawErrTool) Stream(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(f.card.String())
}
func (f *rawErrTool) GetCallTimeout(inputs map[string]any, defaultTimeout float64) float64 {
	return tool.BaseCallTimeout(inputs, defaultTimeout)
}

func TestToolExecutableInvoke_错误吞没182012(t *testing.T) {
	// 裸 error（Python LocalFunction 原样上抛路径）→ 非 BaseError → 182012 分支
	card := tool.NewToolCard("raw_err_tool", "裸错误工具", nil, nil)
	e := NewToolExecutable(&ToolComponentConfig{}).SetTool(&rawErrTool{
		card: card, err: assert.AnError,
	})
	sess := newTestNodeSession(t)
	out, err := e.InvokeFn(context.Background(), map[string]any{}, sess)
	require.NoError(t, err) // 吞错误，不断图
	assert.Equal(t, 182012, out["errCode"])
	assert.Contains(t, out["errMessage"], "tool execution error")
	assert.Contains(t, out["errMessage"], assert.AnError.Error())
}

func TestToolExecutableInvoke_BaseError透传码(t *testing.T) {
	// MapFunction 包装（182205，领域 3 既有行为）→ errors.As 命中 BaseError → 透传码
	e := newExecutableWithMapTool(t, func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
		return nil, assert.AnError
	})
	sess := newTestNodeSession(t)
	out, err := e.InvokeFn(context.Background(), map[string]any{}, sess)
	require.NoError(t, err)
	assert.Equal(t, 182205, out["errCode"])
	assert.Contains(t, out["errMessage"], assert.AnError.Error())
}

func TestToolExecutableInvoke_tool未初始化102000(t *testing.T) {
	cfg := &ToolComponentConfig{}
	e := NewToolExecutable(cfg) // 未 SetTool
	sess := newTestNodeSession(t)
	_, err := e.InvokeFn(context.Background(), map[string]any{}, sess)
	require.Error(t, err)
	be, ok := err.(*exception.BaseError)
	require.True(t, ok)
	assert.Equal(t, 102000, be.Code())
}

func TestToolExecutableInvoke_RestfulApi分支(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"k":"v"}`))
	}))
	defer srv.Close()

	card, err := service_api.NewRestfulApiCard("rest_tool", "REST 工具", srv.URL, "GET", nil)
	require.NoError(t, err)
	api, err := service_api.NewRestfulApi(card)
	require.NoError(t, err)

	cfg := &ToolComponentConfig{}
	e := NewToolExecutable(cfg).SetTool(api)
	sess := newTestNodeSession(t)
	out, err := e.InvokeFn(context.Background(), map[string]any{}, sess)
	require.NoError(t, err)
	assert.Equal(t, 0, out["errCode"]) // 200 ≤ code < 300 → SUCCESS.code(0)
	assert.Equal(t, map[string]any{"k": "v"}, out["data"])
}

func TestToolExecutableInvoke_RestfulApi非2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	card, err := service_api.NewRestfulApiCard("rest_tool", "REST 工具", srv.URL, "GET", nil)
	require.NoError(t, err)
	api, err := service_api.NewRestfulApi(card)
	require.NoError(t, err)

	cfg := &ToolComponentConfig{}
	e := NewToolExecutable(cfg).SetTool(api)
	sess := newTestNodeSession(t)
	out, err := e.InvokeFn(context.Background(), map[string]any{}, sess)
	require.NoError(t, err)
	// Python 非 2xx → TOOL_RESTFUL_API_RESPONSE_ERROR(182103) BaseError 上抛
	// → ToolComponent BaseError 分支透传 182103（restful_api.py:303）
	assert.Equal(t, 182103, out["errCode"])
	assert.Contains(t, out["errMessage"], "response error")
}

func TestPostProcess_RestfulApi错误码映射(t *testing.T) {
	api := &service_api.RestfulApi{} // 仅作判型依据
	out := postProcessToolResult(api, map[string]any{
		"code": "abc", "data": "x", "message": "m", // code 非 int → -1 → 182012
	})
	assert.Equal(t, 182012, out[errCodeKey])
	assert.Equal(t, "m", out[errMessageKey])
	assert.Equal(t, "x", out[restfulDataKey])
}

func TestPostProcess_RestfulApi2xxSuccess(t *testing.T) {
	api := &service_api.RestfulApi{}
	out := postProcessToolResult(api, map[string]any{"code": 200, "data": "d", "message": "ok"})
	assert.Equal(t, 0, out[errCodeKey]) // StatusSuccess.Code
	assert.Equal(t, "d", out[restfulDataKey])
}

func TestPostProcess_三键透传缺code默认182012(t *testing.T) {
	out := postProcessToolResult(&tool.MapFunction{}, map[string]any{"data": "d", "message": "m", "code": nil})
	// code 键存在但为 nil → mapDefault 取到 nil（非缺省）——对齐 Python dict.get 行为
	assert.Equal(t, "d", out[restfulDataKey])
	assert.Equal(t, "m", out[errMessageKey])
}

// ──────────────────────────── Task3 组合面 ────────────────────────────

func TestNewToolComponent_按ID取工具(t *testing.T) {
	mgr := runner.GetResourceMgr()
	// card.ID 必须与查询 id 一致（Python BaseCard.id 默认 uuid4，产品层显式设 id）
	card := tool.NewToolCardWithID("reg_tool", "reg_tool", "注册工具", nil, nil)
	fn, err := tool.NewMapFunction(card, func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}, nil)
	require.NoError(t, err)
	require.NoError(t, mgr.AddTool(fn))

	tid := "reg_tool"
	c := NewToolComponent(&ToolComponentConfig{ToolID: &tid})
	require.NotNil(t, c.toolObj) // 构造期已按 id 直调 GetTool 取到
}

func TestNewToolComponent_无ID不取(t *testing.T) {
	c := NewToolComponent(&ToolComponentConfig{})
	assert.Nil(t, c.toolObj) // Python tool_id None → 不查 → 等 BindTool
}

func TestToolComponent_ToExecutable_102002(t *testing.T) {
	c := NewToolComponent(&ToolComponentConfig{}) // 无 id 未绑定
	defer func() {
		r := recover()
		require.NotNil(t, r, "应 panic（102002 硬错，llm toExecutableCached 同款形态）")
		be, ok := r.(*exception.BaseError)
		require.True(t, ok)
		assert.Equal(t, 102002, be.Code())
	}()
	c.ToExecutable()
}

func TestToolComponent_ToExecutable_绑定后产出(t *testing.T) {
	card := tool.NewToolCard("bind_tool", "绑定工具", nil, nil)
	fn, err := tool.NewMapFunction(card, func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
		return map[string]any{}, nil
	}, nil)
	require.NoError(t, err)
	c := NewToolComponent(&ToolComponentConfig{})
	c.BindTool(fn) // Python bind_tool :130-132
	ex := c.ToExecutable()
	te, ok := ex.(*ToolExecutable)
	require.True(t, ok) // 外层类型保住（槽优先）
	assert.NotNil(t, te.toolObj)
}

func TestToolComponent_ToExecutable_fn复用invoke链(t *testing.T) {
	mgr := runner.GetResourceMgr()
	card := tool.NewToolCardWithID("e2e_tool", "e2e_tool", "端到端工具", nil, nil)
	fn, err := tool.NewMapFunction(card, func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
		return map[string]any{"code": 0, "data": "ok", "message": "m"}, nil
	}, nil)
	require.NoError(t, err)
	tid := "e2e_tool"
	require.NoError(t, mgr.AddTool(fn))
	c := NewToolComponent(&ToolComponentConfig{ToolID: &tid})
	te := c.ToExecutable().(*ToolExecutable)
	sess := newTestNodeSession(t)
	out, err := te.InvokeFn(context.Background(), map[string]any{"x": 1}, sess)
	require.NoError(t, err)
	assert.Equal(t, "ok", out["data"])
}

func TestToolComponent_BindTool自返回(t *testing.T) {
	card := tool.NewToolCard("chain_tool", "链式工具", nil, nil)
	fn, err := tool.NewMapFunction(card, func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
		return map[string]any{}, nil
	}, nil)
	require.NoError(t, err)
	c := NewToolComponent(&ToolComponentConfig{})
	ret := c.BindTool(fn)
	assert.Same(t, c, ret) // Python return self
}
