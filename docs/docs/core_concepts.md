---
sidebar_position: 3
---

# CH-UI Core

## 1. Application State Management

CH-UI utilizes Zustand for efficient and straightforward state management. The central state store forms the backbone of the application's data flow.

### Key Features:

- **ClickHouse Client Integration**: A ClickHouse client instance is maintained in the state, facilitating seamless communication with the ClickHouse server.
- **Credential Management**: User credentials for ClickHouse are securely stored and managed within the state.
- **Workspace Persistence**: The current state of all open tabs and workspaces is maintained, enabling a persistent and responsive user interface.

State updates are performed using actions defined in the store, allowing components to easily access and modify the application state. This centralized approach ensures consistency across the application and simplifies data flow management.

## 2. Workspace and Tabs

The CH-UI workspace is built around a flexible tab system, allowing users to manage multiple queries or views simultaneously.

### Key Features:

- **Dynamic Tab Creation**: Users can create new tabs for various content types (SQL queries, information views, etc.).
- **Tab Persistence**: Tabs are persisted using IndexedDB, allowing work recovery across sessions.
- **Tab Management**: Users can reorder, rename, and close tabs as needed.

This tab system provides a familiar and efficient interface for users to organize their work. It allows for multitasking and easy navigation between different queries or database views, enhancing productivity in database management tasks.

## 3. Database Explorer

The Database Explorer provides a hierarchical view of ClickHouse databases and tables, enhancing navigation and data discovery.

### Key Features:

- **Tree Structure**: Databases and tables are displayed in an expandable tree structure.
- **Search Functionality**: Users can search for specific databases or tables.
- **Real-time Refresh**: The explorer can be refreshed to fetch the latest database structure.

This component serves as the primary navigation tool for users to explore their ClickHouse databases. It provides an intuitive interface for users to understand the structure of their data and quickly access specific tables or databases.

## 4. Query Execution and Big Data Handling

Query execution is a core feature of CH-UI, designed to handle large datasets efficiently.

### Key Aspects:

- **Query Execution**: Queries are sent to the ClickHouse server using the `@clickhouse/client-web` library.
- **Result Processing**: Query results are processed and stored in the application state or IndexedDB, depending on their size.
- **Large Dataset Management**: CH-UI uses a combination of chunked data retrieval, IndexedDB storage, and virtualized rendering for efficient big data handling.

### Big Data Handling Strategies:

1. **Chunked Data Retrieval**:
   Large query results are retrieved in manageable chunks to prevent memory overflow. This allows the application to handle queries that return massive datasets without overwhelming the browser's memory.

2. **IndexedDB for Large Datasets**:
   Query results exceeding a size threshold are stored in IndexedDB instead of in-memory state. This strategy enables the application to work with datasets that are too large to fit in memory, providing a seamless experience even with big data.

3. **Virtualized Rendering**:
   Large datasets are rendered using virtualization techniques, displaying only the visible portion of the data. This significantly improves performance when dealing with tables containing thousands or millions of rows.

By implementing these strategies, CH-UI provides a robust solution for working with big data in a web-based ClickHouse client. Users can execute complex queries on large datasets and interact with the results smoothly, making it suitable for data analysis and exploration tasks on substantial ClickHouse databases.

These core concepts work together to create a powerful, efficient, and user-friendly interface for ClickHouse database management. The application's architecture ensures smooth handling of large datasets, responsive user interactions, and persistent workspace management, catering to the needs of data professionals working with ClickHouse databases.
