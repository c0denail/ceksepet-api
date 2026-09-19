package httpapi

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"ceksepet-api/internal/store"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMongoMarketplaceHTTPScreens(t *testing.T) {
	marketplaceScreensEndToEnd(t, mongoTestApp(t))
}

func TestMongoProfileHTTPScreens(t *testing.T) {
	profileScreens(t, mongoTestApp(t))
}

func TestMongoCancelledRaffleHTTP(t *testing.T) {
	cancelledRaffleRefundAndFilters(t, mongoTestApp(t))
}

func mongoTestApp(t *testing.T) *testApp {
	t.Helper()
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("MONGODB_TEST_URI is not set")
	}
	database := fmt.Sprintf("ceksepet_http_test_%d", time.Now().UnixNano())
	dataStore, err := store.Open(uri, database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		client, err := mongo.Connect(options.Client().ApplyURI(uri))
		if err != nil {
			t.Error(err)
			return
		}
		defer client.Disconnect(ctx)
		if err := client.Database(database).Drop(ctx); err != nil {
			t.Error(err)
		}
		if err := dataStore.Close(); err != nil {
			t.Error(err)
		}
	})
	return newTestAppWithStore(t, dataStore)
}
