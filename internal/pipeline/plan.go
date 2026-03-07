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
			Title:       "源字幕提取",
			Description: "优先读取同名外挂字幕，不存在时尝试从视频内嵌文本字幕轨提取。",
			Owner:       "ffmpeg",
		},
		{
			Key:         "extract_audio",
			Title:       "音频提取",
			Description: "如果没有可用源字幕，则先从视频中提取单声道语音音频。",
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
			Title:       "双语 SRT 输出",
			Description: "合并原文和译文，生成最终双语 SRT 文件并落盘到输出目录。",
			Owner:       "字幕渲染模块",
		},
	}
}
