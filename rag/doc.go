// Package rag is the root of Pando's retrieval module: the knowledge-base
// store with hybrid (FTS5 + vector) search, the embedding providers, the
// document converter and the schema migrations they need.
//
// It is a separate Go module (github.com/digiogithub/pando/rag) so that other
// programs, such as the gintrack hub, can import it without pulling in the
// rest of Pando's dependency graph. The packages take their database handle,
// logger and settings explicitly: nothing in this module reads Pando's
// configuration or global state.
package rag
