package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"soroban-studio-backend/internal/executor"
)

// FileTreeHandler handles the GET /files endpoint for lazy-loading folder contents.
type FileTreeHandler struct {
	workspaceDir string
}

// NewFileTreeHandler creates a new handler for serving file tree data.
func NewFileTreeHandler() *FileTreeHandler {
	workspaceDir := os.Getenv("WORKSPACE_DIR")
	if workspaceDir == "" {
		workspaceDir = "/app/workspaces"
	}

	return &FileTreeHandler{
		workspaceDir: workspaceDir,
	}
}

// Handle processes GET /files?session_id=xxx&path=some/folder requests.
// Returns the shallow contents of the specified directory within a session's workspace.
func (h *FileTreeHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		http.Error(w, `{"error":"session_id is required"}`, http.StatusBadRequest)
		return
	}

	// Sanitize session ID — must be alphanumeric + hyphens only
	for _, ch := range sessionID {
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-') {
			http.Error(w, `{"error":"invalid session_id"}`, http.StatusBadRequest)
			return
		}
	}

	relPath := r.URL.Query().Get("path")

	// Security: prevent path traversal
	if strings.Contains(relPath, "..") {
		http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
		return
	}

	// Build the full path
	dirPath := filepath.Join(h.workspaceDir, sessionID)
	if relPath != "" {
		dirPath = filepath.Join(dirPath, relPath)
	}

	// Verify the path exists and is a directory
	info, err := os.Stat(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, `{"error":"path not found"}`, http.StatusNotFound)
		} else {
			http.Error(w, `{"error":"failed to access path"}`, http.StatusInternalServerError)
		}
		return
	}

	if !info.IsDir() {
		http.Error(w, `{"error":"path is not a directory"}`, http.StatusBadRequest)
		return
	}

	log.Printf("[filetree] scanning: session=%s, path=%q", sessionID, relPath)

	// Scan using the executor's ScanDirectory (shallow, 1 level)
	nodes := executor.ScanDirectory(dirPath, false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}
