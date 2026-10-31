package carrier

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cuihairu/courier/gateway/auth"
)

func authStub(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(),
			auth.Identity{AccountID: "acct1", SessionID: "ses_1"})))
	})
}

func newProvider() *Provider { return New(Options{RequireAuth: authStub}) }

func get(t *testing.T, p *Provider, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func post(t *testing.T, p *Provider, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestPublishAndList(t *testing.T) {
	p := newProvider()
	m, err := p.Publish("", "hi", "body", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	code, body := get(t, p, "/v1/mail/")
	if code != 200 || !strings.Contains(body, m.ID) {
		t.Fatalf("list=%d %s", code, body)
	}
}

func TestClaimIdempotent(t *testing.T) {
	p := newProvider()
	m, _ := p.Publish("acct1", "hi", "body", []Attachment{{ID: "a", Type: "coin", Amount: 100}}, nil)
	if code, _ := post(t, p, "/v1/mail/"+m.ID+"/claim"); code != 200 {
		t.Fatal("first claim")
	}
	if code, body := post(t, p, "/v1/mail/"+m.ID+"/claim"); code != 200 || !strings.Contains(body, `"repeated"`) && !strings.Contains(body, `"id"`) {
		t.Fatalf("repeat claim=%d %s", code, body)
	}
}

func TestMailNotFound(t *testing.T) {
	p := newProvider()
	if code, _ := get(t, p, "/v1/mail/nope"); code != 404 {
		t.Fatal("want 404")
	}
}
