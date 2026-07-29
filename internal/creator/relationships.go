package creator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *Service) ResourceRelationships(ctx context.Context, resourceID string) ([]Collaborator, *ResourceSource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id::text,u.username,u.avatar_url,c.accepted_at FROM resource_collaborators c JOIN users u ON u.id=c.user_id WHERE c.resource_id=$1 ORDER BY c.accepted_at NULLS LAST,c.created_at`, resourceID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var collaborators []Collaborator
	for rows.Next() {
		var item Collaborator
		if err := rows.Scan(&item.UserID, &item.Username, &item.AvatarURL, &item.AcceptedAt); err != nil {
			return nil, nil, err
		}
		collaborators = append(collaborators, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var source ResourceSource
	err = s.db.QueryRowContext(ctx, `SELECT author_name,source_url,license_name,authorization_note FROM resource_sources WHERE resource_id=$1`, resourceID).
		Scan(&source.AuthorName, &source.SourceURL, &source.LicenseName, &source.AuthorizationNote)
	if errors.Is(err, sql.ErrNoRows) {
		return collaborators, nil, nil
	}
	return collaborators, &source, err
}

func (s *Service) CollaborationInvitations(ctx context.Context, userID string) ([]CollaborationInvitation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT resource.id::text,COALESCE(revision.name,resource.draft_name),owner.username,collaboration.created_at
FROM resource_collaborators collaboration
JOIN resources resource ON resource.id=collaboration.resource_id
JOIN users owner ON owner.id=resource.owner_id
LEFT JOIN resource_revisions revision ON revision.id=resource.current_revision_id
WHERE collaboration.user_id=$1 AND collaboration.accepted_at IS NULL
ORDER BY collaboration.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var invitations []CollaborationInvitation
	for rows.Next() {
		var invitation CollaborationInvitation
		if err := rows.Scan(&invitation.ResourceID, &invitation.ResourceName, &invitation.Owner, &invitation.InvitedAt); err != nil {
			return nil, err
		}
		invitations = append(invitations, invitation)
	}
	return invitations, rows.Err()
}

func (s *Service) InviteCollaborator(ctx context.Context, ownerID, resourceID string, bandBBSUserID int64) error {
	if bandBBSUserID <= 0 {
		return fmt.Errorf("%w: BandBBS user id", ErrInvalid)
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO resource_collaborators(resource_id,user_id,invited_by)
SELECT r.id,u.id,r.owner_id FROM resources r JOIN users u ON u.bandbbs_user_id=$3 WHERE r.id=$1 AND r.owner_id=$2 AND u.id<>r.owner_id
ON CONFLICT(resource_id,user_id) DO UPDATE SET invited_by=EXCLUDED.invited_by,accepted_at=NULL,created_at=now()`, resourceID, ownerID, bandBBSUserID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) AcceptCollaborator(ctx context.Context, userID, resourceID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE resource_collaborators SET accepted_at=now() WHERE resource_id=$1 AND user_id=$2 AND accepted_at IS NULL`, resourceID, userID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) RemoveCollaborator(ctx context.Context, actorID, resourceID, collaboratorID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM resource_collaborators c USING resources r WHERE c.resource_id=r.id AND c.resource_id=$1 AND c.user_id=$2 AND (r.owner_id=$3 OR c.user_id=$3)`, resourceID, collaboratorID, actorID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) SetResourceSource(ctx context.Context, ownerID, resourceID string, source ResourceSource) error {
	source.AuthorName = strings.TrimSpace(source.AuthorName)
	source.SourceURL = strings.TrimSpace(source.SourceURL)
	source.LicenseName = strings.TrimSpace(source.LicenseName)
	source.AuthorizationNote = strings.TrimSpace(source.AuthorizationNote)
	if len([]rune(source.AuthorName)) > 120 || len(source.SourceURL) > 2000 || len([]rune(source.LicenseName)) > 120 || len([]rune(source.AuthorizationNote)) > 1000 {
		return fmt.Errorf("%w: resource source", ErrInvalid)
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO resource_sources(resource_id,author_name,source_url,license_name,authorization_note)
SELECT id,$3,$4,$5,$6 FROM resources WHERE id=$1 AND owner_id=$2
ON CONFLICT(resource_id) DO UPDATE SET author_name=EXCLUDED.author_name,source_url=EXCLUDED.source_url,license_name=EXCLUDED.license_name,authorization_note=EXCLUDED.authorization_note,updated_at=now()`, resourceID, ownerID, source.AuthorName, source.SourceURL, source.LicenseName, source.AuthorizationNote)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrNotFound
	}
	return nil
}
