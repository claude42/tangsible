package eeinit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The community base image's registry and repository; its tags are
// <ansible-core version>-<build> ("2.19.5-1", also "2.17-1" for 2.17.0).
const (
	RegistryBase  = "https://ghcr.io"
	BaseRepo      = "ansible-community/community-ee-base"
	BaseImageName = "ghcr.io/" + BaseRepo

	registryTimeout  = 5 * time.Second
	registryMaxPages = 10
)

// RegistryClient lists tags on a registry that hands out anonymous pull
// tokens (ghcr.io; Base is its URL without a path).
type RegistryClient struct {
	HTTP *http.Client
	Base string
}

// NewRegistryClient is a client for ghcr.io, with a timeout per request; the
// environment's proxy settings apply.
func NewRegistryClient() *RegistryClient {
	return &RegistryClient{HTTP: &http.Client{Timeout: registryTimeout}, Base: RegistryBase}
}

func (c *RegistryClient) get(ctx context.Context, u, token string, into any) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "tangsible")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered HTTP %d", c.Base, resp.StatusCode)
	}
	return resp.Header, json.NewDecoder(resp.Body).Decode(into)
}

// ListTags is every tag of repo ("namespace/name"), in no particular order.
func (c *RegistryClient) ListTags(ctx context.Context, repo string) ([]string, error) {
	host := strings.TrimPrefix(strings.TrimPrefix(c.Base, "https://"), "http://")
	var tok struct {
		Token string `json:"token"`
	}
	q := url.Values{"service": {host}, "scope": {"repository:" + repo + ":pull"}}
	if _, err := c.get(ctx, c.Base+"/token?"+q.Encode(), "", &tok); err != nil {
		return nil, err
	}
	var tags []string
	next := c.Base + "/v2/" + repo + "/tags/list?n=1000"
	for page := 0; next != "" && page < registryMaxPages; page++ {
		var r struct {
			Tags []string `json:"tags"`
		}
		h, err := c.get(ctx, next, tok.Token, &r)
		if err != nil {
			return nil, err
		}
		tags = append(tags, r.Tags...)
		next = nextLink(h.Get("Link"), c.Base)
	}
	return tags, nil
}

// nextLink reads the rel="next" target of a Link header ("" if none).
func nextLink(h, base string) string {
	for _, part := range strings.Split(h, ",") {
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		s, e := strings.Index(part, "<"), strings.Index(part, ">")
		if s < 0 || e < s {
			return ""
		}
		target := part[s+1 : e]
		if strings.HasPrefix(target, "/") {
			return base + target
		}
		return target
	}
	return ""
}

var coreTag = regexp.MustCompile(`^(\d+\.\d+(?:\.\d+)?)-(\d+)$`)

type baseTag struct {
	tag   string
	core  []int
	build int
}

func parseBaseTags(tags []string) []baseTag {
	var out []baseTag
	for _, t := range tags {
		m := coreTag.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		core, ok := parseDotted(m[1])
		build, err := strconv.Atoi(m[2])
		if !ok || err != nil {
			continue
		}
		out = append(out, baseTag{tag: t, core: core, build: build})
	}
	sort.Slice(out, func(i, j int) bool { // newest first
		if c := compareDotted(out[i].core, out[j].core); c != 0 {
			return c > 0
		}
		return out[i].build > out[j].build
	})
	return out
}

// PickBaseTag chooses a tag of the community base image: with latest, the
// newest release; otherwise the newest build of the host's exact ansible-core
// version, else of the newest release in the same major.minor. how says which
// ("" = none: use the built-in default). Tags that aren't release versions
// (latest, devel, digests) are ignored.
func PickBaseTag(tags []string, hostCore string, latest bool) (tag, how string) {
	all := parseBaseTags(tags)
	if len(all) == 0 {
		return "", ""
	}
	if latest {
		return all[0].tag, "newest"
	}
	host, ok := parseDotted(numericPrefix(hostCore))
	if !ok || len(host) < 2 {
		return "", ""
	}
	for _, t := range all {
		if compareDotted(t.core, host) == 0 {
			return t.tag, "exact"
		}
	}
	for _, t := range all {
		if t.core[0] == host[0] && t.core[1] == host[1] {
			return t.tag, "minor"
		}
	}
	return "", ""
}

var hostCoreRe = regexp.MustCompile(`\[core ([0-9][^\] ]*)\]`)

// HostAnsibleCore is the ansible-core version of the host's ansible-playbook
// ("" if it can't be told).
func HostAnsibleCore() string {
	out, err := exec.Command("ansible-playbook", "--version").Output()
	if err != nil {
		return ""
	}
	return parseHostCore(string(out))
}

func parseHostCore(out string) string {
	if m := hostCoreRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

// BaseChoice is the outcome of picking the default base image.
type BaseChoice struct {
	Image string
	// Note says in the draft how the image was chosen ("" = plain default).
	Note []string
	// Warning is for stderr: why the lookup fell back.
	Warning string
}

// ChooseDefaultBase picks the base image when nobody named one: the
// community image matching the host's ansible-core (the newest release with
// latest), or DefaultBaseImage when offline, when the version or the registry
// can't be had, or when no tag fits. It never fails.
func ChooseDefaultBase(ctx context.Context, c *RegistryClient, hostCore string, latest, offline bool) BaseChoice {
	fallback := func(why string) BaseChoice {
		return BaseChoice{
			Image:   DefaultBaseImage,
			Note:    []string{"# " + why + "; using the built-in default."},
			Warning: why,
		}
	}
	switch {
	case offline:
		return BaseChoice{Image: DefaultBaseImage}
	case hostCore == "" && !latest:
		return fallback("the host's ansible-core version could not be determined")
	}
	tags, err := c.ListTags(ctx, BaseRepo)
	if err != nil {
		return fallback(fmt.Sprintf("could not list the tags of %s (%v)", BaseImageName, err))
	}
	tag, how := PickBaseTag(tags, hostCore, latest)
	if tag == "" {
		return fallback(fmt.Sprintf("no %s tag fits ansible-core %s", BaseImageName, hostCore))
	}
	var note string
	switch how {
	case "newest":
		note = "the newest release of " + BaseImageName + " (--latest)"
	case "exact":
		note = fmt.Sprintf("the %s build for this host's ansible-core %s", BaseImageName, hostCore)
	default:
		note = fmt.Sprintf("the newest %s release in this host's ansible-core series (%s)", BaseImageName, hostCore)
	}
	return BaseChoice{Image: BaseImageName + ":" + tag, Note: []string{"# The base image is " + note + "."}}
}
