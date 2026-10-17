package creator

// Publish bundle layout: manifest.json plus the payload files it references.
// Media files are pre-processed by the client (resized, encoded); the server
// only verifies them. Artifact payloads are re-analyzed server-side.
type publishMediaRef struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type publishArtifactRef struct {
	File         string   `json:"file"`
	OriginalName string   `json:"original_name"`
	Type         string   `json:"type"`
	PackageID    string   `json:"package_id"`
	Version      string   `json:"package_version"`
	SHA256       string   `json:"sha256"`
	DeviceIDs    []string `json:"device_ids"`
}

// publishBindingRef declares the external identity an imported resource
// already has on another platform; saveBundle records it so later
// publications update that identity instead of creating a new one.
type ImportedBinding = publishBindingRef

type publishBindingRef struct {
	Provider    string `json:"provider"`
	ExternalID  string `json:"external_id"`
	ExternalURL string `json:"external_url"`
	// Meta carries provider-specific extras, e.g. the AstroBox repo owner/name
	// the external identity lives in, used to update it in place later.
	Meta map[string]string `json:"meta,omitempty"`
}

type publishManifest struct {
	Version           int              `json:"version"`
	Kind              string           `json:"kind"`
	Name              string           `json:"name"`
	Summary           string           `json:"summary"`
	Description       string           `json:"description"`
	DescriptionFormat string           `json:"description_format"`
	PaidType          ResourcePaidType `json:"paid_type"`
	PurchaseLink      string           `json:"purchase_link"`
	PurchasePrice     *float64         `json:"purchase_price"`
	PurchaseCurrency  string           `json:"purchase_currency"`
	Attributes        []string         `json:"attributes"`
	Links             []ResourceLink   `json:"links"`
	Media             struct {
		Icon     *publishMediaRef  `json:"icon"`
		Cover    *publishMediaRef  `json:"cover"`
		Previews []publishMediaRef `json:"previews"`
	} `json:"media"`
	Artifacts    []publishArtifactRef `json:"artifacts"`
	Publications []PublicationRequest `json:"publications"`
	Bindings     []publishBindingRef  `json:"bindings"`
}
