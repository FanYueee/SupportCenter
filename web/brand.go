package web

import "context"

// Brand is the logo set shown for a hostname. Every hostname shares the same
// accounts, sign-in, and data; only these images and the logo text differ.
type Brand struct {
	Key        string
	Name       string
	LogoAlt    string
	Logo       string
	Icon       string
	LogoWidth  string
	LogoHeight string
}

const DefaultBrand = "atomroute"

var brands = map[string]Brand{
	"atomroute": {Key: "atomroute", Name: "AtomRoute", LogoAlt: "AtomRoute Global Network", Logo: "atomroute-logo.png", Icon: "atomroute-icon.png", LogoWidth: "1200", LogoHeight: "280"},
	"vproxy":    {Key: "vproxy", Name: "vProxy Cloud", LogoAlt: "vProxy Cloud", Logo: "vproxy-logo.png", Icon: "vproxy-icon.png", LogoWidth: "824", LogoHeight: "280"},
}

func KnownBrand(key string) bool { _, ok := brands[key]; return ok }

type brandKey struct{}

func WithBrand(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, brandKey{}, key)
}

func brandOf(ctx context.Context) Brand {
	if key, ok := ctx.Value(brandKey{}).(string); ok {
		if b, ok := brands[key]; ok {
			return b
		}
	}
	return brands[DefaultBrand]
}
