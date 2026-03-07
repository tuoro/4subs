package subtitle

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Block struct {
	Index int           `json:"index"`
	Start time.Duration `json:"start"`
	End   time.Duration `json:"end"`
	Lines []string      `json:"lines"`
}

func ParseFile(path string) ([]Block, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseSRT(string(raw))
}

func ParseSRT(content string) ([]Block, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, errors.New("字幕内容为空")
	}
	chunks := strings.Split(content, "\n\n")
	blocks := make([]Block, 0, len(chunks))
	for _, chunk := range chunks {
		lines := splitNonEmptyLines(chunk)
		if len(lines) < 3 {
			continue
		}
		index, err := strconv.Atoi(strings.TrimSpace(lines[0]))
		if err != nil {
			continue
		}
		timeParts := strings.Split(lines[1], "-->")
		if len(timeParts) != 2 {
			continue
		}
		start, err := parseSRTTimestamp(strings.TrimSpace(timeParts[0]))
		if err != nil {
			continue
		}
		end, err := parseSRTTimestamp(strings.TrimSpace(timeParts[1]))
		if err != nil {
			continue
		}
		body := make([]string, 0, len(lines)-2)
		for _, line := range lines[2:] {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				body = append(body, trimmed)
			}
		}
		if len(body) == 0 {
			continue
		}
		blocks = append(blocks, Block{Index: index, Start: start, End: end, Lines: body})
	}
	if len(blocks) == 0 {
		return nil, errors.New("未解析到有效的 SRT 字幕块")
	}
	return blocks, nil
}

func RenderSRT(blocks []Block) string {
	var builder strings.Builder
	for index, block := range blocks {
		builder.WriteString(strconv.Itoa(index + 1))
		builder.WriteString("\n")
		builder.WriteString(formatSRTTimestamp(block.Start))
		builder.WriteString(" --> ")
		builder.WriteString(formatSRTTimestamp(block.End))
		builder.WriteString("\n")
		for _, line := range block.Lines {
			builder.WriteString(strings.TrimSpace(line))
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

func RenderBilingualSRT(blocks []Block, translations []string, layout string) (string, error) {
	if len(blocks) != len(translations) {
		return "", errors.New("字幕块与翻译数量不一致")
	}
	var builder strings.Builder
	for index, block := range blocks {
		builder.WriteString(strconv.Itoa(index + 1))
		builder.WriteString("\n")
		builder.WriteString(formatSRTTimestamp(block.Start))
		builder.WriteString(" --> ")
		builder.WriteString(formatSRTTimestamp(block.End))
		builder.WriteString("\n")
		originLines := block.Lines
		translationLines := splitTranslationLines(translations[index])
		if strings.TrimSpace(layout) == "translation_above" {
			for _, line := range translationLines {
				builder.WriteString(line)
				builder.WriteString("\n")
			}
			for _, line := range originLines {
				builder.WriteString(line)
				builder.WriteString("\n")
			}
		} else {
			for _, line := range originLines {
				builder.WriteString(line)
				builder.WriteString("\n")
			}
			for _, line := range translationLines {
				builder.WriteString(line)
				builder.WriteString("\n")
			}
		}
		builder.WriteString("\n")
	}
	return builder.String(), nil
}

func RenderBilingualASS(blocks []Block, translations []string, layout string) (string, error) {
	if len(blocks) != len(translations) {
		return "", errors.New("字幕块与翻译数量不一致")
	}
	var builder strings.Builder
	builder.WriteString("[Script Info]\n")
	builder.WriteString("Title: 4subs Bilingual ASS\n")
	builder.WriteString("ScriptType: v4.00+\n")
	builder.WriteString("WrapStyle: 2\n")
	builder.WriteString("ScaledBorderAndShadow: yes\n")
	builder.WriteString("PlayResX: 1920\n")
	builder.WriteString("PlayResY: 1080\n\n")
	builder.WriteString("[V4+ Styles]\n")
	builder.WriteString("Format: Name,Fontname,Fontsize,PrimaryColour,SecondaryColour,OutlineColour,BackColour,Bold,Italic,Underline,StrikeOut,ScaleX,ScaleY,Spacing,Angle,BorderStyle,Outline,Shadow,Alignment,MarginL,MarginR,MarginV,Encoding\n")
	builder.WriteString("Style: Default,Microsoft YaHei,32,&H00FFFFFF,&H000000FF,&H64000000,&H32000000,0,0,0,0,100,100,0,0,1,2,0,2,40,40,36,1\n\n")
	builder.WriteString("[Events]\n")
	builder.WriteString("Format: Layer,Start,End,Style,Name,MarginL,MarginR,MarginV,Effect,Text\n")
	for index, block := range blocks {
		originText := escapeASSText(strings.Join(block.Lines, "\\N"))
		translationText := escapeASSText(strings.Join(splitTranslationLines(translations[index]), "\\N"))
		var eventText string
		if strings.TrimSpace(layout) == "translation_above" {
			eventText = fmt.Sprintf("{\\fs26\\c&H00A5FF&}%s\\N{\\rDefault\\fs32\\c&H00FFFFFF&}%s", translationText, originText)
		} else {
			eventText = fmt.Sprintf("{\\fs32\\c&H00FFFFFF&}%s\\N{\\fs26\\c&H00A5FF&}%s", originText, translationText)
		}
		builder.WriteString(fmt.Sprintf("Dialogue: 0,%s,%s,Default,,0,0,0,,%s\n", formatASSTimestamp(block.Start), formatASSTimestamp(block.End), eventText))
	}
	return builder.String(), nil
}

func JoinText(lines []string) string {
	return strings.Join(lines, "\n")
}

func splitTranslationLines(text string) []string {
	scanner := bufio.NewScanner(strings.NewReader(strings.ReplaceAll(text, "\r\n", "\n")))
	lines := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return []string{"[翻译为空]"}
	}
	return lines
}

func splitNonEmptyLines(chunk string) []string {
	chunk = strings.ReplaceAll(chunk, "\r\n", "\n")
	parts := strings.Split(chunk, "\n")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			result = append(result, part)
		}
	}
	return result
}

