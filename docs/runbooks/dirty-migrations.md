## Symptom
Deployment fails with an error like: `migrate: Dirty database version X. Fix and force version.`

## Cause
A migration was applied but failed halfway through (e.g., a syntax error, a lock timeout, or a connection drop). The `schema_migrations` table marks the version as `dirty=true` to prevent further migrations from running on a half-broken state.

## Resolution
1. **Investigate the failure:** Check the application logs or the Kubernetes Job logs for the exact SQL error.
2. **Fix the schema manually:** Connect directly to the database using `psql` (bypass PgBouncer) and either revert the partial changes or complete the missing ones.
3. **Clear the dirty flag:** Once the schema matches what the migration intended to do, force the migration version to the failed version number to clear the flag.
   ~~~bash
   # Example: if version 4 failed
   MIGRATION_DATABASE_URL="postgres://..." go run ./cmd/migrate force 4
   ~~~
4. **Resume deployments:** Trigger a new deployment. The PreSync job will see `dirty=false` and resume from the next migration.

## Prevention
- Always write down migrations and test them locally.
- Never write destructive migrations (like dropping a column) in the same step as code that stops using it. Use the expand-and-contract pattern.