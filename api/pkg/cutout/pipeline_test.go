package cutout

import (
	"context"
	"image"
	"net/http"
	"net/http/httptest"
	"testing"
)

// rembgStub serves a fixed PNG (or status) from /api/remove, standing in for
// the rembg sidecar so the pipeline can be exercised without the container.
func rembgStub(t *testing.T, status int, png []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/remove" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
}

func heroPNG(t *testing.T) []byte {
	t.Helper()
	img := blank(200, 200)
	fillRect(img, 50, 40, 150, 200) // one dominant central blob -> passes the gate
	return encode(t, img)
}

func TestProcessImage_HeroUploadsAndReturnsHero(t *testing.T) {
	srv := rembgStub(t, http.StatusOK, heroPNG(t))
	defer srv.Close()

	var uploaded []byte
	upload := func(_ context.Context, png []byte) error { uploaded = png; return nil }

	res := New(Config{ServiceURL: srv.URL}).ProcessImage(context.Background(), SourceUpload, []byte("src"), upload)
	if res.Outcome != OutcomeHero || res.Style != StyleHero {
		t.Fatalf("want hero, got outcome=%q style=%q err=%v", res.Outcome, res.Style, res.Err)
	}
	if len(uploaded) == 0 {
		t.Fatal("expected the hero PNG to be uploaded")
	}
}

func TestProcessImage_GateRejectFallsBackToFrameWithoutUpload(t *testing.T) {
	// All-transparent PNG -> gate rejects (too_small) -> frame, no upload.
	srv := rembgStub(t, http.StatusOK, encode(t, image.Image(blank(200, 200))))
	defer srv.Close()

	uploadCalled := false
	upload := func(_ context.Context, _ []byte) error { uploadCalled = true; return nil }

	res := New(Config{ServiceURL: srv.URL}).ProcessImage(context.Background(), SourceBackfill, []byte("src"), upload)
	if res.Outcome != OutcomeFrame || res.Style != StyleFrame {
		t.Fatalf("want frame, got outcome=%q style=%q", res.Outcome, res.Style)
	}
	if uploadCalled {
		t.Fatal("upload must not run for a gate-rejected cutout")
	}
	if res.Gate.Code != ReasonTooSmall {
		t.Fatalf("want gate code %q, got %q", ReasonTooSmall, res.Gate.Code)
	}
}

func TestProcessImage_RemoveErrorIsError(t *testing.T) {
	srv := rembgStub(t, http.StatusInternalServerError, nil)
	defer srv.Close()

	res := New(Config{ServiceURL: srv.URL}).ProcessImage(context.Background(), SourceUpload, []byte("src"),
		func(_ context.Context, _ []byte) error { return nil })
	if res.Outcome != OutcomeError || res.Style != StyleFrame || res.Err == nil {
		t.Fatalf("want error+frame, got outcome=%q style=%q err=%v", res.Outcome, res.Style, res.Err)
	}
}
