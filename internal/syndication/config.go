package syndication

import (
	"sort"
	"strings"
)

// SubmissionConfig is the per-submission catalog/ops metadata the client submits
// at draft/publish time. It is stored on resource_submissions.config and is NOT
// written into the resource repo manifest (the manifest stays artifact-only).
type SubmissionConfig struct {
	Targets []string `json:"targets"`
	Catalog struct {
		Tags     []string `json:"tags"`
		PaidType string   `json:"paid_type"`
	} `json:"catalog"`
	BandBBS struct {
		Purchase *ExternalPurchase `json:"purchase,omitempty"`
	} `json:"bandbbs"`
	AstroBox map[string]any `json:"astrobox,omitempty"`
	// Reserved for future OronBox private paid / encryption + Afdian entitlement.
	Encryption  map[string]any `json:"encryption,omitempty"`
	Entitlement map[string]any `json:"entitlement,omitempty"`
}

// ExternalPurchase is the外部购买 config used by 米坛 (external_purchase).
type ExternalPurchase struct {
	Link     string  `json:"link"`
	Price    float64 `json:"price"`
	Currency string  `json:"currency"`
}

// NormalizePaidType maps the internal paid type to the index_v2.csv raw value.
// ABCC's parser only accepts "" / "paid" / "force_paid" and DROPS rows with any
// other value, and "" means free — so internal "free"/"" both serialize to "".
func NormalizePaidType(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "paid":
		return "paid"
	case "force_paid":
		return "force_paid"
	default: // "", "free", anything unknown → free
		return ""
	}
}

// Paid reports whether the normalized paid type denotes a paid resource.
func Paid(normalizedPaidType string) bool { return normalizedPaidType != "" }

// DeviceVendors returns the de-duplicated, sorted vendors of the given download
// device compat ids, resolved via devices.json (used for index.csv.device_vendors).
func DeviceVendors(downloads map[string]DownloadEntry, devices map[string]DeviceSpec) string {
	byCompat := map[string]string{}
	for codename, spec := range devices {
		if spec.ID != "" {
			byCompat[spec.ID] = codename
		}
	}
	set := map[string]struct{}{}
	for compatID := range downloads {
		if codename, ok := byCompat[compatID]; ok {
			if v := strings.TrimSpace(devices[codename].Vendor); v != "" {
				set[v] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return strings.Join(out, ";")
}
