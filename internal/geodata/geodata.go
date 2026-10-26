// Package geodata downloads geosite.dat and geoip.dat when a profile's
// routing lists use "geosite:" or "geoip:", and keeps only the lists the
// profile needs: the full Russian files weigh about 90 MB, a trimmed copy a
// few megabytes.
package geodata

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// Default sources, used when the profile names none.
const (
	DefaultGeoSite = "https://github.com/runetfreedom/russia-v2ray-rules-dat/releases/latest/download/geosite.dat"
	DefaultGeoIP   = "https://github.com/runetfreedom/russia-v2ray-rules-dat/releases/latest/download/geoip.dat"
)

// File names Xray looks for in its asset directory.
const (
	GeoSiteFile = "geosite.dat"
	GeoIPFile   = "geoip.dat"
)

// maxEntry caps one list inside a downloaded file.
const maxEntry = 256 << 20

// Codes returns the geosite and geoip list names the profile refers to, in
// upper case as Xray stores them. extraIPs are more "geoip:" lists to keep.
func Codes(p *profile.Profile, extraIPs ...string) (sites, ips []string) {
	add := func(list *[]string, code string) {
		code = strings.ToUpper(strings.TrimPrefix(code, "!"))
		if i := strings.IndexByte(code, '@'); i >= 0 {
			code = code[:i]
		}
		if code != "" && !slices.Contains(*list, code) {
			*list = append(*list, code)
		}
	}
	for _, d := range slices.Concat(p.Routing.Blocked.Domains, p.Routing.Direct.Domains) {
		if code, ok := strings.CutPrefix(d, "geosite:"); ok {
			add(&sites, code)
		}
	}
	for _, ip := range slices.Concat(p.Routing.Blocked.IPs, p.Routing.Direct.IPs, extraIPs) {
		if code, ok := strings.CutPrefix(ip, "geoip:"); ok {
			add(&ips, code)
		}
	}
	return sites, ips
}

// Ensure makes sure dir holds geosite.dat and geoip.dat with every list the
// profile uses. A file is downloaded again when a list is missing or the file
// is older than maxAge (0 keeps it forever). If a refresh of an old but
// complete file fails, the old file stays and Ensure succeeds.
func Ensure(ctx context.Context, c *http.Client, dir string, p *profile.Profile, maxAge time.Duration, extraIPs ...string) error {
	if c == nil {
		c = http.DefaultClient
	}
	sites, ips := Codes(p, extraIPs...)
	siteURL, ipURL := DefaultGeoSite, DefaultGeoIP
	if g := p.Geodata; g != nil {
		if g.GeoSite != "" {
			siteURL = g.GeoSite
		}
		if g.GeoIP != "" {
			ipURL = g.GeoIP
		}
	}
	for _, f := range []struct {
		name, url string
		codes     []string
	}{{GeoSiteFile, siteURL, sites}, {GeoIPFile, ipURL, ips}} {
		if len(f.codes) == 0 {
			continue
		}
		if err := ensureFile(ctx, c, filepath.Join(dir, f.name), f.url, f.codes, maxAge); err != nil {
			return err
		}
	}
	return nil
}

// Ready reports whether dir already holds every list the profile uses, so a
// start needs no download.
func Ready(dir string, p *profile.Profile, extraIPs ...string) bool {
	sites, ips := Codes(p, extraIPs...)
	return (len(sites) == 0 || missing(filepath.Join(dir, GeoSiteFile), sites) == nil) &&
		(len(ips) == 0 || missing(filepath.Join(dir, GeoIPFile), ips) == nil)
}

func ensureFile(ctx context.Context, c *http.Client, path, url string, codes []string, maxAge time.Duration) error {
	complete := false
	if fi, err := os.Stat(path); err == nil {
		complete = missing(path, codes) == nil
		if complete && (maxAge == 0 || time.Since(fi.ModTime()) < maxAge) {
			return nil
		}
	}
	err := download(ctx, c, path, url, codes)
	if err != nil && complete {
		return nil
	}
	return err
}

func download(ctx context.Context, c *http.Client, path, url string, codes []string) error {
	name := filepath.Base(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("не удалось скачать %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("не удалось скачать %s: сервер ответил %s", name, resp.Status)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), name+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	w := bufio.NewWriter(tmp)
	found, err := trim(resp.Body, w, codes)
	if err == nil {
		err = w.Flush()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("не удалось скачать %s: %w", name, err)
	}
	for _, code := range codes {
		if !slices.Contains(found, code) {
			return fmt.Errorf("в %s нет списка %s", name, strings.ToLower(code))
		}
	}
	return os.Rename(tmp.Name(), path)
}

// trim copies the entries of a GeoSiteList or GeoIPList whose code is in
// codes and returns the codes it found. Both messages are a repeated field 1
// of entries that start with their code in field 1, so one reader handles both.
func trim(r io.Reader, w io.Writer, codes []string) (found []string, err error) {
	err = entries(r, func(code string, length int, body io.Reader) error {
		if !slices.Contains(codes, code) {
			_, err := io.CopyN(io.Discard, body, int64(length))
			return err
		}
		found = append(found, code)
		hdr := binary.AppendUvarint([]byte{0x0a}, uint64(length))
		if _, err := w.Write(hdr); err != nil {
			return err
		}
		_, err := io.CopyN(w, body, int64(length))
		return err
	})
	return found, err
}

// missing returns an error naming the first code the file at path lacks.
func missing(path string, codes []string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var found []string
	err = entries(f, func(code string, length int, body io.Reader) error {
		found = append(found, code)
		_, err := io.CopyN(io.Discard, body, int64(length))
		return err
	})
	if err != nil {
		return err
	}
	for _, code := range codes {
		if !slices.Contains(found, code) {
			return fmt.Errorf("нет списка %s", code)
		}
	}
	return nil
}

var errFormat = errors.New("файл не похож на geosite.dat или geoip.dat")

// entries calls fn for every top-level entry with its upper-cased code. fn
// must consume exactly length bytes of body, which starts at the entry's
// first byte.
func entries(r io.Reader, fn func(code string, length int, body io.Reader) error) error {
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		tag, err := br.ReadByte()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if tag != 0x0a {
			return errFormat
		}
		n, err := binary.ReadUvarint(br)
		if err != nil || n == 0 || n > maxEntry {
			return errFormat
		}
		// The entry's own field 1 is its code: tag, length, bytes.
		head, err := br.Peek(min(int(n), 2+128))
		if err != nil || len(head) < 2 || head[0] != 0x0a {
			return errFormat
		}
		codeLen := int(head[1])
		if codeLen >= 0x80 || 2+codeLen > len(head) {
			return errFormat
		}
		code := strings.ToUpper(string(head[2 : 2+codeLen]))
		if err := fn(code, int(n), br); err != nil {
			return err
		}
	}
}
