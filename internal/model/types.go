package model

// RunRequest represents the incoming request from a client.
// Files is a map of relative file paths to their content.
// Command is the CLI command to execute (e.g. "stellar contract build", "stellar --version").
type RunRequest struct {
	Files   map[string]string `json:"files"`
	Command string            `json:"command"`
}

// RunResponse is returned after a job has been enqueued.
type RunResponse struct {
	SessionID string `json:"session_id"`
}

// OutputMessage represents a single output chunk sent via WebSocket.
// Type can be: "stdout", "stderr", "info", "error", "done"
type OutputMessage struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// Job represents a queued job ready for processing.
type Job struct {
	SessionID string
	WorkDir   string
	Command   string // The validated command string to execute
}

// FileTreeNode represents a file or folder in the scanned workspace.
// Used for fileTreeUpdate WebSocket messages and GET /files responses.
type FileTreeNode struct {
	Name     string         `json:"name"`
	Type     string         `json:"type"`               // "file" or "folder"
	Content  string         `json:"content,omitempty"`   // File content (for files)
	Lazy     bool           `json:"lazy,omitempty"`      // true for large dirs
	Children []FileTreeNode `json:"children,omitempty"`
}
