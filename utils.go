package main

import (
	"path/filepath"
	"strings"
)

func trimFileExtension(filename string) string {
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}
