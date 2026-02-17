package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.wasmcloud.dev/component/net/wasihttp"
)

type ResourceType string

const (
	ResourceEnergy   ResourceType = "energy"
	ResourceMineral  ResourceType = "mineral"
	ResourceFood     ResourceType = "food"
	ResourceWater    ResourceType = "water"
	ResourceLabor    ResourceType = "labor"
	ResourceCapital  ResourceType = "capital"
	ResourceTech     ResourceType = "technology"
	ResourceMaterial ResourceType = "material"
)

type Resource struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Type        ResourceType `json:"type"`
	Unit        string       `json:"unit"`
	Description string       `json:"description"`
}

type RegionStats struct {
	RegionID    string  `json:"regionId"`
	RegionName  string  `json:"regionName"`
	Year        int     `json:"year"`
	Production  float64 `json:"production"`
	Consumption float64 `json:"consumption"`
	Export      float64 `json:"export"`
	Import      float64 `json:"import"`
	Reserve     float64 `json:"reserve"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
}

type ResourceFlow struct {
	ID           string  `json:"id"`
	ResourceID   string  `json:"resourceId"`
	SourceRegion string  `json:"sourceRegion"`
	TargetRegion string  `json:"targetRegion"`
	Year         int     `json:"year"`
	Volume       float64 `json:"volume"`
	Value        float64 `json:"value"`
}

type GraphNode struct {
	ID    string  `json:"id"`
	Label string  `json:"label"`
	Type  string  `json:"type"`
	Value float64 `json:"value"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Z     float64 `json:"z"`
	Color string  `json:"color"`
	Size  float64 `json:"size"`
}

type GraphEdge struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Weight float64 `json:"weight"`
	Color  string  `json:"color"`
}

type GraphData struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

type SystemNode struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Category string `json:"category"`
	Level    string `json:"level"`
}

type SystemEdge struct {
	Source   string `json:"source"`
	Target  string `json:"target"`
	Polarity string `json:"polarity"`
	Delay    bool   `json:"delay"`
}

type SystemModel struct {
	ID    string       `json:"id"`
	Name  string       `json:"name"`
	Nodes []SystemNode `json:"nodes"`
	Edges []SystemEdge `json:"edges"`
}

