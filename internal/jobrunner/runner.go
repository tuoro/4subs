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
	"github.com/gayhub/4subs/internal/joblog"
	"github.com/gayhub/4subs/internal/media"
	"github.com/gayhub/4subs/internal/model"
	ocrprovider "github.com/gayhub/4subs/internal/ocr"
	"github.com/gayhub/4subs/internal/subtitle"
	"github.com/gayhub/4subs/internal/translator/deepseek"
)

type Runner struct {
	cfg        config.Config
	repo       *db.Repository
	translator deepseek.Client
	asr        openai.Client
	ocr        ocrprovider.Provider
	logger     *joblog.Store
	queue      chan string
	active     sync.Map
	cancels    sync.Map
}

func New(cfg config.Config, repo *db.Repository, translator deepseek.Client, asrClient openai.Client, ocrClient ocrprovider.Provider, logger *joblog.Store) *Runner {
	runner := &Runner{
		cfg:        cfg,
		repo:       repo,
		translator: translator,
		asr:        asrClient,
		ocr:        ocrClient,
		logger:     logger,
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
			if err := r.updateProgress(context.Background(), jobID, "cancelling", job.CurrentStage, job.Progress, "任务取消中", paths, ""); err != nil {
				return err
			}
			cancelFn()
			return nil
		}
	}
	r.active.Delete(jobID)
	return r.markCancelled(jobID, job, paths)
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
	if err := r.updateProgress(ctx, jobID, "running", "extract_subtitle", 10, "正在尝试获取源字幕", paths, ""); err != nil {
		return err
	}

	blocks, sourceSubtitlePath, err := r.resolveSourceBlocks(ctx, job)
	paths.SourcePath = sourceSubtitlePath
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return r.markCancelled(jobID, job, paths)
		}
		_ = r.updateProgress(context.Background(), jobID, "failed", "extract_subtitle", 10, "获取源字幕失败", paths, err.Error())
		return err
	}
	if err := r.updateProgress(ctx, jobID, "running", "translate", 55, fmt.Sprintf("开始翻译，共 %d 条字幕", len(blocks)), paths, ""); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return r.markCancelled(jobID, job, paths)
		}
		return err
	}

	translations, err := r.translator.TranslateBlocks(ctx, buildTranslationPrompt(settings), job.SourceLanguage, job.TargetLanguage, blocks, settings.MaxSubtitlePerBatch)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return r.markCancelled(jobID, job, paths)
		}
		_ = r.updateProgress(context.Background(), jobID, "failed", "translate", 55, "字幕翻译失败", paths, err.Error())
		return err
	}
	if err := r.updateProgress(ctx, jobID, "running", "render", 85, "翻译完成，正在生成输出字幕", paths, ""); err != nil {
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
		_ = r.updateProgress(context.Background(), jobID, "failed", "render", 85, "字幕文件生成失败", paths, err.Error())
		return err
	}
	paths.PrimaryPath = outputs.PrimaryPath
	paths.SRTPath = outputs.SRTPath
	paths.ASSPath = outputs.ASSPath
	return r.updateProgress(context.Background(), jobID, "completed", "completed", 100, "字幕输出已生成，可进入详情页校对", paths, "")
}

