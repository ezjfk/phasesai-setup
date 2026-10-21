package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
	"github.com/tailscale/hujson"
)

type fieldChange struct {
	Path  []string
	Value any
}

func configFields(harness, model string) ([]fieldChange, error) {
	switch harness {
	case "claude":
		return []fieldChange{{[]string{"env", "ANTHROPIC_BASE_URL"}, origin}, {[]string{"env", "ANTHROPIC_MODEL"}, model}}, nil
	case "codex":
		out := []fieldChange{{[]string{"model"}, model}, {[]string{"model_provider"}, "phasesai"}}
		for _, v := range []struct {
			k string
			v any
		}{{"name", "PhasesAI"}, {"base_url", origin + "/v1"}, {"env_key", "PHASESAI_API_KEY"}, {"wire_api", "responses"}, {"requires_openai_auth", false}, {"supports_websockets", false}} {
			out = append(out, fieldChange{[]string{"model_providers", "phasesai", v.k}, v.v})
		}
		return out, nil
	case "opencode":
		return []fieldChange{
			{[]string{"model"}, "phasesai/" + model}, {[]string{"provider", "phasesai", "npm"}, "@ai-sdk/openai"}, {[]string{"provider", "phasesai", "name"}, "PhasesAI"},
			{[]string{"provider", "phasesai", "options", "baseURL"}, origin + "/v1"}, {[]string{"provider", "phasesai", "options", "apiKey"}, "{env:PHASESAI_API_KEY}"},
			{[]string{"provider", "phasesai", "models", model, "name"}, model},
		}, nil
	default:
		return nil, errors.New("unsupported harness")
	}
}

func decodeConfig(harness string, data []byte) (map[string]any, error) {
	out := map[string]any{}
	if len(bytes.TrimSpace(data)) == 0 {
		return out, nil
	}
	if harness == "codex" {
		if toml.Unmarshal(data, &out) != nil || out == nil {
			return nil, errors.New("invalid TOML configuration")
		}
		return out, nil
	}
	if harness == "claude" && !json.Valid(data) {
		return nil, errors.New("Claude settings require strict JSON")
	}
	v, err := hujson.Parse(bytes.Clone(data))
	if err != nil {
		return nil, errors.New("invalid JSON/JSONC configuration")
	}
	if err = uniqueJSON(v); err != nil {
		return nil, err
	}
	v.Standardize()
	d := json.NewDecoder(bytes.NewReader(v.Pack()))
	d.UseNumber()
	if d.Decode(&out) != nil || out == nil {
		return nil, errors.New("configuration must be an object")
	}
	return out, nil
}

func uniqueJSON(v hujson.Value) error {
	switch o := v.Value.(type) {
	case *hujson.Object:
		seen := map[string]bool{}
		for _, m := range o.Members {
			k := m.Name.Value.(hujson.Literal).String()
			if seen[k] {
				return errors.New("duplicate JSON keys require manual cleanup")
			}
			seen[k] = true
			if err := uniqueJSON(m.Value); err != nil {
				return err
			}
		}
	case *hujson.Array:
		for _, e := range o.Elements {
			if err := uniqueJSON(e); err != nil {
				return err
			}
		}
	}
	return nil
}

