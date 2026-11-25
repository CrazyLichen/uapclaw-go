//go:build integration && llm

// 8.24 交互提问组件真链冒烟（复用 27.4 内置智谱免费模型域；用户拍板
// 2026-11-24：WithQuestionerLLM 注入缝之外的 LLM 网络边界真实验证）。
//
// 双守卫（真实外部服务）：
//  1. integration && llm 标签
//  2. ZHIPU_BUILTIN_API_KEY 未注入（且内置常量为空）→ Skip
//
// 运行方式：
//
//	go test -tags "sqlite_fts5 test integration llm" \
//	  ./tests/integration/agentcore/workflow_questioner/ -run TestQuestionerSuite_RealLLM -count=1 -v
//
// 断言纪律（17.x 真链先例同款）：只钉确定性结构契约（状态机 INPUT_REQUIRED→
// 恢复→终态、INTERACTION 帧存在、输出字段形状），不断言具体抽取值——与模型
// 行为无关。
package workflow_questioner

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/workflowbase"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/flow"
	llmcomp "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/llm"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/control/configpanel"
)

// QuestionerRealLLMSuite 8.24 真链冒烟：内置智谱免费模型走完整
// 抽取→追问挂起→InteractiveInput 恢复→终态链路。
type QuestionerRealLLMSuite struct {
	suite.Suite
}

func TestQuestionerSuite_RealLLM(t *testing.T) {
	suite.Run(t, new(QuestionerRealLLMSuite))
}

// newRealQuestionerComp 构造真链提问组件（不注入 fake，双路径模型创建走
// 直接构造路径：ModelClientConfig+ModelConfig → foundation/llm.NewModel）
func (s *QuestionerRealLLMSuite) newRealQuestionerComp() *llmcomp.QuestionerComponent {
	entries := configpanel.ZhipuFreeModelEntries()
	s.Require().NotEmpty(entries, "ZHIPU_BUILTIN_API_KEY 未注入且内置常量为空，跳过真链冒烟")
	mcc := entries[0]["model_client_config"].(map[string]any)
	modelID := mcc["model_name"].(string)
	s.T().Logf("选用内置免费模型：%s", modelID)

	cfg := &llmcomp.QuestionerConfig{
		ModelClientConfig: &schema.ModelClientConfig{
			ClientID:       "zhipu-free-realchain", // registry 强制非空（181002）
			ClientProvider: "OpenAI",               // 智谱 OpenAI 兼容端点
			APIKey:         mcc["api_key"].(string),
			APIBase:        mcc["api_base"].(string),
			Timeout:        120,
		},
		ModelConfig: &schema.ModelRequestConfig{
			ModelName: modelID,
			MaxTokens: ptrIntQ(1024),
		},
		ExtractFieldsFromResponse: boolPtrQ(true),
		FieldNames: []llmcomp.QuestionerFieldInfo{
			{FieldName: "name", CnFieldName: "姓名", Description: "用户姓名", Type: "string", Required: true},
		},
	}
	return llmcomp.NewQuestionerComponent(cfg)
}

func ptrIntQ(i int) *int { return &i }

// buildRealWorkflow 装配 start → questioner → end（模板引用抽取字段）
func (s *QuestionerRealLLMSuite) buildRealWorkflow(comp *llmcomp.QuestionerComponent) *workflow.Workflow {
	inputsSchema := map[string]any{"query": "${query}"}
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	w.AddWorkflowComp("ask", comp, nil, inputsSchema, nil, nil, nil, nil, 0, -1.0, nil, nil)
	endCfg, err := flow.NewEndConfig("collected:{{name}}")
	s.Require().NoError(err)
	end, err := flow.NewEnd(endCfg)
	s.Require().NoError(err)
	w.SetEndComp("end", end, map[string]any{"name": "${ask.name}"}, nil, nil, nil, "batch", nil)
	w.AddConnection([]string{"start"}, "ask")
	w.AddConnection([]string{"ask"}, "end")
	return w
}

// interactionFrameText 提取 Result 中 INTERACTION 帧文本
func (s *QuestionerRealLLMSuite) interactionFrameText(wo *workflowbase.WorkflowOutput) string {
	chunks, ok := wo.Result.([]stream.Schema)
	if !ok {
		return ""
	}
	for _, c := range chunks {
		if sc, isSchema := c.(stream.OutputSchema); isSchema && sc.Type == interaction.InteractionType {
			if io, isIO := sc.Payload.(interaction.InteractionOutput); isIO {
				return fmt.Sprintf("%v", io.Value)
			}
		}
	}
	return ""
}

// TestQuestioner_真实链冒烟 真实 LLM 抽取全链：结构性断言（状态机转换 +
// 帧形状 + 输出字段），不断言具体抽取值。
func (s *QuestionerRealLLMSuite) TestQuestioner_真实链冒烟() {
	entries := configpanel.ZhipuFreeModelEntries()
	if len(entries) == 0 {
		s.T().Skip("ZHIPU_BUILTIN_API_KEY 未注入且内置常量为空，跳过真链冒烟")
	}

	comp := s.newRealQuestionerComp()
	w := s.buildRealWorkflow(comp)
	sess := session.NewSession(session.WithEnvs(map[string]any{}))
	ctx := context.Background()

	// ---- 首调："你好" 无姓名 → 期望挂起（若模型幻觉补全则本用例降级为直通，
	// 以 state 实际值分流断言，保持与模型行为无关）----
	out, err := w.Invoke(ctx, map[string]any{"query": "你好"}, sess, nil, nil)
	s.Require().NoError(err)
	wo, ok := out.(*workflowbase.WorkflowOutput)
	s.Require().True(ok)
	s.T().Logf("首调 state=%v result=%v", wo.State, s.interactionFrameText(wo))

	if wo.State == workflowbase.WorkflowExecutionStateInputRequired {
		// 抽取模式主链：必填缺失 → 追问挂起
		s.NotEmpty(s.interactionFrameText(wo), "INPUT_REQUIRED 应携带 INTERACTION 帧")

		// ---- 二次调用：真实回答 → 恢复执行 ----
		ii, err := interaction.NewInteractiveInput()
		s.Require().NoError(err)
		s.Require().NoError(ii.Update("ask", "张三"))
		out2, err := w.Invoke(ctx, ii, sess, nil, nil)
		if err != nil {
			// Python :484-487 语义：模型首轮若抽出字段表外的键（如 greeting），
			// extracted_key_fields 计数 ≥ 字段表数 → USER_INTERACT 态落
			// "question_content is empty and no extractable fields"（101072）——
			// 真实模型行为相关的合法结局，按结构契约接受（与模型行为无关纪律）
			s.Contains(err.Error(), "101072",
				"恢复轮仅允许 COMPLETED / 追问挂起 / 101072（抽取键计数饱和），got: %v", err)
			return
		}
		wo2, ok := out2.(*workflowbase.WorkflowOutput)
		s.Require().True(ok)
		// 终态两可：抽齐 → COMPLETED；模型仍缺必填（未超限）→ 再次追问挂起
		s.Contains([]workflowbase.WorkflowExecutionState{
			workflowbase.WorkflowExecutionStateCompleted,
			workflowbase.WorkflowExecutionStateInputRequired,
		}, wo2.State, "got state=%v result=%v", wo2.State, wo2.Result)
	} else {
		// 模型幻觉补全直通：仅验证 COMPLETED 契约与下游渲染链路
		s.Equal(workflowbase.WorkflowExecutionStateCompleted, wo.State)
	}
}
