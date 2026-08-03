package models

import (
	"net/url"
	"strings"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

// init registers the custom binding tags used by the request structs in this
// package. It lives next to the structs so a tag can never ship without its
// implementation, and it covers every binary and test that binds a model.
func init() {
	v, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		return
	}
	if err := v.RegisterValidation("https_url", validateHTTPSURL); err != nil {
		panic("models: cannot register the https_url validator: " + err.Error())
	}
}

// urlUnsafeChars would let a URL break out of the HTML attribute or the
// plaintext line it is rendered into. url.Parse happily keeps them in the
// path.
const urlUnsafeChars = "\"'<>`\\ \t\r\n"

// validateHTTPSURL restricts a field to an absolute https URL with a host.
// validator's built-in `url` tag is not a substitute: it only requires a
// scheme plus a host or opaque part, so `javascript:alert(1)`,
// `data:text/html,...`, `mailto:a@b.c` and `https://evil.example/x">` all
// pass it (the scheme-restricting tag is the separate, unused `http_url`).
func validateHTTPSURL(fl validator.FieldLevel) bool {
	raw := fl.Field().String()
	if strings.ContainsAny(raw, urlUnsafeChars) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "https" && u.Host != "" && u.User == nil
}
