package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"mtproxy-control/apps/api/internal/database"
	"mtproxy-control/apps/api/internal/healthchecks"
	"mtproxy-control/apps/api/internal/httpserver"
	"mtproxy-control/apps/api/internal/inventory"
	"mtproxy-control/apps/api/internal/serverevents"
	"mtproxy-control/apps/api/internal/sshcredentials"
	"mtproxy-control/apps/api/internal/sshlayer"
	"mtproxy-control/apps/api/internal/telegramalerts"
	"mtproxy-control/apps/api/internal/telemtconfig"
)

func main() {
	db := mustOpenDatabase()
	defer db.Close()

	sshService := sshlayer.NewTester()
	healthInterval := mustHealthcheckInterval()
	startHealthScheduler(db, healthInterval, sshService)

	addr := listenAddr()
	server := &http.Server{
		Addr: addr,
		Handler: httpserver.NewWithOptions(
			db,
			httpserver.WithHealthCheckInterval(healthInterval),
			httpserver.WithSSHTester(sshService),
			httpserver.WithSSHExecutor(sshService),
		).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("mtproxy-control api listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("api server stopped: %v", err)
	}
}

func mustOpenDatabase() *database.DB {
	path := strings.TrimSpace(os.Getenv("DATABASE_PATH"))
	if path == "" {
		path = "./data/panel.db"
	}

	db, err := database.Open(path)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}

	return db
}

func listenAddr() string {
	if value := strings.TrimSpace(os.Getenv("API_ADDR")); value != "" {
		return value
	}
	if port := strings.TrimSpace(os.Getenv("API_PORT")); port != "" {
		if strings.HasPrefix(port, ":") {
			return port
		}
		return ":" + port
	}
	return ":8080"
}

func mustHealthcheckInterval() time.Duration {
	interval, err := healthchecks.ParseInterval(strings.TrimSpace(os.Getenv("HEALTHCHECK_INTERVAL")))
	if err != nil {
		log.Fatalf("parse HEALTHCHECK_INTERVAL: %v", err)
	}
	return interval
}

func startHealthScheduler(db *database.DB, interval time.Duration, sshService *sshlayer.Service) {
	service := healthchecks.NewService(
		inventory.NewRepository(db),
		telemtconfig.NewRepository(db),
		sshcredentials.NewRepository(db),
		healthchecks.NewRepository(db),
		serverevents.NewRepository(db),
		telegramalerts.NewService(telegramalerts.NewRepository(db), nil),
		sshService,
	)

	go func() {
		run := func() {
			if err := service.RunCycle(context.Background()); err != nil {
				log.Printf("health scheduler cycle: %v", err)
			}
		}

		run()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			run()
		}
	}()
}
