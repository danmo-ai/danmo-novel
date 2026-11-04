package books

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AtomicWriteFiles commits one accepted candidate's file set with rollback.
// Readers may briefly see a previous version during replacement; no failure may
// leave only a prefix of the requested changes on disk.
func AtomicWriteFiles(root string, writes map[string][]byte) error {
	if len(writes) == 0 {
		return nil
	}
	stamp := time.Now().UTC().Format("20060102-150405.000000000")
	backupRoot := filepath.Join(root, "backups", "pre-accept-"+stamp)
	stageRoot := filepath.Join(backupRoot, "staging")
	type savedFile struct {
		path   string
		body   []byte
		mode   os.FileMode
		exists bool
	}
	saved := map[string]savedFile{}
	keys := make([]string, 0, len(writes))
	for rel, body := range writes {
		rel = filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
		if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
			return fmt.Errorf("非法写入路径 %s", rel)
		}
		keys = append(keys, rel)
		abs := filepath.Join(root, filepath.FromSlash(rel))
		old, err := os.ReadFile(abs)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		entry := savedFile{path: abs, body: old, mode: 0o644, exists: err == nil}
		if info, statErr := os.Stat(abs); statErr == nil {
			entry.mode = info.Mode().Perm()
		}
		saved[rel] = entry
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		staged := filepath.Join(stageRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(staged, body, entry.mode); err != nil {
			return err
		}
		if entry.exists {
			backup := filepath.Join(backupRoot, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(backup, old, entry.mode); err != nil {
				return err
			}
		}
	}
	rollback := func() {
		for _, entry := range saved {
			if entry.exists {
				_ = os.WriteFile(entry.path, entry.body, entry.mode)
			} else {
				_ = os.Remove(entry.path)
			}
		}
		_ = os.RemoveAll(stageRoot)
	}
	for _, rel := range keys {
		entry := saved[rel]
		staged := filepath.Join(stageRoot, filepath.FromSlash(rel))
		if err := os.Rename(staged, entry.path); err != nil {
			rollback()
			return fmt.Errorf("提交 %s：%w", rel, err)
		}
	}
	_ = os.RemoveAll(stageRoot)
	return nil
}
