package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Object struct {
	SHA256 string
	Size   int64
	Key    string
}

type Store interface {
	Put(context.Context, io.Reader) (Object, error)
	Open(context.Context, string) (ReadSeekCloser, error)
	Delete(context.Context, string) error
}

// SHA256Key returns the canonical object key shared by every storage backend.
// Two fan-out levels keep both local directories and object-store prefixes
// manageable while the complete digest remains visible at the leaf.
func SHA256Key(digest string) string {
	return filepath.ToSlash(filepath.Join("sha256", digest[:2], digest[2:4], digest))
}

func (s *Local) Delete(_ context.Context, key string) error {
	clean := filepath.Clean(key)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || len(clean) >= 3 && clean[:3] == ".."+string(filepath.Separator) {
		return fmt.Errorf("invalid blob key")
	}
	err := os.Remove(filepath.Join(s.root, clean))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

// Local is the authoritative content-addressed store. Files are written to a
// temporary sibling and atomically renamed only after their digest is known.
type Local struct{ root string }

func NewLocal(root string) (*Local, error) {
	if err := os.MkdirAll(filepath.Join(root, "sha256"), 0o750); err != nil {
		return nil, err
	}
	return &Local{root: root}, nil
}

func (s *Local) Put(ctx context.Context, source io.Reader) (Object, error) {
	temporary, err := os.CreateTemp(s.root, ".upload-*")
	if err != nil {
		return Object{}, err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)

	hash := sha256.New()
	written, err := copyContext(ctx, io.MultiWriter(temporary, hash), source)
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Object{}, err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	key := SHA256Key(digest)
	destination := filepath.Join(s.root, key)
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return Object{}, err
	}
	if _, err := os.Stat(destination); err == nil {
		return Object{SHA256: digest, Size: written, Key: key}, nil
	} else if !os.IsNotExist(err) {
		return Object{}, err
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return Object{}, fmt.Errorf("commit blob: %w", err)
	}
	return Object{SHA256: digest, Size: written, Key: key}, nil
}

func (s *Local) Open(_ context.Context, key string) (ReadSeekCloser, error) {
	clean := filepath.Clean(key)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || len(clean) >= 3 && clean[:3] == ".."+string(filepath.Separator) {
		return nil, fmt.Errorf("invalid blob key")
	}
	return os.Open(filepath.Join(s.root, clean))
}

func copyContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 128<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			written, err := destination.Write(buffer[:read])
			total += int64(written)
			if err != nil {
				return total, err
			}
			if written != read {
				return total, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}
