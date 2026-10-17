package reviewdiff

import (
	"encoding/json"
	"fmt"
	"strings"
)

func Compare(base, current Snapshot) Diff {
	diff := Diff{HasBase: base.ID != "", MetadataFields: []string{}, Metadata: []FieldChange{}}
	metadata := []struct {
		name, label   string
		base, current any
		pretty        bool
	}{
		{"name", "名称", base.Name, current.Name, false},
		{"summary", "摘要", base.Summary, current.Summary, false},
		{"description", "正文", base.Description, current.Description, false},
		{"description_format", "正文格式", base.DescriptionFormat, current.DescriptionFormat, false},
		{"paid_type", "付费类型", base.PaidType, current.PaidType, false},
		{"publication_plan", "发布计划", base.PublicationPlan, current.PublicationPlan, true},
		{"governance", "治理信息", base.Governance, current.Governance, true},
	}
	for _, field := range metadata {
		before, after := stringifyReviewValue(field.base, field.pretty), stringifyReviewValue(field.current, field.pretty)
		if before == after {
			continue
		}
		diff.MetadataFields = append(diff.MetadataFields, field.name)
		diff.Metadata = append(diff.Metadata, FieldChange{Field: field.name, Label: field.label, Before: before, After: after})
	}
	diff.MetadataChanged = len(diff.MetadataFields) > 0
	diff.Attributes = diffStringSets(base.Attributes, current.Attributes)
	diff.Links = diffKeyedValues(base.Links, current.Links)
	diff.Media = diffKeyedValues(base.Media, current.Media)
	diff.Artifacts = diffKeyedValues(base.Artifacts, current.Artifacts)
	diff.Devices = diffStringSets(base.Devices, current.Devices)
	diff.AttributeItems = stringSetItemChanges(base.Attributes, current.Attributes)
	diff.LinkItems = keyedItemChanges(base.Links, current.Links, func(key, value string) string {
		title, url, _ := strings.Cut(value, "\x00")
		if title == "" {
			return url
		}
		return title + " · " + url
	})
	diff.MediaItems = keyedItemChanges(base.Media, current.Media, func(key, value string) string {
		sha, _, _ := strings.Cut(value, ":")
		return key + " · " + sha
	})
	diff.ArtifactItems = keyedItemChanges(base.Artifacts, current.Artifacts, func(key, value string) string {
		name := key
		if _, rest, ok := strings.Cut(key, "\x00"); ok {
			name = rest
		}
		sha, _, _ := strings.Cut(value, ":")
		return name + " · " + sha
	})
	diff.DeviceItems = stringSetItemChanges(base.Devices, current.Devices)
	return diff
}

func stringifyReviewValue(value any, pretty bool) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	}
	if pretty {
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return fmt.Sprint(value)
		}
		text := strings.TrimSpace(string(raw))
		if text == "null" || text == "\"\"" {
			return ""
		}
		return text
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return strings.Trim(strings.TrimSpace(string(raw)), `"`)
}

func stringSetItemChanges(base, current []string) []ItemChange {
	left, right := map[string]struct{}{}, map[string]struct{}{}
	for _, value := range base {
		left[value] = struct{}{}
	}
	for _, value := range current {
		right[value] = struct{}{}
	}
	changes := []ItemChange{}
	for _, value := range current {
		if _, ok := left[value]; !ok {
			changes = append(changes, ItemChange{Key: value, Label: value, Change: "added", After: value})
		}
	}
	for _, value := range base {
		if _, ok := right[value]; !ok {
			changes = append(changes, ItemChange{Key: value, Label: value, Change: "removed", Before: value})
		}
	}
	return changes
}

func keyedItemChanges(base, current map[string]string, label func(key, value string) string) []ItemChange {
	changes := []ItemChange{}
	for key, value := range current {
		old, ok := base[key]
		switch {
		case !ok:
			changes = append(changes, ItemChange{Key: key, Label: label(key, value), Change: "added", After: label(key, value)})
		case old != value:
			changes = append(changes, ItemChange{Key: key, Label: label(key, value), Change: "changed", Before: label(key, old), After: label(key, value)})
		}
	}
	for key, value := range base {
		if _, ok := current[key]; !ok {
			changes = append(changes, ItemChange{Key: key, Label: label(key, value), Change: "removed", Before: label(key, value)})
		}
	}
	return changes
}

func diffStringSets(base, current []string) Count {
	left, right := map[string]struct{}{}, map[string]struct{}{}
	for _, value := range base {
		left[value] = struct{}{}
	}
	for _, value := range current {
		right[value] = struct{}{}
	}
	var result Count
	for value := range right {
		if _, ok := left[value]; !ok {
			result.Added++
		}
	}
	for value := range left {
		if _, ok := right[value]; !ok {
			result.Removed++
		}
	}
	return result
}

func diffKeyedValues(base, current map[string]string) Count {
	var result Count
	for key, value := range current {
		old, ok := base[key]
		if !ok {
			result.Added++
		} else if old != value {
			result.Changed++
		}
	}
	for key := range base {
		if _, ok := current[key]; !ok {
			result.Removed++
		}
	}
	return result
}
