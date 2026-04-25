package netbox

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config configures a Client.
type Config struct {
	URL           string
	Token         string
	AuthScheme    string // "token" (default) or "bearer"
	Timeout       time.Duration
	CacheTTL      time.Duration
	TLSSkipVerify bool
	Logger        *slog.Logger
}

// Client talks to a Netbox instance with an in-memory TTL cache.
// Construct via NewClient. Methods return (nil, nil) when Netbox has no
// matching object so the caller can record found=false without an error.
type Client struct {
	apiBase    *url.URL
	uiBase     string
	httpClient *http.Client
	authHeader string
	cache      *cache
	logger     *slog.Logger
}

// NewClient validates the config and constructs a Client. Returns an error
// for missing URL or unknown auth scheme — the caller should log and disable
// the feature rather than fail startup.
func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, errors.New("netbox: url is required")
	}
	base, err := url.Parse(strings.TrimRight(cfg.URL, "/"))
	if err != nil {
		return nil, fmt.Errorf("netbox: parse url: %w", err)
	}

	var authHeader string
	switch strings.ToLower(strings.TrimSpace(cfg.AuthScheme)) {
	case "", "token":
		authHeader = "Token " + cfg.Token
	case "bearer":
		authHeader = "Bearer " + cfg.Token
	default:
		return nil, fmt.Errorf("netbox: invalid auth_scheme %q (must be \"token\" or \"bearer\")", cfg.AuthScheme)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	cacheTTL := cfg.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = 10 * time.Minute
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("netbox: http.DefaultTransport is not *http.Transport")
	}
	tr := transport.Clone()
	if cfg.TLSSkipVerify {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit opt-in via config for self-signed test instances
	}

	return &Client{
		apiBase:    base,
		uiBase:     base.String(),
		httpClient: &http.Client{Timeout: timeout, Transport: tr},
		authHeader: authHeader,
		cache:      newCache(cacheTTL),
		logger:     logger,
	}, nil
}

// Close releases background resources.
func (c *Client) Close() {
	c.cache.stop()
}

// uiURL builds a Netbox UI URL for an object given its UI path (e.g.
// "/dcim/devices/42/"). Falls back to empty string if id <= 0.
func (c *Client) uiURL(uiPath string, id int) string {
	if id <= 0 {
		return ""
	}
	return c.uiBase + uiPath + fmt.Sprintf("%d/", id)
}

// listResponse is the standard Netbox paginated response envelope.
type listResponse[T any] struct {
	Count   int `json:"count"`
	Results []T `json:"results"`
}

// get performs a GET against the Netbox API and decodes into out.
// A 404 response is reported via notFoundErr so callers can convert it to
// (nil, nil). Non-2xx other than 404 returns a wrapped error.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := *c.apiBase
	u.Path = strings.TrimRight(c.apiBase.Path, "/") + path
	if q != nil {
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("netbox: build request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("netbox: %s: %w", path, err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort body close after read.

	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("netbox: %s: status %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("netbox: decode %s: %w", path, err)
	}
	return nil
}

var errNotFound = errors.New("netbox: not found")

// ----- Wire-format types -------------------------------------------------
// These mirror only the Netbox fields we actually surface. Unused fields in
// the response are ignored by the JSON decoder.

type nbNamed struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Display string `json:"display"`
	Slug    string `json:"slug"`
}

type nbDevice struct {
	ID         int     `json:"id"`
	Name       string  `json:"name"`
	Display    string  `json:"display"`
	Status     nbLabel `json:"status"`
	Role       nbNamed `json:"role"`
	Site       nbNamed `json:"site"`
	DeviceType struct {
		ID           int     `json:"id"`
		Display      string  `json:"display"`
		Manufacturer nbNamed `json:"manufacturer"`
		Model        string  `json:"model"`
	} `json:"device_type"`
	PrimaryIP struct {
		Address string `json:"address"`
	} `json:"primary_ip"`
	Description string `json:"description"`
}

type nbLabel struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type nbIP struct {
	ID             int     `json:"id"`
	Address        string  `json:"address"`
	Status         nbLabel `json:"status"`
	Role           nbLabel `json:"role"`
	DNSName        string  `json:"dns_name"`
	VRF            nbNamed `json:"vrf"`
	Description    string  `json:"description"`
	AssignedObject struct {
		Name   string  `json:"name"`
		Device nbNamed `json:"device"`
	} `json:"assigned_object"`
}

