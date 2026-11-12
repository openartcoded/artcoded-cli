package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// API talks to the artcoded backend with the keycloak token.
type API struct {
	cfg     *Config
	auth    *Auth
	http    *http.Client
	token   *Tokens
	verbose bool
}

type APIError struct {
	Status  int
	Message string
	Path    string
}

func (e *APIError) Error() string {
	switch e.Status {
	case http.StatusForbidden:
		return fmt.Sprintf("forbidden (%s): your role cannot do this", e.Path)
	case http.StatusNotFound:
		if e.Message == "" {
			return fmt.Sprintf("not found (%s)", e.Path)
		}
	}
	if e.Message == "" {
		return fmt.Sprintf("%s: HTTP %d", e.Path, e.Status)
	}
	return e.Message
}

func newAPI(cfg *Config, verbose bool) *API {
	return &API{cfg: cfg, auth: &Auth{cfg: cfg, http: httpClient(cfg, 30*time.Second)}, http: httpClient(cfg, 5*time.Minute), verbose: verbose}
}

// Q builds query parameters: Q("id", id, "page", 1). Empty / nil values are skipped, slices repeat the key.
func Q(kv ...any) url.Values {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		k := fmt.Sprint(kv[i])
		switch val := kv[i+1].(type) {
		case nil:
		case string:
			if val != "" {
				v.Set(k, val)
			}
		case []string:
			for _, s := range val {
				v.Add(k, s)
			}
		case *bool:
			if val != nil {
				v.Set(k, fmt.Sprint(*val))
			}
		default:
			v.Set(k, fmt.Sprint(val))
		}
	}
	return v
}

type request struct {
	method      string
	path        string
	query       url.Values
	body        []byte
	contentType string
	accept      string
	absolute    bool // path is a full url (sparql endpoint)
}

func (a *API) ensureToken(force bool) error {
	if !force && a.token.Valid() {
		return nil
	}
	t, err := a.auth.Token(force)
	if err != nil {
		return err
	}
	a.token = t
	return nil
}

func (a *API) send(r request) (*http.Response, error) {
	if err := a.ensureToken(false); err != nil {
		return nil, err
	}
	u := r.path
	if !r.absolute {
		u = strings.TrimRight(a.cfg.BackendURL, "/") + r.path
	}
	if len(r.query) > 0 {
		u += "?" + r.query.Encode()
	}
	for attempt := 0; ; attempt++ {
		var body io.Reader
		if r.body != nil {
			body = bytes.NewReader(r.body)
		}
		req, err := http.NewRequest(r.method, u, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+a.token.AccessToken)
		req.Header.Set("User-Agent", "artcoded-cli/"+version)
		if r.contentType != "" {
			req.Header.Set("Content-Type", r.contentType)
		}
		if r.accept != "" {
			req.Header.Set("Accept", r.accept)
		}
		if a.verbose {
			fmt.Fprintf(os.Stderr, "%s %s\n", r.method, u)
		}
		res, err := a.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("cannot reach the backend (%s): %w", a.cfg.BackendURL, err)
		}
		if res.StatusCode == http.StatusUnauthorized && attempt == 0 {
			res.Body.Close()
			if err := a.ensureToken(true); err != nil {
				return nil, err
			}
			continue
		}
		if res.StatusCode >= 400 {
			defer res.Body.Close()
			data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
			return nil, &APIError{Status: res.StatusCode, Message: errorMessage(data), Path: r.path}
		}
		return res, nil
	}
}

// errorMessage: same logic as the backoffice (plain text, {message}, {error})
func errorMessage(data []byte) string {
	s := strings.TrimSpace(string(data))
	if s == "" {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(data, &m) == nil {
		for _, k := range []string{"message", "error", "detail", "title"} {
			if v, ok := m[k].(string); ok && v != "" {
				return v
			}
		}
	}
	if strings.HasPrefix(s, "<") {
		return ""
	}
	return truncate(s, 500)
}

func (a *API) do(method, path string, query url.Values, in, out any) error {
	r := request{method: method, path: path, query: query}
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		r.body, r.contentType = data, "application/json"
	} else if method == http.MethodPost {
		// the backoffice always posts a body ({} by default)
		r.body, r.contentType = []byte("{}"), "application/json"
	}
	res, err := a.send(r)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		if sp, ok := out.(*any); ok {
			*sp = string(data)
			return nil
		}
		return fmt.Errorf("%s: unexpected answer: %v", path, err)
	}
	return nil
}

func (a *API) Get(path string, q url.Values, out any) error {
	return a.do(http.MethodGet, path, q, nil, out)
}
func (a *API) Post(path string, q url.Values, in, out any) error {
	return a.do(http.MethodPost, path, q, in, out)
}
func (a *API) Delete(path string, q url.Values, out any) error {
	return a.do(http.MethodDelete, path, q, nil, out)
}

