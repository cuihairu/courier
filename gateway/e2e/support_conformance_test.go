// 批次 73 客服域第二实现一致性 e2e(批次 18/64/67/68/69/70/71/72 先例同构):
// 同一 wire 场景对 croupier(读时组织:全表扫描+属主过滤+读时排序,偏移游标)
// 与 croupier-alt(写时组织:属主有序索引即时重排,键集游标)各跑一遍、同一断言
// ——「客服 Provider 可换」有行为证明而非仅接口冻结。两实现异构面不在本文件
// 断言,由各自单测承载(croupier 由 e2e/m2 承载 / providers/croupieralt)。
package e2e

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/providers/account"
	"github.com/cuihairu/courier/gateway/providers/croupier"
	"github.com/cuihairu/courier/gateway/providers/croupieralt"
	"github.com/cuihairu/courier/gateway/routing"
)

// supportHarness 装配 account + 一个客服实现(经路由,与 main 同构)。
type supportHarness struct {
	srv  *httptest.Server
	coup *croupier.SupportProvider
	alt  *croupieralt.SupportProvider
}

// steppingClock 每 tick 前进 1ms:保证工单 updatedAt 毫秒互异——同毫秒内的次序
// 是契约未指定行为(两实现各自取稳定全序,彼此值不可比),契约链需要一个确定的
// 序键才能对两实现断言同一页序。
func steppingClock() func() time.Time {
	seq := 0
	return func() time.Time {
		seq++
		return time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC).Add(time.Duration(seq) * time.Millisecond)
	}
}

func newSupportHarness(t *testing.T, useAlt bool) *supportHarness {
	t.Helper()
	reg := providers.NewRegistry()
	acc := account.New(account.Options{Iterations: 1000})
	reqAuth := auth.RequireAuth(acc.Verifier())

	var supP providers.Handler
	var coup *croupier.SupportProvider
	var alt *croupieralt.SupportProvider
	if useAlt {
		alt = croupieralt.New(croupieralt.Options{
			Clock:               steppingClock(),
			RequireAuth:         reqAuth,
			TicketRatePerMinute: 100,
			TicketRateBurst:     100,
			AttachmentSigner:    presignSigner{},
			PenaltyStore:        &altPenaltyStore{appeals: map[string]bool{}},
		})
		supP = alt
	} else {
		coup = croupier.New(croupier.Options{
			Clock:               steppingClock(),
			RequireAuth:         reqAuth,
			TicketRatePerMinute: 100,
			TicketRateBurst:     100,
			AttachmentSigner:    presignSigner{},
			PenaltyStore:        &defPenaltyStore{appeals: map[string]bool{}},
		})
		supP = coup
	}
	if err := reg.Register(supP); err != nil {
		t.Fatalf("register support provider: %v", err)
	}
	if err := reg.Register(acc); err != nil {
		t.Fatalf("register acc: %v", err)
	}
	cfg := providers.Config{
		providers.CapIdentity: {Primary: account.DefaultName},
		providers.CapSupport:  {Primary: supP.Name()},
	}
	chain := middleware.Chain(middleware.NewRateLimiter(120, 60))
	srv := httptest.NewServer(routing.New(reg, cfg, chain))
	t.Cleanup(srv.Close)
	return &supportHarness{srv: srv, coup: coup, alt: alt}
}

// presignSigner 直传预签名替身(两实现同形;契约只看 URL 形状与 method)。
type presignSigner struct{}

func (presignSigner) SignPresign(base string, ttl time.Duration) (string, map[string]string) {
	return "https://store.example/" + base + "?sig=x", map[string]string{"x-amz-meta": "courier"}
}

// defPenaltyStore / altPenaltyStore 投影源替身:两实现同形状(接口不同类型,
// 同语义);按 owner 确定性给出两条处罚(pen_b 更晚签发 → 列表在前)。
// 处罚判定与执行全在管理面,测试只验网关的只读投影与申诉入口。
var penaltyIssuedAt = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

type defPenaltyStore struct{ appeals map[string]bool }

func (s *defPenaltyStore) List(string) []croupier.PenaltyItem {
	return []croupier.PenaltyItem{
		{ID: "pen_a", Type: "MUTE", Reason: "刷屏", State: "ACTIVE", IssuedAt: penaltyIssuedAt},
		{ID: "pen_b", Type: "BAN", Reason: "外挂", State: "ACTIVE",
			IssuedAt: penaltyIssuedAt.Add(time.Hour)},
	}
}

