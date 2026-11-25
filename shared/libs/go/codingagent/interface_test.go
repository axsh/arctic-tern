package codingagent_test

import (
	"context"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

// interruptCapableStub satisfies both native interrupt resume interfaces.
type interruptCapableStub struct{}

func (interruptCapableStub) SupportsInterruptResume() bool { return true }

func (interruptCapableStub) InterruptTurn(context.Context, string) (<-chan codingagent.StreamEvent, error) {
	return nil, nil
}

func TestInterruptInterfacesAreImplementable(t *testing.T) {
	var v any = interruptCapableStub{}
	sup, ok := v.(codingagent.InterruptResumeSupporter)
	if !ok || !sup.SupportsInterruptResume() {
		t.Fatal("stub must satisfy InterruptResumeSupporter")
	}
	if _, ok := v.(codingagent.TurnInterrupter); !ok {
		t.Fatal("stub must satisfy TurnInterrupter")
	}
}
