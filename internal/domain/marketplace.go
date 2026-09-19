package domain

import "time"

const (
	RafflePending   = "pending"
	RaffleActive    = "active"
	RaffleCompleted = "completed"
	RaffleRejected  = "rejected"
)

type Category struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parentId,omitempty"`
}

type Brand struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	CategoryIDs []string `json:"categoryIds"`
}

// Money is represented in kuruş; tickets are always whole integers.
type PriceQuote struct {
	PriceKurus        int64 `json:"priceKurus" bson:"price_kurus"`
	TicketValueKurus  int64 `json:"ticketValueKurus" bson:"ticket_value_kurus"`
	CommissionBPS     int64 `json:"commissionBps" bson:"commission_bps"`
	SellerTickets     int64 `json:"sellerTickets" bson:"seller_tickets"`
	CommissionTickets int64 `json:"commissionTickets" bson:"commission_tickets"`
	TotalTickets      int64 `json:"totalTickets" bson:"total_tickets"`
}

type Raffle struct {
	ID              string     `json:"id" bson:"_id"`
	SellerID        string     `json:"sellerId" bson:"seller_id"`
	Title           string     `json:"title" bson:"title"`
	Description     string     `json:"description" bson:"description"`
	CategoryID      string     `json:"categoryId" bson:"category_id"`
	BrandID         string     `json:"brandId" bson:"brand_id"`
	ShippingPayer   string     `json:"shippingPayer" bson:"shipping_payer"`
	PhotoIDs        []string   `json:"photoIds" bson:"photo_ids"`
	Quote           PriceQuote `json:"quote" bson:"quote"`
	SoldTickets     int64      `json:"soldTickets" bson:"sold_tickets"`
	Status          string     `json:"status" bson:"status"`
	RejectionReason string     `json:"rejectionReason,omitempty" bson:"rejection_reason,omitempty"`
	CreatedAt       time.Time  `json:"createdAt" bson:"created_at"`
	PublishedAt     *time.Time `json:"publishedAt,omitempty" bson:"published_at,omitempty"`
}

type Photo struct {
	ID          string    `bson:"_id"`
	OwnerID     string    `bson:"owner_id"`
	RaffleID    string    `bson:"raffle_id"`
	ContentType string    `bson:"content_type"`
	Data        []byte    `bson:"data"`
	CreatedAt   time.Time `bson:"created_at"`
}

type Participation struct {
	ID        string    `json:"id" bson:"_id"`
	UserID    string    `json:"-" bson:"user_id"`
	RaffleID  string    `json:"raffleId" bson:"raffle_id"`
	Tickets   int64     `json:"tickets" bson:"tickets"`
	UpdatedAt time.Time `json:"updatedAt" bson:"updated_at"`
}

// TicketEntry is both an immutable ledger entry and the receipt for retries.
type TicketEntry struct {
	ID           string    `json:"id" bson:"_id"`
	UserID       string    `json:"-" bson:"user_id"`
	Key          string    `json:"-" bson:"key"`
	Kind         string    `json:"kind" bson:"kind"`
	RaffleID     string    `json:"raffleId,omitempty" bson:"raffle_id"`
	Delta        int64     `json:"delta" bson:"delta"`
	BalanceAfter int64     `json:"balanceAfter" bson:"balance_after"`
	SoldTickets  int64     `json:"soldTickets,omitempty" bson:"sold_tickets"`
	RaffleStatus string    `json:"raffleStatus,omitempty" bson:"raffle_status"`
	Reason       string    `json:"reason,omitempty" bson:"reason"`
	Actor        string    `json:"-" bson:"actor"`
	CreatedAt    time.Time `json:"createdAt" bson:"created_at"`
}

type Question struct {
	ID        string    `json:"id" bson:"_id"`
	RaffleID  string    `json:"raffleId" bson:"raffle_id"`
	UserID    string    `json:"-" bson:"user_id"`
	ParentID  string    `json:"parentId,omitempty" bson:"parent_id"`
	Body      string    `json:"body" bson:"body"`
	CreatedAt time.Time `json:"createdAt" bson:"created_at"`
}

type Review struct {
	ID        string    `json:"id" bson:"_id"`
	SellerID  string    `json:"sellerId" bson:"seller_id"`
	UserID    string    `json:"-" bson:"user_id"`
	RaffleID  string    `json:"raffleId" bson:"raffle_id"`
	Rating    int       `json:"rating" bson:"rating"`
	Body      string    `json:"body" bson:"body"`
	CreatedAt time.Time `json:"createdAt" bson:"created_at"`
}

type Notification struct {
	ID        string     `json:"id" bson:"_id"`
	UserID    string     `json:"-" bson:"user_id"`
	Kind      string     `json:"kind" bson:"kind"`
	RaffleID  string     `json:"raffleId" bson:"raffle_id"`
	Message   string     `json:"message" bson:"message"`
	ReadAt    *time.Time `json:"readAt" bson:"read_at"`
	CreatedAt time.Time  `json:"createdAt" bson:"created_at"`
}
