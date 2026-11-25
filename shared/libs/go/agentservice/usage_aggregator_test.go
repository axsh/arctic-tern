package agentservice

import (
	"fmt"
	"sync"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestTurnUsageAggregator_ConcurrentObserveAndMerge(t *testing.T) {
	a := newTurnUsageAggregator("t1")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				a.Observe(codingagent.StreamEvent{Type: codingagent.EventText,
					Usage: &codingagent.TokenUsage{CallID: fmt.Sprintf("o-%d-%d", i, j), InputTokens: 1}})
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				a.MergeCalls([]codingagent.TokenUsage{{CallID: fmt.Sprintf("m-%d-%d", i, j), InputTokens: 1}})
			}
		}(i)
	}
	wg.Wait()
	rec, ok := a.Finalize()
	if !ok || len(rec.Calls) != 3200 {
		t.Fatalf("ok=%v calls=%d, want 3200", ok, len(rec.Calls))
	}
}

func TestTurnUsageAggregator_DedupAndPreferResult(t *testing.T) {
	a := newTurnUsageAggregator("turn-1")
	a.Observe(codingagent.StreamEvent{
		Type: codingagent.EventText,
		Usage: &codingagent.TokenUsage{
			InputTokens: 100, OutputTokens: 10, CallID: "msg_1",
			Source: codingagent.UsageSourceClaudeAssistant, Confidence: codingagent.UsageConfidenceHigh,
		},
	})
	a.Observe(codingagent.StreamEvent{
		Type: codingagent.EventToolUse,
		Usage: &codingagent.TokenUsage{
			InputTokens: 100, OutputTokens: 10, CallID: "msg_1",
			Source: codingagent.UsageSourceClaudeAssistant, Confidence: codingagent.UsageConfidenceHigh,
		},
	})
	a.Observe(codingagent.StreamEvent{
		Type: codingagent.EventResult,
		Usage: &codingagent.TokenUsage{
			InputTokens: 200, OutputTokens: 30,
			Source: codingagent.UsageSourceClaudeResult, Confidence: codingagent.UsageConfidenceHigh,
		},
	})
	rec, ok := a.Finalize()
	if !ok {
		t.Fatal("expected finalize ok")
	}
	if len(rec.Calls) != 1 {
		t.Fatalf("calls = %d", len(rec.Calls))
	}
	if rec.Usage.InputTokens != 200 || rec.Usage.OutputTokens != 30 {
		t.Fatalf("turn = %+v", rec.Usage)
	}
	if !rec.Usage.CallsSumMismatch {
		t.Error("expected CallsSumMismatch")
	}
}
