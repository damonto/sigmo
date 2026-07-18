package webpush

import "time"

type updateRequest struct {
	Enabled *bool `json:"enabled"`
}

type renameRequest struct {
	Label string `json:"label"`
}

type overviewResponse struct {
	Enabled       bool                   `json:"enabled"`
	PublicKey     string                 `json:"publicKey"`
	Subscriptions []subscriptionResponse `json:"subscriptions"`
}

type subscriptionResponse struct {
	ID        string    `json:"id"`
	Endpoint  string    `json:"endpoint"`
	Label     string    `json:"label"`
	UserAgent string    `json:"userAgent"`
	Platform  string    `json:"platform"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
