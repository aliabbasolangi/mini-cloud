# SafeKeeping

A small personal file vault: a Go API plus a web UI. You log in, upload files, put them in folders, and share a link that works for 24 hours.

Files are stored as raw bytes on disk, named by a fingerprint. Names, owners, and share links live in a SQLite catalog.

## Run it

You need [Go](https://go.dev/dl/) 1.22 or newer.

```powershell
go test ./...
go run ./cmd/api
```

Open http://127.0.0.1:8080 and create an account.

Uploads, the database, and the login secret stay in `data/` on your machine. That folder is gitignored.

## Host it

This is one Go process plus a disk. Use Railway, Fly, Render, or a VPS — not Vercel.

Point a persistent volume at `/data` and set:

```
BLOB_DIR=/data/blobs
THUMB_DIR=/data/thumbs
AVATAR_DIR=/data/avatars
DATABASE_PATH=/data/minicloud.db
JWT_SECRET_FILE=/data/.jwt-secret
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your-2fa-mailbox@gmail.com
SMTP_PASS=your-app-password
SMTP_FROM=SafeKeeping <your-2fa-mailbox@gmail.com>
MAIL_LOG=0
```

Railway Hobby and Trial block outbound SMTP, so Gmail will fail there. Use Resend over HTTPS (`RESEND_API_KEY`) or set `MAIL_LOG=1` and read codes from Deploy logs.

The app also reads `PORT` from the host. Do not commit `.env`.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `HTTP_ADDR` | `0.0.0.0:8080` | Where the server listens |
| `MAX_UPLOAD_MB` | `32` | Largest file you can upload |
| `JWT_SECRET` | (auto file in `data/.jwt-secret`) | Signs login tokens |

See `.env.example`. Do not commit a real `.env`.

## What this is not

Not a Dropbox desktop sync client, and not a full Amazon S3 clone. One process, one disk, your files.
