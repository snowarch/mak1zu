package events

import "testing"

func TestRingIsBoundedAndIDsGrow(t *testing.T) {
	h := NewHub(3)
	for i := 0; i < 10; i++ {
		h.Emit(Event{Type: "heard"})
	}
	got := h.Since(0)
	if len(got) != 3 || got[0].ID != 8 || got[2].ID != 10 {
		t.Fatalf("%+v", got)
	}
	if n := len(h.Since(9)); n != 1 {
		t.Fatalf("since 9 gave %d", n)
	}
}

func TestSlowSubscriberNeverBlocksEmit(t *testing.T) {
	h := NewHub(10)
	c, cancel := h.Subscribe()
	defer cancel()
	for i := 0; i < 500; i++ { // far more than the channel holds
		h.Emit(Event{Type: "heard"})
	}
	if len(c) == 0 {
		t.Fatal("subscriber saw nothing")
	}
}

func TestCancelStopsDelivery(t *testing.T) {
	h := NewHub(10)
	c, cancel := h.Subscribe()
	cancel()
	h.Emit(Event{Type: "heard"})
	if len(c) != 0 {
		t.Fatal("delivered after cancel")
	}
}

func TestNilHubIsSafe(t *testing.T) {
	var h *Hub
	h.Emit(Event{Type: "heard"})
}
