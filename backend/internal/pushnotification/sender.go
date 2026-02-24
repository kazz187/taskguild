package pushnotification

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/kazz187/taskguild/backend/internal/config"
	"github.com/kazz187/taskguild/backend/internal/pushsubscription"
)

type NotificationPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url,omitempty"`
	Tag   string `json:"tag,omitempty"`
}

type Sender struct {
	vapidEnv *config.VAPIDEnv
	repo     pushsubscription.Repository
}

func NewSender(vapidEnv *config.VAPIDEnv, repo pushsubscription.Repository) *Sender {
	return &Sender{
		vapidEnv: vapidEnv,
		repo:     repo,
	}
}

func (s *Sender) SendToAll(ctx context.Context, payload *NotificationPayload) {
	if s.vapidEnv.VAPIDPrivateKey == "" || s.vapidEnv.VAPIDPublicKey == "" {
		slog.Warn("push notification: VAPID keys not configured, skipping")
		return
	}

	subs, err := s.repo.List(ctx)
	if err != nil {
		slog.Error("push notification: failed to list subscriptions", "error", err)
		return
	}

	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("push notification: failed to marshal payload", "error", err)
		return
	}

	for _, sub := range subs {
		s.sendToSubscription(ctx, sub, data)
	}
}

func (s *Sender) sendToSubscription(ctx context.Context, sub *pushsubscription.Subscription, data []byte) {
	wpSub := &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.P256dhKey,
			Auth:   sub.AuthKey,
		},
	}

	resp, err := webpush.SendNotification(data, wpSub, &webpush.Options{
		VAPIDPublicKey:  s.vapidEnv.VAPIDPublicKey,
		VAPIDPrivateKey: s.vapidEnv.VAPIDPrivateKey,
		Subscriber:      s.vapidEnv.VAPIDContact,
		TTL:             86400,
	})
	if err != nil {
		slog.Error("push notification: failed to send", "endpoint", sub.Endpoint, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusGone {
		slog.Info("push notification: subscription expired, removing", "endpoint", sub.Endpoint)
		if err := s.repo.Delete(ctx, sub.ID); err != nil {
			slog.Error("push notification: failed to delete expired subscription", "id", sub.ID, "error", err)
		}
		return
	}

	if resp.StatusCode >= 400 {
		slog.Warn("push notification: unexpected status", "endpoint", sub.Endpoint, "status", resp.StatusCode)
	}
}
