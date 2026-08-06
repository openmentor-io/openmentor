package services_test

import (
	"io"
	"net/http"
	"strings"

	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
	"github.com/openmentor-io/openmentor/api/pkg/turnstile"
)

// siteverifyClient is the ONE httpclient.Client fake the service tests share. It
// answers Cloudflare's siteverify with a fixed verdict and every other request —
// the fire-and-forget worker triggers — with a bare 200, which is what the real
// client does.
//
// It replaced three near-identical stubs that each answered the captcha through
// Post while returning `{}` from Do. `{}` decodes to Success:false, so the moment
// Verify started carrying a context (and therefore using Do), those stubs
// silently flipped from "captcha passes" to "captcha fails" — the same class of
// defect as a mock that accepts a NULL into a *string. Routing on the URL keeps
// the fake honest about which call it is answering.
type siteverifyClient struct {
	verdict string
}

func (c siteverifyClient) Do(req *http.Request) (*http.Response, error) {
	body := "{}"
	if req.URL.String() == turnstile.VerifyURL {
		body = c.verdict
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

// Post is unreachable on purpose: nothing in the API may verify a captcha or
// fire a trigger without a context, and Post cannot carry one.
func (siteverifyClient) Post(_, _ string, _ io.Reader) (*http.Response, error) {
	panic("httpclient.Client.Post carries no context; use Do")
}

func (siteverifyClient) Get(string) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("{}")),
		Header:     make(http.Header),
	}, nil
}

var _ httpclient.Client = siteverifyClient{}

// captchaOK / captchaFail are the two verdicts the tests need.
var (
	captchaOK   = siteverifyClient{verdict: `{"success":true}`}
	captchaFail = siteverifyClient{verdict: `{"success":false,"error-codes":["invalid-input-response"]}`}
)
