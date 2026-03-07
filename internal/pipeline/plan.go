package pipeline

import "github.com/gayhub/4subs/internal/model"

func DefaultSteps() []model.PipelineStep {
	return []model.PipelineStep{
		{
			Key:         "scan",
			Title:       "媒体扫描",
			Description: "从本地挂载目录识别视频文件，建立待处理素材列表。",
			Owner:       "后端服务",
		},
		{
			Key:         "extract-audio",
			Title:       "音频提取",
			Description: "使用 ffmpeg 抽取适合语音识别的音频，输出到工作目录。",
			Owner:       "容器内媒体处理层",
		},
		{
			Key:         "asr",
			Title:       "原文字幕识别",
			Description: "调用语音识别引擎得到带时间轴的原文字幕块。",
			Owner:       "识别适配层",
		},
		{
			Key:         "translate",
			Title:       "DeepSeek 翻译",
			Description: "逐条翻译字幕文本，保留条目顺序和时间轴映射关系。",
			Owner:       "翻译适配层",
		},
		{
			Key:         "render",
			Title:       "双语字幕生成",
			Description: "合并原文与译文，生成 SRT，后续扩展 ASS 样式输出。",
			Owner:       "字幕生成层",
		},
	}
}

