package translator

type Segment struct {
	Index      int    `json:"index"`
	SourceText string `json:"source_text"`
}

type Provider interface {
	Name() string
	Ready() bool
}