func (s *defPenaltyStore) Get(_ string, id string) (croupier.PenaltyItem, bool) {
	for _, it := range s.List("") {
		if it.ID == id {
			return it, true
		}
	}
	return croupier.PenaltyItem{}, false
}

func (s *defPenaltyStore) Appeal(_ string, id string) (croupier.PenaltyItem, error) {
	if s.appeals[id] {
		return croupier.PenaltyItem{}, croupier.ErrAppealExists
	}
	s.appeals[id] = true
	it, ok := s.Get("", id)
	if !ok {
		return croupier.PenaltyItem{}, errSupportPenaltyUnknown
	}
	it.Appeal = &croupier.AppealItem{State: "PENDING", SubmittedAt: penaltyIssuedAt}
	return it, nil
}

type altPenaltyStore struct{ appeals map[string]bool }

func (s *altPenaltyStore) List(string) []croupieralt.PenaltyItem {
	return []croupieralt.PenaltyItem{
		{ID: "pen_a", Type: "MUTE", Reason: "刷屏", State: "ACTIVE", IssuedAt: penaltyIssuedAt},
		{ID: "pen_b", Type: "BAN", Reason: "外挂", State: "ACTIVE",
			IssuedAt: penaltyIssuedAt.Add(time.Hour)},
	}
}

func (s *altPenaltyStore) Get(_ string, id string) (croupieralt.PenaltyItem, bool) {
	for _, it := range s.List("") {
		if it.ID == id {
			return it, true
		}
	}
	return croupieralt.PenaltyItem{}, false
}

func (s *altPenaltyStore) Appeal(_ string, id string) (croupieralt.PenaltyItem, error) {
	if s.appeals[id] {
		return croupieralt.PenaltyItem{}, croupieralt.ErrAppealExists
	}
	s.appeals[id] = true
	it, ok := s.Get("", id)
	if !ok {
		return croupieralt.PenaltyItem{}, errSupportPenaltyUnknown
	}
	it.Appeal = &croupieralt.AppealItem{State: "PENDING", SubmittedAt: penaltyIssuedAt}
	return it, nil
}

// errSupportPenaltyUnknown 投影源侧「无此处罚」哨兵(网关归一为 404)。
var errSupportPenaltyUnknown = errors.New("penalty not found")

func (h *supportHarness) scopeHeaders(r *http.Request) {
	r.Header.Set("X-Courier-Game-Id", "game_demo")
	r.Header.Set("X-Courier-Env", "prod")
}

func (h *supportHarness) login(t *testing.T) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/identity/guest",
		strings.NewReader(`{"deviceId":"support-e2e","platform":"linux"}`))
	h.scopeHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("guest login: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Data.AccessToken == "" {
		t.Fatalf("login decode: status=%d err=%v", resp.StatusCode, err)
	}
	return out.Data.AccessToken
}

func (h *supportHarness) do(t *testing.T, method, path, token, body string) (int, string) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, rdr)
	h.scopeHeaders(req)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func (h *supportHarness) get(t *testing.T, path, token string) (int, string) {
	return h.do(t, http.MethodGet, path, token, "")
}

func (h *supportHarness) post(t *testing.T, path, token, body string) (int, string) {
	return h.do(t, http.MethodPost, path, token, body)
}

