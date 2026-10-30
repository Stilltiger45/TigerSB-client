package profile

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

const (
	wgPrivate = "eCtXsJZ27+4PbhDkHnB923tkUn2Gj59wZw5wFA75MnU="
	wgPublic  = "Cr8hWlKvtDt7nrvf+f0brNQQzabAqrjfBvas9pmowjo="
	wgPSK     = "31aIhAPwktDGpH4JDhA8GNvjFXEf/a6+UaQRyOAiyfM="
)

// awgConf is an AmneziaWG 3 client config as the AmneziaWG apps export it.
const awgConf = `[Interface]
PrivateKey = ` + wgPrivate + `
Address = 10.8.1.2/32, fd00::2/128
DNS = 1.1.1.1, 1.0.0.1
MTU = 1280
Jc = 5
Jmin = 61
Jmax = 190
S1 = 88
S2 = 47
S3 = 29
S4 = 17
H1 = 234567-345678
H2 = 3456789-4567890
H3 = 56789012-67890123
H4 = 456789012-567890123
I1 = <r 2><b 0x858000010002000000001c><rc 28>
HeaderProtectionKey = ` + wgPSK + `
ContentPaddingAddition = 32-128
J1 = <r 10>

[Peer]
PublicKey = ` + wgPublic + `
PresharedKey = ` + wgPSK + `
Endpoint = awg.example.org:39743
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 20-30
`

type wgOutbound struct {
	Protocol string `json:"protocol"`
	Settings struct {
		SecretKey string   `json:"secretKey"`
		Address   []string `json:"address"`
		DNS       []string `json:"dns"`
		MTU       int      `json:"mtu"`
		Peers     []struct {
			PublicKey    string   `json:"publicKey"`
			PreSharedKey string   `json:"preSharedKey"`
			Endpoint     string   `json:"endpoint"`
			KeepAlive    any      `json:"keepAlive"`
			AllowedIPs   []string `json:"allowedIPs"`
		} `json:"peers"`
		Amnezia map[string]string `json:"amnezia"`
	} `json:"settings"`
}

func readWG(t *testing.T, raw json.RawMessage) wgOutbound {
	t.Helper()
	var ob wgOutbound
	if err := json.Unmarshal(raw, &ob); err != nil {
		t.Fatal(err)
	}
	return ob
}

