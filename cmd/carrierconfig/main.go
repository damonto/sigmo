// Command carrierconfig validates and encrypts private files for R2 publication.
package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/damonto/sigmo/internal/pkg/carrierconfig"
)

func main() {
	directory := flag.String("input", "", "directory containing the three private JSON files")
	output := flag.String("output", "", "directory for encrypted release and current.json")
	keyFile := flag.String("key-file", "", "file containing the base64 storage key; otherwise use SIGMO_CONFIG_ENCRYPTION_KEY")
	flag.Parse()
	if *keyFile != "" {
		data, err := os.ReadFile(*keyFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "read configuration encryption key:", err)
			os.Exit(1)
		}
		if err := os.Setenv("SIGMO_CONFIG_ENCRYPTION_KEY", strings.TrimSpace(string(data))); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if err := run(*directory, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(directory, output string) error {
	if directory == "" || output == "" {
		return errors.New("input and output directories are required")
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("SIGMO_CONFIG_ENCRYPTION_KEY"))
	if err != nil || len(key) != 32 {
		return errors.New("SIGMO_CONFIG_ENCRYPTION_KEY must be a base64-encoded 32-byte key")
	}
	files := carrierconfig.Files{SchemaVersion: 1}
	for name, target := range map[string]*json.RawMessage{
		"entitlement.json": &files.Entitlement, "wfc.json": &files.WFC, "ims.json": &files.IMS,
	} {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		*target = data
	}
	compressed, revision, err := carrierconfig.Pack(files)
	if err != nil {
		return err
	}
	encrypter, err := jose.NewEncrypter(jose.A256GCM, jose.Recipient{Algorithm: jose.DIRECT, Key: key}, nil)
	if err != nil {
		return fmt.Errorf("create configuration encryption: %w", err)
	}
	encrypted, err := encrypter.Encrypt(compressed)
	if err != nil {
		return fmt.Errorf("encrypt carrier files: %w", err)
	}
	compact, err := encrypted.CompactSerialize()
	if err != nil {
		return fmt.Errorf("serialize carrier files: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(output, "releases"), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(output, "releases", revision+".enc"), []byte(compact), 0o600); err != nil {
		return err
	}
	pointer, err := json.Marshal(struct {
		SchemaVersion int    `json:"schemaVersion"`
		Revision      string `json:"revision"`
	}{SchemaVersion: 1, Revision: revision})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(output, "current.json"), pointer, 0o600); err != nil {
		return err
	}
	fmt.Println(revision)
	return nil
}
