package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestOptionalMDNCatalog(t *testing.T) {
	t.Setenv("SIGMO_CONFIG_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	input := t.TempDir()
	for _, name := range []string{"entitlement.json", "wfc.json", "ims.json"} {
		if err := os.WriteFile(filepath.Join(input, name), []byte("[]"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := run(input, t.TempDir()); err != nil {
		t.Fatalf("legacy input: %v", err)
	}
	if err := os.WriteFile(filepath.Join(input, "mdn.json"), []byte("null"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(input, t.TempDir()); err == nil {
		t.Fatal("accepted malformed optional MDN catalog")
	}
	if err := os.WriteFile(filepath.Join(input, "mdn.json"), []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(input, t.TempDir()); err != nil {
		t.Fatalf("MDN input: %v", err)
	}
}
