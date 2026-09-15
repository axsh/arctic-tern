// Package api provides HTTP handlers for the Tern artifact API.
package api

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
)

// SystemArtifactHandler handles /api/v1/artifacts/system routes.
type SystemArtifactHandler struct {
	store           store.ArtifactStore
	workDirResolver SessionWorkDirResolver
}

// SessionWorkDirResolver resolves session_id to work_dir for explicit artifact writes.
type SessionWorkDirResolver func(sessionID string) (workDir string, ok bool)

// NewSystemArtifactHandler creates a handler backed by the given store.
// resolver is optional; when set, write APIs may omit actual_path.
func NewSystemArtifactHandler(s store.ArtifactStore, resolver ...SessionWorkDirResolver) *SystemArtifactHandler {
	var r SessionWorkDirResolver
	if len(resolver) > 0 {
		r = resolver[0]
	}
	return &SystemArtifactHandler{store: s, workDirResolver: r}
}

// RegisterRoutes registers the system artifact routes on mux under prefix.
// prefix should be "/api/v1/artifacts/system" (no trailing slash).
func (h *SystemArtifactHandler) RegisterRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc(prefix, h.routeRoot)
	mux.HandleFunc(prefix+"/", h.routeByKey)
}

// routeRoot dispatches GET (list).
func (h *SystemArtifactHandler) routeRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleList(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// routeByKey dispatches:
//   - POST .../archive       → handleArchive
//   - POST .../events        → handleAppendEvent
//   - GET  .../{key}/content → handleContent
//   - GET  .../{key}         → handleGetByKey
//   - PUT  .../{key}         → handlePutByKey
//   - DELETE .../{key}       → handleDeleteByKey
func (h *SystemArtifactHandler) routeByKey(w http.ResponseWriter, r *http.Request) {
	// Extract the sub-path after the prefix+"/".
	// e.g. "/api/v1/artifacts/system/foo/bar.go/content" → "foo/bar.go/content"
	subPath := strings.TrimPrefix(r.URL.Path, "/api/v1/artifacts/system/")
	subPath = strings.Trim(subPath, "/")

	if r.Method == http.MethodPost {
		switch subPath {
		case "archive":
			h.handleArchive(w, r)
		case "events":
			h.handleAppendEvent(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	if strings.HasSuffix(subPath, "/content") {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		key := strings.TrimSuffix(subPath, "/content")
		h.handleContent(w, r, key)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.handleGetByKey(w, r, subPath)
	case http.MethodPut:
		h.handlePutByKey(w, r, subPath)
	case http.MethodDelete:
		h.handleDeleteByKey(w, r, subPath)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleList serves GET /api/v1/artifacts/system.
func (h *SystemArtifactHandler) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.SystemArtifactFilter{
		Q:              q.Get("q"),
		SessionIDs:     q["session_id"],
		AgentIDs:       q["agent_id"],
		TurnIDs:        q["turn_id"],
		CorrelationIDs: q["correlation_id"],
		Operation:      q.Get("operation"),
		IncludeDeleted: q.Get("include_deleted") == "true",
		Sort:           q.Get("sort"),
		Order:          q.Get("order"),
	}
	if p := q.Get("page"); p != "" {
		f.Page, _ = strconv.Atoi(p)
	}
	if pp := q.Get("per_page"); pp != "" {
		f.PerPage, _ = strconv.Atoi(pp)
	}
	if s := q.Get("since"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			f.Since = &t
		}
	}
	if u := q.Get("until"); u != "" {
		if t, err := time.Parse(time.RFC3339, u); err == nil {
			f.Until = &t
		}
	}

	page, err := h.store.ListSystemArtifacts(r.Context(), f)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"source":      "system",
		"total_count": page.TotalCount,
		"page":        page.Page,
		"per_page":    page.PerPage,
		"items":       systemItemsJSON(page.Items),
	})
}

// handleGetByKey serves GET /api/v1/artifacts/system/{key}.
func (h *SystemArtifactHandler) handleGetByKey(w http.ResponseWriter, r *http.Request, key string) {
	events, err := h.store.GetSystemArtifactByKey(r.Context(), key)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if len(events) == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{
		"source":     "system",
		"key":        key,
		"operations": systemItemsJSON(events),
	})
}

// handleContent serves GET /api/v1/artifacts/system/{key}/content.
func (h *SystemArtifactHandler) handleContent(w http.ResponseWriter, r *http.Request, key string) {
	events, err := h.store.GetSystemArtifactByKey(r.Context(), key)
	if err != nil || len(events) == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	// Use the ActualPath from the most recent event.
	latest := events[len(events)-1]
	if latest.ActualPath == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	resolved := resolveArtifactPath(latest.ActualPath)
	f, err := os.Open(resolved)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Disposition",
		`attachment; filename="`+filepath.Base(latest.ActualPath)+`"`)
	io.Copy(w, f) //nolint:errcheck
}

