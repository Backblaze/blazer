package b2

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSDKHarnessContract(t *testing.T) {
	root := filepath.Join("..", ".sdkharness")
	content, err := os.ReadFile(filepath.Join(root, "tests.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(content), "\r\n", "\n")), "\n")
	if len(lines) != 42 || lines[0] != "test_level\tscenario\ttarget\texecutable" {
		t.Fatalf("unexpected tests.tsv schema: %q", string(content))
	}
	wantExecutable := map[string]string{
		"conformance": "./.sdkharness/tests/run-conformance",
		"health":      "./.sdkharness/tests/run-health",
		"resilience":  "./.sdkharness/tests/run-resilience",
	}
	wantCount := map[string]int{"conformance": 25, "health": 1, "resilience": 15}
	seen := make(map[string]bool)
	counts := make(map[string]int)
	for _, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) != 4 || fields[2] != "simulator" || fields[3] != wantExecutable[fields[0]] {
			t.Fatalf("unexpected contract row: %q", line)
		}
		key := fields[0] + "/" + fields[1]
		if seen[key] {
			t.Fatalf("duplicate contract row: %s", key)
		}
		seen[key] = true
		counts[fields[0]]++
		if fields[0] == "health" {
			if fields[1] != "golden-path" {
				t.Fatalf("unexpected health scenario: %s", fields[1])
			}
		}
		scenarioPath := filepath.Join(root, "tests", fields[0], fields[1])
		info, statErr := os.Stat(scenarioPath)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
			t.Fatalf("scenario is not executable: %s", scenarioPath)
		}
		scenario, readErr := os.ReadFile(scenarioPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !strings.Contains(string(scenario), "replace github.com/Backblaze/blazer => $REPO_ROOT") {
			t.Fatalf("scenario does not select the checked-out module: %s", scenarioPath)
		}
	}
	for level, count := range wantCount {
		if counts[level] != count {
			t.Fatalf("%s rows: got %d, want %d", level, counts[level], count)
		}
	}
	for _, executable := range wantExecutable {
		info, statErr := os.Stat(filepath.Join("..", executable[2:]))
		if statErr != nil {
			t.Fatal(statErr)
		}
		if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
			t.Fatalf("contract executable is not executable: %s", executable)
		}
	}
}
