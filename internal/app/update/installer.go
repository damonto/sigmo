package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var updateStateMu sync.Mutex

type pendingUpdate struct {
	Backup   string `json:"backup"`
	Attempts int    `json:"attempts"`
}

func Apply(ctx context.Context, executable string, release Release, body io.Reader) error {
	if !release.Verified {
		return ErrInvalidSignature
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	current, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("inspect executable: %w", err)
	}
	dir := filepath.Dir(resolved)
	tmp, err := os.CreateTemp(dir, ".sigmo-update-*")
	if err != nil {
		return fmt.Errorf("create update file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = removeIfExists(tmpPath) }() // Best-effort temporary-file cleanup.

	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(body, release.Artifact.Size+1))
	if copyErr != nil {
		closeBestEffort(tmp)
		return fmt.Errorf("write update file: %w", copyErr)
	}
	if written != release.Artifact.Size {
		closeBestEffort(tmp)
		return fmt.Errorf("verify update size: got %d, want %d", written, release.Artifact.Size)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(got, release.Artifact.SHA256) {
		closeBestEffort(tmp)
		return errors.New("verify update checksum: sha256 mismatch")
	}
	if err := tmp.Chmod(current.Mode().Perm()); err != nil {
		closeBestEffort(tmp)
		return fmt.Errorf("set update permissions: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		closeBestEffort(tmp)
		return fmt.Errorf("sync update file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close update file: %w", err)
	}
	if err := verifyExecutable(ctx, tmpPath, release.Manifest.Version); err != nil {
		return err
	}

	// Health confirmation and a new installation can overlap immediately after
	// startup. Serialize marker and backup changes so confirmation cannot remove
	// state that belongs to the next update.
	updateStateMu.Lock()
	defer updateStateMu.Unlock()

	backup := resolved + ".previous"
	if err := copyFile(resolved, backup, current.Mode().Perm()); err != nil {
		return fmt.Errorf("back up executable: %w", err)
	}
	marker := markerPath(resolved)
	if err := writePending(marker, pendingUpdate{Backup: backup}); err != nil {
		if removeErr := removeIfExists(backup); removeErr != nil {
			return errors.Join(err, fmt.Errorf("remove update backup: %w", removeErr))
		}
		return err
	}
	if err := os.Rename(tmpPath, resolved); err != nil {
		cleanupErr := errors.Join(
			wrapRemoveError("remove pending update marker", marker),
			wrapRemoveError("remove update backup", backup),
		)
		return errors.Join(fmt.Errorf("replace executable: %w", err), cleanupErr)
	}
	if err := syncDir(dir); err != nil {
		return err
	}
	return nil
}

// RecoverPending gives a newly installed binary one startup attempt. If that
// attempt did not mark itself healthy, the following startup restores the
// previous executable before any application state is opened.
func RecoverPending(executable string) (bool, error) {
	updateStateMu.Lock()
	defer updateStateMu.Unlock()

	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return false, fmt.Errorf("resolve executable path: %w", err)
	}
	marker := markerPath(resolved)
	data, err := os.ReadFile(marker)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read pending update: %w", err)
	}
	var pending pendingUpdate
	if err := json.Unmarshal(data, &pending); err != nil {
		return false, fmt.Errorf("decode pending update: %w", err)
	}
	if err := validatePendingUpdate(resolved, pending); err != nil {
		return false, err
	}
	if pending.Attempts == 0 {
		pending.Attempts = 1
		return false, writePending(marker, pending)
	}
	if err := os.Rename(pending.Backup, resolved); err != nil {
		return false, fmt.Errorf("restore previous executable: %w", err)
	}
	if err := os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("remove pending update marker: %w", err)
	}
	if err := syncDir(filepath.Dir(resolved)); err != nil {
		return false, err
	}
	return true, nil
}

func MarkHealthy(executable string) error {
	updateStateMu.Lock()
	defer updateStateMu.Unlock()

	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	marker := markerPath(resolved)
	data, err := os.ReadFile(marker)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read pending update: %w", err)
	}
	var pending pendingUpdate
	if err := json.Unmarshal(data, &pending); err != nil {
		return fmt.Errorf("decode pending update: %w", err)
	}
	if err := validatePendingUpdate(resolved, pending); err != nil {
		return err
	}
	if pending.Attempts == 0 {
		return nil
	}
	if err := os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove pending update marker: %w", err)
	}
	if err := syncDir(filepath.Dir(resolved)); err != nil {
		return err
	}
	if err := os.Remove(pending.Backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove previous executable: %w", err)
	}
	return syncDir(filepath.Dir(resolved))
}

func validatePendingUpdate(executable string, pending pendingUpdate) error {
	if filepath.Clean(pending.Backup) != executable+".previous" {
		return errors.New("validate pending update: backup path is invalid")
	}
	if pending.Attempts < 0 || pending.Attempts > 1 {
		return errors.New("validate pending update: attempt count is invalid")
	}
	return nil
}

func verifyExecutable(ctx context.Context, path string, wantVersion string) error {
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(checkCtx, path, "--version").Output()
	if err != nil {
		return fmt.Errorf("run downloaded executable: %w", err)
	}
	if strings.TrimSpace(string(output)) != wantVersion {
		return fmt.Errorf("verify downloaded version: got %q, want %q", strings.TrimSpace(string(output)), wantVersion)
	}
	return nil
}

func copyFile(from string, to string, mode os.FileMode) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.CreateTemp(filepath.Dir(to), ".sigmo-backup-*")
	if err != nil {
		return err
	}
	tmpPath := target.Name()
	defer func() { _ = removeIfExists(tmpPath) }() // Best-effort temporary-file cleanup.
	if err := target.Chmod(mode); err != nil {
		closeBestEffort(target)
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		closeBestEffort(target)
		return err
	}
	if err := target.Sync(); err != nil {
		closeBestEffort(target)
		return err
	}
	if err := target.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, to); err != nil {
		return err
	}
	return syncDir(filepath.Dir(to))
}

func writePending(path string, pending pendingUpdate) error {
	data, err := json.Marshal(pending)
	if err != nil {
		return fmt.Errorf("encode pending update: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sigmo-update-pending-*")
	if err != nil {
		return fmt.Errorf("create pending update marker: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = removeIfExists(tmpPath) }() // Best-effort temporary-file cleanup.
	if err := tmp.Chmod(0o600); err != nil {
		closeBestEffort(tmp)
		return fmt.Errorf("protect pending update marker: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		closeBestEffort(tmp)
		return fmt.Errorf("write pending update: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		closeBestEffort(tmp)
		return fmt.Errorf("sync pending update marker: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close pending update marker: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("commit pending update: %w", err)
	}
	return syncDir(filepath.Dir(path))
}

func markerPath(executable string) string { return executable + ".update-pending" }

func closeBestEffort(file *os.File) {
	_ = file.Close() // The caller is already returning the more useful I/O error.
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func wrapRemoveError(action, path string) error {
	if err := removeIfExists(path); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open executable directory: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync executable directory: %w", err)
	}
	return nil
}
