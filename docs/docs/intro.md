---
sidebar_position: 1
title: Intro
---

### Let's discover **ch-ui**.

## What it does?

It makes your life and your data projects **easy** to manage and maintain (hopefully).

1. User Interface to interact with your data on a **ClickHouse instance**.
2. **Data visualization** with **charts** and **tables**.
3. **Query editor** to run SQL queries.
4. **Data management** with **tables** and **databases**.
5. **User management** with **roles** and **permissions**.
6. **Data import** and **export**.
7. **Data transformation** with **SQL**.
8. **Data monitoring** with **metrics**.
9. Everything else we need on the way - IT'S OPEN SOURCE, your idea can be implemented.

## Is it really necessary?

- [ClickHouse](https://clickhouse.com) is a powerful **OLAP** database, (the best in my opinion) but it relies on 3rd party Cloud to deploy and have the built-in UI (which is totally fine), they already make the engine open-source and free to use, which I'm personally grateful for.

## How it works?

### Client

- The client is built with **React** and **TypeScript**.

> Client - "Responsible for the user interface and user experience".

1. Nothing is stored on the client side other than the app state ( **Zustand** ).
2. Client communicates with the server through **REST API**. ( **React Query** ).
3. Shows the data in a user-friendly way and allows the user to interact with it.

### Server

- The server is built with **Node.js** and **Express**.

> Server - "Responsible for processing requests, handling business logic, and interacting with the database".

1. Manages user authentication and authorization. **JWT**
2. Exposes REST API endpoints for client interactions.
3. Connects to the ClickHouse database to execute queries and manage data. [ClickHouse's JS(NodeJs) Client ](https://clickhouse.com/docs/en/integrations/language-clients/javascript)
4. Handles data import and export operations. **CSV/JSON**
5. Manages user roles and permissions for secure access control.

### Data Flow

1. **User Request**: The user interacts with the client application (UI) and makes a request (e.g., to run a query or visualize data).
2. **Client to Server**: The client sends the request to the server via a REST API call.
3. **Server Processing**: The server processes the request, which involve querying the ClickHouse database or performing some data transformation via [ClickHouse's JS(NodeJs) Client ](https://clickhouse.com/docs/en/integrations/language-clients/javascript).
4. **Response**: The server sends the data back to the client.
5. **Data Display**: The client receives the data and displays it in a user-friendly format (e.g., charts, tables).

### Key Features

- **Query Editor**: Write and execute SQL queries directly from the UI.
- **User Management**: Control access with roles and permissions. ** within ch-ui** 
- **Data Management**: Import, export, and manage your data tables and databases.
- **Monitoring**: Track and visualize key metrics for your data and queries.

## Why choose ch-ui?

- **Open Source**: Customize and extend the platform to fit your needs.
- **User-Friendly**: Intuitive UI that makes it easy to interact with your data.
- **Powerful**: Leverage the full capabilities of ClickHouse with an enhanced user experience.
- **Community-Driven**: Contribute your ideas and improvements to the project.
