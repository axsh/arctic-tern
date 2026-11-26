package main

import "testing"

func TestDecideUtteranceRoute(t *testing.T) {
	send, action, reason := DecideUtteranceRoute(false, "こんにちは", "auto")
	if send || action != ActionNone || reason != "auto_skip_chat" {
		t.Fatalf("greeting: send=%v action=%s reason=%s", send, action, reason)
	}

	work := "Please run a long shell command that sleeps for about 60 seconds and create a file."
	send, action, reason = DecideUtteranceRoute(false, work, "auto")
	if !send || action != ActionMessages || reason != "auto_work" {
		t.Fatalf("work idle: send=%v action=%s reason=%s", send, action, reason)
	}

	send, action, reason = DecideUtteranceRoute(true, "何やってんの?", "auto")
	if send || reason != "auto_skip_busy_chat" {
		t.Fatalf("busy chat: send=%v reason=%s", send, reason)
	}

	send, action, reason = DecideUtteranceRoute(true, "やめて、別のファイルを作って", "auto")
	if !send || action != ActionInterrupt {
		t.Fatalf("busy change: send=%v action=%s reason=%s", send, action, reason)
	}

	send, action, reason = DecideUtteranceRoute(false, "hi", "always")
	if !send || action != ActionMessages || reason != "always" {
		t.Fatalf("always: send=%v action=%s reason=%s", send, action, reason)
	}

	send, _, reason = DecideUtteranceRoute(false, work, "never")
	if send || reason != "never" {
		t.Fatalf("never: send=%v reason=%s", send, reason)
	}
}
