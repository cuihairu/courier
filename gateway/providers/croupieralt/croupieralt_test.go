// croupier-alt 单元测试:契约面(建单/追加/回复/关单/FAQ/附件/举报/处罚/推送)
// 与 croupier 同源断言 + 异构面(写时属主索引、键集游标分页、索引重排、锚点失效
// 空页、非法游标 400)+ 两实现分页等价显式证明(与 croupier 同脚本同页内容)。
package croupieralt

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/providers/croupier"
)

// steppingClock 每 tick 前进 2ms(毫秒键集序与读时排序可见序一致的前提)。
func steppingClock() func() time.Time {
	seq := 0
	return func() time.Time {
		seq++
		return time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC).Add(time.Duration(seq*2) * time.Millisecond)
	}
}

func harness(t *testing.T, mutate func(*Options)) (*SupportProvider, *string) {
	t.Helper()
	cur := new(string)
	*cur = "acc_1"
	opts := Options{
		Clock: steppingClock(),
		RequireAuth: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := auth.WithIdentity(r.Context(), auth.Identity{AccountID: *cur, SessionID: "ses_1"})
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		},
	}
	if mutate != nil {
		mutate(&opts)
	}
	return New(opts), cur
}

func call(t *testing.T, p *SupportProvider, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequest(method, path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	return rec
}

type envelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

// createTicket 便利封装:建单并返回 id。
func createTicket(t *testing.T, p *SupportProvider, title string) string {
	t.Helper()
	rec := call(t, p, http.MethodPost, "/v1/support/tickets", `{"title":"`+title+`","body":"body text"}`)
	if rec.Code != 200 {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(env.Data, &out); err != nil || out.ID == "" {
		t.Fatalf("decode: %s", rec.Body.String())
	}
	return out.ID
}

func TestMeta(t *testing.T) {
	p, _ := harness(t, nil)
	if p.Name() != DefaultName {
		t.Fatalf("name = %s", p.Name())
	}
	if string(p.Capability()) != "support" {
		t.Fatalf("capability = %v", p.Capability())
	}
	if err := p.HealthCheck(t.Context()); err != nil {
		t.Fatalf("health: %v", err)
	}
}

func TestFailClosedWithoutAuth(t *testing.T) {
	p := New(Options{})
	rec := call(t, p, http.MethodGet, "/v1/support/tickets", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth = %d, want 401", rec.Code)
	}
}

func TestCreateTicket_ServerSideValidation(t *testing.T) {
	p, _ := harness(t, nil)
	// title 超限 / body 超限 / 空body
	for _, tc := range []struct {
		name string
		body string
	}{
		{"title 超限", `{"title":"` + strings.Repeat("长", 121) + `","body":"b"}`},
		{"title 空", `{"title":"","body":"b"}`},
		{"body 超 4000", `{"title":"t","body":"` + strings.Repeat("字", 4001) + `"}`},
		{"body 空", `{"title":"t","body":""}`},
	} {
		rec := call(t, p, http.MethodPost, "/v1/support/tickets", tc.body)
		if rec.Code != 400 {
			t.Fatalf("%s = %d, want 400: %s", tc.name, rec.Code, rec.Body.String())
		}
	}
	// 缺省 category = OTHER;合法创建。
	rec := call(t, p, http.MethodPost, "/v1/support/tickets", `{"title":"t","body":"b"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"OPEN"`) ||
		!strings.Contains(rec.Body.String(), `"category":"OTHER"`) {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
}

func TestTicketOwnership_404NoLeak(t *testing.T) {
	p, cur := harness(t, nil)
	id := createTicket(t, p, "mine")
	*cur = "acc_2"
	if rec := call(t, p, http.MethodGet, "/v1/support/tickets/"+id, ""); rec.Code != 404 ||
		!strings.Contains(rec.Body.String(), "SUPPORT_TICKET_NOT_FOUND") {
		t.Fatalf("other detail = %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(t, p, http.MethodPost, "/v1/support/tickets/"+id+"/messages", `{"body":"hi"}`); rec.Code != 404 {
		t.Fatalf("other append = %d", rec.Code)
	}
	if rec := call(t, p, http.MethodGet, "/v1/support/tickets", ""); strings.Contains(rec.Body.String(), id) {
		t.Fatalf("other list leaks: %s", rec.Body.String())
	}
}

func TestPlayerAppend_DoesNotChangeStatus_ClosedRejected(t *testing.T) {
	p, _ := harness(t, nil)
	id := createTicket(t, p, "t")
	// 追加:状态不变(契约:坐席回复才转 REPLIED)。
	if rec := call(t, p, http.MethodPost, "/v1/support/tickets/"+id+"/messages", `{"body":"second"}`); rec.Code != 200 {
		t.Fatalf("append = %d %s", rec.Code, rec.Body.String())
	}
	rec := call(t, p, http.MethodGet, "/v1/support/tickets/"+id, "")
	if !strings.Contains(rec.Body.String(), `"status":"OPEN"`) ||
		!strings.Contains(rec.Body.String(), `"body":"second"`) {
		t.Fatalf("detail = %s", rec.Body.String())
	}
	// 关单后玩家追加 → 409 SUPPORT_TICKET_CLOSED。
	if err := p.AgentClose(id); err != nil {
		t.Fatalf("close: %v", err)
	}
	rec = call(t, p, http.MethodPost, "/v1/support/tickets/"+id+"/messages", `{"body":"late"}`)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "SUPPORT_TICKET_CLOSED") {
		t.Fatalf("append closed = %d %s", rec.Code, rec.Body.String())
	}
}

func TestAgentReply_TransitionsReplied_Notifies(t *testing.T) {
	var events []string
	p, _ := harness(t, func(o *Options) {
		o.Notify = func(typ string, data any) { events = append(events, typ) }
	})
	id := createTicket(t, p, "t")
	if err := p.AgentReply(id, "agent here"); err != nil {
		t.Fatalf("reply: %v", err)
	}
	rec := call(t, p, http.MethodGet, "/v1/support/tickets/"+id, "")
	if !strings.Contains(rec.Body.String(), `"status":"REPLIED"`) ||
		!strings.Contains(rec.Body.String(), `"senderType":"AGENT"`) {
		t.Fatalf("detail = %s", rec.Body.String())
	}
	if len(events) != 1 || events[0] != EventTicketReplied {
		t.Fatalf("events = %v", events)
	}
	// 不存在的单:agentAppend → not found。
	if err := p.AgentReply("tkt_nope", "x"); err == nil {
		t.Fatal("reply missing should error")
	}
}

// TestList_KeysetPagination 异构面核心:写时属主索引 + 键集游标翻页。
func TestList_KeysetPagination(t *testing.T) {
	p, _ := harness(t, nil)
	for _, title := range []string{"a", "b", "c", "d", "e"} {
		createTicket(t, p, title)
	}
	// 第一页 limit=2:updatedAt 倒序 = e,d。
	rec := call(t, p, http.MethodGet, "/v1/support/tickets?limit=2", "")
	if rec.Code != 200 {
		t.Fatalf("page1 = %d %s", rec.Code, rec.Body.String())
	}
	var page1 struct {
		Data struct {
			Items []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"items"`
			NextCursor string `json:"nextCursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page1); err != nil {
		t.Fatal(err)
	}
	if len(page1.Data.Items) != 2 || page1.Data.Items[0].Title != "e" || page1.Data.Items[1].Title != "d" {
		t.Fatalf("page1 = %+v", page1.Data)
	}
	if page1.Data.NextCursor == "" {
		t.Fatal("page1 应有 nextCursor")
	}
	// 游标翻页直到末页:全序应为 e,d,c,b,a;无重复;末页 nextCursor 空。
	seen := map[string]bool{page1.Data.Items[0].ID: true, page1.Data.Items[1].ID: true}
	order := []string{page1.Data.Items[0].Title, page1.Data.Items[1].Title}
	cursor := page1.Data.NextCursor
	for i := 0; i < 5 && cursor != ""; i++ {
		rec = call(t, p, http.MethodGet, "/v1/support/tickets?limit=2&cursor="+cursor, "")
		if rec.Code != 200 {
			t.Fatalf("page = %d %s", rec.Code, rec.Body.String())
		}
		var page struct {
			Data struct {
				Items []struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"items"`
				NextCursor string `json:"nextCursor"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		for _, it := range page.Data.Items {
			if seen[it.ID] {
				t.Fatalf("重复条目: %s (%v)", it.ID, it.Title)
			}
			seen[it.ID] = true
			order = append(order, it.Title)
		}
		cursor = page.Data.NextCursor
	}
	if cursor != "" {
		t.Fatalf("未在末页终止: cursor=%s", cursor)
	}
	if len(seen) != 5 {
		t.Fatalf("翻页覆盖 %d 条, want 5", len(seen))
	}
	if strings.Join(order, ",") != "e,d,c,b,a" {
		t.Fatalf("翻页全序 = %v", order)
	}
}

// TestList_KeysetReposition 异构面:updatedAt 变更 → 索引即时重排(回复最旧单
// 后它跃居最新)。
func TestList_KeysetReposition(t *testing.T) {
	p, _ := harness(t, nil)
	a := createTicket(t, p, "a")
	createTicket(t, p, "b")
	// 当前序:b,a。回复 a → a updatedAt 最新 → 序:a,b。
	if err := p.AgentReply(a, "fresh"); err != nil {
		t.Fatalf("reply: %v", err)
	}
	rec := call(t, p, http.MethodGet, "/v1/support/tickets", "")
	var page struct {
		Data struct {
			Items []struct {
				Title string `json:"title"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data.Items) != 2 || page.Data.Items[0].Title != "a" || page.Data.Items[1].Title != "b" {
		t.Fatalf("repositioned = %+v", page.Data.Items)
	}
}

func TestList_CursorAndLimitValidation(t *testing.T) {
	p, _ := harness(t, nil)
	createTicket(t, p, "t")
	// 非法游标:非锚点格式 → 400(契约:非法 cursor → COMMON_INVALID_ARGUMENT)。
	for _, c := range []string{"abc", "-1", "x", "|tkt_x", "5|", "922337203685477580712345|x"} {
		rec := call(t, p, http.MethodGet, "/v1/support/tickets?cursor="+c, "")
		if rec.Code != 400 {
			t.Fatalf("cursor %q = %d, want 400", c, rec.Code)
		}
	}
	// 锚点格式但条目未知 → 200 空页(与读时偏移越界同形)。
	rec := call(t, p, http.MethodGet, "/v1/support/tickets?cursor=1699999999999%7Ctkt_unknown", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("stale anchor = %d %s", rec.Code, rec.Body.String())
	}
	// limit 边界:0/101/非数字 → 400;缺省 20。
	for _, l := range []string{"0", "101", "x"} {
		if rec := call(t, p, http.MethodGet, "/v1/support/tickets?limit="+l, ""); rec.Code != 400 {
			t.Fatalf("limit %s = %d, want 400", l, rec.Code)
		}
	}
}

func TestContextValidation(t *testing.T) {
	p, cur := harness(t, nil)
	_ = cur
	cases := []struct {
		name string
		ctx  string
	}{
		{"11 键", `{"k1":1,"k2":2,"k3":3,"k4":4,"k5":5,"k6":6,"k7":7,"k8":8,"k9":9,"k10":10,"k11":11}`},
		{"非法键字符", `{"bad-key!":"v"}`},
		{"嵌套值", `{"k":{"a":1}}`},
	}
	for _, tc := range cases {
		rec := call(t, p, http.MethodPost, "/v1/support/tickets", `{"title":"t","body":"b","context":`+tc.ctx+`}`)
		if rec.Code != 400 {
			t.Fatalf("%s = %d, want 400: %s", tc.name, rec.Code, rec.Body.String())
		}
	}
	// 合法:原始值 + 零解释回显。
	rec := call(t, p, http.MethodPost, "/v1/support/tickets",
		`{"title":"t","body":"b","context":{"level":3,"vip":true,"plan":"gold"}}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"level":3`) ||
		!strings.Contains(rec.Body.String(), `"vip":true`) {
		t.Fatalf("valid context = %d %s", rec.Code, rec.Body.String())
	}
}

func TestFAQ_MatchAndValidation(t *testing.T) {
	p, cur := harness(t, nil)
	_ = cur
	p.AddFAQ("如何充值", "前往商店", []string{"充值", "recharge"})
	p.AddFAQ("如何退款", "联系客服", []string{"refund"})
	// keyword 命中问题。
	if rec := call(t, p, http.MethodGet, "/v1/support/faq?keyword=%E5%85%85%E5%80%BC", ""); rec.Code != 200 ||
		!strings.Contains(rec.Body.String(), "如何充值") {
		t.Fatalf("faq hit = %d %s", rec.Code, rec.Body.String())
	}
	// keyword 命中 keywords(大小写不敏感)。
	if rec := call(t, p, http.MethodGet, "/v1/support/faq?keyword=REFUND", ""); !strings.Contains(rec.Body.String(), "如何退款") {
		t.Fatalf("faq keyword = %s", rec.Body.String())
	}
	// 空 keyword 参数 → 400;无 keyword → 全量。
	if rec := call(t, p, http.MethodGet, "/v1/support/faq?keyword=", ""); rec.Code != 400 {
		t.Fatalf("empty keyword = %d", rec.Code)
	}
	if rec := call(t, p, http.MethodGet, "/v1/support/faq", ""); !strings.Contains(rec.Body.String(), "如何退款") {
		t.Fatalf("faq all = %s", rec.Body.String())
	}
	// limit 边界 0/51 → 400。
	if rec := call(t, p, http.MethodGet, "/v1/support/faq?limit=51", ""); rec.Code != 400 {
		t.Fatalf("faq limit = %d", rec.Code)
	}
}

// fakeSigner 测试存储面。
type fakeSigner struct{}

func (fakeSigner) SignPresign(base string, ttl time.Duration) (string, map[string]string) {
	return "https://storage.example/" + base + "?sig=x", map[string]string{"x-amz-meta": "courier"}
}

func TestAttachments(t *testing.T) {
	// 未配置 Signer → 501 COMMON_CAPABILITY_DISABLED(fail-closed)。
	p0, _ := harness(t, nil)
	id0 := createTicket(t, p0, "t")
	rec := call(t, p0, http.MethodPost, "/v1/support/tickets/"+id0+"/attachments",
		`{"fileName":"log.txt","contentType":"text/plain","sizeBytes":10}`)
	if rec.Code != 501 || !strings.Contains(rec.Body.String(), "COMMON_CAPABILITY_DISABLED") {
		t.Fatalf("no signer = %d %s", rec.Code, rec.Body.String())
	}

	// 配置 Signer:合法登记 → 直传 URL + 详情登记行。
	p, _ := harness(t, func(o *Options) { o.AttachmentSigner = fakeSigner{} })
	id := createTicket(t, p, "t")
	rec = call(t, p, http.MethodPost, "/v1/support/tickets/"+id+"/attachments",
		`{"fileName":"log.txt","contentType":"text/plain","sizeBytes":10,"kind":"LOG"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"method":"PUT"`) ||
		!strings.Contains(rec.Body.String(), "support/uploads/acc_1/") {
		t.Fatalf("upload = %d %s", rec.Code, rec.Body.String())
	}
	// 校验:非法 contentType / sizeBytes 0 / 非法 kind。
	for _, body := range []string{
		`{"fileName":"a","contentType":"exe/x","sizeBytes":1}`,
		`{"fileName":"a","contentType":"text/plain","sizeBytes":0}`,
		`{"fileName":"a","contentType":"text/plain","sizeBytes":1,"kind":"VIDEO"}`,
	} {
		if rec := call(t, p, http.MethodPost, "/v1/support/tickets/"+id+"/attachments", body); rec.Code != 400 {
			t.Fatalf("attach validate = %d %s", rec.Code, rec.Body.String())
		}
	}
	// 计数上限(缺省 3):再补 2 条后第 4 条 → 409 SUPPORT_ATTACHMENT_LIMIT。
	for range 2 {
		call(t, p, http.MethodPost, "/v1/support/tickets/"+id+"/attachments",
			`{"fileName":"a","contentType":"text/plain","sizeBytes":1}`)
	}
	rec = call(t, p, http.MethodPost, "/v1/support/tickets/"+id+"/attachments",
		`{"fileName":"a","contentType":"text/plain","sizeBytes":1}`)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "SUPPORT_ATTACHMENT_LIMIT") {
		t.Fatalf("limit = %d %s", rec.Code, rec.Body.String())
	}
	// 详情 attachments[] 追加序 + downloadUrl。
	rec = call(t, p, http.MethodGet, "/v1/support/tickets/"+id, "")
	if !strings.Contains(rec.Body.String(), `"attachments":[`) ||
		!strings.Contains(rec.Body.String(), `"downloadUrl":"https://storage.example/`) {
		t.Fatalf("detail attachments = %s", rec.Body.String())
	}
}

// fakePenaltyStore 测试投影源。
type fakePenaltyStore struct {
	items   []PenaltyItem
	appeals map[string]bool
}

func (s *fakePenaltyStore) List(owner string) []PenaltyItem { return s.items }
func (s *fakePenaltyStore) Get(owner, id string) (PenaltyItem, bool) {
	for _, it := range s.items {
		if it.ID == id {
			return it, true
		}
	}
	return PenaltyItem{}, false
}
func (s *fakePenaltyStore) Appeal(owner, id string) (PenaltyItem, error) {
	if s.appeals[id] {
		return PenaltyItem{}, ErrAppealExists
	}
	s.appeals[id] = true
	it, ok := s.Get(owner, id)
	if !ok {
		return PenaltyItem{}, errNotFound
	}
	it.Appeal = &AppealItem{State: "PENDING", SubmittedAt: time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)}
	return it, nil
}

func TestReports_Validation(t *testing.T) {
	p, cur := harness(t, nil)
	_ = cur
	// 非法 targetType / reason / 空 targetId。
	for _, body := range []string{
		`{"targetType":"GAME","targetId":"x","reason":"CHEATING"}`,
		`{"targetType":"PLAYER","targetId":"","reason":"CHEATING"}`,
		`{"targetType":"PLAYER","targetId":"x","reason":"REVENGE"}`,
	} {
		if rec := call(t, p, http.MethodPost, "/v1/support/reports", body); rec.Code != 400 {
			t.Fatalf("report validate = %d %s", rec.Code, rec.Body.String())
		}
	}
	// 合法受理:仅回 id(进展不向举报人反馈)。
	rec := call(t, p, http.MethodPost, "/v1/support/reports",
		`{"targetType":"PLAYER","targetId":"acc_9","reason":"CHEATING","detail":"sparkle"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"id":"rpt_`) {
		t.Fatalf("report = %d %s", rec.Code, rec.Body.String())
	}
}

func TestPenalties_ProjectionAndAppeal(t *testing.T) {
	base := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	store := &fakePenaltyStore{appeals: map[string]bool{}, items: []PenaltyItem{
		{ID: "pen_b", Type: "MUTE", Reason: "刷屏", State: "ACTIVE", IssuedAt: base},
		{ID: "pen_a", Type: "BAN", Reason: "外挂", State: "ACTIVE",
			IssuedAt: base.Add(4 * time.Minute)}, // 更晚签发 → 列表在前
	}}
	p, _ := harness(t, func(o *Options) { o.PenaltyStore = store })

	// 未配置投影源 → 501(fail-closed)。
	p0, _ := harness(t, nil)
	if rec := call(t, p0, http.MethodGet, "/v1/support/penalties", ""); rec.Code != 501 {
		t.Fatalf("no store = %d", rec.Code)
	}

	// 列表:issuedAt 倒序 + 键集分页。
	rec := call(t, p, http.MethodGet, "/v1/support/penalties?limit=1", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"id":"pen_a"`) {
		t.Fatalf("penalties page1 = %d %s", rec.Code, rec.Body.String())
	}
	var page1 struct {
		Data struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor string            `json:"nextCursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page1); err != nil {
		t.Fatal(err)
	}
	if page1.Data.NextCursor == "" {
		t.Fatal("page1 应有 nextCursor")
	}
	rec = call(t, p, http.MethodGet, "/v1/support/penalties?limit=1&cursor="+page1.Data.NextCursor, "")
	if !strings.Contains(rec.Body.String(), `"id":"pen_b"`) || strings.Contains(rec.Body.String(), `"id":"pen_a"`) {
		t.Fatalf("page2 = %s", rec.Body.String())
	}
	// 非本人/未知处罚 → 404(不泄露存在性)。
	if rec := call(t, p, http.MethodGet, "/v1/support/penalties/pen_zzz", ""); rec.Code != 404 ||
		!strings.Contains(rec.Body.String(), "SUPPORT_PENALTY_NOT_FOUND") {
		t.Fatalf("unknown penalty = %d %s", rec.Code, rec.Body.String())
	}
	// 申诉:PENDING → 二次申诉 409 SUPPORT_APPEAL_EXISTS。
	if rec := call(t, p, http.MethodPost, "/v1/support/penalties/pen_b/appeal", ""); rec.Code != 200 ||
		!strings.Contains(rec.Body.String(), `"state":"PENDING"`) {
		t.Fatalf("appeal = %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(t, p, http.MethodPost, "/v1/support/penalties/pen_b/appeal", ""); rec.Code != 409 ||
		!strings.Contains(rec.Body.String(), "SUPPORT_APPEAL_EXISTS") {
		t.Fatalf("re-appeal = %d %s", rec.Code, rec.Body.String())
	}
}

func TestTicketRateLimit(t *testing.T) {
	p, _ := harness(t, func(o *Options) {
		o.TicketRatePerMinute = 2
		o.TicketRateBurst = 2
	})
	for i := range 2 {
		if rec := call(t, p, http.MethodPost, "/v1/support/tickets", `{"title":"t","body":"b"}`); rec.Code != 200 {
			t.Fatalf("create %d = %d", i, rec.Code)
		}
	}
	rec := call(t, p, http.MethodPost, "/v1/support/tickets", `{"title":"t","body":"b"}`)
	if rec.Code != 429 || !strings.Contains(rec.Body.String(), `"code":"RATE_LIMITED"`) {
		t.Fatalf("rate limited = %d %s", rec.Code, rec.Body.String())
	}
}

// TestPaginationEquivalenceWithCroupier 等价证明:同一建单脚本对 croupier(读时
// 扫描 + 偏移游标)与本实现(写时索引 + 键集游标)各跑一遍,逐页比对 items 内容
// (按标题,UUID 各自生成)——「键集页 = 偏移页」的可执行证明。
func TestPaginationEquivalenceWithCroupier(t *testing.T) {
	clock := steppingClock()
	def := croupier.New(croupier.Options{Clock: clock, RequireAuth: func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.WithIdentity(r.Context(), auth.Identity{AccountID: "acc_1", SessionID: "ses_1"})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}})
	alt := New(Options{Clock: steppingClock(), RequireAuth: defRequire(t)})

	for _, title := range []string{"t1", "t2", "t3", "t4", "t5"} {
		for _, pr := range []http.Handler{def, alt} {
			rec := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodPost, "/v1/support/tickets",
				strings.NewReader(`{"title":"`+title+`","body":"b"}`))
			pr.ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
			}
		}
	}

	// 各自翻页(limit=2)收集全序标题;游标各自原样回传(不透明)。
	collectTitles := func(p http.Handler) []string {
		t.Helper()
		var out []string
		cursor := ""
		for i := 0; i < 10; i++ {
			path := "/v1/support/tickets?limit=2"
			if cursor != "" {
				path += "&cursor=" + cursor
			}
			rec := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, path, nil)
			p.ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
			}
			var page struct {
				Data struct {
					Items []struct {
						Title string `json:"title"`
					} `json:"items"`
					NextCursor string `json:"nextCursor"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			for _, it := range page.Data.Items {
				out = append(out, it.Title)
			}
			if page.Data.NextCursor == "" {
				break
			}
			cursor = page.Data.NextCursor
		}
		return out
	}
	defOrder := collectTitles(def)
	altOrder := collectTitles(alt)
	if strings.Join(defOrder, ",") != strings.Join(altOrder, ",") {
		t.Fatalf("分页全序分叉: croupier=%v croupieralt=%v", defOrder, altOrder)
	}
	if strings.Join(defOrder, ",") != "t5,t4,t3,t2,t1" {
		t.Fatalf("全序 = %v", defOrder)
	}
}

func defRequire(t *testing.T) func(http.Handler) http.Handler {
	t.Helper()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.WithIdentity(r.Context(), auth.Identity{AccountID: "acc_1", SessionID: "ses_1"})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
