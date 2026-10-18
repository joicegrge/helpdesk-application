# HelpDesk login API

This small Go API implements login, the current-user endpoint, and logout. It connects to XAMPP MariaDB through its MySQL-compatible protocol and verifies bcrypt password hashes.

## XAMPP MariaDB settings

The local XAMPP configuration was checked at `C:\xampp\mysql\bin\my.ini` and `C:\xampp\phpMyAdmin\config.inc.php`: it uses host `127.0.0.1`, port `3306`, and phpMyAdmin is configured as `root` with a blank password. The backend `.env` is set to match. If you later change the MariaDB root password in phpMyAdmin, update `MYSQL_PASSWORD` in `.env` to the same value.

Start MySQL from the XAMPP Control Panel before starting the Go API. You have already created `helpdesk_db` and `users` in phpMyAdmin, so you do not need to execute the schema again. The schema is preserved in `schema.sql` for reference.

## Create the first administrator

From this folder, run the hash helper. It prompts for a password without echoing it and prints a bcrypt hash:

```powershell
go run ./cmd/hash-password
```

In phpMyAdmin, select the `helpdesk_db` database, open the **SQL** tab, and run this statement after replacing the sample details and hash with your values:

```sql
INSERT INTO users (full_name, email, `password`, role)
VALUES ('Helpdesk Administrator', 'admin@example.com', '<paste-generated-bcrypt-hash>', 'ADMIN');
```

The helper is a local utility. The API has no registration endpoint and never stores a plaintext password.

## Configure and start the backend

The local ignored `.env` file uses:

```dotenv
MYSQL_HOST=127.0.0.1
MYSQL_PORT=3306
MYSQL_USER=root
MYSQL_PASSWORD=
MYSQL_DATABASE=helpdesk_db
```

It also contains a generated private `JWT_SECRET` and CORS origins for Angular on ports 4200 and 4300. Keep `.env` local. If you use another MariaDB user, change `MYSQL_USER` and `MYSQL_PASSWORD` to that account. A dedicated user only needs `SELECT` access to `helpdesk_db.users` for this login-only API.

From this folder, run:

```powershell
go mod tidy
go run .
```

Run `go mod tidy` once when Go module downloads are available. The API listens on `http://localhost:8080`.

## Start Angular

Open another terminal and run:

```powershell
cd ..\helpdesk-frontend
npm start
```

Open `http://localhost:4200`. If port 4200 is occupied, use `npm start -- --port 4300`; that origin is allowed in `.env`.

## Endpoints

- `POST /api/auth/login` accepts `{ "email": "...", "password": "...", "remember_me": false }` and returns a signed bearer token and public user details. A remembered session lasts 30 days; otherwise it lasts 12 hours.
- `GET /api/auth/me` returns the current public user. Send `Authorization: Bearer <access_token>`.
- `POST /api/auth/logout` revokes the token while this API process is running. Send `Authorization: Bearer <access_token>`.

Logout revocations are kept in memory, so restarting the API clears that revocation list. Tokens still expire according to their signed expiry time.
