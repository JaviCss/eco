package port

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestConformanceRefusesToRunWithoutOutage(t *testing.T) {
	if os.Getenv("ECO_CONFORMANCE_WITHOUT_OUTAGE") == "1" {
		Conformance(t, func() Port { return NewFake() }, nil)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestConformanceRefusesToRunWithoutOutage", "-test.count=1")
	cmd.Env = append(os.Environ(), "ECO_CONFORMANCE_WITHOUT_OUTAGE=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("Conformance without newOutage reported success, so the outage rules never ran:\n%s", out)
	}
	if !strings.Contains(string(out), "requires newOutage") {
		t.Fatalf("the child failed for another reason:\n%s", out)
	}
}