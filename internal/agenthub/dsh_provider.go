package agenthub

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// dshProvider registers the DeepSeek Harness runtime as a built-in Alpha
// Agent provider. The runtime mounts the Alpha-Nexus business tools plugin
// (read-only) and routes model traffic through the deployment's own
// configured gateway, so no credentials cross this boundary.
func dshProvider() Provider {
	resolve := func() string { return resolvePath("dsh") }
	return Provider{
		Descriptor: ProviderDescriptor{
			ID: ProviderDsh, Label: "DSH Agent", BuiltIn: true,
			Capabilities: []string{"DeepSeek Harness runtime", "chat + read-only business tools", "no broker", "streaming chat", "native resume", "cancel"},
		},
		SupportsBroker: false,
		Probe: func(ctx context.Context) ProviderProbe {
			return (commandProbe{id: ProviderDsh, binary: resolve, ready: true, reason: "DSH runtime not found", version: commandVersion}).Probe(ctx)
		},
		ModelProbe: func(ctx context.Context) ProviderProbe {
			probe := (commandProbe{id: ProviderDsh, binary: resolve, ready: true, reason: "DSH runtime not found", version: commandVersion}).Probe(ctx)
			if !probe.Available || !probe.Ready {
				return probe
			}
			// Mirror the whole DSH provider/model catalog 1:1 so the app can
			// offer exactly the routes the dsh runtime accepts.
			if home, err := os.UserHomeDir(); err == nil {
				if catalog, catalogErr := ReadDshCatalogForProfile(home, "alpha"); catalogErr == nil && len(catalog.ModelIDs) > 0 {
					probe.ModelCandidates = catalog.ModelIDs
					// The deployment default route is the pair the dsh runtime
					// itself launches with, so it is already exercised by every
					// dsh start; the rest still need one nonce call.
					if route, ok := dshRouteID(catalog.DefaultProvider, catalog.DefaultModel); ok {
						probe.Models = []string{route}
					}
					return probe
				}
			}
			model := dshDefaultModel()
			if model == "" {
				probe.Ready = false
				probe.Reason = "DSH default model not configured (agent-default-model)"
				return probe
			}
			probe.ModelCandidates = []string{model}
			return probe
		},
		ValidateModel: func(ctx context.Context, modelID, workDir, nonce string) error {
			_, err := runModelValidation(ctx, &dshRunner{binary: resolve}, ProviderDsh, modelID, workDir, nonce)
			return err
		},
		Runner: &dshRunner{binary: resolve},
	}
}

var (
	dshDefaultModelSection      = regexp.MustCompile(`(?m)^agent-default-model:\s*\n((?:[ \t]+.*\n?)*)`)
	dshDefaultModelPatchSection = regexp.MustCompile(`(?m)^-\s+id:\s*agent-default-model\s*\n(?:\s+name:\s*[^\n]+\n)?\s+config:\s*\n((?:[ \t]+.*\n?)*)`)
	dshProviderField            = regexp.MustCompile(`(?m)^\s+provider:\s*([^\s#]+)`)
	dshModelField               = regexp.MustCompile(`(?m)^\s+model:\s*([^\s#]+)`)
)

// dshDefaultRoute reads the deployment default provider/model route from the
// DSH user settings (~/.dsh/cordis.patch.yml or ~/.dsh/settings.yaml agent-default-model). The SDK
// runtime routes every SDK-created agent through that deployment route and
// rejects unknown provider names during initialize, so the app's own provider
// id must never be sent on the wire.
func dshDefaultRoute() (string, string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", ""
	}
	return dshDefaultRouteFrom(home)
}

// dshDefaultRouteFrom resolves the route against an explicit home directory.
// Taking home as an argument keeps the candidate lookup testable without
// depending on the machine's real ~/.dsh contents.
func dshDefaultRouteFrom(home string) (string, string) {
	if home == "" {
		return "", ""
	}
	candidates := []string{
		filepath.Join(home, ".dsh", "profiles", "alpha", "cordis.patch.yml"),
		filepath.Join(home, ".dsh", "cordis.patch.yml"),
		filepath.Join(home, ".dsh", "settings.yaml"),
	}
	for _, candidate := range candidates {
		raw, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		var section []string
		if strings.HasSuffix(candidate, ".patch.yml") {
			section = dshDefaultModelPatchSection.FindStringSubmatch(string(raw))
		} else {
			section = dshDefaultModelSection.FindStringSubmatch(string(raw))
		}
		if section == nil {
			continue
		}
		provider := ""
		model := ""
		if match := dshProviderField.FindStringSubmatch(section[1]); match != nil {
			provider = strings.TrimSpace(match[1])
		}
		if match := dshModelField.FindStringSubmatch(section[1]); match != nil {
			model = strings.TrimSpace(match[1])
		}
		if model != "" {
			return provider, model
		}
	}
	return "", ""
}

// dshRouteForModel resolves an app model id to a DSH provider/model pair, but
// only when that pair exists in the mirrored catalog. Unknown ids keep the
// deployment default so a stale selection never breaks the runtime handshake.
func dshRouteForModel(modelID string) (string, string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return dshDefaultRoute()
	}
	catalog, catalogErr := ReadDshCatalogForProfile(home, "alpha")
	if catalogErr != nil {
		return dshDefaultRoute()
	}
	if provider, model, ok := dshRouteFromModelID(modelID); ok {
		for _, entry := range catalog.Providers {
			if entry.ID != provider {
				continue
			}
			for _, candidate := range entry.Models {
				if candidate.ID == model {
					return provider, model
				}
			}
		}
	}
	if catalog.DefaultModel == "" {
		return dshDefaultRoute()
	}
	return catalog.DefaultProvider, catalog.DefaultModel
}

// dshDefaultModel reads the deployment default model for probe reporting.
func dshDefaultModel() string {
	_, model := dshDefaultRoute()
	return model
}

// dshDefaultModelFrom is the injectable-home form of dshDefaultModel.
func dshDefaultModelFrom(home string) string {
	_, model := dshDefaultRouteFrom(home)
	return model
}
