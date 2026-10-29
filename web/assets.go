package web

import (
	"crypto/sha256"
	"embed"
	"fmt"
)

//go:embed assets/*
var Assets embed.FS

// Content versions prevent a browser from keeping old styles after deployment.
// Compute once; serving a page does not read or hash the assets again.
var assetVersions = make(map[string]string)

var assetPaths = func() map[string]string {
	paths := make(map[string]string)
	entries, err := Assets.ReadDir("assets")
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		data, err := Assets.ReadFile("assets/" + entry.Name())
		if err != nil {
			panic(err)
		}
		sum := sha256.Sum256(data)
		assetVersions[entry.Name()] = fmt.Sprintf("%x", sum[:6])
		paths[entry.Name()] = "/assets/" + entry.Name() + "?v=" + assetVersions[entry.Name()]
	}
	return paths
}()

func assetURL(name string) string { return assetPaths[name] }

// AssetVersion is the content hash of an embedded asset, used as its ETag and
// as the ?v= value in page links.
func AssetVersion(name string) (string, bool) {
	version, ok := assetVersions[name]
	return version, ok
}
