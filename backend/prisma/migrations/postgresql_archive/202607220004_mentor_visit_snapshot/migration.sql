ALTER TABLE "prospects"
  ADD COLUMN "website_url" TEXT,
  ADD COLUMN "google_maps_url" TEXT;

CREATE TABLE "prospect_visits" (
  "id" UUID NOT NULL,
  "prospect_id" UUID NOT NULL,
  "sales_executive_id" UUID NOT NULL,
  "check_in_at" TIMESTAMPTZ(6) NOT NULL,
  "check_in_latitude" DOUBLE PRECISION NOT NULL,
  "check_in_longitude" DOUBLE PRECISION NOT NULL,
  "check_out_at" TIMESTAMPTZ(6),
  "check_out_latitude" DOUBLE PRECISION,
  "check_out_longitude" DOUBLE PRECISION,
  "selfie_reference" TEXT NOT NULL DEFAULT '',
  "visit_notes" TEXT NOT NULL DEFAULT '',
  "follow_up_notes" TEXT NOT NULL DEFAULT '',
  "created_at" TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "prospect_visits_pkey" PRIMARY KEY ("id"),
  CONSTRAINT "prospect_visits_prospect_id_fkey" FOREIGN KEY ("prospect_id") REFERENCES "prospects"("id") ON DELETE RESTRICT ON UPDATE CASCADE,
  CONSTRAINT "prospect_visits_sales_executive_id_fkey" FOREIGN KEY ("sales_executive_id") REFERENCES "users"("id") ON DELETE RESTRICT ON UPDATE CASCADE
);

CREATE INDEX "prospect_visits_prospect_id_check_in_at_idx" ON "prospect_visits"("prospect_id", "check_in_at");
CREATE INDEX "prospect_visits_sales_executive_id_check_in_at_idx" ON "prospect_visits"("sales_executive_id", "check_in_at");
CREATE UNIQUE INDEX "prospect_visits_one_open_visit_idx" ON "prospect_visits"("prospect_id") WHERE "check_out_at" IS NULL;
