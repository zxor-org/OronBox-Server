package astrobox

import (
	"fmt"
	"path"
	"strings"
)

// Share identical packages; only suffix a filename when different bytes collide.
func artifactPaths(artifacts []artifact) (map[string]string, error) {
	paths, claimed := map[string]string{}, map[string]string{}
	for _, item := range artifacts {
		devices := supportedArtifactDevices(item.Devices)
		if len(devices) == 0 {
			continue
		}
		if paths[item.Blob] != "" {
			continue
		}
		name := path.Base(strings.ReplaceAll(strings.TrimSpace(item.Name), "\\", "/"))
		if name == "" || name == "." || name == ".." || name == "/" || strings.ContainsAny(name, "\r\n\x00") {
			return nil, fmt.Errorf("invalid AstroBox package filename")
		}
		candidate := name
		if claimed[candidate] != "" {
			ext := path.Ext(name)
			candidate = strings.TrimSuffix(name, ext) + "-" + devices[0].ID + ext
			if claimed[candidate] != "" && claimed[candidate] != item.Blob {
				return nil, fmt.Errorf("conflicting AstroBox package filename %s", candidate)
			}
		}
		claimed[candidate] = item.Blob
		paths[item.Blob] = "downloads/" + candidate
	}
	return paths, nil
}

func resourcePRTitle(mode, name, current string) string {
	prefix, action := "[OBCC]", "Add new resource"
	if mode == "edit" {
		action = "Update resource"
	}
	for _, p := range []string{"[ABCC]", "[OBCC]"} {
		if strings.HasPrefix(current, p+" Add ") {
			prefix, action = p, "Add new resource"
		}
		if strings.HasPrefix(current, p+" Update ") {
			prefix, action = p, "Update resource"
		}
		for _, a := range []string{"Add new resource", "Update resource"} {
			if strings.HasPrefix(current, p+" "+a+":") {
				prefix, action = p, a
			}
		}
	}
	return prefix + " " + action + ": " + strings.TrimSpace(name)
}
