package main

import "testing"

func TestDecideUtteranceAction(t *testing.T) {
	tests := []struct {
		name    string
		busy    bool
		text    string
		want    Action
		wantErr bool
	}{
		{name: "idle hello", busy: false, text: "hello", want: ActionMessages},
		{name: "busy change plan", busy: true, text: "change plan", want: ActionInterrupt},
		{name: "idle whitespace", busy: false, text: "  \n\t ", want: ActionNone, wantErr: true},
		{name: "busy empty", busy: true, text: "", want: ActionNone, wantErr: true},
		{name: "idle trim", busy: false, text: "  build a file  ", want: ActionMessages},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecideUtteranceAction(tt.busy, tt.text)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got action=%s", got)
				}
				if got != ActionNone {
					t.Fatalf("got action=%s, want none on error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("action=%s, want %s", got, tt.want)
			}
			// Barge-in must never look like cancel: we do not even define ActionCancel.
			if tt.busy && got != ActionInterrupt {
				t.Fatalf("busy path must be interrupt, got %s", got)
			}
		})
	}
}
