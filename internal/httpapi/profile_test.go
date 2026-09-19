package httpapi

import (
	"bytes"
	"strings"
	"testing"

	"ceksepet-api/internal/domain"
)

func TestProfileScreens(t *testing.T) { profileScreens(t, newTestApp(t)) }

func profileScreens(t *testing.T, app *testApp) {
	seller := registerUser(t, app, "+905552000001", "123456")
	buyer := registerUser(t, app, "+905552000002", "123456")
	outsider := registerUser(t, app, "+905552000003", "123456")
	operator := operatorToken(t, app)
	creditTestTickets(t, app, buyer.User.ID, operator, 200)
	me := requestJSON(t, app.handler, "GET", "/api/v1/users/me", nil, buyer.AccessToken)
	assertStatus(t, me, 200)
	var profile userResponse
	decodeResponse(t, me, &profile)
	if profile.TicketBalance != 200 || profile.DisplayName != profile.FirstName+" "+profile.LastName || profile.AvatarURL == "" {
		t.Fatalf("profile card incomplete: %s", me.Body.String())
	}
	assertStatus(t, requestJSON(t, app.handler, "GET", "/api/v1/users/me", nil, ""), 401)
	sellerPath := "/api/v1/sellers/" + seller.User.ID
	for _, suffix := range []string{"", "/raffles", "/reviews"} {
		assertStatus(t, requestJSON(t, app.handler, "GET", "/api/v1/sellers/missing"+suffix, nil, ""), 404)
	}
	pending := createTestRaffle(t, app, seller, 10000)
	active := createTestRaffle(t, app, seller, 10000)
	completed := createTestRaffle(t, app, seller, 1000)
	for _, id := range []string{active.ID, completed.ID} {
		approveTestRaffle(t, app, id, operator)
	}
	assertStatus(t, marketRequest(t, app, "POST", "/api/v1/raffles/"+completed.ID+"/participations", buyer.AccessToken, "profile-join-0001", map[string]any{"tickets": completed.Quote.TotalTickets, "confirmed": true}), 201)
	reviewPath := "/api/v1/raffles/" + completed.ID + "/reviews"
	assertStatus(t, requestJSON(t, app.handler, "POST", "/api/v1/operator/raffles/"+completed.ID+"/cancel", map[string]string{"reason": "Sonlanmış çekiliş iptal edilemez."}, operator), 409)
	for _, rating := range []float64{0, 5.5, 4.3} {
		assertStatus(t, requestJSON(t, app.handler, "POST", reviewPath, map[string]any{"rating": rating, "body": "Ürün hakkında yorum."}, buyer.AccessToken), 400)
	}
	review := map[string]any{"rating": 4.5, "body": "Satıcının iletişiminden memnun kaldım."}
	assertStatus(t, requestJSON(t, app.handler, "POST", reviewPath, review, outsider.AccessToken), 403)
	assertStatus(t, requestJSON(t, app.handler, "POST", reviewPath, review, buyer.AccessToken), 201)
	assertStatus(t, requestJSON(t, app.handler, "POST", reviewPath, review, buyer.AccessToken), 409)
	assertStatus(t, requestJSON(t, app.handler, "PUT", "/api/v1/raffles/"+active.ID+"/favorite", nil, buyer.AccessToken), 200)
	for _, token := range []string{"", seller.AccessToken, buyer.AccessToken} {
		response := requestJSON(t, app.handler, "GET", sellerPath, nil, token)
		assertStatus(t, response, 200)
		var header struct {
			Seller marketProfile
			Rating struct {
				Average float64
				Count   int64
			}
			RaffleCount int64
			CanReport   bool
		}
		decodeResponse(t, response, &header)
		if header.Seller.ID != seller.User.ID || header.Seller.AvatarURL == "" || header.Rating.Average != 4.5 || header.Rating.Count != 1 || header.RaffleCount != 2 || header.CanReport != (token == buyer.AccessToken) {
			t.Fatalf("seller header: %s", response.Body.String())
		}
		for _, field := range []string{"phone", "ticketBalance", "pinHash"} {
			if bytes.Contains(response.Body.Bytes(), []byte(`"`+field+`"`)) {
				t.Fatalf("private field leaked: %s", field)
			}
		}
		list := requestJSON(t, app.handler, "GET", sellerPath+"/raffles?status=all&limit=1", nil, token)
		assertStatus(t, list, 200)
		var page struct {
			Items   []raffleCard
			Total   int64
			HasMore bool
		}
		decodeResponse(t, list, &page)
		if page.Total != 2 || len(page.Items) != 1 || !page.HasMore || page.Items[0].ID == pending.ID {
			t.Fatalf("seller raffle visibility: %s", list.Body.String())
		}
	}
	assertStatus(t, requestJSON(t, app.handler, "GET", sellerPath+"/raffles?status=pending", nil, seller.AccessToken), 400)
	list := requestJSON(t, app.handler, "GET", sellerPath+"/raffles?status=active", nil, buyer.AccessToken)
	var cards struct{ Items []raffleCard }
	decodeResponse(t, list, &cards)
	if len(cards.Items) != 1 || cards.Items[0].ID != active.ID || !cards.Items[0].IsFavorite {
		t.Fatalf("favorite in seller grid: %s", list.Body.String())
	}
	reviews := requestJSON(t, app.handler, "GET", sellerPath+"/reviews?limit=1", nil, "")
	assertStatus(t, reviews, 200)
	var reviewPage struct {
		Items         []reviewView
		AverageRating float64
		Total         int64
	}
	decodeResponse(t, reviews, &reviewPage)
	if reviewPage.Total != 1 || reviewPage.AverageRating != 4.5 || len(reviewPage.Items) != 1 || reviewPage.Items[0].Author.DisplayName == "" || reviewPage.Items[0].CreatedAt.IsZero() {
		t.Fatalf("review rows: %s", reviews.Body.String())
	}
	faqs := requestJSON(t, app.handler, "GET", "/api/v1/faqs", nil, "")
	assertStatus(t, faqs, 200)
	var faq struct {
		Title, AboutTitle, About string
		Items                    []faqItem
	}
	decodeResponse(t, faqs, &faq)
	if faq.Title == "" || faq.AboutTitle == "" || faq.About == "" || len(faq.Items) != 4 {
		t.Fatalf("FAQ screen: %s", faqs.Body.String())
	}
	for _, item := range faq.Items {
		if item.ID == "" || item.Question == "" || item.Answer == "" {
			t.Fatal("empty FAQ field")
		}
	}
}

