# CH-UI plugin for Claude Code

Bundles the CH-UI MCP server connection and a skill that teaches Claude the
schema-first ClickHouse analytics workflow (search the catalog, describe
before querying, estimate before scanning, prefer verified saved queries).

## Install

```bash
export CH_UI_URL=https://your-ch-ui.example.com
export CH_UI_MCP_KEY=chm_...   # from Admin → MCP Server; keep it out of files

claude plugin marketplace add caioricciuti/ch-ui
claude plugin install ch-ui@ch-ui
```

The MCP server entry reads both variables at load time, so the key never
lands in a config file. Any MCP client can use the server without the plugin;
see [docs/mcp.md](../../docs/mcp.md).

## What is inside

- `.mcp.json`: the `ch-ui` MCP server over streamable HTTP with a bearer key.
- `skills/clickhouse-analytics/SKILL.md`: when and how to use each CH-UI tool,
  ClickHouse SQL specifics that trip up models, and a cost-aware query loop.
