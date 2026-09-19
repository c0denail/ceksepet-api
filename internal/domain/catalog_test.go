package domain

import "testing"

func TestPriceQuoteUsesWholeTicketsAndRoundsUp(t *testing.T) {
	for _, test := range []struct{ price, value, bps, seller, commission, total int64 }{
		{234500, 100, 1000, 2345, 235, 2580}, {10001, 100, 1000, 101, 11, 112},
		{1, 100, 0, 1, 0, 1}, {100_000_000, 1, 10000, 100_000_000, 100_000_000, 200_000_000},
	} {
		q, err := CalculateQuote(test.price, test.value, test.bps)
		if err != nil || q.SellerTickets != test.seller || q.CommissionTickets != test.commission || q.TotalTickets != test.total {
			t.Fatalf("quote(%+v)=%+v, %v", test, q, err)
		}
	}
	for _, test := range [][3]int64{{0, 100, 1000}, {-1, 100, 1000}, {100_000_001, 100, 1000}, {100, 0, 1000}, {100, 100, -1}, {100, 100, 10001}} {
		if _, err := CalculateQuote(test[0], test[1], test[2]); err == nil {
			t.Fatalf("accepted invalid quote %v", test)
		}
	}
}
