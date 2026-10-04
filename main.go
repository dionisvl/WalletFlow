// WalletFlow: local ledger of your crypto wallets.
package main

import (
	"context"
	"errors"
	"flag"
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
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	dbPath := flag.String("db", "", "database file (default: user config dir)")
	noBrowser := flag.Bool("no-browser", false, "do not open the browser")
	flag.Parse()

	if *dbPath == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			log.Fatal(err)
		}
		dir = filepath.Join(dir, "WalletFlow")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			log.Fatal(err)
		}
		*dbPath = filepath.Join(dir, "walletflow.db")
	}

	st, err := openStore(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := newApp(ctx, st, *dbPath)
	if err != nil {
		log.Fatal(err)
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Handler: app.routes(), ReadHeaderTimeout: 10 * time.Second}

	url := "http://" + ln.Addr().String()
	log.Printf("WalletFlow: %s (db %s)", url, *dbPath)
	if !*noBrowser {
		openBrowser(url)
	}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
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
