package joblog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gayhub/4subs/internal/model"
)

type Store struct {
	dir string
	mu  sync.Mutex
}

func New(baseDir string) *Store {
	return &Store{dir: filepath.Join(baseDir, "job-logs")}
}

func (s *Store) Append(jobID string, level string, stage string, message string, detail string) error {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil
	}
	entry := model.JobLogEntry{
		Timestamp: time.Now().UTC(),
		Level:     normalizeLevel(level),
		Stage:     strings.TrimSpace(stage),
		Message:   strings.TrimSpace(message),
		Detail:    strings.TrimSpace(detail),
	}
	if entry.Message == "" {
		entry.Message = "任务状态已更新"
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(s.filePath(jobID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(payload, '\n')); err != nil {
		return err
	}
	return nil
}

func (s *Store) List(jobID string, limit int) ([]model.JobLogEntry, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return []model.JobLogEntry{}, nil
	}
	file, err := os.Open(s.filePath(jobID))
	if err != nil {
		if os.IsNotExist(err) {
			return []model.JobLogEntry{}, nil
		}
		return nil, err
	}
	defer file.Close()

	entries := make([]model.JobLogEntry, 0, 64)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry model.JobLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	return entries, nil
}

func (s *Store) filePath(jobID string) string {
	return filepath.Join(s.dir, jobID+".jsonl")
}

func normalizeLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug", "warn", "error":
		return strings.ToLower(strings.TrimSpace(level))
	default:
		return "info"
	}
}
