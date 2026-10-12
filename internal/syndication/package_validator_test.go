package syndication

import "testing"

func TestQuickAppPackageUsesVelaManifest(t *testing.T) {
	data := zipBytes(t, `{"package":"com.example.app","versionName":"1.0.0"}`, map[string][]byte{"app.js": []byte("app")})
	if err := ValidateQuickAppPackage(data, "com.example.app"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateQuickAppPackage(data, "com.other.app"); err == nil {
		t.Fatal("accepted mismatched package id")
	}
	if err := ValidateQuickAppPackage(bundle(t), "com.example.app"); err == nil {
		t.Fatal("accepted a repository bundle as an RPK")
	}
}

func TestQuickAppPackageRejectsTraversal(t *testing.T) {
	data := zipBytes(t, `{"package":"com.example.app"}`, map[string][]byte{"../escape": []byte("bad")})
	if err := ValidateQuickAppPackage(data, "com.example.app"); err == nil {
		t.Fatal("accepted path traversal")
	}
}
