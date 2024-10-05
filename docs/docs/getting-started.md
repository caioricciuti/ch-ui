---
sidebar_position: 2
---

# Getting Started 🚀

Let's start with **CH-UI in less than 5 minutes**.

## Prerequisites

- A running ClickHouse server instance [ClickHouse Official Installation Guide](https://clickhouse.com/docs/en/install#quick-install)

## 🐳 Docker

You can deploy CH-UI in a few minutes using Docker. Just run the following command:

```bash
docker run -p 5521:5521 ghcr.io/caioricciuti/ch-ui:latest
```

We also have a docker-compose file that you can use to deploy CH-UI. Just create a `docker-compose.yml` file with the following content:

```yaml
services:
  ch-ui:
    image: ghcr.io/caioricciuti/ch-ui:latest
    restart: always
    ports:
      - "${CH_UI_PORT:-5521}:5521"
    environment:
      VITE_CLICKHOUSE_URL: "http://your-clickhouse-server:8123"
      VITE_CLICKHOUSE_USER: "your-username"
      VITE_CLICKHOUSE_PASS: "your-password"
```

And then run:

```bash
# use -d to run in detached mode
docker-compose up -d
```

Environment variables:

- `VITE_CLICKHOUSE_URL`: ClickHouse server URL (optional)
- `VITE_CLICKHOUSE_USER`: ClickHouse user (optional)
- `VITE_CLICKHOUSE_PASS`: ClickHouse password (optional)

## 💻 Build from source

You can also build CH-UI from source.

Clone the repository:

```bash
git clone https://github.com/caioricciuti/ch-ui.git
```

Install the dependencies:

```bash
cd ch-ui
npm install
```

Run the development server:

```bash
npm run dev
```

Open [http://localhost:5521](http://localhost:5521) in your browser.

## Project Structure

Here's an overview of the key directories and files in the project:

- `/src`: Main source directory
  - `/components`: Reusable React components
  - `/pages`: Main page components (Home, Metrics, Settings)
  - `/store`: State management with Zustand
  - `/lib`: Utility functions
  - `/types`: TypeScript type definitions
- `App.tsx`: Main application component
- `main.tsx`: Application entry point

## Next Steps

Once you have the application running, you can:

1. Connect to your ClickHouse server through the Settings page
2. Explore your databases and tables using the Database Explorer
3. Write and execute SQL queries in the SQL Editor
4. Organize your work using the Workspace Tabs

For more detailed information on each feature, please refer to the respective sections in this documentation.

## Contributing

If you want to contribute to CH-UI, please read the [contributing guide](/docs/contributing.md).
