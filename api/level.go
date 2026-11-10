package api

import (
	_ "embed"
	"encoding/json"
)

//go:embed package.json
var manifest []byte

// TODO: at the next stable release, raise oldestAPILevel to apiLevel, then remove what only the earlier levels need:
//   - api/*.d.ts: delete the @available apiLevel annotations, and merge defineAction's stream into run as an async generator.
//   - internal/proto: delete the reserved fields and renumber the rest.
//   - internal/api: delete unversionedLevel and reject requests without swipt-api-level.
var Level, OldestLevel = func() (int64, int64) {
	var fields struct {
		APILevel       int64 `json:"apiLevel"`
		OldestAPILevel int64 `json:"oldestAPILevel"`
	}
	if err := json.Unmarshal(manifest, &fields); err != nil {
		panic(err)
	}
	return fields.APILevel, fields.OldestAPILevel
}()
