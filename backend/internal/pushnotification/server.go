package pushnotification

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/backend/internal/config"
	"github.com/kazz187/taskguild/backend/internal/pushsubscription"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
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

func (s *Server) GetVapidPublicKey(_ context.Context, _ *connect.Request[taskguildv1.GetVapidPublicKeyRequest]) (*connect.Response[taskguildv1.GetVapidPublicKeyResponse], error) {
	if s.vapidEnv.VAPIDPublicKey == "" {
		return nil, cerr.NewError(cerr.FailedPrecondition, "VAPID keys not configured", nil).ConnectError()
	}
	return connect.NewResponse(&taskguildv1.GetVapidPublicKeyResponse{
		PublicKey: s.vapidEnv.VAPIDPublicKey,
	}), nil
}

func (s *Server) RegisterPushSubscription(ctx context.Context, req *connect.Request[taskguildv1.RegisterPushSubscriptionRequest]) (*connect.Response[taskguildv1.RegisterPushSubscriptionResponse], error) {
	if req.Msg.Endpoint == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "endpoint is required", nil).ConnectError()
	}
	if req.Msg.P256DhKey == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "p256dh_key is required", nil).ConnectError()
	}
	if req.Msg.AuthKey == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "auth_key is required", nil).ConnectError()
	}

	// Idempotent: if endpoint already exists, update it.
	existing, err := s.repo.FindByEndpoint(ctx, req.Msg.Endpoint)
	if err == nil && existing != nil {
		existing.P256dhKey = req.Msg.P256DhKey
		existing.AuthKey = req.Msg.AuthKey
		if delErr := s.repo.Delete(ctx, existing.ID); delErr != nil {
			return nil, delErr
		}
		if crErr := s.repo.Create(ctx, existing); crErr != nil {
			return nil, crErr
		}
		return connect.NewResponse(&taskguildv1.RegisterPushSubscriptionResponse{}), nil
	}

	sub := &pushsubscription.Subscription{
		ID:        ulid.Make().String(),
		Endpoint:  req.Msg.Endpoint,
		P256dhKey: req.Msg.P256DhKey,
		AuthKey:   req.Msg.AuthKey,
		CreatedAt: time.Now(),
	}
	if err := s.repo.Create(ctx, sub); err != nil {
		return nil, err
	}

	return connect.NewResponse(&taskguildv1.RegisterPushSubscriptionResponse{}), nil
}

func (s *Server) UnregisterPushSubscription(ctx context.Context, req *connect.Request[taskguildv1.UnregisterPushSubscriptionRequest]) (*connect.Response[taskguildv1.UnregisterPushSubscriptionResponse], error) {
	if req.Msg.Endpoint == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "endpoint is required", nil).ConnectError()
	}

	if err := s.repo.DeleteByEndpoint(ctx, req.Msg.Endpoint); err != nil {
		return nil, err
	}

	return connect.NewResponse(&taskguildv1.UnregisterPushSubscriptionResponse{}), nil
}

func (s *Server) SendTestNotification(ctx context.Context, _ *connect.Request[taskguildv1.SendTestNotificationRequest]) (*connect.Response[taskguildv1.SendTestNotificationResponse], error) {
	s.sender.SendToAll(ctx, &NotificationPayload{
		Title: "TaskGuild Test",
		Body:  "Push notifications are working!",
	})
	return connect.NewResponse(&taskguildv1.SendTestNotificationResponse{}), nil
}
