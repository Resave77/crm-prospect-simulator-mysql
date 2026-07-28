-- Allow direct customer creation without a source prospect
-- Drop unique constraints first (nullable unique is not useful here)
DROP INDEX IF EXISTS "customer_sites_source_prospect_id_key";
DROP INDEX IF EXISTS "customer_sites_source_google_place_id_key";

-- Drop the foreign key on source_prospect_id (will be NULL for manual customers)
ALTER TABLE "customer_sites" DROP CONSTRAINT IF EXISTS "customer_sites_source_prospect_id_fkey";

-- Make columns nullable
ALTER TABLE "customer_sites" ALTER COLUMN "source_prospect_id" DROP NOT NULL;
ALTER TABLE "customer_sites" ALTER COLUMN "source_google_place_id" DROP NOT NULL;
