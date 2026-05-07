# Task Plan: Nekode Self-Hosted Slock Bootstrap And Backend Phase 2

## Goal
Create the first repo foundation for Nekode so the team can implement a hostable Slock-style collaboration server with a clear protocol, backend skeleton, frontend lane, and acceptance path.

## Phases
- [x] Phase 1: Clone and inspect repository
- [x] Phase 2: Import reusable protocol/design references
- [x] Phase 3: Bootstrap Go backend, health API, proto generation, Docker files
- [x] Phase 4: Add unit tests and run bootstrap verification
- [x] Phase 5: Reserve non-Web interaction endpoint extension points
- [x] Phase 6: Coordinate frontend/product follow-up tasks
- [x] Phase 7: Commit and push task #91 branch
- [x] Phase 8: Implement SQLite storage and migrations
- [x] Phase 9: Implement auth/session service
- [x] Phase 10: Implement interaction endpoint, message, and task APIs
- [x] Phase 11: Verify, commit, and push task #94

## Key Questions
1. Should the reusable protobuf keep the old package path or use a project-local Nekode package?
2. What is the minimum backend surface that unblocks frontend and product work?
3. Which parts belong to task #91 versus task #92 and task #93?

## Decisions Made
- Use module `github.com/ca-x/nekode`: matches the GitHub repository and keeps import paths stable.
- Use proto package `nekode.daemon.v1`: keep field numbers/RPC semantics reusable while avoiding an old application name in new generated code.
- Start with standard library HTTP and minimal dependencies: leaves room to add framework/storage after the API shape is clearer.
- Put frontend design/implementation in task #92 and product/deploy acceptance in task #93: keeps #91 focused on repo and architecture foundations.
- Model Web, CLI, API, webhook, MCP, IM, mobile, IDE, and future clients as interaction endpoints instead of hardcoding Web as the only write surface.
- Use a first-user bootstrap endpoint instead of shipping default admin credentials.
- Use Ent ORM like Nekobot instead of hand-written SQL; support sqlite/postgres/mysql through db type and DSN.

## Errors Encountered
- Started bootstrap before the `plan-with-files` request arrived: recovered by adding this plan, notes, and deliverable docs before committing.
- Initial hand-written SQL storage was replaced with Ent ORM after review feedback; this is early enough that no data migration is needed.

## Status
**Task #94 complete** - Storage/auth/core API implementation is verified and ready for review.
