package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/damonto/sigmo/internal/pkg/settings"
	"github.com/damonto/sigmo/internal/pkg/storage"
)

const authTestTTL = transactionTTL
const maxAuthTests = 64

// Auth test states and reasons are stable API values consumed by both clients.
type AuthTestStatus string
type AuthTestReason string

const (
	AuthTestStatusPending   AuthTestStatus = "pending"
	AuthTestStatusSucceeded AuthTestStatus = "succeeded"
	AuthTestStatusFailed    AuthTestStatus = "failed"

	AuthTestReasonDiscoveryFailed        AuthTestReason = "discovery_failed"
	AuthTestReasonAuthorizationFailed    AuthTestReason = "authorization_failed"
	AuthTestReasonTokenExchangeFailed    AuthTestReason = "token_exchange_failed"
	AuthTestReasonTokenInvalid           AuthTestReason = "token_invalid"
	AuthTestReasonSubjectNotAllowed      AuthTestReason = "subject_not_allowed"
	AuthTestReasonAccessAssertionMissing AuthTestReason = "access_assertion_missing"
	AuthTestReasonAccessAssertionInvalid AuthTestReason = "access_assertion_invalid"
)

// AuthTestResult is the safe, presentation-oriented result of an authentication test.
type AuthTestResult struct {
	Status  AuthTestStatus `json:"status"`
	Reason  AuthTestReason `json:"reason,omitempty"`
	Subject string         `json:"subject,omitempty"`
}

