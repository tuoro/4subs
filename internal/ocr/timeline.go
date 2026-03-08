package ocr

import (
	"sort"
	"strings"
	"time"

	"github.com/gayhub/4subs/internal/subtitle"
)

type Observation struct {
	At         time.Duration `json:"at"`
	Text       string        `json:"text"`
	Confidence float64       `json:"confidence"`
}

type TimelineOptions struct {
	MinDuration     time.Duration
	MaxJoinGap      time.Duration
	MinTextLength   int
	StableFrames    int
	ConfidenceFloor float64
}

func DefaultTimelineOptions() TimelineOptions {
	return TimelineOptions{
		MinDuration:     800 * time.Millisecond,
		MaxJoinGap:      400 * time.Millisecond,
		MinTextLength:   1,
		StableFrames:    2,
		ConfidenceFloor: 0.45,
	}
}

func BuildBlocks(observations []Observation, options TimelineOptions) []subtitle.Block {
	if options.MinDuration <= 0 {
		options.MinDuration = 800 * time.Millisecond
	}
	if options.MaxJoinGap <= 0 {
		options.MaxJoinGap = 400 * time.Millisecond
	}
	if options.StableFrames <= 0 {
		options.StableFrames = 2
	}
	filtered := normalizeObservations(observations, options)
	if len(filtered) == 0 {
		return nil
	}
	blocks := make([]subtitle.Block, 0, len(filtered))
	current := filtered[0]
	count := 1
	index := 1
	for i := 1; i < len(filtered); i++ {
		next := filtered[i]
		if next.Text == current.Text && next.At-current.End <= options.MaxJoinGap {
			current.End = next.At
			count++
			continue
		}
		if count >= options.StableFrames {
			blocks = append(blocks, makeBlock(index, current, options.MinDuration))
			index++
		}
		current = next
		count = 1
	}
	if count >= options.StableFrames {
		blocks = append(blocks, makeBlock(index, current, options.MinDuration))
	}
	return compactBlocks(blocks, options.MaxJoinGap)
}

type normalizedObservation struct {
	At   time.Duration
	End  time.Duration
	Text string
}

func normalizeObservations(observations []Observation, options TimelineOptions) []normalizedObservation {
	items := make([]Observation, 0, len(observations))
	for _, item := range observations {
		text := cleanText(item.Text)
		if text == "" {
			continue
		}
		if options.MinTextLength > 0 && len([]rune(text)) < options.MinTextLength {
			continue
		}
		if options.ConfidenceFloor > 0 && item.Confidence > 0 && item.Confidence < options.ConfidenceFloor {
			continue
		}
		item.Text = text
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].At < items[j].At
	})
	result := make([]normalizedObservation, 0, len(items))
	for _, item := range items {
		result = append(result, normalizedObservation{At: item.At, End: item.At, Text: item.Text})
	}
	return result
}

func makeBlock(index int, item normalizedObservation, minDuration time.Duration) subtitle.Block {
	end := item.End
	if end <= item.At {
		end = item.At + minDuration
	}
	if end-item.At < minDuration {
		end = item.At + minDuration
	}
	return subtitle.Block{
		Index: index,
		Start: item.At,
		End:   end,
		Lines: strings.Split(item.Text, "\n"),
	}
}

func compactBlocks(blocks []subtitle.Block, maxJoinGap time.Duration) []subtitle.Block {
	if len(blocks) == 0 {
		return nil
	}
	result := make([]subtitle.Block, 0, len(blocks))
	current := blocks[0]
	for i := 1; i < len(blocks); i++ {
		next := blocks[i]
		if joinable(current, next, maxJoinGap) {
			current.End = next.End
			continue
		}
		current.Index = len(result) + 1
		result = append(result, current)
		current = next
	}
	current.Index = len(result) + 1
	result = append(result, current)
	return result
}

func joinable(left subtitle.Block, right subtitle.Block, maxJoinGap time.Duration) bool {
	if cleanText(strings.Join(left.Lines, "\n")) != cleanText(strings.Join(right.Lines, "\n")) {
		return false
	}
	return right.Start-left.End <= maxJoinGap
}

func cleanText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return strings.Join(result, "\n")
}
