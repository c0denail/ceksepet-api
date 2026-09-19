package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"mime/multipart"
	"net/http/httptest"
	"sync"
	"testing"

	"ceksepet-api/internal/domain"
)

func operatorToken(t *testing.T, app *testApp) string {
	r := requestJSON(t, app.handler, "POST", "/api/v1/operator/login", map[string]string{"username": "operator", "password": "test-operator-password-with-32-characters"}, "")
	assertStatus(t, r, 200)
	var auth operatorAuthResponse
	decodeResponse(t, r, &auth)
	return auth.AccessToken
}

func marketRequest(t *testing.T, app *testApp, method, path, token, key string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	app.handler.ServeHTTP(w, r)
	return w
}

func uploadTestPhoto(t *testing.T, app *testApp, token string) string {
	t.Helper()
	var file bytes.Buffer
	if err := png.Encode(&file, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	w := uploadBytes(t, app, token, file.Bytes())
	assertStatus(t, w, 201)
	var photo struct {
		ID string `json:"id"`
	}
	decodeResponse(t, w, &photo)
	return photo.ID
}

func uploadBytes(t *testing.T, app *testApp, token string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "photo.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/v1/photos", &body)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	app.handler.ServeHTTP(w, r)
	return w
}

func createTestRaffle(t *testing.T, app *testApp, seller authResponse, price int64) domain.Raffle {
	t.Helper()
	photo := uploadTestPhoto(t, app, seller.AccessToken)
	response := requestJSON(t, app.handler, "POST", "/api/v1/raffles", createRaffleRequest{Title: "Samsung Galaxy S Tablet", Description: "Kutusu ve tüm aksesuarları ile temiz tablet.", CategoryID: "tablet", BrandID: "samsung", ShippingPayer: "buyer", PhotoIDs: []string{photo}, PriceKurus: price}, seller.AccessToken)
	assertStatus(t, response, 201)
	var raffle domain.Raffle
	decodeResponse(t, response, &raffle)
	return raffle
}

func approveTestRaffle(t *testing.T, app *testApp, id, operator string) {
	t.Helper()
	assertStatus(t, requestJSON(t, app.handler, "POST", "/api/v1/operator/raffles/"+id+"/moderation", map[string]string{"decision": "approve"}, operator), 200)
}

func creditTestTickets(t *testing.T, app *testApp, user, operator string, tickets int64) {
	t.Helper()
	r := marketRequest(t, app, "POST", "/api/v1/operator/users/"+user+"/tickets", operator, "credit-"+user, map[string]any{"tickets": tickets, "reason": "Test bakiyesi"})
	assertStatus(t, r, 201)
}

func TestMarketplaceScreensEndToEnd(t *testing.T) {
	marketplaceScreensEndToEnd(t, newTestApp(t))
}

