package pushnotification

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/internal/config"
	"github.com/kazz187/taskguild/internal/pushsubscription"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.PushNotificationServiceHandler = (*Server)(nil)

type Server struct {
	vapidEnv *config.VAPIDEnv
	repo     pushsubscription.Repository
	sender   *Sender
}

func NewServer(vapidEnv *config.VAPIDEnv, repo pushsubscription.Repository, sender *Sender) *Server {
	return &Server{
		vapidEnv: vapidEnv,
		repo:     repo,
		sender:   sender,
	}
}

func (s *Server) GetVapidPublicKey(_ context.Context, _ *taskguildv1.GetVapidPublicKeyRequest) (*taskguildv1.GetVapidPublicKeyResponse, error) {
	if s.vapidEnv.VAPIDPublicKey == "" {
		return nil, cerr.NewError(cerr.FailedPrecondition, "VAPID keys not configured", nil).ConnectError()
	}

	return &taskguildv1.GetVapidPublicKeyResponse{
		PublicKey: s.vapidEnv.VAPIDPublicKey,
	}, nil
}

func (s *Server) RegisterPushSubscription(ctx context.Context, req *taskguildv1.RegisterPushSubscriptionRequest) (*taskguildv1.RegisterPushSubscriptionResponse, error) {
	if req.GetEndpoint() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "endpoint is required", nil).ConnectError()
	}

	if req.GetP256DhKey() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "p256dh_key is required", nil).ConnectError()
	}

	if req.GetAuthKey() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "auth_key is required", nil).ConnectError()
	}

	// Idempotent: if endpoint already exists, update it.
	existing, err := s.repo.FindByEndpoint(ctx, req.GetEndpoint())
	if err == nil && existing != nil {
		existing.P256dhKey = req.GetP256DhKey()

		existing.AuthKey = req.GetAuthKey()

		delErr := s.repo.Delete(ctx, existing.ID)
		if delErr != nil {
			return nil, delErr
		}

		crErr := s.repo.Create(ctx, existing)
		if crErr != nil {
			return nil, crErr
		}

		return &taskguildv1.RegisterPushSubscriptionResponse{}, nil
	}

	sub := &pushsubscription.Subscription{
		ID:        ulid.Make().String(),
		Endpoint:  req.GetEndpoint(),
		P256dhKey: req.GetP256DhKey(),
		AuthKey:   req.GetAuthKey(),
		CreatedAt: time.Now(),
	}
	if err := s.repo.Create(ctx, sub); err != nil {
		return nil, err
	}

	return &taskguildv1.RegisterPushSubscriptionResponse{}, nil
}

func (s *Server) UnregisterPushSubscription(ctx context.Context, req *taskguildv1.UnregisterPushSubscriptionRequest) (*taskguildv1.UnregisterPushSubscriptionResponse, error) {
	if req.GetEndpoint() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "endpoint is required", nil).ConnectError()
	}

	err := s.repo.DeleteByEndpoint(ctx, req.GetEndpoint())
	if err != nil {
		return nil, err
	}

	return &taskguildv1.UnregisterPushSubscriptionResponse{}, nil
}

func (s *Server) SendTestNotification(ctx context.Context, _ *taskguildv1.SendTestNotificationRequest) (*taskguildv1.SendTestNotificationResponse, error) {
	s.sender.SendToAll(ctx, &NotificationPayload{
		Title: "TaskGuild Test",
		Body:  "Push notifications are working!",
	})

	return &taskguildv1.SendTestNotificationResponse{}, nil
}
