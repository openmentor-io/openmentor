package models_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor/api/internal/models"
)

// bindCalendarURL binds a JSON body carrying only calendarUrl into the given
// request struct and reports whether validation accepted it. It goes through
// gin's binding so the test exercises the same tag pipeline the handlers do.
func bindCalendarURL(t *testing.T, target func() any, calendarURL string) bool {
	t.Helper()

	body, err := json.Marshal(map[string]string{"calendarUrl": calendarURL})
	if err != nil {
		t.Fatal(err)
	}

	accepted := false
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/", func(c *gin.Context) {
		// Only the calendarUrl verdict matters, so an error naming another
		// field counts as acceptance.
		if err := c.ShouldBindJSON(target()); err != nil {
			accepted = !bytes.Contains([]byte(err.Error()), []byte("CalendarURL"))
			return
		}
		accepted = true
	})

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), req)
	return accepted
}

// TestCalendarURLBindingRejectsNonHTTPSSchemes covers every request struct
// that accepts a mentor-supplied calendar URL. The built-in `url` tag these
// fields used only required a scheme plus a host or opaque part, so
// javascript:, data: and a URL carrying a quote all passed it.
func TestCalendarURLBindingRejectsNonHTTPSSchemes(t *testing.T) {
	structs := map[string]func() any{
		"RegisterMentorRequest":           func() any { return &models.RegisterMentorRequest{} },
		"SaveProfileRequest":              func() any { return &models.SaveProfileRequest{} },
		"AdminMentorProfileUpdateRequest": func() any { return &models.AdminMentorProfileUpdateRequest{} },
	}

	cases := []struct {
		url  string
		want bool
	}{
		{"https://calendly.com/johndoe", true},
		{"https://cal.com/team/openmentor/30min?month=2026-08", true},
		{"", true}, // omitempty: the field is optional

		{"javascript:alert(1)", false},
		{"JaVaScRiPt:alert(1)", false},
		{"data:text/html,<script>alert(1)</script>", false},
		{"http://calendly.com/johndoe", false},
		{"mailto:mentor@example.com", false},
		{"ftp://files.example/cal.ics", false},
		{"https:///no-host", false},
		{"not-a-valid-url", false},
		{`https://evil.example/x">`, false},
		{`https://evil.example/x"><script>alert(1)</script>`, false},
		{"https://evil.example/a b", false},
		{"https://user:pass@evil.example/x", false},
	}

	for name, target := range structs {
		for _, tc := range cases {
			t.Run(name+"/"+tc.url, func(t *testing.T) {
				if got := bindCalendarURL(t, target, tc.url); got != tc.want {
					t.Errorf("binding accepted=%v, want %v for calendarUrl=%q", got, tc.want, tc.url)
				}
			})
		}
	}
}
