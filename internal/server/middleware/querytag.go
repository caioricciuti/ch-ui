package middleware

import (
	"strings"

	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// surfaceNames maps the first /api path segment to the surface written into
// log_comment where the segment itself is not the right word. Any other
// segment is used as is, with dashes turned into underscores.
var surfaceNames = map[string]string{
	"query":              "editor",
	"saved-queries":      "saved_query",
	"dashboards":         "dashboard",
	"pipelines":          "pipeline",
	"models":             "model",
	"schedules":          "schedule",
	"costs":              "cost_center",
	"operations-reports": "operations_report",
	"reports":            "report",
	"connections":        "connection",
}

// SurfaceForPath names the CH-UI feature behind an API path, for the query
// tag: "/api/query/execute" is "editor", "/api/cluster-health/x" is
// "cluster_health". Paths outside /api get "api".
func SurfaceForPath(path string) string {
	rest := strings.TrimPrefix(path, "/api/")
	if rest == path {
		return "api"
	}
	seg := rest
	if i := strings.IndexByte(seg, '/'); i >= 0 {
		seg = seg[:i]
	}
	if seg == "" {
		return "api"
	}
	if name, ok := surfaceNames[seg]; ok {
		return name
	}
	return strings.ReplaceAll(seg, "-", "_")
}

// QueryTagFor is the tag a session request's queries carry: the surface from
// the path and the person behind the session. Handlers that know the
// workspace object refine it with tunnel.WithObjectTag.
func QueryTagFor(path string, session *SessionInfo) tunnel.QueryTag {
	return tunnel.QueryTag{Surface: SurfaceForPath(path), Person: Actor(session)}
}
