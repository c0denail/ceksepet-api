package domain

import "errors"

var Categories = []Category{
	{ID: "electronics", Name: "Elektronik"},
	{ID: "phone", Name: "Telefon", ParentID: "electronics"},
	{ID: "tablet", Name: "Tablet", ParentID: "electronics"},
	{ID: "computer", Name: "Bilgisayar", ParentID: "electronics"},
	{ID: "fashion", Name: "Moda"},
	{ID: "clothing", Name: "Giyim", ParentID: "fashion"},
	{ID: "shoes", Name: "Ayakkabı", ParentID: "fashion"},
	{ID: "home", Name: "Ev ve Yaşam"},
	{ID: "other", Name: "Diğer"},
}

var Brands = []Brand{
	{ID: "apple", Name: "Apple", CategoryIDs: []string{"phone", "tablet", "computer"}},
	{ID: "samsung", Name: "Samsung", CategoryIDs: []string{"phone", "tablet", "computer"}},
	{ID: "xiaomi", Name: "Xiaomi", CategoryIDs: []string{"phone", "tablet"}},
	{ID: "lenovo", Name: "Lenovo", CategoryIDs: []string{"tablet", "computer"}},
	{ID: "zara", Name: "Zara", CategoryIDs: []string{"clothing", "shoes"}},
	{ID: "adidas", Name: "Adidas", CategoryIDs: []string{"clothing", "shoes"}},
	{ID: "other", Name: "Diğer / Markasız", CategoryIDs: []string{"phone", "tablet", "computer", "clothing", "shoes", "home", "other"}},
}

func CategoryByID(id string) (Category, bool) {
	for _, v := range Categories {
		if v.ID == id {
			return v, true
		}
	}
	return Category{}, false
}

func BrandByID(id string) (Brand, bool) {
	for _, v := range Brands {
		if v.ID == id {
			return v, true
		}
	}
	return Brand{}, false
}

func BrandFits(brandID, categoryID string) bool {
	b, ok := BrandByID(brandID)
	if !ok {
		return false
	}
	for _, id := range b.CategoryIDs {
		if id == categoryID {
			return true
		}
	}
	return false
}

func CategoryIDs(id string) []string {
	ids := []string{id}
	for _, c := range Categories {
		if c.ParentID == id {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// CalculateQuote rounds up to whole tickets without using floating point money.
func CalculateQuote(price, ticketValue, commissionBPS int64) (PriceQuote, error) {
	if price < 1 || price > 100_000_000 || ticketValue < 1 || ticketValue > 1_000_000 || commissionBPS < 0 || commissionBPS > 10_000 {
		return PriceQuote{}, errors.New("invalid pricing parameters")
	}
	earnings := (price + ticketValue - 1) / ticketValue
	commission := (earnings*commissionBPS + 9999) / 10000
	return PriceQuote{PriceKurus: price, TicketValueKurus: ticketValue, CommissionBPS: commissionBPS,
		SellerTickets: earnings, CommissionTickets: commission, TotalTickets: earnings + commission}, nil
}
