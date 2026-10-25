package config

import "runtime/debug"

var buildVersion = "unreleased"
var buildCommit string

func buildMetadata() (string, string) {
	commit := buildCommit
	modified := false
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if commit == "" {
					commit = setting.Value
				}
			case "vcs.modified":
				modified = setting.Value == "true"
			}
		}
	}
	if modified && commit != "" {
		commit += "-dirty"
	}
	return buildVersion, commit
}
