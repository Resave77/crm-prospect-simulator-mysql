package bootstrap

import (
	"context"
	"database/sql"
	"fmt"

	"crm-prospect-simulator/backend/config"
	"crm-prospect-simulator/backend/internal/auth/repository"
	"crm-prospect-simulator/backend/internal/auth/service"
	customerrepository "crm-prospect-simulator/backend/internal/customer/repository"
	customerservice "crm-prospect-simulator/backend/internal/customer/service"
	prospectrepository "crm-prospect-simulator/backend/internal/prospect/repository"
	prospectservice "crm-prospect-simulator/backend/internal/prospect/service"
	"crm-prospect-simulator/backend/platform/database"
	"crm-prospect-simulator/backend/server"
	"github.com/gofiber/fiber/v2"
)

type Application struct {
	Fiber *fiber.App
	DB    *sql.DB
}

func Build(ctx context.Context) (*Application, config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, config.Config{}, fmt.Errorf("load configuration: %w", err)
	}
	db, err := database.ConnectMySQL(ctx, cfg.DatabaseURL, database.PoolConfig{
		MaxOpenConns:    cfg.DBMaxOpenConns,
		MaxIdleConns:    cfg.DBMaxIdleConns,
		ConnMaxLifetime: cfg.DBConnMaxLifetime,
		ConnMaxIdleTime: cfg.DBConnMaxIdleTime,
	})
	if err != nil {
		return nil, config.Config{}, err
	}
	repo := repository.NewMySQLRepository(db)
	tokens := service.NewTokenManager(cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTAudience, cfg.AccessTokenTTL)
	authService := service.NewAuthService(repo, repo, tokens, cfg.RefreshTokenTTL)
	prospectRepo := prospectrepository.NewMySQLRepository(db)
	placesClient := prospectservice.NewGooglePlacesClient(cfg.GoogleMapsAPIKey)
	prospectService := prospectservice.New(prospectRepo, placesClient)
	customerRepo := customerrepository.NewMySQLRepository(db)
	customerService := customerservice.New(customerRepo, prospectService)
	return &Application{Fiber: server.New(cfg, authService, prospectService, customerService), DB: db}, cfg, nil
}
