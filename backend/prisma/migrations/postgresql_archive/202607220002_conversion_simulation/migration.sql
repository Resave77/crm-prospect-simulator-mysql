-- Point 10-14 CRM simulation only. Code sequences are intentionally local simulation formats,
-- not a claim about the final ERP-owned numbering rules.
CREATE TYPE "ProspectStatus" AS ENUM ('FOLLOW_UP', 'WON', 'LOST', 'CONVERTED');

CREATE SEQUENCE "parent_company_code_seq" START 1;
CREATE SEQUENCE "customer_site_code_seq" START 1;

CREATE TABLE "prospects" (
    "id" UUID NOT NULL,
    "google_place_id" TEXT NOT NULL,
    "place_name" TEXT NOT NULL,
    "formatted_address" TEXT NOT NULL,
    "latitude" DOUBLE PRECISION,
    "longitude" DOUBLE PRECISION,
    "place_category" TEXT NOT NULL,
    "place_types" JSONB NOT NULL DEFAULT '[]',
    "phone_number" TEXT,
    "assigned_sales_executive_id" UUID NOT NULL,
    "visit_notes" TEXT NOT NULL DEFAULT '',
    "follow_up_notes" TEXT NOT NULL DEFAULT '',
    "status" "ProspectStatus" NOT NULL DEFAULT 'FOLLOW_UP',
    "converted_at" TIMESTAMPTZ(6),
    "created_at" TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "prospects_pkey" PRIMARY KEY ("id")
);

CREATE TABLE "prospect_status_history" (
    "id" UUID NOT NULL,
    "prospect_id" UUID NOT NULL,
    "from_status" "ProspectStatus",
    "to_status" "ProspectStatus" NOT NULL,
    "changed_by_user_id" UUID NOT NULL,
    "notes" TEXT NOT NULL DEFAULT '',
    "created_at" TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "prospect_status_history_pkey" PRIMARY KEY ("id")
);

CREATE TABLE "parent_companies" (
    "id" UUID NOT NULL,
    "parent_code" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "address_mode" TEXT NOT NULL DEFAULT 'MANUAL',
    "province" TEXT NOT NULL DEFAULT '',
    "district" TEXT NOT NULL DEFAULT '',
    "sub_district" TEXT NOT NULL DEFAULT '',
    "village" TEXT NOT NULL DEFAULT '',
    "latitude" DOUBLE PRECISION,
    "longitude" DOUBLE PRECISION,
    "preview_address" TEXT NOT NULL DEFAULT '',
    "company_contacts" JSONB NOT NULL DEFAULT '[]',
    "npwp_name" TEXT NOT NULL DEFAULT '',
    "npwp_address" TEXT NOT NULL DEFAULT '',
    "npwp_number" TEXT NOT NULL DEFAULT '',
    "term_of_payment" TEXT NOT NULL DEFAULT '',
    "kam_assignments" JSONB NOT NULL DEFAULT '[]',
    "created_at" TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "parent_companies_pkey" PRIMARY KEY ("id")
);

