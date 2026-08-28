package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestBindEnvironmentNamesCanonicalPrecedesLegacy(t *testing.T) {
	t.Setenv("NOSTR_RELAY_NAME", "canonical-relay")
	t.Setenv("SHUGUR_RELAY_NAME", "legacy-relay")

	v := viper.New()
	v.SetDefault("relay.name", "default-relay")
	bindEnvironmentNames(v)

	if got := v.GetString("relay.name"); got != "canonical-relay" {
		t.Fatalf("relay.name = %q, want canonical NOSTR value", got)
	}
}

func TestBindEnvironmentNamesSupportsLegacyFlatInstallerVariables(t *testing.T) {
	t.Setenv("SHUGUR_WS_ADDR", ":8188")

	v := viper.New()
	v.SetDefault("relay.ws_addr", ":8080")
	bindEnvironmentNames(v)

	if got := v.GetString("relay.ws_addr"); got != ":8188" {
		t.Fatalf("relay.ws_addr = %q, want legacy flat alias value", got)
	}
}

func TestBindEnvironmentNamesCanonicalFlatAliasPrecedesLegacyFlatAlias(t *testing.T) {
	t.Setenv("NOSTR_WS_ADDR", ":9191")
	t.Setenv("SHUGUR_WS_ADDR", ":8188")

	v := viper.New()
	v.SetDefault("relay.ws_addr", ":8080")
	bindEnvironmentNames(v)

	if got := v.GetString("relay.ws_addr"); got != ":9191" {
		t.Fatalf("relay.ws_addr = %q, want canonical flat alias value", got)
	}
}
