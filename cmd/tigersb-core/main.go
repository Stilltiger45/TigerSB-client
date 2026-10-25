// Command tigersb-core is the TigerSB client core as a console tool: import a
// key, build the Xray config and run it with local proxy ports. The Android
// and Windows apps will drive the same core.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/geodata"
	"github.com/Stilltiger45/TigerSB-client/internal/smart"
	"github.com/Stilltiger45/TigerSB-client/internal/subscription"
	"github.com/Stilltiger45/TigerSB-client/internal/tuic"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// geodataMaxAge is how long downloaded geosite.dat and geoip.dat stay fresh.
const geodataMaxAge = 24 * time.Hour

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `TigerSB client core

Использование:
  tigersb-core import <ключ>   добавить ключ (tigersb://, https:// или ссылку vless://, vmess://, trojan://, ss://)
  tigersb-core update          обновить профиль по подписке
  tigersb-core show            показать выходы и транспорты профиля
  tigersb-core config          напечатать конфиг Xray
  tigersb-core run             запустить VPN с локальными портами SOCKS и HTTP
  tigersb-core version         версия

Для панели и отладки:
  tigersb-core keygen          создать ключи подписи профиля
  tigersb-core sign            подписать профиль (JSON) закрытым ключом
  tigersb-core link            собрать ключ tigersb:// из адреса подписки и публичного ключа

Режимы (-mode):
  auto (по умолчанию)  каждый сайт проверяется через все серверы и идёт через
                       самый быстрый, который его открывает, а если не открывает
                       ни один — напрямую; банки и вузы сразу напрямую
  basic                российские сайты (.ru, .рф и российские IP), банки и
                       вузы напрямую, остальное через VPN
  main                 заблокированное через выход за границей, остальное через
                       выход в РФ, банки и вузы напрямую
  all                  всё через VPN, кроме локальной сети
  off                  без VPN
`

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	subscription.UserAgent = "TigerSB-client/" + version
	var err error
	args := os.Args[2:]
	switch os.Args[1] {
	case "import":
		err = cmdImport(args)
	case "update":
		err = cmdUpdate(args)
	case "show":
		err = cmdShow(args)
	case "config":
		err = cmdConfig(args)
	case "run":
		err = cmdRun(args)
	case "keygen":
		err = cmdKeygen(args)
	case "sign":
		err = cmdSign(args)
	case "link":
		err = cmdLink(args)
	case "version":
		fmt.Println(version)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		log.Fatal("Ошибка: ", err)
	}
}

func stateDirFlag(fs *flag.FlagSet) *string {
	def, _ := subscription.DefaultDir()
	return fs.String("state", def, "каталог состояния клиента")
}

func cmdImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	dir := stateDirFlag(fs)
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("нужен ровно один ключ: tigersb-core import <ключ>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := &subscription.Client{}
	st, p, err := c.Import(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if err := subscription.Save(*dir, st); err != nil {
		return err
	}
	fmt.Printf("Ключ добавлен: %s\n", title(p))
	printExits(p)
	return nil
}

func cmdUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	dir := stateDirFlag(fs)
	_ = fs.Parse(args)
	st, err := subscription.Load(*dir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := &subscription.Client{}
	p, err := c.Refresh(ctx, st)
	if err != nil {
		return err
	}
	if err := subscription.Save(*dir, st); err != nil {
		return err
	}
	fmt.Printf("Профиль обновлён: %s (от %s)\n", title(p), p.Updated.Format(time.DateTime))
	return nil
}

func cmdShow(args []string) error {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	dir := stateDirFlag(fs)
	_ = fs.Parse(args)
	p, err := loadProfile(*dir)
	if err != nil {
		return err
	}
	fmt.Printf("Профиль: %s, обновлён %s\n", title(p), p.Updated.Format(time.DateTime))
	printExits(p)
	fmt.Printf("Заблокированное: %d доменов, %d адресов. Напрямую: %d доменов от панели + встроенный список банков и вузов.\n",
		len(p.Routing.Blocked.Domains), len(p.Routing.Blocked.IPs), len(p.Routing.Direct.Domains))
	return nil
}

type runFlags struct {
	dir    *string
	mode   *string
	socks  *string
	http   *string
	assets *string
	log    *string
}

func addRunFlags(fs *flag.FlagSet) runFlags {
	return runFlags{
		dir:    stateDirFlag(fs),
		mode:   fs.String("mode", string(xrayconf.ModeAuto), "режим: auto, basic, main, all или off"),
		socks:  fs.String("socks", "127.0.0.1:10808", "адрес локального SOCKS5 (пусто — не открывать)"),
		http:   fs.String("http", "127.0.0.1:10809", "адрес локального HTTP-прокси (пусто — не открывать)"),
		assets: fs.String("assets", "", "каталог с geosite.dat и geoip.dat (по умолчанию скачиваются в каталог состояния)"),
		log:    fs.String("log", "warning", "уровень журнала Xray: debug, info, warning, error, none"),
	}
}

func (f runFlags) build() ([]byte, *profile.Profile, error) {
	p, err := loadProfile(*f.dir)
	if err != nil {
		return nil, nil, err
	}
	mode, err := xrayconf.ParseMode(*f.mode)
	if err != nil {
		return nil, nil, err
	}
	opt := xrayconf.Options{Mode: mode, LogLevel: *f.log}
	for _, ib := range []struct{ tag, proto, addr string }{
		{"socks-in", "socks", *f.socks},
		{"http-in", "http", *f.http},
	} {
		if ib.addr == "" {
			continue
		}
		host, portStr, err := net.SplitHostPort(ib.addr)
		if err != nil {
			return nil, nil, fmt.Errorf("неверный адрес %q: %w", ib.addr, err)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil || port < 1 || port > 65535 {
			return nil, nil, fmt.Errorf("неверный порт в адресе %q", ib.addr)
		}
		opt.Inbounds = append(opt.Inbounds, xrayconf.Inbound{Tag: ib.tag, Protocol: ib.proto, Listen: host, Port: port})
	}
	cfg, err := xrayconf.Build(p, opt)
	return cfg, p, err
}

func cmdConfig(args []string) error {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	f := addRunFlags(fs)
	_ = fs.Parse(args)
	cfg, _, err := f.build()
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(cfg, '\n'))
	return err
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	f := addRunFlags(fs)
	_ = fs.Parse(args)
	cfg, p, err := f.build()
	if err != nil {
		return err
	}
	assets := *f.assets
	mode, _ := xrayconf.ParseMode(*f.mode)
	if needed, extra := xrayconf.GeoData(p, mode); needed && assets == "" {
		assets = filepath.Join(*f.dir, "geodata")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		err := geodata.Ensure(ctx, nil, assets, p, geodataMaxAge, extra...)
		cancel()
		if err != nil {
			return err
		}
	}
	if err := engine.SetAssetDir(assets); err != nil {
		return err
	}
	inst, err := engine.Start(cfg, append(tuic.Hooks(p), smart.Hooks(p, mode)...)...)
	if err != nil {
		return err
	}
	defer engine.Stop(inst)
	fmt.Printf("VPN запущен: %s, режим %s.", title(p), *f.mode)
	if *f.socks != "" {
		fmt.Printf(" SOCKS5 %s.", *f.socks)
	}
	if *f.http != "" {
		fmt.Printf(" HTTP %s.", *f.http)
	}
	fmt.Println(" Остановить: Ctrl+C")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	fmt.Println("Остановка…")
	return nil
}

func cmdKeygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	_ = fs.Parse(args)
	pub, priv, err := profile.GenerateKey()
	if err != nil {
		return err
	}
	fmt.Println("Публичный ключ (кладётся в ключ tigersb://):", profile.EncodeKey(pub))
	fmt.Println("Закрытый ключ (храните только на панели):  ", profile.EncodeKey(priv.Seed()))
	return nil
}

func cmdSign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	keyFile := fs.String("key", "", "файл с закрытым ключом (base64url)")
	in := fs.String("in", "", "профиль JSON")
	out := fs.String("out", "", "куда записать конверт (по умолчанию stdout)")
	_ = fs.Parse(args)
	if *keyFile == "" || *in == "" {
		return errors.New("нужны -key и -in")
	}
	keyText, err := os.ReadFile(*keyFile)
	if err != nil {
		return err
	}
	priv, err := profile.DecodePrivateKey(string(bytes.TrimSpace(keyText)))
	if err != nil {
		return err
	}
	data, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	env, err := profile.Sign(priv, data)
	if err != nil {
		return err
	}
	env = append(env, '\n')
	if *out == "" {
		_, err = os.Stdout.Write(env)
		return err
	}
	return os.WriteFile(*out, env, 0o644)
}

func cmdLink(args []string) error {
	fs := flag.NewFlagSet("link", flag.ExitOnError)
	u := fs.String("url", "", "адрес подписки https://")
	pk := fs.String("pk", "", "публичный ключ подписи (base64url)")
	name := fs.String("name", "", "название (необязательно)")
	_ = fs.Parse(args)
	pub, err := profile.DecodePublicKey(*pk)
	if err != nil {
		return err
	}
	k := profile.Key{Kind: profile.KindSubscription, URL: *u, PK: pub, Name: *name}
	if _, err := profile.ParseKey(k.String()); err != nil {
		return err
	}
	fmt.Println(k.String())
	return nil
}

func loadProfile(dir string) (*profile.Profile, error) {
	st, err := subscription.Load(dir)
	if err != nil {
		return nil, err
	}
	return st.Open()
}

func title(p *profile.Profile) string {
	if p.Name != "" {
		return p.Name
	}
	return p.ID
}

func printExits(p *profile.Profile) {
	names := map[profile.Role]string{profile.RoleForeign: "за границей", profile.RoleRU: "в РФ"}
	for _, e := range p.Exits {
		fmt.Printf("  выход %s (%s): %d транспорт(ов)\n", e.ID, names[e.Role], len(e.Transports))
	}
	if !p.HasRole(profile.RoleRU) {
		if len(p.Routing.Blocked.Domains)+len(p.Routing.Blocked.IPs) == 0 {
			fmt.Println("  Выхода в РФ и списков блокировок нет: в режиме main всё, кроме банков и вузов, пойдёт через VPN.")
		} else {
			fmt.Println("  Выхода в РФ нет: в режиме main всё, кроме заблокированного, пойдёт напрямую.")
		}
	}
}
