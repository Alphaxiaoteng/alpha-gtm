# L008: 并发写 SQLite 报 database is locked

| 字段 | 值 |
|------|-----|
| Status | resolved |
| Area | db |
| Severity | medium |
| Date | 2026-06-22 |

## Symptoms

启动浏览器或后台轮询时：`database is locked (5) (SQLITE_BUSY)`。

## Root cause

多 goroutine 同时写；默认无 busy 等待；非 WAL 时读写互斥更狠。

## Fix

打开 DB 后：

```sql
PRAGMA busy_timeout = 5000;
PRAGMA journal_mode = WAL;
```

## Prevention

- 新开 DB 连接路径必须套同一 pragma
- 长事务尽快提交；避免在持锁时做网络/CDP

## Related

- Code: `internal/db/db.go`
