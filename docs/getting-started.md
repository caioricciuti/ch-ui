# Getting Started

Welcome to CH-UI! This guide will help you get up and running quickly with our modern interface for ClickHouse databases.

## Quick Start ⚡

Choose your preferred installation method:

## Docker (Recommended)

### Simple Docker Setup

```bash
docker run --name ch-ui -p 5521:5521 ghcr.io/caioricciuti/ch-ui:latest
```

### Docker with Environment Variables

```bash
docker run --name ch-ui -p 5521:5521 \
  -e VITE_CLICKHOUSE_URL=http://your-clickhouse-server:8123 \
  -e VITE_CLICKHOUSE_USER=your-username \
  -e VITE_CLICKHOUSE_PASS=your-password \
  ghcr.io/caioricciuti/ch-ui:latest
```

## Docker Compose

### Complete Example with ClickHouse

Here's a complete example of running both CH-UI and ClickHouse in the same Docker Compose file:

```yaml
services:
  clickhouse:
    image: clickhouse/clickhouse-server
    environment:
      CLICKHOUSE_USER: default
      CLICKHOUSE_DB: my-clickhouse-db
    volumes:
      # Store data to HDD
      - ./clickhouse-data:/var/lib/clickhouse/
      # Base Clickhouse cfg
      - ./clickhouse/config.xml:/etc/clickhouse-server/config.d/config.xml
      - ./clickhouse/users.xml:/etc/clickhouse-server/users.d/users.xml
    ports:
      - "8123:8123/tcp"
      - "9000:9000/tcp"

  ch-ui:
    image: ghcr.io/caioricciuti/ch-ui:latest
    environment:
      VITE_CLICKHOUSE_URL: http://my-docker-host-ip-or-fqdn:8123
      VITE_CLICKHOUSE_USER: default
      VITE_CLICKHOUSE_PASS: ""
    ports:
      - "5521:5521/tcp"
```

::: tip Important Configuration Notes
1. The ClickHouse port `8123` is published to make it accessible on the Docker host
2. For `VITE_CLICKHOUSE_URL`, use your Docker host's IP address or FQDN instead of the internal Docker network name
:::

### Basic Docker Compose

For a simpler setup without ClickHouse:

```yaml
services:
  ch-ui:
    image: ghcr.io/caioricciuti/ch-ui:latest
    restart: always
    ports:
      - "${CH_UI_PORT:-5521}:5521"
    environment:
      # Core Configuration
      VITE_CLICKHOUSE_URL: "${CLICKHOUSE_URL}"
      VITE_CLICKHOUSE_USER: "${CLICKHOUSE_USER}"
      VITE_CLICKHOUSE_PASS: "${CLICKHOUSE_PASS}"
      
      # Advanced Options (Optional)
      VITE_CLICKHOUSE_USE_ADVANCED: "${CLICKHOUSE_USE_ADVANCED:-false}"
      VITE_CLICKHOUSE_CUSTOM_PATH: "${CLICKHOUSE_CUSTOM_PATH:-}"
      VITE_CLICKHOUSE_REQUEST_TIMEOUT: "${CLICKHOUSE_REQUEST_TIMEOUT:-30000}"
      
      # Deployment Options (Optional)
      VITE_BASE_PATH: "${BASE_PATH:-/}"
```

Start the service:

```bash
docker-compose up -d
```

## Build from Source

### Clone Repository

```bash
git clone https://github.com/caioricciuti/ch-ui.git
cd ch-ui
```

### Install Dependencies

```bash
npm install
```

### Build Project

```bash
npm run build
```

### Start Server

For production:

```bash
npm run preview
```

For development:

```bash
npm run dev
```

## System Requirements 🖥️

### Prerequisites

- A running ClickHouse server ([Installation Guide](https://clickhouse.com/docs/en/install#quick-install))
- For Docker: Docker Engine 20.10.0 or newer
- For building from source:
  - Node.js >= 20.x
  - npm >= 10.x

## Configuration Options ⚙️

### Environment Variables

| Variable | Description | Required | Default | Since |
|----------|-------------|----------|---------|-------|
| **Core Configuration** |
| `VITE_CLICKHOUSE_URL` | ClickHouse server URL | Yes | - | v1.0.0 |
| `VITE_CLICKHOUSE_USER` | ClickHouse username | Yes | - | v1.0.0 |
| `VITE_CLICKHOUSE_PASS` | ClickHouse password | No | `""` | v1.0.0 |
| **Advanced Features** |
| `VITE_CLICKHOUSE_USE_ADVANCED` | Enable advanced ClickHouse features (e.g., custom settings, system tables access) | No | `false` | v1.4.0 |
| `VITE_CLICKHOUSE_CUSTOM_PATH` | Custom path for ClickHouse HTTP interface | No | - | v1.4.0 |
| `VITE_CLICKHOUSE_REQUEST_TIMEOUT` | Request timeout in milliseconds | No | `30000` | v1.4.0 |
| **Deployment Configuration** |
| `VITE_BASE_PATH` | Base path for reverse proxy deployment (e.g., "/ch-ui") | No | `/` | v1.5.30 |

For detailed environment variable documentation, see our [Environment Variables Reference](/environment-variables).

## Development Environment 🛠️

### Local ClickHouse Instance

Run a local ClickHouse instance for development:

```bash
# Start ClickHouse
docker-compose -f docker-compose-dev.yml up -d

# Stop ClickHouse
docker-compose -f docker-compose-dev.yml down
```

Default credentials:

- URL: http://localhost:8123
- Username: dev
- Password: dev

::: info
Data is persisted in `.clickhouse_local_data` directory.
:::

## Security Recommendations 🔒

### Reverse Proxy Setup

- Use Nginx/Apache as a reverse proxy
- Enable HTTPS
- Implement authentication

### Network Security

- Run on a private network when possible
- Use VPN for remote access
- Implement IP whitelisting

### Access Control

- Use minimal privilege ClickHouse users
- Regularly rotate credentials
- Monitor access logs

For detailed permission requirements, see our [Permissions Guide](/permissions).

## Next Steps

- [Configure ClickHouse permissions](/permissions) for CH-UI access
- [Contribute](/contributing) to the project
- Check the [Changelog](https://github.com/caioricciuti/ch-ui/releases) for latest updates

---

### Support the Project

If you find CH-UI helpful, consider:

<div style="text-align: center; margin: 2rem 0;">
  <a href="https://buymeacoffee.com/caioricciuti" target="_blank">
    <img src="https://img.buymeacoffee.com/button-api/?text=Buy me a coffee&emoji=&slug=caioricciuti&button_colour=FF813F&font_colour=ffffff&font_family=Cookie&outline_colour=000000&coffee_colour=FFDD00" alt="Buy Me A Coffee" />
  </a>
</div>