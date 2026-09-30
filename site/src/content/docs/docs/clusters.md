---
title: ClickHouse Clusters & Load Balancers
description: Sticky routing with X-CH-UI-Session for multi-node ClickHouse deployments behind chproxy, HAProxy, or Kubernetes
---

If your ClickHouse runs as a single server, you can skip this page: CH-UI works out of the box. This page is for deployments where CH-UI talks to a **load balancer in front of multiple ClickHouse pods**: chproxy, HAProxy, an nginx ingress, a Kubernetes Service, etc.

## The problem

ClickHouse's `system.*` tables are **node-local**. `system.query_log`, `system.processes`, `system.metrics`, `system.parts`: each only contains data for the specific pod that handled that request.

Without sticky routing, every page refresh in CH-UI can hit a different pod. Query history flickers between pods. Running queries vanish. Part counts contradict each other. The UI becomes unusable.

The fix is **session affinity**: every request from a given CH-UI session must land on the same pod.

## How CH-UI signals affinity

Every request CH-UI sends to ClickHouse carries this header:

```
X-CH-UI-Session: ch-ui-<32 hex>
```

The value is a stable per-process identifier, generated once when the CH-UI server (direct connections) or the remote tunnel agent (tunneled connections) starts, sent verbatim on every subsequent request. Configure your load balancer to hash on this header for upstream selection.

### Why not ClickHouse's native `session_id`?

ClickHouse's built-in `session_id` URL parameter would seem like the obvious fit, but it triggers a **server-side mutex**. Only one query can run at a time per session. A dashboard with 8 concurrent panel queries deadlocks instantly with `SESSION_IS_LOCKED`.

The custom header gives load balancers something to hash on **without** invoking ClickHouse's session locking. Pods serve concurrent queries normally; only the routing is sticky.

## Load balancer configuration

### chproxy

```ini
[server.http]
listen_addr = ":8123"

[server.proxy]
# Hash on the session header for sticky upstream selection.
[server.proxy.hash]
header_name = "X-CH-UI-Session"

[[users]]
name = "chui"
password = "..."
to_cluster = "ch_cluster"
```

### HAProxy

```ini
frontend ch_in
    bind *:8123
    default_backend ch_pool

backend ch_pool
    balance roundrobin
    # Stick on the session header: store hash for 1h.
    stick-table type string len 64 size 5k expire 1h
    stick on req.hdr(X-CH-UI-Session)
    server ch01 10.0.0.11:8123 check
    server ch02 10.0.0.12:8123 check
    server ch03 10.0.0.13:8123 check
```

### Kubernetes: nginx ingress

The default `sessionAffinity: ClientIP` on a `Service` does **not** work for connections that go through a tunnel agent, because all traffic arrives from the agent's single IP. Use an L7 ingress instead:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: clickhouse
  annotations:
    # Hash on the session header. Header → variable: lowercase + dashes→underscores → prefix $http_
    nginx.ingress.kubernetes.io/upstream-hash-by: "$http_x_ch_ui_session"
spec:
  rules:
    - host: clickhouse.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: clickhouse
                port: { number: 8123 }
```

### Kubernetes: Traefik

```yaml
apiVersion: traefik.containo.us/v1alpha1
kind: Middleware
metadata:
  name: chui-affinity
spec:
  plugin:
    headerHash:
      header: X-CH-UI-Session
```

(or use a `ServersTransport` with `disableHTTP2: false` plus the [`stickyCookie`](https://doc.traefik.io/traefik/routing/services/#sticky-sessions) approach if you don't have a hashing plugin available: fall back to a cookie strategy and copy the value from the header in middleware).

## How to verify it's working

1. **Sidebar**: the bottom of the CH-UI sidebar shows the hostname of the pod currently serving your session. Refresh a few times. The hostname should not change.
2. **Admin → Overview → Cluster Topology**: shows the full cluster: every shard, every replica, host + address + port. The pod serving you has a green dot in the `Local` column.
3. **Backend log on the LB**: confirm the hash header is being read. For chproxy: `--log-level=debug` shows the selected upstream per request. For HAProxy: `show table mytable` shows the stick-table populated.

## What you see if it's not working

- Hostname in the sidebar changes between page loads → sticky routing isn't active. The header is being sent (always is), but the LB isn't hashing on it.
- Dashboards show inconsistent counts on refresh → same root cause.
- `SESSION_IS_LOCKED` errors → you've configured ClickHouse's native `session_id` somewhere instead of (or in addition to) the header. Remove that and use only the header.

## Single-node setups

If you're on a single ClickHouse server, the header is still sent. ClickHouse ignores unknown headers, so it's a no-op. The sidebar shows the hostname; the Admin cluster topology shows "Single-node setup detected". Nothing to configure.

## See also

- [Deployment](/docs/deployment): production topology
- [Admin](/docs/admin): where the cluster topology view lives
- [Troubleshooting](/docs/troubleshooting): common multi-node issues
