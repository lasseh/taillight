# Taillight

## Auto-commit
After completing a task or feature, always run /commit before stopping.

## Go Error Handling
Never silence unchecked errors with `defer func() { _ = err }()` wrappers.
If the error matters, handle it (log or return). If it truly doesn't matter (e.g., `defer resp.Body.Close()`), use `//nolint:errcheck` with a reason.
