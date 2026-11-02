// Command wincheck checks the installed Windows app's VPN service on a CI
// runner, as the app drives it: it adds a key of the test servers
// (scripts/soak, started with -outside on this computer), connects in the
// given mode, opens sites through the TUN, reads the traffic through the
// server, follows a restart of the service and disconnects. Whatever fails,
// and the service's journal, go to the run's annotations.
//
//	wincheck -key vless://... -mode all
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	key := flag.String("key", "", "key of the test servers")
	mode := flag.String("mode", "all", "routing mode")
	flag.Parse()
	c, err := dial()
	if err == nil {
		err = check(c, *key, *mode)
	}
	if err != nil {
		fmt.Printf("::error::%s\n", err)
		if c != nil {
			journal(c)
		}
		os.Exit(1)
	}
	fmt.Println("wincheck: all checks passed")
}

type conn struct {
	c      net.Conn
	r      *bufio.Reader
	id     int
	status chan map[string]any
	answer chan map[string]any
}

func controlFile() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, "TigerSB", "control.json")
}

// dial connects as the app does, waiting for the service to come up.
func dial() (*conn, error) {
	var ctl struct {
		Port  int    `json:"port"`
		Token string `json:"token"`
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		b, err := os.ReadFile(controlFile())
		if err == nil && json.Unmarshal(b, &ctl) == nil {
			if c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", ctl.Port)); err == nil {
				out := &conn{c: c, r: bufio.NewReader(c), status: make(chan map[string]any, 64), answer: make(chan map[string]any, 8)}
				fmt.Fprintf(c, "{\"token\":%q}\n", ctl.Token)
				go out.read()
				return out, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("служба не ответила за 30 с (файл управления: %v)", err)
		}
		time.Sleep(time.Second)
	}
}

func (c *conn) read() {
	for {
		line, err := c.r.ReadBytes('\n')
		if err != nil {
			close(c.status)
			close(c.answer)
			return
		}
		var m map[string]any
		json.Unmarshal(line, &m)
		if m["ev"] == "status" {
			fmt.Printf("status: %v %v\n", m["phase"], m["error"])
			c.status <- m
		} else {
			c.answer <- m
		}
	}
}

func (c *conn) call(m string, args map[string]any) (any, error) {
	c.id++
	b, _ := json.Marshal(map[string]any{"id": c.id, "m": m, "a": args})
	if _, err := c.c.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	select {
	case a, ok := <-c.answer:
		if !ok {
			return nil, errors.New("служба закрыла соединение")
		}
		if e, ok := a["e"].(string); ok {
			return nil, fmt.Errorf("%s: %s", m, e)
		}
		return a["r"], nil
	case <-time.After(2 * time.Minute):
		return nil, fmt.Errorf("%s: нет ответа", m)
	}
}

// waitPhase waits for the status to reach phase.
func (c *conn) waitPhase(phase string, d time.Duration) error {
	timeout := time.After(d)
	for {
		select {
		case m, ok := <-c.status:
			if !ok {
				return errors.New("служба закрыла соединение")
			}
			if m["phase"] == phase {
				return nil
			}
			if m["phase"] == "error" {
				return fmt.Errorf("ошибка VPN: %v", m["error"])
			}
		case <-timeout:
			return fmt.Errorf("за %s VPN не дошёл до %s", d, phase)
		}
	}
}

func check(c *conn, key, mode string) error {
	if err := c.waitPhase("disconnected", 5*time.Second); err != nil {
		return err
	}
	if _, err := c.call("addKey", map[string]any{"key": key}); err != nil {
		return err
	}
	if _, err := c.call("setMode", map[string]any{"mode": mode}); err != nil {
		return err
	}
	if _, err := c.call("connect", nil); err != nil {
		return err
	}
	if err := c.waitPhase("connected", time.Minute); err != nil {
		return err
	}
	show("route", "print", "-4")
	show("ipconfig", "/all")
	for _, site := range []string{"https://www.google.com/generate_204", "https://cp.cloudflare.com/generate_204", "https://github.com/"} {
		if err := get(site); err != nil {
			return fmt.Errorf("через VPN: %w", err)
		}
	}
	stats, err := c.call("stats", nil)
	if err != nil {
		return err
	}
	var st struct{ Up, Down, Ping int64 }
	json.Unmarshal([]byte(stats.(string)), &st)
	fmt.Printf("stats: %s\n", stats)
	if st.Down < 10_000 {
		return fmt.Errorf("сайты открылись, но не через сервер: получено через него %d байт", st.Down)
	}
	if err := c.waitPhaseNot(); err != nil {
		return err
	}
	if err := refusesIPv6(); err != nil {
		return err
	}
	if err := c.programOutside(); err != nil {
		return err
	}

	// Off and on again: the TUN adapter of the first start must be gone.
	for i := range 2 {
		if _, err := c.call("disconnect", nil); err != nil {
			return err
		}
		if err := c.waitPhase("disconnected", 10*time.Second); err != nil {
			return err
		}
		if _, err := c.call("connect", nil); err != nil {
			return err
		}
		if err := c.waitPhase("connected", time.Minute); err != nil {
			return fmt.Errorf("включение №%d после выключения: %w", i+2, err)
		}
		if err := get("https://cp.cloudflare.com/generate_204"); err != nil {
			return fmt.Errorf("включение №%d после выключения: %w", i+2, err)
		}
	}

	// After a restart of the service the app's connection comes back and
	// the user's choice stays: the VPN is off, as the service stopped, and
	// remembered as on for the next start with Windows.
	if out, err := exec.Command("sc.exe", "stop", "TigerSB").CombinedOutput(); err != nil {
		return fmt.Errorf("sc stop: %v %s", err, out)
	}
	time.Sleep(5 * time.Second)
	if err := get("https://www.google.com/generate_204"); err != nil {
		return fmt.Errorf("после остановки службы сайты не открываются напрямую: %w", err)
	}
	if out, err := exec.Command("sc.exe", "start", "TigerSB").CombinedOutput(); err != nil {
		return fmt.Errorf("sc start: %v %s", err, out)
	}
	c.c.Close()
	time.Sleep(2 * time.Second)
	c2, err := dial()
	if err != nil {
		return err
	}
	*c = *c2
	if on, err := c.call("wasOn", nil); err != nil || on != true {
		return fmt.Errorf("после перезапуска службы не помнит, что VPN был включён: %v %v", on, err)
	}
	if _, err := c.call("connect", nil); err != nil {
		return err
	}
	if err := c.waitPhase("connected", time.Minute); err != nil {
		return fmt.Errorf("после перезапуска службы: %w", err)
	}
	if err := get("https://cp.cloudflare.com/generate_204"); err != nil {
		return fmt.Errorf("после перезапуска службы: %w", err)
	}
	if _, err := c.call("disconnect", nil); err != nil {
		return err
	}
	if err := c.waitPhase("disconnected", 10*time.Second); err != nil {
		return err
	}
	if err := get("https://www.google.com/generate_204"); err != nil {
		return fmt.Errorf("после отключения VPN сайты не открываются: %w", err)
	}
	journal(c)
	return nil
}

