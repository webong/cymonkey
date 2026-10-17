// Package volumearchive contains safe local volume snapshot archive mechanics.
// Volume identity, authorization, retention, and transfer policy remain
// outside this package.
package volumearchive

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Export writes a gzip-compressed tar archive containing regular files and
// directories below root. Symlinks and special files are intentionally
// rejected so an archive cannot capture an unexpected host surface.
func Export(ctx context.Context, output io.Writer, root string) error {
	pinned, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer pinned.Close()
	return ExportRoot(ctx, output, pinned)
}

// ExportRoot uses a pinned directory handle so concurrent renames cannot escape it.
func ExportRoot(ctx context.Context, output io.Writer, root *os.Root) error {
	gzipWriter := gzip.NewWriter(output)
	tarWriter := tar.NewWriter(gzipWriter)
	writeErr := fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("snapshot refuses unsupported file %q", path)
		}
		var input *os.File
		if !info.IsDir() {
			input, err = root.Open(path)
			if err != nil {
				return err
			}
			defer input.Close()
			info, err = input.Stat()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return errors.New("snapshot source changed type")
			}
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = path
		if info.IsDir() {
			header.Name += "/"
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		_, err = io.Copy(tarWriter, contextReader{ctx: ctx, reader: input})
		return err
	})
	closeTarErr := tarWriter.Close()
	closeGzipErr := gzipWriter.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeTarErr != nil {
		return closeTarErr
	}
	return closeGzipErr
}

// Restore restores an archive into an empty destination. It stages the
// extraction beside the destination and validates every archive path before
// moving the staged entries into place.
// Limits bounds decompressed archive contents for a device transfer.
type Limits struct {
	Bytes   int64
	Entries int
}

func Restore(ctx context.Context, archivePath, destination string) error {
	return RestoreWithLimits(ctx, archivePath, destination, Limits{Bytes: 1 << 30, Entries: 100000})
}

func RestoreWithLimits(ctx context.Context, archivePath, destination string, limits Limits) error {
	if err := ensureEmptyDirectory(destination); err != nil {
		return err
	}
	pinned, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer pinned.Close()
	return RestoreRootWithLimits(ctx, archivePath, pinned, limits)
}

// RestoreRootWithLimits stages inside the pinned empty destination.
func RestoreRootWithLimits(ctx context.Context, archivePath string, destination *os.Root, limits Limits) error {
	if limits.Bytes <= 0 || limits.Entries <= 0 {
		return errors.New("archive requires positive extraction limits")
	}
	existing, err := fs.ReadDir(destination.FS(), ".")
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return errors.New("volume restore requires an empty destination")
	}
	var key [16]byte
	if _, err = rand.Read(key[:]); err != nil {
		return err
	}
	temporary := ".restore-" + hex.EncodeToString(key[:])
	if err = destination.Mkdir(temporary, 0700); err != nil {
		return err
	}
	defer destination.RemoveAll(temporary)
	stage, err := destination.OpenRoot(temporary)
	if err != nil {
		return err
	}
	defer stage.Close()
	archive, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		return fmt.Errorf("open volume snapshot: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	var extracted int64
	entriesRead := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read volume snapshot: %w", err)
		}
		entriesRead++
		if header.Size < 0 || header.Size > limits.Bytes-extracted || entriesRead > limits.Entries {
			return errors.New("archive extraction limit exceeded")
		}
		extracted += header.Size
		path, err := archiveDestination(".", header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := stage.MkdirAll(path, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := stage.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return err
			}
			output, err := stage.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(output, contextReader{ctx: ctx, reader: tarReader})
			closeErr := output.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("snapshot contains unsupported archive entry %q", header.Name)
		}
	}
	entries, err := fs.ReadDir(stage.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := destination.Rename(filepath.Join(temporary, entry.Name()), entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func ensureEmptyDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("volume restore requires an empty destination")
	}
	return nil
}

func archiveDestination(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || filepath.VolumeName(clean) != "" || strings.ContainsRune(clean, '\\') || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("snapshot archive entry %q escapes destination", name)
	}
	path := filepath.Join(root, clean)
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("snapshot archive entry %q escapes destination", name)
	}
	return path, nil
}

// FileDigest returns the byte size and SHA-256 digest of an archive.
func FileDigest(path string) (int64, string, error) {
	input, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer input.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, input)
	if err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
