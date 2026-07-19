package skills

import (
	"archive/zip"
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"time"
)

//go:embed sigmo-control
var skillFiles embed.FS

func SigmoControlArchive() ([]byte, error) {
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	modified := time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)
	err := fs.WalkDir(skillFiles, "sigmo-control", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := skillFiles.ReadFile(path)
		if err != nil {
			return err
		}
		header := &zip.FileHeader{Name: path, Method: zip.Deflate, Modified: modified}
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		if _, err := writer.Write(data); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		_ = archive.Close()
		return nil, fmt.Errorf("build Sigmo skill archive: %w", err)
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("close Sigmo skill archive: %w", err)
	}
	return buf.Bytes(), nil
}
