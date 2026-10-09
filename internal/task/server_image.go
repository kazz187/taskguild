package task

import (
	"context"

	"connectrpc.com/connect/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

func (s *Server) UploadTaskImage(ctx context.Context, req *taskguildv1.UploadTaskImageRequest) (*taskguildv1.UploadTaskImageResponse, error) {
	if s.imageStore == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, "image storage not configured")
	}

	msg := req

	// Validate media type.
	if !ValidImageMediaTypes[msg.GetMediaType()] {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "unsupported media type: %s", msg.GetMediaType())
	}

	// Validate size.
	if len(msg.GetData()) > MaxImageSizeBytes {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "image too large: %d bytes (max %d)", len(msg.GetData()), MaxImageSizeBytes)
	}

	// Look up task to get project ID.
	t, err := s.repo.Get(ctx, msg.GetTaskId())
	if err != nil {
		return nil, err
	}

	meta, err := s.imageStore.Upload(ctx, t.ProjectID, t.ID, msg.GetFilename(), msg.GetMediaType(), msg.GetData())
	if err != nil {
		return nil, connect.Errorf(connect.CodeInternal, "upload image: %v", err).WithCause(err)
	}

	return &taskguildv1.UploadTaskImageResponse{
		Image: imageMetaToProto(meta),
	}, nil
}

func (s *Server) GetTaskImage(ctx context.Context, req *taskguildv1.GetTaskImageRequest) (*taskguildv1.GetTaskImageResponse, error) {
	if s.imageStore == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, "image storage not configured")
	}

	t, err := s.repo.Get(ctx, req.GetTaskId())
	if err != nil {
		return nil, err
	}

	meta, data, err := s.imageStore.Get(ctx, t.ProjectID, t.ID, req.GetImageId())
	if err != nil {
		return nil, connect.Errorf(connect.CodeNotFound, "image not found: %v", err).WithCause(err)
	}

	return &taskguildv1.GetTaskImageResponse{
		Image: imageMetaToProto(meta),
		Data:  data,
	}, nil
}

func (s *Server) ListTaskImages(ctx context.Context, req *taskguildv1.ListTaskImagesRequest) (*taskguildv1.ListTaskImagesResponse, error) {
	if s.imageStore == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, "image storage not configured")
	}

	t, err := s.repo.Get(ctx, req.GetTaskId())
	if err != nil {
		return nil, err
	}

	metas, err := s.imageStore.List(ctx, t.ProjectID, t.ID)
	if err != nil {
		return nil, connect.Errorf(connect.CodeInternal, "list images: %v", err).WithCause(err)
	}

	images := make([]*taskguildv1.TaskImage, len(metas))
	for i, m := range metas {
		images[i] = imageMetaToProto(m)
	}

	return &taskguildv1.ListTaskImagesResponse{
		Images: images,
	}, nil
}

func (s *Server) DeleteTaskImage(ctx context.Context, req *taskguildv1.DeleteTaskImageRequest) (*taskguildv1.DeleteTaskImageResponse, error) {
	if s.imageStore == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, "image storage not configured")
	}

	t, err := s.repo.Get(ctx, req.GetTaskId())
	if err != nil {
		return nil, err
	}

	if err := s.imageStore.Delete(ctx, t.ProjectID, t.ID, req.GetImageId()); err != nil {
		return nil, connect.Errorf(connect.CodeInternal, "delete image: %v", err).WithCause(err)
	}

	return &taskguildv1.DeleteTaskImageResponse{}, nil
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