func TestSellerReports(t *testing.T) {
	app := newTestApp(t)
	seller := registerUser(t, app, "+905552100001", "123456")
	buyer := registerUser(t, app, "+905552100002", "123456")
	operator := operatorToken(t, app)
	reasons := requestJSON(t, app.handler, "GET", "/api/v1/seller-report-reasons", nil, "")
	assertStatus(t, reasons, 200)
	var catalog struct {
		Items                []domain.ReportReason
		MaxDescriptionLength int
	}
	decodeResponse(t, reasons, &catalog)
	if len(catalog.Items) != 3 || catalog.MaxDescriptionLength != 2000 {
		t.Fatalf("report reasons: %s", reasons.Body.String())
	}
	path := "/api/v1/sellers/" + seller.User.ID + "/reports"
	body := map[string]string{"reasonId": catalog.Items[0].ID, "description": "  Ürün açıklamasındaki bilgi yanıltıcı.  "}
	assertStatus(t, requestJSON(t, app.handler, "POST", path, body, ""), 401)
	assertStatus(t, requestJSON(t, app.handler, "POST", path, body, seller.AccessToken), 403)
	assertStatus(t, requestJSON(t, app.handler, "POST", "/api/v1/sellers/missing/reports", body, buyer.AccessToken), 404)
	assertStatus(t, requestJSON(t, app.handler, "POST", path, map[string]string{"reasonId": "unknown"}, buyer.AccessToken), 400)
	assertStatus(t, requestJSON(t, app.handler, "POST", path, map[string]string{"reasonId": catalog.Items[0].ID, "description": strings.Repeat("ç", 2001)}, buyer.AccessToken), 400)
	response := requestJSON(t, app.handler, "POST", path, body, buyer.AccessToken)
	assertStatus(t, response, 201)
	var report domain.SellerReport
	decodeResponse(t, response, &report)
	if report.ID == "" || report.Status != "open" || report.Description != strings.TrimSpace(body["description"]) {
		t.Fatalf("report response: %s", response.Body.String())
	}
	assertStatus(t, requestJSON(t, app.handler, "POST", path, body, buyer.AccessToken), 409)
	operatorPath := "/api/v1/operator/seller-reports"
	assertStatus(t, requestJSON(t, app.handler, "GET", operatorPath, nil, seller.AccessToken), 401)
	assertStatus(t, requestJSON(t, app.handler, "GET", operatorPath+"?status=unknown", nil, operator), 400)
	inbox := requestJSON(t, app.handler, "GET", operatorPath+"?limit=1", nil, operator)
	assertStatus(t, inbox, 200)
	var reports struct {
		Items []domain.SellerReport
		Total int64
	}
	decodeResponse(t, inbox, &reports)
	if reports.Total != 1 || len(reports.Items) != 1 || reports.Items[0].ID != report.ID {
		t.Fatalf("report inbox: %s", inbox.Body.String())
	}
	resolutionPath := operatorPath + "/" + report.ID + "/resolution"
	resolution := map[string]string{"status": "resolved", "resolution": "Satıcı bilgileri kontrol edilip düzeltildi."}
	assertStatus(t, requestJSON(t, app.handler, "POST", resolutionPath, resolution, buyer.AccessToken), 401)
	assertStatus(t, requestJSON(t, app.handler, "POST", resolutionPath, map[string]string{"status": "open", "resolution": "Test"}, operator), 400)
	assertStatus(t, requestJSON(t, app.handler, "POST", resolutionPath, resolution, operator), 200)
	assertStatus(t, requestJSON(t, app.handler, "POST", resolutionPath, resolution, operator), 200)
	resolution["status"] = "dismissed"
	assertStatus(t, requestJSON(t, app.handler, "POST", resolutionPath, resolution, operator), 409)
	inbox = requestJSON(t, app.handler, "GET", operatorPath, nil, operator)
	decodeResponse(t, inbox, &reports)
	if reports.Total != 0 || len(reports.Items) != 0 {
		t.Fatal("resolved report in open inbox")
	}
	assertStatus(t, requestJSON(t, app.handler, "POST", path, map[string]string{"reasonId": catalog.Items[1].ID}, buyer.AccessToken), 201)
}

