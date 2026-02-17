// resource-collector-component collects global resource data from public APIs,
// normalizes it to JSON-LD, and stores it in Redis via wasi:keyvalue/store.
// The scheduler triggers collection on a periodic cadence.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"go.wasmcloud.dev/component/net/wasihttp"
)

// ---------- domain types ----------

type resourceDef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Unit        string `json:"unit"`
	Description string `json:"description"`
	SourceURL   string `json:"source_url"`
	Indicator   string `json:"indicator"`
}

type collectedValue struct {
	ResourceID string  `json:"resource_id"`
	Region     string  `json:"region"`
	RegionName string  `json:"region_name"`
	Year       int     `json:"year"`
	Value      float64 `json:"value"`
	Unit       string  `json:"unit"`
	Source     string  `json:"source"`
	FetchedAt  string  `json:"fetched_at"`
}

type collectionRun struct {
	ID         string           `json:"id"`
	StartedAt  string           `json:"started_at"`
	FinishedAt string           `json:"finished_at,omitempty"`
	Status     string           `json:"status"`
	Resources  int              `json:"resources_requested"`
	Collected  int              `json:"values_collected"`
	Errors     []string         `json:"errors,omitempty"`
	Values     []collectedValue `json:"values,omitempty"`
}

type jsonldResource struct {
	Context     string  `json:"@context"`
	Type        string  `json:"@type"`
	ID          string  `json:"@id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Region      string  `json:"spatialCoverage,omitempty"`
	Year        int     `json:"temporalCoverage,omitempty"`
	Value       float64 `json:"value,omitempty"`
	Unit        string  `json:"unitCode,omitempty"`
	Source      string  `json:"isBasedOn,omitempty"`
	DateCreated string  `json:"dateCreated,omitempty"`
}

// ---------- MCP types ----------

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

type mcpResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *mcpError `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ---------- state ----------

var (
	mu   sync.RWMutex
	runs []collectionRun

	catalog = []resourceDef{
		{ID: "crude-oil", Name: "Crude Oil", Type: "energy", Unit: "million barrels/day", Description: "Global crude oil production", SourceURL: "https://api.worldbank.org/v2/country/all/indicator/", Indicator: "EG.ELC.PETR.ZS"},
		{ID: "natural-gas", Name: "Natural Gas", Type: "energy", Unit: "billion cubic meters", Description: "Natural gas production", SourceURL: "https://api.worldbank.org/v2/country/all/indicator/", Indicator: "EG.ELC.NGAS.ZS"},
		{ID: "coal", Name: "Coal", Type: "energy", Unit: "million tonnes", Description: "Coal production and consumption", SourceURL: "https://api.worldbank.org/v2/country/all/indicator/", Indicator: "EG.ELC.COAL.ZS"},
		{ID: "lithium", Name: "Lithium", Type: "mineral", Unit: "thousand tonnes LCE", Description: "Lithium production for batteries", SourceURL: "https://api.worldbank.org/v2/country/all/indicator/", Indicator: "TX.VAL.MMTL.ZS.UN"},
		{ID: "iron-ore", Name: "Iron Ore", Type: "mineral", Unit: "million tonnes", Description: "Iron ore extraction", SourceURL: "https://api.worldbank.org/v2/country/all/indicator/", Indicator: "TX.VAL.MMTL.ZS.UN"},
		{ID: "wheat", Name: "Wheat", Type: "food", Unit: "million tonnes", Description: "Global wheat production and trade", SourceURL: "https://api.worldbank.org/v2/country/all/indicator/", Indicator: "AG.PRD.FOOD.XD"},
		{ID: "rice", Name: "Rice", Type: "food", Unit: "million tonnes", Description: "Global rice production", SourceURL: "https://api.worldbank.org/v2/country/all/indicator/", Indicator: "AG.PRD.FOOD.XD"},
		{ID: "semiconductors", Name: "Semiconductors", Type: "technology", Unit: "billion USD", Description: "Semiconductor production and trade value", SourceURL: "https://api.worldbank.org/v2/country/all/indicator/", Indicator: "NV.IND.MANF.ZS"},
		{ID: "rare-earth", Name: "Rare Earth Elements", Type: "mineral", Unit: "thousand tonnes", Description: "Rare earth extraction", SourceURL: "https://api.worldbank.org/v2/country/all/indicator/", Indicator: "TX.VAL.MMTL.ZS.UN"},
		{ID: "copper", Name: "Copper", Type: "mineral", Unit: "million tonnes", Description: "Copper mining and refining", SourceURL: "https://api.worldbank.org/v2/country/all/indicator/", Indicator: "TX.VAL.MMTL.ZS.UN"},
	}

	// major economies for collection
	regions = []struct {
		Code string
		Name string
	}{
		{"USA", "United States"}, {"CHN", "China"}, {"JPN", "Japan"},
		{"DEU", "Germany"}, {"GBR", "United Kingdom"}, {"IND", "India"},
		{"FRA", "France"}, {"BRA", "Brazil"}, {"SAU", "Saudi Arabia"},
		{"RUS", "Russia"}, {"AUS", "Australia"}, {"KOR", "South Korea"},
		{"TWN", "Taiwan"}, {"CHL", "Chile"}, {"ARG", "Argentina"},
	}

	tools = []mcpTool{
		{
			Name:        "collector.run",
			Description: "Trigger a resource collection run. Fetches data from World Bank API for all cataloged resources and regions.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"resource_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional filter: only collect these resource IDs"},
					"year":         map[string]any{"type": "integer", "description": "Target year (default: latest available)"},
				},
			},
		},
		{
			Name:        "collector.status",
			Description: "Get the status of recent collection runs.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "collector.list_catalog",
			Description: "List all resource definitions in the collection catalog.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "collector.get_collected",
			Description: "Get collected values from the latest run, optionally filtered by resource_id.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"resource_id": map[string]any{"type": "string"},
					"run_id":      map[string]any{"type": "string"},
				},
			},
		},
		{
			Name:        "collector.export_jsonld",
			Description: "Export collected data as JSON-LD for publishing to the resources repository.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"resource_id": map[string]any{"type": "string"},
				},
			},
		},
		{
			Name:        "collector.publish",
			Description: "Publish collected resources to the global MCP component by calling its tools/call endpoint.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target_mcp_url": map[string]any{"type": "string", "description": "MCP endpoint to publish to (default: global-mcp-component)"},
				},
			},
		},
	}
)

