package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"crm-prospect-simulator/backend/internal/prospect/model"
	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

type MySQLRepository struct {
	db *sql.DB
}

func NewMySQLRepository(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

const mysqlProspectSelect = `
	SELECT p.id, p.google_place_id, p.place_name, p.formatted_address,
	       p.latitude, p.longitude, p.place_category, p.place_types,
	       p.industry_group,
	       COALESCE(p.phone_number, ''), COALESCE(p.website_url, ''), COALESCE(p.google_maps_url, ''), p.assigned_sales_executive_id,
	       u.full_name, p.visit_notes, p.follow_up_notes, p.status,
	       p.converted_at, p.created_at, p.updated_at
	FROM prospects p
	JOIN users u ON u.id = p.assigned_sales_executive_id`

func (r *MySQLRepository) ListAssigned(ctx context.Context, salesExecutiveID uuid.UUID) ([]model.Prospect, error) {
	rows, err := r.db.QueryContext(ctx, mysqlProspectSelect+`
		WHERE p.assigned_sales_executive_id = ?
		ORDER BY p.updated_at DESC`, salesExecutiveID.String())
	if err != nil {
		return nil, fmt.Errorf("list assigned prospects: %w", err)
	}
	defer rows.Close()
	return mysqlScanProspects(rows)
}

func (r *MySQLRepository) ListAll(ctx context.Context) ([]model.Prospect, error) {
	rows, err := r.db.QueryContext(ctx, mysqlProspectSelect+` ORDER BY p.updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list prospects: %w", err)
	}
	defer rows.Close()
	return mysqlScanProspects(rows)
}

func (r *MySQLRepository) ListSalesExecutives(ctx context.Context) ([]model.SalesExecutive, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT u.id, u.full_name, COUNT(p.id) AS active_prospect_count
		FROM users u
		LEFT JOIN prospects p ON p.assigned_sales_executive_id = u.id AND p.status IN ('NEW_LEAD','CONTACTED','INTERESTED','QUALIFIED','PROPOSAL_SENT','NEGOTIATION')
		WHERE u.role = 'SALES_EXECUTIVE' AND u.status = 'ACTIVE'
		GROUP BY u.id, u.full_name
		ORDER BY u.full_name`)
	if err != nil {
		return nil, fmt.Errorf("list sales executives: %w", err)
	}
	defer rows.Close()
	items := make([]model.SalesExecutive, 0)
	for rows.Next() {
		var item model.SalesExecutive
		var idStr string
		var count int64
		if err := rows.Scan(&idStr, &item.FullName, &count); err != nil {
			return nil, err
		}
		parsedID, err := uuid.Parse(idStr)
		if err != nil {
			return nil, fmt.Errorf("parse sales executive id from database: %w", err)
		}
		item.ID = parsedID
		item.ActiveProspectCount = int(count)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *MySQLRepository) ListWon(ctx context.Context) ([]model.Prospect, error) {
	rows, err := r.db.QueryContext(ctx, mysqlProspectSelect+`
		WHERE p.status = 'WON' ORDER BY p.updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list won prospects: %w", err)
	}
	defer rows.Close()
	return mysqlScanProspects(rows)
}

func (r *MySQLRepository) FindReview(ctx context.Context, id uuid.UUID) (model.Review, error) {
	prospect, err := mysqlScanProspect(r.db.QueryRowContext(ctx, mysqlProspectSelect+` WHERE p.id = ?`, id.String()))
	if err != nil {
		return model.Review{}, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT h.id, h.from_status, h.to_status, h.changed_by_user_id,
		       u.full_name, h.notes, h.created_at
		FROM prospect_status_history h
		JOIN users u ON u.id = h.changed_by_user_id
		WHERE h.prospect_id = ? ORDER BY h.created_at`, id.String())
	if err != nil {
		return model.Review{}, fmt.Errorf("list prospect history: %w", err)
	}
	defer rows.Close()
	history := make([]model.StatusHistory, 0)
	for rows.Next() {
		var item model.StatusHistory
		var idStr, changedByIDStr string
		var from sql.NullString
		if err := rows.Scan(&idStr, &from, &item.ToStatus, &changedByIDStr, &item.ChangedByName, &item.Notes, &item.CreatedAt); err != nil {
			return model.Review{}, fmt.Errorf("scan prospect history: %w", err)
		}
		parsedID, err := uuid.Parse(idStr)
		if err != nil {
			return model.Review{}, fmt.Errorf("parse history id from database: %w", err)
		}
		parsedChangedBy, err := uuid.Parse(changedByIDStr)
		if err != nil {
			return model.Review{}, fmt.Errorf("parse history changed_by_user_id from database: %w", err)
		}
		item.ID = parsedID
		item.ChangedByUserID = parsedChangedBy
		if from.Valid {
			status := model.Status(from.String)
			item.FromStatus = &status
		}
		history = append(history, item)
	}
	if err := rows.Err(); err != nil {
		return model.Review{}, err
	}
	visitRows, err := r.db.QueryContext(ctx, `
		SELECT v.id, v.prospect_id, v.sales_executive_id, u.full_name,
		       v.check_in_at, v.check_in_latitude, v.check_in_longitude,
		       v.check_out_at, v.check_out_latitude, v.check_out_longitude,
		       v.selfie_reference, v.visit_notes, v.follow_up_notes
		FROM prospect_visits v JOIN users u ON u.id = v.sales_executive_id
		WHERE v.prospect_id = ? ORDER BY v.check_in_at DESC`, id.String())
	if err != nil {
		return model.Review{}, fmt.Errorf("list prospect visits: %w", err)
	}
	defer visitRows.Close()
	visits := make([]model.Visit, 0)
	for visitRows.Next() {
		item, scanErr := mysqlScanVisit(visitRows)
		if scanErr != nil {
			return model.Review{}, scanErr
		}
		visits = append(visits, item)
	}
	return model.Review{Prospect: prospect, History: history, Visits: visits}, visitRows.Err()
}

func (r *MySQLRepository) Transition(ctx context.Context, id, salesExecutiveID uuid.UUID, expected, status model.Status, notes string) (model.Prospect, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return model.Prospect{}, fmt.Errorf("begin prospect decision: %w", err)
	}
	defer tx.Rollback()

	var current model.Status
	var ownerStr string
	err = tx.QueryRowContext(ctx, `SELECT status, assigned_sales_executive_id FROM prospects WHERE id = ? FOR UPDATE`, id.String()).Scan(&current, &ownerStr)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Prospect{}, ErrNotFound
	}
	if err != nil {
		return model.Prospect{}, fmt.Errorf("lock prospect decision: %w", err)
	}
	owner, err := uuid.Parse(ownerStr)
	if err != nil {
		return model.Prospect{}, fmt.Errorf("parse prospect owner from database: %w", err)
	}
	if owner != salesExecutiveID {
		return model.Prospect{}, ErrNotOwner
	}
	if current != expected {
		return model.Prospect{}, ErrInvalidStatus
	}
	if _, err = tx.ExecContext(ctx, `UPDATE prospects SET status = ?, updated_at = UTC_TIMESTAMP(6) WHERE id = ?`, string(status), id.String()); err != nil {
		return model.Prospect{}, fmt.Errorf("update prospect decision: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO prospect_status_history
			(id, prospect_id, from_status, to_status, changed_by_user_id, notes)
		VALUES (?, ?, ?, ?, ?, ?)`, uuid.New().String(), id.String(), string(current), string(status), salesExecutiveID.String(), notes); err != nil {
		return model.Prospect{}, fmt.Errorf("record prospect decision: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return model.Prospect{}, fmt.Errorf("commit prospect decision: %w", err)
	}
	return mysqlScanProspect(r.db.QueryRowContext(ctx, mysqlProspectSelect+` WHERE p.id = ?`, id.String()))
}

func (r *MySQLRepository) Create(ctx context.Context, input model.SaveProspectInput, administratorID uuid.UUID) (model.Prospect, error) {
	placeTypes, err := json.Marshal(input.Place.PlaceTypes)
	if err != nil {
		return model.Prospect{}, fmt.Errorf("encode place types: %w", err)
	}
	id := uuid.New()

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return model.Prospect{}, fmt.Errorf("begin create prospect: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO prospects (id, google_place_id, place_name, formatted_address, latitude, longitude,
			place_category, industry_group, place_types, phone_number, website_url, google_maps_url, assigned_sales_executive_id, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'NEW_LEAD')`,
		id.String(), input.Place.GooglePlaceID, input.Place.PlaceName, input.Place.FormattedAddress,
		input.Place.Latitude, input.Place.Longitude, input.Place.PlaceCategory, input.IndustryGroup,
		placeTypes, input.Place.PhoneNumber, input.Place.WebsiteURL, input.Place.GoogleMapsURL,
		input.AssignedSalesExecutiveID.String())
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return model.Prospect{}, ErrDuplicate
		}
		return model.Prospect{}, fmt.Errorf("create prospect: %w", err)
	}

	historyID := uuid.New()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO prospect_status_history (id, prospect_id, from_status, to_status, changed_by_user_id, notes)
		VALUES (?, ?, NULL, 'NEW_LEAD', ?, 'Saved from Prospect Finder and assigned')`,
		historyID.String(), id.String(), administratorID.String())
	if err != nil {
		return model.Prospect{}, fmt.Errorf("record prospect creation history: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return model.Prospect{}, fmt.Errorf("commit create prospect: %w", err)
	}
	return mysqlScanProspect(r.db.QueryRowContext(ctx, mysqlProspectSelect+` WHERE p.id = ?`, id.String()))
}

func (r *MySQLRepository) CheckIn(ctx context.Context, prospectID, salesExecutiveID uuid.UUID, input model.CheckInInput) (model.Visit, error) {
	selfie := input.SelfieReference
	id := uuid.New()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO prospect_visits (id, prospect_id, sales_executive_id, check_in_at, check_in_latitude, check_in_longitude, selfie_reference, visit_notes)
		SELECT ?, p.id, ?, UTC_TIMESTAMP(6), ?, ?, ?, ? FROM prospects p
		WHERE p.id = ? AND p.assigned_sales_executive_id = ? AND p.status NOT IN ('LOST','CONVERTED')`,
		id.String(), salesExecutiveID.String(), input.Latitude, input.Longitude, selfie, input.VisitNotes,
		prospectID.String(), salesExecutiveID.String())
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return model.Visit{}, ErrVisitOpen
		}
		return model.Visit{}, fmt.Errorf("check in prospect visit: %w", err)
	}
	var exists bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM prospect_visits WHERE id = ?)`, id.String()).Scan(&exists); err != nil {
		return model.Visit{}, err
	}
	if !exists {
		return model.Visit{}, ErrNotOwner
	}
	if input.VisitNotes != "" {
		_, _ = r.db.ExecContext(ctx, `UPDATE prospects SET visit_notes = ?, updated_at = UTC_TIMESTAMP(6) WHERE id = ?`, input.VisitNotes, prospectID.String())
	}
	return r.findVisit(ctx, id)
}

func (r *MySQLRepository) CheckOut(ctx context.Context, prospectID, visitID, salesExecutiveID uuid.UUID, input model.CheckOutInput) (model.Visit, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE prospect_visits v
		JOIN prospects p ON p.id = v.prospect_id
		SET v.check_out_at = UTC_TIMESTAMP(6),
		    v.check_out_latitude = ?,
		    v.check_out_longitude = ?,
		    v.follow_up_notes = ?,
		    v.updated_at = UTC_TIMESTAMP(6)
		WHERE v.id = ? AND v.prospect_id = ? AND v.sales_executive_id = ? AND p.assigned_sales_executive_id = ? AND v.check_out_at IS NULL`,
		input.Latitude, input.Longitude, input.FollowUpNotes,
		visitID.String(), prospectID.String(), salesExecutiveID.String(), salesExecutiveID.String())
	if err != nil {
		return model.Visit{}, fmt.Errorf("check out prospect visit: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.Visit{}, fmt.Errorf("check out affected rows: %w", err)
	}
	if affected != 1 {
		var exists bool
		_ = r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM prospect_visits WHERE id = ? AND prospect_id = ?)`, visitID.String(), prospectID.String()).Scan(&exists)
		if exists {
			return model.Visit{}, ErrVisitClosed
		}
		return model.Visit{}, ErrNotOwner
	}
	if input.FollowUpNotes != "" {
		_, _ = r.db.ExecContext(ctx, `UPDATE prospects SET follow_up_notes = ?, updated_at = UTC_TIMESTAMP(6) WHERE id = ?`, input.FollowUpNotes, prospectID.String())
	}
	return r.findVisit(ctx, visitID)
}

func (r *MySQLRepository) ListVisitMonitoring(ctx context.Context, filter model.VisitMonitoringFilter) ([]model.VisitMonitoringItem, error) {
	const baseQuery = `
		SELECT v.id, v.prospect_id, p.place_name, p.place_category,
		       COALESCE(p.industry_group, ''),
		       COALESCE(p.formatted_address, ''),
		       COALESCE(p.phone_number, ''),
		       p.latitude, p.longitude,
		       v.sales_executive_id, u.full_name,
		       v.check_in_at, v.check_out_at,
		       v.check_in_latitude, v.check_in_longitude,
		       v.check_out_latitude, v.check_out_longitude,
		       CASE WHEN p.latitude IS NOT NULL AND p.longitude IS NOT NULL THEN
		           (2 * 6371000 * ASIN(SQRT(
		             POWER(SIN(RADIANS(v.check_in_latitude - p.latitude) / 2), 2) +
		             COS(RADIANS(p.latitude)) * COS(RADIANS(v.check_in_latitude)) *
		             POWER(SIN(RADIANS(v.check_in_longitude - p.longitude) / 2), 2)
		           )))
		       ELSE 0 END AS distance_meters,
		       CASE WHEN v.check_out_at IS NOT NULL THEN
		           TIMESTAMPDIFF(MICROSECOND, v.check_in_at, v.check_out_at) / 1000000.0
		       ELSE NULL END AS duration_seconds,
		       CASE WHEN p.latitude IS NOT NULL AND p.longitude IS NOT NULL THEN
		           CASE WHEN (2 * 6371000 * ASIN(SQRT(
		             POWER(SIN(RADIANS(v.check_in_latitude - p.latitude) / 2), 2) +
		             COS(RADIANS(p.latitude)) * COS(RADIANS(v.check_in_latitude)) *
		             POWER(SIN(RADIANS(v.check_in_longitude - p.longitude) / 2), 2)
		           ))) <= 100 THEN 'INSIDE' ELSE 'OUTSIDE' END
		       ELSE 'UNKNOWN' END AS radius_status,
		       p.status AS prospect_status,
		       v.selfie_reference, v.visit_notes, v.follow_up_notes,
		       (SELECT COUNT(*) FROM prospect_visits pv WHERE pv.prospect_id = v.prospect_id) AS visit_count
		FROM prospect_visits v
		JOIN users u ON u.id = v.sales_executive_id
		JOIN prospects p ON p.id = v.prospect_id`

	whereClauses := make([]string, 0)
	args := make([]any, 0)

	if filter.DateFrom != "" {
		whereClauses = append(whereClauses, "DATE(v.check_in_at) >= ?")
		args = append(args, filter.DateFrom)
	}
	if filter.DateTo != "" {
		whereClauses = append(whereClauses, "DATE(v.check_in_at) <= ?")
		args = append(args, filter.DateTo)
	}
	if filter.SalesExecutiveID != "" {
		whereClauses = append(whereClauses, "v.sales_executive_id = ?")
		args = append(args, filter.SalesExecutiveID)
	}
	if filter.CustomerName != "" {
		whereClauses = append(whereClauses, "p.place_name LIKE ?")
		args = append(args, "%"+filter.CustomerName+"%")
	}

	query := baseQuery
	if len(whereClauses) > 0 {
		query += " WHERE "
		for i, clause := range whereClauses {
			if i > 0 {
				query += " AND "
			}
			query += clause
		}
	}
	query += " ORDER BY v.check_in_at DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list visit monitoring: %w", err)
	}
	defer rows.Close()

	items := make([]model.VisitMonitoringItem, 0)
	for rows.Next() {
		var item model.VisitMonitoringItem
		var idStr, prospectIDStr, salesExecIDStr string
		var prospectLat, prospectLon sql.NullFloat64
		var checkOutAt sql.NullTime
		var checkOutLat, checkOutLon sql.NullFloat64
		var distanceMeters float64
		var durationSeconds sql.NullFloat64
		var visitCount int64
		if err := rows.Scan(
			&idStr, &prospectIDStr, &item.CustomerName, &item.CustomerCategory,
			&item.IndustryGroup, &item.FormattedAddress, &item.PhoneNumber,
			&prospectLat, &prospectLon,
			&salesExecIDStr, &item.SalesExecutiveName,
			&item.CheckInAt, &checkOutAt,
			&item.CheckInLatitude, &item.CheckInLongitude,
			&checkOutLat, &checkOutLon,
			&distanceMeters, &durationSeconds, &item.RadiusStatus,
			&item.ProspectStatus, &item.SelfieReference, &item.VisitNotes, &item.FollowUpNotes,
			&visitCount,
		); err != nil {
			return nil, fmt.Errorf("scan visit monitoring: %w", err)
		}
		parsedID, err := uuid.Parse(idStr)
		if err != nil {
			return nil, fmt.Errorf("parse visit id from database: %w", err)
		}
		parsedProspectID, err := uuid.Parse(prospectIDStr)
		if err != nil {
			return nil, fmt.Errorf("parse visit prospect id from database: %w", err)
		}
		parsedSalesExecID, err := uuid.Parse(salesExecIDStr)
		if err != nil {
			return nil, fmt.Errorf("parse visit sales executive id from database: %w", err)
		}
		item.ID = parsedID
		item.ProspectID = parsedProspectID
		item.SalesExecutiveID = parsedSalesExecID
		if prospectLat.Valid {
			item.ProspectLatitude = &prospectLat.Float64
		}
		if prospectLon.Valid {
			item.ProspectLongitude = &prospectLon.Float64
		}
		if checkOutAt.Valid {
			item.CheckOutAt = &checkOutAt.Time
		}
		if checkOutLat.Valid {
			item.CheckOutLatitude = &checkOutLat.Float64
		}
		if checkOutLon.Valid {
			item.CheckOutLongitude = &checkOutLon.Float64
		}
		item.DistanceMeters = distanceMeters
		if durationSeconds.Valid {
			item.DurationSeconds = &durationSeconds.Float64
		}
		item.VisitCount = int(visitCount)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if filter.RadiusStatus != "" && filter.RadiusStatus != "ALL" {
		filtered := make([]model.VisitMonitoringItem, 0)
		for _, item := range items {
			if item.RadiusStatus == filter.RadiusStatus {
				filtered = append(filtered, item)
			}
		}
		return filtered, nil
	}

	return items, nil
}

func (r *MySQLRepository) DeleteVisit(ctx context.Context, visitID uuid.UUID, adminID uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM prospect_visits WHERE id = ?`, visitID.String())
	if err != nil {
		return fmt.Errorf("delete visit: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete visit affected rows: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *MySQLRepository) findVisit(ctx context.Context, id uuid.UUID) (model.Visit, error) {
	return mysqlScanVisit(r.db.QueryRowContext(ctx, `SELECT v.id,v.prospect_id,v.sales_executive_id,u.full_name,v.check_in_at,v.check_in_latitude,v.check_in_longitude,v.check_out_at,v.check_out_latitude,v.check_out_longitude,v.selfie_reference,v.visit_notes,v.follow_up_notes FROM prospect_visits v JOIN users u ON u.id=v.sales_executive_id WHERE v.id=?`, id.String()))
}

type mysqlRowScanner interface {
	Scan(...any) error
}

func mysqlScanVisit(row mysqlRowScanner) (model.Visit, error) {
	var item model.Visit
	var idStr, prospectIDStr, salesExecIDStr string
	var checkOutAt sql.NullTime
	var checkOutLat, checkOutLon sql.NullFloat64
	err := row.Scan(&idStr, &prospectIDStr, &salesExecIDStr, &item.SalesExecutiveName,
		&item.CheckInAt, &item.CheckInLatitude, &item.CheckInLongitude,
		&checkOutAt, &checkOutLat, &checkOutLon,
		&item.SelfieReference, &item.VisitNotes, &item.FollowUpNotes)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Visit{}, ErrNotFound
	}
	if err != nil {
		return model.Visit{}, fmt.Errorf("scan prospect visit: %w", err)
	}
	parsedID, err := uuid.Parse(idStr)
	if err != nil {
		return model.Visit{}, fmt.Errorf("parse visit id from database: %w", err)
	}
	parsedProspectID, err := uuid.Parse(prospectIDStr)
	if err != nil {
		return model.Visit{}, fmt.Errorf("parse visit prospect id from database: %w", err)
	}
	parsedSalesExecID, err := uuid.Parse(salesExecIDStr)
	if err != nil {
		return model.Visit{}, fmt.Errorf("parse visit sales executive id from database: %w", err)
	}
	item.ID = parsedID
	item.ProspectID = parsedProspectID
	item.SalesExecutiveID = parsedSalesExecID
	if checkOutAt.Valid {
		item.CheckOutAt = &checkOutAt.Time
	}
	if checkOutLat.Valid {
		item.CheckOutLatitude = &checkOutLat.Float64
	}
	if checkOutLon.Valid {
		item.CheckOutLongitude = &checkOutLon.Float64
	}
	return item, nil
}

