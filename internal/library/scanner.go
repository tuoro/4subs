package library

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gayhub/4subs/internal/model"
)

var supportedVideoExt = map[string]struct{}{
	".mp4": {},
	".mkv": {},
	".avi": {},
	".mov": {},
	".wmv": {},
	".m4v": {},
	".ts":  {},
}

func ScanMediaPaths(paths []string) ([]model.MediaAsset, error) {
	assets := make([]model.MediaAsset, 0)
	for _, root := range paths {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if _, err := os.Stat(root); err != nil {
			continue
		}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if _, ok := supportedVideoExt[ext]; !ok {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return nil
			}
			relativePath, err := filepath.Rel(root, path)
			if err != nil {
				relativePath = entry.Name()
			}
			assets = append(assets, model.MediaAsset{
				Title:        strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())),
				RootPath:     root,
				RelativePath: relativePath,
				FilePath:     path,
				FileSize:     info.Size(),
				Status:       "ready",
				UpdatedAt:    time.Now().UTC(),
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(assets, func(i, j int) bool {
		return strings.ToLower(assets[i].FilePath) < strings.ToLower(assets[j].FilePath)
	})
	return assets, nil
}

