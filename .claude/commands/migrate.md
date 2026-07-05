Run the local database migration using `src/Install/local_migration.go`.

Database credentials come from `docker-compose.yml`:
- DB name: `cuento`
- DB user: `user`
- DB password: `password`
- Docker container: `cuento-backend-db-1`

## Steps

1. **Generate migration SQL** by running from the project root:
   ```
   go run src/Install/local_migration.go -db cuento -user user -pass password -container cuento-backend-db-1 -mysql-bin mariadb 2>/tmp/migrate_stderr.txt
   ```
   Capture stdout as the migration SQL. Show the contents of `/tmp/migrate_stderr.txt` for status.

2. **If the output is empty**, report "Schema is already up to date — no migration needed." and stop.

3. **If there is SQL output**, show it to the user and ask for confirmation before applying.

4. **Apply the migration** by saving the SQL to `/tmp/migration.sql` and running it inside the container:
   ```
   docker exec -i cuento-backend-db-1 mysql -u user -ppassword cuento < /tmp/migration.sql
   ```

5. Report success or any errors.