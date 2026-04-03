module mimusic-plugin-lxmusic

go 1.26

require (
	github.com/knqyf263/go-plugin v0.9.0
	github.com/mimusic-org/musicsdk v0.0.0-00010101000000-000000000000
	github.com/mimusic-org/plugin v0.1.2
	github.com/mimusic-org/plugin/pkg/go-plugin-http v0.0.0-20260403050353-9ace8993728e
)

require (
	github.com/tetratelabs/wazero v1.11.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/mimusic-org/musicsdk => ../musicsdk
