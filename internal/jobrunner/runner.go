package jobrunner

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gayhub/4subs/internal/config"
	"github.com/gayhub/4subs/internal/db"
	"github.com/gayhub/4subs/internal/media"
	"github.com/gayhub/4subs/internal/subtitle"
	"github.com/gayhub/4subs/internal/translator/deepseek"
)

type Runner struct {
	cfg        config.Config
	repo       *db.Repository
	translator deepseek.Client
	queue      chan string
	running    sync.Map
}

func New(cfg config.Config, repo *db.Repository, translator deepseek.Client) *Runner {
	runner := &Runner{
		cfg:        cfg,
		repo:       repo,
		translator: translator,
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	job, err := r.repo.GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	settings, err := r.repo.GetSettings(ctx)
	if err != nil {
		return err
	}
	if err := r.repo.UpdateJobProgress(ctx, jobID, "running", "extract_subtitle", 10, "正在提取源字幕", job.SourceSubtitlePath, job.OutputSubtitlePath, ""); err != nil {
		return err
	}

	sourceSubtitlePath, err := media.ExtractSubtitleSource(ctx, r.cfg.FFmpegBin, job.MediaPath, r.cfg.WorkDir)
	if err != nil {
		_ = r.repo.UpdateJobProgress(ctx, jobID, "failed", "extract_subtitle", 10, "源字幕提取失败", "", "", err.Error())
		return err
	}
	if err := r.repo.UpdateJobProgress(ctx, jobID, "running", "parse_subtitle", 25, "已取得源字幕，正在解析 SRT", sourceSubtitlePath, "", ""); err != nil {
		return err
	}

	blocks, err := subtitle.ParseFile(sourceSubtitlePath)
	if err != nil {
		_ = r.repo.UpdateJobProgress(ctx, jobID, "failed", "parse_subtitle", 25, "字幕解析失败", sourceSubtitlePath, "", err.Error())
		return err
	}
	if err := r.repo.UpdateJobProgress(ctx, jobID, "running", "translate", 50, fmt.Sprintf("开始翻译，共 %d 条字幕", len(blocks)), sourceSubtitlePath, "", ""); err != nil {
		return err
	}

	translations, err := r.translator.TranslateBlocks(ctx, settings.TranslationPrompt, job.SourceLanguage, job.TargetLanguage, blocks, settings.MaxSubtitlePerBatch)
	if err != nil {
		_ = r.repo.UpdateJobProgress(ctx, jobID, "failed", "translate", 50, "字幕翻译失败", sourceSubtitlePath, "", err.Error())
		return err
	}
	if err := r.repo.UpdateJobProgress(ctx, jobID, "running", "render", 80, "翻译完成，正在生成双语字幕", sourceSubtitlePath, "", ""); err != nil {
		return err
	}

	bilingualContent, err := subtitle.RenderBilingualSRT(blocks, translations, settings.BilingualLayout)
	if err != nil {
		_ = r.repo.UpdateJobProgress(ctx, jobID, "failed", "render", 80, "双语字幕生成失败", sourceSubtitlePath, "", err.Error())
		return err
	}
	outputPath, err := media.WriteBilingualSRT(job.MediaPath, settings.MediaPaths, r.cfg.SubtitleOutputPath, job.TargetLanguage, bilingualContent)
	if err != nil {
		_ = r.repo.UpdateJobProgress(ctx, jobID, "failed", "render", 80, "字幕文件写入失败", sourceSubtitlePath, "", err.Error())
		return err
	}

	return r.repo.UpdateJobProgress(ctx, jobID, "completed", "completed", 100, "双语字幕已生成", sourceSubtitlePath, outputPath, "")
}

func IsNotFound(err error) bool {
	return err != nil && err == sql.ErrNoRows
}
