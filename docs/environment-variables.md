# Environment Variables

This page provides a complete reference for all environment variables supported by CH-UI.

## Overview

CH-UI uses environment variables for configuration, allowing you to customize the application without rebuilding the Docker image. All variables are injected at runtime, making deployment flexible and secure.

## Variable Reference

### Core Configuration

These variables are essential for basic CH-UI operation.

#### VITE_CLICKHOUSE_URL
- **Description**: The URL of your ClickHouse server HTTP interface
- **Required**: Yes
- **Default**: None
- **Example**: `http://localhost:8123`
- **Since**: v1.0.0

```bash
docker run -e VITE_CLICKHOUSE_URL=http://clickhouse.example.com:8123 ...
```

#### VITE_CLICKHOUSE_USER
- **Description**: Username for ClickHouse authentication
- **Required**: Yes
- **Default**: None
- **Example**: `default`, `ch_ui_user`
- **Since**: v1.0.0

#### VITE_CLICKHOUSE_PASS
- **Description**: Password for ClickHouse authentication
- **Required**: No
- **Default**: Empty string (`""`)
- **Example**: `your-secure-password`
- **Since**: v1.0.0

### Advanced Features

These variables enable additional functionality and customization.

#### VITE_CLICKHOUSE_USE_ADVANCED
- **Description**: Enable advanced ClickHouse features such as custom settings and system tables access
- **Required**: No
- **Default**: `false`
- **Values**: `true` or `false`
- **Since**: v1.4.0

When enabled, CH-UI will:
- Allow access to system tables
- Enable custom query settings
- Show advanced configuration options

#### VITE_CLICKHOUSE_CUSTOM_PATH
- **Description**: Custom HTTP path for ClickHouse interface (useful for proxied setups)
- **Required**: No (required if `VITE_CLICKHOUSE_USE_ADVANCED` is `true`)
- **Default**: None
- **Example**: `/custom/clickhouse/path`
- **Since**: v1.4.0

#### VITE_CLICKHOUSE_REQUEST_TIMEOUT
- **Description**: HTTP request timeout in milliseconds
- **Required**: No
- **Default**: `30000` (30 seconds)
- **Example**: `60000` (60 seconds)
- **Since**: v1.4.0

Useful for:
- Long-running queries
- Slow network connections
- Large data transfers

### Deployment Configuration

These variables help with specific deployment scenarios.

#### VITE_BASE_PATH
- **Description**: Base path for deploying CH-UI behind a reverse proxy
- **Required**: No
- **Default**: `/`
- **Example**: `/ch-ui`, `/analytics/clickhouse`
- **Since**: v1.5.30

Used when deploying CH-UI at a subpath like `https://example.com/ch-ui/` instead of the root.

## Complete Configuration Examples

### Minimal Configuration

```yaml
environment:
  VITE_CLICKHOUSE_URL: "http://localhost:8123"
  VITE_CLICKHOUSE_USER: "default"
```

### Standard Production Setup

```yaml
environment:
  # Core
  VITE_CLICKHOUSE_URL: "http://clickhouse.prod.example.com:8123"
  VITE_CLICKHOUSE_USER: "ch_ui_user"
  VITE_CLICKHOUSE_PASS: "${SECURE_PASSWORD}"
  
  # Performance
  VITE_CLICKHOUSE_REQUEST_TIMEOUT: "60000"
```

### Advanced Setup with Reverse Proxy

```yaml
environment:
  # Core
  VITE_CLICKHOUSE_URL: "http://clickhouse-internal:8123"
  VITE_CLICKHOUSE_USER: "admin"
  VITE_CLICKHOUSE_PASS: "${ADMIN_PASSWORD}"
  
  # Advanced Features
  VITE_CLICKHOUSE_USE_ADVANCED: "true"
  VITE_CLICKHOUSE_CUSTOM_PATH: "/clickhouse"
  VITE_CLICKHOUSE_REQUEST_TIMEOUT: "120000"
  
  # Deployment
  VITE_BASE_PATH: "/ch-ui"
```