func (r *Runner) resolveSourceBlocks(ctx context.Context, job model.SubtitleJob) ([]subtitle.Block, string, error) {
	path, err := media.ExtractSubtitleSource(ctx, r.cfg.FFmpegBin, job.MediaPath, r.cfg.WorkDir)
	if err == nil {
		if parseErr := r.updateProgress(ctx, job.ID, "running", "parse_subtitle", 30, "已取得源字幕，正在解析 SRT", db.JobOutputPaths{SourcePath: path}, ""); parseErr != nil {
			return nil, path, parseErr
		}
		blocks, parseErr := subtitle.ParseFile(path)
		if parseErr != nil {
			return nil, path, parseErr
		}
		return blocks, path, nil
	}

	var ocrErr error
	if r.ocr != nil && r.ocr.Ready() {
		if progressErr := r.updateProgress(ctx, job.ID, "running", "ocr_extract", 20, "未找到可用文本字幕，正在抽取硬字幕关键帧", db.JobOutputPaths{}, ""); progressErr != nil {
			return nil, "", progressErr
		}
		frames, frameErr := media.ExtractSubtitleFrames(
			ctx,
			r.cfg.FFmpegBin,
			job.MediaPath,
			r.cfg.WorkDir,
			time.Duration(r.cfg.OCRFrameIntervalMS)*time.Millisecond,
			r.cfg.OCRCropTopPercent,
			r.cfg.OCRCropHeightPercent,
		)
		if frameErr != nil {
			ocrErr = frameErr
		} else {
			if progressErr := r.updateProgress(ctx, job.ID, "running", "ocr_recognize", 35, fmt.Sprintf("已抽取 %d 张关键帧，正在调用远程 OCR", len(frames)), db.JobOutputPaths{}, ""); progressErr != nil {
				return nil, "", progressErr
			}
			observations := make([]ocrprovider.Observation, 0, len(frames))
			var firstRecognizeErr error
			for _, frame := range frames {
				if err := ctx.Err(); err != nil {
					return nil, "", err
				}
				text, confidence, recognizeErr := r.ocr.RecognizeImage(ctx, frame.Path)
				if recognizeErr != nil {
					if firstRecognizeErr == nil {
						firstRecognizeErr = recognizeErr
					}
					continue
				}
				text = strings.TrimSpace(text)
				if text == "" {
					continue
				}
				observations = append(observations, ocrprovider.Observation{
					At:         frame.At,
					Text:       text,
					Confidence: confidence,
				})
			}
			blocks := ocrprovider.BuildBlocks(observations, ocrprovider.DefaultTimelineOptions())
			if len(blocks) > 0 {
				sourceContent := subtitle.RenderSRT(blocks)
				sourcePath, writeErr := media.WriteOCRSRT(job.MediaPath, r.cfg.WorkDir, sourceContent)
				if writeErr != nil {
					return nil, "", writeErr
				}
				if progressErr := r.updateProgress(ctx, job.ID, "running", "parse_subtitle", 45, fmt.Sprintf("OCR 识别成功，已恢复 %d 条时间轴字幕", len(blocks)), db.JobOutputPaths{SourcePath: sourcePath}, ""); progressErr != nil {
					return nil, sourcePath, progressErr
				}
				return blocks, sourcePath, nil
			}
			if firstRecognizeErr != nil {
				ocrErr = fmt.Errorf("OCR 未识别出有效字幕，首个错误: %w", firstRecognizeErr)
			} else {
				ocrErr = fmt.Errorf("OCR 未识别出有效字幕")
			}
		}
	}

	if !r.asr.Ready() {
		reasons := []string{fmt.Sprintf("文本字幕提取失败: %v", err)}
		if r.ocr != nil && r.ocr.Ready() {
			if ocrErr != nil {
				reasons = append(reasons, fmt.Sprintf("OCR 失败: %v", ocrErr))
			} else {
				reasons = append(reasons, "OCR 未产出有效字幕")
			}
		} else {
			reasons = append(reasons, "OCR 未配置")
		}
		return nil, "", fmt.Errorf(strings.Join(reasons, "；") + "；且 ASR 未配置")
	}

	fallbackMessage := "未找到可用文本字幕，转为提取音频进行 ASR"
	if ocrErr != nil {
		fallbackMessage = "文本字幕不可用，且 OCR 未产出有效结果，转为提取音频进行 ASR"
	}
	if progressErr := r.updateProgress(ctx, job.ID, "running", "extract_audio", 20, fallbackMessage, db.JobOutputPaths{}, ""); progressErr != nil {
		return nil, "", progressErr
	}
	audioPath, audioErr := media.ExtractAudio(ctx, r.cfg.FFmpegBin, job.MediaPath, r.cfg.WorkDir)
	if audioErr != nil {
		return nil, "", audioErr
	}
	if progressErr := r.updateProgress(ctx, job.ID, "running", "transcribe", 40, "音频提取完成，正在调用 ASR 转写", db.JobOutputPaths{}, ""); progressErr != nil {
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
	return r.updateProgress(context.Background(), jobID, "cancelled", "cancelled", job.Progress, "任务已取消", paths, "")
}

func (r *Runner) updateProgress(ctx context.Context, jobID string, status string, stage string, progress int, details string, paths db.JobOutputPaths, errorMessage string) error {
	if err := r.repo.UpdateJobProgress(ctx, jobID, status, stage, progress, details, paths, errorMessage); err != nil {
		return err
	}
	if r.logger != nil {
		level := "info"
		if status == "cancelling" || status == "cancelled" {
			level = "warn"
		}
		if status == "failed" || strings.TrimSpace(errorMessage) != "" {
			level = "error"
		}
		_ = r.logger.Append(jobID, level, stage, details, errorMessage)
	}
	return nil
}

func buildTranslationPrompt(settings model.AppSettings) string {
	sections := []string{strings.TrimSpace(settings.TranslationPrompt)}
	switch strings.TrimSpace(settings.TranslationStyle) {
	case "faithful":
		sections = append(sections, "翻译风格：优先忠实原文，不主动扩写，不使用过度意译。")
	case "concise":
		sections = append(sections, "翻译风格：尽量简洁，保留核心信息，避免冗长表达。")
	case "formal":
		sections = append(sections, "翻译风格：整体正式、克制、书面化，避免过于口语化。")
	case "natural":
		sections = append(sections, "翻译风格：自然流畅、符合中文口语习惯，但不要脱离原意。")
	case "custom":
		if strings.TrimSpace(settings.CustomStylePrompt) != "" {
			sections = append(sections, "自定义风格要求："+strings.TrimSpace(settings.CustomStylePrompt))
		}
	}
	glossary := strings.TrimSpace(settings.Glossary)
	if glossary != "" {
		sections = append(sections, "术语表要求（若命中请优先遵守）：\n"+glossary)
	}
	sections = append(sections,
		"保持字幕条目一一对应，不要合并或拆分字幕。",
		"如果原文包含俚语、语气词或场景化表达，请结合上下文给出自然译文。",
	)
	result := make([]string, 0, len(sections))
	for _, section := range sections {
		trimmed := strings.TrimSpace(section)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return "请逐条翻译字幕文本，只输出目标语言译文，不要解释，不要合并或拆分字幕。"
	}
	return strings.Join(result, "\n\n")
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
