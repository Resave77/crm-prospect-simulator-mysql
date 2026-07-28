package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"crm-prospect-simulator/backend/internal/customer/model"
	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

type MySQLRepository struct {
	db *sql.DB
}

func NewMySQLRepository(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

const mysqlParentSelect = `
	SELECT pc.id, pc.parent_code, pc.name, pc.address_mode, pc.province,
	       pc.district, pc.sub_district, pc.village, pc.latitude, pc.longitude,
	       pc.preview_address, pc.company_contacts, pc.npwp_name,
	       pc.npwp_address, pc.npwp_number, pc.term_of_payment, pc.kam_assignments
	FROM parent_companies pc`

const mysqlCustomerSelect = `
	SELECT cs.id, cs.customer_code, cs.parent_company_id, pc.parent_code, pc.name,
	       cs.source_prospect_id, cs.source_google_place_id, cs.name, cs.segment,
	       cs.category, cs.address_mode, cs.province, cs.district, cs.sub_district,
	       cs.village, cs.latitude, cs.longitude, cs.preview_address, cs.site_contacts,
	       cs.ppn, cs.id_tku_number, cs.nik, cs.shipment_cost, cs.invoice_type,
	       cs.bank_account, cs.bill_to_source, cs.ship_to_source,
	       cs.billing_address_preview, cs.shipping_address_preview,
	       cs.sales_executive_id, u.full_name, cs.sales_assignments,
	       cs.converted_at, cs.updated_at, cs.converted_by_admin_id
	FROM customer_sites cs
	JOIN parent_companies pc ON pc.id = cs.parent_company_id
	JOIN users u ON u.id = cs.sales_executive_id`

func (r *MySQLRepository) SearchParentCompanies(ctx context.Context, search string) ([]model.ParentCompany, error) {
	pattern := "%" + strings.TrimSpace(search) + "%"
	rows, err := r.db.QueryContext(ctx, mysqlParentSelect+`
		WHERE (pc.name LIKE ? OR pc.parent_code LIKE ?)
		ORDER BY pc.name LIMIT 20`, pattern, pattern)
	if err != nil {
		return nil, fmt.Errorf("search parent companies: %w", err)
	}
	defer rows.Close()
	items := make([]model.ParentCompany, 0)
	for rows.Next() {
		item, err := mysqlScanParent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *MySQLRepository) ListActiveSalesExecutives(ctx context.Context) ([]model.UserOption, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, full_name FROM users
		WHERE role = 'SALES_EXECUTIVE' AND status = 'ACTIVE' ORDER BY full_name`)
	if err != nil {
		return nil, fmt.Errorf("list active sales executives: %w", err)
	}
	defer rows.Close()
	items := make([]model.UserOption, 0)
	for rows.Next() {
		var item model.UserOption
		var idStr string
		if err := rows.Scan(&idStr, &item.FullName); err != nil {
			return nil, fmt.Errorf("scan sales executive: %w", err)
		}
		parsedID, err := uuid.Parse(idStr)
		if err != nil {
			return nil, fmt.Errorf("parse sales executive id from database: %w", err)
		}
		item.ID = parsedID
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *MySQLRepository) Convert(ctx context.Context, prospectID, administratorID uuid.UUID, input model.ConversionInput) (model.CustomerSite, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("begin prospect conversion: %w", err)
	}
	defer tx.Rollback()

	var prospectStatus, googlePlaceID string
	err = tx.QueryRowContext(ctx, `
		SELECT status, google_place_id FROM prospects WHERE id = ? FOR UPDATE`,
		prospectID.String()).Scan(&prospectStatus, &googlePlaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return model.CustomerSite{}, ErrNotFound
	}
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("lock prospect for conversion: %w", err)
	}
	if prospectStatus == "CONVERTED" {
		return model.CustomerSite{}, ErrAlreadyConverted
	}
	if prospectStatus != "WON" {
		return model.CustomerSite{}, ErrProspectNotWon
	}

	var duplicate bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM customer_sites WHERE source_prospect_id = ? OR source_google_place_id = ?)`,
		prospectID.String(), googlePlaceID).Scan(&duplicate); err != nil {
		return model.CustomerSite{}, fmt.Errorf("check customer duplicate: %w", err)
	}
	if duplicate {
		return model.CustomerSite{}, ErrDuplicatePlace
	}

	parent, err := r.resolveParentCompany(ctx, tx, input)
	if err != nil {
		return model.CustomerSite{}, err
	}

	var salesName string
	err = tx.QueryRowContext(ctx, `
		SELECT full_name FROM users WHERE id = ? AND role = 'SALES_EXECUTIVE' AND status = 'ACTIVE'`,
		input.SalesExecutiveID.String()).Scan(&salesName)
	if errors.Is(err, sql.ErrNoRows) {
		return model.CustomerSite{}, ErrSalesUnavailable
	}
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("validate sales executive: %w", err)
	}
	for _, assignment := range input.SalesAssignments {
		if assignment.OwnerID == "" {
			continue
		}
		assignmentOwnerID, parseErr := uuid.Parse(assignment.OwnerID)
		if parseErr != nil {
			return model.CustomerSite{}, ErrSalesUnavailable
		}
		var active bool
		if queryErr := tx.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND role = 'SALES_EXECUTIVE' AND status = 'ACTIVE')`,
			assignmentOwnerID.String()).Scan(&active); queryErr != nil {
			return model.CustomerSite{}, fmt.Errorf("validate additional sales assignment: %w", queryErr)
		}
		if !active {
			return model.CustomerSite{}, ErrSalesUnavailable
		}
	}

	customerSequence, err := r.nextCode(ctx, tx, "customer_site_code")
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("generate customer code: %w", err)
	}
	customerCode := mysqlSimulationCustomerCode(parent.ParentCode, customerSequence)

	siteContacts, err := json.Marshal(input.SiteContacts)
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("encode site contacts: %w", err)
	}
	salesAssignments, err := json.Marshal(input.SalesAssignments)
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("encode sales assignments: %w", err)
	}
	convertedAt := time.Now().UTC()
	customerID := uuid.New()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO customer_sites (
			id, customer_code, parent_company_id, source_prospect_id, source_google_place_id,
			name, segment, category, address_mode, province, district, sub_district, village,
			latitude, longitude, preview_address, site_contacts, ppn, id_tku_number, nik,
			shipment_cost, invoice_type, bank_account, bill_to_source, ship_to_source,
			billing_address_preview, shipping_address_preview, sales_executive_id,
			sales_assignments, converted_at, converted_by_admin_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		customerID.String(), customerCode, parent.ID.String(), prospectID.String(), googlePlaceID,
		input.CustomerName, input.CustomerSegment, input.CustomerCategory,
		input.SiteAddress.Mode, input.SiteAddress.Province, input.SiteAddress.District,
		input.SiteAddress.SubDistrict, input.SiteAddress.Village, input.SiteAddress.Latitude,
		input.SiteAddress.Longitude, input.SiteAddress.PreviewAddress, siteContacts,
		input.PPN, input.IDTKUNumber, input.NIK, input.ShipmentCost, input.InvoiceType,
		input.BankAccount, input.BillToSource, input.ShipToSource,
		input.BillingAddressPreview, input.ShippingAddressPreview,
		input.SalesExecutiveID.String(), salesAssignments, convertedAt, administratorID.String())
	if err != nil {
		return model.CustomerSite{}, mysqlMapDatabaseError(err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE prospects SET status = 'CONVERTED', converted_at = ?, updated_at = ?
		WHERE id = ? AND status = 'WON'`, convertedAt, convertedAt, prospectID.String())
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("mark prospect converted: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("check prospect conversion: %w", err)
	}
	if affected != 1 {
		return model.CustomerSite{}, ErrAlreadyConverted
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO prospect_status_history
			(id, prospect_id, from_status, to_status, changed_by_user_id, notes)
		VALUES (?, ?, 'WON', 'CONVERTED', ?, ?)`,
		uuid.New().String(), prospectID.String(), administratorID.String(),
		"Converted to Customer Existing "+customerCode)
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("record conversion history: %w", err)
	}
	result2, err := mysqlScanCustomer(tx.QueryRowContext(ctx, mysqlCustomerSelect+` WHERE cs.id = ?`, customerID.String()))
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("read converted customer: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return model.CustomerSite{}, mysqlMapDatabaseError(err)
	}
	return result2, nil
}

func (r *MySQLRepository) AutoConvert(ctx context.Context, prospectID uuid.UUID) (model.CustomerSite, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("begin auto conversion: %w", err)
	}
	defer tx.Rollback()

	var prospectStatus, googlePlaceID, placeName, formattedAddress, placeCategory string
	var latitude, longitude sql.NullFloat64
	var assignedSalesExecID string
	err = tx.QueryRowContext(ctx, `
		SELECT status, google_place_id, place_name, formatted_address,
		       latitude, longitude, COALESCE(place_category, ''), assigned_sales_executive_id
		FROM prospects WHERE id = ? FOR UPDATE`, prospectID.String()).
		Scan(&prospectStatus, &googlePlaceID, &placeName, &formattedAddress, &latitude, &longitude, &placeCategory, &assignedSalesExecID)
	if errors.Is(err, sql.ErrNoRows) {
		return model.CustomerSite{}, ErrNotFound
	}
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("lock prospect for auto conversion: %w", err)
	}
	if prospectStatus == "CONVERTED" {
		return model.CustomerSite{}, ErrAlreadyConverted
	}
	if prospectStatus != "WON" {
		return model.CustomerSite{}, ErrProspectNotWon
	}

	var duplicate bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM customer_sites WHERE source_prospect_id = ? OR source_google_place_id = ?)`,
		prospectID.String(), googlePlaceID).Scan(&duplicate); err != nil {
		return model.CustomerSite{}, fmt.Errorf("check customer duplicate: %w", err)
	}
	if duplicate {
		return model.CustomerSite{}, ErrDuplicatePlace
	}

	var salesName string
	if err := tx.QueryRowContext(ctx, `
		SELECT full_name FROM users WHERE id = ? AND role = 'SALES_EXECUTIVE' AND status = 'ACTIVE'`,
		assignedSalesExecID).Scan(&salesName); err != nil {
		return model.CustomerSite{}, ErrSalesUnavailable
	}

	parentSequence, err := r.nextCode(ctx, tx, "parent_company_code")
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("generate parent code: %w", err)
	}
	parentCode := mysqlSimulationParentCode(parentSequence)
	parentID := uuid.New()
	emptyContacts, _ := json.Marshal([]model.Contact{})
	emptyKams, _ := json.Marshal([]model.PeriodAssignment{})
	_, err = tx.ExecContext(ctx, `
		INSERT INTO parent_companies (
			id, parent_code, name, address_mode, province, district, sub_district, village,
			latitude, longitude, preview_address, company_contacts, npwp_name,
			npwp_address, npwp_number, term_of_payment, kam_assignments)
		VALUES (?,?,?,'AUTO_CONVERTED','','','','',?,?,?,?,','','','',?)`,
		parentID.String(), parentCode, placeName,
		nullableFloat64Value(latitude), nullableFloat64Value(longitude),
		formattedAddress, emptyContacts, emptyKams)
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("create auto parent company: %w", err)
	}

	customerSequence, err := r.nextCode(ctx, tx, "customer_site_code")
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("generate customer code: %w", err)
	}
	customerCode := mysqlSimulationCustomerCode(parentCode, customerSequence)

	customerID := uuid.New()
	convertedAt := time.Now().UTC()
	siteContacts, _ := json.Marshal([]model.Contact{})
	salesAssignments, _ := json.Marshal([]model.PeriodAssignment{})
	if placeCategory == "" {
		placeCategory = "Uncategorized"
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO customer_sites (
			id, customer_code, parent_company_id, source_prospect_id, source_google_place_id,
			name, segment, category, address_mode, province, district, sub_district, village,
			latitude, longitude, preview_address, site_contacts, ppn, id_tku_number, nik,
			shipment_cost, invoice_type, bank_account, bill_to_source, ship_to_source,
			billing_address_preview, shipping_address_preview, sales_executive_id,
			sales_assignments, converted_at, converted_by_admin_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		customerID.String(), customerCode, parentID.String(), prospectID.String(), googlePlaceID,
		placeName, "General Trade", placeCategory, "AUTO_CONVERTED", "", "", "", "",
		nullableFloat64Value(latitude), nullableFloat64Value(longitude),
		formattedAddress, siteContacts, "", "", "", "", "", "", "",
		"", "",
		assignedSalesExecID, salesAssignments, convertedAt, uuid.Nil.String())
	if err != nil {
		return model.CustomerSite{}, mysqlMapDatabaseError(err)
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE prospects SET status = 'CONVERTED', converted_at = ?, updated_at = ?
		WHERE id = ? AND status = 'WON'`, convertedAt, convertedAt, prospectID.String()); err != nil {
		return model.CustomerSite{}, fmt.Errorf("mark prospect converted: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO prospect_status_history
			(id, prospect_id, from_status, to_status, changed_by_user_id, notes)
		VALUES (?, ?, 'WON', 'CONVERTED', ?, ?)`,
		uuid.New().String(), prospectID.String(), assignedSalesExecID,
		"Auto-converted on WON transition "+customerCode); err != nil {
		return model.CustomerSite{}, fmt.Errorf("record auto conversion history: %w", err)
	}
	result, err := mysqlScanCustomer(tx.QueryRowContext(ctx, mysqlCustomerSelect+` WHERE cs.id = ?`, customerID.String()))
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("read auto converted customer: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return model.CustomerSite{}, mysqlMapDatabaseError(err)
	}
	return result, nil
}

