module github.com/tylersavery/channel3

go 1.26.2

// The Vite build in web/ installs a dependency that ships a Go package.
// Without this the ./... pattern walks into node_modules.
ignore ./web/node_modules

require (
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef
	golang.org/x/image v0.46.0
	golang.org/x/sys v0.48.0
	golang.org/x/term v0.46.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)
