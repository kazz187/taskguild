package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type Manager struct {
	repoPath      string
	worktreesPath string
}

func NewManager(repoPath string) (*Manager, error) {
	worktreesPath := filepath.Join(repoPath, ".taskguild", "worktrees")
	if err := os.MkdirAll(worktreesPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create worktrees directory: %w", err)
	}
	return &Manager{
		repoPath:      repoPath,
		worktreesPath: worktreesPath,
	}, nil
}

func (m *Manager) CreateWorktree(taskID, branchName string) (string, error) {
	worktreePath := filepath.Join(m.worktreesPath, taskID)

	// Check if worktree already exists
	if _, err := os.Stat(worktreePath); err == nil {
		return worktreePath, nil
	}

	// Use git command to create worktree and branch
	cmd := exec.Command("git", "worktree", "add", "-b", branchName, worktreePath)
	cmd.Dir = m.repoPath
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to create git worktree: %w", err)
	}

	return worktreePath, nil
}

func (m *Manager) RemoveWorktree(taskID string) error {
	worktreePath := filepath.Join(m.worktreesPath, taskID)

	// Check if worktree exists
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return nil
	}

	// Remove worktree using git command
	cmd := exec.Command("git", "worktree", "remove", worktreePath)
	cmd.Dir = m.repoPath
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to remove git worktree: %w", err)
	}

	return nil
}

func (m *Manager) GetWorktreePath(taskID string) string {
	return filepath.Join(m.worktreesPath, taskID)
}
