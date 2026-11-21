# 0003. 浏览器生命周期以 BrowserFacade 为真源

| 字段 | 值 |
|------|-----|
| Status | accepted |
| Date | 2026-06-01 |
| Area | browser |

## Context

曾并存 `BrowserManager`（阻塞单实例）与 Supervisor（多实例）。UI 还有自建 `processes map`，导致列表空、端口失控。

## Decision

- **BrowserFacade** = Supervisor + LeaseManager + PortAllocator  
- CLI / UI 启停浏览器一律经 Facade  
- legacy Manager 仅兼容，禁止新旁路  

## Consequences

### 正

- 端口、租约、runtime 状态单一事实来源  
- 多开与互斥可测  

### 负 / 代价

- 文档与命令名需持续澄清  
- CLI 短进程模型下 heartbeat 受限（见 L009）  

## Related

- Lessons: L001, L009  
- Code: `internal/browser/facade.go`
