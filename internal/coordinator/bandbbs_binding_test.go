package coordinator

import (
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
