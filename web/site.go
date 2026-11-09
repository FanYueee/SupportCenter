package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"strconv"
	"strings"

	_ "golang.org/x/image/webp"
	"supportcenter/internal/i18n"
)

// Site is how this support center presents itself. The operator sets it with
// the SITE_* settings; without them pages say only 支援中心 and use a plain icon.
type Site struct {
	Name    string // shown before 支援中心, e.g. "Example Cloud"
	Owner   string // copyright holder in the footer; Name when empty
	Website string // the footer links here when set
	Logo    *Image // wide header logo; without one the header shows the icon and name
	Icon    *Image // square icon for phones, the sign-in page and the browser tab
	PlanURL string // where customers see and buy support plans, from their plan in the header
}

// Title names the site in emails and Discord messages, which are in Chinese.
func (s Site) Title() string { return strings.TrimSpace(s.Name + " 支援中心") }

func (s Site) holder() string {
	if s.Owner != "" {
		return s.Owner
	}
	return s.Name
}

// Image is an operator's logo file, read once at startup and served from memory.
type Image struct {
	File          string // served at /branding/<File>
	Type          string
	Width, Height int
	Data          []byte
	Version       string // content hash, used as the ETag and ?v= value
	Accent        string // the logo's main color, dark enough for white text; "" when it has none
}

func (i *Image) URL() string { return "/branding/" + i.File + "?v=" + i.Version }

// headerSize fits a logo in the header: 48px tall and at most 240px wide.
func (i *Image) headerSize() (width, height int) { return i.fit(48, 240) }

func (i *Image) fit(maxHeight, maxWidth int) (width, height int) {
	width, height = i.Width*maxHeight/i.Height, maxHeight
	if width > maxWidth {
		width, height = maxWidth, i.Height*maxWidth/i.Width
	}
	return max(width, 1), max(height, 1)
}

func (i *Image) headerWidth() string  { w, _ := i.headerSize(); return strconv.Itoa(w) }
func (i *Image) headerHeight() string { _, h := i.headerSize(); return strconv.Itoa(h) }

const maxImageBytes = 1 << 20

// LoadImage reads a PNG, JPEG or WebP file to serve as name plus its extension.
func LoadImage(path, name string) (*Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	image, err := NewImage(data, name)
	if err != nil {
		return nil, errors.New(path + ": " + err.Error())
	}
	return image, nil
}

func NewImage(data []byte, name string) (*Image, error) {
	if len(data) > maxImageBytes {
		return nil, errors.New("the image is larger than 1 MiB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	ext := map[string]string{"png": "png", "jpeg": "jpg", "webp": "webp"}[format]
	if err != nil || ext == "" {
		return nil, errors.New("the image must be a PNG, JPEG or WebP file")
	}
	if config.Width < 16 || config.Height < 16 || config.Width > 4096 || config.Height > 4096 {
		return nil, errors.New("the image must be between 16 and 4096 pixels on each side")
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("the image must be a PNG, JPEG or WebP file")
	}
	sum := sha256.Sum256(data)
	return &Image{File: name + "." + ext, Type: "image/" + format, Width: config.Width, Height: config.Height, Data: data, Version: fmt.Sprintf("%x", sum[:6]), Accent: accentColor(decoded)}, nil
}

// accentColor finds the most common vivid hue in a logo, ignoring transparent,
// grey, near-white and near-black pixels, and darkens it until white text on it
// reaches a 4.5:1 contrast. A logo without such color has no accent.
func accentColor(img image.Image) string {
	type bucket struct{ count, r, g, b float64 }
	var hues [24]bucket
	var opaque, vivid float64
	bounds := img.Bounds()
	step := max(1, int(math.Sqrt(float64(bounds.Dx()*bounds.Dy())/250000)))
	for y := bounds.Min.Y; y < bounds.Max.Y; y += step {
		for x := bounds.Min.X; x < bounds.Max.X; x += step {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if c.A < 128 {
				continue
			}
			opaque++
			r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
			hi, lo := max(r, g, b), min(r, g, b)
			if hi < 0.2 || hi-lo < 0.3 || (hi > 0.95 && lo > 0.85) {
				continue
			}
			var hue float64
			switch hi {
			case r:
				hue = math.Mod((g-b)/(hi-lo)+6, 6)
			case g:
				hue = (b-r)/(hi-lo) + 2
			default:
				hue = (r-g)/(hi-lo) + 4
			}
			h := &hues[int(hue*4)%24]
			h.count++
			h.r, h.g, h.b = h.r+r, h.g+g, h.b+b
			vivid++
		}
	}
	best := 0
	for i := range hues {
		if hues[i].count > hues[best].count {
			best = i
		}
	}
	if vivid == 0 || vivid < opaque/100 {
		return ""
	}
	h := hues[best]
	r, g, b := h.r/h.count, h.g/h.count, h.b/h.count
	linear := func(v float64) float64 {
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	for 0.2126*linear(r)+0.7152*linear(g)+0.0722*linear(b) > 1.05/4.5-0.05 {
		r, g, b = r*0.95, g*0.95, b*0.95
	}
	return fmt.Sprintf("#%02x%02x%02x", int(math.Round(r*255)), int(math.Round(g*255)), int(math.Round(b*255)))
}

type siteKey struct{}

func WithSite(ctx context.Context, s Site) context.Context {
	return context.WithValue(ctx, siteKey{}, s)
}

func siteOf(ctx context.Context) Site {
	s, _ := ctx.Value(siteKey{}).(Site)
	return s
}

// siteTitle is the site's name in the reader's language, e.g. "Example 支援中心".
func siteTitle(ctx context.Context) string {
	return strings.TrimSpace(siteOf(ctx).Name + " " + i18n.T(ctx, "支援中心"))
}

func signInHeading(ctx context.Context) string {
	if siteOf(ctx).Name == "" {
		return i18n.T(ctx, "登入支援中心")
	}
	return strings.ReplaceAll(i18n.T(ctx, "登入 {site}"), "{site}", siteTitle(ctx))
}

func websiteLabel(ctx context.Context) string {
	name := siteOf(ctx).Name
	if name == "" {
		name = siteOf(ctx).holder()
	}
	return strings.ReplaceAll(i18n.T(ctx, "{name} 官方網站"), "{name}", name)
}

func iconURL(ctx context.Context) string {
	if icon := siteOf(ctx).Icon; icon != nil {
		return icon.URL()
	}
	return assetURL("icon.svg")
}

func iconType(ctx context.Context) string {
	if icon := siteOf(ctx).Icon; icon != nil {
		return icon.Type
	}
	return "image/svg+xml"
}
