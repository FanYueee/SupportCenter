package app

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http/httptest"
	"os/exec"
	"supportcenter/internal/model"
	"testing"
)

func TestImageUploadWebPPreviewAndPrivacy(t *testing.T) {
	if _, err := exec.LookPath("cwebp"); err != nil {
		t.Skip("cwebp is required for image integration test")
	}
	a, h, alice, _, _ := testApp(t)
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 15), G: uint8(y * 15), B: 90, A: 255})
		}
	}
	var original bytes.Buffer
	if err := png.Encode(&original, img); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	f, _ := form.CreateFormFile("attachments", "sample.png")
	f.Write(original.Bytes())
	form.Close()
	req := httptest.NewRequest("POST", "https://support.example.test/tickets", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	defer req.MultipartForm.RemoveAll()
	files, err := a.uploads(req)
	if err != nil || len(files) != 1 {
		t.Fatal("image upload failed", err)
	}
	defer a.removeFiles(files)
	ctx := context.Background()
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "image", Category: "technical", Priority: "low"}, "image", files)
	if err != nil {
		t.Fatal(err)
	}
	var attachmentID int64
	if err := a.Store.Pool.QueryRow(ctx, `SELECT id FROM ticket_attachments WHERE storage_key=$1`, files[0].Key).Scan(&attachmentID); err != nil {
		t.Fatal(err)
	}
	previewPath := fmt.Sprintf("/attachments/%d/preview", attachmentID)
	for _, tc := range []struct {
		session string
		status  int
	}{{"alice", 200}, {"bob", 404}, {"staff", 200}, {"", 303}} {
		response := request(h, "GET", previewPath, tc.session, nil)
		if response.Code != tc.status {
			t.Fatalf("preview session=%s status=%d", tc.session, response.Code)
		}
		if tc.status == 200 {
			data := response.Body.Bytes()
			if response.Header().Get("Content-Type") != "image/webp" || len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
				t.Fatal("preview was not WebP")
			}
			if _, err := fileType(data, "client-upload.webp"); err != nil {
				t.Fatal("WebP upload was rejected", err)
			}
		}
	}
	if err := a.Files.Remove(previewKey(files[0].Key)); err != nil {
		t.Fatal(err)
	}
	if response := request(h, "GET", previewPath, "alice", nil); response.Code != 200 {
		t.Fatal("older image preview was not regenerated", response.Code)
	}
	download := request(h, "GET", fmt.Sprintf("/attachments/%d", attachmentID), "alice", nil)
	if download.Code != 200 || !bytes.Equal(download.Body.Bytes(), original.Bytes()) {
		t.Fatal("original attachment was modified")
	}
	page := request(h, "GET", fmt.Sprintf("/tickets/%d", id), "alice", nil)
	if page.Code != 200 || !bytes.Contains(page.Body.Bytes(), []byte(`data-image-preview`)) || !bytes.Contains(page.Body.Bytes(), []byte(`data-original-url="`+fmt.Sprintf("/attachments/%d", attachmentID)+`"`)) {
		t.Fatal("ticket did not show the image preview")
	}
}