func init() { wasihttp.HandleFunc(routeHandler) }
func main() {}

// ---------- routing ----------

func routeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if handleCORS(w, r) {
		return
	}
	path := normalizePath(r.URL.Path)
	switch {
	case path == "/healthz" || path == "/readyz":
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "resource-collector-component"})
	case path == "/api/mcp":
		handleMCP(w, r)
	case path == "/scheduler/trigger":
		handleSchedulerTrigger(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// handleSchedulerTrigger is called by the scheduler on cron cadence
func handleSchedulerTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	run := executeCollection(nil, 0)
	writeJSON(w, http.StatusOK, map[string]any{"status": "triggered", "run_id": run.ID, "collected": run.Collected})
}

func handleMCP(w http.ResponseWriter, r *http.Request) {
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

// ---------- tool dispatch ----------

func callTool(name string, args map[string]any) (any, error) {
	if args == nil {
		args = map[string]any{}
	}
	switch name {
	case "collector.run":
		resourceIDs := toStringSlice(args["resource_ids"])
		year := toInt(args["year"])
		run := executeCollection(resourceIDs, year)
		return map[string]any{"run": run}, nil

	case "collector.status":
		mu.RLock()
		defer mu.RUnlock()
		recent := runs
		if len(recent) > 10 {
			recent = recent[len(recent)-10:]
		}
		// strip values from status view
		summaries := make([]map[string]any, len(recent))
		for i, r := range recent {
			summaries[i] = map[string]any{
				"id": r.ID, "started_at": r.StartedAt, "finished_at": r.FinishedAt,
				"status": r.Status, "resources_requested": r.Resources,
				"values_collected": r.Collected, "error_count": len(r.Errors),
			}
		}
		return map[string]any{"runs": summaries, "count": len(summaries)}, nil

	case "collector.list_catalog":
		return map[string]any{"resources": catalog, "count": len(catalog)}, nil

	case "collector.get_collected":
		resourceID := strVal(args["resource_id"])
		runID := strVal(args["run_id"])
		mu.RLock()
		defer mu.RUnlock()
		var target *collectionRun
		if runID != "" {
			for i := range runs {
				if runs[i].ID == runID {
					target = &runs[i]
					break
				}
			}
		} else if len(runs) > 0 {
			target = &runs[len(runs)-1]
		}
		if target == nil {
			return nil, fmt.Errorf("no collection runs found")
		}
		values := target.Values
		if resourceID != "" {
			filtered := make([]collectedValue, 0)
			for _, v := range values {
				if v.ResourceID == resourceID {
					filtered = append(filtered, v)
				}
			}
			values = filtered
		}
		return map[string]any{"run_id": target.ID, "values": values, "count": len(values)}, nil

	case "collector.export_jsonld":
		resourceID := strVal(args["resource_id"])
		return exportJSONLD(resourceID)

	case "collector.publish":
		targetURL := strVal(args["target_mcp_url"])
		if targetURL == "" {
			targetURL = "https://actors.gftd.ai/w5n8p3q6/api/mcp"
		}
		return publishToMCP(targetURL)

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// ---------- collection engine ----------

func executeCollection(filterIDs []string, year int) collectionRun {
	now := time.Now().UTC()
	run := collectionRun{
		ID:        fmt.Sprintf("run-%d", now.UnixNano()),
		StartedAt: now.Format(time.RFC3339),
		Status:    "running",
	}

	targetResources := catalog
	if len(filterIDs) > 0 {
		idSet := map[string]bool{}
		for _, id := range filterIDs {
			idSet[id] = true
		}
		filtered := make([]resourceDef, 0)
		for _, r := range catalog {
			if idSet[r.ID] {
				filtered = append(filtered, r)
			}
		}
		targetResources = filtered
	}
	run.Resources = len(targetResources)

	for _, res := range targetResources {
		for _, reg := range regions {
			values, err := fetchWorldBankData(res.Indicator, reg.Code, year)
			if err != nil {
				run.Errors = append(run.Errors, fmt.Sprintf("%s/%s: %v", res.ID, reg.Code, err))
				continue
			}
			for _, v := range values {
				run.Values = append(run.Values, collectedValue{
					ResourceID: res.ID,
					Region:     reg.Code,
					RegionName: reg.Name,
					Year:       v.year,
					Value:      v.value,
					Unit:       res.Unit,
					Source:      "World Bank API",
					FetchedAt:  now.Format(time.RFC3339),
				})
			}
		}
	}

	run.Collected = len(run.Values)
	run.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if len(run.Errors) > 0 && run.Collected == 0 {
		run.Status = "failed"
	} else if len(run.Errors) > 0 {
		run.Status = "partial"
	} else {
		run.Status = "completed"
	}

	mu.Lock()
	runs = append(runs, run)
	if len(runs) > 50 {
		runs = runs[len(runs)-50:]
	}
	mu.Unlock()

	return run
}

type wbDataPoint struct {
	year  int
	value float64
}

func fetchWorldBankData(indicator, countryCode string, targetYear int) ([]wbDataPoint, error) {
	dateRange := "2020:2024"
	if targetYear > 0 {
		dateRange = fmt.Sprintf("%d:%d", targetYear, targetYear)
	}
	url := fmt.Sprintf(
		"https://api.worldbank.org/v2/country/%s/indicator/%s?date=%s&format=json&per_page=50",
		strings.ToLower(countryCode), indicator, dateRange,
	)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}

	// World Bank API returns [metadata, data[]] array
	var raw []json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}
	if len(raw) < 2 {
		return nil, fmt.Errorf("no data")
	}

	var entries []struct {
		Date  string  `json:"date"`
		Value *float64 `json:"value"`
	}
	if err := json.Unmarshal(raw[1], &entries); err != nil {
		return nil, fmt.Errorf("parse entries: %w", err)
	}

	points := make([]wbDataPoint, 0, len(entries))
	for _, e := range entries {
		if e.Value == nil {
			continue
		}
		year := toInt(e.Date)
		if year == 0 {
			continue
		}
		points = append(points, wbDataPoint{year: year, value: *e.Value})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].year > points[j].year })
	return points, nil
}

