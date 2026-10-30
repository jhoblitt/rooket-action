package tools

import "strings"

// The rooket release the helper installs by default. Moving it is manual:
// verify the cosign bundle over the release's checksums.txt against the
// identity in rooketSecondSource, then copy the linux/amd64 archive's sha256
// from that file. The version stays below v2 while rooket's module path has
// no /vN suffix.
const (
	defaultRooketVersion = "v0.1.0"
	defaultRooketSHA256  = "f528fd390223bfcf12b10227c8be5851a9c5048ae45f2df41e0b3a6951d56e17"
	rooketSecondSource   = "cosign keyless bundle checksums.txt.sigstore.json over the release's checksums.txt, " +
		"identity https://github.com/jhoblitt/rooket/.github/workflows/release.yml@refs/heads/main, " +
		"issuer https://token.actions.githubusercontent.com"
)

// Rooket returns the pin for rooket's release version, whose linux/amd64
// archive has the hex digest sha256. version may omit its leading v; the
// pin's Version carries it. Rooket does not check that version is
// well-formed. The pin's SecondSource is empty, because the caller vouches
// for the hash.
func Rooket(version, sha256 string) Pin {
	v := strings.TrimPrefix(version, "v")
	return Pin{
		Name:    "rooket",
		Version: "v" + v,
		URL:     "https://github.com/jhoblitt/rooket/releases/download/v" + v + "/rooket_" + v + "_linux_amd64.tar.gz",
		SHA256:  sha256,
		Member:  "rooket",
	}
}

// DefaultRooket returns the pin for the rooket release the helper installs
// unless a consumer overrides it.
func DefaultRooket() Pin {
	p := Rooket(defaultRooketVersion, defaultRooketSHA256)
	p.SecondSource = rooketSecondSource
	return p
}
