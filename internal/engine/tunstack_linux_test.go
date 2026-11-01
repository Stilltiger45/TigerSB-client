package engine

import "github.com/xtls/xray-core/proxy/tun"

func platformTun() tun.Tun { return &tun.LinuxTun{} }
