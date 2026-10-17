package bandbbs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (c *Client) newAttachmentKey(ctx context.Context, token, kind, sha, name string, contextFields map[string]string) (string, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("type", kind)
	for key, value := range contextFields {
		_ = writer.WriteField("context["+key+"]", value)
	}
	if err := writeAttachmentPart(writer, c, ctx, sha, name); err != nil {
		return "", "", err
	}
	writer.Close()
	var response struct {
		Key        string `json:"key"`
		Attachment struct {
			DirectURL string `json:"direct_url"`
		} `json:"attachment"`
	}
	err := c.request(ctx, token, http.MethodPost, c.apiURL+"/attachments/new-key", &body, writer.FormDataContentType(), &response)
	if err != nil {
		return "", "", err
	}
	if response.Key == "" {
		return "", "", fmt.Errorf("BandBBS did not return an attachment key")
	}
	return response.Key, response.Attachment.DirectURL, nil
}

func writeAttachmentPart(writer *multipart.Writer, c *Client, ctx context.Context, sha, name string) error {
	record, err := c.store.Blob(ctx, sha)
	if err != nil {
		return err
	}
	file, err := c.blobs.Open(ctx, record.LocalKey)
	if err != nil {
		return err
	}
	defer file.Close()
	part, err := writer.CreateFormFile("attachment", name)
	if err != nil {
		return err
	}
	_, err = io.Copy(part, file)
	return err
}

func mediaExtension(storage *store.Store, ctx context.Context, sha string) string {
	record, err := storage.Blob(ctx, sha)
	if err != nil {
		return ".bin"
	}
	switch record.MediaType {
	case "image/webp":
		return ".webp"
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	default:
		return ".bin"
	}
}
