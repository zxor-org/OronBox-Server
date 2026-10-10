package catalog

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/config"
)

// R2Client handles S3-compatible uploads to Cloudflare R2 using AWS SigV4
type R2Client struct {
	cfg        config.R2Config
	httpClient *http.Client
}

func NewR2Client(cfg config.R2Config) *R2Client {
	return &R2Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 300 * time.Second},
	}
}

func (c *R2Client) IsConfigured() bool {
	return c.cfg.AccountID != "" && c.cfg.BucketName != "" && c.cfg.AccessKeyID != "" && c.cfg.SecretAccessKey != ""
}

// PutObject uploads a stream with known sha256 to R2
func (c *R2Client) PutObject(ctx context.Context, objectKey string, mediaType string, body io.Reader, size int64, sha256Hex string) error {
	if !c.IsConfigured() {
		return fmt.Errorf("r2 is not configured")
	}

	host := fmt.Sprintf("%s.r2.cloudflarestorage.com", c.cfg.AccountID)
	escapedKey := escapeObjectKey(objectKey)
	canonicalURI := fmt.Sprintf("/%s/%s", c.cfg.BucketName, escapedKey)
	targetURL := fmt.Sprintf("https://%s%s", host, canonicalURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, targetURL, body)
	if err != nil {
		return err
	}

	req.ContentLength = size
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	req.Header.Set("Content-Type", mediaType)

	now := time.Now().UTC()
	c.signSigV4(req, host, canonicalURI, http.MethodPut, sha256Hex, now)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("r2 put failed: status %d body %s", resp.StatusCode, string(respBody))
}

// DeleteObject deletes an object from R2
func (c *R2Client) DeleteObject(ctx context.Context, objectKey string) error {
	if !c.IsConfigured() {
		return nil
	}

	host := fmt.Sprintf("%s.r2.cloudflarestorage.com", c.cfg.AccountID)
	escapedKey := escapeObjectKey(objectKey)
	canonicalURI := fmt.Sprintf("/%s/%s", c.cfg.BucketName, escapedKey)
	targetURL := fmt.Sprintf("https://%s%s", host, canonicalURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, targetURL, nil)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	emptyPayloadHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	c.signSigV4(req, host, canonicalURI, http.MethodDelete, emptyPayloadHash, now)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("r2 delete failed: status %d body %s", resp.StatusCode, string(respBody))
}

func (c *R2Client) signSigV4(req *http.Request, host, canonicalURI, method, payloadHash string, t time.Time) {
	dateStamp := t.Format("20060102")
	amzDate := t.Format("20060102T150405Z")

	req.Header.Set("Host", host)
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	canonicalHeaders := fmt.Sprintf("host:%s\nx-amz-content-sha256:%s\nx-amz-date:%s\n", host, payloadHash, amzDate)
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalReq := fmt.Sprintf("%s\n%s\n\n%s\n%s\n%s",
		method,
		canonicalURI,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	)

	hasher := sha256.New()
	hasher.Write([]byte(canonicalReq))
	canonicalReqHash := hex.EncodeToString(hasher.Sum(nil))

	scope := fmt.Sprintf("%s/auto/s3/aws4_request", dateStamp)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s",
		amzDate,
		scope,
		canonicalReqHash,
	)

	signingKey := getSignatureKey(c.cfg.SecretAccessKey, dateStamp, "auto", "s3")
	sig := hmacSHA256(signingKey, []byte(stringToSign))
	signatureHex := hex.EncodeToString(sig)

	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.cfg.AccessKeyID,
		scope,
		signedHeaders,
		signatureHex,
	)
	req.Header.Set("Authorization", authHeader)
}

func escapeObjectKey(key string) string {
	parts := strings.Split(key, "/")
	escaped := make([]string, len(parts))
	for i, p := range parts {
		escaped[i] = url.PathEscape(p)
	}
	return strings.Join(escaped, "/")
}

func hmacSHA256(key []byte, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func getSignatureKey(key, dateStamp, regionName, serviceName string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+key), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(regionName))
	kService := hmacSHA256(kRegion, []byte(serviceName))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	return kSigning
}
