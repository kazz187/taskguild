package task

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/pkg/storage"
)

// ImageMeta is the metadata for a stored image.
type ImageMeta struct {
	ID        string    `yaml:"id"`
	Filename  string    `yaml:"filename"`
	MediaType string    `yaml:"media_type"`
	SizeBytes int64     `yaml:"size_bytes"`
	CreatedAt time.Time `yaml:"created_at"`
}

// ImageStore provides CRUD operations for task images.
type ImageStore interface {
	Upload(ctx context.Context, projectID, taskID string, filename, mediaType string, data []byte) (*ImageMeta, error)
	Get(ctx context.Context, projectID, taskID, imageID string) (*ImageMeta, []byte, error)
	List(ctx context.Context, projectID, taskID string) ([]*ImageMeta, error)
	Delete(ctx context.Context, projectID, taskID, imageID string) error
}

// storageImageStore implements ImageStore using the storage.Storage interface.
type storageImageStore struct {
	store storage.Storage
}

// NewImageStore creates an ImageStore backed by the given storage.
func NewImageStore(store storage.Storage) ImageStore {
	return &storageImageStore{store: store}
}

func imageDataPath(projectID, taskID, imageID string) string {
	return fmt.Sprintf("projects/%s/%s/images/%s.dat", projectID, taskID, imageID)
}

func imageMetaPath(projectID, taskID, imageID string) string {
	return fmt.Sprintf("projects/%s/%s/images/%s.meta.yaml", projectID, taskID, imageID)
}

func imagesPrefix(projectID, taskID string) string {
	return fmt.Sprintf("projects/%s/%s/images/", projectID, taskID)
}

// ValidImageMediaTypes is the set of accepted image media types.
var ValidImageMediaTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

const MaxImageSizeBytes = 10 * 1024 * 1024 // 10MB

func (s *storageImageStore) Upload(ctx context.Context, projectID, taskID string, filename, mediaType string, data []byte) (*ImageMeta, error) {
	// Determine next image ID by listing existing images.
	existing, err := s.List(ctx, projectID, taskID)
	if err != nil {
		return nil, fmt.Errorf("list existing images: %w", err)
	}

	nextID := 1

	for _, img := range existing {
		n, _ := strconv.Atoi(img.ID)
		if n >= nextID {
			nextID = n + 1
		}
	}

	id := strconv.Itoa(nextID)
	meta := &ImageMeta{
		ID:        id,
		Filename:  filename,
		MediaType: mediaType,
		SizeBytes: int64(len(data)),
		CreatedAt: time.Now().UTC(),
	}

	// Write image data.
	if err := s.store.Write(ctx, imageDataPath(projectID, taskID, id), data); err != nil {
		return nil, fmt.Errorf("write image data: %w", err)
	}

	// Write metadata.
	metaBytes, err := yaml.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal image meta: %w", err)
	}

	if err := s.store.Write(ctx, imageMetaPath(projectID, taskID, id), metaBytes); err != nil {
		return nil, fmt.Errorf("write image meta: %w", err)
	}

	return meta, nil
}

func (s *storageImageStore) Get(ctx context.Context, projectID, taskID, imageID string) (*ImageMeta, []byte, error) {
	metaBytes, err := s.store.Read(ctx, imageMetaPath(projectID, taskID, imageID))
	if err != nil {
		return nil, nil, fmt.Errorf("read image meta: %w", err)
	}

	var meta ImageMeta
	if err := yaml.Unmarshal(metaBytes, &meta); err != nil {
		return nil, nil, fmt.Errorf("unmarshal image meta: %w", err)
	}

	data, err := s.store.Read(ctx, imageDataPath(projectID, taskID, imageID))
	if err != nil {
		return nil, nil, fmt.Errorf("read image data: %w", err)
	}

	return &meta, data, nil
}

func (s *storageImageStore) List(ctx context.Context, projectID, taskID string) ([]*ImageMeta, error) {
	files, err := s.store.List(ctx, imagesPrefix(projectID, taskID))
	if err != nil {
		// Empty directory is not an error — just no images.
		return nil, nil
	}

	var metas []*ImageMeta

	for _, f := range files {
		if !strings.HasSuffix(f, ".meta.yaml") {
			continue
		}

		metaBytes, err := s.store.Read(ctx, f)
		if err != nil {
			continue
		}

		var meta ImageMeta
		if yaml.Unmarshal(metaBytes, &meta) == nil {
			metas = append(metas, &meta)
		}
	}

	sort.Slice(metas, func(i, j int) bool {
		ni, _ := strconv.Atoi(metas[i].ID)
		nj, _ := strconv.Atoi(metas[j].ID)

		return ni < nj
	})

	return metas, nil
}

func (s *storageImageStore) Delete(ctx context.Context, projectID, taskID, imageID string) error {
	var firstErr error
	err := s.store.Delete(ctx, imageDataPath(projectID, taskID, imageID))
	if err != nil {
		firstErr = fmt.Errorf("delete image data: %w", err)
	}

	err = s.store.Delete(ctx, imageMetaPath(projectID, taskID, imageID))
	if err != nil && firstErr == nil {
		firstErr = fmt.Errorf("delete image meta: %w", err)
	}

	return firstErr
}
