package monitor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Checkpoint represents the last processed block state
type Checkpoint struct {
	LastBlock uint64 `json:"last_block"`
	UpdatedAt string `json:"updated_at"`
}

// CheckpointManager handles persisting and loading block checkpoints
type CheckpointManager struct {
	filepath string
}

// NewCheckpointManager creates a new checkpoint manager
func NewCheckpointManager(dataDir string) (*CheckpointManager, error) {
	// Create data directory if it doesn't exist
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	return &CheckpointManager{
		filepath: filepath.Join(dataDir, "checkpoint.json"),
	}, nil
}

// Save persists the current block number to disk
func (c *CheckpointManager) Save(blockNumber uint64) error {
	checkpoint := Checkpoint{
		LastBlock: blockNumber,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint: %w", err)
	}

	// Write atomically by writing to temp file then renaming
	tempFile := c.filepath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write checkpoint: %w", err)
	}

	if err := os.Rename(tempFile, c.filepath); err != nil {
		return fmt.Errorf("failed to rename checkpoint file: %w", err)
	}

	return nil
}

// Load reads the last checkpoint from disk
func (c *CheckpointManager) Load() (uint64, error) {
	data, err := os.ReadFile(c.filepath)
	if err != nil {
		if os.IsNotExist(err) {
			// No checkpoint file, start from 0
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read checkpoint: %w", err)
	}

	var checkpoint Checkpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return 0, fmt.Errorf("failed to unmarshal checkpoint: %w", err)
	}

	return checkpoint.LastBlock, nil
}

// GetPath returns the checkpoint file path
func (c *CheckpointManager) GetPath() string {
	return c.filepath
}
