package eeinit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GalaxyBase is the public Galaxy server; the only one asked (see
// GalaxyServersConfigured).
const GalaxyBase = "https://galaxy.ansible.com"

const (
	galaxyPageSize = 100
	// galaxyMaxPages bounds the paging for one collection (1000 versions).
	galaxyMaxPages = 10
	// galaxyParallel is how many collections are asked at the same time.
	galaxyParallel = 6
	galaxyTimeout  = 10 * time.Second
)

// GalaxyVersion is one version of a collection as Galaxy lists it.
type GalaxyVersion struct {
	Version         string `json:"version"`
	RequiresAnsible string `json:"requires_ansible"` // "" for null
}

// GalaxyInfo is what Galaxy said about one collection, ready for the draft:
// a Version (with Note "from Galaxy: ..."), or none (Note "unpinned: ..." says
// why), and whether the collection is flagged deprecated.
type GalaxyInfo struct {
	Version    string
	Note       string
	Deprecated bool
	// Err is why Galaxy could not be asked (network, status, unreadable
	// answer), for the warning; it is also reflected in Note.
	Err error
}

// Admits is whether a collection's requires_ansible specifier admits the
// ansible-core version core. The shapes Galaxy returns are ">=N.N[.N]" with an
// optional ",<N.N[.N]"; the operators >, <=, == and != are read too. An empty
// specifier (null) and an empty core admit everything, and so does a clause
// that cannot be read: a failure on our side must not hide versions.
func Admits(spec, core string) bool {
	core = numericPrefix(core)
	if strings.TrimSpace(spec) == "" || core == "" {
		return true
	}
	have, ok := parseDotted(core)
	if !ok {
		return true
	}
	for _, clause := range strings.Split(spec, ",") {
		clause = strings.TrimSpace(clause)
		op := ""
		for _, o := range []string{">=", "<=", "==", "!=", ">", "<"} {
			if strings.HasPrefix(clause, o) {
				op = o
				break
			}
		}
		want, ok := parseDotted(strings.TrimSpace(strings.TrimPrefix(clause, op)))
		if op == "" || !ok {
			return true
		}
		c := compareDotted(have, want)
		pass := false
		switch op {
		case ">=":
			pass = c >= 0
		case ">":
			pass = c > 0
		case "<=":
			pass = c <= 0
		case "<":
			pass = c < 0
		case "==":
			pass = c == 0
		case "!=":
			pass = c != 0
		}
		if !pass {
			return false
		}
	}
	return true
}

// numericPrefix is the leading digits and dots of a version ("2.21.0rc1" ->
// "2.21.0").
func numericPrefix(v string) string {
	v = strings.TrimSpace(v)
	i := 0
	for i < len(v) && (v[i] == '.' || (v[i] >= '0' && v[i] <= '9')) {
		i++
	}
	return strings.Trim(v[:i], ".")
}

func parseDotted(v string) ([]int, bool) {
	if v == "" {
		return nil, false
	}
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// compareDotted compares two dotted versions, a missing part being 0.
func compareDotted(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// IsPrerelease is whether a Galaxy version is a pre-release ("6.0.0-a1").
func IsPrerelease(version string) bool { return strings.Contains(version, "-") }

// Newest is the first version in versions (Galaxy lists them newest first)
// that is not a pre-release and whose requires_ansible admits core; "" if none.
func Newest(versions []GalaxyVersion, core string) string {
	for _, v := range versions {
		if !IsPrerelease(v.Version) && Admits(v.RequiresAnsible, core) {
			return v.Version
		}
	}
	return ""
}

// GalaxyClient asks a Galaxy server (Base is its URL without a path).
type GalaxyClient struct {
	HTTP *http.Client
	Base string
}

// NewGalaxyClient is a client for the public Galaxy, with a timeout per
// request; the environment's proxy settings apply.
func NewGalaxyClient() *GalaxyClient {
	return &GalaxyClient{HTTP: &http.Client{Timeout: galaxyTimeout}, Base: GalaxyBase}
}

func (c *GalaxyClient) get(ctx context.Context, path string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "tangsible")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError(resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

type statusError int

func (s statusError) Error() string { return fmt.Sprintf("Galaxy answered HTTP %d", int(s)) }

const collectionPath = "/api/v3/plugin/ansible/content/published/collections/index/%s/%s/"

// errNotFound: the collection has no versions on Galaxy (it answers the
// versions URL of an unknown collection with 200 and an empty list).
var errNotFound = errors.New("not found on Galaxy")

// newestCompatible pages through the versions of coll ("namespace.name"),
// newest first, and returns the first release version that admits core;
// "" with a nil error when there is none.
func (c *GalaxyClient) newestCompatible(ctx context.Context, ns, name, core string) (string, error) {
	next := fmt.Sprintf(collectionPath+"versions/?limit=%d&ordering=-version", ns, name, galaxyPageSize)
	for page := 0; next != "" && page < galaxyMaxPages; page++ {
		var r struct {
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
			Data []GalaxyVersion `json:"data"`
		}
		if err := c.get(ctx, next, &r); err != nil {
			return "", err
		}
		if page == 0 && r.Meta.Count == 0 {
			return "", errNotFound
		}
		if v := Newest(r.Data, core); v != "" {
			return v, nil
		}
		next = r.Links.Next
	}
	return "", nil
}

func (c *GalaxyClient) deprecated(ctx context.Context, ns, name string) (bool, error) {
	var r struct {
		Deprecated bool `json:"deprecated"`
	}
	if err := c.get(ctx, fmt.Sprintf(collectionPath, ns, name), &r); err != nil {
		return false, err
	}
	return r.Deprecated, nil
}

// Lookup asks Galaxy about one collection ("namespace.name") for an image with
// ansible-core core ("" = unknown: no version is excluded).
func (c *GalaxyClient) Lookup(ctx context.Context, coll, core string) GalaxyInfo {
	ns, name, ok := strings.Cut(coll, ".")
	if !ok {
		return GalaxyInfo{Note: "unpinned: not a collection name"}
	}
	var info GalaxyInfo
	version, err := c.newestCompatible(ctx, ns, name, core)
	switch {
	case errors.Is(err, errNotFound):
		info.Note = "unpinned: not found on Galaxy"
		return info
	case err != nil:
		info.Err = fmt.Errorf("%s: %w", coll, err)
		info.Note = "unpinned: " + galaxyFailure(err)
		return info
	case version == "":
		info.Note = "unpinned: no version on Galaxy admits ansible-core " + core
	case core == "":
		info.Version, info.Note = version, "from Galaxy: newest version (the image's ansible-core is unknown)"
	default:
		info.Version, info.Note = version, "from Galaxy: newest compatible with ansible-core "+core
	}
	// A failure of this request loses only the mark.
	if dep, err := c.deprecated(ctx, ns, name); err == nil {
		info.Deprecated = dep
	}
	return info
}

func galaxyFailure(err error) string {
	var st statusError
	if errors.As(err, &st) {
		return st.Error()
	}
	return "Galaxy unreachable"
}

// LookupAll asks about colls, a few at a time.
func (c *GalaxyClient) LookupAll(ctx context.Context, colls []string, core string) map[string]GalaxyInfo {
	out := make(map[string]GalaxyInfo, len(colls))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, galaxyParallel)
	for _, coll := range colls {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			info := c.Lookup(ctx, coll, core)
			mu.Lock()
			out[coll] = info
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// GalaxyServersConfigured is whether the host's Ansible has a non-default
// GALAXY_SERVER_LIST (ansible.cfg, environment): the user's collections may
// then live on a server we cannot ask.
func GalaxyServersConfigured() (bool, error) {
	out, err := exec.Command("ansible-config", "dump").Output()
	if err != nil {
		return false, fmt.Errorf("ansible-config dump: %w", err)
	}
	return parseGalaxyServerList(string(out)), nil
}

// parseGalaxyServerList reads `ansible-config dump` for GALAXY_SERVER_LIST:
// "GALAXY_SERVER_LIST(default) = None" when unset.
func parseGalaxyServerList(dump string) bool {
	for _, line := range strings.Split(dump, "\n") {
		if !strings.HasPrefix(line, "GALAXY_SERVER_LIST(") {
			continue
		}
		_, value, ok := strings.Cut(line, "=")
		if !ok {
			return false
		}
		switch strings.TrimSpace(value) {
		case "", "None", "[]":
			return false
		}
		return true
	}
	return false
}