// OIDCTestStatus includes the expiry that bounds client-side polling.
type OIDCTestStatus struct {
	AuthTestResult
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

// OIDCTestRequest creates a short-lived test login without changing stored settings.
type OIDCTestRequest struct {
	Policy           settings.Auth
	Origin, BasePath string
	Native           bool
	State            string
}

// OIDCTestStartRequest identifies the browser entry point for a pending test.
type OIDCTestStartRequest struct {
	ID, Origin string
}

// OIDCTestCallback completes a provider callback for a test transaction.
type OIDCTestCallback struct {
	Origin, State, Code, Binding string
}

// OIDCTestCompletion reports where a native test must return after callback handling.
type OIDCTestCompletion struct {
	TestID, NativeState string
	Native              bool
	Result              AuthTestResult
}

type oidcTestStage string

const (
	oidcTestStageStart    oidcTestStage = "start"
	oidcTestStageStarting oidcTestStage = "starting"
	oidcTestStageCallback oidcTestStage = "callback"
	oidcTestStageComplete oidcTestStage = "complete"
)

type oidcTest struct {
	policy                        settings.Auth
	origin, basePath, nativeState string
	native                        bool
	stage                         oidcTestStage
	status                        AuthTestStatus
	reason                        AuthTestReason
	subject                       string
	expiresAt                     time.Time
}

// CreateOIDCTest records a bounded test transaction without persisting its client secret.
func (s *Store) CreateOIDCTest(req OIDCTestRequest) (string, time.Time, error) {
	if !req.Policy.HasMethod("oidc") || !validBrowserOrigin(req.Origin) || !validBrowserBasePath(req.BasePath) {
		return "", time.Time{}, ErrInvalidLogin
	}
	if req.Native && (len(req.State) < 32 || len(req.State) > 128) {
		return "", time.Time{}, ErrInvalidLogin
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.cleanupAuthTestsLocked(now)
	if len(s.tests) >= maxAuthTests {
		return "", time.Time{}, ErrRateLimited
	}
	id, expiresAt := rand.Text(), now.Add(authTestTTL)
	s.tests[id] = &oidcTest{
		policy: req.Policy.Clone(), origin: req.Origin, basePath: req.BasePath,
		native: req.Native, nativeState: req.State, stage: oidcTestStageStart,
		status: AuthTestStatusPending, expiresAt: expiresAt,
	}
	return id, expiresAt, nil
}

// StartOIDCTest resolves provider metadata and redirects with PKCE and nonce proofs.
func (s *Store) StartOIDCTest(ctx context.Context, req OIDCTestStartRequest) (BrowserStart, error) {
	s.mu.Lock()
	now := time.Now()
	s.cleanupAuthTestsLocked(now)
	test, ok := s.tests[req.ID]
	if !ok || test.stage != oidcTestStageStart || req.Origin != test.origin || !now.Before(test.expiresAt) {
		s.mu.Unlock()
		return BrowserStart{}, ErrInvalidLogin
	}
	test.stage = oidcTestStageStarting
	policy := test.policy.Clone()
	origin, basePath, native, nativeState := test.origin, test.basePath, test.native, test.nativeState
	s.mu.Unlock()

	transaction := login{
		kind: loginKindTestCallback, policyID: policy.PolicyID(), testID: req.ID,
		origin: origin, basePath: basePath, native: native, nativeState: nativeState,
		expiresAt: time.Now().Add(authTestTTL),
	}
	start, err := s.beginOIDCLogin(ctx, policy, transaction)
	if err != nil {
		reason := AuthTestReasonAuthorizationFailed
		var failure *oidcStartFailure
		if errors.As(err, &failure) && failure.kind == oidcStartDiscovery {
			reason = AuthTestReasonDiscoveryFailed
		}
		s.finishOIDCTest(req.ID, AuthTestStatusFailed, reason, "")
		return BrowserStart{}, err
	}
	s.mu.Lock()
	if current, ok := s.tests[req.ID]; ok && current.stage == oidcTestStageStarting {
		current.stage = oidcTestStageCallback
	}
	s.mu.Unlock()

	return start, nil
}

// IsOIDCTest reports whether a callback state belongs to a pending OIDC test.
func (s *Store) IsOIDCTest(state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	transaction, ok := s.logins[state]
	return ok && transaction.kind == loginKindTestCallback && time.Now().Before(transaction.expiresAt)
}

// CompleteOIDCTest validates the provider result and never issues an application session.
func (s *Store) CompleteOIDCTest(ctx context.Context, req OIDCTestCallback) OIDCTestCompletion {
	s.mu.Lock()
	transaction, ok := s.logins[req.State]
	if ok {
		delete(s.logins, req.State)
	}
	if !ok || transaction.kind != loginKindTestCallback || !time.Now().Before(transaction.expiresAt) {
		s.mu.Unlock()
		return OIDCTestCompletion{}
	}
	test := s.tests[transaction.testID]
	if test == nil {
		s.mu.Unlock()
		return OIDCTestCompletion{
			TestID: transaction.testID, Native: transaction.native, NativeState: transaction.nativeState,
			Result: AuthTestResult{Status: AuthTestStatusFailed, Reason: AuthTestReasonAuthorizationFailed},
		}
	}
	policy := test.policy.Clone()
	native, nativeState := transaction.native, transaction.nativeState
	s.mu.Unlock()

	completion := OIDCTestCompletion{TestID: transaction.testID, Native: native, NativeState: nativeState}
	fail := func(reason AuthTestReason, subject string) OIDCTestCompletion {
		completion.Result = s.finishOIDCTest(transaction.testID, AuthTestStatusFailed, reason, subject)
		return completion
	}
	if req.Origin != transaction.origin || req.Code == "" || !equal(transaction.binding, Hash(req.Binding)) {
		return fail(AuthTestReasonAuthorizationFailed, "")
	}
	identity, failure := s.completeOIDCIdentity(ctx, policy, transaction, req.Code)
	if failure != nil {
		return fail(failure.authTestReason(), failure.subject)
	}
	completion.Result = s.finishOIDCTest(transaction.testID, AuthTestStatusSucceeded, "", identity.Subject)
	return completion
}

// OIDCTestResult returns the current safe status while the test transaction is retained.
func (s *Store) OIDCTestResult(id string) (OIDCTestStatus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.cleanupAuthTestsLocked(now)
	test, ok := s.tests[id]
	if !ok {
		return OIDCTestStatus{}, false
	}
	return OIDCTestStatus{AuthTestResult: test.result(), ExpiresAt: test.expiresAt}, true
}

// TestCloudflareAccess verifies the live edge assertion against a candidate policy.
func (s *Store) TestCloudflareAccess(ctx context.Context, policy settings.Auth, assertion string) AuthTestResult {
	if assertion == "" {
		return AuthTestResult{Status: AuthTestStatusFailed, Reason: AuthTestReasonAccessAssertionMissing}
	}
	verification, err := s.verifyAccess(ctx, AccessRequest{Policy: policy, Assertion: assertion})
	if err != nil {
		if errors.Is(err, storage.ErrInvalidSession) {
			return AuthTestResult{
				Status: AuthTestStatusFailed, Reason: verification.authTestReason(), Subject: verification.Subject,
			}
		}
		return AuthTestResult{Status: AuthTestStatusFailed, Reason: AuthTestReasonAccessAssertionInvalid}
	}
	return AuthTestResult{Status: AuthTestStatusSucceeded, Subject: verification.Session.Subject}
}

func (s *Store) finishOIDCTest(id string, status AuthTestStatus, reason AuthTestReason, subject string) AuthTestResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	test, ok := s.tests[id]
	if !ok {
		return AuthTestResult{Status: status, Reason: reason, Subject: subject}
	}
	test.stage, test.status, test.reason, test.subject = oidcTestStageComplete, status, reason, subject
	test.policy = settings.Auth{}
	test.origin, test.basePath, test.nativeState = "", "", ""
	return test.result()
}

func (s *Store) cleanupAuthTestsLocked(now time.Time) {
	for id, test := range s.tests {
		if !now.Before(test.expiresAt) {
			delete(s.tests, id)
		}
	}
	for id, transaction := range s.logins {
		if !now.Before(transaction.expiresAt) {
			delete(s.logins, id)
		}
	}
}

func (t *oidcTest) result() AuthTestResult {
	return AuthTestResult{Status: t.status, Reason: t.reason, Subject: t.subject}
}

func (f *oidcIdentityFailure) authTestReason() AuthTestReason {
	switch f.kind {
	case oidcFailureDiscovery:
		return AuthTestReasonDiscoveryFailed
	case oidcFailureTokenExchange:
		return AuthTestReasonTokenExchangeFailed
	case oidcFailureSubjectNotAllowed:
		return AuthTestReasonSubjectNotAllowed
	default:
		return AuthTestReasonTokenInvalid
	}
}

func (v accessVerification) authTestReason() AuthTestReason {
	switch v.Failure {
	case accessFailureMissing:
		return AuthTestReasonAccessAssertionMissing
	case accessFailureSubjectNotAllowed:
		return AuthTestReasonSubjectNotAllowed
	default:
		return AuthTestReasonAccessAssertionInvalid
	}
}
