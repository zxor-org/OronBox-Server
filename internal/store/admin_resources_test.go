package store

import "testing"

func TestAdminExternalBindingPresentation(t *testing.T) {
	band := AdminExternalBinding{
		Provider:   "bandbbs",
		ExternalID: `{"102":"4324","101":"4325"}`,
	}
	band.present(nil)
	if len(band.Entries) != 2 || band.Entries[0].Label != "分区 101" || band.Entries[0].Value != "资源 4325" {
		t.Fatalf("BandBBS entries = %#v", band.Entries)
	}
	if band.Entries[0].URL != "https://www.bandbbs.cn/resources/4325/" {
		t.Fatalf("BandBBS entry URL = %q", band.Entries[0].URL)
	}

	astro := AdminExternalBinding{Provider: "astrobox", ExternalID: "moe.orpu.neomusic"}
	astro.present([]byte(`{"repo_owner":"OrPudding","repo_name":"NeoMusic-AstroBox-Resource","imported":"true"}`))
	if astro.Repository != "OrPudding/NeoMusic-AstroBox-Resource" {
		t.Fatalf("AstroBox repository = %q", astro.Repository)
	}
	if len(astro.Entries) != 1 || astro.Entries[0].Value != "moe.orpu.neomusic" {
		t.Fatalf("AstroBox entries = %#v", astro.Entries)
	}
}
