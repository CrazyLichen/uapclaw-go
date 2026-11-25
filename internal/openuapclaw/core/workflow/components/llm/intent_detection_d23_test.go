package llm

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/config"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	condition "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/condition"
)

// TestD23_2_表达式缺键实证 spec D-23-2：缺键 → GetGlobal nil → var 比较 false
// （Python get_global 缺键 None → None == 1 → False 同语义）
func TestD23_2_表达式缺键实证(t *testing.T) {
	cond, err := condition.NewExpressionCondition("${classificationId} == 1")
	if err != nil {
		t.Fatal(err)
	}
	inner := &fakeInnerSession{cfg: config.NewSessionConfig(context.Background()), id: "d23"}
	sess := session.NewWorkflowVertexSession(inner, "d23node", "test", false)
	ok, evalErr := cond.Invoke(context.Background(), nil, sess)
	if evalErr != nil {
		t.Fatalf("缺键求值不应报错: %v", evalErr)
	}
	if ok {
		t.Fatal("缺键应求值 false（Python None == 1 → False）")
	}
	// staged 不可见（8.20 同款语义实证）：UpdateGlobal 只进 updates，GetGlobal 不可见
	sess.State().UpdateGlobal(map[string]any{"classificationId": 1})
	okStaged, _ := cond.Invoke(context.Background(), nil, sess)
	if okStaged {
		t.Fatal("staged 未 commit 应不可见（false）")
	}
	// commit 后可见 → true
	if cs, ok := sess.State().(interface{ Commit() }); ok {
		cs.Commit()
	} else {
		t.Fatalf("State 不支持 Commit: %T", sess.State())
	}
	ok2, _ := cond.Invoke(context.Background(), nil, sess)
	if !ok2 {
		t.Fatal("commit 后键可见且命中应求值 true")
	}
}
