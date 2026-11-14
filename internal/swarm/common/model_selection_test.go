package common

import (
	"encoding/json"
	"errors"
	"testing"
)

// ModelSelection Validate 三个 validator 正反例。
// 对齐 Python: model_selection.py:16/:24/:34 与 pydantic ValueError 文案。
func TestModelSelectionValidate_三个约束(t *testing.T) {
	// 正例：合法 model 引用
	s := &ModelSelection{Type: "model", ID: "mdl_a"}
	if err := s.Validate(); err != nil {
		t.Fatalf("合法 model 引用不应报错: %v", err)
	}
	// 正例：合法 model_group 引用带 route_id
	s = &ModelSelection{Type: "model_group", ID: "mgp_a", RouteID: "rte_1"}
	if err := s.Validate(); err != nil {
		t.Fatalf("合法 model_group 引用不应报错: %v", err)
	}
	// validate_id：空白 id 报错
	s = &ModelSelection{Type: "model", ID: "  "}
	err := s.Validate()
	if err == nil || err.Error() != "id must not be empty" {
		t.Errorf("id 空白错误文案不符: %v", err)
	}
	// validate_route_id：route_id 全空白报错
	s = &ModelSelection{Type: "model_group", ID: "mgp_a", RouteID: " \t"}
	err = s.Validate()
	if err == nil || err.Error() != "route_id must not be empty" {
		t.Errorf("route_id 空白错误文案不符: %v", err)
	}
	// validate_route_scope：model 引用带 route_id 报错
	s = &ModelSelection{Type: "model", ID: "mdl_a", RouteID: "rte_1"}
	err = s.Validate()
	if err == nil || err.Error() != "route_id is only valid for a model_group selection" {
		t.Errorf("route_id 越界错误文案不符: %v", err)
	}
}

// ResolvedModelGroup JSON round-trip：序列化键名与上游契约逐字一致。
func TestResolvedModelGroup_JSON键名快照(t *testing.T) {
	group := ResolvedModelGroup{
		ModelGroupID: "mgp_a",
		Routes: []ResolvedRoute{{
			RouteID:          "rte_1",
			Model:            ResolvedModel{ModelID: "mdl_a", Source: "defaults", ModelName: "gpt-x", ClientOptions: map[string]any{}, RequestDefaults: map[string]any{}},
			Enabled:          true,
			RequestOverrides: map[string]any{"temperature": 0.7},
			TPM:              100,
		}},
		RequestConfig: map[string]any{},
		Routing:       map[string]any{"timeout_seconds": 30},
	}
	raw, err := json.Marshal(group)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var round ResolvedModelGroup
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if round.ModelGroupID != "mgp_a" || len(round.Routes) != 1 || round.Routes[0].Model.ModelID != "mdl_a" {
		t.Errorf("round-trip 字段丢失: %+v", round)
	}
	// 键名快照
	var asMap map[string]any
	_ = json.Unmarshal(raw, &asMap)
	for _, key := range []string{"model_group_id", "routes", "request_config", "routing"} {
		if _, ok := asMap[key]; !ok {
			t.Errorf("缺少契约键 %q", key)
		}
	}
	routeMap := round.Routes[0]
	if routeID := routeMap.RouteID; routeID != "rte_1" {
		t.Errorf("route_id 键映射错误: %s", routeID)
	}
}

// ModelSelectionError：Error() 返回 Message，ToPayload 合并 details。
func TestModelSelectionError_构造与ToPayload(t *testing.T) {
	e := NewModelSelectionError(ModelSelectionNotFound, "unknown model_id 'mdl_x'", map[string]any{"model_id": "mdl_x"})
	if e.Error() != "unknown model_id 'mdl_x'" {
		t.Errorf("Error() 应返回 Message: %q", e.Error())
	}
	payload := e.ToPayload()
	if payload["code"] != "MODEL_SELECTION_NOT_FOUND" || payload["message"] != "unknown model_id 'mdl_x'" || payload["model_id"] != "mdl_x" {
		t.Errorf("ToPayload 形状不符: %v", payload)
	}
	var target *ModelSelectionError
	if !errors.As(error(e), &target) || target.Code != ModelSelectionNotFound {
		t.Errorf("errors.As 应能还原 *ModelSelectionError")
	}
}

// 十二个错误码常量值逐字快照（Python: model_errors.py:8-19）。
func TestModelSelectionErrorCodes_逐字快照(t *testing.T) {
	cases := map[string]string{
		ModelSelectionNotFound:     "MODEL_SELECTION_NOT_FOUND",
		ModelSelectionDisabled:     "MODEL_SELECTION_DISABLED",
		ModelSelectionForbidden:    "MODEL_SELECTION_FORBIDDEN",
		ModelGroupInvalid:          "MODEL_GROUP_INVALID",
		ModelGroupNoAvailableRoute: "MODEL_GROUP_NO_AVAILABLE_ROUTE",
		ModelRequestConfigInvalid:  "MODEL_REQUEST_CONFIG_INVALID",
		ModelRouteFailed:           "MODEL_ROUTE_FAILED",
		ModelStreamInterrupted:     "MODEL_STREAM_INTERRUPTED",
		ModelOutcomeUnknown:        "MODEL_OUTCOME_UNKNOWN",
		ModelSelectionReferenced:   "MODEL_SELECTION_REFERENCED",
		ModelRuntimeUnavailable:    "MODEL_RUNTIME_UNAVAILABLE",
		TeamModelSelectionStale:    "TEAM_MODEL_SELECTION_STALE",
	}
	// 机器核对：12 个常量（Python wc 实测 12 行常量定义）
	if len(cases) != 12 {
		t.Fatalf("错误码数量应为 12，实际 %d", len(cases))
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("错误码漂移: got %q want %q", got, want)
		}
	}
}
