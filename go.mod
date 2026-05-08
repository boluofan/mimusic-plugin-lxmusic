module mimusic-plugin-lxmusic

go 1.26

require (
	github.com/knqyf263/go-plugin v0.9.0
	github.com/mimusic-org/musicsdk v1.0.4
	github.com/mimusic-org/plugin v0.1.8
	github.com/mimusic-org/plugin/pkg/go-plugin-http v0.0.0-20260417035450-0b166733a3ca
)

require (
	github.com/tetratelabs/wazero v1.11.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

// replace github.com/mimusic-org/musicsdk => ../musicsdk
// replace github.com/mimusic-org/plugin => ../../plugin
