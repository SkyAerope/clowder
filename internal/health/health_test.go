package health

import "testing"

func TestNativeHealthIsIsolated(t *testing.T) {
	tracker := New()
	tracker.MarkSuccess("provider", "native-a", 120)
	tracker.MarkSuccess("provider", "native-b", 240)
	tracker.MarkModelDown("provider", "native-a")

	if tracker.Routable("provider", "native-a") {
		t.Fatal("failed native should not be routable")
	}
	if !tracker.Routable("provider", "native-b") {
		t.Fatal("sibling native should remain routable")
	}
	if got, ok := tracker.AvgTTFT("provider", "native-b"); !ok || got != 240 {
		t.Fatalf("native-b TTFT = %d, ok=%v", got, ok)
	}
}
