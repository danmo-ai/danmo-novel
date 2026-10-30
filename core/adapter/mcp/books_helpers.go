package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
)

// readBookFile mirrors GET /books/:id/file (path traversal guarded).
func readBookFile(ctx context.Context, store *sqlite.Store, bookID, rel string) (string, error) {
	if rel == "" || strings.Contains(rel, "..") {
		return "", fmt.Errorf("invalid path")
	}
	root, _, err := store.GetBookRoot(ctx, bookID)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// writeBookFile mirrors PUT /books/:id/file (read-only + traversal guarded).
func writeBookFile(ctx context.Context, store *sqlite.Store, bookID, rel, content string) error {
	if rel == "" || strings.Contains(rel, "..") {
		return fmt.Errorf("invalid path")
	}
	root, readOnly, err := store.GetBookRoot(ctx, bookID)
	if err != nil {
		return err
	}
	if readOnly {
		return fmt.Errorf("read-only book")
	}
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o644)
}

// acceptVolume mirrors POST /books/:id/accept-volume.
func acceptVolume(ctx context.Context, store *sqlite.Store, bookID string, volume int, raw string, cast []castCardInput) (string, error) {
	root, readOnly, err := store.GetBookRoot(ctx, bookID)
	if err != nil {
		return "", err
	}
	if readOnly {
		return "", fmt.Errorf("read-only book")
	}
	if volume < 1 || raw == "" {
		return "", fmt.Errorf("volume and raw required")
	}
	volTag := fmt.Sprintf("v%02d", volume)
	for _, card := range cast {
		dir := filepath.Join(root, "canon", "cast")
		_ = os.MkdirAll(dir, 0o755)
		md := card.CardMarkdown
		if md == "" {
			md = fmt.Sprintf("# %s\n\n`status`: canon\n`role`: %s\n", card.Name, card.Role)
		}
		md = strings.Replace(md, "`status`: candidate", "`status`: canon", 1)
		_ = os.WriteFile(filepath.Join(dir, card.Stem+".md"), []byte(md), 0o644)
	}
	dir := filepath.Join(root, "outline", "volumes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, volTag+".md"), []byte(raw), 0o644); err != nil {
		return "", err
	}
	return volTag, nil
}

// promoteCast mirrors POST /books/:id/cast/:stem/promote.
func promoteCast(ctx context.Context, store *sqlite.Store, bookID, stem string) error {
	root, readOnly, err := store.GetBookRoot(ctx, bookID)
	if err != nil {
		return err
	}
	if readOnly {
		return fmt.Errorf("read-only book")
	}
	if stem == "" || strings.Contains(stem, "/") || strings.Contains(stem, "..") {
		return fmt.Errorf("invalid stem")
	}
	path := filepath.Join(root, "canon", "cast", stem+".md")
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cast card not found")
	}
	text := string(raw)
	if !strings.Contains(text, "`status`: candidate") {
		return fmt.Errorf("only candidate cards can be promoted")
	}
	if !strings.Contains(text, "视觉") && !strings.Contains(text, "不知") {
		return fmt.Errorf("请先补全人物卡（三锚点 / 知识边界），再晋升 canon")
	}
	text = strings.Replace(text, "`status`: candidate", "`status`: canon", 1)
	return os.WriteFile(path, []byte(text), 0o644)
}
