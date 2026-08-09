package redact

import (
	"encoding/json"
	"os"
	"testing"
)

// emailMaskingContract is the shared fixture that keeps redact.Email and
// web/src/lib/pii.ts's maskEmail from drifting. C11's finding was three
// implementations of one function; two survive because Go and TypeScript cannot
// share code, and this file is what makes them one rule rather than two.
const emailMaskingContract = "testdata/email_masking.json"

func TestEmailMatchesTheSharedContract(t *testing.T) {
	contents, err := os.ReadFile(emailMaskingContract)
	if err != nil {
		t.Fatalf("read %s: %v", emailMaskingContract, err)
	}

	var contract struct {
		Cases []struct {
			Why string `json:"why"`
			In  string `json:"in"`
			Out string `json:"out"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(contents, &contract); err != nil {
		t.Fatalf("parse %s: %v", emailMaskingContract, err)
	}
	if len(contract.Cases) == 0 {
		t.Fatal("the contract has no cases, so it pins nothing")
	}

	for _, testCase := range contract.Cases {
		if got := Email(testCase.In); got != testCase.Out {
			t.Errorf("Email(%q) = %q, want %q — %s", testCase.In, got, testCase.Out, testCase.Why)
		}
	}
}
