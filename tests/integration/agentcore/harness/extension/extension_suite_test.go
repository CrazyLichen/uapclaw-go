//go:build integration

package extension_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ExtensionSuite 领域 15.15 插件/模板热加载端到端套件。
type ExtensionSuite struct {
	isuite.AgentSuite
}

// TestExtensionSuite 套件入口。
func TestExtensionSuite(t *testing.T) {
	suite.Run(t, new(ExtensionSuite))
}
