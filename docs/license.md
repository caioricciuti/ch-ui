# CH-UI Licensing

CH-UI uses a dual-license model: open source core + commercial Pro modules.

---

## CH-UI Core (Community Edition)

**License:** [Apache License 2.0](../LICENSE)

The core of CH-UI is free and open source. This includes:

- SQL Editor
- Data Explorer
- Saved Queries
- Single ClickHouse connection
- Tunnel connector (`ch-ui connect`)
- Embedded web frontend
- All CLI commands

You can use, modify, and distribute CH-UI Core freely under the Apache 2.0 license.

## CH-UI Pro

**License:** Commercial (proprietary)

Pro modules extend CH-UI with advanced features:

- Dashboards & panel builder
- Scheduled query jobs
- Brain AI assistant
- Governance, access, and policy controls
- Admin panel
- Multi-connection management

Pro features require a valid license file. Licenses are per-deployment and include a customer name, expiration date, and feature set.

### How to activate

1. Open CH-UI in your browser
2. Go to **Settings > License**
3. Paste or upload your license file
4. Pro features unlock immediately

### How to get a license

Visit [ch-ui.com/pricing](https://ch-ui.com/pricing) or contact **c.ricciuti@ch-ui.com**.

## License boundary

The licensing boundary is clear and scope-separated:

- **Core files** (Apache 2.0): everything in the repository except Pro module handlers
- **Pro module handlers**: `internal/server/handlers/` for dashboards, schedules, brain, admin, governance — these are proprietary
- **Frontend Pro components**: UI components gated behind the Pro license check

The Pro license check is enforced both server-side (HTTP 402 middleware) and client-side (UI gate).

## FAQ

**Can I use CH-UI Core in production?**
Yes, freely. Apache 2.0 allows commercial use.

**Can I modify CH-UI Core?**
Yes. You must retain the copyright notice and license.

**Do I need Pro for a single ClickHouse connection?**
No. Core supports one connection out of the box.

**What happens when a Pro license expires?**
Pro features become locked. Core features continue working. Your data is never lost.
