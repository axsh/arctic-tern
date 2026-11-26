package main

import "strings"

const (
	outputSpeedDefault = 1.0
	outputSpeedMin     = 0.25
	outputSpeedMax     = 1.5
)

// ResolveOutputAudio normalizes Realtime audio.output voice and speed.
func ResolveOutputAudio(voice string, speed float64) (string, float64) {
	v := strings.TrimSpace(strings.ToLower(voice))
	if v == "" || !isCuratedVoice(v) {
		v = defaultRealtimeVoice
	}
	s := speed
	if s == 0 {
		s = outputSpeedDefault
	}
	s = clampFloat(s, outputSpeedMin, outputSpeedMax)
	return v, s
}
