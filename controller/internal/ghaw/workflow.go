// Package ghaw parses gh-aw workflow sources, compiled lock files and run artifacts.
package ghaw

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/rancher/agentic-extension/controller/api/v1alpha1"
)

// Workflow is what the controller learns from X.md + X.lock.yml.
type Workflow struct {
	DisplayName    string
	Description    string
	Engine         string
	Model          string
	TimeoutMinutes int
	Triggers       []string
	Schedule       string
	SlashCommand   string
	Dispatchable   bool
	DispatchInputs []v1alpha1.DispatchInput
	SafeOutputs    []string
}

// internalInputs are dispatch inputs gh-aw adds for its own use.
var internalInputs = map[string]bool{"aw_context": true}

// nonTriggerKeys are keys of the frontmatter "on" block that are not triggers.
var nonTriggerKeys = map[string]bool{"reaction": true, "stop-after": true, "manual-approval": true, "status-comment": true}

// safeOutputConfigKeys are keys of "safe-outputs" that configure behaviour rather than name a kind.
var safeOutputConfigKeys = map[string]bool{
	"threat-detection": true, "staged": true, "github-token": true, "env": true, "jobs": true,
	"runs-on": true, "max-patch-size": true, "messages": true, "footer": true, "app": true,
	"allowed-domains": true, "mentions": true, "steps": true,
}

// ExtractFrontmatter returns the YAML frontmatter of a gh-aw markdown file, dedented.
// The block starts at the first line whose trimmed text is "---" and ends at the next one.
func ExtractFrontmatter(md string) (string, error) {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "---" {
			start = i
			break
		}
		if strings.TrimSpace(l) != "" {
			return "", fmt.Errorf("no frontmatter: first non-empty line is not ---")
		}
	}
	if start < 0 {
		return "", fmt.Errorf("no frontmatter found")
	}
	end := -1
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return "", fmt.Errorf("frontmatter not terminated")
	}
	body := lines[start+1 : end]
	return strings.Join(dedent(body), "\n"), nil
}

func dedent(lines []string) []string {
	indent := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " \t"))
		if indent < 0 || n < indent {
			indent = n
		}
	}
	if indent <= 0 {
		return lines
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		if len(l) >= indent {
			out[i] = l[indent:]
		} else {
			out[i] = strings.TrimLeft(l, " \t")
		}
	}
	return out
}

// Parse builds a Workflow from the markdown source and the compiled lock file.
func Parse(markdown, lock []byte) (*Workflow, error) {
	fmText, err := ExtractFrontmatter(string(markdown))
	if err != nil {
		return nil, err
	}
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
		return nil, fmt.Errorf("parsing frontmatter: %w", err)
	}
	w := &Workflow{}
	w.Description = strings.TrimSpace(str(fm["description"]))
	w.TimeoutMinutes = toInt(fm["timeout-minutes"])

	switch e := fm["engine"].(type) {
	case string:
		w.Engine = e
	case map[string]any:
		w.Engine = str(e["id"])
		w.Model = str(e["model"])
	}

	if on, ok := fm["on"].(map[string]any); ok {
		for k, v := range on {
			if nonTriggerKeys[k] {
				continue
			}
			w.Triggers = append(w.Triggers, k)
			switch k {
			case "schedule":
				w.Schedule = parseSchedule(v)
			case "workflow_dispatch":
				w.Dispatchable = true
			case "slash_command":
				switch sc := v.(type) {
				case map[string]any:
					w.SlashCommand = str(sc["name"])
				case string:
					w.SlashCommand = sc
				}
			}
		}
	} else if s, ok := fm["on"].(string); ok && s != "" {
		w.Triggers = []string{s}
		w.Dispatchable = s == "workflow_dispatch"
	}
	sort.Strings(w.Triggers)

	if so, ok := fm["safe-outputs"].(map[string]any); ok {
		for k := range so {
			if !safeOutputConfigKeys[k] {
				w.SafeOutputs = append(w.SafeOutputs, k)
			}
		}
		sort.Strings(w.SafeOutputs)
	}

	if len(lock) > 0 {
		var lk map[string]any
		if err := yaml.Unmarshal(lock, &lk); err != nil {
			return nil, fmt.Errorf("parsing lock file: %w", err)
		}
		w.DisplayName = str(lk["name"])
		if w.Dispatchable {
			w.DispatchInputs = lockInputs(lk)
		}
	}
	return w, nil
}

func parseSchedule(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []any:
		var crons []string
		for _, item := range s {
			switch it := item.(type) {
			case map[string]any:
				if c := str(it["cron"]); c != "" {
					crons = append(crons, c)
				}
			case string:
				crons = append(crons, it)
			}
		}
		return strings.Join(crons, ", ")
	case map[string]any:
		return str(s["cron"])
	}
	return ""
}

func lockInputs(lk map[string]any) []v1alpha1.DispatchInput {
	on, ok := lk["on"].(map[string]any)
	if !ok {
		return nil
	}
	wd, ok := on["workflow_dispatch"].(map[string]any)
	if !ok {
		return nil
	}
	inputs, ok := wd["inputs"].(map[string]any)
	if !ok {
		return nil
	}
	var out []v1alpha1.DispatchInput
	for name, raw := range inputs {
		if internalInputs[name] {
			continue
		}
		in := v1alpha1.DispatchInput{Name: name}
		if m, ok := raw.(map[string]any); ok {
			in.Description = str(m["description"])
			in.Type = str(m["type"])
			if b, ok := m["required"].(bool); ok {
				in.Required = b
			}
			if d, ok := m["default"]; ok && d != nil {
				in.Default = str(d)
			}
			if opts, ok := m["options"].([]any); ok {
				for _, o := range opts {
					in.Options = append(in.Options, str(o))
				}
			}
		}
		out = append(out, in)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func toInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		var n int
		_, _ = fmt.Sscanf(t, "%d", &n)
		return n
	}
	return 0
}
