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
			Key:         "parse_subtitle",
			Title:       "字幕解析",
			Description: "将提取到的 SRT 解析为可翻译的字幕块，保留编号与时间轴。",
			Owner:       "字幕处理模块",
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
