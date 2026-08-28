package model

import "time"

type User struct {
	ID              string     `json:"id"`
	BandBBSUserID   int64      `json:"bandbbs_user_id"`
	Username        string     `json:"username"`
	AvatarURL       string     `json:"avatar_url"`
	Role            string     `json:"role"`
	BannedAt        *time.Time `json:"banned_at,omitempty"`
	BanReason       string     `json:"ban_reason,omitempty"`
	CreatorFrozenAt *time.Time `json:"creator_frozen_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type SessionTokens struct {
	AccessToken      string        `json:"access_token"`
	RefreshToken     string        `json:"refresh_token"`
	TokenType        string        `json:"token_type"`
	ExpiresIn        int64         `json:"expires_in"`
	RefreshExpiresIn int64         `json:"refresh_expires_in"`
	User             User          `json:"user"`
	BandBBS          *TokenPayload `json:"bandbbs,omitempty"`
}

type ClientMeta struct {
	AppID    string
	Version  string
	Platform string
	Build    string
	IP       string
	UA       string
}

type TokenPayload struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	IssueDate    int64  `json:"issue_date"`
	Scope        string `json:"scope,omitempty"`
}

type OAuthEvent struct {
	ID             int64  `json:"id"`
	CreatedAt      string `json:"created_at"`
	Provider       string `json:"provider"`
	EventType      string `json:"event_type"`
	Result         string `json:"result"`
	AppID          string `json:"app_id"`
	AppVersion     string `json:"app_version"`
	AppBuild       string `json:"app_build"`
	Platform       string `json:"platform"`
	IP             string `json:"ip"`
	UserAgent      string `json:"user_agent"`
	StateID        string `json:"state_id"`
	TicketID       string `json:"ticket_id"`
	ProviderUserID string `json:"provider_user_id"`
	ExpectedScopes string `json:"expected_scopes"`
	ActualScopes   string `json:"actual_scopes"`
	ErrorCode      string `json:"error_code"`
	ErrorMessage   string `json:"error_message"`
	LatencyMS      int64  `json:"latency_ms"`
}

type OAuthState struct {
	ID         string `json:"id"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at"`
	UsedAt     string `json:"used_at,omitempty"`
	AppID      string `json:"app_id"`
	AppVersion string `json:"app_version"`
	AppBuild   string `json:"app_build"`
	Platform   string `json:"platform"`
	ReturnURI  string `json:"return_uri"`
	IP         string `json:"ip"`
	UserAgent  string `json:"user_agent"`
	Provider   string `json:"provider"`
	Purpose    string `json:"purpose"`
}

type OAuthTicket struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
	UsedAt    string `json:"used_at,omitempty"`
	AppID     string `json:"app_id"`
	Platform  string `json:"platform"`
	ReturnURI string `json:"return_uri"`
	UserLabel string `json:"user_label"`
	HasToken  bool   `json:"has_token"`
}

type Stats struct {
	StartedAt          time.Time
	OAuthStartToday    int64
	CallbackOKToday    int64
	CallbackFailToday  int64
	ExchangeOKToday    int64
	RefreshOKToday     int64
	RefreshFailToday   int64
	ScopeMismatchToday int64
	ActiveStates       int64
	ActiveTickets      int64
	ResourcesTotal     int64
	PublishedResources int64
	PendingReviews     int64
	OpenReports        int64
	FailedPublications int64
}
