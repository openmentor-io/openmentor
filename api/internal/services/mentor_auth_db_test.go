package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/cache"
	"github.com/openmentor-io/openmentor/api/internal/repository"
	"github.com/openmentor-io/openmentor/api/test/dbtest"
)

// These drive the mentor auth service against a real database, because the two
// facts they establish are properties of the SQL and of the service's ordering
// around it, and the service holds a concrete *repository.MentorRepository (so
// there is no seam to inject a fake into). Skips without a database — see the
// dbtest package docs.

const dbTestJWTSecret = "service-db-test-secret-0123456789abcdef"

func authServiceConfig() *config.Config {
	cfg := &config.Config{}
	cfg.MentorSession.JWTSecret = dbTestJWTSecret
	cfg.MentorSession.JWTIssuer = "openmentor-api-test"
	cfg.MentorSession.SessionTTLHours = 24
	cfg.MentorSession.LoginTokenTTLMinutes = 30
	return cfg
}

func newAuthServiceWithDB(t *testing.T) (*MentorAuthService, *repository.MentorRepository, string) {
	t.Helper()

	pool := dbtest.Pool(t)
	repo := repository.NewMentorRepository(pool, cache.NewTagsCache(
		func(context.Context) (map[string]string, error) { return map[string]string{}, nil },
	))

	var mentorID string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO mentors (slug, name, status, email)
		VALUES ('auth-service-db-test', 'Auth Service Test', 'active', 'auth-db@example.test')
		RETURNING id
	`).Scan(&mentorID)
	if err != nil {
		t.Fatalf("seed mentor: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM mentors WHERE id = $1`, mentorID)
	})

	return NewMentorAuthService(repo, authServiceConfig(), nil, nil), repo, mentorID
}

// TestVerifyLoginMintsNoSessionWhenTheConsumptionFails is an H1 acceptance
// criterion. The old code logged a failed clear and minted the session anyway, so
// a transient database error handed out a 24-hour session AND left the link
// reusable. A canceled context is the injection: the consuming UPDATE fails for a
// reason that is not "no such token".
func TestVerifyLoginMintsNoSessionWhenTheConsumptionFails(t *testing.T) {
	svc, repo, mentorID := newAuthServiceWithDB(t)

	const token = "mtk_injected_failure_probe"
	if err := repo.SetLoginToken(context.Background(), mentorID, token, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("SetLoginToken: %v", err)
	}

	failing, cancel := context.WithCancel(context.Background())
	cancel()

	session, jwtToken, err := svc.VerifyLogin(failing, token)
	if err == nil {
		t.Fatal("VerifyLogin succeeded despite a failing consumption")
	}
	if errors.Is(err, ErrInvalidLoginToken) {
		// Reporting a database failure as a bad token sends the mentor off to
		// request another link that will fail exactly the same way.
		t.Errorf("error = %v, want a server error rather than ErrInvalidLoginToken", err)
	}
	if session != nil || jwtToken != "" {
		t.Fatalf("a session was minted anyway: session=%v token_len=%d", session, len(jwtToken))
	}

	// The link is untouched, so the mentor's next click still works.
	if _, _, err := svc.VerifyLogin(context.Background(), token); err != nil {
		t.Fatalf("the link stopped working after a failed attempt: %v", err)
	}
}

// TestVerifyLoginSpendsTheTokenExactlyOnce: the second use of a link must fail,
// and the session it did mint must satisfy the tightened JWT contract.
func TestVerifyLoginSpendsTheTokenExactlyOnce(t *testing.T) {
	svc, repo, mentorID := newAuthServiceWithDB(t)
	ctx := context.Background()

	const token = "mtk_single_use_probe"
	if err := repo.SetLoginToken(ctx, mentorID, token, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("SetLoginToken: %v", err)
	}

	session, jwtToken, err := svc.VerifyLogin(ctx, token)
	if err != nil {
		t.Fatalf("VerifyLogin: %v", err)
	}
	if session == nil || session.MentorID != mentorID {
		t.Fatalf("session = %+v, want one for %s", session, mentorID)
	}

	claims, err := svc.GetTokenManager().ValidateMentorToken(jwtToken)
	if err != nil {
		t.Fatalf("the session token this service minted does not satisfy its own contract: %v", err)
	}
	if claims.SessionVersion != 1 {
		t.Errorf("session_version = %d, want the row's value (1) — revocation needs it", claims.SessionVersion)
	}

	if _, _, err := svc.VerifyLogin(ctx, token); !errors.Is(err, ErrInvalidLoginToken) {
		t.Fatalf("second use of the link: error = %v, want ErrInvalidLoginToken", err)
	}
}

// TestVerifyLoginRefusesADeclinedMentor: eligibility is re-checked against the row
// the UPDATE returned, not against a snapshot read beforehand.
func TestVerifyLoginRefusesADeclinedMentor(t *testing.T) {
	svc, repo, mentorID := newAuthServiceWithDB(t)
	ctx := context.Background()

	const token = "mtk_declined_probe"
	if err := repo.SetLoginToken(ctx, mentorID, token, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("SetLoginToken: %v", err)
	}
	pool := dbtest.Pool(t)
	if _, err := pool.Exec(ctx, `UPDATE mentors SET status = 'declined' WHERE id = $1`, mentorID); err != nil {
		t.Fatalf("decline the mentor: %v", err)
	}

	session, jwtToken, err := svc.VerifyLogin(ctx, token)
	if !errors.Is(err, ErrMentorNotEligible) {
		t.Fatalf("error = %v, want ErrMentorNotEligible", err)
	}
	if session != nil || jwtToken != "" {
		t.Fatal("a declined mentor was given a session")
	}
}

// TestRevokeSessionInvalidatesAnIssuedToken is what makes logout more than a cookie
// delete: the token itself has to stop being accepted.
func TestRevokeSessionInvalidatesAnIssuedToken(t *testing.T) {
	svc, repo, mentorID := newAuthServiceWithDB(t)
	ctx := context.Background()

	const token = "mtk_revocation_probe"
	if err := repo.SetLoginToken(ctx, mentorID, token, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("SetLoginToken: %v", err)
	}
	_, jwtToken, err := svc.VerifyLogin(ctx, token)
	if err != nil {
		t.Fatalf("VerifyLogin: %v", err)
	}

	claims, err := svc.GetTokenManager().ValidateMentorToken(jwtToken)
	if err != nil {
		t.Fatalf("ValidateMentorToken: %v", err)
	}

	if err := svc.RevokeSession(ctx, jwtToken); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}

	_, version, err := repo.MentorSessionState(ctx, mentorID)
	if err != nil {
		t.Fatalf("MentorSessionState: %v", err)
	}
	if version == claims.SessionVersion {
		t.Fatalf("session_version is still %d after a logout: nothing was revoked", version)
	}

	// An unparseable cookie is not an error worth failing a logout over.
	if err := svc.RevokeSession(ctx, "not-a-jwt"); err != nil {
		t.Errorf("RevokeSession on garbage = %v, want nil", err)
	}
}
