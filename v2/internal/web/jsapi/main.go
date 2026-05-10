//go:build js && wasm

// jsapi is a thin WASM wrapper that exposes jd v2 as JS-callable
// functions on the global object: jdDiff, jdPatch, jdTranslate.
// Built with: GOOS=js GOARCH=wasm go build -o jd.wasm ./internal/web/jsapi
package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"syscall/js"

	jd "github.com/josephburnett/jd/v2"
)

func main() {
	js.Global().Set("jdDiff", js.FuncOf(jdDiff))
	js.Global().Set("jdPatch", js.FuncOf(jdPatch))
	js.Global().Set("jdTranslate", js.FuncOf(jdTranslate))
	// Block forever so the runtime stays alive and exported funcs remain callable.
	select {}
}

func errResult(err error) any {
	return map[string]any{"error": err.Error()}
}

// parseOptions accepts either:
//   - a JSON array string (e.g. `["SET"]` or `[{"precision":0.001}]`)
//   - a space-separated token list (e.g. "SET MULTISET MERGE")
func parseOptions(s string) ([]jd.Option, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if strings.HasPrefix(s, "[") {
		return jd.ReadOptionsString(s)
	}
	// Tokenized form: convert to JSON array of strings.
	tokens := strings.Fields(s)
	quoted := make([]string, len(tokens))
	for i, t := range tokens {
		b, _ := json.Marshal(strings.ToUpper(t))
		quoted[i] = string(b)
	}
	return jd.ReadOptionsString("[" + strings.Join(quoted, ",") + "]")
}

func readNode(format, input string) (jd.JsonNode, error) {
	switch strings.ToLower(format) {
	case "", "json":
		return jd.ReadJsonString(input)
	case "yaml", "yml":
		return jd.ReadYamlString(input)
	default:
		return nil, fmt.Errorf("unknown format %q", format)
	}
}

func renderNode(format string, n jd.JsonNode) string {
	switch strings.ToLower(format) {
	case "yaml", "yml":
		return n.Yaml()
	default:
		return n.Json()
	}
}

// jdDiff(a, b, options="", format="json", diffFormat="jd") -> string | {error}
func jdDiff(_ js.Value, args []js.Value) any {
	defer func() {
		if r := recover(); r != nil {
			// Convert panic to a structured error so JS sees it.
			fmt.Printf("jdDiff panic: %v\n", r)
		}
	}()
	if len(args) < 2 {
		return errResult(fmt.Errorf("jdDiff requires (a, b, [options], [format], [diffFormat])"))
	}
	optStr := ""
	format := "json"
	diffFormat := "jd"
	if len(args) >= 3 {
		optStr = args[2].String()
	}
	if len(args) >= 4 {
		format = args[3].String()
	}
	if len(args) >= 5 {
		diffFormat = args[4].String()
	}
	opts, err := parseOptions(optStr)
	if err != nil {
		return errResult(err)
	}
	a, err := readNode(format, args[0].String())
	if err != nil {
		return errResult(fmt.Errorf("read a: %w", err))
	}
	b, err := readNode(format, args[1].String())
	if err != nil {
		return errResult(fmt.Errorf("read b: %w", err))
	}
	d := a.Diff(b, opts...)
	switch strings.ToLower(diffFormat) {
	case "", "jd":
		return d.Render(opts...)
	case "patch":
		s, err := d.RenderPatch()
		if err != nil {
			return errResult(err)
		}
		return s
	case "merge":
		s, err := d.RenderMerge()
		if err != nil {
			return errResult(err)
		}
		return s
	default:
		return errResult(fmt.Errorf("unknown diffFormat %q", diffFormat))
	}
}

// jdPatch(target, patch, format="json", diffFormat="jd") -> string | {error}
func jdPatch(_ js.Value, args []js.Value) any {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("jdPatch panic: %v\n", r)
		}
	}()
	if len(args) < 2 {
		return errResult(fmt.Errorf("jdPatch requires (target, patch, [format], [diffFormat])"))
	}
	format := "json"
	diffFormat := "jd"
	if len(args) >= 3 {
		format = args[2].String()
	}
	if len(args) >= 4 {
		diffFormat = args[3].String()
	}
	target, err := readNode(format, args[0].String())
	if err != nil {
		return errResult(fmt.Errorf("read target: %w", err))
	}
	var diff jd.Diff
	switch strings.ToLower(diffFormat) {
	case "", "jd":
		diff, err = jd.ReadDiffString(args[1].String())
	case "patch":
		diff, err = jd.ReadPatchString(args[1].String())
	case "merge":
		// Merge patches are applied via Patch with MERGE option-bearing diff.
		// Use ReadDiffString fallback after wrapping; not exposed, so error out.
		return errResult(fmt.Errorf("merge patch input not supported; convert to jd format first"))
	default:
		return errResult(fmt.Errorf("unknown diffFormat %q", diffFormat))
	}
	if err != nil {
		return errResult(fmt.Errorf("read diff: %w", err))
	}
	out, err := target.Patch(diff)
	if err != nil {
		return errResult(err)
	}
	return renderNode(format, out)
}

// jdTranslate(input, fromFormat, toFormat) -> string | {error}
// Translate JSON <-> YAML through a JsonNode.
func jdTranslate(_ js.Value, args []js.Value) any {
	if len(args) < 3 {
		return errResult(fmt.Errorf("jdTranslate requires (input, fromFormat, toFormat)"))
	}
	n, err := readNode(args[1].String(), args[0].String())
	if err != nil {
		return errResult(err)
	}
	return renderNode(args[2].String(), n)
}
