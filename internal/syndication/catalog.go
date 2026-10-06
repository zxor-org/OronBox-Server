package syndication

import (
	"encoding/csv"
	"strings"
)

// CatalogCSVHeader is the central repo index.csv header (community-repository-spec §2.3).
const CatalogCSVHeader = "id,name,restype,author,repo,repo_commit_hash,icon,cover,tags,device_vendors,devices,paid_type"

// CatalogRow is one index.csv row.
type CatalogRow struct {
	ID, Name, Restype, Author, Repo, RepoCommitHash, Icon, Cover, Tags, DeviceVendors, Devices, PaidType string
}

func (r CatalogRow) values() []string {
	return []string{r.ID, r.Name, r.Restype, r.Author, r.Repo, r.RepoCommitHash, r.Icon, r.Cover, r.Tags, r.DeviceVendors, r.Devices, r.PaidType}
}

// UpsertCatalogRow replaces the row with the same id (or appends it) and returns
// the full CSV. It is idempotent and preserves unrelated rows/columns.
func UpsertCatalogRow(existing string, row CatalogRow) (string, error) {
	records := [][]string{strings.Split(CatalogCSVHeader, ",")}
	if strings.TrimSpace(existing) != "" {
		r := csv.NewReader(strings.NewReader(existing))
		r.FieldsPerRecord = -1
		parsed, err := r.ReadAll()
		if err != nil {
			return "", err
		}
		if len(parsed) > 0 {
			records = parsed
		}
	}
	values := row.values()
	replaced := false
	for i := 1; i < len(records); i++ {
		if len(records[i]) > 0 && records[i][0] == row.ID {
			records[i] = values
			replaced = true
			break
		}
	}
	if !replaced {
		records = append(records, values)
	}
	var b strings.Builder
	w := csv.NewWriter(&b)
	if err := w.WriteAll(records); err != nil {
		return "", err
	}
	w.Flush()
	return b.String(), w.Error()
}
