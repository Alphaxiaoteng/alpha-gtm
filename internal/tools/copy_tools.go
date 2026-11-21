package tools

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"alpha-nexus-egolite/internal/copyhumanize"
)

// copyToolsPath is where an operator's rule pack lives inside the data dir.
func copyToolsPath(rt *Runtime) string {
	if rt == nil || rt.DataDir == "" {
		return ""
	}
	return filepath.Join(rt.DataDir, "copy-rules.json")
}

func registerCopyTools(r *Registry) {
	r.Register(Def{
		Name: "copy.humanize", Category: "copy", SideEffect: "read",
		Description: "Rewrite machine-sounding Chinese copy with explainable rules; returns the rewrite plus every change",
		Params: []Param{
			{Name: "text", Type: "string", Required: true},
		},
		Handler: handleCopyHumanize,
	})
	r.Register(Def{
		Name: "copy.rules.list", Category: "copy", SideEffect: "read",
		Description: "List the effective copy rules, including disabled ones",
		Handler: func(_ context.Context, rt *Runtime, _ map[string]interface{}) (*Result, error) {
			rules, err := copyhumanize.LoadRules(copyToolsPath(rt))
			if err != nil {
				return nil, err
			}
			return okResult("ok", map[string]interface{}{"count": len(rules), "rules": rules}), nil
		},
	})
}

func handleCopyHumanize(_ context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	text := argString(args, "text", "")
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("text is required")
	}
	rules, err := copyhumanize.LoadRules(copyToolsPath(rt))
	if err != nil {
		return nil, err
	}
	result, err := copyhumanize.Apply(text, rules)
	if err != nil {
		return nil, err
	}
	return okResult("ok", map[string]interface{}{
		"text": result.Text, "changed": result.Changed, "matches": result.Matches,
	}), nil
}
