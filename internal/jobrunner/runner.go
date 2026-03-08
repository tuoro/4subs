package jobrunner

import (
	"context"
	"errors"
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
	active     sync.Map
	cancels    sync.Map
}

func New(cfg config.Config, repo *db.Repository, translator deepseek.Client, asrClient openai.Client) *Runner {
	runner := &Runner{
		cfg:        cfg,
		repo:       repo,
		translator: translator,
		asr:        asrClient,
		queue:      make(chan string, 256),
	}
	workerCount := cfg.JobConcurrency
	if workerCount <= 0 {
		workerCount = 1
	}
	for index := 0; index < workerCount; index++ {
		go runner.worker(index + 1)
	}
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
	if _, loaded := r.active.LoadOrStore(jobID, struct{}{}); loaded {
		return
	}
	select {
	case r.queue <- jobID:
	default:
		go func() { r.queue <- jobID }()
	}
}

func (r *Runner) Cancel(jobID string) error {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return errors.New("任务 ID 不能为空")
	}
	job, err := r.repo.GetJob(context.Background(), jobID)
	if err != nil {
		return err
	}
	if job.Status == "completed" || job.Status == "failed" || job.Status == "cancelled" {
		return nil
	}
	paths := db.JobOutputPaths{
		SourcePath:  job.SourceSubtitlePath,
		PrimaryPath: job.OutputSubtitlePath,
		SRTPath:     job.OutputSRTPath,
		ASSPath:     job.OutputASSPath,
	}
	if cancelFnValue, ok := r.cancels.Load(jobID); ok {
		if cancelFn, ok := cancelFnValue.(context.CancelFunc); ok {
			_ = r.repo.UpdateJobProgress(context.Background(), jobID, "cancelling", job.CurrentStage, job.Progress, "任务取消中", paths, "")
			cancelFn()
			return nil
		}
	}
	r.active.Delete(jobID)
	return r.repo.UpdateJobProgress(context.Background(), jobID, "cancelled", "cancelled", job.Progress, "任务已取消", paths, "")
}

func (r *Runner) worker(index int) {
	for jobID := range r.queue {
		if err := r.process(jobID); err != nil {
			log.Printf("worker %d process job %s failed: %v", index, jobID, err)
		}
		r.active.Delete(jobID)
	}
}

func (r *Runner) process(jobID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	r.cancels.Store(jobID, cancel)
	defer r.cancels.Delete(jobID)

	job, err := r.repo.GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	if job.Status == "cancelled" {
		return nil
	}
	if job.Status == "cancelling" {
		return r.markCancelled(jobID, job, db.JobOutputPaths{
			SourcePath:  job.SourceSubtitlePath,
			PrimaryPath: job.OutputSubtitlePath,
			SRTPath:     job.OutputSRTPath,
			ASSPath:     job.OutputASSPath,
		})
	}
	settings, err := r.repo.GetSettings(ctx)
	if err != nil {
		return err
	}

	paths := db.JobOutputPaths{
		SourcePath:  job.SourceSubtitlePath,
		PrimaryPath: job.OutputSubtitlePath,
		SRTPath:     job.OutputSRTPath,
		ASSPath:     job.OutputASSPath,
	}
	if err := r.repo.UpdateJobProgress(ctx, jobID, "running", "extract_subtitle", 10, "正在尝试获取源字幕", paths, ""); err != nil {
		return err
	}

	blocks, sourceSubtitlePath, err := r.resolveSourceBlocks(ctx, job)
	paths.SourcePath = sourceSubtitlePath
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return r.markCancelled(jobID, job, paths)
		}
		_ = r.repo.UpdateJobProgress(context.Background(), jobID, "failed", "extract_subtitle", 10, "获取源字幕失败", paths, err.Error())
		return err
	}
	if err := r.repo.UpdateJobProgress(ctx, jobID, "running", "translate", 55, fmt.Sprintf("开始翻译，共 %d 条字幕", len(blocks)), paths, ""); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return r.markCancelled(jobID, job, paths)
		}
		return err
	}

	translations, err := r.translator.TranslateBlocks(ctx, settings.TranslationPrompt, job.SourceLanguage, job.TargetLanguage, blocks, settings.MaxSubtitlePerBatch)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return r.markCancelled(jobID, job, paths)
		}
		_ = r.repo.UpdateJobProgress(context.Background(), jobID, "failed", "translate", 55, "字幕翻译失败", paths, err.Error())
		return err
	}
	if err := r.repo.UpdateJobProgress(ctx, jobID, "running", "render", 85, "翻译完成，正在生成输出字幕", paths, ""); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return r.markCancelled(jobID, job, paths)
		}
		return err
	}

	outputs, err := r.renderOutputs(job, settings, blocks, translations)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return r.markCancelled(jobID, job, paths)
		}
		_ = r.repo.UpdateJobProgress(context.Background(), jobID, "failed", "render", 85, "字幕文件生成失败", paths, err.Error())
		return err
	}
	paths.PrimaryPath = outputs.PrimaryPath
	paths.SRTPath = outputs.SRTPath
	paths.ASSPath = outputs.ASSPath
	return r.repo.UpdateJobProgress(context.Background(), jobID, "completed", "completed", 100, "字幕输出已生成，可进入详情页校对", paths, "")
}

