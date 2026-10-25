package bandbbs

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/blob"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type Client struct {
	apiURL string
	http   *http.Client
	blobs  blob.Store
	store  *store.Store
}

type Result struct {
	// Resources maps the decimal category id to the published resource.
	Resources map[string]CategoryResult
}

type CategoryResult struct {
	ResourceID string `json:"resource_id"`
	URL        string `json:"url"`
	VersionID  string `json:"version_id,omitempty"`
	UpdateID   string `json:"update_id,omitempty"`
}

func New(apiURL string, blobs blob.Store, storage *store.Store) *Client {
	return &Client{apiURL: strings.TrimRight(apiURL, "/"), http: &http.Client{Timeout: 5 * time.Minute}, blobs: blobs, store: storage}
}

type snapshot struct {
	Revision struct {
		Name              string   `json:"name"`
		Summary           string   `json:"summary"`
		Description       string   `json:"description"`
		DescriptionFormat string   `json:"description_format"`
		PurchaseLink      string   `json:"purchase_link"`
		PurchasePrice     *float64 `json:"purchase_price"`
		PurchaseCurrency  string   `json:"purchase_currency"`
		Links             []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"links"`
	} `json:"revision"`
	Media []struct {
		Blob string `json:"blob_sha256"`
		Role string `json:"role"`
	} `json:"media"`
	Artifacts []struct {
		Blob      string `json:"blob_sha256"`
		Name      string `json:"original_name"`
		PackageID string `json:"package_id"`
		Version   string `json:"version"`
	} `json:"artifacts"`
}

type target struct {
	CategoryID     int    `json:"category_id"`
	PrefixID       int    `json:"prefix_id"`
	ThreadPrefixID int    `json:"thread_prefix_id"`
	ForumID        int    `json:"forum_id"`
	PackageID      string `json:"package_id"`
}

type config struct {
	Description          string   `json:"description"`
	DescriptionFormat    string   `json:"description_format"`
	VersionMessageFormat string   `json:"version_message_format"`
	VersionTitle         string   `json:"version_title"`
	VersionMessage       string   `json:"version_message"`
	OverwritePrevious    bool     `json:"overwrite_previous_version"`
	Agreement            bool     `json:"agreement"`
	Price                float64  `json:"price"`
	Targets              []target `json:"targets"`
}

const externalPurchaseCurrency = "CNY"

// Publish fans one revision out to one BandBBS resource per configured
// category. existing maps a decimal category id to the bound resource id.
func (c *Client) Publish(ctx context.Context, token string, existing map[string]string, rawSnapshot, rawConfig []byte) (Result, error) {
	return c.PublishWithProgress(ctx, token, "", existing, nil, rawSnapshot, rawConfig)
}

// PublishWithProgress resumes a multi-step publication using the external
// resource, version, and update IDs already persisted by the coordinator.
// Replaying a failed attempt therefore edits the resource and skips completed
// side effects instead of creating duplicate versions or updates.
func (c *Client) PublishWithProgress(ctx context.Context, token, creatorID string, existing map[string]string, progress map[string]CategoryResult, rawSnapshot, rawConfig []byte) (Result, error) {
	var snap snapshot
	var cfg config
	if err := json.Unmarshal(rawSnapshot, &snap); err != nil {
		return Result{}, err
	}
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return Result{}, err
	}
	if !cfg.Agreement || len(cfg.Targets) == 0 {
		return Result{}, fmt.Errorf("BandBBS targets and agreement are required")
	}
	cfg.VersionTitle = strings.TrimSpace(cfg.VersionTitle)
	cfg.VersionMessage = strings.TrimSpace(cfg.VersionMessage)
	// BandBBS builds its version update from the title/notes pair. Plans have
	// shipped with only one side filled, which used to sink the whole
	// publication; default the missing side from the revision metadata so the
	// update stays well-formed instead of failing at dispatch time. Both empty
	// still means no update at all.
	if cfg.VersionTitle == "" && cfg.VersionMessage != "" {
		cfg.VersionTitle = strings.TrimSpace(snap.Revision.Name)
	}
	if cfg.VersionMessage == "" && cfg.VersionTitle != "" {
		cfg.VersionMessage = strings.TrimSpace(snap.Revision.Summary)
	}
	purchaseLink := strings.TrimSpace(snap.Revision.PurchaseLink)
	externalPurchase := purchaseLink != ""
	if externalPurchase {
		if snap.Revision.PurchasePrice == nil || math.IsNaN(*snap.Revision.PurchasePrice) || math.IsInf(*snap.Revision.PurchasePrice, 0) || *snap.Revision.PurchasePrice <= 0 {
			return Result{}, fmt.Errorf("BandBBS external purchase amount is missing from resource metadata")
		}
		if math.IsNaN(cfg.Price) || math.IsInf(cfg.Price, 0) || (cfg.Price != 0 && math.Abs(cfg.Price-*snap.Revision.PurchasePrice) > 0.005) {
			return Result{}, fmt.Errorf("BandBBS external purchase amount does not match resource metadata")
		}
		cfg.Price = *snap.Revision.PurchasePrice
		if cfg.Price <= 0 {
			return Result{}, fmt.Errorf("BandBBS external purchase amount must be a positive CNY value")
		}
	}
	if (cfg.VersionTitle == "") != (cfg.VersionMessage == "") {
		return Result{}, fmt.Errorf("BandBBS version title and update notes must be provided together")
	}
	description := strings.TrimSpace(cfg.Description)
	format := cfg.DescriptionFormat
	if description == "" {
		description = snap.Revision.Description
		format = snap.Revision.DescriptionFormat
	}
	if format == "" {
		format = "plain"
	}
	switch format {
	case "markdown":
		description = MarkdownToBBCode(description)
	case "plain":
		description = escapeBBCodeText(description)
	case "bbcode":
	default:
		return Result{}, fmt.Errorf("invalid BandBBS description format")
	}
	switch cfg.VersionMessageFormat {
	case "markdown":
		cfg.VersionMessage = MarkdownToBBCode(cfg.VersionMessage)
	case "", "plain":
		cfg.VersionMessage = escapeBBCodeText(cfg.VersionMessage)
	case "bbcode":
	default:
		return Result{}, fmt.Errorf("invalid BandBBS update format")
	}

	if len(snap.Revision.Links) > 0 {
		var b strings.Builder
		b.WriteString("相关链接：\n")
		for _, link := range snap.Revision.Links {
			if strings.TrimSpace(link.URL) != "" {
				title := strings.TrimSpace(link.Title)
				if title == "" {
					title = "链接"
				}
				fmt.Fprintf(&b, "- %s：%s\n", escapeBBCodeText(title), strings.TrimSpace(link.URL))
			}
		}
		b.WriteString("\n")
		description = b.String() + description
	}
	// Previews are shared by every category, so upload them once against the
	// first target. RM MarketPlace registers no attachment content type for
	// description images, so previews go up as plain attachments and embed by
	// URL.
	previewContext := map[string]string{"resource_category_id": strconv.Itoa(cfg.Targets[0].CategoryID)}
	if existingID := existing[strconv.Itoa(cfg.Targets[0].CategoryID)]; existingID != "" {
		previewContext = map[string]string{"resource_id": existingID}
	}
	attachments := map[string]string{}
	for _, m := range snap.Media {
		attachments[m.Blob] = ""
	}
	// Reject unsupported inline media before any upload or external content write.
	if _, err := rewriteAttachedImages(description, attachments); err != nil {
		return Result{}, err
	}
	if _, err := rewriteAttachedImages(cfg.VersionMessage, attachments); err != nil {
		return Result{}, err
	}
	var previewImages strings.Builder
	for index, media := range snap.Media {
		if media.Role != "preview" && !strings.Contains(description, media.Blob) && !strings.Contains(cfg.VersionMessage, media.Blob) {
			continue
		}
		if attachments[media.Blob] != "" {
			continue
		}
		ext := mediaExtension(c.store, ctx, media.Blob)
		name := fmt.Sprintf("image-%d%s", index+1, ext)
		_, directURL, uploadErr := c.newAttachmentKey(ctx, token, "resource_version", media.Blob, name, previewContext)
		if uploadErr != nil {
			return Result{}, fmt.Errorf("upload image attachment: %w", uploadErr)
		}
		if !safeBBCodeURL(directURL) {
			return Result{}, fmt.Errorf("forum attachment response has no valid image URL")
		}
		attachments[media.Blob] = directURL
		if media.Role == "preview" {
			fmt.Fprintf(&previewImages, "\n\n[IMG]%s[/IMG]", directURL)
		}
	}
	var err error
	description, err = rewriteAttachedImages(description, attachments)
	if err != nil {
		return Result{}, err
	}
	cfg.VersionMessage, err = rewriteAttachedImages(cfg.VersionMessage, attachments)
	if err != nil {
		return Result{}, err
	}
	description += previewImages.String()

	result := Result{Resources: make(map[string]CategoryResult, len(cfg.Targets))}
	for _, target := range cfg.Targets {
		category := strconv.Itoa(target.CategoryID)
		artifact := -1
		for index := range snap.Artifacts {
			if snap.Artifacts[index].PackageID == target.PackageID {
				artifact = index
				break
			}
		}
		if artifact < 0 && !externalPurchase {
			return Result{}, fmt.Errorf("package %s is not among the snapshot artifacts", target.PackageID)
		}
		var pack struct {
			Blob      string
			Name      string
			PackageID string
			Version   string
		}
		if artifact >= 0 {
			pack.Blob = snap.Artifacts[artifact].Blob
			pack.Name = snap.Artifacts[artifact].Name
			pack.PackageID = snap.Artifacts[artifact].PackageID
			pack.Version = snap.Artifacts[artifact].Version
		}
		packSize := c.blobSize(ctx, pack.Blob)
		categoryResult := progress[category]
		reconcileVersion := categoryResult.ResourceID != "" && categoryResult.VersionID == ""
		reconcileUpdate := categoryResult.ResourceID != "" && categoryResult.UpdateID == ""
		attachContext := map[string]string{"resource_category_id": category}
		existingID := existing[category]
		if existingID == "" {
			existingID = categoryResult.ResourceID
		}
		if existingID == "" && creatorID != "" {
			resource, findErr := c.findResource(ctx, token, creatorID, target.CategoryID, snap.Revision.Name)
			if findErr != nil {
				return result, fmt.Errorf("find existing resource in category %d: %w", target.CategoryID, findErr)
			}
			if resource.ID != 0 {
				existingID = strconv.Itoa(resource.ID)
			}
		}
		if existingID != "" {
			attachContext = map[string]string{"resource_id": existingID}
		}
		categoryResult.ResourceID = existingID
		createdResource := existingID == ""
		if categoryResult.ResourceID != "" {
			result.Resources[category] = categoryResult
		}
		versionKey := ""
		if categoryResult.VersionID == "" && !externalPurchase {
			var err error
			versionKey, _, err = c.newAttachmentKey(ctx, token, "resource_version", pack.Blob, pack.Name, attachContext)
			if err != nil {
				if categoryResult.ResourceID != "" {
					result.Resources[category] = categoryResult
				}
				return result, fmt.Errorf("upload version attachment for package %s: %w", target.PackageID, err)
			}
		}
		form := url.Values{"title": {snap.Revision.Name}, "tag_line": {truncateRunes(strings.TrimSpace(snap.Revision.Summary), 100)}, "description": {description}}
		if target.PrefixID > 0 {
			form.Set("prefix_id", strconv.Itoa(target.PrefixID))
		}
		if target.ThreadPrefixID > 0 {
			form.Set("thread_prefix_id", strconv.Itoa(target.ThreadPrefixID))
		}
		if existingID == "" {
			form.Set("resource_category_id", category)
			if externalPurchase {
				form.Set("resource_type", "external_purchase")
				form.Set("external_purchase_url", purchaseLink)
				form.Set("price", strconv.FormatFloat(cfg.Price, 'f', 2, 64))
				form.Set("currency", externalPurchaseCurrency)
			} else {
				form.Set("resource_type", "download_local")
				form.Set("version_attachment_key", versionKey)
			}
			if pack.Version != "" {
				form.Set("version_string", pack.Version)
			}
		} else if externalPurchase {
			form.Set("resource_type", "external_purchase")
			form.Set("external_purchase_url", purchaseLink)
			form.Set("price", strconv.FormatFloat(cfg.Price, 'f', 2, 64))
			form.Set("currency", externalPurchaseCurrency)
		}
		var response struct {
			Resource struct {
				ID                 int    `json:"resource_id"`
				DiscussionThreadID int    `json:"discussion_thread_id"`
				ViewURL            string `json:"view_url"`
			} `json:"resource"`
		}
		endpoint := c.apiURL + "/resources/"
		if existingID != "" {
			endpoint += url.PathEscape(existingID) + "/"
		}
		if err := c.request(ctx, token, http.MethodPost, endpoint, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", &response); err != nil {
			return Result{}, fmt.Errorf("create resource in category %d: %w", target.CategoryID, err)
		}
		if response.Resource.ID == 0 && existingID != "" {
			response.Resource.ID, _ = strconv.Atoi(existingID)
		}
		if response.Resource.ID == 0 {
			return result, fmt.Errorf("BandBBS returned no resource id for category %d", target.CategoryID)
		}
		if target.ThreadPrefixID > 0 && response.Resource.DiscussionThreadID > 0 {
			threadForm := url.Values{"prefix_id": {strconv.Itoa(target.ThreadPrefixID)}}
			_ = c.request(ctx, token, http.MethodPost, c.apiURL+"/threads/"+strconv.Itoa(response.Resource.DiscussionThreadID)+"/", strings.NewReader(threadForm.Encode()), "application/x-www-form-urlencoded", nil)
		}
		resourceID := strconv.Itoa(response.Resource.ID)
		categoryResult.ResourceID = resourceID
		if createdResource && categoryResult.VersionID == "" {
			// Resource creation includes its first version. The API does not
			// consistently expose that version ID in the resource response, so
			// retain an opaque completion marker for retry reconciliation.
			categoryResult.VersionID = "initial"
		}
		if response.Resource.ViewURL != "" {
			categoryResult.URL = response.Resource.ViewURL
		}
		result.Resources[category] = categoryResult
		if categoryResult.VersionID == "" && !externalPurchase {
			version, versionErr := c.replaceVersion(ctx, token, resourceID, pack.Version, pack.Name, packSize, versionKey, reconcileVersion, cfg.OverwritePrevious)
			if versionErr != nil {
				return result, fmt.Errorf("create version in category %d: %w", target.CategoryID, versionErr)
			}
			categoryResult.VersionID = version.ID
		}
		if cfg.VersionTitle != "" && categoryResult.UpdateID == "" {
			update, updateErr := c.createUpdate(ctx, token, resourceID, cfg.VersionTitle, cfg.VersionMessage, reconcileUpdate)
			if updateErr != nil {
				return result, fmt.Errorf("create update in category %d: %w", target.CategoryID, updateErr)
			}
			categoryResult.UpdateID = update.ID
		}
		result.Resources[category] = categoryResult
	}
	return result, nil
}
