// Package values builds the data context used to evaluate template
// conditions and to render templates: values files (like helm -f) deep
// merged in order, then --values yq assignment expressions applied on top.
package values

import (
	"fmt"
	"os"
	"strings"

	"github.com/mikefarah/yq/v4/pkg/yqlib"
	"gopkg.in/yaml.v3"
)

// Build loads every values file (yaml or json) in order with deep merge
// (later wins), then applies each --values expression through yqlib.
func Build(files []string, exprs []string) (map[string]any, error) {
	data := map[string]any{}

	for _, file := range files {
		content, err := os.ReadFile(file) // #nosec
		if err != nil {
			return nil, err
		}
		var loaded map[string]any
		if err := yaml.Unmarshal(content, &loaded); err != nil {
			return nil, fmt.Errorf("values file %s: %w", file, err)
		}
		data = deepMerge(data, loaded)
	}

	if len(exprs) > 0 {
		merged, err := applyExpressions(data, exprs)
		if err != nil {
			return nil, err
		}
		data = merged
	}

	return data, nil
}

// applyExpressions runs each yq assignment against the yaml-serialized data
// document and decodes the result back.
func applyExpressions(data map[string]any, exprs []string) (map[string]any, error) {
	serialized, err := yaml.Marshal(data)
	if err != nil {
		return nil, err
	}

	yqlib.InitExpressionParser()
	prefs := yqlib.NewDefaultYamlPreferences()
	prefs.ColorsEnabled = false
	evaluator := yqlib.NewStringEvaluator()

	doc := string(serialized)
	for _, expr := range exprs {
		doc, err = evaluator.Evaluate(expr, doc, yqlib.NewYamlEncoder(prefs), yqlib.NewYamlDecoder(prefs))
		if err != nil {
			return nil, fmt.Errorf("--values '%s': %w", expr, err)
		}
	}

	var result map[string]any
	if err := yaml.Unmarshal([]byte(doc), &result); err != nil {
		return nil, fmt.Errorf("applying --values expressions: %w", err)
	}
	if result == nil {
		result = map[string]any{}
	}
	return result, nil
}

func deepMerge(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for key, srcVal := range src {
		if dstMap, ok := dst[key].(map[string]any); ok {
			if srcMap, ok := srcVal.(map[string]any); ok {
				dst[key] = deepMerge(dstMap, srcMap)
				continue
			}
		}
		dst[key] = srcVal
	}
	return dst
}

// Describe returns a compact single-line yaml rendering of the data, used
// in verbose logs.
func Describe(data map[string]any) string {
	serialized, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Sprintf("%v", data)
	}
	return strings.TrimSpace(string(serialized))
}
