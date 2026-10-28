package coordinator

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseBandBBSBindingAcceptsHistoricalFormats(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		target []int
		want   map[string]string
	}{
		{
			name: "canonical category map",
			raw:  `{"12":"345","13":"678"}`,
			want: map[string]string{"12": "345", "13": "678"},
		},
		{
			name: "publication result map",
			raw:  `{"12":{"resource_id":"345","url":"https://example.com/345"},"13":{"resource_id":"678","url":"https://example.com/678"}}`,
			want: map[string]string{"12": "345", "13": "678"},
		},
		{
			name:   "legacy single resource",
			raw:    `345`,
			target: []int{12},
			want:   map[string]string{"12": "345"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBandBBSBinding(tt.raw, tt.target)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseBandBBSBindingRejectsUnknownShape(t *testing.T) {
	if _, err := parseBandBBSBinding(`{"12":{"url":"https://example.com/345"}}`, []int{12}); err == nil {
		t.Fatal("expected malformed binding to fail")
	}
}

func TestBandBBSRetryPreservesTargetMapping(t *testing.T) {
	raw := []byte(`{"version_title":"Update","targets":[{"category_id":12,"prefix_id":82,"package_id":"same.app","artifact_sha256":"aaa","thread_prefix_id":4,"forum_id":8},{"category_id":13,"prefix_id":82,"package_id":"same.app","artifact_sha256":"bbb","thread_prefix_id":5,"forum_id":9}]}`)
	detail := []byte(`{"completed_categories":{"12":{"resource_id":"345","version_id":"v1","update_id":"u1"}}}`)
	filtered, _ := filterBandBBSConfig(raw, detail)
	var got struct {
		Targets []map[string]any `json:"targets"`
	}
	if err := json.Unmarshal(filtered, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Targets) != 1 || got.Targets[0]["artifact_sha256"] != "bbb" || got.Targets[0]["package_id"] != "same.app" || got.Targets[0]["prefix_id"] != float64(82) || got.Targets[0]["thread_prefix_id"] != float64(5) || got.Targets[0]["forum_id"] != float64(9) {
		t.Fatalf("retry lost target mapping: %s", filtered)
	}
}
