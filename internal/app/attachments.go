package app

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/webp"
	"supportcenter/internal/model"
)

const maxFile = 10 << 20

var storageKey = regexp.MustCompile(`^[a-f0-9]{64}$`)

func previewKey(key string) string { return key + ".webp" }

func (a *App) savePreview(key string, data []byte) error {
	temporary := hash(token()) + ".tmp"
	out, err := a.Files.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer a.Files.Remove(temporary)
	_, err = out.Write(data)
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return a.Files.Rename(temporary, key)
}

// Preview encoding never replaces the original attachment. Resizing and lossy
// WebP are confined to the small, private preview displayed in the ticket.
func webpPreview(ctx context.Context, data []byte, name string, width, height int) ([]byte, error) {
	dir, err := os.MkdirTemp("", "support-preview-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	input := filepath.Join(dir, "input"+strings.ToLower(filepath.Ext(name)))
	output := filepath.Join(dir, "preview.webp")
	if err = os.WriteFile(input, data, 0600); err != nil {
		return nil, err
	}
	args := []string{"-quiet", "-q", "82", "-m", "6"}
	if width > 1600 || height > 1600 {
		if width >= height {
			args = append(args, "-resize", "1600", "0")
		} else {
			args = append(args, "-resize", "0", "1600")
		}
	}
	args = append(args, input, "-o", output)
	deadline, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err = exec.CommandContext(deadline, "cwebp", args...).Run(); err != nil {
		return nil, errors.New("無法產生圖片預覽")
	}
	result, err := os.ReadFile(output)
	if err != nil || len(result) < 12 || len(result) > 4<<20 || string(result[:4]) != "RIFF" || string(result[8:12]) != "WEBP" {
		return nil, errors.New("圖片預覽格式不正確")
	}
	return result, nil
}

func fileType(data []byte, name string) (string, error) {
	ext := strings.ToLower(filepath.Ext(name))
	detected := http.DetectContentType(data)
	switch ext {
	case ".jpg", ".jpeg", ".png":
		expected := "image/jpeg"
		if ext == ".png" {
			expected = "image/png"
		}
		if detected != expected {
			break
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 100000000 {
			break
		}
		return expected, nil
	case ".webp":
		if detected == "image/webp" {
			cfg, e := webp.DecodeConfig(bytes.NewReader(data))
			if e == nil && cfg.Width > 0 && cfg.Height > 0 && int64(cfg.Width)*int64(cfg.Height) <= 100000000 {
				return "image/webp", nil
			}
		}
	case ".pdf":
		if bytes.HasPrefix(data, []byte("%PDF-")) && bytes.Contains(data[max(0, len(data)-1024):], []byte("%%EOF")) {
			return "application/pdf", nil
		}
	case ".zip":
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err == nil && len(z.File) <= 1000 {
			var size uint64
			for _, f := range z.File {
				if f.UncompressedSize64 > 100<<20 {
					return "", errors.New("ZIP 解壓後過大")
				}
				size += f.UncompressedSize64
			}
			if size <= 100<<20 {
				return "application/zip", nil
			}
		}
	case ".txt", ".log":
		if utf8.Valid(data) && !bytes.ContainsRune(data, 0) {
			for _, r := range string(data) {
				if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
					return "", errors.New("文字檔包含不支援的控制字元")
				}
			}
			return "text/plain; charset=utf-8", nil
		}
	}
	return "", errors.New("檔案格式或內容不符合允許的附件類型")
}
func safeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return -1
		}
		return r
	}, name)
	r := []rune(name)
	if len(r) > 150 {
		name = string(r[:150])
	}
	return name
}
func (a *App) uploads(r *http.Request) (files []model.Attachment, err error) {
	if r.MultipartForm == nil {
		return nil, nil
	}
	headers := r.MultipartForm.File["attachments"]
	total := 0
	for _, v := range r.MultipartForm.File {
		total += len(v)
	}
	if total > 3 || total != len(headers) {
		return nil, errors.New("最多可上傳 3 個附件")
	}
	defer func() {
		if err != nil {
			a.removeFiles(files)
		}
	}()
	for _, h := range headers {
		if h.Size <= 0 || h.Size > maxFile {
			return files, errors.New("每個附件須介於 1 byte 與 10 MB")
		}
		f, e := h.Open()
		if e != nil {
			return files, errors.New("無法讀取附件，請重新選擇檔案")
		}
		data, e := io.ReadAll(io.LimitReader(f, maxFile+1))
		f.Close()
		if e != nil {
			return files, errors.New("無法讀取附件，請重新選擇檔案")
		}
		if len(data) > maxFile {
			return files, errors.New("單一附件不可超過 10 MB")
		}
		name := safeName(h.Filename)
		mt, e := fileType(data, name)
		if e != nil {
			return files, e
		}
		key := hash(token())
		out, e := a.Files.OpenFile(key, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return files, fmt.Errorf("無法儲存附件，請稍後再試")
		}
		_, e = out.Write(data)
		if e == nil {
			e = out.Sync()
		}
		closeErr := out.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			a.Files.Remove(key)
			return files, errors.New("附件儲存失敗，請稍後再試")
		}
		files = append(files, model.Attachment{Name: name, Key: key, MIME: mt, Checksum: hash(string(data)), Size: int64(len(data))})
		if strings.HasPrefix(mt, "image/") {
			cfg, _, e := image.DecodeConfig(bytes.NewReader(data))
			if e != nil {
				return files, errors.New("無法讀取圖片")
			}
			preview, e := webpPreview(r.Context(), data, name, cfg.Width, cfg.Height)
			if e != nil {
				return files, e
			}
			if e = a.savePreview(previewKey(key), preview); e != nil {
				return files, errors.New("無法儲存圖片預覽")
			}
		}
	}
	return files, nil
}
func (a *App) removeFiles(files []model.Attachment) {
	for _, f := range files {
		if storageKey.MatchString(f.Key) {
			if strings.HasPrefix(f.MIME, "image/") {
				if err := a.Files.Remove(previewKey(f.Key)); err != nil && !errors.Is(err, os.ErrNotExist) {
					a.Log.Error("preview cleanup failed")
				}
			}
			if err := a.Files.Remove(f.Key); err != nil && !errors.Is(err, os.ErrNotExist) {
				a.Log.Error("attachment cleanup failed")
			}
		}
	}
}
func (a *App) preview(w http.ResponseWriter, r *http.Request) {
	id, ok := idFrom(r)
	if !ok {
		a.problem(w, r, 404, "找不到圖片。")
		return
	}
	at, err := a.Store.Attachment(r.Context(), current(r).User, id)
	if err != nil || !strings.HasPrefix(at.MIME, "image/") || !storageKey.MatchString(at.Key) {
		a.problem(w, r, 404, "找不到圖片。")
		return
	}
	key := previewKey(at.Key)
	info, err := a.Files.Lstat(key)
	if errors.Is(err, os.ErrNotExist) {
		// Existing images uploaded before previews were introduced are converted once.
		original, openErr := a.Files.Open(at.Key)
		if openErr == nil {
			data, readErr := io.ReadAll(io.LimitReader(original, maxFile+1))
			original.Close()
			if readErr == nil && len(data) <= maxFile {
				cfg, _, decodeErr := image.DecodeConfig(bytes.NewReader(data))
				if decodeErr == nil {
					converted, convertErr := webpPreview(r.Context(), data, at.Name, cfg.Width, cfg.Height)
					if convertErr == nil {
						a.savePreview(key, converted)
					}
				}
			}
		}
		info, err = a.Files.Lstat(key)
	}
	if err != nil || !info.Mode().IsRegular() {
		a.problem(w, r, 404, "找不到圖片預覽。")
		return
	}
	f, err := a.Files.Open(key)
	if err != nil {
		a.problem(w, r, 404, "找不到圖片預覽。")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "preview.webp", info.ModTime(), f)
}
func (a *App) download(w http.ResponseWriter, r *http.Request) {
	id, ok := idFrom(r)
	if !ok {
		a.problem(w, r, 404, "找不到附件。")
		return
	}
	at, err := a.Store.Attachment(r.Context(), current(r).User, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if !storageKey.MatchString(at.Key) {
		a.problem(w, r, 404, "找不到附件。")
		return
	}
	// Root confines access; Lstat rejects even in-root symlinks. Only this process writes here.
	info, err := a.Files.Lstat(at.Key)
	if err != nil || !info.Mode().IsRegular() {
		a.problem(w, r, 404, "找不到附件。")
		return
	}
	f, err := a.Files.Open(at.Key)
	if err != nil {
		a.problem(w, r, 404, "找不到附件。")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", at.MIME)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": at.Name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, at.Name, info.ModTime(), f)
}
