package middleware

import "testing"

func TestSurfaceForPath(t *testing.T) {
	cases := map[string]string{
		"/api/query/execute":           "editor",
		"/api/query":                   "editor",
		"/api/dashboards/42/panels/1":  "dashboard",
		"/api/cluster-health/overview": "cluster_health",
		"/api/saved-queries/9/run":     "saved_query",
		"/api/brain/chat":              "brain",
		"/api/admin/users":             "admin",
		"/api/":                        "api",
		"/healthz":                     "api",
	}
	for path, want := range cases {
		if got := SurfaceForPath(path); got != want {
			t.Errorf("%s: got %s want %s", path, got, want)
		}
	}
	tag := QueryTagFor("/api/query/execute", &SessionInfo{ClickhouseUser: "svc", AuthSubject: "caio@example.com"})
	if tag.Surface != "editor" || tag.Person != "caio@example.com" {
		t.Errorf("tag: %+v", tag)
	}
	tag = QueryTagFor("/api/query/execute", &SessionInfo{ClickhouseUser: "alice"})
	if tag.Person != "alice" {
		t.Errorf("password login must be tagged with the ClickHouse user: %+v", tag)
	}
}
