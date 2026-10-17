package store

import (
	"context"

	"github.com/zxor-org/OronBox-Server/internal/store/resources"
)

type AdminResourceQuery = resources.AdminResourceQuery
type AdminPublication = resources.AdminPublication
type AdminResourceItem = resources.AdminResourceItem
type AdminResourcePage = resources.AdminResourcePage
type AdminRevision = resources.AdminRevision
type AdminExternalBinding = resources.AdminExternalBinding
type AdminExternalBindingEntry = resources.AdminExternalBindingEntry
type AdminResourceEvent = resources.AdminResourceEvent
type AdminResourceDetail = resources.AdminResourceDetail
type AdminLink = resources.AdminLink
type AdminRevisionDetail = resources.AdminRevisionDetail
type AdminArtifact = resources.AdminArtifact
type AdminArtifactDevice = resources.AdminArtifactDevice
type AdminMedia = resources.AdminMedia
type AdminResourceActionResult = resources.AdminResourceActionResult

var ErrAdminResourceNotFound = resources.ErrAdminResourceNotFound
var ErrAdminResourceConflict = resources.ErrAdminResourceConflict

func (s *Store) resourceRepository() *resources.Repository { return resources.New(s.db) }
func (s *Store) AdminResources(ctx context.Context, q AdminResourceQuery) (AdminResourcePage, error) {
	return s.resourceRepository().AdminResources(ctx, q)
}
func (s *Store) AdminResource(ctx context.Context, id string) (AdminResourceDetail, error) {
	return s.resourceRepository().AdminResource(ctx, id)
}
func (s *Store) AdminResourceRevision(ctx context.Context, resourceID, revisionID string) (AdminRevisionDetail, error) {
	return s.resourceRepository().AdminResourceRevision(ctx, resourceID, revisionID)
}
func (s *Store) AdminManageResource(ctx context.Context, id, action, reason string, actor AdminSession) (AdminResourceActionResult, error) {
	return s.resourceRepository().AdminManageResource(ctx, id, action, reason, actor)
}