// ticketIDs 列表页标题序(逐页游标翻到底;游标两实现各自不透明,只原样回传)。
func (h *supportHarness) ticketIDs(t *testing.T, token string) []string {
	t.Helper()
	var out []string
	cursor := ""
	for i := 0; i < 20; i++ {
		path := "/v1/support/tickets?limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		code, body := h.get(t, path, token)
		if code != http.StatusOK {
			t.Fatalf("list = %d %s", code, body)
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
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatalf("decode: %v (%s)", err, body)
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

// agentReply 坐席回复(管理面入口;两实现同签名)。
func (h *supportHarness) agentReply(t *testing.T, useAlt bool, ticketID, body string) {
	t.Helper()
	if useAlt {
		if err := h.alt.AgentReply(ticketID, body); err != nil {
			t.Fatalf("agent reply: %v", err)
		}
		return
	}
	if err := h.coup.AgentReply(ticketID, body); err != nil {
		t.Fatalf("agent reply: %v", err)
	}
}

func (h *supportHarness) agentClose(t *testing.T, useAlt bool, ticketID string) {
	t.Helper()
	if useAlt {
		if err := h.alt.AgentClose(ticketID); err != nil {
			t.Fatalf("agent close: %v", err)
		}
		return
	}
	if err := h.coup.AgentClose(ticketID); err != nil {
		t.Fatalf("agent close: %v", err)
	}
}

func (h *supportHarness) addFAQ(t *testing.T, useAlt bool, question, answer string, keywords []string) {
	t.Helper()
	if useAlt {
		h.alt.AddFAQ(question, answer, keywords)
		return
	}
	h.coup.AddFAQ(question, answer, keywords)
}

// createTicket 提单并返回 id。
func (h *supportHarness) createTicket(t *testing.T, token, title, body string) string {
	t.Helper()
	code, raw := h.post(t, "/v1/support/tickets", token, `{"title":"`+title+`","body":"`+body+`"}`)
	if code != http.StatusOK {
		t.Fatalf("create = %d %s", code, raw)
	}
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out.Data.ID == "" {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	return out.Data.ID
}

// runSupportContract 客服契约链:认证 → 路由形状 → 提单校验 → 归属隔离 →
// 状态机(玩家追加不转态/关单拒回)→ 列表倒序与整轮翻页 → 游标/limit 校验 →
// 工单上下文 → FAQ 检索 → 附件直传 → 举报受理 → 处罚投影与申诉。
func runSupportContract(t *testing.T, h *supportHarness, useAlt bool) {
	t.Helper()
	token := h.login(t)

	// 匿名 → 401 COMMON_UNAUTHENTICATED。
	if code, body := h.get(t, "/v1/support/tickets", ""); code != http.StatusUnauthorized ||
		!strings.Contains(body, "COMMON_UNAUTHENTICATED") {
		t.Fatalf("anon = %d %s, want 401 COMMON_UNAUTHENTICATED", code, body)
	}
	// 路由形状:未知路径 404;不支持的方法 400。
	if code, _ := h.get(t, "/v1/support/nope", token); code != http.StatusNotFound {
		t.Fatalf("unknown path = %d, want 404", code)
	}
	if code, _ := h.do(t, http.MethodDelete, "/v1/support/tickets", token, ""); code != http.StatusBadRequest {
		t.Fatalf("DELETE tickets = %d, want 400", code)
	}

	// 提单校验:title 1-120 / body 1-4000 → 400 COMMON_INVALID_ARGUMENT。
	for _, bad := range []string{
		`{"title":"","body":"b"}`,
		`{"title":"` + strings.Repeat("标", 121) + `","body":"b"}`,
		`{"title":"t","body":""}`,
		`{"title":"t","body":"` + strings.Repeat("字", 4001) + `"}`,
	} {
		if code, body := h.post(t, "/v1/support/tickets", token, bad); code != http.StatusBadRequest ||
			!strings.Contains(body, "COMMON_INVALID_ARGUMENT") {
			t.Fatalf("create %s = %d %s, want 400", bad, code, body)
		}
	}

	// 合法建单:缺省 category=OTHER,状态 OPEN(首条消息在详情里回显,建单响应只回工单)。
	code, body := h.post(t, "/v1/support/tickets", token, `{"title":"丢登录","body":"重登后卡在选角"}`)
	if code != http.StatusOK || !strings.Contains(body, `"status":"OPEN"`) ||
		!strings.Contains(body, `"category":"OTHER"`) {
		t.Fatalf("create = %d %s", code, body)
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ticketID := created.Data.ID

	// 归属隔离:他人令牌 → 404 SUPPORT_TICKET_NOT_FOUND(不泄露存在性)。
	other := h.loginOther(t)
	if code, body := h.get(t, "/v1/support/tickets/"+ticketID, other); code != http.StatusNotFound ||
		!strings.Contains(body, "SUPPORT_TICKET_NOT_FOUND") {
		t.Fatalf("other detail = %d %s, want 404 SUPPORT_TICKET_NOT_FOUND", code, body)
	}
	if code, body := h.post(t, "/v1/support/tickets/"+ticketID+"/messages", other, `{"body":"hi"}`); code != http.StatusNotFound {
		t.Fatalf("other append = %d %s, want 404", code, body)
	}
	// 他人列表不含我的工单。
	_, otherList := h.get(t, "/v1/support/tickets", other)
	if strings.Contains(otherList, ticketID) {
		t.Fatalf("他人列表泄露: %s", otherList)
	}

	// 状态机:玩家追加不改状态;坐席回复 → REPLIED;关单后玩家再回 → 409。
	if code, body := h.post(t, "/v1/support/tickets/"+ticketID+"/messages", token, `{"body":"补充一下"}`); code != http.StatusOK ||
		!strings.Contains(body, `"senderType":"PLAYER"`) {
		t.Fatalf("append = %d %s", code, body)
	}
	if code, body := h.get(t, "/v1/support/tickets/"+ticketID, token); code != http.StatusOK ||
		!strings.Contains(body, `"status":"OPEN"`) ||
		!strings.Contains(body, `"senderType":"PLAYER"`) {
		t.Fatalf("玩家追加后应仍 OPEN: %d %s", code, body)
	}
	h.agentReply(t, useAlt, ticketID, "已定位,稍后修复")
	if code, body := h.get(t, "/v1/support/tickets/"+ticketID, token); code != http.StatusOK ||
		!strings.Contains(body, `"status":"REPLIED"`) || !strings.Contains(body, `"senderType":"AGENT"`) {
		t.Fatalf("坐席回复后应 REPLIED: %d %s", code, body)
	}
	h.agentClose(t, useAlt, ticketID)
	if code, body := h.post(t, "/v1/support/tickets/"+ticketID+"/messages", token, `{"body":"还能回吗"}`); code != http.StatusConflict ||
		!strings.Contains(body, "SUPPORT_TICKET_CLOSED") {
		t.Fatalf("关单后追加 = %d %s, want 409 SUPPORT_TICKET_CLOSED", code, body)
	}

	// 列表:updatedAt 倒序 + 整轮翻页不重不漏。丢登录已被关单(updatedAt = T3),
	// 次新/最旧建于其后 → 全序 = 最旧, 次新, 丢登录。
	for _, title := range []string{"次新", "最旧"} {
		h.createTicket(t, token, title, "b")
	}
	order := h.ticketIDs(t, token)
	if strings.Join(order, ",") != "最旧,次新,丢登录" {
		t.Fatalf("列表全序 = %v, want 最旧,次新,丢登录", order)
	}
	// 单页读完:nextCursor 空。
	_, body = h.get(t, "/v1/support/tickets?limit=100", token)
	if !strings.Contains(body, `"nextCursor":""`) {
		t.Fatalf("末页 nextCursor 应为空: %s", body)
	}
	// 非法游标 → 400(契约:游标不透明,非法即 COMMON_INVALID_ARGUMENT)。
	if code, body := h.get(t, "/v1/support/tickets?cursor=not-a-cursor", token); code != http.StatusBadRequest ||
		!strings.Contains(body, "COMMON_INVALID_ARGUMENT") {
		t.Fatalf("坏游标 = %d %s, want 400", code, body)
	}
	// limit 越界 → 400。
	for _, l := range []string{"0", "101", "abc"} {
		if code, _ := h.get(t, "/v1/support/tickets?limit="+l, token); code != http.StatusBadRequest {
			t.Fatalf("limit=%s = %d, want 400", l, code)
		}
	}

	// 工单上下文(v1.1):零解释透传回显 + 限额校验。
	code, body = h.post(t, "/v1/support/tickets", token,
		`{"title":"带上下文","body":"b","context":{"level":3,"vip":true,"orderId":"o_1"}}`)
	if code != http.StatusOK || !strings.Contains(body, `"level":3`) || !strings.Contains(body, `"vip":true`) {
		t.Fatalf("context 回显 = %d %s", code, body)
	}
	for _, badCtx := range []string{`{"bad-key!":"v"}`, `{"k":{"nested":1}}`} {
		if code, body := h.post(t, "/v1/support/tickets", token,
			`{"title":"t","body":"b","context":`+badCtx+`}`); code != http.StatusBadRequest {
			t.Fatalf("非法 context %s = %d %s", badCtx, code, body)
		}
	}

	// FAQ 检索:问题/关键词命中,大小写不敏感;空 keyword → 400;limit 越界 → 400。
	h.addFAQ(t, useAlt, "如何重置密码", "登录页点忘记密码", []string{"找回", "password"})
	h.addFAQ(t, useAlt, "如何充值", "钱包页选择渠道", []string{"充值"})
	if code, body := h.get(t, "/v1/support/faq?keyword="+"%E5%AF%86%E7%A0%81", token); code != http.StatusOK ||
		!strings.Contains(body, "如何重置密码") || strings.Contains(body, "如何充值") {
		t.Fatalf("faq 命中 = %d %s", code, body)
	}
	if code, body := h.get(t, "/v1/support/faq?keyword=Recharge%20none", token); code != http.StatusOK ||
		strings.Contains(body, "如何充值") {
		t.Fatalf("faq 不该命中 = %d %s", code, body)
	}
	if code, _ := h.get(t, "/v1/support/faq?keyword=", token); code != http.StatusBadRequest {
		t.Fatalf("空 keyword = %d, want 400", code)
	}
	if code, _ := h.get(t, "/v1/support/faq?limit=51", token); code != http.StatusBadRequest {
		t.Fatalf("faq limit=51 = %d, want 400", code)
	}

	// 附件直传(F44):预签名 URL + 详情登记行(不暴露存储路径)。
	fresh := h.createTicket(t, token, "带附件", "截图见附件")
	code, body = h.post(t, "/v1/support/tickets/"+fresh+"/attachments", token,
		`{"fileName":"shot.png","contentType":"image/png","sizeBytes":2048,"kind":"IMAGE"}`)
	if code != http.StatusOK || !strings.Contains(body, `"method":"PUT"`) ||
		!strings.Contains(body, "https://store.example/") {
		t.Fatalf("上传申请 = %d %s", code, body)
	}
	_, body = h.get(t, "/v1/support/tickets/"+fresh, token)
	if !strings.Contains(body, `"fileName":"shot.png"`) || !strings.Contains(body, `"downloadUrl"`) {
		t.Fatalf("详情缺附件登记: %s", body)
	}
	if code, body := h.post(t, "/v1/support/tickets/"+fresh+"/attachments", token,
		`{"fileName":"a","contentType":"application/x-msdownload","sizeBytes":1}`); code != http.StatusBadRequest {
		t.Fatalf("非法 contentType = %d %s, want 400", code, body)
	}

	// 举报受理(F45):只回 id,不回显进展/理由。
	code, body = h.post(t, "/v1/support/reports", token,
		`{"targetType":"PLAYER","targetId":"acc_other","reason":"CHEATING","detail":"开挂"}`)
	if code != http.StatusOK || !strings.Contains(body, `"id":"rpt_`) {
		t.Fatalf("举报 = %d %s", code, body)
	}
	if strings.Contains(body, `"reason"`) || strings.Contains(body, `"targetId"`) {
		t.Fatalf("举报响应不应回显: %s", body)
	}
	for _, badRep := range []string{
		`{"targetType":"GAME","targetId":"x","reason":"CHEATING"}`,
		`{"targetType":"PLAYER","targetId":"x","reason":"NOT_A_REASON"}`,
		`{"targetType":"PLAYER","targetId":"","reason":"CHEATING"}`,
	} {
		if code, _ := h.post(t, "/v1/support/reports", token, badRep); code != http.StatusBadRequest {
			t.Fatalf("非法举报 %s = %d, want 400", badRep, code)
		}
	}

	// 处罚投影与申诉(F45):投影源已登记处罚 → issuedAt 倒序 + 分页 + 单例申诉。
	code, body = h.get(t, "/v1/support/penalties", token)
	if code != http.StatusOK || !strings.Contains(body, `"id":"pen_b"`) ||
		strings.Index(body, `"id":"pen_b"`) > strings.Index(body, `"id":"pen_a"`) {
		t.Fatalf("处罚倒序 = %d %s", code, body)
	}
	if code, body := h.get(t, "/v1/support/penalties/pen_zzz", token); code != http.StatusNotFound ||
		!strings.Contains(body, "SUPPORT_PENALTY_NOT_FOUND") {
		t.Fatalf("未知处罚 = %d %s, want 404", code, body)
	}
	code, body = h.post(t, "/v1/support/penalties/pen_b/appeal", token, "")
	if code != http.StatusOK || !strings.Contains(body, `"state":"PENDING"`) {
		t.Fatalf("申诉 = %d %s", code, body)
	}
	if code, body := h.post(t, "/v1/support/penalties/pen_b/appeal", token, ""); code != http.StatusConflict ||
		!strings.Contains(body, "SUPPORT_APPEAL_EXISTS") {
		t.Fatalf("重复申诉 = %d %s, want 409 SUPPORT_APPEAL_EXISTS", code, body)
	}
}

// loginOther 第二个玩家(归属隔离用例;游客登录带不同 deviceId)。
func (h *supportHarness) loginOther(t *testing.T) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/identity/guest",
		strings.NewReader(`{"deviceId":"support-e2e-other","platform":"linux"}`))
	h.scopeHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("guest login: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Data.AccessToken == "" {
		t.Fatalf("login decode: status=%d err=%v", resp.StatusCode, err)
	}
	return out.Data.AccessToken
}

// TestSupportContractConformance 两实现 × 同一契约,同一断言。
func TestSupportContractConformance(t *testing.T) {
	for _, impl := range []struct {
		name   string
		useAlt bool
	}{
		{"croupier", false},
		{"croupier-alt", true},
	} {
		t.Run(impl.name, func(t *testing.T) {
			runSupportContract(t, newSupportHarness(t, impl.useAlt), impl.useAlt)
		})
	}
}
