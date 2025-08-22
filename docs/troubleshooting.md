# Troubleshooting

This guide helps you resolve common issues when using CH-UI.

## Common Issues

### Environment Variables Not Working

#### Problem
Environment variables aren't being applied when running CH-UI in Docker.

#### Solution

1. **Verify you're using the latest image**:
```bash
docker pull ghcr.io/caioricciuti/ch-ui:latest
```

2. **Check Docker logs**:
```bash
docker logs ch-ui
```
The logs will show which variables are SET/NOT SET.

3. **Verify variables are set in the container**:
```bash
docker exec ch-ui env | grep VITE_
```

4. **Ensure correct format**:
```yaml
# Correct
environment:
  VITE_CLICKHOUSE_URL: "http://clickhouse:8123"
  
# Wrong (missing VITE_ prefix)
environment:
  CLICKHOUSE_URL: "http://clickhouse:8123"
```

### Reverse Proxy Issues

#### Assets Not Loading (404 Errors)

**Problem**: CSS, JavaScript, or other static files return 404 errors.

**Solution**:
1. Ensure `VITE_BASE_PATH` matches your proxy location:
```yaml
# If your proxy location is /ch-ui/
environment:
  VITE_BASE_PATH: "/ch-ui"  # No trailing slash
```

2. Check browser console for exact paths being requested
3. Verify nginx/Apache configuration matches the base path

#### Blank Page After Proxy Setup

**Problem**: CH-UI loads but shows a blank page.

**Solution**:
```nginx
# Ensure these headers are set
proxy_set_header Host $host;
proxy_set_header X-Real-IP $remote_addr;
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
proxy_set_header X-Forwarded-Proto $scheme;
```

### Connection Issues

#### Cannot Connect to ClickHouse

**Problem**: "Connection refused" or "Failed to fetch" errors.

**Solutions**:

1. **Check ClickHouse is running**:
```bash
curl http://your-clickhouse:8123/ping
```

2. **Verify URL format**:
```yaml
# Correct - includes protocol
VITE_CLICKHOUSE_URL: "http://clickhouse:8123"

# Wrong - missing protocol
VITE_CLICKHOUSE_URL: "clickhouse:8123"
```

3. **Docker networking issues**:
```yaml
# Use host IP instead of container name for external ClickHouse
VITE_CLICKHOUSE_URL: "http://192.168.1.100:8123"

# Or use host.docker.internal on Docker Desktop
VITE_CLICKHOUSE_URL: "http://host.docker.internal:8123"
```

#### Authentication Failed

**Problem**: "Authentication failed" error despite correct credentials.

**Solutions**:

1. **Check password special characters**:
```yaml
# Escape special characters or use quotes
VITE_CLICKHOUSE_PASS: "p@$$w0rd!"
```

2. **Verify user permissions in ClickHouse**:
```sql
SHOW GRANTS FOR 'your_user';
```

3. **Test with clickhouse-client**:
```bash
clickhouse-client --host your-host --user your_user --password
```

### Query Execution Issues

#### Queries Timing Out

**Problem**: Long-running queries fail with timeout errors.

**Solution**:
1. **Increase request timeout**:
```yaml
environment:
  VITE_CLICKHOUSE_REQUEST_TIMEOUT: "120000"  # 2 minutes
```

2. **Increase proxy timeout** (nginx):
```nginx
location /ch-ui/ {
    proxy_read_timeout 600s;
    proxy_send_timeout 600s;
}
```

#### Large Result Sets Cause Browser to Hang

**Problem**: Browser becomes unresponsive with large query results.

**Solutions**:
1. Add `LIMIT` to your queries
2. Use pagination in your queries
3. Export results instead of displaying in browser

### Column Names with Special Characters

#### Problem
Tables with column names containing dots, spaces, or special characters don't display correctly.

#### Solution
This is automatically handled in CH-UI v1.5.30+. For older versions:
1. Update to the latest version
2. Use backticks in manual queries:
```sql
SELECT `column.with.dots`, `column with spaces` FROM table
```

### Docker-Specific Issues

#### Container Exits Immediately

**Problem**: CH-UI container starts then immediately stops.

**Solutions**:

1. **Check logs**:
```bash
docker logs ch-ui
```

2. **Verify port availability**:
```bash
lsof -i :5521
```

3. **Check resource limits**:
```yaml
services:
  ch-ui:
    mem_limit: 512m  # Increase if needed
```

#### Permission Denied Errors

