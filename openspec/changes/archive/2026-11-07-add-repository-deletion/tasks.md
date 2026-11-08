# Tasks

## 1. Authorization level and policy contract

- [x] 1.1 Add `AccessAdmin` above `AccessWrite`, accept `"admin"` in `Access.UnmarshalJSON`, return `"admin"` from `Access.String`, add `OpDelete`, compute `CanCreate` as `best >= AccessWrite` and `CanDelete` as `best >= AccessAdmin`, and allow `OpDelete` only for `AccessAdmin` in `Decide`; verify `go test ./internal/authz`.
- [x] 1.2 Rewrite the authz tests for the new contract: use a different unknown access value instead of `"admin"` in `TestAccessRejectsUnknownValue`, use a different unknown operation instead of `Op("delete")` in `TestDecide`, add admin read/write/create/delete cases and `CanDelete` assertions, add `"admin"` to `TestAccessParsesDocumentedValues`, and assert the public identity cannot delete; verify `go test ./internal/authz`.
- [x] 1.3 Change `identity.Development()` to `AccessAdmin`, replace `"admin"` with another unknown value in `TestResolveUnknownAccessGrantsNothing`, add a resolve case where `admin` yields `AccessAdmin` and `CanDelete`, update `TestDevelopmentIdentityIsWriter` to assert admin and delete, and assert the Funnel principal cannot delete; verify `go test ./internal/identity`.
- [x] 1.4 Update `docs/access-control.md` with the `admin` row, the `read < write < admin` hierarchy, the delete authority, an admin entry in both the fixture and the complete-grant example, and the fail-closed downgrade note; update `TestDocumentedCapabilityFixture` for the new fixture and assert admin deletes while write does not; verify `go test ./internal/authz`.
- [x] 1.5 Update the authorization section of `docs/architecture.md` to describe the `admin` level, `OpDelete`, and the public principal's exclusion; verify by reading the section against `internal/authz/authz.go`.
- [x] 1.6 Add the admin policy fragment to `docs/deployment.md`, using the same payload shape as `docs/access-control.md`; verify the JSON parses with `jq .` and names `github.com/kevindurb/tsgit/cap/repo` with `"access": "admin"`.

## 2. Store deletion

- [x] 2.1 Add a `sync.Mutex` to `repos.Store` and hold it in `Create` from the existence check through the final rename; verify `TestCreateConcurrent` and `go test ./internal/repos`.
- [x] 2.2 Implement `Store.Delete(ctx, name)` with `ParseName`, `Resolve`, an `Lstat` existence check, symlink and non-directory refusal, a locked `MkdirTemp` + `Rename` into `.tsgit-tmp/delete-*`, an unlock before `RemoveAll`, an `ErrCleanup` wrapper when removal fails, and best-effort namespace cleanup; verify with a new `TestDelete` that asserts the directory and objects are gone.
- [x] 2.3 Add store tests for a missing repository (`ErrNotExist`), a symlinked repository directory (refused, link and target unchanged), isolation when another repository shares the namespace, empty-namespace cleanup, and recreation after deletion; verify `go test ./internal/repos`.
- [x] 2.4 Add a concurrent `Create`/`Delete` test that runs both against the same name and asserts a coherent outcome with no staging debris; verify `go test ./internal/repos -run 'Delete|Create'`.
- [x] 2.5 Update the storage section of `docs/architecture.md` with the delete-to-`.tsgit-tmp` sequence, the shared mutex, and the in-flight transfer behavior; verify `TestDocumentedStorageLayout` and a manual read against `internal/repos/repos.go`.

## 3. Delete web surface and wiring

- [x] 3.1 Add `web.DeleteHandler` with `Matches`, `NewDeleteHandler`, the `/.tsgit/delete/<ns>/<repo>` route, a dedicated `delete.html` template, and shared render/security helpers extracted from the existing handler; verify a `GET` returns `200` with the canonical name and the permanence warning.
- [x] 3.2 Add the signed token helper (node, repo, action, expiry) and the `Origin` / `Sec-Fetch-Site` checks; verify tests for a cross-site `POST` returning `403` with no deletion, a missing or expired token returning `403`, and a same-origin valid `POST` returning `303`.
- [x] 3.3 Add the typed canonical-name check, the 64 KiB body limit, and the `Store.Delete` call; verify tests for a matching name (repository gone, `303`), a mismatch (`400`, repository still exists), a missing repository (`404`), a non-admin (`404`), an identity failure (`503`), and an unsupported method (`405`).
- [x] 3.4 Add `Deletable` to `repoView`, render the Delete link in `repo.html` only for admins, and render a post-delete notice from a validated `?deleted=` value on the index; verify the summary test shows the link for an admin but not a writer, and the index test shows the notice.
- [x] 3.5 Wire the handler: add the optional delete handler to `front`, generate the CSRF key once in `cmd/tsgit`, thread it through `serveDev`, `serveTailnet`, `serveTLS`, `newTailnetHandler`, and `composeTailnet`, and pass `nil` from `composePublic`; update the `front` and `composeTailnet` test calls; verify `go test ./cmd/tsgit`.
- [x] 3.6 Update `README.md` with the admin grant example, the "Delete a repository" flow, the permanence warning, and the client-side rename recipe; update the scope paragraph in `docs/project.md`; verify the documented delete flow against the development server.
- [x] 3.7 Add the delete route to the HTTP surface table and the layer notes in `docs/architecture.md`, and state that the public composition does not mount it; verify the table against the route list in `cmd/tsgit` and `internal/web`.

## 4. Integration checks

- [x] 4.1 Run `just ci` and `just spec-validate`; verify both pass.
- [x] 4.2 Run the development server with `--dev-listen`, create and push a repository, open its summary, confirm the Delete link, delete it with the typed name, and verify the index no longer lists it, the clone URL returns 404, and the log records the deletion.
- [x] 4.3 Confirm the Funnel composition passes `nil` for the delete handler and that a public request to `/.tsgit/delete/...` returns 404; verify with the public composition test.
- [x] 4.4 Review the docs for the fail-closed downgrade note, the narrow admin grant, and the rename recipe; verify the mirror-push recipe on a scratch repository in development mode.
