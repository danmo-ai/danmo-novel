package novel

import _ "embed"

// The program-rule files live outside knowledge/ on purpose: knowledge articles
// are picked into prompts by stage, and a regex table has no business in a
// prompt. Keeping them in a separate embed target makes accidental injection
// structurally impossible rather than a convention to remember.

//go:embed gates/deslop_rules.yaml
var deslopRulesYAML []byte

// DeslopRulesYAML returns the built-in local-gate rule table.
// Read by gates.BuiltinRules; never reaches an LLM call.
func DeslopRulesYAML() string { return string(deslopRulesYAML) }
