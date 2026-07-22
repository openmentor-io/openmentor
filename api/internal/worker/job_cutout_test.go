package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openmentor-io/openmentor/api/pkg/cutout"
)

// fakeObjectStore is an in-memory ObjectStore for the cutout job tests.
type fakeObjectStore struct {
	get       map[string][]byte // key -> bytes (nil/absent => not found)
	getErr    error
	uploaded  map[string][]byte // key -> bytes written
	uploadErr error
}

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{get: map[string][]byte{}, uploaded: map[string][]byte{}}
}

func (f *fakeObjectStore) GetObject(_ context.Context, key string) ([]byte, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.get[key], nil
}

func (f *fakeObjectStore) UploadObject(_ context.Context, data []byte, key, _ string) (string, error) {
	if f.uploadErr != nil {
		return "", f.uploadErr
	}
	f.uploaded[key] = data
	return "https://storage.example/" + key, nil
}

// heroPNGBytes builds a PNG with one dominant central blob that passes the gate.
func heroPNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 200, 200))
	for y := 40; y < 200; y++ {
		for x := 50; x < 150; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 200, G: 150, B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

// wireCutout attaches a stub rembg server and a fake object store to the env's
// handlers, enabling the cutout endpoints. Returns the object store for asserts.
func (e *jobsTestEnv) wireCutout(t *testing.T, pngBytes []byte) (*fakeObjectStore, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/remove" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	store := newFakeObjectStore()
	e.handlers.objects = store
	e.handlers.cutout = cutout.New(cutout.Config{ServiceURL: srv.URL})
	return store, srv
}

func TestCutoutMentor_HappyPathWritesHero(t *testing.T) {
	env := newJobsTestEnv()
	store, srv := env.wireCutout(t, heroPNGBytes(t))
	defer srv.Close()

	env.repo.cutoutByID = map[string]*CutoutMentor{"m1": {ID: "m1", Slug: "jane-doe-1"}}
	store.get["jane-doe-1/full"] = []byte("source-photo")

	w := env.do("POST", "/jobs/cutout-mentor?mentorId=m1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := env.repo.photoStyleUpdates["m1"]; got != cutout.StyleHero {
		t.Fatalf("want photo_style hero written, got %q", got)
	}
	if _, ok := store.uploaded["jane-doe-1/hero"]; !ok {
		t.Fatal("expected the hero asset to be uploaded")
	}

	var body struct {
		Success bool               `json:"success"`
		Result  CutoutMentorResult `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body.Success || body.Result.Outcome != "ok" || body.Result.PhotoStyle != cutout.StyleHero {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestCutoutMentor_NoPhotoDoesNotWriteStyle(t *testing.T) {
	env := newJobsTestEnv()
	_, srv := env.wireCutout(t, heroPNGBytes(t))
	defer srv.Close()

	env.repo.cutoutByID = map[string]*CutoutMentor{"m1": {ID: "m1", Slug: "jane-doe-1"}}
	// no source object in the store => no_photo

	w := env.do("POST", "/jobs/cutout-mentor?mentorId=m1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if _, wrote := env.repo.photoStyleUpdates["m1"]; wrote {
		t.Fatal("photo_style must not be written when there is no source photo")
	}
}

func TestCutoutMentor_MissingMentorID(t *testing.T) {
	env := newJobsTestEnv()
	_, srv := env.wireCutout(t, heroPNGBytes(t))
	defer srv.Close()

	if w := env.do("POST", "/jobs/cutout-mentor", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestCutoutMentor_NotFound(t *testing.T) {
	env := newJobsTestEnv()
	_, srv := env.wireCutout(t, heroPNGBytes(t))
	defer srv.Close()

	env.repo.cutoutByID = map[string]*CutoutMentor{}
	if w := env.do("POST", "/jobs/cutout-mentor?mentorId=nope", nil); w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestCutoutMentor_NoSlugUnprocessable(t *testing.T) {
	env := newJobsTestEnv()
	_, srv := env.wireCutout(t, heroPNGBytes(t))
	defer srv.Close()

	env.repo.cutoutByID = map[string]*CutoutMentor{"m1": {ID: "m1", Slug: ""}}
	if w := env.do("POST", "/jobs/cutout-mentor?mentorId=m1", nil); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d", w.Code)
	}
}

func TestCutoutMentor_DisabledReturns503(t *testing.T) {
	env := newJobsTestEnv() // default: no cutout/objects configured
	if w := env.do("POST", "/jobs/cutout-mentor?mentorId=m1", nil); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 when cutout disabled, got %d", w.Code)
	}
}

func TestBackfillCutouts_AggregatesOutcomes(t *testing.T) {
	env := newJobsTestEnv()
	store, srv := env.wireCutout(t, heroPNGBytes(t))
	defer srv.Close()

	// m1 has a photo (-> hero), m2 has none (-> no_photo).
	env.repo.cutoutMentors = []CutoutMentor{{ID: "m1", Slug: "a-1"}, {ID: "m2", Slug: "b-2"}}
	store.get["a-1/full"] = []byte("src")

	w := env.do("POST", "/jobs/backfill-cutouts", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Result BackfillCutoutsResult `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	r := body.Result
	if r.Total != 2 || r.Hero != 1 || r.NoPhoto != 1 || r.Processed != 1 || r.Errors != 0 {
		t.Fatalf("unexpected aggregate: %+v", r)
	}
	if env.repo.photoStyleUpdates["m1"] != cutout.StyleHero {
		t.Fatalf("m1 should be hero, got %q", env.repo.photoStyleUpdates["m1"])
	}
	if _, wrote := env.repo.photoStyleUpdates["m2"]; wrote {
		t.Fatal("m2 (no photo) must not get a photo_style write")
	}
}
