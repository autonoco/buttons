package releaseconfig

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readRepoFile(t *testing.T, path ...string) string {
	t.Helper()

	parts := append([]string{"..", ".."}, path...)
	contents, err := os.ReadFile(filepath.Join(parts...))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(path...), err)
	}
	return string(contents)
}

func requireContains(t *testing.T, contents, want string) {
	t.Helper()
	if !strings.Contains(contents, want) {
		t.Errorf("configuration is missing %q", want)
	}
}

func TestReleaseWorkflowsShareSerializedReleaseGroup(t *testing.T) {
	want := "concurrency:\n  group: buttons-release\n  cancel-in-progress: false"

	for _, path := range []string{"auto-release.yml", "release.yml"} {
		t.Run(path, func(t *testing.T) {
			contents := readRepoFile(t, ".github", "workflows", path)
			requireContains(t, contents, want)
		})
	}
}

func TestAutoReleaseCanRecoverAnExistingTagWithoutCreatingANewTag(t *testing.T) {
	contents := readRepoFile(t, ".github", "workflows", "auto-release.yml")

	for _, want := range []string{
		"workflow_dispatch:",
		"ref: ${{ inputs.tag || github.sha }}",
		"REQUESTED_TAG=\"${{ inputs.tag }}\"",
		"git rev-parse --verify \"refs/tags/$REQUESTED_TAG^{commit}\"",
		"echo \"next=$REQUESTED_TAG\" >> \"$GITHUB_OUTPUT\"",
		"echo \"create_tag=false\" >> \"$GITHUB_OUTPUT\"",
		"echo \"create_tag=true\" >> \"$GITHUB_OUTPUT\"",
		"if: steps.version.outputs.create_tag == 'true'",
	} {
		requireContains(t, contents, want)
	}
}

func TestRollingManifestUsesImmutableVersionedImages(t *testing.T) {
	contents := readRepoFile(t, ".goreleaser.yaml")

	for _, forbidden := range []string{
		"ghcr.io/autonoco/buttons:latest-amd64",
		"ghcr.io/autonoco/buttons:latest-arm64",
	} {
		if strings.Contains(contents, forbidden) {
			t.Errorf("rolling manifest still depends on mutable image %q", forbidden)
		}
	}

	rollingManifest := regexp.MustCompile(`(?s)- name_template: "ghcr\.io/autonoco/buttons:latest"\s+image_templates:\s+- "ghcr\.io/autonoco/buttons:\{\{ \.Version \}\}-amd64"\s+- "ghcr\.io/autonoco/buttons:\{\{ \.Version \}\}-arm64"`)
	if !rollingManifest.MatchString(contents) {
		t.Error("rolling manifest does not reference immutable versioned architecture images")
	}
}
