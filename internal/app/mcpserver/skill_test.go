package mcpserver

import (
	"archive/zip"
	"bytes"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestSkillArchive(t *testing.T) {
	first, err := SkillArchive()
	if err != nil {
		t.Fatalf("SkillArchive() error = %v", err)
	}
	second, err := SkillArchive()
	if err != nil {
		t.Fatalf("SkillArchive() second error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("SkillArchive() is not deterministic")
	}

	archive, err := zip.NewReader(bytes.NewReader(first), int64(len(first)))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	names := make([]string, 0, len(archive.File))
	contents := make(map[string]string, len(archive.File))
	for _, file := range archive.File {
		names = append(names, file.Name)
		reader, err := file.Open()
		if err != nil {
			t.Fatalf("Open(%q) error = %v", file.Name, err)
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			t.Fatalf("ReadAll(%q) error = %v", file.Name, readErr)
		}
		if closeErr != nil {
			t.Fatalf("Close(%q) error = %v", file.Name, closeErr)
		}
		contents[file.Name] = string(data)
	}
	slices.Sort(names)
	want := []string{
		"sigmo-control/SKILL.md",
		"sigmo-control/agents/openai.yaml",
		"sigmo-control/references/tools.md",
		"sigmo-control/references/workflows.md",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("archive files = %v, want %v", names, want)
	}

	for name, content := range contents {
		if strings.Contains(content, "TODO") {
			t.Fatalf("archive file %q contains a TODO placeholder", name)
		}
	}
	tools := contents["sigmo-control/references/tools.md"]
	if !strings.Contains(tools, "`ussd.execute` | `execute_ussd`") {
		t.Fatal("tools reference does not include USSD")
	}
}
