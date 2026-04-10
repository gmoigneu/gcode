package ai

// CalculateCost returns the monetary cost of an assistant response based on
// the model's per-million-token pricing and the usage counters.
func CalculateCost(model Model, usage Usage) Cost {
	per := func(ratePerMillion float64, tokens int) float64 {
		if tokens <= 0 || ratePerMillion <= 0 {
			return 0
		}
		return (ratePerMillion / 1_000_000) * float64(tokens)
	}
	c := Cost{
		Input:      per(model.Cost.Input, usage.Input),
		Output:     per(model.Cost.Output, usage.Output),
		CacheRead:  per(model.Cost.CacheRead, usage.CacheRead),
		CacheWrite: per(model.Cost.CacheWrite, usage.CacheWrite),
	}
	c.Total = c.Input + c.Output + c.CacheRead + c.CacheWrite
	return c
}
