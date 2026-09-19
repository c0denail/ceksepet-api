package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type WebhookNotifier struct {
	URL    string
	Token  string
	Client *http.Client
}

func NewWebhookNotifier(url, token string) *WebhookNotifier {
	return &WebhookNotifier{
		URL: url, Token: token,
		Client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *WebhookNotifier) Send(ctx context.Context, channel, destination, code string) error {
	body, err := json.Marshal(map[string]string{
		"channel": channel, "destination": destination, "code": code,
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, n.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if n.Token != "" {
		request.Header.Set("Authorization", "Bearer "+n.Token)
	}
	response, err := n.Client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("OTP webhook returned status %d", response.StatusCode)
	}
	return nil
}
