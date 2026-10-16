package tunnel

import (
	"context"
	"encoding/json"
	"strings"
)

// QueryTag names who and what is behind a query. The gateway writes it into
// the log_comment setting, so every row in system.query_log can be split by
// person and by workspace object even when ClickHouse itself sees a shared
// account (SSO service accounts, background accounts).
//
// Surface is the CH-UI feature issuing the query ("editor", "dashboard",
// "schedule"). Person is the human it runs for: the SSO email or the
// ClickHouse user for password logins, the job owner for background work.
// Object points at the workspace object, as "<kind>:<id>" ("dash:42",
// "schedule:7"), and is empty for ad-hoc queries.
type QueryTag struct {
	Surface string
	Person  string
	Object  string
}

// maxTagField bounds each field: the tag lands in every query_log row.
const maxTagField = 128

type logCommentJSON struct {
	Surface string `json:"chui"`
	Person  string `json:"person,omitempty"`
	Object  string `json:"object,omitempty"`
}

// LogComment renders the tag as the compact JSON stored in log_comment, with
// "chui" always first so filters can match on LogCommentPrefix. Empty when
// the tag has no surface: a query_log row without our tag is a foreign query.
func (t QueryTag) LogComment() string {
	if t.Surface == "" {
		return ""
	}
	b, err := json.Marshal(logCommentJSON{
		Surface: clipTagField(t.Surface),
		Person:  clipTagField(t.Person),
		Object:  clipTagField(t.Object),
	})
	if err != nil {
		return ""
	}
	return string(b)
}

// LogCommentPrefix is the start of every log_comment this surface writes, for
// startsWith() filters in query_log readers that must skip their own queries.
func LogCommentPrefix(surface string) string {
	b, _ := json.Marshal(clipTagField(surface))
	return `{"chui":` + string(b)
}

// WithObject returns the tag pointing at a workspace object.
func (t QueryTag) WithObject(kind, id string) QueryTag {
	if id == "" {
		return t
	}
	t.Object = kind + ":" + id
	return t
}

func clipTagField(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxTagField {
		return s[:maxTagField]
	}
	return s
}

type tagKey struct{}

// WithTag returns ctx carrying tag. Queries run through the gateway with this
// context get the tag as log_comment unless the caller set one explicitly.
func WithTag(ctx context.Context, tag QueryTag) context.Context {
	return context.WithValue(ctx, tagKey{}, tag)
}

// TagFrom returns the tag carried by ctx, if any.
func TagFrom(ctx context.Context) (QueryTag, bool) {
	if ctx == nil {
		return QueryTag{}, false
	}
	t, ok := ctx.Value(tagKey{}).(QueryTag)
	return t, ok && t.Surface != ""
}

// WithObjectTag refines the tag already on ctx with a workspace object. A
// context without a tag is returned unchanged.
func WithObjectTag(ctx context.Context, kind, id string) context.Context {
	t, ok := TagFrom(ctx)
	if !ok {
		return ctx
	}
	return WithTag(ctx, t.WithObject(kind, id))
}

// TagSettings merges the context's tag into settings as log_comment when the
// caller has not set one. The caller's map is never modified: it may be shared
// across retries or requests. The gateway applies it on the query paths that
// take a context; stream and export callers apply it themselves.
func TagSettings(ctx context.Context, settings map[string]string) map[string]string {
	if _, set := settings["log_comment"]; set {
		return settings
	}
	tag, ok := TagFrom(ctx)
	if !ok {
		return settings
	}
	out := make(map[string]string, len(settings)+1)
	for k, v := range settings {
		out[k] = v
	}
	out["log_comment"] = tag.LogComment()
	return out
}
