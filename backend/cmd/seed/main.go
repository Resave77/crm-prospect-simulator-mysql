package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"crm-prospect-simulator/backend/config"
	"crm-prospect-simulator/backend/internal/auth/model"
	"crm-prospect-simulator/backend/internal/auth/repository"
	"crm-prospect-simulator/backend/platform/database"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	db, err := database.ConnectMySQL(ctx, cfg.DatabaseURL, database.PoolConfig{
		MaxOpenConns:    cfg.DBMaxOpenConns,
		MaxIdleConns:    cfg.DBMaxIdleConns,
		ConnMaxLifetime: cfg.DBConnMaxLifetime,
		ConnMaxIdleTime: cfg.DBConnMaxIdleTime,
	})
	if err != nil {
		return err
	}
	defer db.Close()
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	repo := repository.NewMySQLRepository(db)
	users := []model.User{
		{ID: uuid.New(), Email: "admin@yummy.test", PasswordHash: string(hash), FullName: "Yummy Administrator", Role: model.RoleAdministrator, Status: model.UserActive},
		{ID: uuid.New(), Email: "sales@yummy.test", PasswordHash: string(hash), FullName: "Nurdin Pratama", Role: model.RoleSalesExecutive, Status: model.UserActive},
		{ID: uuid.New(), Email: "sales2@yummy.test", PasswordHash: string(hash), FullName: "Alicia Ramadhan", Role: model.RoleSalesExecutive, Status: model.UserActive},
		{ID: uuid.New(), Email: "sales3@yummy.test", PasswordHash: string(hash), FullName: "Rizky Ananda", Role: model.RoleSalesExecutive, Status: model.UserActive},
	}
	for _, user := range users {
		if err := repo.UpsertSeed(ctx, user); err != nil {
			return err
		}
		log.Printf("seeded account %s", user.Role)
	}
	// Historical simulator records use these immutable IDs and Google Place IDs.
	// Delete only those records; independently created Prospect Finder data is never matched.
	demoProspectIDs := []uuid.UUID{
		uuid.MustParse("10000000-0000-4000-8000-000000000001"), uuid.MustParse("10000000-0000-4000-8000-000000000002"),
		uuid.MustParse("10000000-0000-4000-8000-000000000003"), uuid.MustParse("10000000-0000-4000-8000-000000000004"),
		uuid.MustParse("10000000-0000-4000-8000-000000000005"), uuid.MustParse("10000000-0000-4000-8000-000000000006"),
		uuid.MustParse("10000000-0000-4000-8000-000000000007"), uuid.MustParse("10000000-0000-4000-8000-000000000008"),
	}
	demoProspectArgs := make([]any, 0, len(demoProspectIDs))
	demoProspectPlaceholders := make([]string, 0, len(demoProspectIDs))
	for _, id := range demoProspectIDs {
		demoProspectArgs = append(demoProspectArgs, id.String())
		demoProspectPlaceholders = append(demoProspectPlaceholders, "?")
	}
	demoProspectList := strings.Join(demoProspectPlaceholders, ",")

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM customer_sites WHERE source_prospect_id IN (`+demoProspectList+`)`, demoProspectArgs...); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM prospect_visits WHERE prospect_id IN (`+demoProspectList+`)`, demoProspectArgs...); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM prospect_status_history WHERE prospect_id IN (`+demoProspectList+`)`, demoProspectArgs...); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM prospects WHERE id IN (`+demoProspectList+`)`, demoProspectArgs...); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM parent_companies WHERE parent_code = 'PC-000900' AND NOT EXISTS (SELECT 1 FROM customer_sites cs WHERE cs.parent_company_id = parent_companies.id)`); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit seed cleanup: %w", err)
	}
	log.Printf("seeded local login accounts; removed legacy simulator business records")
	return nil
}
