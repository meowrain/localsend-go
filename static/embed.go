package static

import "embed"

//go:embed *
var EmbeddedStaticFiles embed.FS
