---
name: csharp-comments
description: XML doc and inline comment rules for C#/.NET. Use when writing C# comments.
---

# Comments and Documentation Skill

**Load `scribe:code-quality` first — its Comments section is the language-agnostic base.**

Guidelines for code comments and XML documentation in C#/.NET.

---

## File header

See `scribe:code-quality`'s Comments section for the file-header rule and its sole-file exception.
In C#, the header is a `///` comment block placed above the `using` statements and the `namespace` declaration.

**Example:**

```csharp
/// OrderProcessor.cs validates, prices, and persists orders for the checkout flow.
/// Each public method corresponds to one stage of the checkout pipeline.

using System;

namespace Checkout
{
```

## XML documentation

- All `public` methods and classes **must** have `/// <summary>` XML doc comments.

## Interface implementations — use `<inheritdoc/>`, never duplicate

- When a class implements an interface member, **never repeat** the interface's `/// <summary>` on the implementation.
- **Always write `/// <inheritdoc/>`** on the implementation.
  This makes the inheritance explicit and signals to future readers (and to Claude) that the doc lives on the interface and no new docstring is needed here.
- Only write a fresh `/// <summary>` on an implementation when it adds information beyond the interface contract, or when the member has no interface counterpart.

## Line-wrap style

XML-doc tooling collapses consecutive `///` comment lines into one rendered paragraph, the same way CommonMark does for markdown, so a semantic line break is invisible to a reader of the rendered doc.
See `prose`'s Line-breaks rule for the full rule.

**Bad example:**

```csharp
/// <summary>
/// Validates the incoming order against the pricing catalog and applies any active
/// discount codes. It then persists the finalized order, and it returns the
/// confirmation number the caller displays to the customer.
/// </summary>
public string ProcessOrder(Order order) {
```

**Good example:**

```csharp
/// <summary>
/// Validates the incoming order against the pricing catalog and applies any active discount codes.
/// It then persists the finalized order,
/// and it returns the confirmation number the caller displays to the customer.
/// </summary>
public string ProcessOrder(Order order) {
```
