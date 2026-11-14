package artifactrsi

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi"
)

// newTestModel 构造测试用空 Model（仅占位，契约层不调用其方法）。
func newTestModel() *llm.Model { return &llm.Model{} }

func TestNewArtifactEngineRequest_访问器(t *testing.T) {
	path := "/tmp/program"
	instr := "把结论改清楚"
	proxy := "http://proxy:7890"
	req := NewArtifactEngineRequest("task-1", "/run/dir", &path, newTestModel(), 5, &instr, &proxy)
	if req.TaskID() != "task-1" || req.RunDir() != "/run/dir" {
		t.Fatalf("task_id/run_dir 不符: %s/%s", req.TaskID(), req.RunDir())
	}
	if !reflect.DeepEqual(req.ArtifactPath(), &path) || req.Model() == nil {
		t.Fatalf("artifact_path/model 不符")
	}
	if req.MaxIterations() != 5 || !reflect.DeepEqual(req.OptimizationInstruction(), &instr) || !reflect.DeepEqual(req.WebProxy(), &proxy) {
		t.Fatalf("max_iterations/optimization_instruction/web_proxy 不符")
	}
}

func TestArtifactRequestError_Error返回首条(t *testing.T) {
	err := &ArtifactRequestError{errors: []map[string]string{
		{"code": "A", "message": "a"}, {"code": "B", "message": "b"},
	}}
	if err.Error() != "A: a" {
		t.Fatalf("Error() = %q, want %q", err.Error(), "A: a")
	}
	var target *ArtifactRequestError
	if !errors.As(err, &target) || len(target.Errors()) != 2 {
		t.Fatalf("errors.As/Errors() 不符")
	}
}

// validProgram 返回一条能通过全部校验的 program 请求。
func validProgram() *rsi.RsiTaskCreateRequest {
	at := rsi.ArtifactTypeProgram
	return rsi.NewRsiTaskCreateRequest(
		rsi.RsiScenarioArtifact, &at, "任务名", strPtr("/tmp/main.py"),
		nil, nil, nil,
		map[string]string{"optimizer": "model-x"}, 3, nil,
	)
}

// validPaper 返回一条能通过全部校验的 paper 请求（无文件但有指令）。
func validPaper() *rsi.RsiTaskCreateRequest {
	at := rsi.ArtifactTypePaper
	return rsi.NewRsiTaskCreateRequest(
		rsi.RsiScenarioArtifact, &at, "任务名", nil,
		strPtr("加强第三章论证"), nil, nil,
		map[string]string{"optimizer": "model-x"}, 3, nil,
	)
}

func codes(v *rsi.ArtifactValidationResult) []string {
	var out []string
	for _, e := range v.Errors() {
		out = append(out, e["code"])
	}
	return out
}

func expectCodes(t *testing.T, v *rsi.ArtifactValidationResult, want ...string) {
	t.Helper()
	got := codes(v)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("codes = %v, want %v", got, want)
	}
}

func TestValidateArtifactTaskRequest_program通过(t *testing.T) {
	expectCodes(t, ValidateArtifactTaskRequest(validProgram()))
}

func TestValidateArtifactTaskRequest_paper通过(t *testing.T) {
	expectCodes(t, ValidateArtifactTaskRequest(validPaper()))
}

