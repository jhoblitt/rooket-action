// Package tools installs the kind, kubectl and helm releases prepare pins,
// each checked against a sha256 compiled into the helper.
package tools

// Pin fixes one tool release to the sha256 of the file its URL serves.
type Pin struct {
	// Name is the tool's name, and the file Install writes into the
	// binary directory.
	Name string
	// Version is the release's tag.
	Version string
	// URL serves the binary itself or, when Member is set, a gzipped tar
	// archive holding it.
	URL string
	// SHA256 is the hex digest of the file URL serves: the archive's, when
	// Member is set.
	SHA256 string
	// Member is the binary's path inside the archive, and empty for a bare
	// binary.
	Member string
	// SecondSource names where the hash was confirmed, independently of
	// the host that serves URL.
	SecondSource string
}

// Pins returns the kind, kubectl and helm releases prepare installs.
func Pins() []Pin {
	return []Pin{
		{
			Name:         "kind",
			Version:      "v0.32.0",
			URL:          "https://kind.sigs.k8s.io/dl/v0.32.0/kind-linux-amd64",
			SHA256:       "50030de23cf40a18505f20426f6a8506bedf13c6e509244bd1fa9463721b0f54",
			SecondSource: "kind's GitHub release",
		},
		{
			Name:         "kubectl",
			Version:      "v1.33.1",
			URL:          "https://dl.k8s.io/release/v1.33.1/bin/linux/amd64/kubectl",
			SHA256:       "5de4e9f2266738fd112b721265a0c1cd7f4e5208b670f811861f699474a100a3",
			SecondSource: "Kubernetes changelog sha512 of the client tarball",
		},
		{
			Name:         "helm",
			Version:      "v3.20.1",
			URL:          "https://get.helm.sh/helm-v3.20.1-linux-amd64.tar.gz",
			SHA256:       "0165ee4a2db012cc657381001e593e981f42aa5707acdd50658326790c9d0dc3",
			Member:       "linux-amd64/helm",
			SecondSource: "helm's GitHub release",
		},
	}
}
