// The desktop application is a separate module so that the archive core stays
// what it is: a module with no external dependencies at all.
//
// That property is not tidiness. The core's claim is that an observation's
// identity is computable and checkable by anyone, and every dependency added to
// it is something a reader has to trust or audit before that claim means
// anything. A window is worth having; it is not worth paying for out of the
// core's ledger.
//
// The replace directive points at the checkout rather than at a published
// version, so the application always builds against the core beside it and a
// change to one cannot silently be tested against an older copy of the other.
module github.com/worldledger/worldledger-mc/desktop

go 1.23

replace github.com/worldledger/worldledger-mc => ..

require github.com/worldledger/worldledger-mc v0.0.0-00010101000000-000000000000
