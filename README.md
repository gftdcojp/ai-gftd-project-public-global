# Global Resource Flow Intelligence Platform

Global resource flow visualization platform. Aggregates worldwide resource statistics, infers time-series flows, and renders interactive visualizations.

## Live

- https://global.gftd.ai

## MCP Endpoint

```
POST https://actors.gftd.ai/{nanoid}/api/mcp
Content-Type: application/json

{"jsonrpc":"2.0","method":"tools/list","id":1}
```

### Tools

| Tool | Description |
|---|---|
| `global.list_resources` | List global resources |
| `global.list_flows` | List resource flows (filter by resource_id, year) |
| `global.get_graph` | Build resource graph for 3D visualization |
| `global.get_resource_stats` | Get region stats for a resource |
| `global.get_timeline` | Get timeline data for a resource |
| `global.list_systems` | List system models |
| `global.get_system` | Get a full systems-thinking model |

## Project Structure

```
PROJECT.jsonld              # Project metadata (JSON-LD)
scheduler.jsonld            # Milestone tracking
MCP_TOOLS.md               # MCP tool contract documentation
wasm/                       # wasmCloud MCP component (TinyGo)
  global-mcp-component/
    main.go                 # MCP JSON-RPC 2.0 handler
    wasmcloud.toml          # Build config
    wit/world.wit           # WIT interface
    wadm/                   # WADM deployment manifest
```

## License

Apache License 2.0 — see [LICENSE](LICENSE).
