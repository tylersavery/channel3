module github.com/tylersavery/channel3

go 1.26.2

// The Vite build in web/ installs a dependency that ships a Go package.
// Without this the ./... pattern walks into node_modules.
ignore ./web/node_modules

require (
	golang.org/x/sys v0.48.0
	golang.org/x/term v0.46.0
	gopkg.in/yaml.v3 v3.0.1
)