func mysqlScanProspects(rows *sql.Rows) ([]model.Prospect, error) {
	items := make([]model.Prospect, 0)
	for rows.Next() {
		item, err := mysqlScanProspectRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func mysqlScanProspectRow(row *sql.Rows) (model.Prospect, error) {
	var item model.Prospect
	var idStr, assignedIDStr string
	var placeTypes []byte
	var latitude, longitude sql.NullFloat64
	var convertedAt sql.NullTime
	err := row.Scan(&idStr, &item.GooglePlaceID, &item.PlaceName, &item.FormattedAddress,
		&latitude, &longitude, &item.PlaceCategory, &placeTypes, &item.IndustryGroup,
		&item.PhoneNumber, &item.WebsiteURL, &item.GoogleMapsURL, &assignedIDStr, &item.AssignedSalesExecutive,
		&item.VisitNotes, &item.FollowUpNotes, &item.Status, &convertedAt,
		&item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return model.Prospect{}, fmt.Errorf("scan prospect: %w", err)
	}
	parsedID, err := uuid.Parse(idStr)
	if err != nil {
		return model.Prospect{}, fmt.Errorf("parse prospect id from database: %w", err)
	}
	parsedAssignedID, err := uuid.Parse(assignedIDStr)
	if err != nil {
		return model.Prospect{}, fmt.Errorf("parse prospect assigned sales executive id from database: %w", err)
	}
	item.ID = parsedID
	item.AssignedSalesExecutiveID = parsedAssignedID
	if latitude.Valid {
		item.Latitude = &latitude.Float64
	}
	if longitude.Valid {
		item.Longitude = &longitude.Float64
	}
	if convertedAt.Valid {
		item.ConvertedAt = &convertedAt.Time
	}
	if err := json.Unmarshal(placeTypes, &item.PlaceTypes); err != nil {
		return model.Prospect{}, fmt.Errorf("decode prospect place types: %w", err)
	}
	return item, nil
}

func mysqlScanProspect(row *sql.Row) (model.Prospect, error) {
	var item model.Prospect
	var idStr, assignedIDStr string
	var placeTypes []byte
	var latitude, longitude sql.NullFloat64
	var convertedAt sql.NullTime
	err := row.Scan(&idStr, &item.GooglePlaceID, &item.PlaceName, &item.FormattedAddress,
		&latitude, &longitude, &item.PlaceCategory, &placeTypes, &item.IndustryGroup,
		&item.PhoneNumber, &item.WebsiteURL, &item.GoogleMapsURL, &assignedIDStr, &item.AssignedSalesExecutive,
		&item.VisitNotes, &item.FollowUpNotes, &item.Status, &convertedAt,
		&item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Prospect{}, ErrNotFound
	}
	if err != nil {
		return model.Prospect{}, fmt.Errorf("scan prospect: %w", err)
	}
	parsedID, err := uuid.Parse(idStr)
	if err != nil {
		return model.Prospect{}, fmt.Errorf("parse prospect id from database: %w", err)
	}
	parsedAssignedID, err := uuid.Parse(assignedIDStr)
	if err != nil {
		return model.Prospect{}, fmt.Errorf("parse prospect assigned sales executive id from database: %w", err)
	}
	item.ID = parsedID
	item.AssignedSalesExecutiveID = parsedAssignedID
	if latitude.Valid {
		item.Latitude = &latitude.Float64
	}
	if longitude.Valid {
		item.Longitude = &longitude.Float64
	}
	if convertedAt.Valid {
		item.ConvertedAt = &convertedAt.Time
	}
	if err := json.Unmarshal(placeTypes, &item.PlaceTypes); err != nil {
		return model.Prospect{}, fmt.Errorf("decode prospect place types: %w", err)
	}
	return item, nil
}
