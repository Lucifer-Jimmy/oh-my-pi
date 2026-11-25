package llm

type Usage struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
}

func (u *Usage) Add(o Usage) {
	u.InputTokens += o.InputTokens
	u.OutputTokens += o.OutputTokens
	u.CacheReadTokens += o.CacheReadTokens
	u.CacheWriteTokens += o.CacheWriteTokens
}

func (u Usage) Total() int {
	return u.InputTokens + u.CacheReadTokens + u.CacheWriteTokens + u.OutputTokens
}

func (u *Usage) MergeEcho(o Usage) {
	if o.OutputTokens > u.OutputTokens {
		u.OutputTokens = o.OutputTokens
	}
	if o.InputTokens > u.InputTokens {
		u.InputTokens = o.InputTokens
	}
	if o.CacheReadTokens > u.CacheReadTokens {
		u.CacheReadTokens = o.CacheReadTokens
	}
	if o.CacheWriteTokens > u.CacheWriteTokens {
		u.CacheWriteTokens = o.CacheWriteTokens
	}
}
