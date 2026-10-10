package catalog

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
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

		w.Header().Set("Content-Type", mediaType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", sizeBytes))
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("ETag", fmt.Sprintf(`"%s"`, sha))

		http.ServeContent(w, r, sha, time.Now(), file)
	}
}

// HandleUploadBlob implements POST /api/v1/blobs
func HandleUploadBlob(s *store.Store, baseStorageDir string, r2 *R2Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		tempFile, err := os.CreateTemp("", "ob_upload_*")
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "temp_error", err.Error())
			return
		}
		defer os.Remove(tempFile.Name())
		defer tempFile.Close()

		hasher := sha256.New()
		tee := io.TeeReader(file, hasher)
		size, err := io.Copy(tempFile, tee)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "write_error", err.Error())
			return
		}

		sha := fmt.Sprintf("%x", hasher.Sum(nil))
		mtype := handler.Header.Get("Content-Type")
		if mtype == "" {
			mtype = "application/octet-stream"
		}

		targetDir := filepath.Join(baseStorageDir, sha[:2], sha[2:4])
		_ = os.MkdirAll(targetDir, 0755)
		targetPath := filepath.Join(targetDir, sha)

		if err := os.Rename(tempFile.Name(), targetPath); err != nil {
			_ = copyFile(tempFile.Name(), targetPath)
		}

		// Initial ref_count is 0 for unattached uploaded blobs; incremented when attached to a revision/plugin/collection
		_, err = s.Pool.Exec(r.Context(), `
			INSERT INTO blobs (sha256, size_bytes, media_type, ref_count, created_at)
			VALUES ($1, $2, $3, 0, now())
			ON CONFLICT (sha256) DO NOTHING
		`, sha, size, mtype)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		objectKey := fmt.Sprintf("%s/%s/%s", sha[:2], sha[2:4], sha)
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO blob_replicas (blob_sha256, backend, object_key, state, updated_at)
			VALUES ($1, 'local', $2, 'ready', now())
			ON CONFLICT (blob_sha256, backend) DO UPDATE SET object_key = $2, state = 'ready', updated_at = now()
		`, sha, objectKey)

		if r2 != nil && r2.IsConfigured() {
			_, _ = s.Pool.Exec(r.Context(), `
				INSERT INTO blob_replicas (blob_sha256, backend, object_key, state, updated_at)
				VALUES ($1, 'r2', $2, 'pending', now())
				ON CONFLICT (blob_sha256, backend) DO UPDATE SET object_key = $2, updated_at = now()
			`, sha, objectKey)

			go func(blobSha, key, mt string, sz int64, filePath string) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				f, err := os.Open(filePath)
				if err != nil {
					_, _ = s.Pool.Exec(ctx, `
						UPDATE blob_replicas 
						SET state = 'failed', error_message = $2, attempts = attempts + 1, updated_at = now()
						WHERE blob_sha256 = $1 AND backend = 'r2'
					`, blobSha, err.Error())
					return
				}
				defer f.Close()

				if err := r2.PutObject(ctx, key, mt, f, sz, blobSha); err != nil {
					_, _ = s.Pool.Exec(ctx, `
						UPDATE blob_replicas 
						SET state = 'failed', error_message = $2, attempts = attempts + 1, updated_at = now()
						WHERE blob_sha256 = $1 AND backend = 'r2'
					`, blobSha, err.Error())
					return
				}

				_, _ = s.Pool.Exec(ctx, `
					UPDATE blob_replicas 
					SET state = 'ready', error_message = '', updated_at = now()
					WHERE blob_sha256 = $1 AND backend = 'r2'
				`, blobSha)
			}(sha, objectKey, mtype, size, targetPath)
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"sha256":     sha,
			"size_bytes": size,
			"media_type": mtype,
		})
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
