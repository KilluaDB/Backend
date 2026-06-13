# Testing Gaps — Priority Areas

## Overall coverage: 58.7%

We are writing Go tests for this backend project. Follow existing test patterns in each package:

- Use `pgxmock` for pool mocking, `mocks.NewUserStore` / `mocks.NewProjectStore` for stores
- Use `testutil.NewGinContext`, `testutil.SetupJWTSecrets`, `testutil.BearerToken` in handler tests
- All tests are deterministic, no real DB connections. Use `t.Setenv` for env vars.
- Do NOT test route registration files (0% is expected). Do NOT test `cmd/` packages.
- Do NOT modify production code — only add test files or add tests to existing `_test.go` files.

---

## 1. `internal/handler/auth_handler.go` — Auth handler paths

Current coverage: Register 86.7%, Login 71.4%, Logout 80%, Refresh 100%

Add tests for **Login** uncovered path:
- Login with valid credentials but refreshStore.Set fails → expected 500 or error response
- Login when FindUserByEmail returns an error (not nil user) → expected 500 or error response

Add tests for **Logout** uncovered path:
- Logout with valid user but refreshStore.Delete fails → expected 500 or error response

Check existing `auth_handler_test.go` for test helpers and patterns.

---

## 2. `internal/handler/google_auth_handler.go` — Google auth handler

Current coverage: Login 83.3%, Callback 84.6%

Add tests for **Callback** uncovered path:
- Successful callback (mock OAuth exchange + state validation) → 200
- Missing or invalid `state` cookie → 401
- OAuth2 exchange error → 400 or 500
- No `code` query param → 400

Check `google_auth_handler_test.go` for existing `stubOAuthConfig` and patterns.

---

## 3. `internal/handler/project_handler.go` — Project handler

Current coverage: Create 70.4%, GetProject 85.7%, ListProjects 69.2%, GetProjectAccess 73.3%, DeleteProject 71.4%

Add tests for uncovered paths:
- CreateProject with validation errors (missing name, invalid db_type, etc.) → 400
- CreateProject when service.CreateProject returns error → 500
- ListProjects when service.GetProjectsByUserID returns error → 500
- DeleteProject when service.DeleteProjectByIDAndUserID returns error → 500
- GetProjectAccess when service.GetExternalConnectionInfo returns error → 500

Check `project_handler_test.go` for patterns.

---

## 4. `internal/service/project_service.go` — Project service

Current coverage: Create 92%, provisionInstanceAsync 58.3%, GetProjectByID 75%, GetProjectByIDAndUserID 71.4%, GetProjectsByUserID 80%, DeleteProjectByIDAndUserID 65%, GetExternalConnectionInfo 71.4%

Add tests for uncovered paths:
- provisionInstanceAsync with provisioner.CreateInstance error → error logged, project status set to "failed"
- GetProjectByID when project not found → return nil
- GetProjectByIDAndUserID with both found and not-found cases
- DeleteProjectByIDAndUserID when beginTx fails → error
- DeleteProjectByIDAndUserID when project delete fails → rollback

Check `project_service_test.go` for patterns (uses pgxmock for tx, mocks.NewProjectStore).

---

## 5. `internal/service/auth_service.go` — Auth service

Current coverage: Register 72.4%, Login 82.4%, Logout 66.7%, Refresh 80%

Add tests for uncovered paths:
- Register: password hash error (e.g. very long password) → error returned
- Login: FindUserByEmail returns DB error (not nil) → ErrUserNotFound
- Logout: nil refreshStore → no error returned (nil guard)
- Refresh: FindUserByID returns error → "user not found"

Check `auth_service_test.go` for failingRefreshStore and other helpers.

---

## 6. `internal/mongodb/service/collection_service.go` — MongoDB collection service

Current coverage: 10% (only validateCollectionName and validateFieldName at 100%)

Nearly all service methods are uncovered (0%):
- NewCollectionService
- ListCollections — stub the connection manager, return mock cursor
- CreateCollection — stub CreateCollection on repo, test success and error
- DeleteCollection — stub DropCollection, test success and error
- AddField — stub AddFieldToDocuments, test success and error
- RemoveField — stub RemoveFieldFromDocuments, test success and error

The service uses `MongoConnectionManager` (from `mongodb/infra`). Use a mock `MongoConnectionManager` that returns a mock `*mongo.Database` / `*mongo.Collection`. For Mongo mocking, use `github.com/ygpark2/mgo` or define a local interface. Check existing `mongodb/service/collection_service_test.go` if it exists.

---

## 7. `internal/postgres/handler/table_handler.go` — Table handler uncovered paths

Many methods below 70%:
- CreateTable (56%), DeleteTable (52%), UpdateTable (57.7%), AddColumn (60%), DropColumn (61.1%), ListIndexes (58.8%), CreateIndex (52%), DropIndex (59.1%)
- parseFilterForDeleteRows (50%)

For each, test the JSON binding error cases (malformed body → 400), instance errors, and validation errors. Check `table_handler_test.go` for existing patterns with `pgxmock` and `requireUserAndProject`.

---

## 8. `internal/postgres/service/table_service_ddl.go` — Table DDL validateCreateTableRequest

Current coverage: 47.6%

Add tests for remaining validation paths:
- Invalid schema name → error
- Invalid column name → error  
- Invalid column type → error
- Duplicate column names → error
- Missing primary key → error
- Invalid foreign key reference → error

Check `table_service_ddl_test.go` for patterns.

---

## 9. `internal/utils/crypto.go` — Crypto utility

Current coverage: EncryptString 78.6%, DecryptString 68.4%, GeneratePasswordBase64 83.3%

Add tests for uncovered paths:
- DecryptString with invalid base64 → error
- DecryptString with ciphertext too short → error
- EncryptString / DecryptString with empty plaintext → succeeds (roundtrip empty string)
- GeneratePasswordBase64 with negative numBytes → defaults to 32

Check `crypto_test.go` for patterns.

---

## 10. `internal/service/user_service.go` — User service

Current coverage: GetUser 85.7%, UpdateUser 82.6%, DeleteUser 93.1%, GetAllUsers 83.3%, beginTx 66.7%

Add tests for uncovered paths:
- UpdateUser: authenticated user not found → error
- UpdateUser: repo.Update fails → error
- GetAllUsers: FindAll returns error → error
- GetAllUsers: clears password hash (verify by inspecting returned users)
- beginTx: with txPool set → uses txPool (already tested in delete tests)

Check `user_handler_delete_test.go` and `user_service_test.go` for patterns.

---

## Running tests

```bash
# Single package
go test ./internal/handler/ -v -count=1 -run TestName

# Coverage for a package
go test ./internal/handler/ -coverprofile=coverage.out -count=1
go tool cover -func=coverage.out | grep -v "100.0%"

# Full suite
go test ./... -count=1 2>&1 | grep -E "FAIL|ok"

# Full coverage
go test $(go list ./... | grep -v mongodb/handler) -coverprofile=coverage.out -count=1
go tool cover -func=coverage.out | grep -v "100.0%"
```