func (r *MySQLRepository) DeleteCustomer(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM customer_sites WHERE id = ?`, id.String())
	if err != nil {
		return fmt.Errorf("delete customer site: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete customer affected rows: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *MySQLRepository) resolveParentCompany(ctx context.Context, tx *sql.Tx, input model.ConversionInput) (model.ParentCompany, error) {
	if input.ParentMethod == model.ParentMethodExisting {
		if input.ExistingParentCompanyID == nil {
			return model.ParentCompany{}, ErrParentUnavailable
		}
		parent, err := mysqlScanParent(tx.QueryRowContext(ctx, mysqlParentSelect+` WHERE pc.id = ? FOR SHARE`, input.ExistingParentCompanyID.String()))
		if errors.Is(err, ErrNotFound) {
			return model.ParentCompany{}, ErrParentUnavailable
		}
		return parent, err
	}

	parentSequence, err := r.nextCode(ctx, tx, "parent_company_code")
	if err != nil {
		return model.ParentCompany{}, fmt.Errorf("generate parent code: %w", err)
	}
	parentCode := mysqlSimulationParentCode(parentSequence)
	contacts, err := json.Marshal(input.CompanyContacts)
	if err != nil {
		return model.ParentCompany{}, fmt.Errorf("encode company contacts: %w", err)
	}
	kams, err := json.Marshal(input.KAMAssignments)
	if err != nil {
		return model.ParentCompany{}, fmt.Errorf("encode KAM assignments: %w", err)
	}
	parentID := uuid.New()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO parent_companies (
			id, parent_code, name, address_mode, province, district, sub_district, village,
			latitude, longitude, preview_address, company_contacts, npwp_name,
			npwp_address, npwp_number, term_of_payment, kam_assignments)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		parentID.String(), parentCode, input.ParentCompanyName, input.CompanyAddress.Mode,
		input.CompanyAddress.Province, input.CompanyAddress.District, input.CompanyAddress.SubDistrict,
		input.CompanyAddress.Village, input.CompanyAddress.Latitude, input.CompanyAddress.Longitude,
		input.CompanyAddress.PreviewAddress, contacts, input.CompanyNPWPName,
		input.CompanyNPWPAddress, input.CompanyNPWPNumber, input.TermOfPayment, kams)
	if err != nil {
		return model.ParentCompany{}, mysqlMapDatabaseError(err)
	}
	return model.ParentCompany{ID: parentID, ParentCode: parentCode, Name: input.ParentCompanyName}, nil
}

func (r *MySQLRepository) ListCustomers(ctx context.Context) ([]model.CustomerSite, error) {
	return r.listCustomers(ctx, mysqlCustomerSelect+` ORDER BY cs.converted_at DESC`)
}

func (r *MySQLRepository) ListCustomersForSales(ctx context.Context, salesExecutiveID uuid.UUID) ([]model.CustomerSite, error) {
	return r.listCustomers(ctx, mysqlCustomerSelect+` WHERE cs.sales_executive_id = ? ORDER BY cs.converted_at DESC`, salesExecutiveID.String())
}

func (r *MySQLRepository) FindCustomerForSales(ctx context.Context, id, salesExecutiveID uuid.UUID) (model.CustomerDetail, error) {
	customer, err := mysqlScanCustomer(r.db.QueryRowContext(ctx, mysqlCustomerSelect+` WHERE cs.id = ? AND cs.sales_executive_id = ?`, id.String(), salesExecutiveID.String()))
	if err != nil {
		return model.CustomerDetail{}, err
	}
	parent, err := mysqlScanParent(r.db.QueryRowContext(ctx, mysqlParentSelect+` WHERE pc.id = ?`, customer.ParentCompanyID.String()))
	if err != nil {
		return model.CustomerDetail{}, err
	}
	var sourceName string
	if customer.SourceProspectID != uuid.Nil {
		if err := r.db.QueryRowContext(ctx, `SELECT place_name FROM prospects WHERE id = ?`, customer.SourceProspectID.String()).Scan(&sourceName); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return model.CustomerDetail{}, fmt.Errorf("read source prospect: %w", err)
		}
	}
	return model.CustomerDetail{Customer: customer, ParentCompany: parent, SourceProspectName: sourceName}, nil
}

func (r *MySQLRepository) FindCustomer(ctx context.Context, id uuid.UUID) (model.CustomerDetail, error) {
	customer, err := mysqlScanCustomer(r.db.QueryRowContext(ctx, mysqlCustomerSelect+` WHERE cs.id = ?`, id.String()))
	if err != nil {
		return model.CustomerDetail{}, err
	}
	parent, err := mysqlScanParent(r.db.QueryRowContext(ctx, mysqlParentSelect+` WHERE pc.id = ?`, customer.ParentCompanyID.String()))
	if err != nil {
		return model.CustomerDetail{}, err
	}
	var sourceName string
	if customer.SourceProspectID != uuid.Nil {
		if err := r.db.QueryRowContext(ctx, `SELECT place_name FROM prospects WHERE id = ?`, customer.SourceProspectID.String()).Scan(&sourceName); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return model.CustomerDetail{}, fmt.Errorf("read source prospect: %w", err)
		}
	}
	return model.CustomerDetail{Customer: customer, ParentCompany: parent, SourceProspectName: sourceName}, nil
}

func (r *MySQLRepository) listCustomers(ctx context.Context, query string, args ...any) ([]model.CustomerSite, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list customer sites: %w", err)
	}
	defer rows.Close()
	items := make([]model.CustomerSite, 0)
	for rows.Next() {
		item, err := mysqlScanCustomer(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const mysqlCustomerListBase = `
	FROM customer_sites cs
	JOIN parent_companies pc ON pc.id = cs.parent_company_id
	JOIN users u ON u.id = cs.sales_executive_id`

const mysqlCustomerListSelect = `
	SELECT cs.id, cs.customer_code, cs.parent_company_id, pc.parent_code, pc.name,
	       cs.source_prospect_id, cs.source_google_place_id, cs.name, cs.segment,
	       cs.category, cs.address_mode, cs.province, cs.district, cs.sub_district,
	       cs.village, cs.latitude, cs.longitude, cs.preview_address, cs.site_contacts,
	       cs.ppn, cs.id_tku_number, cs.nik, cs.shipment_cost, cs.invoice_type,
	       cs.bank_account, cs.bill_to_source, cs.ship_to_source,
	       cs.billing_address_preview, cs.shipping_address_preview,
	       cs.sales_executive_id, u.full_name, cs.sales_assignments,
	       cs.converted_at, cs.updated_at, cs.converted_by_admin_id
` + mysqlCustomerListBase

func (r *MySQLRepository) ListCustomersPaged(ctx context.Context, params model.CustomerListParams) (model.CustomerListResult, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.Limit < 1 || params.Limit > 100 {
		params.Limit = 20
	}

	where, args := mysqlBuildCustomerWhere(params)
	sortClause := mysqlBuildCustomerSort(params.Sort)

	countQuery := `SELECT COUNT(*) ` + mysqlCustomerListBase + where
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return model.CustomerListResult{}, fmt.Errorf("count customer sites: %w", err)
	}

	pages := total / params.Limit
	if total%params.Limit > 0 {
		pages++
	}

	offset := (params.Page - 1) * params.Limit
	dataQuery := mysqlCustomerListSelect + where + sortClause + ` LIMIT ? OFFSET ?`
	dataArgs := append(args, params.Limit, offset)

	rows, err := r.db.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return model.CustomerListResult{}, fmt.Errorf("query customer sites: %w", err)
	}
	defer rows.Close()

	items := make([]model.CustomerSite, 0)
	for rows.Next() {
		item, err := mysqlScanCustomer(rows)
		if err != nil {
			return model.CustomerListResult{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return model.CustomerListResult{}, err
	}

	return model.CustomerListResult{
		Items: items,
		Total: total,
		Page:  params.Page,
		Limit: params.Limit,
		Pages: pages,
	}, nil
}

func (r *MySQLRepository) ListFilterOptions(ctx context.Context) (model.ListFilterOptions, error) {
	segments, err := r.distinctColumn(ctx, `SELECT DISTINCT segment FROM customer_sites WHERE segment != '' ORDER BY segment`)
	if err != nil {
		return model.ListFilterOptions{}, err
	}
	categories, err := r.distinctColumn(ctx, `SELECT DISTINCT category FROM customer_sites WHERE category != '' ORDER BY category`)
	if err != nil {
		return model.ListFilterOptions{}, err
	}
	regions, err := r.distinctColumn(ctx, `SELECT DISTINCT province FROM customer_sites WHERE province != '' ORDER BY province`)
	if err != nil {
		return model.ListFilterOptions{}, err
	}
	sales, err := r.ListActiveSalesExecutives(ctx)
	if err != nil {
		return model.ListFilterOptions{}, err
	}
	return model.ListFilterOptions{
		Segments:   segments,
		Categories: categories,
		Regions:    regions,
		SalesExec:  sales,
	}, nil
}

func (r *MySQLRepository) distinctColumn(ctx context.Context, query string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query distinct column: %w", err)
	}
	defer rows.Close()
	items := make([]string, 0)
	for rows.Next() {
		var val string
		if err := rows.Scan(&val); err != nil {
			return nil, err
		}
		items = append(items, val)
	}
	return items, rows.Err()
}

func mysqlBuildCustomerWhere(params model.CustomerListParams) (string, []any) {
	conditions := make([]string, 0)
	args := make([]any, 0)

	if params.Keyword != "" {
		pattern := "%" + strings.TrimSpace(params.Keyword) + "%"
		conditions = append(conditions, `(
			cs.name LIKE ? OR
			cs.customer_code LIKE ? OR
			pc.name LIKE ? OR
			pc.parent_code LIKE ? OR
			CAST(cs.site_contacts AS CHAR) LIKE ? OR
			cs.preview_address LIKE ?
		)`)
		args = append(args, pattern, pattern, pattern, pattern, pattern, pattern)
	}
	if params.Segment != "" {
		conditions = append(conditions, `cs.segment = ?`)
		args = append(args, params.Segment)
	}
	if params.Category != "" {
		conditions = append(conditions, `cs.category = ?`)
		args = append(args, params.Category)
	}
	if params.Sales != "" {
		conditions = append(conditions, `u.full_name LIKE ?`)
		args = append(args, "%"+params.Sales+"%")
	}
	if params.Region != "" {
		conditions = append(conditions, `cs.province = ?`)
		args = append(args, params.Region)
	}

	if len(conditions) == 0 {
		return "", args
	}
	where := " WHERE " + strings.Join(conditions, " AND ")
	return where, args
}

func mysqlBuildCustomerSort(sort string) string {
	switch sort {
	case "oldest":
		return ` ORDER BY cs.converted_at ASC`
	case "name":
		return ` ORDER BY cs.name ASC`
	case "code":
		return ` ORDER BY cs.customer_code ASC`
	case "converted":
		return ` ORDER BY cs.converted_at DESC`
	case "updated":
		return ` ORDER BY cs.updated_at DESC`
	default:
		return ` ORDER BY cs.converted_at DESC`
	}
}

type mysqlRowScanner interface {
	Scan(...any) error
}

func mysqlScanParent(row mysqlRowScanner) (model.ParentCompany, error) {
	var item model.ParentCompany
	var contacts, kams []byte
	var latitude, longitude sql.NullFloat64
	var idStr string
	err := row.Scan(&idStr, &item.ParentCode, &item.Name, &item.Address.Mode,
		&item.Address.Province, &item.Address.District, &item.Address.SubDistrict,
		&item.Address.Village, &latitude, &longitude,
		&item.Address.PreviewAddress, &contacts, &item.NPWPName, &item.NPWPAddress,
		&item.NPWPNumber, &item.TermOfPayment, &kams)
	if errors.Is(err, sql.ErrNoRows) {
		return model.ParentCompany{}, ErrNotFound
	}
	if err != nil {
		return model.ParentCompany{}, fmt.Errorf("scan parent company: %w", err)
	}
	parsedID, err := uuid.Parse(idStr)
	if err != nil {
		return model.ParentCompany{}, fmt.Errorf("parse parent company id from database: %w", err)
	}
	item.ID = parsedID
	if latitude.Valid {
		item.Address.Latitude = &latitude.Float64
	}
	if longitude.Valid {
		item.Address.Longitude = &longitude.Float64
	}
	if err := mysqlDecodeJSON(contacts, &item.Contacts); err != nil {
		return model.ParentCompany{}, err
	}
	if err := mysqlDecodeJSON(kams, &item.KAMAssignments); err != nil {
		return model.ParentCompany{}, err
	}
	return item, nil
}

func mysqlScanCustomer(row mysqlRowScanner) (model.CustomerSite, error) {
	var item model.CustomerSite
	var contacts, assignments []byte
	var sourceProspectID, sourceGooglePlaceID sql.NullString
	var latitude, longitude sql.NullFloat64
	var idStr, parentCompanyIDStr, salesExecIDStr, convertedByAdminIDStr string
	err := row.Scan(&idStr, &item.CustomerCode, &parentCompanyIDStr, &item.ParentCode,
		&item.ParentCompanyName, &sourceProspectID, &sourceGooglePlaceID,
		&item.Name, &item.Segment, &item.Category, &item.Address.Mode,
		&item.Address.Province, &item.Address.District, &item.Address.SubDistrict,
		&item.Address.Village, &latitude, &longitude,
		&item.Address.PreviewAddress, &contacts, &item.PPN, &item.IDTKUNumber,
		&item.NIK, &item.ShipmentCost, &item.InvoiceType, &item.BankAccount,
		&item.BillToSource, &item.ShipToSource, &item.BillingAddressPreview,
		&item.ShippingAddressPreview, &salesExecIDStr, &item.SalesExecutiveName,
		&assignments, &item.ConvertedAt, &item.UpdatedAt, &convertedByAdminIDStr)
	if errors.Is(err, sql.ErrNoRows) {
		return model.CustomerSite{}, ErrNotFound
	}
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("scan customer site: %w", err)
	}
	parsedID, err := uuid.Parse(idStr)
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("parse customer id from database: %w", err)
	}
	parsedParentID, err := uuid.Parse(parentCompanyIDStr)
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("parse parent company id from database: %w", err)
	}
	parsedSalesExecID, err := uuid.Parse(salesExecIDStr)
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("parse sales executive id from database: %w", err)
	}
	parsedAdminID, err := uuid.Parse(convertedByAdminIDStr)
	if err != nil {
		return model.CustomerSite{}, fmt.Errorf("parse converted by admin id from database: %w", err)
	}
	item.ID = parsedID
	item.ParentCompanyID = parsedParentID
	item.SalesExecutiveID = parsedSalesExecID
	item.ConvertedByAdminID = parsedAdminID
	if sourceProspectID.Valid && sourceProspectID.String != "" {
		parsed, parseErr := uuid.Parse(sourceProspectID.String)
		if parseErr != nil {
			return model.CustomerSite{}, fmt.Errorf("parse source prospect id from database: %w", parseErr)
		}
		item.SourceProspectID = parsed
	}
	if sourceGooglePlaceID.Valid {
		item.SourceGooglePlaceID = sourceGooglePlaceID.String
	}
	item.Region = item.Address.Province
	if latitude.Valid {
		item.Address.Latitude = &latitude.Float64
	}
	if longitude.Valid {
		item.Address.Longitude = &longitude.Float64
	}
	if err := mysqlDecodeJSON(contacts, &item.Contacts); err != nil {
		return model.CustomerSite{}, err
	}
	if err := mysqlDecodeJSON(assignments, &item.SalesAssignments); err != nil {
		return model.CustomerSite{}, err
	}
	return item, nil
}

func mysqlDecodeJSON(raw []byte, destination any) error {
	if err := json.Unmarshal(raw, destination); err != nil {
		return fmt.Errorf("decode simulation JSON: %w", err)
	}
	return nil
}

func mysqlMapDatabaseError(err error) error {
	if err == nil {
		return nil
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return ErrCodeConflict
	}
	return fmt.Errorf("persist conversion: %w", err)
}

func (r *MySQLRepository) nextCode(ctx context.Context, tx *sql.Tx, counterName string) (int64, error) {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO code_counters (name, current_value, updated_at)
		VALUES (?, LAST_INSERT_ID(1), UTC_TIMESTAMP(6))
		ON DUPLICATE KEY UPDATE current_value = LAST_INSERT_ID(current_value + 1), updated_at = UTC_TIMESTAMP(6)`, counterName)
	if err != nil {
		return 0, fmt.Errorf("increment %s counter: %w", counterName, err)
	}
	var value int64
	if err := tx.QueryRowContext(ctx, `SELECT LAST_INSERT_ID()`).Scan(&value); err != nil {
		return 0, fmt.Errorf("read %s counter: %w", counterName, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s counter returned non-positive value %d", counterName, value)
	}
	return value, nil
}

func nullableFloat64Value(n sql.NullFloat64) any {
	if n.Valid {
		return n.Float64
	}
	return nil
}

// These formats exist only for the local mentor simulation. Final ownership,
// prefixes, and sequencing remain an ERP/business decision.
func mysqlSimulationParentCode(sequence int64) string {
	return fmt.Sprintf("PC-%06d", sequence)
}

func mysqlSimulationCustomerCode(parentCode string, sequence int64) string {
	return fmt.Sprintf("%s-S%03d", parentCode, sequence)
}