func marketplaceScreensEndToEnd(t *testing.T, app *testApp) {
	seller := registerUser(t, app, "+905551000001", "123456")
	buyer := registerUser(t, app, "+905551000002", "123456")
	operator := operatorToken(t, app)
	for _, path := range []string{"/api/v1/catalog?categoryId=tablet", "/api/v1/content/bag-raffle", "/api/v1/content/raffle-safety", "/api/v1/content/selling"} {
		assertStatus(t, requestJSON(t, app.handler, "GET", path, nil, ""), 200)
	}
	quote := requestJSON(t, app.handler, "POST", "/api/v1/raffles/quote", map[string]int64{"priceKurus": 10000}, "")
	assertStatus(t, quote, 200)
	var q domain.PriceQuote
	decodeResponse(t, quote, &q)
	if q.TotalTickets != 110 || q.CommissionTickets != 10 {
		t.Fatalf("bad quote: %+v", q)
	}
	raffle := createTestRaffle(t, app, seller, 10000)
	path := "/api/v1/raffles/" + raffle.ID
	if raffle.Status != domain.RafflePending {
		t.Fatal("new raffle must be pending")
	}
	assertStatus(t, requestJSON(t, app.handler, "GET", path, nil, ""), 404)
	assertStatus(t, requestJSON(t, app.handler, "GET", path, nil, seller.AccessToken), 200)
	photoPath := "/api/v1/photos/" + raffle.PhotoIDs[0]
	assertStatus(t, requestJSON(t, app.handler, "GET", photoPath, nil, ""), 404)
	assertStatus(t, requestJSON(t, app.handler, "GET", photoPath, nil, operator), 200)
	assertStatus(t, requestJSON(t, app.handler, "GET", "/api/v1/operator/raffles", nil, buyer.AccessToken), 401)
	approveTestRaffle(t, app, raffle.ID, operator)
	assertStatus(t, requestJSON(t, app.handler, "GET", photoPath, nil, ""), 200)
	assertStatus(t, requestJSON(t, app.handler, "PUT", path+"/favorite", nil, buyer.AccessToken), 200)
	assertStatus(t, requestJSON(t, app.handler, "PUT", path+"/favorite", nil, buyer.AccessToken), 200)
	list := requestJSON(t, app.handler, "GET", "/api/v1/raffles?q=Samsung&categoryId=electronics&sort=popular&limit=1", nil, buyer.AccessToken)
	assertStatus(t, list, 200)
	var cards struct {
		Items []raffleCard `json:"items"`
		Total int64        `json:"total"`
	}
	decodeResponse(t, list, &cards)
	if cards.Total != 1 || len(cards.Items) != 1 || !cards.Items[0].IsFavorite || cards.Items[0].Badge != "Yeni" {
		t.Fatalf("bad cards: %s", list.Body.String())
	}
	creditTestTickets(t, app, buyer.User.ID, operator, 200)
	preview := requestJSON(t, app.handler, "POST", path+"/participation-preview", map[string]int64{"tickets": 100}, buyer.AccessToken)
	assertStatus(t, preview, 200)
	wallet := requestJSON(t, app.handler, "GET", "/api/v1/users/me/wallet", nil, buyer.AccessToken)
	if !bytes.Contains(wallet.Body.Bytes(), []byte(`"balance":200`)) {
		t.Fatal("preview spent tickets")
	}
	question := requestJSON(t, app.handler, "POST", path+"/questions", map[string]string{"body": "Garantisi devam ediyor mu?"}, buyer.AccessToken)
	assertStatus(t, question, 201)
	var questionBody questionView
	decodeResponse(t, question, &questionBody)
	replyPath := path + "/questions/" + questionBody.ID + "/replies"
	assertStatus(t, requestJSON(t, app.handler, "POST", replyPath, map[string]string{"body": "Evet, bir yıl garantisi var."}, seller.AccessToken), 201)
	detail := requestJSON(t, app.handler, "GET", path, nil, buyer.AccessToken)
	assertStatus(t, detail, 200)
	if !bytes.Contains(detail.Body.Bytes(), []byte(`"isSeller":true`)) || !bytes.Contains(detail.Body.Bytes(), []byte(`"replyCount":1`)) {
		t.Fatalf("detail lacks seller reply: %s", detail.Body.String())
	}
	for _, forbidden := range []string{`"phone"`, `"pinHash"`, `"ticketBalance"`} {
		if bytes.Contains(detail.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("private data in detail: %s", forbidden)
		}
	}
	join := map[string]any{"tickets": 110, "confirmed": true}
	first := marketRequest(t, app, "POST", path+"/participations", buyer.AccessToken, "join-key-0001", join)
	assertStatus(t, first, 201)
	retry := marketRequest(t, app, "POST", path+"/participations", buyer.AccessToken, "join-key-0001", join)
	assertStatus(t, retry, 200)
	var receipt struct {
		Transaction domain.TicketEntry `json:"transaction"`
		Replayed    bool               `json:"replayed"`
	}
	decodeResponse(t, retry, &receipt)
	if !receipt.Replayed || receipt.Transaction.BalanceAfter != 90 || receipt.Transaction.RaffleStatus != domain.RaffleCompleted {
		t.Fatalf("bad receipt: %s", retry.Body.String())
	}
	joined := requestJSON(t, app.handler, "GET", "/api/v1/users/me/participations", nil, buyer.AccessToken)
	assertStatus(t, joined, 200)
	decodeResponse(t, joined, &cards)
	if cards.Total != 1 || cards.Items[0].MyTickets != 110 || cards.Items[0].Badge != "Sonlandı" {
		t.Fatalf("bad joined list: %s", joined.Body.String())
	}
	assertStatus(t, requestJSON(t, app.handler, "POST", path+"/reviews", map[string]any{"rating": 5, "body": "Satıcı sorularıma yanıt verdi."}, buyer.AccessToken), 201)
	assertStatus(t, requestJSON(t, app.handler, "POST", path+"/reviews", map[string]any{"rating": 4, "body": "Tekrar puan."}, buyer.AccessToken), 409)
	reviews := requestJSON(t, app.handler, "GET", "/api/v1/sellers/"+seller.User.ID+"/reviews", nil, "")
	assertStatus(t, reviews, 200)
	if !bytes.Contains(reviews.Body.Bytes(), []byte(`"averageRating":5`)) {
		t.Fatal("rating missing")
	}
	notifications := requestJSON(t, app.handler, "GET", "/api/v1/users/me/notifications?unreadOnly=true", nil, buyer.AccessToken)
	assertStatus(t, notifications, 200)
	var inbox struct {
		Items []domain.Notification `json:"items"`
	}
	decodeResponse(t, notifications, &inbox)
	if len(inbox.Items) != 1 {
		t.Fatalf("wrong notifications: %s", notifications.Body.String())
	}
	readPath := "/api/v1/users/me/notifications/" + inbox.Items[0].ID + "/read"
	assertStatus(t, requestJSON(t, app.handler, "PUT", readPath, nil, seller.AccessToken), 404)
	assertStatus(t, requestJSON(t, app.handler, "PUT", readPath, nil, buyer.AccessToken), 204)
	assertStatus(t, requestJSON(t, app.handler, "DELETE", path+"/favorite", nil, buyer.AccessToken), 200)
	assertStatus(t, requestJSON(t, app.handler, "DELETE", "/api/v1/users/me", map[string]string{"username": buyer.User.Username, "pin": "123456"}, buyer.AccessToken), 409)
}

