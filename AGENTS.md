# AI Agent Directives for Kynomesh

## Contribution Requirements

- **Require an Issue:** DO NOT create a Pull Request unless there is an existing
  GitHub Issue that explicitly requests this work.

- **Semantic PR Titles:** You must use Semantic Pull Request formatting for your
  PR title. Valid prefixes are:
  - `ci:` - Updates or improvements for the Continuous Integration workflows
  - `fix:` - Bug fixes
  - `feat:` - New features
  - `test:` - Addition of tests to the code base, or improvements of existing
    ones
  - `docs:` - Documentation improvements
  - `chore:` - Internals, build processes, unit tests, etc.
  - `refactor:` - Refactoring of the code base, without adding new features or
    fixing bugs
  - `revert:` - Reverts a previous commit

- **PR Templates:** You must fully complete the Pull Request template. Do not
  delete the template sections or leave them blank.

## Required Local Checks (Do This Before Committing)

Do not finalize your code or suggest a commit to your user without ensuring the
following `make` targets pass successfully:

1. **Build the Code:** `make build`
2. **Generate API Code & Manifests:** `make codegen` _(CRITICAL: Must be run if
   any API structs are changed)_
3. **Linting:** `make lint`
4. **Testing:** `make test`

If any of these commands fail, you must fix the errors before proceeding.

## Documentation (`docs/`)

If you are modifying or adding a feature, you must also update the corresponding
documentation.

- Write in clear, direct English.
- Use GitHub style admonition blocks (e.g., `> [!NOTE]`, `> [!WARNING]`)
  compatible with MkDocs Material.
- Code examples in documentation must be complete, accurate, and include the
  language identifier for syntax highlighting (e.g., ````yaml`).

## Summary of Agent Workflow

1. Verify an open issue exists.
2. Write code matching Kynomesh's Go standards.
3. Run `make codegen`, `make lint`, and `make test`.
4. Format the PR title properly (e.g.,
   `fix: race conditions when running multiple pipelines (#12345)`).
