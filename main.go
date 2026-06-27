package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"sekailink/server/internal/client"
	"sekailink/server/internal/config"
)

func main() {
	log.SetFlags(log.Ltime | log.Lshortfile)

	cfgPath := flag.String("config", "", "path to config file")
	relay := flag.String("relay", "wss://sekailink.vercel.app", "relay server address")
	urlProvider := flag.String("url-provider", "", "URL provider (from web dashboard)")
	apiKey := flag.String("api-key", "", "API key (from web dashboard)")
	localHost := flag.String("local-host", "localhost", "local host to proxy to")
	localPort := flag.Int("local-port", 3000, "local port to proxy to")
	allowExternalHost := flag.Bool("allow-external-host", false, "allow proxying to non-loopback addresses")
	maxBodySize := flag.Int("max-body-size", config.DefaultMaxBodySizeMB, "max response body size in MB (1-100)")
	flag.Parse()

	if *cfgPath == "" {
		defaultPath, err := config.DefaultConfigPath()
		if err != nil {
			log.Fatalf("cannot determine default config path: %v", err)
		}
		cfgPath = &defaultPath
	}

	cfg := &config.Config{
		Relay:             *relay,
		LocalHost:         *localHost,
		LocalPort:         *localPort,
		URLProvider:       *urlProvider,
		APIKey:            *apiKey,
		AllowExternalHost: *allowExternalHost,
		MaxBodySizeMB:     *maxBodySize,
	}

	if existing, err := config.LoadFile(*cfgPath); err == nil {
		cfg = existing
	} else if !os.IsNotExist(err) {
		log.Printf("warning: cannot read config %s: %v", *cfgPath, err)
	}

	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "relay":
			cfg.Relay = f.Value.String()
		case "url-provider":
			cfg.URLProvider = f.Value.String()
		case "api-key":
			cfg.APIKey = f.Value.String()
		case "local-host":
			cfg.LocalHost = f.Value.String()
		case "local-port":
			if v, err := strconv.Atoi(f.Value.String()); err == nil {
				cfg.LocalPort = v
			}
		case "allow-external-host":
			cfg.AllowExternalHost = true
		case "max-body-size":
			if v, err := strconv.Atoi(f.Value.String()); err == nil {
				cfg.MaxBodySizeMB = v
			}
		}
	})

	// SEKAILINK_API_KEY env var tiene prioridad sobre flag y config file
	envKey := os.Getenv("SEKAILINK_API_KEY")
	keyFromEnv := envKey != ""
	if keyFromEnv {
		cfg.APIKey = envKey
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	// No guardar la api_key en disco si viene de variable de entorno
	if keyFromEnv {
		origKey := cfg.APIKey
		cfg.APIKey = ""
		if err := cfg.Save(*cfgPath); err != nil {
			log.Printf("warning: cannot save config: %v", err)
		}
		cfg.APIKey = origKey
	} else if err := cfg.Save(*cfgPath); err != nil {
		log.Printf("warning: cannot save config: %v", err)
	}

	log.Printf("url_provider: %s", cfg.URLProvider)
	log.Printf("relay: %s", cfg.Relay)
	log.Printf("local: %s:%d", cfg.LocalHost, cfg.LocalPort)

	cl := client.New(cfg)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sig
		log.Println("received signal, shutting down...")
		cl.Stop()
	}()

	fmt.Println()
	log.Println("sekai-server started")
	if err := cl.Run(); err != nil {
		log.Fatalf("client error: %v", err)
	}
	log.Println("sekai-server stopped")
}