type nbPrefix struct {
	ID          int     `json:"id"`
	Prefix      string  `json:"prefix"`
	Status      nbLabel `json:"status"`
	Role        nbNamed `json:"role"`
	Site        nbNamed `json:"site"`
	VLAN        nbNamed `json:"vlan"`
	VRF         nbNamed `json:"vrf"`
	Description string  `json:"description"`
}

type nbASN struct {
	ID          int    `json:"id"`
	ASN         int64  `json:"asn"`
	Description string `json:"description"`
	RIR         struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"rir"`
}

type nbInterface struct {
	ID                 int     `json:"id"`
	Name               string  `json:"name"`
	Device             nbNamed `json:"device"`
	Type               nbLabel `json:"type"`
	MTU                int     `json:"mtu"`
	MACAddress         string  `json:"mac_address"`
	Description        string  `json:"description"`
	Enabled            bool    `json:"enabled"`
	LAG                nbNamed `json:"lag"`
	ConnectedEndpoints []struct {
		Display string  `json:"display"`
		Device  nbNamed `json:"device"`
	} `json:"connected_endpoints"`
}

// ----- Lookup methods ----------------------------------------------------

// LookupDevice fetches a device by exact name. Returns (nil, nil) when no
// device matches.
func (c *Client) LookupDevice(ctx context.Context, name string) (*DeviceResult, error) {
	if name == "" {
		return nil, nil
	}
	key := EntityDevice + ":" + name
	if v, ok := c.cache.get(key); ok {
		if v == nil {
			return nil, nil
		}
		d, _ := v.(*DeviceResult)
		return d, nil
	}

	var resp listResponse[nbDevice]
	err := c.get(ctx, "/api/dcim/devices/", url.Values{"name": []string{name}}, &resp)
	if errors.Is(err, errNotFound) || (err == nil && len(resp.Results) == 0) {
		c.cache.set(key, nil)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d := c.deviceFromWire(resp.Results[0])
	c.cache.set(key, d)
	return d, nil
}

// LookupIP fetches IP details. Address may include or omit the prefix length.
func (c *Client) LookupIP(ctx context.Context, address string) (*IPResult, error) {
	if address == "" {
		return nil, nil
	}
	key := EntityIP + ":" + address
	if v, ok := c.cache.get(key); ok {
		if v == nil {
			return nil, nil
		}
		ip, _ := v.(*IPResult)
		return ip, nil
	}

	var resp listResponse[nbIP]
	err := c.get(ctx, "/api/ipam/ip-addresses/", url.Values{"address": []string{address}}, &resp)
	if errors.Is(err, errNotFound) || (err == nil && len(resp.Results) == 0) {
		c.cache.set(key, nil)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ip := c.ipFromWire(resp.Results[0])
	c.cache.set(key, ip)
	return ip, nil
}

// LookupPrefix fetches an IPAM prefix by exact match.
func (c *Client) LookupPrefix(ctx context.Context, prefix string) (*PrefixResult, error) {
	if prefix == "" {
		return nil, nil
	}
	key := EntityPrefix + ":" + prefix
	if v, ok := c.cache.get(key); ok {
		if v == nil {
			return nil, nil
		}
		p, _ := v.(*PrefixResult)
		return p, nil
	}

	var resp listResponse[nbPrefix]
	err := c.get(ctx, "/api/ipam/prefixes/", url.Values{"prefix": []string{prefix}}, &resp)
	if errors.Is(err, errNotFound) || (err == nil && len(resp.Results) == 0) {
		c.cache.set(key, nil)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p := c.prefixFromWire(resp.Results[0])
	c.cache.set(key, p)
	return p, nil
}

// LookupASN fetches an ASN by number.
func (c *Client) LookupASN(ctx context.Context, asn string) (*ASNResult, error) {
	if asn == "" {
		return nil, nil
	}
	key := EntityASN + ":" + asn
	if v, ok := c.cache.get(key); ok {
		if v == nil {
			return nil, nil
		}
		a, _ := v.(*ASNResult)
		return a, nil
	}

	var resp listResponse[nbASN]
	err := c.get(ctx, "/api/ipam/asns/", url.Values{"asn": []string{asn}}, &resp)
	if errors.Is(err, errNotFound) || (err == nil && len(resp.Results) == 0) {
		c.cache.set(key, nil)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a := c.asnFromWire(resp.Results[0])
	c.cache.set(key, a)
	return a, nil
}

// LookupInterface fetches an interface by device + name. The device name is
// required because interface names aren't unique across devices.
func (c *Client) LookupInterface(ctx context.Context, device, name string) (*InterfaceResult, error) {
	if device == "" || name == "" {
		return nil, nil
	}
	key := EntityInterface + ":" + device + ":" + name
	if v, ok := c.cache.get(key); ok {
		if v == nil {
			return nil, nil
		}
		iface, _ := v.(*InterfaceResult)
		return iface, nil
	}

	var resp listResponse[nbInterface]
	err := c.get(ctx, "/api/dcim/interfaces/", url.Values{
		"device": []string{device},
		"name":   []string{name},
	}, &resp)
	if errors.Is(err, errNotFound) || (err == nil && len(resp.Results) == 0) {
		c.cache.set(key, nil)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	iface := c.interfaceFromWire(resp.Results[0])
	c.cache.set(key, iface)
	return iface, nil
}

// ----- Wire → result mapping ---------------------------------------------

func (c *Client) deviceFromWire(w nbDevice) *DeviceResult {
	return &DeviceResult{
		ID:           w.ID,
		Name:         firstNonEmpty(w.Name, w.Display),
		Status:       firstNonEmpty(w.Status.Label, w.Status.Value),
		Role:         firstNonEmpty(w.Role.Display, w.Role.Name),
		Site:         firstNonEmpty(w.Site.Display, w.Site.Name),
		DeviceType:   firstNonEmpty(w.DeviceType.Display, w.DeviceType.Model),
		Manufacturer: firstNonEmpty(w.DeviceType.Manufacturer.Display, w.DeviceType.Manufacturer.Name),
		PrimaryIP:    w.PrimaryIP.Address,
		Description:  w.Description,
		URL:          c.uiURL("/dcim/devices/", w.ID),
	}
}

func (c *Client) ipFromWire(w nbIP) *IPResult {
	r := &IPResult{
		ID:          w.ID,
		Address:     w.Address,
		Status:      firstNonEmpty(w.Status.Label, w.Status.Value),
		Role:        firstNonEmpty(w.Role.Label, w.Role.Value),
		DNSName:     w.DNSName,
		VRF:         firstNonEmpty(w.VRF.Display, w.VRF.Name),
		Description: w.Description,
		Interface:   w.AssignedObject.Name,
		Device:      firstNonEmpty(w.AssignedObject.Device.Display, w.AssignedObject.Device.Name),
		URL:         c.uiURL("/ipam/ip-addresses/", w.ID),
	}
	return r
}

func (c *Client) prefixFromWire(w nbPrefix) *PrefixResult {
	return &PrefixResult{
		ID:          w.ID,
		Prefix:      w.Prefix,
		Status:      firstNonEmpty(w.Status.Label, w.Status.Value),
		Role:        firstNonEmpty(w.Role.Display, w.Role.Name),
		Site:        firstNonEmpty(w.Site.Display, w.Site.Name),
		VLAN:        firstNonEmpty(w.VLAN.Display, w.VLAN.Name),
		VRF:         firstNonEmpty(w.VRF.Display, w.VRF.Name),
		Description: w.Description,
		URL:         c.uiURL("/ipam/prefixes/", w.ID),
	}
}

func (c *Client) asnFromWire(w nbASN) *ASNResult {
	return &ASNResult{
		ID:          w.ID,
		ASN:         w.ASN,
		Description: w.Description,
		RIR:         w.RIR.Name,
		URL:         c.uiURL("/ipam/asns/", w.ID),
	}
}

func (c *Client) interfaceFromWire(w nbInterface) *InterfaceResult {
	enabled := w.Enabled
	r := &InterfaceResult{
		ID:          w.ID,
		Name:        w.Name,
		Device:      firstNonEmpty(w.Device.Display, w.Device.Name),
		Type:        firstNonEmpty(w.Type.Label, w.Type.Value),
		MTU:         w.MTU,
		MACAddress:  w.MACAddress,
		Description: w.Description,
		Enabled:     &enabled,
		LAG:         firstNonEmpty(w.LAG.Display, w.LAG.Name),
		URL:         c.uiURL("/dcim/interfaces/", w.ID),
	}
	if len(w.ConnectedEndpoints) > 0 {
		ep := w.ConnectedEndpoints[0]
		device := firstNonEmpty(ep.Device.Display, ep.Device.Name)
		switch {
		case device != "" && ep.Display != "":
			r.ConnectedEndpoint = device + ":" + ep.Display
		case device != "":
			r.ConnectedEndpoint = device
		default:
			r.ConnectedEndpoint = ep.Display
		}
	}
	return r
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
