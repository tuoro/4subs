package scanner

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gayhub/4subs/internal/model"
)

var (
	episodePattern = regexp.MustCompile(`(?i)[.\s_\-]s(\d{1,2})e(\d{1,2})[.\s_\-]?`)
	yearPattern    = regexp.MustCompile(`\b(19|20)\d{2}\b`)
)

var videoExtSet = map[string]struct{}{
	".mkv":  {},
	".mp4":  {},
	".avi":  {},
	".mov":  {},
	".wmv":  {},
	".flv":  {},
	".m4v":  {},
	".ts":   {},
	".m2ts": {},
	".webm": {},
}

var subtitleExtSet = map[string]struct{}{
	".srt": {},
	".ass": {},
	".ssa": {},
	".vtt": {},
	".sub": {},
}

type Result struct {
	Items                []model.MediaItem
	ScannedVideoFiles    int
	MissingSubtitleFiles int
}

func Run(paths []string) (Result, error) {
	items := make([]model.MediaItem, 0, 256)
	pathSeen := make(map[string]struct{})

	for _, root := range paths {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if _, err := os.Stat(root); err != nil {
			continue
		}
		if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}

			ext := strings.ToLower(filepath.Ext(d.Name()))
			if _, ok := videoExtSet[ext]; !ok {
				return nil
			}

			absPath := path
			if !filepath.IsAbs(absPath) {
				resolved, err := filepath.Abs(path)
				if err == nil {
					absPath = resolved
				}
			}
			if _, exists := pathSeen[absPath]; exists {
				return nil
			}
			pathSeen[absPath] = struct{}{}

			mediaType, title, year, season, episode := parseMetadata(d.Name())
			hasSubtitle := hasLocalSubtitle(path)
			items = append(items, model.MediaItem{
				MediaType:   mediaType,
				Title:       title,
				Year:        year,
				Season:      season,
				Episode:     episode,
				FilePath:    absPath,
				HasSubtitle: hasSubtitle,
			})

			return nil
		}); err != nil {
			return Result{}, fmt.Errorf("scan path %s: %w", root, err)
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].FilePath < items[j].FilePath
	})

	missing := 0
	for _, item := range items {
		if !item.HasSubtitle {
			missing++
		}
	}

	return Result{
		Items:                items,
		ScannedVideoFiles:    len(items),
		MissingSubtitleFiles: missing,
	}, nil
}

func hasLocalSubtitle(videoPath string) bool {
	dir := filepath.Dir(videoPath)
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	matches, err := filepath.Glob(filepath.Join(dir, base+".*"))
	if err != nil {
		return false
	}
	for _, match := range matches {
		ext := strings.ToLower(filepath.Ext(match))
		if _, ok := subtitleExtSet[ext]; ok {
			return true
		}
	}
	return false
}

func parseMetadata(filename string) (mediaType, title string, year, season, episode *int) {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	normalized := strings.ReplaceAll(name, ".", " ")
	normalized = strings.ReplaceAll(normalized, "_", " ")
	normalized = strings.ReplaceAll(normalized, "-", " ")
	normalized = strings.Join(strings.Fields(normalized), " ")

	mediaType = "movie"
	if match := episodePattern.FindStringSubmatch(" " + normalized + " "); len(match) == 3 {
		mediaType = "episode"
		if parsed, err := strconv.Atoi(match[1]); err == nil {
			season = &parsed
		}
		if parsed, err := strconv.Atoi(match[2]); err == nil {
			episode = &parsed
		}
	}

	if rawYear := yearPattern.FindString(normalized); rawYear != "" {
		if parsed, err := strconv.Atoi(rawYear); err == nil {
			year = &parsed
		}
	}

	title = normalized
	if mediaType == "episode" {
		if idx := episodePattern.FindStringIndex(" " + normalized + " "); len(idx) == 2 {
			cleaned := strings.TrimSpace((" " + normalized + " ")[:idx[0]])
			if cleaned != "" {
				title = cleaned
			}
		}
	}
	if year != nil {
		title = strings.TrimSpace(strings.ReplaceAll(title, strconv.Itoa(*year), ""))
	}
	if title == "" {
		title = name
	}

	return mediaType, title, year, season, episode
}
