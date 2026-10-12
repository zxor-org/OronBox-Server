package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

// HandleServeBlob implements GET /api/v1/blobs/{sha256}
func HandleServeBlob(s *store.Store, baseStorageDir string, r2PublicURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sha := strings.TrimPrefix(r.URL.Path, "/api/v1/blobs/")
		sha = strings.Trim(sha, "/")
		if len(sha) != 64 {
			httputil.Error(w, http.StatusBadRequest, "invalid_sha256", "哈希长度无效")
			return
		}
		if _, err := hex.DecodeString(sha); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_sha256", "哈希格式无效")
			return
		}

		var sizeBytes int64
		var mediaType string
		err := s.Pool.QueryRow(r.Context(), `
			SELECT size_bytes, media_type FROM blobs WHERE sha256 = $1
		`, sha).Scan(&sizeBytes, &mediaType)

		if err == pgx.ErrNoRows {
			httputil.Error(w, http.StatusNotFound, "blob_not_found", "文件不存在")
			return
		} else if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		if r.URL.Query().Get("line") != "local" {
			var objectKey string
			_ = s.Pool.QueryRow(r.Context(), `
				SELECT object_key FROM blob_replicas 
				WHERE blob_sha256 = $1 AND backend = 'r2' AND state = 'ready'
			`, sha).Scan(&objectKey)
			if objectKey != "" && r2PublicURL != "" {
				target := strings.TrimRight(r2PublicURL, "/") + "/" + objectKey
				http.Redirect(w, r, target, http.StatusFound)
				return
			}
		}

		path := filepath.Join(baseStorageDir, sha[:2], sha[2:4], sha)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			path = filepath.Join(baseStorageDir, sha)
		}

		file, err := os.Open(path)
		if err != nil {
			httputil.Error(w, http.StatusNotFound, "file_missing", "存储文件缺失")
			return
		}
		defer file.Close()

		mediaType = safeMediaType(mediaType)
		w.Header().Set("Content-Type", mediaType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if mediaType == "application/octet-stream" {
			w.Header().Set("Content-Disposition", "attachment; filename=\""+sha+"\"")
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", sizeBytes))
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("ETag", fmt.Sprintf(`"%s"`, sha))

		http.ServeContent(w, r, sha, time.Now(), file)
	}
}

