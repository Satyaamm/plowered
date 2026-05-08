// Package graph is the in-process metadata graph engine.
//
// It owns the Asset / Edge / Tag types and the persistence-agnostic
// operations: insert, upsert, traverse, search-by-property. Storage is
// abstracted behind the Store interface so the same engine runs over
// Postgres+AGE in production and SQLite in dev.
package graph
