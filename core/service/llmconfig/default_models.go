package llmconfig

import (
	_ "embed"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"gopkg.in/yaml.v3"
)

//go:embed default_models.yaml
var defaultModelsYAML []byte

// BuiltInDefaultModels returns the embedded per-model parameter catalog
// (capacity, sampling, available_efforts), aligned with danmo-work.
func BuiltInDefaultModels() []domain.ModelConfig {
	var models []domain.ModelConfig
	if err := yaml.Unmarshal(defaultModelsYAML, &models); err != nil {
		return nil
	}
	return domain.StripLegacyModelConfigFields(models)
}
