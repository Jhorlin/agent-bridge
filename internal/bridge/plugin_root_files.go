package bridge

import "regexp"

var pluginRootImage = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}\.(png|jpg|jpeg|webp|svg)$`)

// These files do not alter native component discovery. Keep this bounded:
// arbitrary root files can be another host's configuration or credentials.
func pluginRootSupportingFile(name string) bool {
	switch name {
	case "PRIVACY.md", "CHANGELOG.md", "NOTICE", "LICENSE.md", "LICENSE.txt":
		return true
	default:
		return pluginRootImage.MatchString(name)
	}
}
