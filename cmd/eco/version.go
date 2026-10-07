package main

import (
	"fmt"
	"runtime/debug"
)

const develVersion = "(devel)"

const revisionWidth = 12

func buildStamp() (string, string, bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return develVersion, "", false
	}
	version := info.Main.Version
	if version == "" || version == develVersion {
		version = develVersion
	}
	revision := ""
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if len(revision) > revisionWidth {
		revision = revision[:revisionWidth]
	}
	return version, revision, modified
}

func versionLine() string {
	version, revision, modified := buildStamp()
	if revision == "" {
		return "eco " + version
	}
	if modified {
		return fmt.Sprintf("eco %s (%s, dirty)", version, revision)
	}
	return fmt.Sprintf("eco %s (%s)", version, revision)
}

func revisionLine(revision string) string {
	if revision == "" {
		return "(none)"
	}
	return revision
}