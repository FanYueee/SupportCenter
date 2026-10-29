package app

import (
	"encoding/json"
	"net/http"
	"regexp"
	"supportcenter/internal/model"
)

var submissionKey = regexp.MustCompile(`^[A-Za-z0-9_-]{20,100}$`)

func operation(r *http.Request, payload any, files []model.Attachment) model.Operation {
	type file struct {
		Name, Checksum string
		Size           int64
	}
	list := make([]file, 0, len(files))
	for _, a := range files {
		list = append(list, file{a.Name, a.Checksum, a.Size})
	}
	data, _ := json.Marshal(struct {
		Path    string
		Payload any
		Files   []file
	}{r.URL.Path, payload, list})
	return model.Operation{Key: r.PostFormValue("submission"), Hash: hash(string(data))}
}
