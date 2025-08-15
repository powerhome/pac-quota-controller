package namespace

import (
	"context"
)

//go:generate mockery

// NamespaceSelector defines the interface for namespace selection operations
type NamespaceSelector interface {
	GetSelectedNamespaces(ctx context.Context) ([]string, error)
	DetermineNamespaceChanges(
		ctx context.Context,
		previousNamespaces []string,
	) (added []string, removed []string, err error)
}
