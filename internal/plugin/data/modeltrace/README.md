# ModelTrace

Source: https://github.com/xqy2006/ModelTrace

Revision: `df3a0f9d3e054c0dc02d6d586686db8daf8fa7c8`

`unified_bank.json` is the unmodified upstream unified bank (16 models).
The scoring algorithm and Chinese challenge generator in
`../../modeltrace_algorithm.go` and `../../modeltrace.go` are Go ports of
upstream `static/fingerprint-core.js` and `static/challenge-browser.js`.
The upstream MIT license is included in this directory and embedded in the
plugin. No upstream runtime or external service is required.

Probabilities describe only candidates in this bank. They do not prove model
identity and may be misleading for models absent from the bank.
