package creator

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"image"
	"image/png"
	"testing"
)

func TestVerifyBundleMediaWebPAndPNG(t *testing.T) {
	service := New(nil, nil, Limits{
		PreviewMaxBytes: 10 << 20,
		PreviewMaxCount: 5,
	})

	// 192x192 WebP image
	webpRaw, err := base64.StdEncoding.DecodeString("UklGRpQAAABXRUJQVlA4IIgAAAAwCwCdASrAAMAAPm02mUmkIyKhIEgAgA2JaW7hdqlwH4AAAJ7Xoq4QZBDVUmu20XCDIIaqk122i4QZBDVUmu20XCDIIaqk122i4QZBDVUmu20XCDIIaqk122i4QZBDVUmugAD+/3A9///kFywuuRr//8gP+QH/ID/+PimZpSEAhAAAAAAAAAAA")
	if err != nil {
		t.Fatalf("failed to decode sample webp: %v", err)
	}
	webpDigest := sha256.Sum256(webpRaw)
	webpSHA := hex.EncodeToString(webpDigest[:])

	// 100x100 PNG image
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, image.NewNRGBA(image.Rect(0, 0, 100, 100))); err != nil {
		t.Fatalf("failed to encode sample png: %v", err)
	}
	pngRaw := pngBuf.Bytes()
	pngDigest := sha256.Sum256(pngRaw)
	pngSHA := hex.EncodeToString(pngDigest[:])

	// Build zip in memory
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)

	wIcon, err := zw.Create("media/icon.webp")
	if err != nil {
		t.Fatalf("zip create icon failed: %v", err)
	}
	if _, err := wIcon.Write(webpRaw); err != nil {
		t.Fatalf("zip write icon failed: %v", err)
	}

	wPreview, err := zw.Create("media/preview-0.png")
	if err != nil {
		t.Fatalf("zip create preview failed: %v", err)
	}
	if _, err := wPreview.Write(pngRaw); err != nil {
		t.Fatalf("zip write preview failed: %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("zip close failed: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipBuf.Bytes()), int64(zipBuf.Len()))
	if err != nil {
		t.Fatalf("zip open failed: %v", err)
	}
	files := make(map[string]*zip.File)
	for _, f := range zr.File {
		files[f.Name] = f
	}

	manifest := &publishManifest{}
	manifest.Media.Icon = &publishMediaRef{
		File:   "media/icon.webp",
		SHA256: webpSHA,
		Width:  192,
		Height: 192,
	}
	manifest.Media.Previews = []publishMediaRef{
		{
			File:   "media/preview-0.png",
			SHA256: pngSHA,
			Width:  100,
			Height: 100,
		},
	}

	verified, err := service.verifyBundleMedia(files, manifest, true)
	if err != nil {
		t.Fatalf("verifyBundleMedia unexpected error: %v", err)
	}

	if len(verified) != 2 {
		t.Fatalf("verifyBundleMedia returned %d items, want 2", len(verified))
	}

	iconItem := verified[0]
	if iconItem.role != "icon" || iconItem.mediaType != "image/webp" || iconItem.width != 192 || iconItem.height != 192 {
		t.Errorf("verified icon = %+v, want role=icon, mediaType=image/webp, 192x192", iconItem)
	}

	previewItem := verified[1]
	if previewItem.role != "preview" || previewItem.mediaType != "image/png" || previewItem.width != 100 || previewItem.height != 100 {
		t.Errorf("verified preview = %+v, want role=preview, mediaType=image/png, 100x100", previewItem)
	}
}

func TestVerifyBundleMediaUndecodableImage(t *testing.T) {
	service := New(nil, nil, Limits{
		PreviewMaxBytes: 10 << 20,
		PreviewMaxCount: 5,
	})

	corruptBytes := []byte("this is not an image at all")
	digest := sha256.Sum256(corruptBytes)
	sha := hex.EncodeToString(digest[:])

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	w, _ := zw.Create("media/icon.webp")
	w.Write(corruptBytes)
	zw.Close()

	zr, _ := zip.NewReader(bytes.NewReader(zipBuf.Bytes()), int64(zipBuf.Len()))
	files := make(map[string]*zip.File)
	for _, f := range zr.File {
		files[f.Name] = f
	}

	manifest := &publishManifest{}
	manifest.Media.Icon = &publishMediaRef{
		File:   "media/icon.webp",
		SHA256: sha,
		Width:  192,
		Height: 192,
	}

	_, err := service.verifyBundleMedia(files, manifest, false)
	if err == nil || !errors.Is(err, ErrInvalid) {
		t.Fatalf("verifyBundleMedia error = %v, want ErrInvalid", err)
	}
}
