package main

import "strings"

// BargeInMode selects how the Realtime (voice) layer stops assistant audio
// when the user barges in. CAWA interrupt timing is NOT controlled here:
// busy utterances still call Interrupt immediately (execution authority).
type BargeInMode string

const (
	BargeInNatural BargeInMode = "natural"
	BargeInAbrupt  BargeInMode = "abrupt"
)

// BargeInParams are knobs for Realtime session config + browser playback.
type BargeInParams struct {
	Mode              BargeInMode
	InterruptResponse bool // Realtime server_vad interrupt_response
	FadeMs            int  // browser remote-audio fade before pause; 0 = hard cut
	StopLabel         string
}

// ResolveBargeIn maps a UI/API mode string to concrete parameters.
// Unknown or empty values fall back to natural (demo default).
func ResolveBargeIn(mode string) BargeInParams {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case string(BargeInAbrupt):
		return BargeInParams{
			Mode:              BargeInAbrupt,
			InterruptResponse: true,
			FadeMs:            0,
			StopLabel:         "abrupt_cut",
		}
	default:
		return BargeInParams{
			Mode:              BargeInNatural,
			InterruptResponse: false,
			FadeMs:            180,
			StopLabel:         "soft_vad+fade",
		}
	}
}
