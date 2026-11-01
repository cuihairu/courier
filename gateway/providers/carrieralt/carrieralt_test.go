package carrieralt

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestListCreatedAtDescending(t *testing.T) {
	base := time.Now()
	n := 0
	clock := func() time.Time {
		n++
		return base.Add(time.Duration(n) * time.Second)
	}
	p := New(Options{RequireAuth: authStub, Clock: clock})
	for _, title := range []string{"old", "middle", "new"} {
		if _, err := p.Publish("acct1", title, "body", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/mail/", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("list=%d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Items []struct {
				Title     string `json:"title"`
				CreatedAt string `json:"createdAt"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(rec.Body.String()), &resp); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"new", "middle", "old"} {
		if resp.Data.Items[i].Title != want {
			t.Fatalf("items[%d].title=%q, want %q", i, resp.Data.Items[i].Title, want)
		}
	}
	for i := 1; i < len(resp.Data.Items); i++ {
		if resp.Data.Items[i-1].CreatedAt < resp.Data.Items[i].CreatedAt {
			t.Fatalf("createdAt 未倒序: %q !> %q", resp.Data.Items[i-1].CreatedAt, resp.Data.Items[i].CreatedAt)
		}
	}
}
