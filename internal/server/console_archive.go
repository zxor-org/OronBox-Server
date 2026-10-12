package server

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxTarFileSize   int64 = 50 * 1024 * 1024  // 单个文件最大 50MB
	maxTarTotalBytes int64 = 250 * 1024 * 1024 // 解压总大小最大 250MB
	maxTarFileCount  int   = 5000              // 解压总文件数最大 5000
)

func untarArchive(src io.Reader, destDir string) error {
	gzr, err := gzip.NewReader(src)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gzr.Close()

	cleanDest := filepath.Clean(destDir)
	tr := tar.NewReader(gzr)
	var totalWritten int64
	var fileCount int

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		fileCount++
		if fileCount > maxTarFileCount {
			return fmt.Errorf("archive exceeds maximum file count limit (%d)", maxTarFileCount)
		}

		cleanName := filepath.Clean(header.Name)
		if strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
			continue
		}
		target := filepath.Join(cleanDest, cleanName)
		if !strings.HasPrefix(target, cleanDest+string(filepath.Separator)) && target != cleanDest {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if header.Size > maxTarFileSize {
				return fmt.Errorf("file %q exceeds max size %d bytes", header.Name, maxTarFileSize)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
			if err != nil {
				return err
			}
			limitedReader := io.LimitReader(tr, maxTarFileSize+1)
			written, err := io.Copy(f, limitedReader)
			f.Close()
			if err != nil {
				return err
			}
			if written > maxTarFileSize {
				return fmt.Errorf("file %q exceeds max size %d bytes", header.Name, maxTarFileSize)
			}
			totalWritten += written
			if totalWritten > maxTarTotalBytes {
				return fmt.Errorf("archive total uncompressed size exceeds limit of %d bytes", maxTarTotalBytes)
			}
		}
	}
	return nil
}
