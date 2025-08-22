# Distributed ClickHouse Support

CH-UI provides comprehensive support for distributed ClickHouse deployments, enabling you to manage cluster operations directly from the interface.

## Overview

Starting from version 1.5.30, CH-UI includes full support for distributed ClickHouse features:

- **ON CLUSTER operations** for tables and users
- **Cluster-aware table creation**
- **Distributed table engine support**
- **Multi-node query execution**

## Enabling Distributed Mode

### Via Settings Page

1. Navigate to the Settings page in CH-UI
2. Enable "Distributed Mode"
3. Specify your cluster name
4. Save settings

### Features Enabled

When Distributed Mode is active:
- Table creation includes "ON CLUSTER" option
- User management operations support cluster-wide changes
- Distributed engine becomes available in table creation
- Cluster-specific metrics are displayed

## Working with Clusters

### Creating Tables on Cluster

When creating a table with Distributed Mode enabled:

1. Check the "ON CLUSTER" option
2. Select your cluster from the dropdown
3. Choose the appropriate engine (ReplicatedMergeTree, Distributed, etc.)

Example DDL generated:
```sql
CREATE TABLE IF NOT EXISTS my_table ON CLUSTER my_cluster
(
    id UInt64,
    name String,
    created DateTime
)
ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/my_table', '{replica}')
ORDER BY id;
```

### Creating Distributed Tables

For distributed tables across shards:

```sql
CREATE TABLE my_table_distributed ON CLUSTER my_cluster
AS my_table
ENGINE = Distributed(my_cluster, default, my_table, rand());
```

### User Management on Cluster

Create users across all nodes:

```sql
CREATE USER 'new_user' ON CLUSTER my_cluster
IDENTIFIED BY 'password'
DEFAULT DATABASE default
SETTINGS max_memory_usage = 10000000000;
```

Grant permissions cluster-wide:

```sql
GRANT SELECT ON *.* TO new_user ON CLUSTER my_cluster;
```

## Cluster Configuration Examples

### Basic Two-Shard Setup

```xml
<clickhouse>
    <remote_servers>
        <my_cluster>
            <shard>
                <replica>
                    <host>clickhouse-01</host>
                    <port>9000</port>
                </replica>
            </shard>
            <shard>
                <replica>
                    <host>clickhouse-02</host>
                    <port>9000</port>
                </replica>
            </shard>
        </my_cluster>
    </remote_servers>
</clickhouse>
```

### Replicated Setup

```xml
<clickhouse>
    <remote_servers>
        <my_cluster>
            <shard>
                <replica>
                    <host>clickhouse-01</host>
                    <port>9000</port>
                </replica>
                <replica>
                    <host>clickhouse-02</host>
                    <port>9000</port>
                </replica>
            </shard>
            <shard>
                <replica>
                    <host>clickhouse-03</host>
                    <port>9000</port>
                </replica>
                <replica>
                    <host>clickhouse-04</host>
                    <port>9000</port>
                </replica>
            </shard>
        </my_cluster>
    </remote_servers>
</clickhouse>
```

## Best Practices

### Table Design

1. **Use ReplicatedMergeTree** for data redundancy
2. **Create Distributed tables** for query routing
3. **Partition strategically** to optimize query performance

### Cluster Operations

1. **Always use ON CLUSTER** for DDL operations
2. **Monitor replication lag** between nodes
3. **Plan maintenance windows** for cluster-wide changes

### Performance Optimization

1. **Shard by high-cardinality columns**
2. **Keep frequently joined data on same shard**
3. **Use local tables for dimension data**

## Docker Compose Example

Complete distributed ClickHouse setup with CH-UI:

```yaml
version: '3.8'

services:
  clickhouse-01:
    image: clickhouse/clickhouse-server
    hostname: clickhouse-01
    volumes:
      - ./config/clickhouse-01:/etc/clickhouse-server/config.d
      - clickhouse-01-data:/var/lib/clickhouse
    networks:
      - clickhouse-network

  clickhouse-02:
    image: clickhouse/clickhouse-server
    hostname: clickhouse-02
    volumes:
      - ./config/clickhouse-02:/etc/clickhouse-server/config.d
      - clickhouse-02-data:/var/lib/clickhouse
    networks:
      - clickhouse-network

  ch-ui:
    image: ghcr.io/caioricciuti/ch-ui:latest
    ports:
      - "5521:5521"
    environment:
      VITE_CLICKHOUSE_URL: "http://clickhouse-01:8123"
      VITE_CLICKHOUSE_USER: "default"
      VITE_CLICKHOUSE_PASS: ""
    networks:
      - clickhouse-network
    depends_on:
      - clickhouse-01
      - clickhouse-02

networks:
  clickhouse-network:

volumes:
  clickhouse-01-data:
  clickhouse-02-data:
```

## Monitoring Distributed Systems

### Cluster Health Queries

Check cluster status:
```sql
SELECT * FROM system.clusters WHERE cluster = 'my_cluster';
```

Monitor replication:
```sql
SELECT 
    database,
    table,
    is_leader,
    total_replicas,
    active_replicas
FROM system.replicas;
```

### Performance Metrics

Track distributed query performance:
```sql
SELECT 
    query,
    query_duration_ms,
    read_rows,
    read_bytes,
    result_rows
FROM system.query_log
WHERE type = 'QueryFinish'
    AND has(tables, 'my_table_distributed')
ORDER BY query_start_time DESC
LIMIT 10;
```

## Troubleshooting

### Common Issues

#### Tables Not Created on All Nodes

**Problem**: DDL executed without ON CLUSTER
**Solution**: Always use ON CLUSTER for distributed operations

#### Replication Lag

**Problem**: Data inconsistency between replicas
**Solution**: Check network connectivity and ZooKeeper health

#### Query Performance

**Problem**: Slow distributed queries
**Solution**: Optimize sharding key and check data distribution

### Debugging Commands

Check connectivity:
```sql
SELECT * FROM system.clusters;
```

Verify replication:
```sql
SELECT * FROM system.replication_queue;
```

Monitor mutations:
```sql
SELECT * FROM system.mutations WHERE is_done = 0;
```

## Migration Guide

### From Standalone to Distributed

1. **Backup existing data**
2. **Setup cluster configuration**
3. **Create replicated tables with ON CLUSTER**
4. **Migrate data using INSERT SELECT**
5. **Create distributed tables**
6. **Update CH-UI settings for distributed mode**

### Example Migration

```sql
-- Create replicated table
CREATE TABLE my_table_replicated ON CLUSTER my_cluster
AS my_table_local
ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/my_table', '{replica}')
ORDER BY id;

-- Copy data
INSERT INTO my_table_replicated SELECT * FROM my_table_local;

-- Create distributed table
CREATE TABLE my_table_distributed ON CLUSTER my_cluster
AS my_table_replicated
ENGINE = Distributed(my_cluster, default, my_table_replicated, rand());
```

## Related Documentation

- [Getting Started](/getting-started) - Initial setup
- [Environment Variables](/environment-variables) - Configuration options
- [Permissions Guide](/permissions) - User access control
- [Troubleshooting](/troubleshooting) - Common issues