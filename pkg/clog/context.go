package clog

import (
	"context"
	"sync"
)

type ctxSlog struct {
	mu         sync.RWMutex
	attributes map[string]any
}

type ctxSlogKey struct{}

func ContextWithSlog(ctx context.Context) context.Context {
	ctxSlog := &ctxSlog{
		attributes: make(map[string]any),
	}
	return context.WithValue(ctx, ctxSlogKey{}, ctxSlog)
}

func AddAttribute(ctx context.Context, key string, value any) {
	l, ok := ctx.Value(ctxSlogKey{}).(*ctxSlog)
	if !ok {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attributes[key] = value
}

func AddAttributes(ctx context.Context, attributes map[string]any) {
	l, ok := ctx.Value(ctxSlogKey{}).(*ctxSlog)
	if !ok {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	mergeMaps(l.attributes, attributes)
}

func GetAttribute[T any](ctx context.Context, key string) T {
	l, ok := ctx.Value(ctxSlogKey{}).(*ctxSlog)
	if !ok {
		return *new(T)
	}
	l.mu.RLock()
	iVal, ok := l.attributes[key]
	l.mu.RUnlock()
	if !ok {
		return *new(T)
	}
	stack, ok := iVal.(T)
	if !ok {
		return *new(T)
	}
	return stack
}

func mergeMaps(dst, src map[string]any) {
	for k, v := range src {
		if vMap, ok := v.(map[string]any); ok {
			if dstMap, ok := dst[k].(map[string]any); ok {
				mergeMaps(dstMap, vMap)
			} else {
				dst[k] = vMap
			}
		} else {
			dst[k] = v
		}
	}
}

const (
	ErrorAttributeKey = "error.message"
	StackAttributeKey = "error.stack"
)

func AddError(ctx context.Context, err error) {
	AddAttribute(ctx, ErrorAttributeKey, err)
}

func GetError(ctx context.Context) error {
	return GetAttribute[error](ctx, ErrorAttributeKey)
}

func AddStack(ctx context.Context, stack string) {
	AddAttribute(ctx, StackAttributeKey, stack)
}

func GetStack(ctx context.Context) string {
	return GetAttribute[string](ctx, StackAttributeKey)
}

func (c *ctxSlog) getAttributes() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	copied := make(map[string]any, len(c.attributes))
	for k, v := range c.attributes {
		copied[k] = v
	}
	return copied
}

func GetAttributes(ctx context.Context) map[string]any {
	l, ok := ctx.Value(ctxSlogKey{}).(*ctxSlog)
	if !ok {
		return nil
	}
	return l.getAttributes()
}
