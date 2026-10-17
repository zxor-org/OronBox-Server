package resources

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

func (binding *AdminExternalBinding) present(rawMeta []byte) {
	if binding.Provider == "bandbbs" {
		ids := map[string]any{}
		if json.Unmarshal([]byte(binding.ExternalID), &ids) == nil {
			keys := make([]string, 0, len(ids))
			for categoryID := range ids {
				keys = append(keys, categoryID)
			}
			sort.Slice(keys, func(i, j int) bool {
				left, leftErr := strconv.Atoi(keys[i])
				right, rightErr := strconv.Atoi(keys[j])
				if leftErr == nil && rightErr == nil {
					return left < right
				}
				return keys[i] < keys[j]
			})
			for _, categoryID := range keys {
				resourceID := ""
				resourceURL := ""
				switch value := ids[categoryID].(type) {
				case string:
					resourceID = value
				case map[string]any:
					resourceID, _ = value["resource_id"].(string)
					resourceURL, _ = value["url"].(string)
				}
				if resourceURL == "" && resourceID != "" {
					resourceURL = "https://www.bandbbs.cn/resources/" + resourceID + "/"
				}
				binding.Entries = append(binding.Entries, AdminExternalBindingEntry{
					Label: "分区 " + categoryID,
					Value: "资源 " + resourceID,
					URL:   resourceURL,
				})
			}
		}
		return
	}
	binding.Entries = append(binding.Entries, AdminExternalBindingEntry{Label: "资源 ID", Value: binding.ExternalID})
	meta := map[string]string{}
	if json.Unmarshal(rawMeta, &meta) == nil {
		binding.Repository = strings.Trim(strings.TrimSpace(meta["repo_owner"])+"/"+strings.TrimSpace(meta["repo_name"]), "/")
	}
}
