# Mini Cloud

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

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `HTTP_ADDR` | `0.0.0.0:8080` | Where the server listens |
| `MAX_UPLOAD_MB` | `32` | Largest file you can upload |
| `JWT_SECRET` | (auto file in `data/.jwt-secret`) | Signs login tokens |

See `.env.example`. Do not commit a real `.env`.

## What this is not

Not a Dropbox desktop sync client, and not a full Amazon S3 clone. One process, one disk, your files.
