When creating a new controller endpoint in this project, always complete all three steps:

1. **Write the controller logic** in the appropriate file under `src/Controllers/`

2. **Register the route** in `main.go` using the correct router:
   - `publicRouter` — no auth required
   - `optionalAuthRouter` — auth optional (context populated if token present)
   - `protectedRouter` — auth required + permissions middleware

3. **Add the permission** to `src/Install/permissions.csv` in the format:
   ```
   role_id,type,permission
   ```
   Where:
   - `role_id`: 1 = guest, 2 = user, 3 = admin
   - `type`: always `0` for endpoint permissions
   - `permission`: the route path as registered in main.go (e.g. `/admin/character-list`)

   Public routes registered on `publicRouter` do not need a permissions.csv entry.

Never finish a controller task without verifying all three steps are done.
