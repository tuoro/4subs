package pipeline

import "github.com/gayhub/4subs/internal/model"

func DefaultSteps() []model.PipelineStep {
	return []model.PipelineStep{
		{
			Key:         "scan",
			Title:       "媒体扫描",
			Description: "扫描本地挂载目录，建立可处理的视频素材列表。",
			Owner:       "后端服务",
		},
		{
			Key:         "extract_subtitle",
			Title:       "文本字幕提取",
			Description: "优先读取同名外挂字幕；若存在文本字幕轨，则提取并转成标准 SRT。",
			Owner:       "ffmpeg",
		},
		{
			Key:         "ocr_extract",
			Title:       "OCR 抽帧",
			Description: "当找不到文本字幕时，截取底部字幕区域关键帧，准备交给远程 OCR。",
			Owner:       "ffmpeg",
		},
		{
			Key:         "ocr_recognize",
			Title:       "远程 OCR",
			Description: "调用远程视觉 API 识别硬字幕，并恢复为带时间轴的字幕块。",
			Owner:       "OCR 适配层",
		},
		{
			Key:         "extract_audio",
			Title:       "音频提取",
			Description: "如果 OCR 仍不可用或失败，则从视频中提取单声道语音音频。",
			Owner:       "ffmpeg",
		},
		{
			Key:         "transcribe",
			Title:       "ASR 转写",
			Description: "调用 OpenAI 兼容音频转写接口，返回带 segment 时间戳的字幕块。",
			Owner:       "ASR 适配层",
		},
		{
			Key:         "translate",
			Title:       "DeepSeek 翻译",
			Description: "按批次调用 DeepSeek Chat Completions 接口，逐条返回译文。",
			Owner:       "翻译适配层",
		},
		{
			Key:         "render",
			Title:       "双语字幕输出",
			Description: "沿用原时间轴合并原文与译文，生成双语 SRT 与 ASS 文件。",
			Owner:       "字幕渲染模块",
		},
	}
}