// waitPhaseNot makes sure the VPN stays connected for a while: the watchdog
// does not tear it down.
func (c *conn) waitPhaseNot() error {
	timeout := time.After(25 * time.Second)
	for {
		select {
		case m, ok := <-c.status:
			if !ok {
				return errors.New("служба закрыла соединение")
			}
			if m["phase"] != "connected" {
				return fmt.Errorf("VPN упал: %v %v", m["phase"], m["error"])
			}
		case <-timeout:
			return nil
		}
	}
}

// refusesIPv6 checks that an IPv6 connection fails quickly while the VPN is
// on (Windows tries a refused connection again for about 2 s), so that a
// browser falls back to IPv4. Before, the core took it and
// closed it only then, and sites with IPv6 addresses, such as YouTube, did
// not open in browsers that resolve names themselves.
func refusesIPv6() error {
	start := time.Now()
	c, err := net.DialTimeout("tcp", "[2606:4700:4700::1111]:443", 10*time.Second)
	took := time.Since(start).Round(time.Millisecond)
	if err == nil {
		c.Close()
		return fmt.Errorf("IPv6-соединение открылось через VPN за %s, а должно сразу отклоняться", took)
	}
	fmt.Printf("IPv6: %v in %s\n", err, took)
	if took > 5*time.Second {
		return fmt.Errorf("IPv6-соединение отклонено только через %s: браузеры будут долго ждать", took)
	}
	return nil
}

// programOutside leaves wincheck itself out of the VPN, as a user leaves a
// game: the service must list it among running programs, and its sites
// must then open without traffic through the server.
func (c *conn) programOutside() error {
	self := filepath.Base(os.Args[0])
	if !strings.HasSuffix(strings.ToLower(self), ".exe") {
		self += ".exe"
	}
	list, err := c.call("apps", nil)
	if err != nil {
		return err
	}
	found := false
	for _, p := range list.([]any) {
		if m, _ := p.(map[string]any); m != nil && strings.EqualFold(fmt.Sprint(m["name"]), self) {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("в списке процессов нет %s: %v", self, list)
	}
	down := func() int64 {
		s, _ := c.call("stats", nil)
		var st struct{ Down int64 }
		json.Unmarshal([]byte(fmt.Sprint(s)), &st)
		return st.Down
	}
	if _, err := c.call("setUserApps", map[string]any{"apps": []string{self}}); err != nil {
		return err
	}
	if err := c.waitPhase("connected", time.Minute); err != nil {
		return fmt.Errorf("после выбора процесса: %w", err)
	}
	before := down()
	for _, site := range []string{"https://github.com/", "https://www.google.com/generate_204"} {
		if err := get(site); err != nil {
			return fmt.Errorf("%s мимо VPN: %w", self, err)
		}
	}
	through := down() - before
	fmt.Printf("%s outside the VPN: %d bytes through the server\n", self, through)
	if through > 5_000 {
		return fmt.Errorf("%s выбран мимо VPN, но через сервер прошло %d байт", self, through)
	}
	if _, err := c.call("setUserApps", map[string]any{"apps": []string{}}); err != nil {
		return err
	}
	return c.waitPhase("connected", time.Minute)
}

func get(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	start := time.Now()
	res, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", url, err)
	}
	res.Body.Close()
	fmt.Printf("%s -> %d in %s\n", url, res.StatusCode, time.Since(start).Round(time.Millisecond))
	if res.StatusCode >= 400 {
		return fmt.Errorf("%s: %d", url, res.StatusCode)
	}
	return nil
}

func show(name string, args ...string) {
	out, _ := exec.Command(name, args...).CombinedOutput()
	fmt.Printf("$ %s %s\n%s\n", name, strings.Join(args, " "), out)
}

// journal puts the service's journal into the annotations.
func journal(c *conn) {
	j, err := c.call("journal", nil)
	if err != nil {
		fmt.Printf("::warning::журнал: %v\n", err)
		return
	}
	text, _ := j.(string)
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > 60 {
		lines = lines[len(lines)-60:]
	}
	fmt.Printf("::notice title=journal::%s\n", strings.ReplaceAll(strings.Join(lines, "\n"), "\n", "%0A"))
}