func fieldValue(root map[string]any, path []string) (any, bool) {
	var value any = root
	for _, key := range path {
		m, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = m[key]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

func changedFields(harness, model string, before []byte) ([]fieldChange, error) {
	root, err := decodeConfig(harness, before)
	if err != nil {
		return nil, err
	}
	fields, err := configFields(harness, model)
	if err != nil {
		return nil, err
	}
	var changes []fieldChange
	for _, f := range fields {
		current := root
		for _, key := range f.Path[:len(f.Path)-1] {
			next, err := object(current, key)
			if err != nil {
				return nil, err
			}
			current = next
		}
		key := f.Path[len(f.Path)-1]
		if old, ok := current[key]; !ok || !reflect.DeepEqual(old, f.Value) {
			changes = append(changes, f)
		}
	}
	return changes, nil
}

func render(harness, model string, before []byte) ([]byte, error) {
	changes, err := changedFields(harness, model, before)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return before, nil
	}
	if harness == "codex" {
		return patchTOML(before, changes)
	}
	source := before
	if len(bytes.TrimSpace(source)) == 0 {
		source = []byte("{}\n")
	}
	tree, err := hujson.Parse(bytes.Clone(source))
	if err != nil {
		return nil, errors.New("cannot parse configuration")
	}
	for _, f := range changes {
		if err := putJSON(&tree, f.Path, f.Value); err != nil {
			return nil, err
		}
	}
	tree.Format()
	out := tree.Pack()
	if _, err := decodeConfig(harness, out); err != nil {
		return nil, err
	}
	return out, nil
}

func putJSON(tree *hujson.Value, path []string, value any) error {
	o, ok := tree.Value.(*hujson.Object)
	if !ok {
		return errors.New("configuration scope must be an object")
	}
	for i := range o.Members {
		if o.Members[i].Name.Value.(hujson.Literal).String() != path[0] {
			continue
		}
		if len(path) > 1 {
			return putJSON(&o.Members[i].Value, path[1:], value)
		}
		b, _ := json.Marshal(value)
		v, err := hujson.Parse(b)
		if err != nil {
			return err
		}
		o.Members[i].Value.Value = v.Value
		return nil
	}
	member := hujson.ObjectMember{Name: hujson.Value{Value: hujson.String(path[0])}, Value: hujson.Value{Value: &hujson.Object{}}}
	o.Members = append(o.Members, member)
	if len(path) > 1 {
		return putJSON(&o.Members[len(o.Members)-1].Value, path[1:], value)
	}
	b, _ := json.Marshal(value)
	v, err := hujson.Parse(b)
	if err == nil {
		o.Members[len(o.Members)-1].Value.Value = v.Value
	}
	return err
}

type byteEdit struct {
	start, end int
	data       []byte
}

func patchTOML(source []byte, fields []fieldChange) ([]byte, error) {
	// Modify only scalar value ranges and insert missing fields into their actual
	// table. All unrelated comments, whitespace, values and ordering remain intact.
	parser := unstable.Parser{}
	parser.Reset(source)
	var table []string
	firstTable := len(source)
	tableEnds := map[string]int{"": len(source)}
	previous := ""
	found := map[int]bool{}
	var edits []byteEdit
	for parser.NextExpression() {
		node := parser.Expression()
		if node.Kind == unstable.Table || node.Kind == unstable.ArrayTable {
			key := node.Key()
			table = nil
			start := len(source)
			for key.Next() {
				n := key.Node()
				table = append(table, string(n.Data))
				if int(n.Raw.Offset) < start {
					start = int(n.Raw.Offset)
				}
			}
			for start > 0 && source[start-1] != '\n' {
				start--
			}
			if start < firstTable {
				firstTable = start
			}
			tableEnds[previous] = start
			previous = strings.Join(table, "\x00")
			tableEnds[previous] = len(source)
			if node.Kind == unstable.ArrayTable {
				table = append(table, "[]")
			}
			continue
		}
		if node.Kind != unstable.KeyValue {
			continue
		}
		keys := append([]string{}, table...)
		it := node.Key()
		for it.Next() {
			keys = append(keys, string(it.Node().Data))
		}
		for i, f := range fields {
			if !reflect.DeepEqual(keys, f.Path) {
				continue
			}
			v := node.Value()
			if v.Raw.Length == 0 {
				return nil, errors.New("unsupported inline TOML layout; merge manually")
			}
			b, _ := json.Marshal(f.Value)
			edits = append(edits, byteEdit{int(v.Raw.Offset), int(v.Raw.Offset + v.Raw.Length), b})
			found[i] = true
		}
	}
	if parser.Error() != nil {
		return nil, errors.New("invalid TOML configuration")
	}
	tableEnds[""] = firstTable
	missing := map[string][]fieldChange{}
	var order []string
	for i, f := range fields {
		if found[i] {
			continue
		}
		t := strings.Join(f.Path[:len(f.Path)-1], "\x00")
		if _, ok := missing[t]; !ok {
			order = append(order, t)
		}
		missing[t] = append(missing[t], f)
	}
	inserts := map[int]string{}
	for _, t := range order {
		pos, exists := tableEnds[t]
		text := ""
		if !exists {
			pos = len(source)
			text = "\n[" + strings.ReplaceAll(t, "\x00", ".") + "]\n"
		}
		if pos > 0 && source[pos-1] != '\n' {
			text = "\n" + text
		}
		for _, f := range missing[t] {
			b, _ := json.Marshal(f.Value)
			text += f.Path[len(f.Path)-1] + " = " + string(b) + "\n"
		}
		inserts[pos] += text
	}
	for pos, text := range inserts {
		edits = append(edits, byteEdit{pos, pos, []byte(text)})
	}
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := bytes.Clone(source)
	for _, e := range edits {
		out = append(append(append([]byte{}, out[:e.start]...), e.data...), out[e.end:]...)
	}
	var parsed map[string]any
	if toml.Unmarshal(out, &parsed) != nil {
		return nil, errors.New("TOML layout cannot be patched safely; merge the displayed field paths manually")
	}
	for _, f := range fields {
		v, ok := fieldValue(parsed, f.Path)
		if !ok || !reflect.DeepEqual(v, f.Value) {
			return nil, errors.New("TOML patch verification failed")
		}
	}
	return out, nil
}

func configConflicts(harness string, data []byte) ([]string, error) {
	root, err := decodeConfig(harness, data)
	if err != nil {
		return nil, err
	}
	var paths [][]string
	switch harness {
	case "claude":
		paths = [][]string{{"env", "ANTHROPIC_API_KEY"}, {"env", "ANTHROPIC_AUTH_TOKEN"}, {"apiKeyHelper"}}
	case "codex":
		for _, k := range []string{"http_headers", "env_http_headers", "experimental_bearer_token", "bearer_token"} {
			paths = append(paths, []string{"model_providers", "phasesai", k})
		}
	case "opencode":
		paths = [][]string{{"provider", "phasesai", "options", "headers"}}
	}
	var out []string
	for _, p := range paths {
		if value, ok := fieldValue(root, p); ok && value != nil && value != "" {
			out = append(out, strings.Join(p, "."))
		}
	}
	return out, nil
}

func parseJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}
