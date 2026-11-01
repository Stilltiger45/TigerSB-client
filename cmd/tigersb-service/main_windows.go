package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/Stilltiger45/TigerSB-client/desktop"
)

// root is the service's directory: %ProgramData%\TigerSB.
func root() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, "TigerSB")
}

func run(args []string) error {
	if len(args) == 0 {
		inService, err := svc.IsWindowsService()
		if err != nil {
			return err
		}
		if !inService {
			return errors.New("запустите install, uninstall или console")
		}
		return svc.Run(serviceName, handler{})
	}
	switch args[0] {
	case "install":
		return install()
	case "uninstall":
		return uninstall()
	case "console":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		return desktop.Run(ctx, root(), version, desktop.Windows{})
	}
	return fmt.Errorf("неизвестная команда %q", args[0])
}

type handler struct{}

func (handler) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- desktop.Run(ctx, root(), version, desktop.Windows{}) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done:
			cancel()
			if err != nil {
				return true, 1
			}
			return false, 0
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case <-done:
				case <-time.After(15 * time.Second):
				}
				return false, 0
			}
		}
	}
}

// install registers the service to start with Windows and restart after a
// crash, then starts it. An old registration is replaced.
func install() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	if s, err := m.OpenService(serviceName); err == nil {
		stopService(s)
		s.Delete()
		s.Close()
		// Windows removes a deleted service once its handles are closed.
		for range 50 {
			s, err := m.OpenService(serviceName)
			if err != nil {
				break
			}
			s.Close()
			time.Sleep(100 * time.Millisecond)
		}
	}
	s, err := m.CreateService(serviceName, exe, mgr.Config{
		DisplayName: "TigerSB VPN",
		Description: "Держит VPN TigerSB: ядро и сетевой адаптер. Приложением TigerSB управляют им.",
		StartType:   mgr.StartAutomatic,
	})
	if err != nil {
		return err
	}
	defer s.Close()
	s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 2 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}, 24*60*60)
	return s.Start()
}

func uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return nil // nothing to remove
	}
	defer s.Close()
	stopService(s)
	return s.Delete()
}

func stopService(s *mgr.Service) {
	st, err := s.Control(svc.Stop)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return
		}
	}
	for i := 0; i < 100 && st.State != svc.Stopped; i++ {
		time.Sleep(100 * time.Millisecond)
		if st, err = s.Query(); err != nil {
			return
		}
	}
}
