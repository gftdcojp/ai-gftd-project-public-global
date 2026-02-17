# Global Project MCP Tools

Endpoint: `POST /api/mcp` (JSON-RPC 2.0)

Supported methods:
- `tools/list`
- `tools/call`

## Envelope

Request:
```json
{
  "jsonrpc": "2.0",
  "id": "any",
  "method": "tools/call",
  "params": {
    "name": "global.list_resources",
    "arguments": {}
  }
}
```

## Tools

### `global.list_resources`
List known global resources.

Arguments: none

Result: `Resource[]` — id, name, type, unit, description

### `global.list_flows`
List resource flows. Filter by `resource_id` and/or `year`.

Arguments:
- `resource_id` string (optional)
- `year` integer (optional)

Result: `ResourceFlow[]` — id, resourceId, sourceRegion, targetRegion, year, volume, value

### `global.get_resource_stats`
Get region stats for a resource.

Arguments:
- `resource_id` string (required)

Result: `RegionStats[]` — regionId, regionName, year, production, consumption, export, import, reserve, lat, lng

### `global.get_graph`
Graph data for 3D graph view.

Arguments:
- `resource_id` string (optional, default: crude-oil)
- `year` integer (optional, default: 2023)

Result: `GraphData` — nodes[], edges[]

### `global.get_timeline`
Year-indexed timeline for a resource.

Arguments:
- `resource_id` string (optional)

Result: `TimelineEntry[]` — year, data

### `global.list_systems`
List available system models.

Arguments: none

Result: `{id, name}[]`

### `global.get_system`
Fetch a full systems-thinking model.

Arguments:
- `system_id` string (required)

Result: `SystemModel` — id, name, nodes[], edges[]
