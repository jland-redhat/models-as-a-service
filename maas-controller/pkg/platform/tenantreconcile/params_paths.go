package tenantreconcile

import (
	"fmt"
	"path/filepath"
)

// ParamsEnvDirForManifest resolves deployment/base/maas-controller/default (params.env and the
// maas-parameters configMapGenerator) from an ODH overlay directory used for kustomize build
// (deployment/overlays/odh or maas-api/deploy/overlays/odh).
func ParamsEnvDirForManifest(manifestDir string) (string, error) {
	abs, err := filepath.Abs(manifestDir)
	if err != nil {
		return "", err
	}
	candidates := []string{
		filepath.Clean(filepath.Join(abs, "../../../../deployment/base/maas-controller/default")),
		filepath.Clean(filepath.Join(abs, "../../base/maas-controller/default")),
	}
	for _, d := range candidates {
		if fileExists(filepath.Join(d, "params.env")) && fileExists(filepath.Join(d, "kustomization.yaml")) {
			return d, nil
		}
	}
	return "", fmt.Errorf("could not find deployment/base/maas-controller/default with params.env from manifest directory %s", manifestDir)
}