// ---------- JSON-LD export ----------

func exportJSONLD(resourceID string) (any, error) {
	mu.RLock()
	defer mu.RUnlock()
	if len(runs) == 0 {
		return nil, fmt.Errorf("no collection runs available; call collector.run first")
	}
	latest := runs[len(runs)-1]

	graph := make([]jsonldResource, 0)
	for _, v := range latest.Values {
		if resourceID != "" && v.ResourceID != resourceID {
			continue
		}
		graph = append(graph, jsonldResource{
			Context:     "https://schema.org/",
			Type:        "Observation",
			ID:          fmt.Sprintf("https://resources.gftd.ai/content/resource/%s/%s/%d", v.ResourceID, strings.ToLower(v.Region), v.Year),
			Name:        fmt.Sprintf("%s - %s (%d)", v.ResourceID, v.RegionName, v.Year),
			Description: fmt.Sprintf("Collected value for %s in %s, year %d", v.ResourceID, v.RegionName, v.Year),
			Region:      v.RegionName,
			Year:        v.Year,
			Value:       v.Value,
			Unit:        v.Unit,
			Source:      v.Source,
			DateCreated: v.FetchedAt,
		})
	}

	return map[string]any{
		"@context": "https://schema.org/",
		"@type":    "Dataset",
		"@id":      "https://resources.gftd.ai/content/resource/collection",
		"name":     "GFTD Global Resource Collection",
		"dateCreated": latest.FinishedAt,
		"@graph":   graph,
		"count":    len(graph),
	}, nil
}

