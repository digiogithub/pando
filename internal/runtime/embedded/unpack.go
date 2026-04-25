package embedded

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
)

// Unpack extracts img into destDir. mutate.Extract emits a flattened tar stream,
// so OCI whiteouts are already resolved before the archive is written to disk.
func Unpack(ctx context.Context, img v1.Image, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create rootfs %q: %w", destDir, err)
	}

	reader := mutate.Extract(img)
	defer reader.Close()

	tarReader := tar.NewReader(reader)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		header, err := tarReader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read image layer tar stream: %w", err)
		}

		targetPath, err := safeJoin(destDir, header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, header.FileInfo().Mode().Perm()); err != nil {
				return fmt.Errorf("create directory %q: %w", targetPath, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("create parent directory for %q: %w", targetPath, err)
			}
			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, header.FileInfo().Mode().Perm())
			if err != nil {
				return fmt.Errorf("create file %q: %w", targetPath, err)
			}
			if _, err := io.Copy(file, tarReader); err != nil {
				file.Close()
				return fmt.Errorf("write file %q: %w", targetPath, err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("close file %q: %w", targetPath, err)
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("create parent directory for symlink %q: %w", targetPath, err)
			}
			if err := os.RemoveAll(targetPath); err != nil {
				return fmt.Errorf("replace symlink %q: %w", targetPath, err)
			}
			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				return fmt.Errorf("create symlink %q -> %q: %w", targetPath, header.Linkname, err)
			}
		case tar.TypeLink:
			linkTarget, err := safeJoin(destDir, header.Linkname)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("create parent directory for hardlink %q: %w", targetPath, err)
			}
			if err := os.RemoveAll(targetPath); err != nil {
				return fmt.Errorf("replace hardlink %q: %w", targetPath, err)
			}
			if err := os.Link(linkTarget, targetPath); err != nil {
				return fmt.Errorf("create hardlink %q -> %q: %w", targetPath, linkTarget, err)
			}
		default:
			return fmt.Errorf("unsupported tar entry %q (type %d)", header.Name, header.Typeflag)
		}
	}
}

func safeJoin(root, name string) (string, error) {
	cleanName := filepath.Clean(filepath.Join(string(os.PathSeparator), name))
	cleanName = strings.TrimPrefix(cleanName, string(os.PathSeparator))
	target := filepath.Join(root, cleanName)
	cleanRoot := filepath.Clean(root)
	if target != cleanRoot && !strings.HasPrefix(target, cleanRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry %q escapes destination root", name)
	}
	return target, nil
}