// PersistUploadedBlob stores a bounded upload in the content-addressed local
// store and records the corresponding Blob row. It is shared by creator draft
// multipart handling and the public upload endpoint.
func PersistUploadedBlob(ctx context.Context, s *store.Store, baseStorageDir string, src io.Reader, mediaType string, r2Clients ...*R2Client) (string, int64, string, error) {
	const maxBlobSize = 100 << 20
	tempFile, err := os.CreateTemp("", "ob_upload_*")
	if err != nil {
		return "", 0, "", err
	}
	tempName := tempFile.Name()
	defer os.Remove(tempName)
	defer tempFile.Close()
	hasher := sha256.New()
	size, err := io.Copy(tempFile, io.LimitReader(io.TeeReader(src, hasher), maxBlobSize+1))
	if err != nil {
		return "", 0, "", err
	}
	if size > maxBlobSize {
		return "", 0, "", fmt.Errorf("blob exceeds 100 MiB")
	}
	if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
		return "", 0, "", err
	}
	header := make([]byte, 512)
	n, _ := tempFile.Read(header)
	if detected := http.DetectContentType(header[:n]); detected != "application/octet-stream" {
		mediaType = detected
	}
	mediaType = safeMediaType(mediaType)
	sha := fmt.Sprintf("%x", hasher.Sum(nil))
	targetDir := filepath.Join(baseStorageDir, sha[:2], sha[2:4])
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", 0, "", err
	}
	if err := tempFile.Close(); err != nil {
		return "", 0, "", err
	}
	targetPath := filepath.Join(targetDir, sha)
	installedByUs := false
	info, statErr := os.Stat(targetPath)
	if os.IsNotExist(statErr) {
		if err := os.Rename(tempName, targetPath); err != nil {
			// A concurrent upload may have installed the same content between
			// Stat and Rename. Preserve that file and discard our temporary copy.
			if _, raceErr := os.Stat(targetPath); raceErr == nil {
				// Another uploader won the race.
			} else if os.IsNotExist(raceErr) {
				if copyErr := copyFileExclusive(tempName, targetPath); copyErr != nil {
					return "", 0, "", copyErr
				}
				installedByUs = true
			} else {
				return "", 0, "", raceErr
			}
		} else {
			installedByUs = true
		}
	} else if statErr != nil {
		return "", 0, "", statErr
	} else if info.IsDir() {
		return "", 0, "", fmt.Errorf("blob target is a directory")
	}
	if _, err := s.Pool.Exec(ctx, `
		INSERT INTO blobs (sha256, size_bytes, media_type, ref_count, created_at)
		VALUES ($1, $2, $3, 1, now())
		ON CONFLICT (sha256) DO UPDATE SET ref_count = blobs.ref_count + 1
	`, sha, size, mediaType); err != nil {
		if installedByUs {
			_ = os.Remove(targetPath)
		}
		return "", 0, "", err
	}
	objectKey := fmt.Sprintf("%s/%s/%s", sha[:2], sha[2:4], sha)
	if _, err := s.Pool.Exec(ctx, `
		INSERT INTO blob_replicas (blob_sha256, backend, object_key, state, updated_at)
		VALUES ($1, 'local', $2, 'ready', now())
		ON CONFLICT (blob_sha256, backend) DO UPDATE SET object_key = $2, state = 'ready', updated_at = now()
	`, sha, objectKey); err != nil {
		return "", 0, "", err
	}
	if len(r2Clients) > 0 && r2Clients[0] != nil && r2Clients[0].IsConfigured() {
		r2 := r2Clients[0]
		if _, err := s.Pool.Exec(ctx, `
			INSERT INTO blob_replicas (blob_sha256, backend, object_key, state, updated_at)
			VALUES ($1, 'r2', $2, 'pending', now())
			ON CONFLICT (blob_sha256, backend) DO UPDATE SET object_key = $2, state = 'pending', updated_at = now()
		`, sha, objectKey); err != nil {
			return "", 0, "", err
		}
		go func() {
			repCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			f, openErr := os.Open(targetPath)
			if openErr != nil {
				_, _ = s.Pool.Exec(repCtx, `UPDATE blob_replicas SET state = 'failed', error_message = $2, attempts = attempts + 1, updated_at = now() WHERE blob_sha256 = $1 AND backend = 'r2'`, sha, openErr.Error())
				return
			}
			defer f.Close()
			if putErr := r2.PutObject(repCtx, objectKey, mediaType, f, size, sha); putErr != nil {
				_, _ = s.Pool.Exec(repCtx, `UPDATE blob_replicas SET state = 'failed', error_message = $2, attempts = attempts + 1, updated_at = now() WHERE blob_sha256 = $1 AND backend = 'r2'`, sha, putErr.Error())
				return
			}
			_, _ = s.Pool.Exec(repCtx, `UPDATE blob_replicas SET state = 'ready', error_message = '', updated_at = now() WHERE blob_sha256 = $1 AND backend = 'r2'`, sha)
		}()
	}
	return sha, size, mediaType, nil
}

// HandleUploadBlob implements POST /api/v1/blobs
func HandleUploadBlob(s *store.Store, baseStorageDir string, r2 *R2Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Leave room for multipart headers while keeping the file itself bounded
		// by PersistUploadedBlob's 100 MiB reader limit.
		r.Body = http.MaxBytesReader(w, r.Body, 110<<20)
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		file, handler, err := r.FormFile("file")
		if err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_file", "请提供 file 表单文件")
			return
		}
		defer file.Close()

		sha, size, mtype, err := PersistUploadedBlob(r.Context(), s, baseStorageDir, file, handler.Header.Get("Content-Type"), r2)
		if err != nil {
			if strings.Contains(err.Error(), "100 MiB") {
				httputil.Error(w, http.StatusRequestEntityTooLarge, "file_too_large", "文件超过 100 MiB 限制")
			} else {
				httputil.Error(w, http.StatusInternalServerError, "upload_failed", "文件保存失败")
			}
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"sha256":     sha,
			"size_bytes": size,
			"media_type": mtype,
		})
	}
}

func safeMediaType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "application/octet-stream"
	}
	if parsed, _, err := mime.ParseMediaType(value); err == nil {
		value = parsed
	}
	switch strings.ToLower(value) {
	case "image/png", "image/jpeg", "image/webp", "image/gif", "text/plain", "application/json", "application/pdf", "application/zip", "application/octet-stream":
		return value
	default:
		return "application/octet-stream"
	}
}

func copyFileExclusive(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
