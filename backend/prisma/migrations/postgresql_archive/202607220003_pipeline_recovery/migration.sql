CREATE TYPE "ProspectStatus_recovery" AS ENUM (
  'NEW_LEAD', 'CONTACTED', 'INTERESTED', 'QUALIFIED',
  'PROPOSAL_SENT', 'NEGOTIATION', 'WON', 'LOST', 'CONVERTED'
);

ALTER TABLE "prospects" ALTER COLUMN "status" DROP DEFAULT;
ALTER TABLE "prospects"
  ALTER COLUMN "status" TYPE "ProspectStatus_recovery"
  USING (CASE WHEN "status"::text = 'FOLLOW_UP' THEN 'NEGOTIATION' ELSE "status"::text END)::"ProspectStatus_recovery";
ALTER TABLE "prospect_status_history"
  ALTER COLUMN "from_status" TYPE "ProspectStatus_recovery"
  USING (CASE WHEN "from_status"::text = 'FOLLOW_UP' THEN 'NEGOTIATION' ELSE "from_status"::text END)::"ProspectStatus_recovery";
ALTER TABLE "prospect_status_history"
  ALTER COLUMN "to_status" TYPE "ProspectStatus_recovery"
  USING (CASE WHEN "to_status"::text = 'FOLLOW_UP' THEN 'NEGOTIATION' ELSE "to_status"::text END)::"ProspectStatus_recovery";
DROP TYPE "ProspectStatus";
ALTER TYPE "ProspectStatus_recovery" RENAME TO "ProspectStatus";
ALTER TABLE "prospects" ALTER COLUMN "status" SET DEFAULT 'NEW_LEAD';
ALTER TABLE "prospects" ADD COLUMN "industry_group" TEXT NOT NULL DEFAULT 'Other';
CREATE INDEX "prospects_industry_group_status_idx" ON "prospects"("industry_group", "status");
