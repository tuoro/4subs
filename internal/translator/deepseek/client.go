package deepseek

type Client struct {
	BaseURL string
	APIKey  string
	Model   string
}

func (c Client) Name() string {
	return "deepseek"
}

func (c Client) Ready() bool {
	return c.APIKey != "" && c.Model != ""
}

