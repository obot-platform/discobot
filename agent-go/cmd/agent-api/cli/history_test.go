package cli

import "testing"

func TestHistoryView_UsesMostRecentFirstOrder(t *testing.T) {
	h := &cmdHistory{entries: []string{"first", "second", "third"}}
	v := historyView{h: h}

	if v.Len() != 3 {
		t.Fatalf("expected len=3, got %d", v.Len())
	}
	if got := v.At(0); got != "third" {
		t.Fatalf("expected most recent entry, got %q", got)
	}
	if got := v.At(2); got != "first" {
		t.Fatalf("expected oldest entry at tail index, got %q", got)
	}
}

func TestHistoryView_AddIsNoop(t *testing.T) {
	h := &cmdHistory{entries: []string{"one"}}
	v := historyView{h: h}
	v.Add("two")
	if len(h.entries) != 1 {
		t.Fatalf("expected Add to be no-op, got %d entries", len(h.entries))
	}
}