func TestMarketplaceValidationAndAuthorization(t *testing.T) {
	app := newTestApp(t)
	seller := registerUser(t, app, "+905551000011", "123456")
	buyer := registerUser(t, app, "+905551000012", "123456")
	operator := operatorToken(t, app)
	raffle := createTestRaffle(t, app, seller, 10000)
	path := "/api/v1/raffles/" + raffle.ID
	assertStatus(t, uploadBytes(t, app, buyer.AccessToken, []byte("<svg><script>evil()</script></svg>")), 400)
	assertStatus(t, requestJSON(t, app.handler, "POST", "/api/v1/raffles", createRaffleRequest{Title: "Yabancı ürün", Description: "Yabancı fotoğraf kullanılamaz.", CategoryID: "tablet", BrandID: "samsung", ShippingPayer: "seller", PhotoIDs: raffle.PhotoIDs, PriceKurus: 10000}, buyer.AccessToken), 403)
	for _, p := range []string{"/api/v1/raffles?status=pending", "/api/v1/raffles?sort=bad", "/api/v1/raffles?limit=101", "/api/v1/raffles?categoryId=unknown", "/api/v1/catalog?categoryId=unknown"} {
		assertStatus(t, requestJSON(t, app.handler, "GET", p, nil, ""), 400)
	}
	assertStatus(t, requestJSON(t, app.handler, "POST", "/api/v1/operator/raffles/"+raffle.ID+"/moderation", map[string]string{"decision": "approve"}, buyer.AccessToken), 401)
	approveTestRaffle(t, app, raffle.ID, operator)
	assertStatus(t, requestJSON(t, app.handler, "GET", path, nil, "bad-token"), 401)
	assertStatus(t, requestJSON(t, app.handler, "PUT", path+"/favorite", nil, ""), 401)
	assertStatus(t, marketRequest(t, app, "POST", path+"/participations", buyer.AccessToken, "", map[string]any{"tickets": 1, "confirmed": true}), 400)
	assertStatus(t, marketRequest(t, app, "POST", path+"/participations", buyer.AccessToken, "no-confirm-key", map[string]any{"tickets": 1, "confirmed": false}), 400)
	assertStatus(t, marketRequest(t, app, "POST", path+"/participations", seller.AccessToken, "own-raffle-key", map[string]any{"tickets": 1, "confirmed": true}), 403)
	assertStatus(t, marketRequest(t, app, "POST", path+"/participations", buyer.AccessToken, "poor-buyer-key", map[string]any{"tickets": 1, "confirmed": true}), 409)
	for _, tickets := range []any{0, -1, 1.5, 200_000_001} {
		assertStatus(t, marketRequest(t, app, "POST", path+"/participations", buyer.AccessToken, "invalid-tickets", map[string]any{"tickets": tickets, "confirmed": true}), 400)
	}
	creditTestTickets(t, app, buyer.User.ID, operator, 10)
	assertStatus(t, marketRequest(t, app, "POST", path+"/participations", buyer.AccessToken, "reused-key-0001", map[string]any{"tickets": 1, "confirmed": true}), 201)
	assertStatus(t, marketRequest(t, app, "POST", path+"/participations", buyer.AccessToken, "reused-key-0001", map[string]any{"tickets": 2, "confirmed": true}), 409)
	assertStatus(t, requestJSON(t, app.handler, "POST", path+"/reviews", map[string]any{"rating": 5, "body": "Henüz bitmedi."}, buyer.AccessToken), 403)
	question := requestJSON(t, app.handler, "POST", path+"/questions", map[string]string{"body": "Soru"}, buyer.AccessToken)
	assertStatus(t, question, 201)
	var q questionView
	decodeResponse(t, question, &q)
	other := createTestRaffle(t, app, seller, 10000)
	approveTestRaffle(t, app, other.ID, operator)
	assertStatus(t, requestJSON(t, app.handler, "POST", "/api/v1/raffles/"+other.ID+"/questions/"+q.ID+"/replies", map[string]string{"body": "Yanlış çekiliş"}, buyer.AccessToken), 404)
	options := httptest.NewRequest("OPTIONS", path+"/favorite", nil)
	options.Header.Set("Origin", "https://app.test")
	w := httptest.NewRecorder()
	app.handler.ServeHTTP(w, options)
	for _, method := range []string{"PUT", "DELETE"} {
		if !bytes.Contains([]byte(w.Header().Get("Access-Control-Allow-Methods")), []byte(method)) {
			t.Fatal("missing CORS method")
		}
	}
	if !bytes.Contains([]byte(w.Header().Get("Access-Control-Allow-Headers")), []byte("Idempotency-Key")) {
		t.Fatal("missing CORS header")
	}
}