### Docker Run with All Variables

```bash
docker run --name ch-ui -p 5521:5521 \
  -e VITE_CLICKHOUSE_URL=http://clickhouse:8123 \
  -e VITE_CLICKHOUSE_USER=myuser \
  -e VITE_CLICKHOUSE_PASS=mypassword \
  -e VITE_CLICKHOUSE_USE_ADVANCED=true \
  -e VITE_CLICKHOUSE_CUSTOM_PATH=/custom/path \
  -e VITE_CLICKHOUSE_REQUEST_TIMEOUT=60000 \
  -e VITE_BASE_PATH=/ch-ui \
  ghcr.io/caioricciuti/ch-ui:latest
```

## Environment Variable Best Practices

### Security

1. **Never hardcode passwords** - Use environment files or secrets management
2. **Use .env files** for local development only
3. **Implement secrets rotation** for production environments

### Docker Compose with .env File

Create a `.env` file:
```env
CLICKHOUSE_URL=http://localhost:8123
CLICKHOUSE_USER=default
CLICKHOUSE_PASS=secure_password_here
```

Reference in `docker-compose.yml`:
```yaml
services:
  ch-ui:
    image: ghcr.io/caioricciuti/ch-ui:latest
    env_file: .env
    environment:
      VITE_CLICKHOUSE_URL: "${CLICKHOUSE_URL}"
      VITE_CLICKHOUSE_USER: "${CLICKHOUSE_USER}"
      VITE_CLICKHOUSE_PASS: "${CLICKHOUSE_PASS}"
```

### Kubernetes Secrets

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: ch-ui-secrets
type: Opaque
data:
  clickhouse-password: <base64-encoded-password>
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ch-ui
spec:
  template:
    spec:
      containers:
      - name: ch-ui
        image: ghcr.io/caioricciuti/ch-ui:latest
        env:
        - name: VITE_CLICKHOUSE_URL
          value: "http://clickhouse-service:8123"
        - name: VITE_CLICKHOUSE_USER
          value: "ch_ui_user"
        - name: VITE_CLICKHOUSE_PASS
          valueFrom:
            secretKeyRef:
              name: ch-ui-secrets
              key: clickhouse-password
```

## Troubleshooting

### Variables Not Applied

If environment variables aren't being applied:

1. Check Docker logs:
```bash
docker logs ch-ui
```

2. Verify variables are set:
```bash
docker exec ch-ui env | grep VITE_
```

3. Ensure you're using the latest image:
```bash
docker pull ghcr.io/caioricciuti/ch-ui:latest
```

### Common Issues

- **Wrong URL format**: Ensure `VITE_CLICKHOUSE_URL` includes protocol (`http://` or `https://`)
- **Timeout errors**: Increase `VITE_CLICKHOUSE_REQUEST_TIMEOUT` for slow queries
- **Base path issues**: Don't include trailing slash in `VITE_BASE_PATH`

## Version Compatibility

| CH-UI Version | New Variables Added |
|---------------|-------------------|
| v1.0.0 | `VITE_CLICKHOUSE_URL`, `VITE_CLICKHOUSE_USER`, `VITE_CLICKHOUSE_PASS` |
| v1.4.0 | `VITE_CLICKHOUSE_USE_ADVANCED`, `VITE_CLICKHOUSE_CUSTOM_PATH`, `VITE_CLICKHOUSE_REQUEST_TIMEOUT` |
| v1.5.30 | `VITE_BASE_PATH` |

## Related Documentation

- [Getting Started](/getting-started) - Quick setup guide
- [Reverse Proxy Setup](/reverse-proxy) - Configure CH-UI behind nginx/Apache
- [Distributed ClickHouse](/distributed-clickhouse) - Cluster configuration
- [Troubleshooting](/troubleshooting) - Common issues and solutions