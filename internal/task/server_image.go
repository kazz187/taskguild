package task

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

func (s *Server) UploadTaskImage(ctx context.Context, req *connect.Request[taskguildv1.UploadTaskImageRequest]) (*connect.Response[taskguildv1.UploadTaskImageResponse], error) {
	if s.imageStore == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("image storage not configured"))
	}

	msg := req.Msg

	// Validate media type.
	if !ValidImageMediaTypes[msg.MediaType] {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unsupported media type: %s", msg.MediaType))
	}

	// Validate size.
	if len(msg.Data) > MaxImageSizeBytes {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("image too large: %d bytes (max %d)", len(msg.Data), MaxImageSizeBytes))
	}

	// Look up task to get project ID.
	t, err := s.repo.Get(ctx, msg.TaskId)
	if err != nil {
		return nil, err
	}

	meta, err := s.imageStore.Upload(ctx, t.ProjectID, t.ID, msg.Filename, msg.MediaType, msg.Data)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("upload image: %w", err))
	}

	return connect.NewResponse(&taskguildv1.UploadTaskImageResponse{
		Image: imageMetaToProto(meta),
	}), nil
}

func (s *Server) GetTaskImage(ctx context.Context, req *connect.Request[taskguildv1.GetTaskImageRequest]) (*connect.Response[taskguildv1.GetTaskImageResponse], error) {
	if s.imageStore == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("image storage not configured"))
	}

	t, err := s.repo.Get(ctx, req.Msg.TaskId)
	if err != nil {
		return nil, err
	}

	meta, data, err := s.imageStore.Get(ctx, t.ProjectID, t.ID, req.Msg.ImageId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("image not found: %w", err))
	}

	return connect.NewResponse(&taskguildv1.GetTaskImageResponse{
		Image: imageMetaToProto(meta),
		Data:  data,
	}), nil
}

func (s *Server) ListTaskImages(ctx context.Context, req *connect.Request[taskguildv1.ListTaskImagesRequest]) (*connect.Response[taskguildv1.ListTaskImagesResponse], error) {
	if s.imageStore == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("image storage not configured"))
	}

	t, err := s.repo.Get(ctx, req.Msg.TaskId)
	if err != nil {
		return nil, err
	}

	metas, err := s.imageStore.List(ctx, t.ProjectID, t.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list images: %w", err))
	}

	images := make([]*taskguildv1.TaskImage, len(metas))
	for i, m := range metas {
		images[i] = imageMetaToProto(m)
	}

	return connect.NewResponse(&taskguildv1.ListTaskImagesResponse{
		Images: images,
	}), nil
}

func (s *Server) DeleteTaskImage(ctx context.Context, req *connect.Request[taskguildv1.DeleteTaskImageRequest]) (*connect.Response[taskguildv1.DeleteTaskImageResponse], error) {
	if s.imageStore == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("image storage not configured"))
	}

	t, err := s.repo.Get(ctx, req.Msg.TaskId)
	if err != nil {
		return nil, err
	}

	if err := s.imageStore.Delete(ctx, t.ProjectID, t.ID, req.Msg.ImageId); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete image: %w", err))
	}

	return connect.NewResponse(&taskguildv1.DeleteTaskImageResponse{}), nil
}

func imageMetaToProto(m *ImageMeta) *taskguildv1.TaskImage {
	return &taskguildv1.TaskImage{
		Id:        m.ID,
		Filename:  m.Filename,
		MediaType: m.MediaType,
		SizeBytes: m.SizeBytes,
		CreatedAt: timestamppb.New(m.CreatedAt),
	}
}
