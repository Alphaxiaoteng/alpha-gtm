package egolite

import (
	"context"
	"encoding/json"
	"fmt"
)

// EgoProfile is one browser profile owned by ego-lite.
type EgoProfile struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
}

// ListProfiles returns the read-only ego-lite profile library. It never opens a
// profile and never reads its contents; only the id/name/default flags are used.
func (service *SpaceService) ListProfiles(ctx context.Context) ([]EgoProfile, error) {
	if service == nil || service.runtime == nil {
		return nil, fmt.Errorf("EGO_RUNTIME_UNAVAILABLE")
	}
	prefixJSON, _ := json.Marshal(resultPrefix)
	program := fmt.Sprintf(`
const result = await listProfiles()
const items = Array.isArray(result) ? result : (Array.isArray(result?.profiles) ? result.profiles : [])
cliLog(%s + JSON.stringify(items.map((item) => ({
  id: String(item.id ?? ""),
  name: String(item.name ?? ""),
  isDefault: item.isDefault === true
}))))
`, prefixJSON)
	var raw []EgoProfile
	if err := service.runtime.Run(ctx, program, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		raw = []EgoProfile{}
	}
	return raw, nil
}
