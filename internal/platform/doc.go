// Package platform implements the first authenticated keel project boundary.
//
// ent schemas and generated clients define the relational models and ordinary
// entity reads. Native PostgreSQL transactions implement the operations needing
// row/advisory locks, atomic throttles, session rotation, append-only audit, and
// idempotency receipts. Their SQL is parameterized; ent's runtime Schema.Create
// is deliberately not invoked. Checked-in embedded migrations are applied only
// by the server-side operator CLI and are checksum-validated by API startup.
package platform
