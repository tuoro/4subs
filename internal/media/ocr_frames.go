package media

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type SubtitleFrame struct {
	Index int
	At    time.Duration
	Path  string
}

func ExtractSubtitleFrames(ctx context.Context, ffmpegBin string, videoPath string, workDir string, interval time.Duration, cropTopPercent int, cropHeightPercent int) ([]SubtitleFrame, error) {
	if interval <= 0 {
		interval = time.Second
	}
	if cropTopPercent < 0 {
		cropTopPercent = 72
	}
	if cropHeightPercent <= 0 || cropHeightPercent > 100 {
		cropHeightPercent = 22
	}
	baseName := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	frameDir := filepath.Join(workDir, safeName(baseName)+".ocr.frames")
	if err := os.MkdirAll(frameDir, 0o755); err != nil {
		return nil, err
	}
	fps := 1 / interval.Seconds()
	if fps <= 0 {
		fps = 1
	}
	cropTop := fmt.Sprintf("ih*%.4f", float64(cropTopPercent)/100)
	cropHeight := fmt.Sprintf("ih*%.4f", float64(cropHeightPercent)/100)
	filter := fmt.Sprintf("fps=%.5f,crop=iw:%s:0:%s", fps, cropHeight, cropTop)
	outputPattern := filepath.Join(frameDir, "%06d.png")
	command := exec.CommandContext(ctx, ffmpegBin, "-y", "-i", videoPath, "-vf", filter, outputPattern)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("OCR 抽帧失败: %w: %s", err, strings.TrimSpace(string(output)))
	}
	entries, err := os.ReadDir(frameDir)
	if err != nil {
		return nil, err
	}
	frames := make([]SubtitleFrame, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".png" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		index, err := strconv.Atoi(name)
		if err != nil {
			continue
		}
		frames = append(frames, SubtitleFrame{
			Index: index,
			At:    time.Duration(index-1) * interval,
			Path:  filepath.Join(frameDir, entry.Name()),
		})
	}
	sort.Slice(frames, func(i, j int) bool {
		return frames[i].Index < frames[j].Index
	})
	if len(frames) == 0 {
		return nil, fmt.Errorf("OCR 抽帧完成，但未生成可用图片")
	}
	return frames, nil
}
