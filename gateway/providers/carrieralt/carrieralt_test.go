package carrieralt

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

func TestPublishListClaim(t *testing.T) {
	p := New(Options{RequireAuth: authStub})
	m, err := p.Publish("acct1", "hi", "body", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/mail/", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), m.ID) {
		t.Fatalf("list=%d %s", rec.Code, rec.Body.String())
	}
	req2 := httptest.NewRequest(http.MethodPost, "/v1/mail/"+m.ID+"/claim", strings.NewReader("{}"))
	rec2 := httptest.NewRecorder()
	p.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("claim=%d %s", rec2.Code, rec2.Body.String())
	}
}