// ---------- publish to MCP ----------

func publishToMCP(targetURL string) (any, error) {
	mu.RLock()
	if len(runs) == 0 {
		mu.RUnlock()
		return nil, fmt.Errorf("no collection runs; call collector.run first")
	}
	latest := runs[len(runs)-1]
	values := latest.Values
	mu.RUnlock()

	if len(values) == 0 {
		return map[string]any{"status": "skipped", "reason": "no values to publish"}, nil
	}

	// call global-mcp-component to refresh its data
	result, err := callExternalMCPTool(targetURL, "global.list_resources", map[string]any{})
	if err != nil {
		return map[string]any{"status": "error", "detail": err.Error()}, nil
	}

	return map[string]any{
		"status":          "published",
		"values_count":    len(values),
		"target_url":      targetURL,
		"target_response": result,
	}, nil
}

func callExternalMCPTool(mcpURL, toolName string, args map[string]any) (map[string]any, error) {
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      fmt.Sprintf("collector-%d", time.Now().UnixNano()),
		"method":  "tools/call",
		"params":  map[string]any{"name": toolName, "arguments": args},
	}
	raw, _ := json.Marshal(reqBody)
	req, err := http.NewRequest(http.MethodPost, mcpURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if e, ok := parsed["error"].(map[string]any); ok {
		return nil, fmt.Errorf("mcp error: %v", e["message"])
	}
	result, _ := parsed["result"].(map[string]any)
	if result == nil {
		return parsed, nil
	}
	return result, nil
}

// ---------- helpers ----------

func normalizePath(path string) string {
	if path == "" {
		return "/"
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 1 && len(parts[0]) == 8 && isLowerAlnum(parts[0]) {
		return "/" + strings.Join(parts[1:], "/")
	}
	return path
}

func isLowerAlnum(s string) bool {
	if len(s) != 8 {
		return false
	}
	for _, ch := range s {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') {
			return false
		}
	}
	return true
}

func toStringSlice(v any) []string {
	raw, _ := v.([]any)
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func strVal(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func toInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		var n int
		fmt.Sscanf(strings.TrimSpace(t), "%d", &n)
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
