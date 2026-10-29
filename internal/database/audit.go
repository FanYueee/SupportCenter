package database

import "context"

type RequestInfo struct {
	IP        string
	UserAgent string
}

type requestInfoKey struct{}

func WithRequestInfo(ctx context.Context, info RequestInfo) context.Context {
	return context.WithValue(ctx, requestInfoKey{}, info)
}

func requestInfo(ctx context.Context) RequestInfo {
	info, _ := ctx.Value(requestInfoKey{}).(RequestInfo)
	return info
}
