//go:build integration

package trajectory

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

// TrajectorySuite 15.11 轨迹适配集成测试套件。
type TrajectorySuite struct {
	suite.Suite
}

// TestTrajectorySuite 集成入口（审计要求：每个 _test.go 自带 suite.Run）。
func TestTrajectorySuite(t *testing.T) {
	suite.Run(t, new(TrajectorySuite))
}