func TestConcurrentParticipationDoesNotOversellOrDoubleSpend(t *testing.T) {
	app := newTestApp(t)
	seller := registerUser(t, app, "+905551000021", "123456")
	buyer := registerUser(t, app, "+905551000022", "123456")
	operator := operatorToken(t, app)
	raffle := createTestRaffle(t, app, seller, 1000)
	approveTestRaffle(t, app, raffle.ID, operator)
	creditTestTickets(t, app, buyer.User.ID, operator, 100)
	path := "/api/v1/raffles/" + raffle.ID + "/participations"
	var wg sync.WaitGroup
	statuses := make(chan int, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := marketRequest(t, app, "POST", path, buyer.AccessToken, fmt.Sprintf("parallel-%08d", i), map[string]any{"tickets": 1, "confirmed": true})
			statuses <- r.Code
		}(i)
	}
	wg.Wait()
	close(statuses)
	success := 0
	for status := range statuses {
		if status == 201 {
			success++
		} else if status != 409 {
			t.Fatalf("unexpected status %d", status)
		}
	}
	if success != 11 {
		t.Fatalf("expected 11 successful reservations, got %d", success)
	}
	wallet := requestJSON(t, app.handler, "GET", "/api/v1/users/me/wallet", nil, buyer.AccessToken)
	if !bytes.Contains(wallet.Body.Bytes(), []byte(`"balance":89`)) {
		t.Fatalf("incorrect balance: %s", wallet.Body.String())
	}
}

func TestEmptyStatesAndRejectedRaffles(t *testing.T) {
	app := newTestApp(t)
	seller := registerUser(t, app, "+905551000031", "123456")
	operator := operatorToken(t, app)
	for _, path := range []string{"/api/v1/raffles", "/api/v1/users/me/participations", "/api/v1/users/me/favorites", "/api/v1/users/me/notifications", "/api/v1/users/me/wallet/transactions"} {
		r := requestJSON(t, app.handler, "GET", path, nil, seller.AccessToken)
		assertStatus(t, r, 200)
		if !bytes.Contains(r.Body.Bytes(), []byte(`"items":[]`)) {
			t.Fatalf("empty list not an array: %s", r.Body.String())
		}
	}
	raffle := createTestRaffle(t, app, seller, 10000)
	assertStatus(t, requestJSON(t, app.handler, "POST", "/api/v1/operator/raffles/"+raffle.ID+"/moderation", map[string]string{"decision": "reject", "reason": "Fotoğraf net değil."}, operator), 200)
	assertStatus(t, requestJSON(t, app.handler, "GET", "/api/v1/raffles/"+raffle.ID, nil, ""), 404)
	assertStatus(t, requestJSON(t, app.handler, "GET", "/api/v1/raffles/"+raffle.ID, nil, seller.AccessToken), 200)
	assertStatus(t, requestJSON(t, app.handler, "DELETE", "/api/v1/users/me", map[string]string{"username": seller.User.Username, "pin": "123456"}, seller.AccessToken), 204)
}
