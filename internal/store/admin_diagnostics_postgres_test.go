package store_test

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func TestAdminOAuthDiagnosticsPaginationFiltersDetailsAndRedaction(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	adminDB, err := store.Open(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	databaseName := "testdb_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminDB.ExecContext(ctx, `CREATE DATABASE `+databaseName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = adminDB.ExecContext(context.Background(), `DROP DATABASE `+databaseName) })
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + databaseName
	db, err := store.Open(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	userID, ticketID := uuid.NewString(), uuid.NewString()
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username) VALUES($1,101,'diagnostic-user')`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO oauth_states(id,provider,purpose,expires_at,used_at,app_id,app_version,app_build,platform,return_uri,user_id,user_agent) VALUES('state-1','bandbbs','login',$1,$2,'app-one','1.2.3','42','android','oronbox://oauth?access_token=state-secret',$3,'test-agent')`, now.Add(time.Hour), now, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO login_tickets(id,ticket_hash,user_id,expires_at,app_id,platform,return_uri,token_cipher) VALUES($1,$2,$3,$4,'app-one','android','oronbox://oauth?ticket=ticket-secret',$5)`, ticketID, []byte("hash-must-not-render"), userID, now.Add(time.Hour), []byte("cipher-must-not-render")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		app, result := "app-one", "success"
		stateID, linkedTicket := "", ""
		if i == 2 {
			app, result, stateID, linkedTicket = "app-two", "failure", "state-1", ticketID
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO oauth_events(provider,event_type,result,app_id,app_version,app_build,platform,state_id,ticket_id,error_code,error_message) VALUES('bandbbs','callback',$1,$2,'1.2.3','42','android',$3,$4,'provider_error','access_token=event-secret Authorization: Bearer bearer-secret')`, result, app, stateID, linkedTicket); err != nil {
			t.Fatal(err)
		}
	}

	diagnostics := store.New(db)
	page, err := diagnostics.AdminOAuthEvents(ctx, store.AdminOAuthEventQuery{App: "app-one", Result: "success", Platform: "android", Page: 2, PerPage: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || page.TotalPages != 2 || len(page.Items) != 1 {
		t.Fatalf("event page = %#v", page)
	}
	failed, err := diagnostics.AdminOAuthEvents(ctx, store.AdminOAuthEventQuery{App: "app-two", Result: "failure", Page: 1, PerPage: 25})
	if err != nil || len(failed.Items) != 1 {
		t.Fatalf("failed events = %#v, %v", failed, err)
	}
	if strings.Contains(failed.Items[0].ErrorMessage, "event-secret") || strings.Contains(failed.Items[0].ErrorMessage, "bearer-secret") {
		t.Fatalf("event leaked secret: %s", failed.Items[0].ErrorMessage)
	}

	eventDetail, err := diagnostics.AdminOAuthEvent(ctx, failed.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if eventDetail.State == nil || eventDetail.Ticket == nil {
		t.Fatalf("event links = %#v", eventDetail)
	}
	stateDetail, err := diagnostics.AdminOAuthState(ctx, "state-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(stateDetail.Events) != 1 || len(stateDetail.Tickets) != 1 || strings.Contains(stateDetail.State.ReturnURI, "state-secret") {
		t.Fatalf("state detail = %#v", stateDetail)
	}
	ticketDetail, err := diagnostics.AdminOAuthTicket(ctx, ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ticketDetail.Events) != 1 || len(ticketDetail.States) != 1 || ticketDetail.UserID != userID || strings.Contains(ticketDetail.Ticket.ReturnURI, "ticket-secret") {
		t.Fatalf("ticket detail = %#v", ticketDetail)
	}

	clients, err := diagnostics.AdminClientStats(ctx, store.AdminClientStatsQuery{App: "app-one", Platform: "android", Page: 1, PerPage: 1})
	if err != nil || clients.Total != 1 || len(clients.Items) != 1 {
		t.Fatalf("clients = %#v, %v", clients, err)
	}
	client, err := diagnostics.AdminClient(ctx, "app-one", "1.2.3", "42", "android", store.AdminOAuthEventQuery{Page: 2, PerPage: 1})
	if err != nil || client.Events.Total != 2 || len(client.Events.Items) != 1 {
		t.Fatalf("client detail = %#v, %v", client, err)
	}
}
