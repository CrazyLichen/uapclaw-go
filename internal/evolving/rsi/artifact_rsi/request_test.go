package artifactrsi

import (
	"errors"
	"reflect"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
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
