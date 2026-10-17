package prod

import "embed"

//go:embed */compose.yml */Caddyfile */.env.example
var Files embed.FS
