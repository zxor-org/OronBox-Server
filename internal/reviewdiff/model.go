package reviewdiff

type Count struct {
	Added   int
	Removed int
	Changed int
}

type FieldChange struct {
	Field  string
	Label  string
	Before string
	After  string
}

type ItemChange struct {
	Key    string
	Label  string
	Change string
	Before string
	After  string
}

type Diff struct {
	HasBase         bool
	MetadataChanged bool
	MetadataFields  []string
	Metadata        []FieldChange
	Attributes      Count
	Links           Count
	Media           Count
	Artifacts       Count
	Devices         Count
	AttributeItems  []ItemChange
	LinkItems       []ItemChange
	MediaItems      []ItemChange
	ArtifactItems   []ItemChange
	DeviceItems     []ItemChange
}

type Snapshot struct {
	ID, Name, Summary, Description, DescriptionFormat, PaidType string
	PublicationPlan, Governance                                 any
	Attributes, Devices                                         []string
	Links, Media, Artifacts                                     map[string]string
}
