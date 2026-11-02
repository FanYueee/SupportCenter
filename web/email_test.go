package web

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
	"testing"
	"time"
)

func pngLogo(t *testing.T, w, h int, paint func(x, y int) color.NRGBA) *Image {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, paint(x, y))
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	logo, err := NewImage(out.Bytes(), "logo")
	if err != nil {
		t.Fatal(err)
	}
	return logo
}

func TestLogoAccentIsItsMainVividColor(t *testing.T) {
	// Mostly transparent, a grey wordmark and a light purple mark too pale for white text.
	logo := pngLogo(t, 400, 100, func(x, y int) color.NRGBA {
		switch {
		case x < 80:
			return color.NRGBA{0x9b, 0x8c, 0xff, 0xff}
		case x < 90:
			return color.NRGBA{0xff, 0x80, 0x00, 0xff}
		case x < 300:
			return color.NRGBA{0x60, 0x60, 0x60, 0xff}
		}
		return color.NRGBA{}
	})
	var r, g, b int
	if _, err := fmt.Sscanf(logo.Accent, "#%02x%02x%02x", &r, &g, &b); err != nil || !(b > r && r > g) {
		t.Fatal("accent should be the purple, got", logo.Accent)
	}
	if contrast := contrastWithWhite(r, g, b); contrast < 4.5 {
		t.Fatal("white text on the accent is too faint", logo.Accent, contrast)
	}
	grey := pngLogo(t, 100, 100, func(x, y int) color.NRGBA { return color.NRGBA{0x30, 0x30, 0x30, 0xff} })
	if grey.Accent != "" {
		t.Fatal("a grey logo has no accent, got", grey.Accent)
	}
}

func TestEmailShowsLogoAndEscapesText(t *testing.T) {
	logo := pngLogo(t, 640, 149, func(x, y int) color.NRGBA { return color.NRGBA{0x66, 0x55, 0xf6, 0xff} })
	site := Site{Name: "Example Cloud", Owner: "Example Cloud Ltd.", Website: "https://example.com/zh", Logo: logo}
	v := NewEmailView(site, "https://support.example.test", time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC))
	v.Label, v.Heading, v.Intro = "新回覆", "客服單 #0001 有新回覆", []string{"您的客服單有新的回覆。"}
	v.Details = []EmailDetail{{Label: "主旨", Value: "<script>alert(1)</script>"}}
	v.QuoteAuthor, v.Quote, v.Action, v.Link = "Fan", "請試試 <b>重新連線</b>", "查看及回覆", "https://support.example.test/tickets/1"
	html, err := EmailHTML(context.Background(), v)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`src="https://support.example.test/branding/logo.png?v=` + logo.Version + `"`, `width="240"`, `height="56"`, `alt="Example Cloud 支援中心"`,
		"background:" + logo.Accent, "&lt;script&gt;", "&lt;b&gt;", "© 2026 Example Cloud Ltd.", `href="https://example.com/zh"`, "Example Cloud 官方網站",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("email missing %s", want)
		}
	}
	if strings.Contains(html, "<script>") || strings.Contains(html, "<b>") {
		t.Fatal("ticket text rendered as markup")
	}
	text := EmailText(v)
	if !strings.Contains(text, "主旨：<script>alert(1)</script>") || !strings.Contains(text, "> 請試試 <b>重新連線</b>") || !strings.Contains(text, "Example Cloud 官方網站：https://example.com/zh") {
		t.Fatal("text part", text)
	}
	// Without a logo the name stands in and the colors stay neutral.
	plain, _ := EmailHTML(context.Background(), NewEmailView(Site{}, "https://support.example.test", time.Now()))
	if strings.Contains(plain, "<img") || !strings.Contains(plain, "background:#292929") || strings.Contains(plain, "©") {
		t.Fatal("email without a logo or owner")
	}
}

func contrastWithWhite(r, g, b int) float64 {
	linear := func(c int) float64 {
		v := float64(c) / 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 1.05 / (0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b) + 0.05)
}
