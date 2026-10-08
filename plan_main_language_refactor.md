# Language-aware evaluation plan

## Goal

Add a per-run prompt language option to candy, fingerprint, ModelTrace, and
pelican evaluations. Chinese remains the default; English makes every prompt
and analysis input English while leaving the management UI language unchanged.

## Steps

- [x] Define shared language validation/defaults and add language to run payloads
  and persisted result records.
- [x] Add English prompt/challenge sources and language-specific fingerprint
  probe selection and baseline comparison.
- [x] Wire language selectors into the existing UI without translating UI copy.
- [x] Update tests for defaults, payload propagation, English prompt paths, and
  persistence; run gofmt, go vet, go test -race, and JavaScript syntax checks.
- [x] Commit only verified relevant files.
