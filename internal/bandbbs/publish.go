package bandbbs

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// ExternalPurchase is the 米坛 external-purchase config (paid resources).
type ExternalPurchase struct {
	Link     string
	Price    float64
	Currency string
}

// PreviewImage is one description image (uploaded as an attachment).
type PreviewImage struct {
	Name string
	Data []byte
}

// PublishInput is one分区的 publish/edit task.
type PublishInput struct {
	CategoryID       int
	CreatorID        string
	Restype          string // watchface | quick_app
	ResourcePrefixID int
	ThreadPrefixID   int
	Title            string
	TagLine          string
	Description      string // BBCode body (author credits + description + links)
	Previews         []PreviewImage

	External *ExternalPurchase // non-nil → external_purchase (no package)

	Version           string
	VersionFileName   string
	VersionData       []byte
	VersionTitle      string
	VersionMessage    string
	OverwritePrevious bool

	ExistingID        int // 0 = create new
	ExistingVersionID int // skip version creation when already published
}

type PublishResult struct {
	ResourceID int
	URL        string
	VersionID  int
	FileIDs    []int
	ThreadID   int
	UpdateID   int
}

// Publish creates or edits one BandBBS resource for a single category, then sets
// the discussion-thread prefix. Metadata, version and update are separate flows.
func (c Client) Publish(ctx context.Context, token string, in PublishInput) (PublishResult, error) {
	res := PublishResult{ResourceID: in.ExistingID}
	cats, err := c.Categories(ctx, token)
	if err != nil {
		return res, fmt.Errorf("read resource categories: %w", err)
	}
	cat, ok := cats[in.CategoryID]
	if !ok {
		return res, fmt.Errorf("BandBBS category %d is unknown", in.CategoryID)
	}
	if !cat.CanAdd && in.ExistingID == 0 {
		return res, fmt.Errorf("BandBBS category %d does not allow new resources", in.CategoryID)
	}
	if in.External == nil && !cat.AllowLocal {
		return res, fmt.Errorf("BandBBS category %d does not allow local downloads", in.CategoryID)
	}
	if in.External != nil && !cat.AllowExternal {
		return res, fmt.Errorf("BandBBS category %d does not allow external downloads", in.CategoryID)
	}

	attachContext := map[string]string{"resource_category_id": strconv.Itoa(in.CategoryID)}
	if in.ExistingID != 0 {
		attachContext = map[string]string{"resource_id": strconv.Itoa(in.ExistingID)}
	}

	description := strings.TrimSpace(in.Description)
	for _, img := range in.Previews {
		_, directURL, uerr := c.NewAttachmentKey(ctx, token, "resource_update", attachContext, img.Name, img.Data)
		if uerr != nil {
			return res, fmt.Errorf("upload preview %s: %w", img.Name, uerr)
		}
		if directURL != "" {
			description += fmt.Sprintf("\n\n[IMG]%s[/IMG]", directURL)
		}
	}

	form := url.Values{
		"title":       {in.Title},
		"tag_line":    {truncateRunes(strings.TrimSpace(in.TagLine), 100)},
		"description": {description},
	}
	if in.ResourcePrefixID > 0 {
		form.Set("prefix_id", strconv.Itoa(in.ResourcePrefixID))
	}

	versionKey := ""
	if in.External == nil {
		versionKey, _, err = c.NewAttachmentKey(ctx, token, "resource_version", attachContext, in.VersionFileName, in.VersionData)
		if err != nil {
			return res, fmt.Errorf("upload version attachment: %w", err)
		}
	}

	if in.ExistingID == 0 {
		form.Set("resource_category_id", strconv.Itoa(in.CategoryID))
		if in.External != nil {
			applyExternalPurchase(form, *in.External)
		} else {
			form.Set("resource_type", "download_local")
			form.Set("version_attachment_key", versionKey)
			if strings.TrimSpace(in.Version) != "" {
				form.Set("version_string", strings.TrimSpace(in.Version))
			}
		}
		resourceID, viewURL, cerr := c.CreateResource(ctx, token, form)
		if cerr != nil {
			return res, fmt.Errorf("create resource in category %d: %w", in.CategoryID, cerr)
		}
		if resourceID == 0 {
			return res, fmt.Errorf("BandBBS returned no resource id for category %d", in.CategoryID)
		}
		res.ResourceID = resourceID
		res.URL = viewURL
	} else {
		if in.External != nil {
			applyExternalPurchase(form, *in.External)
		}
		if uerr := c.UpdateResource(ctx, token, in.ExistingID, form); uerr != nil {
			return res, fmt.Errorf("update resource in category %d: %w", in.CategoryID, uerr)
		}
	}

	// Version flow (local packages only). Skip when already published.
	if in.External == nil && in.ExistingVersionID == 0 {
		if !cat.EnableVersioning && in.ExistingID != 0 {
			return res, fmt.Errorf("BandBBS category %d does not allow new versions", in.CategoryID)
		}
		if in.OverwritePrevious {
			if versions, verr := c.Versions(ctx, token, res.ResourceID); verr == nil {
				if old := latestVersion(versions); old != nil {
					_ = c.DeleteVersion(ctx, token, old.ID)
				}
			}
		}
		versionID, fileIDs, verr := c.CreateVersion(ctx, token, res.ResourceID, in.Version, versionKey)
		if verr != nil {
			return res, fmt.Errorf("create version in category %d: %w", in.CategoryID, verr)
		}
		res.VersionID = versionID
		res.FileIDs = fileIDs
	}

	// Update-log flow.
	if strings.TrimSpace(in.VersionTitle) != "" || strings.TrimSpace(in.VersionMessage) != "" {
		title := strings.TrimSpace(in.VersionTitle)
		message := strings.TrimSpace(in.VersionMessage)
		if title == "" {
			title = in.Title
		}
		if message == "" {
			message = title
		}
		if updateID, uerr := c.CreateUpdate(ctx, token, res.ResourceID, title, message); uerr == nil {
			res.UpdateID = updateID
		}
	}

	// Discussion-thread prefix (requires thread:write). Best-effort: the resource
	// is already live; a prefix failure must not roll the publication back.
	if in.ThreadPrefixID > 0 {
		if threadID, terr := c.ResourcePageThreadID(ctx, token, res.ResourceID); terr == nil {
			res.ThreadID = threadID
			_ = c.SetThreadPrefix(ctx, token, threadID, in.ThreadPrefixID)
		}
	}

	return res, nil
}

func applyExternalPurchase(form url.Values, p ExternalPurchase) {
	form.Set("resource_type", "external_purchase")
	form.Set("external_purchase_url", strings.TrimSpace(p.Link))
	form.Set("price", strconv.FormatFloat(p.Price, 'f', 2, 64))
	currency := strings.TrimSpace(p.Currency)
	if currency == "" {
		currency = "CNY"
	}
	form.Set("currency", currency)
}

func latestVersion(versions []Version) *Version {
	var latest *Version
	for i := range versions {
		v := &versions[i]
		if latest == nil || v.ID > latest.ID {
			latest = v
		}
	}
	return latest
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// ValidExternalPurchase enforces a positive CNY price and non-empty link.
func ValidExternalPurchase(p *ExternalPurchase) bool {
	return p != nil && strings.TrimSpace(p.Link) != "" && p.Price > 0 && !math.IsNaN(p.Price) && !math.IsInf(p.Price, 0)
}
