package asr

import (
	"context"

	"github.com/gayhub/4subs/internal/subtitle"
)

type Provider interface {
	Name() string
	Ready() bool
	Transcribe(ctx context.Context, audioPath string, sourceLanguage string) ([]subtitle.Block, error)
}
