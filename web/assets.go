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
		paths[entry.Name()] = fmt.Sprintf("/assets/%s?v=%x", entry.Name(), sum[:6])
	}
	return paths
}()

func assetURL(name string) string { return assetPaths[name] }
