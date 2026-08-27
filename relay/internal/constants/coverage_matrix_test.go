package constants

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestNIPCoverageMatrixMatchesAdvertisedRegistry(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate coverage matrix relative to source")
	}
	matrixPath := filepath.Join(filepath.Dir(sourceFile), "..", "..", "tests", "nips", "coverage.tsv")
	matrixFile, err := os.Open(matrixPath)
	if err != nil {
		t.Fatalf("open coverage matrix: %v", err)
	}
	defer matrixFile.Close()

	advertised := make([]string, 0, len(DefaultSupportedNIPs))
	for _, raw := range DefaultSupportedNIPs {
		switch value := raw.(type) {
		case int:
			advertised = append(advertised, fmt.Sprintf("NIP-%02d", value))
		case string:
			advertised = append(advertised, "NIP-"+value)
		default:
			t.Fatalf("unsupported registry value type %T", raw)
		}
	}

	matrixIDs := make([]string, 0, len(advertised))
	seen := make(map[string]struct{}, len(advertised))
	scanner := bufio.NewScanner(matrixFile)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("coverage matrix line %d has %d fields, want 5", lineNumber, len(fields))
		}
		id, area, execution, evidence, notes := fields[0], fields[1], fields[2], fields[3], fields[4]
		if !strings.HasPrefix(id, "NIP-") || notes == "" {
			t.Fatalf("coverage matrix line %d has invalid identifier or empty notes", lineNumber)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("coverage matrix contains duplicate identifier %s", id)
		}
		seen[id] = struct{}{}
		matrixIDs = append(matrixIDs, id)

		switch area {
		case "relay-event", "relay-control", "client-ecosystem", "blossom":
		default:
			t.Fatalf("coverage matrix line %d has invalid area %q", lineNumber, area)
		}
		switch execution {
		case "integration":
			testPath := filepath.Join(filepath.Dir(matrixPath), evidence)
			info, statErr := os.Stat(testPath)
			if statErr != nil {
				t.Fatalf("coverage matrix line %d references missing test %q: %v", lineNumber, evidence, statErr)
			}
			if info.Mode()&0111 == 0 {
				t.Fatalf("coverage matrix line %d references non-executable test %q", lineNumber, evidence)
			}
		case "contract":
			if evidence != "registry-contract" {
				t.Fatalf("coverage matrix line %d has unexpected registry-contract evidence %q", lineNumber, evidence)
			}
		case "manual", "external":
			if evidence != "manual" {
				t.Fatalf("coverage matrix line %d has unexpected manual evidence %q", lineNumber, evidence)
			}
		default:
			t.Fatalf("coverage matrix line %d has invalid execution class %q", lineNumber, execution)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read coverage matrix: %v", err)
	}

	if len(matrixIDs) != 77 {
		t.Fatalf("coverage matrix contains %d rows, want 77", len(matrixIDs))
	}
	sort.Strings(advertised)
	sort.Strings(matrixIDs)
	if !reflect.DeepEqual(matrixIDs, advertised) {
		t.Fatalf("coverage matrix does not match advertised registry\n got: %v\nwant: %v", matrixIDs, advertised)
	}
}
