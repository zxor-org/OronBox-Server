package store

import (
	"encoding/json"
	"testing"
)

func TestAdminJSONInputsUsePublicFieldNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		value any
		check func(t *testing.T, value any)
	}{
		{
			name:  "collection",
			input: `{"name":"Suite","summary":"Summary","enabled":true,"representative_resource_id":"resource","resource_ids":["resource"]}`,
			value: new(AdminCollectionMetadataInput),
			check: func(t *testing.T, value any) {
				input := value.(*AdminCollectionMetadataInput)
				if input.Name != "Suite" || input.RepresentativeResourceID != "resource" || len(input.ResourceIDs) != 1 || !input.Enabled {
					t.Fatalf("collection input = %#v", *input)
				}
			},
		},
		{
			name:  "device",
			input: `{"display_name":"Band 11","codename":"p66","platform":"vela_os","vendor":"xiaomi","astrobox_id":"xmb11","enabled":true}`,
			value: new(AdminDeviceInput),
			check: func(t *testing.T, value any) {
				input := value.(*AdminDeviceInput)
				if input.DisplayName != "Band 11" || input.Codename != "p66" || input.AstroBoxID != "xmb11" || !input.Enabled {
					t.Fatalf("device input = %#v", *input)
				}
			},
		},
		{
			name:  "release notes",
			input: `{"minimum_version":"1.1.0","notes_zh":"中文","notes_en":"English"}`,
			value: new(AdminReleaseNotesInput),
			check: func(t *testing.T, value any) {
				input := value.(*AdminReleaseNotesInput)
				if input.MinimumVersion != "1.1.0" || input.NotesZH != "中文" || input.NotesEN != "English" {
					t.Fatalf("release notes input = %#v", *input)
				}
			},
		},
		{
			name:  "plugin metadata",
			input: `{"name":"Plugin","author":"Author","description":"Description"}`,
			value: new(AdminPluginMetadataRevisionInput),
			check: func(t *testing.T, value any) {
				input := value.(*AdminPluginMetadataRevisionInput)
				if input.Name != "Plugin" || input.Author != "Author" || input.Description != "Description" {
					t.Fatalf("plugin metadata input = %#v", *input)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(test.input), test.value); err != nil {
				t.Fatal(err)
			}
			test.check(t, test.value)
		})
	}
}
