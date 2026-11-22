package nvidia

import "testing"

func TestResetMinMaxRestartsTracking(t *testing.T) {
	const id = "/gpu-nvidia/9/temperature/0"
	track(id, 40)
	track(id, 70)
	if min, max := track(id, 55); min != 40 || max != 70 {
		t.Fatalf("before reset: %v..%v, want 40..70", min, max)
	}
	if !ResetMinMax(id) {
		t.Fatal("known sensor reported as unknown")
	}
	if min, max := track(id, 55); min != 55 || max != 55 {
		t.Fatalf("after reset: %v..%v, want 55..55", min, max)
	}
	if ResetMinMax("/gpu-nvidia/9/never-seen") {
		t.Fatal("unknown sensor reported as known")
	}
}
