package templates

import "embed"

//go:embed templates/*
var templatesFS embed.FS

// GetTemplatesFS returns the embedded templates filesystem
func GetTemplatesFS() embed.FS {
	return templatesFS
}