CREATE TABLE "customer_sites" (
    "id" UUID NOT NULL,
    "customer_code" TEXT NOT NULL,
    "parent_company_id" UUID NOT NULL,
    "source_prospect_id" UUID NOT NULL,
    "source_google_place_id" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "segment" TEXT NOT NULL,
    "category" TEXT NOT NULL,
    "address_mode" TEXT NOT NULL DEFAULT 'MANUAL',
    "province" TEXT NOT NULL DEFAULT '',
    "district" TEXT NOT NULL DEFAULT '',
    "sub_district" TEXT NOT NULL DEFAULT '',
    "village" TEXT NOT NULL DEFAULT '',
    "latitude" DOUBLE PRECISION,
    "longitude" DOUBLE PRECISION,
    "preview_address" TEXT NOT NULL,
    "site_contacts" JSONB NOT NULL DEFAULT '[]',
    "ppn" TEXT NOT NULL DEFAULT '',
    "id_tku_number" TEXT NOT NULL DEFAULT '',
    "nik" TEXT NOT NULL DEFAULT '',
    "shipment_cost" TEXT NOT NULL DEFAULT '',
    "invoice_type" TEXT NOT NULL DEFAULT '',
    "bank_account" TEXT NOT NULL DEFAULT '',
    "bill_to_source" TEXT NOT NULL DEFAULT '',
    "ship_to_source" TEXT NOT NULL DEFAULT '',
    "billing_address_preview" TEXT NOT NULL DEFAULT '',
    "shipping_address_preview" TEXT NOT NULL DEFAULT '',
    "sales_executive_id" UUID NOT NULL,
    "sales_assignments" JSONB NOT NULL DEFAULT '[]',
    "converted_at" TIMESTAMPTZ(6) NOT NULL,
    "converted_by_admin_id" UUID NOT NULL,
    "created_at" TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "customer_sites_pkey" PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "prospects_google_place_id_key" ON "prospects"("google_place_id");
CREATE INDEX "prospects_assigned_sales_executive_id_status_idx" ON "prospects"("assigned_sales_executive_id", "status");
CREATE INDEX "prospects_status_created_at_idx" ON "prospects"("status", "created_at");
CREATE INDEX "prospect_status_history_prospect_id_created_at_idx" ON "prospect_status_history"("prospect_id", "created_at");
CREATE UNIQUE INDEX "parent_companies_parent_code_key" ON "parent_companies"("parent_code");
CREATE INDEX "parent_companies_name_idx" ON "parent_companies"("name");
CREATE UNIQUE INDEX "customer_sites_customer_code_key" ON "customer_sites"("customer_code");
CREATE UNIQUE INDEX "customer_sites_source_prospect_id_key" ON "customer_sites"("source_prospect_id");
CREATE UNIQUE INDEX "customer_sites_source_google_place_id_key" ON "customer_sites"("source_google_place_id");
CREATE INDEX "customer_sites_sales_executive_id_converted_at_idx" ON "customer_sites"("sales_executive_id", "converted_at");
CREATE INDEX "customer_sites_parent_company_id_idx" ON "customer_sites"("parent_company_id");

ALTER TABLE "prospects" ADD CONSTRAINT "prospects_assigned_sales_executive_id_fkey"
FOREIGN KEY ("assigned_sales_executive_id") REFERENCES "users"("id") ON DELETE RESTRICT ON UPDATE CASCADE;
ALTER TABLE "prospect_status_history" ADD CONSTRAINT "prospect_status_history_prospect_id_fkey"
FOREIGN KEY ("prospect_id") REFERENCES "prospects"("id") ON DELETE RESTRICT ON UPDATE CASCADE;
ALTER TABLE "prospect_status_history" ADD CONSTRAINT "prospect_status_history_changed_by_user_id_fkey"
FOREIGN KEY ("changed_by_user_id") REFERENCES "users"("id") ON DELETE RESTRICT ON UPDATE CASCADE;
ALTER TABLE "customer_sites" ADD CONSTRAINT "customer_sites_parent_company_id_fkey"
FOREIGN KEY ("parent_company_id") REFERENCES "parent_companies"("id") ON DELETE RESTRICT ON UPDATE CASCADE;
ALTER TABLE "customer_sites" ADD CONSTRAINT "customer_sites_source_prospect_id_fkey"
FOREIGN KEY ("source_prospect_id") REFERENCES "prospects"("id") ON DELETE RESTRICT ON UPDATE CASCADE;
ALTER TABLE "customer_sites" ADD CONSTRAINT "customer_sites_sales_executive_id_fkey"
FOREIGN KEY ("sales_executive_id") REFERENCES "users"("id") ON DELETE RESTRICT ON UPDATE CASCADE;
ALTER TABLE "customer_sites" ADD CONSTRAINT "customer_sites_converted_by_admin_id_fkey"
FOREIGN KEY ("converted_by_admin_id") REFERENCES "users"("id") ON DELETE RESTRICT ON UPDATE CASCADE;
