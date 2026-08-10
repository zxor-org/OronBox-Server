package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	adminReleaseDefaultPerPage = 25
	adminReleaseMaxPerPage     = 100
)

var (
	ErrAdminReleaseNotFound = errors.New("app release was not found")
	ErrAdminReleaseConflict = errors.New("app release state transition is not allowed")
	ErrAdminReleaseInvalid  = errors.New("app release is invalid")
)

type AdminReleaseQuery struct {
	Search   string
	Platform string
	Arch     string
	Channel  string
	Enabled  string
	Sort     string
	Page     int
	PerPage  int
}

type AdminReleaseItem struct {
	AppRelease
	Enabled   bool       `json:"enabled"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	UpdatedAt time.Time  `json:"updated_at"`
	CreatedBy string     `json:"created_by,omitempty"`
	Creator   string     `json:"creator,omitempty"`
}

func (item AdminReleaseItem) State() string {
	if item.RevokedAt != nil {
		return "revoked"
	}
	if item.Enabled {
		return "enabled"
	}
	return "disabled"
}

type AdminReleasePage struct {
	Items      []AdminReleaseItem
	Total      int
	Page       int
	PerPage    int
	TotalPages int
	Query      AdminReleaseQuery
}

type AdminReleaseNotesInput struct {
	MinimumVersion string
	NotesZH        string
	NotesEN        string
}

type AdminReleaseVersionInput struct {
	Version        string
	MinimumVersion string
	Channel        string
	Platform       string
	Arch           string
}

func (query AdminReleaseQuery) normalized() AdminReleaseQuery {
	query.Search = strings.TrimSpace(query.Search)
	query.Platform = strings.ToLower(strings.TrimSpace(query.Platform))
	query.Arch = strings.ToLower(strings.TrimSpace(query.Arch))
	query.Channel = strings.ToLower(strings.TrimSpace(query.Channel))
	query.Enabled = strings.ToLower(strings.TrimSpace(query.Enabled))
	if query.Enabled != "enabled" && query.Enabled != "disabled" && query.Enabled != "revoked" {
		query.Enabled = ""
	}
	switch query.Sort {
	case "published_asc", "version_desc", "version_asc", "updated_desc", "updated_asc":
	default:
		query.Sort = "published_desc"
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PerPage < 1 {
		query.PerPage = adminReleaseDefaultPerPage
	}
	if query.PerPage > adminReleaseMaxPerPage {
		query.PerPage = adminReleaseMaxPerPage
	}
	return query
}

func adminReleaseOrder(sort string) string {
	switch sort {
	case "published_asc":
		return "release.published_at ASC, release.id ASC"
	case "version_desc":
		return "release.version DESC, release.published_at DESC, release.id DESC"
	case "version_asc":
		return "release.version ASC, release.published_at ASC, release.id ASC"
	case "updated_desc":
		return "release.updated_at DESC, release.id DESC"
	case "updated_asc":
		return "release.updated_at ASC, release.id ASC"
	default:
		return "release.published_at DESC, release.id DESC"
	}
}

func (s *Store) AdminReleases(ctx context.Context, raw AdminReleaseQuery) (AdminReleasePage, error) {
	query := raw.normalized()
	const filter = `($1='' OR concat_ws(' ',release.id::text,release.version,release.channel,release.platform,release.arch,release.minimum_version,release.notes_zh,release.notes_en,release.download_url,creator.username) ILIKE '%'||$1||'%')
 AND ($2='' OR release.platform=$2) AND ($3='' OR release.arch=$3) AND ($4='' OR release.channel=$4)
 AND ($5='' OR ($5='enabled' AND release.enabled AND release.revoked_at IS NULL) OR ($5='disabled' AND NOT release.enabled AND release.revoked_at IS NULL) OR ($5='revoked' AND release.revoked_at IS NOT NULL))`
	args := []any{query.Search, query.Platform, query.Arch, query.Channel, query.Enabled}
	page := AdminReleasePage{Items: []AdminReleaseItem{}, Page: query.Page, PerPage: query.PerPage, Query: query}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM app_releases release LEFT JOIN users creator ON creator.id=release.created_by WHERE `+filter, args...).Scan(&page.Total); err != nil {
		return AdminReleasePage{}, err
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT release.id::text,release.version,release.channel,release.platform,release.arch,release.minimum_version,release.notes_zh,release.notes_en,release.download_url,release.published_at,release.enabled,release.revoked_at,release.updated_at,COALESCE(release.created_by::text,''),COALESCE(creator.username,'') FROM app_releases release LEFT JOIN users creator ON creator.id=release.created_by WHERE %s ORDER BY %s LIMIT $6 OFFSET $7`, filter, adminReleaseOrder(query.Sort)), append(args, query.PerPage, (query.Page-1)*query.PerPage)...)
	if err != nil {
		return AdminReleasePage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanAdminRelease(rows)
		if err != nil {
			return AdminReleasePage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminReleasePage{}, err
	}
	if page.Total > 0 {
		page.TotalPages = (page.Total + page.PerPage - 1) / page.PerPage
	}
	return page, nil
}

func (s *Store) AdminRelease(ctx context.Context, id string) (AdminReleaseItem, error) {
	if _, err := uuid.Parse(id); err != nil {
		return AdminReleaseItem{}, ErrAdminReleaseNotFound
	}
	item, err := scanAdminRelease(s.db.QueryRowContext(ctx, `SELECT release.id::text,release.version,release.channel,release.platform,release.arch,release.minimum_version,release.notes_zh,release.notes_en,release.download_url,release.published_at,release.enabled,release.revoked_at,release.updated_at,COALESCE(release.created_by::text,''),COALESCE(creator.username,'') FROM app_releases release LEFT JOIN users creator ON creator.id=release.created_by WHERE release.id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AdminReleaseItem{}, ErrAdminReleaseNotFound
	}
	return item, err
}