// A .conf becomes a wireguard:// link that keeps everything in it.
func TestWireGuardConf(t *testing.T) {
	key, err := ParseKey("\n" + awgConf)
	if err != nil {
		t.Fatal(err)
	}
	if key.Kind != KindVLESS || !strings.HasPrefix(key.Raw, "wireguard://") {
		t.Fatalf("key %+v", key)
	}
	p, err := FromVLESS(key.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "awg.example.org" {
		t.Errorf("name %q", p.Name)
	}
	ob := readWG(t, p.Exits[0].Transports[0].Xray)
	s := ob.Settings
	if ob.Protocol != "wireguard" || s.SecretKey != wgPrivate || s.MTU != 1280 ||
		strings.Join(s.Address, ",") != "10.8.1.2/32,fd00::2/128" || strings.Join(s.DNS, ",") != "1.1.1.1,1.0.0.1" {
		t.Errorf("interface %+v", s)
	}
	peer := s.Peers[0]
	if peer.PublicKey != wgPublic || peer.PreSharedKey != wgPSK || peer.Endpoint != "awg.example.org:39743" ||
		peer.KeepAlive != "20-30" || len(peer.AllowedIPs) != 2 {
		t.Errorf("peer %+v", peer)
	}
	want := map[string]string{
		"Jc": "5", "Jmin": "61", "Jmax": "190", "S1": "88", "S2": "47", "S3": "29", "S4": "17",
		"H1": "234567-345678", "H4": "456789012-567890123", "I1": "<r 2><b 0x858000010002000000001c><rc 28>",
		"HeaderProtectionKey": wgPSK, "ContentPaddingAddition": "32-128",
	}
	for k, v := range want {
		if s.Amnezia[k] != v {
			t.Errorf("%s = %q, want %q", k, s.Amnezia[k], v)
		}
	}
	if _, ok := s.Amnezia["J1"]; ok {
		t.Error("AmneziaWG 1.5's J1 kept")
	}
}

// Links as v2rayN and other clients write them, keys unescaped and all.
func TestWireGuardLinks(t *testing.T) {
	link := "wireguard://" + wgPrivate + "@162.159.192.1:2408/?publickey=" + wgPublic +
		"&reserved=1,2,3&address=172.16.0.2/32,2606:4700::2/128&mtu=1420&jc=4&JMIN=10&jmax=50&h1=5&i1=%3Cr%2016%3E#WARP"
	raw, name, err := ParseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	ob := readWG(t, raw)
	if name != "WARP" || ob.Settings.SecretKey != wgPrivate || ob.Settings.Peers[0].PublicKey != wgPublic ||
		ob.Settings.Peers[0].KeepAlive != float64(25) || ob.Settings.MTU != 1420 ||
		ob.Settings.Amnezia["Jmin"] != "10" || ob.Settings.Amnezia["I1"] != "<r 16>" || ob.Settings.Amnezia["H1"] != "5" {
		t.Errorf("outbound %s", raw)
	}
	for _, bad := range []string{
		"wireguard://x@1.2.3.4:51820?publickey=" + wgPublic + "&address=10.0.0.2/32",
		"wireguard://" + wgPrivate + "@1.2.3.4:51820?address=10.0.0.2/32",
		"wireguard://" + wgPrivate + "@1.2.3.4:51820?publickey=" + wgPublic,
		"wireguard://" + wgPrivate + "@1.2.3.4?publickey=" + wgPublic + "&address=10.0.0.2/32",
		"wg://" + wgPrivate + "@1.2.3.4:51820?publickey=" + wgPublic + "&address=10.0.0.2/32&headerprotectionkey=short",
	} {
		if _, _, err := ParseLink(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	if _, err := ParseWireGuardConf("[Interface]\nPrivateKey = " + wgPrivate + "\nAddress = 10.0.0.2/32\n"); err == nil {
		t.Error("a config without [Peer] accepted")
	}
}

// amneziaKey writes an Amnezia vpn:// key as the Amnezia app shares it.
func amneziaKey(t *testing.T, config map[string]any) string {
	t.Helper()
	b, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var z bytes.Buffer
	binary.Write(&z, binary.BigEndian, uint32(len(b)))
	w := zlib.NewWriter(&z)
	w.Write(b)
	w.Close()
	return "vpn://" + base64.RawURLEncoding.EncodeToString(z.Bytes())
}

func TestAmneziaKey(t *testing.T) {
	conf := strings.Replace(awgConf, "DNS = 1.1.1.1, 1.0.0.1", "DNS = $PRIMARY_DNS, $SECONDARY_DNS", 1)
	conf = strings.Replace(conf, "Jc = 5\n", "", 1)
	last, _ := json.Marshal(map[string]any{"config": conf, "Jc": "6", "H1": "1", "hostName": "awg.example.org", "mtu": "1280"})
	key := amneziaKey(t, map[string]any{
		"containers": []any{
			map[string]any{"container": "amnezia-xray", "xray": map[string]any{"last_config": "{}"}},
			map[string]any{"container": "amnezia-awg2", "awg": map[string]any{"last_config": string(last), "port": "39743"}},
		},
		"defaultContainer": "amnezia-awg2",
		"description":      "Мой сервер",
		"dns1":             "9.9.9.9",
		"dns2":             "149.112.112.112",
	})
	p, err := FromVLESS(key)
	if err != nil {
		t.Fatal(err)
	}
	ob := readWG(t, p.Exits[0].Transports[0].Xray)
	if p.Name != "Мой сервер" || strings.Join(ob.Settings.DNS, ",") != "9.9.9.9,149.112.112.112" ||
		ob.Settings.Amnezia["Jc"] != "6" || ob.Settings.Amnezia["H1"] != "234567-345678" {
		t.Errorf("profile %s: %+v", p.Name, ob.Settings)
	}
	premium := amneziaKey(t, map[string]any{"api_config": map[string]any{"service_type": "amnezia-premium"}})
	if _, err := FromVLESS(premium); err == nil || !strings.Contains(err.Error(), "Amnezia") {
		t.Errorf("premium key: %v", err)
	}
}
