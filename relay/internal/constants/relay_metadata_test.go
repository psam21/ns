package constants

import (
	"fmt"
	"reflect"
	"testing"
)

func TestDefaultSupportedNIPsCanonicalRegistry(t *testing.T) {
	expected := []string{
		"NIP-01", "NIP-02", "NIP-05", "NIP-07", "NIP-09", "NIP-10", "NIP-11", "NIP-13", "NIP-17", "NIP-18",
		"NIP-19", "NIP-21", "NIP-22", "NIP-23", "NIP-24", "NIP-25", "NIP-27", "NIP-29", "NIP-30", "NIP-32",
		"NIP-34", "NIP-35", "NIP-36", "NIP-37", "NIP-38", "NIP-39", "NIP-40", "NIP-42", "NIP-43", "NIP-44",
		"NIP-45", "NIP-46", "NIP-47", "NIP-48", "NIP-49", "NIP-50", "NIP-51", "NIP-52", "NIP-53", "NIP-5A",
		"NIP-54", "NIP-56", "NIP-57", "NIP-58", "NIP-59", "NIP-60", "NIP-61", "NIP-62", "NIP-64", "NIP-65",
		"NIP-66", "NIP-67", "NIP-69", "NIP-70", "NIP-71", "NIP-75", "NIP-77", "NIP-78", "NIP-84", "NIP-85",
		"NIP-86", "NIP-87", "NIP-88", "NIP-89", "NIP-92", "NIP-94", "NIP-98", "NIP-99", "NIP-7D", "NIP-A0",
		"NIP-A4", "NIP-B0", "NIP-C0", "NIP-F4", "NIP-CC", "NIP-C7", "NIP-B7",
	}

	actual := make([]string, 0, len(DefaultSupportedNIPs))
	for _, raw := range DefaultSupportedNIPs {
		switch value := raw.(type) {
		case int:
			actual = append(actual, fmt.Sprintf("NIP-%02d", value))
		case string:
			actual = append(actual, "NIP-"+value)
		default:
			t.Fatalf("unsupported registry value type %T", raw)
		}
	}

	if len(actual) != 77 {
		t.Fatalf("DefaultSupportedNIPs contains %d entries, want 77", len(actual))
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("DefaultSupportedNIPs drifted\n got: %v\nwant: %v", actual, expected)
	}
}
