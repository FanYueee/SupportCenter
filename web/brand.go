package web

import "context"

// Brand is the logo set shown for a hostname. Every hostname shares the same
// accounts, sign-in, and data; only these images and the logo text differ.
type Brand struct {
	Key        string
	Name       string
	SignIn     string // sign-in heading, translated through internal/i18n
	Owner      string // copyright holder in the footer
	Website    string
	SiteLabel  string // footer link label, translated through internal/i18n
	LogoAlt    string
	Logo       string
	Icon       string
	LogoWidth  string
	LogoHeight string
}

const DefaultBrand = "atomroute"

var brands = map[string]Brand{
	"atomroute": {Key: "atomroute", Name: "AtomRoute", SignIn: "登入 AtomRoute 支援中心", Owner: "AtomRoute Global Network (US) LLC", Website: "https://www.atomroute.com/zh", SiteLabel: "AtomRoute 官方網站", LogoAlt: "AtomRoute Global Network", Logo: "atomroute-logo.png", Icon: "atomroute-icon.png", LogoWidth: "640", LogoHeight: "149"},
	"vproxy":    {Key: "vproxy", Name: "vProxy Cloud", SignIn: "登入 vProxy Cloud 支援中心", Owner: "vProxy Cloud", Website: "https://vproxy.cloud", SiteLabel: "vProxy Cloud 官方網站", LogoAlt: "vProxy Cloud", Logo: "vproxy-logo.png", Icon: "vproxy-icon.png", LogoWidth: "440", LogoHeight: "150"},
}

func KnownBrand(key string) bool { _, ok := brands[key]; return ok }

type brandKey struct{}

func WithBrand(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, brandKey{}, key)
}

// BrandKey is the brand of the site the request came in on.
func BrandKey(ctx context.Context) string { return brandOf(ctx).Key }

// BrandByKey returns a known brand, or the default one.
func BrandByKey(key string) Brand {
	if b, ok := brands[key]; ok {
		return b
	}
	return brands[DefaultBrand]
}

func brandOf(ctx context.Context) Brand {
	if key, ok := ctx.Value(brandKey{}).(string); ok {
		if b, ok := brands[key]; ok {
			return b
		}
	}
	return brands[DefaultBrand]
}
