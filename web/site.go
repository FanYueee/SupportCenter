package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
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
}

func (i *Image) URL() string { return "/branding/" + i.File + "?v=" + i.Version }

// headerSize fits a logo in the header: 48px tall and at most 240px wide.
func (i *Image) headerSize() (width, height int) {
	width, height = i.Width*48/i.Height, 48
	if width > 240 {
		width, height = 240, i.Height*240/i.Width
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
	sum := sha256.Sum256(data)
	return &Image{File: name + "." + ext, Type: "image/" + format, Width: config.Width, Height: config.Height, Data: data, Version: fmt.Sprintf("%x", sum[:6])}, nil
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
