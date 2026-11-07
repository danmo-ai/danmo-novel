package books

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// demoBookFS 是随二进制内置的示例书。
//
// 为什么内置而不是让前端逐个写文件：示例书要完整（总纲/卷纲/细纲/正文/人物/
// 账本），而这些文件之间是有结构校验的——book_outline 与卷纲都要过
// ValidateStructuredOutlineFile，细纲的 state_changes 要跟身份时间线对得上。
// 放在 Go 侧一次写盘，才可能用测试保证它真的能通过校验，而不是"前端点了没报错"。
//go:embed demo/demo-fog-harbor
var demoBookFS embed.FS

// DemoBookID 是示例书的固定 id：重复点击"看示例"不会造出第二本，
// 也不会覆盖作者已经改过的内容。
const DemoBookID = "demo-fog-harbor"

// DemoRoot 返回示例书在库里的落点（不保证已存在）。
func DemoRoot(libraryRoot string) string {
	return filepath.Join(libraryRoot, "novel", DemoBookID)
}

// CreateDemoBook 把内置示例书写到库里，返回书根与"本次是否新建"。
//
// 已存在时直接返回，不覆盖：作者可能已经在这本书上改过设定或写过正文，
// 把示例当资源包重新解压会把他的工作抹掉。
func CreateDemoBook(libraryRoot string) (string, bool, error) {
	root := DemoRoot(libraryRoot)
	if _, err := os.Stat(root); err == nil {
		return root, false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", false, err
	}

	dirs := []string{
		root,
		filepath.Join(root, "canon", "cast"),
		filepath.Join(root, "outline", "volumes"),
		filepath.Join(root, "outline", "units"),
		filepath.Join(root, "units"),
		filepath.Join(root, "continuity", "summaries"),
		filepath.Join(root, "continuity", "commits"),
		filepath.Join(root, "reviews"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", false, err
		}
	}

	src := filepath.ToSlash(filepath.Join("demo", DemoBookID))
	err := fs.WalkDir(demoBookFS, src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		data, err := demoBookFS.ReadFile(path)
		if err != nil {
			return err
		}
		dst := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		// 写到一半失败会留下半本书：删掉重来，别让下次 Stat 认为"已存在"。
		_ = os.RemoveAll(root)
		return "", false, fmt.Errorf("写入示例书：%w", err)
	}
	return root, true, nil
}
