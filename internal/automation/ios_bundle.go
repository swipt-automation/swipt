//go:build !android && cgo

package automation

import "embed"

//go:embed bundles/ios/runtime.js
var runtimeSource string

//go:embed bundles/ios/chunks
var chunkSources embed.FS

const systemChunks = "bundles/ios/chunks"