func TestValidateArtifactTaskRequest_逐条规则(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*rsi.RsiTaskCreateRequest)
		want []string
	}{
		{"scenario错误", func(r *rsi.RsiTaskCreateRequest) {
			*r = *rsi.NewRsiTaskCreateRequest("harness", r.ArtifactType(), r.Name(), r.ArtifactPath(), r.OptimizationInstruction(), r.DatasetFile(), r.SearchWidth(), r.ModelRefs(), r.MaxIterations(), r.WebProxy())
		}, []string{"ARTIFACT_SCENARIO_REQUIRED"}},
		{"artifact_type缺失", func(r *rsi.RsiTaskCreateRequest) {
			*r = *rsi.NewRsiTaskCreateRequest(rsi.RsiScenarioArtifact, nil, r.Name(), r.ArtifactPath(), r.OptimizationInstruction(), r.DatasetFile(), r.SearchWidth(), r.ModelRefs(), r.MaxIterations(), r.WebProxy())
		}, []string{"ARTIFACT_TYPE_REQUIRED"}},
		{"web_proxy非paper", func(r *rsi.RsiTaskCreateRequest) {
			*r = *rsi.NewRsiTaskCreateRequest(rsi.RsiScenarioArtifact, r.ArtifactType(), r.Name(), r.ArtifactPath(), r.OptimizationInstruction(), r.DatasetFile(), r.SearchWidth(), r.ModelRefs(), r.MaxIterations(), strPtr("http://p"))
		}, []string{"WEB_PROXY_UNSUPPORTED"}},
		{"name空白", func(r *rsi.RsiTaskCreateRequest) {
			*r = *rsi.NewRsiTaskCreateRequest(rsi.RsiScenarioArtifact, r.ArtifactType(), "  ", r.ArtifactPath(), r.OptimizationInstruction(), r.DatasetFile(), r.SearchWidth(), r.ModelRefs(), r.MaxIterations(), r.WebProxy())
		}, []string{"NAME_REQUIRED"}},
		{"model_refs缺失optimizer", func(r *rsi.RsiTaskCreateRequest) {
			*r = *rsi.NewRsiTaskCreateRequest(rsi.RsiScenarioArtifact, r.ArtifactType(), r.Name(), r.ArtifactPath(), r.OptimizationInstruction(), r.DatasetFile(), r.SearchWidth(), map[string]string{}, r.MaxIterations(), r.WebProxy())
		}, []string{"OPTIMIZER_MODEL_REQUIRED"}},
		{"max_iterations为零", func(r *rsi.RsiTaskCreateRequest) {
			*r = *rsi.NewRsiTaskCreateRequest(rsi.RsiScenarioArtifact, r.ArtifactType(), r.Name(), r.ArtifactPath(), r.OptimizationInstruction(), r.DatasetFile(), r.SearchWidth(), r.ModelRefs(), 0, r.WebProxy())
		}, []string{"MAX_ITERATIONS_INVALID"}},
		{"program缺路径", func(r *rsi.RsiTaskCreateRequest) {
			*r = *rsi.NewRsiTaskCreateRequest(rsi.RsiScenarioArtifact, r.ArtifactType(), r.Name(), nil, r.OptimizationInstruction(), r.DatasetFile(), r.SearchWidth(), r.ModelRefs(), r.MaxIterations(), r.WebProxy())
		}, []string{"PROGRAM_ARTIFACT_PATH_REQUIRED"}},
		{"program带指令", func(r *rsi.RsiTaskCreateRequest) {
			*r = *rsi.NewRsiTaskCreateRequest(rsi.RsiScenarioArtifact, r.ArtifactType(), r.Name(), r.ArtifactPath(), strPtr("不要写这个"), r.DatasetFile(), r.SearchWidth(), r.ModelRefs(), r.MaxIterations(), r.WebProxy())
		}, []string{"PROGRAM_INSTRUCTION_UNSUPPORTED"}},
		{"paper两者皆缺", func(r *rsi.RsiTaskCreateRequest) {
			*r = *rsi.NewRsiTaskCreateRequest(rsi.RsiScenarioArtifact, r.ArtifactType(), r.Name(), nil, nil, r.DatasetFile(), r.SearchWidth(), r.ModelRefs(), r.MaxIterations(), r.WebProxy())
		}, []string{"PAPER_INPUT_REQUIRED"}},
		{"dataset_file不支持", func(r *rsi.RsiTaskCreateRequest) {
			*r = *rsi.NewRsiTaskCreateRequest(rsi.RsiScenarioArtifact, r.ArtifactType(), r.Name(), r.ArtifactPath(), r.OptimizationInstruction(), strPtr("data.csv"), r.SearchWidth(), r.ModelRefs(), r.MaxIterations(), r.WebProxy())
		}, []string{"DATASET_FILE_UNSUPPORTED"}},
		{"search_width不支持", func(r *rsi.RsiTaskCreateRequest) {
			*r = *rsi.NewRsiTaskCreateRequest(rsi.RsiScenarioArtifact, r.ArtifactType(), r.Name(), r.ArtifactPath(), r.OptimizationInstruction(), r.DatasetFile(), func() *int { i := 3; return &i }(), r.ModelRefs(), r.MaxIterations(), r.WebProxy())
		}, []string{"SEARCH_WIDTH_UNSUPPORTED"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validProgram()
			if tc.name == "paper两者皆缺" {
				req = validPaper()
			}
			tc.mut(req)
			expectCodes(t, ValidateArtifactTaskRequest(req), tc.want...)
		})
	}
}

func TestBuildRequest_program字段置nil(t *testing.T) {
	at := rsi.ArtifactTypeProgram
	env := rsi.NewRsiTaskEnvelope("t-1", "/run", &at, validProgram())
	req, err := BuildRequest(env, ValidateArtifactTaskRequest(env.Config()), newTestModel())
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if req.TaskID() != "t-1" || req.RunDir() != "/run" || req.Model() == nil {
		t.Fatalf("基础字段不符")
	}
	if req.OptimizationInstruction() != nil || req.WebProxy() != nil {
		t.Fatalf("program 工单 instruction/web_proxy 必须为 nil")
	}
	if !reflect.DeepEqual(req.ArtifactPath(), strPtr("/tmp/main.py")) || req.MaxIterations() != 3 {
		t.Fatalf("artifact_path/max_iterations 不符")
	}
}

