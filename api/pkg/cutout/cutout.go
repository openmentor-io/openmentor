// Package cutout removes photo backgrounds via a rembg sidecar service and
// quality-gates the result, powering the catalog's "hero" cut-out cards.
//
// The client POSTs the original image to the rembg HTTP server (an internal
// compose service) and receives a PNG with an alpha channel. Because model
// output on arbitrary user photos can be garbage (shredded masks, empty or
// full frames), every cutout goes through QualityGate before it is accepted;
// rejected cutouts leave the mentor on the 'frame' treatment.
package cutout

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"
)

// Photo styles, matching the values stored in mentors.photo_style and the
// frontend treatment. Kept here (a package both the API service layer and the
// worker import) so producers agree on the strings.
const (
	StyleHero  = "hero"
	StyleFrame = "frame"
)

// Client calls a rembg server (https://github.com/danielgatis/rembg, `rembg s`).
type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

// Config configures the cutout client. An empty ServiceURL disables cutouts
// entirely (callers should check Enabled()).
type Config struct {
	ServiceURL     string // e.g. http://rembg:7000
	Model          string // rembg model name, e.g. isnet-general-use, birefnet-portrait
	TimeoutSeconds int
}

// New creates a cutout client, or nil when the service URL is empty
// (feature disabled). A nil *Client is safe to pass around; Enabled()
// reports false on it.
func New(cfg Config) *Client {
	if cfg.ServiceURL == "" {
		return nil
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL: cfg.ServiceURL,
		model:   cfg.Model,
		http:    &http.Client{Timeout: timeout},
	}
}

// Enabled reports whether background removal is configured.
func (c *Client) Enabled() bool {
	return c != nil
}

// Remove sends the image bytes to the rembg server and returns the cutout as
// PNG bytes (RGBA with transparent background). The caller is expected to run
// QualityGate on the result before using it.
func (c *Client) Remove(ctx context.Context, imageBytes []byte) ([]byte, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("cutout service not configured")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "image")
	if err != nil {
		return nil, fmt.Errorf("failed to build multipart body: %w", err)
	}
	if _, err = part.Write(imageBytes); err != nil {
		return nil, fmt.Errorf("failed to write multipart body: %w", err)
	}
	if err = writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize multipart body: %w", err)
	}

	endpoint := c.baseURL + "/api/remove"
	if c.model != "" {
		endpoint += "?model=" + url.QueryEscape(c.model)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, fmt.Errorf("failed to build cutout request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cutout service request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close() //nolint:errcheck
	}()

	if resp.StatusCode != http.StatusOK {
		// Read a short error excerpt for the log; never trust it further.
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, 512)) //nolint:errcheck
		return nil, fmt.Errorf("cutout service returned %d: %s", resp.StatusCode, string(excerpt))
	}

	png, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20)) // 32 MB cap
	if err != nil {
		return nil, fmt.Errorf("failed to read cutout response: %w", err)
	}
	if len(png) == 0 {
		return nil, fmt.Errorf("cutout service returned an empty body")
	}
	return png, nil
}
