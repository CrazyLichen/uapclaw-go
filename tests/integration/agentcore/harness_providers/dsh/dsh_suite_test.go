//go:build integration

package dsh

import (
	"testing"

	"github.com/stretchr/testify/suite"

	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// DshHarnessSuite 15.9 dsh provider 端到端集成测试。
type DshHarnessSuite struct {
	isuite.AgentSuite
}

// TestDshHarnessSuite 集成入口（审计要求：每个 _test.go 自带 suite.Run）。
func TestDshHarnessSuite(t *testing.T) {
	suite.Run(t, new(DshHarnessSuite))
}
