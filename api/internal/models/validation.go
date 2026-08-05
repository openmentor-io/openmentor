package models

import (
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"

	"github.com/openmentor-io/openmentor/api/pkg/safeurl"
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

// validateHTTPSURL restricts a field to an absolute https URL with a host.
// The predicate lives in pkg/safeurl because api/config applies the same rule
// to configured URLs; see that package for why validator's built-in `url` tag
// is not a substitute.
func validateHTTPSURL(fl validator.FieldLevel) bool {
	return safeurl.IsHTTPS(fl.Field().String())
}