type TimelineEntry struct {
	Year int `json:"year"`
	Data any `json:"data"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Method  string `json:"method"`
	Params  struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"params"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *mcpError `json:"error,omitempty"`
}

var (
	resources = []Resource{
		{ID: "crude-oil", Name: "Crude Oil", Type: ResourceEnergy, Unit: "million barrels/day", Description: "Global crude oil production and trade flows"},
		{ID: "natural-gas", Name: "Natural Gas", Type: ResourceEnergy, Unit: "billion cubic meters", Description: "Natural gas extraction and LNG flows"},
		{ID: "lithium", Name: "Lithium", Type: ResourceMineral, Unit: "thousand tonnes LCE", Description: "Lithium extraction for batteries"},
		{ID: "wheat", Name: "Wheat", Type: ResourceFood, Unit: "million tonnes", Description: "Global wheat production and trade"},
		{ID: "semiconductors", Name: "Semiconductors", Type: ResourceTech, Unit: "billion USD", Description: "Semiconductor production and trade value"},
	}
	resourceStats = map[string][]RegionStats{
		"crude-oil": {
			{RegionID: "us", RegionName: "United States", Year: 2023, Production: 12.9, Consumption: 20.0, Export: 4.0, Import: 6.4, Reserve: 68.8, Lat: 39.8, Lng: -98.6},
			{RegionID: "sa", RegionName: "Saudi Arabia", Year: 2023, Production: 10.1, Consumption: 3.4, Export: 7.1, Import: 0.2, Reserve: 267.0, Lat: 23.9, Lng: 45.1},
			{RegionID: "cn", RegionName: "China", Year: 2023, Production: 4.2, Consumption: 15.4, Export: 0.1, Import: 11.2, Reserve: 26.0, Lat: 35.8, Lng: 104.2},
		},
		"semiconductors": {
			{RegionID: "tw", RegionName: "Taiwan", Year: 2023, Production: 178.0, Consumption: 40.0, Export: 132.0, Import: 9.0, Reserve: 0, Lat: 23.7, Lng: 121.0},
			{RegionID: "kr", RegionName: "South Korea", Year: 2023, Production: 112.0, Consumption: 52.0, Export: 68.0, Import: 14.0, Reserve: 0, Lat: 36.2, Lng: 127.9},
			{RegionID: "us", RegionName: "United States", Year: 2023, Production: 96.0, Consumption: 164.0, Export: 41.0, Import: 93.0, Reserve: 0, Lat: 39.8, Lng: -98.6},
		},
	}
	flows = []ResourceFlow{
		{ID: "oil-sa-cn-2023", ResourceID: "crude-oil", SourceRegion: "sa", TargetRegion: "cn", Year: 2023, Volume: 1.8, Value: 49.0},
		{ID: "oil-us-eu-2023", ResourceID: "crude-oil", SourceRegion: "us", TargetRegion: "eu", Year: 2023, Volume: 1.2, Value: 33.0},
		{ID: "chip-tw-us-2023", ResourceID: "semiconductors", SourceRegion: "tw", TargetRegion: "us", Year: 2023, Volume: 38.0, Value: 64.0},
		{ID: "chip-kr-us-2023", ResourceID: "semiconductors", SourceRegion: "kr", TargetRegion: "us", Year: 2023, Volume: 28.0, Value: 41.0},
	}
	systems = []SystemModel{
		{
			ID:   "global-energy-balance",
			Name: "Global Energy Balance",
			Nodes: []SystemNode{
				{ID: "demand", Label: "Energy Demand", Category: "demand", Level: "stock"},
				{ID: "production", Label: "Energy Production", Category: "supply", Level: "flow"},
				{ID: "price", Label: "Commodity Price", Category: "market", Level: "auxiliary"},
			},
			Edges: []SystemEdge{
				{Source: "demand", Target: "price", Polarity: "+", Delay: false},
				{Source: "price", Target: "production", Polarity: "+", Delay: true},
				{Source: "production", Target: "price", Polarity: "-", Delay: false},
			},
		},
	}
	tools = []mcpTool{
		{Name: "global.list_resources", Description: "List global resources", InputSchema: map[string]any{"type": "object", "properties": map[string]any{}}},
		{Name: "global.list_flows", Description: "List resource flows", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"resource_id": map[string]any{"type": "string"}, "year": map[string]any{"type": "integer"}}}},
		{Name: "global.get_graph", Description: "Build resource graph", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"resource_id": map[string]any{"type": "string"}, "year": map[string]any{"type": "integer"}}}},
		{Name: "global.get_resource_stats", Description: "Get region resource stats", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"resource_id": map[string]any{"type": "string"}}}},
		{Name: "global.get_timeline", Description: "Get timeline data", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"resource_id": map[string]any{"type": "string"}}}},
		{Name: "global.list_systems", Description: "List system models", InputSchema: map[string]any{"type": "object", "properties": map[string]any{}}},
		{Name: "global.get_system", Description: "Get system model", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"system_id": map[string]any{"type": "string"}}, "required": []string{"system_id"}}},
	}
)

func init() {
	wasihttp.HandleFunc(routeHandler)
}

func main() {}

func routeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if handleCORS(w, r) {
		return
	}

	path := normalizePath(r.URL.Path)

	switch {
	case path == "/healthz":
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "global-mcp-component"})
		return
	case path == "/api/":
		writeJSON(w, http.StatusGone, map[string]string{"error": "legacy REST APIs are removed", "detail": "Use MCP endpoint /api/mcp with tools/list and tools/call"})
		return
	case path != "/api/mcp":
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req mcpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, mcpResponse{JSONRPC: "2.0", ID: nil, Error: &mcpError{Code: -32700, Message: "parse error"}})
		return
	}
	if req.JSONRPC != "2.0" {
		writeJSON(w, http.StatusBadRequest, mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: &mcpError{Code: -32600, Message: "invalid request"}})
		return
	}

	switch req.Method {
	case "tools/list":
		writeJSON(w, http.StatusOK, mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": tools}})
	case "tools/call":
		result, err := callTool(req.Params.Name, req.Params.Arguments)
		if err != nil {
			writeJSON(w, http.StatusOK, mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: &mcpError{Code: -32000, Message: err.Error()}})
			return
		}
		writeJSON(w, http.StatusOK, mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
	default:
		writeJSON(w, http.StatusBadRequest, mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: &mcpError{Code: -32601, Message: "method not found"}})
	}
}

func normalizePath(raw string) string {
	if raw == "" {
		return "/"
	}
	parts := strings.Split(strings.TrimPrefix(raw, "/"), "/")
	if len(parts) >= 3 && parts[1] == "api" && parts[2] == "mcp" {
		return "/api/mcp"
	}
	if len(parts) >= 2 && parts[1] == "healthz" {
		return "/healthz"
	}
	if len(parts) >= 2 && parts[1] == "api" && len(parts) == 2 {
		return "/api/"
	}
	return raw
}

func callTool(name string, args map[string]any) (any, error) {
	switch name {
	case "global.list_resources":
		items := make([]Resource, len(resources))
		copy(items, resources)
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		return map[string]any{"resources": items, "count": len(items)}, nil
	case "global.list_flows":
		resourceID, _ := args["resource_id"].(string)
		year := toInt(args["year"])
		out := make([]ResourceFlow, 0)
		for _, f := range flows {
			if resourceID != "" && f.ResourceID != resourceID {
				continue
			}
			if year > 0 && f.Year != year {
				continue
			}
			out = append(out, f)
		}
		return map[string]any{"flows": out, "count": len(out)}, nil
	case "global.get_graph":
		resourceID, _ := args["resource_id"].(string)
		year := toInt(args["year"])
		if year == 0 {
			year = time.Now().Year()
		}
		return buildGraph(resourceID, year), nil
	case "global.get_resource_stats":
		resourceID, _ := args["resource_id"].(string)
		if resourceID == "" {
			return nil, fmt.Errorf("resource_id is required")
		}
		return map[string]any{"resource_id": resourceID, "stats": resourceStats[resourceID], "count": len(resourceStats[resourceID])}, nil
	case "global.get_timeline":
		resourceID, _ := args["resource_id"].(string)
		if resourceID == "" {
			return nil, fmt.Errorf("resource_id is required")
		}
		entries := make([]TimelineEntry, 0)
		for _, s := range resourceStats[resourceID] {
			entries = append(entries, TimelineEntry{Year: s.Year, Data: s})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Year < entries[j].Year })
		return map[string]any{"resource_id": resourceID, "timeline": entries, "count": len(entries)}, nil
	case "global.list_systems":
		index := make([]map[string]string, 0, len(systems))
		for _, s := range systems {
			index = append(index, map[string]string{"id": s.ID, "name": s.Name})
		}
		return map[string]any{"systems": index, "count": len(index)}, nil
	case "global.get_system":
		systemID, _ := args["system_id"].(string)
		for _, s := range systems {
			if s.ID == systemID {
				return s, nil
			}
		}
		return nil, fmt.Errorf("system not found: %s", systemID)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func buildGraph(resourceID string, year int) GraphData {
	nodes := map[string]GraphNode{}
	edges := make([]GraphEdge, 0)

	for _, f := range flows {
		if resourceID != "" && f.ResourceID != resourceID {
			continue
		}
		if year > 0 && f.Year != year {
			continue
		}
		sid := "region:" + f.SourceRegion
		tid := "region:" + f.TargetRegion
		if _, ok := nodes[sid]; !ok {
			nodes[sid] = GraphNode{ID: sid, Label: strings.ToUpper(f.SourceRegion), Type: "region", Value: 1, Color: "#3b82f6", Size: 14}
		}
		if _, ok := nodes[tid]; !ok {
			nodes[tid] = GraphNode{ID: tid, Label: strings.ToUpper(f.TargetRegion), Type: "region", Value: 1, Color: "#10b981", Size: 14}
		}
		edges = append(edges, GraphEdge{Source: sid, Target: tid, Weight: f.Volume, Color: "#9ca3af"})
	}

	outNodes := make([]GraphNode, 0, len(nodes))
	for _, n := range nodes {
		outNodes = append(outNodes, n)
	}
	sort.Slice(outNodes, func(i, j int) bool { return outNodes[i].ID < outNodes[j].ID })
	return GraphData{Nodes: outNodes, Edges: edges}
}

func toInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	default:
		return 0
	}
}

func handleCORS(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
