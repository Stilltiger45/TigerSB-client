package xrayconf

import (
	"encoding/base64"
	"encoding/json"
	stderrors "errors"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/proto"
)

// MieruProfile decodes the client config of a full mieru:// link and returns
// its active profile.
func MieruProfile(enc string) (*appctlpb.ClientProfile, error) {
	b, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, stderrors.New("неверная ссылка mieru://")
	}
	c := &appctlpb.ClientConfig{}
	if err := proto.Unmarshal(b, c); err != nil {
		return nil, stderrors.New("неверная ссылка mieru://")
	}
	for _, p := range c.GetProfiles() {
		if p.GetProfileName() == c.GetActiveProfile() {
			return p, nil
		}
	}
	if len(c.GetProfiles()) == 0 {
		return nil, stderrors.New("в ссылке mieru:// нет профиля")
	}
	return c.GetProfiles()[0], nil
}

// mieruOverUDP reports whether a mieru outbound's first port is UDP, from
// the simple link's ports or the full link's config.
func mieruOverUDP(settings json.RawMessage) bool {
	var s struct {
		Ports []struct {
			Protocol string `json:"protocol"`
		} `json:"ports"`
		Config string `json:"config"`
	}
	if json.Unmarshal(settings, &s) != nil {
		return false
	}
	if len(s.Ports) > 0 {
		return s.Ports[0].Protocol == "UDP"
	}
	p, err := MieruProfile(s.Config)
	if err != nil {
		return false
	}
	for _, server := range p.GetServers() {
		for _, b := range server.GetPortBindings() {
			return b.GetProtocol() == appctlpb.TransportProtocol_UDP
		}
	}
	return false
}
