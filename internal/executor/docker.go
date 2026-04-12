package executor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"soroban-studio-backend/internal/model"
	"soroban-studio-backend/internal/session"
)

// lazyFolders are directories that should be marked as lazy (not expanded on scan).
var lazyFolders = map[string]bool{
	"node_modules": true,
	"target":       true,
	".git":         true,
	"dist":         true,
	"build":        true,
	"vendor":       true,
	"deps":         true,
	"__pycache__":  true,
	".venv":        true,
	"venv":         true,
	"env":          true,
	".env":         true,
	"cache":        true,
	"tmp":          true,
	"temp":         true,
	".next":        true,
	".nuxt":        true,
	".output":      true,
	"out":          true,
}

// Executor handles running `docker exec` commands inside the shared Soroban
// runner container. It streams stdout/stderr in real-time via the session manager.
type Executor struct {
	sessionMgr    *session.Manager
	containerName string
	workspaceDir  string
}

// New creates a new Executor with configuration from environment variables.
func New(sessionMgr *session.Manager) *Executor {
	containerName := os.Getenv("RUNNER_CONTAINER")
	if containerName == "" {
		containerName = "soroban-runner"
	}

	workspaceDir := os.Getenv("WORKSPACE_DIR")
	if workspaceDir == "" {
		workspaceDir = "/app/workspaces"
	}

	return &Executor{
		sessionMgr:    sessionMgr,
		containerName: containerName,
		workspaceDir:  workspaceDir,
	}
}

// Execute runs the user's command inside the runner container for
// the given job. Output is streamed in real-time via WebSocket.
// After completion, scans the workspace and sends a fileTreeUpdate.
func (e *Executor) Execute(job model.Job) {
	command := job.Command
	if command == "" {
		command = "stellar contract build"
	}

	log.Printf("[executor] executing command for session %s: %q", job.SessionID, command)

	// Parse the command into individual arguments (safe — no shell involved)
	cmdArgs := strings.Fields(command)
	log.Printf("[executor] parsed args: %v", cmdArgs)

	// Build the docker exec argument list:
	// docker exec --workdir /app/workspaces/{session} {container} {cmd} {args...}
	workDir := fmt.Sprintf("/app/workspaces/%s", job.SessionID)
	dockerArgs := []string{
		"exec",
		"--workdir", workDir,
		e.containerName,
	}
	dockerArgs = append(dockerArgs, cmdArgs...)

	log.Printf("[executor] docker command: docker %v", dockerArgs)

	cmd := exec.Command("docker", dockerArgs...)

	// Create pipes for real-time output streaming
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		e.sendError(job.SessionID, fmt.Sprintf("Failed to create stdout pipe: %v", err))
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		e.sendError(job.SessionID, fmt.Sprintf("Failed to create stderr pipe: %v", err))
		return
	}

	// Start the process (non-blocking)
	if err := cmd.Start(); err != nil {
		e.sendError(job.SessionID, fmt.Sprintf("Failed to start command: %v", err))
		return
	}

	// Stream stdout and stderr concurrently using goroutines
	var wg sync.WaitGroup
	wg.Add(2)

	// Stream stdout
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			e.sessionMgr.Send(job.SessionID, model.OutputMessage{
				Type:    "stdout",
				Content: scanner.Text(),
			})
		}
	}()

	// Stream stderr (Cargo/Rust output goes to stderr)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			e.sessionMgr.Send(job.SessionID, model.OutputMessage{
				Type:    "stderr",
				Content: scanner.Text(),
			})
		}
	}()

	// Wait for all output to be read before calling cmd.Wait()
	wg.Wait()

	// Wait for the process to exit and check the result
	if err := cmd.Wait(); err != nil {
		e.sessionMgr.Send(job.SessionID, model.OutputMessage{
			Type:    "error",
			Content: fmt.Sprintf("Command failed: %v", err),
		})
	}

	// Scan workspace and send file tree update
	e.sendFileTreeUpdate(job)

	// Signal that execution is complete
	e.sessionMgr.Send(job.SessionID, model.OutputMessage{
		Type:    "done",
		Content: "",
	})

	// NOTE: Workspace is NOT cleaned up here.
	// It persists so the user can run more commands or browse the file tree.
}

// sendFileTreeUpdate scans workspace directory and sends file tree via WebSocket.
func (e *Executor) sendFileTreeUpdate(job model.Job) {
	workspacePath := filepath.Join(e.workspaceDir, job.SessionID)
	
	// First scan with shallow=false to get all folders including target/debug
	log.Printf("[executor] scanning workspace path: %s", workspacePath)
	tree := ScanDirectory(workspacePath, false)
	
	// Debug: log what we found
	for _, node := range tree {
		if node.Type == "folder" {
			log.Printf("[executor] found folder: %s (lazy: %v, children: %d)", node.Name, node.Lazy, len(node.Children))
			for _, child := range node.Children {
				if child.Type == "folder" {
					log.Printf("[executor]   - subfolder: %s (lazy: %v)", child.Name, child.Lazy)
				}
			}
		}
	}

	if len(tree) == 0 {
		log.Printf("[executor] no files found in workspace %s", job.SessionID)
		return
	}

	treeJSON, err := json.Marshal(tree)
	if err != nil {
		log.Printf("[executor] failed to marshal file tree: %v", err)
		return
	}

	e.sessionMgr.Send(job.SessionID, model.OutputMessage{
		Type:    "fileTreeUpdate",
		Content: string(treeJSON),
	})

	log.Printf("[executor] sent fileTreeUpdate for session %s (%d entries)", job.SessionID, len(tree))
}

// ScanDirectory reads a directory and returns a shallow file tree.
// If shallow is true, lazy folders (node_modules, target, etc.) are marked
// as lazy and their children are NOT listed.
func ScanDirectory(dirPath string, shallow bool) []model.FileTreeNode {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		log.Printf("[executor] failed to read directory %s: %v", dirPath, err)
		return nil
	}

	nodes := make([]model.FileTreeNode, 0, len(entries))

	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files/folders except .gitignore
		if strings.HasPrefix(name, ".") && name != ".gitignore" {
			continue
		}

		node := model.FileTreeNode{
			Name: name,
		}

		if entry.IsDir() {
			node.Type = "folder"

			// Always check if this is a lazy folder
			if lazyFolders[name] {
				node.Lazy = true
				// For lazy folders, scan immediate children only if it's target folder
				if name == "target" {
					childPath := filepath.Join(dirPath, name)
					node.Children = ScanDirectory(childPath, false) // Scan children for target
				} else if shallow {
					// For other lazy folders in shallow mode, don't scan children
					node.Children = []model.FileTreeNode{}
				} else {
					// In deep scan mode, scan children but keep lazy flag
					childPath := filepath.Join(dirPath, name)
					node.Children = ScanDirectory(childPath, shallow)
				}
			} else {
				// Non-lazy folders: scan normally
				childPath := filepath.Join(dirPath, name)
				node.Children = ScanDirectory(childPath, shallow)
			}
		} else {
			node.Type = "file"
		}

		nodes = append(nodes, node)
	}

	return nodes
}

// sendError sends an error message followed by a done signal.
func (e *Executor) sendError(sessionID, msg string) {
	log.Printf("[executor] error for session %s: %s", sessionID, msg)
	e.sessionMgr.Send(sessionID, model.OutputMessage{
		Type:    "error",
		Content: msg,
	})
	e.sessionMgr.Send(sessionID, model.OutputMessage{
		Type:    "done",
		Content: "",
	})
}
