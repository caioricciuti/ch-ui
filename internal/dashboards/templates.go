package dashboards

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Each template is a dashboard in the export format (dashboard, variables,
// panels) plus id, name, description and tags.
//
//go:embed templates/*.json
var templateFS embed.FS

// Template describes one built-in dashboard template.
type Template struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	PanelCount  int      `json:"panel_count"`
	Variables   []string `json:"variables"`
}

type templateFile struct {
	Template
	Panels    []json.RawMessage `json:"panels"`
	Variables []Variable        `json:"variables"`
}

func loadTemplate(name string) ([]byte, Template, error) {
	raw, err := templateFS.ReadFile("templates/" + name)
	if err != nil {
		return nil, Template{}, err
	}
	var f templateFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, Template{}, fmt.Errorf("template %s: %w", name, err)
	}
	if err := Validate(f.Variables); err != nil {
		return nil, Template{}, fmt.Errorf("template %s: %w", name, err)
	}
	t := f.Template
	t.PanelCount = len(f.Panels)
	t.Variables = []string{}
	for _, v := range f.Variables {
		t.Variables = append(t.Variables, v.Name)
	}
	if t.Tags == nil {
		t.Tags = []string{}
	}
	return raw, t, nil
}

// Templates lists the built-in templates, by name.
func Templates() ([]Template, error) {
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		return nil, err
	}
	out := []Template{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		_, t, err := loadTemplate(e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// TemplateContent returns the export JSON of a template and its summary.
func TemplateContent(id string) ([]byte, Template, error) {
	list, err := Templates()
	if err != nil {
		return nil, Template{}, err
	}
	for _, t := range list {
		if t.ID == id {
			return loadTemplate(id + ".json")
		}
	}
	return nil, Template{}, fmt.Errorf("template %q not found", id)
}
