package store

import (
	"context"
	"errors"
	"testing"
)

func TestAdminUserDetailQueryNormalizesEveryGroup(t *testing.T) {
	q := (AdminUserDetailQuery{
		Resources: AdminUserDetailPageQuery{Page: -2, PerPage: 500},
		Comments:  AdminUserDetailPageQuery{Page: 3, PerPage: 50},
	}).normalized()
	if q.Resources.Page != 1 || q.Resources.PerPage != 100 {
		t.Fatalf("resources = %#v", q.Resources)
	}
	if q.Comments.Page != 3 || q.Comments.PerPage != 50 {
		t.Fatalf("comments = %#v", q.Comments)
	}
	for name, group := range map[string]AdminUserDetailPageQuery{
		"tickets": q.Tickets, "messages": q.Messages, "ledger": q.Ledger,
		"sessions": q.Sessions, "audit": q.Audit,
	} {
		if group.Page != 1 || group.PerPage != 25 {
			t.Errorf("%s = %#v", name, group)
		}
	}
}

func TestAdminUserDetailPageMetadata(t *testing.T) {
	p := newAdminUserDetailPage[string](AdminUserDetailPageQuery{Page: 2, PerPage: 25})
	p.Total = 51
	finishAdminUserDetailPage(&p)
	if p.TotalPages != 3 || p.Items == nil {
		t.Fatalf("page = %#v", p)
	}
}

func TestAdminUserDetailRejectsInvalidIdentifiersBeforeDatabase(t *testing.T) {
	var s Store
	if _, err := s.AdminUserDetail(context.Background(), "invalid", AdminUserDetailQuery{}); !errors.Is(err, ErrAdminUserNotFound) {
		t.Fatalf("detail error = %v", err)
	}
	if err := s.AdminRevokeUserSession(context.Background(), "invalid", "invalid"); !errors.Is(err, ErrAdminUserNotFound) {
		t.Fatalf("single revoke user error = %v", err)
	}
	if _, err := s.AdminRevokeAllUserSessions(context.Background(), "invalid"); !errors.Is(err, ErrAdminUserNotFound) {
		t.Fatalf("all revoke error = %v", err)
	}
}
