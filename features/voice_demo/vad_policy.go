package main

// VADParams are OpenAI Realtime server_vad knobs for "when has the user finished speaking?".
// Larger SilenceDurationMs waits longer through mid-utterance pauses before the model replies.
type VADParams struct {
	SilenceDurationMs int
	Threshold         float64
	PrefixPaddingMs   int
}

// VADInput is the optional JSON body from the demo UI. Nil fields use defaults.
type VADInput struct {
	SilenceDurationMs *int
	Threshold         *float64
	PrefixPaddingMs   *int
}

const (
	vadSilenceDefault = 900
	vadSilenceMin     = 200
	vadSilenceMax     = 2000
	vadThresholdDef   = 0.5
	vadPrefixDefault  = 300
	vadPrefixMin      = 0
	vadPrefixMax      = 1000
)

// ResolveVAD applies defaults and clamps to demo-safe ranges.
func ResolveVAD(in VADInput) VADParams {
	out := VADParams{
		SilenceDurationMs: vadSilenceDefault,
		Threshold:         vadThresholdDef,
		PrefixPaddingMs:   vadPrefixDefault,
	}
	if in.SilenceDurationMs != nil {
		out.SilenceDurationMs = clampInt(*in.SilenceDurationMs, vadSilenceMin, vadSilenceMax)
	}
	if in.Threshold != nil {
		out.Threshold = clampFloat(*in.Threshold, 0, 1)
	}
	if in.PrefixPaddingMs != nil {
		out.PrefixPaddingMs = clampInt(*in.PrefixPaddingMs, vadPrefixMin, vadPrefixMax)
	}
	return out
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
