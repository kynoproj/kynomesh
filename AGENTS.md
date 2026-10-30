# AI Agent Directives for Kynomesh

## Contribution Requirements

- **Require an Issue:** DO NOT create a Pull Request unless there is an existing
  GitHub Issue that explicitly requests this work.

- **Be brief.** Code comments, commit messages, PR descriptions and issues
  should be short, plain English, and read the way an experienced maintainer
  writes by hand. Reviewer time and agent context windows are limited.
  - Write short, direct sentences with one idea each, subject first.
  - Use the real identifiers from the code. Don't coin new names, and avoid noun
    stacks like "the store-wide pin-lock pre-flight".
  - Prefer verbs to abstract nouns: "evicts" instead of "performs eviction".
  - Say each thing once. Don't restate the diff, open with "This PR …" or "This
    commit …", or repeat the same explanation in several comments.
  - Explain why only when the code doesn't make it obvious, in a clause or a
    line.
  - Avoid:
    - em-dashes, and chains of semicolons
    - headings, bold, or test plans in commit messages
    - epigrams and contrast pairs, like "X, not Y" or "So treat ENOENT as what
      it is."
    - numbers added for color, like "measured at a flat 51ms"
    - giving code motives or editorializing about it, like "wants", "decides",
      "is responsible for", "lands", "owns", or "X is what makes Y". Plain
      active voice is fine: "the reaper collects" is how Go doc comments read.
    - filler words: load-bearing, footgun, surfaced, robust, comprehensive,
      seamless, leverage
    - "No behavior change" unless the diff is a pure refactor
    - long sentences that pile up clauses, or put the subject after a long
      lead-in

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
