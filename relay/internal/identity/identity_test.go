package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateRelayIdentity(t *testing.T) {
	identity, err := GenerateRelayIdentity()
	if err != nil {
		t.Fatalf("GenerateRelayIdentity() error = %v", err)
	}
	if len(identity.PublicKey) != 64 || len(identity.PrivateKey) != 128 {
		t.Fatalf("unexpected key lengths: public=%d private=%d", len(identity.PublicKey), len(identity.PrivateKey))
	}
	if identity.RelayID != "relay-"+identity.PublicKey[:16] {
		t.Fatalf("RelayID = %q, want prefix derived from public key", identity.RelayID)
	}
}

func TestSaveAndLoadRelayIdentity(t *testing.T) {
	identity, err := GenerateRelayIdentity()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), RelayIDFileName)
	if err := saveRelayIdentity(identity, path); err != nil {
		t.Fatalf("saveRelayIdentity() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("identity file mode = %o, want 600", got)
	}
	loaded, err := loadRelayIdentity(path)
	if err != nil {
		t.Fatalf("loadRelayIdentity() error = %v", err)
	}
	if loaded.PublicKey != identity.PublicKey || loaded.PrivateKey != identity.PrivateKey || loaded.RelayID != identity.RelayID {
		t.Fatalf("loaded identity does not match generated identity")
	}
}

func TestLoadRelayIdentityRejectsTraversal(t *testing.T) {
	if _, err := loadRelayIdentity(filepath.Join(t.TempDir(), "..", "secret")); err == nil {
		t.Fatal("loadRelayIdentity() accepted a traversal path")
	}
}
