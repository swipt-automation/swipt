//go:build android && cgo

package automation

import "embed"

//go:embed bundles/android/runtime.js
var runtimeSource string

//go:embed bundles/android/chunks
var chunkSources embed.FS

const systemChunks = "bundles/android/chunks"
