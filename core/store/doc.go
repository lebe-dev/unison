// Package store persists node data in SQLite with encryption at rest.
//
// Plaintext columns hold only the fields needed to index and query data:
// identifiers, origin device, sequence numbers, expiry times and blob hashes.
// Every other column holds a value produced by Sealer, which binds the
// ciphertext to its table and row key.
//
// The data encryption key is a random 32 byte value kept in the platform
// keystore. Losing the key makes an existing database unreadable.
//
// Schema changes are forward-only SQL migrations applied by Open. Packages that
// own a domain embed their own files, read them with MigrationsFromFS and pass
// them through Options.Migrations. The database records every applied version,
// so a migration added to a lower block still runs on a database that already
// has migrations of higher blocks.
package store
