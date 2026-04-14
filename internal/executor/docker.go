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



// Executor handles running `docker exec` commands inside the shared Soroban
// runner container. It streams stdout/stderr in real-time via the session manager.
type Executor struct {
	sessionMgr    *session.Manager
	containerName string
	workspaceDir  string
	mu            sync.Mutex // Protects the fixed /app/project path
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

	workDir := fmt.Sprintf("/app/workspaces/%s", job.SessionID)

	// To achieve sub-second speeds across different sessions, we must use a CONSTANT path
	// inside the runner (e.g., /app/project) because Cargo's incremental state is path-absolute.
	// We create a symlink to the session's workspace, then run the command inside that symlink.
	e.mu.Lock()
	defer e.mu.Unlock()

	// 1. Point the fixed /app/project path to our session's workspace
	linkCmd := exec.Command("docker", "exec", e.containerName, "ln", "-sfn", workDir, "/app/project")
	if err := linkCmd.Run(); err != nil {
		log.Printf("[executor] warning: failed to create session symlink: %v", err)
	}

	// 2. Build the docker exec argument list for the actual command
	// We use /app/project as the workdir and HOME to ensure absolute paths are constant.
	fixedProjectDir := "/app/project"
	homeEnv := fmt.Sprintf("HOME=%s", fixedProjectDir)
	dockerArgs := []string{
		"exec",
		"--workdir", fixedProjectDir,
		"--env", homeEnv,
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

	// NOTE: Workspace is NOT cleaned up here.
	// It persists so the user can run more commands.

	// POST-COMMAND HOOKS:
	// If it was an 'init' command and it succeeded, send a file tree update
	// MUST be sent before the 'done' signal to ensure frontend receives it
	if strings.Contains(command, "contract init") && cmd.ProcessState != nil && cmd.ProcessState.Success() {
		log.Printf("[executor] 'stellar contract init' detected, scanning workspace for session %s", job.SessionID)
		tree, err := e.scanDirectory(workDir)
		if err == nil {
			treeJSON, _ := json.Marshal(tree)
			e.sessionMgr.Send(job.SessionID, model.OutputMessage{
				Type:    "fileTreeUpdate",
				Content: string(treeJSON),
			})
		} else {
			log.Printf("[executor] failed to scan workspace after init: %v", err)
		}
	}

	// Signal that execution is complete
	e.sessionMgr.Send(job.SessionID, model.OutputMessage{
		Type:    "done",
		Content: "",
	})
}

// scanDirectory recursively walks a directory and returns a tree of FileTreeNodes.
func (e *Executor) scanDirectory(dirPath string) ([]model.FileTreeNode, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	var nodes []model.FileTreeNode
	for _, entry := range entries {
		name := entry.Name()
		// Skip hidden files, target, and git directories
		if strings.HasPrefix(name, ".") || name == "target" || name == ".git" {
			continue
		}

		fullPath := filepath.Join(dirPath, name)
		node := model.FileTreeNode{
			Name: name,
		}

		if entry.IsDir() {
			node.Type = "folder"
			children, err := e.scanDirectory(fullPath)
			if err != nil {
				return nil, err
			}
			node.Children = children
		} else {
			node.Type = "file"
			// Read file content and store it in the node
			content, err := os.ReadFile(fullPath)
			if err == nil {
				node.Content = string(content)
			}
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
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
