---
name: golang-comments
description: Godoc and inline comment mechanics for Go. Use when writing or reviewing Go comments.
---

# Go Comments Skill

Go-specific mechanics for comments and documentation.
See `code-quality`'s Comments section for the underlying rule — a comment is a standalone contract, never in terms of another named symbol — and for whether to write one at all (restraint first).
This skill assumes that call is already made;
every example below follows the rule.

---

## File-level and package doc comments

See `code-quality`'s Comments section for the file-header rule and its sole-file exception.
Go distinguishes the two by whether a blank line separates the comment from `package`:

- **File-level comment** (every file, blank line before `package`) — describes this file's role within the package, not the package itself, in one to three lines.
- **Package doc comment** (exactly one file per package, no blank line before `package`) — starts with `Package <name>`, states what the package is for.
  Use `doc.go` when the package is large;
  otherwise the main file.

**Example — file-level:**

```go
// handlers_auth.go implements the HTTP handlers for login, logout, and token refresh.
// Each handler validates the request, delegates to the auth service, and writes a structured JSON response.

package auth
```

**Example — package doc, no blank line:**

```go
// Package auth provides user authentication and session management.
// It handles login, token validation, and logout for HTTP services.
package auth
```

---

## Exported symbol doc comments

All exported functions, types, methods, variables, and constants get a doc comment.

- Immediately before the declaration, no blank line between comment and code.
- Begin with the name of the symbol — Go's required self-reference, not a cross-reference violation.
- State what the symbol does **and** why it exists — not a restatement of the name.
- Methods: don't repeat the type name — the receiver is already part of the signature ("Delete removes this user," not "User deletes the user").
- Boolean-returning functions: "reports whether," not "returns true if" ("IsActive reports whether the account is active," not "...returns true if...").

**Bad — leans on another symbol to be readable (the rule this skill exists to catch):**

```go
// GetUser is like ListUsers but for a single ID, and skips the cache LoadSession uses.
func GetUser(id int) (*User, error) {
```

**Bad — sufficient on the "what," but padded with irrelevant remarks:**

```go
// GetUser retrieves a user by ID from the database.
// Originally written for the v1 login flow; also see the admin panel's user list for a related view.
func GetUser(id int) (*User, error) {
```

**Good — standalone, states what and why, nothing else:**

```go
// GetUser retrieves a user by ID from the database.
// It is used during login to load the user's profile and verify credentials.
// Returns an error if the user does not exist or the database query fails.
func GetUser(id int) (*User, error) {
```

---

## Types

Document what an instance of the type represents, not just its name.

- State the purpose and domain meaning of the type.
- Document the zero value if it's meaningful or behaves differently than a reader might expect.
- Document concurrency safety if the type is used concurrently ("safe for concurrent use" / "not safe for concurrent use without external synchronization").

**Example:**

```go
// User represents an authenticated user in the system.
// The zero User value is not valid and must not be used.
// User is safe for concurrent reads but not concurrent writes.
type User struct {
	ID    int
	Email string
}
```

---

## Constants and variables

- A group of related constants/variables gets one introductory comment explaining the group's purpose.
- An individual item gets a short comment on the line above it only when the name alone doesn't convey the meaning — never an end-of-line comment (see `code-quality`'s Comments section).

**Example:**

```go
// HTTP status codes used by the API.
const (
	// StatusOK indicates the request succeeded.
	StatusOK = 200
	// StatusBadReq indicates the request was malformed.
	StatusBadReq = 400
	// StatusNotFound indicates the resource does not exist.
	StatusNotFound = 404
)
```

---

## Interface implementations

Naming the stdlib interface being satisfied is the language-contract exception from `code-quality`'s Comments section — permitted.
Write a brief acknowledgment when the delegation isn't obvious;
write a full doc comment only when the implementation adds behavior beyond the interface contract.

**Example:**

```go
// Write implements io.Writer by forwarding to the underlying buffer.
func (b *Buffer) Write(p []byte) (int, error) {
	return b.buf.Write(p)
}
```

---

## Line-wrap style

Godoc collapses consecutive `//` comment lines into one rendered paragraph, the same way CommonMark does for markdown, so a semantic line break is invisible to a reader of the rendered doc.
See `prose`'s Line-breaks rule for the full rule.

**Bad — fixed-column wrap, breaking mid-phrase:**

```go
// LoadPortfolio reads every position file in dir and validates each one against the
// schema. It merges the valid files into a single Portfolio, and it returns an error
// if any file fails validation or two files declare the same position ID.
func LoadPortfolio(dir string) (*Portfolio, error) {
```

**Good — one sentence per line, breaking at the independent clause:**

```go
// LoadPortfolio reads every position file in dir and validates each one against the schema.
// It merges the valid files into a single Portfolio,
// and it returns an error if any file fails validation or two files declare the same position ID.
func LoadPortfolio(dir string) (*Portfolio, error) {
```

---

## Inline comments — restraint first

See `code-quality`'s Restraint-first rule — applied to Go below.

**Good — silent where the code is self-evident, one comment where the ordering constraint isn't:**

```go
func ProcessOrder(order *Order) error {
	// Check availability for every item before reserving any of them:
	// a partial reservation followed by a failure would leak stock with no rollback path.
	for _, item := range order.Items {
		if !warehouse.HasStock(item.ID) {
			return fmt.Errorf("item out of stock: %d", item.ID)
		}
	}

	for _, item := range order.Items {
		warehouse.DecrementStock(item.ID, item.Qty)
	}
	return nil
}
```

---

## Error handling

- Wrap with `fmt.Errorf("context: %w", err)` to preserve the error chain so callers can use `errors.Is`/`errors.Unwrap`.
  Naming `errors.Is`/`error` here is the stdlib-contract exception, not a violation.
- Comment why you're wrapping, per `code-quality`'s restraint rule — only when it's not self-evident.

**Example:**

```go
if err := db.Query(ctx, sql); err != nil {
	// Wrapped so callers upstream can errors.Is() this against ErrUserNotFound
	// without needing to know this call goes through the database layer at all.
	return fmt.Errorf("load user profile: %w", err)
}
```

---

## Prohibited patterns

See `code-quality`'s Prohibited list — commented-out code, edit-history comments, mechanical restatement, comparative rationale, end-of-line comments, and padding all apply here unchanged.
One Go-specific addition:

- **No `/* block comments */` inside function bodies.**
  Use `//` line comments only.
