package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var sidecarExtensions = []string{".srt", ".ass", ".ssa", ".vtt"}

func ExtractSubtitleSource(ctx context.Context, ffmpegBin string, videoPath string, workDir string) (string, error) {
	baseName := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	videoDir := filepath.Dir(videoPath)
	for _, ext := range sidecarExtensions {
		candidate := filepath.Join(videoDir, baseName+ext)
		if _, err := os.Stat(candidate); err == nil {
			return ensureSRT(ctx, ffmpegBin, candidate, filepath.Join(workDir, safeName(baseName)+".source.srt"))
		}
	}
	return extractEmbeddedSubtitle(ctx, ffmpegBin, videoPath, filepath.Join(workDir, safeName(baseName)+".embedded.srt"))
}

func ExtractAudio(ctx context.Context, ffmpegBin string, videoPath string, workDir string) (string, error) {
	baseName := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	outputPath := filepath.Join(workDir, safeName(baseName)+".wav")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, ffmpegBin, "-y", "-i", videoPath, "-vn", "-ac", "1", "-ar", "16000", "-acodec", "pcm_s16le", outputPath)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("音频提取失败: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return outputPath, nil
}

func ensureSRT(ctx context.Context, ffmpegBin string, inputPath string, outputPath string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return "", err
	}
	if strings.EqualFold(filepath.Ext(inputPath), ".srt") {
		raw, err := os.ReadFile(inputPath)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(outputPath, raw, 0o644); err != nil {
			return "", err
		}
		return outputPath, nil
	}
	command := exec.CommandContext(ctx, ffmpegBin, "-y", "-i", inputPath, outputPath)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("字幕转 SRT 失败: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return outputPath, nil
}

func extractEmbeddedSubtitle(ctx context.Context, ffmpegBin string, videoPath string, outputPath string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, ffmpegBin, "-y", "-i", videoPath, "-map", "0:s:0", outputPath)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("未找到可提取的外挂或内嵌文本字幕；将尝试 ASR。ffmpeg 输出: %s", strings.TrimSpace(string(output)))
	}
	if _, err := os.Stat(outputPath); err != nil {
		return "", errors.New("字幕提取命令执行成功，但未生成字幕文件")
	}
	return outputPath, nil
}

func WriteTextFile(outputPath string, content string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(outputPath, []byte(content), 0o644); err != nil {
		return "", err
	}
	return outputPath, nil
}

func WriteBilingualSRT(mediaPath string, mediaRoots []string, outputRoot string, targetLanguage string, content string) (string, error) {
	return WriteBilingualFile(mediaPath, mediaRoots, outputRoot, targetLanguage, ".srt", "bilingual", content)
}

func WriteBilingualASS(mediaPath string, mediaRoots []string, outputRoot string, targetLanguage string, content string) (string, error) {
	return WriteBilingualFile(mediaPath, mediaRoots, outputRoot, targetLanguage, ".ass", "bilingual", content)
}

func WriteBilingualFile(mediaPath string, mediaRoots []string, outputRoot string, targetLanguage string, extension string, suffix string, content string) (string, error) {
	relativeDir := relativeMediaDir(mediaPath, mediaRoots)
	targetDir := filepath.Join(outputRoot, relativeDir)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err
	}
	baseName := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	languageCode := strings.ToLower(strings.ReplaceAll(targetLanguage, "_", "-"))
	fileName := fmt.Sprintf("%s.%s.%s%s", baseName, languageCode, suffix, extension)
	targetPath := filepath.Join(targetDir, fileName)
	if err := os.WriteFile(targetPath, []byte(content), 0o644); err != nil {
		return "", err
	}
	return targetPath, nil
}

func WriteSourceSRT(mediaPath string, workDir string, content string) (string, error) {
	baseName := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	return WriteTextFile(filepath.Join(workDir, safeName(baseName)+".asr.srt"), content)
}

func WriteOCRSRT(mediaPath string, workDir string, content string) (string, error) {
	baseName := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	return WriteTextFile(filepath.Join(workDir, safeName(baseName)+".ocr.srt"), content)
}

func relativeMediaDir(mediaPath string, mediaRoots []string) string {
	for _, root := range mediaRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		relative, err := filepath.Rel(root, mediaPath)
		if err != nil || strings.HasPrefix(relative, "..") {
			continue
		}
		dir := filepath.Dir(relative)
		if dir == "." {
			return ""
		}
		return dir
	}
	return ""
}

func safeName(name string) string {
	replacer := strings.NewReplacer(" ", "_", "/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	return fmt.Sprintf("%d_%s", time.Now().UnixNano(), replacer.Replace(name))
}
