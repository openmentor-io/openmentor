package models

import (
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"

	"github.com/openmentor-io/openmentor/api/pkg/price"
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
	if err := v.RegisterValidation("price", validatePrice); err != nil {
		panic("models: cannot register the price validator: " + err.Error())
	}
}

// validateHTTPSURL restricts a field to an absolute https URL with a host.
// The predicate lives in pkg/safeurl because api/config applies the same rule
// to configured URLs; see that package for why validator's built-in `url` tag
// is not a substitute.
func validateHTTPSURL(fl validator.FieldLevel) bool {
	return safeurl.IsHTTPS(fl.Field().String())
}

// validatePrice restricts a field to the canonical price grammar — "Free",
// "Negotiable" or "$N" for a whole 1..1000. The predicate lives in pkg/price
// because mentors_price_chk enforces the same grammar in SQL; this tag is what
// turns a violation into a 400 on the field instead of a constraint error the
// repository layer surfaces as a 500.
//
// The `max=100` alongside it is deliberately left in place. It is what
// middleware's worst-case body arithmetic sizes this field against, and that
// calculation should stay bounded by the tag rather than by a predicate it
// cannot read.
func validatePrice(fl validator.FieldLevel) bool {
	return price.IsCanonical(fl.Field().String())
}
