// Command walletflow runs the local WalletFlow app and opens it in the browser.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/dionisvl/walletflow/internal/config"
	"github.com/dionisvl/walletflow/internal/demo"
	"github.com/dionisvl/walletflow/internal/store"
	"github.com/dionisvl/walletflow/internal/web"
)

// version is set at release build time.
var version = "dev"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	dataDir, err := config.DataDir()
	if err != nil {
		return err
	}
	// .env in the working directory wins over the one in the data dir.
	cfg, err := config.Load(".env", filepath.Join(dataDir, ".env"))
	if err != nil {
		return err
	}
	flag.StringVar(&cfg.Addr, "addr", cfg.Addr, "listen address (WALLETFLOW_ADDR)")
	flag.StringVar(&cfg.DBPath, "db", cfg.DBPath, "database file (WALLETFLOW_DB, default: data dir)")
	noBrowser := flag.Bool("no-browser", false, "do not open the browser")
	flag.BoolVar(&cfg.Demo, "demo", false, "start with made-up sample data in a temporary database")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("walletflow", version)
		return nil
	}

	if cfg.Demo {
		cfg.DBPath = filepath.Join(os.TempDir(), "walletflow-demo.db")
		for _, suffix := range []string{"", "-wal", "-shm"} {
			os.Remove(cfg.DBPath + suffix)
		}
	}
	if cfg.DBPath == "" {
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			return err
		}
		cfg.DBPath = filepath.Join(dataDir, "walletflow.db")
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	if cfg.Demo {
		if err := demo.Seed(context.Background(), st, time.Now()); err != nil {
			return fmt.Errorf("demo: %w", err)
		}
		log.Print("demo mode: sample data, sync is off")
	}
	if n, err := st.PurgeOrphans(context.Background()); err != nil {
		return err
	} else if n > 0 {
		log.Printf("removed %d transfers of addresses that are no longer yours", n)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv, err := web.New(ctx, st, cfg)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}

	url := "http://" + ln.Addr().String()
	log.Printf("WalletFlow: %s (db %s)", url, cfg.DBPath)
	if cfg.EnvFile != "" {
		log.Printf("config: %s", cfg.EnvFile)
	}
	if !*noBrowser {
		openBrowser(url)
	}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hs.Shutdown(shutdown)
	}()
	if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("open browser: %v", err)
	}
}