// Part of a multipart request: a value, or a file (Path) under Name.
type Part struct {
	Name     string
	Value    string
	Path     string
	Content  []byte // in-memory file, named FileName
	FileName string
}

func F(name, value string) Part               { return Part{Name: name, Value: value} }
func FFile(name, path string) Part            { return Part{Name: name, Path: path} }
func FBytes(name, file string, b []byte) Part { return Part{Name: name, Content: b, FileName: file} }

func buildMultipart(parts []Part) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, p := range parts {
		switch {
		case p.Path != "":
			data, err := os.ReadFile(p.Path)
			if err != nil {
				return nil, "", err
			}
			if err := writeFilePart(w, p.Name, filepath.Base(p.Path), data); err != nil {
				return nil, "", err
			}
		case p.FileName != "":
			if err := writeFilePart(w, p.Name, p.FileName, p.Content); err != nil {
				return nil, "", err
			}
		default:
			if err := w.WriteField(p.Name, p.Value); err != nil {
				return nil, "", err
			}
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// Multipart posts a form; out: json target, or *string for a text answer.
func (a *API) Multipart(path string, q url.Values, parts []Part, out any) error {
	body, ct, err := buildMultipart(parts)
	if err != nil {
		return err
	}
	res, err := a.send(request{method: http.MethodPost, path: path, query: q, body: body, contentType: ct})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if sp, ok := out.(*string); ok {
		*sp = string(data)
		return nil
	}
	if out != nil && len(bytes.TrimSpace(data)) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// MultipartFetch posts a form answering a file.
func (a *API) MultipartFetch(path string, parts []Part, fallbackName string) (*Download, error) {
	body, ct, err := buildMultipart(parts)
	if err != nil {
		return nil, err
	}
	res, err := a.send(request{method: http.MethodPost, path: path, body: body, contentType: ct})
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return &Download{Name: filenameFromDisposition(res.Header.Get("Content-Disposition"), fallbackName), ContentType: res.Header.Get("Content-Type"), Data: data}, nil
}

func writeFilePart(w *multipart.Writer, field, filename string, data []byte) error {
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeQuotes(field), escapeQuotes(filename)))
	ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
	if ct == "" {
		ct = http.DetectContentType(data)
	}
	h.Set("Content-Type", ct)
	pw, err := w.CreatePart(h)
	if err != nil {
		return err
	}
	_, err = pw.Write(data)
	return err
}

func escapeQuotes(s string) string { return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) }

// Downloaded file
type Download struct {
	Name        string
	ContentType string
	Data        []byte
}

// Fetch a binary resource. in: json body for a POST (nil for a GET).
func (a *API) Fetch(method, path string, q url.Values, in any, fallbackName string) (*Download, error) {
	r := request{method: method, path: path, query: q}
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		r.body, r.contentType = data, "application/json"
	}
	res, err := a.send(r)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	name := filenameFromDisposition(res.Header.Get("Content-Disposition"), fallbackName)
	return &Download{Name: name, ContentType: res.Header.Get("Content-Type"), Data: data}, nil
}

func (a *API) FetchText(method, path string, q url.Values, body []byte, contentType string) (string, error) {
	res, err := a.send(request{method: method, path: path, query: q, body: body, contentType: contentType})
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	return string(data), err
}

func filenameFromDisposition(header, fallback string) string {
	if header == "" {
		return fallback
	}
	_, params, err := mime.ParseMediaType(header)
	if err == nil {
		if fn := params["filename"]; fn != "" {
			return filepath.Base(fn)
		}
	}
	if i := strings.Index(strings.ToLower(header), "filename="); i >= 0 {
		fn := strings.Trim(strings.SplitN(header[i+9:], ";", 2)[0], `" `)
		if fn != "" {
			return filepath.Base(fn)
		}
	}
	return fallback
}

// FileDownload: an upload of the file service (/api/resource/download)
func (a *API) FileDownload(id string) (*Download, error) {
	if id == "" {
		return nil, errors.New("no file")
	}
	var info map[string]any
	fallback := id
	if a.Get("/api/resource/find-by-id", Q("id", id), &info) == nil {
		if n := str(info, "originalFilename"); n != "" {
			fallback = n
		}
	}
	d, err := a.Fetch(http.MethodGet, "/api/resource/download", Q("id", id), nil, fallback)
	if err != nil {
		return nil, err
	}
	if d.Name == id && fallback != id {
		d.Name = fallback
	}
	return d, nil
}

// Upload a file to the file service, returns its upload.
func (a *API) Upload(path string, correlationID string, public bool) (map[string]any, error) {
	parts := []Part{FFile("file", path), F("publicResource", fmt.Sprint(public))}
	if correlationID != "" {
		parts = append(parts, F("correlationId", correlationID))
	}
	var out map[string]any
	err := a.Multipart("/api/resource/upload", nil, parts, &out)
	return out, err
}
