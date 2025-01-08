# CH-UI 🚀

[![GitHub license](https://img.shields.io/github/license/caioricciuti/ch-ui)](https://github.com/caioricciuti/ch-ui/blob/main/LICENSE)
![Node.js Version](https://img.shields.io/badge/node-%3E%3D20.x-brightgreen)
![npm Version](https://img.shields.io/badge/npm-%3E%3D10.x-brightgreen)
[![Docker Image](https://img.shields.io/badge/docker-ghcr.io%2Fcaioricciuti%2Fch--ui-blue)](https://github.com/caioricciuti/ch-ui/pkgs/container/ch-ui)

![Docker Pulls](https://img.shields.io/badge/pulls-30.6k-blue?logo=docker&style=flat-square)

A modern, feature-rich web interface for ClickHouse databases. CH-UI provides an intuitive platform for managing ClickHouse databases, executing queries, and visualizing metrics about your instance.

## 🌟 Key Features

### Core Functionality
- **🔄 ClickHouse Integration**: Seamless connection and interaction with ClickHouse databases
- **📝 Advanced SQL Editor**: 
  - Intelligent IntelliSense with autocomplete suggestions
  - Syntax highlighting
  - Query history tracking
  - Multi-tab query execution
- **📊 Dynamic Data Visualization**: 
  - Interactive data tables with sorting and filtering
  - Custom visualization options
  - Real-time data updates

### Performance & Architecture
- **⚡ Optimized Performance**:
  - IndexedDB-based caching system
  - Efficient state management
  - Responsive UI even with large datasets
- **🔒 TypeScript Implementation**: Full TypeScript support for improved code quality and developer experience
- **📦 Custom Table Management**: Built-in table handling without third-party dependencies

### Monitoring & Analytics
- **📈 Enhanced Metrics Dashboard**:
  - Query performance monitoring
  - Table statistics and insights
  - System settings overview
  - Network performance metrics
  - Resource utilization tracking

### User Experience
- **🎨 Modern UI/UX**:
  - Clean, intuitive interface
  - Responsive design
  - Dark/Light mode support
  - Customizable layouts

## 📸 Screenshots

<div style="display: flex; justify-content: space-between; margin-bottom: 20px;">
  <img src="./public/screen-shots/settings.png" alt="Settings Interface" width="32%" />
  <img src="./public/screen-shots/main-page.png" alt="Main Dashboard" width="32%" />
  <img src="./public/screen-shots/instance-metrics.png" alt="Instance Metrics" width="32%" />
</div>

## 🚀 Getting Started

### Option 1: Docker (Recommended)

#### Simple Start
```bash
docker run -p 5521:5521 ghcr.io/caioricciuti/ch-ui:latest
```

#### Using Docker Compose
Create a `docker-compose.yml`:
```yaml
services:
  ch-ui:
    image: ghcr.io/caioricciuti/ch-ui:latest
    restart: always
    ports:
      - "5521:5521"
    environment:
      VITE_CLICKHOUSE_URL: "http://your-clickhouse-server:8123"
      VITE_CLICKHOUSE_USER: "your-username"
      VITE_CLICKHOUSE_PASS: "your-password"
```

Then run:
```bash
docker-compose up -d
```

#### Environment Variables
| Variable | Description | Required | Default |
|----------|-------------|-----------|---------|
| VITE_CLICKHOUSE_URL | ClickHouse server URL | Yes | - |
| VITE_CLICKHOUSE_USER | ClickHouse username | Yes | - |
| VITE_CLICKHOUSE_PASS | ClickHouse password | No | "" |
| VITE_CLICKHOUSE_USE_ADVANCED | Enable advanced ClickHouse features (e.g., custom settings, system tables access) | No | false |
| VITE_CLICKHOUSE_CUSTOM_PATH | Custom path for ClickHouse HTTP interface | No | - |

#### Advanced Docker Configuration
```yaml
services:
  ch-ui:
    image: ghcr.io/caioricciuti/ch-ui:latest
    restart: always
    ports:
      - "5521:5521"
    environment:
      VITE_CLICKHOUSE_URL: "http://your-clickhouse-server:8123"
      VITE_CLICKHOUSE_USER: "your-username"
      VITE_CLICKHOUSE_PASS: "your-password"
      # Advanced Options
      VITE_CLICKHOUSE_USE_ADVANCED: "true"  # Enable advanced features
      VITE_CLICKHOUSE_CUSTOM_PATH: "/custom/path"  # Optional: Custom HTTP path
```

Or using Docker run with advanced options:
```bash
docker run -p 5521:5521 \
  -e VITE_CLICKHOUSE_URL=http://your-clickhouse-server:8123 \
  -e VITE_CLICKHOUSE_USER=your-username \
  -e VITE_CLICKHOUSE_PASS=your-password \
  -e VITE_CLICKHOUSE_USE_ADVANCED=true \
  -e VITE_CLICKHOUSE_CUSTOM_PATH=/custom/path \
  ghcr.io/caioricciuti/ch-ui:latest
```

### Option 2: Build from Source

#### Prerequisites
- Node.js >= 20.x
- npm >= 10.x

#### Installation Steps
```bash
# Clone the repository
git clone https://github.com/caioricciuti/ch-ui.git

# Navigate to project directory
cd ch-ui

# Install dependencies
npm install

# Build the project
npm run build

# Start for development
npm run dev

# Start for production
npm run preview
```

## 🧪 Development Environment

### Local ClickHouse Instance
For development purposes, you can run a local ClickHouse instance using Docker:

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

Data is persisted in `.clickhouse_local_data` directory.

## 🔒 Security Recommendations

### Production Deployment
When deploying CH-UI in a production environment, consider the following security measures:

1. **Reverse Proxy Setup**
   - Use Nginx/Apache as a reverse proxy
   - Enable HTTPS
   - Implement authentication

2. **Network Security**
   - Run on a private network when possible
   - Use VPN for remote access
   - Implement IP whitelisting

### Example Nginx Configuration with Basic Auth
```nginx
server {
    listen 80;
    server_name your-domain.com;

    location / {
        auth_basic "Restricted Access";
        auth_basic_user_file /etc/nginx/.htpasswd;
        
        proxy_pass http://localhost:5521;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

## 📚 Documentation

![Website](https://img.shields.io/website?url=https%3A%2F%2Fch-ui.caioricciuti.com)


For detailed documentation, visit our [official documentation](https://ch-ui.caioricciuti.com/docs/getting-started?utm_source=ch-ui&utm_medium=gitHubReadme). 

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## ❤️ Sponsors

<div align="center">
  <a href="https://iberodata.es/?utm_source=ch-ui&utm_medium=github" target="_blank">
    <img src="https://iberodata.es/logo.png" alt="Iberodata" width="100"/>
  </a>
  
  <p><strong>Iberodata</strong> - Empowering businesses with data-driven solutions</p>
</div>

## ☕ Support the Project

If you find CH-UI helpful, consider supporting its development:

<div align="center">
  <a href="https://buymeacoffee.com/caioricciuti?utm_source=ch-ui&utm_medium=github">
    <img src="https://img.buymeacoffee.com/button-api/?text=Buy me a coffee&emoji=&slug=caioricciuti&button_colour=FF813F&font_colour=ffffff&font_family=Cookie&outline_colour=000000&coffee_colour=FFDD00" alt="Buy Me A Coffee" />
  </a>
</div>

Your support helps maintain and improve CH-UI! ✨

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](./LICENSE.md) file for details.