type adminReleaseScanner interface{ Scan(...any) error }

func scanAdminRelease(scanner adminReleaseScanner) (AdminReleaseItem, error) {
	var item AdminReleaseItem
	err := scanner.Scan(&item.ID, &item.Version, &item.Channel, &item.Platform, &item.Arch, &item.MinimumVersion, &item.NotesZH, &item.NotesEN, &item.DownloadURL, &item.PublishedAt, &item.Enabled, &item.RevokedAt, &item.UpdatedAt, &item.CreatedBy, &item.Creator)
	return item, err
}

func (s *Store) AdminUpdateReleaseNotes(ctx context.Context, id string, raw AdminReleaseNotesInput) (AdminReleaseItem, error) {
	if _, err := uuid.Parse(id); err != nil {
		return AdminReleaseItem{}, ErrAdminReleaseNotFound
	}
	input, err := raw.normalized()
	if err != nil {
		return AdminReleaseItem{}, err
	}
	current, err := s.AdminRelease(ctx, id)
	if err != nil {
		return AdminReleaseItem{}, err
	}
	if current.RevokedAt != nil {
		return AdminReleaseItem{}, ErrAdminReleaseConflict
	}
	if err := ValidateAdminReleaseVersion(AdminReleaseVersionInput{
		Version: current.Version, MinimumVersion: input.MinimumVersion,
		Channel: current.Channel, Platform: current.Platform, Arch: current.Arch,
	}); err != nil {
		return AdminReleaseItem{}, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE app_releases SET minimum_version=$2,notes_zh=$3,notes_en=$4,updated_at=now() WHERE id=$1 AND revoked_at IS NULL`, id, input.MinimumVersion, input.NotesZH, input.NotesEN)
	if err != nil {
		return AdminReleaseItem{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		if item, lookupErr := s.AdminRelease(ctx, id); lookupErr == nil && item.RevokedAt != nil {
			return AdminReleaseItem{}, ErrAdminReleaseConflict
		}
		return AdminReleaseItem{}, ErrAdminReleaseNotFound
	}
	return s.AdminRelease(ctx, id)
}

func (input AdminReleaseNotesInput) normalized() (AdminReleaseNotesInput, error) {
	input.MinimumVersion = strings.TrimSpace(input.MinimumVersion)
	input.NotesZH = strings.TrimSpace(input.NotesZH)
	input.NotesEN = strings.TrimSpace(input.NotesEN)
	if len(input.NotesZH) > 20000 || len(input.NotesEN) > 20000 {
		return input, fmt.Errorf("%w: release notes exceed 20000 characters", ErrAdminReleaseInvalid)
	}
	if input.MinimumVersion != "" {
		if _, err := parseAdminSemver(input.MinimumVersion); err != nil {
			return input, fmt.Errorf("%w: minimum version", ErrAdminReleaseInvalid)
		}
	}
	return input, nil
}

func (s *Store) AdminSetReleaseState(ctx context.Context, id, action string) (AdminReleaseItem, error) {
	if _, err := uuid.Parse(id); err != nil {
		return AdminReleaseItem{}, ErrAdminReleaseNotFound
	}
	var result sql.Result
	var err error
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "enable":
		result, err = s.db.ExecContext(ctx, `UPDATE app_releases SET enabled=true,updated_at=now() WHERE id=$1 AND NOT enabled AND revoked_at IS NULL`, id)
	case "disable":
		result, err = s.db.ExecContext(ctx, `UPDATE app_releases SET enabled=false,updated_at=now() WHERE id=$1 AND enabled AND revoked_at IS NULL`, id)
	case "revoke":
		result, err = s.db.ExecContext(ctx, `UPDATE app_releases SET enabled=false,revoked_at=now(),updated_at=now() WHERE id=$1 AND revoked_at IS NULL`, id)
	default:
		return AdminReleaseItem{}, ErrAdminReleaseConflict
	}
	if err != nil {
		return AdminReleaseItem{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		if _, lookupErr := s.AdminRelease(ctx, id); errors.Is(lookupErr, ErrAdminReleaseNotFound) {
			return AdminReleaseItem{}, lookupErr
		}
		return AdminReleaseItem{}, ErrAdminReleaseConflict
	}
	return s.AdminRelease(ctx, id)
}

var adminReleaseTokenPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)
var adminSemverPattern = regexp.MustCompile(`^(?:v)?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

type adminSemver struct {
	major, minor, patch uint64
	pre                 string
}

func ValidateAdminReleaseVersion(raw AdminReleaseVersionInput) error {
	input := AdminReleaseVersionInput{Version: strings.TrimSpace(raw.Version), MinimumVersion: strings.TrimSpace(raw.MinimumVersion), Channel: strings.ToLower(strings.TrimSpace(raw.Channel)), Platform: strings.ToLower(strings.TrimSpace(raw.Platform)), Arch: strings.ToLower(strings.TrimSpace(raw.Arch))}
	version, err := parseAdminSemver(input.Version)
	if err != nil {
		return fmt.Errorf("%w: version must be semantic versioning", ErrAdminReleaseInvalid)
	}
	if input.Channel != "stable" && input.Channel != "beta" && input.Channel != "nightly" {
		return fmt.Errorf("%w: unsupported channel", ErrAdminReleaseInvalid)
	}
	if !adminReleaseTokenPattern.MatchString(input.Platform) || !adminReleaseTokenPattern.MatchString(input.Arch) {
		return fmt.Errorf("%w: invalid platform or architecture", ErrAdminReleaseInvalid)
	}
	if input.MinimumVersion != "" {
		minimum, parseErr := parseAdminSemver(input.MinimumVersion)
		if parseErr != nil {
			return fmt.Errorf("%w: minimum version must be semantic versioning", ErrAdminReleaseInvalid)
		}
		if compareAdminSemver(minimum, version) > 0 {
			return fmt.Errorf("%w: minimum version cannot exceed release version", ErrAdminReleaseInvalid)
		}
	}
	return nil
}

func parseAdminSemver(value string) (adminSemver, error) {
	match := adminSemverPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return adminSemver{}, ErrAdminReleaseInvalid
	}
	major, err := strconv.ParseUint(match[1], 10, 64)
	if err != nil {
		return adminSemver{}, ErrAdminReleaseInvalid
	}
	minor, err := strconv.ParseUint(match[2], 10, 64)
	if err != nil {
		return adminSemver{}, ErrAdminReleaseInvalid
	}
	patch, err := strconv.ParseUint(match[3], 10, 64)
	if err != nil {
		return adminSemver{}, ErrAdminReleaseInvalid
	}
	for _, identifier := range strings.Split(match[4], ".") {
		if len(identifier) > 1 && identifier[0] == '0' {
			if _, err := strconv.ParseUint(identifier, 10, 64); err == nil {
				return adminSemver{}, ErrAdminReleaseInvalid
			}
		}
	}
	return adminSemver{major: major, minor: minor, patch: patch, pre: match[4]}, nil
}

func compareAdminSemver(left, right adminSemver) int {
	for _, pair := range [][2]uint64{{left.major, right.major}, {left.minor, right.minor}, {left.patch, right.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if left.pre == right.pre {
		return 0
	}
	if left.pre == "" {
		return 1
	}
	if right.pre == "" {
		return -1
	}
	return compareAdminPrerelease(left.pre, right.pre)
}

func compareAdminPrerelease(left, right string) int {
	a, b := strings.Split(left, "."), strings.Split(right, ".")
	for index := 0; index < len(a) && index < len(b); index++ {
		if a[index] == b[index] {
			continue
		}
		av, aerr := strconv.ParseUint(a[index], 10, 64)
		bv, berr := strconv.ParseUint(b[index], 10, 64)
		switch {
		case aerr == nil && berr == nil && av < bv:
			return -1
		case aerr == nil && berr == nil:
			return 1
		case aerr == nil:
			return -1
		case berr == nil:
			return 1
		case a[index] < b[index]:
			return -1
		default:
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}
