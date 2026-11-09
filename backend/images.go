package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// maxImageEdge keeps each image around ~1k tokens when sent to Claude.
const maxImageEdge = 1024

// ImageCache downloads listing images, downsizes them and caches them as JPEG on disk.
type ImageCache struct {
	dir    string
	client *http.Client
}

func newImageCache(dir string) (*ImageCache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &ImageCache{dir: dir, client: &http.Client{Timeout: 30 * time.Second}}, nil
}

// get returns a downsized JPEG for the image URL.
func (c *ImageCache) get(ctx context.Context, url string) ([]byte, error) {
	sum := sha1.Sum([]byte(url))
	path := filepath.Join(c.dir, hex.EncodeToString(sum[:])+".jpg")
	if b, err := os.ReadFile(path); err == nil {
		return b, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "image/jpeg,image/png,image/webp;q=0.8")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: status %d", url, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 30<<20))
	if err != nil {
		return nil, err
	}

	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", url, err)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > maxImageEdge || h > maxImageEdge {
		if w >= h {
			h = h * maxImageEdge / w
			w = maxImageEdge
		} else {
			w = w * maxImageEdge / h
			h = maxImageEdge
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pickEvenly picks up to n items spread evenly over the slice, always including the first.
func pickEvenly(items []string, n int) []string {
	if len(items) <= n {
		return items
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, items[i*len(items)/n])
	}
	return out
}
