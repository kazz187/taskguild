package repositoryimpl

import (
	"context"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/backend/internal/pushsubscription"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
	"github.com/kazz187/taskguild/backend/pkg/storage"
)

const pushSubscriptionsPrefix = "push_subscriptions"

type YAMLRepository struct {
	storage storage.Storage
}

func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func path(id string) string {
	return fmt.Sprintf("%s/%s.yaml", pushSubscriptionsPrefix, id)
}

func (r *YAMLRepository) Create(ctx context.Context, s *pushsubscription.Subscription) error {
	exists, err := r.storage.Exists(ctx, path(s.ID))
	if err != nil {
		return cerr.WrapStorageWriteError("push_subscription", err)
	}
	if exists {
		return cerr.NewError(cerr.AlreadyExists, "push subscription already exists", nil)
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal push subscription: %w", err))
	}
	if err := r.storage.Write(ctx, path(s.ID), data); err != nil {
		return cerr.WrapStorageWriteError("push_subscription", err)
	}
	return nil
}

func (r *YAMLRepository) Get(ctx context.Context, id string) (*pushsubscription.Subscription, error) {
	data, err := r.storage.Read(ctx, path(id))
	if err != nil {
		return nil, cerr.WrapStorageReadError("push_subscription", err)
	}
	var s pushsubscription.Subscription
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to unmarshal push subscription: %w", err))
	}
	return &s, nil
}

func (r *YAMLRepository) List(ctx context.Context) ([]*pushsubscription.Subscription, error) {
	paths, err := r.storage.List(ctx, pushSubscriptionsPrefix)
	if err != nil {
		return nil, cerr.WrapStorageReadError("push_subscriptions", err)
	}

	sort.Strings(paths)

	var all []*pushsubscription.Subscription
	for _, p := range paths {
		data, err := r.storage.Read(ctx, p)
		if err != nil {
			continue
		}
		var s pushsubscription.Subscription
		if err := yaml.Unmarshal(data, &s); err != nil {
			continue
		}
		all = append(all, &s)
	}
	return all, nil
}

func (r *YAMLRepository) Delete(ctx context.Context, id string) error {
	if err := r.storage.Delete(ctx, path(id)); err != nil {
		return cerr.WrapStorageDeleteError("push_subscription", err)
	}
	return nil
}

func (r *YAMLRepository) FindByEndpoint(ctx context.Context, endpoint string) (*pushsubscription.Subscription, error) {
	paths, err := r.storage.List(ctx, pushSubscriptionsPrefix)
	if err != nil {
		return nil, cerr.WrapStorageReadError("push_subscriptions", err)
	}

	for _, p := range paths {
		data, err := r.storage.Read(ctx, p)
		if err != nil {
			continue
		}
		var s pushsubscription.Subscription
		if err := yaml.Unmarshal(data, &s); err != nil {
			continue
		}
		if s.Endpoint == endpoint {
			return &s, nil
		}
	}
	return nil, cerr.NewError(cerr.NotFound, "push subscription not found", nil)
}

func (r *YAMLRepository) DeleteByEndpoint(ctx context.Context, endpoint string) error {
	s, err := r.FindByEndpoint(ctx, endpoint)
	if err != nil {
		return err
	}
	return r.Delete(ctx, s.ID)
}
