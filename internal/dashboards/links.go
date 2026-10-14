package dashboards

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/caioricciuti/ch-ui/internal/queryproc"
)

// Panel link kinds: what a click on a data point does.
const (
	LinkSetVariable = "setVariable"
	LinkDashboard   = "dashboard"
	LinkSQL         = "sql"
	LinkURL         = "url"
)

const (
	// MaxPanelLinks bounds the links on one panel.
	MaxPanelLinks  = 10
	maxLinkLabel   = 100
	maxLinkParams  = 30
	maxLinkParam   = 1000
	maxURLTemplate = 4000
	maxSQLTemplate = 20000
)

// linkID is the shape of a dashboard id: it ends up in the app's URL path.
var linkID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// PanelLink is one entry of the links array in a panel's config JSON.
type PanelLink struct {
	Kind           string            `json:"kind"`
	Label          string            `json:"label,omitempty"`
	Variable       string            `json:"variable,omitempty"`
	DashboardID    string            `json:"dashboardId,omitempty"`
	CarryVariables bool              `json:"carryVariables,omitempty"`
	Params         map[string]string `json:"params,omitempty"`
	Template       string            `json:"template,omitempty"`
}

// hasRawToken reports a ${__value:raw} token, which inserts the clicked
// value unquoted. Matched loosely, so no spelling of it slips through.
func hasRawToken(s string) bool {
	return strings.Contains(strings.ToLower(s), "__value:raw")
}

// panelLinks returns the links array of a panel config, or nil when the
// config has none. A config that is not a JSON object has no links.
func panelLinks(config string) (json.RawMessage, map[string]json.RawMessage) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(config)), &obj); err != nil {
		return nil, nil
	}
	raw, ok := obj["links"]
	if !ok || string(raw) == "null" {
		return nil, obj
	}
	return raw, obj
}

// ValidatePanelLinks checks the links in a panel config: a known kind, the
// fields that kind needs, bounded lengths, url templates that start with
// http:// or https://, and the raw value token only in sql links.
func ValidatePanelLinks(config string) error {
	raw, _ := panelLinks(config)
	if raw == nil {
		return nil
	}
	var links []PanelLink
	if err := json.Unmarshal(raw, &links); err != nil {
		return fmt.Errorf("links must be an array of link objects")
	}
	if len(links) > MaxPanelLinks {
		return fmt.Errorf("at most %d links per panel", MaxPanelLinks)
	}
	for i, l := range links {
		where := fmt.Sprintf("link %d", i+1)
		if utf8.RuneCountInString(l.Label) > maxLinkLabel {
			return fmt.Errorf("%s: label is longer than %d characters", where, maxLinkLabel)
		}
		if l.Kind != LinkSQL {
			if hasRawToken(l.Template) {
				return fmt.Errorf("%s: ${__value:raw} is only allowed in SQL links", where)
			}
			for _, v := range l.Params {
				if hasRawToken(v) {
					return fmt.Errorf("%s: ${__value:raw} is only allowed in SQL links", where)
				}
			}
		}
		switch l.Kind {
		case LinkSetVariable:
			if !queryproc.ValidVarName(l.Variable) {
				return fmt.Errorf("%s: pick the variable to set", where)
			}
		case LinkDashboard:
			if !linkID.MatchString(l.DashboardID) {
				return fmt.Errorf("%s: pick the dashboard to open", where)
			}
			if len(l.Params) > maxLinkParams {
				return fmt.Errorf("%s: at most %d parameters", where, maxLinkParams)
			}
			for k, v := range l.Params {
				if !queryproc.ValidVarName(k) {
					return fmt.Errorf("%s: %q is not a variable name", where, k)
				}
				if len(v) > maxLinkParam {
					return fmt.Errorf("%s: parameter %s is too long", where, k)
				}
			}
		case LinkSQL:
			if strings.TrimSpace(l.Template) == "" {
				return fmt.Errorf("%s: the SQL template is required", where)
			}
			if len(l.Template) > maxSQLTemplate {
				return fmt.Errorf("%s: the SQL template is too long", where)
			}
		case LinkURL:
			if len(l.Template) > maxURLTemplate {
				return fmt.Errorf("%s: the URL is too long", where)
			}
			lower := strings.ToLower(l.Template)
			if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
				return fmt.Errorf("%s: the URL must start with http:// or https://", where)
			}
		default:
			return fmt.Errorf("%s: kind must be one of setVariable, dashboard, sql, url", where)
		}
	}
	return nil
}

// PublicPanelConfig returns a panel config as a share link viewer may see
// it: only setVariable and url links, with only the fields those use. SQL
// templates and links to other dashboards stay on the server.
func PublicPanelConfig(config string) string {
	raw, obj := panelLinks(config)
	if raw == nil {
		return config
	}
	var links []PanelLink
	_ = json.Unmarshal(raw, &links)
	kept := make([]PanelLink, 0, len(links))
	for _, l := range links {
		switch l.Kind {
		case LinkSetVariable:
			kept = append(kept, PanelLink{Kind: l.Kind, Label: l.Label, Variable: l.Variable})
		case LinkURL:
			kept = append(kept, PanelLink{Kind: l.Kind, Label: l.Label, Template: l.Template})
		}
	}
	if len(kept) == 0 {
		delete(obj, "links")
	} else {
		b, _ := json.Marshal(kept)
		obj["links"] = b
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return "{}"
	}
	return string(out)
}
