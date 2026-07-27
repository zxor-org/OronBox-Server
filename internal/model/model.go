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
	ID             int64
	CreatedAt      string
	Provider       string
	EventType      string
	Result         string
	AppID          string
	AppVersion     string
	AppBuild       string
	Platform       string
	IP             string
	UserAgent      string
	StateID        string
	TicketID       string
	ProviderUserID string
	ExpectedScopes string
	ActualScopes   string
	ErrorCode      string
	ErrorMessage   string
	LatencyMS      int64
}

type OAuthState struct {
	ID         string
	CreatedAt  string
	ExpiresAt  string
	UsedAt     string
	AppID      string
	AppVersion string
	AppBuild   string
	Platform   string
	ReturnURI  string
	IP         string
	UserAgent  string
	Provider   string
	Purpose    string
}

type OAuthTicket struct {
	ID        string
	CreatedAt string
	ExpiresAt string
	UsedAt    string
	AppID     string
	Platform  string
	ReturnURI string
	UserLabel string
	HasToken  bool
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
