package catalog

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
)

func TestResponseFormatErrorAlwaysHasDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	httputil.Error(rec, http.StatusBadRequest, "test_error", "测试错误说明")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if payload["code"] != "test_error" {
		t.Errorf("expected code test_error, got %v", payload["code"])
	}
	if payload["message"] != "测试错误说明" {
		t.Errorf("expected message 测试错误说明, got %v", payload["message"])
	}
	if _, ok := payload["detail"]; !ok {
		t.Errorf("expected detail field to be present even when empty")
	}
}

func TestCatalogItemSpecFieldNames(t *testing.T) {
	item := CatalogItem{
		ID:            "com.test.app",
		Name:          "测试应用",
		Kind:          "quick_app",
		Summary:       "一句话描述",
		Author:        "TestDev",
		Owner:         "TestDev",
		OwnerUID:      191699,
		Devices:       []string{"xmb9p"},
		Tags:          []string{"实用工具"},
		Attributes:    []string{"原创"},
		PaidType:      "free",
		IconURL:       "https://ob-api.zxor.org/api/v1/blobs/abc",
		CoverURL:      "https://ob-api.zxor.org/api/v1/blobs/def",
		Version:       "1.0.0",
		DownloadCount: 10,
		CoinCount:     5,
		Rating:        5.0,
		RatingCount:   1,
		CurationGrade: "standard",
		UpdatedAt:     "2026-10-10T20:00:00Z",
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]any
	_ = json.Unmarshal(data, &m)

	requiredFields := []string{"id", "name", "kind", "summary", "author", "owner_uid", "owner", "devices", "tags", "paid_type", "icon_url", "cover_url", "version", "download_count", "coin_count", "rating", "rating_count", "curation_grade", "updated_at"}
	for _, f := range requiredFields {
		if _, ok := m[f]; !ok {
			t.Errorf("missing spec required field in CatalogItem: %s", f)
		}
	}
}

func TestDownloadDeviceRedirectInvalidPath(t *testing.T) {
	handler := HandleDownloadDeviceRedirect(nil, "https://ob-api.zxor.org")

	// 1. Path too short
	req := httptest.NewRequest("GET", "/api/v1/resources/test/download", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short path, got %d", rec.Code)
	}

	// 2. Missing id/device
	req2 := httptest.NewRequest("GET", "/api/v1/resources//download/", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty id/device, got %d", rec2.Code)
	}
}

func TestR2ClientSigV4Signing(t *testing.T) {
	client := NewR2Client(config.R2Config{
		AccountID:       "acc123",
		AccessKeyID:     "key123",
		SecretAccessKey: "secret123",
		BucketName:      "test-bucket",
		PublicURL:       "https://pub.r2.dev",
	})

	if !client.IsConfigured() {
		t.Fatal("expected client to be configured")
	}

	req, _ := http.NewRequest("PUT", "https://acc123.r2.cloudflarestorage.com/test-bucket/ab/cd/testfile", nil)
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	payloadHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	client.signSigV4(req, "acc123.r2.cloudflarestorage.com", "/test-bucket/ab/cd/testfile", "PUT", payloadHash, now)

	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=key123/20261010/auto/s3/aws4_request") {
		t.Errorf("unexpected authorization header: %s", auth)
	}
	if !strings.Contains(auth, "SignedHeaders=host;x-amz-content-sha256;x-amz-date") {
		t.Errorf("expected SignedHeaders in auth header: %s", auth)
	}
	if !strings.Contains(auth, "Signature=") {
		t.Errorf("expected Signature in auth header: %s", auth)
	}

	if req.Header.Get("x-amz-date") != "20261010T120000Z" {
		t.Errorf("unexpected amz date: %s", req.Header.Get("x-amz-date"))
	}
	if req.Header.Get("x-amz-content-sha256") != payloadHash {
		t.Errorf("unexpected content sha256: %s", req.Header.Get("x-amz-content-sha256"))
	}
}

func TestEscapeObjectKey(t *testing.T) {
	key := "ab/cd/foo bar.bin"
	escaped := escapeObjectKey(key)
	if escaped != "ab/cd/foo%20bar.bin" {
		t.Errorf("expected ab/cd/foo%%20bar.bin, got %s", escaped)
	}
}

func TestSafeMediaTypeBlocksExecutableContent(t *testing.T) {
	for _, input := range []string{"text/html", "image/svg+xml", "application/javascript", "text/html; charset=utf-8"} {
		if got := safeMediaType(input); got != "application/octet-stream" {
			t.Fatalf("%q normalized to %q", input, got)
		}
	}
	if got := safeMediaType("image/webp"); got != "image/webp" {
		t.Fatalf("safe image type normalized to %q", got)
	}
}
