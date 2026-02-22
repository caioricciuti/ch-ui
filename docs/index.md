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

## Custom Branch Features

This custom branch includes significant enhancements beyond the main repository:

### SQL Editor Enhancements
- **Vim Mode**: Full Vim keybinding support with results table integration
- **Code Folding**: Collapse and expand SQL code blocks
- **Advanced Syntax Highlighting**: Window functions, compression codecs, aggregate types, DDL keywords, ALIAS keyword, projection support
- **Smart Autocomplete**: Context-aware suggestions with usage-based sorting and column name suggestions for ORDER BY

### EXPLAIN Query Visualization
- **Tree View**: Interactive node tree with dragging, right-click panning, and parent path highlighting
- **JSON & Text Views**: Multiple ways to view EXPLAIN output
- **Node Details Panel**: Click any node to see detailed information

### Comprehensive Permissions System
- **Hierarchical Permission Tree**: Visual tree-based permission management
- **Role Inheritance**: ClickHouse roles support (replaces legacy privilege presets)
- **Audit Logging**: Full audit trail of all permission changes
- **Export/Import**: Backup and restore permissions
- **Enhanced User Management**: Inline editing, grant pre-selection, improved permissions UX

### Enhanced Results Table
- **AG Grid Integration**: Advanced data grid with custom pagination
- **Transpose Rows**: Flip rows to columns for better data viewing
- **Value Sidebar**: Dedicated panel to view individual cell values
- **Query Statistics**: Display row count and query timing in pagination bar

### Appearance & Themes
- **Unified Theme System**: Monaco editor themes sync with UI
- **Popular Community Themes**: Tokyo Night, Gruvbox, Monokai, Nord, Dracula, Solarized, and more
- **Font Customization**: Font family and size selection
- **Theme-Specific Grid Styling**: Selected rows match your color scheme

### Multi-Connection Support
- **Multiple Connections**: Manage multiple ClickHouse connections
- **Per-Connection State**: Remember last-selected database per connection
- **Connection Switching**: Switch between connections without re-authentication

### Enhanced UX
- **Loading Skeletons**: Visual feedback during data loading
- **Toast Notifications**: Enhanced notifications with undo capability
- **Keyboard Navigation**: Full keyboard support in permissions UI
- **Accessibility**: Comprehensive ARIA labels and screen reader support
- **Error Boundaries**: Improved error handling with copy functionality

### Quality Assurance
- **E2E Tests**: Comprehensive Playwright tests for SQL editor features
- **Unit Tests**: Coverage for permissions, query parsing, SQL utilities

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
