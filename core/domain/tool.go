package domain

// ToolSchema is retained for LLM adapter compatibility. Novel pipelines never
// send tools to the model (CallPolicy.Tools must stay empty).
type ToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	RiskLevel   RiskLevel      `json:"riskLevel,omitempty"`
}

type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)
