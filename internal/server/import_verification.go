package server

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/creator"
)

// Read only the configured provider APIs. Submitted URLs never become request
// destinations, and submitted metadata is not evidence of ownership.
func (a *App) verifyImportedBinding(ctx context.Context, uid string, b creator.ImportedBinding) (creator.ImportedBinding, error) {
	request := func(endpoint, token string, out any) error {
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
		res, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("binding verification upstream unavailable")
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return fmt.Errorf("binding verification HTTP %d", res.StatusCode)
		}
		return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out)
	}
	switch b.Provider {
	case "bandbbs":
		user, err := a.store.UserByID(ctx, uid)
		if err != nil {
			return b, err
		}
		next, subject, err := a.bandbbs.GrantToken(ctx, a.store, a.secrets, uid, "bandbbs_publish", "")
		if err != nil {
			return b, err
		}
		if subject != strconv.FormatInt(user.BandBBSUserID, 10) {
			return b, fmt.Errorf("BandBBS authorization subject mismatch")
		}
		token := next.AccessToken
		ids := map[string]string{}
		if strings.HasPrefix(b.ExternalID, "{") {
			if json.Unmarshal([]byte(b.ExternalID), &ids) != nil {
				return b, fmt.Errorf("invalid BandBBS identities")
			}
		} else {
			ids[""] = b.ExternalID
		}
		if len(ids) == 0 || len(ids) > 50 {
			return b, fmt.Errorf("invalid BandBBS identities")
		}
		canonical := map[string]string{}
		for category, id := range ids {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil || n <= 0 {
				return b, fmt.Errorf("invalid BandBBS identity")
			}
			var body struct {
				Resource struct {
					User struct {
						ID int64 `json:"user_id"`
					} `json:"User"`
					UserID     int64 `json:"user_id"`
					CategoryID int   `json:"resource_category_id"`
				} `json:"resource"`
			}
			if err := request(strings.TrimRight(a.cfg.BandBBS.APIURL, "/")+"/resources/"+strconv.FormatInt(n, 10), token, &body); err != nil {
				return b, err
			}
			r := body.Resource
			owner := r.UserID
			if owner == 0 {
				owner = r.User.ID
			}
			if owner != user.BandBBSUserID || r.CategoryID <= 0 {
				return b, fmt.Errorf("BandBBS resource does not belong to authorized user")
			}
			actual := strconv.Itoa(r.CategoryID)
			if category != "" && category != actual {
				return b, fmt.Errorf("BandBBS category mismatch")
			}
			if prior, ok := canonical[actual]; ok && prior != id {
				return b, fmt.Errorf("duplicate BandBBS category")
			}
			canonical[actual] = strconv.FormatInt(n, 10)
		}
		raw, _ := json.Marshal(canonical)
		b.ExternalID = string(raw)
		b.ExternalURL = "https://www.bandbbs.cn/resources/"
		b.Meta = map[string]string{"identity_kind": "resource"}
		return b, nil
	case "astrobox":
		cipher, err := a.store.GitHubAccessTokenCipher(ctx, uid)
		if err != nil {
			return b, fmt.Errorf("GitHub authorization required")
		}
		token, err := a.secrets.Decrypt(cipher)
		if err != nil {
			return b, err
		}
		var file struct {
			Content  string `json:"content"`
			Encoding string `json:"encoding"`
			SHA      string `json:"sha"`
		}
		api := strings.TrimRight(a.cfg.GitHub.APIURL, "/")
		path := "/repos/" + url.PathEscape(a.cfg.AstroBox.RepoOwner) + "/" + url.PathEscape(a.cfg.AstroBox.RepoName) + "/contents/" + a.cfg.AstroBox.CatalogPath + "?ref=" + url.QueryEscape(a.cfg.AstroBox.RepoBranch)
		if err := request(api+path, token, &file); err != nil {
			return b, err
		}
		raw, err := decodeGitHubContent(file.Content)
		if err != nil {
			return b, err
		}
		reader := csv.NewReader(strings.NewReader(string(raw)))
		reader.FieldsPerRecord = -1
		rows, err := reader.ReadAll()
		if err != nil {
			return b, err
		}
		var match []string
		for i, row := range rows {
			if i > 0 && len(row) == 12 && row[0] == b.ExternalID {
				if match != nil {
					return b, fmt.Errorf("ambiguous AstroBox identity")
				}
				match = row
			}
		}
		if match == nil {
			return b, fmt.Errorf("AstroBox identity absent from official catalog")
		}
		owner, name := match[3], match[4]
		var repo struct {
			Permissions struct {
				Push  bool `json:"push"`
				Admin bool `json:"admin"`
			} `json:"permissions"`
			Archived bool `json:"archived"`
		}
		if err := request(api+"/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name), token, &repo); err != nil {
			return b, err
		}
		if repo.Archived || (!repo.Permissions.Push && !repo.Permissions.Admin) {
			return b, fmt.Errorf("GitHub maintenance permission required")
		}
		if v := b.Meta["repo_name"]; v != "" && v != name {
			return b, fmt.Errorf("AstroBox repository mismatch")
		}
		if v := b.Meta["repo_owner"]; v != "" && !strings.EqualFold(v, owner) {
			return b, fmt.Errorf("AstroBox owner mismatch")
		}
		b.ExternalURL = "https://github.com/" + owner + "/" + name
		b.Meta = map[string]string{"item_id": b.ExternalID, "repo_owner": owner, "repo_name": name, "repo_commit_hash": match[5], "catalog_commit": file.SHA}
		return b, nil
	default:
		return b, fmt.Errorf("unsupported binding provider")
	}
}

func decodeGitHubContent(value string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.ReplaceAll(value, "\n", ""))
}
