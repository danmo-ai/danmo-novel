package books

import (
	"os"
	"path/filepath"
	"time"
)

// ArchiveUnitProse copies units/<unitID>.md into
// backups/<YYYYMMDD-HHMMSS>-chain/units/ before a rewrite overwrites it.
//
// Returns the archive path ("" when the unit has no prose yet, i.e. 新写).
// backups/ is in auxiliaryPrefixes, so the loader ignores these copies.
func ArchiveUnitProse(root, unitID string) (string, error) {
	src := filepath.Join(root, "units", unitID+".md")
	raw, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	dir := filepath.Join(root, "backups", time.Now().UTC().Format("20060102-150405")+"-chain", "units")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, unitID+".md")
	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		return "", err
	}
	return dst, nil
}
