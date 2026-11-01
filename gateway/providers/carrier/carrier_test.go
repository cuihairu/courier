package carrier

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

func TestListCreatedAtDescending(t *testing.T) {
	base := time.Now()
	n := 0
	clock := func() time.Time {
		n++
		return base.Add(time.Duration(n) * time.Second)
	}
	p := New(Options{RequireAuth: authStub, Clock: clock})
	for _, title := range []string{"old", "middle", "new"} {
		if _, err := p.Publish("", title, "body", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	code, body := get(t, p, "/v1/mail/")
	if code != 200 {
		t.Fatalf("list=%d %s", code, body)
	}
	var resp struct {
		Data struct {
			Items []struct {
				Title     string `json:"title"`
				CreatedAt string `json:"createdAt"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
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

func TestMailIDUUIDv7(t *testing.T) {
	p := newProvider()
	m, err := p.Publish("", "hi", "body", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimPrefix(m.ID, "mail_")
	if len(id) != 36 || strings.Count(id, "-") != 4 {
		t.Fatalf("ID 非 UUIDv7 形状: %q", m.ID)
	}
	if id[14] != '7' {
		t.Fatalf("ID version nibble = %q, 期望 7", id[14])
	}
	if id[19] != '8' && id[19] != '9' && id[19] != 'a' && id[19] != 'b' {
		t.Fatalf("ID variant nibble = %q, 期望 8/9/a/b", id[19])
	}
}

func TestListCursorPagination(t *testing.T) {
	base := time.Now()
	n := 0
	clock := func() time.Time {
		n++
		return base.Add(time.Duration(n) * time.Second)
	}
	p := New(Options{RequireAuth: authStub, Clock: clock})
	for _, title := range []string{"a", "b", "c", "d", "e"} {
		if _, err := p.Publish("", title, "body", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	var all []string
	cursor := ""
	pages := 0
	for {
		path := "/v1/mail/?limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		code, body := get(t, p, path)
		if code != 200 {
			t.Fatalf("page=%d %s", code, body)
		}
		var resp struct {
			Data struct {
				Items []struct {
					Title string `json:"title"`
				} `json:"items"`
				NextCursor string `json:"nextCursor"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatal(err)
		}
		for _, it := range resp.Data.Items {
			all = append(all, it.Title)
		}
		cursor = resp.Data.NextCursor
		pages++
		if cursor == "" {
			break
		}
		if pages > 10 {
			t.Fatal("翻页未终止")
		}
	}
	if got, want := strings.Join(all, ","), "e,d,c,b,a"; got != want {
		t.Fatalf("全序=%q, want %q", got, want)
	}
	if pages != 3 {
		t.Fatalf("页数=%d, want 3", pages)
	}
	// 非法 cursor → 400 COMMON_INVALID_ARGUMENT。
	if code, _ := get(t, p, "/v1/mail/?cursor=not-a-cursor"); code != 400 {
		t.Fatalf("非法 cursor 应 400, got %d", code)
	}
}
