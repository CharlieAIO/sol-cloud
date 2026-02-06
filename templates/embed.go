package templates

import "embed"

// Files contains deployment templates embedded from this directory.
//
//go:embed *.tmpl
var Files embed.FS

func ReadFile(name string) ([]byte, error) {
	return Files.ReadFile(name)
}