func TestBuildRequest_paper保留instruction与proxy(t *testing.T) {
	at := rsi.ArtifactTypePaper
	env2 := rsi.NewRsiTaskEnvelope("t-2", "/run", &at, rsi.NewRsiTaskCreateRequest(
		rsi.RsiScenarioArtifact, &at, "任务名", nil,
		strPtr("加强论证"), nil, nil,
		map[string]string{"optimizer": "m"}, 3, strPtr("  http://p:1  "),
	))
	req, err := BuildRequest(env2, ValidateArtifactTaskRequest(env2.Config()), newTestModel())
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if req.OptimizationInstruction() == nil || req.WebProxy() == nil || *req.WebProxy() != "http://p:1" {
		t.Fatalf("paper 工单 instruction/web_proxy 不符（proxy 须 strip）")
	}
}

func TestBuildRequest_拒绝路径(t *testing.T) {
	at := rsi.ArtifactTypeProgram
	// 校验失败
	badEnv := rsi.NewRsiTaskEnvelope("t", "/run", &at, rsi.NewRsiTaskCreateRequest(
		rsi.RsiScenarioArtifact, &at, "", strPtr("/p"), nil, nil, nil,
		map[string]string{"optimizer": "m"}, 1, nil,
	))
	if _, err := BuildRequest(badEnv, ValidateArtifactTaskRequest(badEnv.Config()), newTestModel()); err == nil {
		t.Fatalf("校验失败须报错")
	}
	// TYPE_MISMATCH：envelope 与 config 的 artifact_type 不一致
	paper := rsi.ArtifactTypePaper
	mismatchEnv := rsi.NewRsiTaskEnvelope("t", "/run", &paper, validProgram())
	if _, err := BuildRequest(mismatchEnv, ValidateArtifactTaskRequest(mismatchEnv.Config()), newTestModel()); err == nil || !strings.Contains(err.Error(), "ARTIFACT_TYPE_MISMATCH") {
		t.Fatalf("须报 ARTIFACT_TYPE_MISMATCH, got %v", err)
	}
	// model 为 nil
	env := rsi.NewRsiTaskEnvelope("t", "/run", &at, validProgram())
	if _, err := BuildRequest(env, ValidateArtifactTaskRequest(env.Config()), nil); err == nil || !strings.Contains(err.Error(), "OPTIMIZER_MODEL_INSTANCE_REQUIRED") {
		t.Fatalf("须报 OPTIMIZER_MODEL_INSTANCE_REQUIRED, got %v", err)
	}
}

// ──────────────────────────── 接口编译断言 ────────────────────────────

// fakeProvider 用于编译期验证 ArtifactProvider 接口可被实现。
type fakeProvider struct{}

func (f *fakeProvider) ArtifactType() rsi.ArtifactType { return rsi.ArtifactTypeProgram }

func (f *fakeProvider) ValidateInput(artifactPath *string) *rsi.ArtifactValidationResult {
	return rsi.NewArtifactValidationResult(true, nil)
}

func (f *fakeProvider) Run(ctx context.Context, req *ArtifactEngineRequest, onEvent rsi.OnEvent) (*rsi.EngineResult, error) {
	return nil, nil
}

func (f *fakeProvider) Pause(ctx context.Context, taskID string, onEvent rsi.OnEvent) (*rsi.EngineResult, error) {
	return nil, nil
}

func (f *fakeProvider) Resume(ctx context.Context, req *ArtifactEngineRequest, onEvent rsi.OnEvent) (*rsi.EngineResult, error) {
	return nil, nil
}

func (f *fakeProvider) ReadState(taskID string) *rsi.EngineState   { return nil }
func (f *fakeProvider) ReadReport(taskID string) *rsi.EngineReport { return nil }
func (f *fakeProvider) GetTree(taskID string) *rsi.TreeResponse    { return nil }

func (f *fakeProvider) LocateArtifact(taskID string, artifactID *string) *rsi.ArtifactRef {
	return nil
}

func (f *fakeProvider) Terminate(ctx context.Context, taskID string, onEvent rsi.OnEvent) (*rsi.EngineResult, error) {
	return nil, nil
}

// 编译期断言：fakeProvider 满足 ArtifactProvider。
var _ ArtifactProvider = (*fakeProvider)(nil)

func TestFakeProvider_满足接口(t *testing.T) {
	var p ArtifactProvider = &fakeProvider{}
	if p.ArtifactType() != rsi.ArtifactTypeProgram {
		t.Fatal("ArtifactType 不符")
	}
	if !p.ValidateInput(nil).Valid() {
		t.Fatal("ValidateInput 不符")
	}
}
