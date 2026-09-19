package domain

import "time"

type ReportReason struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

var SellerReportReasons = []ReportReason{
	{ID: "misleading_listing", Label: "Yanıltıcı ürün veya satıcı bilgisi"},
	{ID: "inappropriate_behavior", Label: "Uygunsuz davranış veya iletişim"},
	{ID: "suspected_fraud", Label: "Dolandırıcılık şüphesi"},
}

type SellerReport struct {
	ID          string     `json:"id" bson:"_id"`
	ReporterID  string     `json:"reporterId" bson:"reporter_id"`
	SellerID    string     `json:"sellerId" bson:"seller_id"`
	ReasonID    string     `json:"reasonId" bson:"reason_id"`
	Description string     `json:"description" bson:"description"`
	Status      string     `json:"status" bson:"status"`
	CreatedAt   time.Time  `json:"createdAt" bson:"created_at"`
	ResolvedAt  *time.Time `json:"resolvedAt,omitempty" bson:"resolved_at,omitempty"`
	Resolution  string     `json:"resolution,omitempty" bson:"resolution,omitempty"`
	ResolvedBy  string     `json:"-" bson:"resolved_by,omitempty"`
}
