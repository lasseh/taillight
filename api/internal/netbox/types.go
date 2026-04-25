// Package netbox provides a small client for enriching log entities (IPs,
// prefixes, ASNs, interfaces, devices) with information from a Netbox
// instance, used by the netlog detail page.
package netbox

// Entity types.
const (
	EntityDevice    = "device"
	EntityIP        = "ip"
	EntityPrefix    = "prefix"
	EntityASN       = "asn"
	EntityInterface = "interface"
)

// Entity is a network object extracted from a log message that we may want to
// enrich. Context carries side data such as the source device for an
// interface lookup (interface names aren't unique across devices).
type Entity struct {
	Type    string            `json:"type"`
	Value   string            `json:"value"`
	Context map[string]string `json:"context,omitempty"`
}

// Lookup carries the result of attempting to enrich a single entity.
// Per-entity errors are surfaced via Error so the request as a whole can
// still succeed.
type Lookup struct {
	Entity Entity      `json:"entity"`
	Found  bool        `json:"found"`
	Data   *LookupData `json:"data,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// LookupData is a tagged union — exactly one of the per-type fields is set,
// matching Entity.Type. Empty fields are omitted from JSON.
type LookupData struct {
	Device    *DeviceResult    `json:"device,omitempty"`
	IP        *IPResult        `json:"ip,omitempty"`
	Prefix    *PrefixResult    `json:"prefix,omitempty"`
	ASN       *ASNResult       `json:"asn,omitempty"`
	Interface *InterfaceResult `json:"interface,omitempty"`
}

// DeviceResult is a trimmed Netbox device suitable for the UI.
type DeviceResult struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Status       string `json:"status,omitempty"`
	Role         string `json:"role,omitempty"`
	Site         string `json:"site,omitempty"`
	DeviceType   string `json:"device_type,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	PrimaryIP    string `json:"primary_ip,omitempty"`
	Description  string `json:"description,omitempty"`
	URL          string `json:"url,omitempty"`
}

// IPResult is a trimmed Netbox IP address.
type IPResult struct {
	ID          int    `json:"id"`
	Address     string `json:"address"`
	Status      string `json:"status,omitempty"`
	Role        string `json:"role,omitempty"`
	DNSName     string `json:"dns_name,omitempty"`
	Device      string `json:"device,omitempty"`
	Interface   string `json:"interface,omitempty"`
	VRF         string `json:"vrf,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
}

// PrefixResult is a trimmed Netbox prefix.
type PrefixResult struct {
	ID          int    `json:"id"`
	Prefix      string `json:"prefix"`
	Status      string `json:"status,omitempty"`
	Role        string `json:"role,omitempty"`
	Site        string `json:"site,omitempty"`
	VLAN        string `json:"vlan,omitempty"`
	VRF         string `json:"vrf,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
}

// ASNResult is a trimmed Netbox ASN.
type ASNResult struct {
	ID          int    `json:"id"`
	ASN         int64  `json:"asn"`
	Description string `json:"description,omitempty"`
	RIR         string `json:"rir,omitempty"`
	URL         string `json:"url,omitempty"`
}

// InterfaceResult is a trimmed Netbox interface.
type InterfaceResult struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	Device            string `json:"device,omitempty"`
	Type              string `json:"type,omitempty"`
	MTU               int    `json:"mtu,omitempty"`
	MACAddress        string `json:"mac_address,omitempty"`
	Description       string `json:"description,omitempty"`
	Enabled           *bool  `json:"enabled,omitempty"`
	LAG               string `json:"lag,omitempty"`
	ConnectedEndpoint string `json:"connected_endpoint,omitempty"`
	URL               string `json:"url,omitempty"`
}
