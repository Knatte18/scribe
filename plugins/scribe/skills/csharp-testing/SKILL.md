---
name: csharp-testing
description: Testing conventions for C#/.NET projects. Use when writing tests.
---

# Testing Skill

Swappable testing conventions for C#/.NET projects.
Replace or extend this file to match your test framework.

---

## General Principles

See `scribe:testing` for language-agnostic rules (assertion strictness, mock discipline, naming).

---

## Framework Configuration

> Replace this section with your chosen framework's conventions.

### Placeholder (xUnit example)

```csharp
// Test class naming: {ClassUnderTest}Tests
public class CsvExporterTests
{
    [Fact]
    public void ExportsAllColumnsForGivenDataSet()
    {
        // Arrange
        var exporter = new CsvExporter(delimiter: ",");

        // Act
        var result = exporter.Export(SampleData.CreateRows(count: 3));

        // Assert
        Assert.Equal(expected: 4, actual: result.Lines.Count); // header + 3 rows
    }
}
```

### Framework discovery

Detect the test framework from the test project's `*.csproj` file:
- **xUnit:** `<PackageReference Include="xunit" .../>` — uses `[Fact]`, `[Theory]`, `Assert.*`.
- **NUnit:** `<PackageReference Include="NUnit" .../>` — uses `[Test]`, `[TestCase]`, `Assert.*`.
- **MSTest:** `<PackageReference Include="MSTest.TestFramework" .../>` — uses `[TestMethod]`, `[DataRow]`, `Assert.*`.

Follow the conventions of whichever framework the project uses.
Do not mix frameworks within a test project.

### Conventions to specify per project

- Test framework: xUnit / NUnit / MSTest
- Assertion library: built-in / FluentAssertions / Shouldly
- Mocking library: Moq / NSubstitute / FakeItEasy (if needed)
- Test project naming convention
- Integration test setup (if applicable)

<!-- Project-specific testing configuration goes here -->
