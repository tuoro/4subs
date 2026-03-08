package ocr

import "context"

type Provider interface {
	Name() string
	Ready() bool
	RecognizeImage(ctx context.Context, imagePath string) (string, float64, error)
}
