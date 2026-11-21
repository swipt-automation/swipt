//go:build !android

package declarations

import _ "embed"

//go:embed ios.d.ts
var TypeScript string