type systemArtifactWriteRequest struct {
	SessionID     string `json:"session_id"`
	TurnID        string `json:"turn_id"`
	CorrelationID string `json:"correlation_id"`
	ActualPath    string `json:"actual_path"`
	ToolName      string `json:"tool_name"`
	OccurredAt    string `json:"occurred_at"`
}

type systemArtifactAppendRequest struct {
	systemArtifactWriteRequest
	Key       string `json:"key"`
	Operation string `json:"operation"`
}

func (h *SystemArtifactHandler) handlePutByKey(w http.ResponseWriter, r *http.Request, rawKey string) {
	key, err := validateSystemKey(rawKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req systemArtifactWriteRequest
	if err := decodeJSONBody(r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.SessionID) == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}

	actualPath, err := h.resolveActualPath(req.SessionID, key, req.ActualPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !artifactPathExists(actualPath) {
		http.Error(w, "actual_path does not exist", http.StatusBadRequest)
		return
	}

	events, err := h.store.GetSystemArtifactByKey(r.Context(), key)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	op := store.OperationCreate
	status := "created"
	code := http.StatusCreated
	if latest := latestOperation(events); latest != "" && latest != store.OperationDelete {
		op = store.OperationUpdate
		status = "updated"
		code = http.StatusOK
	}

	occurredAt, err := parseOccurredAt(req.OccurredAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	toolName := normalizedToolName(req.ToolName)
	event := store.SystemArtifactEvent{
		SessionID:     req.SessionID,
		AgentID:       req.SessionID,
		TurnID:        req.TurnID,
		CorrelationID: req.CorrelationID,
		Key:           key,
		ActualPath:    actualPath,
		Operation:     op,
		OccurredAt:    occurredAt,
		ToolName:      toolName,
	}
	if err := h.store.SaveSystemArtifactEvent(r.Context(), event); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeSystemWriteResponse(w, code, key, op, status, event)
}

func (h *SystemArtifactHandler) handleDeleteByKey(w http.ResponseWriter, r *http.Request, rawKey string) {
	key, err := validateSystemKey(rawKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req systemArtifactWriteRequest
	if err := decodeJSONBody(r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.SessionID) == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}

	existing, err := h.store.GetSystemArtifactByKey(r.Context(), key)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if latest := latestOperation(existing); latest == "" || latest == store.OperationDelete {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	actualPath := ""
	if strings.TrimSpace(req.ActualPath) != "" {
		actualPath, err = h.resolveActualPath(req.SessionID, key, req.ActualPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		actualPath = existing[len(existing)-1].ActualPath
		if actualPath == "" {
			if resolved, resolveErr := h.resolveActualPath(req.SessionID, key, ""); resolveErr == nil {
				actualPath = resolved
			}
		}
	}

	occurredAt, err := parseOccurredAt(req.OccurredAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	event := store.SystemArtifactEvent{
		SessionID:     req.SessionID,
		AgentID:       req.SessionID,
		TurnID:        req.TurnID,
		CorrelationID: req.CorrelationID,
		Key:           key,
		ActualPath:    actualPath,
		Operation:     store.OperationDelete,
		OccurredAt:    occurredAt,
		ToolName:      normalizedToolName(req.ToolName),
	}
	if err := h.store.SaveSystemArtifactEvent(r.Context(), event); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeSystemWriteResponse(w, http.StatusOK, key, store.OperationDelete, "deleted", event)
}

func (h *SystemArtifactHandler) handleAppendEvent(w http.ResponseWriter, r *http.Request) {
	var req systemArtifactAppendRequest
	if err := decodeJSONBody(r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	key, err := validateSystemKey(req.Key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.SessionID) == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}
	op := strings.TrimSpace(req.Operation)
	switch op {
	case store.OperationCreate, store.OperationUpdate, store.OperationDelete:
	default:
		http.Error(w, "operation must be create, update, or delete", http.StatusBadRequest)
		return
	}

	actualPath := ""
	if op == store.OperationCreate || op == store.OperationUpdate {
		actualPath, err = h.resolveActualPath(req.SessionID, key, req.ActualPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !artifactPathExists(actualPath) {
			http.Error(w, "actual_path does not exist", http.StatusBadRequest)
			return
		}
	} else if strings.TrimSpace(req.ActualPath) != "" {
		actualPath, err = h.resolveActualPath(req.SessionID, key, req.ActualPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		existing, _ := h.store.GetSystemArtifactByKey(r.Context(), key)
		if len(existing) > 0 {
			actualPath = existing[len(existing)-1].ActualPath
		}
	}

	occurredAt, err := parseOccurredAt(req.OccurredAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	event := store.SystemArtifactEvent{
		SessionID:     req.SessionID,
		AgentID:       req.SessionID,
		TurnID:        req.TurnID,
		CorrelationID: req.CorrelationID,
		Key:           key,
		ActualPath:    actualPath,
		Operation:     op,
		OccurredAt:    occurredAt,
		ToolName:      normalizedToolName(req.ToolName),
	}
	if err := h.store.SaveSystemArtifactEvent(r.Context(), event); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeSystemWriteResponse(w, http.StatusCreated, key, op, "appended", event)
}

// archiveRequest is the JSON body for POST .../archive.
type archiveRequest struct {
	Keys      []string `json:"keys"`
	Q         string   `json:"q"`
	SessionID []string `json:"session_id"`
}

// handleArchive serves POST /api/v1/artifacts/system/archive.
func (h *SystemArtifactHandler) handleArchive(w http.ResponseWriter, r *http.Request) {
	var req archiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Collect unique keys to include.
	keys := make(map[string]string) // key → actualPath

	// Keys specified directly.
	for _, k := range req.Keys {
		events, _ := h.store.GetSystemArtifactByKey(r.Context(), k)
		if len(events) > 0 {
			keys[k] = events[len(events)-1].ActualPath
		}
	}

	// Keys matched by glob.
	if req.Q != "" {
		events, _ := h.store.ListAllSystemArtifacts(r.Context(), store.SystemArtifactFilter{
			Q:          req.Q,
			SessionIDs: req.SessionID,
			Sort:       "occurred_at",
			Order:      "asc",
		})
		for _, e := range events {
			// Ascending occurred_at: later events overwrite ActualPath for the same key.
			keys[e.Key] = e.ActualPath
		}
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="artifacts.zip"`)

	zw := zip.NewWriter(w)
	defer zw.Close()

	for key, actualPath := range keys {
		if actualPath == "" {
			continue
		}
		resolved := resolveArtifactPath(actualPath)
		f, err := os.Open(resolved)
		if err != nil {
			continue // skip missing files silently
		}
		zf, err := zw.Create(key)
		if err != nil {
			f.Close()
			continue
		}
		io.Copy(zf, f) //nolint:errcheck
		f.Close()
	}
}

func decodeJSONBody(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil && err != io.EOF {
		return err
	}
	return nil
}

func validateSystemKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", fmt.Errorf("key is required")
	}
	if filepath.IsAbs(key) || path.IsAbs(key) || filepath.VolumeName(key) != "" {
		return "", fmt.Errorf("key must be a relative logical path")
	}

	slash := filepath.ToSlash(key)
	parts := strings.Split(slash, "/")
	for _, p := range parts {
		if p == ".." {
			return "", fmt.Errorf("key must not contain '..'")
		}
	}
	clean := path.Clean(slash)
	if clean == "." || clean == "" || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("invalid key")
	}
	return clean, nil
}

func (h *SystemArtifactHandler) resolveActualPath(sessionID, key, actualPath string) (string, error) {
	p := strings.TrimSpace(actualPath)
	if p == "" {
		workDir, ok := h.lookupWorkDir(sessionID)
		if !ok || workDir == "" {
			return "", fmt.Errorf("actual_path is required when session_id has no resolvable work_dir")
		}
		return filepath.Clean(filepath.Join(workDir, filepath.FromSlash(key))), nil
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	if workDir, ok := h.lookupWorkDir(sessionID); ok && workDir != "" {
		return filepath.Clean(filepath.Join(workDir, filepath.FromSlash(p))), nil
	}
	if abs, err := filepath.Abs(p); err == nil {
		return filepath.Clean(abs), nil
	}
	return filepath.Clean(p), nil
}

func (h *SystemArtifactHandler) lookupWorkDir(sessionID string) (string, bool) {
	if h.workDirResolver == nil || strings.TrimSpace(sessionID) == "" {
		return "", false
	}
	workDir, ok := h.workDirResolver(sessionID)
	if !ok || strings.TrimSpace(workDir) == "" {
		return "", false
	}
	return workDir, true
}

func parseOccurredAt(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now(), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("occurred_at must be RFC3339")
}

func normalizedToolName(toolName string) string {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return "api:system_manual"
	}
	return toolName
}

func latestOperation(events []store.SystemArtifactEvent) string {
	if len(events) == 0 {
		return ""
	}
	return events[len(events)-1].Operation
}

func writeSystemWriteResponse(w http.ResponseWriter, code int, key, operation, status string, e store.SystemArtifactEvent) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"source":         "system",
		"key":            key,
		"operation":      operation,
		"status":         status,
		"session_id":     e.SessionID,
		"turn_id":        e.TurnID,
		"correlation_id": e.CorrelationID,
		"tool_name":      e.ToolName,
		"occurred_at":    e.OccurredAt.UTC().Format(time.RFC3339),
	}) //nolint:errcheck
}

func artifactPathExists(actualPath string) bool {
	resolved := resolveArtifactPath(actualPath)
	_, err := os.Stat(resolved)
	return err == nil
}

// ---- JSON helpers ----

func systemItemsJSON(events []store.SystemArtifactEvent) []map[string]any {
	out := make([]map[string]any, len(events))
	for i, e := range events {
		out[i] = map[string]any{
			"key":            e.Key,
			"operation":      e.Operation,
			"agent_id":       e.AgentID,
			"session_id":     e.SessionID,
			"turn_id":        e.TurnID,
			"correlation_id": e.CorrelationID,
			"occurred_at":    e.OccurredAt.UTC().Format(time.RFC3339),
			"tool_name":      e.ToolName,
			"sha":            e.ContentSHA,
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// resolveArtifactPath maps stored ActualPath values to a path the OS can open.
// On Windows with Git Bash/MSYS, agents may report paths like /tmp/... while
// the physical file lives under %TEMP%/...
func resolveArtifactPath(actualPath string) string {
	candidates := []string{actualPath, filepath.FromSlash(actualPath)}
	if runtime.GOOS == "windows" {
		slash := filepath.ToSlash(actualPath)
		if strings.HasPrefix(slash, "/tmp/") {
			if temp := os.Getenv("TEMP"); temp != "" {
				suffix := strings.TrimPrefix(slash, "/tmp/")
				candidates = append(candidates, filepath.Join(temp, suffix))
			}
		}
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return actualPath
}