func (r *Runner) resolveSourceBlocks(ctx context.Context, job model.SubtitleJob) ([]subtitle.Block, string, error) {
	path, err := media.ExtractSubtitleSource(ctx, r.cfg.FFmpegBin, job.MediaPath, r.cfg.WorkDir)
	if err == nil {
		if parseErr := r.repo.UpdateJobProgress(ctx, job.ID, "running", "parse_subtitle", 30, "已取得源字幕，正在解析 SRT", db.JobOutputPaths{SourcePath: path}, ""); parseErr != nil {
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
	if progressErr := r.repo.UpdateJobProgress(ctx, job.ID, "running", "extract_audio", 20, "未找到可用字幕，转为提取音频进行 ASR", db.JobOutputPaths{}, ""); progressErr != nil {
		return nil, "", progressErr
	}
	audioPath, audioErr := media.ExtractAudio(ctx, r.cfg.FFmpegBin, job.MediaPath, r.cfg.WorkDir)
	if audioErr != nil {
		return nil, "", audioErr
	}
	if progressErr := r.repo.UpdateJobProgress(ctx, job.ID, "running", "transcribe", 40, "音频提取完成，正在调用 ASR 转写", db.JobOutputPaths{}, ""); progressErr != nil {
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

func (r *Runner) renderOutputs(job model.SubtitleJob, settings model.AppSettings, blocks []subtitle.Block, translations []string) (db.JobOutputPaths, error) {
	requested := normalizeFormats(job.OutputFormats)
	if len(requested) == 0 {
		requested = []string{"srt", "ass"}
	}
	paths := db.JobOutputPaths{}
	for _, format := range requested {
		switch format {
		case "srt":
			content, err := subtitle.RenderBilingualSRT(blocks, translations, settings.BilingualLayout)
			if err != nil {
				return db.JobOutputPaths{}, err
			}
			path, err := media.WriteBilingualSRT(job.MediaPath, settings.MediaPaths, r.cfg.SubtitleOutputPath, job.TargetLanguage, content)
			if err != nil {
				return db.JobOutputPaths{}, err
			}
			paths.SRTPath = path
		case "ass":
			content, err := subtitle.RenderBilingualASS(blocks, translations, settings.BilingualLayout)
			if err != nil {
				return db.JobOutputPaths{}, err
			}
			path, err := media.WriteBilingualASS(job.MediaPath, settings.MediaPaths, r.cfg.SubtitleOutputPath, job.TargetLanguage, content)
			if err != nil {
				return db.JobOutputPaths{}, err
			}
			paths.ASSPath = path
		}
	}
	paths.PrimaryPath = pickPrimaryPath(requested, paths)
	return paths, nil
}

func (r *Runner) markCancelled(jobID string, job model.SubtitleJob, paths db.JobOutputPaths) error {
	if paths.SourcePath == "" {
		paths.SourcePath = job.SourceSubtitlePath
	}
	if paths.PrimaryPath == "" {
		paths.PrimaryPath = job.OutputSubtitlePath
	}
	if paths.SRTPath == "" {
		paths.SRTPath = job.OutputSRTPath
	}
	if paths.ASSPath == "" {
		paths.ASSPath = job.OutputASSPath
	}
	return r.repo.UpdateJobProgress(context.Background(), jobID, "cancelled", "cancelled", job.Progress, "任务已取消", paths, "")
}

func normalizeFormats(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed != "srt" && trimmed != "ass" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func pickPrimaryPath(formats []string, paths db.JobOutputPaths) string {
	for _, format := range formats {
		if format == "srt" && paths.SRTPath != "" {
			return paths.SRTPath
		}
		if format == "ass" && paths.ASSPath != "" {
			return paths.ASSPath
		}
	}
	if paths.SRTPath != "" {
		return paths.SRTPath
	}
	return paths.ASSPath
}
