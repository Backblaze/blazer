package b2

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
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
	if len(lines) != 50 || lines[0] != "test_level\tscenario\ttarget\texecutable" {
		t.Fatalf("unexpected tests.tsv schema: %q", string(content))
	}
	wantExecutable := map[string]string{
		"conformance": "./.sdkharness/tests/run-conformance",
		"health":      "./.sdkharness/tests/run-health",
		"resilience":  "./.sdkharness/tests/run-resilience",
	}
	wantCount := map[string]int{"conformance": 33, "health": 1, "resilience": 15}
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
		if err := sdkHarnessScenarioChecks(string(scenario)); err != nil {
			t.Fatalf("scenario %s: %v", scenarioPath, err)
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

// Inspect the embedded Go entry point, rather than accepting shell boilerplate
// as evidence of a scenario. This structural gate does not replace running it.
func sdkHarnessScenarioChecks(script string) error {
	_, program, ok := strings.Cut(script, "\npackage main\n")
	if !ok {
		return fmt.Errorf("no embedded Go program")
	}
	program, _, _ = strings.Cut(program, "\n__EOF_GO__")
	file, err := parser.ParseFile(token.NewFileSet(), "scenario.go", "package main\n"+program, 0)
	if err != nil {
		return err
	}
	functions := make(map[string]*ast.FuncDecl)
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Recv == nil {
			functions[function.Name.Name] = function
		}
	}
	entry := functions["runCheck"]
	if entry == nil {
		entry = functions["run"]
	}
	if entry == nil {
		entry = functions["check"]
	}
	if entry == nil {
		entry = functions["main"] // The inbound-webhook scenario runs directly in main.
	}
	if entry == nil || functions["main"] == nil {
		return fmt.Errorf("no scenario entry point")
	}
	called, conditional, asserted := entry == functions["main"], false, false
	ast.Inspect(functions["main"].Body, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			if name, ok := call.Fun.(*ast.Ident); ok && name.Name == entry.Name.Name {
				called = true
			}
		}
		return true
	})
	seenAssertions := make(map[string]bool)
	var inspect func(ast.Node) bool
	inspect = func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			switch name := call.Fun.(type) {
			case *ast.Ident:
				asserted = asserted || name.Name == "failf" || name.Name == "fail"
			case *ast.SelectorExpr:
				if pkg, ok := name.X.(*ast.Ident); ok {
					asserted = asserted || pkg.Name == "fmt" && name.Sel.Name == "Errorf"
				}
			}
			if name, ok := call.Fun.(*ast.Ident); ok && (strings.HasPrefix(name.Name, "assert") || strings.HasPrefix(name.Name, "check")) && !seenAssertions[name.Name] {
				seenAssertions[name.Name] = true
				if helper := functions[name.Name]; helper != nil {
					ast.Inspect(helper.Body, inspect)
				}
			}
		}
		if _, ok := node.(*ast.IfStmt); ok {
			conditional = true
		}
		return true
	}
	ast.Inspect(entry.Body, inspect)
	if !called || !conditional || !asserted {
		return fmt.Errorf("scenario must be called by main and contain conditions and failure assertions")
	}
	return nil
}

func TestSDKHarnessRejectsNoOpScenario(t *testing.T) {
	for _, program := range []string{
		"func main() {}",
		"func runCheck() error { return nil }; func main() { runCheck() }",
		`func runCheck() error { if false { return fmt.Errorf("failed") }; return nil }; func main() {}`,
	} {
		if err := sdkHarnessScenarioChecks("replace github.com/Backblaze/blazer => $REPO_ROOT\npackage main\n" + program); err == nil {
			t.Fatalf("accepted no-op scenario %q", program)
		}
	}
}
