package gates

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/resource/novel"
	"gopkg.in/yaml.v3"
)

// Signal is one data-driven 去 AI 味 check. The whole rule set lives in
// deslop-rules.yaml; adding a word or a threshold never touches Go code.
type Signal struct {
	ID       string  `yaml:"id"`
	Lens     string  `yaml:"lens"`
	Title    string  `yaml:"title"`
	Severity string  `yaml:"severity"` // blocking | advisory | weak
	Pattern  string  `yaml:"pattern"`
	PerMille float64 `yaml:"perMille"`
	MinHits  int     `yaml:"minHits"`
	Suggest  string  `yaml:"suggest"`
	Source   string  `yaml:"source"`

	re *regexp.Regexp
}

// Ruleset is a parsed deslop rule set.
type Ruleset struct {
	Version   int      `yaml:"version"`
	Signals   []Signal `yaml:"signals"`
	ClusterAt int      `yaml:"clusterAt"`
	// Source records where this set came from: book | library | builtin.
	Source string `yaml:"-"`
}

// Signal severity values.
const (
	SeverityBlocking = "blocking"
	SeverityAdvisory = "advisory"
	SeverityWeak     = "weak"
)

// LoadRules parses a rule set from YAML and compiles every pattern.
// A malformed pattern is a hard error: a silently dropped rule is worse than
// a loud failure, because the author would believe the check is running.
func LoadRules(raw []byte, source string) (*Ruleset, error) {
	var rs Ruleset
	if err := yaml.Unmarshal(raw, &rs); err != nil {
		return nil, fmt.Errorf("deslop-rules: %w", err)
	}
	for i := range rs.Signals {
		sig := &rs.Signals[i]
		if sig.ID == "" || sig.Pattern == "" {
			return nil, fmt.Errorf("deslop-rules: signal %d missing id or pattern", i)
		}
		switch sig.Severity {
		case SeverityBlocking, SeverityAdvisory, SeverityWeak:
		default:
			return nil, fmt.Errorf("deslop-rules: signal %s has unknown severity %q", sig.ID, sig.Severity)
		}
		re, err := regexp.Compile(sig.Pattern)
		if err != nil {
			return nil, fmt.Errorf("deslop-rules: %s pattern: %w", sig.ID, err)
		}
		sig.re = re
	}
	if rs.ClusterAt <= 0 {
		rs.ClusterAt = 3
	}
	rs.Source = source
	return &rs, nil
}

// BuiltinRules returns the compiled rule table shipped in the binary.
// The table is embedded from core/resource/novel/gates/ (deliberately outside
// knowledge/) so it can never be picked into a prompt.
func BuiltinRules() *Ruleset {
	rs, err := LoadRules([]byte(novel.DeslopRulesYAML()), RulesFromBuiltin)
	if err != nil {
		// 内置文件坏掉属于打包错误，宁可崩也不要静默失去全部检查。
		panic(err)
	}
	return rs
}

// ResolveRules picks the effective rule set: 本书 > 作者机器 > 内置。
// Both override files are plain YAML on disk, never knowledge articles.
func ResolveRules(bookYAML, authorYAML string) (*Ruleset, error) {
	switch {
	case strings.TrimSpace(bookYAML) != "":
		return LoadRules([]byte(bookYAML), RulesFromBook)
	case strings.TrimSpace(authorYAML) != "":
		return LoadRules([]byte(authorYAML), RulesFromAuthor)
	}
	return BuiltinRules(), nil
}

// RulesFrom* label where a rule set came from. Precedence:
// 本书 canon/deslop-rules.yaml > 作者机器 gates/deslop-rules.yaml > 内置。
const (
	RulesFromBook    = "book"
	RulesFromAuthor  = "author"
	RulesFromBuiltin = "builtin"
)

// hits returns how many times the signal matched in body.
func (s *Signal) hits(body string) int {
	if s.re == nil {
		return 0
	}
	return len(s.re.FindAllString(body, -1))
}
