package jobrunner

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gayhub/4subs/internal/asr/openai"
	"github.com/gayhub/4subs/internal/config"
	"github.com/gayhub/4subs/internal/db"
	"github.com/gayhub/4subs/internal/media"
	"github.com/gayhub/4subs/internal/model"
	"github.com/gayhub/4subs/internal/subtitle"
	"github.com/gayhub/4subs/internal/translator/deepseek"
)

type Runner struct {
	cfg        config.Config
	repo       *db.Repository
	translator deepseek.Client
	asr        openai.Client
	queue      chan string
	running    sync.Map
}

func New(cfg config.Config, repo *db.Repository, translator deepseek.Client, asrClient openai.Client) *Runner {
	runner := &Runner{
		cfg:        cfg,
		repo:       repo,
		translator: translator,
		asr:        asrClient,
		queue:      make(chan string, 128),
	}
	go runner.loop()
	return runner
}

func (r *Runner) ResumePending(ctx context.Context) {
	jobs, err := r.repo.ListPendingJobs(ctx)
	if err != nil {
		log.Printf("resume pending jobs failed: %v", err)
		return
	}
	for _, job := range jobs {
		r.Enqueue(job.ID)
	}
}

func (r *Runner) Enqueue(jobID string) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return
	}
	if _, loaded := r.running.LoadOrStore(jobID, struct{}{}); loaded {
		return
	}
	select {
	case r.queue <- jobID:
	default:
		go func() { r.queue <- jobID }()
	}
}

func (r *Runner) loop() {
	for jobID := range r.queue {
		if err := r.process(jobID); err != nil {
			log.Printf("process job %s failed: %v", jobID, err)
		}
		r.running.Delete(jobID)
	}
}

func (r *Runner) process(jobID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	job, err := r.repo.GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	settings, err := r.repo.GetSettings(ctx)
	if err != nil {
		return err
	}

	sourceSubtitlePath := job.SourceSubtitlePath
	if err := r.repo.UpdateJobProgress(ctx, jobID, "running", "extract_subtitle", 10, "正在尝试获取源字幕", sourceSubtitlePath, job.OutputSubtitlePath, ""); err != nil {
		return err
	}

	blocks, sourceSubtitlePath, err := r.resolveSourceBlocks(ctx, job)
	if err != nil {
		_ = r.repo.UpdateJobProgress(ctx, jobID, "failed", "extract_subtitle", 10, "获取源字幕失败", sourceSubtitlePath, "", err.Error())
		return err
	}
	if err := r.repo.UpdateJobProgress(ctx, jobID, "running", "translate", 55, fmt.Sprintf("开始翻译，共 %d 条字幕", len(blocks)), sourceSubtitlePath, "", ""); err != nil {
		return err
	}

	translations, err := r.translator.TranslateBlocks(ctx, settings.TranslationPrompt, job.SourceLanguage, job.TargetLanguage, blocks, settings.MaxSubtitlePerBatch)
	if err != nil {
		_ = r.repo.UpdateJobProgress(ctx, jobID, "failed", "translate", 55, "字幕翻译失败", sourceSubtitlePath, "", err.Error())
		return err
	}
	if err := r.repo.UpdateJobProgress(ctx, jobID, "running", "render", 85, "翻译完成，正在生成双语字幕", sourceSubtitlePath, "", ""); err != nil {
		return err
	}

	bilingualContent, err := subtitle.RenderBilingualSRT(blocks, translations, settings.BilingualLayout)
	if err != nil {
		_ = r.repo.UpdateJobProgress(ctx, jobID, "failed", "render", 85, "双语字幕生成失败", sourceSubtitlePath, "", err.Error())
		return err
	}
	outputPath, err := media.WriteBilingualSRT(job.MediaPath, settings.MediaPaths, r.cfg.SubtitleOutputPath, job.TargetLanguage, bilingualContent)
	if err != nil {
		_ = r.repo.UpdateJobProgress(ctx, jobID, "failed", "render", 85, "字幕文件写入失败", sourceSubtitlePath, "", err.Error())
		return err
	}
	return r.repo.UpdateJobProgress(ctx, jobID, "completed", "completed", 100, "双语字幕已生成", sourceSubtitlePath, outputPath, "")
}

func (r *Runner) resolveSourceBlocks(ctx context.Context, job model.SubtitleJob) ([]subtitle.Block, string, error) {
	path, err := media.ExtractSubtitleSource(ctx, r.cfg.FFmpegBin, job.MediaPath, r.cfg.WorkDir)
	if err == nil {
		if parseErr := r.repo.UpdateJobProgress(ctx, job.ID, "running", "parse_subtitle", 30, "已取得源字幕，正在解析 SRT", path, "", ""); parseErr != nil {
			return nil, path, parseErr
		}
		blocks, parseErr := subtitle.ParseFile(path)
		if parseErr != nil {
			return nil, path, parseErr
		}
		return blocks, path, nil
	}
	if !r.asr.Ready() {
		return nil, "", fmt.Errorf("未找到可用字幕，且 ASR 未配置: %w", err)
	}
	if progressErr := r.repo.UpdateJobProgress(ctx, job.ID, "running", "extract_audio", 20, "未找到可用字幕，转为提取音频进行 ASR", "", "", ""); progressErr != nil {
		return nil, "", progressErr
	}
	audioPath, audioErr := media.ExtractAudio(ctx, r.cfg.FFmpegBin, job.MediaPath, r.cfg.WorkDir)
	if audioErr != nil {
		return nil, "", audioErr
	}
	if progressErr := r.repo.UpdateJobProgress(ctx, job.ID, "running", "transcribe", 40, "音频提取完成，正在调用 ASR 转写", "", "", ""); progressErr != nil {
		return nil, "", progressErr
	}
	blocks, transcribeErr := r.asr.Transcribe(ctx, audioPath, job.SourceLanguage)
	if transcribeErr != nil {
		return nil, "", transcribeErr
	}
	sourceContent := subtitle.RenderSRT(blocks)
	sourcePath, writeErr := media.WriteSourceSRT(job.MediaPath, r.cfg.WorkDir, sourceContent)
	if writeErr != nil {
		return nil, "", writeErr
	}
	return blocks, sourcePath, nil
}
