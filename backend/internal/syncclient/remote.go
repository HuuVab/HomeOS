package syncclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type RemoteEntry struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Size    int64  `json:"size"`
	Version int64  `json:"version"`
}

type Change struct {
	ID        int64  `json:"id"`
	Path      string `json:"path"`
	Operation string `json:"operation"`
	Version   *int64 `json:"version"`
}

type Remote struct {
	base  *url.URL
	http  *http.Client
	csrf  string
	owner int64
}

func NewRemote(rawURL string) (*Remote, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return nil, errors.New("expected a server origin URL without credentials or a path")
	}
	if parsed.Scheme != "https" {
		host := parsed.Hostname()
		if parsed.Scheme != "http" || (host != "localhost" && net.ParseIP(host) == nil) || (host != "localhost" && !net.ParseIP(host).IsLoopback()) {
			return nil, errors.New("HTTP sync is permitted only for a loopback server")
		}
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &Remote{base: parsed, http: &http.Client{Jar: jar, Timeout: 10 * time.Minute}}, nil
}

func (r *Remote) Origin() string { return r.base.String() }
func (r *Remote) OwnerID() int64 { return r.owner }

func (r *Remote) endpoint(resource string, parameters url.Values) string {
	u := *r.base
	u.Path = resource
	u.RawQuery = parameters.Encode()
	return u.String()
}

func (r *Remote) request(ctx context.Context, method, resource string, parameters url.Values, body io.Reader, contentType string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, r.endpoint(resource, parameters), body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if method != http.MethodGet && r.csrf != "" {
		request.Header.Set("X-CSRF-Token", r.csrf)
	}
	return r.http.Do(request)
}

func readJSON(response *http.Response, target any) error {
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("HomeOS returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(target)
}

func (r *Remote) Login(ctx context.Context, username, password string) error {
	data, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		return err
	}
	response, err := r.request(ctx, http.MethodPost, "/api/v1/auth/login", nil, bytes.NewReader(data), "application/json")
	if err != nil {
		return err
	}
	var session struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
		CSRF string `json:"csrfToken"`
	}
	if err := readJSON(response, &session); err != nil {
		return err
	}
	if session.User.ID < 1 || session.CSRF == "" {
		return errors.New("HomeOS login returned an incomplete session")
	}
	r.owner, r.csrf = session.User.ID, session.CSRF
	return nil
}

func validPath(name string) bool {
	if name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || path.Clean(name) != name {
		return false
	}
	for _, component := range strings.Split(name, "/") {
		if component == "." || component == ".." || component == stateDir || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") || strings.ContainsAny(component, ":\x00") {
			return false
		}
	}
	return filepath.IsLocal(filepath.FromSlash(name))
}

func (r *Remote) ListAll(ctx context.Context) (map[string]RemoteEntry, error) {
	if r.owner < 1 {
		return nil, errors.New("login required")
	}
	result := make(map[string]RemoteEntry)
	caseFolded := make(map[string]string)
	folders := []string{""}
	for len(folders) > 0 {
		folder := folders[0]
		folders = folders[1:]
		parameters := url.Values{}
		if folder != "" {
			parameters.Set("path", folder)
		}
		response, err := r.request(ctx, http.MethodGet, "/api/v1/drive", parameters, nil, "")
		if err != nil {
			return nil, err
		}
		var page struct {
			Entries []RemoteEntry `json:"entries"`
		}
		if err := readJSON(response, &page); err != nil {
			return nil, err
		}
		for _, entry := range page.Entries {
			if !validPath(entry.Path) || path.Dir(entry.Path) != folder && !(folder == "" && !strings.Contains(entry.Path, "/")) || (entry.Kind != "file" && entry.Kind != "folder") || entry.Version < 1 {
				return nil, fmt.Errorf("server returned an unsafe file entry: %q", entry.Path)
			}
			if _, exists := result[entry.Path]; exists {
				return nil, fmt.Errorf("server repeated path %q", entry.Path)
			}
			if runtime.GOOS == "windows" {
				folded := strings.ToLower(entry.Path)
				if other := caseFolded[folded]; other != "" && other != entry.Path {
					return nil, fmt.Errorf("server paths %q and %q collide on Windows", other, entry.Path)
				}
				caseFolded[folded] = entry.Path
			}
			result[entry.Path] = entry
			if len(result) > 10000 {
				return nil, errors.New("sync prototype is limited to 10,000 entries")
			}
			if entry.Kind == "folder" {
				folders = append(folders, entry.Path)
			}
		}
	}
	return result, nil
}

func (r *Remote) Changes(ctx context.Context, after int64) ([]Change, int64, error) {
	parameters := url.Values{"after": {strconv.FormatInt(after, 10)}}
	response, err := r.request(ctx, http.MethodGet, "/api/v1/drive/changes", parameters, nil, "")
	if err != nil {
		return nil, after, err
	}
	var page struct {
		Events     []Change `json:"events"`
		NextCursor int64    `json:"nextCursor"`
	}
	if err := readJSON(response, &page); err != nil {
		return nil, after, err
	}
	if page.NextCursor < after {
		return nil, after, errors.New("server change cursor moved backwards")
	}
	return page.Events, page.NextCursor, nil
}

func (r *Remote) Download(ctx context.Context, name string) (*http.Response, error) {
	return r.request(ctx, http.MethodGet, "/api/v1/drive/files", url.Values{"path": {name}}, nil, "")
}

func (r *Remote) CreateFolder(ctx context.Context, name string) error {
	data, _ := json.Marshal(map[string]string{"path": name})
	response, err := r.request(ctx, http.MethodPost, "/api/v1/drive/folders", nil, bytes.NewReader(data), "application/json")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return fmt.Errorf("create folder %q: HTTP %d", name, response.StatusCode)
	}
	return nil
}

func (r *Remote) UploadNew(ctx context.Context, name string, body io.Reader) error {
	response, err := r.request(ctx, http.MethodPut, "/api/v1/drive/files", url.Values{"path": {name}}, body, "application/octet-stream")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return fmt.Errorf("upload %q: HTTP %d", name, response.StatusCode)
	}
	return nil
}
