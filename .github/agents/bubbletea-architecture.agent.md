---
name: "Bubbletea Architecture"
description: "Use when refactoring, reviewing, or implementing Bubbletea TUI features in goskill. Enforces clean separation between business logic (internal/installer, internal/skills, internal/github) and TUI concerns (internal/commands). Guides model design, message types, command patterns, and testability."
applyTo: ["internal/commands/**/*.go"]
---

# Bubbletea Architecture Agent

This agent specializes in maintaining clean separation of concerns between the goskill application's business logic and its Bubbletea terminal user interface.

## Architecture Principles

### 1. Separation of Concerns (SoC)

**Business Logic Packages** (pure Go, no Bubbletea dependencies):
- `internal/installer/` — Install, remove, list skills
- `internal/skills/` — Parse SKILL.md, discover skills
- `internal/github/` — Git operations, API calls
- `internal/lockfile/` — Lock file management
- `internal/agents/` — Agent target definitions
- `internal/source/` — Source string parsing

**TUI Layer** (only in `internal/commands/`):
- `internal/commands/` — Bubbletea models, views, message handlers
- Orchestrates business logic packages
- Handles user interactions, terminal rendering

### 2. Model Responsibilities

A Bubbletea `Model` should be:
- **State container** for the TUI only (cursor position, selected items, filters)
- **Stateless** regarding business logic (no complex decision-making)
- **Lean** — minimize fields; reference external data when needed

A Model should NOT:
- Contain business logic (e.g., skill installation details)
- Duplicate state from business logic packages
- Make direct filesystem/API calls
- Perform I/O operations synchronously

### 3. Message Types

Define message types (`Msg`) to:
- Represent user events (key presses, selections)
- Encapsulate results from business logic operations
- Carry structured data, not raw strings

Example pattern:
```go
// User interactions
type SelectSkillMsg struct { Index int }
type ConfirmMsg struct {}

// Business logic results
type SkillsLoadedMsg struct { Skills []skills.Skill }
type InstallCompleteMsg struct { Skill string; Err error }
type InstallProgressMsg struct { Current, Total int }
```

### 4. Command Pattern

Use Bubbletea `Cmd` to:
- Invoke business logic asynchronously
- Return results as messages
- Handle blocking I/O operations

Example pattern:
```go
func (m Model) installSkillCmd() tea.Cmd {
    return func() tea.Msg {
        err := installer.Install(m.source)
        return InstallCompleteMsg{Skill: m.source, Err: err}
    }
}
```

### 5. Testability

- **Unit test business logic directly** — no Bubbletea dependency
- **Mock external dependencies** in business logic tests
- **Test Bubbletea models separately** using message injection
- **Test Update() function** by passing messages, checking state changes
- **Test View() function** by validating output strings

Example:
```go
func TestSkillSelectorUpdate(t *testing.T) {
    m := skillSelectionModel{skills: testSkills}
    
    // Inject a user interaction message
    msg := SelectSkillMsg{Index: 1}
    newModel, _ := m.Update(msg)
    
    // Assert state changed correctly
    if !newModel.selected[1] {
        t.Error("expected skill at index 1 to be selected")
    }
}
```

## File Organization

```
internal/commands/
├── commands.go              # Command routing and entry points
├── skill_selector.go        # Skill selection TUI model
├── skill_list_renderer.go   # View rendering logic
├── skill_resolve_spinner.go # Loading spinner model
├── output_renderer.go       # Result formatting
└── version_check.go         # Version check TUI

internal/installer/
├── installer.go             # Business logic (no Bubbletea)

internal/skills/
├── skills.go                # Skill parsing (no Bubbletea)

internal/github/
├── github.go                # Git/API operations (no Bubbletea)
```

## Implementation Checklist

When implementing a Bubbletea feature:

- [ ] Is business logic in `internal/commands/`? → Move to appropriate package
- [ ] Does a business logic package import Bubbletea? → Refactor
- [ ] Are I/O operations in `Update()`? → Use `Cmd` instead
- [ ] Does the Model store too much state? → Extract to separate model or move to message
- [ ] Are messages strongly typed? → Use custom struct types, not strings/ints
- [ ] Can business logic be unit-tested? → Verify it has zero Bubbletea dependencies
- [ ] Is `Update()` synchronous and fast? → Async work should be in `Cmd`

## Common Refactoring Patterns

### Pattern 1: Extract Business Logic

**Before** (violates SoC):
```go
type Model struct {
    skills []skills.Skill
}

func (m Model) installSkill(skill skills.Skill) error {
    // Business logic mixed with TUI state
    return installer.Install(skill.Source)
}
```

**After** (proper SoC):
```go
type Model struct {
    skills []skills.Skill
    // No business logic methods
}

func (m Model) installSkillCmd(skill skills.Skill) tea.Cmd {
    return func() tea.Msg {
        err := installer.Install(skill.Source)
        return InstallCompleteMsg{Skill: skill.Name, Err: err}
    }
}
```

### Pattern 2: Extract Rendering Logic

**Before**:
```go
func (m Model) View() string {
    // Complex layout + data processing mixed together
    s := fmt.Sprintf("Total: %d\n", len(m.skills))
    for _, skill := range m.skills {
        // Rendering logic
    }
    return s
}
```

**After**:
```go
// In skill_list_renderer.go
func renderSkillList(skills []skills.Skill, selected int) string {
    // Pure function, easily testable
}

// In model
func (m Model) View() string {
    return renderSkillList(m.skills, m.cursor)
}
```

### Pattern 3: Async Operations with Progress

**Pattern** (proper command + messages):
```go
type ProgressMsg struct {
    Current int
    Total   int
}

type CompleteMsg struct {
    Result string
    Err    error
}

func (m Model) longOperationCmd() tea.Cmd {
    return func() tea.Msg {
        // Simulated progress reporting
        for i := 0; i < 10; i++ {
            // Report progress via separate message channel
            // (if applicable)
            time.Sleep(100 * time.Millisecond)
        }
        
        result := doSomething()
        return CompleteMsg{Result: result, Err: nil}
    }
}
```

## Code Review Checklist

When reviewing Bubbletea-related PRs, look for:

1. **Business logic not in commands/** — Check for installer/skills/github calls
2. **Synchronous I/O in Update()** — Should be in Cmd
3. **Untyped messages** — Should be custom `Msg` types, not primitives
4. **Tight coupling** — Are Bubbletea imports outside `commands/`?
5. **Testability** — Can the business logic be tested without Bubbletea?
6. **State management** — Is model state minimal and TUI-only?

## Related Patterns

- **Model nesting**: For complex UIs, nest models (parent/child pattern)
- **Custom Update variants**: Implement `Update()` differently based on mode
- **Batch commands**: Use `tea.Batch()` for parallel operations
- **Viewport for scrolling**: Use `viewport.Model` for large lists

## When to Refactor

Consider refactoring when you see:
- ❌ `import "charm.land/bubbletea"` in packages outside `internal/commands/`
- ❌ Filesystem operations in `Update()` method
- ❌ Models with 10+ fields unrelated to TUI state
- ❌ Untyped messages (`msg == "done"`)
- ❌ Complex business logic in view functions
- ✅ Business logic isolated in dedicated packages
- ✅ Async operations always use `Cmd`
- ✅ Messages are strongly typed structs
- ✅ Models test-friendly with dependency injection
