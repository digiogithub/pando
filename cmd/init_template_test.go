package cmd

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

// A fresh config must carry the shared decision model block and never the
// legacy [ModelAutoMode.Router] one.
func TestInitTemplatesUseDecisionModelRouter(t *testing.T) {
	for name, tmpl := range map[string]string{
		"pando init":             defaultConfigTemplate,
		"config.DefaultTemplate": config.DefaultConfigTemplate,
	} {
		if !strings.Contains(tmpl, "[DecisionModel.Router]") {
			t.Errorf("%s template lacks [DecisionModel.Router]", name)
		}
		if strings.Contains(tmpl, "[ModelAutoMode.Router]") {
			t.Errorf("%s template still has [ModelAutoMode.Router]", name)
		}
	}
}
