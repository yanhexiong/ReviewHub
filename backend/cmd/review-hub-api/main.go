package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/yanhexiong/review-hub/backend/internal/config"
	"github.com/yanhexiong/review-hub/backend/internal/httpapi"
	"github.com/yanhexiong/review-hub/backend/internal/importer"
	"github.com/yanhexiong/review-hub/backend/internal/realtime"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
	"github.com/yanhexiong/review-hub/backend/internal/service"
	"github.com/yanhexiong/review-hub/backend/internal/update"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("go api stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	store, err := repository.Open(configuration.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.SeedPlatformDefaults(context.Background(), repository.PlatformDefaults{
		MaxPDFBytes:        configuration.MaxPDFBytes,
		MaxImportBytes:     configuration.MaxImportBytes,
		MaxProjectsPerUser: configuration.MaxProjectsPerUser,
		MaxUsers:           configuration.MaxUsers,
		AllowedRoots:       configuration.DefaultAllowedRoots,
	}); err != nil {
		return err
	}

	hub := realtime.NewHub()
	auth := service.NewAuthService(store, configuration.SessionSecret, configuration.Production, configuration.MaxUsers)
	review := service.NewReviewService(store, hub)
	api := httpapi.New(store, auth, review, logger)
	api.SetEncryptionKey(configuration.EncryptionKey)
	api.SetDataDirectory(configuration.DataDirectory)
	api.SetListenerWriter(configuration.SaveFrontendListener)
	api.SetImporter(importer.New(configuration.DataDirectory, int64(configuration.MaxImportBytes)))
	staticDirectory := os.Getenv("REVIEW_HUB_STATIC_DIR")
	if staticDirectory != "" {
		api.SetStaticDirectory(staticDirectory)
	}
	defer api.Close()
	api.SetUpdateService(update.New(update.Runtime{
		AppImagePath:  configuration.AppImagePath,
		UpdaterPath:   configuration.UpdaterPath,
		DataDirectory: configuration.DataDirectory,
		ParentPID:     configuration.ParentPID,
	}))
	listenAddress := configuration.ListenAddress
	if staticDirectory != "" {
		listenAddress = net.JoinHostPort(configuration.FrontendListenHost, strconv.Itoa(configuration.FrontendListenPort))
	}
	server := &http.Server{
		Addr:              listenAddress,
		Handler:           api,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       90 * time.Second,
	}

	errorsChannel := make(chan error, 1)
	go func() {
		logger.Info("go api listening", "address", listenAddress, "static", staticDirectory != "")
		errorsChannel <- server.ListenAndServe()
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case signal := <-signals:
		logger.Info("go api shutdown requested", "signal", signal.String())
		context, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return server.Shutdown(context)
	case err := <-errorsChannel:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