func TestCancelledRaffleRefundAndFilters(t *testing.T) {
	cancelledRaffleRefundAndFilters(t, newTestApp(t))
}

func cancelledRaffleRefundAndFilters(t *testing.T, app *testApp) {
	seller := registerUser(t, app, "+905552200001", "123456")
	buyer := registerUser(t, app, "+905552200002", "123456")
	operator := operatorToken(t, app)
	raffle := createTestRaffle(t, app, seller, 10000)
	approveTestRaffle(t, app, raffle.ID, operator)
	creditTestTickets(t, app, buyer.User.ID, operator, 100)
	path := "/api/v1/raffles/" + raffle.ID
	for _, key := range []string{"cancel-join-0001", "cancel-join-0002"} {
		assertStatus(t, marketRequest(t, app, "POST", path+"/participations", buyer.AccessToken, key, map[string]any{"tickets": 20, "confirmed": true}), 201)
	}
	assertStatus(t, requestJSON(t, app.handler, "PUT", path+"/favorite", nil, buyer.AccessToken), 200)
	cancelPath := "/api/v1/operator/raffles/" + raffle.ID + "/cancel"
	body := map[string]string{"reason": "Ürün satıcı tarafından temin edilemiyor."}
	assertStatus(t, requestJSON(t, app.handler, "POST", cancelPath, body, seller.AccessToken), 401)
	assertStatus(t, requestJSON(t, app.handler, "POST", cancelPath, map[string]string{"reason": ""}, operator), 400)
	for i := 0; i < 2; i++ {
		assertStatus(t, requestJSON(t, app.handler, "POST", cancelPath, body, operator), 200)
	}
	wallet := requestJSON(t, app.handler, "GET", "/api/v1/users/me/wallet", nil, buyer.AccessToken)
	var balance struct{ Balance int64 }
	decodeResponse(t, wallet, &balance)
	if balance.Balance != 100 {
		t.Fatalf("refund not exactly once: %s", wallet.Body.String())
	}
	history := requestJSON(t, app.handler, "GET", "/api/v1/users/me/wallet/transactions", nil, buyer.AccessToken)
	var entries struct{ Items []domain.TicketEntry }
	decodeResponse(t, history, &entries)
	refunds := 0
	for _, e := range entries.Items {
		if e.Kind == "raffle_refund" {
			refunds++
			if e.Delta != 40 || e.RaffleID != raffle.ID {
				t.Fatalf("refund receipt: %+v", e)
			}
		}
	}
	if refunds != 1 {
		t.Fatalf("refund entries = %d", refunds)
	}
	for _, filter := range []struct {
		Path, Token string
		Count       int
	}{
		{"/api/v1/users/me/raffles?status=all", seller.AccessToken, 1},
		{"/api/v1/users/me/raffles?status=cancelled", seller.AccessToken, 1},
		{"/api/v1/users/me/raffles?status=active", seller.AccessToken, 0},
		{"/api/v1/users/me/participations?status=all", buyer.AccessToken, 1},
		{"/api/v1/users/me/participations?status=cancelled", buyer.AccessToken, 1},
		{"/api/v1/users/me/participations?status=completed", buyer.AccessToken, 0},
		{"/api/v1/users/me/participations", seller.AccessToken, 0},
		{"/api/v1/raffles", buyer.AccessToken, 0},
	} {
		response := requestJSON(t, app.handler, "GET", filter.Path, nil, filter.Token)
		assertStatus(t, response, 200)
		var rows struct{ Items []raffleCard }
		decodeResponse(t, response, &rows)
		if len(rows.Items) != filter.Count {
			t.Fatalf("filter %s: %s", filter.Path, response.Body.String())
		}
		if filter.Count == 1 {
			card := rows.Items[0]
			if card.Badge != "İptal edildi" || card.RemainingTickets != 0 || card.CancelledAt == nil || card.CancellationReason == "" {
				t.Fatalf("cancelled card: %+v", card)
			}
			if filter.Token == buyer.AccessToken && (card.MyTickets != 40 || card.MyRefundedTickets != 40) {
				t.Fatalf("participation refund counts: %+v", card)
			}
		}
	}
	assertStatus(t, requestJSON(t, app.handler, "GET", path, nil, buyer.AccessToken), 200)
	assertStatus(t, requestJSON(t, app.handler, "GET", "/api/v1/photos/"+raffle.PhotoIDs[0], nil, ""), 200)
	assertStatus(t, requestJSON(t, app.handler, "PUT", path+"/favorite", nil, buyer.AccessToken), 409)
	assertStatus(t, requestJSON(t, app.handler, "DELETE", path+"/favorite", nil, buyer.AccessToken), 200)
	assertStatus(t, marketRequest(t, app, "POST", path+"/participations", buyer.AccessToken, "cancel-join-0003", map[string]any{"tickets": 1, "confirmed": true}), 409)
	assertStatus(t, requestJSON(t, app.handler, "POST", path+"/reviews", map[string]any{"rating": 4.5, "body": "Yorum"}, buyer.AccessToken), 403)
	pending := createTestRaffle(t, app, seller, 10000)
	assertStatus(t, requestJSON(t, app.handler, "POST", "/api/v1/operator/raffles/"+pending.ID+"/cancel", body, operator), 200)
	assertStatus(t, requestJSON(t, app.handler, "GET", "/api/v1/raffles/"+pending.ID, nil, ""), 404)
	assertStatus(t, requestJSON(t, app.handler, "GET", "/api/v1/photos/"+pending.PhotoIDs[0], nil, ""), 404)
}
