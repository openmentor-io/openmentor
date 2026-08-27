package models_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/stretchr/testify/require"
)

// bindPrice binds a JSON body carrying only price into the given request struct
// and reports whether validation accepted it, through gin's binding so the test
// exercises the same tag pipeline the handlers do.
func bindPrice(t *testing.T, target func() any, priceValue string) bool {
	t.Helper()

	body, err := json.Marshal(map[string]string{"price": priceValue})
	if err != nil {
		t.Fatal(err)
	}

	accepted := false
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/", func(c *gin.Context) {
		// Only the price verdict matters, so an error naming another field
		// counts as acceptance.
		if bindErr := c.ShouldBindJSON(target()); bindErr != nil {
			accepted = !bytes.Contains([]byte(bindErr.Error()), []byte("Price"))
			return
		}
		accepted = true
	})

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), req)
	return accepted
}

// The price column was free text with only `max=100` on it (D3), so the dropdown
// in the browser was the only thing deciding what a price could be — and D44
// already had to punch a hole in that dropdown for the values the getmentor
// migration wrote. This covers every request struct that accepts a
// mentor-supplied price; the grammar itself is tested in pkg/price.
func TestPriceBindingAcceptsOnlyCanonicalValues(t *testing.T) {
	structs := map[string]func() any{
		"RegisterMentorRequest":           func() any { return &models.RegisterMentorRequest{} },
		"SaveProfileRequest":              func() any { return &models.SaveProfileRequest{} },
		"AdminMentorProfileUpdateRequest": func() any { return &models.AdminMentorProfileUpdateRequest{} },
	}

	cases := []struct {
		price string
		want  bool
	}{
		{"Free", true},
		{"Negotiable", true},
		{"$1", true},
		{"$50", true},
		{"$1000", true},

		{"", false},
		{"$0", false},
		{"$1001", false},
		{"$050", false},
		{"50", false},
		{"$50.00", false},
		{"$50 / hour", false},
		{"free", false},
		{"Negotiable ", false},
		// 100 runes: legal under max=100, rejected by the grammar. Without the
		// price tag this is exactly what the field would still accept.
		{string(bytes.Repeat([]byte("a"), 100)), false},
	}

	for name, newTarget := range structs {
		for _, tc := range cases {
			t.Run(name+"/"+tc.price, func(t *testing.T) {
				require.Equal(t, tc.want, bindPrice(t, newTarget, tc.price),
					"%s rejected/accepted %q against expectation", name, tc.price)
			})
		}
	}
}

// The analog of TestEveryBoundURLFieldRequiresHTTPS: read the source of every
// request struct rather than naming the fields, so a FOURTH price field added
// next month is covered without anyone remembering this file exists. A bound
// price field without the tag is back to free text, and the database constraint
// then turns the save into a 500 from the repository instead of a 400 on the
// field.
func TestEveryBoundPriceFieldRequiresTheGrammar(t *testing.T) {
	checked := 0
	for _, file := range modelSourceFiles(t) {
		for _, s := range boundStructs(t, file) {
			for _, field := range s.fields {
				if field.name != "Price" {
					continue
				}
				checked++
				t.Run(s.name+"."+field.name, func(t *testing.T) {
					require.Contains(t, splitBinding(field.binding), "price",
						"%s.%s is a price on a struct bound from a request body, so it must carry "+
							"the price tag (see pkg/price); binding tag is %q",
						s.name, field.name, field.binding)
				})
			}
		}
	}
	require.NotZero(t, checked, "found no bound Price fields; the guard is not reaching the models")
}