func parseSRTTimestamp(raw string) (time.Duration, error) {
	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		return 0, fmt.Errorf("非法时间戳: %s", raw)
	}
	hms := strings.Split(parts[0], ":")
	if len(hms) != 3 {
		return 0, fmt.Errorf("非法时间戳: %s", raw)
	}
	hours, err := strconv.Atoi(hms[0])
	if err != nil {
		return 0, err
	}
	minutes, err := strconv.Atoi(hms[1])
	if err != nil {
		return 0, err
	}
	seconds, err := strconv.Atoi(hms[2])
	if err != nil {
		return 0, err
	}
	milliseconds, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}
	return time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second + time.Duration(milliseconds)*time.Millisecond, nil
}

func formatSRTTimestamp(value time.Duration) string {
	totalMilliseconds := value.Milliseconds()
	hours := totalMilliseconds / 3600000
	minutes := (totalMilliseconds % 3600000) / 60000
	seconds := (totalMilliseconds % 60000) / 1000
	milliseconds := totalMilliseconds % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", hours, minutes, seconds, milliseconds)
}

func formatASSTimestamp(value time.Duration) string {
	centiseconds := value.Milliseconds() / 10
	hours := centiseconds / 360000
	minutes := (centiseconds % 360000) / 6000
	seconds := (centiseconds % 6000) / 100
	remainder := centiseconds % 100
	return fmt.Sprintf("%d:%02d:%02d.%02d", hours, minutes, seconds, remainder)
}

func escapeASSText(value string) string {
	value = strings.ReplaceAll(value, "\\", `\\`)
	value = strings.ReplaceAll(value, "{", `\{`)
	value = strings.ReplaceAll(value, "}", `\}`)
	value = strings.ReplaceAll(value, "\r\n", `\N`)
	value = strings.ReplaceAll(value, "\n", `\N`)
	value = strings.ReplaceAll(value, "\r", `\N`)
	return value
}
