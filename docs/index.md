---
layout: home

hero:
  name: "Data is better when we see it!"
  tagline: "CH-UI makes working with data easy. This UI connects you directly to your ClickHouse instance, allowing you to view, filter, and export your data with ease."
  image:
    src: /logo.png
    alt: CH-UI
  actions:
    - theme: brand
      text: Get Started
      link: /getting-started
    - theme: alt
      text: View on GitHub
      link: https://github.com/caioricciuti/ch-ui

features:
  - title: SQL Editor
    details: Advanced editor with IntelliSense, syntax highlighting, and query history tracking

  - title: Database Explorer
    details: Browse databases, tables, and columns with support for special characters

  - title: Query Tabs
    details: Work with multiple queries simultaneously using a familiar tab interface

  - title: Metrics Dashboard
    details: Monitor query performance, table statistics, and system metrics

  - title: Distributed Support
    details: Full support for ON CLUSTER operations and distributed table management

  - title: Reverse Proxy Ready
    details: Deploy behind nginx/Apache with custom base paths using VITE_BASE_PATH
---

## Screenshots

<ScreenshotGallery />

## Quick Installation

### Docker

```bash
docker run --name ch-ui -p 5521:5521 \
  -e VITE_CLICKHOUSE_URL=http://your-clickhouse:8123 \
  -e VITE_CLICKHOUSE_USER=default \
  -e VITE_CLICKHOUSE_PASS=password \
  ghcr.io/caioricciuti/ch-ui:latest
```

### Docker Compose

```yaml
services:
  ch-ui:
    image: ghcr.io/caioricciuti/ch-ui:latest
    ports:
      - "5521:5521"
    environment:
      VITE_CLICKHOUSE_URL: http://clickhouse:8123
      VITE_CLICKHOUSE_USER: default
      VITE_CLICKHOUSE_PASS: ""
      # Optional: Advanced features
      VITE_CLICKHOUSE_USE_ADVANCED: "false"
      VITE_CLICKHOUSE_REQUEST_TIMEOUT: "30000"
      # Optional: For reverse proxy deployment
      VITE_BASE_PATH: "/"
```

[Full Installation Guide →](/getting-started)

## Requirements

- ClickHouse server (version 21.x or higher)
- Modern web browser (Chrome 88+, Firefox 79+, Safari 14+)
- Docker (for containerized deployment) or Node.js 20+ (for building from source)

## Sponsors

We would like to thank our sponsors for their support:

### [Ibero Data](https://www.iberodata.es/?utm_source=ch-ui&utm_medium=docs)

Empowering businesses with data-driven solutions

[**Become a Sponsor →**](mailto:caio.ricciuti+sponsorship@outlook.com?subject=CH-UI%20Sponsorship%20Inquiry)

## Support

- 📖 [Documentation](/getting-started)
- 🐛 [Report Issues](https://github.com/caioricciuti/ch-ui/issues)
- 💬 [Discussions](https://github.com/caioricciuti/ch-ui/discussions)
- ⭐ [Star on GitHub](https://github.com/caioricciuti/ch-ui)

## License

CH-UI is open source software [licensed under Apache 2.0](/license).

---

[![Buy Me A Coffee](https://img.buymeacoffee.com/button-api/?text=Buy%20me%20a%20coffee&emoji=&slug=caioricciuti&button_colour=FF813F&font_colour=ffffff&font_family=Cookie&outline_colour=000000&coffee_colour=FFDD00)](https://buymeacoffee.com/caioricciuti)
