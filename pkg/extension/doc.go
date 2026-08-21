// Package extension is the public, stable contract for Pando extensions.
//
// An extension is a Go package compiled into the Pando binary that adds
// capabilities (tools, HTTP endpoints, frontend assets, memory sinks, ...)
// without the core knowing about it. Registration happens at init() time:
//
//	func init() { extension.Register(MyExtension{}) }
//
// Importing the package is the installation. A build that should contain an
// extension blank-imports it; a build that should not, does not. This is the
// Caddy model, and the same one already used by remembrances-mcp.
//
// # Rules for this package
//
// This package is imported by out-of-tree modules (notably the private
// enterprise module github.com/digiogithub/alchemai-agent), which means:
//
//  1. It must never import github.com/digiogithub/pando/internal/... — Go
//     forbids that from another module, so any such import would silently make
//     the contract unusable outside this repository.
//  2. Every type crossing the boundary is declared here, in terms of the
//     standard library only. Core adapts these types to its internal ones.
//  3. Changes here are contract changes. Adding a capability interface or an
//     optional method is safe (capabilities are discovered by type assertion);
//     changing an existing signature is not.
//
// # Lifecycle
//
//	New() -> Provision(ctx, HostServices) -> Validate() -> [use] -> Cleanup()
//
// Provision and Validate and Cleanup are all optional: implement only the ones
// the extension needs, and the manager discovers them by type assertion.
package extension
