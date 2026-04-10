package main

import (
	"log/slog"
	"os"

	"github.com/yay101/mediarr/internal/config"
	"github.com/yay101/mediarr/internal/db"
	"github.com/yay101/mediarr/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	database, err := db.New(cfg.Database.Path)
	if err != nil {
		slog.Error("failed to create database", "error", err)
		os.Exit(1)
	}

	stopped := make(chan struct{})

	app := &server.App{
		Config:  func() *config.Config { return cfg },
		DB:      func() *db.Database { return database },
		Stopped: func() <-chan struct{} { return stopped },
	}

	srv := server.New(app)
	if err := srv.Start(); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