**Problem**: Container can't write to mounted volumes.

**Solution**:
```yaml
# Set user in docker-compose
services:
  ch-ui:
    user: "1000:1000"  # Match host user
```

### Build Issues (From Source)

#### Node Version Mismatch

**Problem**: Build fails with Node.js errors.

**Solution**:
```bash
# Check Node version
node --version  # Should be >= 20.x

# Use nvm to switch versions
nvm use 20
```

#### Dependencies Installation Fails

**Problem**: `npm install` fails with errors.

**Solution**:
```bash
# Clear cache and reinstall
rm -rf node_modules package-lock.json
npm cache clean --force
npm install
```

### Performance Issues

#### Slow UI Response

**Problem**: UI feels sluggish or unresponsive.

**Solutions**:

1. **Enable caching** (already enabled by default)
2. **Check network latency**:
```bash
ping your-clickhouse-server
```
3. **Reduce query complexity**
4. **Check ClickHouse server load**:
```sql
SELECT * FROM system.metrics WHERE metric LIKE '%CPU%';
```

#### High Memory Usage

**Problem**: CH-UI uses excessive browser memory.

**Solutions**:
1. Clear browser cache
2. Limit query result size
3. Close unused query tabs
4. Restart browser if needed

### Distributed ClickHouse Issues

#### ON CLUSTER Operations Fail

**Problem**: DDL with ON CLUSTER returns errors.

**Solutions**:

1. **Verify cluster configuration**:
```sql
SELECT * FROM system.clusters;
```

2. **Check ZooKeeper connectivity**:
```sql
SELECT * FROM system.zookeeper WHERE path = '/';
```

3. **Ensure all nodes are accessible**:
```bash
for node in node1 node2 node3; do
  curl http://$node:8123/ping
done
```

#### Replication Lag

**Problem**: Data not consistent across replicas.

**Solution**:
```sql
-- Check replication status
SELECT 
    database,
    table,
    is_leader,
    total_replicas,
    active_replicas,
    queue_size
FROM system.replicas;

-- Force sync if needed
SYSTEM SYNC REPLICA table_name;
```

## Debug Mode

### Enable Verbose Logging

For detailed debugging, run CH-UI with verbose logging:

```bash
# Development mode with debug output
npm run dev -- --debug
```

### Browser Developer Tools

1. Open Developer Tools (F12)
2. Check Console for errors
3. Monitor Network tab for failed requests
4. Check Application > Local Storage for cached data

### Check Container Health

```bash
# Health check
docker inspect ch-ui --format='{{.State.Health.Status}}'

# Resource usage
docker stats ch-ui

# Network connectivity
docker exec ch-ui ping -c 3 clickhouse-server
```

## Getting Help

If you can't resolve your issue:

1. **Search existing issues**: [GitHub Issues](https://github.com/caioricciuti/ch-ui/issues)
2. **Create a new issue** with:
   - CH-UI version
   - ClickHouse version
   - Docker/deployment method
   - Error messages
   - Steps to reproduce
3. **Join discussions**: [GitHub Discussions](https://github.com/caioricciuti/ch-ui/discussions)

### Collecting Debug Information

```bash
# System information
docker version
docker-compose version

# CH-UI information
docker inspect ch-ui | grep -i version

# ClickHouse version
docker exec clickhouse clickhouse-client --query "SELECT version()"

# Environment variables (sanitized)
docker exec ch-ui env | grep VITE_ | sed 's/PASS=.*/PASS=***/'

# Logs
docker logs ch-ui --tail 100
```

## Quick Fixes Reference

| Issue | Quick Fix |
|-------|-----------|
| Blank page | Check `VITE_BASE_PATH` matches proxy |
| 404 on assets | Remove trailing slash from base path |
| Can't connect | Include `http://` in `VITE_CLICKHOUSE_URL` |
| Timeout errors | Increase `VITE_CLICKHOUSE_REQUEST_TIMEOUT` |
| Auth loops | Check proxy cookie settings |
| Slow queries | Add `LIMIT` clause to queries |
| Special characters | Update to v1.5.30+ |
| Container exits | Check port 5521 availability |

## Related Documentation

- [Getting Started](/getting-started) - Initial setup guide
- [Environment Variables](/environment-variables) - Complete configuration reference
- [Reverse Proxy](/reverse-proxy) - Proxy configuration
- [Permissions](/permissions) - ClickHouse access control
- [Distributed ClickHouse](/distributed-clickhouse) - Cluster setup