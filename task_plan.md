# Task Plan: Nekode Self-Hosted Slock Bootstrap

## Goal
Create the first repo foundation for Nekode so the team can implement a hostable Slock-style collaboration server with a clear protocol, backend skeleton, frontend lane, and acceptance path.

## Phases
- [x] Phase 1: Clone and inspect repository
- [x] Phase 2: Import reusable protocol/design references
- [x] Phase 3: Bootstrap Go backend, health API, proto generation, Docker files
- [x] Phase 4: Add unit tests and run bootstrap verification
- [x] Phase 5: Reserve non-Web interaction endpoint extension points
- [ ] Phase 6: Coordinate frontend/product follow-up tasks
- [ ] Phase 7: Commit and push task #91 branch

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

## Errors Encountered
- Started bootstrap before the `plan-with-files` request arrived: recovered by adding this plan, notes, and deliverable docs before committing.

## Status
**Currently in Phase 7** - Bootstrap code and verification are complete; preparing commit and push.
